package rt

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// Python 的 str.isprintable() / repr() 用到的「可打印」判据（CPython 3.12，Unicode 15.0.0）。
//
// CPython 的定义：不属于 Cc / Cf / Cs / Co / Cn / Zl / Zp / Zs（ASCII 空格除外）的码位可打印。
// Go 的 unicode.IsPrint 恰好是同一条定义（L / M / N / P / S 加 ASCII 空格），区别只在 Unicode
// 版本：本机 Go 的表是 {unicode.Version}（17.0.0），CPython 3.12 是 15.0.0。15.1–17.0 新分配的
// 码位在 CPython 3.12 里仍是 Cn（不可打印）。下表就是全部差集：对 0..0x10FFFF 逐码位比对
// unicode.IsPrint 与 CPython 3.12.3 的 chr(c).isprintable() 生成，共 90 段；反方向（CPython 可打印、
// Go 不可打印）为空。tests/run/31_isprintable 对全部码位（代理区除外）回归。
var py312Unassigned = [][2]rune{
	{0x0088F, 0x0088F}, {0x00897, 0x00897}, {0x00C5C, 0x00C5C}, {0x00CDC, 0x00CDC},
	{0x01ACF, 0x01ADD}, {0x01AE0, 0x01AEB}, {0x01B4E, 0x01B4F}, {0x01B7F, 0x01B7F},
	{0x01C89, 0x01C8A}, {0x020C1, 0x020C1}, {0x02427, 0x02429}, {0x02B96, 0x02B96},
	{0x02FFC, 0x02FFF}, {0x031E4, 0x031E5}, {0x031EF, 0x031EF}, {0x0A7CB, 0x0A7CF},
	{0x0A7D2, 0x0A7D2}, {0x0A7D4, 0x0A7D4}, {0x0A7DA, 0x0A7DC}, {0x0A7F1, 0x0A7F1},
	{0x0FBC3, 0x0FBD2}, {0x0FD90, 0x0FD91}, {0x0FDC8, 0x0FDCE}, {0x105C0, 0x105F3},
	{0x10940, 0x10959}, {0x10D40, 0x10D65}, {0x10D69, 0x10D85}, {0x10D8E, 0x10D8F},
	{0x10EC2, 0x10EC7}, {0x10ED0, 0x10ED8}, {0x10EFA, 0x10EFC}, {0x11380, 0x11389},
	{0x1138B, 0x1138B}, {0x1138E, 0x1138E}, {0x11390, 0x113B5}, {0x113B7, 0x113C0},
	{0x113C2, 0x113C2}, {0x113C5, 0x113C5}, {0x113C7, 0x113CA}, {0x113CC, 0x113D5},
	{0x113D7, 0x113D8}, {0x113E1, 0x113E2}, {0x116D0, 0x116E3}, {0x11B60, 0x11B67},
	{0x11BC0, 0x11BE1}, {0x11BF0, 0x11BF9}, {0x11DB0, 0x11DDB}, {0x11DE0, 0x11DE9},
	{0x11F5A, 0x11F5A}, {0x13460, 0x143FA}, {0x16100, 0x16139}, {0x16D40, 0x16D79},
	{0x16EA0, 0x16EB8}, {0x16EBB, 0x16ED3}, {0x16FF2, 0x16FF6}, {0x187F8, 0x187FF},
	{0x18CFF, 0x18CFF}, {0x18D09, 0x18D1E}, {0x18D80, 0x18DF2}, {0x1CC00, 0x1CCFC},
	{0x1CD00, 0x1CEB3}, {0x1CEBA, 0x1CED0}, {0x1CEE0, 0x1CEF0}, {0x1E5D0, 0x1E5FA},
	{0x1E5FF, 0x1E5FF}, {0x1E6C0, 0x1E6DE}, {0x1E6E0, 0x1E6F5}, {0x1E6FE, 0x1E6FF},
	{0x1F6D8, 0x1F6D8}, {0x1F777, 0x1F77A}, {0x1F8B2, 0x1F8BB}, {0x1F8C0, 0x1F8C1},
	{0x1F8D0, 0x1F8D8}, {0x1FA54, 0x1FA57}, {0x1FA89, 0x1FA8A}, {0x1FA8E, 0x1FA8F},
	{0x1FABE, 0x1FABE}, {0x1FAC6, 0x1FAC6}, {0x1FAC8, 0x1FAC8}, {0x1FACD, 0x1FACD},
	{0x1FADC, 0x1FADC}, {0x1FADF, 0x1FADF}, {0x1FAE9, 0x1FAEA}, {0x1FAEF, 0x1FAEF},
	{0x1FBCB, 0x1FBEF}, {0x1FBFA, 0x1FBFA}, {0x2B73A, 0x2B73F}, {0x2CEA2, 0x2CEAD},
	{0x2EBF0, 0x2EE5D}, {0x323B0, 0x33479},
}

// PyIsPrintable 是 CPython 3.12 的 Py_UNICODE_ISPRINTABLE(ch)。
func PyIsPrintable(r rune) bool {
	if !unicode.IsPrint(r) {
		return false
	}
	i := sort.Search(len(py312Unassigned), func(i int) bool { return py312Unassigned[i][1] >= r })
	return !(i < len(py312Unassigned) && py312Unassigned[i][0] <= r)
}

// StrIsPrintable 对应 Python 的 str.isprintable()：空串为真（与 isdigit 等不同）。
func StrIsPrintable(s string) bool {
	for _, r := range s {
		if !PyIsPrintable(r) {
			return false
		}
	}
	return true
}

// CharIsPrintable 对应单字符的 str.isprintable()。
func CharIsPrintable(c Char) bool { return PyIsPrintable(rune(c)) }

// PyReprStr 是 CPython 3.12 的 repr(str)（Objects/unicodeobject.c 的 unicode_repr）：
//   - 默认单引号；串里有 ' 且没有 " 时换双引号；当前引号与反斜杠前加反斜杠；
//   - \t \n \r 写成转义；其余 ASCII 控制符与 DEL 写成 \xhh；
//   - 非 ASCII：可打印（PyIsPrintable）原样保留，否则按码位大小写成 \xhh / \uhhhh / \Uhhhhhhhh（小写十六进制）。
//
// Aether 的 {x!r} 与内建 repr() 是 Aether 自己的形态（双引号），与此不同。
func PyReprStr(s string) string {
	quote := '\''
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var b strings.Builder
	b.WriteRune(quote)
	for _, r := range s {
		switch {
		case r == quote || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < ' ' || r == 0x7F:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x7F:
			b.WriteRune(r)
		case PyIsPrintable(r):
			b.WriteRune(r)
		case r <= 0xFF:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r <= 0xFFFF:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	b.WriteRune(quote)
	return b.String()
}
