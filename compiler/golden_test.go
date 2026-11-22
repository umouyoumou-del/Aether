package compiler

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"aether/assets"
)

func testCache(t *testing.T) string {
	if d := os.Getenv("AETHER_CACHE"); d != "" {
		return d
	}
	d, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(d, "aether-test")
}

// TestRun 编译并运行 tests/run 下的每个程序，把标准输出与 .out 文件比较。
// 设置 UPDATE=1 可以重新生成 .out 文件。
func TestRun(t *testing.T) {
	files, _ := filepath.Glob("../tests/run/*.ae")
	sort.Strings(files)
	if len(files) == 0 {
		t.Skip("没有测试程序")
	}
	cache := testCache(t)
	for _, f := range files {
		f := f
		name := strings.TrimSuffix(filepath.Base(f), ".ae")
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			diag := &Diagnostics{}
			prog := Load(f, assets.Lib(), diag)
			testMode := strings.HasPrefix(name, "test_")
			src, err := Compile(prog, testMode, GenOptions{})
			if err != nil {
				t.Fatalf("编译失败：%v\n%s", err, diag.Format(false))
			}
			exe, err := BuildGo(src, BuildKey(f, "golden"), BuildConfig{CacheDir: cache, RT: assets.RT()})
			if err != nil {
				t.Fatalf("%v", err)
			}
			work := t.TempDir()
			cmd := exec.Command(exe)
			cmd.Dir = work
			stdinFile := strings.TrimSuffix(f, ".ae") + ".in"
			if data, err := os.ReadFile(stdinFile); err == nil {
				cmd.Stdin = bytes.NewReader(data)
			}
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			runErr := cmd.Run()
			got := strings.ReplaceAll(stdout.String(), "\r\n", "\n")
			if stderr.Len() > 0 {
				got += "--- stderr ---\n" + normalizeStderr(stderr.String())
			}
			if runErr != nil {
				if ee, ok := runErr.(*exec.ExitError); ok {
					got += "--- exit " + strconv.Itoa(ee.ExitCode()) + " ---\n"
				} else {
					t.Fatalf("运行失败：%v", runErr)
				}
			}
			outFile := strings.TrimSuffix(f, ".ae") + ".out"
			if os.Getenv("UPDATE") == "1" {
				os.WriteFile(outFile, []byte(got), 0o644)
				return
			}
			want, err := os.ReadFile(outFile)
			if err != nil {
				t.Fatalf("缺少期望输出 %s（设置 UPDATE=1 生成）\n实际输出：\n%s", outFile, got)
			}
			w := strings.ReplaceAll(string(want), "\r\n", "\n")
			if w != got {
				t.Errorf("输出不一致\n--- 期望 ---\n%s\n--- 实际 ---\n%s", w, got)
			}
		})
	}
}

var pathRe = regexp.MustCompile(`文件 "[^"]*[\\/]`)

func normalizeStderr(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return pathRe.ReplaceAllString(s, `文件 "`)
}

var errMarkRe = regexp.MustCompile(`#\s*(错误|警告)[:：]\s*(.+)$`)

// TestErrors 检查 tests/errors 下的程序产生预期的编译错误。
// 在出错的行末写注释：  # 错误: 期望的错误信息片段
func TestErrors(t *testing.T) {
	files, _ := filepath.Glob("../tests/errors/*.ae")
	sort.Strings(files)
	for _, f := range files {
		f := f
		t.Run(strings.TrimSuffix(filepath.Base(f), ".ae"), func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			type expect struct {
				line    int
				warning bool
				text    string
				found   bool
			}
			var exps []*expect
			for i, line := range strings.Split(string(data), "\n") {
				if m := errMarkRe.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil {
					exps = append(exps, &expect{line: i + 1, warning: m[1] == "警告", text: strings.TrimSpace(m[2])})
				}
			}
			diag := &Diagnostics{}
			prog := Load(f, assets.Lib(), diag)
			Compile(prog, false, GenOptions{})
			for _, d := range diag.List {
				matched := false
				for _, e := range exps {
					if e.line == d.Pos.Line && e.warning == d.Warning && strings.Contains(d.Msg+strings.Join(d.Notes, " "), e.text) {
						e.found = true
						matched = true
					}
					if e.line == d.Pos.Line {
						matched = true
					}
				}
				if !matched && !d.Warning {
					t.Errorf("意外的错误 %d:%d: %s", d.Pos.Line, d.Pos.Col, d.Msg)
				}
			}
			for _, e := range exps {
				if !e.found {
					kind := "错误"
					if e.warning {
						kind = "警告"
					}
					t.Errorf("第 %d 行缺少期望的%s：%s", e.line, kind, e.text)
				}
			}
		})
	}
}

func init() {
	_ = runtime.GOOS
}

// TestExamples 确保 examples 目录下的每个示例都能编译并正常运行。
func TestExamples(t *testing.T) {
	files, _ := filepath.Glob("../examples/*.ae")
	sort.Strings(files)
	cache := testCache(t)
	for _, f := range files {
		f := f
		t.Run(strings.TrimSuffix(filepath.Base(f), ".ae"), func(t *testing.T) {
			t.Parallel()
			diag := &Diagnostics{}
			prog := Load(f, assets.Lib(), diag)
			src, err := Compile(prog, false, GenOptions{})
			if err != nil {
				t.Fatalf("编译失败：%v\n%s", err, diag.Format(false))
			}
			exe, err := BuildGo(src, BuildKey(f, "example"), BuildConfig{CacheDir: cache, RT: assets.RT()})
			if err != nil {
				t.Fatalf("%v", err)
			}
			cmd := exec.Command(exe)
			cmd.Dir = t.TempDir()
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("运行失败：%v\n%s", err, out)
			}
		})
	}
}
