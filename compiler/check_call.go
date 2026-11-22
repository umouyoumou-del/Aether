package compiler

import (
	"fmt"
	"strings"
)

// call 检查函数调用。
func (c *Checker) call(e *Call, want Type) Type {
	switch fn := e.Fn.(type) {
	case *Attr:
		if _, ok := fn.X.(*SuperExpr); ok {
			return c.superCall(e, fn, want)
		}
		rt := c.expr(fn.X, nil)
		if isInvalid(rt) {
			c.checkArgsQuiet(e)
			return tInvalid
		}
		switch r := rt.(type) {
		case *ModT:
			ft := c.moduleMember(fn, r.M)
			fn.T = ft
			return c.callValueOf(e, fn.Sym, ft, want)
		case *MetaT:
			return c.staticCall(e, fn, r.T, want)
		case *GenericT:
			if td := r.Sym.TD; td != nil && td.Kind == DEnum {
				if v := td.VariantMap[fn.Name]; v != nil && v.HasPayload {
					fn.Kind = AttrVariantCtor
					fn.Variant = v
					fn.Owner = &NamedT{D: td}
					fn.T = &FuncT{}
					return c.variantCall(e, fn, want)
				}
			}
			c.errorf(fn.Pos, "访问 %s 的静态成员需要先给出类型实参", r.Sym.Name)
			c.checkArgsQuiet(e)
			return tInvalid
		}
		return c.methodCall(e, fn, rt, want)
	case *Ident:
		sym := c.scope.lookup(fn.Name)
		if sym == nil {
			c.undefined(fn.Pos, fn.Name)
			c.checkArgsQuiet(e)
			return tInvalid
		}
		if sym.Kind == SBuiltin || (sym.Kind == SType && sym.Builtin == "any") {
			fn.Sym = sym
			fn.T = &FuncRefT{Name: sym.Name}
			return c.builtinCall(e, sym.Builtin, want)
		}
		ft := c.expr(fn, nil)
		if isInvalid(ft) {
			c.checkArgsQuiet(e)
			return tInvalid
		}
		return c.callValueOf(e, sym, ft, want)
	}
	ft := c.expr(e.Fn, nil)
	if isInvalid(ft) {
		c.checkArgsQuiet(e)
		return tInvalid
	}
	var explicit []Type
	switch f := e.Fn.(type) {
	case *TypeApp:
		explicit = f.Types
	case *Index:
		if f.IsTypeApp {
			if fr, ok := ft.(*FuncRefT); ok {
				args, _ := indexTypeArgs(f.Idx)
				for _, a := range args {
					explicit = append(explicit, c.resolveType(a, c.scope))
				}
				_ = fr
			}
		}
	}
	if fr, ok := ft.(*FuncRefT); ok && explicit != nil && fr.Sig != nil {
		// max_of[float](...)
		var sym *Symbol
		switch f := e.Fn.(type) {
		case *TypeApp:
			sym = calleeSym(f.X)
		case *Index:
			sym = calleeSym(f.X)
		}
		e.Kind = CallFunc
		e.Func = sym
		if sym != nil && sym.Kind != SFunc {
			e.Kind = CallValue
		}
		if a, ok := unwrapTypeAppX(e.Fn).(*Attr); ok {
			if a.Kind == AttrStaticMethod {
				e.Kind = CallStatic
				e.Method = a.Method
			} else if a.Kind == AttrModule {
				e.Kind = CallFunc
				e.Func = a.Sym
			} else if a.Kind == AttrMethodValue {
				e.Kind = CallMethod
				e.Method = a.Method
				e.Recv = a.X
				if a.Method.Ext != nil {
					e.Kind = CallExt
				}
			}
		}
		e.Sig = fr.Sig
		return c.checkCall(e, fr.Sig, nil, fr.Sig.TParams, explicit, want)
	}
	return c.callValueOf(e, nil, ft, want)
}

func unwrapTypeAppX(e Expr) Expr {
	switch f := e.(type) {
	case *TypeApp:
		return f.X
	case *Index:
		return f.X
	}
	return e
}

func calleeSym(e Expr) *Symbol {
	switch x := e.(type) {
	case *Ident:
		return x.Sym
	case *Attr:
		return x.Sym
	}
	return nil
}

// callValueOf 根据被调用者的符号/类型分派。
func (c *Checker) callValueOf(e *Call, sym *Symbol, ft Type, want Type) Type {
	switch f := ft.(type) {
	case *MetaT:
		return c.typeCall(e, f.T, want)
	case *GenericT:
		return c.genericTypeCall(e, f.Sym, want)
	case *FuncRefT:
		if f.Sig == nil {
			if sym != nil && sym.Kind == SBuiltin {
				return c.builtinCall(e, sym.Builtin, want)
			}
			c.errorf(e.Pos, "不能调用 %s", f.Name)
			c.checkArgsQuiet(e)
			return tInvalid
		}
		e.Kind = CallFunc
		e.Func = sym
		e.Sig = f.Sig
		if sym != nil && sym.Func != nil && sym.Func.GoExtern != "" {
			e.Kind = CallExtern
		}
		return c.checkCall(e, f.Sig, nil, f.Sig.TParams, nil, want)
	case *FuncT:
		if sym != nil && sym.Kind == SFunc && sym.Sig != nil {
			e.Kind = CallFunc
			e.Func = sym
			e.Sig = sym.Sig
			if sym.Func != nil && sym.Func.GoExtern != "" {
				e.Kind = CallExtern
			}
			return c.checkCall(e, sym.Sig, nil, nil, nil, want)
		}
		if sym != nil && sym.LocalFunc && sym.Sig != nil {
			e.Kind = CallLocalFunc
			e.Func = sym
			e.Sig = sym.Sig
			return c.checkCall(e, sym.Sig, nil, nil, nil, want)
		}
		if a, ok := e.Fn.(*Attr); ok && a.Kind == AttrStaticMethod {
			e.Kind = CallStatic
			e.Method = a.Method
			e.Sig = a.Method.Sig
			return c.checkCall(e, a.Method.Sig, makeSubst(a.Owner.D.TParams, a.Owner.Args), nil, nil, want)
		}
		if a, ok := e.Fn.(*Attr); ok && a.Kind == AttrVariantCtor {
			return c.variantCall(e, a, want)
		}
		e.Kind = CallValue
		return c.callFuncValue(e, f, want)
	case *OptT:
		if _, ok := f.Elem.(*FuncT); ok {
			d := c.errorf(e.Pos, "函数值可能为 null，不能直接调用")
			d.Notes = append(d.Notes, "先判断是否为 null，或使用 f!(...)")
			c.checkArgsQuiet(e)
			return tInvalid
		}
	}
	c.errorf(e.Pos, "%s 不能被调用", ft)
	c.checkArgsQuiet(e)
	return tInvalid
}

func (c *Checker) checkArgsQuiet(e *Call) {
	for _, a := range e.Args {
		if a.X.Typ() == nil {
			if _, ok := a.X.(*Lambda); ok {
				continue
			}
			c.expr(a.X, nil)
		}
	}
}

// callFuncValue 调用函数类型的值（只支持位置参数）。
func (c *Checker) callFuncValue(e *Call, ft *FuncT, want Type) Type {
	sig := &FuncSig{Ret: ft.Ret}
	for i, p := range ft.Params {
		pi := &ParamInfo{Name: fmt.Sprintf("参数%d", i+1), T: p}
		if ft.Variadic && i == len(ft.Params)-1 {
			pi.Variadic = true
			if l, ok := p.(*ListT); ok {
				pi.T = l.Elem
			}
		}
		sig.Params = append(sig.Params, pi)
	}
	for _, a := range e.Args {
		if a.Name != "" {
			c.errorf(a.Pos, "调用函数值时不能使用关键字参数")
			c.checkArgsQuiet(e)
			return tInvalid
		}
	}
	e.Sig = sig
	return c.checkCall(e, sig, nil, nil, nil, want)
}

type boundArg struct {
	x      *Expr
	extras []*Arg
	spread bool
	set    bool
}

// matchArgs 把实参与形参对应起来。
func (c *Checker) matchArgs(e *Call, sig *FuncSig, what string, quiet bool) ([]boundArg, bool) {
	n := len(sig.Params)
	bound := make([]boundArg, n)
	pos := 0
	ok := true
	vi := -1 // 可变参数的位置；其后的参数只能用关键字传递
	kw := n   // 第一个只能用关键字传递的参数
	for i, p := range sig.Params {
		if p.Variadic {
			vi = i
		}
		if p.KwOnly && !p.Variadic && i < kw {
			kw = i
		}
	}
	report := func(p Pos, format string, args ...any) {
		ok = false
		if !quiet {
			c.errorf(p, format, args...)
		}
	}
	for i := range e.Args {
		a := e.Args[i]
		if a.Name == "" && !a.Star {
			if pos < n && pos < kw && !sig.Params[pos].Variadic && (vi < 0 || pos < vi) {
				bound[pos] = boundArg{x: &e.Args[i].X, set: true}
				pos++
				continue
			}
			if vi >= 0 && pos >= vi {
				bound[vi].extras = append(bound[vi].extras, a)
				bound[vi].set = true
				pos = vi
				continue
			}
			if kw < n {
				var names []string
				for _, p := range sig.Params[kw:] {
					if !p.Variadic {
						names = append(names, p.Name)
					}
				}
				report(a.Pos, "%s最多接受 %d 个位置参数，但给了 %d 个（%s 只能用关键字传递，例如 %s=...）", what, kw, countPositional(e.Args), strings.Join(names, "、"), names[0])
			} else {
				report(a.Pos, "%s最多接受 %d 个参数，但给了 %d 个", what, n, countPositional(e.Args))
			}
			continue
		}
		if a.Star {
			if vi < 0 {
				report(a.Pos, "%s没有可变参数，不能使用 *展开", what)
				continue
			}
			if len(bound[vi].extras) > 0 || bound[vi].spread {
				report(a.Pos, "*展开必须是传给可变参数的唯一实参")
				continue
			}
			bound[vi] = boundArg{x: &e.Args[i].X, spread: true, set: true}
			e.Spread = true
			pos = vi
			continue
		}
		idx := -1
		for j, p := range sig.Params {
			if p.Name == a.Name {
				idx = j
				break
			}
		}
		if idx < 0 {
			var names []string
			for _, p := range sig.Params {
				names = append(names, p.Name)
			}
			report(a.Pos, "%s没有名为 %s 的参数（参数有：%s）", what, a.Name, strings.Join(names, ", "))
			continue
		}
		if sig.Params[idx].Variadic {
			report(a.Pos, "可变参数 %s 不能用关键字传递", a.Name)
			continue
		}
		if bound[idx].set {
			report(a.Pos, "参数 %s 被重复传递", a.Name)
			continue
		}
		bound[idx] = boundArg{x: &e.Args[i].X, set: true}
	}
	for i, p := range sig.Params {
		if !bound[i].set && p.Default == nil && !p.Variadic {
			report(e.Pos, "%s缺少参数 %s", what, p.Name)
		}
	}
	return bound, ok
}

func countPositional(args []*Arg) int {
	n := 0
	for _, a := range args {
		if a.Name == "" {
			n++
		}
	}
	return n
}

func isLambdaLike(e Expr) bool {
	switch x := e.(type) {
	case *Lambda:
		for _, p := range x.Params {
			if p.Type == nil {
				return true
			}
		}
		return x.Ret == nil
	case *Comp:
		return false
	}
	return false
}

// partialType 代入已知的类型参数；未知部分为 nil。
func partialType(t Type, m Subst, free map[*TypeParam]bool) Type {
	if t == nil {
		return nil
	}
	if !containsTParam(t, free) {
		return t
	}
	t2 := subst(t, m)
	if !containsTParam(t2, free) {
		return t2
	}
	if ft, ok := t2.(*FuncT); ok {
		n := &FuncT{Variadic: ft.Variadic}
		for _, p := range ft.Params {
			if containsTParam(p, free) {
				n.Params = append(n.Params, nil)
			} else {
				n.Params = append(n.Params, p)
			}
		}
		if !containsTParam(ft.Ret, free) {
			n.Ret = ft.Ret
		}
		return n
	}
	return nil
}

// checkCall 检查实参并推断类型参数。pre 是已知的代换（接收者的类型实参），
// infer 是需要推断的类型参数，explicit 是显式给出的类型实参。
func (c *Checker) checkCall(e *Call, sig *FuncSig, pre Subst, infer []*TypeParam, explicit []Type, want Type) Type {
	what := "函数"
	if e.Kind == CallMethod || e.Kind == CallExt {
		what = "方法"
	} else if e.Kind == CallCtor {
		what = "构造函数"
	}
	if e.Func != nil {
		what = "函数 " + e.Func.Name + " "
	} else if e.Method != nil {
		what = "方法 " + e.Method.Name + " "
	} else if e.Ctor != nil {
		what = e.Ctor.D.Name + " 的构造函数"
	}
	bound, ok := c.matchArgs(e, sig, what, false)
	if !ok {
		c.checkArgsQuiet(e)
		return tInvalid
	}
	m := Subst{}
	for k, v := range pre {
		m[k] = v
	}
	free := map[*TypeParam]bool{}
	if explicit != nil {
		if len(explicit) != len(infer) {
			c.errorf(e.Pos, "需要 %d 个类型实参，但给了 %d 个", len(infer), len(explicit))
			return tInvalid
		}
		for i, tp := range infer {
			m[tp] = explicit[i]
		}
	} else {
		for _, tp := range infer {
			free[tp] = true
		}
	}
	// 若期望类型已知，用它辅助推断（例如 s: Stack[int] = Stack()）
	type deferred struct {
		slot *Expr
		pt   Type
	}
	var lambdas, literals []deferred
	ptypeOf := func(i int) Type { return sig.Params[i].T }
	argT := map[*Expr]Type{}
	visit := func(slot *Expr, pt Type) {
		if len(free) > 0 && containsTParam(pt, free) {
			if isLambdaLike(*slot) {
				lambdas = append(lambdas, deferred{slot, pt})
				return
			}
			if isUntypedLit(*slot) {
				literals = append(literals, deferred{slot, pt})
				return
			}
			w := partialType(pt, m, free)
			t := c.expr(*slot, w)
			argT[slot] = t
			c.unify(pt, t, m, free)
			return
		}
		t := c.expr(*slot, subst(pt, m))
		argT[slot] = t
	}
	for i, b := range bound {
		if !b.set {
			continue
		}
		pt := ptypeOf(i)
		if sig.Params[i].Variadic {
			if b.spread {
				visit(b.x, &ListT{pt})
				continue
			}
			for _, a := range b.extras {
				visit(&a.X, pt)
			}
			continue
		}
		visit(b.x, pt)
	}
	for _, d := range literals {
		w := partialType(d.pt, m, free)
		t := c.expr(*d.slot, w)
		argT[d.slot] = t
		c.unify(d.pt, t, m, free)
	}
	for _, d := range lambdas {
		w := partialType(d.pt, m, free)
		t := c.expr(*d.slot, w)
		argT[d.slot] = t
		c.unify(d.pt, t, m, free)
	}
	if len(free) > 0 && want != nil {
		for tp := range free {
			if m[tp] == nil {
				c.unify(sig.Ret, optElemIf(want, sig.Ret), m, free)
				if e.Kind == CallCtor && e.Ctor != nil {
					c.unify(&NamedT{D: e.Ctor.D, Args: tparamTypes(e.Ctor.D.TParams)}, want, m, free)
				}
				break
			}
		}
	}
	for _, tp := range infer {
		if m[tp] == nil {
			d := c.errorf(e.Pos, "无法推断类型参数 %s", tp.Name)
			d.Notes = append(d.Notes, "请显式给出类型实参，例如 f[int](...)")
			return tInvalid
		}
		if isNull(m[tp]) {
			c.errorf(e.Pos, "无法从 null 推断类型参数 %s", tp.Name)
			return tInvalid
		}
	}
	if len(infer) > 0 {
		var args []Type
		for _, tp := range infer {
			args = append(args, m[tp])
		}
		if !c.checkTArgs(infer, args, e.Pos) {
			return tInvalid
		}
		e.TArgs = args
	}
	e.Subst = m
	// 转换实参并整理 Final
	e.Final = make([]Expr, len(sig.Params))
	for i, b := range bound {
		pt := subst(ptypeOf(i), m)
		if !b.set {
			continue
		}
		pname := sig.Params[i].Name
		if sig.Params[i].Variadic {
			if b.spread {
				c.convert(b.x, argT[b.x], &ListT{pt}, "参数 "+pname)
				e.Final[i] = *b.x
				continue
			}
			ll := &ListLit{exprBase: exprBase{Pos: e.Pos, T: &ListT{pt}}}
			for _, a := range b.extras {
				c.convert(&a.X, argT[&a.X], pt, "参数 "+pname)
				ll.Elems = append(ll.Elems, a.X)
			}
			e.Final[i] = ll
			continue
		}
		c.convert(b.x, argT[b.x], pt, "参数 "+pname)
		e.Final[i] = *b.x
	}
	// 没有传的可变参数：空列表
	for i, p := range sig.Params {
		if p.Variadic && !bound[i].set {
			e.Final[i] = &ListLit{exprBase: exprBase{Pos: e.Pos, T: &ListT{subst(p.T, m)}}}
		}
	}
	ret := subst(sig.Ret, m)
	if e.Kind == CallCtor && e.Ctor != nil && len(e.Ctor.Args) == 0 && len(e.Ctor.D.TParams) > 0 {
		e.Ctor = &NamedT{D: e.Ctor.D, Args: e.TArgs}
		return e.Ctor
	}
	if sig.Async && c.taskTD != nil {
		return &NamedT{D: c.taskTD, Args: []Type{taskArg(ret)}}
	}
	return ret
}

func optElemIf(want, ret Type) Type {
	if !isOpt(ret) {
		return optElem(want)
	}
	return want
}

// ===== 方法调用 =====

func (c *Checker) methodCall(e *Call, fn *Attr, rt Type, want Type) Type {
	safe := false
	if o, ok := rt.(*OptT); ok {
		if !fn.Safe {
			d := c.errorf(fn.Pos, "值可能为 null，不能直接调用 .%s()", fn.Name)
			d.Notes = append(d.Notes, "使用 ?. 安全调用，或先判断 if x != null，或用 x!."+fn.Name+"()")
			c.checkArgsQuiet(e)
			return tInvalid
		}
		safe = true
		rt = o.Elem
	} else if fn.Safe {
		c.warnf(fn.Pos, "%s 不可能为 null，?. 可以改成 .", rt)
		fn.Safe = false
	}
	fn.RecvT = rt
	res := c.methodCall0(e, fn, rt, want)
	fn.T = rt
	if safe && !isInvalid(res) {
		if isVoid(res) || isOpt(res) || isAny(res) {
			return res
		}
		return &OptT{res}
	}
	return res
}

func (c *Checker) methodCall0(e *Call, fn *Attr, rt Type, want Type) Type {
	recv := fn.X
	switch r := rt.(type) {
	case *NamedT:
		mem, owner, m := c.findMember(r, fn.Name)
		if mem != nil {
			if !c.privateOK(fn.Name, r.D.Mod) {
				c.errorf(fn.NamePos, "'%s' 是私有成员，只能在模块 %s 中访问", fn.Name, r.D.Mod.Name)
			}
			fn.Owner = owner
			switch mm := mem.(type) {
			case *Method:
				if mm.Static {
					c.errorf(fn.NamePos, "静态方法 %s 请通过类型调用：%s.%s(...)", fn.Name, r.D.Name, fn.Name)
					c.checkArgsQuiet(e)
					return tInvalid
				}
				fn.Kind = AttrMethodValue
				fn.Method = mm
				e.Kind = CallMethod
				e.Method = mm
				e.Recv = recv
				e.Sig = mm.Sig
				if mm.Extern != "" {
					e.Kind = CallMethod
				}
				if r.D.Kind == DStruct {
					c.structCalls = append(c.structCalls, structCall{recv: recv, method: mm, pos: e.Pos})
				}
				return c.checkCall(e, mm.Sig, m, mm.Sig.TParams, nil, want)
			case *Field:
				if mm.Static {
					break
				}
				fn.Kind = AttrField
				fn.Field = mm
				ft := subst(mm.T, m)
				fn.T = ft
				if f, ok := ft.(*FuncT); ok {
					e.Kind = CallValue
					return c.callFuncValue(e, f, want)
				}
				c.errorf(fn.Pos, "字段 %s 的类型是 %s，不能调用", fn.Name, ft)
				c.checkArgsQuiet(e)
				return tInvalid
			case *Prop:
				fn.Kind = AttrProp
				fn.Prop = mm
				pt := subst(mm.T, m)
				if f, ok := pt.(*FuncT); ok {
					e.Kind = CallValue
					return c.callFuncValue(e, f, want)
				}
				c.errorf(fn.Pos, "属性 %s 不是方法，不能调用（去掉括号即可访问）", fn.Name)
				c.checkArgsQuiet(e)
				return tInvalid
			}
		}
		if r.D.Kind == DEnum && !r.D.ADT && (fn.Name == "name" || fn.Name == "value") {
			c.errorf(fn.Pos, "%s 是属性，不需要括号", fn.Name)
			c.checkArgsQuiet(e)
			return tInvalid
		}
	case *TParamT:
		if r.P.Bound == BoundIface {
			fn.RecvT = r
			ms, _, _ := c.ifaceMembers(r.P.Iface)
			if sig := ms[fn.Name]; sig != nil {
				mem, _, _ := c.findIfaceMember(r.P.Iface, fn.Name, map[*TypeDecl]bool{})
				mm, _ := mem.(*Method)
				fn.Kind = AttrMethodValue
				fn.Method = mm
				e.Kind = CallMethod
				e.Method = mm
				e.Recv = recv
				e.Sig = sig
				return c.checkCall(e, sig, nil, nil, nil, want)
			}
		}
	case *Basic:
		if r.Kind == BAny {
			d := c.errorf(fn.NamePos, "any 类型的值没有方法 %s", fn.Name)
			d.Notes = append(d.Notes, "先用 as 转换为具体类型，例如 (x as str)."+fn.Name+"()")
			c.checkArgsQuiet(e)
			return tInvalid
		}
	}
	// 扩展方法
	cands := c.extCandidates(rt, fn.Name)
	var matching []*extCand
	for i := range cands {
		cd := &cands[i]
		if cd.method == nil {
			if cd.prop != nil && len(matching) == 0 {
				pt := subst(cd.prop.T, cd.subst)
				if f, ok := pt.(*FuncT); ok {
					c.noteExtUse(cd, rt, fn.NamePos)
					fn.Kind = AttrProp
					fn.Prop = cd.prop
					e.Kind = CallValue
					return c.callFuncValue(e, f, want)
				}
				c.errorf(fn.Pos, "%s 是属性，不需要括号", fn.Name)
				c.checkArgsQuiet(e)
				return tInvalid
			}
			continue
		}
		if _, ok := c.matchArgs(e, cd.method.Sig, "", true); ok {
			matching = append(matching, cd)
		}
	}
	chosen, rival := c.pickExt(matching)
	if rival != nil {
		c.extAmbiguous(fn.NamePos, rt, fn.Name, chosen, rival)
		c.checkArgsQuiet(e)
		return tInvalid
	}
	if chosen == nil && len(cands) > 0 {
		for i := range cands {
			if cands[i].method != nil {
				chosen = &cands[i]
				break
			}
		}
	}
	if chosen != nil {
		c.noteExtUse(chosen, rt, fn.NamePos)
		mm := chosen.method
		fn.Kind = AttrMethodValue
		fn.Method = mm
		e.Kind = CallExt
		e.Method = mm
		e.Recv = recv
		e.Sig = mm.Sig
		return c.checkCall(e, mm.Sig, chosen.subst, mm.Sig.TParams, nil, want)
	}
	if bad := c.extUnsatisfied(rt, fn.Name); bad != "" {
		c.errorf(fn.NamePos, "%s", bad)
		c.checkArgsQuiet(e)
		return tInvalid
	}
	d := c.errorf(fn.NamePos, "类型 %s 没有方法 %s", rt, fn.Name)
	d.Notes = append(d.Notes, c.extMissingNotes(rt, fn.Name, func(s string) string { return "是不是想用 " + s + "？" })...)
	c.checkArgsQuiet(e)
	return tInvalid
}

func (c *Checker) superCall(e *Call, fn *Attr, want Type) Type {
	f := c.fn
	for f != nil && f.IsLambda {
		f = f.Parent
	}
	if f == nil || f.Owner == nil || f.Owner.Kind != DClass || f.Method == nil {
		c.errorf(fn.Pos, "super() 只能用在类的方法中")
		c.checkArgsQuiet(e)
		return tInvalid
	}
	td := f.Owner
	if td.Base == nil {
		c.errorf(fn.Pos, "类 %s 没有基类", td.Name)
		c.checkArgsQuiet(e)
		return tInvalid
	}
	base := td.Base
	mem, owner, m := c.findMember(base, fn.Name)
	mm, ok := mem.(*Method)
	if !ok || mm.Static {
		c.errorf(fn.NamePos, "基类 %s 中没有方法 %s", base, fn.Name)
		c.checkArgsQuiet(e)
		return tInvalid
	}
	if mm.Abstract {
		c.errorf(fn.NamePos, "基类方法 %s 是抽象方法，不能通过 super() 调用", fn.Name)
	}
	fn.Kind = AttrMethodValue
	fn.Method = mm
	fn.Owner = owner
	fn.X.setTyp(base)
	e.Kind = CallSuper
	e.Method = mm
	e.Sig = mm.Sig
	e.Ctor = owner
	return c.checkCall(e, mm.Sig, m, mm.Sig.TParams, nil, want)
}

// staticCall 处理 Type.member(...) 形式的调用。
func (c *Checker) staticCall(e *Call, fn *Attr, t Type, want Type) Type {
	if nt, ok := t.(*NamedT); ok && nt.D.Kind == DEnum && !nt.D.ADT {
		switch fn.Name {
		case "values":
			if len(e.Args) != 0 {
				c.errorf(e.Pos, "values() 不接受参数")
			}
			e.Kind = CallBuiltin
			e.Builtin = "enum_values"
			e.Ctor = nt
			fn.T = t
			return &ListT{nt}
		case "from_value":
			if len(e.Args) != 1 {
				c.errorf(e.Pos, "from_value 需要一个整数参数")
				return tInvalid
			}
			c.exprTo(&e.Args[0].X, tInt, "枚举值")
			e.Kind = CallBuiltin
			e.Builtin = "enum_from_value"
			e.Ctor = nt
			e.Final = []Expr{e.Args[0].X}
			fn.T = t
			return nt
		}
	}
	ft := c.staticMember(fn, t)
	if isInvalid(ft) {
		c.checkArgsQuiet(e)
		return tInvalid
	}
	switch fn.Kind {
	case AttrVariantCtor:
		return c.variantCall(e, fn, want)
	case AttrStaticMethod:
		e.Kind = CallStatic
		e.Method = fn.Method
		e.Sig = fn.Method.Sig
		var pre Subst
		if fn.Owner != nil {
			pre = makeSubst(fn.Owner.D.TParams, fn.Owner.Args)
		}
		return c.checkCall(e, fn.Method.Sig, pre, fn.Method.Sig.TParams, nil, want)
	case AttrStaticField:
		if f, ok := ft.(*FuncT); ok {
			e.Kind = CallValue
			return c.callFuncValue(e, f, want)
		}
	}
	c.errorf(e.Pos, "%s 不能被调用", ft)
	c.checkArgsQuiet(e)
	return tInvalid
}

func (c *Checker) variantCall(e *Call, fn *Attr, want Type) Type {
	v := fn.Variant
	nt := fn.Owner
	td := v.Owner
	sig := &FuncSig{Ret: &NamedT{D: td, Args: tparamTypes(td.TParams)}}
	for _, f := range v.Fields {
		sig.Params = append(sig.Params, &ParamInfo{Name: f.Name, T: f.T, Default: f.Default, Mod: td.Mod})
	}
	e.Kind = CallVariant
	e.Variant = v
	e.Sig = sig
	if len(td.TParams) > 0 && (nt == nil || len(nt.Args) == 0) {
		// 推断泛型枚举的类型实参：Option.Some(5)
		e.Ctor = &NamedT{D: td}
		r := c.checkCall(e, sig, nil, td.TParams, nil, want)
		if isInvalid(r) {
			return r
		}
		e.Ctor = &NamedT{D: td, Args: e.TArgs}
		return e.Ctor
	}
	e.Ctor = nt
	return c.checkCall(e, sig, makeSubst(td.TParams, nt.Args), nil, nil, want)
}

// typeCall 处理以类型作为被调用者的调用：构造函数或类型转换。
func (c *Checker) typeCall(e *Call, t Type, want Type) Type {
	switch x := t.(type) {
	case *Basic:
		return c.convCall(e, x, want)
	case *NamedT:
		return c.ctorCall(e, x, want)
	case *ListT, *SetT, *DictT:
		// list[int]() 或 list[int](iterable)
		e.Kind = CallEmptyColl
		e.ConvTo = t
		if len(e.Args) == 0 {
			return t
		}
		if len(e.Args) > 1 || e.Args[0].Name != "" {
			c.errorf(e.Pos, "%s(...) 最多接受一个可迭代对象", t)
			c.checkArgsQuiet(e)
			return t
		}
		c.collectionFrom(e, t)
		return t
	case *ChanT:
		e.Kind = CallEmptyColl
		e.ConvTo = t
		if len(e.Args) > 1 {
			c.errorf(e.Pos, "chan[T](容量) 最多一个参数")
		}
		if len(e.Args) == 1 {
			c.exprTo(&e.Args[0].X, tInt, "通道容量")
			e.Final = []Expr{e.Args[0].X}
		}
		return t
	case *TParamT:
		c.errorf(e.Pos, "不能调用类型参数 %s", x.P.Name)
	case *IterT:
		c.errorf(e.Pos, "不能直接创建 iter；请使用生成器函数或推导式")
	default:
		c.errorf(e.Pos, "%s 不能被调用", t)
	}
	c.checkArgsQuiet(e)
	return tInvalid
}

func (c *Checker) collectionFrom(e *Call, t Type) {
	var want Type
	switch x := t.(type) {
	case *ListT:
		want = &IterT{x.Elem}
	case *SetT:
		want = &IterT{x.Elem}
	case *DictT:
		want = &IterT{&TupleT{Elems: []Type{x.Key, x.Val}}}
	}
	at := c.expr(e.Args[0].X, nil)
	if dt, ok := t.(*DictT); ok {
		if src, ok := at.(*DictT); ok && identical(src, dt) {
			e.Final = []Expr{e.Args[0].X}
			e.Extra = "dict"
			return
		}
	}
	c.convert(&e.Args[0].X, at, want, "转换的来源")
	e.Final = []Expr{e.Args[0].X}
}

// genericTypeCall 处理未给出类型实参的泛型类型调用：list(xs)、Stack()、Box(5)。
func (c *Checker) genericTypeCall(e *Call, sym *Symbol, want Type) Type {
	switch sym.Builtin {
	case "list", "set":
		if len(e.Args) == 0 {
			w := optElem(want)
			if sym.Builtin == "list" {
				if lt, ok := w.(*ListT); ok {
					e.Kind = CallEmptyColl
					e.ConvTo = lt
					return lt
				}
			} else if st, ok := w.(*SetT); ok {
				e.Kind = CallEmptyColl
				e.ConvTo = st
				return st
			}
			d := c.errorf(e.Pos, "无法推断 %s() 的元素类型", sym.Name)
			d.Notes = append(d.Notes, fmt.Sprintf("请写成 %s[int]() 或给变量加类型注解", sym.Name))
			return tInvalid
		}
		if len(e.Args) != 1 || e.Args[0].Name != "" {
			c.errorf(e.Pos, "%s(...) 只接受一个可迭代对象", sym.Name)
			c.checkArgsQuiet(e)
			return tInvalid
		}
		at := c.expr(e.Args[0].X, nil)
		if isInvalid(at) {
			return tInvalid
		}
		et := c.elemOf(at, e.Args[0].X.P(), nil)
		if isInvalid(et) {
			return tInvalid
		}
		var rt Type
		if sym.Builtin == "list" {
			rt = &ListT{et}
		} else {
			c.requireHashable(et, e.Pos, "集合元素")
			rt = &SetT{et}
		}
		c.convert(&e.Args[0].X, at, &IterT{et}, "转换的来源")
		e.Kind = CallEmptyColl
		e.ConvTo = rt
		e.Final = []Expr{e.Args[0].X}
		return rt
	case "dict":
		if len(e.Args) == 0 {
			if dt, ok := optElem(want).(*DictT); ok {
				e.Kind = CallEmptyColl
				e.ConvTo = dt
				return dt
			}
			c.errorf(e.Pos, "无法推断 dict() 的类型；请写成 dict[str, int]()")
			return tInvalid
		}
		if len(e.Args) != 1 {
			c.errorf(e.Pos, "dict(...) 只接受一个参数")
			c.checkArgsQuiet(e)
			return tInvalid
		}
		at := c.expr(e.Args[0].X, nil)
		if dt, ok := at.(*DictT); ok {
			e.Kind = CallEmptyColl
			e.ConvTo = dt
			e.Final = []Expr{e.Args[0].X}
			e.Extra = "dict"
			return dt
		}
		et := c.elemOf(at, e.Args[0].X.P(), nil)
		tt, ok := et.(*TupleT)
		if !ok || len(tt.Elems) != 2 {
			if !isInvalid(et) {
				c.errorf(e.Args[0].X.P(), "dict(...) 需要由 (键, 值) 二元组组成的序列，得到 %s 的序列", et)
			}
			return tInvalid
		}
		c.requireHashable(tt.Elems[0], e.Pos, "字典的键")
		dt := &DictT{tt.Elems[0], tt.Elems[1]}
		c.convert(&e.Args[0].X, at, &IterT{tt}, "转换的来源")
		e.Kind = CallEmptyColl
		e.ConvTo = dt
		e.Final = []Expr{e.Args[0].X}
		return dt
	case "chan":
		c.errorf(e.Pos, "创建通道需要写出元素类型，例如 chan[int]() 或 chan[int](10)")
		c.checkArgsQuiet(e)
		return tInvalid
	case "iter":
		c.errorf(e.Pos, "不能直接创建 iter")
		c.checkArgsQuiet(e)
		return tInvalid
	}
	if sym.TD != nil {
		return c.ctorCall(e, &NamedT{D: sym.TD}, want)
	}
	c.errorf(e.Pos, "%s 不能被调用", sym.Name)
	return tInvalid
}

func (c *Checker) ctorCall(e *Call, nt *NamedT, want Type) Type {
	td := nt.D
	switch {
	case td.Kind == DInterface:
		c.errorf(e.Pos, "接口 %s 不能被实例化", td.Name)
	case td.Kind == DEnum:
		d := c.errorf(e.Pos, "枚举 %s 不能这样创建", td.Name)
		if len(td.Variants) > 0 {
			d.Notes = append(d.Notes, fmt.Sprintf("请使用 %s.%s", td.Name, td.Variants[0].Name))
		}
	case td.Kind == DClass && td.Abstract:
		c.errorf(e.Pos, "抽象类 %s 不能被实例化", td.Name)
	case td.Ctor == nil:
		c.errorf(e.Pos, "%s 不能直接创建", td.Name)
	default:
		e.Kind = CallCtor
		e.Ctor = nt
		e.Sig = td.Ctor
		if len(td.TParams) > 0 && len(nt.Args) == 0 {
			if wt, ok := optElem(want).(*NamedT); ok && wt.D == td {
				e.Ctor = wt
				return c.checkCallCtor(e, wt, want)
			}
			return c.checkCall(e, td.Ctor, nil, td.TParams, nil, want)
		}
		return c.checkCallCtor(e, nt, want)
	}
	c.checkArgsQuiet(e)
	return tInvalid
}

func (c *Checker) checkCallCtor(e *Call, nt *NamedT, want Type) Type {
	r := c.checkCall(e, nt.D.Ctor, makeSubst(nt.D.TParams, nt.Args), nil, nil, want)
	if isInvalid(r) {
		return r
	}
	return nt
}

// convCall 处理 int(x)、float(x)、str(x)、bool(x)、char(x) 等类型转换。
func (c *Checker) convCall(e *Call, to *Basic, want Type) Type {
	e.Kind = CallConv
	e.ConvTo = to
	if to.Kind == BAny {
		return c.builtinCall(e, "any", want)
	}
	if to.Kind == BVoid {
		c.errorf(e.Pos, "void 不能被调用")
		return tInvalid
	}
	if len(e.Args) == 0 {
		// int() == 0, str() == "" 等零值
		e.Final = nil
		return to
	}
	if to.Kind == BInt && len(e.Args) == 2 {
		// int(s, base)
		c.exprTo(&e.Args[0].X, tStr, "int 的参数")
		c.exprTo(&e.Args[1].X, tInt, "进制")
		e.Final = []Expr{e.Args[0].X, e.Args[1].X}
		e.Extra = "base"
		return to
	}
	if len(e.Args) != 1 || e.Args[0].Name != "" {
		c.errorf(e.Pos, "%s(...) 只接受一个参数", to)
		c.checkArgsQuiet(e)
		return to
	}
	at := c.expr(e.Args[0].X, numericWant(to))
	e.Final = []Expr{e.Args[0].X}
	if isInvalid(at) {
		return to
	}
	from := at
	ok := false
	switch to.Kind {
	case BStr:
		ok = !isVoid(from)
		switch from.(type) {
		case *MetaT, *ModT, *GenericT, *FuncRefT:
			ok = false
		}
	case BBool:
		ok = isNumeric(from) || isBool(from) || isStr(from) || isOpt(from)
		switch from.(type) {
		case *ListT, *DictT, *SetT:
			ok = true
		}
	case BChar:
		ok = isInteger(from) || basicKind(from) == BChar || isStr(from)
	default:
		if isNumeric(to) {
			ok = isNumeric(from) || isStr(from) || isBool(from) || basicKind(from) == BChar
			if d := namedDecl(from); d != nil && d.Kind == DEnum && !d.ADT && isInteger(to) {
				ok = true
			}
		}
	}
	if !ok {
		c.errorf(e.Args[0].X.P(), "不能把 %s 转换为 %s", from, to)
	}
	return to
}

// ===== 内置函数 =====

func (c *Checker) builtinCall(e *Call, name string, want Type) Type {
	e.Kind = CallBuiltin
	e.Builtin = name
	args := e.Args
	kw := map[string]*Arg{}
	var pos []*Arg
	for _, a := range args {
		if a.Star {
			c.errorf(a.Pos, "内置函数 %s 不支持 *展开", name)
			continue
		}
		if a.Name != "" {
			kw[a.Name] = a
		} else {
			pos = append(pos, a)
		}
	}
	allowKw := func(names ...string) {
		for k, a := range kw {
			found := false
			for _, n := range names {
				if n == k {
					found = true
				}
			}
			if !found {
				c.errorf(a.Pos, "%s 没有名为 %s 的参数", name, k)
			}
		}
	}
	nargs := func(lo, hi int) bool {
		if len(pos) < lo || len(pos) > hi {
			if lo == hi {
				c.errorf(e.Pos, "%s 需要 %d 个参数，但给了 %d 个", name, lo, len(pos))
			} else {
				c.errorf(e.Pos, "%s 需要 %d 到 %d 个参数，但给了 %d 个", name, lo, hi, len(pos))
			}
			c.checkArgsQuiet(e)
			return false
		}
		return true
	}
	e.Final = nil
	switch name {
	case "print":
		allowKw("sep", "end")
		for _, a := range pos {
			t := c.expr(a.X, nil)
			c.validElem(t, a.X.P(), "print 的参数")
			e.Final = append(e.Final, a.X)
		}
		for _, k := range []string{"sep", "end"} {
			if a := kw[k]; a != nil {
				c.exprTo(&a.X, tStr, k)
			}
		}
		e.Extra = kw
		return tVoid
	case "len":
		allowKw()
		if !nargs(1, 1) {
			return tInt
		}
		t := c.expr(pos[0].X, nil)
		e.Final = []Expr{pos[0].X}
		switch x := t.(type) {
		case *ListT, *DictT, *SetT, *ChanT:
			return tInt
		case *TupleT:
			return tInt
		case *Basic:
			if x.Kind == BStr {
				return tInt
			}
		case *NamedT:
			if m, _ := c.operatorMethod(t, "__len__"); m != nil {
				e.Method = m
				return tInt
			}
		case *OptT:
			c.errorf(pos[0].X.P(), "值可能为 null，请先判断")
			return tInt
		}
		if !isInvalid(t) {
			c.errorf(pos[0].X.P(), "%s 没有长度（len 用于 str、list、dict、set 或实现了 __len__ 的类型）", t)
		}
		return tInt
	case "range":
		allowKw()
		et := c.rangeArgs(e)
		for _, a := range e.Args {
			e.Final = append(e.Final, a.X)
		}
		return &IterT{et}
	case "abs":
		allowKw()
		if !nargs(1, 1) {
			return tInvalid
		}
		t := c.expr(pos[0].X, numericWant(want))
		e.Final = []Expr{pos[0].X}
		if !isNumeric(t) && !isInvalid(t) {
			c.errorf(pos[0].X.P(), "abs 需要数字，得到 %s", t)
		}
		return t
	case "min", "max":
		return c.minMax(e, name, pos, kw, want)
	case "sum":
		allowKw("start")
		if !nargs(1, 1) {
			return tInvalid
		}
		at := c.expr(pos[0].X, nil)
		et := c.elemOf(at, pos[0].X.P(), nil)
		if isInvalid(et) {
			return tInvalid
		}
		if !isNumeric(et) {
			if _, ok := et.(*ListT); !ok || kw["start"] == nil {
				c.errorf(pos[0].X.P(), "sum 需要数字序列，得到 %s 的序列", et)
				return tInvalid
			}
		}
		c.convert(&pos[0].X, at, &IterT{et}, "sum 的参数")
		e.Final = []Expr{pos[0].X}
		if a := kw["start"]; a != nil {
			c.exprTo(&a.X, et, "start")
			e.Final = append(e.Final, a.X)
		}
		return et
	case "sorted":
		allowKw("key", "reverse")
		if !nargs(1, 1) {
			return tInvalid
		}
		at := c.expr(pos[0].X, nil)
		et := c.elemOf(at, pos[0].X.P(), nil)
		if isInvalid(et) {
			return tInvalid
		}
		c.convert(&pos[0].X, at, &IterT{et}, "sorted 的参数")
		e.Final = []Expr{pos[0].X, nil, nil}
		if a := kw["key"]; a != nil {
			kt := c.keyFunc(a, et)
			e.TArgs = []Type{et, kt}
			e.Final[1] = a.X
		} else if !isOrdered(et) && !c.hasLess(et) {
			if _, ok := et.(*TupleT); !ok {
				d := c.errorf(pos[0].X.P(), "元素类型 %s 不能直接排序", et)
				if note := c.lessNote(et); note != "" {
					d.Notes = append(d.Notes, note)
				} else {
					d.Notes = append(d.Notes, "请提供 key，例如 sorted(xs, key=x => x.age)，或为类型定义 __lt__")
				}
			}
		}
		if a := kw["reverse"]; a != nil {
			c.exprTo(&a.X, tBool, "reverse")
			e.Final[2] = a.X
		}
		return &ListT{et}
	case "reversed":
		allowKw()
		if !nargs(1, 1) {
			return tInvalid
		}
		at := c.expr(pos[0].X, nil)
		et := c.elemOf(at, pos[0].X.P(), nil)
		if isInvalid(et) {
			return tInvalid
		}
		c.convert(&pos[0].X, at, &IterT{et}, "reversed 的参数")
		e.Final = []Expr{pos[0].X}
		return &ListT{et}
	case "enumerate":
		allowKw("start")
		if !nargs(1, 2) {
			return tInvalid
		}
		at := c.expr(pos[0].X, nil)
		et := c.elemOf(at, pos[0].X.P(), nil)
		if isInvalid(et) {
			return tInvalid
		}
		raw := pos[0].X
		conv := raw
		c.convert(&conv, at, &IterT{et}, "enumerate 的参数")
		e.Args = []*Arg{{Pos: pos[0].Pos, X: raw}}
		e.Final = []Expr{conv, nil}
		var start *Arg
		if len(pos) == 2 {
			start = pos[1]
		} else if a := kw["start"]; a != nil {
			start = a
		}
		if start != nil {
			c.exprTo(&start.X, tInt, "start")
			e.Final[1] = start.X
			e.Args = append(e.Args, start)
		}
		return &IterT{&TupleT{Elems: []Type{tInt, et}}}
	case "zip":
		allowKw()
		if !nargs(2, 3) {
			return tInvalid
		}
		var elems []Type
		var raws []*Arg
		for _, a := range pos {
			at := c.expr(a.X, nil)
			et := c.elemOf(at, a.X.P(), nil)
			if isInvalid(et) {
				return tInvalid
			}
			raw := a.X
			conv := raw
			c.convert(&conv, at, &IterT{et}, "zip 的参数")
			e.Final = append(e.Final, conv)
			raws = append(raws, &Arg{Pos: a.Pos, X: raw})
			elems = append(elems, et)
		}
		e.Args = raws
		return &IterT{&TupleT{Elems: elems}}
	case "any", "all":
		allowKw()
		if !nargs(1, 1) {
			return tBool
		}
		at := c.expr(pos[0].X, nil)
		et := c.elemOf(at, pos[0].X.P(), nil)
		if isInvalid(et) {
			return tBool
		}
		if !isBool(et) {
			d := c.errorf(pos[0].X.P(), "%s 需要 bool 序列，得到 %s 的序列", name, et)
			d.Notes = append(d.Notes, "例如 "+name+"(x > 0 for x in xs)")
			return tBool
		}
		c.convert(&pos[0].X, at, &IterT{tBool}, name+" 的参数")
		e.Final = []Expr{pos[0].X}
		return tBool
	case "input":
		allowKw("prompt")
		if !nargs(0, 1) {
			return tStr
		}
		if len(pos) == 1 {
			c.exprTo(&pos[0].X, tStr, "提示")
			e.Final = []Expr{pos[0].X}
		} else if a := kw["prompt"]; a != nil {
			c.exprTo(&a.X, tStr, "提示")
			e.Final = []Expr{a.X}
		}
		return tStr
	case "repr", "type_name":
		allowKw()
		if !nargs(1, 1) {
			return tStr
		}
		t := c.expr(pos[0].X, nil)
		c.validElem(t, pos[0].X.P(), name+" 的参数")
		e.Final = []Expr{pos[0].X}
		return tStr
	case "ord":
		allowKw()
		if !nargs(1, 1) {
			return tInt
		}
		t := c.expr(pos[0].X, tChar)
		if basicKind(t) != BChar && !isStr(t) && !isInvalid(t) {
			c.errorf(pos[0].X.P(), "ord 需要字符，得到 %s", t)
		}
		e.Final = []Expr{pos[0].X}
		return tInt
	case "chr":
		allowKw()
		if !nargs(1, 1) {
			return tStr
		}
		c.exprTo(&pos[0].X, tInt, "chr 的参数")
		e.Final = []Expr{pos[0].X}
		return tStr
	case "hex", "bin", "oct":
		allowKw()
		if !nargs(1, 1) {
			return tStr
		}
		t := c.expr(pos[0].X, tInt)
		if !isInteger(t) && !isInvalid(t) {
			c.errorf(pos[0].X.P(), "%s 需要整数，得到 %s", name, t)
		}
		e.Final = []Expr{pos[0].X}
		return tStr
	case "round":
		allowKw("ndigits")
		if !nargs(1, 2) {
			return tInt
		}
		t := c.expr(pos[0].X, nil)
		e.Final = []Expr{pos[0].X}
		if !isNumeric(t) && !isInvalid(t) {
			c.errorf(pos[0].X.P(), "round 需要数字，得到 %s", t)
		}
		c.convert(&e.Final[0], t, tFloat, "round 的参数")
		var nd *Arg
		if len(pos) == 2 {
			nd = pos[1]
		} else if a := kw["ndigits"]; a != nil {
			nd = a
		}
		if nd != nil {
			c.exprTo(&nd.X, tInt, "ndigits")
			e.Final = append(e.Final, nd.X)
			return tFloat
		}
		return tInt
	case "divmod":
		allowKw()
		if !nargs(2, 2) {
			return tInvalid
		}
		a := c.expr(pos[0].X, nil)
		b := c.expr(pos[1].X, numericWant(a))
		j := c.join(a, b)
		if j == nil || !isNumeric(j) {
			c.errorf(e.Pos, "divmod 需要两个数字")
			return tInvalid
		}
		c.convert(&pos[0].X, a, j, "divmod")
		c.convert(&pos[1].X, b, j, "divmod")
		e.Final = []Expr{pos[0].X, pos[1].X}
		return &TupleT{Elems: []Type{j, j}}
	case "open":
		allowKw("mode", "encoding")
		if !nargs(1, 2) {
			return tInvalid
		}
		c.exprTo(&pos[0].X, tStr, "文件路径")
		e.Final = []Expr{pos[0].X, nil}
		var mode *Arg
		if len(pos) == 2 {
			mode = pos[1]
		} else {
			mode = kw["mode"]
		}
		if mode != nil {
			c.exprTo(&mode.X, tStr, "打开模式")
			e.Final[1] = mode.X
		}
		if a := kw["encoding"]; a != nil {
			c.exprTo(&a.X, tStr, "encoding")
		}
		sym := c.universe.lookup("File")
		if sym == nil || sym.TD == nil {
			c.errorf(e.Pos, "内部错误：找不到 File 类型")
			return tInvalid
		}
		return &NamedT{D: sym.TD}
	case "exit":
		allowKw()
		if !nargs(0, 1) {
			return tVoid
		}
		if len(pos) == 1 {
			c.exprTo(&pos[0].X, tInt, "退出码")
			e.Final = []Expr{pos[0].X}
		}
		return tVoid
	case "isinstance":
		allowKw()
		if !nargs(2, 2) {
			return tBool
		}
		te, ok := exprToTypeExpr(pos[1].X)
		if !ok {
			c.errorf(pos[1].X.P(), "isinstance 的第二个参数必须是类型")
			return tBool
		}
		ie := &IsExpr{exprBase: exprBase{Pos: e.Pos}, X: pos[0].X, Type: te}
		c.expr(ie, nil)
		e.Extra = ie
		return tBool
	case "hash":
		c.errorf(e.Pos, "五行不提供 hash()；字典和集合会自动处理哈希")
		c.checkArgsQuiet(e)
		return tInt
	case "panic":
		c.errorf(e.Pos, "请使用 raise RuntimeError(\"...\") 抛出异常")
		c.checkArgsQuiet(e)
		return tVoid
	}
	c.errorf(e.Pos, "未知的内置函数 %s", name)
	c.checkArgsQuiet(e)
	return tInvalid
}

// hasLess 判断类型是否定义了 __lt__(self, other: 同类型) -> bool，即实现了 Comparable[T]。
// 类型参数要有这样的接口约束（例如 T: Comparable[T]）。这样的类型可以直接排序、求最值。
func (c *Checker) hasLess(t Type) bool {
	m, sig := c.orderMethod(t, "__lt__")
	return m != nil && len(sig.Params) == 1 && identical(sig.Params[0].T, t) && isBool(sig.Ret)
}

// lessNote 当 t 有 __lt__、但参数类型不是 t 本身（例如从基类继承的 __lt__）时，返回说明。
func (c *Checker) lessNote(t Type) string {
	m, sig := c.orderMethod(t, "__lt__")
	if m == nil || len(sig.Params) != 1 || identical(sig.Params[0].T, t) {
		return ""
	}
	pt := sig.Params[0].T
	return fmt.Sprintf("%s 的 __lt__ 的参数类型是 %s，而直接排序、求最值要求它就是元素类型本身；可以把元素类型写成 %s（例如 list[%s]），或者提供 key", t, pt, pt, pt)
}

// keyFunc 检查 key= 参数，返回键类型。
func (c *Checker) keyFunc(a *Arg, et Type) Type {
	want := &FuncT{Params: []Type{et}}
	t := c.expr(a.X, want)
	ft, ok := t.(*FuncT)
	if !ok || len(ft.Params) != 1 {
		if !isInvalid(t) {
			c.errorf(a.X.P(), "key 需要一个单参数函数，例如 key=x => x.age")
		}
		return tInvalid
	}
	if !identical(ft.Params[0], et) {
		c.errorf(a.X.P(), "key 函数的参数类型应为 %s", et)
	}
	if !isOrdered(ft.Ret) {
		if tt, ok := ft.Ret.(*TupleT); ok {
			for _, el := range tt.Elems {
				if !isOrdered(el) {
					c.errorf(a.X.P(), "key 返回的元组中 %s 不可比较", el)
				}
			}
			return ft.Ret
		}
		if !isInvalid(ft.Ret) {
			c.errorf(a.X.P(), "key 函数必须返回可比较的值（数字、str、char 或它们组成的元组），得到 %s", ft.Ret)
		}
		return tInvalid
	}
	return ft.Ret
}

func (c *Checker) minMax(e *Call, name string, pos []*Arg, kw map[string]*Arg, want Type) Type {
	for k, a := range kw {
		if k != "key" && k != "default" {
			c.errorf(a.Pos, "%s 没有名为 %s 的参数", name, k)
		}
	}
	if len(pos) == 0 {
		c.errorf(e.Pos, "%s 至少需要一个参数", name)
		return tInvalid
	}
	if len(pos) == 1 {
		// min(iterable)
		at := c.expr(pos[0].X, nil)
		et := c.elemOf(at, pos[0].X.P(), nil)
		if isInvalid(et) {
			return tInvalid
		}
		c.convert(&pos[0].X, at, &IterT{et}, name+" 的参数")
		e.Final = []Expr{pos[0].X, nil, nil}
		e.Extra = "iter"
		if a := kw["key"]; a != nil {
			kt := c.keyFunc(a, et)
			e.TArgs = []Type{et, kt}
			e.Final[1] = a.X
		} else if !isOrdered(et) && !c.hasLess(et) {
			if _, ok := et.(*TupleT); !ok {
				d := c.errorf(pos[0].X.P(), "元素类型 %s 不可比较；请提供 key= 或定义 __lt__", et)
				if note := c.lessNote(et); note != "" {
					d.Notes = append(d.Notes, note)
				}
			}
		}
		if a := kw["default"]; a != nil {
			c.exprTo(&a.X, et, "default")
			e.Final[2] = a.X
		}
		return et
	}
	if kw["default"] != nil {
		c.errorf(kw["default"].Pos, "default 只能在 %s(可迭代对象) 中使用", name)
	}
	var exprs []Expr
	for _, a := range pos {
		exprs = append(exprs, a.X)
	}
	et := c.inferElems(exprs, name+" 的参数")
	if isInvalid(et) {
		return tInvalid
	}
	for i := range pos {
		pos[i].X = exprs[i]
	}
	e.Final = exprs
	e.Extra = "args"
	if a := kw["key"]; a != nil {
		kt := c.keyFunc(a, et)
		e.TArgs = []Type{et, kt}
		e.Extra = a.X
	} else if !isOrdered(et) && !c.hasLess(et) {
		if _, ok := et.(*TupleT); !ok {
			d := c.errorf(e.Pos, "%s 的参数类型 %s 不可比较；请提供 key= 或定义 __lt__", name, et)
			if note := c.lessNote(et); note != "" {
				d.Notes = append(d.Notes, note)
			}
		}
	}
	return et
}
