package rt

import (
	"sync"
	"sync/atomic"
	"time"
)

// Task 是 async 函数调用的结果（在独立协程中运行）。
type Task[T any] struct {
	done chan struct{}
	val  T
	exc  any
}

// Async 在新协程中运行 f，返回可以 await 的 Task。
func Async[T any](f func() T) *Task[T] {
	t := &Task[T]{done: make(chan struct{})}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				if PCsOf(r) == nil {
					r = &Thrown{Exc: r, PCs: callers(3)}
				}
				t.exc = r
			}
			close(t.done)
		}()
		t.val = f()
	}()
	return t
}

// Await 等待任务完成并返回结果；任务中的异常会在这里重新抛出。
func (t *Task[T]) Await() T {
	<-t.done
	if t.exc != nil {
		panic(t.exc)
	}
	return t.val
}

func (t *Task[T]) Done() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

func (t *Task[T]) Wait(seconds float64) bool {
	select {
	case <-t.done:
		return true
	case <-time.After(time.Duration(seconds * float64(time.Second))):
		return false
	}
}

func (t *Task[T]) String() string {
	if t.Done() {
		return "<Task 已完成>"
	}
	return "<Task 运行中>"
}

// AwaitAll 等待列表中的所有任务。
func AwaitAll[T any](ts *List[*Task[T]]) *List[T] {
	out := make([]T, len(ts.S))
	for i, t := range ts.S {
		out[i] = t.Await()
	}
	return &List[T]{S: out}
}

func AwaitAllVoid[T any](ts *List[*Task[T]]) {
	for _, t := range ts.S {
		t.Await()
	}
}

func Gather[T any](ts *List[*Task[T]]) *List[T] { return AwaitAll(ts) }

// ParallelMap 并行地对每个元素调用 f。
func ParallelMap[T, U any](l *List[T], f func(T) U) *List[U] {
	out := make([]U, len(l.S))
	var wg sync.WaitGroup
	var first atomic.Value
	for i, x := range l.S {
		wg.Add(1)
		go func(i int, x T) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					first.CompareAndSwap(nil, r)
				}
			}()
			out[i] = f(x)
		}(i, x)
	}
	wg.Wait()
	if r := first.Load(); r != nil {
		panic(r)
	}
	return &List[U]{S: out}
}

// ===== 同步原语 =====

type Mutex struct{ mu sync.Mutex }

func NewMutex() *Mutex      { return &Mutex{} }
func (m *Mutex) Lock()      { m.mu.Lock() }
func (m *Mutex) Unlock()    { m.mu.Unlock() }
func (m *Mutex) TryLock() bool { return m.mu.TryLock() }
func (m *Mutex) String() string { return "<Mutex>" }

type RWMutex struct{ mu sync.RWMutex }

func NewRWMutex() *RWMutex     { return &RWMutex{} }
func (m *RWMutex) Lock()       { m.mu.Lock() }
func (m *RWMutex) Unlock()     { m.mu.Unlock() }
func (m *RWMutex) RLock()      { m.mu.RLock() }
func (m *RWMutex) RUnlock()    { m.mu.RUnlock() }

type WaitGroup struct{ wg sync.WaitGroup }

func NewWaitGroup() *WaitGroup       { return &WaitGroup{} }
func (w *WaitGroup) Add(n int)       { w.wg.Add(n) }
func (w *WaitGroup) Done()           { w.wg.Done() }
func (w *WaitGroup) Wait()           { w.wg.Wait() }
func (w *WaitGroup) Go(f func()) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		defer GoGuard()
		f()
	}()
}

type AtomicInt struct{ v atomic.Int64 }

func NewAtomicInt(v int) *AtomicInt {
	a := &AtomicInt{}
	a.v.Store(int64(v))
	return a
}
func (a *AtomicInt) Get() int        { return int(a.v.Load()) }
func (a *AtomicInt) Set(v int)       { a.v.Store(int64(v)) }
func (a *AtomicInt) Add(d int) int   { return int(a.v.Add(int64(d))) }
func (a *AtomicInt) Incr() int       { return int(a.v.Add(1)) }
func (a *AtomicInt) String() string  { return Itoa(a.Get()) }
func (a *AtomicInt) CompareAndSwap(old, new int) bool {
	return a.v.CompareAndSwap(int64(old), int64(new))
}

// ===== 通道 =====

// Recv 实现 ch.recv()：通道关闭且为空时抛出 ChannelClosedError。
func Recv[T any](ch chan T) T {
	v, ok := <-ch
	if !ok {
		Raise("ChannelClosedError", "通道已关闭")
	}
	return v
}

func TryRecv[T any](ch chan T) Opt[T] {
	select {
	case v, ok := <-ch:
		if !ok {
			return Opt[T]{}
		}
		return Opt[T]{V: v, Ok: true}
	default:
		return Opt[T]{}
	}
}

func RecvOpt[T any](ch chan T) Opt[T] {
	v, ok := <-ch
	if !ok {
		return Opt[T]{}
	}
	return Opt[T]{V: v, Ok: true}
}

func TrySend[T any](ch chan T, v T) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
		}
	}()
	select {
	case ch <- v:
		return true
	default:
		return false
	}
}

func ClosedErr() any {
	return &Thrown{Exc: &Error{Kind: "ChannelClosedError", Msg: "通道已关闭"}, PCs: callers(3)}
}

// After 返回一个在 seconds 秒后收到 true 的通道。
func After(seconds float64) chan bool {
	ch := make(chan bool, 1)
	time.AfterFunc(time.Duration(seconds*float64(time.Second)), func() { ch <- true })
	return ch
}

// Tick 返回每隔 seconds 秒收到一次 true 的通道。
func Tick(seconds float64) chan bool {
	ch := make(chan bool, 1)
	go func() {
		t := time.NewTicker(time.Duration(seconds * float64(time.Second)))
		for range t.C {
			select {
			case ch <- true:
			default:
			}
		}
	}()
	return ch
}
