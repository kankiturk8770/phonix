package server

import (
	"context"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"net"
	"sync"
	"time"

	"github.com/phoenixtunnel/phoenix/core"
)

// ---------------- PhoenixUDP — پروتکل اختصاصی ترانسپورت UDP ----------------
// طراحی برای زنده ماندن روی UDP مسدودنشده با throttling و packet-loss بالا:
//   handshake: salt مذاکره + HMAC proof (ضد پروب)
//   framing:   سشن + seq + ack + flag
//   ARQ:       بازارسال انتخابی با timeout تطبیقی
//   رمز:       payload هر DATA با AES-256-GCM (کلید از کلید مشترک + دو salt)
//   EOF:       DATA با متن خالی = پایان جریان در آن جهت
//   padding:   هر دیتاگرام به 512 بایت می‌رسد تا الگوی طول لو نرود
//
// وایرفرمت دیتاگرام:
//   magic "PX" (2) | version (1) | type (1) | session (4) | seq (4) | len/ack (4) | payload...
//   type: 0=HELLO 1=ACCEPT 2=DATA 3=ACK 4=PING 5=PONG
//   برای DATA فیلد چهارم = طول payload رمزشده (padding بعد از آن دور ریخته می‌شود)

const (
	pxMagic0, pxMagic1 = 'P', 'X'
	pxVer              = 1
	hdrLen             = 20

	tHello  = 0
	tAccept = 1
	tData   = 2
	tAck    = 3
	tPing   = 4
	tPong   = 5
)

const (
	udpMSS      = 1200 // سقف payload در هر دیتاگرام
	udpPadTo    = 512  // padding تا این حداقل
	resendBase  = 120 * time.Millisecond
	maxResend   = 12
	upQueueLen  = 4096 // صف نوشتن به مقصد هر سشن
	sessionIdle = 3 * time.Minute
)

type PhoenixUDPEngine struct {
	conf core.HydraConf // از همان کانفیگ کانال‌ها/FEC استفاده می‌کند
	keys []byte

	mu    sync.Mutex
	stats core.Stats
	pc    net.PacketConn

	sessions map[uint32]*udpSession
}

type udpSession struct {
	id   uint32
	peer net.Addr
	send cipher.AEAD // s2c
	recv cipher.AEAD // c2s

	mu       sync.Mutex
	sendSeq  uint32
	unacked  map[uint32]*pendingPkt
	recvBuf  map[uint32][]byte // برای مرتب‌سازی ورودی (رمزگشایی‌شده)
	nextRecv uint32
	lastSeen time.Time

	salt      []byte // salt سرور (برای ACCEPT تکراری)
	target    string
	upQ       chan []byte // داده‌ی مرتب برای مقصد؛ nil-frame = EOF کلاینت
	closeOnce sync.Once
	closeCh   chan struct{}
}

type pendingPkt struct {
	data     []byte // دیتاگرام کامل
	lastSent time.Time
	attempts int
}

func (e *PhoenixUDPEngine) Name() string { return "phoenix-udp" }

func (e *PhoenixUDPEngine) Listen(ctx context.Context, addr string) error {
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return err
	}
	e.pc = pc
	e.sessions = map[uint32]*udpSession{}
	e.stats.Since = time.Now()
	e.stats.Name = e.Name()
	go func() {
		<-ctx.Done()
		pc.Close()
	}()
	go e.reaper(ctx)
	buf := make([]byte, 4096)
	for {
		n, peer, err := pc.ReadFrom(buf)
		if err != nil {
			return err
		}
		e.dispatch(peer, buf[:n])
	}
}

// reaper حذف سشن‌های بیکار (کلاینتی که بدون EOF رفته)
func (e *PhoenixUDPEngine) reaper(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.mu.Lock()
			var idle []*udpSession
			for _, s := range e.sessions {
				s.mu.Lock()
				if time.Since(s.lastSeen) > sessionIdle {
					idle = append(idle, s)
				}
				s.mu.Unlock()
			}
			e.mu.Unlock()
			for _, s := range idle {
				s.close(e)
			}
		}
	}
}

// dispatch هرگز مسدود نمی‌شود: نوشتن به مقصد در گوروتین هر سشن انجام می‌شود
func (e *PhoenixUDPEngine) dispatch(peer net.Addr, d []byte) {
	if len(d) < hdrLen || d[0] != pxMagic0 || d[1] != pxMagic1 || d[2] != pxVer {
		return // پروب/نویز — بی‌صدا
	}
	typ, sessID := d[3], binary.BigEndian.Uint32(d[4:8])
	seq, ack := binary.BigEndian.Uint32(d[8:12]), binary.BigEndian.Uint32(d[12:16])
	payload := d[hdrLen:]

	switch typ {
	case tHello:
		e.handleHello(peer, sessID, payload)
	case tData:
		e.mu.Lock()
		s := e.sessions[sessID]
		e.mu.Unlock()
		l := int(ack) // برای DATA فیلد چهارم = طول payload است
		if l < 0 || l > len(payload) {
			return
		}
		if s != nil {
			s.onData(e, seq, payload[:l])
		}
	case tAck:
		e.mu.Lock()
		s := e.sessions[sessID]
		e.mu.Unlock()
		if s != nil {
			s.onAck(ack)
		}
	case tPing:
		e.reply(peer, sessID, tPong, 0, 0, nil)
	case tPong:
		// liveness
	}
	e.mu.Lock()
	e.stats.BytesUp += int64(len(d))
	e.mu.Unlock()
}

// handleHello: payload = salt(32) || proof(16)
func (e *PhoenixUDPEngine) handleHello(peer net.Addr, id uint32, payload []byte) {
	if len(payload) < 48 {
		return
	}
	salt, proof := payload[:32], payload[32:48]
	mac := hmac.New(sha256.New, e.keys)
	mac.Write(salt)
	if !hmac.Equal(proof, mac.Sum(nil)[:16]) {
		return // پروب فعال — بی‌صدا
	}
	e.mu.Lock()
	if old := e.sessions[id]; old != nil {
		e.mu.Unlock()
		// HELLO تکراری (ACCEPT گم شده): همان ACCEPT را دوباره بفرست
		e.reply(peer, id, tAccept, 0, 0, old.serverSalt())
		return
	}
	e.mu.Unlock()

	servSalt := make([]byte, 32)
	randomRead(servSalt)
	c2s, s2c, err := core.PUDPKeys(e.keys, salt, servSalt)
	if err != nil {
		return
	}
	s := &udpSession{
		id: id, peer: peer, send: s2c, recv: c2s,
		unacked:  map[uint32]*pendingPkt{},
		recvBuf:  map[uint32][]byte{},
		lastSeen: time.Now(),
		upQ:      make(chan []byte, upQueueLen),
		closeCh:  make(chan struct{}),
	}
	s.salt = servSalt
	e.mu.Lock()
	e.sessions[id] = s
	e.stats.ConnsActive++
	e.mu.Unlock()
	go s.senderLoop(e)
	e.reply(peer, id, tAccept, 0, 0, servSalt)
}

func (e *PhoenixUDPEngine) reply(peer net.Addr, sessID uint32, typ byte, seq, ack uint32, payload []byte) {
	d := make([]byte, hdrLen+len(payload))
	d[0], d[1], d[2], d[3] = pxMagic0, pxMagic1, pxVer, typ
	binary.BigEndian.PutUint32(d[4:8], sessID)
	binary.BigEndian.PutUint32(d[8:12], seq)
	if typ == tAck {
		binary.BigEndian.PutUint32(d[12:16], ack)
	} else {
		binary.BigEndian.PutUint32(d[12:16], uint32(len(payload))) // ack یا len
	}
	copy(d[hdrLen:], payload)
	e.pc.WriteTo(padDatagram(d), peer)
}

func padDatagram(d []byte) []byte {
	if len(d) >= udpPadTo {
		return d
	}
	out := make([]byte, udpPadTo)
	copy(out, d)
	// بقیه تصادفی است
	randomRead(out[len(d):])
	return out
}

// onData قاب داده: رمزگشایی، مرتب‌سازی، اولین قاب = آدرس مقصد
func (s *udpSession) onData(e *PhoenixUDPEngine, seq uint32, sealed []byte) {
	plain, err := s.recv.Open(nil, core.PUDPNonce(s.id, seq), sealed, nil)
	if err != nil {
		return // جعلی یا خراب — بی‌صدا
	}
	s.mu.Lock()
	s.lastSeen = time.Now()
	if seq < s.nextRecv {
		s.mu.Unlock()
		e.reply(s.peer, s.id, tAck, 0, seq, nil) // ACK تکراری
		return
	}
	if _, dup := s.recvBuf[seq]; !dup {
		s.recvBuf[seq] = plain
	}
	startPump := ""
	for {
		p, ok := s.recvBuf[s.nextRecv]
		if !ok {
			break
		}
		if s.nextRecv == 0 && s.target == "" {
			t, err := core.DecodeTarget(p)
			if err != nil {
				break
			}
			s.target, startPump = t, t
		} else {
			select {
			case s.upQ <- p:
			default:
				// صف پر: این قاب را نگه دار؛ ACK نمی‌شود و کلاینت بازارسال می‌کند
				s.mu.Unlock()
				return
			}
		}
		delete(s.recvBuf, s.nextRecv)
		s.nextRecv++
	}
	s.mu.Unlock()
	if startPump != "" {
		go s.pump(e, startPump)
	}
	e.reply(s.peer, s.id, tAck, 0, seq, nil)
}

func (s *udpSession) serverSalt() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.salt
}

// onAck حذف از صف بازارسال
func (s *udpSession) onAck(ack uint32) {
	s.mu.Lock()
	delete(s.unacked, ack)
	s.lastSeen = time.Now()
	s.mu.Unlock()
}

// pump جریان از/به مقصد TCP
func (s *udpSession) pump(e *PhoenixUDPEngine, target string) {
	defer s.close(e)
	up, err := net.DialTimeout("tcp", target, 10*time.Second)
	if err != nil {
		s.queueSend(e, nil) // EOF به کلاینت
		s.waitAcked(5 * time.Second)
		return
	}
	defer up.Close()

	// کلاینت → مقصد
	go func() {
		for {
			select {
			case p := <-s.upQ:
				if len(p) == 0 { // EOF کلاینت
					if cw, ok := up.(interface{ CloseWrite() error }); ok {
						cw.CloseWrite()
					}
					continue
				}
				if _, err := up.Write(p); err != nil {
					up.Close()
					return
				}
			case <-s.closeCh:
				return
			}
		}
	}()

	// مقصد → کلاینت
	buf := make([]byte, 16384)
	for {
		n, err := up.Read(buf)
		if n > 0 {
			s.queueSend(e, buf[:n])
		}
		if err != nil {
			break
		}
	}
	s.queueSend(e, nil) // EOF
	s.waitAcked(10 * time.Second)
}

// waitAcked صبر تا تحویل همه‌ی قاب‌ها (یا timeout)
func (s *udpSession) waitAcked(max time.Duration) {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		n := len(s.unacked)
		s.mu.Unlock()
		if n == 0 {
			return
		}
		select {
		case <-s.closeCh:
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// senderLoop بازارسال تطبیقی
func (s *udpSession) senderLoop(e *PhoenixUDPEngine) {
	t := time.NewTicker(resendBase / 2)
	defer t.Stop()
	for {
		select {
		case <-s.closeCh:
			return
		case <-t.C:
			now := time.Now()
			s.mu.Lock()
			for seq, p := range s.unacked {
				if now.Sub(p.lastSent) > resendBase*time.Duration(1<<p.attempts) {
					if p.attempts >= maxResend {
						delete(s.unacked, seq)
						continue
					}
					e.pc.WriteTo(padDatagram(p.data), s.peer) // p.data دیتاگرام کامل است
					p.lastSent = now
					p.attempts++
				}
			}
			s.mu.Unlock()
		}
	}
}

// queueSend رمزنگاری + ارسال با seq؛ payload خالی = EOF
func (s *udpSession) queueSend(e *PhoenixUDPEngine, payload []byte) {
	for {
		n := udpMSS
		if n > len(payload) {
			n = len(payload)
		}
		s.mu.Lock()
		seq := s.sendSeq
		s.sendSeq++
		sealed := s.send.Seal(nil, core.PUDPNonce(s.id, seq), payload[:n], nil)
		d := make([]byte, hdrLen+len(sealed))
		d[0], d[1], d[2], d[3] = pxMagic0, pxMagic1, pxVer, tData
		binary.BigEndian.PutUint32(d[4:8], s.id)
		binary.BigEndian.PutUint32(d[8:12], seq)
		binary.BigEndian.PutUint32(d[12:16], uint32(len(sealed)))
		copy(d[hdrLen:], sealed)
		s.unacked[seq] = &pendingPkt{data: d, lastSent: time.Now()}
		s.mu.Unlock()
		e.pc.WriteTo(padDatagram(d), s.peer)
		e.mu.Lock()
		e.stats.BytesDown += int64(len(d))
		e.mu.Unlock()
		payload = payload[n:]
		if len(payload) == 0 {
			return
		}
	}
}

func (s *udpSession) close(e *PhoenixUDPEngine) {
	s.closeOnce.Do(func() {
		close(s.closeCh)
		e.mu.Lock()
		if e.sessions[s.id] == s {
			delete(e.sessions, s.id)
			e.stats.ConnsActive--
		}
		e.mu.Unlock()
	})
}

func (e *PhoenixUDPEngine) Dial(ctx context.Context, addr string) (net.Conn, error) {
	return net.DialTimeout("udp", addr, 5*time.Second)
}
func (e *PhoenixUDPEngine) ProbeOK(challenge []byte) bool { return true }
func (e *PhoenixUDPEngine) Stats() core.Stats {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stats
}
func (e *PhoenixUDPEngine) Close() error {
	if e.pc != nil {
		return e.pc.Close()
	}
	return nil
}
