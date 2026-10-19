package client

import (
	"context"
	"crypto/cipher"
	"crypto/hmac"
	crand "crypto/rand"
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
// payload هر DATA با AES-256-GCM رمز می‌شود؛ DATA خالی = EOF.
const (
	puHdr     = 20
	puMSS     = 1200
	puPadTo   = 512
	puInQueue = 4096
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
	salt   []byte
	send   cipher.AEAD // c2s
	recv   cipher.AEAD // s2c

	mu       sync.Mutex
	sendSeq  uint32
	unacked  map[uint32]*pudpPending
	recvBuf  map[uint32][]byte
	nextRecv uint32

	in     chan []byte // داده‌ی مرتب؛ nil = EOF
	rbuf   []byte
	eof    bool
	closed chan struct{}
	once   sync.Once
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
	var idb [4]byte
	crand.Read(idb[:])
	c := &pudpClient{
		conf:    conf,
		sessID:  binary.BigEndian.Uint32(idb[:]),
		conn:    conn,
		unacked: map[uint32]*pudpPending{},
		recvBuf: map[uint32][]byte{},
		in:      make(chan []byte, puInQueue),
		closed:  make(chan struct{}),
	}
	c.peer, err = net.ResolveUDPAddr("udp", conf.Server)
	if err != nil {
		conn.Close()
		return nil, err
	}

	// HELLO با retry
	c.salt = make([]byte, 32)
	coreRand(c.salt)
	mac := hmac.New(sha256.New, conf.Keys)
	mac.Write(c.salt)
	hello := make([]byte, puHdr+48)
	hello[0], hello[1], hello[2], hello[3] = 'P', 'X', 1, 0
	binary.BigEndian.PutUint32(hello[4:8], c.sessID)
	copy(hello[puHdr:], c.salt)
	copy(hello[puHdr+32:], mac.Sum(nil)[:16])

	accepted := make(chan []byte, 1)
	go c.recvLoop(accepted)
	var servSalt []byte
	for i := 0; i < 5 && servSalt == nil; i++ {
		conn.WriteTo(padTo(hello, puPadTo), c.peer)
		select {
		case servSalt = <-accepted:
		case <-time.After(800 * time.Millisecond):
		case <-ctx.Done():
			conn.Close()
			return nil, ctx.Err()
		}
	}
	if servSalt == nil {
		conn.Close()
		return nil, fmt.Errorf("phoenix-udp: no ACCEPT")
	}

	// اولین قاب = آدرس مقصد
	tgt, err := core.EncodeTarget(target)
	if err != nil {
		c.Close()
		return nil, err
	}
	c.sendFrame(tgt)
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
	crand.Read(out[len(d):])
	return out
}

// ---------------- net.Conn ----------------

func (c *pudpClient) Read(b []byte) (int, error) {
	if len(c.rbuf) == 0 {
		if c.eof {
			return 0, io.EOF
		}
		select {
		case p := <-c.in:
			if p == nil {
				c.eof = true
				return 0, io.EOF
			}
			c.rbuf = p
		case <-c.closed:
			return 0, io.EOF
		}
	}
	n := copy(b, c.rbuf)
	c.rbuf = c.rbuf[n:]
	return n, nil
}

func (c *pudpClient) Write(b []byte) (int, error) {
	select {
	case <-c.closed:
		return 0, io.ErrClosedPipe
	default:
	}
	total := 0
	for len(b) > 0 {
		n := puMSS
		if n > len(b) {
			n = len(b)
		}
		c.sendFrame(b[:n])
		total += n
		b = b[n:]
	}
	return total, nil
}

// sendFrame رمزنگاری و ارسال یک قاب با seq بعدی (p خالی = EOF)
func (c *pudpClient) sendFrame(p []byte) {
	c.mu.Lock()
	seq := c.sendSeq
	c.sendSeq++
	sealed := c.send.Seal(nil, core.PUDPNonce(c.sessID, seq), p, nil)
	d := make([]byte, puHdr+len(sealed))
	d[0], d[1], d[2], d[3] = 'P', 'X', 1, 2 // DATA
	binary.BigEndian.PutUint32(d[4:8], c.sessID)
	binary.BigEndian.PutUint32(d[8:12], seq)
	binary.BigEndian.PutUint32(d[12:16], uint32(len(sealed)))
	copy(d[puHdr:], sealed)
	c.unacked[seq] = &pudpPending{dgram: d, lastSent: time.Now()}
	c.mu.Unlock()
	c.conn.WriteTo(padTo(d, puPadTo), c.peer)
}

// CloseWrite پایان ارسال (EOF به سرور)؛ دریافت ادامه دارد
func (c *pudpClient) CloseWrite() error {
	c.sendFrame(nil)
	return nil
}

func (c *pudpClient) Close() error {
	c.once.Do(func() {
		c.sendFrame(nil) // EOF به سرور تا اتصال مقصد بسته شود
		close(c.closed)
		go func() {
			// کمی صبر تا EOF تحویل شود، بعد سوکت بسته شود
			for i := 0; i < 20; i++ {
				c.mu.Lock()
				n := len(c.unacked)
				c.mu.Unlock()
				if n == 0 {
					break
				}
				c.mu.Lock()
				for _, p := range c.unacked {
					c.conn.WriteTo(padTo(p.dgram, puPadTo), c.peer)
				}
				c.mu.Unlock()
				time.Sleep(100 * time.Millisecond)
			}
			c.conn.Close()
		}()
	})
	return nil
}

func (c *pudpClient) LocalAddr() net.Addr                { return c.conn.LocalAddr() }
func (c *pudpClient) RemoteAddr() net.Addr               { return c.peer }
func (c *pudpClient) SetDeadline(t time.Time) error      { return nil }
func (c *pudpClient) SetReadDeadline(t time.Time) error  { return nil }
func (c *pudpClient) SetWriteDeadline(t time.Time) error { return nil }

// ---------------- دریافت و ARQ ----------------

func (c *pudpClient) recvLoop(accepted chan []byte) {
	buf := make([]byte, 4096)
	for {
		n, _, err := c.conn.ReadFrom(buf)
		if err != nil {
			return
		}
		if n < puHdr || buf[0] != 'P' || buf[1] != 'X' {
			continue
		}
		if binary.BigEndian.Uint32(buf[4:8]) != c.sessID {
			continue
		}
		typ, seq := buf[3], binary.BigEndian.Uint32(buf[8:12])
		field := binary.BigEndian.Uint32(buf[12:16])
		switch typ {
		case 1: // ACCEPT: payload = salt سرور
			if field != 32 || n < puHdr+32 || c.recv != nil {
				continue
			}
			servSalt := append([]byte(nil), buf[puHdr:puHdr+32]...)
			c2s, s2c, err := core.PUDPKeys(c.conf.Keys, c.salt, servSalt)
			if err != nil {
				continue
			}
			c.send, c.recv = c2s, s2c
			select {
			case accepted <- servSalt:
			default:
			}
		case 2: // DATA
			l := int(field)
			if c.recv == nil || l < 0 || puHdr+l > n {
				continue
			}
			plain, err := c.recv.Open(nil, core.PUDPNonce(c.sessID, seq), buf[puHdr:puHdr+l], nil)
			if err != nil {
				continue
			}
			// ACK هر قاب سالم تا سرور بیهوده بازارسال نکند
			c.sendAck(seq)
			var ready [][]byte
			c.mu.Lock()
			if seq >= c.nextRecv {
				if _, dup := c.recvBuf[seq]; !dup {
					c.recvBuf[seq] = plain
				}
				for {
					p, ok := c.recvBuf[c.nextRecv]
					if !ok {
						break
					}
					delete(c.recvBuf, c.nextRecv)
					c.nextRecv++
					ready = append(ready, p)
				}
			}
			c.mu.Unlock()
			// تحویل خارج از قفل (فقط همین گوروتین می‌نویسد → ترتیب حفظ می‌شود)
			for _, p := range ready {
				if len(p) == 0 {
					p = nil // EOF
				}
				select {
				case c.in <- p:
				case <-c.closed:
					return
				}
			}
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
	c.conn.WriteTo(padTo(d, puPadTo), c.peer)
}

func (c *pudpClient) arqLoop() {
	t := time.NewTicker(60 * time.Millisecond)
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
					c.conn.WriteTo(padTo(p.dgram, puPadTo), c.peer)
					p.lastSent = now
					p.attempts++
				}
			}
			c.mu.Unlock()
		}
	}
}
