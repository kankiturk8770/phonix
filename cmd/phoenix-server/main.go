// phoenix-server — دیمون اصلی سرور (لینوکس)
// استفاده: phoenix-server -config /etc/phoenix/profile.json
package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"

	"github.com/phoenixtunnel/phoenix/core"
	"github.com/phoenixtunnel/phoenix/server"
)

func main() {
	cfg := flag.String("config", "profile.json", "path to profile.json")
	flag.Parse()

	profile, err := core.LoadProfile(*cfg)
	if err != nil {
		log.Fatalf("load profile: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("PhoenixTunnel server starting — node %q", profile.Name)
	if err := server.New(profile).Run(ctx); err != nil {
		log.Fatalf("server: %v", err)
	}
	log.Println("shutdown complete")
}
