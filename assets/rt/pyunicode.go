package rt

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// Python 3.12 的 Unicode 判定与小写映射（数据见 pyunicode_tables.go，由 tools/gen_pyunicode.py 从 CPython 逐码位
// 读出）。prelude 里与 Python 同名的 str / char 方法（isalpha / isdecimal / isdigit / isnumeric / isalnum /
// isspace / lower）以及 strip / split 的「空白」都按这里，与 CPython 3.12 逐码位一致；Go 自己的 unicode 包
// 跟的是更新的 Unicode 版本，并且 IsDigit / IsNumber / IsSpace 的定义与 Python 也不一样（例如 Python 的
// isdigit 认上标 ²、isalnum 认 ½、isspace 认 \x1c–\x1f）。

func inRanges(t [][2]rune, r rune) bool {
	i := sort.Search(len(t), func(i int) bool { return t[i][1] >= r })
	return i < len(t) && t[i][0] <= r
}

// PyIsAlpha 对应 Python 的 str.isalpha()（单字符）。
func PyIsAlpha(r rune) bool {
	if r < 0x80 {
		return (r|0x20) >= 'a' && (r|0x20) <= 'z'
	}
	return inRanges(pyAlpha, r)
}

// PyIsDecimal 对应 str.isdecimal()：Unicode 类别 Nd。正则 \d（str 模式）就是它。
func PyIsDecimal(r rune) bool {
	if r < 0x80 {
		return r >= '0' && r <= '9'
	}
	return inRanges(pyDecimal, r)
}

// PyIsDigit 对应 str.isdigit()：Numeric_Type 为 Digit 或 Decimal（含上标 ²³¹ 等）。
func PyIsDigit(r rune) bool {
	if r < 0x80 {
		return r >= '0' && r <= '9'
	}
	return inRanges(pyDigit, r)
}

// PyIsNumeric 对应 str.isnumeric()：Numeric_Type 为 Digit / Decimal / Numeric（含 ½、Ⅻ、一 等）。
func PyIsNumeric(r rune) bool {
	if r < 0x80 {
		return r >= '0' && r <= '9'
	}
	return inRanges(pyNumeric, r)
}

// PyIsAlnum 对应 str.isalnum()：isalpha / isdecimal / isdigit / isnumeric 之一。正则 \w（str 模式）是它加 '_'。
func PyIsAlnum(r rune) bool {
	if r < 0x80 {
		return (r >= '0' && r <= '9') || ((r|0x20) >= 'a' && (r|0x20) <= 'z')
	}
	return inRanges(pyAlpha, r) || inRanges(pyNumeric, r)
}

// PyIsWord 是正则 \w 在 str 模式下的判据（isalnum() 或 '_'）。
func PyIsWord(r rune) bool { return r == '_' || PyIsAlnum(r) }

// PyIsSpace 对应 str.isspace()，也是 strip() / split() 无参数时的「空白」：29 个码位，含 \x1c–\x1f。
func PyIsSpace(r rune) bool {
	if r < 0x80 {
		return r == ' ' || (r >= '\t' && r <= '\r') || (r >= 0x1C && r <= 0x1F)
	}
	return inRanges(pySpace, r)
}

// PyLower 对应 str.lower()：完整小写映射（U+0130 变成两个码位）与希腊文末尾 sigma 规则
// （Objects/unicodeobject.c 的 handle_capital_sigma）。
func PyLower(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return strings.ToLower(s)
	}
	rs := []rune(s)
	var sb strings.Builder
	sb.Grow(len(s))
	for i, r := range rs {
		if r == 0x3A3 {
			sb.WriteRune(pyFinalSigma(rs, i))
			continue
		}
		if m, ok := pyLower[r]; ok {
			for _, x := range m {
				sb.WriteRune(x)
			}
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// pyFinalSigma：\p{cased}\p{case-ignorable}* Σ !(\p{case-ignorable}* \p{cased}) 时是 ς，否则 σ。
func pyFinalSigma(rs []rune, i int) rune {
	j := i - 1
	for j >= 0 && inRanges(pyCaseIgnorable, rs[j]) {
		j--
	}
	final := j >= 0 && inRanges(pyCasedNotIgnorable, rs[j])
	if final && i+1 < len(rs) {
		k := i + 1
		for k < len(rs) && inRanges(pyCaseIgnorable, rs[k]) {
			k++
		}
		final = k == len(rs) || !inRanges(pyCasedNotIgnorable, rs[k])
	}
	if final {
		return 0x3C2
	}
	return 0x3C3
}

// PyCharLower 是单字符的 lower()：char 放不下两个码位，U+0130 取首码位 'i'（Python 给 "i̇"）。
func PyCharLower(r rune) rune {
	if r < 0x80 {
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return r
	}
	if r == 0x3A3 {
		return 0x3C3
	}
	if m, ok := pyLower[r]; ok {
		return m[0]
	}
	return r
}

// pyTrimSpace 是 str.strip() 无参数时的结果。
func pyTrimSpace(s string) string {
	return strings.TrimFunc(s, PyIsSpace)
}

// pySplitWhitespace 是 str.split(None, maxsplit)（maxsplit < 0 不限），逐步对应 CPython
// Objects/stringlib/split.h 的 split_whitespace：剩余部分去掉开头空白、保留结尾空白。按字节下标走，非法
// UTF-8 字节原样保留在结果里。
func pySplitWhitespace(s string, maxsplit int) []string {
	out := []string{}
	n, i, count := len(s), 0, maxsplit
	for count != 0 {
		if count > 0 {
			count--
		}
		for i < n {
			r, w := utf8.DecodeRuneInString(s[i:])
			if !PyIsSpace(r) {
				break
			}
			i += w
		}
		if i == n {
			break
		}
		j := i
		for i < n {
			r, w := utf8.DecodeRuneInString(s[i:])
			if PyIsSpace(r) {
				break
			}
			i += w
		}
		out = append(out, s[j:i])
	}
	if i < n {
		for i < n {
			r, w := utf8.DecodeRuneInString(s[i:])
			if !PyIsSpace(r) {
				break
			}
			i += w
		}
		if i != n {
			out = append(out, s[i:])
		}
	}
	return out
}

// pyRSplitWhitespace 是 str.rsplit(None, maxsplit)，对应 rsplit_whitespace：剩余的左段保留开头空白、
// 去掉结尾空白（"  a b c".rsplit(None, 1) == ["  a b", "c"]）。
func pyRSplitWhitespace(s string, maxsplit int) []string {
	out := []string{}
	i, count := len(s), maxsplit
	for count != 0 {
		if count > 0 {
			count--
		}
		for i > 0 {
			r, w := utf8.DecodeLastRuneInString(s[:i])
			if !PyIsSpace(r) {
				break
			}
			i -= w
		}
		if i == 0 {
			break
		}
		j := i
		for i > 0 {
			r, w := utf8.DecodeLastRuneInString(s[:i])
			if PyIsSpace(r) {
				break
			}
			i -= w
		}
		out = append(out, s[i:j])
	}
	if i > 0 {
		for i > 0 {
			r, w := utf8.DecodeLastRuneInString(s[:i])
			if !PyIsSpace(r) {
				break
			}
			i -= w
		}
		if i > 0 {
			out = append(out, s[:i])
		}
	}
	for a, b := 0, len(out)-1; a < b; a, b = a+1, b-1 {
		out[a], out[b] = out[b], out[a]
	}
	return out
}
