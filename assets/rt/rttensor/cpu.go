package rttensor

import (
	"math"
	"runtime"
	"sync"
)

// cpuBackend 用 Go 代码计算；数据量大时把工作分给多个协程。
type cpuBackend struct{}

var cpuDev backend = &cpuBackend{}

func (*cpuBackend) name() string { return "cpu" }

func (c *cpuBackend) alloc(n int) *storage { return &storage{dev: c, cpu: make([]float32, n)} }

func (c *cpuBackend) upload(d []float32) *storage {
	return &storage{dev: c, cpu: append([]float32(nil), d...)}
}

func (*cpuBackend) download(s *storage, n int) []float32 { return append([]float32(nil), s.cpu[:n]...) }

func (*cpuBackend) fill(s *storage, n int, v float32) {
	d := s.cpu[:n]
	for i := range d {
		d[i] = v
	}
}

func (*cpuBackend) copyTo(dst, src *storage, n int) { copy(dst.cpu[:n], src.cpu[:n]) }

// parallelFor 把 [0, n) 分成若干段并行执行；n 较小时直接在当前协程中执行。
func parallelFor(n, grain int, f func(lo, hi int)) {
	workers := runtime.GOMAXPROCS(0)
	if workers == 1 || n < 2*grain {
		f(0, n)
		return
	}
	chunks := min(workers*2, (n+grain-1)/grain)
	size := (n + chunks - 1) / chunks
	var wg sync.WaitGroup
	for lo := 0; lo < n; lo += size {
		hi := min(lo+size, n)
		wg.Add(1)
		go func(lo, hi int) {
			defer wg.Done()
			f(lo, hi)
		}(lo, hi)
	}
	wg.Wait()
}

const grainElem = 1 << 15

func geluF(x float32) float32 {
	v := float64(x)
	return float32(0.5 * v * (1 + math.Tanh(0.7978845608028654*(v+0.044715*v*v*v))))
}

func geluGradF(x float32) float32 {
	v := float64(x)
	u := 0.7978845608028654 * (v + 0.044715*v*v*v)
	t := math.Tanh(u)
	du := 0.7978845608028654 * (1 + 3*0.044715*v*v)
	return float32(0.5*(1+t) + 0.5*v*(1-t*t)*du)
}

func unaryFunc(op int, a, b float32) func(float32) float32 {
	switch op {
	case uNeg:
		return func(x float32) float32 { return -x }
	case uExp:
		return func(x float32) float32 { return float32(math.Exp(float64(x))) }
	case uLog:
		return func(x float32) float32 { return float32(math.Log(float64(x))) }
	case uSqrt:
		return func(x float32) float32 { return float32(math.Sqrt(float64(x))) }
	case uAbs:
		return func(x float32) float32 { return float32(math.Abs(float64(x))) }
	case uRelu:
		return func(x float32) float32 { return max(x, 0) }
	case uSigmoid:
		return func(x float32) float32 { return float32(1 / (1 + math.Exp(-float64(x)))) }
	case uTanh:
		return func(x float32) float32 { return float32(math.Tanh(float64(x))) }
	case uGelu:
		return geluF
	case uSin:
		return func(x float32) float32 { return float32(math.Sin(float64(x))) }
	case uCos:
		return func(x float32) float32 { return float32(math.Cos(float64(x))) }
	case uAddS:
		return func(x float32) float32 { return x + a }
	case uMulS:
		return func(x float32) float32 { return x * a }
	case uRSubS:
		return func(x float32) float32 { return a - x }
	case uRDivS:
		return func(x float32) float32 { return a / x }
	case uPowS:
		return func(x float32) float32 { return float32(math.Pow(float64(x), float64(a))) }
	case uClamp:
		return func(x float32) float32 { return min(max(x, a), b) }
	case uLeaky:
		return func(x float32) float32 {
			if x > 0 {
				return x
			}
			return a * x
		}
	case uSquare:
		return func(x float32) float32 { return x * x }
	case uRecip:
		return func(x float32) float32 { return 1 / x }
	case uSign:
		return func(x float32) float32 {
			switch {
			case x > 0:
				return 1
			case x < 0:
				return -1
			}
			return 0
		}
	case uGtS:
		return func(x float32) float32 { return b2f(x > a) }
	case uLtS:
		return func(x float32) float32 { return b2f(x < a) }
	case uGeluGrad:
		return geluGradF
	case uSigDeriv:
		return func(y float32) float32 { return y * (1 - y) }
	case uTanhDeriv:
		return func(y float32) float32 { return 1 - y*y }
	case uLeakyGrad:
		return func(x float32) float32 {
			if x > 0 {
				return 1
			}
			return a
		}
	case uPowSGrad:
		return func(x float32) float32 { return a * float32(math.Pow(float64(x), float64(a-1))) }
	}
	panic("未知的一元运算")
}

func b2f(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

func binaryFunc(op int) func(x, y float32) float32 {
	switch op {
	case bAdd:
		return func(x, y float32) float32 { return x + y }
	case bSub:
		return func(x, y float32) float32 { return x - y }
	case bMul:
		return func(x, y float32) float32 { return x * y }
	case bDiv:
		return func(x, y float32) float32 { return x / y }
	case bPow:
		return func(x, y float32) float32 { return float32(math.Pow(float64(x), float64(y))) }
	case bMax:
		return func(x, y float32) float32 { return max(x, y) }
	case bMin:
		return func(x, y float32) float32 { return min(x, y) }
	case bGt:
		return func(x, y float32) float32 { return b2f(x > y) }
	case bLt:
		return func(x, y float32) float32 { return b2f(x < y) }
	case bEq:
		return func(x, y float32) float32 { return b2f(x == y) }
	case bGe:
		return func(x, y float32) float32 { return b2f(x >= y) }
	case bLe:
		return func(x, y float32) float32 { return b2f(x <= y) }
	}
	panic("未知的二元运算")
}

func (*cpuBackend) unary(op int, x, y *storage, n int, a, b float32) {
	f := unaryFunc(op, a, b)
	xs, ys := x.cpu[:n], y.cpu[:n]
	parallelFor(n, grainElem, func(lo, hi int) {
		for i := lo; i < hi; i++ {
			ys[i] = f(xs[i])
		}
	})
}

func (*cpuBackend) binary(op int, x, y, out *storage, n int, bc *bcast) {
	xs, ys, os := x.cpu, y.cpu, out.cpu[:n]
	if bc == nil {
		// 形状相同：最常见的几种运算单独写成循环
		xs, ys = xs[:n], ys[:n]
		parallelFor(n, grainElem, func(lo, hi int) {
			switch op {
			case bAdd:
				for i := lo; i < hi; i++ {
					os[i] = xs[i] + ys[i]
				}
			case bSub:
				for i := lo; i < hi; i++ {
					os[i] = xs[i] - ys[i]
				}
			case bMul:
				for i := lo; i < hi; i++ {
					os[i] = xs[i] * ys[i]
				}
			default:
				f := binaryFunc(op)
				for i := lo; i < hi; i++ {
					os[i] = f(xs[i], ys[i])
				}
			}
		})
		return
	}
	f := binaryFunc(op)
	nd := bc.ndim
	parallelFor(n, grainElem, func(lo, hi int) {
		// 由线性下标 lo 计算多维下标，然后像里程表一样递增
		var idx [maxDims]int
		rem := lo
		ox, oy := 0, 0
		for d := nd - 1; d >= 0; d-- {
			s := int(bc.shape[d])
			idx[d] = rem % s
			rem /= s
			ox += idx[d] * int(bc.sx[d])
			oy += idx[d] * int(bc.sy[d])
		}
		for i := lo; i < hi; i++ {
			os[i] = f(xs[ox], ys[oy])
			for d := nd - 1; d >= 0; d-- {
				idx[d]++
				ox += int(bc.sx[d])
				oy += int(bc.sy[d])
				if idx[d] < int(bc.shape[d]) {
					break
				}
				ox -= idx[d] * int(bc.sx[d])
				oy -= idx[d] * int(bc.sy[d])
				idx[d] = 0
			}
		}
	})
}

func (*cpuBackend) reduce(op int, x, out, idx *storage, outer, dim, inner int) {
	xs, os := x.cpu, out.cpu
	parallelFor(outer*inner, 256, func(lo, hi int) {
		for t := lo; t < hi; t++ {
			o, i := t/inner, t%inner
			base := o*dim*inner + i
			switch op {
			case rSum:
				var acc float64
				for d := 0; d < dim; d++ {
					acc += float64(xs[base+d*inner])
				}
				os[t] = float32(acc)
			case rMax, rMin:
				best, bi := xs[base], 0
				for d := 1; d < dim; d++ {
					v := xs[base+d*inner]
					if (op == rMax && v > best) || (op == rMin && v < best) {
						best, bi = v, d
					}
				}
				os[t] = best
				if idx != nil {
					idx.cpu[t] = float32(bi)
				}
			}
		}
	})
}

func (*cpuBackend) matmul(a, b, c *storage, batch, m, k, n int, ta, tb bool, sa, sb int) {
	as, bs, cs := a.cpu, b.cpu, c.cpu
	// 先把转置的输入整理成行优先的 [m, k] 与 [k, n]，内层循环才能顺序访问内存
	tmpA, tmpB := as, bs
	if ta {
		tmpA = transposeBatches(as, batch, sa, k, m)
		if sa != 0 {
			sa = m * k
		}
	}
	if tb {
		tmpB = transposeBatches(bs, batch, sb, n, k)
		if sb != 0 {
			sb = k * n
		}
	}
	parallelFor(batch*m, max(1, 4096/max(1, n*k/64)), func(lo, hi int) {
		for r := lo; r < hi; r++ {
			bi, i := r/m, r%m
			arow := tmpA[bi*sa+i*k : bi*sa+i*k+k]
			crow := cs[bi*m*n+i*n : bi*m*n+i*n+n]
			for j := range crow {
				crow[j] = 0
			}
			bb := tmpB[bi*sb:]
			for p, av := range arow {
				if av == 0 {
					continue
				}
				brow := bb[p*n : p*n+n]
				for j, bv := range brow {
					crow[j] += av * bv
				}
			}
		}
	})
}

// transposeBatches 把每个批次的 [rows, cols] 矩阵转置为 [cols, rows]。
func transposeBatches(x []float32, batch, stride, rows, cols int) []float32 {
	nb := batch
	if stride == 0 {
		nb = 1
	}
	out := make([]float32, nb*rows*cols)
	for b := 0; b < nb; b++ {
		src := x[b*stride:]
		dst := out[b*rows*cols:]
		for r := 0; r < rows; r++ {
			for c := 0; c < cols; c++ {
				dst[c*rows+r] = src[r*cols+c]
			}
		}
	}
	return out
}

func (*cpuBackend) softmax(x, y *storage, rows, cols int, log bool) {
	xs, ys := x.cpu, y.cpu
	parallelFor(rows, 64, func(lo, hi int) {
		for r := lo; r < hi; r++ {
			row := xs[r*cols : r*cols+cols]
			out := ys[r*cols : r*cols+cols]
			mx := row[0]
			for _, v := range row {
				mx = max(mx, v)
			}
			var sum float64
			for j, v := range row {
				e := math.Exp(float64(v - mx))
				sum += e
				if !log {
					out[j] = float32(e)
				}
			}
			if log {
				ls := float32(math.Log(sum))
				for j, v := range row {
					out[j] = v - mx - ls
				}
			} else {
				inv := float32(1 / sum)
				for j := range out {
					out[j] *= inv
				}
			}
		}
	})
}

func (*cpuBackend) permute(x, y *storage, shape, perm []int) {
	nd := len(shape)
	inStride := make([]int, nd)
	s := 1
	for d := nd - 1; d >= 0; d-- {
		inStride[d] = s
		s *= shape[d]
	}
	outShape := make([]int, nd)
	strides := make([]int, nd) // 输出第 d 维对应的输入步长
	for d, p := range perm {
		outShape[d] = shape[p]
		strides[d] = inStride[p]
	}
	n := numel(shape)
	xs, ys := x.cpu, y.cpu
	parallelFor(n, grainElem, func(lo, hi int) {
		for i := lo; i < hi; i++ {
			rem, off := i, 0
			for d := nd - 1; d >= 0; d-- {
				off += (rem % outShape[d]) * strides[d]
				rem /= outShape[d]
			}
			ys[i] = xs[off]
		}
	})
}

func (*cpuBackend) narrow(x, y *storage, outer, dim, inner, start, length int) {
	for o := 0; o < outer; o++ {
		copy(y.cpu[o*length*inner:(o+1)*length*inner], x.cpu[(o*dim+start)*inner:(o*dim+start+length)*inner])
	}
}

func (*cpuBackend) place(x, y *storage, outer, dim, inner, start, length int) {
	for o := 0; o < outer; o++ {
		copy(y.cpu[(o*dim+start)*inner:(o*dim+start+length)*inner], x.cpu[o*length*inner:(o+1)*length*inner])
	}
}

func (*cpuBackend) indexSelect(w, idx, out *storage, nidx, rows, cols int) {
	for i := 0; i < nidx; i++ {
		r := int(idx.cpu[i])
		if r < 0 || r >= rows {
			raiseIndex(r, rows)
		}
		copy(out.cpu[i*cols:(i+1)*cols], w.cpu[r*cols:(r+1)*cols])
	}
}

func (*cpuBackend) indexAdd(g, idx, out *storage, nidx, rows, cols int) {
	for i := 0; i < nidx; i++ {
		r := int(idx.cpu[i])
		dst := out.cpu[r*cols : (r+1)*cols]
		for j, v := range g.cpu[i*cols : (i+1)*cols] {
			dst[j] += v
		}
	}
}

func (*cpuBackend) pick(x, idx, out *storage, rows, cols int) {
	for i := 0; i < rows; i++ {
		c := int(idx.cpu[i])
		if c < 0 || c >= cols {
			raiseIndex(c, cols)
		}
		out.cpu[i] = x.cpu[i*cols+c]
	}
}

func (*cpuBackend) pickBack(g, idx, out *storage, rows, cols int) {
	for i := 0; i < rows; i++ {
		out.cpu[i*cols+int(idx.cpu[i])] += g.cpu[i]
	}
}

func (*cpuBackend) im2col(x, cols *storage, p *convParams) {
	xs, cs := x.cpu, cols.cpu
	rowsPer, colsPer := p.colRows(), p.colCols()
	parallelFor(p.n*rowsPer, 16, func(lo, hi int) {
		for t := lo; t < hi; t++ {
			b, r := t/rowsPer, t%rowsPer
			ci, kr := r/(p.kh*p.kw), r%(p.kh*p.kw)
			ki, kj := kr/p.kw, kr%p.kw
			dst := cs[(b*rowsPer+r)*colsPer:]
			src := xs[(b*p.c+ci)*p.h*p.w:]
			for oy := 0; oy < p.oh; oy++ {
				iy := oy*p.sh - p.ph + ki
				for ox := 0; ox < p.ow; ox++ {
					ix := ox*p.sw - p.pw + kj
					var v float32
					if iy >= 0 && iy < p.h && ix >= 0 && ix < p.w {
						v = src[iy*p.w+ix]
					}
					dst[oy*p.ow+ox] = v
				}
			}
		}
	})
}

func (*cpuBackend) col2im(cols, x *storage, p *convParams) {
	xs, cs := x.cpu, cols.cpu
	rowsPer, colsPer := p.colRows(), p.colCols()
	// 按 (样本, 通道) 并行：不同通道写入不同的位置
	parallelFor(p.n*p.c, 1, func(lo, hi int) {
		for t := lo; t < hi; t++ {
			b, ci := t/p.c, t%p.c
			dst := xs[(b*p.c+ci)*p.h*p.w:]
			for kr := 0; kr < p.kh*p.kw; kr++ {
				ki, kj := kr/p.kw, kr%p.kw
				src := cs[(b*rowsPer+ci*p.kh*p.kw+kr)*colsPer:]
				for oy := 0; oy < p.oh; oy++ {
					iy := oy*p.sh - p.ph + ki
					if iy < 0 || iy >= p.h {
						continue
					}
					for ox := 0; ox < p.ow; ox++ {
						ix := ox*p.sw - p.pw + kj
						if ix >= 0 && ix < p.w {
							dst[iy*p.w+ix] += src[oy*p.ow+ox]
						}
					}
				}
			}
		}
	})
}

func (*cpuBackend) maxpool(x, y, idx *storage, p *convParams) {
	xs, ys, is := x.cpu, y.cpu, idx.cpu
	parallelFor(p.n*p.c, 1, func(lo, hi int) {
		for t := lo; t < hi; t++ {
			src := xs[t*p.h*p.w:]
			for oy := 0; oy < p.oh; oy++ {
				for ox := 0; ox < p.ow; ox++ {
					best := float32(math.Inf(-1))
					bi := -1
					for ki := 0; ki < p.kh; ki++ {
						iy := oy*p.sh - p.ph + ki
						if iy < 0 || iy >= p.h {
							continue
						}
						for kj := 0; kj < p.kw; kj++ {
							ix := ox*p.sw - p.pw + kj
							if ix < 0 || ix >= p.w {
								continue
							}
							if v := src[iy*p.w+ix]; bi < 0 || v > best {
								best, bi = v, iy*p.w+ix
							}
						}
					}
					o := (t*p.oh+oy)*p.ow + ox
					ys[o] = best
					is[o] = float32(bi)
				}
			}
		}
	})
}

func (*cpuBackend) maxpoolBack(g, idx, dx *storage, p *convParams) {
	gs, is, ds := g.cpu, idx.cpu, dx.cpu
	parallelFor(p.n*p.c, 1, func(lo, hi int) {
		for t := lo; t < hi; t++ {
			for o := t * p.oh * p.ow; o < (t+1)*p.oh*p.ow; o++ {
				if i := int(is[o]); i >= 0 {
					ds[t*p.h*p.w+i] += gs[o]
				}
			}
		}
	})
}

// hash32 是计数器式随机数用的整数哈希（CPU 与 GPU 使用同一个公式，结果一致）。
func hash32(x uint32) uint32 {
	x ^= x >> 16
	x *= 0x7feb352d
	x ^= x >> 15
	x *= 0x846ca68b
	x ^= x >> 16
	return x
}

func randBits(i uint32, seed uint64) uint32 {
	return hash32(hash32(i+uint32(seed)) ^ uint32(seed>>32))
}

// uniform01 返回 (0, 1] 中的均匀随机数。
func uniform01(i uint32, seed uint64) float32 {
	return (float32(randBits(i, seed)>>8) + 1) * (1.0 / 16777216.0)
}

func (*cpuBackend) random(s *storage, n int, seed uint64, normal bool) {
	d := s.cpu[:n]
	parallelFor(n, grainElem, func(lo, hi int) {
		for i := lo; i < hi; i++ {
			if !normal {
				d[i] = uniform01(uint32(i), seed) - 1.0/16777216.0
				continue
			}
			u1 := uniform01(uint32(2*i), seed)
			u2 := uniform01(uint32(2*i+1), seed)
			d[i] = float32(math.Sqrt(-2*math.Log(float64(u1))) * math.Cos(2*math.Pi*float64(u2)))
		}
	})
}

func (*cpuBackend) adam(p, g, m, v *storage, n int, lr, b1, b2, eps, wd, bc1, bc2 float32, decoupled bool) {
	ps, gs, ms, vs := p.cpu[:n], g.cpu[:n], m.cpu[:n], v.cpu[:n]
	parallelFor(n, grainElem, func(lo, hi int) {
		for i := lo; i < hi; i++ {
			gi := gs[i]
			if decoupled {
				ps[i] -= lr * wd * ps[i]
			} else {
				gi += wd * ps[i]
			}
			ms[i] = b1*ms[i] + (1-b1)*gi
			vs[i] = b2*vs[i] + (1-b2)*gi*gi
			mh := ms[i] / bc1
			vh := vs[i] / bc2
			ps[i] -= lr * mh / (float32(math.Sqrt(float64(vh))) + eps)
		}
	})
}

func (*cpuBackend) sgd(p, g, buf *storage, n int, lr, momentum, wd float32, useBuf bool) {
	ps, gs := p.cpu[:n], g.cpu[:n]
	var bs []float32
	if useBuf {
		bs = buf.cpu[:n]
	}
	parallelFor(n, grainElem, func(lo, hi int) {
		for i := lo; i < hi; i++ {
			gi := gs[i] + wd*ps[i]
			if useBuf {
				bs[i] = momentum*bs[i] + gi
				gi = bs[i]
			}
			ps[i] -= lr * gi
		}
	})
}
