package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
)

// RandomKey کلید مشترک تصادفی ۳۲ بایتی (hex)
func RandomKey() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// DefaultProfile پروفایل کامل و آماده‌ی اجرا برای یک سرور تازه
func DefaultProfile(name, host string, chameleonPort, udpPort int) *Profile {
	if chameleonPort == 0 {
		chameleonPort = 443
	}
	if udpPort == 0 {
		udpPort = 51820
	}
	seed := RandomKey()
	p := &Profile{
		NodeID: "node-" + seed[:8],
		Name:   name,
		Engines: []EngineConf{
			{Name: "chameleon", Enabled: true, Listen: "0.0.0.0:" + strconv.Itoa(chameleonPort)},
			{Name: "reality", Enabled: true, Listen: "0.0.0.0:8443"},
			{Name: "phoenix-udp", Enabled: true, Listen: "0.0.0.0:" + strconv.Itoa(udpPort)},
			{Name: "phantom", Enabled: true, Listen: "0.0.0.0:53"},
		},
		Chameleon: ChameleonConf{
			Enabled:             true,
			FakeSNIs:            []string{"www.google.com", "www.microsoft.com", "cdn.jsdelivr.net"},
			FragmentOffsets:     []int{2, 5, 1, 3},
			EarlySilencePackets: 3,
			CoverEnabled:        true,
			CoverMinBytes:       256,
			CoverMaxBytes:       1400,
			MimicProfile:        "chrome-android",
		},
		Hydra: HydraConf{Enabled: true, Channels: 4, FrameSize: 1200, FEC_K: 8, FEC_N: 12},
		Phantom: PhantomConf{
			Enabled: true, Domain: "ns1.example.com", DGASeed: seed,
			RotateMinutes: 10, ResolversUp: []string{"1.1.1.1:53", "8.8.8.8:53"}, MaxSessions: 8,
		},
		Rotation: RotationConf{Enabled: true, IntervalMinutes: 30},
		Fallback: FallbackConf{ProbeIntervalSeconds: 3, FailureThreshold: 0.6},
		Keys:     KeyMaterial{StaticPriv: RandomKey()},
		Meta:     map[string]string{"public_host": host, "camouflage": "www.microsoft.com:443"},
	}
	if p.Name == "" {
		p.Name = "Phoenix"
	}
	return p
}

// SaveProfile نوشتن پروفایل روی دیسک (فقط root بخواند)
func SaveProfile(path string, p *Profile) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
