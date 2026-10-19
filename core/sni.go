package core

import (
	"bufio"
	"crypto/tls"
	"io"
	"math/rand"
	"net"
	"time"
)

// ChameleonListener — لایه‌ی استتار SNI روی TLS خام.
// ترتیب ارسال:
//  1) early-silence: N پکت جعلی با SNI مجاز (TTL کم — فقط میانجی می‌بیند)
//  2) ClientHello واقعی به‌صورت تکه‌تکه‌شده در چند TCP segment
//  3) TLS معمولی روی همان کانکشن
//
// نکته: این فایل لایه‌ی server-side را پیاده می‌کند؛ کلاینت در هسته‌ی
// اندروید با همان توالی، segmentation را انجام می‌دهد.

type ChameleonListener struct {
	conf     ChameleonConf
	listener net.Listener
	tlsConf  *tls.Config
}

func NewChameleonListener(conf ChameleonConf, l net.Listener, tlsConf *tls.Config) *ChameleonListener {
	if len(conf.FakeSNIs) == 0 {
		conf.FakeSNIs = []string{"www.google.com"}
	}
	if len(conf.FragmentOffsets) == 0 {
		conf.FragmentOffsets = []int{2, 5, 1}
	}
	if conf.EarlySilencePackets == 0 {
		conf.EarlySilencePackets = 3
	}
	if conf.CoverMinBytes == 0 {
		conf.CoverMinBytes, conf.CoverMaxBytes = 256, 1400
	}
	return &ChameleonListener{conf: conf, listener: l, tlsConf: tlsConf}
}

// Accept یک اتصال TLS آماده (با handshake کامل‌شده) برمی‌گرداند.
// پکت‌های early-silence در سمت کلاینت فرستاده می‌شوند؛ سرور فقط
// fragment-aware handshake را تحمل می‌کند و cover traffic را می‌خواند.
func (c *ChameleonListener) Accept() (net.Conn, error) {
	raw, err := c.listener.Accept()
	if err != nil {
		return nil, err
	}
	return &chameleonConn{Conn: raw, conf: &c.conf, br: bufio.NewReaderSize(raw, 8192)}, nil
}

func (c *ChameleonListener) Close() error { return c.listener.Close() }
func (c *ChameleonListener) Addr() net.Addr { return c.listener.Addr() }

type chameleonConn struct {
	net.Conn
	conf    *ChameleonConf
	br      *bufio.Reader
	pending []byte
	raw     bool // بعد از handshake فونیکس: بدون cover-detection
}

// SetRaw حالت خام — بعد از موفقیت handshake فراخوانی می‌شود.
// از این به بعد بایت 0xEE معنای cover ندارد (قاب‌های رمزنگاری‌شده
// ممکن است هر بایتی داشته باشند).
func (cc *chameleonConn) SetRaw() { cc.raw = true }

// RawConn کانکشن TCP زیرین — موتورها بعد از handshake باید روی آن کار کنند
// تا cover-detection روی بایت‌های تصادفی قاب‌های رمزنگاری‌شده اثر نگذارد.
func (cc *chameleonConn) RawConn() net.Conn { return cc.Conn }

// Read پکت‌های cover (padding) را دور می‌ریزد.
// فرمت cover: 0xEE || uint16 len || random — کلاینت این‌ها را در میانه‌ی
// جریان می‌فرستد تا الگوی بیکاری لو نرود. خواندن از بافر انجام می‌شود
// تا مصرف زیرین استریم با اندازه‌ی Read هم‌خوانی نداشته باشد.
func (cc *chameleonConn) Read(b []byte) (int, error) {
	if cc.raw {
		if len(cc.pending) > 0 {
			n := copy(b, cc.pending)
			cc.pending = cc.pending[n:]
			return n, nil
		}
		return cc.br.Read(b)
	}
	for len(cc.pending) == 0 {
		c0, err := cc.br.ReadByte()
		if err != nil {
			return 0, err
		}
		if c0 != 0xEE {
			cc.pending = []byte{c0}
			break
		}
		h := make([]byte, 2)
		if _, err := io.ReadFull(cc.br, h); err != nil {
			return 0, err
		}
		l := int(h[0])<<8 | int(h[1])
		discard := make([]byte, l)
		if _, err := io.ReadFull(cc.br, discard); err != nil {
			return 0, err
		}
	}
	n := copy(b, cc.pending)
	cc.pending = cc.pending[n:]
	return n, nil
}

// SendCover تابع کمکی کلاینت: تولید یک پکت cover طبیعی
func SendCover(conn net.Conn, min, max int) (int, error) {
	if min <= 0 {
		min, max = 256, 1400
	}
	l := min + rand.Intn(max-min)
	b := make([]byte, 3+l)
	b[0] = 0xEE
	b[1] = byte(l >> 8)
	b[2] = byte(l)
	rand.Read(b[3:])
	return conn.Write(b)
}

// CoverLoop ارسال دوره‌ای cover وقتی تونل بیکار است
func CoverLoop(conn net.Conn, conf ChameleonConf, stop <-chan struct{}) {
	t := time.NewTicker(time.Duration(2+rand.Intn(4)) * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			SendCover(conn, conf.CoverMinBytes, conf.CoverMaxBytes)
		}
	}
}

func readFull(c net.Conn, b []byte) (int, error) {
	total := 0
	for total < len(b) {
		n, err := c.Read(b[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
