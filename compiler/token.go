// Package compiler 实现 Aether 语言的编译器：词法、语法、类型检查与 Go 代码生成。
package compiler

import "fmt"

// SourceFile 是一个已加载的源文件。
type SourceFile struct {
	Path  string // 绝对路径
	Name  string // 用于显示的路径
	Src   []byte
	lines []int // 每行起始偏移
}

func NewSourceFile(path, name string, src []byte) *SourceFile {
	// 去掉 UTF-8 BOM
	if len(src) >= 3 && src[0] == 0xEF && src[1] == 0xBB && src[2] == 0xBF {
		src = src[3:]
	}
	f := &SourceFile{Path: path, Name: name, Src: src}
	f.lines = append(f.lines, 0)
	for i, b := range src {
		if b == '\n' {
			f.lines = append(f.lines, i+1)
		}
	}
	return f
}

// Line 返回第 n 行（从 1 开始）的文本，不含换行符。
func (f *SourceFile) Line(n int) string {
	if f == nil || n < 1 || n > len(f.lines) {
		return ""
	}
	start := f.lines[n-1]
	end := len(f.Src)
	if n < len(f.lines) {
		end = f.lines[n] - 1
	}
	if end < start {
		end = start
	}
	s := string(f.Src[start:end])
	if len(s) > 0 && s[len(s)-1] == '\r' {
		s = s[:len(s)-1]
	}
	return s
}

// Pos 是源码位置。Col 以字符（code point）计，从 1 开始。
type Pos struct {
	File *SourceFile
	Line int
	Col  int
}

func (p Pos) String() string {
	name := "?"
	if p.File != nil {
		name = p.File.Name
	}
	return fmt.Sprintf("%s:%d:%d", name, p.Line, p.Col)
}

func (p Pos) IsValid() bool { return p.Line > 0 }

type TokKind int

const (
	TEOF TokKind = iota
	TNewline
	TIndent
	TDedent
	TIdent
	TInt
	TFloat
	TString
	TFString
	TChar

	// 运算符与分隔符
	TPlus      // +
	TMinus     // -
	TStar      // *
	TSlash     // /
	TDSlash    // //
	TPercent   // %
	TDStar     // **
	TAssign    // =
	TPlusEq    // +=
	TMinusEq   // -=
	TStarEq    // *=
	TSlashEq   // /=
	TDSlashEq  // //=
	TPercentEq // %=
	TDStarEq   // **=
	TAmpEq     // &=
	TPipeEq    // |=
	TCaretEq   // ^=
	TShlEq     // <<=
	TShrEq     // >>=
	TEq        // ==
	TNe        // !=
	TLt        // <
	TGt        // >
	TLe        // <=
	TGe        // >=
	TAmp       // &
	TPipe      // |
	TCaret     // ^
	TTilde     // ~
	TShl       // <<
	TShr       // >>
	TLParen    // (
	TRParen    // )
	TLBrack    // [
	TRBrack    // ]
	TLBrace    // {
	TRBrace    // }
	TComma     // ,
	TColon     // :
	TWalrus    // :=
	TDot       // .
	TSemi      // ;
	TArrow     // ->
	TFatArrow  // =>
	TQuestion  // ?
	TQDot      // ?.
	TQQ        // ??
	TBang      // !
	TAndAnd    // &&
	TOrOr      // ||
	TAt        // @

	// 关键字
	kwBegin
	KAnd
	KAs
	KAssert
	KAsync
	KAwait
	KBreak
	KClass
	KConst
	KContinue
	KDef
	KDefer
	KDel
	KElif
	KElse
	KEnum
	KExcept
	KExtend
	KFalse
	KFinally
	KFor
	KFrom
	KGo
	KIf
	KImport
	KIn
	KInterface
	KIs
	KNot
	KNull
	KOr
	KPass
	KRaise
	KReturn
	KStruct
	KTrue
	KTry
	KWhile
	KWith
	KYield
	kwEnd

	// 仅供 AST 使用的合成运算符
	KNotIn
	KIsNot
)

var keywords = map[string]TokKind{
	"and": KAnd, "as": KAs, "assert": KAssert, "async": KAsync, "await": KAwait,
	"break": KBreak, "class": KClass, "const": KConst, "continue": KContinue,
	"def": KDef, "defer": KDefer, "del": KDel, "elif": KElif, "else": KElse,
	"enum": KEnum, "except": KExcept, "extend": KExtend, "false": KFalse,
	"finally": KFinally, "for": KFor, "from": KFrom, "go": KGo, "if": KIf,
	"import": KImport, "in": KIn, "interface": KInterface, "is": KIs, "not": KNot,
	"null": KNull, "or": KOr, "pass": KPass, "raise": KRaise, "return": KReturn,
	"struct": KStruct, "true": KTrue, "try": KTry, "while": KWhile, "with": KWith,
	"yield": KYield,
}

var tokNames = map[TokKind]string{
	TEOF: "文件结尾", TNewline: "换行", TIndent: "缩进", TDedent: "取消缩进",
	TIdent: "标识符", TInt: "整数", TFloat: "浮点数", TString: "字符串", TFString: "f-字符串", TChar: "字符",
	TPlus: "+", TMinus: "-", TStar: "*", TSlash: "/", TDSlash: "//", TPercent: "%", TDStar: "**",
	TAssign: "=", TPlusEq: "+=", TMinusEq: "-=", TStarEq: "*=", TSlashEq: "/=", TDSlashEq: "//=",
	TPercentEq: "%=", TDStarEq: "**=", TAmpEq: "&=", TPipeEq: "|=", TCaretEq: "^=", TShlEq: "<<=", TShrEq: ">>=",
	TEq: "==", TNe: "!=", TLt: "<", TGt: ">", TLe: "<=", TGe: ">=",
	TAmp: "&", TPipe: "|", TCaret: "^", TTilde: "~", TShl: "<<", TShr: ">>",
	TLParen: "(", TRParen: ")", TLBrack: "[", TRBrack: "]", TLBrace: "{", TRBrace: "}",
	TComma: ",", TColon: ":", TWalrus: ":=", TDot: ".", TSemi: ";", TArrow: "->", TFatArrow: "=>",
	TQuestion: "?", TQDot: "?.", TQQ: "??", TBang: "!", TAndAnd: "&&", TOrOr: "||", TAt: "@",
	KNotIn: "not in", KIsNot: "is not",
}

func (k TokKind) String() string {
	if s, ok := tokNames[k]; ok {
		return s
	}
	if k > kwBegin && k < kwEnd {
		for name, kk := range keywords {
			if kk == k {
				return name
			}
		}
	}
	return fmt.Sprintf("tok(%d)", int(k))
}

func (k TokKind) IsKeyword() bool { return k > kwBegin && k < kwEnd }

// FPart 是 f-字符串的一段：要么是字面文本，要么是 {表达式[!r][:格式]}。
type FPart struct {
	Lit     string
	IsExpr  bool
	Src     string // 表达式源码
	SrcPos  Pos    // 表达式起始位置
	Spec    string // 格式说明（冒号之后）
	Conv    byte   // 'r' 表示 repr，'s' 表示 str，0 表示默认
	Debug   bool   // {x=} 形式
	X       Expr   // 由语法分析器填充
}

type Token struct {
	Kind   TokKind
	Text   string // 标识符名、数字原文、解码后的字符串
	Pos    Pos
	End    Pos
	IntVal uint64
	FltVal float64
	Char   rune
	Parts  []*FPart
	// SpaceBefore 表示该记号前面有空白（用于区分 x! 与 x !）
	SpaceBefore bool
}

func (t Token) String() string {
	switch t.Kind {
	case TIdent:
		return "标识符 '" + t.Text + "'"
	case TInt, TFloat:
		return "数字 " + t.Text
	case TString:
		return "字符串"
	}
	if t.Kind.IsKeyword() {
		return "关键字 '" + t.Kind.String() + "'"
	}
	return "'" + t.Kind.String() + "'"
}
