package compiler

import (
	"sort"
	"strings"
)

func (c *Checker) matchStmt(s *MatchStmt) {
	st := c.expr(s.Subject, nil)
	s.SubjT = st
	if isVoid(st) {
		c.errorf(s.Subject.P(), "这个表达式没有值，不能用于 match")
		st = tInvalid
	}
	entry := c.flow.copy()
	var flows []*flowState
	exhaustive := false
	covered := map[*Variant]bool{}
	bools := map[bool]bool{}
	nullCovered := false
	listLens := map[int]bool{}
	listMin := -1
	for i, cs := range s.Cases {
		if exhaustive {
			c.warnf(cs.Pos, "前面的 case 已经匹配了所有情况，这个 case 永远不会执行")
		}
		c.flow = entry.copy()
		c.pushScope()
		c.pattern(cs.Pat, st)
		if cs.Guard != nil {
			thenN, _ := c.cond(&cs.Guard)
			c.applyNarrow(thenN)
		}
		c.stmts(cs.Body)
		c.popScope()
		flows = append(flows, c.flow)
		if cs.Guard == nil {
			if irrefutable(cs.Pat) {
				exhaustive = true
			}
			collectCovered(cs.Pat, covered, bools, &nullCovered)
			if lp, ok := cs.Pat.(*ListPat); ok {
				all := true
				for _, e := range lp.Elems {
					if !irrefutable(e) {
						all = false
					}
				}
				if all {
					if lp.RestIdx >= 0 {
						if listMin < 0 || len(lp.Elems) < listMin {
							listMin = len(lp.Elems)
						}
					} else {
						listLens[len(lp.Elems)] = true
					}
				}
				if listMin >= 0 {
					full := true
					for k := 0; k < listMin; k++ {
						if !listLens[k] {
							full = false
						}
					}
					if full {
						exhaustive = true
					}
				}
			}
		}
		_ = i
	}
	if !exhaustive && !isInvalid(st) {
		base := st
		if o, ok := st.(*OptT); ok {
			base = o.Elem
			if !nullCovered {
				base = nil
			}
		}
		if d := namedDecl(base); d != nil && d.Kind == DEnum {
			var missing []string
			for _, v := range d.Variants {
				if !covered[v] {
					missing = append(missing, v.Name)
				}
			}
			if len(missing) == 0 {
				exhaustive = true
			} else if len(missing) < len(d.Variants) {
				sort.Strings(missing)
				c.warnf(s.Pos, "match 没有覆盖 %s 的成员：%s（可以加 case _ 处理其余情况）", d.Name, strings.Join(missing, "、"))
			}
		}
		if isBool(base) && bools[true] && bools[false] {
			exhaustive = true
		}
	}
	if exhaustive {
		c.flow = mergeFlows(flows...)
	} else {
		c.flow = mergeFlows(append(flows, entry)...)
	}
	s.Exhaustive = exhaustive
}

func irrefutable(p Pattern) bool {
	switch x := p.(type) {
	case *WildPat:
		return true
	case *NamePat:
		return x.Kind == patCapture
	case *AsPat:
		return irrefutable(x.Pat)
	case *OrPat:
		for _, a := range x.Alts {
			if irrefutable(a) {
				return true
			}
		}
	case *TuplePat:
		for _, e := range x.Elems {
			if !irrefutable(e) {
				return false
			}
		}
		return true
	case *CallPat:
		if !x.Total {
			return false
		}
		for _, a := range x.Args {
			if !irrefutable(a) {
				return false
			}
		}
		return true
	case *TypePat:
		return x.Total
	}
	return false
}

func collectCovered(p Pattern, covered map[*Variant]bool, bools map[bool]bool, null *bool) {
	switch x := p.(type) {
	case *NamePat:
		if x.Kind == patVariant && x.Variant != nil {
			covered[x.Variant] = true
		}
	case *CallPat:
		if x.Variant != nil {
			all := true
			for _, a := range x.Args {
				if !irrefutable(a) {
					all = false
				}
			}
			if all {
				covered[x.Variant] = true
			}
		}
	case *OrPat:
		for _, a := range x.Alts {
			collectCovered(a, covered, bools, null)
		}
	case *AsPat:
		collectCovered(x.Pat, covered, bools, null)
	case *LitPat:
		switch l := x.X.(type) {
		case *BoolLit:
			bools[l.Val] = true
		case *NullLit:
			*null = true
		}
	}
}

// bindPattern 声明模式中绑定的变量。
func (c *Checker) bindPat(name string, t Type, pos Pos) *Symbol {
	if name == "_" {
		return nil
	}
	if c.scope.Syms[name] != nil {
		c.errorf(pos, "模式中的名字 %s 重复绑定", name)
	}
	sym := c.declareLocal(name, t, pos, true)
	sym.Assigns = 1
	c.flow.assigned[sym] = true
	return sym
}

// testable 判断能否对类型 t 的值做运行时类型测试。
func testable(t Type) bool {
	t = optElem(t)
	if isAny(t) {
		return true
	}
	d := namedDecl(t)
	return d != nil && (d.Kind == DClass || d.Kind == DInterface)
}

func (c *Checker) pattern(p Pattern, t Type) {
	if isInvalid(t) {
		c.bindAllInvalid(p)
		return
	}
	switch x := p.(type) {
	case *WildPat:
	case *NamePat:
		c.namePattern(x, t)
	case *LitPat:
		c.litPattern(x, t)
	case *OrPat:
		for _, a := range x.Alts {
			if patBinds(a) {
				c.errorf(a.P(), "或模式（|）中不能绑定变量")
			}
			c.pattern(a, t)
		}
	case *TuplePat:
		tt, ok := t.(*TupleT)
		if !ok {
			c.errorf(x.Pos, "元组模式不能匹配 %s 类型的值", t)
			c.bindAllInvalid(p)
			return
		}
		if len(tt.Elems) != len(x.Elems) {
			c.errorf(x.Pos, "元组模式有 %d 个元素，但值是 %d 元组", len(x.Elems), len(tt.Elems))
			c.bindAllInvalid(p)
			return
		}
		x.TT = tt
		for i, e := range x.Elems {
			c.pattern(e, tt.Elems[i])
		}
	case *ListPat:
		lt, ok := t.(*ListT)
		if !ok {
			c.errorf(x.Pos, "列表模式不能匹配 %s 类型的值", t)
			c.bindAllInvalid(p)
			return
		}
		x.Elem = lt.Elem
		for _, e := range x.Elems {
			c.pattern(e, lt.Elem)
		}
		if x.RestIdx >= 0 && x.RestName != "" && x.RestName != "_" {
			x.RestSym = c.bindPat(x.RestName, lt, x.Pos)
		}
	case *CallPat:
		c.callPattern(x, t)
	case *TypePat:
		to := c.resolveType(x.Type, c.scope)
		x.To = to
		if !isInvalid(to) {
			c.checkTypeTest(x.Pos, t, to)
			x.Total = identical(t, to)
		}
		x.Sym = c.bindPat(x.Name, to, x.Pos)
	case *AsPat:
		c.pattern(x.Pat, t)
		bt := t
		switch inner := x.Pat.(type) {
		case *TypePat:
			bt = inner.To
		case *NamePat:
			if inner.Kind == patType {
				bt = inner.Type
			}
		case *CallPat:
			if inner.TD != nil && inner.Variant == nil {
				bt = inner.TD
			}
		}
		x.Sym = c.bindPat(x.Name, bt, x.Pos)
	}
}

func patBinds(p Pattern) bool {
	switch x := p.(type) {
	case *NamePat:
		return x.Kind == patCapture || (x.Kind == 0 && len(x.Names) == 1 && x.Names[0] != "_" && strings.ToLower(x.Names[0][:1]) == x.Names[0][:1])
	case *TypePat, *AsPat:
		return true
	case *TuplePat:
		for _, e := range x.Elems {
			if patBinds(e) {
				return true
			}
		}
	case *ListPat:
		if x.RestIdx >= 0 && x.RestName != "_" {
			return true
		}
		for _, e := range x.Elems {
			if patBinds(e) {
				return true
			}
		}
	case *CallPat:
		for _, a := range x.Args {
			if patBinds(a) {
				return true
			}
		}
	}
	return false
}

func (c *Checker) bindAllInvalid(p Pattern) {
	switch x := p.(type) {
	case *NamePat:
		if len(x.Names) == 1 && x.Names[0] != "_" {
			if sym := c.scope.lookup(x.Names[0]); sym == nil || (sym.Kind != SType && sym.Kind != SConst) {
				x.Kind = patCapture
				x.Sym = c.bindPat(x.Names[0], tInvalid, x.Pos)
			}
		}
	case *TypePat:
		x.Sym = c.bindPat(x.Name, tInvalid, x.Pos)
	case *AsPat:
		c.bindAllInvalid(x.Pat)
		x.Sym = c.bindPat(x.Name, tInvalid, x.Pos)
	case *TuplePat:
		for _, e := range x.Elems {
			c.bindAllInvalid(e)
		}
	case *ListPat:
		for _, e := range x.Elems {
			c.bindAllInvalid(e)
		}
		if x.RestIdx >= 0 && x.RestName != "_" {
			x.RestSym = c.bindPat(x.RestName, tInvalid, x.Pos)
		}
	case *CallPat:
		for _, a := range x.Args {
			c.bindAllInvalid(a)
		}
	case *OrPat:
	}
}

func (c *Checker) checkTypeTest(pos Pos, from, to Type) {
	if !testable(from) {
		if identical(optElem(from), to) {
			return
		}
		c.errorf(pos, "不能对 %s 类型的值做类型匹配（只能用于 any、类和接口）", from)
		return
	}
	fd, td := namedDecl(optElem(from)), namedDecl(to)
	if fd != nil && td != nil && fd.Kind == DClass && td.Kind == DClass && !isSubclassDecl(td, fd) && !isSubclassDecl(fd, td) {
		c.errorf(pos, "%s 不可能是 %s", from, to)
	}
}

func (c *Checker) namePattern(x *NamePat, t Type) {
	base := optElem(t)
	if len(x.Names) == 1 {
		name := x.Names[0]
		if d := namedDecl(base); d != nil && d.Kind == DEnum {
			if v := d.VariantMap[name]; v != nil {
				x.Kind = patVariant
				x.Variant = v
				x.Type = base
				return
			}
		}
		sym := c.scope.lookup(name)
		if sym != nil {
			switch sym.Kind {
			case SType:
				var to Type
				if sym.AliasDecl != nil {
					c.resolveAlias(sym)
					if len(sym.AliasTPs) > 0 {
						c.errorf(x.Pos, "泛型类型别名 %s 的类型模式需要类型实参", name)
						return
					}
					if isInvalid(sym.Alias) {
						return
					}
					to = sym.Alias
				} else if sym.Alias != nil {
					to = sym.Alias
				} else if sym.TD != nil {
					if len(sym.TD.TParams) > 0 {
						c.errorf(x.Pos, "泛型类型 %s 的类型模式需要类型实参", name)
						return
					}
					to = &NamedT{D: sym.TD}
				} else {
					c.errorf(x.Pos, "%s 需要类型实参", name)
					return
				}
				x.Kind = patType
				x.Type = to
				c.checkTypeTest(x.Pos, t, to)
				return
			case SConst:
				id := &Ident{exprBase: exprBase{Pos: x.Pos}, Name: name}
				x.Value = id
				c.exprTo(&x.Value, base, "模式中的常量")
				x.Kind = patValue
				return
			}
		}
		if len(name) > 0 && strings.ToUpper(name[:1]) == name[:1] && name[:1] != "_" && isEnumLike(base) {
			c.errorf(x.Pos, "%s 中没有成员 %s", base, name)
		}
		x.Kind = patCapture
		x.Sym = c.bindPat(name, t, x.Pos)
		return
	}
	// 点分名字：Color.Red、mod.CONST
	var e Expr = &Ident{exprBase: exprBase{Pos: x.Pos}, Name: x.Names[0]}
	for _, n := range x.Names[1:] {
		e = &Attr{exprBase: exprBase{Pos: x.Pos}, X: e, Name: n, NamePos: x.Pos}
	}
	vt := c.expr(e, nil)
	if isInvalid(vt) {
		return
	}
	if a, ok := e.(*Attr); ok && a.Kind == AttrEnumCase {
		if !identical(optElem(t), vt) {
			c.errorf(x.Pos, "模式 %s 的类型是 %s，但值的类型是 %s", strings.Join(x.Names, "."), vt, t)
			return
		}
		x.Kind = patVariant
		x.Variant = a.Variant
		x.Type = vt
		return
	}
	if a, ok := e.(*Attr); ok && a.Kind == AttrVariantCtor {
		x.Kind = patVariant
		x.Variant = a.Variant
		x.Type = base
		return
	}
	if mt, ok := vt.(*MetaT); ok {
		x.Kind = patType
		x.Type = mt.T
		c.checkTypeTest(x.Pos, t, mt.T)
		return
	}
	x.Value = e
	c.convert(&x.Value, vt, base, "模式中的值")
	x.Kind = patValue
}

func isEnumLike(t Type) bool {
	d := namedDecl(t)
	return d != nil && d.Kind == DEnum
}

func (c *Checker) litPattern(x *LitPat, t Type) {
	base := optElem(t)
	if _, ok := x.X.(*NullLit); ok {
		c.expr(x.X, nil)
		if !isOpt(t) && !isAny(t) {
			c.warnf(x.Pos, "%s 不可能为 null", t)
		}
		return
	}
	if isAny(base) {
		c.expr(x.X, nil)
		return
	}
	lt := c.expr(x.X, base)
	if isInvalid(lt) {
		return
	}
	if !c.convert(&x.X, lt, base, "模式中的字面量") {
		return
	}
	if _, ok := lt.(*FuncT); ok {
		c.errorf(x.Pos, "函数不能用于模式匹配")
	}
}

func (c *Checker) callPattern(x *CallPat, t Type) {
	base := optElem(t)
	// 枚举变体
	var v *Variant
	var ent *NamedT
	if len(x.Names) == 1 {
		if d := namedDecl(base); d != nil && d.Kind == DEnum {
			v = d.VariantMap[x.Names[0]]
			ent = base.(*NamedT)
		}
	}
	if v == nil && len(x.Names) >= 2 {
		var e Expr = &Ident{exprBase: exprBase{Pos: x.Pos}, Name: x.Names[0]}
		for _, n := range x.Names[1:] {
			e = &Attr{exprBase: exprBase{Pos: x.Pos}, X: e, Name: n, NamePos: x.Pos}
		}
		vt := c.expr(e, nil)
		if a, ok := e.(*Attr); ok && (a.Kind == AttrVariantCtor || a.Kind == AttrEnumCase) {
			v = a.Variant
			if bn, ok := base.(*NamedT); ok && bn.D == v.Owner {
				ent = bn
			} else {
				c.errorf(x.Pos, "成员 %s 属于 %s，但值的类型是 %s", strings.Join(x.Names, "."), v.Owner.Name, t)
				c.bindAllInvalid(x)
				return
			}
		} else if mt, ok := vt.(*MetaT); ok {
			if nt, ok := mt.T.(*NamedT); ok {
				c.classPattern(x, t, nt)
				return
			}
		}
		if v == nil {
			if !isInvalid(vt) {
				c.errorf(x.Pos, "%s 不能用作模式", strings.Join(x.Names, "."))
			}
			c.bindAllInvalid(x)
			return
		}
	}
	if v != nil {
		x.Variant = v
		x.TD = ent
		m := makeSubst(v.Owner.TParams, ent.Args)
		if len(x.Args) > len(v.Fields) {
			c.errorf(x.Pos, "成员 %s 只有 %d 个字段，但模式中有 %d 个", v.Name, len(v.Fields), len(x.Args))
			c.bindAllInvalid(x)
			return
		}
		c.subPatterns(x, v.Fields, m)
		return
	}
	// 类/结构体模式：Point(x, y)
	sym := c.scope.lookup(x.Names[0])
	if sym != nil && sym.AliasDecl != nil {
		c.resolveAlias(sym)
		if nt, ok := sym.Alias.(*NamedT); ok && len(sym.AliasTPs) == 0 {
			c.classPattern(x, t, nt)
			return
		}
	}
	if sym == nil || sym.Kind != SType || sym.TD == nil {
		if sym == nil {
			c.errorf(x.Pos, "未定义的名字 %s", x.Names[0])
		} else {
			c.errorf(x.Pos, "%s 不能用作模式", x.Names[0])
		}
		c.bindAllInvalid(x)
		return
	}
	nt := &NamedT{D: sym.TD}
	if len(sym.TD.TParams) > 0 {
		if bn, ok := base.(*NamedT); ok && bn.D == sym.TD {
			nt = bn
		} else {
			c.errorf(x.Pos, "泛型类型 %s 只能匹配同类型的值", sym.TD.Name)
			c.bindAllInvalid(x)
			return
		}
	}
	c.classPattern(x, t, nt)
}

func (c *Checker) classPattern(x *CallPat, t Type, nt *NamedT) {
	x.TD = nt
	if !identical(optElem(t), nt) {
		c.checkTypeTest(x.Pos, t, nt)
	} else if !isOpt(t) {
		x.Total = true
	}
	if nt.D.Kind != DClass && nt.D.Kind != DStruct {
		c.errorf(x.Pos, "%s 不能用作结构模式", nt)
		c.bindAllInvalid(x)
		return
	}
	// 位置参数对应字段的声明顺序（含继承）
	var fields []*Field
	chain := supertypes(nt)
	for i := len(chain) - 1; i >= 0; i-- {
		fields = append(fields, chain[i].D.Fields...)
	}
	m := Subst{}
	for _, anc := range chain {
		for k, v := range makeSubst(anc.D.TParams, anc.Args) {
			m[k] = v
		}
	}
	c.subPatterns(x, fields, m)
}

func (c *Checker) subPatterns(x *CallPat, fields []*Field, m Subst) {
	x.Fields = make([]*Field, len(x.Args))
	used := map[string]bool{}
	pos := 0
	for i, a := range x.Args {
		var f *Field
		if kw := x.KwNames[i]; kw != "" {
			for _, ff := range fields {
				if ff.Name == kw {
					f = ff
				}
			}
			if f == nil {
				c.errorf(a.P(), "没有名为 %s 的字段", kw)
				c.bindAllInvalid(a)
				continue
			}
		} else {
			if pos >= len(fields) {
				c.errorf(a.P(), "模式中的位置参数太多")
				c.bindAllInvalid(a)
				continue
			}
			f = fields[pos]
			pos++
		}
		if used[f.Name] {
			c.errorf(a.P(), "字段 %s 在模式中重复", f.Name)
		}
		used[f.Name] = true
		x.Fields[i] = f
		ft := f.T
		if ft == nil {
			ft = tInvalid
		}
		c.pattern(a, subst(ft, m))
	}
}
