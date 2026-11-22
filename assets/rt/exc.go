package rt

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Error 是运行时库抛出的异常（由生成代码转换为对应的五行异常类）。
type Error struct {
	Kind string
	Msg  string
}

func (e *Error) Error() string { return e.Kind + ": " + e.Msg }

// Raise 抛出一个运行时异常。
func Raise(kind, format string, args ...any) {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	panic(&Thrown{Exc: &Error{Kind: kind, Msg: msg}, PCs: callers(3)})
}

// Thrown 包装被抛出的异常对象及抛出点的调用栈。
type Thrown struct {
	Exc any
	PCs []uintptr
}

func (t *Thrown) Error() string { return fmt.Sprint(t.Exc) }

// Throw 包装五行代码中 raise 的异常对象。
func Throw(exc any) *Thrown {
	return &Thrown{Exc: exc, PCs: callers(3)}
}

func callers(skip int) []uintptr {
	pcs := make([]uintptr, 64)
	n := runtime.Callers(skip, pcs)
	return pcs[:n]
}

// PCsOf 返回异常抛出点的调用栈（若有）。
func PCsOf(r any) []uintptr {
	if t, ok := r.(*Thrown); ok {
		return t.PCs
	}
	return nil
}

var (
	// ConvertHook 由生成代码设置：把 *Error 转换为五行异常对象。
	ConvertHook func(e *Error) any
	// DescribeHook 由生成代码设置：返回异常对象的类名和消息。
	DescribeHook func(exc any) (string, string)
	// IsExcHook 判断值是否为五行异常对象。
	IsExcHook func(x any) bool
)

// Catch 把 recover() 得到的值转换为五行异常对象。
func Catch(r any) any {
	if t, ok := r.(*Thrown); ok {
		r = t.Exc
	}
	if IsExcHook != nil && IsExcHook(r) {
		return r
	}
	var e *Error
	switch x := r.(type) {
	case *Error:
		e = x
	default:
		kind, msg := Classify(x)
		e = &Error{Kind: kind, Msg: msg}
	}
	if ConvertHook != nil {
		return ConvertHook(e)
	}
	return e
}

// Classify 把 Go 运行时错误归类为五行异常类型。
func Classify(r any) (string, string) {
	if err, ok := r.(runtime.Error); ok {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "index out of range"):
			return "IndexError", "下标越界：" + strings.TrimPrefix(msg, "runtime error: ")
		case strings.Contains(msg, "slice bounds out of range"):
			return "IndexError", "切片越界：" + strings.TrimPrefix(msg, "runtime error: ")
		case strings.Contains(msg, "integer divide by zero"):
			return "ZeroDivisionError", "整数除以零"
		case strings.Contains(msg, "nil pointer dereference"), strings.Contains(msg, "invalid memory address"):
			return "NullError", "访问了 null 值（对象未初始化）"
		case strings.Contains(msg, "assignment to entry in nil map"):
			return "NullError", "字典未初始化"
		case strings.Contains(msg, "interface conversion"):
			return "TypeError", "类型转换失败：" + strings.TrimPrefix(msg, "interface conversion: ")
		case strings.Contains(msg, "hash of unhashable type"):
			return "TypeError", "值不可哈希：" + msg
		case strings.Contains(msg, "comparing uncomparable"):
			return "TypeError", "值不可比较：" + msg
		case strings.Contains(msg, "makeslice"):
			return "ValueError", "长度无效：" + msg
		case strings.Contains(msg, "negative shift amount"):
			return "ValueError", "移位位数不能为负数"
		}
		return "RuntimeError", msg
	}
	switch x := r.(type) {
	case error:
		msg := x.Error()
		switch {
		case strings.Contains(msg, "send on closed channel"), strings.Contains(msg, "close of closed channel"):
			return "ChannelClosedError", "通道已关闭"
		case strings.Contains(msg, "close of nil channel"):
			return "NullError", "通道未初始化"
		case strings.Contains(msg, "negative Repeat count"):
			return "ValueError", "重复次数不能为负数"
		case strings.Contains(msg, "all goroutines are asleep"):
			return "RuntimeError", "死锁：所有协程都在等待"
		}
		return "RuntimeError", msg
	case string:
		if strings.Contains(x, "send on closed channel") || strings.Contains(x, "close of closed channel") {
			return "ChannelClosedError", "通道已关闭"
		}
		return "RuntimeError", x
	}
	return "RuntimeError", fmt.Sprint(r)
}

// ===== 回溯 =====

var srcCache sync.Map

func srcLine(file string, line int) string {
	v, ok := srcCache.Load(file)
	if !ok {
		data, err := os.ReadFile(file)
		var lines []string
		if err == nil {
			s := strings.ReplaceAll(string(data), "\r\n", "\n")
			lines = strings.Split(s, "\n")
		}
		srcCache.Store(file, lines)
		v = lines
	}
	lines := v.([]string)
	if line < 1 || line > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[line-1])
}

// demangle 把 Go 函数名还原为五行中的名字。
func demangle(fn string) string {
	if i := strings.LastIndex(fn, "/"); i >= 0 {
		fn = fn[i+1:]
	}
	fn = strings.TrimPrefix(fn, "main.")
	// 方法：(*Dog).speak 或 Dog.speak
	fn = strings.ReplaceAll(fn, "(*", "")
	fn = strings.ReplaceAll(fn, ")", "")
	if i := strings.Index(fn, "["); i >= 0 {
		if j := strings.Index(fn[i:], "]"); j >= 0 {
			fn = fn[:i] + fn[i+j+1:]
		}
	}
	isLambda := false
	parts := strings.Split(fn, ".")
	var out []string
	for _, p := range parts {
		if strings.HasPrefix(p, "func") && len(p) > 4 && p[4] >= '0' && p[4] <= '9' {
			isLambda = true
			continue
		}
		if p == "gowrap1" || strings.HasPrefix(p, "deferwrap") || strings.HasPrefix(p, "gowrap") {
			continue
		}
		out = append(out, p)
	}
	name := strings.Join(out, ".")
	name = strings.ReplaceAll(name, "ꞏꞏ", ".")
	switch {
	case strings.HasPrefix(name, "ꞏinit_"):
		return "<模块 " + strings.TrimPrefix(name, "ꞏinit_") + ">"
	case name == "main" || name == "main.main":
		return "<主程序>"
	case strings.HasPrefix(name, "ꞏtest"):
		return "<测试>"
	}
	name = strings.ReplaceAll(name, "ꞏnew", ".__init__")
	name = strings.ReplaceAll(name, ".ꞏinit", ".__init__")
	if i := strings.Index(name, "ꞏsuperꞏ"); i >= 0 {
		rest := name[i+len("ꞏsuperꞏ"):]
		name = strings.ReplaceAll(rest, "ꞏ", ".")
	}
	name = strings.ReplaceAll(name, "ꞏget_", "")
	name = strings.ReplaceAll(name, "ꞏset_", "")
	name = strings.ReplaceAll(name, "ꞏ", "")
	if isLambda && name == "" {
		return "<lambda>"
	}
	if isLambda {
		name += " 中的闭包"
	}
	return name
}

type frame struct {
	file string
	line int
	fn   string
}

func aeFrames(pcs []uintptr) []frame {
	var out []frame
	if len(pcs) == 0 {
		return nil
	}
	frames := runtime.CallersFrames(pcs)
	for {
		f, more := frames.Next()
		if strings.HasSuffix(f.File, ".ae") && !strings.Contains(f.File, "<stdlib>") {
			out = append(out, frame{file: f.File, line: f.Line, fn: demangle(f.Function)})
		}
		if !more {
			break
		}
	}
	// 反转：最近的调用在最后
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func displayPath(p string) string {
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, p); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return p
}

// FormatTraceback 生成异常的回溯文本。
func FormatTraceback(r any, pcs []uintptr) string {
	var sb strings.Builder
	frames := aeFrames(pcs)
	if len(frames) > 0 {
		sb.WriteString("回溯（最近的调用在最后）：\n")
		for _, f := range frames {
			fmt.Fprintf(&sb, "  文件 \"%s\"，第 %d 行，在 %s\n", displayPath(f.file), f.line, f.fn)
			if s := srcLine(f.file, f.line); s != "" {
				fmt.Fprintf(&sb, "    %s\n", s)
			}
		}
	}
	exc := Catch(r)
	name, msg := "RuntimeError", fmt.Sprint(exc)
	if DescribeHook != nil && IsExcHook != nil && IsExcHook(exc) {
		name, msg = DescribeHook(exc)
	} else if e, ok := exc.(*Error); ok {
		name, msg = e.Kind, e.Msg
	}
	if msg != "" {
		fmt.Fprintf(&sb, "%s: %s\n", name, msg)
	} else {
		fmt.Fprintf(&sb, "%s\n", name)
	}
	return sb.String()
}

// PrintUncaught 把未捕获的异常打印到标准错误。
func PrintUncaught(r any, pcs []uintptr) {
	os.Stderr.WriteString(FormatTraceback(r, pcs))
}

// ExcMessage 返回异常对象的消息文本。
func ExcMessage(exc any) string {
	if DescribeHook != nil && IsExcHook != nil && IsExcHook(exc) {
		_, msg := DescribeHook(exc)
		return msg
	}
	return fmt.Sprint(exc)
}

// Traceback 返回当前（或给定）异常的回溯文本，供 except 块中使用。
func Traceback(r any) string {
	return FormatTraceback(r, PCsOf(r))
}

// ===== 断言与测试 =====

// AssertFail 抛出 AssertionError。
func AssertFail(src string, msg string) {
	text := "断言失败：" + src
	if msg != "" {
		text = msg + "（" + src + "）"
	}
	panic(&Thrown{Exc: &Error{Kind: "AssertionError", Msg: text}, PCs: callers(3)})
}

// AssertEqFail 抛出带有左右值的 AssertionError。
func AssertEqFail(src, op, left, right, msg string) {
	text := fmt.Sprintf("断言失败：%s\n  左边：%s\n  右边：%s", src, left, right)
	if msg != "" {
		text = fmt.Sprintf("%s（%s）\n  左边：%s\n  右边：%s", msg, src, left, right)
	}
	panic(&Thrown{Exc: &Error{Kind: "AssertionError", Msg: text}, PCs: callers(3)})
}

// Unreachable 用于满足 Go 对函数终止语句的要求。
func Unreachable() any {
	return &Error{Kind: "RuntimeError", Msg: "内部错误：执行到了不可达的位置"}
}
