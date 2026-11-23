package compiler

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// ===== 编辑器支持：语言服务器（aether lsp）使用的分析结果 =====

// Analysis 是对一个源文件做语法与类型检查之后的结果，用于诊断、悬停提示、跳转到定义、补全与大纲。
type Analysis struct {
	Path  string
	Src   *SourceFile
	Diags []*Diag // 本文件的诊断（其它模块中的错误显示在对应的 import 语句上）
	Mod   *Module
	c     *Checker
	refs  []*Ref
	funcs []funcSpan
	// 调用、下标、切片表达式的类型，按其左括号的位置（行、列）记录：用于 foo(). 和 xs[0]. 的成员补全
	brackets map[[2]int]Type
}

// Ref 是源文件中一处名字（标识符、成员名、声明的名字、类型名）及其含义。
type Ref struct {
	Line, Col, End int // 行、列从 1 开始，列按字符计；End 不含
	Name           string
	T              Type
	Sym            *Symbol
	Attr           *Attr
	Def            Pos    // 定义的位置（Line 为 0 表示未知）
	Info           string // 悬停时显示的说明（为空时按 T / Sym / Attr 生成）
}

// funcSpan 记录函数体占据的行，用于补全时找出光标所在函数的局部变量。
type funcSpan struct {
	from, to int
	fn       *FuncCtx
}

// Analyze 加载并检查 path 处的源文件；src 是它的内容（可以是编辑器中尚未保存的内容）。
// 导入的模块从磁盘读取。
func Analyze(path string, src []byte, std fs.FS) (a *Analysis) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	a = &Analysis{Path: abs}
	diag := &Diagnostics{}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(tooManyErrors); !ok {
				diag.List = append(diag.List, &Diag{Pos: Pos{File: a.Src, Line: 1, Col: 1}, Msg: fmt.Sprintf("内部错误：%v", r)})
			}
		}
		a.finishDiags(diag)
	}()
	ld := &loader{std: std, diag: diag, byKey: map[string]*Module{}, state: map[*Module]int{}, mainDir: filepath.Dir(abs)}
	prelude := ld.loadStd("prelude", Pos{})
	if prelude == nil {
		diag.Errorf(Pos{}, "内部错误：找不到 prelude")
		return a
	}
	prelude.IsPrelude = true
	ld.visit(prelude)
	main := ld.parse(abs, filepath.Base(abs), src, "main")
	main.IsMain = true
	ld.byKey[abs] = main
	a.Src, a.Mod = main.Src, main
	ld.visit(main)
	if diag.HasErrors() {
		return a
	}
	a.c = Check(ld.order, diag, false)
	a.collect()
	return a
}

// finishDiags 留下本文件的诊断；其它模块中的错误放到导入它的语句上。
func (a *Analysis) finishDiags(diag *Diagnostics) {
	for _, d := range diag.List {
		if d.Pos.File == a.Src && a.Src != nil {
			a.Diags = append(a.Diags, d)
			continue
		}
		if d.Warning {
			continue
		}
		pos := Pos{File: a.Src, Line: 1, Col: 1}
		name := "?"
		if d.Pos.File != nil {
			name = d.Pos.File.Name
			if a.Mod != nil && a.Mod.File != nil {
				for _, st := range a.Mod.File.Stmts {
					if is, ok := st.(*ImportStmt); ok && is.Mod != nil && is.Mod.Src == d.Pos.File {
						pos = is.Pos
					}
				}
			}
		}
		msg := fmt.Sprintf("被导入的模块中有错误（%s:%d:%d）：%s", name, d.Pos.Line, d.Pos.Col, d.Msg)
		a.Diags = append(a.Diags, &Diag{Pos: pos, Msg: msg, Notes: d.Notes})
	}
}

// ===== 收集名字 =====

func (a *Analysis) add(pos Pos, name string, r *Ref) {
	if pos.File != a.Src || pos.Line == 0 || name == "" || strings.Contains(name, "ꞏ") {
		return
	}
	r.Line, r.Col, r.End, r.Name = pos.Line, pos.Col, pos.Col+utf8.RuneCountInString(name), name
	a.refs = append(a.refs, r)
}

func (a *Analysis) collect() {
	for _, st := range a.Mod.File.Stmts {
		a.stmt(st)
	}
	sort.SliceStable(a.refs, func(i, j int) bool {
		if a.refs[i].Line != a.refs[j].Line {
			return a.refs[i].Line < a.refs[j].Line
		}
		return a.refs[i].Col < a.refs[j].Col
	})
}

func (a *Analysis) stmts(list []Stmt) {
	for _, s := range list {
		a.stmt(s)
	}
}

func (a *Analysis) stmt(st Stmt) {
	switch s := st.(type) {
	case *FuncDecl:
		a.funcDecl(s)
		return
	case *FuncDeclStmt:
		a.funcDecl(s.Fn)
		return
	case *ClassDecl:
		a.classDecl(s)
		return
	case *EnumDecl:
		a.enumDecl(s)
		return
	case *ExtendDecl:
		a.typeExpr(s.Target)
		for _, m := range s.Methods {
			a.funcDecl(m)
		}
		for _, p := range s.Props {
			a.propDecl(p, "")
		}
		return
	case *TestDecl:
		if s.Fn != nil {
			a.addSpan(s.Pos.Line, s.Body, s.Fn)
		}
		a.stmts(s.Body)
		return
	case *TypeAliasDecl:
		if s.Sym != nil {
			a.add(s.Pos, s.Name, &Ref{Sym: s.Sym, Def: s.Pos, Info: a.typeInfo(s.Sym)})
		}
		a.typeExpr(s.Type)
		return
	case *ConstDecl:
		if s.Sym != nil {
			a.add(s.NamePos, s.Name, &Ref{Sym: s.Sym, T: s.Sym.T, Def: s.NamePos})
		}
		a.typeExpr(s.Type)
	case *VarDecl:
		a.typeExpr(s.Type)
	case *IfStmt:
		a.stmts(s.Then)
		a.stmts(s.Else)
	case *WhileStmt:
		a.stmts(s.Body)
	case *ForStmt:
		a.expr(s.Target)
		a.stmts(s.Body)
	case *TryStmt:
		a.stmts(s.Body)
		for _, h := range s.Handlers {
			for _, t := range h.Types {
				a.typeExpr(t)
			}
			a.stmts(h.Body)
		}
		a.stmts(s.Else)
		a.stmts(s.Finally)
	case *WithStmt:
		a.stmts(s.Body)
	case *MatchStmt:
		for _, cs := range s.Cases {
			a.pattern(cs.Pat)
			a.stmts(cs.Body)
		}
	case *SelectStmt:
		for _, cs := range s.Cases {
			a.stmts(cs.Body)
		}
	}
	for _, e := range stmtExprs(st) {
		a.expr(e)
	}
}

func (a *Analysis) funcDecl(fd *FuncDecl) {
	r := &Ref{Def: fd.NamePos, Sym: fd.Sym}
	if fd.Sig != nil {
		owner := ""
		if fd.Method != nil && fd.Method.Owner != nil {
			owner = fd.Method.Owner.Name
		} else if fd.Method != nil && fd.Method.Ext != nil {
			owner = typeStr(fd.Method.Ext.Target)
		}
		r.Info = sigText(fd.Name, owner, fd.Sig, fd.Method != nil && fd.Method.Ext != nil)
	}
	a.add(fd.NamePos, fd.Name, r)
	for _, p := range fd.Params {
		a.param(p)
	}
	a.typeExpr(fd.Ret)
	if fd.Fn != nil {
		a.addSpan(fd.NamePos.Line, fd.Body, fd.Fn)
	}
	a.stmts(fd.Body)
}

func (a *Analysis) param(p *Param) {
	if p.Sym != nil {
		pos := p.Pos
		if p.Variadic {
			pos.Col++ // *name
		}
		a.add(pos, p.Name, &Ref{Sym: p.Sym, T: p.Sym.T, Def: pos})
	}
	a.typeExpr(p.Type)
	a.expr(p.Default)
}

func (a *Analysis) addSpan(from int, body []Stmt, fn *FuncCtx) {
	to := from
	walkStmts(body, func(s Stmt) {
		if l := s.P().Line; l > to {
			to = l
		}
	})
	// 最后一条语句可能占多行：延伸到下一条顶层语句之前由调用者处理不了，这里放宽一些
	a.funcs = append(a.funcs, funcSpan{from: from, to: to + 1, fn: fn})
}

func (a *Analysis) classDecl(cd *ClassDecl) {
	td := cd.TD
	if td != nil && td.Sym != nil {
		a.add(cd.NamePos, cd.Name, &Ref{Sym: td.Sym, Def: cd.NamePos, Info: a.typeInfo(td.Sym)})
	}
	for _, b := range cd.Bases {
		a.typeExpr(b)
	}
	for _, f := range cd.Fields {
		if f.Field != nil {
			a.add(f.Pos, f.Name, &Ref{Def: f.Pos, Info: fieldInfo(f.Field)})
		}
		a.typeExpr(f.Type)
		a.expr(f.Default)
	}
	for _, m := range cd.Methods {
		a.funcDecl(m)
	}
	for _, p := range cd.Props {
		a.propDecl(p, cd.Name)
	}
}

func (a *Analysis) enumDecl(ed *EnumDecl) {
	td := ed.TD
	if td != nil && td.Sym != nil {
		a.add(ed.NamePos, ed.Name, &Ref{Sym: td.Sym, Def: ed.NamePos, Info: a.typeInfo(td.Sym)})
	}
	for _, v := range ed.Variants {
		info := ed.Name + "." + v.Name
		if td != nil {
			if vv := td.VariantMap[v.Name]; vv != nil {
				info = variantInfo(vv)
			}
		}
		a.add(v.Pos, v.Name, &Ref{Def: v.Pos, Info: info})
		for _, p := range v.Fields {
			a.typeExpr(p.Type)
		}
		a.expr(v.Value)
	}
	for _, m := range ed.Methods {
		a.funcDecl(m)
	}
	for _, p := range ed.Props {
		a.propDecl(p, ed.Name)
	}
}

func (a *Analysis) propDecl(pd *PropDecl, owner string) {
	if pd.Prop != nil {
		a.add(pd.NamePos, pd.Name, &Ref{Def: pd.NamePos, Info: propInfo(pd.Prop)})
	}
	a.typeExpr(pd.Type)
	if pd.GetFn != nil {
		a.addSpan(pd.NamePos.Line, pd.Getter, pd.GetFn)
	}
	if pd.SetFn != nil {
		a.addSpan(pd.NamePos.Line, pd.Setter, pd.SetFn)
	}
	a.stmts(pd.Getter)
	a.stmts(pd.Setter)
}

func (a *Analysis) pattern(p Pattern) {
	switch x := p.(type) {
	case *NamePat:
		if x.Kind == patCapture && x.Sym != nil && len(x.Names) == 1 {
			a.add(x.Pos, x.Names[0], &Ref{Sym: x.Sym, T: x.Sym.T, Def: x.Pos})
		}
	case *LitPat:
		a.expr(x.X)
	case *OrPat:
		for _, alt := range x.Alts {
			a.pattern(alt)
		}
	case *TuplePat:
		for _, el := range x.Elems {
			a.pattern(el)
		}
	case *ListPat:
		for _, el := range x.Elems {
			a.pattern(el)
		}
	case *CallPat:
		for _, sub := range x.Args {
			a.pattern(sub)
		}
	case *TypePat:
		a.typeExpr(x.Type)
	case *AsPat:
		a.pattern(x.Pat)
	}
}

func (a *Analysis) expr(e Expr) {
	if e == nil {
		return
	}
	walkExpr(e, func(x Expr) bool {
		switch n := x.(type) {
		case *Ident:
			if n.Sym != nil || n.T != nil {
				a.add(n.Pos, n.Name, &Ref{T: n.T, Sym: n.Sym, Def: a.symDef(n.Sym)})
			}
		case *Attr:
			a.add(n.NamePos, n.Name, &Ref{T: n.T, Attr: n, Sym: n.Sym, Def: a.attrDef(n)})
		case *Call, *Index, *SliceExpr:
			if p := x.P(); p.File == a.Src && x.Typ() != nil {
				if a.brackets == nil {
					a.brackets = map[[2]int]Type{}
				}
				a.brackets[[2]int{p.Line, p.Col}] = x.Typ()
			}
		case *Lambda:
			for _, p := range n.Params {
				a.param(p)
			}
		case *Comp:
			for _, cl := range n.Clauses {
				if cl.Target != nil {
					a.expr(cl.Target)
				}
			}
		case *Cast:
			a.typeExpr(n.Type)
		case *IsExpr:
			a.typeExpr(n.Type)
		case *TypeApp:
			for _, t := range n.Args {
				a.typeExpr(t)
			}
		}
		return true
	})
}

func (a *Analysis) typeExpr(t TypeExpr) {
	switch x := t.(type) {
	case *NamedTypeX:
		var sym *Symbol
		pos := x.Pos
		if x.Mod != "" {
			if ms := a.Mod.Scope.lookup(x.Mod); ms != nil && ms.ModRef != nil && ms.ModRef.Scope != nil {
				a.add(x.Pos, x.Mod, &Ref{Sym: ms, Def: a.symDef(ms)})
				sym = ms.ModRef.Scope.Syms[x.Name]
			}
			pos.Col += utf8.RuneCountInString(x.Mod) + 1
		} else {
			sym = a.Mod.Scope.lookup(x.Name)
		}
		if sym != nil && sym.Kind == SType {
			a.add(pos, x.Name, &Ref{Sym: sym, Def: a.symDef(sym), Info: a.typeInfo(sym)})
		}
		for _, arg := range x.Args {
			a.typeExpr(arg)
		}
	case *OptTypeX:
		a.typeExpr(x.Elem)
	case *TupleTypeX:
		for _, el := range x.Elems {
			a.typeExpr(el)
		}
	case *FuncTypeX:
		for _, p := range x.Params {
			a.typeExpr(p)
		}
		a.typeExpr(x.Ret)
	}
}

// ===== 定义的位置 =====

func (a *Analysis) symDef(sym *Symbol) Pos {
	if sym == nil {
		return Pos{}
	}
	switch sym.Kind {
	case SType:
		if sym.AliasDecl != nil {
			return sym.AliasDecl.Pos
		}
		if td := sym.TD; td != nil {
			return tdPos(td)
		}
		return Pos{}
	case SFunc:
		if sym.Func != nil {
			return sym.Func.NamePos
		}
	case SConst:
		if cd, ok := sym.DeclStmt.(*ConstDecl); ok {
			return cd.NamePos
		}
	case SModule:
		if sym.ModRef != nil && sym.ModRef.Src != nil {
			return Pos{File: sym.ModRef.Src, Line: 1, Col: 1}
		}
		return Pos{}
	case SBuiltin:
		return Pos{}
	}
	if sym.Field != nil {
		return sym.Field.Pos
	}
	return sym.Pos
}

func tdPos(td *TypeDecl) Pos {
	if td.Class != nil && td.Class.NamePos.Line > 0 {
		return td.Class.NamePos
	}
	if td.Enum != nil && td.Enum.NamePos.Line > 0 {
		return td.Enum.NamePos
	}
	return td.Pos
}

func (a *Analysis) attrDef(n *Attr) Pos {
	switch n.Kind {
	case AttrField, AttrStaticField:
		if n.Field != nil {
			return n.Field.Pos
		}
	case AttrProp, AttrExtProp:
		if n.Prop != nil && n.Prop.Decl != nil {
			return n.Prop.Decl.NamePos
		}
	case AttrMethodValue, AttrStaticMethod:
		if n.Method != nil {
			return n.Method.Pos
		}
	case AttrEnumCase, AttrVariantCtor:
		if n.Variant != nil {
			return n.Variant.Pos
		}
	case AttrModule, AttrType:
		return a.symDef(n.Sym)
	}
	return Pos{}
}

// ===== 查询 =====

// RefAt 返回光标（行、列从 1 开始，列按字符计）所在的名字。
func (a *Analysis) RefAt(line, col int) *Ref {
	var best *Ref
	for _, r := range a.refs {
		if r.Line == line && r.Col <= col && col <= r.End {
			if best == nil || r.End-r.Col < best.End-best.Col || (col < r.End && best.End == col) {
				best = r
			}
		}
	}
	return best
}

// RefEndingAt 返回在 (line, col) 处结束的名字（用于成员补全：光标前的接收者）。
func (a *Analysis) RefEndingAt(line, col int) *Ref {
	var best *Ref
	for _, r := range a.refs {
		if r.Line == line && r.End == col {
			best = r
		}
	}
	return best
}

// Hover 返回光标处名字的说明（Markdown），没有时返回空串。
func (a *Analysis) Hover(line, col int) string {
	r := a.RefAt(line, col)
	if r == nil {
		return ""
	}
	text := r.Info
	if text == "" {
		text = a.describe(r)
	}
	if text == "" {
		return ""
	}
	return "```aether\n" + text + "\n```"
}

// Definition 返回光标处名字的定义位置。
func (a *Analysis) Definition(line, col int) (path string, defLine, defCol int, ok bool) {
	r := a.RefAt(line, col)
	if r == nil || r.Def.Line == 0 || r.Def.File == nil || strings.HasPrefix(r.Def.File.Path, "<") {
		return "", 0, 0, false
	}
	return r.Def.File.Path, r.Def.Line, r.Def.Col, true
}

func (a *Analysis) describe(r *Ref) string {
	if n := r.Attr; n != nil {
		switch n.Kind {
		case AttrField, AttrStaticField:
			if n.Field != nil {
				return fieldInfo(n.Field)
			}
		case AttrProp, AttrExtProp:
			if n.Prop != nil {
				return propInfo(n.Prop)
			}
		case AttrMethodValue, AttrStaticMethod:
			if m := n.Method; m != nil {
				owner := ""
				if m.Owner != nil {
					owner = m.Owner.Name
				} else if m.Ext != nil {
					owner = typeStr(m.Ext.Target)
				}
				return sigText(m.Name, owner, m.Sig, m.Ext != nil)
			}
		case AttrEnumCase, AttrVariantCtor:
			if n.Variant != nil {
				return variantInfo(n.Variant)
			}
		case AttrModule, AttrType:
			if n.Sym != nil {
				return a.symInfo(n.Sym, n.T)
			}
		}
		if n.T != nil && !isInvalid(n.T) {
			return n.Name + ": " + typeStr(n.T)
		}
		return ""
	}
	if r.Sym != nil {
		return a.symInfo(r.Sym, r.T)
	}
	if r.T != nil && !isInvalid(r.T) {
		return r.Name + ": " + typeStr(r.T)
	}
	return ""
}

func (a *Analysis) symInfo(sym *Symbol, t Type) string {
	switch sym.Kind {
	case SVar:
		prefix := ""
		switch {
		case sym.IsSelf:
			prefix = ""
		case sym.Param:
			prefix = "(参数) "
		case sym.Global:
			prefix = "(全局变量) "
		}
		s := prefix + sym.Name + ": " + typeStr(sym.T)
		if t != nil && sym.T != nil && !isInvalid(t) && !identical(t, sym.T) {
			s += "\n# 这里已窄化为 " + typeStr(t)
		}
		return s
	case SConst:
		return "const " + sym.Name + ": " + typeStr(sym.T)
	case SFunc:
		if sym.Sig != nil {
			return sigText(sym.Name, "", sym.Sig, false)
		}
	case SType:
		return a.typeInfo(sym)
	case SModule:
		if sym.ModRef != nil {
			return "import " + sym.ModRef.Name
		}
	case SBuiltin:
		return "内置函数 " + sym.Name
	case STParam:
		if sym.TP != nil {
			return "类型参数 " + sym.TP.Name + ": " + sym.TP.BoundName()
		}
	}
	return sym.Name
}

func (a *Analysis) typeInfo(sym *Symbol) string {
	if sym.AliasDecl != nil {
		s := "type " + sym.Name
		if len(sym.AliasTPs) > 0 {
			s += tparamsText(sym.AliasTPs)
		}
		if sym.Alias != nil {
			s += " = " + typeStr(sym.Alias)
		}
		return s
	}
	if sym.Alias != nil {
		return typeStr(sym.Alias) + "（内置类型）"
	}
	if sym.Builtin != "" {
		return sym.Builtin + "（内置泛型类型）"
	}
	td := sym.TD
	if td == nil {
		return sym.Name
	}
	kw := map[DeclKind]string{DClass: "class", DStruct: "struct", DInterface: "interface", DEnum: "enum", DExtern: "class"}[td.Kind]
	if td.Kind == DClass && td.Abstract {
		kw = "abstract class"
	}
	s := kw + " " + td.Name + tparamsText(td.TParams)
	var bases []string
	if td.Base != nil {
		bases = append(bases, typeStr(td.Base))
	}
	for _, i := range td.Ifaces {
		bases = append(bases, typeStr(i))
	}
	if len(bases) > 0 {
		s += "(" + strings.Join(bases, ", ") + ")"
	}
	return s
}

func tparamsText(tps []*TypeParam) string {
	if len(tps) == 0 {
		return ""
	}
	var parts []string
	for _, tp := range tps {
		p := tp.Name
		if tp.Bound != BoundAny {
			p += ": " + tp.BoundName()
		}
		parts = append(parts, p)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// sigText 返回函数签名的文本，例如 def Point.dist(self, o: Point) -> float。
func sigText(name, owner string, sig *FuncSig, ext bool) string {
	var parts []string
	kwOnly := false
	for _, p := range sig.Params {
		s := p.Name + ": " + typeStr(p.T)
		if p.Variadic {
			s = "*" + s
			kwOnly = true
		} else if p.KwOnly && !kwOnly {
			parts = append(parts, "*")
			kwOnly = true
		}
		if p.Default != nil {
			s += " = …"
		}
		parts = append(parts, s)
	}
	full := name
	if owner != "" {
		full = owner + "." + name
	}
	s := "def " + full + tparamsText(sig.TParams) + "(" + strings.Join(parts, ", ") + ")"
	if sig.Ret != nil && !isVoid(sig.Ret) {
		s += " -> " + typeStr(sig.Ret)
	}
	if sig.Async {
		s = "async " + s
	}
	if ext {
		s += "    # 扩展方法"
	}
	return s
}

func fieldInfo(f *Field) string {
	prefix := "(字段) "
	if f.Const {
		prefix = "const "
	} else if f.Static {
		prefix = "(静态字段) "
	}
	owner := ""
	if f.Owner != nil {
		owner = f.Owner.Name + "."
	}
	return prefix + owner + f.Name + ": " + typeStr(f.T)
}

func propInfo(p *Prop) string {
	owner := ""
	if p.Owner != nil {
		owner = p.Owner.Name + "."
	} else if p.Ext != nil {
		owner = typeStr(p.Ext.Target) + "."
	}
	rw := "只读"
	if p.HasSet {
		rw = "可读写"
	}
	return "prop " + owner + p.Name + " -> " + typeStr(p.T) + "    # " + rw + "属性"
}

func variantInfo(v *Variant) string {
	owner := ""
	if v.Owner != nil {
		owner = v.Owner.Name + "."
	}
	if !v.HasPayload {
		return owner + v.Name
	}
	var parts []string
	for _, f := range v.Fields {
		parts = append(parts, f.Name+": "+typeStr(f.T))
	}
	return owner + v.Name + "(" + strings.Join(parts, ", ") + ")"
}

// ===== 补全 =====

// Completion 是一个补全项。Kind 为 field、method、property、function、variable、constant、
// class、struct、interface、enum、module、keyword、enumMember、typeParameter 之一。
type Completion struct {
	Label  string
	Kind   string
	Detail string
}

var keywordList = []string{"and", "as", "assert", "async", "await", "break", "class", "const", "continue", "def", "defer", "del",
	"elif", "else", "enum", "except", "extend", "false", "finally", "for", "from", "go", "if", "import", "in", "interface", "is",
	"not", "null", "or", "pass", "raise", "return", "struct", "true", "try", "while", "with", "yield",
	"match", "case", "select", "prop", "static", "abstract", "override", "global", "type", "test", "self"}

// MembersBefore 返回第 line 行、位于第 dotCol 列的 “.” 之前的表达式的成员：
// 名字（x.、self.、math.）、调用或下标的结果（f().、xs[0].）、字符串字面量（"...".）。
func (a *Analysis) MembersBefore(line, dotCol int) []Completion {
	rs := []rune(a.LineText(line))
	i := dotCol - 2 // “.” 之前的字符（下标从 0 开始）
	for i >= 0 && (rs[i] == ' ' || rs[i] == '\t') {
		i--
	}
	if i < 0 || i >= len(rs) || a.c == nil {
		return nil
	}
	switch rs[i] {
	case ')', ']':
		open := map[rune]rune{')': '(', ']': '['}[rs[i]]
		depth := 0
		for j := i; j >= 0; j-- {
			switch rs[j] {
			case ')', ']':
				depth++
			case '(', '[':
				depth--
				if depth == 0 {
					if rs[j] != open {
						return nil
					}
					if t, ok := a.brackets[[2]int{line, j + 1}]; ok {
						return a.members(t)
					}
					return nil
				}
			}
		}
		return nil
	case '"':
		return a.members(tStr)
	case '\'':
		return a.members(tChar)
	}
	return a.MembersAt(line, i+2)
}

// MembersAt 返回在 (line, col) 处结束的表达式（即 “.” 之前的部分）的成员。
func (a *Analysis) MembersAt(line, col int) []Completion {
	r := a.RefEndingAt(line, col)
	if r == nil || a.c == nil {
		return nil
	}
	t := r.T
	if t == nil && r.Sym != nil {
		t = r.Sym.T
	}
	if r.Sym != nil && r.Sym.Kind == SModule && r.Sym.ModRef != nil {
		t = &ModT{r.Sym.ModRef}
	}
	return a.members(t)
}

func (a *Analysis) members(t Type) []Completion {
	if t == nil || isInvalid(t) {
		return nil
	}
	var out []Completion
	seen := map[string]bool{}
	addC := func(c Completion) {
		if seen[c.Label] || strings.HasPrefix(c.Label, "__") || strings.Contains(c.Label, "ꞏ") {
			return
		}
		if strings.HasPrefix(c.Label, "_") && !strings.HasPrefix(c.Label, "__") {
			return
		}
		seen[c.Label] = true
		out = append(out, c)
	}
	switch x := t.(type) {
	case *ModT:
		names := make([]string, 0, len(x.M.Scope.Syms))
		for n := range x.M.Scope.Syms {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			sym := x.M.Scope.Syms[n]
			if sym.Mod != x.M && sym.Kind != SFunc && sym.Kind != SType {
				continue
			}
			addC(a.symCompletion(sym))
		}
		return out
	case *MetaT:
		if nt, ok := x.T.(*NamedT); ok {
			for _, v := range nt.D.Variants {
				addC(Completion{Label: v.Name, Kind: "enumMember", Detail: variantInfo(v)})
			}
			for _, anc := range supertypes(nt) {
				for _, m := range anc.D.MethodList {
					if m.Static {
						addC(Completion{Label: m.Name, Kind: "method", Detail: sigText(m.Name, anc.D.Name, m.Sig, false)})
					}
				}
				for _, f := range anc.D.Statics {
					addC(Completion{Label: f.Name, Kind: "constant", Detail: fieldInfo(f)})
				}
			}
			if nt.D.Kind == DEnum && !nt.D.ADT {
				addC(Completion{Label: "values", Kind: "method", Detail: "def values() -> list[" + nt.D.Name + "]"})
				addC(Completion{Label: "from_value", Kind: "method", Detail: "def from_value(v: int) -> " + nt.D.Name})
			}
		}
		return out
	case *OptT:
		return a.members(x.Elem)
	case *NamedT:
		if x.D.Kind == DInterface {
			methods, props, propT := a.c.ifaceMembers(x)
			names := sortedKeys(methods)
			for _, n := range names {
				addC(Completion{Label: n, Kind: "method", Detail: sigText(n, x.D.Name, methods[n], false)})
			}
			for _, n := range sortedKeys(props) {
				addC(Completion{Label: n, Kind: "property", Detail: "prop " + n + " -> " + typeStr(propT[n])})
			}
		} else {
			for _, anc := range supertypes(x) {
				m := makeSubst(anc.D.TParams, anc.Args)
				for _, f := range anc.D.Fields {
					fc := *f
					fc.T = subst(f.T, m)
					addC(Completion{Label: f.Name, Kind: "field", Detail: fieldInfo(&fc)})
				}
				for _, p := range anc.D.PropList {
					if !p.Static {
						addC(Completion{Label: p.Name, Kind: "property", Detail: propInfo(p)})
					}
				}
				for _, mt := range anc.D.MethodList {
					if !mt.Static {
						addC(Completion{Label: mt.Name, Kind: "method", Detail: sigText(mt.Name, anc.D.Name, substSig(mt.Sig, m), false)})
					}
				}
			}
			if x.D.Kind == DEnum && !x.D.ADT {
				addC(Completion{Label: "name", Kind: "property", Detail: "prop name -> str"})
				addC(Completion{Label: "value", Kind: "property", Detail: "prop value -> int"})
			}
		}
	case *TupleT:
		for i, el := range x.Elems {
			addC(Completion{Label: fmt.Sprint(i), Kind: "field", Detail: fmt.Sprintf("%d: %s", i, typeStr(el))})
		}
	}
	// 扩展方法与属性（包括内置类型在 prelude 中声明的方法）
	for _, c := range a.extMembers(t) {
		addC(c)
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// extMembers 列出适用于类型 t 的扩展方法与属性。
func (a *Analysis) extMembers(t Type) []Completion {
	var blocks []*ExtBlock
	for _, k := range extKeys(t) {
		blocks = append(blocks, a.c.exts[k]...)
	}
	blocks = append(blocks, a.c.ifaceExts...)
	var out []Completion
	for _, blk := range blocks {
		m, ok := a.c.extApplies(blk, t)
		if !ok {
			continue
		}
		good := true
		for _, tp := range blk.TParams {
			if m[tp] == nil {
				good = false
				break
			}
			if ok, _ := a.c.satisfiesIn(m[tp], tp, m); !ok {
				good = false
				break
			}
		}
		if !good {
			continue
		}
		for _, name := range sortedKeys(blk.Methods) {
			for _, mt := range blk.Methods[name] {
				out = append(out, Completion{Label: name, Kind: "method", Detail: sigText(name, typeStr(t), substSig(mt.Sig, m), false)})
			}
		}
		for _, name := range sortedKeys(blk.Props) {
			p := blk.Props[name]
			out = append(out, Completion{Label: name, Kind: "property", Detail: "prop " + name + " -> " + typeStr(subst(p.T, m))})
		}
	}
	return out
}

func (a *Analysis) symCompletion(sym *Symbol) Completion {
	c := Completion{Label: sym.Name, Detail: a.symInfo(sym, nil)}
	switch sym.Kind {
	case SVar:
		c.Kind = "variable"
	case SConst:
		c.Kind = "constant"
	case SFunc, SBuiltin:
		c.Kind = "function"
	case SModule:
		c.Kind = "module"
	case STParam:
		c.Kind = "typeParameter"
	case SType:
		c.Kind = "class"
		if sym.TD != nil {
			switch sym.TD.Kind {
			case DStruct:
				c.Kind = "struct"
			case DInterface:
				c.Kind = "interface"
			case DEnum:
				c.Kind = "enum"
			}
		}
	}
	return c
}

// NamesAt 返回在 (line, col) 处可以使用的名字：所在函数（及外层函数）的局部变量与参数、
// 模块中的名字、内置名字和关键字。
func (a *Analysis) NamesAt(line, col int) []Completion {
	var out []Completion
	seen := map[string]bool{}
	addC := func(c Completion) {
		if c.Label == "" || seen[c.Label] || strings.Contains(c.Label, "ꞏ") {
			return
		}
		seen[c.Label] = true
		out = append(out, c)
	}
	// 局部变量：从最内层的函数向外
	var spans []funcSpan
	for _, s := range a.funcs {
		if s.from <= line && line <= s.to {
			spans = append(spans, s)
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].to-spans[i].from < spans[j].to-spans[j].from })
	for _, s := range spans {
		for f := s.fn; f != nil; f = f.Parent {
			for _, p := range f.Params {
				addC(a.symCompletion(p))
			}
			if f.Self != nil {
				addC(a.symCompletion(f.Self))
			}
			for _, l := range f.Locals {
				if l.Pos.Line <= line {
					addC(a.symCompletion(l))
				}
			}
		}
	}
	if a.Mod != nil && a.Mod.Scope != nil {
		for _, n := range sortedKeys(a.Mod.Scope.Syms) {
			addC(a.symCompletion(a.Mod.Scope.Syms[n]))
		}
	}
	if a.c != nil {
		for _, n := range sortedKeys(a.c.universe.Syms) {
			if strings.HasPrefix(n, "_") {
				continue
			}
			addC(a.symCompletion(a.c.universe.Syms[n]))
		}
	}
	for _, k := range keywordList {
		addC(Completion{Label: k, Kind: "keyword"})
	}
	return out
}

// ===== 大纲 =====

// DocSymbol 是大纲中的一项。
type DocSymbol struct {
	Name     string
	Kind     string // function、class、struct、interface、enum、method、field、property、constant、variable、enumMember、module
	Detail   string
	Line     int // 定义所在的行（名字的位置）
	Col      int
	EndLine  int // 整个定义的最后一行
	Children []DocSymbol
}

// Outline 返回文件的大纲（顶层的函数、类型、常量与它们的成员）。
func (a *Analysis) Outline() []DocSymbol {
	if a.Mod == nil || a.Mod.File == nil {
		return nil
	}
	stmts := a.Mod.File.Stmts
	var out []DocSymbol
	for i, st := range stmts {
		end := 0
		if i+1 < len(stmts) {
			end = stmts[i+1].P().Line - 1
		} else if a.Src != nil {
			end = len(a.Src.lines)
		}
		switch s := st.(type) {
		case *FuncDecl:
			out = append(out, DocSymbol{Name: s.Name, Kind: "function", Detail: funcDetail(s), Line: s.NamePos.Line, Col: s.NamePos.Col, EndLine: end})
		case *ClassDecl:
			kind := map[ClassKind]string{KindClass: "class", KindStruct: "struct", KindInterface: "interface"}[s.Kind]
			ds := DocSymbol{Name: s.Name, Kind: kind, Line: s.NamePos.Line, Col: s.NamePos.Col, EndLine: end}
			for _, f := range s.Fields {
				k := "field"
				if f.Const {
					k = "constant"
				}
				ds.Children = append(ds.Children, DocSymbol{Name: f.Name, Kind: k, Line: f.Pos.Line, Col: f.Pos.Col, EndLine: f.Pos.Line})
			}
			for _, p := range s.Props {
				ds.Children = append(ds.Children, DocSymbol{Name: p.Name, Kind: "property", Line: p.NamePos.Line, Col: p.NamePos.Col, EndLine: p.NamePos.Line})
			}
			for _, m := range s.Methods {
				ds.Children = append(ds.Children, DocSymbol{Name: m.Name, Kind: "method", Detail: funcDetail(m), Line: m.NamePos.Line, Col: m.NamePos.Col, EndLine: m.NamePos.Line})
			}
			sortSymbols(ds.Children)
			out = append(out, ds)
		case *EnumDecl:
			ds := DocSymbol{Name: s.Name, Kind: "enum", Line: s.NamePos.Line, Col: s.NamePos.Col, EndLine: end}
			for _, v := range s.Variants {
				ds.Children = append(ds.Children, DocSymbol{Name: v.Name, Kind: "enumMember", Line: v.Pos.Line, Col: v.Pos.Col, EndLine: v.Pos.Line})
			}
			for _, m := range s.Methods {
				ds.Children = append(ds.Children, DocSymbol{Name: m.Name, Kind: "method", Detail: funcDetail(m), Line: m.NamePos.Line, Col: m.NamePos.Col, EndLine: m.NamePos.Line})
			}
			out = append(out, ds)
		case *ExtendDecl:
			name := "extend"
			if s.Ext != nil && s.Ext.Target != nil {
				name = "extend " + typeStr(s.Ext.Target)
			}
			ds := DocSymbol{Name: name, Kind: "module", Line: s.Pos.Line, Col: s.Pos.Col, EndLine: end}
			for _, m := range s.Methods {
				ds.Children = append(ds.Children, DocSymbol{Name: m.Name, Kind: "method", Detail: funcDetail(m), Line: m.NamePos.Line, Col: m.NamePos.Col, EndLine: m.NamePos.Line})
			}
			out = append(out, ds)
		case *ConstDecl:
			out = append(out, DocSymbol{Name: s.Name, Kind: "constant", Line: s.NamePos.Line, Col: s.NamePos.Col, EndLine: s.NamePos.Line})
		case *TypeAliasDecl:
			out = append(out, DocSymbol{Name: s.Name, Kind: "class", Detail: "类型别名", Line: s.Pos.Line, Col: s.Pos.Col, EndLine: s.Pos.Line})
		case *TestDecl:
			out = append(out, DocSymbol{Name: "test " + s.Name, Kind: "function", Line: s.Pos.Line, Col: s.Pos.Col, EndLine: end})
		}
	}
	return out
}

func funcDetail(fd *FuncDecl) string {
	if fd.Sig == nil {
		return ""
	}
	s := sigText(fd.Name, "", fd.Sig, false)
	return strings.TrimPrefix(strings.TrimPrefix(s, "async "), "def "+fd.Name)
}

func sortSymbols(xs []DocSymbol) {
	sort.SliceStable(xs, func(i, j int) bool { return xs[i].Line < xs[j].Line })
}

// LineText 返回源文件第 n 行（从 1 开始）的内容。
func (a *Analysis) LineText(n int) string {
	if a.Src == nil {
		return ""
	}
	return a.Src.Line(n)
}

// Checked 判断是否完成了类型检查（有语法错误时没有类型信息）。
func (a *Analysis) Checked() bool { return a.c != nil }
