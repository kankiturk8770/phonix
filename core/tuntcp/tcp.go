// Package tuntcp — پشته TCP/IPv4 فشرده برای TUN (بدون وابستگی خارجی)
// جریان: اپ اندروید → TUN → این پشته → SOCKS/tunnel → اینترنت
// پشتیبانی: SYN/SYN-ACK/ACK، داده PSH با مرتب‌سازی seq، FIN، RST، checksum.
// طراحی minimal اما صحیح — برای حجم ترافیک موبایل کافی است.
package tuntcp

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	mss       = 1200
	winSize   = 65535
	quadFmt   = "%s:%d>%s:%d"
)

// Quad یک جریان TCP را مشخص می‌کند (از دید گوشی: src=گوشی، dst=اینترنت)
type Quad struct {
	SrcIP, DstIP     [4]byte
	SrcPort, DstPort uint16
}

func (q Quad) String() string {
	return fmt.Sprintf(quadFmt, ipStr(q.SrcIP), q.SrcPort, ipStr(q.DstIP), q.DstPort)
}

func ipStr(b [4]byte) string { return fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3]) }

// Stack پشته TCP روی یک TUN
type Stack struct {
	mu       sync.Mutex
	conns    map[Quad]*Conn
	nextISS  uint32
	onAccept func(c *Conn) // برای هر SYN جدید صدا زده می‌شود
	out      chan []byte   // پکت‌های خروجی به TUN
}

// NewStack ساخت پشته؛ onAccept برای اتصال‌های جدید (باید goroutine بسازد)
func NewStack(onAccept func(c *Conn)) *Stack {
	return &Stack{
		conns:    map[Quad]*Conn{},
		nextISS:  uint32(time.Now().UnixNano()),
		onAccept: onAccept,
		out:      make(chan []byte, 1024),
	}
}

// Out کانال پکت‌های خروجی به سمت TUN
func (s *Stack) Out() <-chan []byte { return s.out }

// Deliver یک پکت IP خام از TUN می‌گیرد؛ پاسخ‌ها (اگر هست) برمی‌گرداند
func (s *Stack) Deliver(pkt []byte) [][]byte {
	if len(pkt) < 20 || pkt[0]>>4 != 4 {
		return nil
	}
	ihl := int(pkt[0]&0x0F) * 4
	if len(pkt) < ihl+20 {
		return nil
	}
	if !ipOK(pkt[:ihl]) {
		return nil
	}
	src := [4]byte{pkt[12], pkt[13], pkt[14], pkt[15]}
	dst := [4]byte{pkt[16], pkt[17], pkt[18], pkt[19]}
	tcp := pkt[ihl:]
	if !tcpOK(src, dst, tcp) {
		return nil
	}
	q := Quad{
		SrcIP: src, DstIP: dst,
		SrcPort: binary.BigEndian.Uint16(tcp[0:2]),
		DstPort: binary.BigEndian.Uint16(tcp[2:4]),
	}
	seq := binary.BigEndian.Uint32(tcp[4:8])
	off := int(tcp[12]>>4) * 4
	flags := tcp[13]
	payload := tcp[off:]

	s.mu.Lock()
	c := s.conns[q]
	if flags&0x02 != 0 && flags&0x10 == 0 { // SYN
		if c != nil {
			s.mu.Unlock()
			return nil
		}
		c = newConn(s, q, seq)
		s.conns[q] = c
		s.mu.Unlock()
		synack := c.reply(0x12, nil, seq+1) // SYN+ACK
		if s.onAccept != nil {
			go s.onAccept(c)
		}
		return [][]byte{synack}
	}
	if c == nil {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	switch {
	case flags&0x04 != 0: // RST
		c.remoteClose()
		s.drop(q)
		return nil
	case flags&0x01 != 0 && len(payload) == 0: // FIN
		c.remoteFin()
		s.drop(q)
		return [][]byte{c.ackPkt()}
	case flags&0x10 != 0 && len(payload) > 0: // ACK+data
		println("TUNTCP data seq=", seq, "len=", len(payload))
		c.onData(seq, payload)
		// ACK داده‌ی دریافتی (حتی out-of-order هم ACK می‌دهیم)
		return [][]byte{c.ackPkt()}
	}
	return nil
}

func (s *Stack) drop(q Quad) {
	s.mu.Lock()
	delete(s.conns, q)
	s.mu.Unlock()
}

// Emit داده‌ی خروجی پشته را به‌صورت پکت PSH+ACK تحویل می‌گیرد
func (c *Conn) Emit(payload []byte) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	pkt := c.build(0x18, payload, c.sndNXT, c.rcvNXT)
	c.sndNXT += uint32(len(payload))
	return pkt
}

// ---------------- Conn ----------------

type Conn struct {
	stack *Stack
	q     Quad

	mu     sync.Mutex
	state  uint8 // 0=syn-recv 1=established 2=closed
	iss    uint32
	sndNXT uint32
	rcvNXT uint32

	in       chan []byte // داده‌ی مرتب‌شده به سمت خواننده
	inBuf    map[uint32][]byte
	leftover []byte // باقی‌مانده‌ی قاب قبلی برای Read
	closed   chan struct{}
	once     sync.Once
}

func newConn(s *Stack, q Quad, clientISN uint32) *Conn {
	iss := s.nextISS
	s.nextISS += 64000
	return &Conn{
		stack: s, q: q, state: 0, iss: iss,
		sndNXT: iss + 1,
		rcvNXT: clientISN + 1,
		in:     make(chan []byte, 256),
		inBuf:  map[uint32][]byte{},
		closed: make(chan struct{}),
	}
}

func (c *Conn) reply(flags uint8, payload []byte, ack uint32) []byte {
	c.mu.Lock()
	pkt := c.buildLocked(flags, payload, c.sndNXT, ack)
	if flags&0x02 != 0 {
		c.sndNXT++
	}
	c.mu.Unlock()
	return pkt
}

func (c *Conn) ackPkt() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buildLocked(0x10, nil, c.sndNXT, c.rcvNXT)
}

func (c *Conn) onData(seq uint32, payload []byte) {
	println("TUNTCP.onData", seq, len(payload))
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == 0 {
		c.state = 1 // ACK نهایی — established
	}
	if seq == c.rcvNXT {
		c.deliverLocked(payload)
		c.rcvNXT += uint32(len(payload))
		for {
			p, ok := c.inBuf[c.rcvNXT]
			if !ok {
				break
			}
			delete(c.inBuf, c.rcvNXT)
			c.deliverLocked(p)
			c.rcvNXT += uint32(len(p))
		}
	} else if seq > c.rcvNXT {
		if _, exists := c.inBuf[seq]; !exists {
			c.inBuf[seq] = append([]byte(nil), payload...)
		}
	}
}

func (c *Conn) deliverLocked(p []byte) {
	select {
	case c.in <- p:
	default: // بافر پر — سقوط ملایم (در عمل نباید رخ دهد)
	}
}

func (c *Conn) remoteFin() {
	c.remoteClose()
}

func (c *Conn) remoteClose() {
	c.once.Do(func() { close(c.closed) })
}

// Target آدرس مقصد این جریان (همان چیزی که اپ به آن وصل شده)
func (c *Conn) Target() string {
	return net.JoinHostPort(ipStr(c.q.DstIP), itoa(int(c.q.DstPort)))
}

// Read داده‌ی دریافتی از اپ را می‌دهد
func (c *Conn) Read(b []byte) (int, error) {
	c.mu.Lock()
	if len(c.leftover) > 0 {
		n := copy(b, c.leftover)
		c.leftover = c.leftover[n:]
		c.mu.Unlock()
		return n, nil
	}
	c.mu.Unlock()
	select {
	case p := <-c.in:
		c.mu.Lock()
		n := copy(b, p)
		if n < len(p) {
			c.leftover = append(c.leftover[:0], p[n:]...)
		}
		c.mu.Unlock()
		return n, nil
	case <-c.closed:
		return 0, ioEOF
	}
}

// Write داده به سمت اپ (از تونل می‌آید)
func (c *Conn) Write(b []byte) (int, error) {
	println("TUNTCP.Write", len(b))
	select {
	case <-c.closed:
		return 0, ioEOF
	default:
	}
	pkt := c.Emit(b)
	select {
	case c.stackOut() <- pkt:
	default:
	}
	return len(b), nil
}

// stackOut صف خروجی پکت‌ها به TUN
func (c *Conn) stackOut() chan<- []byte { return c.stack.out }

func (c *Conn) Close() error {
	c.once.Do(func() {
		c.mu.Lock()
		pkt := c.buildLocked(0x11, nil, c.sndNXT, c.rcvNXT) // FIN+ACK
		c.mu.Unlock()
		select {
		case c.stack.out <- pkt:
		default:
		}
		close(c.closed)
		c.stack.drop(c.q)
	})
	return nil
}

func (c *Conn) LocalAddr() net.Addr  { return &addr{ipStr(c.q.SrcIP), c.q.SrcPort} }
func (c *Conn) RemoteAddr() net.Addr { return &addr{ipStr(c.q.DstIP), c.q.DstPort} }
func (c *Conn) SetDeadline(t time.Time) error      { return nil }
func (c *Conn) SetReadDeadline(t time.Time) error  { return nil }
func (c *Conn) SetWriteDeadline(t time.Time) error { return nil }

// ---------------- بسته‌سازی ----------------

// build ساخت پکت IP+TCP کامل (قفل باید گرفته شده باشد)
func (c *Conn) build(flags uint8, payload []byte, seq, ack uint32) []byte {
	return c.buildLocked(flags, payload, seq, ack)
}

func (c *Conn) buildLocked(flags uint8, payload []byte, seq, ack uint32) []byte {
	tcpLen := 20 + len(payload)
	ipLen := 20 + tcpLen
	pkt := make([]byte, ipLen)

	// IPv4
	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], uint16(ipLen))
	pkt[8] = 64
	pkt[9] = 6 // TCP
	copy(pkt[12:16], c.q.DstIP[:]) // src = ما (مقصد اصلی از دید اپ)
	copy(pkt[16:20], c.q.SrcIP[:]) // dst = گوشی
	binary.BigEndian.PutUint16(pkt[10:12], ipChecksum(pkt[:20]))

	tcp := pkt[20:]
	binary.BigEndian.PutUint16(tcp[0:2], c.q.DstPort)
	binary.BigEndian.PutUint16(tcp[2:4], c.q.SrcPort)
	binary.BigEndian.PutUint32(tcp[4:8], seq)
	binary.BigEndian.PutUint32(tcp[8:12], ack)
	tcp[12] = 0x50 // data offset 5
	tcp[13] = flags
	binary.BigEndian.PutUint16(tcp[14:16], winSize)
	copy(tcp[20:], payload)
	binary.BigEndian.PutUint16(tcp[16:18], tcpChecksum(c.q.DstIP, c.q.SrcIP, tcp))
	return pkt
}

// ---------------- checksum ها ----------------

func ipChecksum(b []byte) uint16 {
	sum := uint32(0)
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(b[i:]))
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum > 0xFFFF {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	return ^uint16(sum)
}

func tcpChecksum(src, dst [4]byte, tcp []byte) uint16 {
	pseudo := make([]byte, 12+len(tcp)+1)
	copy(pseudo[0:4], src[:])
	copy(pseudo[4:8], dst[:])
	pseudo[9] = 6
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(tcp)))
	copy(pseudo[12:], tcp)
	if len(tcp)%2 == 1 {
		pseudo[len(pseudo)-1] = 0
	}
	return ipChecksum(pseudo[:12+len(tcp)])
}

func ipOK(ip []byte) bool {
	return ipChecksum(ip) == 0
}

func tcpOK(src, dst [4]byte, tcp []byte) bool {
	return tcpChecksum(src, dst, tcp) == 0
}

type addr struct{ ip string; port uint16 }

func (a *addr) Network() string { return "tcp" }
func (a *addr) String() string  { return net.JoinHostPort(a.ip, itoa(int(a.port))) }

func itoa(i int) string { return fmt.Sprintf("%d", i) }

var ioEOF = io.EOF

