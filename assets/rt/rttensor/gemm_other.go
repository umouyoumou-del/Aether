//go:build !amd64

package rttensor

// 其它架构上没有汇编内核，矩阵乘法使用 Go 代码。
const haveSIMD = false

func sgemmKernel6x16(k int, a, b, c *float32, ldc int) { panic("没有 SIMD 内核") }
