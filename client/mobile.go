// mobile.go — API صادرشده برای gomobile bind (اندروید)
// بیلد: gomobile bind -target=android -o phoenix.aar github.com/phoenixtunnel/phoenix/client
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/phoenixtunnel/phoenix/core"
)

// MobileConfig کانفیگ ساده‌ی کلاینت موبایل
type MobileConfig struct {
	Server     string   `json:"server"`      // آدرس سرور Chameleon
	Keys       string   `json:"keys"`        // کلید استاتیک (base64 یا خام)
	Engine     string   `json:"engine"`      // chameleon | phoenix-udp
	FakeSNIs   []string `json:"fake_snis"`
	UDPServer  string   `json:"udp_server"`  // برای phoenix-udp
}

var (
	mobileMu   sync.Mutex
	mobileVPNs = map[int]*VPN{}
	mobileSeq  int
)

// StartVPN شروع تونل روی یک TUN fd (دستگیره‌ی fd اندروید).
// خروجی: شناسه‌ی نشست برای StopVPN — در گوروتین جدا اجرا می‌شود.
func StartVPN(fd int, configJSON string) (int, error) {
	var cfg MobileConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return 0, fmt.Errorf("config: %w", err)
	}
	if cfg.Engine == "" {
		cfg.Engine = "chameleon"
	}
	f := os.NewFile(uintptr(fd), "tun")
	if f == nil {
		return 0, fmt.Errorf("bad fd %d", fd)
	}
	keys := []byte(cfg.Keys)
	dial := func(target string) (net_Conn, error) {
		ctx, cancel := context.WithTimeout(context.Background(), coreDialTimeout)
		defer cancel()
		if cfg.Engine == "phoenix-udp" {
			return DialPhoenixUDP(ctx, PUDPConf{Server: firstNonEmpty(cfg.UDPServer, cfg.Server), Keys: keys}, target)
		}
		return DialChameleon(ctx, ChameleonConf{
			Server: cfg.Server, Keys: keys, FakeSNIs: cfg.FakeSNIs,
		}, target)
	}
	vpn := NewVPN(f, dial)

	mobileMu.Lock()
	mobileSeq++
	id := mobileSeq
	mobileVPNs[id] = vpn
	mobileMu.Unlock()

	go func() {
		vpn.Run()
		mobileMu.Lock()
		delete(mobileVPNs, id)
		mobileMu.Unlock()
	}()
	return id, nil
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
	}
}

// ProtectCallback تزریق تابع protect از اندروید — برای عبور سوکت‌های هسته
// از بیرون VPN interface (ضروری برای اندروید تا حلقه ایجاد نشود).
// در این نسخه کلاینت مستقیم به سرور وصل می‌شود و protect در سمت Kotlin
// با فراخوانی روی سوکت قبل از اتصال اعمال می‌شود.
type ProtectCallback interface {
	Protect(fd int) bool
}

var _ = core.EncodeTarget

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

type net_Conn = net.Conn

const coreDialTimeout = 10 * time.Second
