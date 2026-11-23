//go:build !windows

package rttensor

import "errors"

// 非 Windows 平台：Aether 的构建不使用 cgo，目前还不能动态加载 libOpenCL.so，GPU 后端不可用
// （张量运算使用 CPU 后端）。

type clProc struct{}

var (
	clGetPlatformIDs, clGetDeviceIDs, clGetDeviceInfo, clCreateContext, clCreateCommandQueue,
	clCreateProgramWithSource, clBuildProgram, clGetProgramBuildInfo, clCreateKernel, clCreateBuffer,
	clReleaseMemObject, clSetKernelArg, clEnqueueNDRangeKernel, clEnqueueReadBuffer,
	clEnqueueWriteBuffer, clFinish, clGetKernelWorkGroupInfo, clWaitForEvents, clGetEventProfilingInfo,
	clReleaseEvent *clProc
)

func clAvailable() error {
	return errors.New("目前只在 Windows 上支持 GPU（通过显卡驱动的 OpenCL.dll）；其它系统使用 CPU")
}

func clCall(p *clProc, args ...uintptr) int32 { return -1 }

func clCallPtr(p *clProc, args ...uintptr) (uintptr, int32) { return 0, -1 }
