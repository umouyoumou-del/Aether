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
