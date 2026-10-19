package core

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PhantomDNS — سمت سرور تونل DNS
// سه اصل: دامنه‌ی چرخان DGA، جداسازی مسیر رفت/برگشت، multi-session.
//
// پروتکل روی DNS TXT:
//   qname = base32( session(2) || seq(4) || payload ) + "." + rotatedDomain
// پاسخ TXT = base64( frame بازشده ) برای downstream.
//
// این فایل منطق دامنه/کدگذاری/rotate را پیاده می‌کند؛ سرور UDP در server/.

var ErrBadDomain = errors.New("phantom: query outside rotated domain")

type PhantomServer struct {
	conf PhantomConf
}

func NewPhantomServer(conf PhantomConf) *PhantomServer {
	if conf.RotateMinutes == 0 {
		conf.RotateMinutes = 10
	}
	return &PhantomServer{conf: conf}
}

// CurrentDomain دامنه‌ی فعال در این بازه‌ی زمانی
func (p *PhantomServer) CurrentDomain(now time.Time) string {
	return p.DomainAt(now)
}

// DomainAt دامنه در زمان دلخواه — DGA مبتنی بر HMAC(seed, time-slice)
func (p *PhantomServer) DomainAt(t time.Time) string {
	slice := t.Unix() / int64(p.conf.RotateMinutes*60)
	mac := hmac.New(sha256.New, []byte(p.conf.DGASeed))
	fmt.Fprintf(mac, "%d", slice)
	sum := mac.Sum(nil)
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	label := strings.ToLower(enc.EncodeToString(sum[:8])) // 16 کاراکتر
	base := p.conf.Domain
	if i := strings.Index(base, "."); i > 0 {
		base = base[i+1:] // ns1.example.com → example.com
	}
	return label + "." + base
}

// ValidateQuery چک می‌کند qname متعلق به بازه‌ی فعال یا قبلی باشد
// (margin یک بازه برای propagation DNS)
func (p *PhantomServer) ValidateQuery(qname string, now time.Time) error {
	for _, t := range []time.Time{now, now.Add(-time.Duration(p.conf.RotateMinutes) * time.Minute)} {
		d := p.DomainAt(t)
		if strings.HasSuffix(strings.ToLower(qname), "."+d) || strings.EqualFold(qname, d) {
			return nil
		}
	}
	return ErrBadDomain
}

// EncodeQuery ساخت qname برای کلاینت
func (p *PhantomServer) EncodeQuery(session uint16, seq uint32, payload []byte, now time.Time) string {
	raw := make([]byte, 6+len(payload))
	raw[0] = byte(session >> 8)
	raw[1] = byte(session)
	raw[2] = byte(seq >> 24)
	raw[3] = byte(seq >> 16)
	raw[4] = byte(seq >> 8)
	raw[5] = byte(seq)
	copy(raw[6:], payload)
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	return strings.ToLower(enc.EncodeToString(raw)) + "." + p.DomainAt(now)
}

// DecodeQuery استخراج session/seq/payload از qname
func (p *PhantomServer) DecodeQuery(qname string) (session uint16, seq uint32, payload []byte, err error) {
	label := strings.Split(qname, ".")[0]
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	raw, err := enc.DecodeString(strings.ToUpper(label))
	if err != nil || len(raw) < 6 {
		return 0, 0, nil, fmt.Errorf("phantom: bad query label")
	}
	session = uint16(raw[0])<<8 | uint16(raw[1])
	seq = uint32(raw[2])<<24 | uint32(raw[3])<<16 | uint32(raw[4])<<8 | uint32(raw[5])
	return session, seq, raw[6:], nil
}

// EncodeAnswer پاسخ TXT downstream
func EncodeAnswer(frame []byte) []string {
	s := base64.StdEncoding.EncodeToString(frame)
	const chunk = 200 // محدودیت رکورد TXT
	var out []string
	for len(s) > 0 {
		n := chunk
		if n > len(s) {
			n = len(s)
		}
		out = append(out, s[:n])
		s = s[n:]
	}
	return out
}
