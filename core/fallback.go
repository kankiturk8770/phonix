package core

import (
	"context"
	"math/rand"
	"net"
	"sync"
	"time"
)

// FallbackEngine — نگهبان سلامت: هر N ثانیه پروب می‌زند،
// نوع خرابی را تشخیص می‌دهد و در صورت نیاز موتور را عوض می‌کند.

type FailureKind int

const (
	FailNone FailureKind = iota
	FailRST                 // تزریق RST — DPI پاسخ جعلی می‌فرستد
	FailTimeout             // timeout — احتمالا بلاک IP/پورت
	FailTLSAlert            // TLS alert — DPI TLS-intercept
	FailUnknown
)

type FallbackEngine struct {
	interval time.Duration
	// classifier: رشته خطا → نوع خرابی
	classify func(error) FailureKind
	// switchTo: تصمیم موتور بعدی بر اساس نوع خرابی
	switchTo func(kind FailureKind) string

	mu     sync.Mutex
	window []bool // true=success — پنجره‌ی لغزان
	thr    float64

	onSwitch func(from, to string)
}

func NewFallbackEngine(conf FallbackConf, classify func(error) FailureKind, switchTo func(FailureKind) string) *FallbackEngine {
	if conf.ProbeIntervalSeconds == 0 {
		conf.ProbeIntervalSeconds = 3
	}
	if conf.FailureThreshold == 0 {
		conf.FailureThreshold = 0.6
	}
	return &FallbackEngine{
		interval: time.Duration(conf.ProbeIntervalSeconds) * time.Second,
		classify: classify,
		switchTo: switchTo,
		thr:      conf.FailureThreshold,
	}
}

func (f *FallbackEngine) OnSwitch(cb func(from, to string)) { f.onSwitch = cb }

// Run حلقه‌ی نگهبانی؛ با cancel متوقف می‌شود
func (f *FallbackEngine) Run(ctx context.Context, reg *Registry) {
	jitter := func() time.Duration {
		return f.interval + time.Duration(rand.Intn(500))*time.Millisecond
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(jitter()):
			kind := f.probe(reg.Current())
			f.record(kind == FailNone)
			if f.failureRate() >= f.thr {
				next := f.switchTo(kind)
				from := reg.CurrentName()
				if next != "" && next != from {
					if err := reg.Switch(next); err == nil && f.onSwitch != nil {
						f.onSwitch(from, next)
					}
				}
			}
		}
	}
}

func (f *FallbackEngine) probe(e Engine) FailureKind {
	if e == nil {
		return FailUnknown
	}
	// پروب واقعی: اتصال کوتاه به خود سرور از داخل تونل
	conn, err := e.Dial(context.Background(), "")
	if err != nil {
		if f.classify != nil {
			return f.classify(err)
		}
		if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
			return FailTimeout
		}
		return FailUnknown
	}
	conn.Close()
	return FailNone
}

func (f *FallbackEngine) record(ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.window = append(f.window, ok)
	if len(f.window) > 20 { // پنجره‌ی ۲۰تایی
		f.window = f.window[1:]
	}
}

func (f *FallbackEngine) failureRate() float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.window) == 0 {
		return 0
	}
	bad := 0
	for _, ok := range f.window {
		if !ok {
			bad++
		}
	}
	return float64(bad) / float64(len(f.window))
}

// ClassifyError تشخیص نوع خرابی از خطای شبکه
func ClassifyError(err error) FailureKind {
	if err == nil {
		return FailNone
	}
	if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
		return FailTimeout
	}
	if op, ok := err.(*net.OpError); ok {
		if _, ok := op.Err.(*net.OpError); ok { // wrapped ECONNRESET
			return FailRST
		}
	}
	return FailUnknown
}
