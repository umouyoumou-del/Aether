//go:build linux && cgo

package rtcl

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdint.h>
#include <stdlib.h>

// 被调用的函数只有整数与指针参数（OpenCL 的函数最多 9 个）：一律按 12 个 uintptr_t 参数调用。多出的参数被调用的
// 函数忽略（x86-64 与 arm64 的 Linux 调用约定中，参数由调用者放置、由调用者清理，所以多传是安全的）；
// 返回 32 位整数的函数只用到返回值的低 32 位。
typedef uintptr_t (*ae_fn)(uintptr_t, uintptr_t, uintptr_t, uintptr_t, uintptr_t, uintptr_t,
                           uintptr_t, uintptr_t, uintptr_t, uintptr_t, uintptr_t, uintptr_t);

static uintptr_t ae_call(void *f, const uintptr_t *a) {
	return ((ae_fn)f)(a[0], a[1], a[2], a[3], a[4], a[5], a[6], a[7], a[8], a[9], a[10], a[11]);
}
*/
import "C"

import (
	"fmt"
	"strings"
	"unsafe"
)

// MaxArgs 是 Call 最多能传的参数个数。
const MaxArgs = 12

// Proc 是动态库中的一个函数，Open 之后才能调用。
type Proc struct {
	Name string
	fn   unsafe.Pointer
}

// Open 依次尝试用 dlopen 加载 names 中的库，在第一个能加载的库中用 dlsym 找出 procs 中的每个函数。
// 返回加载的库名。
func Open(names []string, procs []*Proc) (string, error) {
	var lib unsafe.Pointer
	var name string
	var errs []string
	for _, n := range names {
		cn := C.CString(n)
		lib = C.dlopen(cn, C.RTLD_NOW|C.RTLD_LOCAL)
		C.free(unsafe.Pointer(cn))
		if lib != nil {
			name = n
			break
		}
		errs = append(errs, C.GoString(C.dlerror()))
	}
	if lib == nil {
		return "", fmt.Errorf("%s", strings.Join(errs, "；"))
	}
	for _, p := range procs {
		cn := C.CString(p.Name)
		p.fn = C.dlsym(lib, cn)
		C.free(unsafe.Pointer(cn))
		if p.fn == nil {
			return name, fmt.Errorf("%s 中没有函数 %s", name, p.Name)
		}
	}
	return name, nil
}

// Call 调用函数。指针参数由调用者保证在调用期间有效（见 rttensor/opencl_linux.go 的 clCall）。
func (p *Proc) Call(args ...uintptr) uintptr {
	if p.fn == nil {
		panic("函数 " + p.Name + " 还没有加载")
	}
	var a [MaxArgs]C.uintptr_t
	if len(args) > len(a) {
		panic(fmt.Sprintf("函数 %s 的参数太多（%d 个）", p.Name, len(args)))
	}
	for i, v := range args {
		a[i] = C.uintptr_t(v)
	}
	return uintptr(C.ae_call(p.fn, &a[0]))
}
