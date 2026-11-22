package rt

import (
	"regexp"
	"strings"
	"sync"
)

// Regex 是编译后的正则表达式（RE2 语法）。
type Regex struct {
	re      *regexp.Regexp
	anchor  *regexp.Regexp
	full    *regexp.Regexp
	pattern string
}

// Match 是一次匹配的结果。
type Match struct {
	s   string
	loc []int
	re  *regexp.Regexp
}

var (
	reMu    sync.Mutex
	reCache = map[string]*Regex{}
)

func ReCompile(pattern string) *Regex {
	reMu.Lock()
	defer reMu.Unlock()
	if r, ok := reCache[pattern]; ok {
		return r
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		Raise("ValueError", "无效的正则表达式 %s：%v", QuoteStr(pattern), err)
	}
	r := &Regex{re: re, pattern: pattern}
	if len(reCache) < 256 {
		reCache[pattern] = r
	}
	return r
}

func (r *Regex) anchored() *regexp.Regexp {
	if r.anchor == nil {
		r.anchor = regexp.MustCompile(`\A(?:` + r.pattern + `)`)
	}
	return r.anchor
}

func (r *Regex) fullRe() *regexp.Regexp {
	if r.full == nil {
		r.full = regexp.MustCompile(`\A(?:` + r.pattern + `)\z`)
	}
	return r.full
}

func mkMatch(re *regexp.Regexp, s string, loc []int) Opt[*Match] {
	if loc == nil {
		return Opt[*Match]{}
	}
	return Opt[*Match]{V: &Match{s: s, loc: loc, re: re}, Ok: true}
}

func (r *Regex) Match(s string) Opt[*Match] {
	re := r.anchored()
	return mkMatch(re, s, re.FindStringSubmatchIndex(s))
}

func (r *Regex) FullMatch(s string) Opt[*Match] {
	re := r.fullRe()
	return mkMatch(re, s, re.FindStringSubmatchIndex(s))
}

func (r *Regex) Search(s string) Opt[*Match] {
	return mkMatch(r.re, s, r.re.FindStringSubmatchIndex(s))
}

func (r *Regex) Test(s string) bool { return r.re.MatchString(s) }

// FindAll 返回所有匹配：没有分组时是整个匹配，有一个分组时是该分组的内容。
func (r *Regex) FindAll(s string) *List[string] {
	ms := r.re.FindAllStringSubmatch(s, -1)
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		if len(m) == 2 {
			out = append(out, m[1])
		} else {
			out = append(out, m[0])
		}
	}
	return &List[string]{S: out}
}

func (r *Regex) FindIter(s string) *List[*Match] {
	locs := r.re.FindAllStringSubmatchIndex(s, -1)
	out := make([]*Match, 0, len(locs))
	for _, loc := range locs {
		out = append(out, &Match{s: s, loc: loc, re: r.re})
	}
	return &List[*Match]{S: out}
}

// pyRepl 把 Python 风格的替换模板（\1、\g<name>）转换为 Go 的 ${1}、${name}。
func pyRepl(repl string) string {
	var sb strings.Builder
	for i := 0; i < len(repl); i++ {
		c := repl[i]
		if c == '$' {
			sb.WriteString("$$")
			continue
		}
		if c == '\\' && i+1 < len(repl) {
			n := repl[i+1]
			if n >= '0' && n <= '9' {
				j := i + 1
				for j < len(repl) && repl[j] >= '0' && repl[j] <= '9' {
					j++
				}
				sb.WriteString("${" + repl[i+1:j] + "}")
				i = j - 1
				continue
			}
			if n == 'g' && i+2 < len(repl) && repl[i+2] == '<' {
				if end := strings.IndexByte(repl[i+3:], '>'); end >= 0 {
					sb.WriteString("${" + repl[i+3:i+3+end] + "}")
					i = i + 3 + end
					continue
				}
			}
			if n == '\\' {
				sb.WriteByte('\\')
				i++
				continue
			}
		}
		sb.WriteByte(c)
	}
	return sb.String()
}

func (r *Regex) Sub(repl, s string, count int) string {
	tmpl := pyRepl(repl)
	locs := r.re.FindAllStringSubmatchIndex(s, -1)
	if count > 0 && len(locs) > count {
		locs = locs[:count]
	}
	var out []byte
	last := 0
	for _, loc := range locs {
		out = append(out, s[last:loc[0]]...)
		out = r.re.ExpandString(out, tmpl, s, loc)
		last = loc[1]
	}
	out = append(out, s[last:]...)
	return string(out)
}

func (r *Regex) SubFunc(f func(*Match) string, s string) string {
	locs := r.re.FindAllStringSubmatchIndex(s, -1)
	var sb strings.Builder
	last := 0
	for _, loc := range locs {
		sb.WriteString(s[last:loc[0]])
		sb.WriteString(f(&Match{s: s, loc: loc, re: r.re}))
		last = loc[1]
	}
	sb.WriteString(s[last:])
	return sb.String()
}

func (r *Regex) Split(s string, maxsplit int) *List[string] {
	n := -1
	if maxsplit > 0 {
		n = maxsplit + 1
	}
	return &List[string]{S: r.re.Split(s, n)}
}

func (r *Regex) Pattern() string { return r.pattern }
func (r *Regex) String() string  { return "re.compile(" + QuoteStr(r.pattern) + ")" }

func (m *Match) Group(i int) string {
	if i < 0 || 2*i+1 >= len(m.loc) {
		Raise("IndexError", "没有第 %d 个分组", i)
	}
	if m.loc[2*i] < 0 {
		return ""
	}
	return m.s[m.loc[2*i]:m.loc[2*i+1]]
}

func (m *Match) GroupName(name string) string {
	i := m.re.SubexpIndex(name)
	if i < 0 {
		Raise("IndexError", "没有名为 %s 的分组", name)
	}
	return m.Group(i)
}

func (m *Match) Groups() *List[string] {
	n := len(m.loc)/2 - 1
	out := make([]string, n)
	for i := 1; i <= n; i++ {
		out[i-1] = m.Group(i)
	}
	return &List[string]{S: out}
}

func (m *Match) Start(i int) int {
	if i < 0 || 2*i >= len(m.loc) || m.loc[2*i] < 0 {
		return -1
	}
	return runeIndex(m.s, m.loc[2*i])
}

func (m *Match) End(i int) int {
	if i < 0 || 2*i+1 >= len(m.loc) || m.loc[2*i+1] < 0 {
		return -1
	}
	return runeIndex(m.s, m.loc[2*i+1])
}

func (m *Match) Text() string   { return m.Group(0) }
func (m *Match) String() string { return "<匹配 " + QuoteStr(m.Group(0)) + ">" }

func ReMatch(pattern, s string) Opt[*Match]     { return ReCompile(pattern).Match(s) }
func ReFullMatch(pattern, s string) Opt[*Match] { return ReCompile(pattern).FullMatch(s) }
func ReSearch(pattern, s string) Opt[*Match]    { return ReCompile(pattern).Search(s) }
func ReTest(pattern, s string) bool             { return ReCompile(pattern).Test(s) }
func ReFindAll(pattern, s string) *List[string] { return ReCompile(pattern).FindAll(s) }
func ReSub(pattern, repl, s string, count int) string {
	return ReCompile(pattern).Sub(repl, s, count)
}
func ReSplit(pattern, s string, maxsplit int) *List[string] {
	return ReCompile(pattern).Split(s, maxsplit)
}
