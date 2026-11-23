package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"aether/assets"
	"aether/compiler"
)

const replHelp = `Aether 交互模式：输入语句或表达式，表达式的值会被打印。
  以冒号结尾的行开始一个代码块，输入空行结束。
  :show   显示当前会话的代码     :undo   撤销上一条输入
  :reset  清空会话               :quit   退出（也可以按 Ctrl+Z 回车 / Ctrl+D）
注意：每次输入都会重新运行整个会话，之前语句的副作用（例如写文件）会重复发生。`

type replSession struct {
	chunks  []string // 已接受的代码片段
	shown   int      // 已经显示过的输出字节数
	dir     string
	counter int
}

func cmdRepl(args []string) int {
	fmt.Printf("Aether %s 交互模式，输入 :help 查看帮助\n", compiler.Version)
	dir, _ := os.Getwd()
	s := &replSession{dir: dir}
	in := bufio.NewReader(os.Stdin)
	for {
		src, ok := readChunk(in)
		if !ok {
			fmt.Println()
			return 0
		}
		trimmed := strings.TrimSpace(src)
		switch trimmed {
		case "":
			continue
		case ":quit", ":q", "exit()", "quit()":
			return 0
		case ":help", ":h":
			fmt.Println(replHelp)
			continue
		case ":show":
			fmt.Println(strings.Join(s.chunks, "\n"))
			continue
		case ":reset":
			s.chunks = nil
			s.shown = 0
			fmt.Println("会话已清空")
			continue
		case ":undo":
			if len(s.chunks) > 0 {
				s.chunks = s.chunks[:len(s.chunks)-1]
				s.rerunQuiet()
			}
			continue
		}
		s.eval(src)
	}
}

// readChunk 读取一条输入；以冒号结尾的行会继续读取，直到空行。
func readChunk(in *bufio.Reader) (string, bool) {
	fmt.Print(">>> ")
	line, err := in.ReadString('\n')
	if err != nil && line == "" {
		return "", false
	}
	line = strings.TrimRight(line, "\r\n")
	lines := []string{line}
	open := strings.HasSuffix(strings.TrimSpace(stripComment(line)), ":") || openBrackets(line) > 0
	for open {
		fmt.Print("... ")
		l, err := in.ReadString('\n')
		l = strings.TrimRight(l, "\r\n")
		if err != nil && l == "" {
			break
		}
		if strings.TrimSpace(l) == "" && openBrackets(strings.Join(lines, "\n")) <= 0 {
			break
		}
		lines = append(lines, l)
	}
	return strings.Join(lines, "\n"), true
}

func stripComment(s string) string {
	inStr := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inStr = !inStr
		case '#':
			if !inStr {
				return s[:i]
			}
		}
	}
	return s
}

func openBrackets(s string) int {
	depth := 0
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			if c == '\\' {
				i++
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '#':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		}
	}
	return depth
}

func (s *replSession) source(extra string) string {
	parts := append(append([]string(nil), s.chunks...), extra)
	return strings.Join(parts, "\n") + "\n"
}

// check 检查加入 extra 之后的整个会话。
func (s *replSession) check(extra string, quiet bool) (*compiler.Program, string, bool) {
	diag := &compiler.Diagnostics{}
	name := filepath.Join(s.dir, "<交互>.ae")
	prog := compiler.LoadSource(name, s.source(extra), assets.Lib(), diag)
	src, err := compiler.Compile(prog, false, compiler.GenOptions{})
	if err != nil {
		if !quiet {
			s.printDiags(diag, extra)
		}
		return prog, "", false
	}
	return prog, src, true
}

func (s *replSession) printDiags(diag *compiler.Diagnostics, extra string) {
	offset := len(strings.Split(strings.Join(s.chunks, "\n"), "\n"))
	if len(s.chunks) == 0 {
		offset = 0
	}
	extraLines := strings.Split(extra, "\n")
	for _, d := range diag.List {
		if d.Warning {
			continue
		}
		ln := d.Pos.Line - offset
		kind := "错误"
		if ln >= 1 && ln <= len(extraLines) {
			fmt.Fprintf(os.Stderr, "%s: %s\n    %s\n", kind, d.Msg, strings.TrimSpace(extraLines[ln-1]))
		} else {
			fmt.Fprintf(os.Stderr, "%s（第 %d 行）: %s\n", kind, d.Pos.Line, d.Msg)
		}
		for _, n := range d.Notes {
			fmt.Fprintf(os.Stderr, "    提示: %s\n", n)
		}
	}
}

// isExpression 判断输入是否为单个有值的表达式。
func isExpression(prog *compiler.Program) bool {
	if prog == nil || prog.Main == nil || prog.Main.File == nil {
		return false
	}
	stmts := prog.Main.File.Stmts
	if len(stmts) == 0 {
		return false
	}
	es, ok := stmts[len(stmts)-1].(*compiler.ExprStmt)
	if !ok {
		return false
	}
	t := es.X.Typ()
	if t == nil {
		return false
	}
	switch t.String() {
	case "void", "<无效>":
		return false
	}
	switch es.X.(type) {
	case *compiler.Call:
	default:
	}
	return true
}

func (s *replSession) eval(src string) {
	// 单行输入：先尝试作为表达式打印
	single := !strings.Contains(src, "\n")
	if single {
		if prog, _, ok := s.check(src, true); ok && isExpression(prog) {
			wrapped := "print(repr(" + strings.TrimSpace(src) + "))"
			if _, gosrc, ok := s.check(wrapped, true); ok {
				// 表达式不加入会话，也不改变“已显示的输出”
				s.run(gosrc, false)
				return
			}
		}
	}
	_, gosrc, ok := s.check(src, false)
	if !ok {
		return
	}
	s.chunks = append(s.chunks, src)
	if !s.run(gosrc, true) {
		// 运行出错：撤销这条输入
		s.chunks = s.chunks[:len(s.chunks)-1]
		s.rerunQuiet()
	}
}

// run 运行整个会话，只显示新增的输出；返回是否正常结束。
// keep 为真时记录新的输出位置（语句会留在会话中）。
func (s *replSession) run(gosrc string, keep bool) bool {
	cfg := compiler.BuildConfig{CacheDir: cacheDir(), RT: assets.RT()}
	exe, err := compiler.BuildGo(gosrc, "repl-"+strconv.Itoa(os.Getpid()), cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return false
	}
	var out, errb bytes.Buffer
	c := exec.Command(exe)
	c.Dir = s.dir
	c.Stdout = &out
	c.Stderr = &errb
	runErr := c.Run()
	all := out.Bytes()
	if len(all) >= s.shown {
		os.Stdout.Write(all[s.shown:])
	} else {
		os.Stdout.Write(all)
	}
	if runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			// 回溯中的行号指向内部拼接的会话代码，交互模式下只显示异常本身
			for _, line := range strings.Split(strings.TrimRight(errb.String(), "\n"), "\n") {
				if strings.HasPrefix(line, "回溯") || strings.HasPrefix(line, "  ") {
					continue
				}
				fmt.Fprintln(os.Stderr, line)
			}
			return false
		}
		fmt.Fprintln(os.Stderr, runErr)
		return false
	}
	os.Stderr.Write(errb.Bytes())
	if keep {
		s.shown = len(all)
	}
	return true
}

// rerunQuiet 重新运行会话以同步“已显示的输出”长度（不打印）。
func (s *replSession) rerunQuiet() {
	_, gosrc, ok := s.check("", true)
	if !ok {
		s.shown = 0
		return
	}
	cfg := compiler.BuildConfig{CacheDir: cacheDir(), RT: assets.RT()}
	exe, err := compiler.BuildGo(gosrc, "repl-"+strconv.Itoa(os.Getpid()), cfg)
	if err != nil {
		return
	}
	var out bytes.Buffer
	c := exec.Command(exe)
	c.Dir = s.dir
	c.Stdout = &out
	c.Run()
	s.shown = out.Len()
}
