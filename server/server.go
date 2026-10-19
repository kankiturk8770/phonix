// Package server — دیمون لینوکس هسته‌ی سرور PhoenixTunnel
// راه‌اندازی listenerها، reverse tunnelها و گزارش به پنل.
package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/phoenixtunnel/phoenix/core"
)

type Server struct {
	profile *core.Profile
	reg     *core.Registry
	servers []engineServer
	wg      sync.WaitGroup
}

// engineServer هر موتور + listener متناظرش
type engineServer struct {
	engine core.Engine
	ln     net.Listener
}

// New ساخت سرور از روی پروفایل
func New(profile *core.Profile) *Server {
	reg := core.NewRegistry()
	return &Server{profile: profile, reg: reg}
}

// Run اجرای همه‌ی موتورهای فعال تا cancel
func (s *Server) Run(ctx context.Context) error {
	if err := s.startEngines(ctx); err != nil {
		return err
	}
	if err := s.startReverseTunnels(ctx); err != nil {
		log.Printf("reverse tunnels: %v", err)
	}

	// fallback نگهبان
	fb := core.NewFallbackEngine(s.profile.Fallback, core.ClassifyError, s.pickFallback)
	fb.OnSwitch(func(from, to string) {
		log.Printf("[FALLBACK] %s -> %s", from, to)
	})
	go fb.Run(ctx, s.reg)

	// گزارش‌دهی دوره‌ای به پنل (اگر agent فعال باشد)
	if s.profile.Meta["panel_url"] != "" {
		go s.reportLoop(ctx)
	}

	<-ctx.Done()
	return nil
}

func (s *Server) startEngines(ctx context.Context) error {
	for _, ec := range s.profile.Engines {
		if !ec.Enabled {
			continue
		}
		eng := s.buildEngine(ec)
		if eng == nil {
			log.Printf("unknown engine %q, skipped", ec.Name)
			continue
		}
		s.reg.Register(eng)
		s.wg.Add(1)
		go func(e core.Engine, addr string) {
			defer s.wg.Done()
			log.Printf("[ENGINE] %s listening on %s", e.Name(), addr)
			if err := e.Listen(ctx, addr); err != nil {
				log.Printf("[ENGINE] %s stopped: %v", e.Name(), err)
			}
		}(eng, ec.Listen)
	}
	return nil
}

// buildEngine کارخانه‌ی موتورها
func (s *Server) buildEngine(ec core.EngineConf) core.Engine {
	switch ec.Name {
	case "chameleon":
		return &ChameleonEngine{conf: s.profile.Chameleon, keys: s.keys()}
	case "phantom":
		return &PhantomEngine{conf: s.profile.Phantom, keys: s.keys()}
	case "phoenix-udp":
		return &PhoenixUDPEngine{conf: s.profile.Hydra, keys: s.keys()}
	case "reality":
		return &RealityLiteEngine{keys: s.keys(), CamouflageAddr: s.profile.Meta["camouflage"]}
	default:
		return nil
	}
}

func (s *Server) keys() []byte {
	// در نسخه‌ی واقعی از KeyMaterial.StaticPriv اعتبارسنجی‌شده
	return []byte(s.profile.Keys.StaticPriv)
}

// pickFallback منطق انتخاب موتور جایگزین
func (s *Server) pickFallback(kind core.FailureKind) string {
	switch kind {
	case core.FailRST:
		return "chameleon" // با تکه‌تکه کردن SNI دوباره تلاش کن
	case core.FailTimeout:
		return "phantom" // IP/پورت بسته — به DNS پناه ببر
	case core.FailTLSAlert:
		return "phoenix-udp" // TLS intercept — به کانال UDP اختصاصی
	default:
		return ""
	}
}

// startReverseTunnels تونل‌های معکوس: سرور داخل ایران به خارج وصل می‌شود
func (s *Server) startReverseTunnels(ctx context.Context) error {
	for _, rt := range s.profile.ReverseTunnels {
		rt := rt
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			loop := func() error {
				conn, err := dialReverse(rt)
				if err != nil {
					return err
				}
				defer conn.Close()
				return runReverseSession(ctx, conn, rt)
			}
			for {
				if err := loop(); err != nil {
					log.Printf("[REVERSE:%s] %v — retry in 5s", rt.Name, err)
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
				}
			}
		}()
	}
	return nil
}

// dialReverse بر اساس نوع تونل معکوس
func dialReverse(rt core.ReverseConf) (net.Conn, error) {
	switch rt.Type {
	case "websocket", "grpc", "cdn-pull":
		// این نوع‌ها از پورت‌های باز CDN/وب عبور می‌کنند
		return net.DialTimeout("tcp", rt.Endpoint, 10*time.Second)
	case "ssh":
		return net.DialTimeout("tcp", rt.Endpoint, 10*time.Second)
	default:
		return net.DialTimeout("tcp", rt.Endpoint, 10*time.Second)
	}
}

// runReverseSession بعد از اتصال، کلید تبادل و رله‌ی داده
func runReverseSession(ctx context.Context, conn net.Conn, rt core.ReverseConf) error {
	conn.SetDeadline(time.Now().Add(30 * time.Second))
	if _, err := fmt.Fprintf(conn, "PHX/1 %s\n", rt.Secret); err != nil {
		return err
	}
	conn.SetDeadline(time.Time{})
	// در نسخه‌ی کامل: multiplexing روی این کانکشن به سمت outbound سرور خارج
	<-ctx.Done()
	return nil
}

// reportLoop ارسال آمار زنده به پنل
func (s *Server) reportLoop(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, st := range s.reg.AllStats() {
				_ = st // در نسخه‌ی کامل: HTTP POST به پنل
			}
		}
	}
}

// Stats آمار زنده‌ی همه‌ی موتورها (برای پنل/ایجنت)
func (s *Server) Stats() []core.Stats { return s.reg.AllStats() }
