// Package rt 是 Aether 程序的运行时库。生成的 Go 代码依赖这里的类型与函数。
package rt

import (
	"bufio"
	"io"
	"os"
	"runtime/debug"
	"sync"
)

// Char 是 Aether 的 char 类型（一个 Unicode 码点）。
type Char int32

// Unit 用于没有返回值的 Task。
type Unit = struct{}

// NoIdx 表示切片中省略的下标。
const NoIdx = -1 << 63

// Integer、Number 是泛型约束。
type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

type Number interface {
	Integer | ~float32 | ~float64
}

type Ordered interface {
	Number | ~string
}

// ===== 标准输出 =====

var (
	outMu       sync.Mutex
	out         = bufio.NewWriterSize(os.Stdout, 64*1024)
	interactive = isTerminal(os.Stdout)
	stdin       *bufio.Reader
)

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

// Write 写出字符串到标准输出。
func Write(s string) {
	outMu.Lock()
	out.WriteString(s)
	if interactive {
		out.Flush()
	}
	outMu.Unlock()
}

// Flush 刷新标准输出缓冲。
func Flush() {
	outMu.Lock()
	out.Flush()
	outMu.Unlock()
}

// Print 实现 print(*args)。
func Print(args ...any) {
	PrintSE(" ", "\n", args...)
}

// PrintSE 实现 print(*args, sep=, end=)。
func PrintSE(sep, end string, args ...any) {
	outMu.Lock()
	for i, a := range args {
		if i > 0 {
			out.WriteString(sep)
		}
		if s, ok := a.(string); ok {
			out.WriteString(s)
		} else {
			out.WriteString(Str(a))
		}
	}
	out.WriteString(end)
	if interactive {
		out.Flush()
	}
	outMu.Unlock()
}

// PrintStr 是 print(s) 的快速路径。
func PrintStr(s string) {
	outMu.Lock()
	out.WriteString(s)
	out.WriteByte('\n')
	if interactive {
		out.Flush()
	}
	outMu.Unlock()
}

// Input 实现 input(prompt)。
func Input(prompt string) string {
	if prompt != "" {
		Write(prompt)
	}
	Flush()
	if stdin == nil {
		stdin = bufio.NewReader(os.Stdin)
	}
	line, err := stdin.ReadString('\n')
	if err != nil {
		if err == io.EOF && line != "" {
			return trimNewline(line)
		}
		if err == io.EOF {
			Raise("EOFError", "输入已结束（EOF）")
		}
		Raise("IOError", "读取输入失败：%v", err)
	}
	return trimNewline(line)
}

func trimNewline(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[len(s)-1] == '\r' {
		s = s[:len(s)-1]
	}
	return s
}

// Exit 刷新输出后退出进程。
func Exit(code int) {
	Flush()
	os.Exit(code)
}

// Run 运行主程序；未捕获的异常会打印回溯并以状态 1 退出。
func Run(body func()) {
	defer func() {
		if r := recover(); r != nil {
			pcs := PCsOf(r)
			if pcs == nil {
				pcs = callers(3)
			}
			Flush()
			PrintUncaught(r, pcs)
			os.Exit(1)
		}
		Flush()
	}()
	body()
}

// GoGuard 用在 go 语句启动的协程中：未捕获的异常会终止整个程序。
func GoGuard() {
	if r := recover(); r != nil {
		pcs := PCsOf(r)
		if pcs == nil {
			pcs = callers(3)
		}
		Flush()
		os.Stderr.WriteString("协程中出现未捕获的异常：\n")
		PrintUncaught(r, pcs)
		os.Exit(1)
	}
}

// Val 返回参数本身；用于阻止 Go 编译器以任意精度折叠常量表达式。
func Val[T any](x T) T { return x }

// If 是简单条件表达式的辅助函数（两个分支都已求值）。
func If[T any](c bool, a, b T) T {
	if c {
		return a
	}
	return b
}

// Is 判断 x 的动态类型是否为 T。
func Is[T any](x any) bool {
	_, ok := x.(T)
	return ok
}

// TryCast 实现 x as? T。
func TryCast[T any](x any) Opt[T] {
	v, ok := x.(T)
	return Opt[T]{V: v, Ok: ok}
}

// Cast 实现 x as T（失败时抛出 TypeError）。
func Cast[T any](x any, target string) T {
	v, ok := x.(T)
	if !ok {
		Raise("TypeError", "无法把 %s 转换为 %s", TypeName(x), target)
	}
	return v
}

// NotNull 实现 any 值的 x!。
func NotNull(x any) any {
	if x == nil {
		Raise("NullError", "值为 null")
	}
	return x
}

func BoolAnd(a, b bool) bool { return a && b }
func BoolOr(a, b bool) bool  { return a || b }
func BoolXor(a, b bool) bool { return a != b }

func B2I(b bool) int {
	if b {
		return 1
	}
	return 0
}

func init() {
	// 无限递归时尽早失败（默认 1GB 太大）
	debug.SetMaxStack(256 << 20)
}
