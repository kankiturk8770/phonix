package client

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

	"github.com/phoenixtunnel/phoenix/core"
	"github.com/phoenixtunnel/phoenix/core/tuntcp"
	"github.com/phoenixtunnel/phoenix/server"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

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
	os.WriteFile(filepath.Join(dir, "certs", "server.crt"),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	keyDER, _ := x509.MarshalECPrivateKey(key)
	os.WriteFile(filepath.Join(dir, "certs", "server.key"),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600)
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
			go io.Copy(c, c)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return ln.Addr()
}

// startChameleonServer سرور Chameleon روی آدرس تصادفی
func startChameleonServer(t *testing.T) string {
	t.Helper()
	setupCerts(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cert, err := tls.LoadX509KeyPair("certs/server.crt", "certs/server.key")
	if err != nil {
		t.Fatal(err)
	}
	cl := core.NewChameleonListener(core.ChameleonConf{}, ln, &tls.Config{
		Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13,
	})
	eng := server.NewChameleonEngineForTest(testKey)
	go func() {
		for {
			c, err := cl.Accept()
			if err != nil {
				return
			}
			go eng.HandleForTest(c)
		}
	}()
	return ln.Addr().String()
}

// TestClientChameleonEndToEnd کلاینت واقعی از پکیج client
func TestClientChameleonEndToEnd(t *testing.T) {
	echo := echoServer(t)
	srv := startChameleonServer(t)

	conn, err := DialChameleon(context.Background(), ChameleonConf{
		Server: srv, Keys: testKey,
		FakeSNIs: []string{"www.google.com"}, DecoyDelay: 5 * time.Millisecond,
	}, echo.String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	msg := []byte("client-package-works")
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write(msg); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, err := io.ReadFull(conn, buf[:len(msg)])
	if err != nil || string(buf[:n]) != string(msg) {
		t.Fatalf("echo: n=%d err=%v got=%q", n, err, buf[:n])
	}
}

// TestClientPhoenixUDP کلاینت PhoenixUDP در برابر سرور واقعی
func TestClientPhoenixUDP(t *testing.T) {
	echo := echoServer(t)
	eng := server.NewPhoenixUDPEngineForTest(testKey)
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	eng.ExportForTest(pc)
	go eng.ServeForTest()

	conn, err := DialPhoenixUDP(context.Background(), PUDPConf{
		Server: pc.LocalAddr().String(), Keys: testKey,
	}, echo.String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	msg := []byte("pudp-client-echo")
	conn.(*pudpClient).conn.(*net.UDPConn).SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write(msg); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, err := io.ReadFull(conn, buf[:len(msg)])
	if err != nil || string(buf[:n]) != string(msg) {
		t.Fatalf("pudp echo: n=%d err=%v", n, err)
	}
}

// TestSOCKS5 socks5 با dialer مستقیم (بدون تونل)
func TestSOCKS5(t *testing.T) {
	echo := echoServer(t)
	s := &SOCKS5{Addr: "127.0.0.1:0", Dialer: func(target string) (net.Conn, error) {
		return net.DialTimeout("tcp", target, 3*time.Second)
	}}
	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		t.Fatal(err)
	}
	s.ExportListener(ln)
	go s.ServeForTest()

	c, err := net.DialTimeout("tcp", ln.Addr().String(), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	host, portStr, _ := net.SplitHostPort(echo.String())
	var port int
	fmtSscanf(portStr, &port)

	c.Write([]byte{5, 1, 0})
	resp := make([]byte, 2)
	io.ReadFull(c, resp)
	if resp[1] != 0 {
		t.Fatal("no-auth rejected")
	}
	req := []byte{5, 1, 0, 3, byte(len(host))}
	req = append(req, host...)
	req = append(req, byte(port>>8), byte(port))
	c.Write(req)
	io.ReadFull(c, make([]byte, 10))

	msg := []byte("via-socks5")
	c.Write(msg)
	n, err := io.ReadFull(c, resp[:0])
	_ = n
	buf := make([]byte, 64)
	n, err = io.ReadFull(c, buf[:len(msg)])
	if err != nil || string(buf[:n]) != string(msg) {
		t.Fatalf("socks echo: n=%d err=%v", n, err)
	}
}

// TestVPNFullChain: TUN(fake) → TCP stack → Chameleon → سرور → echo
// کامل‌ترین تست: کل مسیر اندروید شبیه‌سازی می‌شود.
func TestVPNFullChain(t *testing.T) {
	echo := echoServer(t)
	srv := startChameleonServer(t)

	tun := newFakeTUN()
	vpn := NewVPN(tun, func(target string) (net.Conn, error) {
		// شبیه‌سازی: هر مقصدی که اپ می‌خواهد به اکوی لوکال می‌رسد
		return DialChameleon(context.Background(), ChameleonConf{
			Server: srv, Keys: testKey,
		}, echo.String())
	})
	go vpn.Run()

	// اپ «مجازی»: 10.0.0.2:40000 → echo (پورت echo در بسته)
	_, portStr, _ := net.SplitHostPort(echo.String())
	var dstPort int
	fmtSscanf(portStr, &dstPort)
	src := [4]byte{10, 0, 0, 2}
	var dst [4]byte
	copy(dst[:], net.ParseIP("93.184.216.34").To4())

	// ۱) SYN
	syn := buildTestTCP(src, dst, 40000, uint16(dstPort), 1000, 0, 0x02, nil)
	tun.inject(syn)
	synack := tun.readPacket(t)
	ihl0 := int(synack[0]&0x0F) * 4
	if synack[ihl0+13]&0x12 != 0x12 {
		t.Fatalf("no SYN-ACK: flags=%x", synack[ihl0+13])
	}
	// seq سرور از بایت ۴ TCP
	ihl := ihl0
	srvSeq := binary.BigEndian.Uint32(synack[ihl+4 : ihl+8])
	// ACK = client ISN+1 = 1001

	// ۲) ACK نهایی + داده
	ack := buildTestTCP(src, dst, 40000, uint16(dstPort), 1001, srvSeq+1, 0x10, nil)
	tun.inject(ack)
	payload := []byte("full-chain-test")
	psh := buildTestTCP(src, dst, 40000, uint16(dstPort), 1001, srvSeq+1, 0x18, payload)
	tun.inject(psh)

	// ۳) انتظار PSH+ACK برگشتی با payload اکو
	deadline := time.Now().Add(5 * time.Second)
	for {
		pkt := tun.readPacket(t)
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for data packet")
		}
		if pkt[13]&0x18 == 0x18 && len(pkt) > ihl+20 {
			got := pkt[ihl+20:]
			if string(got) == string(payload) {
				break
			}
		}
	}
	fmt.Println("VPN full chain OK")
}

// buildTestTCP ساخت پکت IPv4+TCP خام برای تزریق به TUN
func buildTestTCP(src, dst [4]byte, sport, dport uint16, seq, ack uint32, flags byte, payload []byte) []byte {
	tcpLen := 20 + len(payload)
	ipLen := 20 + tcpLen
	pkt := make([]byte, ipLen)
	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], uint16(ipLen))
	pkt[8] = 64
	pkt[9] = 6
	copy(pkt[12:16], src[:])
	copy(pkt[16:20], dst[:])
	binary.BigEndian.PutUint16(pkt[10:12], ipSum(pkt[:20]))
	tcp := pkt[20:]
	binary.BigEndian.PutUint16(tcp[0:2], sport)
	binary.BigEndian.PutUint16(tcp[2:4], dport)
	binary.BigEndian.PutUint32(tcp[4:8], seq)
	binary.BigEndian.PutUint32(tcp[8:12], ack)
	tcp[12] = 0x50
	tcp[13] = flags
	binary.BigEndian.PutUint16(tcp[14:16], 65535)
	copy(tcp[20:], payload)
	binary.BigEndian.PutUint16(tcp[16:18], tcpSumT(src, dst, tcp))
	return pkt
}

func ipSum(b []byte) uint16 {
	sum := uint32(0)
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(b[i:]))
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum > 0xFFFF {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	return ^uint16(sum)
}

func tcpSumT(src, dst [4]byte, tcp []byte) uint16 {
	pseudo := make([]byte, 12+len(tcp))
	copy(pseudo[0:4], src[:])
	copy(pseudo[4:8], dst[:])
	pseudo[9] = 6
	binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(tcp)))
	copy(pseudo[12:], tcp)
	return ipSum(pseudo)
}

// fakeTUN شبیه‌ساز TUN برای تست
type fakeTUN struct {
	in  chan []byte // پکت‌هایی که اپ می‌فرستد
	out chan []byte // پکت‌هایی که پشته می‌فرستد
}

func newFakeTUN() *fakeTUN {
	return &fakeTUN{in: make(chan []byte, 64), out: make(chan []byte, 64)}
}

func (f *fakeTUN) Read(b []byte) (int, error) {
	p := <-f.in
	return copy(b, p), nil
}

func (f *fakeTUN) Write(b []byte) (int, error) {
	f.out <- append([]byte(nil), b...)
	return len(b), nil
}

func (f *fakeTUN) Close() error { return nil }

func (f *fakeTUN) inject(pkt []byte) { f.in <- pkt }

func (f *fakeTUN) readPacket(t *testing.T) []byte {
	t.Helper()
	select {
	case p := <-f.out:
		return p
	case <-time.After(5 * time.Second):
		t.Fatal("no packet from stack")
		return nil
	}
}

func fmtSscanf(s string, out *int) {
	n := 0
	for _, ch := range s {
		n = n*10 + int(ch-'0')
	}
	*out = n
}

var _ = tuntcp.Quad{}
