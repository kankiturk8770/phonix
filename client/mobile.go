// mobile.go — API موبایل (اندروید) روی VPN؛ پکیج mobile/ آن را به gomobile می‌دهد.
package client

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/phoenixtunnel/phoenix/core"
)

// MobileConfig کانفیگ کلاینت موبایل — همان قالب لینک phoenix://
type MobileConfig = core.ClientConfig

var (
	mobileMu   sync.Mutex
	mobileVPNs = map[int]*VPN{}
	mobileSeq  int
)

// StartVPN شروع تونل روی یک TUN fd (دستگیره‌ی fd اندروید).
// config می‌تواند JSON کانفیگ یا لینک phoenix:// باشد.
// مالکیت fd به هسته منتقل می‌شود (سمت Kotlin باید detachFd کند).
// خروجی: شناسه‌ی نشست برای StopVPN — در گوروتین جدا اجرا می‌شود.
func StartVPN(fd int, config string) (int, error) {
	cfg, err := core.DecodeLink(config)
	if err != nil {
		syscall.Close(fd)
		return 0, fmt.Errorf("config: %w", err)
	}
	// fd غیرمسدود → poller گو؛ بدون این، Close خواندنِ در جریان را قطع نمی‌کند
	// و رابط VPN بعد از قطع باز می‌ماند.
	if err := syscall.SetNonblock(fd, true); err != nil {
		syscall.Close(fd)
		return 0, fmt.Errorf("tun fd: %w", err)
	}
	f := os.NewFile(uintptr(fd), "tun")
	if f == nil {
		return 0, fmt.Errorf("bad fd %d", fd)
	}
	vpn := NewVPN(f, NewDialer(cfg))
	if cfg.DNS != "" {
		vpn.DNSUpstream = cfg.DNS
	}

	mobileMu.Lock()
	mobileSeq++
	id := mobileSeq
	mobileVPNs[id] = vpn
	mobileMu.Unlock()

	go func() {
		vpn.Run()
		vpn.Close()
		mobileMu.Lock()
		delete(mobileVPNs, id)
		mobileMu.Unlock()
	}()
	return id, nil
}

// NewDialer ساخت تابع dial تونل بر اساس موتور کانفیگ
func NewDialer(cfg core.ClientConfig) func(target string) (net.Conn, error) {
	keys := []byte(cfg.Keys)
	return func(target string) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(context.Background(), coreDialTimeout)
		defer cancel()
		if cfg.Engine == "phoenix-udp" {
			return DialPhoenixUDP(ctx, PUDPConf{Server: firstNonEmpty(cfg.UDPServer, cfg.Server), Keys: keys}, target)
		}
		return DialChameleon(ctx, ChameleonConf{
			Server: cfg.Server, Keys: keys, FakeSNIs: cfg.FakeSNIs,
		}, target)
	}
}

// TestConnection یک اتصال آزمایشی کامل (handshake + مقصد) و زمان آن به میلی‌ثانیه
func TestConnection(config string) (int, error) {
	cfg, err := core.DecodeLink(config)
	if err != nil {
		return 0, err
	}
	start := time.Now()
	c, err := NewDialer(cfg)("1.1.1.1:443")
	if err != nil {
		return 0, err
	}
	c.Close()
	return int(time.Since(start).Milliseconds()), nil
}

// WaitVPN مسدود می‌شود تا پایان نشست
func WaitVPN(id int) {
	mobileMu.Lock()
	vpn := mobileVPNs[id]
	mobileMu.Unlock()
	if vpn != nil {
		<-vpn.Done()
	}
}

// StopVPN توقف نشست
func StopVPN(id int) {
	mobileMu.Lock()
	vpn := mobileVPNs[id]
	delete(mobileVPNs, id)
	mobileMu.Unlock()
	if vpn != nil {
		vpn.Close()
		select {
		case <-vpn.Done():
		case <-time.After(3 * time.Second):
		}
	}
}

// VPNTraffic بایت‌های ارسال و دریافت نشست
func VPNTraffic(id int) (up, down int64) {
	mobileMu.Lock()
	vpn := mobileVPNs[id]
	mobileMu.Unlock()
	if vpn == nil {
		return 0, 0
	}
	return vpn.Traffic()
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

const coreDialTimeout = 10 * time.Second
