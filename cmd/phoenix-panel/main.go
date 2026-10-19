// phoenix-panel — باینری پنل کنترل
//
//	استفاده: phoenix-panel -listen 0.0.0.0:2053 -data /var/lib/phoenix-panel \
//	           -local-profile /etc/phoenix/profile.json
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/phoenixtunnel/phoenix/panel"
)

var version = "dev"

func main() {
	listen := flag.String("listen", "127.0.0.1:2053", "listen address")
	data := flag.String("data", "./panel-data", "data directory")
	adminUser := flag.String("admin-user", "admin", "initial admin username (only when no user exists)")
	adminPass := flag.String("admin-pass", "", "initial admin password (only when no user exists)")
	resetPass := flag.Bool("reset-admin", false, "set -admin-user's password to -admin-pass and exit")
	localProfile := flag.String("local-profile", "/etc/phoenix/profile.json", "profile.json of the core on this server ('' = none)")
	localStats := flag.String("local-stats", "http://127.0.0.1:9090/stats", "stats URL of the local core ('' = none)")
	certFile := flag.String("tls-cert", "", "TLS certificate (optional, enables HTTPS)")
	keyFile := flag.String("tls-key", "", "TLS key")
	flag.Parse()

	store, err := panel.NewStore(*data)
	if err != nil {
		log.Fatalf("store: %v", err)
	}

	if *resetPass {
		if *adminPass == "" {
			log.Fatal("-admin-pass required")
		}
		if err := store.SetPassword(*adminUser, *adminPass); err != nil {
			log.Fatal(err)
		}
		log.Printf("password set for %s", *adminUser)
		return
	}

	// ساخت کاربر ادمین اولیه فقط اگر هیچ کاربری وجود ندارد
	if !store.HasUsers() {
		if *adminPass == "" {
			log.Fatal("no users yet: start once with -admin-pass '<strong password>'")
		}
		store.CreateUser(&panel.User{Username: *adminUser, Password: *adminPass, Role: "admin"})
		log.Printf("initial admin created: %s", *adminUser)
	}

	pin := ""
	if *certFile != "" {
		if b, err := os.ReadFile(*certFile); err == nil {
			if blk, _ := pem.Decode(b); blk != nil {
				sum := sha256.Sum256(blk.Bytes)
				pin = hex.EncodeToString(sum[:])
			}
		}
	}
	api := panel.NewAPI(store, panel.Options{
		LocalProfile: *localProfile, LocalStatsURL: *localStats, Version: version, TLSPin: pin,
	})
	srv := &http.Server{
		Addr: *listen, Handler: api.Handler(),
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second,
	}
	log.Printf("PhoenixTunnel panel %s listening on %s", version, *listen)
	if *certFile != "" {
		log.Fatal(srv.ListenAndServeTLS(*certFile, *keyFile))
	}
	log.Fatal(srv.ListenAndServe())
}
