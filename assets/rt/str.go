package rt

import (
	"fmt"
	"iter"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
	"unsafe"
)

// ===== str() 与 repr() =====

type reprer interface{ Reprꞏ() string }

// Str 实现 str(x)。
func Str(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case float64:
		return FloatStr(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case Char:
		return string(rune(x))
	case int8:
		return strconv.FormatInt(int64(x), 10)
	case int16:
		return strconv.FormatInt(int64(x), 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint8:
		return strconv.FormatUint(uint64(x), 10)
	case uint16:
		return strconv.FormatUint(uint64(x), 10)
	case uint32:
		return strconv.FormatUint(uint64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case float32:
		return FloatStr32(x)
	case fmt.Stringer:
		return x.String()
	case error:
		return x.Error()
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Func:
		if rv.IsNil() {
			return "null"
		}
		return "<函数>"
	case reflect.Chan:
		return "<通道>"
	case reflect.Pointer:
		if rv.IsNil() {
			return "null"
		}
	}
	return fmt.Sprint(v)
}

// Repr 实现 repr(x)：字符串带引号。
func Repr(v any) string {
	switch x := v.(type) {
	case string:
		return QuoteStr(x)
	case Char:
		return quoteChar(rune(x))
	case reprer:
		return x.Reprꞏ()
	}
	return Str(v)
}

// QuoteStr 返回带双引号和转义的字符串。
func QuoteStr(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\t':
			sb.WriteString(`\t`)
		case '\r':
			sb.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&sb, `\x%02x`, r)
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

func quoteChar(r rune) string {
	switch r {
	case '\'':
		return `'\''`
	case '\\':
		return `'\\'`
	case '\n':
		return `'\n'`
	case '\t':
		return `'\t'`
	case '\r':
		return `'\r'`
	}
	if r < 0x20 {
		return fmt.Sprintf(`'\x%02x'`, r)
	}
	return "'" + string(r) + "'"
}

// FloatStr 按 Python 的 repr 规则格式化浮点数。
func FloatStr(f float64) string { return floatRepr(f, 64) }

func FloatStr32(f float32) string { return floatRepr(float64(f), 32) }

func floatRepr(f float64, bits int) string {
	switch {
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case math.IsNaN(f):
		return "nan"
	case f == 0:
		if math.Signbit(f) {
			return "-0.0"
		}
		return "0.0"
	}
	e := strconv.FormatFloat(f, 'e', -1, bits)
	neg := false
	if e[0] == '-' {
		neg = true
		e = e[1:]
	}
	mant, expS, _ := strings.Cut(e, "e")
	exp, _ := strconv.Atoi(expS)
	digits := strings.Replace(mant, ".", "", 1)
	var s string
	if exp < -4 || exp >= 16 {
		if len(digits) == 1 {
			s = digits
		} else {
			s = digits[:1] + "." + digits[1:]
		}
		sign := "+"
		if exp < 0 {
			sign = "-"
			exp = -exp
		}
		s += fmt.Sprintf("e%s%02d", sign, exp)
	} else if exp < 0 {
		s = "0." + strings.Repeat("0", -exp-1) + digits
	} else if exp+1 >= len(digits) {
		s = digits + strings.Repeat("0", exp+1-len(digits)) + ".0"
	} else {
		s = digits[:exp+1] + "." + digits[exp+1:]
	}
	if neg {
		return "-" + s
	}
	return s
}

func BoolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func CharStr(c Char) string { return string(rune(c)) }

// TypeName 返回值的五行类型名。
func TypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case int:
		return "int"
	case float64:
		return "float"
	case string:
		return "str"
	case bool:
		return "bool"
	case Char:
		return "char"
	case int8:
		return "i8"
	case int16:
		return "i16"
	case int32:
		return "i32"
	case int64:
		return "int"
	case uint8:
		return "u8"
	case uint16:
		return "u16"
	case uint32:
		return "u32"
	case uint64:
		return "u64"
	case float32:
		return "f32"
	case interface{ TypeNameꞏ() string }:
		return v.(interface{ TypeNameꞏ() string }).TypeNameꞏ()
	}
	t := reflect.TypeOf(v)
	name := t.String()
	switch {
	case strings.Contains(name, "rt.List["):
		return "list"
	case strings.Contains(name, "rt.Dict["):
		return "dict"
	case strings.Contains(name, "rt.Set["):
		return "set"
	case strings.Contains(name, "rt.Opt["):
		return "可空值"
	case strings.Contains(name, "rt.T"):
		return "tuple"
	case t.Kind() == reflect.Func:
		return "fn"
	case t.Kind() == reflect.Chan:
		return "chan"
	}
	return strings.TrimPrefix(strings.TrimPrefix(name, "*"), "main.")
}

// ===== 格式化（f-字符串的格式说明） =====

type fmtSpec struct {
	fill     rune
	align    byte
	sign     byte
	alt      bool
	zero     bool
	width    int
	group    byte
	prec     int
	typ      byte
}

func parseSpec(spec string) fmtSpec {
	fs := fmtSpec{fill: ' ', prec: -1}
	rs := []rune(spec)
	i := 0
	isAlign := func(r rune) bool { return r == '<' || r == '>' || r == '^' || r == '=' }
	if len(rs) >= 2 && isAlign(rs[1]) {
		fs.fill = rs[0]
		fs.align = byte(rs[1])
		i = 2
	} else if len(rs) >= 1 && isAlign(rs[0]) {
		fs.align = byte(rs[0])
		i = 1
	}
	if i < len(rs) && (rs[i] == '+' || rs[i] == '-' || rs[i] == ' ') {
		fs.sign = byte(rs[i])
		i++
	}
	if i < len(rs) && rs[i] == '#' {
		fs.alt = true
		i++
	}
	if i < len(rs) && rs[i] == '0' {
		fs.zero = true
		i++
	}
	for i < len(rs) && rs[i] >= '0' && rs[i] <= '9' {
		fs.width = fs.width*10 + int(rs[i]-'0')
		i++
	}
	if i < len(rs) && (rs[i] == ',' || rs[i] == '_') {
		fs.group = byte(rs[i])
		i++
	}
	if i < len(rs) && rs[i] == '.' {
		i++
		fs.prec = 0
		for i < len(rs) && rs[i] >= '0' && rs[i] <= '9' {
			fs.prec = fs.prec*10 + int(rs[i]-'0')
			i++
		}
	}
	if i < len(rs) {
		fs.typ = byte(rs[i])
		i++
	}
	if i < len(rs) {
		Raise("ValueError", "无效的格式说明 '%s'", spec)
	}
	return fs
}

func groupDigits(s string, sep byte, every int) string {
	if len(s) <= every {
		return s
	}
	var sb strings.Builder
	pre := len(s) % every
	if pre > 0 {
		sb.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += every {
		if sb.Len() > 0 {
			sb.WriteByte(sep)
		}
		sb.WriteString(s[i : i+every])
	}
	return sb.String()
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int8:
		return float64(x), true
	case int16:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint8:
		return float64(x), true
	case uint16:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	}
	return 0, false
}

func toInt(v any) (int64, bool, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true, false
	case int8:
		return int64(x), true, false
	case int16:
		return int64(x), true, false
	case int32:
		return int64(x), true, false
	case int64:
		return x, true, false
	case uint8:
		return int64(x), true, false
	case uint16:
		return int64(x), true, false
	case uint32:
		return int64(x), true, false
	case uint64:
		return int64(x), true, x > math.MaxInt64
	case Char:
		return int64(x), true, false
	case bool:
		if x {
			return 1, true, false
		}
		return 0, true, false
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() >= reflect.Int && rv.Kind() <= reflect.Int64 {
		if _, ok := v.(fmt.Stringer); !ok {
			return rv.Int(), true, false
		}
	}
	return 0, false, false
}

// Fmt 按格式说明格式化一个值（f"{x:.2f}"）。
func Fmt(v any, spec string) string {
	if spec == "" {
		return Str(v)
	}
	fs := parseSpec(spec)
	var body, sign, prefix string
	numeric := false
	if c, ok := v.(Char); ok && (fs.typ == 0 || fs.typ == 's' || fs.typ == 'c') {
		body = string(rune(c))
		v = body
	}
	if iv, isInt, big := toInt(v); isInt && !big && fs.typ != 's' {
		numeric = true
		switch fs.typ {
		case 0, 'd', 'n':
			if iv < 0 {
				sign = "-"
				body = strconv.FormatUint(uint64(-iv), 10)
			} else {
				body = strconv.FormatInt(iv, 10)
			}
			if fs.group != 0 {
				body = groupDigits(body, fs.group, 3)
			}
		case 'x', 'X', 'o', 'b':
			base := map[byte]int{'x': 16, 'X': 16, 'o': 8, 'b': 2}[fs.typ]
			u := uint64(iv)
			if iv < 0 {
				sign = "-"
				u = uint64(-iv)
			}
			body = strconv.FormatUint(u, base)
			if fs.typ == 'X' {
				body = strings.ToUpper(body)
			}
			if fs.alt {
				prefix = map[byte]string{'x': "0x", 'X': "0X", 'o': "0o", 'b': "0b"}[fs.typ]
			}
			if fs.group != 0 {
				body = groupDigits(body, '_', 4)
			}
		case 'c':
			body = string(rune(iv))
			numeric = false
		case 'e', 'E', 'f', 'F', 'g', 'G', '%':
			return Fmt(float64(iv), spec)
		default:
			Raise("ValueError", "整数不支持格式 '%c'", fs.typ)
		}
	} else if f, isF := toFloat(v); isF && fs.typ != 's' {
		numeric = true
		if math.Signbit(f) && !math.IsNaN(f) {
			sign = "-"
			f = -f
		}
		prec := fs.prec
		switch fs.typ {
		case 'f', 'F':
			if prec < 0 {
				prec = 6
			}
			body = strconv.FormatFloat(f, 'f', prec, 64)
		case 'e', 'E':
			if prec < 0 {
				prec = 6
			}
			body = strconv.FormatFloat(f, 'e', prec, 64)
			if fs.typ == 'E' {
				body = strings.ToUpper(body)
			}
		case 'g', 'G':
			if prec < 0 {
				prec = 6
			}
			if prec == 0 {
				prec = 1
			}
			body = strconv.FormatFloat(f, 'g', prec, 64)
			if fs.typ == 'G' {
				body = strings.ToUpper(body)
			}
		case '%':
			if prec < 0 {
				prec = 6
			}
			body = strconv.FormatFloat(f*100, 'f', prec, 64) + "%"
		case 0, 'n':
			if prec >= 0 {
				body = strconv.FormatFloat(f, 'g', max(prec, 1), 64)
			} else {
				body = FloatStr(f)
			}
		default:
			Raise("ValueError", "浮点数不支持格式 '%c'", fs.typ)
		}
		if math.IsInf(f, 0) {
			body = "inf"
		} else if math.IsNaN(f) {
			body = "nan"
		}
		if fs.group != 0 {
			intPart, frac, hasFrac := strings.Cut(body, ".")
			intPart = groupDigits(intPart, fs.group, 3)
			if hasFrac {
				body = intPart + "." + frac
			} else {
				body = intPart
			}
		}
	} else {
		body = Str(v)
		if fs.prec >= 0 {
			rs := []rune(body)
			if len(rs) > fs.prec {
				body = string(rs[:fs.prec])
			}
		}
	}
	if numeric && sign == "" {
		if fs.sign == '+' {
			sign = "+"
		} else if fs.sign == ' ' {
			sign = " "
		}
	}
	content := sign + prefix + body
	n := utf8.RuneCountInString(content)
	if fs.width <= n {
		return content
	}
	pad := fs.width - n
	align := fs.align
	fill := fs.fill
	if fs.zero && align == 0 && numeric {
		align = '='
		fill = '0'
	}
	if align == 0 {
		if numeric {
			align = '>'
		} else {
			align = '<'
		}
	}
	padding := func(k int) string { return strings.Repeat(string(fill), k) }
	switch align {
	case '<':
		return content + padding(pad)
	case '^':
		left := pad / 2
		return padding(left) + content + padding(pad-left)
	case '=':
		return sign + prefix + padding(pad) + body
	}
	return padding(pad) + content
}

// ===== 字符串：按字符（码点）下标 =====

type strCache struct {
	s     string
	runes []rune // nil 表示纯 ASCII
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

var strMu sync.Mutex
var strLast strCache

// runesOf 返回字符串的码点切片（ASCII 字符串返回 nil）。带单项缓存。
func runesOf(s string) ([]rune, bool) {
	strMu.Lock()
	c := strLast
	strMu.Unlock()
	if len(c.s) == len(s) && unsafe.StringData(c.s) == unsafe.StringData(s) && c.s != "" {
		return c.runes, c.runes == nil
	}
	var r []rune
	ascii := isASCII(s)
	if !ascii {
		r = []rune(s)
	}
	if len(s) > 16 {
		strMu.Lock()
		strLast = strCache{s: s, runes: r}
		strMu.Unlock()
	}
	return r, ascii
}

// StrLen 返回字符数（码点数）。
func StrLen(s string) int {
	if len(s) < 64 {
		for i := 0; i < len(s); i++ {
			if s[i] >= utf8.RuneSelf {
				return utf8.RuneCountInString(s)
			}
		}
		return len(s)
	}
	return utf8.RuneCountInString(s)
}

// StrAt 实现 s[i]。
func StrAt(s string, i int) Char {
	r, ascii := runesOf(s)
	n := len(s)
	if !ascii {
		n = len(r)
	}
	if i < 0 {
		i += n
	}
	if i < 0 || i >= n {
		Raise("IndexError", "字符串下标 %d 超出范围（长度为 %d）", i, n)
	}
	if ascii {
		return Char(s[i])
	}
	return Char(r[i])
}

// StrSlice 实现 s[lo:hi:step]。
func StrSlice(s string, lo, hi, step int) string {
	r, ascii := runesOf(s)
	n := len(s)
	if !ascii {
		n = len(r)
	}
	lo, hi, step = sliceBounds(n, lo, hi, step)
	if step == 1 {
		if hi <= lo {
			return ""
		}
		if ascii {
			return s[lo:hi]
		}
		return string(r[lo:hi])
	}
	var sb strings.Builder
	at := func(i int) {
		if ascii {
			sb.WriteByte(s[i])
		} else {
			sb.WriteRune(r[i])
		}
	}
	if step > 0 {
		for i := lo; i < hi; i += step {
			at(i)
		}
	} else {
		for i := lo; i > hi; i += step {
			at(i)
		}
	}
	return sb.String()
}

func StrIter(s string) iter.Seq[Char] {
	return func(yield func(Char) bool) {
		for _, r := range s {
			if !yield(Char(r)) {
				return
			}
		}
	}
}

func StrChars(s string) *List[Char] {
	out := make([]Char, 0, len(s))
	for _, r := range s {
		out = append(out, Char(r))
	}
	return &List[Char]{S: out}
}

func StrBytes(s string) *List[uint8] {
	return &List[uint8]{S: []byte(s)}
}

func StrFromChars(l *List[Char]) string {
	var sb strings.Builder
	for _, c := range l.S {
		sb.WriteRune(rune(c))
	}
	return sb.String()
}

func StrFromBytes(l *List[uint8]) string { return string(l.S) }

func StrToChar(s string) Char {
	if utf8.RuneCountInString(s) != 1 {
		Raise("ValueError", "char() 需要长度为 1 的字符串，得到 %s", QuoteStr(s))
	}
	r, _ := utf8.DecodeRuneInString(s)
	return Char(r)
}

func Ord(s string) int {
	if utf8.RuneCountInString(s) != 1 {
		Raise("ValueError", "ord() 需要一个字符，得到长度为 %d 的字符串", utf8.RuneCountInString(s))
	}
	r, _ := utf8.DecodeRuneInString(s)
	return int(r)
}

func Chr(i int) string {
	if i < 0 || i > unicode.MaxRune {
		Raise("ValueError", "chr() 的参数 %d 超出 Unicode 范围", i)
	}
	return string(rune(i))
}

func StrRepeat(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(s, n)
}

// runeIndex 把字节偏移转换为字符下标。
func runeIndex(s string, byteOff int) int {
	if byteOff < 0 {
		return -1
	}
	return utf8.RuneCountInString(s[:byteOff])
}

// byteOffset 把字符下标转换为字节偏移。
func byteOffset(s string, ri int) int {
	if ri <= 0 {
		return 0
	}
	i := 0
	for off := range s {
		if i == ri {
			return off
		}
		i++
	}
	return len(s)
}

func StrFind(s, sub string, start int) int {
	if start < 0 {
		start += StrLen(s)
		if start < 0 {
			start = 0
		}
	}
	off := byteOffset(s, start)
	if off > len(s) {
		return -1
	}
	j := strings.Index(s[off:], sub)
	if j < 0 {
		return -1
	}
	return runeIndex(s, off+j)
}

func StrRFind(s, sub string) int {
	return runeIndex(s, strings.LastIndex(s, sub))
}

func StrIndex(s, sub string, start int) int {
	i := StrFind(s, sub, start)
	if i < 0 {
		Raise("ValueError", "找不到子串 %s", QuoteStr(sub))
	}
	return i
}

func StrCount(s, sub string) int { return strings.Count(s, sub) }

func StrReplace(s, old, new string, count int) string {
	return strings.Replace(s, old, new, count)
}

func StrSplit(s string, sep Opt[string], maxsplit int) *List[string] {
	if !sep.Ok {
		if maxsplit < 0 {
			return &List[string]{S: strings.Fields(s)}
		}
		var out []string
		rest := strings.TrimLeftFunc(s, unicode.IsSpace)
		for len(out) < maxsplit && rest != "" {
			i := strings.IndexFunc(rest, unicode.IsSpace)
			if i < 0 {
				break
			}
			out = append(out, rest[:i])
			rest = strings.TrimLeftFunc(rest[i:], unicode.IsSpace)
		}
		if rest != "" {
			out = append(out, rest)
		}
		return &List[string]{S: out}
	}
	if sep.V == "" {
		Raise("ValueError", "分隔符不能为空字符串")
	}
	n := -1
	if maxsplit >= 0 {
		n = maxsplit + 1
	}
	return &List[string]{S: strings.SplitN(s, sep.V, n)}
}

func StrRSplit(s string, sep Opt[string], maxsplit int) *List[string] {
	if maxsplit < 0 {
		return StrSplit(s, sep, -1)
	}
	if !sep.Ok {
		fields := strings.Fields(s)
		if len(fields) <= maxsplit+1 {
			return &List[string]{S: fields}
		}
		// 保留左侧原文
		k := len(fields) - maxsplit
		cut := 0
		count := 0
		inField := false
		for i, r := range s {
			if unicode.IsSpace(r) {
				if inField {
					count++
					inField = false
					if count == k {
						cut = i
						break
					}
				}
			} else {
				inField = true
			}
		}
		left := strings.TrimSpace(s[:cut])
		return &List[string]{S: append([]string{left}, fields[k:]...)}
	}
	parts := strings.Split(s, sep.V)
	if len(parts) <= maxsplit+1 {
		return &List[string]{S: parts}
	}
	k := len(parts) - maxsplit
	left := strings.Join(parts[:k], sep.V)
	return &List[string]{S: append([]string{left}, parts[k:]...)}
}

func StrSplitLines(s string) *List[string] {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if s == "" {
		return &List[string]{}
	}
	parts := strings.Split(s, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return &List[string]{S: parts}
}

func StrJoin(sep string, it iter.Seq[string]) string {
	var sb strings.Builder
	first := true
	for x := range it {
		if !first {
			sb.WriteString(sep)
		}
		first = false
		sb.WriteString(x)
	}
	return sb.String()
}

func StrStrip(s string, chars Opt[string]) string {
	if !chars.Ok {
		return strings.TrimSpace(s)
	}
	return strings.Trim(s, chars.V)
}

func StrLStrip(s string, chars Opt[string]) string {
	if !chars.Ok {
		return strings.TrimLeftFunc(s, unicode.IsSpace)
	}
	return strings.TrimLeft(s, chars.V)
}

func StrRStrip(s string, chars Opt[string]) string {
	if !chars.Ok {
		return strings.TrimRightFunc(s, unicode.IsSpace)
	}
	return strings.TrimRight(s, chars.V)
}

func strAll(s string, f func(rune) bool) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !f(r) {
			return false
		}
	}
	return true
}

func StrIsDigit(s string) bool { return strAll(s, unicode.IsDigit) }
func StrIsAlpha(s string) bool { return strAll(s, unicode.IsLetter) }
func StrIsSpace(s string) bool { return strAll(s, unicode.IsSpace) }
func StrIsAlnum(s string) bool {
	return strAll(s, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) })
}
func StrIsNumeric(s string) bool { return strAll(s, unicode.IsNumber) }

func StrIsUpper(s string) bool {
	cased := false
	for _, r := range s {
		if unicode.IsLower(r) {
			return false
		}
		if unicode.IsUpper(r) {
			cased = true
		}
	}
	return cased
}

func StrIsLower(s string) bool {
	cased := false
	for _, r := range s {
		if unicode.IsUpper(r) {
			return false
		}
		if unicode.IsLower(r) {
			cased = true
		}
	}
	return cased
}

func StrTitle(s string) string {
	var sb strings.Builder
	prev := false
	for _, r := range s {
		if unicode.IsLetter(r) {
			if prev {
				sb.WriteRune(unicode.ToLower(r))
			} else {
				sb.WriteRune(unicode.ToTitle(r))
			}
			prev = true
		} else {
			sb.WriteRune(r)
			prev = false
		}
	}
	return sb.String()
}

func StrCapitalize(s string) string {
	for i, r := range s {
		return string(unicode.ToUpper(r)) + strings.ToLower(s[i+utf8.RuneLen(r):])
	}
	return s
}

func StrSwapCase(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsUpper(r):
			sb.WriteRune(unicode.ToLower(r))
		case unicode.IsLower(r):
			sb.WriteRune(unicode.ToUpper(r))
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func StrCenter(s string, width int, fill Opt[string]) string {
	f := " "
	if fill.Ok {
		f = fill.V
	}
	n := StrLen(s)
	if width <= n {
		return s
	}
	pad := width - n
	left := pad / 2
	if pad%2 == 1 && n%2 == 1 {
		left++
	}
	return strings.Repeat(f, left) + s + strings.Repeat(f, pad-left)
}

func StrLJust(s string, width int, fill Opt[string]) string {
	f := " "
	if fill.Ok {
		f = fill.V
	}
	n := StrLen(s)
	if width <= n {
		return s
	}
	return s + strings.Repeat(f, width-n)
}

func StrRJust(s string, width int, fill Opt[string]) string {
	f := " "
	if fill.Ok {
		f = fill.V
	}
	n := StrLen(s)
	if width <= n {
		return s
	}
	return strings.Repeat(f, width-n) + s
}

func StrZFill(s string, width int) string {
	n := StrLen(s)
	if width <= n {
		return s
	}
	sign := ""
	if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") {
		sign, s = s[:1], s[1:]
	}
	return sign + strings.Repeat("0", width-n) + s
}

func StrPartition(s, sep string) T3[string, string, string] {
	before, after, found := strings.Cut(s, sep)
	if !found {
		return T3[string, string, string]{s, "", ""}
	}
	return T3[string, string, string]{before, sep, after}
}

func StrRPartition(s, sep string) T3[string, string, string] {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return T3[string, string, string]{"", "", s}
	}
	return T3[string, string, string]{s[:i], sep, s[i+len(sep):]}
}

func StrReversed(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

func StrIsEmpty(s string) bool { return s == "" }

// ===== 字符方法 =====

func CharIsDigit(c Char) bool  { return unicode.IsDigit(rune(c)) }
func CharIsAlpha(c Char) bool  { return unicode.IsLetter(rune(c)) }
func CharIsSpace(c Char) bool  { return unicode.IsSpace(rune(c)) }
func CharIsUpper(c Char) bool  { return unicode.IsUpper(rune(c)) }
func CharIsLower(c Char) bool  { return unicode.IsLower(rune(c)) }
func CharIsAlnum(c Char) bool  { return unicode.IsLetter(rune(c)) || unicode.IsDigit(rune(c)) }
func CharUpper(c Char) Char    { return Char(unicode.ToUpper(rune(c))) }
func CharLower(c Char) Char    { return Char(unicode.ToLower(rune(c))) }
func CharIsPunct(c Char) bool  { return unicode.IsPunct(rune(c)) }
func CharDigitValue(c Char) int {
	if c >= '0' && c <= '9' {
		return int(c - '0')
	}
	Raise("ValueError", "%s 不是数字字符", quoteChar(rune(c)))
	return 0
}

// ===== 数字与字符串的转换 =====

func cleanNum(s string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "_") {
		s = strings.ReplaceAll(s, "_", "")
	}
	return s
}

func ParseInt(s string) int {
	v, err := strconv.ParseInt(cleanNum(s), 10, 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			Raise("OverflowError", "整数 %s 超出范围", QuoteStr(s))
		}
		Raise("ValueError", "无效的整数：%s", QuoteStr(s))
	}
	return int(v)
}

func ParseIntBase(s string, base int) int {
	c := cleanNum(s)
	low := strings.ToLower(c)
	neg := false
	if strings.HasPrefix(low, "-") {
		neg = true
		low = low[1:]
	} else if strings.HasPrefix(low, "+") {
		low = low[1:]
	}
	switch {
	case base == 16 && strings.HasPrefix(low, "0x"), base == 2 && strings.HasPrefix(low, "0b"), base == 8 && strings.HasPrefix(low, "0o"):
		low = low[2:]
	case base == 0:
		base = 10
		switch {
		case strings.HasPrefix(low, "0x"):
			base, low = 16, low[2:]
		case strings.HasPrefix(low, "0b"):
			base, low = 2, low[2:]
		case strings.HasPrefix(low, "0o"):
			base, low = 8, low[2:]
		}
	}
	if base < 2 || base > 36 {
		Raise("ValueError", "进制必须在 2 到 36 之间")
	}
	v, err := strconv.ParseInt(low, base, 64)
	if err != nil {
		Raise("ValueError", "无效的 %d 进制整数：%s", base, QuoteStr(s))
	}
	if neg {
		v = -v
	}
	return int(v)
}

func ParseFloat(s string) float64 {
	v, err := strconv.ParseFloat(cleanNum(s), 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return v
		}
		Raise("ValueError", "无效的浮点数：%s", QuoteStr(s))
	}
	return v
}

func Hex(i int) string {
	if i < 0 {
		return "-0x" + strconv.FormatUint(uint64(-i), 16)
	}
	return "0x" + strconv.FormatInt(int64(i), 16)
}

func Bin(i int) string {
	if i < 0 {
		return "-0b" + strconv.FormatUint(uint64(-i), 2)
	}
	return "0b" + strconv.FormatInt(int64(i), 2)
}

func Oct(i int) string {
	if i < 0 {
		return "-0o" + strconv.FormatUint(uint64(-i), 8)
	}
	return "0o" + strconv.FormatInt(int64(i), 8)
}

func Itoa(i int) string { return strconv.Itoa(i) }

// ReprFields 用于类/结构体的默认 __str__：Name(a=1, b="x")。
func ReprFields(name string, names []string, vals ...any) string {
	var sb strings.Builder
	sb.WriteString(name)
	sb.WriteByte('(')
	for i, n := range names {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(n)
		sb.WriteByte('=')
		sb.WriteString(reprGuard(vals[i]))
	}
	sb.WriteByte(')')
	return sb.String()
}

var (
	reprDepthMu sync.Mutex
	reprDepth   int
)

func reprGuard(v any) string {
	reprDepthMu.Lock()
	reprDepth++
	d := reprDepth
	reprDepthMu.Unlock()
	defer func() {
		reprDepthMu.Lock()
		reprDepth--
		reprDepthMu.Unlock()
	}()
	if d > 12 {
		return "..."
	}
	return Repr(v)
}
