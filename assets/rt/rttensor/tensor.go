// Package rttensor 实现 Aether 的张量库（tensor 模块）：张量、自动求导、神经网络运算，
// 以及 CPU 与 GPU 两个后端。GPU 后端使用 OpenCL（NVIDIA、AMD、Intel 的显卡驱动都自带），
// 内核在运行时由驱动编译，不需要安装 CUDA 或 ROCm。只在程序导入 tensor 时才会链接。
package rttensor

import (
	"fmt"
	"math"
	"strings"

	"aether/assets/rt"
)

// Tensor 是 float32 的多维数组（行优先、连续存储）。reshape 等改变形状的操作与原张量共享存储。
type Tensor struct {
	shape        []int
	n            int
	st           *storage
	requiresGrad bool
	grad         *Tensor
	fn           *node
}

// storage 是一块设备上的数据。
type storage struct {
	dev  backend
	cpu  []float32 // CPU 后端
	gbuf *gpuBuf   // GPU 后端
}

func numel(shape []int) int {
	n := 1
	for _, d := range shape {
		n *= d
	}
	return n
}

func checkShape(shape []int) {
	for _, d := range shape {
		if d < 0 {
			rt.Raise("ValueError", "张量的形状不能是负数：%v", shapeStr(shape))
		}
	}
}

func shapeStr(shape []int) string {
	parts := make([]string, len(shape))
	for i, d := range shape {
		parts[i] = fmt.Sprint(d)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// newTensor 在设备 dev 上创建形状为 shape 的张量（内容未初始化）。
func newTensor(dev backend, shape []int) *Tensor {
	checkShape(shape)
	n := numel(shape)
	return &Tensor{shape: append([]int(nil), shape...), n: n, st: dev.alloc(n)}
}

func (t *Tensor) dev() backend { return t.st.dev }

// view 返回共享存储、形状不同的张量（不记录求导信息）。
func (t *Tensor) view(shape []int) *Tensor {
	return &Tensor{shape: append([]int(nil), shape...), n: t.n, st: t.st}
}

func (t *Tensor) host() []float32 { return t.dev().download(t.st, t.n) }

func sameShape(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// normDim 把可能为负的维度下标转换为非负下标。
func normDim(dim, ndim int, what string) int {
	if dim < 0 {
		dim += ndim
	}
	if dim < 0 || dim >= ndim {
		rt.Raise("IndexError", "%s的维度 %d 超出范围（张量有 %d 维）", what, dim, ndim)
	}
	return dim
}

func sameDevice(op string, ts ...*Tensor) backend {
	dev := ts[0].dev()
	for _, t := range ts[1:] {
		if t.dev() != dev {
			rt.Raise("RuntimeError", "%s：张量在不同的设备上（%s 和 %s）；请先用 .to(...) 移到同一设备", op, dev.name(), t.dev().name())
		}
	}
	return dev
}

// ===== 创建 =====

var defaultDev backend = cpuDev

// SetDefaultDevice 设置之后创建张量时使用的设备："cpu" 或 "gpu"。
func SetDefaultDevice(name string) { defaultDev = deviceByName(name) }

func DefaultDevice() string { return defaultDev.name() }

func deviceByName(name string) backend {
	switch strings.ToLower(name) {
	case "cpu":
		return cpuDev
	case "gpu", "cuda", "opencl", "rocm":
		g := gpu()
		if g == nil {
			rt.Raise("RuntimeError", "没有可用的 GPU：%s", gpuError())
		}
		return g
	}
	rt.Raise("ValueError", "未知的设备 %s（可以是 \"cpu\" 或 \"gpu\"）", rt.QuoteStr(name))
	return nil
}

func shapeOf(l *rt.List[int]) []int { return append([]int(nil), l.S...) }

func Zeros(shape *rt.List[int]) *Tensor {
	t := newTensor(defaultDev, shapeOf(shape))
	t.dev().fill(t.st, t.n, 0)
	return t
}

func Ones(shape *rt.List[int]) *Tensor {
	t := newTensor(defaultDev, shapeOf(shape))
	t.dev().fill(t.st, t.n, 1)
	return t
}

func Full(shape *rt.List[int], v float64) *Tensor {
	t := newTensor(defaultDev, shapeOf(shape))
	t.dev().fill(t.st, t.n, float32(v))
	return t
}

func full(dev backend, shape []int, v float32) *Tensor {
	t := newTensor(dev, shape)
	dev.fill(t.st, t.n, v)
	return t
}

func fromHost(dev backend, shape []int, data []float32) *Tensor {
	if numel(shape) != len(data) {
		rt.Raise("ValueError", "数据有 %d 个元素，与形状 %s 不一致", len(data), shapeStr(shape))
	}
	return &Tensor{shape: append([]int(nil), shape...), n: len(data), st: dev.upload(data)}
}

// FromList 实现 tensor.tensor(data, shape=null)：由一维的数据（和可选的形状）创建张量。
func FromList(data *rt.List[float64], shape rt.Opt[*rt.List[int]]) *Tensor {
	d := make([]float32, len(data.S))
	for i, v := range data.S {
		d[i] = float32(v)
	}
	sh := []int{len(d)}
	if shape.Ok {
		sh = shapeOf(shape.V)
	}
	return fromHost(defaultDev, sh, d)
}

// FromInts 由整数列表创建张量（例如类别标签）。
func FromInts(data *rt.List[int], shape rt.Opt[*rt.List[int]]) *Tensor {
	d := make([]float32, len(data.S))
	for i, v := range data.S {
		d[i] = float32(v)
	}
	sh := []int{len(d)}
	if shape.Ok {
		sh = shapeOf(shape.V)
	}
	return fromHost(defaultDev, sh, d)
}

// Scalar 创建零维张量。
func Scalar(v float64) *Tensor { return fromHost(defaultDev, nil, []float32{float32(v)}) }

// FromRows 由二维列表创建形状为 [行数, 列数] 的张量。
func FromRows(rows *rt.List[*rt.List[float64]]) *Tensor {
	if len(rows.S) == 0 {
		return fromHost(defaultDev, []int{0, 0}, nil)
	}
	cols := len(rows.S[0].S)
	d := make([]float32, 0, len(rows.S)*cols)
	for i, r := range rows.S {
		if len(r.S) != cols {
			rt.Raise("ValueError", "第 %d 行有 %d 个元素，第 0 行有 %d 个；每一行的长度必须相同", i, len(r.S), cols)
		}
		for _, v := range r.S {
			d = append(d, float32(v))
		}
	}
	return fromHost(defaultDev, []int{len(rows.S), cols}, d)
}

// Arange 返回 [start, stop) 中步长为 step 的数。
func Arange(start, stop, step float64) *Tensor {
	if step == 0 {
		rt.Raise("ValueError", "arange 的步长不能为 0")
	}
	var d []float32
	for v := start; (step > 0 && v < stop) || (step < 0 && v > stop); v += step {
		d = append(d, float32(v))
	}
	return fromHost(defaultDev, []int{len(d)}, d)
}

func Linspace(start, stop float64, n int) *Tensor {
	d := make([]float32, n)
	for i := range d {
		if n == 1 {
			d[i] = float32(start)
		} else {
			d[i] = float32(start + (stop-start)*float64(i)/float64(n-1))
		}
	}
	return fromHost(defaultDev, []int{n}, d)
}

func Eye(n int) *Tensor {
	d := make([]float32, n*n)
	for i := 0; i < n; i++ {
		d[i*n+i] = 1
	}
	return fromHost(defaultDev, []int{n, n}, d)
}

func ZerosLike(t *Tensor) *Tensor { return full(t.dev(), t.shape, 0) }
func OnesLike(t *Tensor) *Tensor  { return full(t.dev(), t.shape, 1) }

// ===== 属性 =====

func (t *Tensor) Shape() *rt.List[int] { return &rt.List[int]{S: append([]int(nil), t.shape...)} }
func (t *Tensor) Ndim() int            { return len(t.shape) }
func (t *Tensor) Numel() int           { return t.n }
func (t *Tensor) DeviceName() string   { return t.dev().name() }
func (t *Tensor) RequiresGrad() bool   { return t.requiresGrad }
func (t *Tensor) IsLeaf() bool         { return t.fn == nil }

func (t *Tensor) Size(dim int) int {
	return t.shape[normDim(dim, len(t.shape), "size ")]
}

func (t *Tensor) Grad() rt.Opt[*Tensor] {
	if t.grad == nil {
		return rt.Opt[*Tensor]{}
	}
	return rt.Opt[*Tensor]{V: t.grad, Ok: true}
}

// SetRequiresGrad 实现 requires_grad_(flag)：返回张量本身。
func (t *Tensor) SetRequiresGrad(flag bool) *Tensor {
	if t.fn != nil && !flag {
		rt.Raise("RuntimeError", "只能修改叶子张量的 requires_grad；对计算结果请使用 detach()")
	}
	t.requiresGrad = flag
	return t
}

// ZeroGrad 清除梯度。
func (t *Tensor) ZeroGrad() { t.grad = nil }

func (t *Tensor) Item() float64 {
	if t.n != 1 {
		rt.Raise("ValueError", "item() 只能用于只有一个元素的张量，这个张量的形状是 %s", shapeStr(t.shape))
	}
	return float64(t.host()[0])
}

// ToList 返回所有元素（按行优先展平）。
func (t *Tensor) ToList() *rt.List[float64] {
	h := t.host()
	out := make([]float64, len(h))
	for i, v := range h {
		out[i] = float64(v)
	}
	return &rt.List[float64]{S: out}
}

// Rows 把二维张量转换为列表的列表。
func (t *Tensor) Rows() *rt.List[*rt.List[float64]] {
	if len(t.shape) != 2 {
		rt.Raise("ValueError", "rows() 需要二维张量，得到形状 %s", shapeStr(t.shape))
	}
	h := t.host()
	out := &rt.List[*rt.List[float64]]{}
	for i := 0; i < t.shape[0]; i++ {
		row := make([]float64, t.shape[1])
		for j := range row {
			row[j] = float64(h[i*t.shape[1]+j])
		}
		out.S = append(out.S, &rt.List[float64]{S: row})
	}
	return out
}

func (t *Tensor) Len() int {
	if len(t.shape) == 0 {
		rt.Raise("TypeError", "零维张量没有长度")
	}
	return t.shape[0]
}

// ===== 设备 =====

// To 返回位于指定设备上的张量（已经在该设备上时返回自身）。
func (t *Tensor) To(name string) *Tensor { return t.toDev(deviceByName(name)) }
func (t *Tensor) Cpu() *Tensor           { return t.toDev(cpuDev) }
func (t *Tensor) Gpu() *Tensor           { return t.To("gpu") }

func (t *Tensor) toDev(dev backend) *Tensor {
	if t.dev() == dev {
		return t
	}
	out := fromHost(dev, t.shape, t.host())
	record(out, "to", []*Tensor{t}, func(g *Tensor) []*Tensor { return []*Tensor{g.toDev(t.dev())} })
	return out
}

// moveTo 把张量的存储原地移到另一个设备（Module.to 使用：参数对象保持不变，优化器仍然引用它们）。
func (t *Tensor) moveTo(dev backend) {
	if t.dev() == dev {
		return
	}
	t.st = dev.upload(t.host())
	if t.grad != nil {
		t.grad.moveTo(dev)
	}
}

// ===== 显示 =====

func (t *Tensor) String() string {
	h := t.host()
	var sb strings.Builder
	sb.WriteString("tensor(")
	if len(t.shape) == 0 {
		sb.WriteString(fmtElem(h[0]))
	} else if t.n == 0 {
		sb.WriteString("[]，形状 " + shapeStr(t.shape))
	} else {
		t.writeNested(&sb, h, 0, 0, len("tensor("))
	}
	if t.dev() != cpuDev {
		sb.WriteString(", device=" + t.dev().name())
	}
	if t.requiresGrad {
		if t.fn != nil {
			sb.WriteString(", grad_fn=" + t.fn.name)
		} else {
			sb.WriteString(", requires_grad=true")
		}
	}
	sb.WriteString(")")
	return sb.String()
}

func fmtElem(v float32) string {
	f := float64(v)
	switch {
	case math.IsNaN(f):
		return "nan"
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	}
	return fmt.Sprintf("%.4f", f)
}

func (t *Tensor) writeNested(sb *strings.Builder, h []float32, dim, off, indent int) {
	sb.WriteByte('[')
	size := t.shape[dim]
	stride := numel(t.shape[dim+1:])
	// 元素很多时只显示开头和结尾各 3 个
	idx := make([]int, 0, size)
	if size > 12 {
		idx = append(idx, 0, 1, 2, -1, size-3, size-2, size-1)
	} else {
		for i := 0; i < size; i++ {
			idx = append(idx, i)
		}
	}
	for k, i := range idx {
		if k > 0 {
			if dim < len(t.shape)-1 {
				sb.WriteString(",")
				sb.WriteString(strings.Repeat("\n", len(t.shape)-1-dim))
				sb.WriteString(strings.Repeat(" ", indent+dim+1))
			} else {
				sb.WriteString(", ")
			}
		}
		switch {
		case i < 0:
			sb.WriteString("...")
		case dim == len(t.shape)-1:
			sb.WriteString(fmtElem(h[off+i]))
		default:
			t.writeNested(sb, h, dim+1, off+i*stride, indent)
		}
	}
	sb.WriteByte(']')
}
