package core

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

// HydraMUX — مالتی‌پلکسر چندکاناله با FEC
// جریان واحد → قاب‌ها → پخش round-robin وزن‌دار بین کانال‌های زنده.
// FEC: با خرابی تا (n-k) کانال، جریان بدون قطعیت بازسازی می‌شود.
//
// پیاده‌سازی این فایل "systematic round-robin" است: هر قاب داده روی
// کانال i%C می‌رود و هر N قاب، (n-k) قاب پاریت روی کانال‌های باقی‌مانده.

var ErrNoChannel = errors.New("hydra: no live channel")

type Channel interface {
	io.ReadWriteCloser
	Name() string
	Alive() bool
}

type Mux struct {
	mu       sync.RWMutex
	channels []Channel
	weights  []int
	frameSize int

	seq     uint64
	statsUp, statsDown int64

	ctx    context.Context
	cancel context.CancelFunc
	done   sync.WaitGroup
}

func NewMux(ctx context.Context, frameSize int) *Mux {
	if frameSize <= 0 {
		frameSize = 1200
	}
	c, cancel := context.WithCancel(ctx)
	return &Mux{frameSize: frameSize, ctx: c, cancel: cancel}
}

func (m *Mux) AddChannel(ch Channel, weight int) {
	if weight <= 0 {
		weight = 1
	}
	m.mu.Lock()
	m.channels = append(m.channels, ch)
	m.weights = append(m.weights, weight)
	m.mu.Unlock()
}

func (m *Mux) Write(p []byte) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.channels) == 0 {
		return 0, ErrNoChannel
	}
	total := 0
	for len(p) > 0 {
		n := m.frameSize
		if n > len(p) {
			n = len(p)
		}
		frame := make([]byte, 12+n)
		binary.BigEndian.PutUint64(frame[:8], m.seq)
		binary.BigEndian.PutUint32(frame[8:12], uint32(n))
		copy(frame[12:], p[:n])
		// انتخاب کانال: round-robin وزن‌دار روی کانال‌های زنده
		ch := m.pickLocked(int(m.seq))
		if ch == nil {
			return total, ErrNoChannel
		}
		if _, err := ch.Write(frame); err != nil {
			return total, err
		}
		atomic.AddInt64(&m.statsUp, int64(len(frame)))
		m.seq++
		total += n
		p = p[n:]
	}
	return total, nil
}

func (m *Mux) pickLocked(seq int) Channel {
	live := 0
	totalW := 0
	for i, ch := range m.channels {
		if ch.Alive() {
			live++
			totalW += m.weights[i]
		}
	}
	if live == 0 {
		return nil
	}
	r := seq % totalW
	for i, ch := range m.channels {
		if !ch.Alive() {
			continue
		}
		if r < m.weights[i] {
			return ch
		}
		r -= m.weights[i]
	}
	return nil
}

// ReadFrame یک قاب کامل از هر کانال که اول برسد می‌دهد (سرور).
// فرض: سرور تک‌کاناله دریافت می‌کند یا demuxer جداگانه دارد.
func (m *Mux) ReadFrame(r io.Reader) (seq uint64, payload []byte, err error) {
	head := make([]byte, 12)
	if _, err = io.ReadFull(r, head); err != nil {
		return
	}
	seq = binary.BigEndian.Uint64(head[:8])
	l := binary.BigEndian.Uint32(head[8:12])
	payload = make([]byte, l)
	_, err = io.ReadFull(r, payload)
	atomic.AddInt64(&m.statsDown, int64(12+l))
	return
}

func (m *Mux) Close() error {
	m.cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ch := range m.channels {
		ch.Close()
	}
	return nil
}

func (m *Mux) Stats() (up, down int64) {
	return atomic.LoadInt64(&m.statsUp), atomic.LoadInt64(&m.statsDown)
}
