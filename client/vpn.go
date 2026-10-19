// vpn.go — اتصال TUN به تونل: قلب اپ اندروید.
// توسط gomobile bind صدا زده می‌شود؛ در دسکتاپ هم مستقیماً قابل استفاده است.
package client

import (
	"io"
	"net"
	"sync"

	"github.com/phoenixtunnel/phoenix/core"
	"github.com/phoenixtunnel/phoenix/core/tuntcp"
)

// VPN اتصال یک TUN (فایل دیسکریپتور اندروید) به اینترنت از طریق تونل
type VPN struct {
	tun    io.ReadWriteCloser
	dial   func(target string) (net.Conn, error)
	stack  *tuntcp.Stack
	stop   chan struct{}
	done   chan struct{}
	once   sync.Once
}

// NewVPN ساخت VPN؛ tun همان fd اندروید است (os.NewFile(fd))
func NewVPN(tun io.ReadWriteCloser, dial func(target string) (net.Conn, error)) *VPN {
	return &VPN{tun: tun, dial: dial, stop: make(chan struct{}), done: make(chan struct{})}
}

// Done بسته می‌شود وقتی Run تمام شود
func (v *VPN) Done() <-chan struct{} { return v.done }

// Run حلقه‌ی اصلی تا بسته شدن
func (v *VPN) Run() (err error) {
	defer func() {
		close(v.done)
		if v.stack != nil {
			// بستن جریان‌های باز
		}
	}()
	v.stack = tuntcp.NewStack(v.onConn)
	errCh := make(chan error, 2)

	// TUN → stack
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := v.tun.Read(buf)
			if err != nil {
				errCh <- err
				return
			}
			for _, pkt := range v.stack.Deliver(buf[:n]) {
				v.tun.Write(pkt)
			}
		}
	}()

	// stack → TUN
	go func() {
		for {
			select {
			case pkt := <-v.stack.Out():
				v.tun.Write(pkt)
			case <-v.stop:
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

func (v *VPN) Close() error {
	v.once.Do(func() { close(v.stop) })
	return v.tun.Close()
}

// onConn برای هر اتصال TCP گوشی: تونل بزن و رله کن
func (v *VPN) onConn(c *tuntcp.Conn) {
	defer c.Close()
	up, err := v.dial(c.Target())
	if err != nil {
		return
	}
	defer up.Close()
	core.Relay(c, up, nil)
}
