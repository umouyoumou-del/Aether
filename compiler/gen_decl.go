package compiler

import (
	"fmt"
	"sort"
	"strings"
)

func (g *Gen) genTypeDecl(td *TypeDecl) {
	g.w = &g.decls
	g.resetLine()
	g.subst = nil
	g.self = nil
	switch td.Kind {
	case DExtern:
		return
	case DInterface:
		g.genIface(td)
	case DClass:
		g.genClass(td)
	case DStruct:
		g.genStruct(td)
	case DEnum:
		if td.ADT {
			g.genADT(td)
		} else {
			g.genEnum(td)
		}
	}
}

// ===== 函数签名 =====

func (g *Gen) paramList(fn *FuncCtx, sig *FuncSig) string {
	var parts []string
	for i, p := range sig.Params {
		var name string
		if fn != nil && i < len(fn.Params) {
			name = g.local(fn.Params[i])
		} else {
			name = fmt.Sprintf("ꞏp%d", i)
		}
		t := p.T
		if p.Variadic {
			t = &ListT{p.T}
		}
		parts = append(parts, name+" "+g.typ(t))
	}
	return strings.Join(parts, ", ")
}

func (g *Gen) asyncInner(t Type) string {
	if isVoid(t) {
		return g.rt() + ".Unit"
	}
	return g.typ(t)
}

func (g *Gen) retSig(sig *FuncSig) string {
	if sig.Async {
		return " *" + g.rt() + ".Task[" + g.asyncInner(sig.Ret) + "]"
	}
	if isVoid(sig.Ret) {
		return ""
	}
	return " " + g.typ(sig.Ret)
}

// funcPrologue 声明提升的局部变量与控制流变量。
func (g *Gen) funcPrologue(fn *FuncCtx) {
	for _, sym := range fn.Locals {
		if sym.T == nil || isInvalid(sym.T) {
			continue
		}
		name := g.local(sym)
		g.line("var %s %s", name, g.typ(sym.T))
		g.line("_ = %s", name)
	}
	if fn.NeedFlow {
		g.line("var ꞏflow int")
		g.line("_ = ꞏflow")
		if g.fn != nil && !isVoid(g.fn.retT) && !g.fn.isGen {
			g.line("var ꞏret %s", g.typ(g.fn.retT))
			g.line("_ = ꞏret")
		}
	}
}

// funcBody 生成函数体（处理 async 与生成器的包装）。
func (g *Gen) funcBody(fn *FuncCtx, body []Stmt, sig *FuncSig) {
	saveFn := g.fn
	defer func() { g.fn = saveFn }()
	if sig.Async {
		inner := g.asyncInner(sig.Ret)
		g.line("return %s.Async(func() %s {", g.rt(), inner)
		g.ind++
		g.fn = &genFn{ctx: fn, retT: sig.Ret, needFlow: fn.NeedFlow, tparams: sig.TParams}
		g.funcPrologue(fn)
		g.stmts(body)
		if isVoid(sig.Ret) {
			g.line("return %s.Unit{}", g.rt())
		} else {
			g.line("panic(%s.Unreachable())", g.rt())
		}
		g.ind--
		g.line("})")
		return
	}
	if fn.IsGen {
		et := fn.YieldT
		g.line("return func(yieldꞏ func(%s) bool) {", g.typ(et))
		g.ind++
		g.fn = &genFn{ctx: fn, retT: tVoid, needFlow: fn.NeedFlow, isGen: true, tparams: sig.TParams}
		g.funcPrologue(fn)
		g.stmts(body)
		g.ind--
		g.line("}")
		return
	}
	g.fn = &genFn{ctx: fn, retT: sig.Ret, needFlow: fn.NeedFlow, tparams: sig.TParams}
	g.funcPrologue(fn)
	g.stmts(body)
	if !isVoid(sig.Ret) {
		g.line("panic(%s.Unreachable())", g.rt())
	}
}

func (g *Gen) genFunc(fd *FuncDecl) {
	if fd.GoExtern != "" || !fd.HasBody || fd.Fn == nil {
		return
	}
	g.w = &g.decls
	g.subst = nil
	g.self = nil
	sig := fd.Sig
	g.pos(fd.Pos)
	g.line("func %s%s(%s)%s {", fd.Sym.GoName, g.tparamDecl(sig.TParams), g.paramList(fd.Fn, sig), g.retSig(sig))
	g.ind++
	g.funcBody(fd.Fn, fd.Body, sig)
	g.ind--
	g.line("}")
	g.resetLine()
	g.line("")
}

// ===== 接口 =====

func (g *Gen) methodSigLine(name string, fn *FuncCtx, sig *FuncSig) string {
	return name + "(" + g.paramList(nil, sig) + ")" + g.retSig(sig)
}

// lessParam 判断 m 是否为 __lt__(self, other)，是则返回 other 的类型。定义了 __lt__ 的类型
// 额外生成导出的 Lessꞏ 方法（包含 __lt__ 的接口也要求它），供运行时库的排序、求最值函数使用
// （rt.Lesser 约束；未导出的 __lt__ 在 rt 包中不可见）。
func lessParam(m *Method) (Type, bool) {
	if m == nil || m.Name != "__lt__" || m.Static || len(m.Sig.Params) != 1 || len(m.Sig.TParams) > 0 {
		return nil, false
	}
	return m.Sig.Params[0].T, true
}

// genCmpLess 在 Lessꞏ 之外再为类型生成 Cmpꞏ：rt.Compare 用它比较元组中的元素、key 函数
// 返回的值等。只在 __lt__ 能双向比较时生成（见 lessSymmetric）。recv 是 Go 接收者类型，
// selfT 是该类型，pt 是 __lt__ 的参数类型（ptGo 是它的 Go 写法）。
func (g *Gen) genCmpLess(recv string, selfT, pt Type, ptGo string) {
	if !g.c.lessSymmetric(selfT, pt) {
		return
	}
	g.line("func (self %s) Cmpꞏ(o any) int { x := o.(%s); if self.Lessꞏ(x) { return -1 }; if x.Lessꞏ(self) { return 1 }; return 0 }", recv, ptGo)
}

func (g *Gen) genIface(td *TypeDecl) {
	g.line("type %s%s interface {", td.GoName, g.tparamDecl(td.TParams))
	g.ind++
	for _, e := range td.Ifaces {
		g.line("%s", g.typ(e))
	}
	for _, m := range td.MethodList {
		g.line("%s", g.methodSigLine(m.GoName, nil, m.Sig))
		if pt, ok := lessParam(m); ok {
			g.line("Lessꞏ(%s) bool", g.typ(pt))
		}
	}
	for _, p := range td.PropList {
		g.line("ꞏget_%s() %s", p.Name, g.typ(p.T))
		if p.HasSet {
			g.line("ꞏset_%s(%s)", p.Name, g.typ(p.T))
		}
	}
	g.ind--
	g.line("}")
	g.line("")
}

// ===== 类 =====

func (g *Gen) selfNT(td *TypeDecl) *NamedT {
	return &NamedT{D: td, Args: tparamTypes(td.TParams)}
}

func (g *Gen) genClass(td *TypeDecl) {
	tpd := g.tparamDecl(td.TParams)
	tpu := tparamUse(g, td.TParams)
	st := td.GoName + tpu
	g.line("type %s%s struct {", td.GoName, tpd)
	g.ind++
	if td.Base != nil {
		g.line("%s", g.structType(td.Base))
	}
	for _, f := range td.Fields {
		g.line("%s %s", f.GoName, g.typ(f.T))
	}
	g.ind--
	g.line("}")
	g.line("")
	g.line("func (self *%s) %sꞏ() *%s { return self }", st, td.GoName, st)
	g.line("")
	if td.Poly() {
		g.line("type %sꞏI%s interface {", td.GoName, tpd)
		g.ind++
		if td.Base != nil {
			g.line("%sꞏI%s", td.Base.D.GoName, g.targs(td.Base.Args))
		}
		g.line("%sꞏ() *%s", td.GoName, st)
		if td.Base == nil {
			g.line("String() string")
			g.line("TypeNameꞏ() string")
			if td.IsExc {
				g.line("ExcNameꞏ() string")
			}
		}
		for _, m := range td.MethodList {
			if m.Static || len(m.Sig.TParams) > 0 || m.Name == "__init__" || m.Name == "__str__" {
				continue
			}
			g.line("%s", g.methodSigLine(m.GoName, nil, m.Sig))
			if pt, ok := lessParam(m); ok {
				g.line("Lessꞏ(%s) bool", g.typ(pt))
			}
		}
		for _, p := range td.PropList {
			if p.Static {
				continue
			}
			g.line("ꞏget_%s() %s", p.Name, g.typ(p.T))
			if p.HasSet {
				g.line("ꞏset_%s(%s)", p.Name, g.typ(p.T))
			}
		}
		g.ind--
		g.line("}")
		g.line("")
	}
	if !td.Abstract {
		g.genClassMethods(td)
		g.genCtor(td)
	}
	g.genStaticAndGeneric(td)
}

// ancestorOf 返回 cls 的祖先链中声明为 def 的那一项（已代入 cls 的类型参数）。
func ancestorOf(cls, def *TypeDecl) *NamedT {
	for _, anc := range supertypes(&NamedT{D: cls, Args: tparamTypes(cls.TParams)}) {
		if anc.D == def {
			return anc
		}
	}
	return nil
}

func (g *Gen) genClassMethods(td *TypeDecl) {
	nt := g.selfNT(td)
	chain := supertypes(nt)
	seen := map[string]bool{}
	g.work = nil
	done := map[string]bool{}
	hasStr := false
	var eqM *Method
	for _, anc := range chain {
		for _, m := range anc.D.MethodList {
			if m.Static || len(m.Sig.TParams) > 0 || m.Name == "__init__" {
				continue
			}
			if seen[m.Name] {
				continue
			}
			seen[m.Name] = true
			if m.Name == "__str__" {
				hasStr = true
			}
			if m.Name == "__eq__" {
				eqM = m
			}
			if m.Abstract || m.Decl == nil || !m.Decl.HasBody {
				continue
			}
			g.work = append(g.work, methodWork{cls: td, m: m, def: anc.D, goName: m.GoName})
		}
		for _, p := range anc.D.PropList {
			if p.Static || seen["prop:"+p.Name] {
				continue
			}
			seen["prop:"+p.Name] = true
			if p.Abstract || p.Decl == nil {
				continue
			}
			g.work = append(g.work, methodWork{cls: td, def: anc.D, prop: p, goName: "ꞏget_" + p.Name})
			if p.HasSet {
				g.work = append(g.work, methodWork{cls: td, def: anc.D, prop: p, setter: true, goName: "ꞏset_" + p.Name})
			}
		}
	}
	if td.CtorInit != nil && td.CtorInit.Decl != nil && td.CtorInit.Decl.HasBody {
		g.work = append(g.work, methodWork{cls: td, m: td.CtorInit, def: td.CtorInit.Owner, goName: "ꞏinit"})
	}
	for len(g.work) > 0 {
		w := g.work[0]
		g.work = g.work[1:]
		key := w.goName
		if done[key] {
			continue
		}
		done[key] = true
		g.genMethodCopy(w)
	}
	st := td.GoName + tparamUse(g, td.TParams)
	if !hasStr {
		g.genDefaultStr("*"+st, td.Name, g.allFields(td), "self.")
	}
	g.line("func (self *%s) TypeNameꞏ() string { return %q }", st, td.Name)
	g.genFields("*"+st, g.allFields(td), "", "")
	if td.IsExc {
		g.line("func (self *%s) ExcNameꞏ() string { return %q }", st, td.Name)
	}
	if eqM != nil {
		anc := ancestorOf(td, eqM.Owner)
		save := g.subst
		g.subst = makeSubst(anc.D.TParams, anc.Args)
		pt := g.typ(eqM.Sig.Params[0].T)
		g.subst = save
		g.line("func (self *%s) Eqꞏ(o any) bool { x, ok := o.(%s); return ok && self.__eq__(x) }", st, pt)
	}
	if mem, _, _ := g.c.findMember(nt, "__lt__"); mem != nil {
		lt, _ := mem.(*Method)
		if ltT, ok := lessParam(lt); ok {
			anc := ancestorOf(td, lt.Owner)
			save := g.subst
			g.subst = makeSubst(anc.D.TParams, anc.Args)
			pt := g.typ(ltT)
			g.subst = save
			g.line("func (self *%s) Lessꞏ(o %s) bool { return self.__lt__(o) }", st, pt)
			g.genCmpLess("*"+st, nt, subst(ltT, makeSubst(anc.D.TParams, anc.Args)), pt)
		}
	}
	// 接口要求的属性由字段满足时，生成访问方法
	for _, f := range g.allFieldsWithOwner(td) {
		if !g.ifaceProps[f.f.Name] || seen["prop:"+f.f.Name] {
			continue
		}
		save := g.subst
		g.subst = makeSubst(f.anc.D.TParams, f.anc.Args)
		ft := g.typ(f.f.T)
		g.subst = save
		g.line("func (self *%s) ꞏget_%s() %s { return self.%s }", st, f.f.Name, ft, f.f.GoName)
		g.line("func (self *%s) ꞏset_%s(v %s) { self.%s = v }", st, f.f.Name, ft, f.f.GoName)
	}
	g.line("")
}

type fieldWithOwner struct {
	f   *Field
	anc *NamedT
}

func (g *Gen) allFieldsWithOwner(td *TypeDecl) []fieldWithOwner {
	chain := supertypes(g.selfNT(td))
	var out []fieldWithOwner
	for i := len(chain) - 1; i >= 0; i-- {
		for _, f := range chain[i].D.Fields {
			out = append(out, fieldWithOwner{f, chain[i]})
		}
	}
	return out
}

func (g *Gen) allFields(td *TypeDecl) []*Field {
	var out []*Field
	for _, fw := range g.allFieldsWithOwner(td) {
		out = append(out, fw.f)
	}
	return out
}

// genFields 生成 Fieldsꞏ 方法（json.dumps 等使用）。
func (g *Gen) genFields(recv string, fields []*Field, extraName, extraVal string) {
	var names, vals []string
	if extraName != "" {
		names = append(names, fmt.Sprintf("%q", extraName))
		vals = append(vals, extraVal)
	}
	for _, f := range fields {
		if strings.HasPrefix(f.Name, "_") {
			continue
		}
		names = append(names, fmt.Sprintf("%q", f.Name))
		vals = append(vals, "self."+f.GoName)
	}
	g.line("func (self %s) Fieldsꞏ() ([]string, []any) { return []string{%s}, []any{%s} }", recv, strings.Join(names, ", "), strings.Join(vals, ", "))
}

func (g *Gen) genDefaultStr(recv, name string, fields []*Field, prefix string) {
	if len(fields) == 0 {
		g.line("func (self %s) String() string { return %q }", recv, name+"()")
		return
	}
	var names, vals []string
	for _, f := range fields {
		names = append(names, fmt.Sprintf("%q", f.Name))
		vals = append(vals, prefix+f.GoName)
	}
	g.line("func (self %s) String() string { return %s.ReprFields(%q, []string{%s}, %s) }", recv, g.rt(), name, strings.Join(names, ", "), strings.Join(vals, ", "))
}

// genMethodCopy 为具体类 cls 生成方法 m（定义于祖先 def）的副本，self 为 *cls。
func (g *Gen) genMethodCopy(w methodWork) {
	cls := w.cls
	anc := ancestorOf(cls, w.def)
	saveSubst, saveSelf, saveFn := g.subst, g.self, g.fn
	g.subst = makeSubst(anc.D.TParams, anc.Args)
	g.self = g.selfNT(cls)
	recv := "*" + cls.GoName + tparamUse(g, cls.TParams)
	if w.prop != nil {
		pd := w.prop.Decl
		if w.setter {
			fn := pd.SetFn
			g.line("func (self %s) %s(%s %s) {", recv, w.goName, g.local(fn.Params[0]), g.typ(w.prop.T))
			g.ind++
			g.fn = &genFn{ctx: fn, retT: tVoid, needFlow: fn.NeedFlow}
			g.funcPrologue(fn)
			g.stmts(pd.Setter)
		} else {
			fn := pd.GetFn
			g.line("func (self %s) %s() %s {", recv, w.goName, g.typ(w.prop.T))
			g.ind++
			g.fn = &genFn{ctx: fn, retT: w.prop.T, needFlow: fn.NeedFlow}
			g.funcPrologue(fn)
			g.stmts(pd.Getter)
			g.line("panic(%s.Unreachable())", g.rt())
		}
		g.ind--
		g.line("}")
		g.resetLine()
		g.line("")
		g.subst, g.self, g.fn = saveSubst, saveSelf, saveFn
		return
	}
	m := w.m
	fd := m.Decl
	sig := m.Sig
	g.pos(fd.Pos)
	g.line("func (self %s) %s(%s)%s {", recv, w.goName, g.paramList(fd.Fn, sig), g.retSig(sig))
	g.ind++
	g.funcBody(fd.Fn, fd.Body, sig)
	g.ind--
	g.line("}")
	g.resetLine()
	g.line("")
	g.subst, g.self, g.fn = saveSubst, saveSelf, saveFn
}

// superName 返回对 def 中方法 m 的 super 调用所用的方法名，并登记需要生成的副本。
func (g *Gen) superName(def *TypeDecl, m *Method) string {
	name := "ꞏsuperꞏ" + def.GoName + "ꞏ" + m.Name
	if g.self != nil {
		g.work = append(g.work, methodWork{cls: g.self.D, m: m, def: def, goName: name})
	}
	return name
}

func (g *Gen) genCtor(td *TypeDecl) {
	tpd := g.tparamDecl(td.TParams)
	tpu := tparamUse(g, td.TParams)
	st := td.GoName + tpu
	sig := td.Ctor
	if sig == nil {
		return
	}
	var params []string
	for i, p := range sig.Params {
		t := p.T
		if p.Variadic {
			t = &ListT{p.T}
		}
		params = append(params, fmt.Sprintf("ꞏp%d %s", i, g.typ(t)))
	}
	isStruct := td.Kind == DStruct
	ret := "*" + st
	if isStruct {
		ret = st
	}
	g.line("func %sꞏnew%s(%s) %s {", td.GoName, tpd, strings.Join(params, ", "), ret)
	g.ind++
	if isStruct {
		g.line("var self %s", st)
	} else {
		g.line("self := &%s{}", st)
	}
	autoFields := map[*Field]bool{}
	if td.AutoCtor {
		for _, f := range td.CtorFields {
			autoFields[f] = true
		}
	}
	saveSubst := g.subst
	for _, fw := range g.allFieldsWithOwner(td) {
		if fw.f.Default == nil || autoFields[fw.f] {
			continue
		}
		g.subst = makeSubst(fw.anc.D.TParams, fw.anc.Args)
		g.pos(fw.f.Pos)
		g.line("self.%s = %s", fw.f.GoName, g.expr(fw.f.Default))
	}
	g.subst = saveSubst
	if td.AutoCtor {
		for i, f := range td.CtorFields {
			g.line("self.%s = ꞏp%d", f.GoName, i)
		}
	} else if td.CtorInit != nil {
		var args []string
		for i := range sig.Params {
			args = append(args, fmt.Sprintf("ꞏp%d", i))
		}
		g.line("self.ꞏinit(%s)", strings.Join(args, ", "))
	}
	g.line("return self")
	g.ind--
	g.line("}")
	g.resetLine()
	g.line("")
}

// genStaticAndGeneric 生成静态方法与泛型方法（作为普通函数）。
func (g *Gen) genStaticAndGeneric(td *TypeDecl) {
	for _, m := range td.MethodList {
		if m.Decl == nil || !m.Decl.HasBody {
			continue
		}
		if !m.Static && len(m.Sig.TParams) == 0 {
			continue
		}
		g.genFreeMethod(td, m)
	}
}

func (g *Gen) genFreeMethod(td *TypeDecl, m *Method) {
	saveSubst, saveSelf := g.subst, g.self
	g.subst = nil
	g.self = nil
	fd := m.Decl
	sig := m.Sig
	var tps []*TypeParam
	if !m.Static {
		tps = append(tps, td.TParams...)
	}
	tps = append(tps, sig.TParams...)
	params := g.paramList(fd.Fn, sig)
	if !m.Static {
		selfT := g.typ(g.selfNT(td))
		if params != "" {
			params = "self " + selfT + ", " + params
		} else {
			params = "self " + selfT
		}
	}
	g.pos(fd.Pos)
	g.line("func %s%s(%s)%s {", m.GoName, g.tparamDecl(tps), params, g.retSig(sig))
	g.ind++
	g.funcBody(fd.Fn, fd.Body, sig)
	g.ind--
	g.line("}")
	g.resetLine()
	g.line("")
	g.subst, g.self = saveSubst, saveSelf
}

// ===== 结构体 =====

func (g *Gen) genStruct(td *TypeDecl) {
	tpd := g.tparamDecl(td.TParams)
	tpu := tparamUse(g, td.TParams)
	st := td.GoName + tpu
	g.line("type %s%s struct {", td.GoName, tpd)
	g.ind++
	for _, f := range td.Fields {
		g.line("%s %s", f.GoName, g.typ(f.T))
	}
	g.ind--
	g.line("}")
	g.line("")
	saveSelf := g.self
	g.self = nil
	hasStr := false
	var eqM *Method
	for _, m := range td.MethodList {
		if m.Name == "__str__" {
			hasStr = true
		}
		if m.Name == "__eq__" {
			eqM = m
		}
		if m.Static || len(m.Sig.TParams) > 0 || m.Decl == nil || !m.Decl.HasBody {
			continue
		}
		recv := st
		if m.Mutating {
			recv = "*" + st
		}
		fd := m.Decl
		g.pos(fd.Pos)
		g.line("func (self %s) %s(%s)%s {", recv, m.GoName, g.paramList(fd.Fn, m.Sig), g.retSig(m.Sig))
		g.ind++
		g.funcBody(fd.Fn, fd.Body, m.Sig)
		g.ind--
		g.line("}")
		g.resetLine()
		g.line("")
	}
	for _, p := range td.PropList {
		pd := p.Decl
		if pd == nil || p.Static {
			continue
		}
		if pd.GetFn != nil {
			g.line("func (self %s) ꞏget_%s() %s {", st, p.Name, g.typ(p.T))
			g.ind++
			g.fn = &genFn{ctx: pd.GetFn, retT: p.T, needFlow: pd.GetFn.NeedFlow}
			g.funcPrologue(pd.GetFn)
			g.stmts(pd.Getter)
			g.line("panic(%s.Unreachable())", g.rt())
			g.ind--
			g.line("}")
		}
		if pd.SetFn != nil {
			g.line("func (self *%s) ꞏset_%s(%s %s) {", st, p.Name, g.local(pd.SetFn.Params[0]), g.typ(p.T))
			g.ind++
			g.fn = &genFn{ctx: pd.SetFn, retT: tVoid, needFlow: pd.SetFn.NeedFlow}
			g.funcPrologue(pd.SetFn)
			g.stmts(pd.Setter)
			g.ind--
			g.line("}")
		}
	}
	g.fn = nil
	if !hasStr {
		g.genDefaultStr(st, td.Name, td.Fields, "self.")
	}
	g.line("func (self %s) TypeNameꞏ() string { return %q }", st, td.Name)
	g.genFields(st, td.Fields, "", "")
	if pt, ok := lessParam(td.Methods["__lt__"]); ok {
		// 总是值接收者：即使 __lt__ 修改 self（指针接收者），Lessꞏ 也在副本上调用，
		// 这样结构体的值类型本身满足 rt.Lesser
		g.line("func (self %s) Lessꞏ(o %s) bool { return self.__lt__(o) }", st, g.typ(pt))
		g.genCmpLess(st, g.selfNT(td), pt, g.typ(pt))
	}
	if eqM != nil {
		g.line("func (self %s) Eqꞏ(o any) bool { x, ok := o.(%s); return ok && self.__eq__(x) }", st, g.typ(eqM.Sig.Params[0].T))
	} else {
		var conds []string
		for _, f := range td.Fields {
			conds = append(conds, fmt.Sprintf("%s.Equal(self.%s, x.%s)", g.rt(), f.GoName, f.GoName))
		}
		if len(conds) > 0 {
			g.line("func (self %s) Eqꞏ(o any) bool { x, ok := o.(%s); return ok && %s }", st, st, strings.Join(conds, " && "))
		} else {
			g.line("func (self %s) Eqꞏ(o any) bool { _, ok := o.(%s); return ok }", st, st)
		}
	}
	for _, f := range td.Fields {
		if g.ifaceProps[f.Name] && td.Props[f.Name] == nil {
			g.line("func (self %s) ꞏget_%s() %s { return self.%s }", st, f.Name, g.typ(f.T), f.GoName)
			g.line("func (self *%s) ꞏset_%s(v %s) { self.%s = v }", st, f.Name, g.typ(f.T), f.GoName)
		}
	}
	g.line("")
	g.self = saveSelf
	g.genCtor(td)
	g.genStaticAndGeneric(td)
}

// ===== 枚举 =====

func (g *Gen) genEnum(td *TypeDecl) {
	g.line("type %s int", td.GoName)
	g.line("")
	g.line("const (")
	for _, v := range td.Variants {
		g.line("\t%s %s = %d", v.GoName, td.GoName, v.Value)
	}
	g.line(")")
	g.line("")
	g.line("func (self %s) EnumNameꞏ() string {", td.GoName)
	g.line("\tswitch self {")
	for _, v := range td.Variants {
		g.line("\tcase %s:", v.GoName)
		g.line("\t\treturn %q", v.Name)
	}
	g.line("\t}")
	g.line("\treturn %s.Itoa(int(self))", g.rt())
	g.line("}")
	g.line("")
	hasStr := td.Methods["__str__"] != nil
	if !hasStr {
		g.line("func (self %s) String() string { return %q + self.EnumNameꞏ() }", td.GoName, td.Name+".")
	}
	g.line("func (self %s) TypeNameꞏ() string { return %q }", td.GoName, td.Name)
	g.line("")
	var vals []string
	for _, v := range td.Variants {
		vals = append(vals, v.GoName)
	}
	g.line("func %sꞏvalues() *%s.List[%s] { return %s.ListOf[%s](%s) }", td.GoName, g.rt(), td.GoName, g.rt(), td.GoName, strings.Join(vals, ", "))
	g.line("")
	g.line("func %sꞏfrom_value(v int) %s {", td.GoName, td.GoName)
	g.line("\tswitch %s(v) {", td.GoName)
	g.line("\tcase %s:", strings.Join(vals, ", "))
	g.line("\t\treturn %s(v)", td.GoName)
	g.line("\t}")
	g.line("\t%s.Raise(\"ValueError\", \"%%d 不是 %s 的有效值\", v)", g.rt(), td.Name)
	g.line("\treturn 0")
	g.line("}")
	g.line("")
	for _, m := range td.MethodList {
		if m.Static || m.Decl == nil || !m.Decl.HasBody {
			continue
		}
		fd := m.Decl
		g.pos(fd.Pos)
		g.line("func (self %s) %s(%s)%s {", td.GoName, m.GoName, g.paramList(fd.Fn, m.Sig), g.retSig(m.Sig))
		g.ind++
		g.funcBody(fd.Fn, fd.Body, m.Sig)
		g.ind--
		g.line("}")
		g.resetLine()
		g.line("")
	}
	if pt, ok := lessParam(td.Methods["__lt__"]); ok {
		g.line("func (self %s) Lessꞏ(o %s) bool { return self.__lt__(o) }", td.GoName, g.typ(pt))
		g.genCmpLess(td.GoName, g.selfNT(td), pt, g.typ(pt))
		g.line("")
	}
	for _, p := range td.PropList {
		if p.Decl == nil || p.Decl.GetFn == nil {
			continue
		}
		g.line("func (self %s) ꞏget_%s() %s {", td.GoName, p.Name, g.typ(p.T))
		g.ind++
		g.fn = &genFn{ctx: p.Decl.GetFn, retT: p.T}
		g.funcPrologue(p.Decl.GetFn)
		g.stmts(p.Decl.Getter)
		g.line("panic(%s.Unreachable())", g.rt())
		g.ind--
		g.line("}")
		g.fn = nil
	}
	g.genStaticAndGeneric(td)
}

func (g *Gen) genADT(td *TypeDecl) {
	tpd := g.tparamDecl(td.TParams)
	tpu := tparamUse(g, td.TParams)
	g.line("type %s%s interface {", td.GoName, tpd)
	g.ind++
	g.line("isꞏ%s()", td.GoName)
	g.line("String() string")
	ltT, hasLt := lessParam(td.Methods["__lt__"])
	for _, m := range td.MethodList {
		if m.Static || len(m.Sig.TParams) > 0 || m.Name == "__str__" {
			continue
		}
		g.line("%s(%s)%s", memberName(m.Name), g.paramList(nil, m.Sig), g.retSig(m.Sig))
	}
	if hasLt {
		g.line("Lessꞏ(%s) bool", g.typ(ltT))
	}
	for _, p := range td.PropList {
		g.line("ꞏget_%s() %s", p.Name, g.typ(p.T))
	}
	g.ind--
	g.line("}")
	g.line("")
	strM := td.Methods["__str__"]
	for _, v := range td.Variants {
		vt := v.GoName + tpu
		g.line("type %s%s struct {", v.GoName, tpd)
		for _, f := range v.Fields {
			g.line("\t%s %s", f.GoName, g.typ(f.T))
		}
		g.line("}")
		g.line("")
		g.line("func (%s) isꞏ%s() {}", vt, td.GoName)
		if strM != nil {
			g.line("func (self %s) String() string { return %s%s(self) }", vt, strM.GoName, tpu)
		} else if len(v.Fields) == 0 {
			g.line("func (self %s) String() string { return %q }", vt, td.Name+"."+v.Name)
		} else {
			g.genDefaultStr(vt, td.Name+"."+v.Name, v.Fields, "self.")
		}
		g.line("func (self %s) TypeNameꞏ() string { return %q }", vt, td.Name)
		g.genFields(vt, v.Fields, "kind", fmt.Sprintf("%q", v.Name))
		var conds []string
		for _, f := range v.Fields {
			conds = append(conds, fmt.Sprintf("%s.Equal(self.%s, x.%s)", g.rt(), f.GoName, f.GoName))
		}
		if len(conds) > 0 {
			g.line("func (self %s) Eqꞏ(o any) bool { x, ok := o.(%s); return ok && %s }", vt, vt, strings.Join(conds, " && "))
		} else {
			g.line("func (self %s) Eqꞏ(o any) bool { _, ok := o.(%s); return ok }", vt, vt)
		}
		for _, m := range td.MethodList {
			if m.Static || len(m.Sig.TParams) > 0 || m.Name == "__str__" {
				continue
			}
			var args []string
			for i := range m.Sig.Params {
				args = append(args, fmt.Sprintf("ꞏp%d", i))
			}
			call := fmt.Sprintf("%s%s(self", m.GoName, tpu)
			if len(args) > 0 {
				call += ", " + strings.Join(args, ", ")
			}
			call += ")"
			ret := g.retSig(m.Sig)
			body := call
			if ret != "" {
				body = "return " + call
			}
			g.line("func (self %s) %s(%s)%s { %s }", vt, memberName(m.Name), g.paramList(nil, m.Sig), ret, body)
		}
		if hasLt {
			g.line("func (self %s) Lessꞏ(o %s) bool { return self.%s(o) }", vt, g.typ(ltT), memberName("__lt__"))
			g.genCmpLess(vt, g.selfNT(td), ltT, g.typ(ltT))
		}
		for _, p := range td.PropList {
			g.line("func (self %s) ꞏget_%s() %s { return %sꞏget_%s%s(self) }", vt, p.Name, g.typ(p.T), td.GoName, p.Name, tpu)
		}
		g.line("")
	}
	// 方法作为普通函数
	for _, m := range td.MethodList {
		if m.Decl == nil || !m.Decl.HasBody {
			continue
		}
		fd := m.Decl
		var tps []*TypeParam
		if !m.Static {
			tps = append(tps, td.TParams...)
		}
		tps = append(tps, m.Sig.TParams...)
		params := g.paramList(fd.Fn, m.Sig)
		if !m.Static {
			st := td.GoName + tpu
			if params != "" {
				params = "self " + st + ", " + params
			} else {
				params = "self " + st
			}
		}
		g.pos(fd.Pos)
		g.line("func %s%s(%s)%s {", m.GoName, g.tparamDecl(tps), params, g.retSig(m.Sig))
		g.ind++
		g.funcBody(fd.Fn, fd.Body, m.Sig)
		g.ind--
		g.line("}")
		g.resetLine()
		g.line("")
	}
	for _, p := range td.PropList {
		if p.Decl == nil || p.Decl.GetFn == nil {
			continue
		}
		g.line("func %sꞏget_%s%s(self %s) %s {", td.GoName, p.Name, tpd, td.GoName+tpu, g.typ(p.T))
		g.ind++
		g.fn = &genFn{ctx: p.Decl.GetFn, retT: p.T}
		g.funcPrologue(p.Decl.GetFn)
		g.stmts(p.Decl.Getter)
		g.line("panic(%s.Unreachable())", g.rt())
		g.ind--
		g.line("}")
		g.fn = nil
	}
}

// ===== 扩展 =====

func (g *Gen) genExt(x *ExtBlock) {
	g.w = &g.decls
	g.subst = nil
	g.self = nil
	for _, fd := range x.Decl.Methods {
		if !fd.HasBody || fd.Method == nil || fd.Fn == nil {
			continue
		}
		m := fd.Method
		tps := append(append([]*TypeParam(nil), x.TParams...), m.Sig.TParams...)
		params := g.paramList(fd.Fn, m.Sig)
		selfP := "self " + g.typ(x.Target)
		if params != "" {
			params = selfP + ", " + params
		} else {
			params = selfP
		}
		g.pos(fd.Pos)
		g.line("func %s%s(%s)%s {", m.GoName, g.tparamDecl(tps), params, g.retSig(m.Sig))
		g.ind++
		g.funcBody(fd.Fn, fd.Body, m.Sig)
		g.ind--
		g.line("}")
		g.resetLine()
		g.line("")
	}
	names := make([]string, 0, len(x.Props))
	for n := range x.Props {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := x.Props[n]
		if p.Extern != "" || p.Decl == nil || p.Decl.GetFn == nil {
			continue
		}
		g.line("func %s%s(self %s) %s {", p.GoName, g.tparamDecl(x.TParams), g.typ(x.Target), g.typ(p.T))
		g.ind++
		g.fn = &genFn{ctx: p.Decl.GetFn, retT: p.T}
		g.funcPrologue(p.Decl.GetFn)
		g.stmts(p.Decl.Getter)
		g.line("panic(%s.Unreachable())", g.rt())
		g.ind--
		g.line("}")
		g.line("")
		g.fn = nil
	}
}
