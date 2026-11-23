//go:build amd64

package rttensor

import "os"

//go:noescape
func sgemmKernel6x16(k int, a, b, c *float32, ldc int)

func cpuid(eaxArg, ecxArg uint32) (eax, ebx, ecx, edx uint32)

func xgetbv0() (eax, edx uint32)

// haveSIMD 表示可以使用 AVX2 + FMA 的矩阵乘法内核（CPU 支持，并且操作系统保存 YMM 寄存器）。
// 设置环境变量 AETHER_NO_SIMD=1 可以关闭（用于对比与测试）。
var haveSIMD = func() bool {
	if os.Getenv("AETHER_NO_SIMD") != "" {
		return false
	}
	maxID, _, _, _ := cpuid(0, 0)
	if maxID < 7 {
		return false
	}
	_, _, ecx1, _ := cpuid(1, 0)
	const fma, osxsave, avx = 1 << 12, 1 << 27, 1 << 28
	if ecx1&fma == 0 || ecx1&osxsave == 0 || ecx1&avx == 0 {
		return false
	}
	if xcr0, _ := xgetbv0(); xcr0&6 != 6 {
		return false
	}
	_, ebx7, _, _ := cpuid(7, 0)
	return ebx7&(1<<5) != 0 // AVX2
}()
