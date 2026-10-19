package core

import (
	"encoding/binary"
	"io"
	"net"
	"time"
)

// ReadFrame خواندن یک قاب: uint32 len + payload (سقف 1MB)
func ReadFrame(r io.Reader) ([]byte, error) {
	head := make([]byte, 4)
	if _, err := io.ReadFull(r, head); err != nil {
		return nil, err
	}
	l := binary.BigEndian.Uint32(head)
	if l > 1<<20 {
		return nil, io.ErrUnexpectedEOF
	}
	frame := make([]byte, l)
	_, err := io.ReadFull(r, frame)
	return frame, err
}

// WriteFrame نوشتن یک قاب: uint32 len + payload
func WriteFrame(w io.Writer, frame []byte) error {
	head := make([]byte, 4)
	binary.BigEndian.PutUint32(head, uint32(len(frame)))
	if _, err := w.Write(head); err != nil {
		return err
	}
	_, err := w.Write(frame)
	return err
}

// FrameConn اتصال net.Conn روی قاب‌های رمزنگاری‌شده با سشن
type FrameConn struct {
	Conn net.Conn
	Sess *Session
	rbuf []byte
}

func (f *FrameConn) Read(b []byte) (int, error) {
	for len(f.rbuf) == 0 {
		frame, err := ReadFrame(f.Conn)
		if err != nil {
			return 0, err
		}
		plain, err := f.Sess.OpenFrame(frame)
		if err != nil {
			return 0, err
		}
		f.rbuf = plain
	}
	n := copy(b, f.rbuf)
	f.rbuf = f.rbuf[n:]
	return n, nil
}

func (f *FrameConn) Write(b []byte) (int, error) {
	frame := f.Sess.SealFrame(b)
	if err := WriteFrame(f.Conn, frame); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (f *FrameConn) Close() error                       { return f.Conn.Close() }
func (f *FrameConn) LocalAddr() net.Addr                { return f.Conn.LocalAddr() }
func (f *FrameConn) RemoteAddr() net.Addr               { return f.Conn.RemoteAddr() }
func (f *FrameConn) SetDeadline(t time.Time) error      { return f.Conn.SetDeadline(t) }
func (f *FrameConn) SetReadDeadline(t time.Time) error  { return f.Conn.SetReadDeadline(t) }
func (f *FrameConn) SetWriteDeadline(t time.Time) error { return f.Conn.SetWriteDeadline(t) }
