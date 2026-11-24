package rttensor

// backend 是一个计算设备（CPU 或 GPU）。所有存储都是连续的 float32 数组；
// 运算的编号在 CPU 实现与 GPU 内核中相同（见 kernels.go）。
type backend interface {
	name() string
	alloc(n int) *storage
	upload(d []float32) *storage
	download(s *storage, n int) []float32
	fill(s *storage, n int, v float32)
	copyTo(dst, src *storage, n int)
	// y = op(x)，参数 a、b 的含义见 unary 运算的编号
	unary(op int, x, y *storage, n int, a, b float32)
	// out = op(x, y)；bc 为 nil 表示形状相同，否则按广播的形状与步长计算
	binary(op int, x, y, out *storage, n int, bc *bcast)
	// 把 x 视为 [outer, dim, inner]，沿中间一维归约到 out [outer, inner]；max 时 idx 记录位置
	reduce(op int, x, out, idx *storage, outer, dim, inner int)
	// c[b] = op(a[b]) @ op(b[b])：a 为 [m, k]（ta 时为 [k, m]），b 为 [k, n]（tb 时为 [n, k]）；
	// sa、sb 是批次之间的步长（0 表示所有批次共用同一个矩阵）
	matmul(a, b, c *storage, batch, m, k, n int, ta, tb bool, sa, sb int)
	softmax(x, y *storage, rows, cols int, log bool)
	// 层归一化（每行 d 个数）：y = xhat * w + b，并保存 xhat 与每行的 1/σ（rstd）供反向使用
	layerNorm(x, w, b, y, xhat, rstd *storage, rows, d int, eps float32)
	// 层归一化的反向（对 x）：dx = rstd * (g - mean(g) - xhat * mean(g * xhat))，其中 g = dy * w
	layerNormBack(dy, w, xhat, rstd, dx *storage, rows, d int)
	// RMSNorm（每行 d 个数）：y = x / sqrt(mean(x²) + eps) * w，保存 xhat 与 rstd；
	// 反向：dx = rstd * (g - xhat * mean(g * xhat))，其中 g = dy * w
	rmsNorm(x, w, y, xhat, rstd *storage, rows, d int, eps float32)
	rmsNormBack(dy, w, xhat, rstd, dx *storage, rows, d int)
	permute(x, y *storage, shape, perm []int)
	narrow(x, y *storage, outer, dim, inner, start, length int)
	place(x, y *storage, outer, dim, inner, start, length int)
	indexSelect(w, idx, out *storage, nidx, rows, cols int)
	indexAdd(g, idx, out *storage, nidx, rows, cols int)
	pick(x, idx, out *storage, rows, cols int)
	pickBack(g, idx, out *storage, rows, cols int)
	im2col(x, cols *storage, p *convParams)
	col2im(cols, x *storage, p *convParams)
	maxpool(x, y, idx *storage, p *convParams)
	maxpoolBack(g, idx, dx *storage, p *convParams)
	random(s *storage, n int, seed uint64, normal bool)
	// y[i] = 第 i 个随机数 ≥ p 时 x[i] * scale，否则 0（随机数由 seed 决定，反向时用同一个 seed 重新生成）
	dropout(x, y *storage, n int, seed uint64, p, scale float32)
	adam(p, g, m, v *storage, n int, lr, b1, b2, eps, wd, bc1, bc2 float32, decoupled bool)
	sgd(p, g, buf *storage, n int, lr, momentum, wd float32, useBuf bool)
}

// 一元运算的编号（a、b 是参数）
const (
	uNeg = iota
	uExp
	uLog
	uSqrt
	uAbs
	uRelu
	uSigmoid
	uTanh
	uGelu
	uSin
	uCos
	uAddS  // x + a
	uMulS  // x * a
	uRSubS // a - x
	uRDivS // a / x
	uPowS  // x ** a
	uClamp // 限制在 [a, b]
	uLeaky // x > 0 ? x : a * x
	uSquare
	uRecip
	uSign
	uGtS       // x > a ? 1 : 0
	uGeluGrad  // gelu'(x)
	uSigDeriv  // 由输出 y 计算 sigmoid'：y * (1 - y)
	uTanhDeriv // 由输出 y 计算 tanh'：1 - y²
	uLeakyGrad // x > 0 ? 1 : a
	uPowSGrad  // a * x ** (a - 1)
	uLtS       // x < a ? 1 : 0
	uSilu      // x * sigmoid(x)
	uSiluGrad  // silu'(x)
)

// 二元运算的编号
const (
	bAdd = iota
	bSub
	bMul
	bDiv
	bPow
	bMax
	bMin
	bGt
	bLt
	bEq
	bGe
	bLe
)

// 归约运算的编号
const (
	rSum = iota
	rMax
	rMin
)

const maxDims = 8

// bcast 描述广播：输出的形状，以及两个输入在每一维上的步长（被广播的维为 0）。
type bcast struct {
	ndim   int
	shape  [maxDims]int32
	sx, sy [maxDims]int32
}

// convParams 描述二维卷积或池化的尺寸。
type convParams struct {
	n, c, h, w     int // 输入
	kh, kw         int // 卷积核
	sh, sw, ph, pw int // 步长与填充
	oh, ow         int // 输出
}

func (p *convParams) colRows() int { return p.c * p.kh * p.kw }
func (p *convParams) colCols() int { return p.oh * p.ow }

// broadcastShapes 计算两个形状广播后的形状与各自的步长。
func broadcastShapes(op string, a, b []int) ([]int, *bcast) {
	nd := len(a)
	if len(b) > nd {
		nd = len(b)
	}
	if nd > maxDims {
		raiseShape("%s：最多支持 %d 维的广播", op, maxDims)
	}
	out := make([]int, nd)
	bc := &bcast{ndim: nd}
	sa, sb := 1, 1
	for i := nd - 1; i >= 0; i-- {
		da, db := 1, 1
		if j := i - (nd - len(a)); j >= 0 {
			da = a[j]
		}
		if j := i - (nd - len(b)); j >= 0 {
			db = b[j]
		}
		switch {
		case da == db:
			out[i] = da
		case da == 1:
			out[i] = db
		case db == 1:
			out[i] = da
		default:
			raiseShape("%s：形状 %s 与 %s 不能广播（第 %d 维：%d 和 %d）", op, shapeStr(a), shapeStr(b), i, da, db)
		}
		bc.shape[i] = int32(out[i])
		if da != 1 {
			bc.sx[i] = int32(sa)
		}
		if db != 1 {
			bc.sy[i] = int32(sb)
		}
		sa *= da
		sb *= db
	}
	return out, bc
}
