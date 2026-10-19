package core

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// لینک اتصال کلاینت: phoenix://<base64url(JSON)>#<نام>
// سرور (phoenix-server -link) و پنل این لینک را می‌سازند؛ اپ اندروید
// با Paste یا اسکن QR آن را وارد می‌کند و همه‌ی تنظیمات فعال می‌شود.

const LinkScheme = "phoenix://"

// ClientConfig کانفیگ کامل کلاینت — همان JSONی که هسته‌ی موبایل می‌خواند
type ClientConfig struct {
	V         int      `json:"v"`
	Name      string   `json:"name,omitempty"`
	Server    string   `json:"server"` // host:port موتور chameleon
	Keys      string   `json:"keys"`   // کلید مشترک (همان keys.static_priv سرور)
	Engine    string   `json:"engine"` // chameleon | phoenix-udp
	FakeSNIs  []string `json:"fake_snis,omitempty"`
	UDPServer string   `json:"udp_server,omitempty"` // host:port موتور phoenix-udp
	DNS       string   `json:"dns,omitempty"`        // DNS upstream داخل تونل
}

// EncodeLink ساخت لینک phoenix://
func EncodeLink(c ClientConfig) string {
	if c.V == 0 {
		c.V = 1
	}
	b, _ := json.Marshal(c)
	link := LinkScheme + base64.RawURLEncoding.EncodeToString(b)
	if c.Name != "" {
		link += "#" + url.PathEscape(c.Name)
	}
	return link
}

// DecodeLink خواندن لینک phoenix:// (یا JSON خام کانفیگ)
func DecodeLink(s string) (ClientConfig, error) {
	var c ClientConfig
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "{") {
		if err := json.Unmarshal([]byte(s), &c); err != nil {
			return c, err
		}
		return c, c.Validate()
	}
	if !strings.HasPrefix(strings.ToLower(s), LinkScheme) {
		return c, errors.New("لینک باید با phoenix:// شروع شود")
	}
	s = s[len(LinkScheme):]
	name := ""
	if i := strings.IndexByte(s, '#'); i >= 0 {
		name, _ = url.PathUnescape(s[i+1:])
		s = s[:i]
	}
	s = strings.TrimRight(s, "=/")
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		if b, err = base64.RawStdEncoding.DecodeString(s); err != nil {
			return c, errors.New("لینک خراب است")
		}
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, errors.New("محتوای لینک خراب است")
	}
	if c.Name == "" {
		c.Name = name
	}
	return c, c.Validate()
}

// Validate بررسی حداقل فیلدهای لازم
func (c *ClientConfig) Validate() error {
	if c.Engine == "" {
		c.Engine = "chameleon"
	}
	if c.Keys == "" {
		return errors.New("کلید در کانفیگ نیست")
	}
	switch c.Engine {
	case "chameleon":
		if _, _, err := net.SplitHostPort(c.Server); err != nil {
			return errors.New("آدرس سرور نامعتبر است (host:port)")
		}
	case "phoenix-udp":
		addr := c.UDPServer
		if addr == "" {
			addr = c.Server
		}
		if _, _, err := net.SplitHostPort(addr); err != nil {
			return errors.New("آدرس سرور UDP نامعتبر است (host:port)")
		}
	default:
		return errors.New("موتور ناشناخته: " + c.Engine)
	}
	return nil
}

// ClientConfigFromProfile ساخت کانفیگ کلاینت از روی پروفایل سرور.
// host آدرس عمومی سرور (IP یا دامنه) است که گوشی به آن وصل می‌شود.
func ClientConfigFromProfile(p *Profile, host string) (ClientConfig, error) {
	c := ClientConfig{V: 1, Name: p.Name, Keys: p.Keys.StaticPriv, Engine: "chameleon"}
	if c.Name == "" {
		c.Name = p.NodeID
	}
	for _, e := range p.Engines {
		if !e.Enabled {
			continue
		}
		port := listenPort(e.Listen)
		if port == "" {
			continue
		}
		switch e.Name {
		case "chameleon":
			c.Server = net.JoinHostPort(host, port)
		case "phoenix-udp":
			c.UDPServer = net.JoinHostPort(host, port)
		}
	}
	if c.Server == "" {
		if c.UDPServer == "" {
			return c, errors.New("هیچ موتور chameleon یا phoenix-udp فعالی در پروفایل نیست")
		}
		c.Engine = "phoenix-udp"
	}
	sni := p.Chameleon.FakeSNIs
	if len(sni) > 2 {
		sni = sni[:2]
	}
	c.FakeSNIs = sni
	if c.Keys == "" {
		return c, errors.New("keys.static_priv در پروفایل خالی است")
	}
	return c, nil
}

func listenPort(listen string) string {
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		if _, e := strconv.Atoi(listen); e == nil {
			return listen
		}
		return ""
	}
	return port
}
