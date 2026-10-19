package server

import (
	"context"
	"encoding/binary"
	"net"
	"time"

	"github.com/phoenixtunnel/phoenix/core"
)

// ---------------- DNS wire format (بدون وابستگی خارجی) ----------------

type dnsQuery struct {
	id    uint16
	flags uint16
	name  string
	qtype uint16
}

// parseDNSQuery استخراج ID و نام دامنه از پرسش DNS
func parseDNSQuery(b []byte) (*dnsQuery, error) {
	if len(b) < 12 {
		return nil, errShort
	}
	q := &dnsQuery{
		id:    binary.BigEndian.Uint16(b[0:2]),
		flags: binary.BigEndian.Uint16(b[2:4]),
	}
	// خواندن QNAME (labels)
	var labels []string
	off := 12
	for {
		if off >= len(b) {
			return nil, errShort
		}
		l := int(b[off])
		if l == 0 {
			off++
			break
		}
		if l&0xC0 != 0 { // pointer در Question مجاز نیست
			return nil, errBadName
		}
		off++
		if off+l > len(b) {
			return nil, errShort
		}
		labels = append(labels, string(b[off:off+l]))
		off += l
	}
	q.name = joinLabels(labels)
	if off+4 > len(b) {
		return nil, errShort
	}
	q.qtype = binary.BigEndian.Uint16(b[off : off+2])
	return q, nil
}

func joinLabels(ls []string) string {
	out := ""
	for i, l := range ls {
		if i > 0 {
			out += "."
		}
		out += l
	}
	return out
}

// buildDNSResponse ساخت پاسخ DNS با یک رکورد TXT در Answer
// answers: رشته‌های TXT (هر کدام ≤ ۲۵۵ بایت)
func buildDNSResponse(q *dnsQuery, answers []string) []byte {
	out := make([]byte, 12, 512)
	binary.BigEndian.PutUint16(out[0:2], q.id)
	// flags: QR=1 AA=1 RD=کپی از پرسش
	rd := q.flags & 0x0100
	binary.BigEndian.PutUint16(out[2:4], 0x8000|0x0400|rd)
	binary.BigEndian.PutUint16(out[4:6], 1) // QDCOUNT
	binary.BigEndian.PutUint16(out[6:8], uint16(len(answers)))
	// Question section: pointer به QNAME خود پرسش + QTYPE/QCLASS
	out = append(out, 0xC0, 0x0C)
	out = append(out, byte(q.qtype>>8), byte(q.qtype), 0, 1)
	for _, a := range answers {
		if len(a) > 255 {
			a = a[:255]
		}
		out = append(out, byte(len(a)))
		out = append(out, a...)
	}
	return out
}

// forwardDNS پرسش را به ریزالور upstream می‌فرستد و پاسخ را برمی‌گرداند
func forwardDNS(raw []byte, upstream string) ([]byte, error) {
	conn, err := net.DialTimeout("udp", upstream, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(4 * time.Second))
	if _, err := conn.Write(raw); err != nil {
		return nil, err
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

// مابقی متدهای PhantomEngine
func (p *PhantomEngine) Dial(ctx context.Context, addr string) (net.Conn, error) {
	return net.DialTimeout("udp", addr, 5*time.Second)
}
func (p *PhantomEngine) ProbeOK(challenge []byte) bool { return true }
func (p *PhantomEngine) Stats() core.Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stats
}
func (p *PhantomEngine) Close() error {
	if p.pc != nil {
		return p.pc.Close()
	}
	return nil
}
