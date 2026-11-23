//go:build windows

package rttensor

import (
	"runtime"
	"syscall"
	"unsafe"
)

// 通过 syscall 直接调用 OpenCL.dll（显卡驱动自带的 OpenCL 加载器），不需要 cgo 与 C 编译器。

var (
	clDLL                     = syscall.NewLazyDLL("OpenCL.dll")
	clGetPlatformIDs          = clDLL.NewProc("clGetPlatformIDs")
	clGetDeviceIDs            = clDLL.NewProc("clGetDeviceIDs")
	clGetDeviceInfo           = clDLL.NewProc("clGetDeviceInfo")
	clCreateContext           = clDLL.NewProc("clCreateContext")
	clCreateCommandQueue      = clDLL.NewProc("clCreateCommandQueue")
	clCreateProgramWithSource = clDLL.NewProc("clCreateProgramWithSource")
	clBuildProgram            = clDLL.NewProc("clBuildProgram")
	clGetProgramBuildInfo     = clDLL.NewProc("clGetProgramBuildInfo")
	clCreateKernel            = clDLL.NewProc("clCreateKernel")
	clCreateBuffer            = clDLL.NewProc("clCreateBuffer")
	clReleaseMemObject        = clDLL.NewProc("clReleaseMemObject")
	clSetKernelArg            = clDLL.NewProc("clSetKernelArg")
	clEnqueueNDRangeKernel    = clDLL.NewProc("clEnqueueNDRangeKernel")
	clEnqueueReadBuffer       = clDLL.NewProc("clEnqueueReadBuffer")
	clEnqueueWriteBuffer      = clDLL.NewProc("clEnqueueWriteBuffer")
	clFinish                  = clDLL.NewProc("clFinish")
	clGetKernelWorkGroupInfo  = clDLL.NewProc("clGetKernelWorkGroupInfo")
)

func clAvailable() error { return clDLL.Load() }

// clCall 调用返回错误码的 OpenCL 函数。指针参数必须在调用处写成 uintptr(unsafe.Pointer(&x))，
// 编译器才会保证 x 在调用期间位于堆上、不会被移动或回收（与 syscall 的 Proc.Call 相同的规则）。
//
//go:uintptrescapes
func clCall(p *syscall.LazyProc, args ...uintptr) int32 {
	r, _, _ := p.Call(args...)
	return int32(r)
}

// clCallPtr 调用返回对象句柄的函数（错误码通过追加的最后一个参数返回）。
//
//go:uintptrescapes
func clCallPtr(p *syscall.LazyProc, args ...uintptr) (uintptr, int32) {
	errc := new(int32)
	var pin runtime.Pinner
	pin.Pin(errc)
	defer pin.Unpin()
	r, _, _ := p.Call(append(args, uintptr(unsafe.Pointer(errc)))...)
	return r, *errc
}
