package rt

import (
	"os"
	"strings"
)

// Python 3.12 的 glob.glob（Lib/glob.py：_iglob / _glob0 / _glob1 / _glob2 / _rlistdir / _iterdir 逐个对应）与
// fnmatch（POSIX：大小写敏感，模式语义逐条对应 fnmatch._translate 产出的正则）。
//
// 与 CPython 唯一的可观测差别是**结果顺序**：Python 按 os.scandir 给出的目录顺序（文件系统决定，未定义），
// 这里每个目录按文件名排序（os.ReadDir）。需要确定顺序的调用方本来就要 sorted()。

// PyGlob 对应 glob.glob(pathname, recursive=..., include_hidden=...)（root_dir / dir_fd 未实现）。
func PyGlob(pathname string, recursive bool, includeHidden bool) *List[string] {
	g := &pyGlobber{recursive: recursive, hidden: includeHidden}
	out := g.iglob(pathname, false)
	if pathname == "" || (recursive && pyIsRecursive(prefix2(pathname))) {
		if len(out) > 0 && out[0] == "" {
			out = out[1:]
		}
	}
	if out == nil {
		out = []string{}
	}
	return &List[string]{S: out}
}

// PyGlobEscape 对应 glob.escape：把 * ? [ 各自包进方括号（POSIX 没有盘符）。
func PyGlobEscape(pathname string) string {
	var sb strings.Builder
	for _, r := range pathname {
		if r == '*' || r == '?' || r == '[' {
			sb.WriteByte('[')
			sb.WriteRune(r)
			sb.WriteByte(']')
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// PyGlobHasMagic 对应 glob.has_magic。
func PyGlobHasMagic(s string) bool { return strings.ContainsAny(s, "*?[") }

// PyFnmatch 对应 fnmatch.fnmatchcase(name, pat)（POSIX 上 fnmatch 与它相同）。
func PyFnmatch(name, pat string) bool { return fnCompile(pat).match([]rune(name)) }

type pyGlobber struct {
	recursive, hidden bool
}

func prefix2(s string) string {
	if len(s) >= 2 {
		return s[:2]
	}
	return s
}

func pyIsRecursive(p string) bool { return p == "**" }

func pyIsHidden(name string) bool { return name != "" && name[0] == '.' }

// posixSplit 对应 posixpath.split。
func posixSplit(p string) (string, string) {
	i := strings.LastIndexByte(p, '/') + 1
	head, tail := p[:i], p[i:]
	if head != "" && head != strings.Repeat("/", len(head)) {
		head = strings.TrimRight(head, "/")
	}
	return head, tail
}

// posixJoin 对应 posixpath.join(a, b)。
func posixJoin(a, b string) string {
	if strings.HasPrefix(b, "/") {
		return b
	}
	if a == "" || strings.HasSuffix(a, "/") {
		return a + b
	}
	return a + "/" + b
}

// globJoin 对应 glob._join。
func globJoin(dirname, basename string) string {
	if dirname == "" || basename == "" {
		if dirname != "" {
			return dirname
		}
		return basename
	}
	return posixJoin(dirname, basename)
}

func pyLexists(p string) bool { _, err := os.Lstat(p); return err == nil }

func pyIsDirPath(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }

// iterdir 对应 _iterdir：dirname 为空时列 "."；dironly 时只要目录（跟随符号链接，同 entry.is_dir()）。
func (g *pyGlobber) iterdir(dirname string, dironly bool) []string {
	arg := dirname
	if arg == "" {
		arg = "."
	}
	entries, err := os.ReadDir(arg)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if dironly {
			isDir := e.IsDir()
			if !isDir && e.Type()&os.ModeSymlink != 0 {
				isDir = pyIsDirPath(posixJoin(arg, e.Name()))
			}
			if !isDir {
				continue
			}
		}
		out = append(out, e.Name())
	}
	return out
}

func (g *pyGlobber) rlistdir(dirname string, dironly bool) []string {
	var out []string
	for _, x := range g.iterdir(dirname, dironly) {
		if g.hidden || !pyIsHidden(x) {
			out = append(out, x)
			path := x
			if dirname != "" {
				path = globJoin(dirname, x)
			}
			for _, y := range g.rlistdir(path, dironly) {
				out = append(out, globJoin(x, y))
			}
		}
	}
	return out
}

func (g *pyGlobber) glob2(dirname, pattern string, dironly bool) []string {
	var out []string
	if dirname == "" || pyIsDirPath(dirname) {
		out = append(out, "")
	}
	return append(out, g.rlistdir(dirname, dironly)...)
}

func (g *pyGlobber) glob1(dirname, pattern string, dironly bool) []string {
	names := g.iterdir(dirname, dironly)
	if g.hidden || !pyIsHidden(pattern) {
		kept := names[:0:0]
		for _, x := range names {
			if g.hidden || !pyIsHidden(x) {
				kept = append(kept, x)
			}
		}
		names = kept
	}
	m := fnCompile(pattern)
	var out []string
	for _, x := range names {
		if m.match([]rune(x)) {
			out = append(out, x)
		}
	}
	return out
}

func (g *pyGlobber) glob0(dirname, basename string, dironly bool) []string {
	if basename != "" {
		if pyLexists(globJoin(dirname, basename)) {
			return []string{basename}
		}
	} else if pyIsDirPath(dirname) {
		return []string{basename}
	}
	return nil
}

// iglob 对应 _iglob（root_dir 恒为 ""）。
func (g *pyGlobber) iglob(pathname string, dironly bool) []string {
	dirname, basename := posixSplit(pathname)
	if !PyGlobHasMagic(pathname) {
		if basename != "" {
			if pyLexists(pathname) {
				return []string{pathname}
			}
		} else if pyIsDirPath(dirname) {
			return []string{pathname}
		}
		return nil
	}
	if dirname == "" {
		if g.recursive && pyIsRecursive(basename) {
			return g.glob2("", basename, dironly)
		}
		return g.glob1("", basename, dironly)
	}
	var dirs []string
	if dirname != pathname && PyGlobHasMagic(dirname) {
		dirs = g.iglob(dirname, true)
	} else {
		dirs = []string{dirname}
	}
	var out []string
	for _, d := range dirs {
		var names []string
		switch {
		case PyGlobHasMagic(basename) && g.recursive && pyIsRecursive(basename):
			names = g.glob2(d, basename, dironly)
		case PyGlobHasMagic(basename):
			names = g.glob1(d, basename, dironly)
		default:
			names = g.glob0(d, basename, dironly)
		}
		for _, n := range names {
			out = append(out, posixJoin(d, n))
		}
	}
	return out
}

// ---------------------------------------------------------------- fnmatch

type fnTok struct {
	kind   byte // '*' 任意串、'?' 任一字符、'c' 字面字符、'[' 字符集
	ch     rune
	neg    bool
	any    bool // 字符集退化成「任一字符」（fnmatch 的 stuff == "!"）
	never  bool // 空字符集：永不匹配
	ranges [][2]rune
}

type fnPattern struct{ toks []fnTok }

// fnCompile 按 fnmatch._translate 的规则把模式切成记号（连续的 * 压成一个）。
func fnCompile(pat string) *fnPattern {
	p := []rune(pat)
	n := len(p)
	var toks []fnTok
	i := 0
	for i < n {
		c := p[i]
		i++
		switch c {
		case '*':
			if len(toks) == 0 || toks[len(toks)-1].kind != '*' {
				toks = append(toks, fnTok{kind: '*'})
			}
		case '?':
			toks = append(toks, fnTok{kind: '?'})
		case '[':
			j := i
			if j < n && p[j] == '!' {
				j++
			}
			if j < n && p[j] == ']' {
				j++
			}
			for j < n && p[j] != ']' {
				j++
			}
			if j >= n {
				toks = append(toks, fnTok{kind: 'c', ch: '['})
				continue
			}
			toks = append(toks, fnClass(p, i, j))
			i = j + 1
		default:
			toks = append(toks, fnTok{kind: 'c', ch: c})
		}
	}
	return &fnPattern{toks: toks}
}

// fnClass 复刻 _translate 对 pat[i:j]（方括号之间）的处理：按 '-' 切块、删掉空区间（前块末字符 > 后块首字符时
// 合并），块与块之间的 '-' 是区间运算符，其余字符（含转义后的 '\\' 与 '-'）都是字面成员。
func fnClass(p []rune, i, j int) fnTok {
	stuff := p[i:j]
	hasDash := false
	for _, r := range stuff {
		if r == '-' {
			hasDash = true
			break
		}
	}
	var chunks [][]rune
	if !hasDash {
		chunks = [][]rune{append([]rune(nil), stuff...)}
	} else {
		k := i + 1
		if p[i] == '!' {
			k = i + 2
		}
		for {
			f := -1
			for q := k; q < j; q++ {
				if p[q] == '-' {
					f = q
					break
				}
			}
			if f < 0 {
				break
			}
			chunks = append(chunks, append([]rune(nil), p[i:f]...))
			i = f + 1
			k = f + 3
		}
		if chunk := p[i:j]; len(chunk) > 0 {
			chunks = append(chunks, append([]rune(nil), chunk...))
		} else {
			chunks[len(chunks)-1] = append(chunks[len(chunks)-1], '-')
		}
		for k := len(chunks) - 1; k > 0; k-- {
			a, b := chunks[k-1], chunks[k]
			if a[len(a)-1] > b[0] {
				merged := append(append([]rune(nil), a[:len(a)-1]...), b[1:]...)
				chunks[k-1] = merged
				chunks = append(chunks[:k], chunks[k+1:]...)
			}
		}
	}
	// 重新拼出 stuff 的「字面形态」以判断空 / "!" 两种特例（转义不改变长度判断：空即空，"!" 即单个 '!'）。
	total := 0
	for _, ch := range chunks {
		total += len(ch)
	}
	total += len(chunks) - 1
	if total <= 0 {
		return fnTok{kind: '[', never: true}
	}
	if total == 1 && len(chunks) == 1 && len(chunks[0]) == 1 && chunks[0][0] == '!' {
		return fnTok{kind: '[', any: true}
	}
	tok := fnTok{kind: '['}
	if len(chunks[0]) > 0 && chunks[0][0] == '!' {
		tok.neg = true
		chunks[0] = chunks[0][1:]
	}
	for k, ch := range chunks {
		for _, r := range ch {
			tok.ranges = append(tok.ranges, [2]rune{r, r})
		}
		if k > 0 {
			prev := chunks[k-1]
			if len(prev) > 0 && len(ch) > 0 {
				tok.ranges = append(tok.ranges, [2]rune{prev[len(prev)-1], ch[0]})
			}
		}
	}
	if len(tok.ranges) == 0 && tok.neg {
		// 只剩一个 '!'（被当成否定号）且再无成员：正则是 [^]…，与 Python 一样按「否定空集」= 任一字符处理不了，
		// 但这种形态已在上面的 "!" 特例里返回，这里不会到达。
		tok.any = true
	}
	return tok
}

func (t *fnTok) matchRune(r rune) bool {
	switch t.kind {
	case '?':
		return true
	case 'c':
		return r == t.ch
	}
	if t.never {
		return false
	}
	if t.any {
		return true
	}
	in := false
	for _, rg := range t.ranges {
		if rg[0] <= r && r <= rg[1] {
			in = true
			break
		}
	}
	return in != t.neg
}

// match：整串匹配（正则 (?s:...)\Z），* 用经典的回溯到上一个星号。
func (m *fnPattern) match(s []rune) bool {
	ti, si := 0, 0
	starT, starS := -1, 0
	for si < len(s) {
		if ti < len(m.toks) && m.toks[ti].kind == '*' {
			starT, starS = ti, si
			ti++
			continue
		}
		if ti < len(m.toks) && m.toks[ti].matchRune(s[si]) {
			ti++
			si++
			continue
		}
		if starT >= 0 {
			ti = starT + 1
			starS++
			si = starS
			continue
		}
		return false
	}
	for ti < len(m.toks) && m.toks[ti].kind == '*' {
		ti++
	}
	return ti == len(m.toks)
}

// Symlink 对应 os.symlink(src, dst)。
func Symlink(src, dst string) {
	if err := os.Symlink(src, dst); err != nil {
		raiseIO(err, dst)
	}
}

// IsLink 对应 os.path.islink(path)：Lstat 判符号链接（断链也算），出错为 false。
func IsLink(path string) bool {
	st, err := os.Lstat(path)
	return err == nil && st.Mode()&os.ModeSymlink != 0
}
