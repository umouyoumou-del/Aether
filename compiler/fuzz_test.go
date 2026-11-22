package compiler

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"testing"

	"aether/assets"
)

// TestMutations 对测试程序做随机的删行、复制行、截断，确认编译器（检查与代码生成）不会崩溃。
func TestMutations(t *testing.T) {
	var files []string
	for _, pat := range []string{"../tests/run/*.ae", "../examples/*.ae", "../tests/errors/*.ae"} {
		fs, _ := filepath.Glob(pat)
		files = append(files, fs...)
	}
	sort.Strings(files)
	n := 30
	if s := os.Getenv("MUTATIONS"); s != "" {
		fmt.Sscanf(s, "%d", &n)
	}
	rng := rand.New(rand.NewSource(1))
	crashes := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(data), "\n")
		for i := 0; i < n; i++ {
			mut := mutate(rng, lines)
			src := strings.Join(mut, "\n")
			if msg := compileNoPanic(f, src); msg != "" {
				crashes++
				if crashes <= 5 {
					t.Errorf("编译器崩溃（%s 第 %d 次变异）：%s\n--- 源码 ---\n%s", filepath.Base(f), i, msg, src)
				}
			}
		}
	}
	if crashes > 5 {
		t.Errorf("共 %d 次崩溃", crashes)
	}
}

func mutate(rng *rand.Rand, lines []string) []string {
	out := append([]string(nil), lines...)
	k := 1 + rng.Intn(3)
	for j := 0; j < k && len(out) > 1; j++ {
		i := rng.Intn(len(out))
		switch rng.Intn(5) {
		case 0, 1: // 删除一行
			out = append(out[:i], out[i+1:]...)
		case 2: // 复制一行
			out = append(out[:i+1], out[i:]...)
		case 3: // 截断一行
			if l := []rune(out[i]); len(l) > 1 {
				out[i] = string(l[:rng.Intn(len(l))])
			}
		case 4: // 交换两行
			jj := rng.Intn(len(out))
			out[i], out[jj] = out[jj], out[i]
		}
	}
	return out
}

func compileNoPanic(name, src string) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprintf("%v\n%s", r, debug.Stack())
		}
	}()
	diag := &Diagnostics{}
	prog := LoadSource(name, src, assets.Lib(), diag)
	Compile(prog, false, GenOptions{})
	return ""
}
