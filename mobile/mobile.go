// Package mobile — رابط ساده‌ی gomobile برای اپ اندروید.
// فقط انواع پایه (int/string/error) صادر می‌شوند تا bind تمیز باشد.
//
//	gomobile bind -target=android -javapkg ir.phoenix.core \
//	    -o android-app/app/libs/phoenix.aar ./mobile
//
// کلاس Java/Kotlin تولیدی: ir.phoenix.core.mobile.Mobile
package mobile

import (
	"encoding/json"
	"fmt"

	"github.com/phoenixtunnel/phoenix/client"
	"github.com/phoenixtunnel/phoenix/core"
)

// version نسخه‌ی هسته (هنگام بیلد با -ldflags "-X .../mobile.version=..." تنظیم می‌شود)
var version = "dev"

// GetVersion نسخه‌ی هسته
func GetVersion() string { return version }

// StartVPN شروع VPN روی TUN fd با لینک phoenix:// یا JSON کانفیگ؛ شناسه‌ی نشست
func StartVPN(fd int, config string) (int, error) { return client.StartVPN(fd, config) }

// StopVPN توقف نشست
func StopVPN(id int) { client.StopVPN(id) }

// WaitVPN مسدود تا پایان نشست
func WaitVPN(id int) { client.WaitVPN(id) }

// Traffic آمار نشست به‌صورت "up,down" (بایت)
func Traffic(id int) string {
	up, down := client.VPNTraffic(id)
	return fmt.Sprintf("%d,%d", up, down)
}

// ParseLink لینک phoenix:// را به JSON کانفیگ تبدیل می‌کند (برای ذخیره در اپ)
func ParseLink(link string) (string, error) {
	c, err := core.DecodeLink(link)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(c)
	return string(b), err
}

// BuildLink JSON کانفیگ را به لینک phoenix:// تبدیل می‌کند (برای اشتراک)
func BuildLink(configJSON string) (string, error) {
	c, err := core.DecodeLink(configJSON)
	if err != nil {
		return "", err
	}
	return core.EncodeLink(c), nil
}

// Ping تست اتصال واقعی به سرور؛ زمان به میلی‌ثانیه
func Ping(config string) (int, error) { return client.TestConnection(config) }
