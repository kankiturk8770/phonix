package server

import (
	"bufio"
	"context"
	"crypto/tls"
	"net"
	"sync"
	"time"

	"github.com/phoenixtunnel/phoenix/core"
)

// ---------------- ChameleonEngine ----------------

type ChameleonEngine struct {
	conf core.ChameleonConf
	keys []byte

	mu    sync.Mutex
	stats core.Stats
	ln    net.Listener
}

func (c *ChameleonEngine) Name() string { return "chameleon" }

func (c *ChameleonEngine) Listen(ctx context.Context, addr string) error {
	raw, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	cert, err := tls.LoadX509KeyPair("certs/server.crt", "certs/server.key")
	if err != nil {
		raw.Close()
		return err
	}
	tlsConf := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
	cl := core.NewChameleonListener(c.conf, raw, tlsConf)
	c.ln = cl
	c.stats.Since = time.Now()
	c.stats.Name = c.Name()

	go func() {
		<-ctx.Done()
		cl.Close()
	}()
	for {
		conn, err := cl.Accept()
		if err != nil {
			return err
		}
		c.track(func() { c.handle(conn) })
	}
}

// handle: handshake احراز هویت → اولین قاب = آدرس مقصد → رله‌ی دوطرفه
func (c *ChameleonEngine) handle(conn net.Conn) {
	defer conn.Close()
	// روی کانکشن خام کار کن: handshake و قاب‌ها نباید cover-detection
	// را ببینند (بایت 0xEE تصادفی در قاب رمزنگاری‌شده = فاجعه)
	conn = unwrapRaw(conn)
	conn.SetDeadline(time.Now().Add(30 * time.Second))
	// یک بافر مشترک برای handshake + قاب اول + رله (جلوگیری از گم‌شدن بایت)
	bc := &bufConn{br: bufio.NewReaderSize(conn, 16384), c: conn}
	sess, err := core.ServerHandshake(bc, c.keys)
	_ = sess
	if err != nil {
		c.fail()
		return // پروب فعال یا کلاینت نامعتبر — بی‌صدا بسته می‌شویم
	}
	frame, err := tolerantReadFrame(bc.br, 256)
	if err != nil {
		c.fail()
		return
	}
	plain, err := sess.OpenFrame(frame)
	if err != nil {
		c.fail()
		return
	}
	target, err := core.DecodeTarget(plain)
	if err != nil {
		c.fail()
		return
	}
	up, err := net.DialTimeout("tcp", target, 10*time.Second)
	if err != nil {
		c.fail()
		return
	}
	defer up.Close()
	conn.SetDeadline(time.Time{})

	// رله روی قاب‌های رمزنگاری‌شده: c→s رمز، s→c رمز
	rw := &core.FrameConn{Conn: bc, Sess: sess}
	var upN, downN int64
	core.Relay(rw, &countConn{Conn: up, up: &upN, down: &downN}, nil)
	c.addBytes(upN, downN)
}

// unwrapRaw بازگرداندن کانکشن خام اگر wrapper ای باشد
func unwrapRaw(conn net.Conn) net.Conn {
	if u, ok := conn.(interface{ RawConn() net.Conn }); ok {
		return u.RawConn()
	}
	return conn
}

func (c *ChameleonEngine) track(f func()) {
	c.mu.Lock()
	c.stats.ConnsActive++
	c.mu.Unlock()
	go func() {
		f()
		c.mu.Lock()
		c.stats.ConnsActive--
		c.mu.Unlock()
	}()
}

func (c *ChameleonEngine) fail() {
	c.mu.Lock()
	c.stats.Failures++
	c.stats.LastFailure = time.Now()
	c.mu.Unlock()
}

func (c *ChameleonEngine) addBytes(up, down int64) {
	c.mu.Lock()
	c.stats.BytesUp += up
	c.stats.BytesDown += down
	c.mu.Unlock()
}

func (c *ChameleonEngine) Dial(ctx context.Context, addr string) (net.Conn, error) {
	return net.DialTimeout("tcp", addr, 5*time.Second)
}
func (c *ChameleonEngine) ProbeOK(challenge []byte) bool { return true }
func (c *ChameleonEngine) Stats() core.Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}
func (c *ChameleonEngine) Close() error {
	if c.ln != nil {
		return c.ln.Close()
	}
	return nil
}

// ---------------- PhantomEngine (سر DNS کامل) ----------------

type PhantomEngine struct {
	conf core.PhantomConf
	keys []byte

	mu    sync.Mutex
	stats core.Stats
	srv   *core.PhantomServer
	pc    net.PacketConn

	// جدول سشن‌ها: session → جریان بازسازی‌شده
	sessions map[uint16]*phantomSession
}

type phantomSession struct {
	mu     sync.Mutex
	buf    map[uint32][]byte // seq → payload
	next   uint32            // seq مورد انتظار برای خروجی
	out    chan []byte       // داده‌ی آماده به سمت مقصد
	target string            // آدرس مقصد از اولین قاب
}

func NewPhantomSession() *phantomSession {
	return &phantomSession{buf: map[uint32][]byte{}, out: make(chan []byte, 256)}
}

func (p *PhantomEngine) Name() string { return "phantom" }

func (p *PhantomEngine) track() {
	p.mu.Lock()
	p.stats.ConnsActive++
	p.mu.Unlock()
	go func() {
		time.Sleep(time.Second)
		p.mu.Lock()
		p.stats.ConnsActive--
		p.mu.Unlock()
	}()
}

func (p *PhantomEngine) addBytes(up, down int64) {
	p.mu.Lock()
	p.stats.BytesUp += up
	p.stats.BytesDown += down
	p.mu.Unlock()
}

func (p *PhantomEngine) Listen(ctx context.Context, addr string) error {
	udp, err := net.ListenPacket("udp", addr)
	if err != nil {
		return err
	}
	p.pc = udp
	p.srv = core.NewPhantomServer(p.conf)
	p.sessions = map[uint16]*phantomSession{}
	p.stats.Since = time.Now()
	p.stats.Name = p.Name()

	go func() {
		<-ctx.Done()
		udp.Close()
	}()
	buf := make([]byte, 4096)
	for {
		n, peer, err := udp.ReadFrom(buf)
		if err != nil {
			return err
		}
		p.track()
		go p.handleQuery(udp, peer, buf[:n])
	}
}

// handleQuery: parse DNS → اگر دامنه‌ی چرخان ما بود تونل، وگرنه forward به upstream
func (p *PhantomEngine) handleQuery(pc net.PacketConn, peer net.Addr, raw []byte) {
	q, err := parseDNSQuery(raw)
	if err != nil {
		return
	}
	now := time.Now()
	if err := p.srv.ValidateQuery(q.name, now); err != nil {
		// دامنه‌ی ما نیست → مثل یک DNS سرور معمولی رفتار کن (استتار)
		resp, err := forwardDNS(raw, "8.8.8.8:53")
		if err == nil {
			pc.WriteTo(resp, peer)
		}
		p.addBytes(int64(len(raw)), 0)
		return
	}
	sessID, seq, payload, err := p.srv.DecodeQuery(q.name)
	if err != nil {
		return
	}
	p.mu.Lock()
	sess, ok := p.sessions[sessID]
	if !ok {
		sess = NewPhantomSession()
		p.sessions[sessID] = sess
	}
	p.mu.Unlock()

	// استخراج قاب رمزنگاری‌شده از payload و باز کردن
	if p.keys != nil {
		// قاب‌ها با کلید استاتیک سشن رمز شده‌اند (ساده‌سازی: AES-GCM با nonce صفر ممنوع —
		// در اینجا payload به‌صورت خام پاسخ می‌خورد؛ لایه رمز در کلاینت کامل است)
	}
	sess.store(seq, payload)

	// اگر این قاب اولین بود، مقصد را بردار و جریان را بساز
	if seq == 0 && sess.target == "" {
		if t, err := core.DecodeTarget(payload); err == nil {
			sess.target = t
			go p.pumpSession(sessID, sess, peer)
		}
	}
	// پاسخ TXT = ACK ساده
	answers := core.EncodeAnswer([]byte{byte(sessID >> 8), byte(sessID), byte(seq >> 24), byte(seq >> 16), byte(seq >> 8), byte(seq)})
	resp := buildDNSResponse(q, answers)
	pc.WriteTo(resp, peer)
	p.addBytes(0, int64(len(resp)))
}

// pumpSession داده‌ی رسیده از DNS را به مقصد TCP پمپ می‌کند و پاسخ را در بافر نگه می‌دارد
func (p *PhantomEngine) pumpSession(id uint16, sess *phantomSession, peer net.Addr) {
	defer func() {
		p.mu.Lock()
		delete(p.sessions, id)
		p.mu.Unlock()
	}()
	up, err := net.DialTimeout("tcp", sess.target, 10*time.Second)
	if err != nil {
		return
	}
	defer up.Close()
	// قاب‌ها را به ترتیب seq به مقصد می‌نویسیم
	go func() {
		for {
			payload, ok := sess.pull(sess.next)
			if !ok {
				return
			}
			if _, err := up.Write(payload); err != nil {
				return
			}
			sess.next++
		}
	}()
	// پاسخ مقصد → بافر خواندنی (در نسخه‌ی کامل از طریق CNAME/TXT poll کلاینت)
	buf := make([]byte, 4096)
	for {
		n, err := up.Read(buf)
		if err != nil {
			return
		}
		sess.out <- buf[:n]
	}
}

// pull منتظر رسیدن قاب seq می‌ماند (تا ۳۰ ثانیه)
func (s *phantomSession) pull(seq uint32) ([]byte, bool) {
	deadline := time.After(30 * time.Second)
	for {
		s.mu.Lock()
		if p, ok := s.buf[seq]; ok {
			delete(s.buf, seq)
			s.mu.Unlock()
			return p, true
		}
		s.mu.Unlock()
		select {
		case <-deadline:
			return nil, false
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// phantomSession helpers
func (s *phantomSession) store(seq uint32, p []byte) {
	s.mu.Lock()
	s.buf[seq] = p
	s.mu.Unlock()
}
