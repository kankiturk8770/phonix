package client

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

// SOCKS5 سرور محلی (بدون احراز هویت) که هر CONNECT را از طریق dialer تونل می‌کند
type SOCKS5 struct {
	Addr   string
	Dialer func(target string) (net.Conn, error)

	ln net.Listener
}

func (s *SOCKS5) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}
	s.ln = ln
	return s.ServeForTest()
}

// ExportListener تزریق listener برای تست/گو موبایل
func (s *SOCKS5) ExportListener(ln net.Listener) { s.ln = ln }

// ServeForTest حلقه‌ی accept (listener قبلاً تزریق شده)
func (s *SOCKS5) ServeForTest() error {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return err
		}
		go s.handle(c)
	}
}

func (s *SOCKS5) Close() error {
	if s.ln != nil {
		return s.ln.Close()
	}
	return nil
}

func (s *SOCKS5) handle(c net.Conn) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(15 * time.Second))

	// greeting
	head := make([]byte, 2)
	if _, err := io.ReadFull(c, head); err != nil || head[0] != 5 {
		return
	}
	methods := make([]byte, head[1])
	if _, err := io.ReadFull(c, methods); err != nil {
		return
	}
	c.Write([]byte{5, 0}) // no-auth

	// request
	req := make([]byte, 4)
	if _, err := io.ReadFull(c, req); err != nil || req[1] != 1 { // CONNECT only
		return
	}
	var host string
	switch req[3] {
	case 1:
		b := make([]byte, 4)
		io.ReadFull(c, b)
		host = fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3])
	case 3:
		lb := make([]byte, 1)
		io.ReadFull(c, lb)
		b := make([]byte, lb[0])
		io.ReadFull(c, b)
		host = string(b)
	case 4:
		b := make([]byte, 16)
		io.ReadFull(c, b)
		host = net.IP(b).String()
	}
	pb := make([]byte, 2)
	io.ReadFull(c, pb)
	target := net.JoinHostPort(host, fmt.Sprintf("%d", binary.BigEndian.Uint16(pb)))

	up, err := s.Dialer(target)
	if err != nil {
		c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer up.Close()
	c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	c.SetDeadline(time.Time{})

	done := make(chan struct{}, 2)
	go func() { io.Copy(up, c); done <- struct{}{} }()
	go func() { io.Copy(c, up); done <- struct{}{} }()
	<-done
}
