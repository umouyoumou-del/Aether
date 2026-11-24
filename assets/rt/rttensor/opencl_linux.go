//go:build linux && cgo

package rttensor

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"unsafe"

	"aether/assets/rt/rtcl"
)

// Linux：用 dlopen 加载 OpenCL 的 ICD 加载器 libOpenCL.so.1（Ubuntu 的 ocl-icd-libopencl1 等），显卡驱动通过
// /etc/OpenCL/vendors/*.icd 向它注册。dlopen 要用 cgo（在 rtcl 包中：用到 cgo 的包里不能有 Go 汇编），所以
// 构建时需要 C 编译器；没有的话使用 opencl_other.go（只有 CPU）。环境变量 AETHER_OPENCL_LIB 可以指定另一个库。

type clProc = rtcl.Proc

var clProcs []*clProc

func newProc(name string) *clProc {
	p := &clProc{Name: name}
	clProcs = append(clProcs, p)
	return p
}

var (
	clGetPlatformIDs          = newProc("clGetPlatformIDs")
	clGetDeviceIDs            = newProc("clGetDeviceIDs")
	clGetDeviceInfo           = newProc("clGetDeviceInfo")
	clCreateContext           = newProc("clCreateContext")
	clCreateCommandQueue      = newProc("clCreateCommandQueue")
	clCreateProgramWithSource = newProc("clCreateProgramWithSource")
	clBuildProgram            = newProc("clBuildProgram")
	clGetProgramBuildInfo     = newProc("clGetProgramBuildInfo")
	clCreateKernel            = newProc("clCreateKernel")
	clCreateBuffer            = newProc("clCreateBuffer")
	clReleaseMemObject        = newProc("clReleaseMemObject")
	clSetKernelArg            = newProc("clSetKernelArg")
	clEnqueueNDRangeKernel    = newProc("clEnqueueNDRangeKernel")
	clEnqueueReadBuffer       = newProc("clEnqueueReadBuffer")
	clEnqueueWriteBuffer      = newProc("clEnqueueWriteBuffer")
	clFinish                  = newProc("clFinish")
	clGetKernelWorkGroupInfo  = newProc("clGetKernelWorkGroupInfo")
	clWaitForEvents           = newProc("clWaitForEvents")
	clGetEventProfilingInfo   = newProc("clGetEventProfilingInfo")
	clReleaseEvent            = newProc("clReleaseEvent")
)

var (
	clOnce sync.Once
	clErr  error
)

func clAvailable() error {
	clOnce.Do(func() {
		names := []string{"libOpenCL.so.1", "libOpenCL.so"}
		if p := os.Getenv("AETHER_OPENCL_LIB"); p != "" {
			names = []string{p}
		}
		if _, err := rtcl.Open(names, clProcs); err != nil {
			clErr = fmt.Errorf("无法加载 OpenCL 加载器 libOpenCL.so（%v）；请安装它（Ubuntu/Debian：apt install ocl-icd-libopencl1）与显卡驱动", err)
		}
	})
	return clErr
}

// clCall 调用返回错误码的 OpenCL 函数。指针参数必须在调用处写成 uintptr(unsafe.Pointer(&x))，
// 编译器才会保证 x 在调用期间位于堆上、不会被回收（与 opencl_windows.go 相同的规则）。
//
//go:uintptrescapes
func clCall(p *clProc, args ...uintptr) int32 {
	return int32(p.Call(args...))
}

// clCallPtr 调用返回对象句柄的函数（错误码通过追加的最后一个参数返回）。
//
//go:uintptrescapes
func clCallPtr(p *clProc, args ...uintptr) (uintptr, int32) {
	errc := new(int32)
	var pin runtime.Pinner
	pin.Pin(errc)
	defer pin.Unpin()
	r := p.Call(append(args, uintptr(unsafe.Pointer(errc)))...)
	return r, *errc
}
