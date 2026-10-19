// dnsproxy.go — پاسخ‌گوی DNS داخل VPN: پرس‌وجوهای UDP گوشی را از داخل
// همان تونل (DNS-over-TCP به upstream، پیش‌فرض 1.1.1.1:53) می‌فرستد.
// یک اتصال تونل پایدار با pipelining (چند پرس‌وجو هم‌زمان، تطبیق با ID)
// + کش کوتاه‌مدت تا باز شدن سایت‌ها سریع باشد.
package client

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

const (
	dnsQueryTimeout = 6 * time.Second
	dnsCacheTTL     = 90 * time.Second
	dnsCacheMax     = 2048
)

var errDNSClosed = errors.New("dns: closed")

type dnsCacheEntry struct {
	resp    []byte
	expires time.Time
}

type tunnelDNS struct {
	dial      func(target string) (net.Conn, error)
	upstream  string
	blockAAAA bool

	mu      sync.Mutex
	conn    net.Conn
	pending map[uint16]chan []byte
	nextID  uint16
	closed  bool

	cacheMu sync.Mutex
	cache   map[string]dnsCacheEntry
}

func newTunnelDNS(dial func(string) (net.Conn, error), upstream string, blockAAAA bool) *tunnelDNS {
	if upstream == "" {
		upstream = "1.1.1.1:53"
	}
	return &tunnelDNS{
		dial: dial, upstream: upstream, blockAAAA: blockAAAA,
		pending: map[uint16]chan []byte{},
		cache:   map[string]dnsCacheEntry{},
	}
}

func (d *tunnelDNS) Close() {
	d.mu.Lock()
	d.closed = true
	if d.conn != nil {
		d.conn.Close()
		d.conn = nil
	}
	d.mu.Unlock()
}

// Query یک پیام DNS خام (UDP) می‌گیرد و پاسخ خام برمی‌گرداند
func (d *tunnelDNS) Query(q []byte) ([]byte, error) {
	if len(q) < 12 {
		return nil, errors.New("dns: short query")
	}
	origID := binary.BigEndian.Uint16(q[0:2])
	key, qtype, ok := dnsQuestionKey(q)
	if ok && d.blockAAAA && qtype == 28 {
		return dnsEmptyAnswer(q), nil
	}
	if ok {
		if resp := d.cacheGet(key); resp != nil {
			binary.BigEndian.PutUint16(resp[0:2], origID)
			return resp, nil
		}
	}
	var resp []byte
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		resp, err = d.roundTrip(q)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint16(resp[0:2], origID)
	if ok && dnsRcode(resp) != 2 { // SERVFAIL کش نمی‌شود
		d.cachePut(key, resp)
	}
	return resp, nil
}

func (d *tunnelDNS) roundTrip(q []byte) ([]byte, error) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil, errDNSClosed
	}
	if d.conn == nil {
		c, err := d.dial(d.upstream)
		if err != nil {
			d.mu.Unlock()
			return nil, err
		}
		d.conn = c
		go d.readLoop(c)
	}
	conn := d.conn
	d.nextID++
	id := d.nextID
	ch := make(chan []byte, 1)
	d.pending[id] = ch

	msg := make([]byte, 2+len(q))
	binary.BigEndian.PutUint16(msg[0:2], uint16(len(q)))
	copy(msg[2:], q)
	binary.BigEndian.PutUint16(msg[2:4], id)
	_, err := conn.Write(msg)
	if err != nil {
		delete(d.pending, id)
		d.dropConnLocked(conn)
		d.mu.Unlock()
		return nil, err
	}
	d.mu.Unlock()

	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, errDNSClosed
		}
		return resp, nil
	case <-time.After(dnsQueryTimeout):
		d.mu.Lock()
		delete(d.pending, id)
		d.dropConnLocked(conn) // اتصال گیرکرده — دفعه‌ی بعد تازه بساز
		d.mu.Unlock()
		return nil, errors.New("dns: timeout")
	}
}

func (d *tunnelDNS) readLoop(c net.Conn) {
	head := make([]byte, 2)
	for {
		if _, err := io.ReadFull(c, head); err != nil {
			break
		}
		l := int(binary.BigEndian.Uint16(head))
		resp := make([]byte, l)
		if _, err := io.ReadFull(c, resp); err != nil {
			break
		}
		if l < 12 {
			continue
		}
		id := binary.BigEndian.Uint16(resp[0:2])
		d.mu.Lock()
		ch := d.pending[id]
		delete(d.pending, id)
		d.mu.Unlock()
		if ch != nil {
			ch <- resp
		}
	}
	d.mu.Lock()
	d.dropConnLocked(c)
	d.mu.Unlock()
}

// dropConnLocked بستن اتصال و شکست دادن پرس‌وجوهای در انتظار آن
func (d *tunnelDNS) dropConnLocked(c net.Conn) {
	if d.conn != c {
		return
	}
	c.Close()
	d.conn = nil
	for id, ch := range d.pending {
		close(ch)
		delete(d.pending, id)
	}
}

func (d *tunnelDNS) cacheGet(key string) []byte {
	d.cacheMu.Lock()
	defer d.cacheMu.Unlock()
	e, ok := d.cache[key]
	if !ok || time.Now().After(e.expires) {
		return nil
	}
	return append([]byte(nil), e.resp...)
}

func (d *tunnelDNS) cachePut(key string, resp []byte) {
	d.cacheMu.Lock()
	defer d.cacheMu.Unlock()
	if len(d.cache) >= dnsCacheMax {
		d.cache = map[string]dnsCacheEntry{}
	}
	d.cache[key] = dnsCacheEntry{resp: append([]byte(nil), resp...), expires: time.Now().Add(dnsCacheTTL)}
}

// dnsQuestionKey کلید کش = بخش question (نام + نوع + کلاس) بدون ID
func dnsQuestionKey(q []byte) (key string, qtype uint16, ok bool) {
	if binary.BigEndian.Uint16(q[4:6]) != 1 { // QDCOUNT
		return "", 0, false
	}
	i := 12
	for i < len(q) {
		l := int(q[i])
		if l == 0 {
			i++
			break
		}
		if l&0xC0 != 0 {
			return "", 0, false
		}
		i += 1 + l
	}
	if i+4 > len(q) {
		return "", 0, false
	}
	qtype = binary.BigEndian.Uint16(q[i : i+2])
	return string(q[12 : i+4]), qtype, true
}

// dnsEmptyAnswer پاسخ NOERROR بدون رکورد (برای AAAA)
func dnsEmptyAnswer(q []byte) []byte {
	key, _, _ := dnsQuestionKey(q)
	resp := make([]byte, 12+len(key))
	copy(resp[0:2], q[0:2])
	resp[2] = 0x80 | (q[2] & 0x79) // QR=1، opcode و RD از پرس‌وجو
	resp[3] = 0x80                 // RA=1, RCODE=0
	binary.BigEndian.PutUint16(resp[4:6], 1)
	copy(resp[12:], key)
	return resp
}

func dnsRcode(resp []byte) byte {
	if len(resp) < 4 {
		return 2
	}
	return resp[3] & 0x0F
}

func deadlineIn(d time.Duration) time.Time { return time.Now().Add(d) }
