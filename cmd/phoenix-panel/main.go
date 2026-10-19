// phoenix-panel — باینری پنل کنترل
// استفاده: phoenix-panel -listen 0.0.0.0:8080 -data /var/lib/phoenix-panel
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/phoenixtunnel/phoenix/panel"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "listen address")
	data := flag.String("data", "./panel-data", "data directory")
	adminUser := flag.String("admin-user", "admin", "initial admin username")
	adminPass := flag.String("admin-pass", "admin", "initial admin password (change on first login)")
	flag.Parse()

	store, err := panel.NewStore(*data)
	if err != nil {
		log.Fatalf("store: %v", err)
	}

	// ساخت کاربر ادمین اولیه اگر پایگاه خالی است
	if len(store.ListNodes()) == 0 {
		store.CreateUser(&panel.User{Username: *adminUser, Password: *adminPass, Role: "admin"})
		log.Printf("initial admin created: %s", *adminUser)
	}

	api := panel.NewAPI(store)
	log.Printf("PhoenixTunnel panel listening on %s", *listen)
	log.Fatal(http.ListenAndServe(*listen, api.Handler()))
}
