// vpn.go — اتصال TUN به تونل: قلب اپ اندروید.
// توسط gomobile bind صدا زده می‌شود؛ در دسکتاپ هم مستقیماً قابل استفاده است.
//
// پشته‌ی TCP/IP سمت TUN با gVisor netstack پیاده شده (همان پشته‌ای که
// tun2socks/sing-box استفاده می‌کنند): بازارسال، پنجره، MSS و IPv6 را
// درست مدیریت می‌کند. هر اتصال TCP گوشی → dial تونل → رله.
// UDP پورت 53 (DNS) از داخل همان تونل به‌صورت DNS-over-TCP پاسخ داده می‌شود.
package client

import (
	"context"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"

	"github.com/phoenixtunnel/phoenix/core"
)

const (
	vpnMTU = 1500
	nicID  = 1
)

// VirtualDNS آدرس DNS مجازی داخل TUN (اپ اندروید همین را به سیستم می‌دهد)
const VirtualDNS = "10.0.0.1"

// VPN اتصال یک TUN (فایل دیسکریپتور اندروید) به اینترنت از طریق تونل
type VPN struct {
	// شمارنده‌ها اول struct تا روی ARM 32 بیتی هم‌تراز ۸ بایت باشند
	up, down int64 // بایت‌های ارسال/دریافت از دید گوشی (atomic)

	tun  io.ReadWriteCloser
	dial func(target string) (net.Conn, error)

	// DNSUpstream سرور DNS که از داخل تونل پرسیده می‌شود (پیش‌فرض 1.1.1.1:53)
	DNSUpstream string
	// BlockIPv6DNS پاسخ خالی به پرس‌وجوی AAAA تا اپ‌ها فقط IPv4 (داخل تونل) بزنند
	BlockIPv6DNS bool

	stack *stack.Stack
	ep    *channel.Endpoint
	dns   *tunnelDNS

	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// NewVPN ساخت VPN؛ tun همان fd اندروید است (os.NewFile(fd))
func NewVPN(tun io.ReadWriteCloser, dial func(target string) (net.Conn, error)) *VPN {
	return &VPN{
		tun: tun, dial: dial,
		DNSUpstream: "1.1.1.1:53", BlockIPv6DNS: true,
		stop: make(chan struct{}), done: make(chan struct{}),
	}
}

// Done بسته می‌شود وقتی Run تمام شود
func (v *VPN) Done() <-chan struct{} { return v.done }

// Traffic بایت‌های بالا/پایین از شروع نشست
func (v *VPN) Traffic() (up, down int64) { return atomic.LoadInt64(&v.up), atomic.LoadInt64(&v.down) }

// Run حلقه‌ی اصلی تا بسته شدن
func (v *VPN) Run() (err error) {
	defer close(v.done)

	if err := v.setupStack(); err != nil {
		return err
	}
	defer v.stack.Close()
	defer v.ep.Close()
	v.dns = newTunnelDNS(v.dial, v.DNSUpstream, v.BlockIPv6DNS)
	defer v.dns.Close()

	errCh := make(chan error, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// TUN → stack
	go func() {
		buf := make([]byte, 65535)
		for {
			n, err := v.tun.Read(buf)
			if err != nil {
				errCh <- err
				return
			}
			if n == 0 {
				continue
			}
			var proto tcpip.NetworkProtocolNumber
			switch buf[0] >> 4 {
			case 4:
				proto = header.IPv4ProtocolNumber
			case 6:
				proto = header.IPv6ProtocolNumber
			default:
				continue
			}
			pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{
				Payload: buffer.MakeWithData(append([]byte(nil), buf[:n]...)),
			})
			v.ep.InjectInbound(proto, pkt)
			pkt.DecRef()
		}
	}()

	// stack → TUN
	go func() {
		for {
			pkt := v.ep.ReadContext(ctx)
			if pkt == nil {
				return
			}
			view := pkt.ToView()
			_, werr := v.tun.Write(view.AsSlice())
			view.Release()
			pkt.DecRef()
			if werr != nil {
				errCh <- werr
				return
			}
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-v.stop:
		return nil
	}
}

func (v *VPN) setupStack() error {
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})
	ep := channel.New(1024, vpnMTU, "")
	if e := s.CreateNIC(nicID, ep); e != nil {
		return errFromTcpip(e)
	}
	s.SetPromiscuousMode(nicID, true)
	s.SetSpoofing(nicID, true)
	s.SetRouteTable([]tcpip.Route{
		{Destination: header.IPv4EmptySubnet, NIC: nicID},
		{Destination: header.IPv6EmptySubnet, NIC: nicID},
	})
	sack := tcpip.TCPSACKEnabled(true)
	s.SetTransportProtocolOption(tcp.ProtocolNumber, &sack)

	tcpFwd := tcp.NewForwarder(s, 0, 4096, v.onTCP)
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpFwd.HandlePacket)
	udpFwd := udp.NewForwarder(s, v.onUDP)
	s.SetTransportProtocolHandler(udp.ProtocolNumber, udpFwd.HandlePacket)

	v.stack, v.ep = s, ep
	return nil
}

func (v *VPN) Close() error {
	v.once.Do(func() { close(v.stop) })
	return v.tun.Close()
}

// onTCP برای هر اتصال TCP گوشی: اول تونل بزن، اگر موفق بود handshake را
// کامل کن (در غیر این صورت RST تا اپ سریع fallback کند) و رله کن.
func (v *VPN) onTCP(r *tcp.ForwarderRequest) {
	id := r.ID()
	target := net.JoinHostPort(id.LocalAddress.String(), strconv.Itoa(int(id.LocalPort)))

	var up net.Conn
	var err error
	if id.LocalAddress.String() == VirtualDNS && id.LocalPort != 53 {
		// مثلاً DNS-over-TLS (853) اندروید به DNS مجازی: سریع رد کن تا به DNS معمولی برگردد
		r.Complete(true)
		return
	}
	if id.LocalPort == 53 && id.LocalAddress.String() == VirtualDNS {
		// DNS-over-TCP به DNS مجازی → به upstream واقعی از داخل تونل
		up, err = v.dial(v.DNSUpstream)
	} else {
		up, err = v.dial(target)
	}
	if err != nil {
		r.Complete(true)
		return
	}
	var wq waiter.Queue
	ep, terr := r.CreateEndpoint(&wq)
	if terr != nil {
		up.Close()
		r.Complete(true)
		return
	}
	r.Complete(false)
	ep.SocketOptions().SetKeepAlive(true)
	c := gonet.NewTCPConn(&wq, ep)
	defer c.Close()
	defer up.Close()
	core.Relay(c, up, &core.RelayStats{Up: &v.down, Down: &v.up})
}

// onUDP: فقط DNS (پورت 53) پاسخ داده می‌شود؛ بقیه‌ی UDP (مثل QUIC) رها می‌شود
// تا اپ‌ها خودکار به TCP (داخل تونل) برگردند.
func (v *VPN) onUDP(r *udp.ForwarderRequest) bool {
	id := r.ID()
	if id.LocalPort != 53 {
		return true // بی‌صدا رها کن
	}
	var wq waiter.Queue
	ep, terr := r.CreateEndpoint(&wq)
	if terr != nil {
		return true
	}
	c := gonet.NewUDPConn(&wq, ep)
	go func() {
		defer c.Close()
		buf := make([]byte, 4096)
		c.SetReadDeadline(deadlineIn(dnsQueryTimeout))
		n, err := c.Read(buf)
		if err != nil {
			return
		}
		atomic.AddInt64(&v.up, int64(n))
		resp, err := v.dns.Query(buf[:n])
		if err != nil {
			return
		}
		atomic.AddInt64(&v.down, int64(len(resp)))
		c.Write(resp)
	}()
	return true
}

type tcpipErr struct{ e tcpip.Error }

func (t tcpipErr) Error() string { return t.e.String() }

func errFromTcpip(e tcpip.Error) error { return tcpipErr{e} }
