package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"

	"github.com/phoenixtunnel/phoenix/core"
)

// pipeTUN دو سر یک TUN مجازی: phone ↔ vpn
type pipeTUN struct {
	in     chan []byte // پکت‌های گوشی به VPN
	out    chan []byte // پکت‌های VPN به گوشی
	closed chan struct{}
}

func (p *pipeTUN) Read(b []byte) (int, error) {
	select {
	case pkt := <-p.in:
		return copy(b, pkt), nil
	case <-p.closed:
		return 0, io.EOF
	}
}
func (p *pipeTUN) Write(b []byte) (int, error) {
	select {
	case p.out <- append([]byte(nil), b...):
		return len(b), nil
	case <-p.closed:
		return 0, io.EOF
	}
}
func (p *pipeTUN) Close() error {
	select {
	case <-p.closed:
	default:
		close(p.closed)
	}
	return nil
}

// newPhone یک پشته‌ی gVisor جدا که نقش گوشی (10.0.0.2) را بازی می‌کند
func newPhone(t *testing.T, tun *pipeTUN) *stack.Stack {
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})
	ep := channel.New(1024, vpnMTU, "")
	if e := s.CreateNIC(1, ep); e != nil {
		t.Fatal(e)
	}
	s.AddProtocolAddress(1, tcpip.ProtocolAddress{
		Protocol:          ipv4.ProtocolNumber,
		AddressWithPrefix: tcpip.AddrFrom4([4]byte{10, 0, 0, 2}).WithPrefix(),
	}, stack.AddressProperties{})
	s.SetRouteTable([]tcpip.Route{{Destination: header.IPv4EmptySubnet, NIC: 1}})
	go func() { // phone → tun
		for {
			pkt := ep.ReadContext(context.Background())
			if pkt == nil {
				return
			}
			v := pkt.ToView()
			tun.in <- append([]byte(nil), v.AsSlice()...)
			v.Release()
			pkt.DecRef()
		}
	}()
	go func() { // tun → phone
		for {
			select {
			case b := <-tun.out:
				pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(b)})
				ep.InjectInbound(ipv4.ProtocolNumber, pkt)
				pkt.DecRef()
			case <-tun.closed:
				return
			}
		}
	}()
	return s
}

// TestVPNNetstackBulkAndDNS: گوشی مجازی → TUN → VPN → Chameleon → سرور
// ۸ مگابایت دانلود با بررسی هش + پرس‌وجوی DNS روی UDP.
func TestVPNNetstackBulkAndDNS(t *testing.T) {
	srv := startChameleonServer(t)

	// سرور «اینترنت»: ۸MB داده‌ی تصادفی
	blob := make([]byte, 8<<20)
	rand.Read(blob)
	want := sha256.Sum256(blob)
	web, _ := net.Listen("tcp", "127.0.0.1:0")
	defer web.Close()
	go func() {
		for {
			c, err := web.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) { defer c.Close(); c.Write(blob) }(c)
		}
	}()

	// DNS upstream مجازی (DNS-over-TCP): پاسخ A ثابت
	dnsLn, _ := net.Listen("tcp", "127.0.0.1:0")
	defer dnsLn.Close()
	go func() {
		for {
			c, err := dnsLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				for {
					h := make([]byte, 2)
					if _, err := io.ReadFull(c, h); err != nil {
						return
					}
					q := make([]byte, binary.BigEndian.Uint16(h))
					if _, err := io.ReadFull(c, q); err != nil {
						return
					}
					resp := append([]byte(nil), q...)
					resp[2] |= 0x80
					resp[7] = 1 // ANCOUNT=1
					resp = append(resp, 0xC0, 12, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 1, 2, 3, 4)
					out := make([]byte, 2+len(resp))
					binary.BigEndian.PutUint16(out, uint16(len(resp)))
					copy(out[2:], resp)
					c.Write(out)
				}
			}(c)
		}
	}()

	tun := &pipeTUN{in: make(chan []byte, 512), out: make(chan []byte, 512), closed: make(chan struct{})}
	vpn := NewVPN(tun, func(target string) (net.Conn, error) {
		real := web.Addr().String()
		if target == "1.1.1.1:53" {
			real = dnsLn.Addr().String()
		}
		return DialChameleon(context.Background(), ChameleonConf{Server: srv, Keys: testKey}, real)
	})
	go vpn.Run()
	defer vpn.Close()
	phone := newPhone(t, tun)
	defer phone.Close()

	// ۱) دانلود بزرگ
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := gonet.DialContextTCP(ctx, phone, tcpip.FullAddress{
		NIC: 1, Addr: tcpip.AddrFrom4([4]byte{93, 184, 216, 34}), Port: 80,
	}, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatalf("phone dial: %v", err)
	}
	c.SetReadDeadline(time.Now().Add(30 * time.Second))
	got, err := io.ReadAll(c)
	c.Close()
	if err != nil {
		t.Fatalf("read: %v (got %d bytes)", err, len(got))
	}
	if sha256.Sum256(got) != want {
		t.Fatalf("data mismatch: got %d bytes, want %d", len(got), len(blob))
	}

	// ۲) DNS روی UDP به DNS مجازی
	uc, err := gonet.DialUDP(phone, nil, &tcpip.FullAddress{
		NIC: 1, Addr: tcpip.AddrFrom4([4]byte{10, 0, 0, 1}), Port: 53,
	}, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatalf("udp dial: %v", err)
	}
	defer uc.Close()
	q := []byte{0xAB, 0xCD, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0,
		7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0, 0, 1, 0, 1}
	uc.Write(q)
	uc.SetReadDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, 512)
	n, err := uc.Read(buf)
	if err != nil {
		t.Fatalf("dns read: %v", err)
	}
	r := buf[:n]
	if r[0] != 0xAB || r[1] != 0xCD || !bytes.HasSuffix(r, []byte{1, 2, 3, 4}) {
		t.Fatalf("bad dns answer: %x", r)
	}

	// ۳) AAAA باید پاسخ خالی بگیرد
	q6 := append([]byte(nil), q...)
	q6[len(q6)-3] = 28
	uc.Write(q6)
	n, err = uc.Read(buf)
	if err != nil || binary.BigEndian.Uint16(buf[6:8]) != 0 || buf[2]&0x80 == 0 {
		t.Fatalf("AAAA not blocked: %x %v", buf[:n], err)
	}
	up, down := vpn.Traffic()
	if down < int64(len(blob)) {
		t.Fatalf("traffic counter: up=%d down=%d", up, down)
	}
}

func TestLinkRoundTrip(t *testing.T) {
	c := core.ClientConfig{Name: "سرور آلمان", Server: "1.2.3.4:443", Keys: "k3y", Engine: "chameleon",
		FakeSNIs: []string{"www.google.com"}, UDPServer: "1.2.3.4:51820"}
	link := core.EncodeLink(c)
	d, err := core.DecodeLink(link)
	if err != nil || d.Server != c.Server || d.Keys != c.Keys || d.Name != c.Name || d.UDPServer != c.UDPServer {
		t.Fatalf("roundtrip: %v %+v (%s)", err, d, link)
	}
}
