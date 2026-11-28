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
	"sync"
	"time"
)

// File 是 open() 返回的文件对象，也用于 sys.stdin、sys.stdout、sys.stderr。
type File struct {
	f      *os.File
	r      *bufio.Reader
	w      *bufio.Writer
	path   string
	mode   string
	closed bool
	std    int // 0：普通文件；1、2、3：标准输入、标准输出、标准错误
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

// ===== 标准流 =====

var (
	stdOnce                           sync.Once
	stdinFile, stdoutFile, stderrFile *File
)

func initStd() {
	stdOnce.Do(func() {
		if stdin == nil {
			stdin = bufio.NewReader(os.Stdin)
		}
		stdinFile = &File{f: os.Stdin, r: stdin, path: "<stdin>", mode: "r", std: 1}
		stdoutFile = &File{f: os.Stdout, path: "<stdout>", mode: "w", std: 2}
		stderrFile = &File{f: os.Stderr, path: "<stderr>", mode: "w", std: 3}
	})
}

// Stdin、Stdout、Stderr 实现 sys.stdin、sys.stdout、sys.stderr。标准输入与 input() 共用缓冲，
// 写入标准输出与 print 共用缓冲（因此顺序一致），标准错误不缓冲。
func Stdin() *File  { initStd(); return stdinFile }
func Stdout() *File { initStd(); return stdoutFile }
func Stderr() *File { initStd(); return stderrFile }

// PrintTo 实现 print(..., file=f, flush=...)。f 为 nil 表示标准输出。
func PrintTo(f *File, sep, end string, flush bool, args ...any) {
	if f == nil || f.std == 2 {
		PrintSE(sep, end, args...)
		if flush {
			Flush()
		}
		return
	}
	var sb strings.Builder
	for i, a := range args {
		if i > 0 {
			sb.WriteString(sep)
		}
		if s, ok := a.(string); ok {
			sb.WriteString(s)
		} else {
			sb.WriteString(Str(a))
		}
	}
	sb.WriteString(end)
	f.Write(sb.String())
	if flush {
		f.Flush()
	}
}

var scriptName = "aether"

// SetScript 记录主程序的源文件名（sys.argv[0]）。
func SetScript(name string) { scriptName = name }

// SysArgv 实现 sys.argv：第一个元素是程序的源文件名。
func SysArgv() *List[string] {
	return &List[string]{S: append([]string{scriptName}, progArgs...)}
}

// SysPlatform 返回与 Python 的 sys.platform 相同的平台名：win32、linux、darwin……
func SysPlatform() string {
	if goos() == "windows" {
		return "win32"
	}
	return goos()
}

func (f *File) check() {
	if f.closed {
		Raise("IOError", "文件已关闭：%s", f.path)
	}
}

func (f *File) Read() string {
	f.check()
	f.beforeRead()
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
	f.beforeRead()
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

// beforeRead：从终端读标准输入之前先输出缓冲中的提示。
func (f *File) beforeRead() {
	if f.std == 1 && interactive {
		Flush()
	}
}

func (f *File) Write(s string) {
	f.check()
	switch f.std {
	case 1:
		Raise("IOError", "不能写入标准输入")
	case 2:
		Write(s)
		return
	case 3:
		os.Stderr.WriteString(s)
		return
	}
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
	if f.std == 2 {
		Flush()
	}
	if f.w != nil {
		f.w.Flush()
	}
}

func (f *File) Close() {
	if f.closed || f.std != 0 {
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

// ReadText 对应 Python 的 Path(path).read_text(encoding=...)（文本模式、errors="strict"、通用换行）。
// 与 ReadFile 的区别正是 Python 文本读的可观测语义：
//   - "utf-8"（及其别名）**保留**开头的 BOM（解出 U+FEFF）；"utf-8-sig" 去掉它——ReadFile 总是去掉；
//   - 非法 UTF-8 抛 UnicodeDecodeError，出错区间、原因与文案与 CPython 的 'utf-8' 编解码器相同；
//   - "\r\n" 与单独的 "\r" 都换成 "\n"；
//   - 其它编码名抛 LookupError("unknown encoding: <名>")。
func ReadText(path, encoding, errors string) string {
	sig, ok := utf8CodecOf(encoding)
	if !ok {
		Raise("LookupError", "unknown encoding: %s", encoding)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		raiseIO(err, path)
	}
	if sig && len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		b = b[3:]
	}
	// errors（P12）：每段非法序列 [start, end) 与 strict 报错的区间相同；ignore 跳过它、replace 换成一个 U+FFFD。
	var sb strings.Builder
	pos := 0
	for {
		start, end, reason, bad := utf8FirstError(b[pos:])
		if !bad {
			sb.Write(b[pos:])
			break
		}
		start, end = start+pos, end+pos
		switch errors {
		case "strict":
			if end == start+1 {
				Raise("UnicodeDecodeError", "'utf-8' codec can't decode byte 0x%02x in position %d: %s", b[start], start, reason)
			}
			Raise("UnicodeDecodeError", "'utf-8' codec can't decode bytes in position %d-%d: %s", start, end-1, reason)
		case "ignore":
			sb.Write(b[pos:start])
		case "replace":
			sb.Write(b[pos:start])
			sb.WriteRune(0xFFFD)
		default:
			Raise("LookupError", "unknown error handler name '%s'", errors)
		}
		pos = end
	}
	s := sb.String()
	if strings.IndexByte(s, '\r') >= 0 {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.ReplaceAll(s, "\r", "\n")
	}
	return s
}

// utf8CodecOf 按 Python 的编码名规范化（小写；连续的非字母数字（点除外）折成一个 "_"）
// 认出 utf-8 与 utf-8-sig 两族。返回 (是否 -sig, 是否认得)。
func utf8CodecOf(encoding string) (bool, bool) {
	var sb strings.Builder
	punct := false
	for _, c := range strings.ToLower(encoding) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' {
			if punct && sb.Len() > 0 {
				sb.WriteByte('_')
			}
			sb.WriteRune(c)
			punct = false
		} else {
			punct = true
		}
	}
	for _, name := range []string{sb.String(), strings.ReplaceAll(sb.String(), ".", "_")} {
		switch name {
		case "utf_8", "utf8", "u8", "utf", "utf8_ucs2", "utf8_ucs4", "cp65001":
			return false, true
		case "utf_8_sig":
			return true, true
		}
	}
	return false, false
}

// utf8FirstError 找第一处非法 UTF-8 序列，返回出错区间 [start, end) 与原因。逐分支对应 CPython
// Objects/stringlib/codecs.h 的 utf8_decode（InvalidStart / InvalidContinuation1..3 / 截断）
// 与 unicodeobject.c 里 unicode_decode_utf8 对这些返回值的区间约定（全量解码，final=True）。
func utf8FirstError(b []byte) (int, int, string, bool) {
	isCont := func(c byte) bool { return c >= 0x80 && c <= 0xBF }
	n := len(b)
	i := 0
	for i < n {
		c := b[i]
		switch {
		case c < 0x80:
			i++
			continue
		case c < 0xC2: // 0x80..0xBF 是续字节，0xC0 / 0xC1 只能编码 0000-007F（过长）
			return i, i + 1, "invalid start byte", true
		case c < 0xE0: // 0080-07FF
			if n-i < 2 {
				return i, n, "unexpected end of data", true
			}
			if !isCont(b[i+1]) {
				return i, i + 1, "invalid continuation byte", true
			}
			i += 2
		case c < 0xF0: // 0800-FFFF：E0 的第二字节须 >= A0（防过长），ED 的须 < A0（防代理）
			badSecond := func(c2 byte) bool {
				return !isCont(c2) || (c2 < 0xA0 && c == 0xE0) || (c2 >= 0xA0 && c == 0xED)
			}
			if n-i < 3 {
				if n-i >= 2 && badSecond(b[i+1]) {
					return i, i + 1, "invalid continuation byte", true
				}
				return i, n, "unexpected end of data", true
			}
			if badSecond(b[i+1]) {
				return i, i + 1, "invalid continuation byte", true
			}
			if !isCont(b[i+2]) {
				return i, i + 2, "invalid continuation byte", true
			}
			i += 3
		case c < 0xF5: // 10000-10FFFF：F0 的第二字节须 >= 90，F4 的须 < 90
			badSecond := func(c2 byte) bool {
				return !isCont(c2) || (c2 < 0x90 && c == 0xF0) || (c2 >= 0x90 && c == 0xF4)
			}
			if n-i < 4 {
				if n-i >= 2 {
					if badSecond(b[i+1]) {
						return i, i + 1, "invalid continuation byte", true
					}
					if n-i >= 3 && !isCont(b[i+2]) {
						return i, i + 2, "invalid continuation byte", true
					}
				}
				return i, n, "unexpected end of data", true
			}
			if badSecond(b[i+1]) {
				return i, i + 1, "invalid continuation byte", true
			}
			if !isCont(b[i+2]) {
				return i, i + 2, "invalid continuation byte", true
			}
			if !isCont(b[i+3]) {
				return i, i + 3, "invalid continuation byte", true
			}
			i += 4
		default: // 0xF5..0xFF
			return i, i + 1, "invalid start byte", true
		}
	}
	return 0, 0, "", false
}

func WriteFile(path, data string) {
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		raiseIO(err, path)
	}
}

// WriteBytes 把字节列表原样写入文件（不做 UTF-8 校验，可写任意二进制）。
func WriteBytes(path string, data *List[uint8]) {
	if err := os.WriteFile(path, data.S, 0o644); err != nil {
		raiseIO(err, path)
	}
}

// ReadBytes 读取文件的原始字节。
func ReadBytes(path string) *List[uint8] {
	b, err := os.ReadFile(path)
	if err != nil {
		raiseIO(err, path)
	}
	return &List[uint8]{S: b}
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

func PathIsAbs(p string) bool   { return filepath.IsAbs(p) }
func PathClean(p string) string { return filepath.Clean(p) }
func PathSep() string           { return string(filepath.Separator) }

// PathRel 实现 os.path.relpath。
func PathRel(p, start string) string {
	if !filepath.IsAbs(p) {
		p = PathAbs(p)
	}
	if !filepath.IsAbs(start) {
		start = PathAbs(start)
	}
	r, err := filepath.Rel(start, p)
	if err != nil {
		Raise("ValueError", "无法计算 %s 相对于 %s 的路径", QuoteStr(p), QuoteStr(start))
	}
	return r
}

// ExpandUser 把开头的 ~ 替换为用户主目录。
func ExpandUser(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return home + p[1:]
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
