// Package rtcl 在 Linux 上用 dlopen 加载动态库（OpenCL 的 libOpenCL.so）并调用其中的 C 函数。
//
// 它是一个单独的包，因为用到 cgo 的包里不能有 Go 汇编（rttensor 的矩阵乘法内核 gemm_amd64.s）。
// 只有 Linux 且构建时启用了 cgo 才有内容（dl_linux.go）；其它平台上这个包是空的，也不会被导入。
package rtcl
