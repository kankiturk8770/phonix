// Package panel — پنل فوق پیشرفته‌ی کنترل PhoenixTunnel
// API کامل + داشبورد وب تعبیه‌شده (single-binary).
package panel

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
	Password string    `json:"password"` // در نسخه‌ی عملیاتی: bcrypt hash
	Role     string    `json:"role"`     // admin | operator | viewer
	Created  time.Time `json:"created"`
}

type Node struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Location  string            `json:"location"` // IR | DE | ...
	IP        string            `json:"ip"`
	Engines   []core.EngineConf `json:"engines"`
	Reverse   []core.ReverseConf `json:"reverse"`
	AgentToken string           `json:"agent_token"`
	LastSeen  time.Time         `json:"last_seen"`
	Online    bool              `json:"online"`
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
	NodeID   string      `json:"node_id"`
	LastSeen time.Time   `json:"last_seen"`
	Stats    []core.Stats `json:"stats"`
	Command  string      `json:"command,omitempty"`
}

// Store ذخیره‌سازی JSON روی دیسک — بدون وابستگی خارجی
type Store struct {
	dir string
	mu  sync.RWMutex

	users    map[string]*User
	nodes    map[string]*Node
	inbounds map[string]*Inbound
	agents   map[string]*AgentRecord
	sessions map[string]string // token -> userID
}

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
		sessions: map[string]string{},
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

func (s *Store) save(name string, v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.MarshalIndent(v, "", "  ")
	tmp := filepath.Join(s.dir, name+".tmp")
	os.WriteFile(tmp, b, 0o600)
	os.Rename(tmp, filepath.Join(s.dir, name))
}

// ---------- کاربران و احراز هویت ----------

func (s *Store) CreateUser(u *User) error {
	u.ID = newID()
	u.Created = time.Now()
	if u.Role == "" {
		u.Role = "operator"
	}
	s.users[u.ID] = u
	s.save("users.json", s.users)
	return nil
}

func (s *Store) Login(username, password string) (string, error) {
	for _, u := range s.users {
		if u.Username == username && u.Password == password { // عملیاتی: bcrypt.Compare
			token := newID() + newID()
			s.sessions[token] = u.ID
			return token, nil
		}
	}
	return "", ErrUnauthorized
}

func (s *Store) UserByToken(token string) *User {
	if id, ok := s.sessions[token]; ok {
		return s.users[id]
	}
	return nil
}

// ---------- نودها ----------

func (s *Store) CreateNode(n *Node) error {
	n.ID = newID()
	if n.AgentToken == "" {
		n.AgentToken = newID() + newID()
	}
	s.nodes[n.ID] = n
	s.save("nodes.json", s.nodes)
	return nil
}

func (s *Store) UpdateNode(id string, fn func(*Node)) (*Node, error) {
	n, ok := s.nodes[id]
	if !ok {
		return nil, ErrNotFound
	}
	fn(n)
	s.save("nodes.json", s.nodes)
	return n, nil
}

func (s *Store) GetNode(id string) (*Node, error) {
	n, ok := s.nodes[id]
	if !ok {
		return nil, ErrNotFound
	}
	return n, nil
}

func (s *Store) ListNodes() []*Node {
	out := make([]*Node, 0, len(s.nodes))
	for _, n := range s.nodes {
		out = append(out, n)
	}
	return out
}

func (s *Store) DeleteNode(id string) error {
	delete(s.nodes, id)
	s.save("nodes.json", s.nodes)
	return nil
}

// ---------- اینباندها ----------

func (s *Store) CreateInbound(in *Inbound) error {
	in.ID = newID()
	in.CreatedAt = time.Now()
	s.inbounds[in.ID] = in
	s.save("inbounds.json", s.inbounds)
	return nil
}

func (s *Store) ListInbounds(nodeID string) []*Inbound {
	out := []*Inbound{}
	for _, in := range s.inbounds {
		if nodeID == "" || in.NodeID == nodeID {
			out = append(out, in)
		}
	}
	return out
}

func (s *Store) DeleteInbound(id string) error {
	delete(s.inbounds, id)
	s.save("inbounds.json", s.inbounds)
	return nil
}

// ---------- ایجنت‌ها ----------

func (s *Store) NodeByAgentToken(token string) *Node {
	for _, n := range s.nodes {
		if n.AgentToken == token {
			return n
		}
	}
	return nil
}

func (s *Store) AgentReport(nodeID string, stats []core.Stats, command string) {
	rec, ok := s.agents[nodeID]
	if !ok {
		rec = &AgentRecord{NodeID: nodeID}
		s.agents[nodeID] = rec
	}
	rec.LastSeen = time.Now()
	rec.Stats = stats
	rec.Command = command
	s.save("agents.json", s.agents)

	// به‌روزرسانی وضعیت آنلاین نود
	if n, ok := s.nodes[nodeID]; ok {
		n.LastSeen = rec.LastSeen
		n.Online = time.Since(rec.LastSeen) < 30*time.Second
		s.save("nodes.json", s.nodes)
	}
}

func (s *Store) AgentCommand(nodeID string) string {
	if rec, ok := s.agents[nodeID]; ok {
		return rec.Command
	}
	return ""
}

func (s *Store) AgentStats(nodeID string) *AgentRecord {
	return s.agents[nodeID]
}

// ---------- کمکی ----------

func newID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}
