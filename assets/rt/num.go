package rt

import (
	"math"
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
func IPow[T Integer](a, b T) T {
	if b < 0 {
		Raise("ValueError", "整数的负数次幂请使用浮点数，例如 2.0 ** -1")
	}
	var r T = 1
	for b > 0 {
		if b&1 == 1 {
			r *= a
		}
		a *= a
		b >>= 1
	}
	return r
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
