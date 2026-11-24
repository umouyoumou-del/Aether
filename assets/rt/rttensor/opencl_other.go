//go:build !windows && !(linux && cgo)

package rttensor

import (
	"errors"
	"runtime"
)

// 没有 GPU 后端的平台（张量运算使用 CPU 后端）：Linux 上构建时没有使用 cgo（找不到 C 编译器），
// 不能用 dlopen 加载 libOpenCL.so（见 opencl_linux.go）；macOS 等其它系统还不支持。

type clProc struct{}

var (
	clGetPlatformIDs, clGetDeviceIDs, clGetDeviceInfo, clCreateContext, clCreateCommandQueue,
	clCreateProgramWithSource, clBuildProgram, clGetProgramBuildInfo, clCreateKernel, clCreateBuffer,
	clReleaseMemObject, clSetKernelArg, clEnqueueNDRangeKernel, clEnqueueReadBuffer,
	clEnqueueWriteBuffer, clFinish, clGetKernelWorkGroupInfo, clWaitForEvents, clGetEventProfilingInfo,
	clReleaseEvent *clProc
)

func clAvailable() error {
	if runtime.GOOS == "linux" {
		return errors.New("这个程序构建时没有使用 cgo（找不到 C 编译器，或者设置了 AETHER_CGO=0），不能加载 libOpenCL.so；安装 gcc 后重新构建即可使用 GPU")
	}
	return errors.New("目前只在 Windows 与 Linux 上支持 GPU；其它系统使用 CPU")
}

func clCall(p *clProc, args ...uintptr) int32 { return -1 }

func clCallPtr(p *clProc, args ...uintptr) (uintptr, int32) { return 0, -1 }
