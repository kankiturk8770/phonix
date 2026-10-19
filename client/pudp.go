package client

import (
	"context"
	crand "crypto/rand"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/phoenixtunnel/phoenix/core"
)

// PhoenixUDP کلاینت پروتکل اختصاصی UDP
// وایرفرمت با سرور یکی است: PX | ver | type | sess | seq | len/ack | payload
const (
	puHdr = 20
)

type PUDPConf struct {
	Server string
	Keys   []byte
}

type pudpClient struct {
	conf   PUDPConf
	sessID uint32
	conn   net.PacketConn
	peer   net.Addr

	mu      sync.Mutex
	sendSeq uint32
	unacked map[uint32]*pudpPending
	recvBuf map[uint32][]byte
	nextRecv uint32
	in      chan []byte
	closed  chan struct{}
	once    sync.Once
}

type pudpPending struct {
	dgram    []byte
	lastSent time.Time
	attempts int
}

// DialPhoenixUDP اتصال به سرور PhoenixUDP و درخواست target
func DialPhoenixUDP(ctx context.Context, conf PUDPConf, target string) (net.Conn, error) {
	conn, err := net.ListenPacket("udp", ":0")
	if err != nil {
		return nil, err
	}
	c := &pudpClient{
		conf:    conf,
		sessID:  uint32(time.Now().UnixNano()),
		conn:    conn,
		unacked: map[uint32]*pudpPending{},
		recvBuf: map[uint32][]byte{},
		in:      make(chan []byte, 512),
		closed:  make(chan struct{}),
	}
	c.peer, err = net.ResolveUDPAddr("udp", conf.Server)
	if err != nil {
		conn.Close()
		return nil, err
	}

	// HELLO با retry
	salt := make([]byte, 32)
	coreRand(salt)
	mac := hmac.New(sha256.New, conf.Keys)
	mac.Write(salt)
	hello := make([]byte, puHdr+48)
	hello[0], hello[1], hello[2], hello[3] = 'P', 'X', 1, 0
	binary.BigEndian.PutUint32(hello[4:8], c.sessID)
	copy(hello[puHdr:], salt)
	copy(hello[puHdr+32:], mac.Sum(nil)[:16])

	accepted := make(chan bool, 1)
	go c.recvLoop(accepted)
	ok := false
	for i := 0; i < 5 && !ok; i++ {
		conn.WriteTo(padTo(hello, 512), c.peer)
		select {
		case <-accepted:
			ok = true
		case <-time.After(800 * time.Millisecond):
		case <-ctx.Done():
			conn.Close()
			return nil, ctx.Err()
		}
	}
	if !ok {
		conn.Close()
		return nil, fmt.Errorf("phoenix-udp: no ACCEPT")
	}

	// اولین قاب = آدرس مقصد
	tgt, err := core.EncodeTarget(target)
	if err != nil {
		conn.Close()
		return nil, err
	}
	c.Write(tgt)
	go c.arqLoop()
	return c, nil
}

func coreRand(b []byte) { crand.Read(b) }

func padTo(d []byte, n int) []byte {
	if len(d) >= n {
		return d
	}
	out := make([]byte, n)
	copy(out, d)
	return out
}

// ---------------- net.Conn ----------------

func (c *pudpClient) Read(b []byte) (int, error) {
	select {
	case p := <-c.in:
		n := copy(b, p)
		if n < len(p) {
			go func() { c.in <- p[n:] }()
		}
		return n, nil
	case <-c.closed:
		return 0, io.EOF
	}
}

func (c *pudpClient) Write(b []byte) (int, error) {
	total := 0
	for len(b) > 0 {
		n := 1200
		if n > len(b) {
			n = len(b)
		}
		c.mu.Lock()
		seq := c.sendSeq
		c.sendSeq++
		d := make([]byte, puHdr+n)
		d[0], d[1], d[2], d[3] = 'P', 'X', 1, 2 // DATA
		binary.BigEndian.PutUint32(d[4:8], c.sessID)
		binary.BigEndian.PutUint32(d[8:12], seq)
		binary.BigEndian.PutUint32(d[12:16], uint32(n))
		copy(d[puHdr:], b[:n])
		c.unacked[seq] = &pudpPending{dgram: d, lastSent: time.Now()}
		c.mu.Unlock()
		c.conn.WriteTo(padTo(d, 512), c.peer)
		total += n
		b = b[n:]
	}
	return total, nil
}

func (c *pudpClient) Close() error {
	c.once.Do(func() {
		close(c.closed)
		c.conn.Close()
	})
	return nil
}

func (c *pudpClient) LocalAddr() net.Addr                { return c.conn.LocalAddr() }
func (c *pudpClient) RemoteAddr() net.Addr               { return c.peer }
func (c *pudpClient) SetDeadline(t time.Time) error      { return nil }
func (c *pudpClient) SetReadDeadline(t time.Time) error  { return nil }
func (c *pudpClient) SetWriteDeadline(t time.Time) error { return nil }

// ---------------- دریافت و ARQ ----------------

func (c *pudpClient) recvLoop(accepted chan bool) {
	buf := make([]byte, 4096)
	for {
		n, _, err := c.conn.ReadFrom(buf)
		if err != nil {
			return
		}
		if n < puHdr || buf[0] != 'P' || buf[1] != 'X' {
			continue
		}
		typ, seq := buf[3], binary.BigEndian.Uint32(buf[8:12])
		field := binary.BigEndian.Uint32(buf[12:16])
		switch typ {
		case 1: // ACCEPT
			select {
			case accepted <- true:
			default:
			}
		case 2: // DATA
			l := int(field)
			if l < 0 || puHdr+l > n {
				continue
			}
			payload := append([]byte(nil), buf[puHdr:puHdr+l]...)
			c.mu.Lock()
			if seq < c.nextRecv {
				c.mu.Unlock()
				c.sendAck(seq)
				continue
			}
			c.recvBuf[seq] = payload
			for {
				p, ok := c.recvBuf[c.nextRecv]
				if !ok {
					break
				}
				delete(c.recvBuf, c.nextRecv)
				select {
				case c.in <- p:
				case <-c.closed:
					c.mu.Unlock()
					return
				}
				c.nextRecv++
			}
			c.mu.Unlock()
		case 3: // ACK
			c.mu.Lock()
			delete(c.unacked, field)
			c.mu.Unlock()
		}
	}
}

func (c *pudpClient) sendAck(seq uint32) {
	d := make([]byte, puHdr)
	d[0], d[1], d[2], d[3] = 'P', 'X', 1, 3
	binary.BigEndian.PutUint32(d[4:8], c.sessID)
	binary.BigEndian.PutUint32(d[12:16], seq)
	c.conn.WriteTo(padTo(d, 512), c.peer)
}

func (c *pudpClient) arqLoop() {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-c.closed:
			return
		case <-t.C:
			now := time.Now()
			c.mu.Lock()
			for seq, p := range c.unacked {
				if now.Sub(p.lastSent) > time.Duration(120*(1<<p.attempts))*time.Millisecond {
					if p.attempts >= 12 {
						delete(c.unacked, seq)
						continue
					}
					c.conn.WriteTo(padTo(p.dgram, 512), c.peer)
					p.lastSent = now
					p.attempts++
				}
			}
			c.mu.Unlock()
		}
	}
}
