package panel

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/phoenixtunnel/phoenix/core"
)

// API تمام روت‌های REST پنل
type API struct {
	store *Store
	mux   *http.ServeMux
}

func NewAPI(store *Store) *API {
	a := &API{store: store, mux: http.NewServeMux()}
	a.routes()
	return a
}

func (a *API) Handler() http.Handler { return a.mux }

func (a *API) routes() {
	// احراز هویت
	a.mux.HandleFunc("POST /api/v1/auth/login", a.login)

	// نودها (نیازمند توکن)
	a.mux.Handle("/api/v1/nodes", a.auth(http.HandlerFunc(a.nodesHandler)))
	a.mux.Handle("/api/v1/nodes/", a.auth(http.HandlerFunc(a.nodeByID)))

	// اینباندها
	a.mux.Handle("/api/v1/inbounds", a.auth(http.HandlerFunc(a.inboundsHandler)))
	a.mux.Handle("/api/v1/inbounds/", a.auth(http.HandlerFunc(a.inboundByID)))

	// کاربران پنل
	a.mux.Handle("/api/v1/users", a.auth(http.HandlerFunc(a.usersHandler)))

	// آمار/فرمان نود
	a.mux.HandleFunc("GET /api/v1/nodes/{id}/stats", a.nodeStats)
	a.mux.HandleFunc("POST /api/v1/nodes/{id}/switch", a.nodeSwitch)

	// اینترفیس ایجنت
	a.mux.HandleFunc("GET /api/v1/agent/profile", a.agentProfile)
	a.mux.HandleFunc("POST /api/v1/agent/report", a.agentReport)

	// داشبورد
	a.mux.HandleFunc("/", a.dashboard)
}

// ---------- middleware ----------

func (a *API) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if a.store.UserByToken(token) == nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func agentToken(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// ---------- auth ----------

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "bad request")
		return
	}
	token, err := a.store.Login(req.Username, req.Password)
	if err != nil {
		writeErr(w, 401, "invalid credentials")
		return
	}
	writeJSON(w, map[string]string{"token": token})
}

// ---------- nodes ----------

func (a *API) nodesHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, a.store.ListNodes())
	case http.MethodPost:
		var n Node
		if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
			writeErr(w, 400, "bad request")
			return
		}
		a.store.CreateNode(&n)
		writeJSON(w, n)
	default:
		writeErr(w, 405, "method not allowed")
	}
}

func (a *API) nodeByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/nodes/")
	switch r.Method {
	case http.MethodGet:
		n, err := a.store.GetNode(id)
		if err != nil {
			writeErr(w, 404, "not found")
			return
		}
		writeJSON(w, n)
	case http.MethodPut:
		var patch map[string]any
		json.NewDecoder(r.Body).Decode(&patch)
		n, err := a.store.UpdateNode(id, func(n *Node) {
			if v, ok := patch["name"].(string); ok {
				n.Name = v
			}
			if v, ok := patch["location"].(string); ok {
				n.Location = v
			}
		})
		if err != nil {
			writeErr(w, 404, "not found")
			return
		}
		writeJSON(w, n)
	case http.MethodDelete:
		a.store.DeleteNode(id)
		w.WriteHeader(204)
	}
}

// ---------- inbounds ----------

func (a *API) inboundsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, a.store.ListInbounds(r.URL.Query().Get("node")))
	case http.MethodPost:
		var in Inbound
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, 400, "bad request")
			return
		}
		a.store.CreateInbound(&in)
		writeJSON(w, in)
	}
}

func (a *API) inboundByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/inbounds/")
	if r.Method == http.MethodDelete {
		a.store.DeleteInbound(id)
		w.WriteHeader(204)
		return
	}
	writeErr(w, 405, "method not allowed")
}

// ---------- users ----------

func (a *API) usersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, 405, "method not allowed")
		return
	}
	var u User
	if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
		writeErr(w, 400, "bad request")
		return
	}
	a.store.CreateUser(&u)
	writeJSON(w, u)
}

// ---------- stats / switch ----------

func (a *API) nodeStats(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec := a.store.AgentStats(id)
	if rec == nil {
		writeJSON(w, map[string]any{"online": false})
		return
	}
	writeJSON(w, rec)
}

func (a *API) nodeSwitch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Engine string `json:"engine"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	a.store.AgentReport(id, nil, "switch:"+req.Engine) // فرمان در گزارش بعدی ایجنت ارسال می‌شود
	writeJSON(w, map[string]string{"status": "queued", "engine": req.Engine})
}

// ---------- agent endpoints ----------

func (a *API) agentProfile(w http.ResponseWriter, r *http.Request) {
	node := a.store.NodeByAgentToken(agentToken(r))
	if node == nil {
		writeErr(w, 401, "unauthorized")
		return
	}
	profile := core.Profile{
		NodeID:         node.ID,
		Name:           node.Name,
		Engines:        node.Engines,
		ReverseTunnels: node.Reverse,
	}
	writeJSON(w, profile)
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
	json.NewDecoder(r.Body).Decode(&req)
	cmd := a.store.AgentCommand(node.ID)
	a.store.AgentReport(node.ID, req.Stats, "") // پاک کردن فرمان بعد از ارسال
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
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(dashboardHTML))
}

var _ = log.Println
