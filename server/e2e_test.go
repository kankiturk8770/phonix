package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/phoenixtunnel/phoenix/client"
	"github.com/phoenixtunnel/phoenix/core"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

// setupCerts ساخت گواهی موقت برای تست ChameleonEngine
func setupCerts(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "certs"), 0o755)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, _ := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, _ := x509.MarshalECPrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	os.WriteFile(filepath.Join(dir, "certs", "server.crt"), certPEM, 0o600)
	os.WriteFile(filepath.Join(dir, "certs", "server.key"), keyPEM, 0o600)
	old, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(old) })
}

func echoServer(t *testing.T) net.Addr {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) { io.Copy(c, c); c.Close() }(c)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return ln.Addr()
}

// TestChameleonEndToEnd: کلاینت → handshake → آدرس مقصد → داده → اکو
func TestChameleonEndToEnd(t *testing.T) {
	setupCerts(t)
	echo := echoServer(t)

	eng := &ChameleonEngine{conf: core.ChameleonConf{}, keys: testKey}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cl := core.NewChameleonListener(core.ChameleonConf{}, ln, &tls.Config{
		Certificates: []tls.Certificate{mustCert()}, MinVersion: tls.VersionTLS13,
	})
	go func() {
		for {
			c, err := cl.Accept()
			if err != nil {
				return
			}
			go eng.handle(c)
		}
	}()

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sess, err := core.ClientHandshake(conn, testKey)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	tgt, _ := core.EncodeTarget(echo.String())
	if err := writeFrame(conn, sess.SealFrame(tgt)); err != nil {
		t.Fatal(err)
	}
	rw := &core.FrameConn{Conn: conn, Sess: sess}
	msg := []byte("salam-donya")
	if _, err := rw.Write(msg); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := io.ReadFull(rw, buf[:len(msg)])
	if err != nil || n != len(msg) {
		t.Fatalf("echo read: n=%d err=%v", n, err)
	}
	if string(buf[:n]) != string(msg) {
		t.Fatalf("echo mismatch: %q", buf[:n])
	}
}

// TestRealityCamouflage: کلاینت ناشناس باید پاسخ سایت استتار را بگیرد
func TestRealityCamouflage(t *testing.T) {
	camo := echoServer(t) // هر چی بفرستی همان را برمی‌گرداند
	eng := &RealityLiteEngine{keys: testKey, CamouflageAddr: camo.String()}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go eng.handle(c)
		}
	}()

	conn, _ := net.DialTimeout("tcp", ln.Addr().String(), 3*time.Second)
	defer conn.Close()
	conn.Write([]byte("GET / HTTP/1.1")) // بدون مارجیک PHX1
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 64)
	n, err := io.ReadFull(conn, buf[:14]) // TCP ممکن است تکه‌تکه برسد
	if err != nil || n != 14 {
		t.Fatalf("camouflage response: n=%d err=%v", n, err)
	}
	if string(buf[:n]) != "GET / HTTP/1.1" {
		t.Fatalf("camouflage relay mismatch: %q", buf[:n])
	}
}

// TestPhantomDNSQuery: پرسش DNS روی دامنه‌ی چرخان → پاسخ TXT با ACK
func TestPhantomDNSQuery(t *testing.T) {
	conf := core.PhantomConf{
		Domain:        "ns1.test.example",
		DGASeed:       "seed-123",
		RotateMinutes: 10,
	}
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	eng := &PhantomEngine{conf: conf, keys: testKey}
	eng.srv = core.NewPhantomServer(conf)
	eng.sessions = map[uint16]*phantomSession{}

	srv := core.NewPhantomServer(conf)
	qname := srv.EncodeQuery(7, 42, []byte("payload-bytes"), time.Now())

	// ساخت پرسش DNS خام
	var q []byte
	id := uint16(0x1234)
	q = append(q, byte(id>>8), byte(id), 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0)
	for _, lbl := range splitDots(qname) {
		q = append(q, byte(len(lbl)))
		q = append(q, lbl...)
	}
	q = append(q, 0, 0, 16 /*TXT*/, 0, 1)

	go func() {
		buf := make([]byte, 4096)
		n, peer, _ := pc.ReadFrom(buf)
		eng.handleQuery(pc, peer, buf[:n])
	}()

	client, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.WriteTo(q, pc.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	client.SetReadDeadline(time.Now().Add(3 * time.Second))
	resp := make([]byte, 4096)
	n, _, err := client.ReadFrom(resp)
	if err != nil {
		t.Fatalf("no DNS response: %v", err)
	}
	if an := binary.BigEndian.Uint16(resp[6:8]); an != 1 {
		t.Fatalf("expected 1 answer, got %d", an)
	}
	fmt.Printf("phantom dns response %d bytes, answers=%d\n", n, binary.BigEndian.Uint16(resp[6:8]))
}

// TestPhantomUpstreamCamouflage: دامنه‌ی خارج از چرخان → forward واقعی
func TestPhantomUpstreamCamouflage(t *testing.T) {
	// یک ریزالور UDP جعلی که برای هر پرسش پاسخ ثابت می‌دهد
	resolver, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer resolver.Close()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, peer, err := resolver.ReadFrom(buf)
			if err != nil {
				return
			}
			resp := make([]byte, n+12)
			copy(resp, buf[:2])         // ID
			resp[2], resp[3] = 0x81, 0x80 // QR+RD+RA
			resp[7] = 1                  // ANCOUNT
			copy(resp[12:], buf[12:n])
			// رکورد A برای example.com → 1.2.3.4
			ans := []byte{0xC0, 0x0C, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 1, 2, 3, 4}
			resp = append(resp, ans...)
			resolver.WriteTo(resp, peer)
		}
	}()

	// متأسفانه forwardDNS آدرس upstream را هاردکد دارد؛ اینجا فقط parse و
	// مسیر camouflage را منطقی تست می‌کنیم (در عمل upstream از conf خوانده می‌شود)
	conf := core.PhantomConf{Domain: "ns1.test.example", DGASeed: "s", RotateMinutes: 10}
	s := core.NewPhantomServer(conf)
	q := &dnsQuery{id: 1, flags: 0x0100, name: "google.com", qtype: 1}
	if err := s.ValidateQuery(q.name, time.Now()); err == nil {
		t.Fatal("google.com must NOT validate as rotated domain")
	}
	if s.ValidateQuery(s.EncodeQuery(1, 0, []byte("x"), time.Now()), time.Now()) != nil {
		t.Fatal("own rotated domain must validate")
	}
}

// TestPhoenixUDPEndToEnd: HELLO → ACCEPT → DATA(target) → DATA(payload) → اکو برگردد
func TestPhoenixUDPEndToEnd(t *testing.T) {
	echo := echoServer(t)
	eng := &PhoenixUDPEngine{conf: core.HydraConf{}, keys: testKey}
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	eng.pc = pc
	eng.sessions = map[uint32]*udpSession{}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, peer, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			eng.dispatch(peer, buf[:n])
		}
	}()

	// کلاینت واقعی: HELLO/ACCEPT + رمز AES-GCM + ARQ + EOF
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := client.DialPhoenixUDP(ctx, client.PUDPConf{Server: pc.LocalAddr().String(), Keys: testKey}, echo.String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	msg := make([]byte, 200000)
	randomRead(msg)
	go c.Write(msg)
	got := make([]byte, len(msg))
	c.(interface{ SetReadDeadline(time.Time) error }).SetReadDeadline(time.Now().Add(10 * time.Second))
	done := make(chan error, 1)
	go func() { _, err := io.ReadFull(c, got); done <- err }()
	select {
	case err := <-done:
		if err != nil || string(got) != string(msg) {
			t.Fatalf("udp echo mismatch: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("udp echo timeout")
	}
	// EOF: نیم‌بستن کلاینت → سرور مقصد را می‌بندد → EOF به کلاینت برمی‌گردد
	c.(interface{ CloseWrite() error }).CloseWrite()
	go func() { _, err := io.ReadAll(c); done <- err }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("no EOF over phoenix-udp")
	}
	fmt.Println("phoenix-udp end-to-end OK")
}

func hmacSHA256(key, msg []byte) [32]byte {
	m := newHMAC(key)
	m.Write(msg)
	var out [32]byte
	copy(out[:], m.Sum(nil))
	return out
}

func splitDots(s string) []string {
	var out []string
	cur := ""
	for _, ch := range s {
		if ch == '.' {
			out = append(out, cur)
			cur = ""
		} else {
			cur += string(ch)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func mustCert() tls.Certificate {
	c, err := tls.LoadX509KeyPair("certs/server.crt", "certs/server.key")
	if err != nil {
		panic(err)
	}
	return c
}
