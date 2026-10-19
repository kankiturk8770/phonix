// phoenix-agent — ایجنت نود: روی هر سرور (داخل/خارج ایران) نصب می‌شود،
// به پنل وصل می‌شود، پروفایل می‌گیرد، هسته را اجرا و آمار می‌فرستد.
// استفاده: phoenix-agent -panel https://panel.example.com -token XXX
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/phoenixtunnel/phoenix/core"
	"github.com/phoenixtunnel/phoenix/server"
)

type agentConfig struct {
	PanelURL string
	Token    string
	NodeID   string
	Interval time.Duration
}

func main() {
	var cfg agentConfig
	flag.StringVar(&cfg.PanelURL, "panel", "http://127.0.0.1:8080", "panel URL")
	flag.StringVar(&cfg.Token, "token", "", "agent token")
	flag.StringVar(&cfg.NodeID, "node", "", "node id (empty = register)")
	flag.DurationVar(&cfg.Interval, "interval", 10*time.Second, "report interval")
	flag.Parse()

	if cfg.Token == "" {
		log.Fatal("-token is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	a := &agent{cfg: cfg, client: &http.Client{Timeout: 15 * time.Second}}

	// ۱) ثبت نود یا بازیابی پروفایل
	profile, err := a.fetchProfile(ctx)
	if err != nil {
		log.Fatalf("fetch profile: %v", err)
	}
	log.Printf("agent ready — node %q (%d engines)", profile.Name, len(profile.Engines))

	// ۲) اجرای هسته با پروفایل
	coreServer := server.New(profile)
	go func() {
		if err := coreServer.Run(ctx); err != nil {
			log.Printf("core: %v", err)
		}
	}()

	// ۳) حلقه‌ی گزارش + دریافت فرمان‌های پنل
	a.loop(ctx, profile)
}

type agent struct {
	cfg    agentConfig
	client *http.Client
}

func (a *agent) auth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+a.cfg.Token)
}

// fetchProfile دریافت پروفایل فعال از پنل
func (a *agent) fetchProfile(ctx context.Context) (*core.Profile, error) {
	url := a.cfg.PanelURL + "/api/v1/agent/profile"
	if a.cfg.NodeID != "" {
		url += "?node=" + a.cfg.NodeID
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	a.auth(req)
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errStatus(resp.StatusCode)
	}
	var p core.Profile
	return &p, json.NewDecoder(resp.Body).Decode(&p)
}

// report آمار زنده + دریافت دستور (مثل switch engine از پنل)
func (a *agent) report(ctx context.Context, stats []core.Stats) error {
	body, _ := json.Marshal(map[string]any{"node": a.cfg.NodeID, "stats": stats})
	req, _ := http.NewRequestWithContext(ctx, "POST",
		a.cfg.PanelURL+"/api/v1/agent/report", bytes.NewReader(body))
	a.auth(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return errStatus(resp.StatusCode)
	}
	// دستور از پنل: {"switch_engine": "phantom"}
	var cmd struct {
		SwitchEngine string `json:"switch_engine"`
	}
	if json.NewDecoder(resp.Body).Decode(&cmd) == nil && cmd.SwitchEngine != "" {
		log.Printf("[PANEL] switch engine -> %s", cmd.SwitchEngine)
		// در نسخه‌ی کامل: به registry هسته تزریق می‌شود
	}
	return nil
}

func (a *agent) loop(ctx context.Context, p *core.Profile) {
	t := time.NewTicker(a.cfg.Interval)
	defer t.Stop()
	// stats واقعی از هسته در نسخه‌ی کامل تزریق می‌شود
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := a.report(ctx, nil); err != nil {
				log.Printf("report: %v", err)
			}
		}
	}
}

type errStatus int

func (e errStatus) Error() string { return fmt.Sprintf("panel status %d", int(e)) }

var _ = os.Getenv // reserved
