package core

import (
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// رله‌ی دوطرفه با شمارش بایت — قلب تونل.

type RelayStats struct {
	Up, Down *int64 // atomic
}

// HalfCloseIdle بعد از پایان یک جهت، اگر جهت دیگر این مدت بیکار بماند
// هر دو طرف بسته می‌شوند (مانند uplinkOnly/downlinkOnly در v2ray).
var HalfCloseIdle = 10 * time.Second

// Relay دوطرفه: a↔b تا زمانی که یک طرف بسته شود.
// up/down به‌ترتیب b→a و a→b شمرده می‌شوند (لحظه‌ای، برای نمایش سرعت زنده).
//
// پایان جریان (EOF) باید از تونل رد شود: قاب‌های رمزنگاری‌شده half-close
// ندارند، پس وقتی پاسخ (b→a) تمام شد و a نیم‌بستن را پشتیبانی نمی‌کند،
// a کامل بسته می‌شود تا طرف مقابل تونل EOF ببیند.
// وقتی a→b تمام شد و b نیم‌بستن ندارد، جهت دیگر تا HalfCloseIdle بیکاری
// فرصت دارد؛ بعد همه بسته می‌شود تا اتصال‌های رهاشده نشت نکنند.
// خطا (نه EOF) در هر جهت → هر دو طرف بسته می‌شوند.
func Relay(a, b io.ReadWriter, st *RelayStats) {
	var wg sync.WaitGroup
	wg.Add(2)
	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			closeRW(a)
			closeRW(b)
		})
	}
	var lastActive atomic.Int64
	lastActive.Store(time.Now().UnixNano())
	done := make(chan struct{})
	var halfOnce sync.Once
	// watchdog پس از پایان یک جهت: بستن در صورت بیکاری جهت باقی‌مانده
	halfClosed := func() {
		halfOnce.Do(func() {
			go func() {
				t := time.NewTicker(time.Second)
				defer t.Stop()
				for {
					select {
					case <-done:
						return
					case <-t.C:
						if time.Since(time.Unix(0, lastActive.Load())) > HalfCloseIdle {
							closeBoth()
							return
						}
					}
				}
			}()
		})
	}
	var up, down *int64
	if st != nil {
		up, down = st.Up, st.Down
	}
	go func() {
		defer wg.Done()
		err := copyCount(a, b, up, &lastActive)
		if err != nil {
			closeBoth()
			return
		}
		if c, ok := a.(interface{ CloseWrite() error }); ok {
			c.CloseWrite()
			halfClosed()
		} else {
			closeRW(a)
		}
	}()
	go func() {
		defer wg.Done()
		err := copyCount(b, a, down, &lastActive)
		if err != nil {
			closeBoth()
			return
		}
		if c, ok := b.(interface{ CloseWrite() error }); ok {
			c.CloseWrite()
		}
		halfClosed()
	}()
	wg.Wait()
	close(done)
}

// copyCount مانند io.Copy اما شمارش لحظه‌ای و ثبت زمان فعالیت؛ EOF → nil
func copyCount(dst io.Writer, src io.Reader, counter *int64, last *atomic.Int64) error {
	buf := make([]byte, 32*1024)
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			last.Store(time.Now().UnixNano())
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
			if counter != nil {
				atomic.AddInt64(counter, int64(n))
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

func closeRW(x io.ReadWriter) {
	if c, ok := x.(io.Closer); ok {
		c.Close()
	}
}
