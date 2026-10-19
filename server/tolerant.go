package server

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"time"
)

// tolerantReadFrame خواندن اولین قاب با تحمل نویز early-silence.
// پکت‌های جعلی کلاینت ممکن است قبل از قاب اول برسند؛ اینجا پنجره‌ی بایت‌ها
// را می‌غزیم تا len معتبر (≤1MB) پیدا شود.
// نکته حیاتی: br باید با بقیه‌ی خوانده‌های کانکشن SHARED باشد وگرنه
// بایت‌های بافرشده گم می‌شوند — موتورها از bufConn استفاده می‌کنند.
func tolerantReadFrame(br *bufio.Reader, maxJunk int) ([]byte, error) {
	window := make([]byte, 0, 4+maxJunk)
	for len(window) < 4+maxJunk {
		b, err := br.ReadByte()
		if err != nil {
			return nil, err
		}
		window = append(window, b)
		if len(window) < 4 {
			continue
		}
		l := binary.BigEndian.Uint32(window[len(window)-4:])
		if l == 0 || l > 1<<20 {
			continue // هنوز هم‌راستا نیستیم — ادامه‌ی غزیدن
		}
		frame := make([]byte, l)
		if _, err := io.ReadFull(br, frame); err != nil {
			return nil, err
		}
		return frame, nil
	}
	return nil, io.ErrUnexpectedEOF
}

// bufConn کانکشنی که خواندن از بافر مشترک و نوشتن روی کانکشن خام را تلفیق می‌کند
type bufConn struct {
	br *bufio.Reader
	c  net.Conn
}

func (b *bufConn) Read(p []byte) (int, error)  { return b.br.Read(p) }
func (b *bufConn) Write(p []byte) (int, error) { return b.c.Write(p) }
func (b *bufConn) Close() error                { return b.c.Close() }
func (b *bufConn) LocalAddr() net.Addr         { return b.c.LocalAddr() }
func (b *bufConn) RemoteAddr() net.Addr        { return b.c.RemoteAddr() }
func (b *bufConn) SetDeadline(t time.Time) error {
	return b.c.SetDeadline(t)
}
func (b *bufConn) SetReadDeadline(t time.Time) error  { return b.c.SetReadDeadline(t) }
func (b *bufConn) SetWriteDeadline(t time.Time) error { return b.c.SetWriteDeadline(t) }
