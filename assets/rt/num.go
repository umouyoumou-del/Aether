package rt

import (
	"math"
	"math/bits"
	"strconv"
)

// FloorDiv 实现整数的 //（向下取整，与 Python 一致）。
func FloorDiv[T Integer](a, b T) T {
	if b == 0 {
		Raise("ZeroDivisionError", "整数除以零")
	}
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// Mod 实现整数的 %（结果符号与除数相同，与 Python 一致）。
func Mod[T Integer](a, b T) T {
	if b == 0 {
		Raise("ZeroDivisionError", "整数对零取模")
	}
	r := a % b
	if r != 0 && ((r < 0) != (b < 0)) {
		r += b
	}
	return r
}

func FFloorDiv(a, b float64) float64 {
	if b == 0 {
		Raise("ZeroDivisionError", "浮点数除以零")
	}
	return math.Floor(a / b)
}

func FMod(a, b float64) float64 {
	if b == 0 {
		Raise("ZeroDivisionError", "浮点数对零取模")
	}
	r := math.Mod(a, b)
	if r != 0 && ((r < 0) != (b < 0)) {
		r += b
	}
	return r
}

func FFloorDiv32(a, b float32) float32 { return float32(FFloorDiv(float64(a), float64(b))) }
func FMod32(a, b float32) float32      { return float32(FMod(float64(a), float64(b))) }

// IPow 实现整数的 **。
// IPow 实现整数的乘方（除 int 以外的整数类型）；结果超出类型的范围时抛出 OverflowError。
func IPow[T Integer](a, b T) T {
	if b < 0 {
		Raise("ValueError", "整数的负数次幂请使用浮点数，例如 2.0 ** -1")
	}
	a0, b0 := a, b
	mul := func(x, y T) T {
		p := x * y
		if x != 0 && (p/x != y || (x == ^T(0) && y < 0 && p < 0)) {
			base := Str(a0)
			if a0 < 0 {
				base = "(" + base + ")"
			}
			Raise("OverflowError", "%s ** %v 超出了 %s 的范围", base, b0, TypeName(a0))
		}
		return p
	}
	var r T = 1
	for b > 0 {
		if b&1 == 1 {
			r = mul(r, a)
		}
		b >>= 1
		if b > 0 {
			a = mul(a, a)
		}
	}
	return r
}

// IPowInt 实现 int ** int；结果超出 int 的范围时抛出 OverflowError（而不是回绕）。
// 平方是最常见的情形，单独处理（可以被内联）。
func IPowInt(a, b int) int {
	if b == 2 {
		m := uint64(a)
		if a < 0 {
			m = -m
		}
		if hi, lo := bits.Mul64(m, m); hi == 0 && lo <= math.MaxInt64 {
			return int(lo)
		}
	}
	return ipowInt(a, b)
}

func ipowInt(a, b int) int {
	if b < 0 {
		Raise("ValueError", "整数的负数次幂请使用浮点数，例如 2.0 ** -1")
	}
	neg := a < 0 && b&1 == 1
	m := uint64(a)
	if a < 0 {
		m = uint64(^a) + 1
	}
	limit := uint64(math.MaxInt64)
	if neg {
		limit++
	}
	a0, b0 := a, b
	over := func() {
		base, fbase := strconv.Itoa(a0), strconv.Itoa(a0)+".0"
		if a0 < 0 {
			base, fbase = "("+base+")", "("+fbase+")"
		}
		Raise("OverflowError", "%s ** %d 超出了 int 的范围（-2^63 到 2^63-1）；需要更大的数时可以用浮点数，例如 %s ** %d", base, b0, fbase, b0)
	}
	var r uint64 = 1
	for b > 0 {
		if b&1 == 1 {
			hi, lo := bits.Mul64(r, m)
			if hi != 0 || lo > limit {
				over()
			}
			r = lo
		}
		b >>= 1
		if b > 0 {
			// 后面还会用到 m 的平方：它本身超出范围时结果一定超出范围
			hi, lo := bits.Mul64(m, m)
			if hi != 0 || lo > limit {
				over()
			}
			m = lo
		}
	}
	if neg {
		return -int(r)
	}
	return int(r)
}

func Pow32(a, b float32) float32 { return float32(math.Pow(float64(a), float64(b))) }

func Abs[T Number](x T) T {
	if x < 0 {
		return -x
	}
	return x
}

func Min[T Ordered](xs ...T) T {
	m := xs[0]
	for _, x := range xs[1:] {
		if x < m {
			m = x
		}
	}
	return m
}

func Max[T Ordered](xs ...T) T {
	m := xs[0]
	for _, x := range xs[1:] {
		if x > m {
			m = x
		}
	}
	return m
}

func MinCmp[T any](xs ...T) T {
	m := xs[0]
	for _, x := range xs[1:] {
		if Compare(x, m) < 0 {
			m = x
		}
	}
	return m
}

func MaxCmp[T any](xs ...T) T {
	m := xs[0]
	for _, x := range xs[1:] {
		if Compare(x, m) > 0 {
			m = x
		}
	}
	return m
}

func MinBy[T any, K any](key func(T) K, xs ...T) T {
	m, mk := xs[0], key(xs[0])
	for _, x := range xs[1:] {
		if k := key(x); Compare(k, mk) < 0 {
			m, mk = x, k
		}
	}
	return m
}

func MaxBy[T any, K any](key func(T) K, xs ...T) T {
	m, mk := xs[0], key(xs[0])
	for _, x := range xs[1:] {
		if k := key(x); Compare(k, mk) > 0 {
			m, mk = x, k
		}
	}
	return m
}

// Round 实现 round(x)：四舍六入五取偶，返回整数（与 Python 一致）。
func Round(x float64) int {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		Raise("ValueError", "不能对 %s 取整", FloatStr(x))
	}
	return int(math.RoundToEven(x))
}

// RoundN 实现 round(x, n)：按浮点数的精确值四舍六入五取偶（与 Python 一致）。
func RoundN(x float64, n int) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	if n >= 0 && n <= 17 {
		v, err := strconv.ParseFloat(strconv.FormatFloat(x, 'f', n, 64), 64)
		if err == nil {
			return v
		}
	}
	p := math.Pow(10, float64(n))
	v := math.RoundToEven(x * p)
	r := v / p
	if math.IsInf(r, 0) || math.IsNaN(r) {
		return x
	}
	return r
}

func Divmod(a, b int) T2[int, int] {
	return T2[int, int]{FloorDiv(a, b), Mod(a, b)}
}

func FDivmod(a, b float64) T2[float64, float64] {
	return T2[float64, float64]{FFloorDiv(a, b), FMod(a, b)}
}

func IntClamp(x, lo, hi int) int {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

func FloatClamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

func FloatToInt(x float64) int {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		Raise("ValueError", "不能把 %s 转换为整数", FloatStr(x))
	}
	if x >= 9.223372036854775807e18 || x < -9.223372036854775808e18 {
		Raise("OverflowError", "%s 超出了 int 的范围", FloatStr(x))
	}
	return int(x)
}

func IntBitLen(x int) int {
	n := 0
	if x < 0 {
		x = -x
	}
	for x > 0 {
		n++
		x >>= 1
	}
	return n
}

func FloatIsInteger(x float64) bool { return x == math.Trunc(x) && !math.IsInf(x, 0) }
