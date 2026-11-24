package rttensor

import (
	"fmt"
	"math"
	"unsafe"

	"aether/assets/rt"
)

// QTensor 是按行量化为 int8 的二维权重 [rows, cols]：W[r, c] ≈ q[r, c] × scale[r]，
// scale[r] = max|W[r, :]| / 127。显存只需要 float32 的四分之一，用于冻结不动的大权重（例如 LoRA 微调时的
// 原模型）。目前只支持 LinearQ8 这一种运算（对输入可以求导，权重不参与求导）。
type QTensor struct {
	rows, cols int
	dev        backend
	q          []int8   // CPU 上的数据
	qbuf       *storage // GPU 上的数据（按字节存放）
	scale      *Tensor  // [rows]
}

// QuantizeInt8 把二维张量按行量化为 int8，结果放在 device 上（没有给出时放在 w 所在的设备上）。
// 在内存中量化再传到 GPU：很大的模型可以先把 float32 的权重读到内存中，再一层一层地量化到显存。
func QuantizeInt8(w *Tensor, device rt.Opt[string]) *QTensor {
	if len(w.shape) != 2 {
		raiseShape("quantize_int8 需要二维的权重，得到形状 %s", shapeStr(w.shape))
	}
	rows, cols := w.shape[0], w.shape[1]
	h := w.host()
	q := make([]int8, rows*cols)
	scales := make([]float32, rows)
	parallelFor(rows, 16, func(lo, hi int) {
		for r := lo; r < hi; r++ {
			row := h[r*cols : (r+1)*cols]
			var m float32
			for _, v := range row {
				m = max(m, float32(math.Abs(float64(v))))
			}
			s := m / 127
			if s == 0 {
				s = 1
			}
			scales[r] = s
			for c, v := range row {
				q[r*cols+c] = int8(math.Round(float64(v / s)))
			}
		}
	})
	dev := w.dev()
	if device.Ok {
		dev = deviceByName(device.V)
	}
	qt := &QTensor{rows: rows, cols: cols, dev: dev, scale: fromHost(dev, []int{rows}, scales)}
	if g, ok := dev.(*gpuBackend); ok {
		// 每 4 个 int8 放进一个 float32 的位置（GPU 内核按字节读取）
		packed := make([]float32, (len(q)+3)/4)
		copy(unsafe.Slice((*int8)(unsafe.Pointer(&packed[0])), len(packed)*4), q)
		qt.qbuf = g.upload(packed)
	} else {
		qt.q = q
	}
	return qt
}

func (q *QTensor) Shape() *rt.List[int] { return &rt.List[int]{S: []int{q.rows, q.cols}} }
func (q *QTensor) DeviceName() string   { return q.dev.name() }
func (q *QTensor) Numel() int           { return q.rows * q.cols }
func (q *QTensor) String() string {
	return fmt.Sprintf("QTensor(int8, shape=[%d, %d], device=%s)", q.rows, q.cols, q.dev.name())
}

// Dequantize 返回 float32 的权重（用于检查量化误差）。
func (q *QTensor) Dequantize() *Tensor {
	s := q.scale.host()
	d := make([]float32, q.rows*q.cols)
	vals := q.q
	if vals == nil {
		packed := q.dev.download(q.qbuf, (q.rows*q.cols+3)/4)
		vals = unsafe.Slice((*int8)(unsafe.Pointer(&packed[0])), len(packed)*4)[:q.rows*q.cols]
	}
	for r := 0; r < q.rows; r++ {
		for c := 0; c < q.cols; c++ {
			d[r*q.cols+c] = float32(vals[r*q.cols+c]) * s[r]
		}
	}
	return fromHost(q.dev, []int{q.rows, q.cols}, d)
}

// LinearQ8 实现 x @ Wᵀ（W 是量化的权重 [out, in]）；对 x 可以求导：dx = (g × scale) @ W。
func LinearQ8(x *Tensor, w *QTensor) *Tensor {
	if len(x.shape) == 0 || x.shape[len(x.shape)-1] != w.cols {
		raiseShape("linear_q8：输入的形状 %s 的最后一维应为 %d", shapeStr(x.shape), w.cols)
	}
	if x.dev() != w.dev {
		rt.Raise("RuntimeError", "linear_q8：输入在 %s 上，量化的权重在 %s 上", x.dev().name(), w.dev.name())
	}
	m := x.n / w.cols
	out := newTensor(x.dev(), append(append([]int(nil), x.shape[:len(x.shape)-1]...), w.rows))
	q8matmul(w, x.st, out.st, m, false)
	out = out.Mul(w.scale)
	return record(out, "linear_q8", []*Tensor{x}, func(g *Tensor) []*Tensor {
		gs := g.Mul(w.scale)
		gx := newTensor(x.dev(), x.shape)
		q8matmul(w, gs.st, gx.st, m, true)
		return []*Tensor{gx}
	})
}

// q8matmul：back 为假时 y[m, rows] = x[m, cols] @ qᵀ，为真时 y[m, cols] = x[m, rows] @ q（都不乘 scale）。
func q8matmul(w *QTensor, x, y *storage, m int, back bool) {
	if g, ok := w.dev.(*gpuBackend); ok {
		n, k := w.rows, w.cols
		if back {
			n, k = w.cols, w.rows
		}
		if m >= 64 && n >= 128 && k >= 16 && n%4 == 0 && k%4 == 0 {
			g.launch("matmul_big4_q8", []int{roundUp(n, 128) / 8, roundUp(m, 128) / 8, 1}, []int{16, 16, 1}, x, w.qbuf, y, m, n, k, !back, 0, 0)
			return
		}
		g.launch("matmul_q8", []int{roundUp(n, 64) / 4, roundUp(m, 64) / 4, 1}, []int{16, 16, 1}, x, w.qbuf, y, m, n, k, !back)
		return
	}
	xs, ys, q := x.cpu, y.cpu, w.q
	if back {
		parallelFor(m, 1, func(lo, hi int) {
			for i := lo; i < hi; i++ {
				out := ys[i*w.cols : (i+1)*w.cols]
				clear(out)
				for r, v := range xs[i*w.rows : (i+1)*w.rows] {
					if v == 0 {
						continue
					}
					for c, qv := range q[r*w.cols : (r+1)*w.cols] {
						out[c] += v * float32(qv)
					}
				}
			}
		})
		return
	}
	parallelFor(m*w.rows, 64, func(lo, hi int) {
		for t := lo; t < hi; t++ {
			i, r := t/w.rows, t%w.rows
			row := xs[i*w.cols : (i+1)*w.cols]
			var s float32
			for c, qv := range q[r*w.cols : (r+1)*w.cols] {
				s += row[c] * float32(qv)
			}
			ys[t] = s
		}
	})
}
