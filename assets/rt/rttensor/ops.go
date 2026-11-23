package rttensor

import (
	"math"
	"sync"
	"sync/atomic"

	"aether/assets/rt"
)

func raiseShape(format string, args ...any) { rt.Raise("ValueError", format, args...) }

func raiseIndex(i, n int) { rt.Raise("IndexError", "下标 %d 超出范围（长度为 %d）", i, n) }

// ===== 逐元素运算 =====

func binaryRaw(name string, op int, a, b *Tensor) *Tensor {
	dev := sameDevice(name, a, b)
	if sameShape(a.shape, b.shape) {
		out := newTensor(dev, a.shape)
		dev.binary(op, a.st, b.st, out.st, out.n, nil)
		return out
	}
	shape, bc := broadcastShapes(name, a.shape, b.shape)
	out := newTensor(dev, shape)
	dev.binary(op, a.st, b.st, out.st, out.n, bc)
	return out
}

func unaryRaw(op int, x *Tensor, a, b float32) *Tensor {
	out := newTensor(x.dev(), x.shape)
	x.dev().unary(op, x.st, out.st, x.n, a, b)
	return out
}

func (a *Tensor) Add(b *Tensor) *Tensor {
	out := binaryRaw("加法", bAdd, a, b)
	return record(out, "add", []*Tensor{a, b}, func(g *Tensor) []*Tensor {
		return []*Tensor{sumTo(g, a.shape), sumTo(g, b.shape)}
	})
}

func (a *Tensor) Sub(b *Tensor) *Tensor {
	out := binaryRaw("减法", bSub, a, b)
	return record(out, "sub", []*Tensor{a, b}, func(g *Tensor) []*Tensor {
		return []*Tensor{sumTo(g, a.shape), sumTo(g.Neg(), b.shape)}
	})
}

func (a *Tensor) Mul(b *Tensor) *Tensor {
	out := binaryRaw("乘法", bMul, a, b)
	return record(out, "mul", []*Tensor{a, b}, func(g *Tensor) []*Tensor {
		var ga, gb *Tensor
		if a.requiresGrad {
			ga = sumTo(g.Mul(b), a.shape)
		}
		if b.requiresGrad {
			gb = sumTo(g.Mul(a), b.shape)
		}
		return []*Tensor{ga, gb}
	})
}

func (a *Tensor) Div(b *Tensor) *Tensor {
	out := binaryRaw("除法", bDiv, a, b)
	return record(out, "div", []*Tensor{a, b}, func(g *Tensor) []*Tensor {
		var ga, gb *Tensor
		if a.requiresGrad {
			ga = sumTo(g.Div(b), a.shape)
		}
		if b.requiresGrad {
			// d(a/b)/db = -a / b²
			gb = sumTo(g.Mul(a).Div(b.Mul(b)).Neg(), b.shape)
		}
		return []*Tensor{ga, gb}
	})
}

// Pow 实现张量的张量次幂（a、b 都是张量）。
func (a *Tensor) Pow(b *Tensor) *Tensor {
	out := binaryRaw("乘方", bPow, a, b)
	return record(out, "pow", []*Tensor{a, b}, func(g *Tensor) []*Tensor {
		var ga, gb *Tensor
		if a.requiresGrad {
			ga = sumTo(g.Mul(b).Mul(a.Pow(b.AddScalar(-1))), a.shape)
		}
		if b.requiresGrad {
			gb = sumTo(g.Mul(out).Mul(a.Log()), b.shape)
		}
		return []*Tensor{ga, gb}
	})
}

// Maximum、Minimum 是逐元素的最大、最小值（相等时梯度给第一个参数）。
func (a *Tensor) Maximum(b *Tensor) *Tensor {
	out := binaryRaw("maximum", bMax, a, b)
	return record(out, "maximum", []*Tensor{a, b}, func(g *Tensor) []*Tensor {
		mask := binaryRaw("maximum", bGe, a, b)
		return []*Tensor{sumTo(g.Mul(mask), a.shape), sumTo(g.Mul(mask.RSubScalar(1)), b.shape)}
	})
}

func (a *Tensor) Minimum(b *Tensor) *Tensor {
	out := binaryRaw("minimum", bMin, a, b)
	return record(out, "minimum", []*Tensor{a, b}, func(g *Tensor) []*Tensor {
		mask := binaryRaw("minimum", bLe, a, b)
		return []*Tensor{sumTo(g.Mul(mask), a.shape), sumTo(g.Mul(mask.RSubScalar(1)), b.shape)}
	})
}

// 比较：结果是 0/1 的张量（不参与求导）
func (a *Tensor) Gt(b *Tensor) *Tensor { return binaryRaw("比较", bGt, a, b) }
func (a *Tensor) Lt(b *Tensor) *Tensor { return binaryRaw("比较", bLt, a, b) }
func (a *Tensor) Eq(b *Tensor) *Tensor { return binaryRaw("比较", bEq, a, b) }
func (a *Tensor) Ge(b *Tensor) *Tensor { return binaryRaw("比较", bGe, a, b) }
func (a *Tensor) Le(b *Tensor) *Tensor { return binaryRaw("比较", bLe, a, b) }

func (x *Tensor) GtScalar(k float64) *Tensor { return unaryRaw(uGtS, x, float32(k), 0) }
func (x *Tensor) LtScalar(k float64) *Tensor { return unaryRaw(uLtS, x, float32(k), 0) }

// ===== 与标量的运算 =====

func (x *Tensor) AddScalar(k float64) *Tensor {
	out := unaryRaw(uAddS, x, float32(k), 0)
	return record(out, "add", []*Tensor{x}, func(g *Tensor) []*Tensor { return []*Tensor{g} })
}

func (x *Tensor) SubScalar(k float64) *Tensor { return x.AddScalar(-k) }

func (x *Tensor) MulScalar(k float64) *Tensor {
	out := unaryRaw(uMulS, x, float32(k), 0)
	return record(out, "mul", []*Tensor{x}, func(g *Tensor) []*Tensor { return []*Tensor{g.MulScalar(k)} })
}

func (x *Tensor) DivScalar(k float64) *Tensor {
	if k == 0 {
		rt.Raise("ZeroDivisionError", "张量除以零")
	}
	return x.MulScalar(1 / k)
}

// RSubScalar 实现 k - x。
func (x *Tensor) RSubScalar(k float64) *Tensor {
	out := unaryRaw(uRSubS, x, float32(k), 0)
	return record(out, "rsub", []*Tensor{x}, func(g *Tensor) []*Tensor { return []*Tensor{g.Neg()} })
}

// RDivScalar 实现 k / x。
func (x *Tensor) RDivScalar(k float64) *Tensor {
	out := unaryRaw(uRDivS, x, float32(k), 0)
	return record(out, "rdiv", []*Tensor{x}, func(g *Tensor) []*Tensor {
		return []*Tensor{g.Mul(out).Div(x).Neg()}
	})
}

func (x *Tensor) PowScalar(p float64) *Tensor {
	out := unaryRaw(uPowS, x, float32(p), 0)
	return record(out, "pow", []*Tensor{x}, func(g *Tensor) []*Tensor {
		return []*Tensor{g.Mul(unaryRaw(uPowSGrad, x, float32(p), 0))}
	})
}

// RPowScalar 实现 k ** x。
func (x *Tensor) RPowScalar(k float64) *Tensor {
	return x.MulScalar(math.Log(k)).Exp()
}

// ===== 一元函数 =====

func (x *Tensor) Neg() *Tensor {
	out := unaryRaw(uNeg, x, 0, 0)
	return record(out, "neg", []*Tensor{x}, func(g *Tensor) []*Tensor { return []*Tensor{g.Neg()} })
}

func (x *Tensor) Exp() *Tensor {
	out := unaryRaw(uExp, x, 0, 0)
	return record(out, "exp", []*Tensor{x}, func(g *Tensor) []*Tensor { return []*Tensor{g.Mul(out)} })
}

func (x *Tensor) Log() *Tensor {
	out := unaryRaw(uLog, x, 0, 0)
	return record(out, "log", []*Tensor{x}, func(g *Tensor) []*Tensor { return []*Tensor{g.Div(x)} })
}

func (x *Tensor) Sqrt() *Tensor {
	out := unaryRaw(uSqrt, x, 0, 0)
	return record(out, "sqrt", []*Tensor{x}, func(g *Tensor) []*Tensor {
		return []*Tensor{g.Div(out).MulScalar(0.5)}
	})
}

func (x *Tensor) Abs() *Tensor {
	out := unaryRaw(uAbs, x, 0, 0)
	return record(out, "abs", []*Tensor{x}, func(g *Tensor) []*Tensor {
		return []*Tensor{g.Mul(unaryRaw(uSign, x, 0, 0))}
	})
}

func (x *Tensor) Relu() *Tensor {
	out := unaryRaw(uRelu, x, 0, 0)
	return record(out, "relu", []*Tensor{x}, func(g *Tensor) []*Tensor {
		return []*Tensor{g.Mul(unaryRaw(uGtS, x, 0, 0))}
	})
}

func (x *Tensor) LeakyRelu(slope float64) *Tensor {
	out := unaryRaw(uLeaky, x, float32(slope), 0)
	return record(out, "leaky_relu", []*Tensor{x}, func(g *Tensor) []*Tensor {
		return []*Tensor{g.Mul(unaryRaw(uLeakyGrad, x, float32(slope), 0))}
	})
}

func (x *Tensor) Sigmoid() *Tensor {
	out := unaryRaw(uSigmoid, x, 0, 0)
	return record(out, "sigmoid", []*Tensor{x}, func(g *Tensor) []*Tensor {
		return []*Tensor{g.Mul(unaryRaw(uSigDeriv, out, 0, 0))}
	})
}

func (x *Tensor) Tanh() *Tensor {
	out := unaryRaw(uTanh, x, 0, 0)
	return record(out, "tanh", []*Tensor{x}, func(g *Tensor) []*Tensor {
		return []*Tensor{g.Mul(unaryRaw(uTanhDeriv, out, 0, 0))}
	})
}

func (x *Tensor) Gelu() *Tensor {
	out := unaryRaw(uGelu, x, 0, 0)
	return record(out, "gelu", []*Tensor{x}, func(g *Tensor) []*Tensor {
		return []*Tensor{g.Mul(unaryRaw(uGeluGrad, x, 0, 0))}
	})
}

func (x *Tensor) Sin() *Tensor {
	out := unaryRaw(uSin, x, 0, 0)
	return record(out, "sin", []*Tensor{x}, func(g *Tensor) []*Tensor {
		return []*Tensor{g.Mul(unaryRaw(uCos, x, 0, 0))}
	})
}

func (x *Tensor) Cos() *Tensor {
	out := unaryRaw(uCos, x, 0, 0)
	return record(out, "cos", []*Tensor{x}, func(g *Tensor) []*Tensor {
		return []*Tensor{g.Mul(unaryRaw(uSin, x, 0, 0)).Neg()}
	})
}

// Clamp 把元素限制在 [lo, hi] 中（超出范围的元素梯度为 0）。
func (x *Tensor) Clamp(lo, hi float64) *Tensor {
	out := unaryRaw(uClamp, x, float32(lo), float32(hi))
	return record(out, "clamp", []*Tensor{x}, func(g *Tensor) []*Tensor {
		inside := unaryRaw(uLtS, x, float32(lo), 0).Add(unaryRaw(uGtS, x, float32(hi), 0)).RSubScalar(1)
		return []*Tensor{g.Mul(inside)}
	})
}

func (x *Tensor) Square() *Tensor { return x.Mul(x) }

// ===== 归约 =====

// sumDim 沿第 dim 维求和。
func (x *Tensor) sumDim(dim int, keep bool) *Tensor {
	dim = normDim(dim, len(x.shape), "sum ")
	outer, d, inner := splitDim(x.shape, dim)
	outShape := reducedShape(x.shape, dim, keep)
	out := newTensor(x.dev(), outShape)
	x.dev().reduce(rSum, x.st, out.st, nil, outer, d, inner)
	return record(out, "sum", []*Tensor{x}, func(g *Tensor) []*Tensor {
		return []*Tensor{expandTo(g.view(reducedShape(x.shape, dim, true)), x.shape)}
	})
}

func splitDim(shape []int, dim int) (outer, d, inner int) {
	return numel(shape[:dim]), shape[dim], numel(shape[dim+1:])
}

func reducedShape(shape []int, dim int, keep bool) []int {
	out := append([]int(nil), shape[:dim]...)
	if keep {
		out = append(out, 1)
	}
	return append(out, shape[dim+1:]...)
}

// sumAll 对所有元素求和，结果是零维张量。
func (x *Tensor) sumAll() *Tensor {
	out := newTensor(x.dev(), nil)
	x.dev().reduce(rSum, x.st, out.st, nil, 1, x.n, 1)
	return record(out, "sum", []*Tensor{x}, func(g *Tensor) []*Tensor {
		return []*Tensor{expandTo(g, x.shape)}
	})
}

// Sum 实现 x.sum(dim=null, keepdim=false)。
func (x *Tensor) Sum(dim rt.Opt[int], keep bool) *Tensor {
	if !dim.Ok {
		return x.sumAll()
	}
	return x.sumDim(dim.V, keep)
}

func (x *Tensor) Mean(dim rt.Opt[int], keep bool) *Tensor {
	if !dim.Ok {
		if x.n == 0 {
			rt.Raise("ValueError", "不能对空张量求平均值")
		}
		return x.sumAll().MulScalar(1 / float64(x.n))
	}
	d := normDim(dim.V, len(x.shape), "mean ")
	return x.sumDim(d, keep).MulScalar(1 / float64(x.shape[d]))
}

// Var 实现方差（unbiased 为真时除以 n-1）。
func (x *Tensor) Var(dim rt.Opt[int], keep bool, unbiased bool) *Tensor {
	mu := x.Mean(dim, true)
	sq := x.Sub(mu).Square()
	n := x.n
	if dim.Ok {
		n = x.shape[normDim(dim.V, len(x.shape), "var ")]
	}
	s := sq.Sum(dim, keep)
	if unbiased && n > 1 {
		return s.MulScalar(1 / float64(n-1))
	}
	return s.MulScalar(1 / float64(n))
}

func (x *Tensor) Std(dim rt.Opt[int], keep bool, unbiased bool) *Tensor {
	return x.Var(dim, keep, unbiased).Sqrt()
}

func (x *Tensor) maxMin(op int, dim rt.Opt[int], keep bool) *Tensor {
	if x.n == 0 {
		rt.Raise("ValueError", "不能对空张量求最大/最小值")
	}
	var d, outer, size, inner int
	var outShape []int
	if dim.Ok {
		d = normDim(dim.V, len(x.shape), "max ")
		outer, size, inner = splitDim(x.shape, d)
		outShape = reducedShape(x.shape, d, keep)
	} else {
		outer, size, inner = 1, x.n, 1
	}
	out := newTensor(x.dev(), outShape)
	x.dev().reduce(op, x.st, out.st, nil, outer, size, inner)
	return record(out, "max", []*Tensor{x}, func(g *Tensor) []*Tensor {
		keepShape := make([]int, len(x.shape))
		for i := range keepShape {
			keepShape[i] = 1
		}
		if dim.Ok {
			keepShape = reducedShape(x.shape, d, true)
		}
		mask := x.Eq(out.view(keepShape))
		return []*Tensor{mask.Mul(expandTo(g.view(keepShape), x.shape))}
	})
}

func (x *Tensor) Max(dim rt.Opt[int], keep bool) *Tensor { return x.maxMin(rMax, dim, keep) }
func (x *Tensor) Min(dim rt.Opt[int], keep bool) *Tensor { return x.maxMin(rMin, dim, keep) }

// Argmax 返回最大值的位置（作为浮点数存储，不参与求导）。
func (x *Tensor) Argmax(dim rt.Opt[int], keep bool) *Tensor { return x.argMaxMin(rMax, dim, keep) }
func (x *Tensor) Argmin(dim rt.Opt[int], keep bool) *Tensor { return x.argMaxMin(rMin, dim, keep) }

func (x *Tensor) argMaxMin(op int, dim rt.Opt[int], keep bool) *Tensor {
	if x.n == 0 {
		rt.Raise("ValueError", "不能对空张量求 argmax")
	}
	outer, size, inner := 1, x.n, 1
	var outShape []int
	if dim.Ok {
		d := normDim(dim.V, len(x.shape), "argmax ")
		outer, size, inner = splitDim(x.shape, d)
		outShape = reducedShape(x.shape, d, keep)
	}
	vals := newTensor(x.dev(), outShape)
	idx := newTensor(x.dev(), outShape)
	x.dev().reduce(op, x.st, vals.st, idx.st, outer, size, inner)
	return idx
}

// sumTo 把（广播后的）梯度按被广播的维求和，得到形状为 shape 的梯度。
func sumTo(g *Tensor, shape []int) *Tensor {
	if sameShape(g.shape, shape) {
		return g
	}
	cur := g
	for len(cur.shape) > len(shape) {
		cur = cur.sumDim(0, false)
	}
	for i, d := range shape {
		if d == 1 && cur.shape[i] != 1 {
			cur = cur.sumDim(i, true)
		}
	}
	if !sameShape(cur.shape, shape) {
		cur = cur.view(shape)
	}
	return cur
}

var (
	zeroMu      sync.Mutex
	zeroScalars = map[backend]*storage{}
)

// zeroScalar 返回设备上一个值为 0 的标量（广播时作为另一个操作数）。
func zeroScalar(dev backend) *storage {
	zeroMu.Lock()
	defer zeroMu.Unlock()
	z := zeroScalars[dev]
	if z == nil {
		z = dev.upload([]float32{0})
		zeroScalars[dev] = z
	}
	return z
}

// expandTo 把 g 广播为形状 shape（复制数据）。
func expandTo(g *Tensor, shape []int) *Tensor {
	if sameShape(g.shape, shape) {
		return g
	}
	dev := g.dev()
	z := zeroScalar(dev)
	out, bc := broadcastShapes("扩展", g.shape, shape)
	for i := range bc.sy {
		bc.sy[i] = 0
	}
	t := newTensor(dev, out)
	dev.binary(bAdd, g.st, z, t.st, t.n, bc)
	return record(t, "expand", []*Tensor{g}, func(gg *Tensor) []*Tensor { return []*Tensor{sumTo(gg, g.shape)} })
}

// Expand 实现 x.expand(shape)。
func (x *Tensor) Expand(shape *rt.List[int]) *Tensor { return expandTo(x, shapeOf(shape)) }

// ===== 形状 =====

// Reshape 改变形状（与原张量共享数据）；形状中可以有一个 -1，由元素个数推出。
func (x *Tensor) Reshape(shape *rt.List[int]) *Tensor { return x.reshape(shapeOf(shape)) }

func (x *Tensor) reshape(shape []int) *Tensor {
	shape = append([]int(nil), shape...)
	infer := -1
	known := 1
	for i, d := range shape {
		if d == -1 {
			if infer >= 0 {
				raiseShape("reshape 的形状中最多只能有一个 -1")
			}
			infer = i
		} else {
			known *= d
		}
	}
	if infer >= 0 {
		if known == 0 || x.n%known != 0 {
			raiseShape("不能把 %d 个元素整理为形状 %s", x.n, shapeStr(shape))
		}
		shape[infer] = x.n / known
	}
	if numel(shape) != x.n {
		raiseShape("不能把形状 %s（%d 个元素）整理为 %s", shapeStr(x.shape), x.n, shapeStr(shape))
	}
	out := x.view(shape)
	return record(out, "reshape", []*Tensor{x}, func(g *Tensor) []*Tensor { return []*Tensor{g.view(x.shape)} })
}

// Flatten 把第 start 到第 end 维合并为一维。
func (x *Tensor) Flatten(start, end int) *Tensor {
	if len(x.shape) == 0 {
		return x.reshape([]int{1})
	}
	s := normDim(start, len(x.shape), "flatten ")
	e := normDim(end, len(x.shape), "flatten ")
	shape := append([]int(nil), x.shape[:s]...)
	shape = append(shape, numel(x.shape[s:e+1]))
	shape = append(shape, x.shape[e+1:]...)
	return x.reshape(shape)
}

func (x *Tensor) Unsqueeze(dim int) *Tensor {
	d := normDim(dim, len(x.shape)+1, "unsqueeze ")
	shape := append([]int(nil), x.shape[:d]...)
	shape = append(shape, 1)
	shape = append(shape, x.shape[d:]...)
	return x.reshape(shape)
}

// Squeeze 去掉长度为 1 的维（给出 dim 时只去掉这一维）。
func (x *Tensor) Squeeze(dim rt.Opt[int]) *Tensor {
	var shape []int
	if dim.Ok {
		d := normDim(dim.V, len(x.shape), "squeeze ")
		if x.shape[d] != 1 {
			return x
		}
		shape = append(append([]int(nil), x.shape[:d]...), x.shape[d+1:]...)
	} else {
		for _, s := range x.shape {
			if s != 1 {
				shape = append(shape, s)
			}
		}
	}
	return x.reshape(shape)
}

// Permute 重新排列各维（复制数据）。
func (x *Tensor) Permute(dims *rt.List[int]) *Tensor { return x.permute(dims.S) }

func (x *Tensor) permute(dims []int) *Tensor {
	nd := len(x.shape)
	if len(dims) != nd {
		raiseShape("permute 需要 %d 个维度，得到 %d 个", nd, len(dims))
	}
	perm := make([]int, nd)
	used := make([]bool, nd)
	for i, d := range dims {
		d = normDim(d, nd, "permute ")
		if used[d] {
			raiseShape("permute 的维度 %d 重复", d)
		}
		used[d] = true
		perm[i] = d
	}
	identity := true
	for i, p := range perm {
		if p != i {
			identity = false
		}
	}
	if identity {
		return x
	}
	shape := make([]int, nd)
	for i, p := range perm {
		shape[i] = x.shape[p]
	}
	out := newTensor(x.dev(), shape)
	x.dev().permute(x.st, out.st, x.shape, perm)
	return record(out, "permute", []*Tensor{x}, func(g *Tensor) []*Tensor {
		inv := make([]int, nd)
		for i, p := range perm {
			inv[p] = i
		}
		return []*Tensor{g.permute(inv)}
	})
}

func (x *Tensor) Transpose(d0, d1 int) *Tensor {
	nd := len(x.shape)
	a, b := normDim(d0, nd, "transpose "), normDim(d1, nd, "transpose ")
	perm := make([]int, nd)
	for i := range perm {
		perm[i] = i
	}
	perm[a], perm[b] = perm[b], perm[a]
	return x.permute(perm)
}

// T 是二维张量的转置。
func (x *Tensor) T() *Tensor {
	if len(x.shape) != 2 {
		raiseShape(".T 只能用于二维张量，得到形状 %s；多维请使用 transpose 或 permute", shapeStr(x.shape))
	}
	return x.Transpose(0, 1)
}

// Narrow 取出第 dim 维上 [start, start+length) 的部分（复制数据）。
func (x *Tensor) Narrow(dim, start, length int) *Tensor {
	d := normDim(dim, len(x.shape), "narrow ")
	size := x.shape[d]
	if start < 0 {
		start += size
	}
	if start < 0 || length < 0 || start+length > size {
		rt.Raise("IndexError", "narrow：第 %d 维的范围 [%d, %d) 超出了长度 %d", d, start, start+length, size)
	}
	outer, _, inner := splitDim(x.shape, d)
	shape := append([]int(nil), x.shape...)
	shape[d] = length
	out := newTensor(x.dev(), shape)
	x.dev().narrow(x.st, out.st, outer, size, inner, start, length)
	return record(out, "narrow", []*Tensor{x}, func(g *Tensor) []*Tensor {
		gx := full(g.dev(), x.shape, 0)
		g.dev().place(g.st, gx.st, outer, size, inner, start, length)
		return []*Tensor{gx}
	})
}

// Slice 实现 x.slice(dim, start, end)：Python 风格的切片（支持负数，end 可以超出范围）。
func (x *Tensor) Slice(dim, start int, end rt.Opt[int]) *Tensor {
	d := normDim(dim, len(x.shape), "slice ")
	size := x.shape[d]
	e := size
	if end.Ok {
		e = end.V
	}
	if start < 0 {
		start = max(start+size, 0)
	}
	if e < 0 {
		e = max(e+size, 0)
	}
	start, e = min(start, size), min(e, size)
	if e < start {
		e = start
	}
	return x.Narrow(d, start, e-start)
}

// Index 实现 x[i]：取出第 0 维上的第 i 项。
func (x *Tensor) Index(i int) *Tensor {
	if len(x.shape) == 0 {
		rt.Raise("IndexError", "零维张量不能用下标访问")
	}
	n := x.shape[0]
	if i < 0 {
		i += n
	}
	if i < 0 || i >= n {
		raiseIndex(i, n)
	}
	return x.Narrow(0, i, 1).reshape(x.shape[1:])
}

// Cat 沿第 dim 维拼接张量。
func Cat(ts *rt.List[*Tensor], dim int) *Tensor { return cat(ts.S, dim) }

func cat(ts []*Tensor, dim int) *Tensor {
	if len(ts) == 0 {
		rt.Raise("ValueError", "cat 至少需要一个张量")
	}
	first := ts[0]
	nd := len(first.shape)
	d := normDim(dim, nd, "cat ")
	sameDevice("cat", ts...)
	total := 0
	for i, t := range ts {
		if len(t.shape) != nd {
			raiseShape("cat：第 %d 个张量有 %d 维，第 0 个有 %d 维", i, len(t.shape), nd)
		}
		for k := range t.shape {
			if k != d && t.shape[k] != first.shape[k] {
				raiseShape("cat：第 %d 个张量的形状 %s 与第 0 个的 %s 在第 %d 维上不同", i, shapeStr(t.shape), shapeStr(first.shape), k)
			}
		}
		total += t.shape[d]
	}
	shape := append([]int(nil), first.shape...)
	shape[d] = total
	out := newTensor(first.dev(), shape)
	outer, _, inner := splitDim(shape, d)
	off := 0
	starts := make([]int, len(ts))
	for i, t := range ts {
		starts[i] = off
		out.dev().place(t.st, out.st, outer, total, inner, off, t.shape[d])
		off += t.shape[d]
	}
	return record(out, "cat", ts, func(g *Tensor) []*Tensor {
		gs := make([]*Tensor, len(ts))
		for i, t := range ts {
			if t.requiresGrad {
				gs[i] = g.Narrow(d, starts[i], t.shape[d])
			}
		}
		return gs
	})
}

// Stack 把形状相同的张量沿新的第 dim 维堆叠。
func Stack(ts *rt.List[*Tensor], dim int) *Tensor {
	if len(ts.S) == 0 {
		rt.Raise("ValueError", "stack 至少需要一个张量")
	}
	d := normDim(dim, len(ts.S[0].shape)+1, "stack ")
	parts := make([]*Tensor, len(ts.S))
	for i, t := range ts.S {
		if !sameShape(t.shape, ts.S[0].shape) {
			raiseShape("stack：第 %d 个张量的形状 %s 与第 0 个的 %s 不同", i, shapeStr(t.shape), shapeStr(ts.S[0].shape))
		}
		parts[i] = t.Unsqueeze(d)
	}
	return cat(parts, d)
}

// ===== 矩阵乘法 =====

// mm 是带转置与批次步长的矩阵乘法（不记录求导）。
func mm(a, b *Tensor, ta, tb bool, batch, m, k, n, sa, sb int) *Tensor {
	dev := sameDevice("矩阵乘法", a, b)
	var shape []int
	if batch > 1 || sa != 0 || sb != 0 {
		shape = []int{batch, m, n}
	} else {
		shape = []int{m, n}
	}
	out := newTensor(dev, shape)
	dev.matmul(a.st, b.st, out.st, batch, m, k, n, ta, tb, sa, sb)
	return out
}

// Matmul 实现 a @ b：支持一维（向量）、二维（矩阵）和带批次的三维及以上张量。
func (a *Tensor) Matmul(b *Tensor) *Tensor {
	na, nb := len(a.shape), len(b.shape)
	switch {
	case na == 0 || nb == 0:
		raiseShape("矩阵乘法不能用于零维张量；标量请使用 *")
	case na == 1 && nb == 1:
		if a.n != b.n {
			raiseShape("向量点积的长度不同：%d 和 %d", a.n, b.n)
		}
		return a.Mul(b).sumAll()
	case na == 1:
		return a.Unsqueeze(0).Matmul(b).Squeeze(rt.Opt[int]{V: -2, Ok: true})
	case nb == 1:
		return a.Matmul(b.Unsqueeze(1)).Squeeze(rt.Opt[int]{V: -1, Ok: true})
	}
	m, k := a.shape[na-2], a.shape[na-1]
	k2, n := b.shape[nb-2], b.shape[nb-1]
	if k != k2 {
		raiseShape("矩阵乘法的形状不匹配：%s @ %s（%d ≠ %d）", shapeStr(a.shape), shapeStr(b.shape), k, k2)
	}
	switch {
	case na == 2 && nb == 2:
		out := mm(a, b, false, false, 1, m, k, n, 0, 0)
		return record(out, "matmul", []*Tensor{a, b}, func(g *Tensor) []*Tensor {
			var ga, gb *Tensor
			if a.requiresGrad {
				ga = mm(g, b, false, true, 1, m, n, k, 0, 0)
			}
			if b.requiresGrad {
				gb = mm(a, g, true, false, 1, k, m, n, 0, 0)
			}
			return []*Tensor{ga, gb}
		})
	case nb == 2:
		// [..., m, k] @ [k, n]：合并批次维后是一次二维乘法
		batchShape := a.shape[:na-2]
		flat := a.reshape([]int{numel(batchShape) * m, k}).Matmul(b)
		return flat.reshape(append(append([]int(nil), batchShape...), m, n))
	case na == 2:
		// [m, k] @ [B..., k, n]
		batchShape := b.shape[:nb-2]
		B := numel(batchShape)
		b3 := b.reshape([]int{B, k, n})
		out := mm(a, b3, false, false, B, m, k, n, 0, k*n)
		out = record(out, "matmul", []*Tensor{a, b3}, func(g *Tensor) []*Tensor {
			var ga, gb *Tensor
			if a.requiresGrad {
				ga = mm(g, b3, false, true, B, m, n, k, m*n, k*n).sumDim(0, false)
			}
			if b3.requiresGrad {
				gb = mm(a, g, true, false, B, k, m, n, 0, m*n)
			}
			return []*Tensor{ga, gb}
		})
		return out.reshape(append(append([]int(nil), batchShape...), m, n))
	}
	// 都有批次维：要求批次维相同
	ba, bb := a.shape[:na-2], b.shape[:nb-2]
	if !sameShape(ba, bb) {
		raiseShape("批量矩阵乘法要求批次维相同：%s @ %s", shapeStr(a.shape), shapeStr(b.shape))
	}
	B := numel(ba)
	a3, b3 := a.reshape([]int{B, m, k}), b.reshape([]int{B, k, n})
	out := mm(a3, b3, false, false, B, m, k, n, m*k, k*n)
	out = record(out, "bmm", []*Tensor{a3, b3}, func(g *Tensor) []*Tensor {
		var ga, gb *Tensor
		if a3.requiresGrad {
			ga = mm(g, b3, false, true, B, m, n, k, m*n, k*n)
		}
		if b3.requiresGrad {
			gb = mm(a3, g, true, false, B, k, m, n, m*k, m*n)
		}
		return []*Tensor{ga, gb}
	})
	return out.reshape(append(append([]int(nil), ba...), m, n))
}

// ===== softmax 与损失 =====

func (x *Tensor) softmaxLast(log bool) *Tensor {
	if len(x.shape) == 0 {
		raiseShape("softmax 不能用于零维张量")
	}
	cols := x.shape[len(x.shape)-1]
	rows := x.n / max(cols, 1)
	out := newTensor(x.dev(), x.shape)
	x.dev().softmax(x.st, out.st, rows, cols, log)
	last := rt.Opt[int]{V: -1, Ok: true}
	if log {
		return record(out, "log_softmax", []*Tensor{x}, func(g *Tensor) []*Tensor {
			return []*Tensor{g.Sub(out.Exp().Mul(g.Sum(last, true)))}
		})
	}
	return record(out, "softmax", []*Tensor{x}, func(g *Tensor) []*Tensor {
		return []*Tensor{out.Mul(g.Sub(g.Mul(out).Sum(last, true)))}
	})
}

func (x *Tensor) softmaxDim(dim int, log bool) *Tensor {
	d := normDim(dim, len(x.shape), "softmax ")
	if d == len(x.shape)-1 {
		return x.softmaxLast(log)
	}
	return x.Transpose(d, -1).softmaxLast(log).Transpose(d, -1)
}

func (x *Tensor) Softmax(dim int) *Tensor    { return x.softmaxDim(dim, false) }
func (x *Tensor) LogSoftmax(dim int) *Tensor { return x.softmaxDim(dim, true) }

// indexOn 返回位于设备 dev 上的下标张量（下标通常很小，不在同一设备时直接复制过去）。
func indexOn(idx *Tensor, dev backend) *Tensor {
	if idx.dev() == dev {
		return idx
	}
	return &Tensor{shape: idx.shape, n: idx.n, st: dev.upload(idx.host())}
}

// pick 取出每一行中 idx 指定的列：out[i] = x[i, idx[i]]。
func pick(x, idx *Tensor) *Tensor {
	if len(x.shape) != 2 {
		raiseShape("pick 需要二维张量，得到形状 %s", shapeStr(x.shape))
	}
	rows, cols := x.shape[0], x.shape[1]
	if idx.n != rows {
		raiseShape("类别标签有 %d 个，但有 %d 行预测", idx.n, rows)
	}
	idx = indexOn(idx, x.dev())
	out := newTensor(x.dev(), []int{rows})
	x.dev().pick(x.st, idx.st, out.st, rows, cols)
	return record(out, "pick", []*Tensor{x}, func(g *Tensor) []*Tensor {
		gx := full(g.dev(), x.shape, 0)
		g.dev().pickBack(g.st, idx.st, gx.st, rows, cols)
		return []*Tensor{gx}
	})
}

// CrossEntropy 实现交叉熵损失：logits 为 [N, C]，target 为 N 个类别下标（取值 0 到 C-1）。
func CrossEntropy(logits, target *Tensor, reduction string) *Tensor {
	if len(logits.shape) != 2 {
		raiseShape("cross_entropy 的输入应为 [样本数, 类别数]，得到形状 %s", shapeStr(logits.shape))
	}
	if target.n != logits.shape[0] {
		raiseShape("cross_entropy：有 %d 个样本，但有 %d 个标签", logits.shape[0], target.n)
	}
	nll := pick(logits.LogSoftmax(-1), target).Neg()
	return reduce(nll, reduction)
}

// NLLLoss 是负对数似然损失：logProbs 为 [N, C]（通常是 log_softmax 的结果），target 为类别下标。
func NLLLoss(logProbs, target *Tensor, reduction string) *Tensor {
	if len(logProbs.shape) != 2 {
		raiseShape("nll_loss 的输入应为 [样本数, 类别数]，得到形状 %s", shapeStr(logProbs.shape))
	}
	return reduce(pick(logProbs, target).Neg(), reduction)
}

func reduce(x *Tensor, reduction string) *Tensor {
	switch reduction {
	case "mean":
		return x.Mean(rt.Opt[int]{}, false)
	case "sum":
		return x.sumAll()
	case "none":
		return x
	}
	rt.Raise("ValueError", "reduction 应为 \"mean\"、\"sum\" 或 \"none\"，得到 %s", rt.QuoteStr(reduction))
	return nil
}

func MSELoss(pred, target *Tensor, reduction string) *Tensor {
	return reduce(pred.Sub(target).Square(), reduction)
}

func L1Loss(pred, target *Tensor, reduction string) *Tensor {
	return reduce(pred.Sub(target).Abs(), reduction)
}

// BCEWithLogits 是带 sigmoid 的二元交叉熵（数值稳定的写法）。
func BCEWithLogits(logits, target *Tensor, reduction string) *Tensor {
	// max(x, 0) - x * t + log(1 + exp(-|x|))
	loss := logits.Relu().Sub(logits.Mul(target)).Add(logits.Abs().Neg().Exp().AddScalar(1).Log())
	return reduce(loss, reduction)
}

// BCE 是二元交叉熵：pred 是 (0, 1) 中的概率。
func BCE(pred, target *Tensor, reduction string) *Tensor {
	p := pred.Clamp(1e-7, 1-1e-7)
	loss := target.Mul(p.Log()).Add(target.RSubScalar(1).Mul(p.RSubScalar(1).Log())).Neg()
	return reduce(loss, reduction)
}

// ===== 嵌入、卷积、池化、Dropout =====

// Embedding 按下标取出 weight [V, D] 的行；idx 可以是任意形状，结果的形状是 idx 的形状加上 [D]。
func Embedding(weight, idx *Tensor) *Tensor {
	if len(weight.shape) != 2 {
		raiseShape("embedding 的权重应为 [词表大小, 维数]，得到形状 %s", shapeStr(weight.shape))
	}
	idx = indexOn(idx, weight.dev())
	rows, cols := weight.shape[0], weight.shape[1]
	out := newTensor(weight.dev(), append(append([]int(nil), idx.shape...), cols))
	weight.dev().indexSelect(weight.st, idx.st, out.st, idx.n, rows, cols)
	return record(out, "embedding", []*Tensor{weight}, func(g *Tensor) []*Tensor {
		gw := full(g.dev(), weight.shape, 0)
		g.dev().indexAdd(g.st, idx.st, gw.st, idx.n, rows, cols)
		return []*Tensor{gw}
	})
}

// IndexSelectDim 实现 x.index_select(dim, index)：取出第 dim 维上 index 指定的位置。
func (x *Tensor) IndexSelectDim(dim int, idx *Tensor) *Tensor {
	d := normDim(dim, len(x.shape), "index_select ")
	if d == 0 {
		return x.IndexSelect(idx)
	}
	return x.Transpose(0, d).IndexSelect(idx).Transpose(0, d)
}

// IndexSelect 取出第 0 维上 idx 指定的行（用于按下标取一批样本）。
func (x *Tensor) IndexSelect(idx *Tensor) *Tensor {
	if len(x.shape) == 0 {
		raiseShape("index_select 不能用于零维张量")
	}
	rows := x.shape[0]
	cols := x.n / max(rows, 1)
	flat := x.reshape([]int{rows, cols})
	out := Embedding(flat, idx.reshape([]int{idx.n}))
	return out.reshape(append([]int{idx.n}, x.shape[1:]...))
}

func convOut(in, k, s, p int) int { return (in+2*p-k)/s + 1 }

func im2col(x *Tensor, p *convParams) *Tensor {
	cols := newTensor(x.dev(), []int{p.n, p.colRows(), p.colCols()})
	x.dev().im2col(x.st, cols.st, p)
	return record(cols, "im2col", []*Tensor{x}, func(g *Tensor) []*Tensor {
		gx := full(g.dev(), x.shape, 0)
		g.dev().col2im(g.st, gx.st, p)
		return []*Tensor{gx}
	})
}

// Conv2d 实现二维卷积：x [N, C, H, W]，weight [O, C, KH, KW]，bias [O] 或 null。
func Conv2d(x, weight *Tensor, bias rt.Opt[*Tensor], stride, padding int) *Tensor {
	if len(x.shape) != 4 || len(weight.shape) != 4 {
		raiseShape("conv2d 的输入应为 [N, C, H, W]、权重为 [O, C, KH, KW]，得到 %s 与 %s", shapeStr(x.shape), shapeStr(weight.shape))
	}
	if x.shape[1] != weight.shape[1] {
		raiseShape("conv2d：输入有 %d 个通道，但卷积核需要 %d 个", x.shape[1], weight.shape[1])
	}
	if stride < 1 || padding < 0 {
		rt.Raise("ValueError", "conv2d 的 stride 必须至少为 1，padding 不能为负数")
	}
	p := &convParams{n: x.shape[0], c: x.shape[1], h: x.shape[2], w: x.shape[3], kh: weight.shape[2], kw: weight.shape[3], sh: stride, sw: stride, ph: padding, pw: padding}
	p.oh, p.ow = convOut(p.h, p.kh, stride, padding), convOut(p.w, p.kw, stride, padding)
	if p.oh <= 0 || p.ow <= 0 {
		raiseShape("conv2d：输入 %dx%d 小于卷积核 %dx%d", p.h, p.w, p.kh, p.kw)
	}
	o := weight.shape[0]
	cols := im2col(x, p)
	out := weight.reshape([]int{o, p.colRows()}).Matmul(cols) // [N, O, OH*OW]
	out = out.reshape([]int{p.n, o, p.oh, p.ow})
	if bias.Ok {
		out = out.Add(bias.V.reshape([]int{1, o, 1, 1}))
	}
	return out
}

// MaxPool2d 实现二维最大池化。
func MaxPool2d(x *Tensor, kernel, stride, padding int) *Tensor {
	if len(x.shape) != 4 {
		raiseShape("max_pool2d 的输入应为 [N, C, H, W]，得到 %s", shapeStr(x.shape))
	}
	if stride <= 0 {
		stride = kernel
	}
	p := &convParams{n: x.shape[0], c: x.shape[1], h: x.shape[2], w: x.shape[3], kh: kernel, kw: kernel, sh: stride, sw: stride, ph: padding, pw: padding}
	p.oh, p.ow = convOut(p.h, kernel, stride, padding), convOut(p.w, kernel, stride, padding)
	if p.oh <= 0 || p.ow <= 0 {
		raiseShape("max_pool2d：输入 %dx%d 小于池化窗口 %d", p.h, p.w, kernel)
	}
	out := newTensor(x.dev(), []int{p.n, p.c, p.oh, p.ow})
	idx := newTensor(x.dev(), out.shape)
	x.dev().maxpool(x.st, out.st, idx.st, p)
	return record(out, "max_pool2d", []*Tensor{x}, func(g *Tensor) []*Tensor {
		gx := full(g.dev(), x.shape, 0)
		g.dev().maxpoolBack(g.st, idx.st, gx.st, p)
		return []*Tensor{gx}
	})
}

// AvgPool2d 用卷积实现平均池化（每个通道单独计算）。
func AvgPool2d(x *Tensor, kernel, stride int) *Tensor {
	if len(x.shape) != 4 {
		raiseShape("avg_pool2d 的输入应为 [N, C, H, W]，得到 %s", shapeStr(x.shape))
	}
	if stride <= 0 {
		stride = kernel
	}
	n, c, h, w := x.shape[0], x.shape[1], x.shape[2], x.shape[3]
	k := full(x.dev(), []int{1, 1, kernel, kernel}, float32(1/float64(kernel*kernel)))
	y := Conv2d(x.reshape([]int{n * c, 1, h, w}), k, rt.Opt[*Tensor]{}, stride, 0)
	return y.reshape([]int{n, c, y.shape[2], y.shape[3]})
}

// Dropout 在训练时以概率 p 把元素置零，并把其余元素放大 1/(1-p)。
func Dropout(x *Tensor, p float64, training bool) *Tensor {
	if !training || p == 0 {
		return x
	}
	if p < 0 || p >= 1 {
		rt.Raise("ValueError", "dropout 的概率必须在 [0, 1) 中，得到 %v", p)
	}
	// 一次融合的运算；反向时用同一个种子重新生成同样的随机数，不需要保存掩码
	seed, scale := nextSeed(), float32(1/(1-p))
	dev := x.dev()
	out := newTensor(dev, x.shape)
	dev.dropout(x.st, out.st, x.n, seed, float32(p), scale)
	return record(out, "dropout", []*Tensor{x}, func(g *Tensor) []*Tensor {
		gx := newTensor(dev, x.shape)
		dev.dropout(g.st, gx.st, g.n, seed, float32(p), scale)
		return []*Tensor{gx}
	})
}

// LayerNorm 对最后一维做层归一化。
// 前向与反向各是一个融合的运算（每行一次），而不是十几个基本运算。
func LayerNorm(x, weight, bias *Tensor, eps float64) *Tensor {
	if len(x.shape) == 0 {
		raiseShape("layer_norm 不能用于零维张量")
	}
	d := x.shape[len(x.shape)-1]
	if weight.n != d || bias.n != d {
		raiseShape("layer_norm：输入的最后一维是 %d，但 weight、bias 的形状是 %s、%s", d, shapeStr(weight.shape), shapeStr(bias.shape))
	}
	dev := sameDevice("layer_norm", x, weight, bias)
	rows := x.n / max(d, 1)
	out, xhat, rstd := newTensor(dev, x.shape), newTensor(dev, x.shape), newTensor(dev, []int{rows})
	dev.layerNorm(x.st, weight.st, bias.st, out.st, xhat.st, rstd.st, rows, d, float32(eps))
	return record(out, "layer_norm", []*Tensor{x, weight, bias}, func(g *Tensor) []*Tensor {
		var gx, gw, gb *Tensor
		if x.requiresGrad {
			gx = newTensor(dev, x.shape)
			dev.layerNormBack(g.st, weight.st, xhat.st, rstd.st, gx.st, rows, d)
		}
		g2 := g.view([]int{rows, d})
		if weight.requiresGrad {
			gw = g2.Mul(xhat.view([]int{rows, d})).sumDim(0, false).view(weight.shape)
		}
		if bias.requiresGrad {
			gb = g2.sumDim(0, false).view(bias.shape)
		}
		return []*Tensor{gx, gw, gb}
	})
}

// ===== 随机数 =====

var (
	seedBase    atomic.Uint64
	seedCounter atomic.Uint64
)

func init() { seedBase.Store(0x5DEECE66D) }

// ManualSeed 设置随机数种子（之后的随机结果可以重现；CPU 与 GPU 上生成的随机数相同）。
func ManualSeed(n int) {
	seedBase.Store(uint64(n)*0x9E3779B97F4A7C15 + 0x5DEECE66D)
	seedCounter.Store(0)
}

func nextSeed() uint64 {
	x := seedBase.Load() + seedCounter.Add(1)*0xBF58476D1CE4E5B9
	x ^= x >> 31
	x *= 0x94D049BB133111EB
	x ^= x >> 29
	return x
}

func Randn(shape *rt.List[int]) *Tensor {
	t := newTensor(defaultDev, shapeOf(shape))
	t.dev().random(t.st, t.n, nextSeed(), true)
	return t
}

func Rand(shape *rt.List[int]) *Tensor {
	t := newTensor(defaultDev, shapeOf(shape))
	t.dev().random(t.st, t.n, nextSeed(), false)
	return t
}

// Uniform 返回 [lo, hi) 中均匀分布的随机张量。
func Uniform(shape *rt.List[int], lo, hi float64) *Tensor {
	r := Rand(shape)
	return r.MulScalar(hi - lo).AddScalar(lo)
}

// Randint 返回 [low, high) 中均匀分布的随机整数（以浮点数存储）。
func Randint(low, high int, shape *rt.List[int]) *Tensor {
	if high <= low {
		rt.Raise("ValueError", "randint 需要 low < high，得到 %d 和 %d", low, high)
	}
	sh := shapeOf(shape)
	checkShape(sh)
	n := numel(sh)
	tmp := cpuDev.alloc(n)
	cpuDev.random(tmp, n, nextSeed(), false)
	d := tmp.cpu
	span := float64(high - low)
	for i, u := range d {
		d[i] = float32(low + min(int(float64(u)*span), high-low-1))
	}
	return fromHost(defaultDev, sh, d)
}

// Randperm 返回 0 到 n-1 的随机排列（作为浮点数张量，用于打乱样本）。
func Randperm(n int) *Tensor {
	keys := make([]float32, n)
	tmp := cpuDev.alloc(n)
	cpuDev.random(tmp, n, nextSeed(), false)
	copy(keys, tmp.cpu)
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	// 按随机键排序得到排列
	sortByKey(idx, keys)
	d := make([]float32, n)
	for i, v := range idx {
		d[i] = float32(v)
	}
	return fromHost(defaultDev, []int{n}, d)
}

func sortByKey(idx []int, keys []float32) {
	// 简单的归并排序，避免依赖 sort 包的反射
	if len(idx) < 2 {
		return
	}
	buf := make([]int, len(idx))
	var rec func(lo, hi int)
	rec = func(lo, hi int) {
		if hi-lo < 2 {
			return
		}
		mid := (lo + hi) / 2
		rec(lo, mid)
		rec(mid, hi)
		i, j, k := lo, mid, lo
		for i < mid && j < hi {
			if keys[idx[i]] <= keys[idx[j]] {
				buf[k] = idx[i]
				i++
			} else {
				buf[k] = idx[j]
				j++
			}
			k++
		}
		for ; i < mid; i++ {
			buf[k] = idx[i]
			k++
		}
		for ; j < hi; j++ {
			buf[k] = idx[j]
			k++
		}
		copy(idx[lo:hi], buf[lo:hi])
	}
	rec(0, len(idx))
}

// Clone 复制数据（保留求导关系）。
func (x *Tensor) Clone() *Tensor {
	out := newTensor(x.dev(), x.shape)
	x.dev().copyTo(out.st, x.st, x.n)
	return record(out, "clone", []*Tensor{x}, func(g *Tensor) []*Tensor { return []*Tensor{g} })
}

// Where 返回 cond 为 1 处取 a、否则取 b 的张量（cond 是 0/1 张量）。
func Where(cond, a, b *Tensor) *Tensor {
	return cond.Mul(a).Add(cond.RSubScalar(1).Mul(b))
}

// Allclose 判断两个张量的形状相同且所有元素都足够接近：|a - b| <= atol + rtol * |b|。
func (a *Tensor) Allclose(b *Tensor, rtol, atol float64) bool {
	if !sameShape(a.shape, b.shape) {
		return false
	}
	x, y := a.host(), b.host()
	for i := range x {
		if math.Abs(float64(x[i]-y[i])) > atol+rtol*math.Abs(float64(y[i])) {
			return false
		}
	}
	return true
}
