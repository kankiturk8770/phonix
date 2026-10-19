package core

import (
	"io"
	"sync"
	"sync/atomic"
)

// رله‌ی دوطرفه با شمارش بایت — قلب تونل.

type RelayStats struct {
	Up, Down *int64 // atomic
}

// Relay دوطرفه: a↔b تا زمانی که یک طرف بسته شود.
// up/down به‌ترتیب b→a و a→b شمرده می‌شوند.
func Relay(a, b io.ReadWriter, st *RelayStats) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		n, _ := io.Copy(a, b)
		if st != nil && st.Up != nil {
			atomic.AddInt64(st.Up, n)
		}
		if c, ok := a.(interface{ CloseWrite() error }); ok {
			c.CloseWrite()
		}
	}()
	go func() {
		defer wg.Done()
		n, _ := io.Copy(b, a)
		if st != nil && st.Down != nil {
			atomic.AddInt64(st.Down, n)
		}
		if c, ok := b.(interface{ CloseWrite() error }); ok {
			c.CloseWrite()
		}
	}()
	wg.Wait()
}
