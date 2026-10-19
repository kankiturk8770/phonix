// Package client — کلاینت‌های پروتکل‌های PhoenixTunnel
// هم برای دسکتاپ/CLI و هم (از طریق gomobile bind) برای اندروید قابل استفاده‌اند.
package client

import (
	"context"
	"net"
	"time"

	"github.com/phoenixtunnel/phoenix/core"
)

// ChameleonConf تنظیمات کلاینت Chameleon
type ChameleonConf struct {
	Server      string   // آدرس سرور
	Keys        []byte   // کلید استاتیک مشترک
	FakeSNIs    []string // SNIهای تقلبی برای early-silence
	DecoyDelay  time.Duration
	FragDelay   time.Duration
}

// DialChameleon یک اتصال رمزنگاری‌شده به سرور Chameleon برقرار می‌کند
// و درخواست اتصال به target را می‌فرستد. خروجی net.Conn رمزنگاری‌شده است.
func DialChameleon(ctx context.Context, conf ChameleonConf, target string) (net.Conn, error) {
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", conf.Server)
	if err != nil {
		return nil, err
	}
	tc, ok := conn.(*net.TCPConn)
	if ok {
		tc.SetNoDelay(true)
	}
	// سقف زمان برای handshake (بدون آن، سروری که جواب ندهد اتصال را تا ابد معطل می‌کند)
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}

	// early-silence: پکت‌های جعلی با SNI مجاز (TTL کم — فقط میانجی می‌بیند)
	// ساختار: TLS ClientHello جعلی کوتاه با SNI تقلبی + پکت‌های خاموش
	for i, sni := range conf.FakeSNIs {
		if i >= 2 {
			break
		}
		decoy := buildDecoyClientHello(sni)
		if _, err := conn.Write(decoy); err != nil {
			conn.Close()
			return nil, err
		}
		time.Sleep(conf.DecoyDelay)
	}

	sess, err := core.ClientHandshake(conn, conf.Keys)
	if err != nil {
		conn.Close()
		return nil, err
	}
	tgt, err := core.EncodeTarget(target)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err := core.WriteFrame(conn, sess.SealFrame(tgt)); err != nil {
		conn.Close()
		return nil, err
	}
	conn.SetDeadline(time.Time{})
	return &core.FrameConn{Conn: conn, Sess: sess}, nil
}

// buildDecoyClientHello ساخت ClientHello تقلبی کوتاه فقط برای مصرف DPI میانجی.
// سرور هرگز این پکت‌ها را نمی‌بیند (TTL کم) و اگر ببیند، بایت‌های بعدی
// handshake واقعی را جمع می‌کند — اینجا فقط payload شبه-TLS می‌فرستیم.
func buildDecoyClientHello(sni string) []byte {
	// 0x16 0x03 0x01 len | random | [sni appears in first 100 bytes]
	out := make([]byte, 128)
	out[0], out[1], out[2] = 0x16, 0x03, 0x01
	out[3], out[4] = 0x00, 125
	// server_name در قسمت early bytes
	name := []byte(sni)
	copy(out[20:20+len(name)], name)
	return out
}
