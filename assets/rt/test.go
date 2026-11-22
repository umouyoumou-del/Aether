package rt

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// TestCase 是一个 test "名字": 块。
type TestCase struct {
	Name string
	File string
	Line int
	Fn   func()
}

func runOne(fn func()) (err any, pcs []uintptr) {
	defer func() {
		if r := recover(); r != nil {
			err = r
			pcs = PCsOf(r)
			if pcs == nil {
				pcs = callers(3)
			}
		}
	}()
	fn()
	return nil, nil
}

// RunTests 运行所有测试并打印结果；有失败时以状态 1 退出。
func RunTests(init func(), tests []TestCase) {
	green, red, gray, reset := "\x1b[32m", "\x1b[31m", "\x1b[90m", "\x1b[0m"
	if !interactive {
		green, red, gray, reset = "", "", "", ""
	}
	if err, pcs := runOne(init); err != nil {
		Flush()
		os.Stderr.WriteString("模块初始化失败：\n")
		PrintUncaught(err, pcs)
		os.Exit(1)
	}
	Flush()
	passed, failed := 0, 0
	start := time.Now()
	var failures []string
	for _, t := range tests {
		t0 := time.Now()
		err, pcs := runOne(t.Fn)
		Flush()
		d := time.Since(t0)
		if err == nil {
			passed++
			fmt.Printf("  %s✓%s %s %s(%s)%s\n", green, reset, t.Name, gray, fmtDur(d), reset)
			continue
		}
		failed++
		fmt.Printf("  %s✗%s %s\n", red, reset, t.Name)
		tb := FormatTraceback(err, pcs)
		var sb strings.Builder
		for _, line := range strings.Split(strings.TrimRight(tb, "\n"), "\n") {
			sb.WriteString("      " + line + "\n")
		}
		failures = append(failures, fmt.Sprintf("测试 \"%s\"（%s:%d）失败：\n%s", t.Name, t.File, t.Line, sb.String()))
	}
	if len(failures) > 0 {
		fmt.Println()
		for _, f := range failures {
			fmt.Print(f)
		}
	}
	fmt.Println()
	total := passed + failed
	if failed == 0 {
		fmt.Printf("%s全部通过%s：%d 个测试，用时 %s\n", green, reset, total, fmtDur(time.Since(start)))
		return
	}
	fmt.Printf("%s%d 个失败%s，%d 个通过，共 %d 个测试\n", red, failed, reset, passed, total)
	os.Exit(1)
}

func fmtDur(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}
