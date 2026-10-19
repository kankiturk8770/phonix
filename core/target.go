package core

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
)

// EncodeTarget آدرس مقصد را به فرمت باینری قاب تبدیل می‌کند
// فرمت: atyp(1) | addr | port(2)
// atyp: 1=IPv4  2=Domain  3=IPv6  — مشابه SOCKS5 اما ساده‌شده
func EncodeTarget(addr string) ([]byte, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("bad port in %q", addr)
	}
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			out := make([]byte, 1+4+2)
			out[0] = 1
			copy(out[1:5], v4)
			binary.BigEndian.PutUint16(out[5:7], uint16(port))
			return out, nil
		}
		out := make([]byte, 1+16+2)
		out[0] = 3
		copy(out[1:17], ip.To16())
		binary.BigEndian.PutUint16(out[17:19], uint16(port))
		return out, nil
	}
	if len(host) > 255 {
		return nil, fmt.Errorf("domain too long")
	}
	out := make([]byte, 1+1+len(host)+2)
	out[0] = 2
	out[1] = byte(len(host))
	copy(out[2:2+len(host)], host)
	binary.BigEndian.PutUint16(out[2+len(host):], uint16(port))
	return out, nil
}

// DecodeTarget بازگرداندن آدرس مقصد از فرمت باینری
func DecodeTarget(b []byte) (string, error) {
	if len(b) < 1 {
		return "", fmt.Errorf("target: empty")
	}
	switch b[0] {
	case 1:
		if len(b) < 7 {
			return "", fmt.Errorf("target: short ipv4")
		}
		return net.JoinHostPort(net.IP(b[1:5]).String(),
			strconv.Itoa(int(binary.BigEndian.Uint16(b[5:7])))), nil
	case 2:
		if len(b) < 2 || len(b) < 2+int(b[1])+2 {
			return "", fmt.Errorf("target: short domain")
		}
		l := int(b[1])
		host := string(b[2 : 2+l])
		port := int(binary.BigEndian.Uint16(b[2+l : 2+l+2]))
		return net.JoinHostPort(host, strconv.Itoa(port)), nil
	case 3:
		if len(b) < 19 {
			return "", fmt.Errorf("target: short ipv6")
		}
		return net.JoinHostPort(net.IP(b[1:17]).String(),
			strconv.Itoa(int(binary.BigEndian.Uint16(b[17:19])))), nil
	}
	return "", fmt.Errorf("target: bad atyp %d", b[0])
}
