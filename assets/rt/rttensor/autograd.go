package rttensor

import (
	"sync/atomic"

	"aether/assets/rt"
)

// node 记录张量是怎样计算出来的：输入，以及由输出的梯度计算各个输入的梯度的函数。
type node struct {
	name   string
	inputs []*Tensor
	back   func(g *Tensor) []*Tensor
}

var noGradDepth atomic.Int32

func gradEnabled() bool { return noGradDepth.Load() == 0 }

func withNoGrad(f func()) {
	noGradDepth.Add(1)
	defer noGradDepth.Add(-1)
	f()
}

// record 在需要求导时为 out 记录计算图中的节点。
func record(out *Tensor, name string, inputs []*Tensor, back func(g *Tensor) []*Tensor) *Tensor {
	if !gradEnabled() {
		return out
	}
	for _, in := range inputs {
		if in != nil && in.requiresGrad {
			out.requiresGrad = true
			out.fn = &node{name: name, inputs: inputs, back: back}
			return out
		}
	}
	return out
}

// NoGradCtx 用于 with tensor.no_grad(): 块中的计算不记录计算图（推理、更新参数时使用）。
type NoGradCtx struct{}

func NoGrad() *NoGradCtx          { return &NoGradCtx{} }
func (*NoGradCtx) Enter()         { noGradDepth.Add(1) }
func (*NoGradCtx) Exit()          { noGradDepth.Add(-1) }
func IsGradEnabled() bool         { return gradEnabled() }
func (*NoGradCtx) String() string { return "<no_grad>" }

// Detach 返回共享数据、但不参与求导的张量。
func (t *Tensor) Detach() *Tensor { return &Tensor{shape: t.shape, n: t.n, st: t.st} }

// Backward 从这个张量（通常是标量损失）开始反向传播，把梯度累加到叶子张量的 grad 上；
// 对非标量张量反向传播时需要给出它的梯度 gradient。
func (t *Tensor) Backward(gradient rt.Opt[*Tensor]) {
	if gradient.Ok {
		t.backward(gradient.V)
	} else {
		t.backward(nil)
	}
}

func (t *Tensor) backward(seed *Tensor) {
	if !t.requiresGrad {
		rt.Raise("RuntimeError", "这个张量不需要梯度：参与计算的张量都没有设置 requires_grad")
	}
	if seed == nil {
		if t.n != 1 {
			rt.Raise("RuntimeError", "只能对只有一个元素的张量调用 backward()，这个张量的形状是 %s；可以先求和：x.sum().backward()", shapeStr(t.shape))
		}
		seed = full(t.dev(), t.shape, 1)
	} else if !sameShape(seed.shape, t.shape) {
		raiseShape("backward 的梯度形状 %s 与张量的形状 %s 不一致", shapeStr(seed.shape), shapeStr(t.shape))
	}
	order := topoSort(t)
	grads := map[*Tensor]*Tensor{t: seed}
	withNoGrad(func() {
		for i := len(order) - 1; i >= 0; i-- {
			x := order[i]
			g := grads[x]
			delete(grads, x)
			if g == nil {
				continue
			}
			if x.fn == nil {
				if x.requiresGrad {
					if x.grad == nil {
						x.grad = g.Detach()
					} else {
						x.grad = x.grad.Add(g)
					}
				}
				continue
			}
			if x.fn.back == nil {
				rt.Raise("RuntimeError", "计算图已经在之前的 backward() 中被释放；每次前向计算只能反向传播一次")
			}
			gs := x.fn.back(g)
			for j, in := range x.fn.inputs {
				if in == nil || !in.requiresGrad || j >= len(gs) || gs[j] == nil {
					continue
				}
				if prev := grads[in]; prev != nil {
					grads[in] = prev.Add(gs[j])
				} else {
					grads[in] = gs[j]
				}
			}
			// 释放保存的中间结果
			x.fn.back = nil
		}
	})
}

// topoSort 返回以 t 为终点的计算图中的张量，输入排在输出之前。
func topoSort(t *Tensor) []*Tensor {
	var order []*Tensor
	seen := map[*Tensor]bool{}
	type frame struct {
		t *Tensor
		i int
	}
	stack := []frame{{t, 0}}
	seen[t] = true
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.t.fn != nil && top.i < len(top.t.fn.inputs) {
			in := top.t.fn.inputs[top.i]
			top.i++
			if in != nil && in.requiresGrad && !seen[in] {
				seen[in] = true
				stack = append(stack, frame{in, 0})
			}
			continue
		}
		order = append(order, top.t)
		stack = stack[:len(stack)-1]
	}
	return order
}

// Checkpoint 实现 tensor.checkpoint(f, x)：计算 f(x)，但不保存 f 内部的中间结果，反向传播到这里时
// 重新计算一遍 f 再求梯度——用大约多三分之一的计算换来大得多的显存（例如对每一层分别使用）。
// 重新计算时随机数的计数器恢复到第一次计算时的值，所以 f 中的 dropout 会得到同样的掩码。
func Checkpoint(f func(*Tensor) *Tensor, x *Tensor) *Tensor {
	seed := seedCounter.Load()
	if !gradEnabled() {
		return f(x)
	}
	var first *Tensor
	withNoGrad(func() { first = f(x) })
	out := &Tensor{shape: first.shape, n: first.n, st: first.st, requiresGrad: true}
	out.fn = &node{name: "checkpoint", inputs: []*Tensor{x}, back: func(g *Tensor) []*Tensor {
		saved := seedCounter.Load()
		seedCounter.Store(seed)
		xi := x.Detach()
		xi.requiresGrad = x.requiresGrad
		// 反向传播时求导是关闭的：重新计算时临时打开
		depth := noGradDepth.Swap(0)
		y := f(xi)
		noGradDepth.Store(depth)
		seedCounter.Store(saved)
		if y.requiresGrad {
			y.backward(g)
		}
		return []*Tensor{xi.grad}
	}}
	return out
}

// GradFn 返回产生这个张量的运算的名字（叶子张量为空串）。
func (t *Tensor) GradFn() string {
	if t.fn == nil {
		return ""
	}
	return t.fn.name
}
