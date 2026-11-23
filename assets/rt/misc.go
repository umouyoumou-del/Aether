package rt

import (
	"math"
	"math/rand/v2"
	"runtime"
	"sync"
	"time"
)

func goos() string { return runtime.GOOS }

// ===== time 模块 =====

var startTime = time.Now()

func Now() float64 { return float64(time.Now().UnixNano()) / 1e9 }

func Monotonic() float64 { return time.Since(startTime).Seconds() }

func Sleep(seconds float64) {
	if seconds <= 0 {
		return
	}
	Flush()
	time.Sleep(time.Duration(seconds * float64(time.Second)))
}

func SleepMs(ms int) {
	Flush()
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

// FormatTime 按 strftime 风格的格式格式化时间戳（本地时间）。
func FormatTime(ts float64, layout string) string {
	t := time.Unix(0, int64(ts*1e9))
	return strftime(t, layout)
}

func NowStr(layout string) string { return strftime(time.Now(), layout) }

// ===== datetime 模块的辅助函数 =====
// datetime 表示不带时区的“墙上时间”（与 Python 的 naive datetime 相同）：
// 内部是按 UTC 解释的、距 1970-01-01 00:00 的微秒数，运算时不受夏令时影响。

func naiveUs(t time.Time) int {
	y, mo, d := t.Date()
	h, mi, sec := t.Clock()
	return int(time.Date(y, mo, d, h, mi, sec, t.Nanosecond(), time.UTC).UnixMicro())
}

// NaiveNowUs 返回当前的本地墙上时间。
func NaiveNowUs() int { return naiveUs(time.Now()) }

// NaiveUTCNowUs 返回当前的 UTC 时间。
func NaiveUTCNowUs() int { return int(time.Now().UTC().UnixMicro()) }

// NaiveFromTimestamp 把时间戳转换为本地墙上时间。
func NaiveFromTimestamp(ts float64) int {
	return naiveUs(time.UnixMicro(int64(math.Round(ts * 1e6))))
}

// NaiveToTimestamp 把本地墙上时间转换为时间戳。
func NaiveToTimestamp(us int) float64 {
	t := time.UnixMicro(int64(us)).UTC()
	local := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.Local)
	return float64(local.UnixMicro()) / 1e6
}

// NaiveFormat 按 strftime 风格的格式格式化墙上时间（%z、%Z 为空，与 Python 的 naive datetime 相同）。
func NaiveFormat(us int, layout string) string {
	return strftimeIn(time.UnixMicro(int64(us)).UTC(), layout, true)
}

// NaiveParse 按 strftime 风格的格式解析墙上时间。
func NaiveParse(s, layout string) int {
	t, err := time.ParseInLocation(goLayout(layout), s, time.UTC)
	if err != nil {
		Raise("ValueError", "时间 %s 与格式 %s 不匹配", QuoteStr(s), QuoteStr(layout))
	}
	return int(t.UnixMicro())
}

func strftime(t time.Time, layout string) string { return strftimeIn(t, layout, false) }

func strftimeIn(t time.Time, layout string, naive bool) string {
	var out []byte
	for i := 0; i < len(layout); i++ {
		c := layout[i]
		if c != '%' || i+1 >= len(layout) {
			out = append(out, c)
			continue
		}
		i++
		switch layout[i] {
		case 'Y':
			out = t.AppendFormat(out, "2006")
		case 'm':
			out = t.AppendFormat(out, "01")
		case 'd':
			out = t.AppendFormat(out, "02")
		case 'H':
			out = t.AppendFormat(out, "15")
		case 'M':
			out = t.AppendFormat(out, "04")
		case 'S':
			out = t.AppendFormat(out, "05")
		case 'y':
			out = t.AppendFormat(out, "06")
		case 'b':
			out = t.AppendFormat(out, "Jan")
		case 'B':
			out = t.AppendFormat(out, "January")
		case 'a':
			out = t.AppendFormat(out, "Mon")
		case 'A':
			out = t.AppendFormat(out, "Monday")
		case 'f':
			out = append(out, []byte(Fmt(t.Nanosecond()/1000, "06d"))...)
		case 'j':
			out = append(out, []byte(Fmt(t.YearDay(), "03d"))...)
		case 'p':
			out = t.AppendFormat(out, "PM")
		case 'I':
			out = t.AppendFormat(out, "03")
		case 'z':
			if !naive {
				out = t.AppendFormat(out, "-0700")
			}
		case 'Z':
			if !naive {
				out = t.AppendFormat(out, "MST")
			}
		case '%':
			out = append(out, '%')
		default:
			out = append(out, '%', layout[i])
		}
	}
	return string(out)
}

// goLayout 把 strftime 风格的格式转换为 Go 的时间格式。
func goLayout(layout string) string {
	repl := map[byte]string{'Y': "2006", 'm': "01", 'd': "02", 'H': "15", 'M': "04", 'S': "05", 'y': "06",
		'b': "Jan", 'B': "January", 'a': "Mon", 'A': "Monday", 'f': "000000", 'p': "PM", 'I': "03", 'j': "002", 'z': "-0700"}
	var out []byte
	for i := 0; i < len(layout); i++ {
		c := layout[i]
		if c == '%' && i+1 < len(layout) {
			if layout[i+1] == '%' {
				out = append(out, '%')
				i++
				continue
			}
			if r, ok := repl[layout[i+1]]; ok {
				out = append(out, r...)
				i++
				continue
			}
		}
		out = append(out, c)
	}
	return string(out)
}

// ParseTime 解析 strftime 风格的时间字符串，返回时间戳。
func ParseTime(s, layout string) float64 {
	t, err := time.ParseInLocation(goLayout(layout), s, time.Local)
	if err != nil {
		Raise("ValueError", "无法按 %s 解析时间 %s", QuoteStr(layout), QuoteStr(s))
	}
	return float64(t.UnixNano()) / 1e9
}

// Stopwatch 是简单的计时器。
type Stopwatch struct{ start time.Time }

func NewStopwatch() *Stopwatch                 { return &Stopwatch{start: time.Now()} }
func (s *Stopwatch) Elapsed() float64          { return time.Since(s.start).Seconds() }
func (s *Stopwatch) ElapsedMs() float64        { return float64(time.Since(s.start).Microseconds()) / 1000 }
func (s *Stopwatch) Reset()                    { s.start = time.Now() }
func (s *Stopwatch) String() string            { return FloatStr(s.Elapsed()) + "s" }

// ===== random 模块 =====

var (
	rngMu sync.Mutex
	rng   = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0x9e3779b97f4a7c15))
)

func Seed(n int) {
	rngMu.Lock()
	rng = rand.New(rand.NewPCG(uint64(n), 0x9e3779b97f4a7c15))
	rngMu.Unlock()
}

func Random() float64 {
	rngMu.Lock()
	defer rngMu.Unlock()
	return rng.Float64()
}

func RandInt(a, b int) int {
	if b < a {
		Raise("ValueError", "randint(%d, %d)：上界小于下界", a, b)
	}
	rngMu.Lock()
	defer rngMu.Unlock()
	return a + rng.IntN(b-a+1)
}

func RandRange(a, b int) int {
	if b <= a {
		Raise("ValueError", "randrange(%d, %d)：范围为空", a, b)
	}
	rngMu.Lock()
	defer rngMu.Unlock()
	return a + rng.IntN(b-a)
}

func Uniform(a, b float64) float64 {
	return a + (b-a)*Random()
}

func Gauss(mu, sigma float64) float64 {
	rngMu.Lock()
	defer rngMu.Unlock()
	return mu + sigma*rng.NormFloat64()
}

func Choice[T any](l *List[T]) T {
	if len(l.S) == 0 {
		Raise("IndexError", "不能从空列表中随机选择")
	}
	rngMu.Lock()
	defer rngMu.Unlock()
	return l.S[rng.IntN(len(l.S))]
}

func Shuffle[T any](l *List[T]) {
	rngMu.Lock()
	defer rngMu.Unlock()
	rng.Shuffle(len(l.S), func(i, j int) { l.S[i], l.S[j] = l.S[j], l.S[i] })
}

func Sample[T any](l *List[T], k int) *List[T] {
	if k < 0 || k > len(l.S) {
		Raise("ValueError", "样本数量 %d 超出范围", k)
	}
	c := l.Copy()
	Shuffle(c)
	c.S = c.S[:k]
	return c
}

// ===== math 模块 =====

func Sqrt(x float64) float64 {
	if x < 0 {
		Raise("ValueError", "不能对负数 %s 开平方", FloatStr(x))
	}
	return math.Sqrt(x)
}

func Log(x float64) float64 {
	if x <= 0 {
		Raise("ValueError", "对数的参数必须为正数，得到 %s", FloatStr(x))
	}
	return math.Log(x)
}

func LogBase(x, base float64) float64 { return Log(x) / Log(base) }

func Log2(x float64) float64 {
	if x <= 0 {
		Raise("ValueError", "对数的参数必须为正数")
	}
	return math.Log2(x)
}

func Log10(x float64) float64 {
	if x <= 0 {
		Raise("ValueError", "对数的参数必须为正数")
	}
	return math.Log10(x)
}

func Floor(x float64) int { return FloatToInt(math.Floor(x)) }
func Ceil(x float64) int  { return FloatToInt(math.Ceil(x)) }
func Trunc(x float64) int { return FloatToInt(math.Trunc(x)) }

func Gcd(a, b int) int {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func Lcm(a, b int) int {
	if a == 0 || b == 0 {
		return 0
	}
	return Abs(a / Gcd(a, b) * b)
}

func Factorial(n int) int {
	if n < 0 {
		Raise("ValueError", "阶乘的参数不能为负数")
	}
	r := 1
	for i := 2; i <= n; i++ {
		r *= i
	}
	return r
}

func Comb(n, k int) int {
	if k < 0 || k > n {
		return 0
	}
	if k > n-k {
		k = n - k
	}
	r := 1
	for i := 1; i <= k; i++ {
		r = r * (n - k + i) / i
	}
	return r
}

func Isqrt(n int) int {
	if n < 0 {
		Raise("ValueError", "isqrt 的参数不能为负数")
	}
	x := int(math.Sqrt(float64(n)))
	for x*x > n {
		x--
	}
	for (x+1)*(x+1) <= n {
		x++
	}
	return x
}

func IsClose(a, b, relTol, absTol float64) bool {
	if a == b {
		return true
	}
	d := math.Abs(a - b)
	return d <= math.Max(relTol*math.Max(math.Abs(a), math.Abs(b)), absTol)
}

func Degrees(x float64) float64 { return x * 180 / math.Pi }
func Radians(x float64) float64 { return x * math.Pi / 180 }

func IsPrime(n int) bool {
	if n < 2 {
		return false
	}
	if n%2 == 0 {
		return n == 2
	}
	for i := 3; i*i <= n; i += 2 {
		if n%i == 0 {
			return false
		}
	}
	return true
}

// LogN 实现 math.log(x, base)；base 为 0 时表示自然对数。
func LogN(x, base float64) float64 {
	if base == 0 {
		return Log(x)
	}
	if base <= 0 || base == 1 {
		Raise("ValueError", "对数的底数无效：%s", FloatStr(base))
	}
	return Log(x) / math.Log(base)
}
