package rttensor

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"aether/assets/rt"
)

// gpuBackend 通过 OpenCL 在显卡上计算：NVIDIA、AMD、Intel 的驱动都自带 OpenCL，
// 内核（kernels.go）在第一次使用时由驱动编译，不需要安装 CUDA、ROCm 等开发工具。
//
// 所有命令提交到同一个按顺序执行的命令队列，所以内核之间不需要额外同步；读回数据时
// 使用阻塞读取，会等待之前的计算完成。
type gpuBackend struct {
	mu      sync.Mutex // 设置内核参数与提交命令需要互斥
	ctx     uintptr
	queue   uintptr
	device  uintptr
	prog    uintptr
	kernels map[string]clKernel
	info    string

	// 环境变量 AETHER_GPU_PROFILE=1 时记录每个内核的运行时间（会让 GPU 串行执行，只用于分析性能）
	profiling bool
	profile   map[string]*kernelStat

	// 显存池：张量被垃圾回收时（终结器）把显存块放回池中，之后分配同样大小的块时直接复用。
	// Go 的垃圾回收器看不到显存的占用，所以需要新的显存块、而占用的显存（正在使用 + 缓存）
	// 已经接近上限 highWater 时，主动触发一次回收；回收之后至少再分配 gcBudget 字节才会再次回收，
	// 避免正在使用的张量本身就很多时反复回收。
	poolMu      sync.Mutex
	pool        map[int][]uintptr
	liveBytes   atomic.Int64
	pooledBytes atomic.Int64
	sinceGC     atomic.Int64
	gcBudget    int64
	highWater   int64
	collects    atomic.Int64
	retries     atomic.Int64
	finalized   atomic.Int64 // 已经放回池中的块数（终结器运行的次数）
}

type gpuBuf struct {
	mem  uintptr
	size int
}

var (
	gpuOnce sync.Once
	gpuInst *gpuBackend
	gpuErr  error
)

// gpu 返回 GPU 后端（第一次调用时初始化）；没有可用的 GPU 时返回 nil。
func gpu() *gpuBackend {
	gpuOnce.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				gpuInst, gpuErr = nil, fmt.Errorf("%v", r)
			}
		}()
		gpuInst, gpuErr = initGPU()
	})
	return gpuInst
}

func gpuError() string {
	if gpuErr == nil {
		return "未知原因"
	}
	return gpuErr.Error()
}

// GpuAvailable 判断是否有可用的 GPU。
func GpuAvailable() bool { return gpu() != nil }

// DeviceInfo 返回 GPU 的名字与参数（没有 GPU 时说明原因）。
func DeviceInfo() string {
	if g := gpu(); g != nil {
		return g.info
	}
	return "没有可用的 GPU：" + gpuError()
}

const (
	clDeviceTypeGPU      = 1 << 2
	clDeviceName         = 0x102B
	clDeviceVendor       = 0x102C
	clDeviceVersion      = 0x102F
	clDeviceComputeUnits = 0x1002
	clDeviceClock        = 0x100C
	clDeviceGlobalMem    = 0x101F
	clDeviceHostUnified  = 0x1035
	clProgramBuildLog    = 0x1183
	clMemReadWrite       = 1 << 0
	clMemObjectAllocFail = -4
	clOutOfResources     = -5
	clOutOfHostMemory    = -6
)

func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

type clDevice struct {
	id           uintptr
	name, vendor string
	version      string
	units, clock uint32
	mem          int64
	unified      bool
}

func deviceString(id uintptr, param uintptr) string {
	buf := make([]byte, 512)
	clCall(clGetDeviceInfo, id, param, uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])), 0)
	return strings.TrimSpace(cString(buf))
}

func listDevices() ([]clDevice, error) {
	if err := clAvailable(); err != nil {
		return nil, fmt.Errorf("找不到 OpenCL：%v", err)
	}
	var np uint32
	if r := clCall(clGetPlatformIDs, 0, 0, uintptr(unsafe.Pointer(&np))); r != 0 || np == 0 {
		return nil, fmt.Errorf("没有找到 OpenCL 平台（错误码 %d）；请确认安装了显卡驱动", r)
	}
	plats := make([]uintptr, np)
	clCall(clGetPlatformIDs, uintptr(np), uintptr(unsafe.Pointer(&plats[0])), 0)
	var out []clDevice
	for _, p := range plats {
		var nd uint32
		if clCall(clGetDeviceIDs, p, clDeviceTypeGPU, 0, 0, uintptr(unsafe.Pointer(&nd))) != 0 || nd == 0 {
			continue
		}
		ids := make([]uintptr, nd)
		clCall(clGetDeviceIDs, p, clDeviceTypeGPU, uintptr(nd), uintptr(unsafe.Pointer(&ids[0])), 0)
		for _, id := range ids {
			d := clDevice{id: id}
			d.name = deviceString(id, clDeviceName)
			d.vendor = deviceString(id, clDeviceVendor)
			d.version = deviceString(id, clDeviceVersion)
			var units, clock, unified uint32
			var mem uint64
			clCall(clGetDeviceInfo, id, clDeviceComputeUnits, 4, uintptr(unsafe.Pointer(&units)), 0)
			clCall(clGetDeviceInfo, id, clDeviceClock, 4, uintptr(unsafe.Pointer(&clock)), 0)
			clCall(clGetDeviceInfo, id, clDeviceGlobalMem, 8, uintptr(unsafe.Pointer(&mem)), 0)
			clCall(clGetDeviceInfo, id, clDeviceHostUnified, 4, uintptr(unsafe.Pointer(&unified)), 0)
			d.units, d.clock, d.mem, d.unified = units, clock, int64(mem), unified != 0
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("OpenCL 中没有找到 GPU 设备")
	}
	return out, nil
}

// gpuMemoryLimit 读取环境变量 AETHER_GPU_MEMORY（例如 2G、1536M；只写数字时单位是 MB），没有设置时返回 0。
func gpuMemoryLimit() (int64, error) {
	s := strings.ToUpper(strings.TrimSpace(os.Getenv("AETHER_GPU_MEMORY")))
	if s == "" {
		return 0, nil
	}
	unit := int64(1 << 20)
	num := strings.TrimSuffix(s, "B")
	switch {
	case strings.HasSuffix(num, "G"):
		unit, num = 1<<30, strings.TrimSuffix(num, "G")
	case strings.HasSuffix(num, "M"):
		num = strings.TrimSuffix(num, "M")
	}
	v, err := strconv.ParseFloat(num, 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("环境变量 AETHER_GPU_MEMORY=%s 无效：应写成 2G、1536M 这样的大小", os.Getenv("AETHER_GPU_MEMORY"))
	}
	return int64(v * float64(unit)), nil
}

// GpuDevices 列出所有可用的 GPU（序号可以写在环境变量 AETHER_GPU 中来选择使用哪一个）。
func GpuDevices() *rt.List[string] {
	out := &rt.List[string]{}
	devs, err := listDevices()
	if err != nil {
		return out
	}
	for _, d := range devs {
		out.S = append(out.S, fmt.Sprintf("%s（%s）", d.name, d.vendor))
	}
	return out
}

func initGPU() (*gpuBackend, error) {
	devs, err := listDevices()
	if err != nil {
		return nil, err
	}
	// 默认选择独立显卡（不与内存共享、计算单元多）；环境变量 AETHER_GPU 可以指定序号
	score := func(d clDevice) float64 {
		s := float64(d.units) * float64(max(d.clock, 1))
		if d.unified {
			s /= 8
		}
		return s
	}
	best := 0
	for i, d := range devs {
		if score(d) > score(devs[best]) {
			best = i
		}
	}
	if s := os.Getenv("AETHER_GPU"); s != "" {
		i, err := strconv.Atoi(s)
		if err != nil || i < 0 || i >= len(devs) {
			return nil, fmt.Errorf("环境变量 AETHER_GPU=%s 无效：共有 %d 个 GPU（序号从 0 开始）", s, len(devs))
		}
		best = i
	}
	d := devs[best]
	g := &gpuBackend{device: d.id, kernels: map[string]clKernel{}, pool: map[int][]uintptr{}}
	g.gcBudget = min(max(d.mem/8, 64<<20), 1<<30)
	g.highWater = d.mem * 85 / 100
	// 与其它程序共用显卡时（例如服务器上），AETHER_GPU_MEMORY 限制这个程序占用的显存（正在使用 + 缓存）
	if lim, err := gpuMemoryLimit(); err != nil {
		return nil, err
	} else if lim > 0 && lim < g.highWater {
		g.highWater = lim
		g.gcBudget = min(g.gcBudget, max(lim/8, 16<<20))
	}
	var errc int32
	g.ctx, errc = clCallPtr(clCreateContext, 0, 1, uintptr(unsafe.Pointer(&g.device)), 0, 0)
	if errc != 0 {
		return nil, fmt.Errorf("创建 OpenCL 上下文失败（错误码 %d）", errc)
	}
	var queueProps uintptr
	if os.Getenv("AETHER_GPU_PROFILE") == "1" {
		g.profiling, g.profile = true, map[string]*kernelStat{}
		queueProps = 1 << 1 // CL_QUEUE_PROFILING_ENABLE
	}
	g.queue, errc = clCallPtr(clCreateCommandQueue, g.ctx, g.device, queueProps)
	if errc != 0 {
		return nil, fmt.Errorf("创建 OpenCL 命令队列失败（错误码 %d）", errc)
	}
	src := []byte(kernelProgram)
	srcs := []uintptr{uintptr(unsafe.Pointer(&src[0]))}
	lens := []uintptr{uintptr(len(src))}
	g.prog, errc = clCallPtr(clCreateProgramWithSource, g.ctx, 1, uintptr(unsafe.Pointer(&srcs[0])), uintptr(unsafe.Pointer(&lens[0])))
	runtime.KeepAlive(src)
	if errc != 0 {
		return nil, fmt.Errorf("创建 OpenCL 程序失败（错误码 %d）", errc)
	}
	opts := []byte("\x00")
	if r := clCall(clBuildProgram, g.prog, 1, uintptr(unsafe.Pointer(&g.device)), uintptr(unsafe.Pointer(&opts[0])), 0, 0); r != 0 {
		log := make([]byte, 1<<16)
		clCall(clGetProgramBuildInfo, g.prog, g.device, clProgramBuildLog, uintptr(len(log)), uintptr(unsafe.Pointer(&log[0])), 0)
		return nil, fmt.Errorf("编译 GPU 内核失败（错误码 %d）：%s", r, cString(log))
	}
	g.info = fmt.Sprintf("%s（%s，%s，%d 个计算单元，%.1f GB 显存）", d.name, d.vendor, d.version, d.units, float64(d.mem)/(1<<30))
	return g, nil
}

func (g *gpuBackend) name() string { return "gpu" }

type clKernel struct {
	h     uintptr
	group int // 一维内核使用的工作组大小
}

const clKernelWorkGroupSize = 0x11B0

// kernel 返回名为 name 的内核（调用者持有 g.mu）。
func (g *gpuBackend) kernel(name string) clKernel {
	if k, ok := g.kernels[name]; ok {
		return k
	}
	b := append([]byte(name), 0)
	h, errc := clCallPtr(clCreateKernel, g.prog, uintptr(unsafe.Pointer(&b[0])))
	if errc != 0 {
		rt.Raise("RuntimeError", "找不到 GPU 内核 %s（错误码 %d）", name, errc)
	}
	// 工作组大小取 256 与设备对这个内核的上限中较小的（向下取 2 的幂）
	var maxGroup uintptr
	clCall(clGetKernelWorkGroupInfo, h, g.device, clKernelWorkGroupSize, unsafe.Sizeof(maxGroup), uintptr(unsafe.Pointer(&maxGroup)), 0)
	group := 256
	for group > 1 && uintptr(group) > maxGroup && maxGroup > 0 {
		group >>= 1
	}
	k := clKernel{h: h, group: group}
	g.kernels[name] = k
	return k
}

// ===== 显存 =====

// sizeClass 把请求的字节数向上取整，使大小相近的张量可以复用同一块显存。
func sizeClass(bytes int) int {
	if bytes <= 256 {
		return 256
	}
	if bytes <= 1<<20 {
		s := 256
		for s < bytes {
			s <<= 1
		}
		return s
	}
	// 1 MB 以上：每个 2 倍的区间分成 8 级（最多浪费 12.5%），长度相近的张量可以复用同一个大小的块
	octave := 1 << 20
	for octave*2 < bytes {
		octave <<= 1
	}
	step := octave / 8
	return (bytes + step - 1) / step * step
}

func (g *gpuBackend) poolGet(size int) uintptr {
	g.poolMu.Lock()
	defer g.poolMu.Unlock()
	l := g.pool[size]
	if len(l) == 0 {
		return 0
	}
	m := l[len(l)-1]
	g.pool[size] = l[:len(l)-1]
	g.pooledBytes.Add(-int64(size))
	return m
}

func (g *gpuBackend) poolPut(b *gpuBuf) {
	g.finalized.Add(1)
	g.poolMu.Lock()
	g.pool[b.size] = append(g.pool[b.size], b.mem)
	g.poolMu.Unlock()
	g.pooledBytes.Add(int64(b.size))
	g.liveBytes.Add(-int64(b.size))
}

// freePool 把所有空闲的显存块还给驱动。
func (g *gpuBackend) freePool() {
	g.poolMu.Lock()
	defer g.poolMu.Unlock()
	for size, l := range g.pool {
		for _, m := range l {
			clCall(clReleaseMemObject, m)
		}
		g.pooledBytes.Add(-int64(size * len(l)))
		delete(g.pool, size)
	}
}

type gcSentinel struct {
	p   *int
	pad [32]byte
}

// collect 运行垃圾回收，并等待终结器把不再使用的张量的显存放回池中。
func (g *gpuBackend) collect() {
	g.collects.Add(1)
	g.sinceGC.Store(0)
	done := make(chan struct{})
	s := &gcSentinel{}
	runtime.SetFinalizer(s, func(*gcSentinel) { close(done) })
	s = nil
	runtime.GC()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
	}
	// 终结器在一个单独的协程中依次运行，哨兵不一定是最后一个：等到放回池中的块数不再增加
	last := g.finalized.Load()
	for i := 0; i < 100; i++ {
		time.Sleep(200 * time.Microsecond)
		cur := g.finalized.Load()
		if cur == last {
			break
		}
		last = cur
	}
}

func (g *gpuBackend) createBuffer(size int) uintptr {
	m, errc := clCallPtr(clCreateBuffer, g.ctx, clMemReadWrite, uintptr(size), 0)
	if errc == 0 {
		return m
	}
	// 分配失败：回收不再使用的张量、释放空闲块，然后重试
	g.retries.Add(1)
	g.collect()
	if m := g.poolGet(size); m != 0 {
		return m
	}
	g.freePool()
	m, errc = clCallPtr(clCreateBuffer, g.ctx, clMemReadWrite, uintptr(size), 0)
	if errc != 0 {
		rt.Raise("RuntimeError", "GPU 显存不足：无法分配 %.1f MB（错误码 %d）", float64(size)/(1<<20), errc)
	}
	return m
}

func (g *gpuBackend) alloc(n int) *storage {
	size := sizeClass(max(n, 1) * 4)
	g.sinceGC.Add(int64(size))
	mem := g.poolGet(size)
	if mem == 0 && g.liveBytes.Load()+g.pooledBytes.Load()+int64(size) > g.highWater {
		// 没有合适的空闲块，显存也快满了：先把其它大小的空闲块还给驱动；
		// 缓存中没有多少空闲块时，才回收不再使用的张量（回收要暂停程序，比较慢）
		if g.pooledBytes.Load() >= int64(size) {
			g.freePool()
		} else if g.sinceGC.Load() > g.gcBudget {
			g.collect()
			mem = g.poolGet(size)
		}
	}
	if mem == 0 {
		mem = g.createBuffer(size)
	}
	g.liveBytes.Add(int64(size))
	s := &storage{dev: g, gbuf: &gpuBuf{mem: mem, size: size}}
	runtime.SetFinalizer(s, func(s *storage) { g.poolPut(s.gbuf) })
	return s
}

// GpuMemoryAllocated 返回现有张量占用的显存字节数。
func GpuMemoryAllocated() int {
	if g := gpuInst; g != nil {
		return int(g.liveBytes.Load())
	}
	return 0
}

// GpuMemoryCached 返回缓存池中空闲的显存字节数（张量被回收后留着复用的显存块）。
func GpuMemoryCached() int {
	g := gpuInst
	if g == nil {
		return 0
	}
	g.poolMu.Lock()
	defer g.poolMu.Unlock()
	total := 0
	for size, l := range g.pool {
		total += size * len(l)
	}
	return total
}

// GpuStats 返回显存分配的统计：显存不足时重试的次数等（用于分析性能）。
func GpuStats() string {
	g := gpuInst
	if g == nil {
		return "没有使用 GPU"
	}
	return fmt.Sprintf("正在使用 %.0f MB，缓存 %.0f MB，回收 %d 次，显存不足后重试 %d 次",
		float64(g.liveBytes.Load())/(1<<20), float64(GpuMemoryCached())/(1<<20), g.collects.Load(), g.retries.Load())
}

// EmptyCache 回收不再使用的张量，并把缓存的空闲显存还给驱动。
func EmptyCache() {
	if g := gpuInst; g != nil {
		g.collect()
		g.freePool()
	}
}

func (g *gpuBackend) upload(d []float32) *storage {
	s := g.alloc(len(d))
	if len(d) > 0 {
		g.mu.Lock()
		r := clCall(clEnqueueWriteBuffer, g.queue, s.gbuf.mem, 1, 0, uintptr(len(d)*4), uintptr(unsafe.Pointer(&d[0])), 0, 0, 0)
		g.mu.Unlock()
		g.check(r, "写入显存")
	}
	return s
}

func (g *gpuBackend) download(s *storage, n int) []float32 {
	out := make([]float32, n)
	if n > 0 {
		g.mu.Lock()
		r := clCall(clEnqueueReadBuffer, g.queue, s.gbuf.mem, 1, 0, uintptr(n*4), uintptr(unsafe.Pointer(&out[0])), 0, 0, 0)
		g.mu.Unlock()
		runtime.KeepAlive(s)
		g.check(r, "读取显存")
	}
	return out
}

func (g *gpuBackend) check(r int32, what string) {
	if r != 0 {
		msg := ""
		switch r {
		case clMemObjectAllocFail, clOutOfResources:
			msg = "（显存不足）"
		case clOutOfHostMemory:
			msg = "（内存不足）"
		}
		rt.Raise("RuntimeError", "GPU 操作失败：%s，OpenCL 错误码 %d%s", what, r, msg)
	}
}

// ===== 内核调用 =====

type local int // 局部内存参数的字节数

type int8v [8]int32 // OpenCL 的 int8 向量

// launch 设置参数并提交内核：global 是全局工作项数（每一维），localSize 为 nil 时由驱动决定工作组大小。
func (g *gpuBackend) launch(name string, global, localSize []int, args ...any) {
	for _, n := range global {
		if n == 0 {
			return
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	kern := g.kernel(name)
	k := kern.h
	// 参数的值放在堆上的 vals 中（clSetKernelArg 会立即复制参数的值）
	vals := make([]int64, 4*len(args))
	for i, a := range args {
		size := uintptr(4)
		slot := unsafe.Pointer(&vals[4*i])
		switch v := a.(type) {
		case *storage:
			*(*uintptr)(slot) = v.gbuf.mem
			size = unsafe.Sizeof(uintptr(0))
		case int:
			*(*int32)(slot) = int32(v)
		case uint32:
			*(*uint32)(slot) = v
		case float32:
			*(*float32)(slot) = v
		case bool:
			if v {
				*(*int32)(slot) = 1
			}
		case int8v:
			*(*int8v)(slot) = v
			size = 32
		case local:
			if r := clCall(clSetKernelArg, k, uintptr(i), uintptr(v), 0); r != 0 {
				rt.Raise("RuntimeError", "设置 GPU 内核 %s 的第 %d 个参数失败（错误码 %d）", name, i, r)
			}
			continue
		default:
			panic(fmt.Sprintf("未知的内核参数类型 %T", a))
		}
		if r := clCall(clSetKernelArg, k, uintptr(i), size, uintptr(slot)); r != 0 {
			rt.Raise("RuntimeError", "设置 GPU 内核 %s 的第 %d 个参数失败（错误码 %d）", name, i, r)
		}
	}
	gs := make([]uintptr, 3)
	ls := make([]uintptr, 3)
	for i, n := range global {
		gs[i] = uintptr(n)
	}
	if localSize == nil {
		// 一维内核：工作项数取整到工作组大小的倍数（内核中会跳过多出的工作项）
		gs[0] = uintptr(roundUp(global[0], kern.group))
		ls[0] = uintptr(kern.group)
	} else {
		for i, n := range localSize {
			ls[i] = uintptr(n)
		}
	}
	if g.profiling {
		g.launchProfiled(name, k, len(global), gs, ls)
		runtime.KeepAlive(args)
		return
	}
	r := clCall(clEnqueueNDRangeKernel, g.queue, k, uintptr(len(global)), 0, uintptr(unsafe.Pointer(&gs[0])), uintptr(unsafe.Pointer(&ls[0])), 0, 0, 0)
	if r == clMemObjectAllocFail || r == clOutOfResources {
		// 有的驱动在第一次使用显存块时才真正分配，所以显存不足表现为内核提交失败：
		// 回收不再使用的张量、把缓存的空闲显存还给驱动，再试一次
		g.retries.Add(1)
		clCall(clFinish, g.queue)
		g.collect()
		g.freePool()
		r = clCall(clEnqueueNDRangeKernel, g.queue, k, uintptr(len(global)), 0, uintptr(unsafe.Pointer(&gs[0])), uintptr(unsafe.Pointer(&ls[0])), 0, 0, 0)
	}
	// 内核排队之后才能让参数中的张量被回收（之后复用这块显存的命令一定排在它后面）
	runtime.KeepAlive(args)
	g.check(r, "运行内核 "+name)
}

type kernelStat struct {
	calls int
	nanos uint64
}

// launchProfiled 提交内核并等待它完成，记录 GPU 上的运行时间（调用者持有 g.mu）。
func (g *gpuBackend) launchProfiled(name string, k uintptr, dims int, gs, ls []uintptr) {
	ev := new(uintptr)
	r := clCall(clEnqueueNDRangeKernel, g.queue, k, uintptr(dims), 0, uintptr(unsafe.Pointer(&gs[0])), uintptr(unsafe.Pointer(&ls[0])), 0, 0, uintptr(unsafe.Pointer(ev)))
	g.check(r, "运行内核 "+name)
	clCall(clWaitForEvents, 1, uintptr(unsafe.Pointer(ev)))
	times := make([]uint64, 2)
	clCall(clGetEventProfilingInfo, *ev, 0x1282, 8, uintptr(unsafe.Pointer(&times[0])), 0) // CL_PROFILING_COMMAND_START
	clCall(clGetEventProfilingInfo, *ev, 0x1283, 8, uintptr(unsafe.Pointer(&times[1])), 0) // CL_PROFILING_COMMAND_END
	clCall(clReleaseEvent, *ev)
	st := g.profile[name]
	if st == nil {
		st = &kernelStat{}
		g.profile[name] = st
	}
	st.calls++
	if times[1] > times[0] {
		st.nanos += times[1] - times[0]
	}
}

// GpuProfile 返回各个 GPU 内核的调用次数与总运行时间（需要设置环境变量 AETHER_GPU_PROFILE=1）。
func GpuProfile() string {
	g := gpuInst
	if g == nil || !g.profiling {
		return "没有性能数据：请设置环境变量 AETHER_GPU_PROFILE=1 后再运行程序（并使用 GPU）"
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	type row struct {
		name string
		st   *kernelStat
	}
	var rows []row
	var total uint64
	for name, st := range g.profile {
		rows = append(rows, row{name, st})
		total += st.nanos
	}
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].st.nanos > rows[j-1].st.nanos; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%-18s %8s %10s %7s\n", "内核", "次数", "毫秒", "占比")
	for _, r := range rows {
		fmt.Fprintf(&sb, "%-18s %8d %10.1f %6.1f%%\n", r.name, r.st.calls, float64(r.st.nanos)/1e6, 100*float64(r.st.nanos)/float64(max(total, 1)))
	}
	fmt.Fprintf(&sb, "合计 %.1f 毫秒", float64(total)/1e6)
	return sb.String()
}

func roundUp(n, m int) int { return (n + m - 1) / m * m }

// Synchronize 等待 GPU 上已提交的计算全部完成（计时时使用）。
func Synchronize() {
	if g := gpuInst; g != nil {
		g.mu.Lock()
		clCall(clFinish, g.queue)
		g.mu.Unlock()
	}
}

func (g *gpuBackend) fill(s *storage, n int, v float32) {
	g.launch("fill", []int{n}, nil, s, n, v)
}

func (g *gpuBackend) copyTo(dst, src *storage, n int) {
	g.launch("copy_buf", []int{n}, nil, src, dst, n)
}

func (g *gpuBackend) unary(op int, x, y *storage, n int, a, b float32) {
	g.launch("unary_op", []int{n}, nil, op, x, y, n, a, b)
}

func (g *gpuBackend) binary(op int, x, y, out *storage, n int, bc *bcast) {
	if bc == nil {
		g.launch("binary_same", []int{n}, nil, op, x, y, out, n)
		return
	}
	var shape, sx, sy int8v
	for i := 0; i < bc.ndim; i++ {
		shape[i], sx[i], sy[i] = bc.shape[i], bc.sx[i], bc.sy[i]
	}
	g.launch("binary_bcast", []int{n}, nil, op, x, y, out, n, bc.ndim, shape, sx, sy)
}

func (g *gpuBackend) reduce(op int, x, out, idx *storage, outer, dim, inner int) {
	if op == rSum && outer*inner == 1 && dim > 8192 {
		// 对很长的一段数据求和：先由多个工作组分别求部分和，再把部分和加起来
		groups := min(256, (dim+255)/256)
		part := g.alloc(groups)
		g.launch("reduce_partial", []int{groups * 256}, []int{256}, x, part, dim, local(256*4))
		g.launch("reduce_dim", []int{1}, nil, rSum, part, out, part, 1, groups, 1, false)
		return
	}
	if op == rSum && inner == 1 && dim >= 32 && outer > 1 {
		// 对每一行求和（例如 softmax 的反向）：一个工作组处理一行
		l := rowGroup(dim)
		g.launch("reduce_rows", []int{outer * l}, []int{l}, x, out, outer, dim, local(l*4))
		return
	}
	if op == rSum && dim > 1024 && outer*inner < 1<<14 {
		// 输出很少、每个输出要加很多数：先分段求和（并行度高得多），再把各段的和加起来
		const chunk = 256
		chunks := (dim + chunk - 1) / chunk
		part := g.alloc(outer * chunks * inner)
		g.launch("reduce_chunks", []int{outer * chunks * inner}, nil, x, part, outer, dim, inner, chunk, chunks)
		g.launch("reduce_dim", []int{outer * inner}, nil, rSum, part, out, out, outer, chunks, inner, false)
		return
	}
	want := idx != nil
	if idx == nil {
		idx = out
	}
	g.launch("reduce_dim", []int{outer * inner}, nil, op, x, out, idx, outer, dim, inner, want)
}

func (g *gpuBackend) matmul(a, b, c *storage, batch, m, k, n int, ta, tb bool, sa, sb int) {
	if k <= 32 || m <= 32 || n <= 32 {
		// 小矩阵或很窄的矩阵：分块的内核会浪费大部分工作项
		g.launch("matmul_small", []int{roundUp(n, 16), roundUp(m, 16), batch}, []int{16, 16, 1}, a, b, c, m, n, k, ta, tb, sa, sb)
		return
	}
	if m >= 128 && n >= 128 && k >= 16 && !ta && k%4 == 0 && n%4 == 0 && sa%4 == 0 && sb%4 == 0 {
		// 大矩阵、各维是 4 的倍数：向量化的 128x128 输出块
		global := []int{roundUp(n, 128) / 8, roundUp(m, 128) / 8, batch}
		g.launch("matmul_big4", global, []int{16, 16, 1}, a, b, c, m, n, k, tb, sa, sb)
		return
	}
	if m >= 128 && n >= 128 && k >= 16 {
		// 大矩阵：128x128 的输出块
		global := []int{roundUp(n, 128) / 8, roundUp(m, 128) / 8, batch}
		g.launch("matmul_big", global, []int{16, 16, 1}, a, b, c, m, n, k, ta, tb, sa, sb)
		return
	}
	global := []int{roundUp(n, 64) / 4, roundUp(m, 64) / 4, batch}
	g.launch("matmul", global, []int{16, 16, 1}, a, b, c, m, n, k, ta, tb, sa, sb)
}

func (g *gpuBackend) softmax(x, y *storage, rows, cols int, log bool) {
	l := rowGroup(cols)
	g.launch("softmax_rows", []int{rows * l}, []int{l}, x, y, rows, cols, log, local(l*4))
}

// rowGroup 返回按行处理的内核使用的工作组大小（2 的幂，32 到 256）。
func rowGroup(cols int) int {
	l := 32
	for l < cols && l < 256 {
		l <<= 1
	}
	return l
}

func (g *gpuBackend) layerNorm(x, w, b, y, xhat, rstd *storage, rows, d int, eps float32) {
	l := rowGroup(d)
	g.launch("layer_norm", []int{rows * l}, []int{l}, x, w, b, y, xhat, rstd, rows, d, eps, local(l*4))
}

func (g *gpuBackend) layerNormBack(dy, w, xhat, rstd, dx *storage, rows, d int) {
	l := rowGroup(d)
	g.launch("layer_norm_back", []int{rows * l}, []int{l}, dy, w, xhat, rstd, dx, rows, d, local(2*l*4))
}

func (g *gpuBackend) rmsNorm(x, w, y, xhat, rstd *storage, rows, d int, eps float32) {
	l := rowGroup(d)
	g.launch("rms_norm", []int{rows * l}, []int{l}, x, w, y, xhat, rstd, rows, d, eps, local(l*4))
}

func (g *gpuBackend) rmsNormBack(dy, w, xhat, rstd, dx *storage, rows, d int) {
	l := rowGroup(d)
	g.launch("rms_norm_back", []int{rows * l}, []int{l}, dy, w, xhat, rstd, dx, rows, d, local(l*4))
}

func (g *gpuBackend) permute(x, y *storage, shape, perm []int) {
	nd := len(shape)
	if nd > maxDims {
		raiseShape("permute 最多支持 %d 维", maxDims)
	}
	inStride := make([]int, nd)
	s := 1
	for d := nd - 1; d >= 0; d-- {
		inStride[d] = s
		s *= shape[d]
	}
	var oshape, strides int8v
	for d, p := range perm {
		oshape[d] = int32(shape[p])
		strides[d] = int32(inStride[p])
	}
	g.launch("permute", []int{numel(shape)}, nil, x, y, numel(shape), nd, oshape, strides)
}

func (g *gpuBackend) narrow(x, y *storage, outer, dim, inner, start, length int) {
	g.launch("narrow", []int{outer * length * inner}, nil, x, y, outer, dim, inner, start, length)
}

func (g *gpuBackend) place(x, y *storage, outer, dim, inner, start, length int) {
	g.launch("place", []int{outer * length * inner}, nil, x, y, outer, dim, inner, start, length)
}

func (g *gpuBackend) indexSelect(w, idx, out *storage, nidx, rows, cols int) {
	g.launch("index_select", []int{nidx * cols}, nil, w, idx, out, nidx, rows, cols)
}

// indexAdd 先在内存中按目标行给梯度行排序（下标张量很小），再由每个工作项求一个输出元素的和。
// 直接用原子加法时，很多行加到同一个位置（例如位置编码、只有两行的段编码）会严重冲突。
func (g *gpuBackend) indexAdd(gr, idx, out *storage, nidx, rows, cols int) {
	h := g.download(idx, nidx)
	offsets := make([]float32, rows+1)
	for _, v := range h {
		if r := int(v); r >= 0 && r < rows {
			offsets[r+1]++
		}
	}
	for r := 0; r < rows; r++ {
		offsets[r+1] += offsets[r]
	}
	next := append([]float32(nil), offsets[:rows]...)
	order := make([]float32, int(offsets[rows]))
	for i, v := range h {
		if r := int(v); r >= 0 && r < rows {
			order[int(next[r])] = float32(i)
			next[r]++
		}
	}
	if len(order) == 0 {
		return
	}
	og, oo := g.upload(order), g.upload(offsets)
	g.launch("index_add_csr", []int{rows * cols}, nil, gr, og, oo, out, rows, cols)
}

func (g *gpuBackend) pick(x, idx, out *storage, rows, cols int) {
	g.launch("pick", []int{rows}, nil, x, idx, out, rows, cols)
}

func (g *gpuBackend) pickBack(gr, idx, out *storage, rows, cols int) {
	g.launch("pick_back", []int{rows}, nil, gr, idx, out, rows, cols)
}

func convArgs(p *convParams) []any {
	return []any{p.n, p.c, p.h, p.w, p.kh, p.kw, p.sh, p.sw, p.ph, p.pw, p.oh, p.ow}
}

func (g *gpuBackend) im2col(x, cols *storage, p *convParams) {
	g.launch("im2col", []int{p.n * p.colRows() * p.colCols()}, nil, append([]any{x, cols}, convArgs(p)...)...)
}

func (g *gpuBackend) col2im(cols, x *storage, p *convParams) {
	g.launch("col2im", []int{p.n * p.c * p.h * p.w}, nil, append([]any{cols, x}, convArgs(p)...)...)
}

func (g *gpuBackend) maxpool(x, y, idx *storage, p *convParams) {
	g.launch("maxpool", []int{p.n * p.c * p.oh * p.ow}, nil, append([]any{x, y, idx}, convArgs(p)...)...)
}

func (g *gpuBackend) maxpoolBack(gr, idx, dx *storage, p *convParams) {
	g.launch("maxpool_back", []int{p.n * p.c * p.oh * p.ow}, nil, gr, idx, dx, p.n, p.c, p.h, p.w, p.oh, p.ow)
}

func (g *gpuBackend) random(s *storage, n int, seed uint64, normal bool) {
	g.launch("random_fill", []int{n}, nil, s, n, uint32(seed), uint32(seed>>32), normal)
}

func (g *gpuBackend) dropout(x, y *storage, n int, seed uint64, p, scale float32) {
	g.launch("dropout", []int{n}, nil, x, y, n, uint32(seed), uint32(seed>>32), p, scale)
}

func (g *gpuBackend) adam(p, gr, m, v *storage, n int, lr, b1, b2, eps, wd, bc1, bc2 float32, decoupled bool) {
	g.launch("adam_step", []int{n}, nil, p, gr, m, v, n, lr, b1, b2, eps, wd, bc1, bc2, decoupled)
}

func (g *gpuBackend) sgd(p, gr, buf *storage, n int, lr, momentum, wd float32, useBuf bool) {
	if buf == nil {
		buf = p
	}
	g.launch("sgd_step", []int{n}, nil, p, gr, buf, n, lr, momentum, wd, useBuf)
}
