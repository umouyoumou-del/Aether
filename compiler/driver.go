package compiler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Version 是编译器版本。
const Version = "0.1.0"

// Program 是加载并检查后的程序。
type Program struct {
	Mods    []*Module
	Main    *Module
	Diag    *Diagnostics
	Checker *Checker
}

type loader struct {
	std      fs.FS
	diag     *Diagnostics
	byKey    map[string]*Module
	state    map[*Module]int
	order    []*Module
	mainDir  string
}

// Load 加载主文件及其依赖的所有模块（含 prelude），按依赖顺序排列。
func Load(mainPath string, std fs.FS, diag *Diagnostics) *Program {
	abs, err := filepath.Abs(mainPath)
	if err != nil {
		abs = mainPath
	}
	ld := &loader{std: std, diag: diag, byKey: map[string]*Module{}, state: map[*Module]int{}, mainDir: filepath.Dir(abs)}
	prog := &Program{Diag: diag}
	prelude := ld.loadStd("prelude", Pos{})
	if prelude == nil {
		diag.Errorf(Pos{}, "内部错误：找不到 prelude")
		return prog
	}
	prelude.IsPrelude = true
	ld.visit(prelude)
	src, err := os.ReadFile(abs)
	if err != nil {
		diag.Errorf(Pos{}, "无法读取文件 %s：%v", mainPath, err)
		return prog
	}
	name := mainPath
	if rel, err := filepath.Rel(mustCwd(), abs); err == nil && !strings.HasPrefix(rel, "..") {
		name = rel
	}
	main := ld.parse(abs, name, src, "main")
	main.IsMain = true
	ld.byKey[abs] = main
	ld.visit(main)
	prog.Mods = ld.order
	prog.Main = main
	return prog
}

// LoadSource 从字符串加载程序（用于测试与工具）。
func LoadSource(name string, src string, std fs.FS, diag *Diagnostics) *Program {
	ld := &loader{std: std, diag: diag, byKey: map[string]*Module{}, state: map[*Module]int{}, mainDir: mustCwd()}
	prog := &Program{Diag: diag}
	prelude := ld.loadStd("prelude", Pos{})
	if prelude == nil {
		diag.Errorf(Pos{}, "内部错误：找不到 prelude")
		return prog
	}
	prelude.IsPrelude = true
	ld.visit(prelude)
	abs, _ := filepath.Abs(name)
	main := ld.parse(abs, name, []byte(src), "main")
	main.IsMain = true
	ld.byKey[abs] = main
	ld.visit(main)
	prog.Mods = ld.order
	prog.Main = main
	return prog
}

func mustCwd() string {
	d, err := os.Getwd()
	if err != nil {
		return "."
	}
	return d
}

func (ld *loader) parse(absPath, name string, src []byte, modName string) *Module {
	sf := NewSourceFile(absPath, name, src)
	file := Parse(sf, ld.diag)
	m := &Module{Name: modName, Path: absPath, File: file, Src: sf}
	return m
}

func (ld *loader) loadStd(name string, pos Pos) *Module {
	key := "<stdlib>/" + name
	if m, ok := ld.byKey[key]; ok {
		return m
	}
	if ld.std == nil {
		return nil
	}
	rel := strings.ReplaceAll(name, ".", "/") + ".ae"
	data, err := fs.ReadFile(ld.std, rel)
	if err != nil {
		return nil
	}
	m := ld.parse("<stdlib>/"+rel, "<stdlib>/"+rel, data, name)
	m.IsStd = true
	ld.byKey[key] = m
	return m
}

// resolve 查找 import 的模块：先相对于当前文件，再相对于主文件目录，最后是标准库。
func (ld *loader) resolve(from *Module, is *ImportStmt) *Module {
	name := strings.Join(is.Path, ".")
	rel := filepath.Join(is.Path...) + ".ae"
	var dirs []string
	if !from.IsStd && !from.IsPrelude {
		dirs = append(dirs, filepath.Dir(from.Path))
	}
	dirs = append(dirs, ld.mainDir)
	for _, d := range dirs {
		p := filepath.Join(d, rel)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			abs, _ := filepath.Abs(p)
			if m, ok := ld.byKey[abs]; ok {
				return m
			}
			src, err := os.ReadFile(abs)
			if err != nil {
				ld.diag.Errorf(is.Pos, "无法读取模块 %s：%v", name, err)
				return nil
			}
			disp := p
			if r, err := filepath.Rel(mustCwd(), abs); err == nil && !strings.HasPrefix(r, "..") {
				disp = r
			}
			m := ld.parse(abs, disp, src, name)
			ld.byKey[abs] = m
			return m
		}
	}
	if m := ld.loadStd(name, is.Pos); m != nil {
		return m
	}
	d := ld.diag.Errorf(is.Pos, "找不到模块 %s", name)
	d.Notes = append(d.Notes, fmt.Sprintf("在 %s 中查找了 %s，标准库中也没有", strings.Join(dirs, "、"), rel))
	return nil
}

func (ld *loader) visit(m *Module) {
	switch ld.state[m] {
	case 2:
		return
	case 1:
		return
	}
	ld.state[m] = 1
	for _, st := range m.File.Stmts {
		is, ok := st.(*ImportStmt)
		if !ok {
			continue
		}
		dep := ld.resolve(m, is)
		if dep == nil {
			continue
		}
		if dep == m {
			ld.diag.Errorf(is.Pos, "模块不能导入自己")
			continue
		}
		if ld.state[dep] == 1 {
			ld.diag.Errorf(is.Pos, "循环导入：%s 与 %s 互相导入", m.Name, dep.Name)
			continue
		}
		is.Mod = dep
		ld.visit(dep)
	}
	ld.state[m] = 2
	ld.order = append(ld.order, m)
}

// Compile 检查程序并生成 Go 源码。
func Compile(prog *Program, testMode bool, opts GenOptions) (string, error) {
	if prog.Diag.HasErrors() {
		return "", errors.New("语法错误")
	}
	c := Check(prog.Mods, prog.Diag, testMode)
	prog.Checker = c
	if prog.Diag.HasErrors() {
		return "", errors.New("类型错误")
	}
	src, err := genSafe(c, testMode, opts)
	if err != nil {
		return "", err
	}
	return src, nil
}

func genSafe(c *Checker, testMode bool, opts GenOptions) (src string, err error) {
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 8192)
			n := runtime.Stack(buf, false)
			err = fmt.Errorf("内部错误：代码生成失败：%v\n%s", r, buf[:n])
		}
	}()
	return Generate(c, testMode, opts)
}

// ===== 构建 =====

// BuildConfig 描述如何调用 Go 工具链。
type BuildConfig struct {
	CacheDir string
	RT       fs.FS // 运行时源码（rt/*.go）
	Verbose  bool
}

func hashOf(parts ...[]byte) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write(p)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeIfChanged(p string, data []byte) (bool, error) {
	if old, err := os.ReadFile(p); err == nil && bytes.Equal(old, data) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(p, data, 0o644)
}

// GoEnv 返回调用 go 命令时使用的环境变量。
func GoEnv(cacheDir string) []string {
	env := os.Environ()
	set := func(k, v string) {
		for i, e := range env {
			if strings.HasPrefix(strings.ToUpper(e), strings.ToUpper(k)+"=") {
				env[i] = k + "=" + v
				return
			}
		}
		env = append(env, k+"="+v)
	}
	if gc := os.Getenv("GOCACHE"); gc != "" {
		if err := os.MkdirAll(gc, 0o755); err != nil {
			set("GOCACHE", filepath.Join(cacheDir, "go-build"))
		}
	}
	if gm := os.Getenv("GOMODCACHE"); gm != "" {
		if err := os.MkdirAll(gm, 0o755); err != nil {
			set("GOMODCACHE", filepath.Join(cacheDir, "go-mod"))
		}
	}
	set("GOTOOLCHAIN", "local")
	set("CGO_ENABLED", "0")
	set("GOWORK", "off")
	set("GOFLAGS", "-mod=mod")
	set("GOPROXY", "off")
	return env
}

// BuildGo 把生成的 Go 源码编译为可执行文件，返回可执行文件路径。
// key 用于区分不同程序的构建目录。
func BuildGo(goSrc string, key string, cfg BuildConfig) (string, error) {
	dir := filepath.Join(cfg.CacheDir, "build", key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if _, err := writeIfChanged(filepath.Join(dir, "go.mod"), []byte("module aeprog\n\ngo 1.23\n")); err != nil {
		return "", err
	}
	var rtParts [][]byte
	if cfg.RT != nil {
		entries, err := fs.ReadDir(cfg.RT, ".")
		if err != nil {
			return "", err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			data, err := fs.ReadFile(cfg.RT, e.Name())
			if err != nil {
				return "", err
			}
			rtParts = append(rtParts, []byte(e.Name()), data)
			if _, err := writeIfChanged(filepath.Join(dir, "rt", e.Name()), data); err != nil {
				return "", err
			}
		}
	}
	if _, err := writeIfChanged(filepath.Join(dir, "main.go"), []byte(goSrc)); err != nil {
		return "", err
	}
	exe := filepath.Join(dir, "prog")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	stamp := hashOf(append(rtParts, []byte(goSrc), []byte(Version), []byte(runtime.Version()))...)
	stampFile := filepath.Join(dir, "stamp")
	if old, err := os.ReadFile(stampFile); err == nil && string(old) == stamp {
		if _, err := os.Stat(exe); err == nil {
			return exe, nil
		}
	}
	goBin := "go"
	if p, err := exec.LookPath("go"); err == nil {
		goBin = p
	} else {
		return "", errors.New("找不到 Go 工具链（go 命令）。五行需要 Go 1.23 或更新版本：https://go.dev/dl/")
	}
	start := time.Now()
	cmd := exec.Command(goBin, "build", "-o", exe, ".")
	cmd.Dir = dir
	cmd.Env = GoEnv(cfg.CacheDir)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", &GoBuildError{Output: out.String()}
	}
	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "[aether] go build 用时 %.2fs\n", time.Since(start).Seconds())
	}
	os.WriteFile(stampFile, []byte(stamp), 0o644)
	return exe, nil
}

// GoBuildError 是 Go 编译生成代码失败（通常意味着编译器的 bug）。
type GoBuildError struct{ Output string }

func (e *GoBuildError) Error() string {
	return "内部错误：生成的 Go 代码编译失败（这是五行编译器的问题，请反馈）：\n" + e.Output
}

// BuildKey 根据主文件路径生成构建目录名。
func BuildKey(mainPath string, extra string) string {
	abs, err := filepath.Abs(mainPath)
	if err != nil {
		abs = mainPath
	}
	h := hashOf([]byte(strings.ToLower(abs)), []byte(extra))
	base := strings.TrimSuffix(filepath.Base(mainPath), filepath.Ext(mainPath))
	base = sanitizeFile(base)
	return base + "-" + h[:12]
}

func sanitizeFile(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('_')
		}
	}
	return sb.String()
}

var _ = path.Join
