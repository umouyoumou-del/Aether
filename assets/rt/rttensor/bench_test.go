package rttensor

import (
	"testing"

	"aether/assets/rt"
)

// BenchmarkCNNStep 是一个小卷积网络在 CPU 上的一步训练（前向与反向）。
func BenchmarkCNNStep(b *testing.B) {
	x := randT(1, -1, 1, 128, 1, 16, 16)
	w1 := randT(2, -0.3, 0.3, 16, 1, 3, 3).SetRequiresGrad(true)
	w2 := randT(3, -0.1, 0.1, 32, 16, 3, 3).SetRequiresGrad(true)
	w3 := randT(4, -0.05, 0.05, 3, 512).SetRequiresGrad(true)
	y := randT(5, 0, 2.99, 128)
	for i := range y.st.cpu {
		y.st.cpu[i] = float32(int(y.st.cpu[i]))
	}
	none := rt.Opt[*Tensor]{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := MaxPool2d(Conv2d(x, w1, none, 1, 1).Relu(), 2, 0, 0)
		h = MaxPool2d(Conv2d(h, w2, none, 1, 1).Relu(), 2, 0, 0)
		loss := CrossEntropy(LinearFn(h.Flatten(1, -1), w3, none), y, "mean")
		loss.Backward(none)
	}
}

func BenchmarkMatmul512(b *testing.B) {
	x, y := randT(1, -1, 1, 512, 512), randT(2, -1, 1, 512, 512)
	for i := 0; i < b.N; i++ {
		x.Matmul(y)
	}
}

// BenchmarkAttentionStep 是一层多头自注意力 + 前馈网络在 CPU 上的一步训练（与 examples/gpt.ae 的尺寸相同）。
func BenchmarkAttentionStep(b *testing.B) {
	const B, T, E, H = 32, 24, 64, 4
	d := E / H
	x := randT(1, -1, 1, B, T, E)
	wqkv := randT(2, -0.1, 0.1, 3*E, E).SetRequiresGrad(true)
	wo := randT(3, -0.1, 0.1, E, E).SetRequiresGrad(true)
	w1 := randT(4, -0.1, 0.1, 4*E, E).SetRequiresGrad(true)
	w2 := randT(5, -0.1, 0.1, E, 4*E).SetRequiresGrad(true)
	lnw, lnb := full(cpuDev, []int{E}, 1).SetRequiresGrad(true), full(cpuDev, []int{E}, 0).SetRequiresGrad(true)
	mask := CausalMask(T)
	nob := rt.Opt[*Tensor]{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := LayerNorm(x, lnw, lnb, 1e-5)
		qkv := LinearFn(h, wqkv, nob)
		q := qkv.Narrow(2, 0, E).Reshape(ints(B, T, H, d)).Transpose(1, 2)
		k := qkv.Narrow(2, E, E).Reshape(ints(B, T, H, d)).Transpose(1, 2)
		v := qkv.Narrow(2, 2*E, E).Reshape(ints(B, T, H, d)).Transpose(1, 2)
		att := q.Matmul(k.Transpose(2, 3)).MulScalar(0.25).Add(mask).Softmax(-1)
		o := LinearFn(att.Matmul(v).Transpose(1, 2).Reshape(ints(B, T, E)), wo, nob)
		y := x.Add(o)
		y = y.Add(LinearFn(LinearFn(LayerNorm(y, lnw, lnb, 1e-5), w1, nob).Gelu(), w2, nob))
		y.Sum(none, false).Backward(nob)
	}
}

// BenchmarkGPULaunch 测量一次很小的 GPU 运算（主要是提交内核的开销）。
func BenchmarkGPULaunch(b *testing.B) {
	g := gpu()
	if g == nil {
		b.Skip("没有 GPU")
	}
	x := full(g, []int{16}, 1)
	y := x
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		y = y.Add(x)
	}
	Synchronize()
}
