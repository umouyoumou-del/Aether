package rttensor

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"aether/assets/rt"
)

func ints(xs ...int) *rt.List[int] { return &rt.List[int]{S: xs} }
func some(i int) rt.Opt[int]       { return rt.Opt[int]{V: i, Ok: true} }

var none = rt.Opt[int]{}

// randT 返回 CPU 上 [lo, hi) 中的随机张量。
func randT(seed uint64, lo, hi float32, shape ...int) *Tensor {
	t := newTensor(cpuDev, shape)
	cpuDev.random(t.st, t.n, seed*0x9E3779B97F4A7C15+1, false)
	for i := range t.st.cpu {
		t.st.cpu[i] = lo + (hi-lo)*t.st.cpu[i]
	}
	return t
}

// away 返回绝对值至少为 0.1 的随机张量（避开 relu、abs 等在 0 处的折点）。
func away(seed uint64, shape ...int) *Tensor {
	t := randT(seed, -1, 1, shape...)
	for i, v := range t.st.cpu {
		if v >= 0 {
			t.st.cpu[i] = v + 0.1
		} else {
			t.st.cpu[i] = v - 0.1
		}
	}
	return t
}

// distinct 返回元素两两相差至少 0.05 的张量（求最大值时不会因为扰动改变最大值的位置）。
func distinct(seed uint64, shape ...int) *Tensor {
	n := numel(shape)
	keys := randT(seed, 0, 1, n).st.cpu
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sortByKey(idx, keys)
	d := make([]float32, n)
	for rank, i := range idx {
		d[i] = float32(rank)*0.05 - float32(n)*0.025
	}
	return fromHost(cpuDev, shape, d)
}

func constT(shape []int, d ...float32) *Tensor { return fromHost(cpuDev, shape, d) }

type gradCase struct {
	name   string
	inputs []*Tensor // 求导的输入
	consts []*Tensor // 不求导的输入（例如类别标签）
	f      func(x, c []*Tensor) *Tensor
}

func gradCases() []gradCase {
	r := func(seed uint64, shape ...int) *Tensor { return randT(seed, -1, 1, shape...) }
	pos := func(seed uint64, shape ...int) *Tensor { return randT(seed, 0.5, 2, shape...) }
	in := func(ts ...*Tensor) []*Tensor { return ts }
	mmCase := func(name string, a, b []int) gradCase {
		return gradCase{name: name, inputs: in(r(40, a...), r(41, b...)), f: func(x, _ []*Tensor) *Tensor { return x[0].Matmul(x[1]) }}
	}
	// a 与 b 每个位置相差至少 0.1，maximum/minimum 不会因为扰动交换
	ma := r(20, 3, 4)
	mb := ma.Detach().Add(away(21, 3, 4).MulScalar(0.5))
	clampIn := constT([]int{8}, -0.9, -0.6, -0.3, 0.1, 0.3, 0.45, 0.7, 0.95)
	return []gradCase{
		{name: "add 广播", inputs: in(r(1, 3, 4), r(2, 4)), f: func(x, _ []*Tensor) *Tensor { return x[0].Add(x[1]) }},
		{name: "sub 广播", inputs: in(r(3, 2, 1, 3), r(4, 4, 1)), f: func(x, _ []*Tensor) *Tensor { return x[0].Sub(x[1]) }},
		{name: "mul 广播", inputs: in(r(5, 3, 1), r(6, 1, 4)), f: func(x, _ []*Tensor) *Tensor { return x[0].Mul(x[1]) }},
		{name: "div", inputs: in(r(7, 3, 4), pos(8, 3, 4)), f: func(x, _ []*Tensor) *Tensor { return x[0].Div(x[1]) }},
		{name: "div 广播", inputs: in(r(7, 2, 3, 4), pos(8, 3, 1)), f: func(x, _ []*Tensor) *Tensor { return x[0].Div(x[1]) }},
		{name: "pow", inputs: in(pos(9, 3, 4), r(10, 3, 4)), f: func(x, _ []*Tensor) *Tensor { return x[0].Pow(x[1]) }},
		{name: "maximum", inputs: in(ma, mb), f: func(x, _ []*Tensor) *Tensor { return x[0].Maximum(x[1]) }},
		{name: "minimum", inputs: in(ma.Clone(), mb.Clone()), f: func(x, _ []*Tensor) *Tensor { return x[0].Minimum(x[1]) }},
		{name: "标量运算", inputs: in(pos(11, 5)), f: func(x, _ []*Tensor) *Tensor {
			return x[0].AddScalar(1).MulScalar(3).RSubScalar(2).Mul(x[0].RDivScalar(2)).Add(x[0].PowScalar(2.5)).Add(x[0].RPowScalar(2)).Add(x[0].DivScalar(4)).Add(x[0].SubScalar(1))
		}},
		{name: "neg exp", inputs: in(r(12, 6)), f: func(x, _ []*Tensor) *Tensor { return x[0].Neg().Exp() }},
		{name: "log sqrt", inputs: in(pos(13, 6)), f: func(x, _ []*Tensor) *Tensor { return x[0].Log().Add(x[0].Sqrt()) }},
		{name: "abs relu leaky", inputs: in(away(14, 8)), f: func(x, _ []*Tensor) *Tensor {
			return x[0].Abs().Add(x[0].Relu().MulScalar(2)).Add(x[0].LeakyRelu(0.1).MulScalar(3))
		}},
		{name: "sigmoid tanh gelu", inputs: in(r(15, 2, 5)), f: func(x, _ []*Tensor) *Tensor {
			return x[0].MulScalar(2).Sigmoid().Add(x[0].Tanh()).Add(x[0].MulScalar(2).Gelu())
		}},
		{name: "sin cos square", inputs: in(r(16, 7)), f: func(x, _ []*Tensor) *Tensor { return x[0].Sin().Mul(x[0].Cos()).Add(x[0].Square()) }},
		{name: "clamp", inputs: in(clampIn), f: func(x, _ []*Tensor) *Tensor { return x[0].Clamp(-0.5, 0.5).MulScalar(3) }},
		{name: "sum", inputs: in(r(17, 2, 3, 4)), f: func(x, _ []*Tensor) *Tensor {
			return x[0].Sum(some(1), true).Mul(x[0]).Sum(none, false).Add(x[0].Sum(some(-1), false).Square().Sum(none, false))
		}},
		{name: "mean var std", inputs: in(r(18, 3, 5)), f: func(x, _ []*Tensor) *Tensor {
			return x[0].Mean(some(0), false).Add(x[0].Var(some(1), false, true).Sum(none, false)).Add(x[0].Std(none, false, false)).Add(x[0].Mean(none, false))
		}},
		{name: "max min", inputs: in(distinct(19, 3, 4)), f: func(x, _ []*Tensor) *Tensor {
			return x[0].Max(some(1), false).Add(x[0].Min(some(0), true).Sum(none, false)).Add(x[0].Max(none, false))
		}},
		{name: "reshape flatten squeeze", inputs: in(r(22, 2, 3, 4)), f: func(x, _ []*Tensor) *Tensor {
			return x[0].Reshape(ints(4, -1)).Flatten(0, -1).Unsqueeze(0).Unsqueeze(2).Squeeze(none).Reshape(ints(6, 4))
		}},
		{name: "permute transpose T", inputs: in(r(23, 2, 3, 4)), f: func(x, _ []*Tensor) *Tensor {
			return x[0].Permute(ints(2, 0, 1)).Transpose(0, 2).Reshape(ints(12, 2)).T()
		}},
		{name: "narrow slice index", inputs: in(r(24, 4, 5)), f: func(x, _ []*Tensor) *Tensor {
			return x[0].Narrow(1, 1, 3).Slice(0, -3, some(-1)).Add(x[0].Index(-1).Narrow(0, 0, 3))
		}},
		{name: "cat stack", inputs: in(r(25, 2, 3), r(26, 2, 1), r(27, 2, 3)), f: func(x, _ []*Tensor) *Tensor {
			c := Cat(&rt.List[*Tensor]{S: []*Tensor{x[0], x[1], x[2]}}, 1)
			s := Stack(&rt.List[*Tensor]{S: []*Tensor{x[0], x[2]}}, 1)
			return c.Sum(some(1), false).Add(s.Reshape(ints(2, 6)).Sum(some(1), false))
		}},
		{name: "expand", inputs: in(r(28, 3, 1)), f: func(x, _ []*Tensor) *Tensor { return x[0].Expand(ints(2, 3, 4)) }},
		mmCase("matmul 向量·向量", []int{5}, []int{5}),
		mmCase("matmul 向量@矩阵", []int{5}, []int{5, 3}),
		mmCase("matmul 矩阵@向量", []int{4, 5}, []int{5}),
		mmCase("matmul 矩阵@矩阵", []int{4, 5}, []int{5, 3}),
		mmCase("matmul 批量@矩阵", []int{2, 4, 5}, []int{5, 3}),
		mmCase("matmul 矩阵@批量", []int{4, 5}, []int{2, 5, 3}),
		mmCase("matmul 批量@批量", []int{2, 4, 5}, []int{2, 5, 3}),
		mmCase("matmul 四维", []int{2, 2, 3, 5}, []int{2, 2, 5, 3}),
		{name: "softmax", inputs: in(r(30, 3, 5)), f: func(x, _ []*Tensor) *Tensor {
			return x[0].Softmax(-1).Add(x[0].Softmax(0)).Add(x[0].LogSoftmax(1))
		}},
		{name: "cross_entropy", inputs: in(r(31, 4, 3)), consts: in(constT([]int{4}, 0, 2, 1, 2)), f: func(x, c []*Tensor) *Tensor {
			return CrossEntropy(x[0], c[0], "mean").Add(CrossEntropy(x[0], c[0], "sum"))
		}},
		{name: "mse l1", inputs: in(r(32, 3, 4)), consts: in(r(33, 3, 4)), f: func(x, c []*Tensor) *Tensor {
			return MSELoss(x[0], c[0], "mean").Add(L1Loss(x[0].AddScalar(5), c[0], "sum"))
		}},
		{name: "bce", inputs: in(randT(34, 0.1, 0.9, 6), r(35, 6)), consts: in(constT([]int{6}, 0, 1, 1, 0, 1, 0)), f: func(x, c []*Tensor) *Tensor {
			return BCE(x[0], c[0], "mean").Add(BCEWithLogits(x[1], c[0], "sum"))
		}},
		{name: "embedding", inputs: in(r(36, 5, 3)), consts: in(constT([]int{2, 3}, 4, 0, 4, 1, 1, 2)), f: func(x, c []*Tensor) *Tensor {
			return Embedding(x[0], c[0])
		}},
		{name: "index_select", inputs: in(r(37, 4, 2, 3)), consts: in(constT([]int{3}, 3, 0, 3)), f: func(x, c []*Tensor) *Tensor {
			return x[0].IndexSelect(c[0])
		}},
		{name: "conv2d", inputs: in(r(38, 2, 3, 5, 6), r(39, 4, 3, 3, 3), r(42, 4)), f: func(x, _ []*Tensor) *Tensor {
			return Conv2d(x[0], x[1], rt.Opt[*Tensor]{V: x[2], Ok: true}, 1, 1)
		}},
		{name: "conv2d 步长 2", inputs: in(r(43, 1, 2, 7, 6), r(44, 3, 2, 3, 2)), f: func(x, _ []*Tensor) *Tensor {
			return Conv2d(x[0], x[1], rt.Opt[*Tensor]{}, 2, 0)
		}},
		{name: "max_pool2d", inputs: in(distinct(45, 2, 2, 4, 6)), f: func(x, _ []*Tensor) *Tensor { return MaxPool2d(x[0], 2, 2, 0) }},
		{name: "max_pool2d 重叠", inputs: in(distinct(46, 1, 2, 5, 5)), f: func(x, _ []*Tensor) *Tensor { return MaxPool2d(x[0], 3, 1, 1) }},
		{name: "avg_pool2d", inputs: in(r(47, 2, 3, 4, 4)), f: func(x, _ []*Tensor) *Tensor { return AvgPool2d(x[0], 2, 0) }},
		{name: "layer_norm", inputs: in(r(48, 3, 6), r(49, 6), r(50, 6)), f: func(x, _ []*Tensor) *Tensor {
			return LayerNorm(x[0], x[1], x[2], 1e-5)
		}},
		{name: "dropout", inputs: in(r(53, 4, 5)), f: func(x, _ []*Tensor) *Tensor {
			ManualSeed(3) // 数值求导时每次前向都要得到同样的掩码
			return Dropout(x[0], 0.3, true)
		}},
		{name: "gather", inputs: in(r(54, 3, 4)), consts: in(constT([]int{3, 2}, 3, 0, 1, 1, 2, 0), constT([]int{2, 4}, 0, 2, 1, 0, 2, 2, 0, 1)), f: func(x, c []*Tensor) *Tensor {
			return x[0].Gather(1, c[0]).Sum(none, false).Add(x[0].Gather(0, c[1]).Mul(x[0].Narrow(0, 0, 2)).Sum(none, false))
		}},
		{name: "topk sort", inputs: in(distinct(55, 3, 5)), f: func(x, _ []*Tensor) *Tensor {
			return x[0].Topk(2, true).V0.Sum(none, false).Add(x[0].Sort(false).V0.Narrow(1, 0, 3).Square().Sum(none, false))
		}},
		{name: "rms_norm silu", inputs: in(r(56, 3, 8), r(57, 8)), f: func(x, _ []*Tensor) *Tensor {
			return RMSNorm(x[0], x[1], 1e-6).Silu()
		}},
		{name: "where clone", inputs: in(r(51, 6), r(52, 6)), consts: in(constT([]int{6}, 1, 0, 0, 1, 1, 0)), f: func(x, c []*Tensor) *Tensor {
			return Where(c[0], x[0], x[1].Clone())
		}},
	}
}

// lossOf 把输出与固定的随机权重相乘后求和，得到标量（各个输出位置的梯度不同）。
func lossOf(out *Tensor, w *Tensor) *Tensor { return out.Mul(w).Sum(none, false) }

func weightsFor(out *Tensor) *Tensor { return randT(99, -1, 1, out.shape...) }

func setup(c gradCase, dev backend) (xs, cs []*Tensor) {
	for _, x := range c.inputs {
		t := fromHost(dev, x.shape, x.host())
		t.requiresGrad = true
		xs = append(xs, t)
	}
	for _, x := range c.consts {
		cs = append(cs, fromHost(dev, x.shape, x.host()))
	}
	return
}

func TestGradients(t *testing.T) {
	const eps = 1e-2
	for _, c := range gradCases() {
		t.Run(c.name, func(t *testing.T) {
			xs, cs := setup(c, cpuDev)
			out := c.f(xs, cs)
			w := weightsFor(out)
			lossOf(out, w).Backward(rt.Opt[*Tensor]{})
			for i, x := range xs {
				if x.grad == nil {
					t.Fatalf("第 %d 个输入没有梯度", i)
				}
				ana := x.grad.host()
				num := make([]float64, x.n)
				withNoGrad(func() {
					for j := range x.st.cpu {
						orig := x.st.cpu[j]
						x.st.cpu[j] = orig + eps
						fp := lossOf(c.f(xs, cs), w).Item()
						x.st.cpu[j] = orig - eps
						fm := lossOf(c.f(xs, cs), w).Item()
						x.st.cpu[j] = orig
						num[j] = (fp - fm) / (2 * eps)
					}
				})
				scale := 1.0
				for _, v := range num {
					scale = math.Max(scale, math.Abs(v))
				}
				for j := range num {
					if d := math.Abs(num[j] - float64(ana[j])); d > 2e-2*scale {
						t.Fatalf("第 %d 个输入的第 %d 个元素：自动求导 %.5f，数值求导 %.5f", i, j, ana[j], num[j])
					}
				}
			}
		})
	}
}

func closeTo(a, b []float32, tol float64) (int, bool) {
	if len(a) != len(b) {
		return -1, false
	}
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		if math.IsNaN(x) != math.IsNaN(y) || math.Abs(x-y) > tol*(1+math.Abs(y)) {
			return i, false
		}
	}
	return 0, true
}

func needGPU(t *testing.T) backend {
	g := gpu()
	if g == nil {
		t.Skip("没有 GPU：" + gpuError())
	}
	return g
}

// TestGPUMatchesCPU 在 GPU 上计算同样的前向与反向，与 CPU 的结果比较。
func TestGPUMatchesCPU(t *testing.T) {
	g := needGPU(t)
	for _, c := range gradCases() {
		t.Run(c.name, func(t *testing.T) {
			xc, cc := setup(c, cpuDev)
			xg, cg := setup(c, g)
			oc, og := c.f(xc, cc), c.f(xg, cg)
			if og.dev() != g {
				t.Fatalf("结果不在 GPU 上")
			}
			if i, ok := closeTo(og.host(), oc.host(), 1e-4); !ok {
				t.Fatalf("前向结果不同（第 %d 个元素）：GPU %v，CPU %v", i, og.host(), oc.host())
			}
			w := weightsFor(oc)
			lossOf(oc, w).Backward(rt.Opt[*Tensor]{})
			lossOf(og, fromHost(g, w.shape, w.host())).Backward(rt.Opt[*Tensor]{})
			for i := range xc {
				if i, ok := closeTo(xg[i].grad.host(), xc[i].grad.host(), 1e-4); !ok {
					t.Fatalf("第 %d 个输入的梯度不同（第 %d 个元素）", i, i)
				}
			}
		})
	}
}

func TestGPUKernels(t *testing.T) {
	g := needGPU(t)
	onBoth := func(f func(dev backend) *Tensor) ([]float32, []float32) {
		return f(cpuDev).host(), f(g).host()
	}
	check := func(name string, a, b []float32, tol float64) {
		t.Helper()
		if i, ok := closeTo(b, a, tol); !ok {
			t.Errorf("%s：第 %d 个元素不同（CPU %v，GPU %v）", name, i, a[min(max(i, 0), len(a)-1)], b[min(max(i, 0), len(b)-1)])
		}
	}
	big := randT(7, -1, 1, 100003)
	a, b := onBoth(func(dev backend) *Tensor { return fromHost(dev, big.shape, big.host()).Sum(none, false) })
	check("长向量求和", a, b, 1e-4)
	a, b = onBoth(func(dev backend) *Tensor { return fromHost(dev, big.shape, big.host()).Max(none, false) })
	check("长向量最大值", a, b, 0)
	// 按行求和（最后一维至少 32 个数时一个工作组处理一行），以及它的反向（softmax）
	rws := randT(16, -1, 1, 37, 70)
	a, b = onBoth(func(dev backend) *Tensor { return fromHost(dev, rws.shape, rws.host()).Sum(some(-1), false) })
	check("按行求和", a, b, 1e-5)
	a, b = onBoth(func(dev backend) *Tensor {
		x := fromHost(dev, rws.shape, rws.host()).SetRequiresGrad(true)
		x.Softmax(-1).Mul(fromHost(dev, rws.shape, randT(17, -1, 1, 37, 70).host())).Sum(none, false).Backward(rt.Opt[*Tensor]{})
		return x.grad
	})
	check("softmax 的反向（长行）", a, b, 1e-5)
	// 沿很长的一维求和（分段求和的路径），以及多维广播
	col := randT(13, -1, 1, 5000, 3, 7)
	a, b = onBoth(func(dev backend) *Tensor { return fromHost(dev, col.shape, col.host()).Sum(some(0), false) })
	check("长维度求和", a, b, 1e-4)
	s4, m3 := randT(14, -1, 1, 3, 4, 5, 6), randT(15, -1, 1, 3, 1, 1, 6)
	a, b = onBoth(func(dev backend) *Tensor {
		return fromHost(dev, s4.shape, s4.host()).Add(fromHost(dev, m3.shape, m3.host())).Mul(fromHost(dev, []int{5, 1}, []float32{1, 2, 3, 4, 5}))
	})
	check("四维广播", a, b, 1e-6)
	m := randT(8, -1, 1, 37, 1001)
	for d := 0; d < 2; d++ {
		a, b = onBoth(func(dev backend) *Tensor { return fromHost(dev, m.shape, m.host()).Argmax(some(d), false) })
		check(fmt.Sprintf("argmax 第 %d 维", d), a, b, 0)
		a, b = onBoth(func(dev backend) *Tensor { return fromHost(dev, m.shape, m.host()).Softmax(d) })
		check(fmt.Sprintf("softmax 第 %d 维", d), a, b, 1e-5)
	}
	// 尺寸不是 64 的倍数的大矩阵，以及转置
	x, y := randT(9, -1, 1, 129, 257), randT(10, -1, 1, 257, 65)
	a, b = onBoth(func(dev backend) *Tensor {
		return fromHost(dev, x.shape, x.host()).Matmul(fromHost(dev, y.shape, y.host()))
	})
	check("matmul 129x257x65", a, b, 1e-4)
	a, b = onBoth(func(dev backend) *Tensor {
		return mm(fromHost(dev, y.shape, y.host()), fromHost(dev, x.shape, x.host()), true, true, 1, 65, 257, 129, 0, 0)
	})
	check("matmul 双转置", a, b, 1e-4)
	// 随机数：均匀分布在两种设备上完全相同，正态分布只有舍入误差
	for _, normal := range []bool{false, true} {
		a, b = onBoth(func(dev backend) *Tensor {
			s := dev.alloc(1000)
			dev.random(s, 1000, 12345, normal)
			return &Tensor{shape: []int{1000}, n: 1000, st: s}
		})
		check(fmt.Sprintf("随机数（正态：%v）", normal), a, b, 1e-5)
	}
	// 优化器的更新步骤
	p0, g0 := randT(11, -1, 1, 1000), randT(12, -1, 1, 1000)
	for _, decoupled := range []bool{false, true} {
		a, b = onBoth(func(dev backend) *Tensor {
			p, gr := fromHost(dev, p0.shape, p0.host()), fromHost(dev, g0.shape, g0.host())
			mm, vv := full(dev, p0.shape, 0.1), full(dev, p0.shape, 0.2)
			dev.adam(p.st, gr.st, mm.st, vv.st, p.n, 0.01, 0.9, 0.999, 1e-8, 0.01, 0.5, 0.3, decoupled)
			return p
		})
		check("adam", a, b, 1e-5)
	}
	a, b = onBoth(func(dev backend) *Tensor {
		p, gr, buf := fromHost(dev, p0.shape, p0.host()), fromHost(dev, g0.shape, g0.host()), full(dev, p0.shape, 0.3)
		dev.sgd(p.st, gr.st, buf.st, p.n, 0.1, 0.9, 0.01, true)
		return p.Add(buf)
	})
	check("sgd", a, b, 1e-5)
}

func expectRaise(t *testing.T, kind, substr string, f func()) {
	t.Helper()
	defer func() {
		t.Helper()
		r := recover()
		th, ok := r.(*rt.Thrown)
		if !ok {
			t.Fatalf("应当抛出 %s，实际：%v", kind, r)
		}
		e := th.Exc.(*rt.Error)
		if e.Kind != kind || !strings.Contains(e.Msg, substr) {
			t.Fatalf("应当抛出包含 %q 的 %s，实际：%s: %s", substr, kind, e.Kind, e.Msg)
		}
	}()
	f()
}

// TestEmptyTensors 检查元素个数为 0 的张量（某一维长度为 0）。
func TestEmptyTensors(t *testing.T) {
	devs := []backend{cpuDev}
	if g := gpu(); g != nil {
		devs = append(devs, g)
	}
	for _, dev := range devs {
		e := full(dev, []int{0, 3}, 0)
		if got := e.Add(full(dev, []int{3}, 1)).shape; !sameShape(got, []int{0, 3}) {
			t.Fatalf("%s：广播后的形状 %v", dev.name(), got)
		}
		if got := e.Permute(ints(1, 0)).shape; !sameShape(got, []int{3, 0}) {
			t.Fatalf("%s：permute 后的形状 %v", dev.name(), got)
		}
		if v := e.Sum(none, false).Item(); v != 0 {
			t.Fatalf("%s：空张量的和 %v", dev.name(), v)
		}
		if got := e.Matmul(full(dev, []int{3, 2}, 1)).shape; !sameShape(got, []int{0, 2}) {
			t.Fatalf("%s：矩阵乘法的形状 %v", dev.name(), got)
		}
	}
}

// TestCheckpoint 检查重新计算得到的梯度与普通的反向传播相同（包括 f 中的 dropout 与参数的梯度）。
func TestCheckpoint(t *testing.T) {
	x0, w0 := randT(70, -1, 1, 4, 6), randT(71, -1, 1, 6, 6)
	f := func(w *Tensor) func(*Tensor) *Tensor {
		return func(h *Tensor) *Tensor { return Dropout(h.Matmul(w).Tanh(), 0.3, true).Add(h) }
	}
	run := func(ckpt bool) ([]float32, []float32) {
		ManualSeed(9)
		x := fromHost(cpuDev, x0.shape, x0.host()).SetRequiresGrad(true)
		w := fromHost(cpuDev, w0.shape, w0.host()).SetRequiresGrad(true)
		h := x
		for i := 0; i < 3; i++ {
			if ckpt {
				h = Checkpoint(f(w), h)
			} else {
				h = f(w)(h)
			}
		}
		h.Square().Sum(none, false).Backward(rt.Opt[*Tensor]{})
		return x.grad.host(), w.grad.host()
	}
	gx1, gw1 := run(false)
	gx2, gw2 := run(true)
	if i, ok := closeTo(gx2, gx1, 1e-5); !ok {
		t.Fatalf("输入的梯度不同（第 %d 个元素）", i)
	}
	if i, ok := closeTo(gw2, gw1, 1e-5); !ok {
		t.Fatalf("参数的梯度不同（第 %d 个元素）", i)
	}
}

// TestQuantizedLinear 比较 int8 权重的线性层与反量化后的 float32 线性层（前向与对输入的梯度）。
func TestQuantizedLinear(t *testing.T) {
	for _, sz := range [][3]int{{70, 133, 15}, {260, 132, 100}, {388, 1024, 70}} {
		testQuantizedLinear(t, sz[0], sz[1], sz[2])
	}
}

func testQuantizedLinear(t *testing.T, rows, cols, m int) {
	devs := []backend{cpuDev}
	if g := gpu(); g != nil {
		devs = append(devs, g)
	}
	w0, x0 := randT(80, -0.5, 0.5, rows, cols), randT(81, -1, 1, m, cols)
	for _, dev := range devs {
		w := fromHost(dev, w0.shape, w0.host())
		q := QuantizeInt8(w, rt.Opt[string]{})
		deq := q.Dequantize()
		if i, ok := closeTo(deq.host(), w0.host(), 0.5/127); !ok {
			t.Fatalf("%s：反量化的误差太大（第 %d 个元素）", dev.name(), i)
		}
		run := func(f func(x *Tensor) *Tensor) ([]float32, []float32) {
			x := fromHost(dev, x0.shape, x0.host()).SetRequiresGrad(true)
			y := f(x)
			y.Mul(fromHost(dev, y.shape, randT(82, -1, 1, y.shape...).host())).Sum(none, false).Backward(rt.Opt[*Tensor]{})
			return y.host(), x.grad.host()
		}
		y1, g1 := run(func(x *Tensor) *Tensor { return LinearQ8(x, q) })
		y2, g2 := run(func(x *Tensor) *Tensor { return LinearFn(x, deq, rt.Opt[*Tensor]{}) })
		if i, ok := closeTo(y1, y2, 1e-4); !ok {
			t.Fatalf("%s：前向结果不同（第 %d 个元素：%v 与 %v）", dev.name(), i, y1[i], y2[i])
		}
		if i, ok := closeTo(g1, g2, 1e-4); !ok {
			t.Fatalf("%s：梯度不同（第 %d 个元素）", dev.name(), i)
		}
	}
}

func TestErrors(t *testing.T) {
	a := Zeros(ints(2, 3))
	expectRaise(t, "ValueError", "不能广播", func() { a.Add(Zeros(ints(4))) })
	expectRaise(t, "ValueError", "形状不匹配", func() { a.Matmul(Zeros(ints(2, 3))) })
	expectRaise(t, "ValueError", "整理为", func() { a.Reshape(ints(4, 2)) })
	expectRaise(t, "IndexError", "超出范围", func() { a.Index(2) })
	expectRaise(t, "RuntimeError", "只能对只有一个元素的张量调用 backward", func() { a.SetRequiresGrad(true).Backward(rt.Opt[*Tensor]{}) })
	expectRaise(t, "IndexError", "超出范围", func() {
		CrossEntropy(Zeros(ints(2, 3)), FromList(&rt.List[float64]{S: []float64{0, 3}}, rt.Opt[*rt.List[int]]{}), "mean")
	})
	expectRaise(t, "ValueError", "reduction", func() { MSELoss(a, a, "avg") })
	x := Ones(ints(2)).SetRequiresGrad(true)
	y := x.Mul(x).Sum(none, false)
	y.Backward(rt.Opt[*Tensor]{})
	expectRaise(t, "RuntimeError", "已经在之前的 backward", func() { y.Backward(rt.Opt[*Tensor]{}) })
}

func TestAutogradBasics(t *testing.T) {
	x := FromList(&rt.List[float64]{S: []float64{1, 2, 3}}, rt.Opt[*rt.List[int]]{}).SetRequiresGrad(true)
	// y = sum(x² + 2x)，dy/dx = 2x + 2；同一个张量用了两次，梯度要累加
	y := x.Mul(x).Add(x.MulScalar(2)).Sum(none, false)
	y.Backward(rt.Opt[*Tensor]{})
	if got := fmt.Sprint(x.grad.host()); got != "[4 6 8]" {
		t.Fatalf("梯度：%s", got)
	}
	// 再次反向传播（新的计算图）时梯度累加
	x.Sum(none, false).Backward(rt.Opt[*Tensor]{})
	if got := fmt.Sprint(x.grad.host()); got != "[5 7 9]" {
		t.Fatalf("累加后的梯度：%s", got)
	}
	// no_grad 中不记录计算图
	ctx := NoGrad()
	ctx.Enter()
	z := x.MulScalar(2)
	ctx.Exit()
	if z.requiresGrad || z.fn != nil {
		t.Fatalf("no_grad 中不应记录计算图")
	}
	if s := x.String(); s != "tensor([1.0000, 2.0000, 3.0000], requires_grad=true)" {
		t.Fatalf("显示：%s", s)
	}
	m := Arange(0, 6, 1).Reshape(ints(2, 3))
	if s := m.String(); s != "tensor([[0.0000, 1.0000, 2.0000],\n        [3.0000, 4.0000, 5.0000]])" {
		t.Fatalf("显示：%q", s)
	}
}

// TestGPUMemoryBounded 检查显存会被回收复用：反复分配大张量时，占用的显存（正在使用 + 缓存）
// 不超过上限 highWater 太多。
func TestGPUMemoryBounded(t *testing.T) {
	needGPU(t)
	g := gpu()
	SetDefaultDevice("gpu")
	defer SetDefaultDevice("cpu")
	w := Randn(ints(1024, 1024)).SetRequiresGrad(true)
	for i := 0; i < 300; i++ {
		x := Randn(ints(256, 1024))
		x.Matmul(w).Relu().Sum(none, false).Backward(rt.Opt[*Tensor]{})
		w.ZeroGrad()
	}
	if total := g.liveBytes.Load() + g.pooledBytes.Load(); total > g.highWater+(64<<20) {
		t.Fatalf("显存占用 %d MB，超过了上限 %d MB", total>>20, g.highWater>>20)
	}
	g.collect()
	if live := g.liveBytes.Load(); live > 64<<20 {
		t.Fatalf("回收之后仍有 %d MB 显存被占用", live>>20)
	}
	if size := sizeClass(5<<20 + 1); size != 5<<20+512<<10 {
		t.Fatalf("5 MB + 1 字节的块大小为 %d", size)
	}
	EmptyCache()
}

// naiveMatmul 是按定义计算的矩阵乘法（float64 累加），用来检查优化过的实现。
func naiveMatmul(a, b []float32, batch, m, k, n int, ta, tb bool, sa, sb int) []float32 {
	out := make([]float32, batch*m*n)
	for bi := 0; bi < batch; bi++ {
		for i := 0; i < m; i++ {
			for j := 0; j < n; j++ {
				var s float64
				for p := 0; p < k; p++ {
					var x, y float32
					if ta {
						x = a[bi*sa+p*m+i]
					} else {
						x = a[bi*sa+i*k+p]
					}
					if tb {
						y = b[bi*sb+j*k+p]
					} else {
						y = b[bi*sb+p*n+j]
					}
					s += float64(x) * float64(y)
				}
				out[bi*m*n+i*n+j] = float32(s)
			}
		}
	}
	return out
}

// TestMatmulShapes 用各种尺寸（包括不是 6、16 的倍数的边缘情况）、转置与批次检查 CPU 和 GPU 的矩阵乘法。
func TestMatmulShapes(t *testing.T) {
	devs := []backend{cpuDev}
	if g := gpu(); g != nil {
		devs = append(devs, g)
	}
	sizes := [][3]int{{1, 1, 1}, {6, 16, 1}, {7, 17, 3}, {5, 300, 33}, {13, 1, 600}, {64, 64, 64}, {97, 131, 259}, {1, 513, 70}, {300, 7, 9},
		{128, 16, 128}, {130, 40, 257}, {255, 129, 140}, {100, 16, 100}, {100, 100, 16}, {40, 33, 33},
		{132, 36, 260}, {300, 1024, 388}, {256, 20, 132}}
	seed := uint64(100)
	for _, sz := range sizes {
		m, k, n := sz[0], sz[1], sz[2]
		for _, batch := range []int{1, 3} {
			for _, shared := range []bool{false, true} {
				for ta := 0; ta < 2; ta++ {
					for tb := 0; tb < 2; tb++ {
						seed++
						sa, sb := m*k, k*n
						na := batch
						if shared {
							sa, na = 0, 1
						}
						a := randT(seed, -1, 1, na*m*k)
						b := randT(seed+1000, -1, 1, batch*k*n)
						want := naiveMatmul(a.st.cpu, b.st.cpu, batch, m, k, n, ta == 1, tb == 1, sa, sb)
						for _, dev := range devs {
							da, db := dev.upload(a.st.cpu), dev.upload(b.st.cpu)
							c := dev.alloc(batch * m * n)
							dev.matmul(da, db, c, batch, m, k, n, ta == 1, tb == 1, sa, sb)
							got := dev.download(c, batch*m*n)
							if i, ok := closeTo(got, want, 1e-4*math.Sqrt(float64(k))); !ok {
								t.Fatalf("%s：m=%d k=%d n=%d 批次=%d 共用 A=%v ta=%d tb=%d：第 %d 个元素 %v，应为 %v",
									dev.name(), m, k, n, batch, shared, ta, tb, i, got[i], want[i])
							}
						}
					}
				}
			}
		}
	}
}

// TestFastMath 检查 float32 的 exp、tanh 的精度。
func TestFastMath(t *testing.T) {
	for i := -2000; i <= 2000; i++ {
		x := float32(i) * 0.0443 // 覆盖 [-88.6, 88.6]
		want := math.Exp(float64(x))
		if got := float64(expf(x)); math.Abs(got-want) > 3e-7*want+1e-37 {
			t.Fatalf("expf(%v) = %v，应为 %v", x, got, want)
		}
		y := x / 10
		if got, want := float64(tanhf(y)), math.Tanh(float64(y)); math.Abs(got-want) > 3e-7*math.Abs(want)+1e-9 {
			t.Fatalf("tanhf(%v) = %v，应为 %v", y, got, want)
		}
	}
	if !math.IsInf(float64(expf(100)), 1) || expf(-100) != 0 || expf(0) != 1 {
		t.Fatalf("expf 的边界值不正确")
	}
}

func TestSortTopkGather(t *testing.T) {
	x := constT([]int{2, 4}, 3, 1, 4, 1, 5, 9, 2, 6)
	s := x.Sort(false)
	if got := fmt.Sprint(s.V0.host(), s.V1.host()); got != "[1 1 3 4 2 5 6 9] [1 3 0 2 2 0 3 1]" {
		t.Fatalf("sort：%s", got)
	}
	k := x.Topk(2, true)
	if got := fmt.Sprint(k.V0.host(), k.V1.host(), k.V0.shape); got != "[4 3 9 6] [2 0 1 3] [2 2]" {
		t.Fatalf("topk：%s", got)
	}
	g := x.Gather(0, constT([]int{1, 4}, 1, 0, 1, 0))
	if got := fmt.Sprint(g.host()); got != "[5 1 2 1]" {
		t.Fatalf("gather：%s", got)
	}
	expectRaise(t, "IndexError", "超出范围", func() { x.Gather(1, constT([]int{2, 1}, 4, 0)) })
}

func TestImageRoundTrip(t *testing.T) {
	img := randT(60, 0, 1, 3, 5, 7)
	path := t.TempDir() + "/a.png"
	SaveImage(img, path)
	back := LoadImage(path, false)
	if !sameShape(back.shape, []int{3, 5, 7}) {
		t.Fatalf("形状 %v", back.shape)
	}
	if i, ok := closeTo(back.host(), img.host(), 1.0/255); !ok {
		t.Fatalf("第 %d 个像素不同", i)
	}
	if g := LoadImage(path, true); !sameShape(g.shape, []int{1, 5, 7}) {
		t.Fatalf("灰度图的形状 %v", g.shape)
	}
}
