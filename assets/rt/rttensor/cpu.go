package rttensor

import (
	"math"
	"runtime"
	"sync"
	"unsafe"
)

// cpuBackend 用 Go 代码计算；数据量大时把工作分给多个协程。
type cpuBackend struct{}

var cpuDev backend = &cpuBackend{}

func (*cpuBackend) name() string { return "cpu" }

// alloc 返回未初始化的存储（与 GPU 后端相同：调用者负责写入每个元素）。
func (c *cpuBackend) alloc(n int) *storage { return &storage{dev: c, cpu: cpuBuffer(n)} }

func (c *cpuBackend) upload(d []float32) *storage {
	s := c.alloc(len(d))
	copy(s.cpu, d)
	return s
}

// 大块内存的缓存池：不再使用的数组被垃圾回收时（数组本身的终结器）放回池中，之后直接复用，
// 省去清零与操作系统分配新页面的开销。终结器挂在数组上而不是 storage 上，所以只要还有
// 切片引用这个数组（例如正在运行的运算），它就不会被放回池中。
var cpuPool = struct {
	sync.Mutex
	free  map[int][][]float32
	bytes int
}{free: map[int][][]float32{}}

const (
	cpuPoolMin   = 1 << 12   // 至少这么多个元素才使用缓存池
	cpuPoolLimit = 512 << 20 // 池中最多保留的字节数
)

func cpuBuffer(n int) []float32 {
	if n < cpuPoolMin {
		return make([]float32, n)
	}
	size := sizeClass(n*4) / 4
	cpuPool.Lock()
	var buf []float32
	if l := cpuPool.free[size]; len(l) > 0 {
		buf = l[len(l)-1]
		l[len(l)-1] = nil
		cpuPool.free[size] = l[:len(l)-1]
		cpuPool.bytes -= size * 4
	}
	cpuPool.Unlock()
	if buf == nil {
		buf = make([]float32, size)
	}
	runtime.SetFinalizer(&buf[0], func(p *float32) {
		cpuPool.Lock()
		defer cpuPool.Unlock()
		if cpuPool.bytes+size*4 <= cpuPoolLimit {
			cpuPool.free[size] = append(cpuPool.free[size], unsafe.Slice(p, size))
			cpuPool.bytes += size * 4
		}
	})
	return buf[:n]
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
	return 0.5 * x * (1 + tanhf(0.7978845608028654*(x+0.044715*x*x*x)))
}

func geluGradF(x float32) float32 {
	t := tanhf(0.7978845608028654 * (x + 0.044715*x*x*x))
	du := 0.7978845608028654 * (1 + 3*0.044715*x*x)
	return 0.5*(1+t) + 0.5*x*(1-t*t)*du
}

func unaryFunc(op int, a, b float32) func(float32) float32 {
	switch op {
	case uNeg:
		return func(x float32) float32 { return -x }
	case uExp:
		return expf
	case uLog:
		return func(x float32) float32 { return float32(math.Log(float64(x))) }
	case uSqrt:
		return func(x float32) float32 { return float32(math.Sqrt(float64(x))) }
	case uAbs:
		return func(x float32) float32 { return float32(math.Abs(float64(x))) }
	case uRelu:
		return func(x float32) float32 { return max(x, 0) }
	case uSigmoid:
		return sigmoidf
	case uTanh:
		return tanhf
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
	case uSilu:
		return siluf
	case uSiluGrad:
		return siluGradf
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
	xs, ys := x.cpu[:n], y.cpu[:n]
	parallelFor(n, grainElem, func(lo, hi int) {
		xs, ys := xs[lo:hi], ys[lo:hi]
		// 最常用的几种运算直接写成循环（避免每个元素一次函数调用）
		switch op {
		case uRelu:
			for i, v := range xs {
				ys[i] = max(v, 0)
			}
		case uAddS:
			for i, v := range xs {
				ys[i] = v + a
			}
		case uMulS:
			for i, v := range xs {
				ys[i] = v * a
			}
		case uNeg:
			for i, v := range xs {
				ys[i] = -v
			}
		case uGtS:
			for i, v := range xs {
				ys[i] = b2f(v > a)
			}
		case uRSubS:
			for i, v := range xs {
				ys[i] = a - v
			}
		default:
			f := unaryFunc(op, a, b)
			for i, v := range xs {
				ys[i] = f(v)
			}
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
	if n == 0 {
		return
	}
	// 广播：最内层一维单独处理（两个输入在这一维上的步长只能是 0 或 1），外层各维用里程表递增
	nd := bc.ndim
	inner := int(bc.shape[nd-1])
	ix, iy := int(bc.sx[nd-1]), int(bc.sy[nd-1])
	rows := n / max(inner, 1)
	f := binaryFunc(op)
	parallelFor(rows, max(1, grainElem/max(inner, 1)), func(lo, hi int) {
		var idx [maxDims]int
		rem := lo
		ox, oy := 0, 0
		for d := nd - 2; d >= 0; d-- {
			s := int(bc.shape[d])
			idx[d] = rem % s
			rem /= s
			ox += idx[d] * int(bc.sx[d])
			oy += idx[d] * int(bc.sy[d])
		}
		for r := lo; r < hi; r++ {
			out := os[r*inner : (r+1)*inner]
			binaryRun(op, f, out, xs, ys, ox, oy, ix, iy)
			for d := nd - 2; d >= 0; d-- {
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

// binaryRun 计算一段：out[j] = op(x[ox + j*ix], y[oy + j*iy])，ix、iy 为 0 或 1。
func binaryRun(op int, f func(x, y float32) float32, out, xs, ys []float32, ox, oy, ix, iy int) {
	n := len(out)
	switch {
	case ix == 1 && iy == 1:
		a, b := xs[ox:ox+n], ys[oy:oy+n]
		switch op {
		case bAdd:
			for j := range out {
				out[j] = a[j] + b[j]
			}
		case bSub:
			for j := range out {
				out[j] = a[j] - b[j]
			}
		case bMul:
			for j := range out {
				out[j] = a[j] * b[j]
			}
		default:
			for j := range out {
				out[j] = f(a[j], b[j])
			}
		}
	case ix == 1:
		a, v := xs[ox:ox+n], ys[oy]
		switch op {
		case bAdd:
			for j := range out {
				out[j] = a[j] + v
			}
		case bSub:
			for j := range out {
				out[j] = a[j] - v
			}
		case bMul:
			for j := range out {
				out[j] = a[j] * v
			}
		case bDiv:
			for j := range out {
				out[j] = a[j] / v
			}
		default:
			for j := range out {
				out[j] = f(a[j], v)
			}
		}
	case iy == 1:
		u, b := xs[ox], ys[oy:oy+n]
		switch op {
		case bAdd:
			for j := range out {
				out[j] = u + b[j]
			}
		case bMul:
			for j := range out {
				out[j] = u * b[j]
			}
		default:
			for j := range out {
				out[j] = f(u, b[j])
			}
		}
	default:
		v := f(xs[ox], ys[oy])
		for j := range out {
			out[j] = v
		}
	}
}

func sumRange(xs []float32) float64 {
	var s0, s1, s2, s3 float64
	i := 0
	for ; i+4 <= len(xs); i += 4 {
		s0 += float64(xs[i])
		s1 += float64(xs[i+1])
		s2 += float64(xs[i+2])
		s3 += float64(xs[i+3])
	}
	for ; i < len(xs); i++ {
		s0 += float64(xs[i])
	}
	return (s0 + s1) + (s2 + s3)
}

func (*cpuBackend) reduce(op int, x, out, idx *storage, outer, dim, inner int) {
	xs, os := x.cpu, out.cpu
	if op == rSum && inner == 1 && dim >= 1<<14 && outer < runtime.GOMAXPROCS(0) {
		// 少数几段很长的连续数据：每段分成固定大小的块并行求和（块的划分与线程数无关，结果确定）
		const chunk = 1 << 13
		nc := ceilDiv(dim, chunk)
		parts := make([]float64, outer*nc)
		parallelFor(outer*nc, 1, func(lo, hi int) {
			for t := lo; t < hi; t++ {
				o, c := t/nc, t%nc
				parts[t] = sumRange(xs[o*dim+c*chunk : o*dim+min((c+1)*chunk, dim)])
			}
		})
		for o := 0; o < outer; o++ {
			var s float64
			for _, v := range parts[o*nc : (o+1)*nc] {
				s += v
			}
			os[o] = float32(s)
		}
		return
	}
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
	if haveSIMD && m*n*k >= 4096 {
		matmulSIMD(as, bs, cs, batch, m, k, n, ta, tb, sa, sb)
		return
	}
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
				e := expf(v - mx)
				sum += float64(e)
				if !log {
					out[j] = e
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

func (*cpuBackend) layerNorm(x, w, b, y, xhat, rstd *storage, rows, d int, eps float32) {
	xs, ws, bs, ys, hs, rs := x.cpu, w.cpu, b.cpu, y.cpu, xhat.cpu, rstd.cpu
	parallelFor(rows, max(1, 4096/max(d, 1)), func(lo, hi int) {
		for r := lo; r < hi; r++ {
			row := xs[r*d : (r+1)*d]
			mean := sumRange(row) / float64(d)
			var v float64
			for _, x := range row {
				dv := float64(x) - mean
				v += dv * dv
			}
			inv := float32(1 / math.Sqrt(v/float64(d)+float64(eps)))
			rs[r] = inv
			h, out := hs[r*d:(r+1)*d], ys[r*d:(r+1)*d]
			m := float32(mean)
			for j, x := range row {
				xh := (x - m) * inv
				h[j] = xh
				out[j] = xh*ws[j] + bs[j]
			}
		}
	})
}

func (*cpuBackend) layerNormBack(dy, w, xhat, rstd, dx *storage, rows, d int) {
	gs, ws, hs, rs, ds := dy.cpu, w.cpu, xhat.cpu, rstd.cpu, dx.cpu
	parallelFor(rows, max(1, 4096/max(d, 1)), func(lo, hi int) {
		for r := lo; r < hi; r++ {
			g, h, out := gs[r*d:(r+1)*d], hs[r*d:(r+1)*d], ds[r*d:(r+1)*d]
			var sg, sgh float64
			for j := range g {
				gw := g[j] * ws[j]
				sg += float64(gw)
				sgh += float64(gw * h[j])
			}
			mg, mgh := float32(sg/float64(d)), float32(sgh/float64(d))
			inv := rs[r]
			for j := range g {
				out[j] = inv * (g[j]*ws[j] - mg - h[j]*mgh)
			}
		}
	})
}

func (*cpuBackend) rmsNorm(x, w, y, xhat, rstd *storage, rows, d int, eps float32) {
	xs, ws, ys, hs, rs := x.cpu, w.cpu, y.cpu, xhat.cpu, rstd.cpu
	parallelFor(rows, max(1, 4096/max(d, 1)), func(lo, hi int) {
		for r := lo; r < hi; r++ {
			row := xs[r*d : (r+1)*d]
			var ss float64
			for _, v := range row {
				ss += float64(v) * float64(v)
			}
			inv := float32(1 / math.Sqrt(ss/float64(d)+float64(eps)))
			rs[r] = inv
			h, out := hs[r*d:(r+1)*d], ys[r*d:(r+1)*d]
			for j, v := range row {
				xh := v * inv
				h[j] = xh
				out[j] = xh * ws[j]
			}
		}
	})
}

func (*cpuBackend) rmsNormBack(dy, w, xhat, rstd, dx *storage, rows, d int) {
	gs, ws, hs, rs, ds := dy.cpu, w.cpu, xhat.cpu, rstd.cpu, dx.cpu
	parallelFor(rows, max(1, 4096/max(d, 1)), func(lo, hi int) {
		for r := lo; r < hi; r++ {
			g, h, out := gs[r*d:(r+1)*d], hs[r*d:(r+1)*d], ds[r*d:(r+1)*d]
			var sgh float64
			for j := range g {
				sgh += float64(g[j] * ws[j] * h[j])
			}
			mgh := float32(sgh / float64(d))
			inv := rs[r]
			for j := range g {
				out[j] = inv * (g[j]*ws[j] - h[j]*mgh)
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
	// 末尾没有变动的维在输入、输出中都是连续的：按“连续段”整段复制
	keep := nd
	for keep > 0 && perm[keep-1] == keep-1 {
		keep--
	}
	run := 1
	for d := keep; d < nd; d++ {
		run *= shape[d]
	}
	outShape := make([]int, keep)
	strides := make([]int, keep) // 输出第 d 维对应的输入步长
	for d := 0; d < keep; d++ {
		outShape[d] = shape[perm[d]]
		strides[d] = inStride[perm[d]]
	}
	segs := numel(shape) / max(run, 1)
	if numel(shape) == 0 {
		return
	}
	xs, ys := x.cpu, y.cpu
	parallelFor(segs, max(1, grainElem/max(run, 1)), func(lo, hi int) {
		// 由段号 lo 计算各维下标，然后像里程表一样递增
		var idx [maxDims]int
		rem, off := lo, 0
		for d := keep - 1; d >= 0; d-- {
			idx[d] = rem % outShape[d]
			rem /= outShape[d]
			off += idx[d] * strides[d]
		}
		for sg := lo; sg < hi; sg++ {
			if run == 1 {
				ys[sg] = xs[off]
			} else {
				copy(ys[sg*run:(sg+1)*run], xs[off:off+run])
			}
			for d := keep - 1; d >= 0; d-- {
				idx[d]++
				off += strides[d]
				if idx[d] < outShape[d] {
					break
				}
				off -= idx[d] * strides[d]
				idx[d] = 0
			}
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
	c, h, w, kh, kw := p.c, p.h, p.w, p.kh, p.kw
	sh, sw, ph, pw, oh, ow := p.sh, p.sw, p.ph, p.pw, p.oh, p.ow
	rowsPer, colsPer := p.colRows(), p.colCols()
	parallelFor(p.n*rowsPer, 16, func(lo, hi int) {
		for t := lo; t < hi; t++ {
			b, r := t/rowsPer, t%rowsPer
			ci, kr := r/(kh*kw), r%(kh*kw)
			ki, kj := kr/kw, kr%kw
			dst := cs[(b*rowsPer+r)*colsPer : (b*rowsPer+r+1)*colsPer]
			src := xs[(b*c+ci)*h*w : (b*c+ci+1)*h*w]
			// 步长为 1 时每一行中有效的部分是连续的：ox ∈ [x0, x1)
			x0, x1 := max(0, pw-kj), min(ow, w+pw-kj)
			for oy := 0; oy < oh; oy++ {
				iy := oy*sh - ph + ki
				row := dst[oy*ow : oy*ow+ow]
				if iy < 0 || iy >= h {
					clear(row)
					continue
				}
				srow := src[iy*w : iy*w+w]
				if sw == 1 {
					if x0 >= x1 {
						clear(row)
						continue
					}
					clear(row[:x0])
					copy(row[x0:x1], srow[x0-pw+kj:x1-pw+kj])
					clear(row[x1:])
					continue
				}
				for ox := range row {
					ix := ox*sw - pw + kj
					if ix >= 0 && ix < w {
						row[ox] = srow[ix]
					} else {
						row[ox] = 0
					}
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
	h, w, kh, kw := p.h, p.w, p.kh, p.kw
	sh, sw, ph, pw, oh, ow := p.sh, p.sw, p.ph, p.pw, p.oh, p.ow
	parallelFor(p.n*p.c, max(1, 4096/max(1, oh*ow*kh*kw)), func(lo, hi int) {
		for t := lo; t < hi; t++ {
			src := xs[t*h*w : (t+1)*h*w]
			out := ys[t*oh*ow : (t+1)*oh*ow]
			outIdx := is[t*oh*ow : (t+1)*oh*ow]
			for oy := 0; oy < oh; oy++ {
				y0 := oy*sh - ph
				ky0, ky1 := max(0, -y0), min(kh, h-y0)
				for ox := 0; ox < ow; ox++ {
					x0 := ox*sw - pw
					kx0, kx1 := max(0, -x0), min(kw, w-x0)
					best := float32(math.Inf(-1))
					bi := -1
					for ki := ky0; ki < ky1; ki++ {
						off := (y0+ki)*w + x0
						for kj := kx0; kj < kx1; kj++ {
							if v := src[off+kj]; bi < 0 || v > best {
								best, bi = v, off+kj
							}
						}
					}
					out[oy*ow+ox] = best
					outIdx[oy*ow+ox] = float32(bi)
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

func (*cpuBackend) dropout(x, y *storage, n int, seed uint64, p, scale float32) {
	xs, ys := x.cpu[:n], y.cpu[:n]
	parallelFor(n, grainElem, func(lo, hi int) {
		for i := lo; i < hi; i++ {
			if uniform01(uint32(i), seed)-1.0/16777216.0 >= p {
				ys[i] = xs[i] * scale
			} else {
				ys[i] = 0
			}
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
