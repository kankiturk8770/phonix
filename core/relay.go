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
//
// پایان جریان (EOF) باید از تونل رد شود: قاب‌های رمزنگاری‌شده half-close
// ندارند، پس وقتی پاسخ (b→a) تمام شد و a نیم‌بستن را پشتیبانی نمی‌کند،
// a کامل بسته می‌شود تا طرف مقابل تونل EOF ببیند (وگرنه دانلودهایی که
// با بستن اتصال تمام می‌شوند تا ابد معطل می‌مانند).
// خطا (نه EOF) در هر جهت → هر دو طرف بسته می‌شوند تا اتصال نشت نکند.
func Relay(a, b io.ReadWriter, st *RelayStats) {
	var wg sync.WaitGroup
	wg.Add(2)
	closeBoth := func() {
		closeRW(a)
		closeRW(b)
	}
	go func() {
		defer wg.Done()
		n, err := io.Copy(a, b)
		if st != nil && st.Up != nil {
			atomic.AddInt64(st.Up, n)
		}
		if err != nil {
			closeBoth()
			return
		}
		if c, ok := a.(interface{ CloseWrite() error }); ok {
			c.CloseWrite()
		} else {
			closeRW(a)
		}
	}()
	go func() {
		defer wg.Done()
		n, err := io.Copy(b, a)
		if st != nil && st.Down != nil {
			atomic.AddInt64(st.Down, n)
		}
		if err != nil {
			closeBoth()
			return
		}
		if c, ok := b.(interface{ CloseWrite() error }); ok {
			c.CloseWrite()
		}
	}()
	wg.Wait()
}

func closeRW(x io.ReadWriter) {
	if c, ok := x.(io.Closer); ok {
		c.Close()
	}
}
