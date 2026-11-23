package rttensor

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"

	"aether/assets/rt"
)

// Optimizer 实现 SGD、Adam 与 AdamW。每一步对每个参数只调用一次融合的更新内核
// （在 GPU 上不需要把数据传回内存）。
type Optimizer struct {
	kind      string
	params    []*Tensor
	lr        float64
	momentum  float64
	wd        float64
	b1, b2    float64
	eps       float64
	decoupled bool
	steps     int
	m, v      []*Tensor // Adam 的一阶、二阶矩；SGD 的动量缓冲区放在 m 中
}

func optimParams(params *rt.List[*Tensor], name string) []*Tensor {
	if len(params.S) == 0 {
		rt.Raise("ValueError", "%s：参数列表为空（模型中没有需要梯度的张量？）", name)
	}
	seen := map[*Tensor]bool{}
	var out []*Tensor
	for _, p := range params.S {
		if p.fn != nil {
			rt.Raise("ValueError", "%s：只能优化叶子张量（由运算得到的张量不能作为参数）", name)
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func checkLr(lr float64) {
	if lr < 0 || math.IsNaN(lr) {
		rt.Raise("ValueError", "学习率不能是负数：%v", lr)
	}
}

// NewSGD 创建随机梯度下降优化器（momentum 为 0 时不使用动量）。
func NewSGD(params *rt.List[*Tensor], lr, momentum, weightDecay float64) *Optimizer {
	checkLr(lr)
	ps := optimParams(params, "SGD")
	return &Optimizer{kind: "SGD", params: ps, lr: lr, momentum: momentum, wd: weightDecay, m: make([]*Tensor, len(ps))}
}

// NewAdam 创建 Adam 优化器；decoupled 为真时是 AdamW（权重衰减直接作用于参数）。
func NewAdam(params *rt.List[*Tensor], lr, beta1, beta2, eps, weightDecay float64, decoupled bool) *Optimizer {
	checkLr(lr)
	if beta1 < 0 || beta1 >= 1 || beta2 < 0 || beta2 >= 1 {
		rt.Raise("ValueError", "Adam 的 betas 必须在 [0, 1) 中，得到 (%v, %v)", beta1, beta2)
	}
	kind := "Adam"
	if decoupled {
		kind = "AdamW"
	}
	ps := optimParams(params, kind)
	return &Optimizer{kind: kind, params: ps, lr: lr, b1: beta1, b2: beta2, eps: eps, wd: weightDecay, decoupled: decoupled,
		m: make([]*Tensor, len(ps)), v: make([]*Tensor, len(ps))}
}

// Step 用参数当前的梯度更新参数（没有梯度的参数跳过）。
func (o *Optimizer) Step() {
	o.steps++
	for i, p := range o.params {
		if p.grad == nil {
			continue
		}
		dev := p.dev()
		if p.grad.dev() != dev {
			rt.Raise("RuntimeError", "%s：参数在 %s 上，但它的梯度在 %s 上", o.kind, dev.name(), p.grad.dev().name())
		}
		if o.m[i] != nil && o.m[i].dev() != dev {
			rt.Raise("RuntimeError", "%s：参数在创建优化器之后被移到了 %s；请先 model.to(...)，再创建优化器", o.kind, dev.name())
		}
		switch o.kind {
		case "SGD":
			if o.momentum != 0 && o.m[i] == nil {
				// 第一步：缓冲区从梯度开始（与 PyTorch 相同）
				o.m[i] = full(dev, p.shape, 0)
				dev.sgd(p.st, p.grad.st, o.m[i].st, p.n, float32(o.lr), 0, float32(o.wd), true)
				continue
			}
			var buf *storage
			if o.m[i] != nil {
				buf = o.m[i].st
			}
			dev.sgd(p.st, p.grad.st, buf, p.n, float32(o.lr), float32(o.momentum), float32(o.wd), o.momentum != 0)
		default:
			if o.m[i] == nil {
				o.m[i], o.v[i] = full(dev, p.shape, 0), full(dev, p.shape, 0)
			}
			bc1 := 1 - math.Pow(o.b1, float64(o.steps))
			bc2 := 1 - math.Pow(o.b2, float64(o.steps))
			dev.adam(p.st, p.grad.st, o.m[i].st, o.v[i].st, p.n, float32(o.lr), float32(o.b1), float32(o.b2),
				float32(o.eps), float32(o.wd), float32(bc1), float32(bc2), o.decoupled)
		}
	}
}

// ZeroGrad 清除所有参数的梯度。
func (o *Optimizer) ZeroGrad() {
	for _, p := range o.params {
		p.grad = nil
	}
}

func (o *Optimizer) Lr() float64 { return o.lr }

func (o *Optimizer) SetLr(lr float64) {
	checkLr(lr)
	o.lr = lr
}

func (o *Optimizer) Steps() int { return o.steps }

func (o *Optimizer) Params() *rt.List[*Tensor] {
	return &rt.List[*Tensor]{S: append([]*Tensor(nil), o.params...)}
}

func (o *Optimizer) String() string {
	switch o.kind {
	case "SGD":
		return fmt.Sprintf("SGD(lr=%g, momentum=%g, weight_decay=%g)", o.lr, o.momentum, o.wd)
	}
	return fmt.Sprintf("%s(lr=%g, betas=(%g, %g), eps=%g, weight_decay=%g)", o.kind, o.lr, o.b1, o.b2, o.eps, o.wd)
}

// ===== 保存与读取 =====

const fileMagic = "AETHERT1"

// Save 把名字到张量的字典保存到文件（例如 model.state_dict()）。
// 格式：魔数、张量个数，然后每个张量依次为名字、维数、各维长度、float32 数据（小端）。
func Save(d *rt.Dict[string, *Tensor], path string) {
	f, err := os.Create(path)
	if err != nil {
		rt.Raise("IOError", "无法创建文件 %s：%v", path, err)
	}
	w := bufio.NewWriter(f)
	put := func(v any) {
		if err == nil {
			err = binary.Write(w, binary.LittleEndian, v)
		}
	}
	w.WriteString(fileMagic)
	keys := d.Keys().S
	put(uint32(len(keys)))
	for _, k := range keys {
		t := d.Get(k)
		put(uint32(len(k)))
		if err == nil {
			_, err = w.WriteString(k)
		}
		put(uint32(len(t.shape)))
		for _, s := range t.shape {
			put(uint64(s))
		}
		put(t.host())
	}
	if err == nil {
		err = w.Flush()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		rt.Raise("IOError", "写入 %s 失败：%v", path, err)
	}
}

// Load 读取 Save 保存的文件（张量放在 CPU 上；load_state_dict 会复制到模型所在的设备）。
func Load(path string) *rt.Dict[string, *Tensor] {
	f, err := os.Open(path)
	if err != nil {
		rt.Raise("FileNotFoundError", "无法打开 %s：%v", path, err)
	}
	defer f.Close()
	r := bufio.NewReader(f)
	bad := func(what string) {
		rt.Raise("ValueError", "%s 不是有效的张量文件（%s）", path, what)
	}
	magic := make([]byte, len(fileMagic))
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != fileMagic {
		bad("文件头不正确")
	}
	get := func(v any) {
		if err := binary.Read(r, binary.LittleEndian, v); err != nil {
			bad("数据不完整")
		}
	}
	var count uint32
	get(&count)
	d := rt.NewDict[string, *Tensor](int(count))
	for i := 0; i < int(count); i++ {
		var nlen, ndim uint32
		get(&nlen)
		if nlen > 1<<16 {
			bad("名字过长")
		}
		name := make([]byte, nlen)
		if _, err := io.ReadFull(r, name); err != nil {
			bad("数据不完整")
		}
		get(&ndim)
		if ndim > maxDims {
			bad("维数过多")
		}
		shape := make([]int, ndim)
		n := 1
		for j := range shape {
			var s uint64
			get(&s)
			if s > 1<<34 {
				bad("形状过大")
			}
			shape[j] = int(s)
			n *= shape[j]
		}
		if n > 1<<31 {
			bad("形状过大")
		}
		data := make([]float32, n)
		get(data)
		d.Set(string(name), fromHost(cpuDev, shape, data))
	}
	return d
}
