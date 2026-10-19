package server

import (
	"bufio"
	"context"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/phoenixtunnel/phoenix/core"
)

// ---------------- RealityLite ----------------
// استتار دوطرفه بدون وابستگی به uTLS:
// کلاینت معتبر بعد از TLS handshake، مارجیک "PHX1" + handshake داخلی می‌فرستد.
// هر چیز دیگر (پروبر فعال DPI، مرورگر کنجکاو) بدون هیچ پاسخ متفاوتی
// به سایت استتار (camouflage) پروکسی می‌شود — پروبر دقیقاً سایت واقعی را می‌بیند.

type RealityLiteEngine struct {
	ListenAddr     string
	CamouflageAddr string // مثال: "www.microsoft.com:443" یا یک وب‌سرور خودی
	keys           []byte

	mu    sync.Mutex
	stats core.Stats
	ln    net.Listener
}

func (r *RealityLiteEngine) Name() string { return "reality" }

func (r *RealityLiteEngine) Listen(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	r.ln = ln
	r.stats.Since = time.Now()
	r.stats.Name = r.Name()
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go r.handle(conn)
	}
}

func (r *RealityLiteEngine) handle(client net.Conn) {
	defer client.Close()
	client = unwrapRaw(client)
	client.SetReadDeadline(time.Now().Add(10 * time.Second))
	bc := &bufConn{br: bufio.NewReaderSize(client, 16384), c: client}

	// TLS handshake واقعی روی این کانکشن با گواهی معتبر (Let's Encrypt)
	// — در این پیاده‌سازی سبک، TLS توسط یک reverse-proxy جلویی (nginx/haproxy)
	// پایان می‌یابد و این موتور روی پورت داخلی TLS-passthrough کار می‌کند.
	// پیک 4 بایتی: مارجیک کلاینت phoenix
	peek := make([]byte, 4)
	if _, err := io.ReadFull(client, peek); err != nil {
		return
	}
	if string(peek) != "PHX1" {
		r.camouflage(client, peek)
		return
	}
	client.SetReadDeadline(time.Now().Add(30 * time.Second))
	sess, err := core.ServerHandshake(bc, r.keys)
	if err != nil {
		r.fail()
		return
	}
	frame, err := tolerantReadFrame(bc.br, 256)
	if err != nil {
		r.fail()
		return
	}
	plain, err := sess.OpenFrame(frame)
	if err != nil {
		r.fail()
		return
	}
	target, err := core.DecodeTarget(plain)
	if err != nil {
		r.fail()
		return
	}
	up, err := net.DialTimeout("tcp", target, 10*time.Second)
	if err != nil {
		r.fail()
		return
	}
	defer up.Close()
	client.SetDeadline(time.Time{})
	rw := &core.FrameConn{Conn: bc, Sess: sess}
	core.Relay(rw, up, nil)
}

// camouflage پروکسی خام به سایت استتار — پروبر هیچ تفاوتی نمی‌بیند
func (r *RealityLiteEngine) camouflage(client net.Conn, first []byte) {
	up, err := net.DialTimeout("tcp", r.CamouflageAddr, 8*time.Second)
	if err != nil {
		return
	}
	defer up.Close()
	up.Write(first)
	done := make(chan struct{}, 2)
	go func() { io.Copy(up, client); done <- struct{}{} }()
	go func() { io.Copy(client, up); done <- struct{}{} }()
	select {
	case <-done:
	case <-time.After(120 * time.Second):
	}
}

func (r *RealityLiteEngine) fail() {
	r.mu.Lock()
	r.stats.Failures++
	r.stats.LastFailure = time.Now()
	r.mu.Unlock()
}

func (r *RealityLiteEngine) Dial(ctx context.Context, addr string) (net.Conn, error) {
	return net.DialTimeout("tcp", addr, 5*time.Second)
}
func (r *RealityLiteEngine) ProbeOK(challenge []byte) bool { return true }
func (r *RealityLiteEngine) Stats() core.Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stats
}
func (r *RealityLiteEngine) Close() error {
	if r.ln != nil {
		return r.ln.Close()
	}
	return nil
}

var _ = log.Println
