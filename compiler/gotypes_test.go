package compiler

import (
	"fmt"
	"go/ast"
	"go/importer"
	goparser "go/parser"
	"go/token"
	"go/types"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"aether/assets"
)

var (
	goImpOnce sync.Once
	goImp     types.Importer
	goImpMu   sync.Mutex
)

// typeCheckGo 用 go/types 检查生成的 Go 代码，返回第一个类型错误。
func typeCheckGo(src string) error {
	goImpOnce.Do(func() {
		goImp = importer.ForCompiler(token.NewFileSet(), "source", nil)
	})
	fset := token.NewFileSet()
	f, err := goparser.ParseFile(fset, "main.go", src, goparser.SkipObjectResolution)
	if err != nil {
		return err
	}
	goImpMu.Lock()
	defer goImpMu.Unlock()
	var first error
	conf := types.Config{
		Importer: goImp,
		Error: func(err error) {
			if first == nil {
				first = err
			}
		},
	}
	conf.Check("main", fset, []*ast.File{f}, nil)
	return first
}

// TestGeneratedGo 对能通过 Aether 检查的程序（含随机变异），确认生成的 Go 代码能通过 Go 的类型检查。
func TestGeneratedGo(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	var files []string
	for _, pat := range []string{"../tests/run/*.ae", "../examples/*.ae"} {
		fs, _ := filepath.Glob(pat)
		files = append(files, fs...)
	}
	sort.Strings(files)
	n := 2
	if s := os.Getenv("MUTATIONS"); s != "" {
		fmt.Sscanf(s, "%d", &n)
	}
	rng := rand.New(rand.NewSource(7))
	checked, bad := 0, 0
	for _, f := range files {
		data, _ := os.ReadFile(f)
		lines := strings.Split(string(data), "\n")
		for i := 0; i <= n; i++ {
			cur := lines
			if i > 0 {
				cur = mutate(rng, lines)
			}
			src := strings.Join(cur, "\n")
			diag := &Diagnostics{}
			prog := LoadSource(f, src, assets.Lib(), diag)
			gosrc, err := func() (s string, err error) {
				defer func() {
					if r := recover(); r != nil {
						err = fmt.Errorf("panic: %v", r)
					}
				}()
				return Compile(prog, false, GenOptions{NoLines: true})
			}()
			if err != nil {
				if strings.HasPrefix(err.Error(), "panic") {
					t.Errorf("%s 变异 %d：%v", filepath.Base(f), i, err)
				}
				continue
			}
			checked++
			if err := typeCheckGo(gosrc); err != nil {
				bad++
				if bad <= 5 {
					t.Errorf("生成的 Go 代码有类型错误（%s 变异 %d）：%v\n--- 源码 ---\n%s", filepath.Base(f), i, err, src)
				}
			}
		}
	}
	t.Logf("检查了 %d 个能编译的程序", checked)
	if bad > 5 {
		t.Errorf("共 %d 个程序生成了错误的 Go 代码", bad)
	}
}
