// Package core — موتور واحد تونلینگ و ضد-DPI
// همه‌ی پروتکل‌ها از این اینترفیس پیروی می‌کنند.
package core

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// Stats آمار زنده‌ی هر موتور
type Stats struct {
	Name        string    `json:"name"`
	BytesUp     int64     `json:"bytes_up"`
	BytesDown   int64     `json:"bytes_down"`
	ConnsActive int       `json:"conns_active"`
	Failures    int       `json:"failures"`
	LastFailure time.Time `json:"last_failure,omitempty"`
	Since       time.Time `json:"since"`
}

// Engine هر پروتکل (Chameleon/Hydra/Phantom/...) این را پیاده می‌کند
type Engine interface {
	Name() string
	// Listen شروع سرویس روی آدرس داده‌شده (server-side)
	Listen(ctx context.Context, addr string) error
	// Dial اتصال به سرور (client-side) — در نسخه‌ی اندروید استفاده می‌شود
	Dial(ctx context.Context, addr string) (net.Conn, error)
	// ProbeOK آیا چالش پروب فعال DPI را به‌درستی رد می‌کند؟
	ProbeOK(challenge []byte) bool
	Stats() Stats
	Close() error
}

// Registry ثبت و انتخاب موتورها + سوییچ خودکار
type Registry struct {
	mu      sync.RWMutex
	engines map[string]Engine
	current string
}

func NewRegistry() *Registry {
	return &Registry{engines: map[string]Engine{}}
}

func (r *Registry) Register(e Engine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.engines[e.Name()] = e
	if r.current == "" {
		r.current = e.Name()
	}
}

func (r *Registry) Get(name string) (Engine, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.engines[name]
	if !ok {
		return nil, fmt.Errorf("engine %q not registered", name)
	}
	return e, nil
}

func (r *Registry) Current() Engine {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.engines[r.current]
}

func (r *Registry) CurrentName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.current
}

// Switch تغییر موتور فعال (با قفل، بدون قطع سرویس‌دهی قبلی)
func (r *Registry) Switch(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.engines[name]; !ok {
		return fmt.Errorf("engine %q not registered", name)
	}
	r.current = name
	return nil
}

// ListNames نام همه‌ی موتورهای فعال
func (r *Registry) ListNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.engines))
	for n := range r.engines {
		out = append(out, n)
	}
	return out
}

// AllStats آمار همه‌ی موتورها برای پنل
func (r *Registry) AllStats() []Stats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Stats, 0, len(r.engines))
	for _, e := range r.engines {
		out = append(out, e.Stats())
	}
	return out
}
