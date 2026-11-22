package compiler

import (
	"strings"
)

type parser struct {
	f    *SourceFile
	toks []Token
	i    int
	diag *Diagnostics
	// 不允许解析三元表达式（推导式的 in 子句中）
	noTernary int
	inClass   int
	noAs      int
}

type parseBail struct{}

// Parse 解析一个源文件。
func Parse(f *SourceFile, diag *Diagnostics) *File {
	toks := Lex(f, diag)
	p := &parser{f: f, toks: toks, diag: diag}
	file := &File{Src: f}
	for !p.at(TEOF) {
		if p.at(TNewline) || p.at(TSemi) {
			p.advance()
			continue
		}
		if p.at(TIndent) {
			p.errorf(p.tok().Pos, "意外的缩进")
			p.advance()
			continue
		}
		if p.at(TDedent) {
			p.advance()
			continue
		}
		file.Stmts = append(file.Stmts, p.stmtSafe(true)...)
	}
	return file
}

func (p *parser) tok() *Token { return &p.toks[p.i] }

func (p *parser) peek(n int) *Token {
	if p.i+n < len(p.toks) {
		return &p.toks[p.i+n]
	}
	return &p.toks[len(p.toks)-1]
}

func (p *parser) at(k TokKind) bool { return p.toks[p.i].Kind == k }

func (p *parser) atSoft(name string) bool {
	t := p.tok()
	return t.Kind == TIdent && t.Text == name
}

func (p *parser) advance() Token {
	t := p.toks[p.i]
	if p.i < len(p.toks)-1 {
		p.i++
	}
	return t
}

func (p *parser) got(k TokKind) bool {
	if p.at(k) {
		p.advance()
		return true
	}
	return false
}

func (p *parser) errorf(pos Pos, format string, args ...any) {
	p.diag.Errorf(pos, format, args...)
}

func (p *parser) fail(format string, args ...any) {
	p.errorf(p.tok().Pos, format, args...)
	panic(parseBail{})
}

func (p *parser) expect(k TokKind, ctx string) Token {
	if !p.at(k) {
		t := p.tok()
		what := describeTok(t)
		if ctx != "" {
			p.fail("%s需要 '%s'，但遇到了 %s", ctx, k, what)
		}
		p.fail("这里需要 '%s'，但遇到了 %s", k, what)
	}
	return p.advance()
}

func describeTok(t *Token) string {
	switch t.Kind {
	case TNewline:
		return "行尾"
	case TIndent:
		return "缩进"
	case TDedent:
		return "代码块结束"
	case TEOF:
		return "文件结尾"
	}
	return t.String()
}

func (p *parser) expectIdent(ctx string) Token {
	if !p.at(TIdent) {
		t := p.tok()
		if t.Kind.IsKeyword() {
			p.fail("%s需要一个名字，但 '%s' 是关键字", ctx, t.Kind)
		}
		p.fail("%s需要一个名字，但遇到了 %s", ctx, describeTok(t))
	}
	return p.advance()
}

// sync 在出错后跳到下一条语句开头。
func (p *parser) sync() {
	depth := 0
	for {
		switch p.tok().Kind {
		case TEOF:
			return
		case TIndent:
			depth++
		case TDedent:
			if depth == 0 {
				return
			}
			depth--
			if depth == 0 {
				p.advance()
				return
			}
		case TNewline:
			if depth == 0 {
				p.advance()
				return
			}
		}
		p.advance()
	}
}

func (p *parser) stmtSafe(top bool) (out []Stmt) {
	start := p.i
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(parseBail); !ok {
				panic(r)
			}
			if p.i == start {
				p.advance()
			}
			p.sync()
			out = nil
		}
	}()
	return p.stmt(top)
}

// block 解析冒号之后的代码块。
func (p *parser) block(ctx string) []Stmt {
	p.expect(TColon, ctx)
	if p.at(TNewline) {
		p.advance()
		if !p.at(TIndent) {
			p.fail("%s后面需要一个缩进的代码块", ctx)
		}
		p.advance()
		var stmts []Stmt
		for !p.at(TDedent) && !p.at(TEOF) {
			if p.at(TNewline) || p.at(TSemi) {
				p.advance()
				continue
			}
			stmts = append(stmts, p.stmtSafe(false)...)
		}
		p.got(TDedent)
		return stmts
	}
	return p.simpleStmts()
}

func (p *parser) simpleStmts() []Stmt {
	var out []Stmt
	for {
		out = append(out, p.simpleStmt())
		if p.got(TSemi) {
			if p.at(TNewline) || p.at(TEOF) {
				break
			}
			continue
		}
		break
	}
	if !p.at(TEOF) && !p.at(TDedent) {
		if !p.at(TNewline) {
			p.fail("语句后面出现了多余的 %s", describeTok(p.tok()))
		}
		p.advance()
	}
	return out
}

func (p *parser) isMatchStmt() bool {
	// match <expr> : 换行
	if !p.atSoft("match") {
		return false
	}
	k := p.peek(1).Kind
	switch k {
	case TAssign, TDot, TComma, TRParen, TNewline, TColon, TPlusEq, TMinusEq, TEq, TNe, TLBrack, TQDot:
		return false
	}
	depth := 0
	for j := p.i + 1; j < len(p.toks); j++ {
		switch p.toks[j].Kind {
		case TLParen, TLBrack, TLBrace:
			depth++
		case TRParen, TRBrack, TRBrace:
			depth--
		case TColon:
			if depth == 0 {
				return p.toks[j+1].Kind == TNewline
			}
		case TNewline, TEOF:
			return false
		}
	}
	return false
}

func (p *parser) stmt(top bool) []Stmt {
	t := p.tok()
	switch t.Kind {
	case TAt:
		return []Stmt{p.decorated(top)}
	case KDef:
		fd := p.funcDecl(nil, false)
		if !top {
			fd.Nested = true
			return []Stmt{&FuncDeclStmt{Pos: fd.Pos, Fn: fd}}
		}
		return []Stmt{fd}
	case KAsync:
		p.advance()
		if !p.at(KDef) {
			p.fail("async 后面需要 def")
		}
		fd := p.funcDecl(nil, true)
		if !top {
			fd.Nested = true
			return []Stmt{&FuncDeclStmt{Pos: fd.Pos, Fn: fd}}
		}
		return []Stmt{fd}
	case KClass, KStruct, KInterface:
		if !top {
			p.fail("类型声明只能出现在模块顶层")
		}
		return []Stmt{p.classDecl(nil, false)}
	case KEnum:
		if !top {
			p.fail("枚举声明只能出现在模块顶层")
		}
		return []Stmt{p.enumDecl(nil)}
	case KExtend:
		if !top {
			p.fail("extend 只能出现在模块顶层")
		}
		return []Stmt{p.extendDecl()}
	case KImport, KFrom:
		if !top {
			p.fail("import 只能出现在模块顶层")
		}
		s := p.importStmt()
		p.endLine()
		return []Stmt{s}
	case KIf:
		return []Stmt{p.ifStmt()}
	case KWhile:
		pos := p.advance().Pos
		cond := p.expr()
		body := p.block("while 语句")
		if p.at(KElse) {
			p.fail("五行不支持 while...else")
		}
		return []Stmt{&WhileStmt{Pos: pos, Cond: cond, Body: body}}
	case KFor:
		return []Stmt{p.forStmt()}
	case KTry:
		return []Stmt{p.tryStmt()}
	case KWith:
		return []Stmt{p.withStmt()}
	case TIdent:
		switch t.Text {
		case "match":
			if p.isMatchStmt() {
				return []Stmt{p.matchStmt()}
			}
		case "select":
			if p.peek(1).Kind == TColon {
				return []Stmt{p.selectStmt()}
			}
		case "test":
			if p.peek(1).Kind == TString && top {
				return []Stmt{p.testDecl()}
			}
		case "global", "nonlocal":
			if p.peek(1).Kind == TIdent {
				pos := p.advance().Pos
				gs := &GlobalStmt{Pos: pos, Nonlocal: t.Text == "nonlocal"}
				for {
					gs.Names = append(gs.Names, p.expectIdent("global 声明").Text)
					if !p.got(TComma) {
						break
					}
				}
				p.endLine()
				return []Stmt{gs}
			}
		case "abstract":
			if p.peek(1).Kind == KClass {
				if !top {
					p.fail("类型声明只能出现在模块顶层")
				}
				p.advance()
				return []Stmt{p.classDecl(nil, true)}
			}
		}
	}
	return p.simpleStmts()
}

func (p *parser) endLine() {
	if p.at(TEOF) || p.at(TDedent) {
		return
	}
	p.got(TSemi)
	if !p.at(TNewline) {
		p.fail("语句后面出现了多余的 %s", describeTok(p.tok()))
	}
	p.advance()
}

func (p *parser) decorators() []*Decorator {
	var decs []*Decorator
	for p.at(TAt) {
		pos := p.advance().Pos
		var name string
		if p.at(KGo) {
			p.advance()
			name = "go"
		} else {
			name = p.expectIdent("装饰器").Text
			for p.got(TDot) {
				name += "." + p.expectIdent("装饰器").Text
			}
		}
		d := &Decorator{Pos: pos, Name: name}
		if p.got(TLParen) {
			d.Args = p.callArgs()
		}
		decs = append(decs, d)
		if !p.got(TNewline) {
			// 允许 @go("x") def f() 写在同一行
			if !p.at(KDef) && !p.at(KClass) && !p.at(KAsync) && !p.at(TAt) && !p.atSoft("static") && !p.atSoft("prop") && !p.at(KConst) {
				p.fail("装饰器后面需要换行")
			}
		}
	}
	return decs
}

func (p *parser) decorated(top bool) Stmt {
	decs := p.decorators()
	switch {
	case p.at(KDef):
		fd := p.funcDecl(decs, false)
		if !top {
			fd.Nested = true
			return &FuncDeclStmt{Pos: fd.Pos, Fn: fd}
		}
		return fd
	case p.at(KAsync):
		p.advance()
		fd := p.funcDecl(decs, true)
		if !top {
			fd.Nested = true
			return &FuncDeclStmt{Pos: fd.Pos, Fn: fd}
		}
		return fd
	case p.at(KClass) || p.at(KStruct) || p.at(KInterface):
		return p.classDecl(decs, false)
	case p.atSoft("abstract") && p.peek(1).Kind == KClass:
		p.advance()
		return p.classDecl(decs, true)
	case p.at(KConst):
		s := p.constStmt()
		if cd, ok := s.(*ConstDecl); ok {
			for _, d := range decs {
				if d.Name == "go" && len(d.Args) == 1 {
					if sl, ok := d.Args[0].X.(*StrLit); ok {
						cd.GoName = sl.Val
					}
				}
			}
		}
		p.endLine()
		return s
	}
	p.fail("装饰器后面需要 def 或 class")
	return nil
}

func (p *parser) typeParams() []*TypeParamX {
	var tps []*TypeParamX
	p.expect(TLBrack, "类型参数")
	for !p.at(TRBrack) {
		nt := p.expectIdent("类型参数")
		tp := &TypeParamX{Pos: nt.Pos, Name: nt.Text}
		if p.got(TColon) {
			tp.Constraint = p.typeExpr()
		}
		tps = append(tps, tp)
		if !p.got(TComma) {
			break
		}
	}
	p.expect(TRBrack, "类型参数列表")
	return tps
}

func (p *parser) funcDecl(decs []*Decorator, async bool) *FuncDecl {
	pos := p.expect(KDef, "").Pos
	var nt Token
	if p.inClass > 0 && p.tok().Kind.IsKeyword() {
		// 方法名可以是关键字，例如 list.extend
		nt = p.advance()
		nt.Text = nt.Kind.String()
	} else {
		nt = p.expectIdent("函数")
	}
	fd := &FuncDecl{Pos: pos, Name: nt.Text, NamePos: nt.Pos, Decorators: decs, Async: async}
	if p.at(TLBrack) {
		fd.TypeParams = p.typeParams()
	}
	p.expect(TLParen, "函数参数列表")
	fd.Params = p.params(false)
	p.expect(TRParen, "函数参数列表")
	if p.got(TArrow) {
		fd.Ret = p.typeExpr()
	}
	if p.at(TColon) {
		fd.HasBody = true
		fd.Body = p.block("函数定义")
		if fd.Body == nil {
			fd.Body = []Stmt{}
		}
	} else {
		p.endLine()
	}
	for _, d := range decs {
		if d.Name == "go" && len(d.Args) >= 1 {
			if sl, ok := d.Args[0].X.(*StrLit); ok {
				fd.GoExtern = sl.Val
			}
		}
	}
	return fd
}

// params 解析形参列表。lambda 为 true 时类型可以省略。
func (p *parser) params(lambda bool) []*Param {
	var ps []*Param
	for !p.at(TRParen) {
		pos := p.tok().Pos
		variadic := p.got(TStar)
		nt := p.expectIdent("参数")
		prm := &Param{Pos: pos, Name: nt.Text, Variadic: variadic}
		if p.got(TColon) {
			prm.Type = p.typeExpr()
		} else if !lambda && nt.Text != "self" {
			p.fail("参数 '%s' 需要类型注解，例如 %s: int", nt.Text, nt.Text)
		}
		if p.got(TAssign) {
			prm.Default = p.expr()
		}
		ps = append(ps, prm)
		if !p.got(TComma) {
			break
		}
	}
	return ps
}

// ===== 类型表达式 =====

func (p *parser) typeExpr() TypeExpr {
	t := p.baseType()
	for p.at(TQuestion) {
		pos := p.advance().Pos
		t = &OptTypeX{Pos: pos, Elem: t}
	}
	return t
}

func (p *parser) baseType() TypeExpr {
	t := p.tok()
	switch t.Kind {
	case TLParen:
		pos := p.advance().Pos
		var elems []TypeExpr
		trailing := false
		for !p.at(TRParen) {
			elems = append(elems, p.typeExpr())
			trailing = false
			if !p.got(TComma) {
				break
			}
			trailing = true
		}
		p.expect(TRParen, "元组类型")
		if len(elems) == 1 && !trailing {
			return elems[0]
		}
		return &TupleTypeX{Pos: pos, Elems: elems}
	case KNull:
		p.advance()
		return &NamedTypeX{Pos: t.Pos, Name: "null"}
	case TIdent:
		p.advance()
		if t.Text == "fn" && p.at(TLParen) {
			p.advance()
			ft := &FuncTypeX{Pos: t.Pos}
			for !p.at(TRParen) {
				ft.Params = append(ft.Params, p.typeExpr())
				if !p.got(TComma) {
					break
				}
			}
			p.expect(TRParen, "函数类型")
			if p.got(TArrow) {
				ft.Ret = p.typeExpr()
			}
			return ft
		}
		nt := &NamedTypeX{Pos: t.Pos, Name: t.Text}
		if p.at(TDot) && p.peek(1).Kind == TIdent {
			p.advance()
			nt.Mod = nt.Name
			nt.Name = p.advance().Text
		}
		if p.at(TLBrack) {
			p.advance()
			for !p.at(TRBrack) {
				nt.Args = append(nt.Args, p.typeExpr())
				if !p.got(TComma) {
					break
				}
			}
			p.expect(TRBrack, "类型实参")
		}
		return nt
	}
	p.fail("这里需要一个类型，但遇到了 %s", describeTok(t))
	return nil
}

// ===== 类、结构体、接口 =====

func (p *parser) classDecl(decs []*Decorator, abstract bool) *ClassDecl {
	kt := p.advance()
	cd := &ClassDecl{Pos: kt.Pos, Decorators: decs, Abstract: abstract}
	switch kt.Kind {
	case KStruct:
		cd.Kind = KindStruct
	case KInterface:
		cd.Kind = KindInterface
	}
	cd.Name = p.expectIdent("类型").Text
	if p.at(TLBrack) {
		cd.TypeParams = p.typeParams()
	}
	if p.got(TLParen) {
		for !p.at(TRParen) {
			cd.Bases = append(cd.Bases, p.typeExpr())
			if !p.got(TComma) {
				break
			}
		}
		p.expect(TRParen, "基类列表")
	}
	p.expect(TColon, "类型声明")
	if !p.at(TNewline) {
		// class X: pass
		if p.got(KPass) {
			p.endLine()
			return cd
		}
		p.fail("类型声明的成员需要写在缩进的代码块中")
	}
	p.advance()
	if !p.at(TIndent) {
		p.fail("类型声明后面需要一个缩进的代码块")
	}
	p.advance()
	p.inClass++
	defer func() { p.inClass-- }()
	for !p.at(TDedent) && !p.at(TEOF) {
		if p.at(TNewline) || p.at(TSemi) {
			p.advance()
			continue
		}
		p.memberSafe(func() { p.classMember(cd) })
	}
	p.got(TDedent)
	return cd
}

func (p *parser) memberSafe(fn func()) {
	start := p.i
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(parseBail); !ok {
				panic(r)
			}
			if p.i == start {
				p.advance()
			}
			p.sync()
		}
	}()
	fn()
}

type modifiers struct {
	static, abstract, override bool
}

func (p *parser) modifiers() modifiers {
	var m modifiers
	for p.at(TIdent) {
		w := p.tok().Text
		if w != "static" && w != "abstract" && w != "override" {
			break
		}
		nk := p.peek(1)
		ok := nk.Kind == KDef || nk.Kind == KAsync || nk.Kind == KConst ||
			(nk.Kind == TIdent && (isModifierWord(nk.Text) || (w == "static" && (p.peek(2).Kind == TColon || p.peek(2).Kind == TAssign))))
		if !ok {
			break
		}
		p.advance()
		switch w {
		case "static":
			m.static = true
		case "abstract":
			m.abstract = true
		case "override":
			m.override = true
		}
	}
	return m
}

func isModifierWord(s string) bool {
	return s == "static" || s == "abstract" || s == "override" || s == "prop"
}

func (p *parser) classMember(cd *ClassDecl) {
	if p.at(KPass) {
		p.advance()
		p.endLine()
		return
	}
	if p.at(TString) {
		// 文档字符串
		p.advance()
		p.endLine()
		return
	}
	var decs []*Decorator
	if p.at(TAt) {
		decs = p.decorators()
	}
	m := p.modifiers()
	switch {
	case p.at(KDef) || p.at(KAsync):
		async := p.got(KAsync)
		fd := p.funcDecl(decs, async)
		fd.Static = m.static
		fd.Abstract = m.abstract
		fd.Override = m.override
		cd.Methods = append(cd.Methods, fd)
	case p.atSoft("prop") && p.peek(1).Kind == TIdent:
		pd := p.propDecl()
		pd.Static = m.static
		pd.Abstract = m.abstract
		pd.Decorators = decs
		for _, d := range decs {
			if d.Name == "go" && len(d.Args) >= 1 {
				if sl, ok := d.Args[0].X.(*StrLit); ok {
					pd.GoExtern = sl.Val
				}
			}
		}
		cd.Props = append(cd.Props, pd)
	case p.at(KConst):
		pos := p.advance().Pos
		nt := p.expectIdent("常量")
		fdl := &FieldDecl{Pos: pos, Name: nt.Text, Static: true, Const: true}
		if p.got(TColon) {
			fdl.Type = p.typeExpr()
		}
		p.expect(TAssign, "常量声明")
		fdl.Default = p.expr()
		p.endLine()
		cd.Fields = append(cd.Fields, fdl)
	case p.at(TIdent) && p.peek(1).Kind == TAssign:
		nt := p.advance()
		p.advance()
		fdl := &FieldDecl{Pos: nt.Pos, Name: nt.Text, Static: m.static}
		fdl.Default = p.expr()
		p.endLine()
		cd.Fields = append(cd.Fields, fdl)
	case p.at(TIdent) && p.peek(1).Kind == TColon:
		nt := p.advance()
		p.advance()
		fdl := &FieldDecl{Pos: nt.Pos, Name: nt.Text, Static: m.static}
		fdl.Type = p.typeExpr()
		if p.got(TAssign) {
			fdl.Default = p.expr()
		}
		p.endLine()
		cd.Fields = append(cd.Fields, fdl)
	default:
		p.fail("类型声明中只能包含字段、方法和属性，但遇到了 %s", describeTok(p.tok()))
	}
}

// propDecl 解析属性：
//
//	prop area -> float:            # 只读，代码块就是 getter
//	    return self.w * self.h
//	prop width -> float:
//	    get:
//	        return self.w
//	    set(v):
//	        self.w = v
//	prop name -> str               # 无实现（接口/抽象）
func (p *parser) propDecl() *PropDecl {
	pos := p.advance().Pos // prop
	nt := p.expectIdent("属性")
	pd := &PropDecl{Pos: pos, Name: nt.Text}
	p.expect(TArrow, "属性声明（写成 prop 名字 -> 类型）")
	pd.Type = p.typeExpr()
	if p.got(TFatArrow) {
		// prop area -> float => self.w * self.h
		e := p.expr()
		pd.HasGet = true
		pd.Getter = []Stmt{&ReturnStmt{Pos: e.P(), Value: e}}
		p.endLine()
		return pd
	}
	if !p.at(TColon) {
		p.endLine()
		return pd
	}
	// 判断是否为 get/set 形式
	if p.peek(1).Kind == TNewline && p.peek(2).Kind == TIndent && p.peek(3).Kind == TIdent &&
		(p.peek(3).Text == "get" || p.peek(3).Text == "set") &&
		(p.peek(4).Kind == TColon || p.peek(4).Kind == TLParen) {
		p.advance() // :
		p.advance() // newline
		p.advance() // indent
		for !p.at(TDedent) && !p.at(TEOF) {
			if p.at(TNewline) {
				p.advance()
				continue
			}
			kw := p.expectIdent("属性访问器")
			switch kw.Text {
			case "get":
				pd.HasGet = true
				pd.Getter = p.block("get 访问器")
			case "set":
				pd.HasSet = true
				pd.SetParam = "value"
				if p.got(TLParen) {
					pd.SetParam = p.expectIdent("set 参数").Text
					p.expect(TRParen, "set 参数")
				}
				pd.Setter = p.block("set 访问器")
			default:
				p.fail("属性中只能有 get 和 set")
			}
		}
		p.got(TDedent)
		return pd
	}
	pd.HasGet = true
	pd.Getter = p.block("属性")
	return pd
}

func (p *parser) enumDecl(decs []*Decorator) *EnumDecl {
	pos := p.advance().Pos
	ed := &EnumDecl{Pos: pos}
	ed.Name = p.expectIdent("枚举").Text
	if p.at(TLBrack) {
		ed.TypeParams = p.typeParams()
	}
	if p.got(TLParen) {
		for !p.at(TRParen) {
			ed.Bases = append(ed.Bases, p.typeExpr())
			if !p.got(TComma) {
				break
			}
		}
		p.expect(TRParen, "接口列表")
	}
	p.expect(TColon, "枚举声明")
	p.expect(TNewline, "枚举声明")
	p.expect(TIndent, "枚举声明")
	p.inClass++
	defer func() { p.inClass-- }()
	for !p.at(TDedent) && !p.at(TEOF) {
		if p.at(TNewline) || p.at(TSemi) {
			p.advance()
			continue
		}
		p.memberSafe(func() {
			if p.at(TString) {
				p.advance()
				p.endLine()
				return
			}
			if p.at(KPass) {
				p.advance()
				p.endLine()
				return
			}
			var decs []*Decorator
			if p.at(TAt) {
				decs = p.decorators()
			}
			m := p.modifiers()
			if p.at(KDef) || p.at(KAsync) {
				async := p.got(KAsync)
				fd := p.funcDecl(decs, async)
				fd.Static = m.static
				ed.Methods = append(ed.Methods, fd)
				return
			}
			if p.atSoft("prop") && p.peek(1).Kind == TIdent {
				pd := p.propDecl()
				pd.Static = m.static
				ed.Props = append(ed.Props, pd)
				return
			}
			// 变体：Red, Green, Blue  或  Circle(radius: float)  或  Red = 1
			for {
				nt := p.expectIdent("枚举成员")
				vd := &VariantDecl{Pos: nt.Pos, Name: nt.Text}
				if p.got(TLParen) {
					vd.HasParens = true
					vd.Fields = p.params(false)
					p.expect(TRParen, "枚举成员字段")
				}
				if p.got(TAssign) {
					vd.Value = p.expr()
				}
				ed.Variants = append(ed.Variants, vd)
				if !p.got(TComma) {
					break
				}
				if p.at(TNewline) {
					break
				}
			}
			p.endLine()
		})
	}
	p.got(TDedent)
	return ed
}

func (p *parser) extendDecl() *ExtendDecl {
	pos := p.advance().Pos
	xd := &ExtendDecl{Pos: pos}
	if p.at(TLBrack) {
		xd.TypeParams = p.typeParams()
	}
	xd.Target = p.typeExpr()
	p.expect(TColon, "extend 声明")
	p.expect(TNewline, "extend 声明")
	p.expect(TIndent, "extend 声明")
	p.inClass++
	defer func() { p.inClass-- }()
	for !p.at(TDedent) && !p.at(TEOF) {
		if p.at(TNewline) || p.at(TSemi) {
			p.advance()
			continue
		}
		p.memberSafe(func() {
			if p.at(KPass) || p.at(TString) {
				p.advance()
				p.endLine()
				return
			}
			var decs []*Decorator
			if p.at(TAt) {
				decs = p.decorators()
			}
			m := p.modifiers()
			if p.atSoft("prop") && p.peek(1).Kind == TIdent {
				pd := p.propDecl()
				pd.Decorators = decs
				for _, d := range decs {
					if d.Name == "go" && len(d.Args) >= 1 {
						if sl, ok := d.Args[0].X.(*StrLit); ok {
							pd.GoExtern = sl.Val
						}
					}
				}
				xd.Props = append(xd.Props, pd)
				return
			}
			async := p.got(KAsync)
			if !p.at(KDef) {
				p.fail("extend 中只能定义方法和属性")
			}
			fd := p.funcDecl(decs, async)
			fd.Static = m.static
			xd.Methods = append(xd.Methods, fd)
		})
	}
	p.got(TDedent)
	return xd
}

func (p *parser) importStmt() *ImportStmt {
	t := p.advance()
	is := &ImportStmt{Pos: t.Pos}
	path := func() []string {
		var parts []string
		parts = append(parts, p.expectIdent("模块名").Text)
		for p.got(TDot) {
			parts = append(parts, p.expectIdent("模块名").Text)
		}
		return parts
	}
	if t.Kind == KImport {
		is.Path = path()
		if p.got(KAs) {
			is.Alias = p.expectIdent("别名").Text
		}
		return is
	}
	is.From = true
	is.Path = path()
	p.expect(KImport, "from 语句")
	if p.got(TStar) {
		is.Star = true
		return is
	}
	paren := p.got(TLParen)
	for {
		nt := p.expectIdent("导入名")
		in := &ImportName{Pos: nt.Pos, Name: nt.Text}
		if p.got(KAs) {
			in.Alias = p.expectIdent("别名").Text
		}
		is.Names = append(is.Names, in)
		if !p.got(TComma) {
			break
		}
		if paren && p.at(TRParen) {
			break
		}
	}
	if paren {
		p.expect(TRParen, "导入列表")
	}
	return is
}

func (p *parser) testDecl() *TestDecl {
	pos := p.advance().Pos
	name := p.advance().Text
	body := p.block("测试")
	return &TestDecl{Pos: pos, Name: name, Body: body}
}

// ===== 复合语句 =====

func (p *parser) ifStmt() *IfStmt {
	pos := p.advance().Pos
	cond := p.expr()
	s := &IfStmt{Pos: pos, Cond: cond}
	s.Then = p.block("if 语句")
	if p.at(KElif) {
		s.Else = []Stmt{p.ifStmt()}
	} else if p.at(KElse) {
		p.advance()
		s.Else = p.block("else 分支")
		if s.Else == nil {
			s.Else = []Stmt{}
		}
	}
	return s
}

// forTargets 解析 for 的循环变量：x、k, v、(i, (a, b))
func (p *parser) forTargets() Expr {
	var items []Expr
	pos := p.tok().Pos
	for {
		items = append(items, p.forTarget())
		if !p.got(TComma) {
			break
		}
		if p.at(KIn) {
			break
		}
	}
	if len(items) == 1 {
		return items[0]
	}
	return &TupleLit{exprBase: exprBase{Pos: pos}, Elems: items}
}

func (p *parser) forTarget() Expr {
	if p.at(TLParen) {
		pos := p.advance().Pos
		var items []Expr
		for !p.at(TRParen) {
			items = append(items, p.forTarget())
			if !p.got(TComma) {
				break
			}
		}
		p.expect(TRParen, "循环变量")
		return &TupleLit{exprBase: exprBase{Pos: pos}, Elems: items}
	}
	nt := p.expectIdent("循环变量")
	return &Ident{exprBase: exprBase{Pos: nt.Pos}, Name: nt.Text}
}

func (p *parser) forStmt() *ForStmt {
	pos := p.advance().Pos
	target := p.forTargets()
	p.expect(KIn, "for 语句")
	iter := p.exprList()
	body := p.block("for 语句")
	if p.at(KElse) {
		p.fail("五行不支持 for...else")
	}
	return &ForStmt{Pos: pos, Target: target, Iter: iter, Body: body}
}

func (p *parser) tryStmt() *TryStmt {
	pos := p.advance().Pos
	ts := &TryStmt{Pos: pos}
	ts.Body = p.block("try 语句")
	for p.at(KExcept) {
		hp := p.advance().Pos
		h := &Handler{Pos: hp}
		if !p.at(TColon) {
			if p.got(TLParen) {
				for !p.at(TRParen) {
					h.Types = append(h.Types, p.typeExpr())
					if !p.got(TComma) {
						break
					}
				}
				p.expect(TRParen, "异常类型列表")
			} else {
				h.Types = append(h.Types, p.typeExpr())
				for p.got(TComma) {
					h.Types = append(h.Types, p.typeExpr())
				}
			}
			if p.got(KAs) {
				h.Name = p.expectIdent("异常变量").Text
			}
		}
		h.Body = p.block("except 子句")
		ts.Handlers = append(ts.Handlers, h)
	}
	if p.at(KElse) && len(ts.Handlers) > 0 {
		p.advance()
		ts.Else = p.block("else 子句")
	}
	if p.got(KFinally) {
		ts.Finally = p.block("finally 子句")
		if ts.Finally == nil {
			ts.Finally = []Stmt{}
		}
	}
	if len(ts.Handlers) == 0 && ts.Finally == nil {
		p.errorf(pos, "try 语句至少需要一个 except 或 finally")
	}
	return ts
}

func (p *parser) withStmt() *WithStmt {
	pos := p.advance().Pos
	ws := &WithStmt{Pos: pos}
	for {
		ip := p.tok().Pos
		p.noAs++
		x := p.expr()
		p.noAs--
		item := &WithItem{X: x, Pos: ip}
		if p.got(KAs) {
			item.Name = p.expectIdent("with 变量").Text
		}
		ws.Items = append(ws.Items, item)
		if !p.got(TComma) {
			break
		}
	}
	ws.Body = p.block("with 语句")
	return ws
}

func (p *parser) matchStmt() *MatchStmt {
	pos := p.advance().Pos
	subj := p.exprList()
	ms := &MatchStmt{Pos: pos, Subject: subj}
	p.expect(TColon, "match 语句")
	p.expect(TNewline, "match 语句")
	p.expect(TIndent, "match 语句")
	for !p.at(TDedent) && !p.at(TEOF) {
		if p.at(TNewline) {
			p.advance()
			continue
		}
		p.memberSafe(func() {
			if !p.atSoft("case") {
				p.fail("match 中只能包含 case 子句")
			}
			cp := p.advance().Pos
			c := &Case{Pos: cp}
			c.Pat = p.pattern(true)
			if p.got(KIf) {
				c.Guard = p.expr()
			}
			c.Body = p.block("case 子句")
			ms.Cases = append(ms.Cases, c)
		})
	}
	p.got(TDedent)
	return ms
}

func (p *parser) selectStmt() *SelectStmt {
	pos := p.advance().Pos
	ss := &SelectStmt{Pos: pos}
	p.expect(TColon, "select 语句")
	p.expect(TNewline, "select 语句")
	p.expect(TIndent, "select 语句")
	for !p.at(TDedent) && !p.at(TEOF) {
		if p.at(TNewline) {
			p.advance()
			continue
		}
		p.memberSafe(func() {
			cp := p.tok().Pos
			if p.atSoft("default") {
				p.advance()
				sc := &SelCase{Pos: cp, Kind: selDefault}
				sc.Body = p.block("default 子句")
				ss.Cases = append(ss.Cases, sc)
				return
			}
			if !p.atSoft("case") {
				p.fail("select 中只能包含 case 和 default 子句")
			}
			p.advance()
			sc := &SelCase{Pos: cp}
			x := p.expr()
			var target Expr
			if p.got(TAssign) {
				target = x
				x = p.expr()
			}
			call, ok := x.(*Call)
			var attr *Attr
			if ok {
				attr, _ = call.Fn.(*Attr)
			}
			if attr == nil || (attr.Name != "recv" && attr.Name != "send") {
				p.errorf(x.P(), "select 的 case 必须是 ch.recv() 或 ch.send(值)")
				panic(parseBail{})
			}
			sc.Chan = attr.X
			if attr.Name == "recv" {
				sc.Kind = selRecv
				sc.Target = target
				if len(call.Args) != 0 {
					p.errorf(x.P(), "recv() 不接受参数")
				}
			} else {
				sc.Kind = selSend
				if target != nil {
					p.errorf(x.P(), "send 不能赋值给变量")
				}
				if len(call.Args) != 1 {
					p.errorf(x.P(), "send() 需要一个参数")
				} else {
					sc.Value = call.Args[0].X
				}
			}
			sc.Body = p.block("case 子句")
			ss.Cases = append(ss.Cases, sc)
		})
	}
	p.got(TDedent)
	return ss
}

// ===== 简单语句 =====

func (p *parser) constStmt() Stmt {
	pos := p.advance().Pos
	nt := p.expectIdent("常量")
	cd := &ConstDecl{Pos: pos, Name: nt.Text}
	if p.got(TColon) {
		cd.Type = p.typeExpr()
	}
	if p.got(TAssign) {
		cd.Value = p.expr()
	} else if cd.Type == nil {
		p.fail("常量需要初始值")
	}
	return cd
}

func (p *parser) simpleStmt() Stmt {
	t := p.tok()
	switch t.Kind {
	case KPass:
		p.advance()
		return &PassStmt{Pos: t.Pos}
	case KBreak:
		p.advance()
		return &BreakStmt{Pos: t.Pos}
	case KContinue:
		p.advance()
		return &ContinueStmt{Pos: t.Pos}
	case KReturn:
		p.advance()
		rs := &ReturnStmt{Pos: t.Pos}
		if !p.at(TNewline) && !p.at(TSemi) && !p.at(TEOF) && !p.at(TDedent) {
			rs.Value = p.exprList()
		}
		return rs
	case KRaise:
		p.advance()
		rs := &RaiseStmt{Pos: t.Pos}
		if !p.at(TNewline) && !p.at(TSemi) && !p.at(TEOF) && !p.at(TDedent) {
			rs.X = p.expr()
		}
		return rs
	case KDefer:
		p.advance()
		return &DeferStmt{Pos: t.Pos, X: p.expr()}
	case KGo:
		p.advance()
		return &GoStmt{Pos: t.Pos, X: p.expr()}
	case KYield:
		p.advance()
		return &YieldStmt{Pos: t.Pos, Value: p.exprList()}
	case KAssert:
		p.advance()
		start := p.i
		as := &AssertStmt{Pos: t.Pos, Cond: p.expr()}
		as.Src = p.srcText(start, p.i)
		if p.got(TComma) {
			as.Msg = p.expr()
		}
		return as
	case KDel:
		p.advance()
		ds := &DelStmt{Pos: t.Pos}
		for {
			ds.Targets = append(ds.Targets, p.expr())
			if !p.got(TComma) {
				break
			}
		}
		return ds
	case KConst:
		return p.constStmt()
	case KImport, KFrom:
		p.fail("import 只能出现在模块顶层")
	case KDef, KClass, KStruct, KEnum, KInterface, KIf, KWhile, KFor, KTry, KWith:
		p.fail("复合语句不能写在分号之后或同一行内")
	}
	return p.exprOrAssign()
}

// srcText 返回记号区间 [a, b) 对应的源码文本。
func (p *parser) srcText(a, b int) string {
	if a >= b || b > len(p.toks) {
		return ""
	}
	start := p.toks[a].Pos
	end := p.toks[b-1].End
	if start.File == nil || start.Line != end.Line {
		// 跨行：拼接记号文本
		var parts []string
		for i := a; i < b; i++ {
			parts = append(parts, tokText(&p.toks[i]))
		}
		return strings.Join(parts, " ")
	}
	line := []rune(start.File.Line(start.Line))
	s, e := start.Col-1, end.Col-1
	if s < 0 || e > len(line) || s > e {
		return ""
	}
	return strings.TrimSpace(string(line[s:e]))
}

func tokText(t *Token) string {
	switch t.Kind {
	case TIdent, TInt, TFloat:
		return t.Text
	case TString:
		return "\"" + t.Text + "\""
	}
	return t.Kind.String()
}

func isAugOp(k TokKind) bool {
	switch k {
	case TPlusEq, TMinusEq, TStarEq, TSlashEq, TDSlashEq, TPercentEq, TDStarEq, TAmpEq, TPipeEq, TCaretEq, TShlEq, TShrEq:
		return true
	}
	return false
}

func (p *parser) exprOrAssign() Stmt {
	pos := p.tok().Pos
	x := p.exprList()
	switch {
	case p.at(TColon):
		// 注解声明 x: T [= v]
		switch x.(type) {
		case *Ident, *Attr:
		default:
			p.fail("只有变量或 self.字段 可以加类型注解")
		}
		p.advance()
		vd := &VarDecl{Pos: pos, Target: x}
		vd.Type = p.typeExpr()
		if p.got(TAssign) {
			vd.Value = p.exprList()
		}
		return vd
	case p.at(TAssign):
		items := []Expr{x}
		for p.got(TAssign) {
			items = append(items, p.exprList())
		}
		for _, t := range items[:len(items)-1] {
			p.checkTarget(t)
		}
		return &AssignStmt{Pos: pos, Targets: items[:len(items)-1], Value: items[len(items)-1], Op: TAssign}
	case isAugOp(p.tok().Kind):
		op := p.advance().Kind
		p.checkTarget(x)
		if _, ok := x.(*TupleLit); ok {
			p.errorf(x.P(), "复合赋值不能用于多个目标")
		}
		v := p.expr()
		return &AssignStmt{Pos: pos, Targets: []Expr{x}, Value: v, Op: op}
	}
	return &ExprStmt{Pos: pos, X: x}
}

func (p *parser) checkTarget(e Expr) {
	switch t := e.(type) {
	case *Ident, *Attr, *Index:
	case *SliceExpr:
		p.errorf(e.P(), "不支持给切片赋值")
	case *TupleLit:
		for _, x := range t.Elems {
			p.checkTarget(x)
		}
	case *ListLit:
		for _, x := range t.Elems {
			p.checkTarget(x)
		}
	default:
		p.errorf(e.P(), "不能给这个表达式赋值")
	}
}

// ===== 表达式 =====

// exprList 解析可能是元组的表达式列表：a, b, c
func (p *parser) exprList() Expr {
	pos := p.tok().Pos
	x := p.expr()
	if !p.at(TComma) {
		return x
	}
	items := []Expr{x}
	for p.got(TComma) {
		if p.exprEnds() {
			break
		}
		items = append(items, p.expr())
	}
	return &TupleLit{exprBase: exprBase{Pos: pos}, Elems: items}
}

func (p *parser) exprEnds() bool {
	switch p.tok().Kind {
	case TNewline, TEOF, TAssign, TColon, TRParen, TRBrack, TRBrace, TSemi, TDedent:
		return true
	}
	return isAugOp(p.tok().Kind)
}

func (p *parser) isLambdaStart() bool {
	t := p.tok()
	if t.Kind == TIdent && p.peek(1).Kind == TFatArrow {
		return true
	}
	if t.Kind != TLParen {
		return false
	}
	depth := 0
	for j := p.i; j < len(p.toks); j++ {
		switch p.toks[j].Kind {
		case TLParen, TLBrack, TLBrace:
			depth++
		case TRParen, TRBrack, TRBrace:
			depth--
			if depth == 0 {
				k := p.toks[j+1].Kind
				return k == TFatArrow || k == TArrow
			}
		case TEOF:
			return false
		}
	}
	return false
}

func (p *parser) expr() Expr {
	if p.isLambdaStart() {
		return p.lambda()
	}
	return p.ternary()
}

func (p *parser) lambda() Expr {
	pos := p.tok().Pos
	lam := &Lambda{exprBase: exprBase{Pos: pos}}
	if p.at(TIdent) {
		nt := p.advance()
		lam.Params = []*Param{{Pos: nt.Pos, Name: nt.Text}}
	} else {
		p.expect(TLParen, "")
		lam.Params = p.params(true)
		p.expect(TRParen, "lambda 参数")
		if p.got(TArrow) {
			lam.Ret = p.typeExpr()
		}
	}
	p.expect(TFatArrow, "lambda")
	lam.Body = p.expr()
	return lam
}

func (p *parser) ternary() Expr {
	x := p.nullCo()
	if p.noTernary == 0 && p.at(KIf) {
		pos := p.advance().Pos
		cond := p.nullCo()
		if !p.at(KElse) {
			p.fail("条件表达式缺少 else：写成 a if 条件 else b")
		}
		p.advance()
		els := p.expr()
		return &Ternary{exprBase: exprBase{Pos: pos}, Cond: cond, Then: x, Else: els}
	}
	return x
}

func (p *parser) nullCo() Expr {
	x := p.orExpr()
	if p.at(TQQ) {
		pos := p.advance().Pos
		y := p.nullCo()
		return &NullCo{exprBase: exprBase{Pos: pos}, X: x, Y: y}
	}
	return x
}

func (p *parser) orExpr() Expr {
	x := p.andExpr()
	for p.at(KOr) || p.at(TOrOr) {
		pos := p.advance().Pos
		y := p.andExpr()
		x = &Logical{exprBase: exprBase{Pos: pos}, Op: KOr, X: x, Y: y}
	}
	return x
}

func (p *parser) andExpr() Expr {
	x := p.notExpr()
	for p.at(KAnd) || p.at(TAndAnd) {
		pos := p.advance().Pos
		y := p.notExpr()
		x = &Logical{exprBase: exprBase{Pos: pos}, Op: KAnd, X: x, Y: y}
	}
	return x
}

func (p *parser) notExpr() Expr {
	if p.at(KNot) || p.at(TBang) {
		pos := p.advance().Pos
		x := p.notExpr()
		return &Unary{exprBase: exprBase{Pos: pos}, Op: KNot, X: x}
	}
	return p.comparison()
}

func compOp(t *Token) TokKind {
	switch t.Kind {
	case TEq, TNe, TLt, TGt, TLe, TGe, KIn:
		return t.Kind
	}
	return TEOF
}

func (p *parser) comparison() Expr {
	pos := p.tok().Pos
	x := p.bitOr()
	var ops []TokKind
	xs := []Expr{x}
	for {
		t := p.tok()
		if t.Kind == KIs {
			ip := p.advance().Pos
			neg := p.got(KNot)
			ie := &IsExpr{exprBase: exprBase{Pos: ip}, X: xs[len(xs)-1], Neg: neg}
			if p.got(KNull) {
				ie.Null = true
			} else {
				ie.Type = p.typeExpr()
			}
			if len(ops) > 0 {
				p.errorf(ip, "is 不能参与链式比较")
			}
			xs[len(xs)-1] = ie
			continue
		}
		if t.Kind == KNot && p.peek(1).Kind == KIn {
			p.advance()
			p.advance()
			ops = append(ops, KNotIn)
			xs = append(xs, p.bitOr())
			continue
		}
		op := compOp(t)
		if op == TEOF {
			break
		}
		p.advance()
		ops = append(ops, op)
		xs = append(xs, p.bitOr())
	}
	if len(ops) == 0 {
		return xs[0]
	}
	return &Compare{exprBase: exprBase{Pos: pos}, Ops: ops, Xs: xs}
}

func (p *parser) binLevel(next func() Expr, ops ...TokKind) Expr {
	x := next()
	for {
		k := p.tok().Kind
		found := false
		for _, o := range ops {
			if k == o {
				found = true
				break
			}
		}
		if !found {
			return x
		}
		pos := p.advance().Pos
		y := next()
		x = &Binary{exprBase: exprBase{Pos: pos}, Op: k, X: x, Y: y}
	}
}

func (p *parser) bitOr() Expr  { return p.binLevel(p.bitXor, TPipe) }
func (p *parser) bitXor() Expr { return p.binLevel(p.bitAnd, TCaret) }
func (p *parser) bitAnd() Expr { return p.binLevel(p.shift, TAmp) }
func (p *parser) shift() Expr  { return p.binLevel(p.arith, TShl, TShr) }
func (p *parser) arith() Expr  { return p.binLevel(p.term, TPlus, TMinus) }
func (p *parser) term() Expr {
	return p.binLevel(p.castExpr, TStar, TSlash, TDSlash, TPercent)
}

func (p *parser) castExpr() Expr {
	x := p.unary()
	for p.at(KAs) && p.noAs == 0 {
		pos := p.advance().Pos
		safe := p.got(TQuestion)
		t := p.typeExpr()
		x = &Cast{exprBase: exprBase{Pos: pos}, X: x, Type: t, Safe: safe}
	}
	return x
}

func (p *parser) unary() Expr {
	t := p.tok()
	switch t.Kind {
	case TMinus, TPlus, TTilde:
		p.advance()
		x := p.unary()
		return &Unary{exprBase: exprBase{Pos: t.Pos}, Op: t.Kind, X: x}
	}
	return p.power()
}

func (p *parser) power() Expr {
	x := p.awaitExpr()
	if p.at(TDStar) {
		pos := p.advance().Pos
		y := p.unary()
		return &Binary{exprBase: exprBase{Pos: pos}, Op: TDStar, X: x, Y: y}
	}
	return x
}

func (p *parser) awaitExpr() Expr {
	if p.at(KAwait) {
		pos := p.advance().Pos
		x := p.awaitExpr()
		return &Await{exprBase: exprBase{Pos: pos}, X: x}
	}
	return p.postfix(p.atom())
}

// bracketHasTypeSyntax 判断 [...] 内是否包含只能出现在类型中的语法。
func (p *parser) bracketHasTypeSyntax() bool {
	depth := 0
	for j := p.i; j < len(p.toks); j++ {
		t := &p.toks[j]
		switch t.Kind {
		case TLBrack, TLParen, TLBrace:
			depth++
		case TRBrack, TRParen, TRBrace:
			depth--
			if depth == 0 {
				return false
			}
		case TQuestion, TArrow:
			return true
		case TIdent:
			if t.Text == "fn" && p.toks[j+1].Kind == TLParen {
				return true
			}
		case TNewline, TEOF:
			return false
		}
	}
	return false
}

func (p *parser) postfix(x Expr) Expr {
	for {
		t := p.tok()
		switch t.Kind {
		case TLParen:
			p.advance()
			args := p.callArgs()
			x = &Call{exprBase: exprBase{Pos: t.Pos}, Fn: x, Args: args}
		case TLBrack:
			if p.bracketHasTypeSyntax() {
				p.advance()
				ta := &TypeApp{exprBase: exprBase{Pos: t.Pos}, X: x}
				for !p.at(TRBrack) {
					ta.Args = append(ta.Args, p.typeExpr())
					if !p.got(TComma) {
						break
					}
				}
				p.expect(TRBrack, "类型实参")
				x = ta
				continue
			}
			p.advance()
			x = p.indexOrSlice(x, t.Pos)
		case TDot:
			p.advance()
			nt := p.tok()
			var name string
			if nt.Kind == TIdent {
				name = nt.Text
			} else if nt.Kind.IsKeyword() {
				name = nt.Kind.String()
			} else if nt.Kind == TInt {
				// 元组下标 t.0
				name = nt.Text
			} else {
				p.fail("'.' 后面需要成员名")
			}
			p.advance()
			x = &Attr{exprBase: exprBase{Pos: t.Pos}, X: x, Name: name, NamePos: nt.Pos}
		case TQDot:
			p.advance()
			nt := p.expectIdent("?. 之后")
			x = &Attr{exprBase: exprBase{Pos: t.Pos}, X: x, Name: nt.Text, NamePos: nt.Pos, Safe: true}
		case TBang:
			if t.SpaceBefore {
				return x
			}
			p.advance()
			x = &Unwrap{exprBase: exprBase{Pos: t.Pos}, X: x}
		default:
			return x
		}
	}
}

func (p *parser) indexOrSlice(x Expr, pos Pos) Expr {
	var lo, hi, step Expr
	isSlice := false
	var items []Expr
	for {
		if !p.at(TColon) {
			lo = p.expr()
		}
		if p.at(TColon) {
			isSlice = true
			p.advance()
			if !p.at(TColon) && !p.at(TRBrack) {
				hi = p.expr()
			}
			if p.got(TColon) {
				if !p.at(TRBrack) {
					step = p.expr()
				}
			}
			break
		}
		items = append(items, lo)
		if !p.got(TComma) {
			break
		}
		if p.at(TRBrack) {
			break
		}
		lo = nil
	}
	p.expect(TRBrack, "下标")
	if isSlice {
		if len(items) > 0 {
			p.errorf(pos, "不支持多维切片")
		}
		return &SliceExpr{exprBase: exprBase{Pos: pos}, X: x, Lo: lo, Hi: hi, Step: step}
	}
	var idx Expr
	if len(items) == 1 {
		idx = items[0]
	} else {
		idx = &TupleLit{exprBase: exprBase{Pos: items[0].P()}, Elems: items}
	}
	return &Index{exprBase: exprBase{Pos: pos}, X: x, Idx: idx}
}

// callArgs 解析实参列表（左括号已消费）。
func (p *parser) callArgs() []*Arg {
	var args []*Arg
	for !p.at(TRParen) {
		pos := p.tok().Pos
		if p.got(TStar) {
			args = append(args, &Arg{Pos: pos, X: p.expr(), Star: true})
		} else if p.at(TIdent) && p.peek(1).Kind == TAssign {
			name := p.advance().Text
			p.advance()
			args = append(args, &Arg{Pos: pos, Name: name, X: p.expr()})
		} else {
			x := p.expr()
			if p.at(KFor) && len(args) == 0 {
				x = p.comprehension(CompGen, x, nil, pos)
			}
			args = append(args, &Arg{Pos: pos, X: x})
		}
		if !p.got(TComma) {
			break
		}
	}
	p.expect(TRParen, "函数调用")
	return args
}

func (p *parser) comprehension(kind CompKind, elem, val Expr, pos Pos) Expr {
	c := &Comp{exprBase: exprBase{Pos: pos}, Kind: kind}
	if kind == CompDict {
		c.Key = elem
		c.Val = val
	} else {
		c.Elem = elem
	}
	for p.at(KFor) || p.at(KIf) {
		cp := p.tok().Pos
		if p.got(KIf) {
			p.noTernary++
			cond := p.nullCo()
			p.noTernary--
			c.Clauses = append(c.Clauses, &CompClause{Pos: cp, IsIf: true, Cond: cond})
			continue
		}
		p.advance()
		target := p.forTargets()
		p.expect(KIn, "推导式")
		p.noTernary++
		iter := p.nullCo()
		p.noTernary--
		c.Clauses = append(c.Clauses, &CompClause{Pos: cp, Target: target, Iter: iter})
	}
	return c
}

func (p *parser) atom() Expr {
	t := p.tok()
	switch t.Kind {
	case TIdent:
		p.advance()
		if t.Text == "super" && p.at(TLParen) && p.peek(1).Kind == TRParen {
			p.advance()
			p.advance()
			return &SuperExpr{exprBase: exprBase{Pos: t.Pos}}
		}
		return &Ident{exprBase: exprBase{Pos: t.Pos}, Name: t.Text}
	case TInt:
		p.advance()
		return &IntLit{exprBase: exprBase{Pos: t.Pos}, Text: t.Text, Val: t.IntVal}
	case TFloat:
		p.advance()
		return &FloatLit{exprBase: exprBase{Pos: t.Pos}, Text: t.Text, Val: t.FltVal}
	case TString:
		p.advance()
		s := t.Text
		for p.at(TString) {
			s += p.advance().Text
		}
		if p.at(TFString) {
			// "abc" f"..." 拼接
			fs := p.fstring(p.advance())
			fs.Parts = append([]*FPart{{Lit: s}}, fs.Parts...)
			return fs
		}
		return &StrLit{exprBase: exprBase{Pos: t.Pos}, Val: s}
	case TFString:
		p.advance()
		fs := p.fstring(*t)
		for p.at(TFString) || p.at(TString) {
			nt := p.advance()
			if nt.Kind == TString {
				fs.Parts = append(fs.Parts, &FPart{Lit: nt.Text})
			} else {
				fs.Parts = append(fs.Parts, p.fstring(nt).Parts...)
			}
		}
		return fs
	case TChar:
		p.advance()
		return &CharLit{exprBase: exprBase{Pos: t.Pos}, Val: t.Char}
	case KTrue, KFalse:
		p.advance()
		return &BoolLit{exprBase: exprBase{Pos: t.Pos}, Val: t.Kind == KTrue}
	case KNull:
		p.advance()
		return &NullLit{exprBase: exprBase{Pos: t.Pos}}
	case TLParen:
		p.advance()
		if p.got(TRParen) {
			return &TupleLit{exprBase: exprBase{Pos: t.Pos}}
		}
		saveAs := p.noAs
		p.noAs = 0
		x := p.expr()
		p.noAs = saveAs
		if p.at(KFor) {
			c := p.comprehension(CompGen, x, nil, t.Pos)
			p.expect(TRParen, "生成器表达式")
			return c
		}
		if p.got(TRParen) {
			return x
		}
		items := []Expr{x}
		for p.got(TComma) {
			if p.at(TRParen) {
				break
			}
			items = append(items, p.expr())
		}
		p.expect(TRParen, "元组")
		return &TupleLit{exprBase: exprBase{Pos: t.Pos}, Elems: items}
	case TLBrack:
		p.advance()
		ll := &ListLit{exprBase: exprBase{Pos: t.Pos}}
		if p.got(TRBrack) {
			return ll
		}
		x := p.expr()
		if p.at(KFor) {
			c := p.comprehension(CompList, x, nil, t.Pos)
			p.expect(TRBrack, "列表推导式")
			return c
		}
		ll.Elems = append(ll.Elems, x)
		for p.got(TComma) {
			if p.at(TRBrack) {
				break
			}
			ll.Elems = append(ll.Elems, p.expr())
		}
		p.expect(TRBrack, "列表")
		return ll
	case TLBrace:
		p.advance()
		if p.got(TRBrace) {
			return &DictLit{exprBase: exprBase{Pos: t.Pos}}
		}
		k := p.expr()
		if p.got(TColon) {
			v := p.expr()
			if p.at(KFor) {
				c := p.comprehension(CompDict, k, v, t.Pos)
				p.expect(TRBrace, "字典推导式")
				return c
			}
			dl := &DictLit{exprBase: exprBase{Pos: t.Pos}, Keys: []Expr{k}, Vals: []Expr{v}}
			for p.got(TComma) {
				if p.at(TRBrace) {
					break
				}
				kk := p.expr()
				p.expect(TColon, "字典")
				dl.Keys = append(dl.Keys, kk)
				dl.Vals = append(dl.Vals, p.expr())
			}
			p.expect(TRBrace, "字典")
			return dl
		}
		if p.at(KFor) {
			c := p.comprehension(CompSet, k, nil, t.Pos)
			p.expect(TRBrace, "集合推导式")
			return c
		}
		sl := &SetLit{exprBase: exprBase{Pos: t.Pos}, Elems: []Expr{k}}
		for p.got(TComma) {
			if p.at(TRBrace) {
				break
			}
			sl.Elems = append(sl.Elems, p.expr())
		}
		p.expect(TRBrace, "集合")
		return sl
	}
	if t.Kind.IsKeyword() {
		p.fail("这里需要一个表达式，但遇到了关键字 '%s'", t.Kind)
	}
	p.fail("这里需要一个表达式，但遇到了 %s", describeTok(t))
	return nil
}

func (p *parser) fstring(t Token) *FString {
	fs := &FString{exprBase: exprBase{Pos: t.Pos}}
	for _, part := range t.Parts {
		if part.IsExpr {
			toks := lexFragment(p.f, part.Src, part.SrcPos, p.diag)
			sub := &parser{f: p.f, toks: toks, diag: p.diag}
			func() {
				defer func() {
					if r := recover(); r != nil {
						if _, ok := r.(parseBail); !ok {
							panic(r)
						}
					}
				}()
				part.X = sub.expr()
				if !sub.at(TEOF) {
					sub.errorf(sub.tok().Pos, "f-字符串中的表达式有多余内容")
				}
			}()
			if part.X == nil {
				part.X = &StrLit{exprBase: exprBase{Pos: part.SrcPos}}
			}
		}
		fs.Parts = append(fs.Parts, part)
	}
	return fs
}

// ===== 模式 =====

func (p *parser) pattern(top bool) Pattern {
	pos := p.tok().Pos
	var pat Pattern
	first := p.closedPattern()
	if p.at(TPipe) {
		op := &OrPat{Pos: pos, Alts: []Pattern{first}}
		for p.got(TPipe) {
			op.Alts = append(op.Alts, p.closedPattern())
		}
		pat = op
	} else {
		pat = first
	}
	if p.at(KAs) {
		p.advance()
		name := p.expectIdent("as 绑定").Text
		pat = &AsPat{Pos: pos, Pat: pat, Name: name}
	}
	if top && p.at(TComma) {
		// case a, b: 视为元组模式
		tp := &TuplePat{Pos: pos, Elems: []Pattern{pat}}
		for p.got(TComma) {
			if p.at(TColon) || p.at(KIf) {
				break
			}
			tp.Elems = append(tp.Elems, p.pattern(false))
		}
		return tp
	}
	return pat
}

func (p *parser) closedPattern() Pattern {
	t := p.tok()
	switch t.Kind {
	case TInt, TFloat, TString, TChar, KTrue, KFalse, KNull, TFString:
		x := p.atom()
		return &LitPat{Pos: t.Pos, X: x}
	case TMinus:
		p.advance()
		nt := p.tok()
		if nt.Kind != TInt && nt.Kind != TFloat {
			p.fail("模式中的 '-' 后面需要数字")
		}
		x := p.atom()
		return &LitPat{Pos: t.Pos, X: &Unary{exprBase: exprBase{Pos: t.Pos}, Op: TMinus, X: x}}
	case TLParen:
		p.advance()
		tp := &TuplePat{Pos: t.Pos}
		for !p.at(TRParen) {
			tp.Elems = append(tp.Elems, p.pattern(false))
			if !p.got(TComma) {
				if len(tp.Elems) == 1 {
					p.expect(TRParen, "模式")
					return tp.Elems[0]
				}
				break
			}
		}
		p.expect(TRParen, "元组模式")
		return tp
	case TLBrack:
		p.advance()
		lp := &ListPat{Pos: t.Pos, RestIdx: -1}
		for !p.at(TRBrack) {
			if p.got(TStar) {
				if lp.RestIdx >= 0 {
					p.fail("列表模式中只能有一个 *")
				}
				lp.RestIdx = len(lp.Elems)
				lp.RestName = p.expectIdent("* 绑定").Text
			} else {
				lp.Elems = append(lp.Elems, p.pattern(false))
			}
			if !p.got(TComma) {
				break
			}
		}
		p.expect(TRBrack, "列表模式")
		return lp
	case TIdent:
		p.advance()
		if t.Text == "_" {
			return &WildPat{Pos: t.Pos}
		}
		names := []string{t.Text}
		for p.at(TDot) && p.peek(1).Kind == TIdent {
			p.advance()
			names = append(names, p.advance().Text)
		}
		if p.at(TLParen) {
			p.advance()
			cp := &CallPat{Pos: t.Pos, Names: names}
			for !p.at(TRParen) {
				if p.at(TIdent) && p.peek(1).Kind == TAssign {
					kw := p.advance().Text
					p.advance()
					cp.KwNames = append(cp.KwNames, kw)
				} else {
					cp.KwNames = append(cp.KwNames, "")
				}
				cp.Args = append(cp.Args, p.pattern(false))
				if !p.got(TComma) {
					break
				}
			}
			p.expect(TRParen, "模式")
			return cp
		}
		if p.at(TLBrack) && len(names) == 1 {
			// list[int] xs
			p.i--
			ty := p.typeExpr()
			nt := p.expectIdent("类型模式")
			return &TypePat{Pos: t.Pos, Type: ty, Name: nt.Text}
		}
		if p.at(TIdent) && len(names) <= 2 {
			nt := p.advance()
			var ty TypeExpr
			if len(names) == 2 {
				ty = &NamedTypeX{Pos: t.Pos, Mod: names[0], Name: names[1]}
			} else {
				ty = &NamedTypeX{Pos: t.Pos, Name: names[0]}
			}
			return &TypePat{Pos: t.Pos, Type: ty, Name: nt.Text}
		}
		return &NamePat{Pos: t.Pos, Names: names}
	}
	p.fail("这里需要一个模式，但遇到了 %s", describeTok(t))
	return nil
}
