package panel

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/phoenixtunnel/phoenix/core"
)

// Options تنظیمات اتصال پنل به هسته‌ی همین سرور
type Options struct {
	LocalProfile  string // مسیر profile.json هسته‌ی محلی (برای نمایش لینک/QR همین سرور)
	LocalStatsURL string // آدرس /stats هسته‌ی محلی (phoenix-server -stats)
	Version       string
	TLSPin        string // اثرانگشت گواهی HTTPS پنل (برای ایجنت‌ها)
}

// API تمام روت‌های REST پنل
type API struct {
	store *Store
	opts  Options
	mux   *http.ServeMux

	failMu sync.Mutex
	fails  map[string]int // تلاش ناموفق ورود به ازای IP
}

func NewAPI(store *Store, opts ...Options) *API {
	a := &API{store: store, mux: http.NewServeMux(), fails: map[string]int{}}
	if len(opts) > 0 {
		a.opts = opts[0]
	}
	a.routes()
	return a
}

func (a *API) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		a.mux.ServeHTTP(w, r)
	})
}

func (a *API) routes() {
	// احراز هویت
	a.mux.HandleFunc("POST /api/v1/auth/login", a.login)
	a.mux.Handle("POST /api/v1/auth/logout", a.auth(a.logout))
	a.mux.Handle("GET /api/v1/auth/me", a.auth(a.me))
	a.mux.Handle("POST /api/v1/auth/password", a.auth(a.changePassword))

	// همین سرور (هسته‌ی محلی)
	a.mux.Handle("GET /api/v1/local", a.auth(a.local))
	a.mux.Handle("GET /api/v1/local/qr.png", a.auth(a.localQR))

	// نودها
	a.mux.Handle("GET /api/v1/nodes", a.auth(a.listNodes))
	a.mux.Handle("POST /api/v1/nodes", a.auth(a.createNode))
	a.mux.Handle("GET /api/v1/nodes/{id}", a.auth(a.getNode))
	a.mux.Handle("PUT /api/v1/nodes/{id}", a.auth(a.updateNode))
	a.mux.Handle("DELETE /api/v1/nodes/{id}", a.auth(a.deleteNode))
	a.mux.Handle("POST /api/v1/nodes/{id}/rotate-key", a.auth(a.rotateKey))
	a.mux.Handle("GET /api/v1/nodes/{id}/link", a.auth(a.nodeLink))
	a.mux.Handle("GET /api/v1/nodes/{id}/qr.png", a.auth(a.nodeQR))
	a.mux.Handle("GET /api/v1/nodes/{id}/stats", a.auth(a.nodeStats))
	a.mux.Handle("POST /api/v1/nodes/{id}/switch", a.auth(a.nodeSwitch))

	// اینباندها
	a.mux.Handle("GET /api/v1/inbounds", a.auth(a.listInbounds))
	a.mux.Handle("POST /api/v1/inbounds", a.auth(a.createInbound))
	a.mux.Handle("DELETE /api/v1/inbounds/{id}", a.auth(a.deleteInbound))

	// کاربران پنل
	a.mux.Handle("POST /api/v1/users", a.auth(a.createUser))

	// اینترفیس ایجنت
	a.mux.HandleFunc("GET /api/v1/agent/profile", a.agentProfile)
	a.mux.HandleFunc("POST /api/v1/agent/report", a.agentReport)

	// داشبورد
	a.mux.HandleFunc("GET /{$}", a.dashboard)
}

// ---------- middleware ----------

func (a *API) auth(next func(http.ResponseWriter, *http.Request, *User)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			token = r.URL.Query().Get("token") // فقط برای <img> کد QR
		}
		u := a.store.UserByToken(token)
		if u == nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r, u)
	})
}

func agentToken(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

// ---------- auth ----------

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	a.failMu.Lock()
	n := a.fails[ip]
	a.failMu.Unlock()
	if n >= 5 { // کندکردن brute-force
		time.Sleep(time.Duration(n-4) * time.Second)
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		writeErr(w, 400, "bad request")
		return
	}
	token, err := a.store.Login(req.Username, req.Password)
	if err != nil {
		a.failMu.Lock()
		if a.fails[ip] < 30 {
			a.fails[ip]++
		}
		a.failMu.Unlock()
		writeErr(w, 401, "invalid credentials")
		return
	}
	a.failMu.Lock()
	delete(a.fails, ip)
	a.failMu.Unlock()
	writeJSON(w, map[string]string{"token": token})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request, u *User) {
	a.store.Logout(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	w.WriteHeader(204)
}

func (a *API) me(w http.ResponseWriter, r *http.Request, u *User) {
	writeJSON(w, map[string]any{"username": u.Username, "role": u.Role, "version": a.opts.Version, "tls_pin": a.opts.TLSPin})
}

func (a *API) changePassword(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "bad request")
		return
	}
	if err := a.store.ChangePassword(u.ID, req.Old, req.New); err != nil {
		if errors.Is(err, ErrUnauthorized) {
			writeErr(w, 403, "رمز فعلی اشتباه است")
			return
		}
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

// ---------- local core ----------

func (a *API) localProfile() (*core.Profile, error) {
	if a.opts.LocalProfile == "" {
		return nil, errors.New("پروفایل محلی تنظیم نشده (phoenix-panel -local-profile)")
	}
	return core.LoadProfile(a.opts.LocalProfile)
}

func (a *API) local(w http.ResponseWriter, r *http.Request, u *User) {
	p, err := a.localProfile()
	if err != nil {
		writeJSON(w, map[string]any{"enabled": false, "error": err.Error()})
		return
	}
	out := map[string]any{"enabled": true, "name": p.Name, "node_id": p.NodeID, "engines": p.Engines,
		"host": p.Meta["public_host"], "fake_snis": p.Chameleon.FakeSNIs}
	if h := p.Meta["public_host"]; h != "" {
		if c, err := core.ClientConfigFromProfile(p, h); err == nil {
			out["link"] = core.EncodeLink(c)
			out["config"] = c
		} else {
			out["link_error"] = err.Error()
		}
	} else {
		out["link_error"] = "public_host در پروفایل خالی است — phoenix-server -init -host <IP>"
	}
	if a.opts.LocalStatsURL != "" {
		cl := http.Client{Timeout: 2 * time.Second}
		if resp, err := cl.Get(a.opts.LocalStatsURL); err == nil {
			var st map[string]any
			if json.NewDecoder(resp.Body).Decode(&st) == nil {
				out["stats"] = st
				out["online"] = true
			}
			resp.Body.Close()
		} else {
			out["online"] = false
		}
	}
	writeJSON(w, out)
}

func (a *API) localQR(w http.ResponseWriter, r *http.Request, u *User) {
	p, err := a.localProfile()
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	c, err := core.ClientConfigFromProfile(p, p.Meta["public_host"])
	if err != nil || p.Meta["public_host"] == "" {
		writeErr(w, 404, "no link")
		return
	}
	writeQR(w, core.EncodeLink(c))
}

// ---------- nodes ----------

// nodeView نود بدون توکن ایجنت در لیست (توکن فقط در جزئیات)
func (a *API) listNodes(w http.ResponseWriter, r *http.Request, u *User) {
	nodes := a.store.ListNodes()
	for _, n := range nodes {
		n.AgentToken = ""
		n.Keys = ""
	}
	writeJSON(w, nodes)
}

func (a *API) createNode(w http.ResponseWriter, r *http.Request, u *User) {
	var n Node
	if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
		writeErr(w, 400, "bad request")
		return
	}
	if strings.TrimSpace(n.Name) == "" {
		writeErr(w, 400, "نام نود لازم است")
		return
	}
	n.AgentToken, n.Keys = "", ""
	a.store.CreateNode(&n)
	writeJSON(w, n)
}

func (a *API) getNode(w http.ResponseWriter, r *http.Request, u *User) {
	n, err := a.store.GetNode(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "not found")
		return
	}
	writeJSON(w, n)
}

func (a *API) updateNode(w http.ResponseWriter, r *http.Request, u *User) {
	var patch struct {
		Name     *string            `json:"name"`
		Location *string            `json:"location"`
		IP       *string            `json:"ip"`
		FakeSNIs []string           `json:"fake_snis"`
		Engines  []core.EngineConf  `json:"engines"`
		Reverse  []core.ReverseConf `json:"reverse"`
	}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErr(w, 400, "bad request")
		return
	}
	n, err := a.store.UpdateNode(r.PathValue("id"), func(n *Node) {
		if patch.Name != nil {
			n.Name = *patch.Name
		}
		if patch.Location != nil {
			n.Location = *patch.Location
		}
		if patch.IP != nil {
			n.IP = strings.TrimSpace(*patch.IP)
		}
		if patch.FakeSNIs != nil {
			n.FakeSNIs = patch.FakeSNIs
		}
		if patch.Engines != nil {
			n.Engines = patch.Engines
		}
		if patch.Reverse != nil {
			n.Reverse = patch.Reverse
		}
	})
	if err != nil {
		writeErr(w, 404, "not found")
		return
	}
	writeJSON(w, n)
}

func (a *API) deleteNode(w http.ResponseWriter, r *http.Request, u *User) {
	a.store.DeleteNode(r.PathValue("id"))
	w.WriteHeader(204)
}

func (a *API) rotateKey(w http.ResponseWriter, r *http.Request, u *User) {
	n, err := a.store.UpdateNode(r.PathValue("id"), func(n *Node) { n.Keys = core.RandomKey() })
	if err != nil {
		writeErr(w, 404, "not found")
		return
	}
	writeJSON(w, n)
}

func (a *API) nodeLink(w http.ResponseWriter, r *http.Request, u *User) {
	n, err := a.store.GetNode(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "not found")
		return
	}
	c, err := ClientConfigForNode(n)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, map[string]any{"link": core.EncodeLink(c), "config": c})
}

func (a *API) nodeQR(w http.ResponseWriter, r *http.Request, u *User) {
	n, err := a.store.GetNode(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "not found")
		return
	}
	c, err := ClientConfigForNode(n)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeQR(w, core.EncodeLink(c))
}

func writeQR(w http.ResponseWriter, text string) {
	png, err := qrcode.Encode(text, qrcode.Medium, 320)
	if err != nil {
		writeErr(w, 500, "qr")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(png)
}

// ---------- inbounds ----------

func (a *API) listInbounds(w http.ResponseWriter, r *http.Request, u *User) {
	writeJSON(w, a.store.ListInbounds(r.URL.Query().Get("node")))
}

func (a *API) createInbound(w http.ResponseWriter, r *http.Request, u *User) {
	var in Inbound
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "bad request")
		return
	}
	a.store.CreateInbound(&in)
	writeJSON(w, in)
}

func (a *API) deleteInbound(w http.ResponseWriter, r *http.Request, u *User) {
	a.store.DeleteInbound(r.PathValue("id"))
	w.WriteHeader(204)
}

// ---------- users ----------

func (a *API) createUser(w http.ResponseWriter, r *http.Request, u *User) {
	if u.Role != "admin" {
		writeErr(w, 403, "admin only")
		return
	}
	var nu User
	if err := json.NewDecoder(r.Body).Decode(&nu); err != nil {
		writeErr(w, 400, "bad request")
		return
	}
	if err := a.store.CreateUser(&nu); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	nu.Password = ""
	writeJSON(w, nu)
}

// ---------- stats / switch ----------

func (a *API) nodeStats(w http.ResponseWriter, r *http.Request, u *User) {
	rec := a.store.AgentStats(r.PathValue("id"))
	if rec == nil {
		writeJSON(w, map[string]any{"online": false})
		return
	}
	writeJSON(w, rec)
}

func (a *API) nodeSwitch(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		Engine string `json:"engine"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	a.store.QueueCommand(r.PathValue("id"), "switch:"+req.Engine) // فرمان در گزارش بعدی ایجنت ارسال می‌شود
	writeJSON(w, map[string]string{"status": "queued", "engine": req.Engine})
}

// ---------- agent endpoints ----------

func (a *API) agentProfile(w http.ResponseWriter, r *http.Request) {
	node := a.store.NodeByAgentToken(agentToken(r))
	if node == nil {
		writeErr(w, 401, "unauthorized")
		return
	}
	writeJSON(w, ProfileForNode(node))
}

func (a *API) agentReport(w http.ResponseWriter, r *http.Request) {
	node := a.store.NodeByAgentToken(agentToken(r))
	if node == nil {
		writeErr(w, 401, "unauthorized")
		return
	}
	var req struct {
		Stats []core.Stats `json:"stats"`
	}
	json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
	cmd := a.store.AgentReport(node.ID, req.Stats) // فرمان بعد از ارسال پاک می‌شود
	writeJSON(w, map[string]string{"switch_engine": strings.TrimPrefix(cmd, "switch:")})
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// dashboard داشبورد تعبیه‌شده
func (a *API) dashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(dashboardHTML))
}
