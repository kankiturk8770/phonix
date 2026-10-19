package core

import (
	"encoding/json"
	"fmt"
	"os"
)

// Profile پروفایل کامل یک نود — از پنل ساخته و به هسته/کلاینت می‌رسد
type Profile struct {
	NodeID        string            `json:"node_id"`
	Name          string            `json:"name"`
	Engines       []EngineConf      `json:"engines"`        // موتورهای فعال روی این نود
	Chameleon     ChameleonConf     `json:"chameleon"`
	Hydra         HydraConf         `json:"hydra"`
	Phantom       PhantomConf       `json:"phantom"`
	Rotation      RotationConf      `json:"rotation"`
	Fallback      FallbackConf      `json:"fallback"`
	ReverseTunnels []ReverseConf    `json:"reverse_tunnels"` // تونل‌های معکوس از داخل ایران
	Keys          KeyMaterial       `json:"keys"`
	Meta          map[string]string `json:"meta,omitempty"`
}

type EngineConf struct {
	Name    string `json:"name"`    // chameleon | hydra | phantom | hysteria2 | reality
	Enabled bool   `json:"enabled"`
	Listen  string `json:"listen"`  // مثال: "0.0.0.0:443"
}

type ChameleonConf struct {
	Enabled          bool     `json:"enabled"`
	FakeSNIs         []string `json:"fake_snis"`          // SNIهای مجاز/تقلبی
	FragmentOffsets  []int    `json:"fragment_offsets"`   // آفست‌های تکه‌تکه کردن ClientHello
	EarlySilencePackets int   `json:"early_silence_packets"` // تعداد پکت اولیه‌ی جعلی
	CoverEnabled     bool     `json:"cover_enabled"`      // cover traffic بیکاری
	CoverMinBytes    int      `json:"cover_min_bytes"`
	CoverMaxBytes    int      `json:"cover_max_bytes"`
	FingerprintDB    string   `json:"fingerprint_db"`     // مسیر بانک fingeprint
	MimicProfile     string   `json:"mimic_profile"`      // chrome-android | instagram-quic | ...
}

type HydraConf struct {
	Enabled     bool   `json:"enabled"`
	Channels    int    `json:"channels"`     // تعداد کانال‌های موازی
	FrameSize   int    `json:"frame_size"`   // اندازه‌ی قاب (پیش‌فرض 1200)
	FEC_K       int    `json:"fec_k"`        // Reed-Solomon k
	FEC_N       int    `json:"fec_n"`
	Weights     map[string]int `json:"weights"` // وزن کانال‌ها
}

type PhantomConf struct {
	Enabled        bool     `json:"enabled"`
	Domain         string   `json:"domain"`          // ns1.example.com
	DGASeed        string   `json:"dga_seed"`        // کلید DGA برای چرخش دامنه
	RotateMinutes  int      `json:"rotate_minutes"`
	ResolversUp    []string `json:"resolvers_up"`    // مسیر رفت
	ResolversDown  []string `json:"resolvers_down"`  // مسیر برگشت
	MaxSessions    int      `json:"max_sessions"`
}

type RotationConf struct {
	Enabled         bool `json:"enabled"`
	IntervalMinutes int  `json:"interval_minutes"`
}

type FallbackConf struct {
	ProbeIntervalSeconds int `json:"probe_interval_seconds"`
	FailureThreshold     float64 `json:"failure_threshold"`
}

// ReverseConf تونل معکوس: سرور ایران به سرور خارج وصل می‌شود، نه برعکس
type ReverseConf struct {
	Name     string `json:"name"`
	Type     string `json:"type"`      // ssh | websocket | grpc | iperf-mimic | cdn-pull
	Endpoint string `json:"endpoint"`  // آدرس سرور خارج
	Secret   string `json:"secret"`
}

type KeyMaterial struct {
	StaticPub  string `json:"static_pub"`
	StaticPriv string `json:"static_priv"`
	PQEnabled  bool   `json:"pq_enabled"` // Kyber برای آینده
}

// LoadProfile خواندن پروفایل از فایل
func LoadProfile(path string) (*Profile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Profile
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("bad profile %s: %w", path, err)
	}
	p.applyDefaults()
	return &p, nil
}

func (p *Profile) applyDefaults() {
	if p.Hydra.FrameSize == 0 {
		p.Hydra.FrameSize = 1200
	}
	if p.Hydra.FEC_K == 0 {
		p.Hydra.FEC_K, p.Hydra.FEC_N = 8, 12
	}
	if p.Phantom.RotateMinutes == 0 {
		p.Phantom.RotateMinutes = 10
	}
	if p.Phantom.MaxSessions == 0 {
		p.Phantom.MaxSessions = 8
	}
	if p.Fallback.ProbeIntervalSeconds == 0 {
		p.Fallback.ProbeIntervalSeconds = 3
	}
	if p.Fallback.FailureThreshold == 0 {
		p.Fallback.FailureThreshold = 0.6
	}
}
