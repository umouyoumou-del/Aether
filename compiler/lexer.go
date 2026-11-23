package compiler

import (
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// 生成代码中使用的命名标记字符（U+A78F），Aether 标识符中禁止出现。
const mangleMark = 'ꞏ'

type lexer struct {
	f       *SourceFile
	src     []byte
	off     int
	line    int
	col     int
	toks    []Token
	indents []string
	depth   int
	diag    *Diagnostics
	space   bool
}

// Lex 把源文件切分为记号序列。
func Lex(f *SourceFile, diag *Diagnostics) []Token {
	lx := &lexer{f: f, src: f.Src, line: 1, col: 1, diag: diag, indents: []string{""}}
	lx.run()
	return lx.toks
}

// lexFragment 对 f-字符串中的表达式片段做词法分析（不处理缩进）。
func lexFragment(f *SourceFile, src string, pos Pos, diag *Diagnostics) []Token {
	lx := &lexer{f: f, src: []byte(src), line: pos.Line, col: pos.Col, diag: diag, indents: []string{""}}
	lx.depth = 1 // 片段内忽略换行
	lx.runFragment()
	return lx.toks
}

func (lx *lexer) pos() Pos { return Pos{File: lx.f, Line: lx.line, Col: lx.col} }

func (lx *lexer) peek() rune {
	if lx.off >= len(lx.src) {
		return -1
	}
	r, _ := utf8.DecodeRune(lx.src[lx.off:])
	return r
}

func (lx *lexer) peekAt(n int) rune {
	off := lx.off
	for i := 0; i < n; i++ {
		if off >= len(lx.src) {
			return -1
		}
		_, sz := utf8.DecodeRune(lx.src[off:])
		off += sz
	}
	if off >= len(lx.src) {
		return -1
	}
	r, _ := utf8.DecodeRune(lx.src[off:])
	return r
}

func (lx *lexer) next() rune {
	if lx.off >= len(lx.src) {
		return -1
	}
	r, sz := utf8.DecodeRune(lx.src[lx.off:])
	lx.off += sz
	if r == '\n' {
		lx.line++
		lx.col = 1
	} else {
		lx.col++
	}
	return r
}

func (lx *lexer) errorf(pos Pos, format string, args ...any) {
	lx.diag.Errorf(pos, format, args...)
}

func (lx *lexer) emit(t Token) {
	t.SpaceBefore = lx.space
	lx.space = false
	if !t.End.IsValid() {
		t.End = lx.pos()
	}
	lx.toks = append(lx.toks, t)
}

func (lx *lexer) lastKind() TokKind {
	if len(lx.toks) == 0 {
		return TNewline
	}
	return lx.toks[len(lx.toks)-1].Kind
}

func (lx *lexer) run() {
	atLineStart := true
	for {
		if atLineStart && lx.depth == 0 {
			atLineStart = false
			if !lx.indentation() {
				break
			}
		}
		r := lx.peek()
		switch {
		case r == -1:
			lx.finish()
			return
		case r == ' ' || r == '\t' || r == '\f':
			lx.next()
			lx.space = true
		case r == '\r':
			lx.next()
		case r == '#':
			for lx.peek() != '\n' && lx.peek() != -1 {
				lx.next()
			}
		case r == '\\' && (lx.peekAt(1) == '\n' || (lx.peekAt(1) == '\r' && lx.peekAt(2) == '\n')):
			lx.next()
			if lx.peek() == '\r' {
				lx.next()
			}
			lx.next()
			lx.space = true
		case r == '\n':
			p := lx.pos()
			lx.next()
			if lx.depth > 0 {
				lx.space = true
				continue
			}
			if lx.dotContinuation() {
				lx.space = true
				continue
			}
			k := lx.lastKind()
			if k != TNewline && k != TIndent && k != TDedent {
				lx.emit(Token{Kind: TNewline, Pos: p, End: p})
			}
			atLineStart = true
		default:
			lx.token()
		}
	}
	lx.finish()
}

func (lx *lexer) runFragment() {
	for {
		r := lx.peek()
		switch {
		case r == -1:
			lx.emit(Token{Kind: TEOF, Pos: lx.pos()})
			return
		case r == ' ' || r == '\t' || r == '\r' || r == '\n':
			lx.next()
			lx.space = true
		default:
			lx.token()
		}
	}
}

func (lx *lexer) finish() {
	p := lx.pos()
	k := lx.lastKind()
	if len(lx.toks) > 0 && k != TNewline && k != TIndent && k != TDedent {
		lx.emit(Token{Kind: TNewline, Pos: p, End: p})
	}
	for len(lx.indents) > 1 {
		lx.indents = lx.indents[:len(lx.indents)-1]
		lx.emit(Token{Kind: TDedent, Pos: p, End: p})
	}
	lx.emit(Token{Kind: TEOF, Pos: p, End: p})
}

// indentation 处理一行开头的缩进。返回 false 表示到达文件末尾。
func (lx *lexer) indentation() bool {
	for {
		start := lx.off
		startPos := lx.pos()
		for lx.peek() == ' ' || lx.peek() == '\t' || lx.peek() == '\f' {
			lx.next()
		}
		ind := string(lx.src[start:lx.off])
		r := lx.peek()
		if r == -1 {
			return false
		}
		if r == '\n' || r == '\r' || r == '#' {
			// 空行或注释行
			for lx.peek() != '\n' && lx.peek() != -1 {
				lx.next()
			}
			if lx.peek() == '\n' {
				lx.next()
				continue
			}
			return false
		}
		if r == '\\' && (lx.peekAt(1) == '\n' || lx.peekAt(1) == '\r') {
			// 只有续行符的行，当空行处理
			lx.next()
			for lx.peek() != '\n' && lx.peek() != -1 {
				lx.next()
			}
			if lx.peek() == '\n' {
				lx.next()
			}
			continue
		}
		top := lx.indents[len(lx.indents)-1]
		p := Pos{File: lx.f, Line: startPos.Line, Col: lx.col}
		switch {
		case ind == top:
		case len(ind) > len(top) && strings.HasPrefix(ind, top):
			lx.indents = append(lx.indents, ind)
			lx.emit(Token{Kind: TIndent, Pos: p, End: p})
		case len(ind) < len(top) && strings.HasPrefix(top, ind):
			for len(lx.indents) > 1 && len(lx.indents[len(lx.indents)-1]) > len(ind) {
				lx.indents = lx.indents[:len(lx.indents)-1]
				lx.emit(Token{Kind: TDedent, Pos: p, End: p})
			}
			if lx.indents[len(lx.indents)-1] != ind {
				lx.errorf(p, "缩进与外层任何一级都不对齐")
				lx.indents = append(lx.indents, ind)
			}
		default:
			if strings.ContainsRune(ind, '\t') != strings.ContainsRune(top, '\t') {
				lx.errorf(p, "缩进混用了 Tab 和空格")
			} else {
				lx.errorf(p, "缩进不一致")
			}
			// 尽量恢复：把当前缩进替换为新值
			lx.indents[len(lx.indents)-1] = ind
		}
		return true
	}
}

// dotContinuation 检查下一个非空行是否以 "." 或 "?." 开头（链式调用续行）。
// 若是，则跳过换行与缩进，返回 true。
func (lx *lexer) dotContinuation() bool {
	off := lx.off
	line, col := lx.line, lx.col
	for off < len(lx.src) {
		c := lx.src[off]
		switch c {
		case ' ', '\t', '\r', '\f':
			off++
			col++
			continue
		case '\n':
			off++
			line++
			col = 1
			continue
		case '#':
			for off < len(lx.src) && lx.src[off] != '\n' {
				off++
			}
			continue
		}
		break
	}
	if off >= len(lx.src) {
		return false
	}
	isDot := lx.src[off] == '.' && off+1 < len(lx.src) && !isDigitByte(lx.src[off+1]) && lx.src[off+1] != '.'
	isQDot := lx.src[off] == '?' && off+1 < len(lx.src) && lx.src[off+1] == '.'
	if !isDot && !isQDot {
		return false
	}
	k := lx.lastKind()
	if k == TNewline || k == TIndent || k == TDedent || k == TColon {
		return false
	}
	// 重新计算列号（按字符）
	lineStart := off
	for lineStart > 0 && lx.src[lineStart-1] != '\n' {
		lineStart--
	}
	lx.off = off
	lx.line = line
	lx.col = utf8.RuneCount(lx.src[lineStart:off]) + 1
	_ = col
	return true
}

func isDigitByte(b byte) bool { return b >= '0' && b <= '9' }

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func (lx *lexer) token() {
	p := lx.pos()
	r := lx.peek()
	switch {
	case isIdentStart(r):
		start := lx.off
		for isIdentPart(lx.peek()) {
			lx.next()
		}
		word := string(lx.src[start:lx.off])
		// 字符串前缀
		if q := lx.peek(); q == '"' {
			lw := strings.ToLower(word)
			if lw == "r" || lw == "f" || lw == "rf" || lw == "fr" {
				lx.str(p, strings.Contains(lw, "r"), strings.Contains(lw, "f"))
				return
			}
		}
		if strings.ContainsRune(word, mangleMark) {
			lx.errorf(p, "标识符中不能包含字符 'ꞏ'")
		}
		if k, ok := keywords[word]; ok {
			lx.emit(Token{Kind: k, Text: word, Pos: p})
			return
		}
		lx.emit(Token{Kind: TIdent, Text: word, Pos: p})
	case r >= '0' && r <= '9':
		lx.number(p)
	case r == '"':
		lx.str(p, false, false)
	case r == '\'':
		lx.char(p)
	default:
		lx.op(p)
	}
}

func (lx *lexer) number(p Pos) {
	start := lx.off
	isFloat := false
	base := 10
	if lx.peek() == '0' && (lx.peekAt(1) == 'x' || lx.peekAt(1) == 'X' || lx.peekAt(1) == 'b' || lx.peekAt(1) == 'B' || lx.peekAt(1) == 'o' || lx.peekAt(1) == 'O') {
		lx.next()
		switch lx.next() {
		case 'x', 'X':
			base = 16
		case 'b', 'B':
			base = 2
		default:
			base = 8
		}
		for {
			c := lx.peek()
			if c == '_' || (c >= '0' && c <= '9') || (base == 16 && ((c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'))) {
				lx.next()
				continue
			}
			break
		}
	} else {
		lx.digits()
		if lx.peek() == '.' && lx.peekAt(1) >= '0' && lx.peekAt(1) <= '9' {
			isFloat = true
			lx.next()
			lx.digits()
		}
		if c := lx.peek(); c == 'e' || c == 'E' {
			c1 := lx.peekAt(1)
			if (c1 >= '0' && c1 <= '9') || ((c1 == '+' || c1 == '-') && lx.peekAt(2) >= '0' && lx.peekAt(2) <= '9') {
				isFloat = true
				lx.next()
				if lx.peek() == '+' || lx.peek() == '-' {
					lx.next()
				}
				lx.digits()
			}
		}
	}
	text := string(lx.src[start:lx.off])
	clean := strings.ReplaceAll(text, "_", "")
	if isIdentStart(lx.peek()) {
		lx.errorf(lx.pos(), "数字后面紧跟了非法字符 '%c'", lx.peek())
	}
	if isFloat {
		v, err := strconv.ParseFloat(clean, 64)
		if err != nil && !math.IsInf(v, 0) {
			lx.errorf(p, "无效的浮点数字面量 %s", text)
		}
		lx.emit(Token{Kind: TFloat, Text: text, Pos: p, FltVal: v})
		return
	}
	digits := clean
	if base != 10 {
		digits = clean[2:]
	}
	if digits == "" {
		lx.errorf(p, "无效的整数字面量 %s", text)
		lx.emit(Token{Kind: TInt, Text: text, Pos: p})
		return
	}
	v, err := strconv.ParseUint(digits, base, 64)
	if err != nil {
		lx.errorf(p, "整数字面量 %s 超出范围", text)
	}
	lx.emit(Token{Kind: TInt, Text: text, Pos: p, IntVal: v})
}

func (lx *lexer) digits() {
	for {
		c := lx.peek()
		if (c >= '0' && c <= '9') || (c == '_' && lx.peekAt(1) >= '0' && lx.peekAt(1) <= '9') {
			lx.next()
			continue
		}
		break
	}
}

// escape 解析一个反斜杠转义（反斜杠已被消费）。
func (lx *lexer) escape(sb *strings.Builder, quote rune) {
	p := lx.pos()
	c := lx.next()
	switch c {
	case 'n':
		sb.WriteByte('\n')
	case 't':
		sb.WriteByte('\t')
	case 'r':
		sb.WriteByte('\r')
	case '0':
		sb.WriteByte(0)
	case 'a':
		sb.WriteByte(7)
	case 'b':
		sb.WriteByte(8)
	case 'f':
		sb.WriteByte(12)
	case 'v':
		sb.WriteByte(11)
	case 'e':
		sb.WriteByte(27)
	case '\\':
		sb.WriteByte('\\')
	case '"':
		sb.WriteByte('"')
	case '\'':
		sb.WriteByte('\'')
	case '{':
		sb.WriteByte('{')
	case '}':
		sb.WriteByte('}')
	case '\n':
		// 续行
	case 'x':
		v := lx.hexDigits(2, p)
		sb.WriteRune(rune(v))
	case 'u':
		if lx.peek() == '{' {
			lx.next()
			start := lx.off
			for lx.peek() != '}' && lx.peek() != -1 && lx.peek() != '\n' {
				lx.next()
			}
			h := string(lx.src[start:lx.off])
			if lx.peek() == '}' {
				lx.next()
			}
			v, err := strconv.ParseUint(h, 16, 32)
			if err != nil || v > unicode.MaxRune {
				lx.errorf(p, "无效的 Unicode 转义 \\u{%s}", h)
			}
			sb.WriteRune(rune(v))
		} else {
			sb.WriteRune(rune(lx.hexDigits(4, p)))
		}
	case 'U':
		v := lx.hexDigits(8, p)
		if v > unicode.MaxRune {
			lx.errorf(p, "无效的 Unicode 转义")
		}
		sb.WriteRune(rune(v))
	default:
		lx.errorf(p, "未知的转义序列 \\%c", c)
		sb.WriteRune(c)
	}
}

func (lx *lexer) hexDigits(n int, p Pos) uint64 {
	var v uint64
	for i := 0; i < n; i++ {
		c := lx.peek()
		var d uint64
		switch {
		case c >= '0' && c <= '9':
			d = uint64(c - '0')
		case c >= 'a' && c <= 'f':
			d = uint64(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = uint64(c-'A') + 10
		default:
			lx.errorf(p, "转义序列需要 %d 位十六进制数字", n)
			return v
		}
		lx.next()
		v = v*16 + d
	}
	return v
}

func (lx *lexer) str(p Pos, raw, isF bool) {
	triple := lx.peekAt(1) == '"' && lx.peekAt(2) == '"'
	if triple {
		lx.next()
		lx.next()
		lx.next()
	} else {
		lx.next()
	}
	var parts []*FPart
	var sb strings.Builder
	flushLit := func() {
		if sb.Len() > 0 {
			parts = append(parts, &FPart{Lit: sb.String()})
			sb.Reset()
		}
	}
	for {
		c := lx.peek()
		if c == -1 {
			lx.errorf(p, "字符串没有结束")
			break
		}
		if c == '\n' && !triple {
			lx.errorf(p, "字符串没有结束（单行字符串中不能直接换行，可使用 \\n 或三引号）")
			break
		}
		if c == '"' {
			if !triple {
				lx.next()
				break
			}
			if lx.peekAt(1) == '"' && lx.peekAt(2) == '"' {
				lx.next()
				lx.next()
				lx.next()
				// 允许 """...""""" 末尾多余引号属于内容
				for lx.peek() == '"' {
					sb.WriteByte('"')
					lx.next()
				}
				break
			}
			lx.next()
			sb.WriteByte('"')
			continue
		}
		if c == '\\' {
			if raw {
				lx.next()
				sb.WriteByte('\\')
				if n := lx.peek(); n == '"' || n == '\\' {
					sb.WriteRune(lx.next())
				}
				continue
			}
			lx.next()
			lx.escape(&sb, '"')
			continue
		}
		if isF && c == '{' {
			if lx.peekAt(1) == '{' {
				lx.next()
				lx.next()
				sb.WriteByte('{')
				continue
			}
			flushLit()
			part := lx.fexpr(p)
			if part != nil {
				parts = append(parts, part)
			}
			continue
		}
		if isF && c == '}' {
			if lx.peekAt(1) == '}' {
				lx.next()
				lx.next()
				sb.WriteByte('}')
				continue
			}
			lx.errorf(lx.pos(), "f-字符串中单独的 '}' 需要写成 '}}'")
			lx.next()
			continue
		}
		if c == '\r' {
			lx.next()
			continue
		}
		sb.WriteRune(lx.next())
	}
	if isF {
		flushLit()
		lx.emit(Token{Kind: TFString, Pos: p, Parts: parts})
		return
	}
	lx.emit(Token{Kind: TString, Text: sb.String(), Pos: p})
}

// fexpr 解析 f-字符串中的 {表达式[=][!r][:格式]} 部分（当前位于 '{'）。
func (lx *lexer) fexpr(strPos Pos) *FPart {
	open := lx.pos()
	lx.next() // {
	exprPos := lx.pos()
	start := lx.off
	depth := 0
	end := -1
	var quote rune
	for {
		c := lx.peek()
		if c == -1 {
			lx.errorf(open, "f-字符串中的 '{' 没有匹配的 '}'")
			return nil
		}
		if quote != 0 {
			if c == '\\' {
				lx.next()
				lx.next()
				continue
			}
			if c == quote {
				quote = 0
			}
			lx.next()
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			lx.next()
			continue
		}
		if c == '(' || c == '[' || c == '{' {
			depth++
		} else if c == ')' || c == ']' || (c == '}' && depth > 0) {
			depth--
		} else if depth == 0 && (c == '}' || c == ':' || (c == '!' && lx.peekAt(1) != '=')) {
			end = lx.off
			break
		}
		lx.next()
	}
	src := string(lx.src[start:end])
	part := &FPart{IsExpr: true, SrcPos: exprPos}
	trimmed := strings.TrimRight(src, " \t")
	if strings.HasSuffix(trimmed, "=") && !strings.HasSuffix(trimmed, "==") && !strings.HasSuffix(trimmed, "!=") &&
		!strings.HasSuffix(trimmed, "<=") && !strings.HasSuffix(trimmed, ">=") {
		part.Debug = true
		part.Lit = src // 包含 '=' 的原文，用于输出
		src = strings.TrimSuffix(trimmed, "=")
	}
	part.Src = src
	if strings.TrimSpace(src) == "" {
		lx.errorf(open, "f-字符串中的 {} 不能为空")
	}
	if lx.peek() == '!' {
		lx.next()
		c := lx.next()
		if c != 'r' && c != 's' {
			lx.errorf(lx.pos(), "f-字符串转换只能是 !r 或 !s")
		}
		part.Conv = byte(c)
	}
	if lx.peek() == ':' {
		lx.next()
		ss := lx.off
		for lx.peek() != '}' && lx.peek() != -1 && lx.peek() != '\n' {
			lx.next()
		}
		part.Spec = string(lx.src[ss:lx.off])
	}
	if lx.peek() != '}' {
		lx.errorf(open, "f-字符串中的 '{' 没有匹配的 '}'")
		return part
	}
	lx.next()
	return part
}

func (lx *lexer) char(p Pos) {
	lx.next() // '
	var sb strings.Builder
	for {
		c := lx.peek()
		if c == -1 || c == '\n' {
			lx.errorf(p, "字符字面量没有结束")
			break
		}
		if c == '\'' {
			lx.next()
			break
		}
		if c == '\\' {
			lx.next()
			lx.escape(&sb, '\'')
			continue
		}
		sb.WriteRune(lx.next())
	}
	s := sb.String()
	n := utf8.RuneCountInString(s)
	var r rune
	if n != 1 {
		if n > 1 {
			lx.errorf(p, "字符字面量只能包含一个字符；字符串请使用双引号 \"...\"")
		} else {
			lx.errorf(p, "空的字符字面量")
		}
	} else {
		r, _ = utf8.DecodeRuneInString(s)
	}
	lx.emit(Token{Kind: TChar, Pos: p, Char: r, Text: s})
}

func (lx *lexer) op(p Pos) {
	c := lx.next()
	k := TEOF
	two := func(next rune, yes, no TokKind) TokKind {
		if lx.peek() == next {
			lx.next()
			return yes
		}
		return no
	}
	switch c {
	case '+':
		k = two('=', TPlusEq, TPlus)
	case '-':
		if lx.peek() == '>' {
			lx.next()
			k = TArrow
		} else {
			k = two('=', TMinusEq, TMinus)
		}
	case '*':
		if lx.peek() == '*' {
			lx.next()
			k = two('=', TDStarEq, TDStar)
		} else {
			k = two('=', TStarEq, TStar)
		}
	case '/':
		if lx.peek() == '/' {
			lx.next()
			k = two('=', TDSlashEq, TDSlash)
		} else {
			k = two('=', TSlashEq, TSlash)
		}
	case '%':
		k = two('=', TPercentEq, TPercent)
	case '=':
		if lx.peek() == '>' {
			lx.next()
			k = TFatArrow
		} else {
			k = two('=', TEq, TAssign)
		}
	case '!':
		k = two('=', TNe, TBang)
	case '<':
		if lx.peek() == '<' {
			lx.next()
			k = two('=', TShlEq, TShl)
		} else {
			k = two('=', TLe, TLt)
		}
	case '>':
		if lx.peek() == '>' {
			lx.next()
			k = two('=', TShrEq, TShr)
		} else {
			k = two('=', TGe, TGt)
		}
	case '&':
		if lx.peek() == '&' {
			lx.next()
			k = TAndAnd
		} else {
			k = two('=', TAmpEq, TAmp)
		}
	case '|':
		if lx.peek() == '|' {
			lx.next()
			k = TOrOr
		} else {
			k = two('=', TPipeEq, TPipe)
		}
	case '^':
		k = two('=', TCaretEq, TCaret)
	case '~':
		k = TTilde
	case '(':
		lx.depth++
		k = TLParen
	case ')':
		if lx.depth > 0 {
			lx.depth--
		}
		k = TRParen
	case '[':
		lx.depth++
		k = TLBrack
	case ']':
		if lx.depth > 0 {
			lx.depth--
		}
		k = TRBrack
	case '{':
		lx.depth++
		k = TLBrace
	case '}':
		if lx.depth > 0 {
			lx.depth--
		}
		k = TRBrace
	case ',':
		k = TComma
	case ':':
		k = two('=', TWalrus, TColon)
	case ';':
		k = TSemi
	case '@':
		k = TAt
	case '.':
		k = TDot
	case '?':
		if lx.peek() == '.' {
			lx.next()
			k = TQDot
		} else {
			k = two('?', TQQ, TQuestion)
		}
	default:
		if c == '：' || c == '，' || c == '（' || c == '）' || c == '“' || c == '”' || c == '；' {
			lx.errorf(p, "使用了全角符号 '%c'，请改用半角符号", c)
		} else {
			lx.errorf(p, "非法字符 '%c' (U+%04X)", c, c)
		}
		return
	}
	lx.emit(Token{Kind: k, Pos: p})
}
