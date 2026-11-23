// aether 是 Aether 语言的命令行工具。
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"aether/assets"
	"aether/compiler"
)

const usage = `Aether %s —— 融合 Python、C++、Go、Java、C# 优点的编程语言

用法：
  aether run   <文件.ae> [参数...]   编译并运行程序
  aether build <文件.ae> [-o 输出]   编译为原生可执行文件
  aether check <文件.ae>             只做语法和类型检查
  aether test  <文件.ae>             运行文件中的 test "..." 测试块
  aether emit  <文件.ae>             显示生成的 Go 代码
  aether repl                        交互模式
  aether lsp                         语言服务器（供编辑器使用）
  aether version                     显示版本
  aether help                        显示本帮助

环境变量：
  AETHER_CACHE   构建缓存目录（默认在用户缓存目录下）
  AETHER_VERBOSE 设为 1 时显示编译耗时
`

func main() {
	if len(os.Args) < 2 {
		fmt.Printf(usage, compiler.Version)
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "run":
		os.Exit(cmdRun(args))
	case "build":
		os.Exit(cmdBuild(args))
	case "check":
		os.Exit(cmdCheck(args))
	case "test":
		os.Exit(cmdTest(args))
	case "emit":
		os.Exit(cmdEmit(args))
	case "repl":
		os.Exit(cmdRepl(args))
	case "lsp":
		os.Exit(cmdLSP(args))
	case "version", "--version", "-v":
		fmt.Printf("Aether %s\n", compiler.Version)
	case "help", "--help", "-h":
		fmt.Printf(usage, compiler.Version)
	default:
		if strings.HasSuffix(cmd, ".ae") {
			os.Exit(cmdRun(os.Args[1:]))
		}
		fmt.Fprintf(os.Stderr, "未知命令 %q\n\n", cmd)
		fmt.Printf(usage, compiler.Version)
		os.Exit(2)
	}
}

func cacheDir() string {
	if d := os.Getenv("AETHER_CACHE"); d != "" {
		return d
	}
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "aether")
	}
	return filepath.Join(os.TempDir(), "aether-cache")
}

func verbose() bool { return os.Getenv("AETHER_VERBOSE") == "1" }

func useColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	st, err := os.Stderr.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// load 加载并检查程序；出错时打印诊断信息。
func load(file string, testMode bool, opts compiler.GenOptions) (string, bool) {
	if _, err := os.Stat(file); err != nil {
		fmt.Fprintf(os.Stderr, "找不到文件 %s\n", file)
		return "", false
	}
	start := time.Now()
	diag := &compiler.Diagnostics{}
	prog := compiler.Load(file, assets.Lib(), diag)
	src, err := compiler.Compile(prog, testMode, opts)
	if len(diag.List) > 0 {
		fmt.Fprint(os.Stderr, diag.Format(useColor()))
	}
	if err != nil {
		if diag.HasErrors() {
			n := diag.ErrorCount()
			if n > compiler.ErrLimit {
				fmt.Fprintf(os.Stderr, "错误太多，只显示前 %d 个\n", compiler.ErrLimit)
			} else {
				fmt.Fprintf(os.Stderr, "共 %d 个错误\n", n)
			}
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		return "", false
	}
	if verbose() {
		fmt.Fprintf(os.Stderr, "[aether] 检查与生成用时 %.3fs\n", time.Since(start).Seconds())
	}
	return src, true
}

func build(file, src string, testMode bool) (string, bool) {
	extra := ""
	if testMode {
		extra = "test"
	}
	cfg := compiler.BuildConfig{CacheDir: cacheDir(), RT: assets.RT(), Verbose: verbose()}
	exe, err := compiler.BuildGo(src, compiler.BuildKey(file, extra), cfg)
	if err != nil {
		var be *compiler.GoBuildError
		if errors.As(err, &be) {
			fmt.Fprintln(os.Stderr, "内部错误：生成的 Go 代码无法编译。这是 Aether 编译器的 bug，下面是 Go 编译器的输出：")
			fmt.Fprintln(os.Stderr, be.Output)
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		return "", false
	}
	return exe, true
}

// fatalFilter 把 Go 运行时的致命错误翻译为简短的中文说明。
type fatalFilter struct {
	buf      []byte
	suppress bool
}

var fatalHints = []struct{ prefix, msg string }{
	{"runtime: goroutine stack exceeds", "RecursionError: 递归太深，栈空间耗尽（是否存在无限递归？）"},
	{"fatal error: stack overflow", "RecursionError: 递归太深，栈空间耗尽（是否存在无限递归？）"},
	{"fatal error: all goroutines are asleep - deadlock!", "死锁：所有协程都在等待（例如从一个没有人发送数据的通道接收，或忘记 close 通道）"},
	{"fatal error: concurrent map writes", "并发错误：多个协程同时修改同一个字典，请用 Mutex 保护"},
	{"fatal error: concurrent map read and map write", "并发错误：多个协程同时读写同一个字典，请用 Mutex 保护"},
	{"fatal error: out of memory", "MemoryError: 内存不足"},
}

func (f *fatalFilter) Write(p []byte) (int, error) {
	if f.suppress {
		return len(p), nil
	}
	f.buf = append(f.buf, p...)
	for {
		i := bytes.IndexByte(f.buf, '\n')
		if i < 0 {
			break
		}
		line := string(f.buf[:i+1])
		f.buf = f.buf[i+1:]
		for _, h := range fatalHints {
			if strings.HasPrefix(line, h.prefix) {
				os.Stderr.WriteString(h.msg + "\n")
				f.suppress = true
				return len(p), nil
			}
		}
		os.Stderr.WriteString(line)
	}
	return len(p), nil
}

func (f *fatalFilter) flush() {
	if !f.suppress && len(f.buf) > 0 {
		os.Stderr.Write(f.buf)
	}
}

func runExe(exe string, args []string) int {
	c := exec.Command(exe, args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	ff := &fatalFilter{}
	c.Stderr = ff
	defer ff.flush()
	if err := c.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func needFile(args []string, cmd string) (string, []string, bool) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "用法：aether %s <文件.ae>\n", cmd)
		return "", nil, false
	}
	return args[0], args[1:], true
}

func cmdRun(args []string) int {
	file, rest, ok := needFile(args, "run")
	if !ok {
		return 2
	}
	src, ok := load(file, false, compiler.GenOptions{})
	if !ok {
		return 1
	}
	exe, ok := build(file, src, false)
	if !ok {
		return 1
	}
	return runExe(exe, rest)
}

func cmdTest(args []string) int {
	file, rest, ok := needFile(args, "test")
	if !ok {
		return 2
	}
	src, ok := load(file, true, compiler.GenOptions{})
	if !ok {
		return 1
	}
	exe, ok := build(file, src, true)
	if !ok {
		return 1
	}
	return runExe(exe, rest)
}

func cmdCheck(args []string) int {
	file, _, ok := needFile(args, "check")
	if !ok {
		return 2
	}
	if _, ok := load(file, false, compiler.GenOptions{}); !ok {
		return 1
	}
	fmt.Println("检查通过：没有发现错误")
	return 0
}

func cmdEmit(args []string) int {
	file, _, ok := needFile(args, "emit")
	if !ok {
		return 2
	}
	src, ok := load(file, false, compiler.GenOptions{NoLines: true})
	if !ok {
		return 1
	}
	fmt.Print(compiler.FormatGo(src))
	return 0
}

func cmdBuild(args []string) int {
	var file, out string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-o" && i+1 < len(args):
			out = args[i+1]
			i++
		case strings.HasPrefix(args[i], "-o="):
			out = strings.TrimPrefix(args[i], "-o=")
		default:
			file = args[i]
		}
	}
	if file == "" {
		fmt.Fprintln(os.Stderr, "用法：aether build <文件.ae> [-o 输出文件]")
		return 2
	}
	src, ok := load(file, false, compiler.GenOptions{})
	if !ok {
		return 1
	}
	exe, ok := build(file, src, false)
	if !ok {
		return 1
	}
	if out == "" {
		out = strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		if filepath.Ext(exe) == ".exe" {
			out += ".exe"
		}
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(out, data, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("已生成 %s\n", out)
	return 0
}
