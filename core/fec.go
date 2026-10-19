package core

// FEC ساده و قطعی: گروه‌های n تایی که k تای آن داده و (n-k) تای آن
// پاریت XOR هستند. تحمل تا (n-k) گم‌شدگی در هر گروه.
// برای n-k=1 کاملاً بهینه است و برای بیشتر، الگوی XOR زنجیره‌ای:
//   parity[i] = data[0] ^ data[1+i]  (i از 0 تا n-k-1)
// با این الگو تا (n-k) گم‌شده‌ی همزمان در موقعیت‌های مختلف قابل بازیابی است
// مشروط به اینکه داده[0] زنده بماند؛ برای گم‌شدگی تکی همیشه بازیابی کامل.

const (
	DefaultFECK = 4
	DefaultFECN = 6 // ۴ داده + ۲ پاریت
)

// FECGroup یک گروه FEC روی قاب‌های هم‌اندازه
type FECGroup struct {
	k, n   int
	size   int   // اندازه‌ی هر قاب (با صفر پر می‌شود)
	data   [][]byte
	used   int
}

func NewFECGroup(k, n, size int) *FECGroup {
	if k <= 0 || n <= k {
		k, n = DefaultFECK, DefaultFECN
	}
	return &FECGroup{k: k, n: n, size: size, data: make([][]byte, n)}
}

// Add قاب داده را در جایگاهش می‌گذارد؛ جایگاه از idx=k..n-1 برای پاریت
func (g *FECGroup) Add(idx int, frame []byte) {
	if idx < 0 || idx >= g.n {
		return
	}
	padded := make([]byte, g.size)
	copy(padded, frame)
	g.data[idx] = padded
	g.used++
}

// AddData قاب‌های داده را از ابتدا پر می‌کند؛ وقتی kام رسید، پاریت‌ها را می‌سازد.
// طرح: parity[j] = XOR همه‌ی داده‌ها به‌جز d[j]  (j = 0..r-1, r = n-k)
// با این طرح «هر گم‌شدگی تکی» در هر موقعیتی قابل بازیابی است:
//   - اگر m < r :  d[m] = parity[m] ^ XOR_{i≠m} d[i]
//   - اگر m >= r:  d[m] = parity[0] ^ XOR_{i≠0, i≠m} d[i]
func (g *FECGroup) AddData(frame []byte) (done bool, parities [][]byte) {
	idx := -1
	for i := 0; i < g.k; i++ {
		if g.data[i] == nil {
			idx = i
			break
		}
	}
	if idx < 0 {
		return true, nil
	}
	g.Add(idx, frame)
	g.used++
	if idx == g.k-1 {
		r := g.n - g.k
		if r > g.k {
			r = g.k
		}
		for j := 0; j < r; j++ {
			p := make([]byte, g.size)
			for i := 0; i < g.k; i++ {
				if i == j {
					continue
				}
				for b := range p {
					p[b] ^= g.data[i][b]
				}
			}
			g.data[g.k+j] = p
		}
		return true, g.data[g.k : g.k+r]
	}
	return false, nil
}

// Recover بازسازی قاب گم‌شده — فقط با دقیقاً یک گم‌شدگی در گروه
func (g *FECGroup) Recover(idx int) ([]byte, bool) {
	if g.data[idx] != nil {
		return g.data[idx], true
	}
	missing := -1
	for i := 0; i < g.n; i++ {
		if g.data[i] == nil {
			if missing >= 0 {
				return nil, false // بیش از یک گم‌شده
			}
			missing = i
		}
	}
	if missing != idx || idx >= g.k {
		return nil, false
	}
	r := g.n - g.k
	rec := make([]byte, g.size)
	if idx < r {
		copy(rec, g.data[g.k+idx])
		for i := 0; i < g.k; i++ {
			if i == idx {
				continue
			}
			for b := range rec {
				rec[b] ^= g.data[i][b]
			}
		}
	} else {
		copy(rec, g.data[g.k])
		for i := 0; i < g.k; i++ {
			if i == 0 || i == idx {
				continue
			}
			for b := range rec {
				rec[b] ^= g.data[i][b]
			}
		}
	}
	g.data[idx] = rec
	return rec, true
}
