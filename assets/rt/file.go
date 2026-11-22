package rt

import (
	"bufio"
	"io"
	"iter"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// File 是 open() 返回的文件对象。
type File struct {
	f      *os.File
	r      *bufio.Reader
	w      *bufio.Writer
	path   string
	mode   string
	closed bool
}

// Open 实现 open(path, mode)。
func Open(path string, mode Opt[string]) *File {
	m := "r"
	if mode.Ok {
		m = mode.V
	}
	m = strings.ReplaceAll(m, "b", "")
	m = strings.ReplaceAll(m, "t", "")
	var flag int
	switch m {
	case "r":
		flag = os.O_RDONLY
	case "w":
		flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	case "a":
		flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	case "x":
		flag = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	case "r+":
		flag = os.O_RDWR
	case "w+":
		flag = os.O_RDWR | os.O_CREATE | os.O_TRUNC
	case "a+":
		flag = os.O_RDWR | os.O_CREATE | os.O_APPEND
	default:
		Raise("ValueError", "无效的文件打开模式 %s", QuoteStr(m))
	}
	f, err := os.OpenFile(path, flag, 0o644)
	if err != nil {
		raiseIO(err, path)
	}
	file := &File{f: f, path: path, mode: m}
	if strings.Contains(m, "r") || strings.Contains(m, "+") {
		file.r = bufio.NewReader(f)
	}
	if m != "r" {
		file.w = bufio.NewWriter(f)
	}
	return file
}

func raiseIO(err error, path string) {
	switch {
	case os.IsNotExist(err):
		Raise("FileNotFoundError", "文件不存在：%s", path)
	case os.IsPermission(err):
		Raise("PermissionError", "没有权限：%s", path)
	case os.IsExist(err):
		Raise("FileExistsError", "文件已存在：%s", path)
	}
	Raise("IOError", "%v", err)
}

func (f *File) check() {
	if f.closed {
		Raise("IOError", "文件已关闭：%s", f.path)
	}
}

func (f *File) Read() string {
	f.check()
	if f.r == nil {
		Raise("IOError", "文件不是以读模式打开的")
	}
	b, err := io.ReadAll(f.r)
	if err != nil {
		raiseIO(err, f.path)
	}
	return string(b)
}

func (f *File) ReadLine() Opt[string] {
	f.check()
	if f.r == nil {
		Raise("IOError", "文件不是以读模式打开的")
	}
	line, err := f.r.ReadString('\n')
	if err != nil {
		if err == io.EOF {
			if line == "" {
				return Opt[string]{}
			}
			return Opt[string]{V: trimNewline(line), Ok: true}
		}
		raiseIO(err, f.path)
	}
	return Opt[string]{V: trimNewline(line), Ok: true}
}

func (f *File) ReadLines() *List[string] {
	l := &List[string]{}
	for line := range f.Lines() {
		l.S = append(l.S, line)
	}
	return l
}

// Lines 逐行遍历文件（不含换行符）。
func (f *File) Lines() iter.Seq[string] {
	return func(yield func(string) bool) {
		for {
			o := f.ReadLine()
			if !o.Ok || !yield(o.V) {
				return
			}
		}
	}
}

func (f *File) Write(s string) {
	f.check()
	if f.w == nil {
		Raise("IOError", "文件不是以写模式打开的")
	}
	if _, err := f.w.WriteString(s); err != nil {
		raiseIO(err, f.path)
	}
}

func (f *File) WriteLine(s string) {
	f.Write(s)
	f.Write("\n")
}

func (f *File) Flush() {
	if f.w != nil {
		f.w.Flush()
	}
}

func (f *File) Close() {
	if f.closed {
		return
	}
	if f.w != nil {
		f.w.Flush()
	}
	f.f.Close()
	f.closed = true
}

func (f *File) Enter() *File { return f }
func (f *File) Exit()        { f.Close() }
func (f *File) Name() string { return f.path }
func (f *File) String() string {
	return "<文件 " + QuoteStr(f.path) + " 模式 " + f.mode + ">"
}

// ===== os 模块 =====

func ReadFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		raiseIO(err, path)
	}
	s := string(b)
	return strings.TrimPrefix(s, string(rune(0xFEFF)))
}

func WriteFile(path, data string) {
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		raiseIO(err, path)
	}
}

func AppendFile(path, data string) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		raiseIO(err, path)
	}
	defer f.Close()
	if _, err := f.WriteString(data); err != nil {
		raiseIO(err, path)
	}
}

func ReadLines(path string) *List[string] {
	return StrSplitLines(ReadFile(path))
}

func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func IsDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func IsFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func FileSize(path string) int {
	st, err := os.Stat(path)
	if err != nil {
		raiseIO(err, path)
	}
	return int(st.Size())
}

func ListDir(path string) *List[string] {
	ents, err := os.ReadDir(path)
	if err != nil {
		raiseIO(err, path)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return &List[string]{S: names}
}

func Walk(root string) *List[string] {
	var out []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	return &List[string]{S: out}
}

func Glob(pattern string) *List[string] {
	m, err := filepath.Glob(pattern)
	if err != nil {
		Raise("ValueError", "无效的通配符：%s", pattern)
	}
	return &List[string]{S: m}
}

func MkDir(path string) {
	if err := os.MkdirAll(path, 0o755); err != nil {
		raiseIO(err, path)
	}
}

func Remove(path string) {
	if err := os.RemoveAll(path); err != nil {
		raiseIO(err, path)
	}
}

func Rename(a, b string) {
	if err := os.Rename(a, b); err != nil {
		raiseIO(err, a)
	}
}

func Getenv(name string) Opt[string] {
	v, ok := os.LookupEnv(name)
	return Opt[string]{V: v, Ok: ok}
}

func Setenv(name, value string) { os.Setenv(name, value) }

var progArgs []string

// SetArgs 由生成代码设置命令行参数（不含程序名）。
func SetArgs(a []string) { progArgs = a }

func Args() *List[string] {
	return &List[string]{S: append([]string(nil), progArgs...)}
}

func Cwd() string {
	d, err := os.Getwd()
	if err != nil {
		raiseIO(err, ".")
	}
	return d
}

func Chdir(p string) {
	if err := os.Chdir(p); err != nil {
		raiseIO(err, p)
	}
}

// Run 执行外部命令并返回其标准输出。
func RunCmd(cmd string, args *List[string]) string {
	c := exec.Command(cmd, args.S...)
	var errb strings.Builder
	c.Stderr = &errb
	out, err := c.Output()
	if err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		Raise("OSError", "命令 %s 执行失败：%s", cmd, msg)
	}
	return string(out)
}

func PathJoin(parts *List[string]) string { return filepath.Join(parts.S...) }
func PathBase(p string) string            { return filepath.Base(p) }
func PathDir(p string) string             { return filepath.Dir(p) }
func PathExt(p string) string             { return filepath.Ext(p) }
func PathAbs(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return a
}

func ModTime(path string) float64 {
	st, err := os.Stat(path)
	if err != nil {
		raiseIO(err, path)
	}
	return float64(st.ModTime().UnixNano()) / float64(time.Second)
}

func Platform() string {
	return goos()
}
