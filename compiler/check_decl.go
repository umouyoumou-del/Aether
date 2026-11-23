package compiler

import (
	"fmt"
	"sort"
	"strings"
)

// ===== 类型表达式解析 =====

func (c *Checker) resolveType(tx TypeExpr, scope *Scope) Type {
	if tx == nil {
		return tVoid
	}
	switch t := tx.(type) {
	case *OptTypeX:
		e := c.resolveType(t.Elem, scope)
		if isInvalid(e) {
			return tInvalid
		}
		if isVoid(e) {
			c.errorf(t.Pos, "void 不能是可空类型")
			return tInvalid
		}
		if isOpt(e) {
			return e
		}
		if isAny(e) {
			c.errorf(t.Pos, "any 本身已经可以为 null，不需要写 any?")
			return tAny
		}
		return &OptT{e}
	case *TupleTypeX:
		if len(t.Elems) < 2 {
			c.errorf(t.Pos, "元组类型至少需要两个元素")
			return tInvalid
		}
		tt := &TupleT{}
		for _, e := range t.Elems {
			et := c.resolveType(e, scope)
			if isVoid(et) {
				c.errorf(e.P(), "元组元素不能是 void")
			}
			tt.Elems = append(tt.Elems, et)
		}
		if len(tt.Elems) > 8 {
			c.errorf(t.Pos, "元组最多支持 8 个元素")
		}
		return tt
	case *FuncTypeX:
		ft := &FuncT{Ret: tVoid}
		for _, p := range t.Params {
			ft.Params = append(ft.Params, c.resolveType(p, scope))
		}
		if t.Ret != nil {
			ft.Ret = c.resolveType(t.Ret, scope)
		}
		return ft
	case *NamedTypeX:
		return c.resolveNamedType(t, scope)
	}
	return tInvalid
}

func (c *Checker) resolveNamedType(t *NamedTypeX, scope *Scope) Type {
	var sym *Symbol
	if t.Mod != "" {
		ms := scope.lookup(t.Mod)
		if ms == nil || ms.Kind != SModule {
			c.errorf(t.Pos, "未知的模块 '%s'", t.Mod)
			return tInvalid
		}
		if strings.HasPrefix(t.Name, "_") {
			c.errorf(t.Pos, "'%s' 是模块 %s 的私有名字", t.Name, t.Mod)
		}
		sym = ms.ModRef.Scope.Syms[t.Name]
		if sym == nil {
			c.errorf(t.Pos, "模块 %s 中没有类型 '%s'", t.Mod, t.Name)
			return tInvalid
		}
	} else {
		if t.Name == "null" {
			c.errorf(t.Pos, "null 不是类型；可空类型写作 T?")
			return tInvalid
		}
		sym = scope.lookup(t.Name)
		if sym == nil {
			switch t.Name {
			case "Ordered", "Hashable", "Number", "Integer", "Sortable":
				c.errorf(t.Pos, "%s 只能用作泛型约束，例如 [T: %s]", t.Name, t.Name)
				return tInvalid
			case "Optional", "Option":
				c.errorf(t.Pos, "可空类型写作 T?")
				return tInvalid
			case "List", "Dict", "Set", "String", "Int", "Float", "Bool", "string", "double", "long", "boolean", "Integer32":
				c.errorf(t.Pos, "未知的类型 '%s'（五行的内置类型名为 int、float、str、bool、list、dict、set）", t.Name)
				return tInvalid
			}
			d := c.errorf(t.Pos, "未知的类型 '%s'", t.Name)
			if s := c.suggest(t.Name); s != "" {
				d.Notes = append(d.Notes, "是不是想写 '"+s+"'？")
			}
			return tInvalid
		}
	}
	args := make([]Type, 0, len(t.Args))
	for _, a := range t.Args {
		args = append(args, c.resolveType(a, scope))
	}
	for _, a := range args {
		if isInvalid(a) {
			return tInvalid
		}
	}
	need := func(n int) bool {
		if len(args) != n {
			c.errorf(t.Pos, "类型 %s 需要 %d 个类型实参，但给了 %d 个", t.Name, n, len(args))
			return false
		}
		return true
	}
	switch sym.Kind {
	case SType:
		if sym.AliasDecl != nil {
			return c.instAlias(sym, args, t.Pos)
		}
		if sym.Alias != nil {
			if len(args) > 0 {
				c.errorf(t.Pos, "类型 %s 不接受类型实参", t.Name)
			}
			return sym.Alias
		}
		switch sym.Builtin {
		case "list":
			if !need(1) {
				return tInvalid
			}
			c.noVoid(args[0], t.Pos)
			return &ListT{args[0]}
		case "set":
			if !need(1) {
				return tInvalid
			}
			c.requireHashable(args[0], t.Pos, "集合元素")
			return &SetT{args[0]}
		case "dict":
			if !need(2) {
				return tInvalid
			}
			c.requireHashable(args[0], t.Pos, "字典的键")
			c.noVoid(args[1], t.Pos)
			return &DictT{args[0], args[1]}
		case "chan":
			if !need(1) {
				return tInvalid
			}
			c.noVoid(args[0], t.Pos)
			return &ChanT{args[0]}
		case "iter":
			if !need(1) {
				return tInvalid
			}
			c.noVoid(args[0], t.Pos)
			return &IterT{args[0]}
		}
		td := sym.TD
		if td == nil {
			return tInvalid
		}
		if len(td.TParams) == 0 && td.Resolved == 0 && td.Class != nil && len(td.Class.TypeParams) > 0 {
			// 头部尚未解析：按 AST 计数
		}
		nparams := len(td.TParams)
		if td.TParams == nil {
			if td.Class != nil {
				nparams = len(td.Class.TypeParams)
			} else if td.Enum != nil {
				nparams = len(td.Enum.TypeParams)
			}
		}
		if len(args) != nparams {
			if nparams == 0 {
				c.errorf(t.Pos, "类型 %s 不是泛型类型", t.Name)
			} else {
				c.errorf(t.Pos, "泛型类型 %s 需要 %d 个类型实参，例如 %s[int]", t.Name, nparams, t.Name)
			}
			return tInvalid
		}
		nt := &NamedT{D: td, Args: args}
		c.checkTArgs(td.TParams, args, t.Pos)
		return nt
	case STParam:
		if len(args) > 0 {
			c.errorf(t.Pos, "类型参数 %s 不接受类型实参", t.Name)
		}
		return &TParamT{sym.TP}
	}
	c.errorf(t.Pos, "'%s' 不是类型", t.Name)
	return tInvalid
}

// resolveAlias 解析类型别名的定义（只做一次）。
func (c *Checker) resolveAlias(sym *Symbol) {
	ad := sym.AliasDecl
	switch sym.aliasState {
	case 2:
		return
	case 1:
		d := c.errorf(ad.Pos, "类型别名 %s 的定义（直接或间接地）引用了它自己", ad.Name)
		d.Notes = append(d.Notes, "递归的数据结构请用 class 定义")
		sym.Alias = tInvalid
		sym.aliasState = 2
		return
	}
	sym.aliasState = 1
	saveMod := c.mod
	c.mod = sym.Mod
	tps, sc := c.makeTParams(ad.TypeParams, sym.Mod.Scope)
	c.resolveConstraints(tps, ad.TypeParams, sc)
	t := c.resolveType(ad.Type, sc)
	c.mod = saveMod
	if sym.aliasState == 2 {
		// 解析过程中发现了自引用
		return
	}
	if isVoid(t) {
		c.errorf(ad.Type.P(), "类型别名不能是 void")
		t = tInvalid
	}
	sym.AliasTPs = tps
	sym.Alias = t
	sym.aliasState = 2
}

// instAlias 返回类型别名在给定类型实参下展开的类型。
func (c *Checker) instAlias(sym *Symbol, args []Type, pos Pos) Type {
	c.resolveAlias(sym)
	if isInvalid(sym.Alias) {
		return tInvalid
	}
	tps := sym.AliasTPs
	if len(args) != len(tps) {
		if len(tps) == 0 {
			c.errorf(pos, "类型别名 %s 不接受类型实参", sym.Name)
		} else {
			c.errorf(pos, "类型别名 %s 需要 %d 个类型实参，但给了 %d 个", sym.Name, len(tps), len(args))
		}
		return tInvalid
	}
	if len(tps) == 0 {
		return sym.Alias
	}
	c.checkTArgs(tps, args, pos)
	return subst(sym.Alias, makeSubst(tps, args))
}

func (c *Checker) noVoid(t Type, pos Pos) {
	if isVoid(t) {
		c.errorf(pos, "这里不能使用 void")
	}
}

func (c *Checker) requireHashable(t Type, pos Pos, what string) {
	if isInvalid(t) {
		return
	}
	if !c.hashable(t) {
		d := c.errorf(pos, "%s的类型必须可哈希，但 %s 不可哈希", what, t)
		if _, ok := t.(*ListT); ok {
			d.Notes = append(d.Notes, "列表可变、不能作为键；可以改用元组")
		}
		if tp, ok := t.(*TParamT); ok {
			d.Notes = append(d.Notes, fmt.Sprintf("给类型参数加约束：[%s: Hashable]", tp.P.Name))
		}
	}
}

// hashable 判断类型能否作为字典键/集合元素（Go 中可比较且语义一致）。
func (c *Checker) hashable(t Type) bool {
	switch x := t.(type) {
	case *Basic:
		return x.Kind != BAny && x.Kind != BVoid && x.Kind != BInvalid
	case *TupleT:
		for _, e := range x.Elems {
			if !c.hashable(e) {
				return false
			}
		}
		return true
	case *OptT:
		return c.hashable(x.Elem)
	case *ChanT:
		return true
	case *TParamT:
		return x.P.Bound == BoundHashable || x.P.Bound == BoundOrdered || x.P.Bound == BoundNumber || x.P.Bound == BoundInteger
	case *NamedT:
		d := x.D
		switch d.Kind {
		case DClass:
			return true // 按身份
		case DExtern:
			return strings.HasPrefix(d.GoType, "*")
		case DInterface:
			return false
		case DEnum:
			if !d.ADT {
				return true
			}
			return c.fieldsHashable(x, d.Variants)
		case DStruct:
			if d.Hashable == 1 {
				return true
			}
			if d.Hashable == 2 {
				return false
			}
			d.Hashable = 1 // 防止递归
			m := makeSubst(d.TParams, x.Args)
			for _, f := range d.Fields {
				if !c.hashable(subst(f.T, m)) {
					d.Hashable = 2
					return false
				}
			}
			return true
		}
	}
	return false
}

func (c *Checker) fieldsHashable(x *NamedT, vs []*Variant) bool {
	m := makeSubst(x.D.TParams, x.Args)
	for _, v := range vs {
		for _, f := range v.Fields {
			ft := subst(f.T, m)
			if n, ok := ft.(*NamedT); ok && n.D == x.D {
				continue
			}
			if !c.hashable(ft) {
				return false
			}
		}
	}
	return true
}

// checkRecursiveStructs 检查结构体是否（经由可空、元组或其他结构体的字段）包含自身。
// 结构体是值类型，这样的类型大小无限（Go 也拒绝编译）。
func (c *Checker) checkRecursiveStructs() {
	for _, td := range c.Types {
		if td.Kind != DStruct {
			continue
		}
		for _, f := range td.Fields {
			if f.T != nil && containsStruct(f.T, td, map[string]bool{}) {
				d := c.errorf(f.Pos, "结构体 %s 的字段 %s 包含了 %s 自身：结构体是值类型，不能直接或间接地包含自己", td.Name, f.Name, td.Name)
				d.Notes = append(d.Notes, "把 "+td.Name+" 改为 class（引用语义），或者把它放进列表等容器中")
				break
			}
		}
	}
}

// containsStruct 判断类型 t 的值是否按值包含结构体 target（列表、类等是引用，不算）。
func containsStruct(t Type, target *TypeDecl, seen map[string]bool) bool {
	switch x := t.(type) {
	case *OptT:
		return containsStruct(x.Elem, target, seen)
	case *TupleT:
		for _, el := range x.Elems {
			if containsStruct(el, target, seen) {
				return true
			}
		}
	case *NamedT:
		if x.D.Kind != DStruct {
			return false
		}
		if x.D == target {
			return true
		}
		if seen[x.String()] {
			return false
		}
		seen[x.String()] = true
		m := makeSubst(x.D.TParams, x.Args)
		for _, f := range x.D.Fields {
			if f.T != nil && containsStruct(subst(f.T, m), target, seen) {
				return true
			}
		}
	}
	return false
}

// satisfies 判断类型 t 是否满足类型参数 tp 的约束。
func (c *Checker) satisfies(t Type, tp *TypeParam) (bool, string) {
	return c.satisfiesIn(t, tp, nil)
}

// satisfiesIn 同 satisfies；env 给出同一组类型参数的实参，用于代入接口约束
// （例如 T: Comparable[T]、[K, V: Container[K]]）。
func (c *Checker) satisfiesIn(t Type, tp *TypeParam, env Subst) (bool, string) {
	switch tp.Bound {
	case BoundAny:
		if isVoid(t) {
			return false, "void 不能作为类型实参"
		}
		return true, ""
	case BoundHashable:
		if c.hashable(t) {
			return true, ""
		}
		return false, fmt.Sprintf("%s 不可哈希", t)
	case BoundOrdered:
		if isOrdered(t) {
			return true, ""
		}
		return false, fmt.Sprintf("%s 不支持 < 比较（Ordered 只包括数字、str、char）", t)
	case BoundNumber:
		if isNumeric(t) {
			return true, ""
		}
		return false, fmt.Sprintf("%s 不是数字类型", t)
	case BoundInteger:
		if isInteger(t) {
			return true, ""
		}
		return false, fmt.Sprintf("%s 不是整数类型", t)
	case BoundSortable:
		if c.sortable(t) {
			return true, ""
		}
		return false, fmt.Sprintf("%s 不能比较大小（Sortable 包括数字、str、char、定义了 __lt__ 的类型，以及由它们组成的元组）", t)
	case BoundIface:
		s := Subst{}
		for k, v := range env {
			s[k] = v
		}
		s[tp] = t
		iface := subst(tp.Iface, s).(*NamedT)
		if ok, miss := c.implements(t, iface); !ok {
			return false, fmt.Sprintf("%s 没有实现接口 %s（%s）", t, iface, miss)
		}
		return true, ""
	}
	return true, ""
}

func (c *Checker) checkTArgs(tps []*TypeParam, args []Type, pos Pos) bool {
	ok := true
	env := makeSubst(tps, args)
	for i, tp := range tps {
		if i >= len(args) {
			break
		}
		if good, why := c.satisfiesIn(args[i], tp, env); !good {
			d := c.errorf(pos, "类型实参 %s 不满足约束 %s: %s", args[i], tp.Name+": "+tp.BoundName(), why)
			if tp.Bound == BoundIface && isOrdered(args[i]) {
				if lt, _, _ := c.findIfaceMember(tp.Iface, "__lt__", map[*TypeDecl]bool{}); lt != nil {
					d.Notes = append(d.Notes, "数字、str、char 等内置类型没有 __lt__ 方法，不能实现 Comparable；只需要支持这些类型时请用约束 Ordered")
				}
			}
			ok = false
		} else if tp.Bound == BoundIface {
			c.noteStructIface(args[i], subst(tp.Iface, env), pos)
		}
	}
	return ok
}

// makeTParams 为类型参数创建符号与作用域。
func (c *Checker) makeTParams(xs []*TypeParamX, parent *Scope) ([]*TypeParam, *Scope) {
	if len(xs) == 0 {
		return nil, parent
	}
	sc := newScope(parent, nil)
	var tps []*TypeParam
	for i, x := range xs {
		tp := &TypeParam{Name: x.Name, Pos: x.Pos, Index: i}
		if sc.Syms[x.Name] != nil {
			c.errorf(x.Pos, "类型参数 '%s' 重复", x.Name)
		}
		sc.insert(&Symbol{Kind: STParam, Name: x.Name, TP: tp, Pos: x.Pos})
		tps = append(tps, tp)
	}
	return tps, sc
}

func (c *Checker) resolveConstraints(tps []*TypeParam, xs []*TypeParamX, sc *Scope) {
	for i, x := range xs {
		tp := tps[i]
		if x.Constraint == nil {
			continue
		}
		if nt, ok := x.Constraint.(*NamedTypeX); ok && nt.Mod == "" && len(nt.Args) == 0 {
			switch nt.Name {
			case "any":
				continue
			case "Hashable", "comparable":
				tp.Bound = BoundHashable
				continue
			case "Ordered", "Comparable":
				tp.Bound = BoundOrdered
				continue
			case "Number":
				tp.Bound = BoundNumber
				continue
			case "Integer":
				tp.Bound = BoundInteger
				continue
			case "Sortable":
				tp.Bound = BoundSortable
				continue
			}
		}
		ct := c.resolveType(x.Constraint, sc)
		if isInvalid(ct) {
			continue
		}
		if n, ok := ct.(*NamedT); ok && n.D.Kind == DInterface {
			tp.Bound = BoundIface
			tp.Iface = n
			continue
		}
		c.errorf(x.Constraint.P(), "约束必须是接口或 Ordered/Sortable/Hashable/Number/Integer，不能是 %s", ct)
	}
}

// ===== 声明解析 =====

func (c *Checker) resolveAll() {
	// 1. 类型参数
	for _, td := range c.Types {
		c.mod = td.Mod
		var xs []*TypeParamX
		if td.Class != nil {
			xs = td.Class.TypeParams
		} else if td.Enum != nil {
			xs = td.Enum.TypeParams
		}
		td.TParams, td.TScope = c.makeTParams(xs, td.Mod.Scope)
		if td.TParams == nil {
			td.TParams = []*TypeParam{}
		}
	}
	for _, td := range c.Types {
		c.mod = td.Mod
		var xs []*TypeParamX
		if td.Class != nil {
			xs = td.Class.TypeParams
		} else if td.Enum != nil {
			xs = td.Enum.TypeParams
		}
		c.resolveConstraints(td.TParams, xs, td.TScope)
	}
	// 2. 基类与接口
	for _, td := range c.Types {
		c.mod = td.Mod
		c.resolveBases(td)
	}
	// 检查继承环
	for _, td := range c.Types {
		seen := map[*TypeDecl]bool{}
		for d := td; d != nil && d.Base != nil; d = d.Base.D {
			if seen[d] {
				c.errorf(td.Pos, "类 %s 的继承关系形成了环", td.Name)
				td.Base = nil
				break
			}
			seen[d] = true
		}
	}
	for _, td := range c.Types {
		if td.Base != nil {
			td.Base.D.Subs = append(td.Base.D.Subs, td)
		}
		if c.excTD != nil && isSubclassDecl(td, c.excTD) {
			td.IsExc = true
		}
	}
	// 3. 成员
	for _, td := range c.Types {
		c.mod = td.Mod
		switch {
		case td.Class != nil:
			c.resolveClassMembers(td)
		case td.Enum != nil:
			c.resolveEnumMembers(td)
		}
	}
	// 4. 函数签名
	for _, m := range c.Mods {
		c.mod = m
		for _, fd := range m.Funcs {
			c.checkDecorators(fd.Decorators)
			fd.Sig = c.resolveSig(fd, m.Scope, nil)
			fd.Sym.Sig = fd.Sig
			if fd.Sym.Name == "main" && m.IsMain {
				if len(fd.Sig.Params) > 0 || !isVoid(fd.Sig.Ret) {
					c.errorf(fd.NamePos, "main 函数不能有参数和返回值")
				}
			}
			if !fd.HasBody && fd.GoExtern == "" {
				c.errorf(fd.NamePos, "函数 %s 缺少函数体", fd.Name)
			}
		}
	}
	// 5. 扩展
	for _, m := range c.Mods {
		c.mod = m
		for _, x := range m.Exts {
			c.resolveExt(x)
		}
	}
	// 类型别名在使用处按需解析；这里保证没有被使用的别名也得到检查
	for _, m := range c.Mods {
		for _, sym := range m.Aliases {
			c.resolveAlias(sym)
		}
	}
	// 6. 常量与注解的全局变量
	for _, m := range c.Mods {
		c.mod = m
		for _, cd := range m.Consts {
			c.resolveConstType(cd)
		}
		for _, sym := range m.Globals {
			if vd, ok := sym.DeclStmt.(*VarDecl); ok && sym.Kind == SVar {
				sym.T = c.resolveType(vd.Type, m.Scope)
				sym.Pending = false
			}
		}
	}
	// 7. 校验
	for _, td := range c.Types {
		c.mod = td.Mod
		c.validateDecl(td)
	}
	for _, td := range c.sortedClasses() {
		c.computeCtor(td)
	}
}

func (c *Checker) resolveBases(td *TypeDecl) {
	var bases []TypeExpr
	if td.Class != nil {
		bases = td.Class.Bases
	} else if td.Enum != nil {
		bases = td.Enum.Bases
	}
	for i, bx := range bases {
		bt := c.resolveType(bx, td.TScope)
		if isInvalid(bt) {
			continue
		}
		nt, ok := bt.(*NamedT)
		if !ok {
			c.errorf(bx.P(), "%s 不能继承 %s", td.Name, bt)
			continue
		}
		switch nt.D.Kind {
		case DInterface:
			td.Ifaces = append(td.Ifaces, nt)
		case DClass:
			if td.Kind != DClass {
				c.errorf(bx.P(), "%s 只能实现接口，不能继承类 %s", td.Kind, nt.D.Name)
				continue
			}
			if i != 0 {
				c.errorf(bx.P(), "基类 %s 必须写在第一个位置（只能有一个基类）", nt.D.Name)
				continue
			}
			td.Base = nt
		case DExtern:
			c.errorf(bx.P(), "不能继承内置类型 %s", nt.D.Name)
		default:
			c.errorf(bx.P(), "不能继承%s %s（%s 是值类型）", nt.D.Kind, nt.D.Name, nt.D.Name)
		}
	}
	if td.Kind == DInterface && td.Base != nil {
		c.errorf(td.Pos, "接口只能继承接口")
		td.Base = nil
	}
}

func (c *Checker) resolveClassMembers(td *TypeDecl) {
	cd := td.Class
	td.FieldMap = map[string]*Field{}
	td.Statics = map[string]*Field{}
	td.Methods = map[string]*Method{}
	td.Props = map[string]*Prop{}
	sc := td.TScope
	names := map[string]Pos{}
	dup := func(name string, pos Pos) bool {
		if old, ok := names[name]; ok {
			c.errorf(pos, "成员 '%s' 重复定义（之前定义于第 %d 行）", name, old.Line)
			return true
		}
		names[name] = pos
		return false
	}
	for _, fdl := range cd.Fields {
		if dup(fdl.Name, fdl.Pos) {
			continue
		}
		f := &Field{Name: fdl.Name, Owner: td, Pos: fdl.Pos, Static: fdl.Static, Const: fdl.Const, Default: fdl.Default, Decl: fdl}
		fdl.Field = f
		if fdl.Type != nil {
			f.T = c.resolveType(fdl.Type, sc)
		}
		if td.Kind == DInterface {
			c.errorf(fdl.Pos, "接口不能包含字段；请使用 prop %s -> T", fdl.Name)
			continue
		}
		if f.Static {
			if len(td.TParams) > 0 {
				c.errorf(fdl.Pos, "泛型类型不能有静态字段")
			}
			td.Statics[f.Name] = f
			f.Sym = &Symbol{Kind: SVar, Name: td.Name + "." + f.Name, T: f.T, Global: true, Mod: td.Mod, Field: f, Pos: f.Pos}
			if f.Const {
				f.Sym.Kind = SConst
			}
			continue
		}
		if td.Kind == DExtern {
			c.errorf(fdl.Pos, "外部类型不能声明字段")
			continue
		}
		td.Fields = append(td.Fields, f)
		td.FieldMap[f.Name] = f
	}
	for _, fd := range cd.Methods {
		if dup(fd.Name, fd.NamePos) {
			continue
		}
		m := c.resolveMethod(td, fd, sc)
		td.Methods[m.Name] = m
		td.MethodList = append(td.MethodList, m)
	}
	for _, pd := range cd.Props {
		if dup(pd.Name, pd.Pos) {
			continue
		}
		p := c.resolveProp(td, nil, pd, sc)
		td.Props[p.Name] = p
		td.PropList = append(td.PropList, p)
	}
}

func (c *Checker) resolveProp(td *TypeDecl, x *ExtBlock, pd *PropDecl, sc *Scope) *Prop {
	p := &Prop{Name: pd.Name, Owner: td, Ext: x, Decl: pd, HasGet: pd.HasGet, HasSet: pd.HasSet, Static: pd.Static, Extern: pd.GoExtern}
	pd.Prop = p
	p.T = c.resolveType(pd.Type, sc)
	if isVoid(p.T) {
		c.errorf(pd.Pos, "属性类型不能是 void")
	}
	if !pd.HasGet && !pd.HasSet {
		if pd.GoExtern != "" {
			p.HasGet = true
		} else if td != nil && (td.Kind == DInterface || pd.Abstract) {
			p.Abstract = true
			p.HasGet = true
		} else {
			c.errorf(pd.Pos, "属性 %s 缺少实现", pd.Name)
		}
	}
	if pd.HasSet && !pd.HasGet {
		c.errorf(pd.Pos, "属性 %s 只有 set 没有 get", pd.Name)
	}
	p.GetSig = &FuncSig{Ret: p.T}
	p.SetSig = &FuncSig{Ret: tVoid, Params: []*ParamInfo{{Name: pd.SetParam, T: p.T}}}
	return p
}

// checkDecorators 对五行不支持的装饰器给出提示。
func (c *Checker) checkDecorators(decs []*Decorator) {
	for _, d := range decs {
		switch d.Name {
		case "go":
			continue
		case "staticmethod", "classmethod":
			c.errorf(d.Pos, "五行用 static def 定义静态方法，不需要 @%s", d.Name)
		case "property":
			c.errorf(d.Pos, "五行用 prop 名字 -> 类型: 定义属性，不需要 @property")
		case "dataclass", "dataclasses.dataclass":
			c.errorf(d.Pos, "不需要 @dataclass：声明了字段但没有 __init__ 的类会自动生成构造函数")
		case "abstractmethod", "abc.abstractmethod":
			c.errorf(d.Pos, "五行用 abstract def 定义抽象方法")
		case "override", "Override":
			c.errorf(d.Pos, "五行用 override def 标记覆盖方法")
		default:
			c.errorf(d.Pos, "五行不支持装饰器 @%s", d.Name)
		}
	}
}

func (c *Checker) resolveMethod(td *TypeDecl, fd *FuncDecl, sc *Scope) *Method {
	c.checkDecorators(fd.Decorators)
	m := &Method{Name: fd.Name, Decl: fd, Owner: td, Static: fd.Static, Abstract: fd.Abstract, Override: fd.Override, Extern: fd.GoExtern, Pos: fd.NamePos}
	fd.Method = m
	fd.Sig = c.resolveSig(fd, sc, td)
	m.Sig = fd.Sig
	if td != nil {
		switch {
		case td.Kind == DInterface:
			if fd.HasBody {
				c.errorf(fd.NamePos, "接口方法不能有实现")
			}
			m.Abstract = true
		case td.Kind == DExtern:
			if fd.GoExtern == "" && fd.Name != "__init__" {
				c.errorf(fd.NamePos, "外部类型的方法需要 @go 注解")
			}
		case !fd.HasBody && fd.GoExtern == "":
			if !fd.Abstract {
				c.errorf(fd.NamePos, "方法 %s 缺少实现（抽象方法请写 abstract def）", fd.Name)
			}
			m.Abstract = true
		case fd.HasBody && fd.Abstract:
			c.errorf(fd.NamePos, "抽象方法不能有实现")
		}
		if m.Abstract && td.Kind == DClass && !td.Abstract {
			c.errorf(fd.NamePos, "类 %s 含有抽象方法 %s，必须声明为 abstract class", td.Name, fd.Name)
		}
		if fd.Abstract && td.Kind == DStruct {
			c.errorf(fd.NamePos, "结构体不能有抽象方法")
		}
	}
	c.checkDunder(td, m)
	if fd.Name == "__init__" && td != nil {
		td.Init = m
		if !isVoid(m.Sig.Ret) {
			c.errorf(fd.NamePos, "__init__ 不能有返回值")
		}
		if fd.Static {
			c.errorf(fd.NamePos, "__init__ 不能是 static")
		}
		if len(m.Sig.TParams) > 0 {
			c.errorf(fd.NamePos, "__init__ 不能有自己的类型参数")
		}
	}
	if fd.Name == "__str__" && td != nil {
		td.HasStr = true
	}
	if fd.Name == "__iter__" && td != nil {
		td.Iter = m
	}
	if len(m.Sig.TParams) > 0 && td != nil && td.Kind == DInterface {
		c.errorf(fd.NamePos, "接口方法不能有自己的类型参数")
	}
	return m
}

func (c *Checker) checkDunder(td *TypeDecl, m *Method) {
	if !strings.HasPrefix(m.Name, "__") || !strings.HasSuffix(m.Name, "__") {
		return
	}
	sig := m.Sig
	n := len(sig.Params)
	want := -1
	ret := Type(nil)
	switch m.Name {
	case "__init__":
		return
	case "__str__", "__repr__":
		want, ret = 0, tStr
	case "__len__":
		want, ret = 0, tInt
	case "__eq__", "__ne__", "__lt__", "__le__", "__gt__", "__ge__", "__contains__":
		want, ret = 1, tBool
	case "__add__", "__sub__", "__mul__", "__truediv__", "__floordiv__", "__mod__", "__pow__",
		"__radd__", "__rsub__", "__rmul__", "__rtruediv__", "__rfloordiv__", "__rmod__",
		"__and__", "__or__", "__xor__", "__lshift__", "__rshift__":
		want = 1
	case "__neg__", "__pos__", "__invert__", "__enter__", "__exit__", "__iter__", "__hash__", "__bool__":
		want = 0
		if m.Name == "__bool__" {
			ret = tBool
		}
		if m.Name == "__hash__" {
			c.errorf(m.Pos, "五行不支持 __hash__：类按对象身份哈希，结构体按字段值哈希")
		}
	case "__getitem__":
		want = 1
	case "__setitem__":
		want, ret = 2, tVoid
	case "__call__", "__del__", "__getattr__", "__setattr__", "__new__", "__format__":
		c.errorf(m.Pos, "五行不支持特殊方法 %s", m.Name)
		return
	default:
		c.errorf(m.Pos, "未知的特殊方法 %s", m.Name)
		return
	}
	if m.Static {
		c.errorf(m.Pos, "特殊方法 %s 不能是 static", m.Name)
	}
	if n != want {
		c.errorf(m.Pos, "特殊方法 %s 应有 %d 个参数（不含 self）", m.Name, want)
	}
	if ret != nil && !identical(sig.Ret, ret) {
		c.errorf(m.Pos, "特殊方法 %s 必须返回 %s", m.Name, ret)
	}
	if m.Name == "__iter__" {
		if _, ok := sig.Ret.(*IterT); !ok {
			c.errorf(m.Pos, "__iter__ 必须返回 iter[T]")
		}
	}
	if m.Name != "__getitem__" && m.Name != "__setitem__" && m.Name != "__enter__" && m.Name != "__exit__" &&
		m.Name != "__iter__" && ret == nil && isVoid(sig.Ret) {
		c.errorf(m.Pos, "运算符方法 %s 需要返回值", m.Name)
	}
}

// resolveSig 解析函数签名。owner 非空时为方法（第一个 self 参数会被跳过）。
func (c *Checker) resolveSig(fd *FuncDecl, parent *Scope, owner *TypeDecl) *FuncSig {
	sig := &FuncSig{Async: fd.Async, Decl: fd}
	var sc *Scope
	sig.TParams, sc = c.makeTParams(fd.TypeParams, parent)
	c.resolveConstraints(sig.TParams, fd.TypeParams, sc)
	sig.Scope = sc
	seen := map[string]bool{}
	params := fd.Params
	if len(params) > 0 && params[0].Name == "self" && params[0].Type == nil {
		if owner == nil && !c.inExt(fd) {
			c.errorf(params[0].Pos, "只有方法才能有 self 参数")
		}
		if fd.Static {
			c.errorf(params[0].Pos, "静态方法不能有 self 参数")
		}
		params = params[1:]
	}
	sawDefault := false
	sawVariadic := false
	for i, p := range params {
		if p.Name == "self" {
			c.errorf(p.Pos, "self 只能是第一个参数")
		}
		if seen[p.Name] {
			c.errorf(p.Pos, "参数 '%s' 重复", p.Name)
		}
		seen[p.Name] = true
		pi := &ParamInfo{Name: p.Name, Variadic: p.Variadic, KwOnly: p.KwOnly, Default: p.Default, Mod: c.mod, Pos: p.Pos}
		if p.Type == nil {
			c.errorf(p.Pos, "参数 '%s' 需要类型注解", p.Name)
			pi.T = tInvalid
		} else {
			pi.T = c.resolveType(p.Type, sc)
		}
		if isVoid(pi.T) {
			c.errorf(p.Pos, "参数类型不能是 void")
		}
		if p.Variadic {
			if sawVariadic {
				c.errorf(p.Pos, "只能有一个可变参数")
			}
			sawVariadic = true
			if p.Default != nil {
				c.errorf(p.Pos, "可变参数不能有默认值")
			}
		}
		if p.Default != nil {
			sawDefault = true
		} else if sawDefault && !p.Variadic && !sawVariadic && !p.KwOnly {
			c.errorf(p.Pos, "没有默认值的参数 '%s' 不能出现在有默认值的参数之后", p.Name)
		}
		_ = i
		sig.Params = append(sig.Params, pi)
	}
	if fd.Ret != nil {
		sig.Ret = c.resolveType(fd.Ret, sc)
	} else {
		sig.Ret = tVoid
	}
	if fd.Async && fd.Ret != nil {
		if n, ok := sig.Ret.(*NamedT); ok && n.D == c.taskTD {
			c.errorf(fd.Ret.P(), "async 函数的返回类型写结果类型本身（例如 -> int），调用时自动得到 Task[int]")
		}
	}
	// 默认值在签名作用域中检查（延后）
	for _, pi := range sig.Params {
		if pi.Default != nil {
			c.checkDefault(pi, sc)
		}
	}
	return sig
}

func (c *Checker) inExt(fd *FuncDecl) bool {
	for _, m := range c.Mods {
		for _, x := range m.Exts {
			for _, f := range x.Decl.Methods {
				if f == fd {
					return true
				}
			}
		}
	}
	return false
}

// checkDefault 检查参数默认值（默认值在调用处求值，只能引用全局名字）。
func (c *Checker) checkDefault(pi *ParamInfo, sc *Scope) {
	if isInvalid(pi.T) {
		return
	}
	saveFn, saveScope, saveFlow := c.fn, c.scope, c.flow
	c.fn = &FuncCtx{Name: "<默认值>", Mod: c.mod, Ret: tVoid}
	c.scope = newScope(sc, c.fn)
	c.fn.Scope = c.scope
	c.flow = newFlow()
	c.exprTo(&pi.Default, pi.T, "参数默认值")
	if len(c.fn.Locals) > 0 {
		c.errorf(pi.Default.P(), "参数默认值中不能声明变量")
	}
	c.fn, c.scope, c.flow = saveFn, saveScope, saveFlow
}

func (c *Checker) resolveEnumMembers(td *TypeDecl) {
	ed := td.Enum
	td.Methods = map[string]*Method{}
	td.Props = map[string]*Prop{}
	td.VariantMap = map[string]*Variant{}
	td.FieldMap = map[string]*Field{}
	td.Statics = map[string]*Field{}
	for _, vd := range ed.Variants {
		if vd.HasParens {
			td.ADT = true
		}
	}
	if len(td.TParams) > 0 && !td.ADT {
		c.errorf(ed.Pos, "只有带数据的枚举才能是泛型")
	}
	next := int64(0)
	usedVals := map[int64]string{}
	for i, vd := range ed.Variants {
		if _, ok := td.VariantMap[vd.Name]; ok {
			c.errorf(vd.Pos, "枚举成员 '%s' 重复", vd.Name)
			continue
		}
		v := &Variant{Name: vd.Name, Owner: td, Index: i, Pos: vd.Pos, HasPayload: len(vd.Fields) > 0}
		if vd.Value != nil {
			if td.ADT {
				c.errorf(vd.Pos, "带数据的枚举成员不能指定数值")
			}
			val, ok := c.constInt(vd.Value)
			if !ok {
				c.errorf(vd.Value.P(), "枚举值必须是整数常量")
			}
			next = val
		}
		v.Value = next
		if old, ok := usedVals[next]; ok && !td.ADT {
			c.errorf(vd.Pos, "枚举成员 %s 的值 %d 与 %s 重复", vd.Name, next, old)
		}
		usedVals[next] = vd.Name
		next++
		fnames := map[string]bool{}
		for _, p := range vd.Fields {
			if fnames[p.Name] {
				c.errorf(p.Pos, "字段 '%s' 重复", p.Name)
			}
			fnames[p.Name] = true
			f := &Field{Name: p.Name, Owner: td, Pos: p.Pos, Default: p.Default}
			if p.Type == nil {
				c.errorf(p.Pos, "枚举成员字段需要类型")
				f.T = tInvalid
			} else {
				f.T = c.resolveType(p.Type, td.TScope)
			}
			v.Fields = append(v.Fields, f)
		}
		td.Variants = append(td.Variants, v)
		td.VariantMap[v.Name] = v
	}
	for _, fd := range ed.Methods {
		if _, ok := td.VariantMap[fd.Name]; ok {
			c.errorf(fd.NamePos, "方法名 '%s' 与枚举成员重名", fd.Name)
		}
		if td.Methods[fd.Name] != nil {
			c.errorf(fd.NamePos, "方法 '%s' 重复定义", fd.Name)
			continue
		}
		m := c.resolveMethod(td, fd, td.TScope)
		if fd.Name == "__init__" {
			c.errorf(fd.NamePos, "枚举不能定义 __init__")
		}
		if !fd.HasBody && fd.GoExtern == "" {
			c.errorf(fd.NamePos, "枚举方法需要实现")
		}
		td.Methods[m.Name] = m
		td.MethodList = append(td.MethodList, m)
	}
	for _, pd := range ed.Props {
		p := c.resolveProp(td, nil, pd, td.TScope)
		if p.HasSet {
			c.errorf(pd.Pos, "枚举的属性不能有 set")
		}
		td.Props[p.Name] = p
		td.PropList = append(td.PropList, p)
	}
	if len(td.Variants) == 0 {
		c.errorf(ed.Pos, "枚举至少需要一个成员")
	}
}

func (c *Checker) constInt(e Expr) (int64, bool) {
	switch x := e.(type) {
	case *IntLit:
		return int64(x.Val), true
	case *Unary:
		if x.Op == TMinus {
			v, ok := c.constInt(x.X)
			return -v, ok
		}
	}
	return 0, false
}

func (c *Checker) resolveExt(x *ExtBlock) {
	xd := x.Decl
	var sc *Scope
	x.TParams, sc = c.makeTParams(xd.TypeParams, c.mod.Scope)
	c.resolveConstraints(x.TParams, xd.TypeParams, sc)
	x.Scope = sc
	x.Target = c.resolveType(xd.Target, sc)
	if isInvalid(x.Target) {
		return
	}
	if o, ok := x.Target.(*OptT); ok {
		_ = o
		c.errorf(xd.Target.P(), "不能扩展可空类型")
		return
	}
	if isAny(x.Target) || isVoid(x.Target) {
		c.errorf(xd.Target.P(), "不能扩展 %s", x.Target)
		return
	}
	if _, ok := x.Target.(*TParamT); ok {
		c.errorf(xd.Target.P(), "不能扩展类型参数")
		return
	}
	// 类型参数必须都出现在目标类型中
	used := map[*TypeParam]bool{}
	for _, tp := range x.TParams {
		used[tp] = false
	}
	for _, tp := range x.TParams {
		if !containsTParam(x.Target, map[*TypeParam]bool{tp: true}) {
			c.errorf(tp.Pos, "类型参数 %s 没有出现在被扩展的类型中", tp.Name)
		}
	}
	x.Key = typeHead(x.Target)
	c.exts[x.Key] = append(c.exts[x.Key], x)
	td := namedDecl(x.Target)
	if td != nil && td.Kind == DInterface {
		// 接口的扩展成员也适用于实现了该接口的类型（见 extBlocks）
		c.ifaceExts = append(c.ifaceExts, x)
	}
	for _, fd := range xd.Methods {
		m := &Method{Name: fd.Name, Decl: fd, Ext: x, Static: fd.Static, Extern: fd.GoExtern, Pos: fd.NamePos}
		fd.Method = m
		fd.Sig = c.resolveSig(fd, sc, nil)
		m.Sig = fd.Sig
		if fd.Static {
			c.errorf(fd.NamePos, "扩展方法不能是 static")
		}
		if !fd.HasBody && fd.GoExtern == "" {
			c.errorf(fd.NamePos, "扩展方法 %s 缺少实现", fd.Name)
		}
		if td != nil {
			if f, _, _ := c.findMember(&NamedT{D: td}, fd.Name); f != nil && !c.mod.IsPrelude {
				c.errorf(fd.NamePos, "类型 %s 已有成员 %s，扩展方法不能与之重名", td.Name, fd.Name)
			}
		}
		if strings.HasPrefix(fd.Name, "__") && fd.GoExtern == "" {
			c.checkDunder(nil, m)
		}
		x.Methods[fd.Name] = append(x.Methods[fd.Name], m)
	}
	for _, pd := range xd.Props {
		p := c.resolveProp(nil, x, pd, sc)
		if p.HasSet && pd.GoExtern == "" {
			c.errorf(pd.Pos, "扩展属性不能有 set")
		}
		x.Props[pd.Name] = p
	}
}

func (c *Checker) resolveConstType(cd *ConstDecl) {
	sym := cd.Sym
	if sym == nil || !sym.Pending {
		return
	}
	if c.constBusy[cd] {
		c.errorf(cd.Pos, "常量 %s 的定义形成了循环引用", cd.Name)
		sym.T = tInvalid
		sym.Pending = false
		return
	}
	c.constBusy[cd] = true
	defer delete(c.constBusy, cd)
	var want Type
	if cd.Type != nil {
		want = c.resolveType(cd.Type, c.mod.Scope)
	}
	if cd.Value == nil {
		if cd.GoName == "" {
			c.errorf(cd.Pos, "常量 %s 需要初始值", cd.Name)
		}
		sym.T = want
		sym.Pending = false
		return
	}
	saveFn, saveScope, saveFlow := c.fn, c.scope, c.flow
	c.fn = c.mod.InitFn
	c.scope = c.mod.Scope
	c.flow = newFlow()
	var t Type
	if want != nil {
		c.exprTo(&cd.Value, want, "常量初始值")
		t = want
	} else {
		t = c.expr(cd.Value, nil)
	}
	c.fn, c.scope, c.flow = saveFn, saveScope, saveFlow
	if isVoid(t) {
		c.errorf(cd.Pos, "常量不能是 void")
		t = tInvalid
	}
	if isNull(t) {
		c.errorf(cd.Pos, "无法从 null 推断常量的类型，请写出类型：const %s: T? = null", cd.Name)
		t = tInvalid
	}
	sym.T = t
	sym.Pending = false
	sym.GoConst = isConstExpr(cd.Value)
}

// isConstExpr 判断表达式是否可以生成为 Go 常量。
func isConstExpr(e Expr) bool {
	switch x := e.(type) {
	case *IntLit, *FloatLit, *StrLit, *BoolLit, *CharLit:
		return true
	case *Unary:
		return x.Op != KNot && isConstExpr(x.X) && x.Op2 == nil
	case *Binary:
		if x.Op2 != nil || x.Op == TDStar || x.Op == TDSlash || x.Op == TPercent || x.Op == TSlash {
			return false
		}
		return isConstExpr(x.X) && isConstExpr(x.Y) && basicKind(x.T) != BInvalid
	case *Ident:
		return x.Sym != nil && x.Sym.Kind == SConst && x.Sym.GoConst
	case *ConvExpr:
		return x.Kind == ConvNumeric && isConstExpr(x.X)
	}
	return false
}

// ===== 校验 =====

func (c *Checker) validateDecl(td *TypeDecl) {
	if td.Kind == DInterface {
		// 嵌入接口的方法不能冲突
		return
	}
	if td.Kind == DClass && td.Base != nil {
		// 字段不能与祖先重名；方法覆盖签名必须一致
		chain := supertypes(&NamedT{D: td, Args: tparamTypes(td.TParams)})
		for _, f := range td.Fields {
			for _, anc := range chain[1:] {
				if anc.D.FieldMap[f.Name] != nil {
					c.errorf(f.Pos, "字段 %s 与基类 %s 的字段重名", f.Name, anc.D.Name)
				}
			}
		}
		for _, m := range td.MethodList {
			if m.Name == "__init__" {
				continue
			}
			var base *Method
			var baseNT *NamedT
			for _, anc := range chain[1:] {
				if bm := anc.D.Methods[m.Name]; bm != nil {
					base, baseNT = bm, anc
					break
				}
				if anc.D.FieldMap[m.Name] != nil || anc.D.Props[m.Name] != nil {
					c.errorf(m.Pos, "方法 %s 与基类 %s 的成员重名", m.Name, anc.D.Name)
				}
			}
			if base == nil {
				if m.Override {
					c.errorf(m.Pos, "方法 %s 标记了 override，但基类中没有同名方法", m.Name)
				}
				continue
			}
			if base.Static != m.Static {
				c.errorf(m.Pos, "方法 %s 与基类方法的 static 属性不一致", m.Name)
				continue
			}
			if m.Static {
				continue
			}
			if len(base.Sig.TParams) > 0 || len(m.Sig.TParams) > 0 {
				c.errorf(m.Pos, "泛型方法 %s 不能被覆盖", m.Name)
				continue
			}
			bsig := substSig(base.Sig, makeSubst(baseNT.D.TParams, baseNT.Args))
			if !sigIdentical(bsig, m.Sig) {
				c.errorf(m.Pos, "方法 %s 覆盖基类方法时签名必须一致：基类为 %s，这里为 %s", m.Name, sigStr(bsig), sigStr(m.Sig))
			}
		}
		for _, p := range td.PropList {
			for _, anc := range chain[1:] {
				if bp := anc.D.Props[p.Name]; bp != nil {
					bt := subst(bp.T, makeSubst(anc.D.TParams, anc.Args))
					if !identical(bt, p.T) {
						c.errorf(p.Decl.Pos, "属性 %s 覆盖基类属性时类型必须一致", p.Name)
					}
					break
				}
			}
		}
	}
	// 非抽象类必须实现所有抽象方法
	if td.Kind == DClass && !td.Abstract {
		nt := &NamedT{D: td, Args: tparamTypes(td.TParams)}
		for _, name := range c.abstractMembers(nt) {
			c.errorf(td.Pos, "类 %s 没有实现抽象成员 %s", td.Name, name)
		}
	}
	// 声明的接口必须实现
	for _, it := range td.Ifaces {
		nt := &NamedT{D: td, Args: tparamTypes(td.TParams)}
		if ok, miss := c.implements(nt, it); !ok {
			if td.Abstract {
				continue
			}
			c.errorf(td.Pos, "%s 声明实现接口 %s，但%s", td.Name, it, miss)
		}
	}
}

func (c *Checker) abstractMembers(nt *NamedT) []string {
	chain := supertypes(nt)
	var out []string
	seen := map[string]bool{}
	for _, anc := range chain {
		for _, m := range anc.D.MethodList {
			if seen[m.Name] {
				continue
			}
			seen[m.Name] = true
			if m.Abstract {
				out = append(out, m.Name)
			}
		}
		for _, p := range anc.D.PropList {
			if seen[p.Name] {
				continue
			}
			seen[p.Name] = true
			if p.Abstract {
				out = append(out, p.Name)
			}
		}
	}
	sort.Strings(out)
	return out
}

func tparamTypes(tps []*TypeParam) []Type {
	if len(tps) == 0 {
		return nil
	}
	out := make([]Type, len(tps))
	for i, tp := range tps {
		out[i] = &TParamT{tp}
	}
	return out
}

func substSig(s *FuncSig, m Subst) *FuncSig {
	if len(m) == 0 {
		return s
	}
	n := &FuncSig{TParams: s.TParams, Ret: subst(s.Ret, m), Async: s.Async, Gen: s.Gen, Decl: s.Decl, Scope: s.Scope}
	for _, p := range s.Params {
		np := *p
		np.T = subst(p.T, m)
		if np.Default != nil {
			np.DefaultSubst = composeSubst(p.DefaultSubst, m)
		}
		n.Params = append(n.Params, &np)
	}
	return n
}

func sigIdentical(a, b *FuncSig) bool {
	if len(a.Params) != len(b.Params) || a.Async != b.Async {
		return false
	}
	for i := range a.Params {
		if !identical(a.Params[i].T, b.Params[i].T) || a.Params[i].Variadic != b.Params[i].Variadic {
			return false
		}
	}
	return identical(a.Ret, b.Ret)
}

func sigStr(s *FuncSig) string {
	var parts []string
	for _, p := range s.Params {
		parts = append(parts, p.Name+": "+typeStr(p.T))
	}
	r := "(" + strings.Join(parts, ", ") + ")"
	if !isVoid(s.Ret) {
		r += " -> " + typeStr(s.Ret)
	}
	return r
}

// computeCtor 计算类/结构体的构造签名。
func (c *Checker) computeCtor(td *TypeDecl) {
	if td.Kind != DClass && td.Kind != DStruct && td.Kind != DExtern {
		return
	}
	if td.Kind == DExtern {
		if td.Init != nil {
			td.Ctor = td.Init.Sig
			td.CtorInit = td.Init
		}
		return
	}
	// 查找最近的 __init__
	for d := td; d != nil; {
		if d.Init != nil {
			td.CtorInit = d.Init
			break
		}
		if d.Base == nil {
			break
		}
		d = d.Base.D
	}
	if td.CtorInit != nil {
		if td.CtorInit.Owner == td {
			td.Ctor = td.CtorInit.Sig
		} else {
			// 继承的 __init__：代入基类实参
			nt := &NamedT{D: td, Args: tparamTypes(td.TParams)}
			for _, anc := range supertypes(nt) {
				if anc.D == td.CtorInit.Owner {
					td.Ctor = substSig(td.CtorInit.Sig, makeSubst(anc.D.TParams, anc.Args))
					break
				}
			}
			for _, f := range td.Fields {
				if f.Default == nil && !f.Inferred && td.Kind == DClass {
					c.warnf(f.Pos, "字段 %s 没有默认值，且 %s 使用继承的 __init__，构造后它将是零值", f.Name, td.Name)
				}
			}
		}
		return
	}
	td.AutoCtor = true
	td.Ctor = &FuncSig{Ret: tVoid}
}

// finishCtor 在字段推断之后补全自动构造函数的参数。
func (c *Checker) finishCtor(td *TypeDecl) {
	if !td.AutoCtor {
		return
	}
	nt := &NamedT{D: td, Args: tparamTypes(td.TParams)}
	chain := supertypes(nt)
	sig := td.Ctor
	sig.Params = nil
	td.CtorFields = nil
	sawDefault := false
	for i := len(chain) - 1; i >= 0; i-- {
		anc := chain[i]
		m := makeSubst(anc.D.TParams, anc.Args)
		for _, f := range anc.D.Fields {
			pi := &ParamInfo{Name: f.Name, T: subst(f.T, m), Default: f.Default, Mod: anc.D.Mod, Pos: f.Pos, DefaultSubst: m}
			if f.Default == nil && sawDefault {
				// 没有默认值的字段排在有默认值的字段之后：只能用关键字参数
			}
			if f.Default != nil {
				sawDefault = true
			}
			sig.Params = append(sig.Params, pi)
			td.CtorFields = append(td.CtorFields, f)
		}
	}
}

func (c *Checker) checkFieldDefaults() {
	for _, td := range c.Types {
		c.mod = td.Mod
		var fields []*Field
		fields = append(fields, td.Fields...)
		var statics []*Field
		for _, f := range td.Statics {
			statics = append(statics, f)
		}
		sort.Slice(statics, func(i, j int) bool { return statics[i].Pos.Line < statics[j].Pos.Line })
		fields = append(fields, statics...)
		for _, v := range td.Variants {
			fields = append(fields, v.Fields...)
		}
		for _, f := range fields {
			if f.Default == nil {
				if f.T == nil {
					c.errorf(f.Pos, "字段 %s 需要类型注解", f.Name)
					f.T = tInvalid
				}
				continue
			}
			saveFn, saveScope, saveFlow := c.fn, c.scope, c.flow
			c.fn = &FuncCtx{Name: "<字段默认值>", Mod: td.Mod, Ret: tVoid}
			c.scope = newScope(td.TScope, c.fn)
			c.fn.Scope = c.scope
			c.flow = newFlow()
			if f.T == nil {
				t := c.expr(f.Default, nil)
				if isNull(t) || isVoid(t) {
					c.errorf(f.Pos, "无法推断字段 %s 的类型，请写出类型注解", f.Name)
					t = tInvalid
				}
				f.T = t
			} else {
				c.exprTo(&f.Default, f.T, "字段默认值")
			}
			if len(c.fn.Locals) > 0 {
				c.errorf(f.Default.P(), "字段默认值中不能声明变量")
			}
			c.fn, c.scope, c.flow = saveFn, saveScope, saveFlow
			if f.Sym != nil {
				f.Sym.T = f.T
			}
		}
	}
}

// ===== 成员查找 =====

// findMember 在类型（及其祖先）中查找成员，返回成员对象（*Field/*Method/*Prop）、
// 成员所属的已实例化类型，以及类型代换。
func (c *Checker) findMember(nt *NamedT, name string) (any, *NamedT, Subst) {
	d := nt.D
	if d.Kind == DInterface {
		return c.findIfaceMember(nt, name, map[*TypeDecl]bool{})
	}
	for _, anc := range supertypes(nt) {
		ad := anc.D
		m := makeSubst(ad.TParams, anc.Args)
		if f := ad.FieldMap[name]; f != nil {
			return f, anc, m
		}
		if p := ad.Props[name]; p != nil {
			return p, anc, m
		}
		if mt := ad.Methods[name]; mt != nil {
			return mt, anc, m
		}
		if f := ad.Statics[name]; f != nil {
			return f, anc, m
		}
	}
	return nil, nil, nil
}

func (c *Checker) findIfaceMember(nt *NamedT, name string, seen map[*TypeDecl]bool) (any, *NamedT, Subst) {
	d := nt.D
	if seen[d] {
		return nil, nil, nil
	}
	seen[d] = true
	m := makeSubst(d.TParams, nt.Args)
	if mt := d.Methods[name]; mt != nil {
		return mt, nt, m
	}
	if p := d.Props[name]; p != nil {
		return p, nt, m
	}
	for _, e := range d.Ifaces {
		en := subst(e, m).(*NamedT)
		if r, o, s := c.findIfaceMember(en, name, seen); r != nil {
			return r, o, s
		}
	}
	return nil, nil, nil
}

// ifaceMethods 返回接口（含嵌入接口）的所有方法与属性（已代入）。
func (c *Checker) ifaceMembers(nt *NamedT) (map[string]*FuncSig, map[string]*Prop, map[string]Type) {
	methods := map[string]*FuncSig{}
	props := map[string]*Prop{}
	propT := map[string]Type{}
	var walk func(n *NamedT, depth int)
	walk = func(n *NamedT, depth int) {
		if depth > 32 {
			return
		}
		m := makeSubst(n.D.TParams, n.Args)
		for _, mt := range n.D.MethodList {
			if _, ok := methods[mt.Name]; !ok {
				methods[mt.Name] = substSig(mt.Sig, m)
			}
		}
		for _, p := range n.D.PropList {
			if _, ok := props[p.Name]; !ok {
				props[p.Name] = p
				propT[p.Name] = subst(p.T, m)
			}
		}
		for _, e := range n.D.Ifaces {
			walk(subst(e, m).(*NamedT), depth+1)
		}
	}
	walk(nt, 0)
	return methods, props, propT
}

// implements 判断类型 t 是否实现接口 iface（结构化）。
func (c *Checker) implements(t Type, iface *NamedT) (bool, string) {
	key := t.String() + "<:" + iface.String()
	if r, ok := c.implCache[key]; ok && r {
		return true, ""
	}
	methods, props, propT := c.ifaceMembers(iface)
	var missing []string
	check := func() {
		names := make([]string, 0, len(methods))
		for n := range methods {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			want := methods[name]
			got := c.methodSigOf(t, name)
			if got == nil {
				missing = append(missing, "缺少方法 "+name)
				continue
			}
			if !sigIdentical(got, want) {
				missing = append(missing, fmt.Sprintf("方法 %s 的签名不一致（需要 %s，实际 %s）", name, sigStr(want), sigStr(got)))
			}
		}
		pnames := make([]string, 0, len(props))
		for n := range props {
			pnames = append(pnames, n)
		}
		sort.Strings(pnames)
		for _, name := range pnames {
			want := propT[name]
			got, hasSet := c.propTypeOf(t, name)
			if got == nil {
				missing = append(missing, "缺少属性 "+name)
				continue
			}
			if !identical(got, want) {
				missing = append(missing, fmt.Sprintf("属性 %s 的类型不一致", name))
			}
			if props[name].HasSet && !hasSet {
				missing = append(missing, fmt.Sprintf("属性 %s 需要 set", name))
			}
		}
	}
	switch x := t.(type) {
	case *NamedT:
		if x.D.Kind == DInterface && x.D == iface.D && identical(x, iface) {
			return true, ""
		}
		check()
	case *TParamT:
		if x.P.Bound == BoundIface {
			if identical(x.P.Iface, iface) {
				return true, ""
			}
			return c.implements(x.P.Iface, iface)
		}
		return false, "类型参数没有接口约束"
	default:
		return false, "内置类型不能实现接口"
	}
	if len(missing) > 0 {
		return false, strings.Join(missing, "；")
	}
	c.implCache[key] = true
	return true, ""
}

// methodSigOf 返回类型 t 上名为 name 的实例方法签名（已代入），用于接口检查。
func (c *Checker) methodSigOf(t Type, name string) *FuncSig {
	nt, ok := t.(*NamedT)
	if !ok {
		return nil
	}
	if nt.D.Kind == DInterface {
		ms, _, _ := c.ifaceMembers(nt)
		return ms[name]
	}
	mem, _, m := c.findMember(nt, name)
	if mt, ok := mem.(*Method); ok && !mt.Static {
		return substSig(mt.Sig, m)
	}
	return nil
}

func (c *Checker) propTypeOf(t Type, name string) (Type, bool) {
	nt, ok := t.(*NamedT)
	if !ok {
		return nil, false
	}
	if nt.D.Kind == DInterface {
		_, ps, pt := c.ifaceMembers(nt)
		if p := ps[name]; p != nil {
			return pt[name], p.HasSet
		}
		return nil, false
	}
	mem, _, m := c.findMember(nt, name)
	switch x := mem.(type) {
	case *Prop:
		return subst(x.T, m), x.HasSet
	case *Field:
		if !x.Static {
			// 字段也可以满足接口中的只读属性
			return subst(x.T, m), true
		}
	}
	return nil, false
}

// ===== 可赋值性 =====

// assignable 判断 from 类型的值能否用作 to 类型，并返回需要的隐式转换。
func (c *Checker) assignable(from, to Type) (ConvKind, bool) {
	if identical(from, to) {
		return ConvNone, true
	}
	if isInvalid(from) || isInvalid(to) {
		return ConvNone, true
	}
	if isVoid(from) || isVoid(to) {
		return ConvNone, false
	}
	if isNull(from) {
		if isOpt(to) || isAny(to) {
			return ConvNull, true
		}
		return ConvNone, false
	}
	if isAny(to) {
		if isOpt(from) {
			return ConvOptAny, true
		}
		if _, ok := from.(*MetaT); ok {
			return ConvNone, false
		}
		if _, ok := from.(*ModT); ok {
			return ConvNone, false
		}
		return ConvToAny, true
	}
	if ot, ok := to.(*OptT); ok {
		if of, ok := from.(*OptT); ok {
			k, ok := c.assignable(of.Elem, ot.Elem)
			if !ok {
				return ConvNone, false
			}
			if k == ConvNone {
				return ConvNone, true
			}
			return ConvOptMap, true
		}
		if _, ok := c.assignable(from, ot.Elem); ok {
			return ConvWrap, true
		}
		return ConvNone, false
	}
	if numWidens(from, to) {
		return ConvNumeric, true
	}
	switch tt := to.(type) {
	case *NamedT:
		switch tt.D.Kind {
		case DClass:
			if fn, ok := from.(*NamedT); ok && fn.D.Kind == DClass && isSubclassDecl(fn.D, tt.D) {
				for _, anc := range supertypes(fn) {
					if anc.D == tt.D {
						if identical(anc, tt) {
							return ConvIface, true
						}
						return ConvNone, false
					}
				}
			}
		case DInterface:
			if ok, _ := c.implements(from, tt); ok {
				return ConvIface, true
			}
		}
	case *IterT:
		var elem Type
		switch f := from.(type) {
		case *ListT:
			elem = f.Elem
		case *SetT:
			elem = f.Elem
		case *DictT:
			elem = f.Key
		case *ChanT:
			elem = f.Elem
		case *Basic:
			if f.Kind == BStr {
				elem = tChar
			}
		}
		if nf, ok := from.(*NamedT); ok {
			if mem, _, m := c.findMember(nf, "__iter__"); mem != nil {
				if mt, ok := mem.(*Method); ok {
					if it, ok := subst(mt.Sig.Ret, m).(*IterT); ok {
						elem = it.Elem
					}
				}
			}
		}
		if elem != nil && identical(elem, tt.Elem) {
			return ConvIter, true
		}
	case *FuncT:
		if ff, ok := from.(*FuncT); ok && len(ff.Params) == len(tt.Params) && ff.Variadic == tt.Variadic {
			for i := range ff.Params {
				if !identical(ff.Params[i], tt.Params[i]) {
					return ConvNone, false
				}
			}
			// 返回值：目标为 void 时可以丢弃返回值
			if isVoid(tt.Ret) && !isVoid(ff.Ret) {
				return ConvFunc, true
			}
			if k, ok := c.assignable(ff.Ret, tt.Ret); ok && k != ConvNone {
				return ConvFunc, true
			}
		}
	}
	return ConvNone, false
}

// join 计算两个类型的公共类型（用于列表字面量、三元表达式等）。
func (c *Checker) join(a, b Type) Type {
	if isInvalid(a) {
		return b
	}
	if isInvalid(b) {
		return a
	}
	if identical(a, b) {
		return a
	}
	if isNull(a) {
		if isOpt(b) || isAny(b) {
			return b
		}
		if isVoid(b) {
			return nil
		}
		return &OptT{b}
	}
	if isNull(b) {
		return c.join(b, a)
	}
	if oa, ok := a.(*OptT); ok {
		j := c.join(oa.Elem, optElem(b))
		if j == nil {
			return nil
		}
		if isOpt(j) || isAny(j) {
			return j
		}
		return &OptT{j}
	}
	if _, ok := b.(*OptT); ok {
		return c.join(b, a)
	}
	if isNumeric(a) && isNumeric(b) {
		if numWidens(a, b) {
			return b
		}
		if numWidens(b, a) {
			return a
		}
		if !isFloat(a) && !isFloat(b) {
			return nil
		}
		return tFloat
	}
	// 类：最近公共祖先
	na, okA := a.(*NamedT)
	nb, okB := b.(*NamedT)
	if okA && okB && na.D.Kind == DClass && nb.D.Kind == DClass {
		for _, x := range supertypes(na) {
			for _, y := range supertypes(nb) {
				if identical(x, y) {
					return x
				}
			}
		}
	}
	if _, ok := c.assignable(a, b); ok {
		return b
	}
	if _, ok := c.assignable(b, a); ok {
		return a
	}
	return nil
}
