package rttensor

import "math"

// float32 的 exp 与 tanh（CPU 后端使用）：比 float64 的 math.Exp、math.Tanh 快得多，
// 相对误差约为 1e-7（与 float32 本身的精度相当）。

// expf 用 Cephes 的方法：exp(x) = 2^n · exp(r)，其中 r = x - n·ln2 ∈ [-ln2/2, ln2/2]，exp(r) 用多项式计算。
func expf(x float32) float32 {
	switch {
	case x != x:
		return x
	case x > 88.72283:
		return float32(math.Inf(1))
	case x < -87.33654:
		return 0
	}
	fx := x*1.44269504088896341 + 0.5
	n := float32(int32(fx))
	if n > fx {
		n-- // 向下取整
	}
	z := x - n*0.693359375 + n*2.12194440e-4
	p := ((((1.9875691500e-4*z+1.3981999507e-3)*z+8.3334519073e-3)*z+4.1665795894e-2)*z+1.6666665459e-1)*z + 5.0000001201e-1
	r := p*z*z + z + 1
	e := int32(n)
	if e > 127 {
		return r * 2 * math.Float32frombits(uint32(e-1+127)<<23)
	}
	return r * math.Float32frombits(uint32(e+127)<<23)
}

// tanhf：|x| 很小时用泰勒展开（避免 1 - 2/(e^2x + 1) 的相消误差），其余用 expf。
func tanhf(x float32) float32 {
	a := x
	if a < 0 {
		a = -a
	}
	if a < 0.2 {
		x2 := x * x
		return x * (1 + x2*(-1.0/3+x2*(2.0/15+x2*(-17.0/315))))
	}
	if a > 9 {
		return float32(math.Copysign(1, float64(x)))
	}
	t := 1 - 2/(expf(2*a)+1)
	if x < 0 {
		return -t
	}
	return t
}

func sigmoidf(x float32) float32 { return 1 / (1 + expf(-x)) }
