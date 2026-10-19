// Package panel — پنل فوق پیشرفته‌ی کنترل PhoenixTunnel
// API کامل + داشبورد وب تعبیه‌شده (single-binary).
package panel

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/phoenixtunnel/phoenix/core"
)

var (
	ErrUnauthorized = errors.New("panel: unauthorized")
	ErrNotFound     = errors.New("panel: not found")
)

// ---------- مدل‌های داده ----------

type User struct {
	ID       string    `json:"id"`
	Username string    `json:"username"`
	Password string    `json:"password,omitempty"` // "sha256$salt$hash" — هرگز خام ذخیره نمی‌شود
	Role     string    `json:"role"`               // admin | operator | viewer
	Created  time.Time `json:"created"`
}

type Node struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Location   string             `json:"location"` // IR | DE | ...
	IP         string             `json:"ip"`       // آدرس عمومی (IP یا دامنه) برای لینک کلاینت
	Engines    []core.EngineConf  `json:"engines"`
	Reverse    []core.ReverseConf `json:"reverse"`
	Keys       string             `json:"keys,omitempty"` // کلید مشترک کلاینت/سرور
	FakeSNIs   []string           `json:"fake_snis,omitempty"`
	AgentToken string             `json:"agent_token"`
	LastSeen   time.Time          `json:"last_seen"`
	Online     bool               `json:"online"`
	Local      bool               `json:"local,omitempty"` // همین سرور (از پروفایل محلی)
}

type Inbound struct {
	ID        string    `json:"id"`
	NodeID    string    `json:"node_id"`
	Protocol  string    `json:"protocol"` // chameleon | hydra | phantom | hysteria2
	Port      int       `json:"port"`
	Settings  string    `json:"settings"` // JSON آزاد
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

type AgentRecord struct {
	NodeID   string       `json:"node_id"`
	LastSeen time.Time    `json:"last_seen"`
	Stats    []core.Stats `json:"stats"`
	Command  string       `json:"command,omitempty"`
}

// Store ذخیره‌سازی JSON روی دیسک — بدون وابستگی خارجی، امن برای هم‌زمانی
type Store struct {
	dir string
	mu  sync.RWMutex

	users    map[string]*User
	nodes    map[string]*Node
	inbounds map[string]*Inbound
	agents   map[string]*AgentRecord
	sessions map[string]session // token -> session
}

type session struct {
	userID  string
	expires time.Time
}

const sessionTTL = 7 * 24 * time.Hour

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{
		dir:      dir,
		users:    map[string]*User{},
		nodes:    map[string]*Node{},
		inbounds: map[string]*Inbound{},
		agents:   map[string]*AgentRecord{},
		sessions: map[string]session{},
	}
	s.load()
	return s, nil
}

func (s *Store) load() {
	for name, dst := range map[string]any{
		"users.json": &s.users, "nodes.json": &s.nodes,
		"inbounds.json": &s.inbounds, "agents.json": &s.agents,
	} {
		if b, err := os.ReadFile(filepath.Join(s.dir, name)); err == nil {
			json.Unmarshal(b, dst)
		}
	}
}

// saveLocked نوشتن اتمیک؛ قفل باید گرفته شده باشد
func (s *Store) saveLocked(name string, v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	tmp := filepath.Join(s.dir, name+".tmp")
	os.WriteFile(tmp, b, 0o600)
	os.Rename(tmp, filepath.Join(s.dir, name))
}

// ---------- کاربران و احراز هویت ----------

func hashPassword(pw string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	return "sha256$" + hex.EncodeToString(salt) + "$" + digest(salt, pw)
}

func digest(salt []byte, pw string) string {
	h := sha256.Sum256(append(append([]byte(nil), salt...), pw...))
	for i := 0; i < 50000; i++ { // کش‌کردن brute-force
		h = sha256.Sum256(h[:])
	}
	return hex.EncodeToString(h[:])
}

func checkPassword(stored, pw string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 3 || parts[0] != "sha256" {
		// سازگاری با فایل‌های قدیمی (رمز خام)
		return stored != "" && subtle.ConstantTimeCompare([]byte(stored), []byte(pw)) == 1
	}
	salt, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(digest(salt, pw)), []byte(parts[2])) == 1
}

func (s *Store) HasUsers() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users) > 0
}

func (s *Store) CreateUser(u *User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u.Username == "" || u.Password == "" {
		return errors.New("username/password required")
	}
	for _, x := range s.users {
		if x.Username == u.Username {
			return errors.New("username exists")
		}
	}
	u.ID = newID()
	u.Created = time.Now()
	if u.Role == "" {
		u.Role = "operator"
	}
	u.Password = hashPassword(u.Password)
	s.users[u.ID] = u
	s.saveLocked("users.json", s.users)
	return nil
}

// SetPassword تغییر رمز (یا ساخت ادمین اگر وجود ندارد)
func (s *Store) SetPassword(username, pw string) error {
	s.mu.Lock()
	for _, u := range s.users {
		if u.Username == username {
			u.Password = hashPassword(pw)
			s.saveLocked("users.json", s.users)
			s.mu.Unlock()
			return nil
		}
	}
	s.mu.Unlock()
	return s.CreateUser(&User{Username: username, Password: pw, Role: "admin"})
}

func (s *Store) Login(username, password string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.Username == username && checkPassword(u.Password, password) {
			if !strings.HasPrefix(u.Password, "sha256$") { // ارتقای رمز خام قدیمی
				u.Password = hashPassword(password)
				s.saveLocked("users.json", s.users)
			}
			token := newID() + newID()
			s.sessions[token] = session{userID: u.ID, expires: time.Now().Add(sessionTTL)}
			return token, nil
		}
	}
	return "", ErrUnauthorized
}

func (s *Store) Logout(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

func (s *Store) UserByToken(token string) *User {
	if token == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ss, ok := s.sessions[token]
	if !ok {
		return nil
	}
	if time.Now().After(ss.expires) {
		delete(s.sessions, token)
		return nil
	}
	return s.users[ss.userID]
}

func (s *Store) ChangePassword(userID, oldPw, newPw string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[userID]
	if !ok || !checkPassword(u.Password, oldPw) {
		return ErrUnauthorized
	}
	if len(newPw) < 6 {
		return errors.New("رمز جدید باید حداقل ۶ کاراکتر باشد")
	}
	u.Password = hashPassword(newPw)
	s.saveLocked("users.json", s.users)
	return nil
}

// ---------- نودها ----------

func (s *Store) CreateNode(n *Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n.ID = newID()
	if n.AgentToken == "" {
		n.AgentToken = newID() + newID()
	}
	if n.Keys == "" {
		n.Keys = core.RandomKey()
	}
	if len(n.Engines) == 0 {
		n.Engines = []core.EngineConf{
			{Name: "chameleon", Enabled: true, Listen: "0.0.0.0:443"},
			{Name: "reality", Enabled: true, Listen: "0.0.0.0:8443"},
			{Name: "phoenix-udp", Enabled: true, Listen: "0.0.0.0:51820"},
		}
	}
	if len(n.FakeSNIs) == 0 {
		n.FakeSNIs = []string{"www.google.com", "www.microsoft.com"}
	}
	n.Local = false
	s.nodes[n.ID] = n
	s.saveLocked("nodes.json", s.nodes)
	return nil
}

func (s *Store) UpdateNode(id string, fn func(*Node)) (*Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.nodes[id]
	if !ok {
		return nil, ErrNotFound
	}
	fn(n)
	s.saveLocked("nodes.json", s.nodes)
	cp := *n
	return &cp, nil
}

func (s *Store) GetNode(id string) (*Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.nodes[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *n
	cp.Online = time.Since(cp.LastSeen) < 30*time.Second
	return &cp, nil
}

func (s *Store) ListNodes() []*Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Node, 0, len(s.nodes))
	for _, n := range s.nodes {
		cp := *n
		cp.Online = time.Since(cp.LastSeen) < 30*time.Second
		out = append(out, &cp)
	}
	return out
}

func (s *Store) DeleteNode(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.nodes, id)
	delete(s.agents, id)
	s.saveLocked("nodes.json", s.nodes)
	s.saveLocked("agents.json", s.agents)
	return nil
}

// ---------- اینباندها ----------

func (s *Store) CreateInbound(in *Inbound) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	in.ID = newID()
	in.CreatedAt = time.Now()
	s.inbounds[in.ID] = in
	s.saveLocked("inbounds.json", s.inbounds)
	return nil
}

func (s *Store) ListInbounds(nodeID string) []*Inbound {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []*Inbound{}
	for _, in := range s.inbounds {
		if nodeID == "" || in.NodeID == nodeID {
			out = append(out, in)
		}
	}
	return out
}

func (s *Store) DeleteInbound(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inbounds, id)
	s.saveLocked("inbounds.json", s.inbounds)
	return nil
}

// ---------- ایجنت‌ها ----------

func (s *Store) NodeByAgentToken(token string) *Node {
	if token == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, n := range s.nodes {
		if subtle.ConstantTimeCompare([]byte(n.AgentToken), []byte(token)) == 1 {
			cp := *n
			return &cp
		}
	}
	return nil
}

// AgentReport ثبت آمار ایجنت و برگرداندن (و پاک کردن) فرمان در انتظار
func (s *Store) AgentReport(nodeID string, stats []core.Stats) (command string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.agents[nodeID]
	if !ok {
		rec = &AgentRecord{NodeID: nodeID}
		s.agents[nodeID] = rec
	}
	rec.LastSeen = time.Now()
	rec.Stats = stats
	command, rec.Command = rec.Command, ""
	s.saveLocked("agents.json", s.agents)

	if n, ok := s.nodes[nodeID]; ok {
		n.LastSeen = rec.LastSeen
		n.Online = true
		s.saveLocked("nodes.json", s.nodes)
	}
	return command
}

// QueueCommand فرمان برای گزارش بعدی ایجنت
func (s *Store) QueueCommand(nodeID, command string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.agents[nodeID]
	if !ok {
		rec = &AgentRecord{NodeID: nodeID}
		s.agents[nodeID] = rec
	}
	rec.Command = command
	s.saveLocked("agents.json", s.agents)
}

func (s *Store) AgentStats(nodeID string) *AgentRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if rec, ok := s.agents[nodeID]; ok {
		cp := *rec
		return &cp
	}
	return nil
}

// ---------- پروفایل/کانفیگ ----------

// ProfileForNode پروفایل کامل اجرای هسته برای ایجنت این نود
func ProfileForNode(n *Node) *core.Profile {
	p := core.DefaultProfile(n.Name, n.IP, 443, 51820)
	p.NodeID = n.ID
	p.Engines = n.Engines
	p.ReverseTunnels = n.Reverse
	p.Keys.StaticPriv = n.Keys
	if len(n.FakeSNIs) > 0 {
		p.Chameleon.FakeSNIs = n.FakeSNIs
	}
	return p
}

// ClientConfigForNode کانفیگ/لینک اپ برای این نود
func ClientConfigForNode(n *Node) (core.ClientConfig, error) {
	if n.IP == "" {
		return core.ClientConfig{}, errors.New("آدرس عمومی (IP) نود خالی است")
	}
	return core.ClientConfigFromProfile(ProfileForNode(n), n.IP)
}

// ---------- کمکی ----------

func newID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}
