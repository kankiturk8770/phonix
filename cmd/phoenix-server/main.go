// phoenix-server — دیمون اصلی سرور (لینوکس)
//
//	phoenix-server -init -host 1.2.3.4      ساخت پروفایل کامل (کلید تصادفی + گواهی) و چاپ لینک
//	phoenix-server -link                    چاپ لینک اتصال اپ + QR در ترمینال
//	phoenix-server -config /etc/phoenix/profile.json   اجرای سرور
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/phoenixtunnel/phoenix/core"
	"github.com/phoenixtunnel/phoenix/server"
)

var version = "dev"

func main() {
	cfg := flag.String("config", "/etc/phoenix/profile.json", "path to profile.json")
	initMode := flag.Bool("init", false, "create a ready profile (random key + certs) and print the client link")
	force := flag.Bool("force", false, "with -init: overwrite an existing profile (new key!)")
	linkMode := flag.Bool("link", false, "print the client link + QR code and exit")
	host := flag.String("host", "", "public IP or domain of this server (stored in profile, used in the link)")
	name := flag.String("name", "", "server name shown in the app (with -init)")
	port := flag.Int("port", 443, "chameleon TCP port (with -init)")
	udpPort := flag.Int("udp-port", 51820, "phoenix-udp port (with -init)")
	statsAddr := flag.String("stats", "127.0.0.1:9090", "local HTTP for stats/link (used by the panel); empty = off")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	if *initMode {
		if err := doInit(*cfg, *host, *name, *port, *udpPort, *force); err != nil {
			log.Fatalf("init: %v", err)
		}
		printLink(*cfg, "")
		return
	}
	if *linkMode {
		printLink(*cfg, *host)
		return
	}

	profile, err := core.LoadProfile(*cfg)
	if err != nil {
		log.Fatalf("load profile: %v", err)
	}
	// فایل‌های certs/ کنار پروفایل؛ موتور chameleon آن‌ها را نسبی باز می‌کند
	dir := filepath.Dir(*cfg)
	if err := os.Chdir(dir); err != nil {
		log.Fatalf("chdir %s: %v", dir, err)
	}
	if err := server.EnsureCerts(".", firstSNI(profile)); err != nil {
		log.Fatalf("certs: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("PhoenixTunnel server %s starting — node %q", version, profile.Name)
	if h := profile.Meta["public_host"]; h != "" {
		if c, err := core.ClientConfigFromProfile(profile, h); err == nil {
			log.Printf("client link: %s", core.EncodeLink(c))
		}
	}
	srv := server.New(profile)
	if *statsAddr != "" {
		go serveStats(*statsAddr, srv, profile)
	}
	if err := srv.Run(ctx); err != nil {
		log.Fatalf("server: %v", err)
	}
	log.Println("shutdown complete")
}

func doInit(path, host, name string, port, udpPort int, force bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil && !force {
		// پروفایل موجود: کلید حفظ می‌شود، فقط host/name به‌روز می‌شوند
		p, err := core.LoadProfile(path)
		if err != nil {
			return err
		}
		if p.Meta == nil {
			p.Meta = map[string]string{}
		}
		if host != "" {
			p.Meta["public_host"] = host
		}
		if name != "" {
			p.Name = name
		}
		if p.Keys.StaticPriv == "" || p.Keys.StaticPriv == "CHANGE-ME-32-BYTES-BASE64" {
			p.Keys.StaticPriv = core.RandomKey()
		}
		if err := core.SaveProfile(path, p); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "profile kept (key unchanged): %s\n", path)
		return server.EnsureCerts(filepath.Dir(path), firstSNI(p))
	}
	p := core.DefaultProfile(name, host, port, udpPort)
	if err := core.SaveProfile(path, p); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "new profile created: %s\n", path)
	return server.EnsureCerts(filepath.Dir(path), firstSNI(p))
}

func printLink(path, host string) {
	p, err := core.LoadProfile(path)
	if err != nil {
		log.Fatalf("load profile: %v", err)
	}
	if host == "" {
		host = p.Meta["public_host"]
	}
	if host == "" {
		log.Fatal("public host unknown — run with -host <server IP or domain>")
	}
	c, err := core.ClientConfigFromProfile(p, host)
	if err != nil {
		log.Fatal(err)
	}
	link := core.EncodeLink(c)
	if qr, err := qrcode.New(link, qrcode.Low); err == nil {
		fmt.Println(qr.ToSmallString(false))
	}
	fmt.Println("Phoenix client link (paste or scan in the Android app):")
	fmt.Println()
	fmt.Println(link)
	fmt.Println()
}

// serveStats HTTP محلی: /stats آمار موتورها، /link لینک کلاینت (فقط localhost)
func serveStats(addr string, srv *server.Server, p *core.Profile) {
	mux := http.NewServeMux()
	start := time.Now()
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"version": version, "name": p.Name, "uptime_sec": int(time.Since(start).Seconds()),
			"engines": srv.Stats(),
		})
	})
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Printf("stats http: %v", err)
	}
}

func firstSNI(p *core.Profile) string {
	if len(p.Chameleon.FakeSNIs) > 0 {
		return p.Chameleon.FakeSNIs[0]
	}
	return ""
}
