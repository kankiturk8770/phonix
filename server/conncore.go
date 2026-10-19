package server

import (
	"net"
	"sync/atomic"

	"github.com/phoenixtunnel/phoenix/core"
)

// readFrame/writeFrame/frameConn در core/framing.go به‌صورت عمومی منتقل شدند.
// اینجا aliasهای داخلی برای سازگاری با بقیه‌ی فایل‌های server نگه داشته شده‌اند.

var (
	readFrame  = core.ReadFrame
	writeFrame = core.WriteFrame
)

type frameConn = core.FrameConn

// countConn شمارش بایت روی یک Conn
type countConn struct {
	net.Conn
	up, down *int64
}

func (c *countConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 && c.down != nil {
		atomic.AddInt64(c.down, int64(n))
	}
	return n, err
}

func (c *countConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if n > 0 && c.up != nil {
		atomic.AddInt64(c.up, int64(n))
	}
	return n, err
}

// CloseWrite نیم‌بستن سمت مقصد (FIN) وقتی کلاینت ارسال را تمام کرد
func (c *countConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}
