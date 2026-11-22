package compiler

import (
	"fmt"
	"math"
	"strings"
)

// expr 检查表达式并记录其类型。want 是期望类型（可为 nil）。
func (c *Checker) expr(e Expr, want Type) Type {
	t := c.expr0(e, want)
	if t == nil {
		t = tInvalid
	}
	e.setTyp(t)
	return t
}

// exprTo 检查 *slot 并把它转换为类型 to（必要时插入隐式转换）。
func (c *Checker) exprTo(slot *Expr, to Type, ctx string) Type {
	t := c.expr(*slot, to)
	c.convert(slot, t, to, ctx)
	return to
}

func isUntypedLit(e Expr) bool {
	switch x := e.(type) {
	case *IntLit, *FloatLit:
		return true
	case *Unary:
		if x.Op == TMinus || x.Op == TPlus {
			return isUntypedLit(x.X)
		}
	}
	return false
}

// convert 把已检查的 *slot（类型 from）转换为 to。
func (c *Checker) convert(slot *Expr, from, to Type, ctx string) bool {
	if isInvalid(from) || isInvalid(to) {
		return true
	}
	if identical(from, to) {
		return true
	}
	// 数字字面量直接改变类型
	if isUntypedLit(*slot) && isNumeric(optElem(to)) {
		target := optElem(to)
		if c.retypeLit(*slot, target) {
			if isOpt(to) {
				*slot = &ConvExpr{exprBase: exprBase{Pos: (*slot).P(), T: to}, X: *slot, Kind: ConvWrap, From: target}
			}
			return true
		}
	}
	k, ok := c.assignable(from, to)
	if !ok {
		d := c.errorf((*slot).P(), "%s：类型不匹配，需要 %s，实际是 %s", ctx, to, from)
		c.mismatchNotes(d, from, to)
		return false
	}
	if k != ConvNone {
		ce := &ConvExpr{exprBase: exprBase{Pos: (*slot).P(), T: to}, X: *slot, Kind: k, From: from}
		if k == ConvWrap {
			ce.Inner, _ = c.assignable(from, optElem(to))
		}
		if k == ConvOptMap {
			ce.Inner, _ = c.assignable(optElem(from), optElem(to))
		}
		*slot = ce
	}
	return true
}

func (c *Checker) mismatchNotes(d *Diag, from, to Type) {
	switch {
	case isOpt(from) && !isOpt(to):
		if _, ok := c.assignable(optElem(from), to); ok {
			d.Notes = append(d.Notes, "这个值可能为 null：先用 if x != null 判断，或用 x! 断言非空，或用 x ?? 默认值")
		}
	case isStr(to) && (isNumeric(from) || basicKind(from) == BChar || isBool(from)):
		d.Notes = append(d.Notes, "用 str(x) 转换为字符串，或使用 f-string")
	case isNumeric(to) && isStr(from):
		d.Notes = append(d.Notes, "用 int(s) 或 float(s) 把字符串解析为数字")
	case isInteger(to) && isFloat(from):
		d.Notes = append(d.Notes, "浮点数不会隐式转换为整数：用 int(x) 截断或 round(x) 四舍五入")
	case basicKind(to) == BChar && isStr(from):
		d.Notes = append(d.Notes, "字符用单引号表示，例如 'a'")
	case isStr(to) && basicKind(from) == BChar:
		d.Notes = append(d.Notes, "用 str(c) 把字符转换为字符串")
	}
	if lf, ok := from.(*ListT); ok {
		if lt, ok := to.(*ListT); ok {
			if _, ok := c.assignable(lf.Elem, lt.Elem); ok {
				d.Notes = append(d.Notes, "列表类型是不变的：list[子类] 不能当作 list[基类] 使用；可以在创建时就写成 list[基类]")
			}
		}
	}
}

// retypeLit 把数字字面量改为目标数字类型（检查范围）。
func (c *Checker) retypeLit(e Expr, target Type) bool {
	switch x := e.(type) {
	case *IntLit:
		if !c.intFits(x.Val, x.Neg, target, x.Pos) {
			return true // 已报错
		}
		x.T = target
		return true
	case *FloatLit:
		if !isFloat(target) {
			if _, ok := target.(*TParamT); ok && isNumeric(target) {
				x.T = target
				return true
			}
			return false
		}
		x.T = target
		return true
	case *Unary:
		if x.Op == TMinus || x.Op == TPlus {
			if il, ok := x.X.(*IntLit); ok {
				neg := x.Op == TMinus
				if !c.intFits(il.Val, neg != il.Neg, target, il.Pos) {
					return true
				}
				if isUnsigned(target) && neg && il.Val != 0 {
					c.errorf(x.Pos, "无符号类型 %s 不能是负数", target)
				}
				il.T = target
				x.T = target
				return true
			}
			if c.retypeLit(x.X, target) {
				x.T = target
				return true
			}
		}
	}
	return false
}

func (c *Checker) intFits(v uint64, neg bool, t Type, pos Pos) bool {
	if isFloat(t) {
		return true
	}
	if _, ok := t.(*TParamT); ok {
		return true
	}
	var lo int64
	var hi uint64
	switch basicKind(t) {
	case BInt:
		lo, hi = math.MinInt64, math.MaxInt64
	case BI8:
		lo, hi = math.MinInt8, math.MaxInt8
	case BI16:
		lo, hi = math.MinInt16, math.MaxInt16
	case BI32:
		lo, hi = math.MinInt32, math.MaxInt32
	case BU8:
		lo, hi = 0, math.MaxUint8
	case BU16:
		lo, hi = 0, math.MaxUint16
	case BU32:
		lo, hi = 0, math.MaxUint32
	case BU64:
		lo, hi = 0, math.MaxUint64
	default:
		return true
	}
	if neg {
		if v == 0 {
			return true
		}
		if lo == 0 {
			c.errorf(pos, "常量 -%d 超出了 %s 的范围", v, t)
			return false
		}
		if v > uint64(-(lo+1))+1 {
			c.errorf(pos, "常量 -%d 超出了 %s 的范围", v, t)
			return false
		}
		return true
	}
	if v > hi {
		c.errorf(pos, "常量 %d 超出了 %s 的范围", v, t)
		return false
	}
	return true
}

func numericWant(want Type) Type {
	w := optElem(want)
	if isNumeric(w) {
		return w
	}
	return nil
}

func (c *Checker) expr0(e Expr, want Type) Type {
	switch x := e.(type) {
	case *Ident:
		return c.ident(x, want)
	case *IntLit:
		if w := numericWant(want); w != nil {
			if x.T != nil && !isInvalid(x.T) {
				return x.T
			}
			c.intFits(x.Val, x.Neg, w, x.Pos)
			return w
		}
		if x.T != nil && !isInvalid(x.T) {
			return x.T
		}
		if !x.Neg && x.Val > math.MaxInt64 {
			c.errorf(x.Pos, "整数 %s 超出了 int 的范围（可以写成 u64 类型）", x.Text)
		}
		return tInt
	case *FloatLit:
		if w := numericWant(want); w != nil && isFloat(w) {
			return w
		}
		if w := numericWant(want); w != nil {
			if _, ok := w.(*TParamT); ok {
				return w
			}
		}
		return tFloat
	case *StrLit:
		return tStr
	case *CharLit:
		return tChar
	case *BoolLit:
		return tBool
	case *NullLit:
		return tNull
	case *FString:
		return c.fstring(x)
	case *ListLit:
		return c.listLit(x, want)
	case *SetLit:
		return c.setLit(x, want)
	case *DictLit:
		return c.dictLit(x, want)
	case *TupleLit:
		return c.tupleLit(x, want)
	case *Comp:
		return c.comp(x, want)
	case *Unary:
		return c.unary(x, want)
	case *Binary:
		return c.binary(x, want)
	case *Compare:
		return c.compare(x)
	case *Logical:
		slot := Expr(x)
		c.condExpr(&slot)
		return tBool
	case *Ternary:
		return c.ternary(x, want)
	case *NullCo:
		return c.nullCo(x, want)
	case *Lambda:
		return c.lambda(x, want)
	case *Call:
		return c.call(x, want)
	case *Index:
		return c.index(x, want)
	case *TypeApp:
		return c.typeApp(x)
	case *SliceExpr:
		return c.slice(x)
	case *Attr:
		return c.attr(x, want)
	case *Unwrap:
		t := c.expr(x.X, optWant(want))
		if isInvalid(t) {
			return t
		}
		if o, ok := t.(*OptT); ok {
			return o.Elem
		}
		if isAny(t) {
			return tAny
		}
		c.warnf(x.Pos, "%s 不是可空类型，! 是多余的", t)
		return t
	case *Await:
		return c.await(x)
	case *Cast:
		return c.cast(x)
	case *IsExpr:
		return c.isExpr(x)
	case *SuperExpr:
		c.errorf(x.Pos, "super() 只能用来调用基类的方法，例如 super().__init__(...)")
		return tInvalid
	case *ConvExpr:
		return x.T
	case *typedPlaceholder:
		return x.T
	}
	c.errorf(e.P(), "无法识别的表达式")
	return tInvalid
}

func optWant(want Type) Type {
	if want == nil || isOpt(want) || isAny(want) {
		return want
	}
	return &OptT{want}
}

func (c *Checker) narrowable(sym *Symbol) bool {
	if sym == nil || sym.Kind != SVar || sym.IsSelf || sym.ClosureAssigned {
		return false
	}
	if sym.Global {
		// 模块顶层代码中可以窄化本模块的全局变量（脚本写法）
		return c.fn != nil && c.fn.IsModInit && sym.Mod == c.mod && sym.Field == nil
	}
	return true
}

func (c *Checker) ident(e *Ident, want Type) Type {
	if e.Name == "_" {
		c.errorf(e.Pos, "_ 只能用来丢弃值，不能读取")
		return tInvalid
	}
	sym := c.scope.lookup(e.Name)
	if sym == nil {
		if c.initPhase && c.mod.pendingGlobals[e.Name] {
			d := c.errorf(e.Pos, "全局变量 %s 的类型要在模块顶层代码检查后才能确定，__init__ 中暂时不能使用", e.Name)
			d.Notes = append(d.Notes, "给它加上类型注解即可，例如 "+e.Name+": int = 0")
			return tInvalid
		}
		c.undefined(e.Pos, e.Name)
		return tInvalid
	}
	e.Sym = sym
	sym.Used = true
	switch sym.Kind {
	case SVar, SConst:
		if sym.Pending {
			if cd, ok := sym.DeclStmt.(*ConstDecl); ok {
				saveMod := c.mod
				c.mod = sym.Mod
				c.resolveConstType(cd)
				c.mod = saveMod
			} else {
				c.errorf(e.Pos, "全局变量 %s 在赋值之前被使用", e.Name)
				return tInvalid
			}
		}
		if sym.T == nil {
			return tInvalid
		}
		if sym.Field == nil && !sym.Param && !sym.LoopVar {
			if !sym.Global && sym.Fn == c.fn && !c.flow.assigned[sym] {
				d := c.errorf(e.Pos, "变量 %s 可能还没有被赋值", e.Name)
				d.Notes = append(d.Notes, "它只在某些分支或循环中被赋值；可以在前面先给它一个初始值")
			}
			if sym.Global && c.fn != nil && c.fn.IsModInit && sym.Mod == c.mod && !c.flow.assigned[sym] && sym.Kind == SVar {
				c.errorf(e.Pos, "全局变量 %s 可能还没有被赋值", e.Name)
			}
		}
		if !sym.Global && sym.Fn != nil && sym.Fn != c.fn {
			sym.Captured = true
		}
		if sym.Global && sym.Kind == SVar && c.fn != nil && !c.fn.IsModInit && c.fn.globalReads != nil {
			if _, ok := c.fn.globalReads[e.Name]; !ok {
				c.fn.globalReads[e.Name] = e.Pos
			}
		}
		if nt, ok := c.flow.narrow[sym]; ok && c.narrowable(sym) {
			e.From = sym.T
			if o, ok := sym.T.(*OptT); ok && identical(o.Elem, nt) {
				e.Narrow = NarrowUnwrap
			} else {
				e.Narrow = NarrowCast
			}
			if !sym.Global && sym.Fn != c.fn && c.fn != nil {
				c.fn.narrowedCaptures = append(c.fn.narrowedCaptures, sym)
			}
			return nt
		}
		return sym.T
	case SFunc:
		if sym.Sig == nil {
			return tInvalid
		}
		if len(sym.Sig.TParams) > 0 {
			return &FuncRefT{Sig: sym.Sig, Name: sym.Name}
		}
		return c.sigFuncType(sym.Sig)
	case SType:
		if sym.Alias != nil {
			return &MetaT{sym.Alias}
		}
		if sym.Builtin != "" {
			return &GenericT{sym}
		}
		if sym.TD != nil {
			if len(sym.TD.TParams) > 0 {
				return &GenericT{sym}
			}
			return &MetaT{&NamedT{D: sym.TD}}
		}
	case SModule:
		return &ModT{sym.ModRef}
	case SBuiltin:
		d := c.errorf(e.Pos, "内置函数 %s 不能作为值使用", sym.Name)
		d.Notes = append(d.Notes, "可以用 lambda 包装，例如 x => "+sym.Name+"(x)")
		return tInvalid
	case STParam:
		return &MetaT{&TParamT{sym.TP}}
	}
	return tInvalid
}

// sigFuncType 返回签名作为函数值时的类型（async 函数返回 Task）。
func (c *Checker) sigFuncType(sig *FuncSig) *FuncT {
	ft := sig.FuncType()
	if sig.Async && c.taskTD != nil {
		ft.Ret = &NamedT{D: c.taskTD, Args: []Type{taskArg(sig.Ret)}}
	}
	return ft
}

// ===== 字面量 =====

func (c *Checker) fstring(x *FString) Type {
	for _, p := range x.Parts {
		if !p.IsExpr {
			continue
		}
		t := c.expr(p.X, nil)
		if isVoid(t) {
			c.errorf(p.X.P(), "这个表达式没有值，不能放进 f-字符串")
			continue
		}
		switch t.(type) {
		case *MetaT, *ModT, *GenericT, *FuncRefT:
			c.errorf(p.X.P(), "f-字符串中不能放 %s", t)
			continue
		}
		if p.Spec != "" {
			c.checkFormatSpec(p, t)
		}
	}
	return tStr
}

func (c *Checker) checkFormatSpec(p *FPart, t Type) {
	spec := p.Spec
	if spec == "" {
		return
	}
	last := spec[len(spec)-1]
	switch last {
	case 'd', 'x', 'X', 'b', 'o', 'c':
		if !isInteger(t) && basicKind(t) != BChar {
			c.errorf(p.X.P(), "格式 '%s' 只能用于整数，但值的类型是 %s", spec, t)
		}
	case 'f', 'F', 'e', 'E', 'g', 'G', '%':
		if !isNumeric(t) {
			c.errorf(p.X.P(), "格式 '%s' 只能用于数字，但值的类型是 %s", spec, t)
		}
	}
	if strings.Contains(spec, ",") || strings.Contains(spec, "_") {
		if !isNumeric(t) {
			c.errorf(p.X.P(), "千位分隔符只能用于数字")
		}
	}
}

func (c *Checker) validElem(t Type, pos Pos, what string) Type {
	switch t.(type) {
	case *MetaT, *ModT, *GenericT:
		c.errorf(pos, "%s不能是类型或模块", what)
		return tInvalid
	case *FuncRefT:
		c.errorf(pos, "泛型函数不能直接作为值使用")
		return tInvalid
	}
	if isVoid(t) {
		c.errorf(pos, "这个表达式没有值，不能作为%s", what)
		return tInvalid
	}
	return t
}

// inferElems 检查一组表达式并求出公共类型。anyCtx 为真时（期望类型是 any），
// 元素类型不一致会退化为 any，而不是报错（便于书写 JSON 风格的数据）。
func (c *Checker) inferElems(elems []Expr, what string, anyCtx ...bool) Type {
	ctx := len(anyCtx) > 0 && anyCtx[0]
	var hint Type
	if ctx {
		hint = tAny
	}
	var et Type
	types := make([]Type, len(elems))
	// 先检查非字面量，避免 [1, 2.5] 之类按 int 推断；空容器也放到后面，用其它元素的类型推断
	deferred := func(x Expr) bool { return isUntypedLit(x) || isEmptyContainer(x) }
	for i, x := range elems {
		if deferred(x) {
			continue
		}
		types[i] = c.validElem(c.expr(x, hint), x.P(), what)
	}
	var known Type
	for _, t := range types {
		if t == nil || isInvalid(t) {
			continue
		}
		if known == nil {
			known = t
		} else if j := c.join(known, t); j != nil {
			known = j
		}
	}
	for i, x := range elems {
		if types[i] != nil {
			continue
		}
		if isEmptyContainer(x) {
			types[i] = c.expr(x, known)
			continue
		}
		types[i] = c.expr(x, numericWant(known))
	}
	for i, t := range types {
		if et == nil {
			et = t
			continue
		}
		j := c.join(et, t)
		if j == nil && ctx {
			et = tAny
			continue
		}
		if j == nil {
			d := c.errorf(elems[i].P(), "%s的类型不一致：前面是 %s，这里是 %s", what, et, t)
			d.Notes = append(d.Notes, "如果确实需要混合类型，可以把容器声明为 any 元素，例如 xs: list[any] = [...]")
			return tInvalid
		}
		et = j
	}
	if isNull(et) {
		if ctx {
			et = tAny
		} else {
			c.errorf(elems[0].P(), "无法从 null 推断%s的类型，请写出类型注解", what)
			return tInvalid
		}
	}
	for i := range elems {
		c.convert(&elems[i], types[i], et, what)
	}
	return et
}

func (c *Checker) listLit(e *ListLit, want Type) Type {
	var elemWant Type
	switch w := optElem(want).(type) {
	case *ListT:
		elemWant = w.Elem
	case *IterT:
		elemWant = w.Elem
	}
	if len(e.Elems) == 0 {
		if elemWant == nil {
			d := c.errorf(e.Pos, "无法推断空列表的元素类型")
			d.Notes = append(d.Notes, "请写出类型，例如 xs: list[int] = []")
			return tInvalid
		}
		return &ListT{elemWant}
	}
	if elemWant != nil && !containsTParamFree(elemWant) {
		for i := range e.Elems {
			c.exprTo(&e.Elems[i], elemWant, "列表元素")
		}
		return &ListT{elemWant}
	}
	if isAny(want) {
		et := c.inferElems(e.Elems, "列表元素", true)
		if isInvalid(et) {
			return tInvalid
		}
		return &ListT{et}
	}
	et := c.inferElems(e.Elems, "列表元素", true)
	if isInvalid(et) {
		return tInvalid
	}
	return &ListT{et}
}

// isEmptyContainer 判断是否为空的列表/字典/集合字面量。
func isEmptyContainer(e Expr) bool {
	switch x := e.(type) {
	case *ListLit:
		return len(x.Elems) == 0
	case *DictLit:
		return len(x.Keys) == 0
	}
	return false
}

// containsTParamFree 判断类型中是否含有 nil（未知部分）。
func containsTParamFree(t Type) bool { return t == nil }

func (c *Checker) setLit(e *SetLit, want Type) Type {
	var elemWant Type
	if st, ok := optElem(want).(*SetT); ok {
		elemWant = st.Elem
	}
	var et Type
	if elemWant != nil {
		for i := range e.Elems {
			c.exprTo(&e.Elems[i], elemWant, "集合元素")
		}
		et = elemWant
	} else {
		et = c.inferElems(e.Elems, "集合元素")
	}
	if isInvalid(et) {
		return tInvalid
	}
	c.requireHashable(et, e.Pos, "集合元素")
	return &SetT{et}
}

func (c *Checker) dictLit(e *DictLit, want Type) Type {
	w := optElem(want)
	if len(e.Keys) == 0 {
		switch wt := w.(type) {
		case *DictT:
			return wt
		case *SetT:
			return wt
		}
		d := c.errorf(e.Pos, "无法推断空字典的类型")
		d.Notes = append(d.Notes, "请写出类型，例如 d: dict[str, int] = {}")
		return tInvalid
	}
	var kt, vt Type
	if dt, ok := w.(*DictT); ok {
		for i := range e.Keys {
			c.exprTo(&e.Keys[i], dt.Key, "字典的键")
			c.exprTo(&e.Vals[i], dt.Val, "字典的值")
		}
		return dt
	}
	kt = c.inferElems(e.Keys, "字典的键")
	vt = c.inferElems(e.Vals, "字典的值", true)
	if isInvalid(kt) || isInvalid(vt) {
		return tInvalid
	}
	c.requireHashable(kt, e.Pos, "字典的键")
	return &DictT{kt, vt}
}

func (c *Checker) tupleLit(e *TupleLit, want Type) Type {
	if len(e.Elems) < 2 {
		if len(e.Elems) == 0 {
			c.errorf(e.Pos, "不支持空元组")
		} else {
			c.errorf(e.Pos, "不支持单元素元组")
		}
		for _, x := range e.Elems {
			c.expr(x, nil)
		}
		return tInvalid
	}
	if len(e.Elems) > 8 {
		c.errorf(e.Pos, "元组最多支持 8 个元素")
	}
	tt := &TupleT{}
	wt, _ := optElem(want).(*TupleT)
	if wt != nil && len(wt.Elems) != len(e.Elems) {
		wt = nil
	}
	for i := range e.Elems {
		var t Type
		if wt != nil {
			c.exprTo(&e.Elems[i], wt.Elems[i], "元组元素")
			t = wt.Elems[i]
		} else {
			t = c.validElem(c.expr(e.Elems[i], nil), e.Elems[i].P(), "元组元素")
			if isNull(t) {
				c.errorf(e.Elems[i].P(), "无法从 null 推断元组元素的类型")
				t = tInvalid
			}
		}
		tt.Elems = append(tt.Elems, t)
	}
	return tt
}

func (c *Checker) comp(e *Comp, want Type) Type {
	c.pushScope()
	saveNarrow := c.saveNarrow()
	for _, cl := range e.Clauses {
		if cl.IsIf {
			thenN, _ := c.cond(&cl.Cond)
			c.applyNarrow(thenN)
			continue
		}
		info := &LoopInfo{}
		cl.Loop = info
		c.iterate(&cl.Iter, cl.Target, info)
	}
	var result Type
	w := optElem(want)
	switch e.Kind {
	case CompList, CompGen, CompSet:
		var ew Type
		switch wt := w.(type) {
		case *ListT:
			ew = wt.Elem
		case *SetT:
			ew = wt.Elem
		case *IterT:
			ew = wt.Elem
		}
		var et Type
		if ew != nil {
			c.exprTo(&e.Elem, ew, "推导式元素")
			et = ew
		} else {
			et = c.validElem(c.expr(e.Elem, nil), e.Elem.P(), "推导式元素")
			if isNull(et) {
				c.errorf(e.Elem.P(), "无法从 null 推断元素类型")
				et = tInvalid
			}
		}
		switch e.Kind {
		case CompList:
			result = &ListT{et}
		case CompSet:
			c.requireHashable(et, e.Pos, "集合元素")
			result = &SetT{et}
		default:
			result = &IterT{et}
		}
	case CompDict:
		var kw, vw Type
		if dt, ok := w.(*DictT); ok {
			kw, vw = dt.Key, dt.Val
		}
		var kt, vt Type
		if kw != nil {
			c.exprTo(&e.Key, kw, "字典的键")
			c.exprTo(&e.Val, vw, "字典的值")
			kt, vt = kw, vw
		} else {
			kt = c.validElem(c.expr(e.Key, nil), e.Key.P(), "字典的键")
			vt = c.validElem(c.expr(e.Val, nil), e.Val.P(), "字典的值")
		}
		c.requireHashable(kt, e.Pos, "字典的键")
		result = &DictT{kt, vt}
	}
	c.restoreNarrow(saveNarrow)
	c.popScope()
	return result
}

func (c *Checker) saveNarrow() map[*Symbol]Type {
	m := make(map[*Symbol]Type, len(c.flow.narrow))
	for k, v := range c.flow.narrow {
		m[k] = v
	}
	return m
}

func (c *Checker) restoreNarrow(m map[*Symbol]Type) { c.flow.narrow = m }

// ===== 运算符 =====

func dunderFor(op TokKind) string {
	switch op {
	case TPlus:
		return "add"
	case TMinus:
		return "sub"
	case TStar:
		return "mul"
	case TSlash:
		return "truediv"
	case TDSlash:
		return "floordiv"
	case TPercent:
		return "mod"
	case TDStar:
		return "pow"
	case TAmp:
		return "and"
	case TPipe:
		return "or"
	case TCaret:
		return "xor"
	case TShl:
		return "lshift"
	case TShr:
		return "rshift"
	}
	return ""
}

// operatorMethod 在类型 t 上查找运算符方法 __name__，并检查参数是否接受 argT。
func (c *Checker) operatorMethod(t Type, name string) (*Method, *FuncSig) {
	nt, ok := t.(*NamedT)
	if !ok {
		return nil, nil
	}
	if nt.D.Kind == DInterface {
		return nil, nil
	}
	mem, _, m := c.findMember(nt, name)
	mt, ok := mem.(*Method)
	if !ok || mt.Static {
		return nil, nil
	}
	return mt, substSig(mt.Sig, m)
}

func (c *Checker) unary(e *Unary, want Type) Type {
	switch e.Op {
	case KNot:
		slot := Expr(e)
		c.condExpr(&slot)
		return tBool
	case TMinus, TPlus:
		if il, ok := e.X.(*IntLit); ok {
			neg := e.Op == TMinus
			w := numericWant(want)
			if w == nil {
				w = tInt
			}
			if isFloat(w) {
				il.T = w
				return w
			}
			if isUnsigned(w) && neg && il.Val != 0 {
				c.errorf(e.Pos, "无符号类型 %s 不能是负数", w)
			}
			c.intFits(il.Val, neg, w, il.Pos)
			il.T = w
			return w
		}
	}
	xt := c.expr(e.X, numericWant(want))
	if isInvalid(xt) {
		return tInvalid
	}
	switch e.Op {
	case TMinus, TPlus:
		if isNumeric(xt) {
			if isUnsigned(xt) && e.Op == TMinus {
				c.errorf(e.Pos, "不能对无符号类型 %s 取负", xt)
			}
			return xt
		}
		name := "__neg__"
		if e.Op == TPlus {
			name = "__pos__"
		}
		if m, sig := c.operatorMethod(xt, name); m != nil {
			e.Op2 = m
			return sig.Ret
		}
		c.errorf(e.Pos, "一元 %s 不能用于 %s", e.Op, xt)
	case TTilde:
		if isInteger(xt) {
			return xt
		}
		if m, sig := c.operatorMethod(xt, "__invert__"); m != nil {
			e.Op2 = m
			return sig.Ret
		}
		c.errorf(e.Pos, "~ 只能用于整数，不能用于 %s", xt)
	}
	return tInvalid
}

func (c *Checker) binary(e *Binary, want Type) Type {
	var xt, yt Type
	if isUntypedLit(e.X) && !isUntypedLit(e.Y) {
		yt = c.expr(e.Y, numericWant(want))
		xt = c.expr(e.X, numericWant(yt))
	} else {
		hint := numericWant(want)
		if e.Op == TShl || e.Op == TShr {
			hint = numericWant(want)
		}
		xt = c.expr(e.X, hint)
		yhint := numericWant(xt)
		if e.Op == TShl || e.Op == TShr {
			yhint = nil
		}
		if isNumeric(xt) || yhint != nil {
			yt = c.expr(e.Y, yhint)
		} else {
			// 让右侧利用左侧类型推断（例如 list[float] + [1, 2]）
			var w Type
			switch xt.(type) {
			case *ListT, *SetT:
				w = xt
			}
			yt = c.expr(e.Y, w)
		}
	}
	if isInvalid(xt) || isInvalid(yt) {
		return tInvalid
	}
	// 运算符重载
	if name := dunderFor(e.Op); name != "" {
		if m, sig := c.operatorMethod(xt, "__"+name+"__"); m != nil {
			if _, ok := c.assignable(yt, sig.Params[0].T); ok {
				e.Op2 = m
				c.convert(&e.Y, yt, sig.Params[0].T, "运算符参数")
				return sig.Ret
			}
		}
		if m, sig := c.operatorMethod(yt, "__r"+name+"__"); m != nil {
			if _, ok := c.assignable(xt, sig.Params[0].T); ok {
				e.Op2 = m
				e.Rev = true
				c.convert(&e.X, xt, sig.Params[0].T, "运算符参数")
				return sig.Ret
			}
		}
	}
	switch e.Op {
	case TPlus:
		if isStr(xt) && isStr(yt) {
			return tStr
		}
		if (isStr(xt) && basicKind(yt) == BChar) || (basicKind(xt) == BChar && isStr(yt)) {
			return tStr
		}
		if lx, ok := xt.(*ListT); ok {
			if identical(xt, yt) {
				return xt
			}
			if c.convert(&e.Y, yt, xt, "列表拼接") {
				return lx
			}
			return tInvalid
		}
		if basicKind(xt) == BChar && isInteger(yt) {
			c.convert(&e.Y, yt, tI32, "字符运算")
			return tChar
		}
		if isStr(xt) || isStr(yt) {
			d := c.errorf(e.Pos, "不能把 %s 和 %s 相加", xt, yt)
			d.Notes = append(d.Notes, "字符串拼接请先用 str(x) 转换，或使用 f\"...{x}...\"")
			return tInvalid
		}
	case TMinus:
		if basicKind(xt) == BChar && basicKind(yt) == BChar {
			return tInt
		}
		if basicKind(xt) == BChar && isInteger(yt) {
			c.convert(&e.Y, yt, tI32, "字符运算")
			return tChar
		}
		if st, ok := xt.(*SetT); ok && identical(xt, yt) {
			return st
		}
	case TStar:
		if isStr(xt) && isInteger(yt) {
			c.convert(&e.Y, yt, tInt, "重复次数")
			return tStr
		}
		if isInteger(xt) && isStr(yt) {
			c.convert(&e.X, xt, tInt, "重复次数")
			return tStr
		}
		if _, ok := xt.(*ListT); ok && isInteger(yt) {
			c.convert(&e.Y, yt, tInt, "重复次数")
			return xt
		}
	case TPercent:
		if isStr(xt) {
			c.errorf(e.Pos, "五行不支持 %% 格式化字符串，请使用 f\"...{x}...\"")
			return tInvalid
		}
	case TAmp, TPipe, TCaret:
		if isBool(xt) && isBool(yt) {
			return tBool
		}
		if st, ok := xt.(*SetT); ok && identical(xt, yt) {
			return st
		}
		if dt, ok := xt.(*DictT); ok && e.Op == TPipe && identical(xt, yt) {
			return dt
		}
	case TShl, TShr:
		if isInteger(xt) && isInteger(yt) {
			if isUntypedLit(e.Y) {
				c.retypeLit(e.Y, tInt)
			}
			return xt
		}
	}
	// 数值运算
	if isNumeric(xt) && isNumeric(yt) {
		return c.numericBinary(e, xt, yt)
	}
	c.errorf(e.Pos, "运算符 %s 不能用于 %s 和 %s", e.Op, xt, yt)
	return tInvalid
}

func (c *Checker) numericBinary(e *Binary, xt, yt Type) Type {
	_, xp := xt.(*TParamT)
	_, yp := yt.(*TParamT)
	var common Type
	if xp || yp {
		if !identical(xt, yt) {
			c.errorf(e.Pos, "运算符 %s 不能用于 %s 和 %s（类型参数只能与同一类型运算）", e.Op, xt, yt)
			return tInvalid
		}
		common = xt
	} else {
		common = c.join(xt, yt)
		if common == nil || !isNumeric(common) {
			d := c.errorf(e.Pos, "运算符 %s 不能直接用于 %s 和 %s", e.Op, xt, yt)
			d.Notes = append(d.Notes, fmt.Sprintf("请显式转换，例如 %s(x)", xt))
			return tInvalid
		}
	}
	switch e.Op {
	case TSlash:
		res := common
		if isInteger(common) || xp {
			res = tFloat
		}
		c.convert(&e.X, xt, res, "除法")
		c.convert(&e.Y, yt, res, "除法")
		return res
	case TDSlash, TPercent:
		if xp {
			if tp := xt.(*TParamT); tp.P.Bound != BoundInteger {
				c.errorf(e.Pos, "%s 用于类型参数时需要 Integer 约束", e.Op)
			}
		}
	case TDStar:
		if xp {
			c.errorf(e.Pos, "** 不能用于类型参数")
		}
	case TAmp, TPipe, TCaret:
		if !isInteger(common) {
			c.errorf(e.Pos, "位运算 %s 只能用于整数", e.Op)
			return tInvalid
		}
	case TShl, TShr:
		return xt
	}
	c.convert(&e.X, xt, common, "运算")
	c.convert(&e.Y, yt, common, "运算")
	return common
}

func (c *Checker) compare(e *Compare) Type {
	types := make([]Type, len(e.Xs))
	// 先检查非字面量
	for i, x := range e.Xs {
		if !isUntypedLit(x) {
			var want Type
			if i > 0 && types[i-1] != nil {
				want = types[i-1]
			}
			if _, ok := x.(*NullLit); ok {
				continue
			}
			if l, ok := x.(*ListLit); ok && len(l.Elems) == 0 {
				continue
			}
			types[i] = c.expr(x, want)
		}
	}
	for i, x := range e.Xs {
		if types[i] != nil {
			continue
		}
		var hint Type
		if i > 0 && types[i-1] != nil {
			hint = types[i-1]
		} else if i+1 < len(types) && types[i+1] != nil {
			hint = types[i+1]
		}
		if i > 0 && i-1 < len(e.Ops) && (e.Ops[i-1] == KIn || e.Ops[i-1] == KNotIn) {
			hint = nil
		}
		types[i] = c.expr(x, numericOrSelf(hint))
	}
	e.Ovl = make([]*Method, len(e.Ops))
	e.Swap = make([]bool, len(e.Ops))
	e.Neg = make([]bool, len(e.Ops))
	for i, op := range e.Ops {
		a, b := types[i], types[i+1]
		if isInvalid(a) || isInvalid(b) {
			continue
		}
		switch op {
		case TEq, TNe:
			c.checkEquality(e, i, a, b)
		case TLt, TGt, TLe, TGe:
			c.checkOrdering(e, i, a, b)
		case KIn, KNotIn:
			c.checkIn(e, i, a, b)
		}
	}
	return tBool
}

func numericOrSelf(t Type) Type {
	if t == nil {
		return nil
	}
	if isNumeric(t) || isOpt(t) {
		return t
	}
	switch t.(type) {
	case *ListT, *DictT, *SetT, *TupleT:
		return t
	}
	return nil
}

func (c *Checker) checkEquality(e *Compare, i int, a, b Type) {
	op := e.Ops[i]
	if m, sig := c.operatorMethod(a, "__eq__"); m != nil && !isNull(b) {
		if _, ok := c.assignable(b, sig.Params[0].T); ok {
			e.Ovl[i] = m
			e.Neg[i] = op == TNe
			c.convert(&e.Xs[i+1], b, sig.Params[0].T, "比较")
			return
		}
	}
	if isNull(a) || isNull(b) {
		other := a
		if isNull(a) {
			other = b
		}
		if isNull(other) {
			return
		}
		if !isOpt(other) && !isAny(other) {
			c.warnf(e.Pos, "%s 不可能为 null，这个比较的结果总是 %v", other, op == TNe)
		}
		return
	}
	if identical(a, b) {
		if _, ok := a.(*FuncT); ok {
			c.errorf(e.Pos, "函数不能比较是否相等")
		}
		return
	}
	j := c.join(a, b)
	if j == nil {
		// 类与接口之间按身份比较
		if (isClassType(a) || isIfaceType(a)) && (isClassType(b) || isIfaceType(b)) {
			return
		}
		if isAny(a) || isAny(b) {
			return
		}
		c.errorf(e.Pos, "不能比较 %s 和 %s", a, b)
		return
	}
	c.convert(&e.Xs[i], a, j, "比较")
	c.convert(&e.Xs[i+1], b, j, "比较")
}

func (c *Checker) checkOrdering(e *Compare, i int, a, b Type) {
	op := e.Ops[i]
	names := map[TokKind][2]string{
		TLt: {"__lt__", "__gt__"}, TGt: {"__gt__", "__lt__"},
		TLe: {"__le__", "__ge__"}, TGe: {"__ge__", "__le__"},
	}
	nm := names[op]
	if m, sig := c.operatorMethod(a, nm[0]); m != nil {
		if _, ok := c.assignable(b, sig.Params[0].T); ok {
			e.Ovl[i] = m
			c.convert(&e.Xs[i+1], b, sig.Params[0].T, "比较")
			return
		}
	}
	if m, sig := c.operatorMethod(b, nm[1]); m != nil {
		if _, ok := c.assignable(a, sig.Params[0].T); ok {
			e.Ovl[i] = m
			e.Swap[i] = true
			c.convert(&e.Xs[i], a, sig.Params[0].T, "比较")
			return
		}
	}
	// a > b 可以借助 b.__lt__(a)
	if op == TGt || op == TLe || op == TGe {
		alt := map[TokKind]string{TGt: "__lt__", TGe: "__le__", TLe: "__ge__"}[op]
		if m, sig := c.operatorMethod(b, alt); m != nil && op == TGt {
			if _, ok := c.assignable(a, sig.Params[0].T); ok {
				e.Ovl[i] = m
				e.Swap[i] = true
				c.convert(&e.Xs[i], a, sig.Params[0].T, "比较")
				return
			}
		}
		if m, sig := c.operatorMethod(a, "__lt__"); m != nil && op == TGe {
			if _, ok := c.assignable(b, sig.Params[0].T); ok {
				e.Ovl[i] = m
				e.Neg[i] = true
				c.convert(&e.Xs[i+1], b, sig.Params[0].T, "比较")
				return
			}
		}
	}
	if isOpt(a) || isOpt(b) {
		c.errorf(e.Pos, "可空值不能直接比较大小；请先判断是否为 null")
		return
	}
	if isOrdered(a) && isOrdered(b) {
		if identical(a, b) {
			return
		}
		j := c.join(a, b)
		if j == nil || !isOrdered(j) {
			c.errorf(e.Pos, "不能比较 %s 和 %s 的大小", a, b)
			return
		}
		c.convert(&e.Xs[i], a, j, "比较")
		c.convert(&e.Xs[i+1], b, j, "比较")
		return
	}
	if ta, ok := a.(*TupleT); ok && identical(a, b) {
		for _, el := range ta.Elems {
			if !isOrdered(el) {
				c.errorf(e.Pos, "元组比较大小要求每个元素都可比较，%s 不行", el)
				return
			}
		}
		return
	}
	c.errorf(e.Pos, "不能比较 %s 和 %s 的大小（需要数字、str、char，或实现 __lt__）", a, b)
}

func (c *Checker) checkIn(e *Compare, i int, a, b Type) {
	switch x := b.(type) {
	case *ListT:
		c.convert(&e.Xs[i], a, x.Elem, "in 的左侧")
		return
	case *SetT:
		c.convert(&e.Xs[i], a, x.Elem, "in 的左侧")
		return
	case *DictT:
		c.convert(&e.Xs[i], a, x.Key, "in 的左侧（字典的键）")
		return
	case *Basic:
		if x.Kind == BStr {
			if isStr(a) || basicKind(a) == BChar {
				return
			}
			c.errorf(e.Pos, "in 字符串时左侧需要是 str 或 char，得到 %s", a)
			return
		}
	case *NamedT:
		if m, sig := c.operatorMethod(b, "__contains__"); m != nil {
			e.Ovl[i] = m
			c.convert(&e.Xs[i], a, sig.Params[0].T, "in 的左侧")
			return
		}
	case *OptT:
		c.errorf(e.Pos, "右侧可能为 null")
		return
	case *TupleT:
		c.errorf(e.Pos, "不支持对元组使用 in；请改用列表或集合")
		return
	case *IterT:
		c.errorf(e.Pos, "不能对迭代器使用 in；请先转换为列表或集合")
		return
	}
	c.errorf(e.Pos, "类型 %s 不支持 in 运算", b)
}

// ===== 条件与窄化 =====

func union(a, b map[*Symbol]Type) map[*Symbol]Type {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	r := map[*Symbol]Type{}
	for k, v := range a {
		r[k] = v
	}
	for k, v := range b {
		r[k] = v
	}
	return r
}

func intersect(a, b map[*Symbol]Type) map[*Symbol]Type {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	r := map[*Symbol]Type{}
	for k, v := range a {
		if w, ok := b[k]; ok && identical(v, w) {
			r[k] = v
		}
	}
	return r
}

// cond 检查条件表达式（必须为 bool 或可真值判断），返回窄化信息。
func (c *Checker) cond(slot *Expr) (thenN, elseN map[*Symbol]Type) {
	thenN, elseN = c.condExpr(slot)
	c.truthy(slot)
	return
}

func (c *Checker) condExpr(slot *Expr) (thenN, elseN map[*Symbol]Type) {
	e := *slot
	switch x := e.(type) {
	case *Logical:
		tA, eA := c.condExpr(&x.X)
		c.truthy(&x.X)
		save := c.saveNarrow()
		if x.Op == KAnd {
			c.applyNarrow(tA)
		} else {
			c.applyNarrow(eA)
		}
		tB, eB := c.condExpr(&x.Y)
		c.truthy(&x.Y)
		c.restoreNarrow(save)
		x.T = tBool
		if x.Op == KAnd {
			return union(tA, tB), intersect(eA, eB)
		}
		return intersect(tA, tB), union(eA, eB)
	case *Unary:
		if x.Op == KNot {
			t, el := c.condExpr(&x.X)
			c.truthy(&x.X)
			x.T = tBool
			return el, t
		}
	case *Compare:
		c.expr(e, nil)
		if len(x.Ops) == 1 && (x.Ops[0] == TEq || x.Ops[0] == TNe) {
			var id *Ident
			if _, ok := x.Xs[1].(*NullLit); ok {
				id, _ = unconv(x.Xs[0]).(*Ident)
			} else if _, ok := x.Xs[0].(*NullLit); ok {
				id, _ = unconv(x.Xs[1]).(*Ident)
			}
			if id != nil && c.narrowable(id.Sym) {
				if o, ok := id.Sym.T.(*OptT); ok {
					n := map[*Symbol]Type{id.Sym: o.Elem}
					if x.Ops[0] == TNe {
						return n, nil
					}
					return nil, n
				}
			}
		}
		return nil, nil
	case *IsExpr:
		c.expr(e, nil)
		id, ok := x.X.(*Ident)
		if !ok || !c.narrowable(id.Sym) || isInvalid(x.T) {
			return nil, nil
		}
		var n map[*Symbol]Type
		if x.Null {
			if o, ok := id.Sym.T.(*OptT); ok {
				n = map[*Symbol]Type{id.Sym: o.Elem}
				if x.Neg {
					return n, nil
				}
				return nil, n
			}
			return nil, nil
		}
		if x.To != nil && !isInvalid(x.To) {
			n = map[*Symbol]Type{id.Sym: x.To}
		}
		if x.Neg {
			return nil, n
		}
		return n, nil
	case *Ident:
		t := c.expr(e, nil)
		if o, ok := t.(*OptT); ok && c.narrowable(x.Sym) {
			return map[*Symbol]Type{x.Sym: o.Elem}, nil
		}
		return nil, nil
	}
	c.expr(e, tBool)
	return nil, nil
}

func unconv(e Expr) Expr {
	for {
		if ce, ok := e.(*ConvExpr); ok {
			e = ce.X
			continue
		}
		return e
	}
}

// truthy 确保条件为 bool；对可空值、字符串、容器插入真值判断。
func (c *Checker) truthy(slot *Expr) {
	t := (*slot).Typ()
	if t == nil || isInvalid(t) || isBool(t) {
		return
	}
	ok := false
	switch x := t.(type) {
	case *OptT, *ListT, *DictT, *SetT:
		ok = true
	case *Basic:
		switch x.Kind {
		case BStr, BAny:
			ok = true
		default:
			if isNumeric(x) {
				d := c.errorf((*slot).P(), "条件需要 bool 类型，得到 %s", t)
				d.Notes = append(d.Notes, "数字请明确写出比较，例如 x != 0")
				return
			}
		}
	case *NamedT:
		if m, _ := c.operatorMethod(t, "__bool__"); m != nil {
			ok = true
		} else if m, _ := c.operatorMethod(t, "__len__"); m != nil {
			ok = true
		}
	}
	if !ok {
		c.errorf((*slot).P(), "条件需要 bool 类型，得到 %s", t)
		return
	}
	*slot = &ConvExpr{exprBase: exprBase{Pos: (*slot).P(), T: tBool}, X: *slot, Kind: ConvTruthy, From: t}
}

func (c *Checker) ternary(e *Ternary, want Type) Type {
	thenN, elseN := c.cond(&e.Cond)
	save := c.saveNarrow()
	c.applyNarrow(thenN)
	tt := c.expr(e.Then, want)
	c.restoreNarrow(save)
	save = c.saveNarrow()
	c.applyNarrow(elseN)
	et := c.expr(e.Else, want)
	c.restoreNarrow(save)
	if isInvalid(tt) || isInvalid(et) {
		return tInvalid
	}
	var res Type
	if want != nil {
		_, ok1 := c.assignable(tt, want)
		_, ok2 := c.assignable(et, want)
		if ok1 && ok2 && !isAny(want) {
			res = want
		}
	}
	if res == nil {
		res = c.join(tt, et)
		if res == nil {
			c.errorf(e.Pos, "条件表达式两个分支的类型不一致：%s 和 %s", tt, et)
			return tInvalid
		}
	}
	if isVoid(res) {
		c.errorf(e.Pos, "条件表达式的分支没有值")
		return tInvalid
	}
	c.convert(&e.Then, tt, res, "条件表达式")
	c.convert(&e.Else, et, res, "条件表达式")
	return res
}

func (c *Checker) nullCo(e *NullCo, want Type) Type {
	xt := c.expr(e.X, optWant(want))
	if isInvalid(xt) {
		c.expr(e.Y, want)
		return tInvalid
	}
	ot, ok := xt.(*OptT)
	if !ok {
		if isAny(xt) {
			yt := c.expr(e.Y, tAny)
			c.convert(&e.Y, yt, tAny, "?? 右侧")
			return tAny
		}
		c.warnf(e.Pos, "%s 不可能为 null，?? 右侧永远不会被使用", xt)
		c.expr(e.Y, xt)
		return xt
	}
	yw := ot.Elem
	if want != nil && isOpt(want) {
		yw = want
	}
	yt := c.expr(e.Y, yw)
	if isInvalid(yt) {
		return tInvalid
	}
	if _, ok := c.assignable(yt, ot.Elem); ok && !isNull(yt) {
		c.convert(&e.Y, yt, ot.Elem, "?? 右侧")
		return ot.Elem
	}
	if _, ok := c.assignable(yt, ot); ok {
		c.convert(&e.Y, yt, ot, "?? 右侧")
		return ot
	}
	c.errorf(e.Y.P(), "?? 右侧的类型 %s 与左侧 %s 不匹配", yt, ot.Elem)
	return tInvalid
}

func (c *Checker) lambda(e *Lambda, want Type) Type {
	ft, _ := optElem(want).(*FuncT)
	if ft != nil && len(ft.Params) != len(e.Params) {
		c.errorf(e.Pos, "lambda 有 %d 个参数，但这里需要 %d 个", len(e.Params), len(ft.Params))
		ft = nil
	}
	var ptypes []Type
	for i, p := range e.Params {
		var pt Type
		if p.Type != nil {
			pt = c.resolveType(p.Type, c.scope)
		} else if ft != nil && ft.Params[i] != nil {
			pt = ft.Params[i]
		} else {
			d := c.errorf(p.Pos, "无法推断 lambda 参数 %s 的类型", p.Name)
			d.Notes = append(d.Notes, "请写出类型，例如 ("+p.Name+": int) => ...")
			pt = tInvalid
		}
		ptypes = append(ptypes, pt)
	}
	var declRet Type
	if e.Ret != nil {
		declRet = c.resolveType(e.Ret, c.scope)
	}
	fn := &FuncCtx{Name: "<lambda>", Mod: c.mod, IsLambda: true, Lambda: e}
	e.Fn = fn
	var ret Type
	c.withFunc(fn, c.scope, func() {
		for i, p := range e.Params {
			sym := &Symbol{Kind: SVar, Name: p.Name, T: ptypes[i], Pos: p.Pos, Fn: fn, Param: true, Assigns: 1}
			p.Sym = sym
			if p.Name != "_" {
				c.scope.insert(sym)
			}
			fn.Params = append(fn.Params, sym)
			c.flow.assigned[sym] = true
		}
		var retWant Type
		if declRet != nil {
			retWant = declRet
		} else if ft != nil && ft.Ret != nil && !isVoid(ft.Ret) {
			retWant = ft.Ret
		}
		fn.Ret = retWant
		bt := c.expr(e.Body, retWant)
		switch {
		case retWant != nil:
			c.convert(&e.Body, bt, retWant, "lambda 返回值")
			ret = retWant
		case ft != nil && ft.Ret != nil && isVoid(ft.Ret):
			ret = tVoid
		default:
			ret = c.validLambdaRet(bt, e.Body.P())
		}
		fn.Ret = ret
	})
	return &FuncT{Params: ptypes, Ret: ret}
}

func (c *Checker) validLambdaRet(t Type, pos Pos) Type {
	switch t.(type) {
	case *MetaT, *ModT, *GenericT, *FuncRefT:
		c.errorf(pos, "lambda 不能返回 %s", t)
		return tInvalid
	}
	if isNull(t) {
		c.errorf(pos, "无法从 null 推断 lambda 的返回类型")
		return tInvalid
	}
	return t
}

// ===== 下标、切片 =====

// exprToTypeExpr 把下标中的表达式（例如 Stack[int] 中的 int）转换为类型表达式。
func exprToTypeExpr(e Expr) (TypeExpr, bool) {
	switch x := e.(type) {
	case *Ident:
		return &NamedTypeX{Pos: x.Pos, Name: x.Name}, true
	case *Attr:
		if id, ok := x.X.(*Ident); ok {
			return &NamedTypeX{Pos: x.Pos, Mod: id.Name, Name: x.Name}, true
		}
	case *Index:
		base, ok := exprToTypeExpr(x.X)
		if !ok {
			return nil, false
		}
		nt, ok := base.(*NamedTypeX)
		if !ok || nt.Args != nil {
			return nil, false
		}
		args, ok := indexTypeArgs(x.Idx)
		if !ok {
			return nil, false
		}
		nt.Args = args
		return nt, true
	case *TupleLit:
		tt := &TupleTypeX{Pos: x.Pos}
		for _, el := range x.Elems {
			te, ok := exprToTypeExpr(el)
			if !ok {
				return nil, false
			}
			tt.Elems = append(tt.Elems, te)
		}
		return tt, true
	case *NullLit:
		return &NamedTypeX{Pos: x.Pos, Name: "null"}, true
	case *TypeApp:
		base, ok := exprToTypeExpr(x.X)
		if !ok {
			return nil, false
		}
		nt, ok := base.(*NamedTypeX)
		if !ok {
			return nil, false
		}
		nt.Args = x.Args
		return nt, true
	}
	return nil, false
}

func indexTypeArgs(idx Expr) ([]TypeExpr, bool) {
	if tl, ok := idx.(*TupleLit); ok && !tl.Paren {
		var out []TypeExpr
		for _, el := range tl.Elems {
			te, ok := exprToTypeExpr(el)
			if !ok {
				return nil, false
			}
			out = append(out, te)
		}
		return out, true
	}
	te, ok := exprToTypeExpr(idx)
	if !ok {
		return nil, false
	}
	return []TypeExpr{te}, true
}

// instantiate 对泛型类型/函数应用类型实参。
func (c *Checker) instantiate(base Type, args []TypeExpr, pos Pos) (Type, []Type) {
	var types []Type
	for _, a := range args {
		t := c.resolveType(a, c.scope)
		if isInvalid(t) {
			return tInvalid, nil
		}
		types = append(types, t)
	}
	switch b := base.(type) {
	case *GenericT:
		sym := b.Sym
		nx := &NamedTypeX{Pos: pos, Name: sym.Name}
		_ = nx
		switch sym.Builtin {
		case "list":
			if len(types) != 1 {
				c.errorf(pos, "list 需要 1 个类型实参")
				return tInvalid, nil
			}
			return &MetaT{&ListT{types[0]}}, types
		case "set":
			if len(types) != 1 {
				c.errorf(pos, "set 需要 1 个类型实参")
				return tInvalid, nil
			}
			c.requireHashable(types[0], pos, "集合元素")
			return &MetaT{&SetT{types[0]}}, types
		case "dict":
			if len(types) != 2 {
				c.errorf(pos, "dict 需要 2 个类型实参")
				return tInvalid, nil
			}
			c.requireHashable(types[0], pos, "字典的键")
			return &MetaT{&DictT{types[0], types[1]}}, types
		case "chan":
			if len(types) != 1 {
				c.errorf(pos, "chan 需要 1 个类型实参")
				return tInvalid, nil
			}
			return &MetaT{&ChanT{types[0]}}, types
		case "iter":
			if len(types) != 1 {
				c.errorf(pos, "iter 需要 1 个类型实参")
				return tInvalid, nil
			}
			return &MetaT{&IterT{types[0]}}, types
		}
		td := sym.TD
		if len(types) != len(td.TParams) {
			c.errorf(pos, "%s 需要 %d 个类型实参，但给了 %d 个", td.Name, len(td.TParams), len(types))
			return tInvalid, nil
		}
		c.checkTArgs(td.TParams, types, pos)
		return &MetaT{&NamedT{D: td, Args: types}}, types
	case *FuncRefT:
		if b.Sig == nil {
			c.errorf(pos, "内置函数 %s 不接受类型实参", b.Name)
			return tInvalid, nil
		}
		if len(types) != len(b.Sig.TParams) {
			c.errorf(pos, "%s 需要 %d 个类型实参，但给了 %d 个", b.Name, len(b.Sig.TParams), len(types))
			return tInvalid, nil
		}
		c.checkTArgs(b.Sig.TParams, types, pos)
		return &FuncRefT{Sig: b.Sig, Name: b.Name}, types
	case *MetaT:
		c.errorf(pos, "%s 不是泛型类型", b.T)
		return tInvalid, nil
	}
	c.errorf(pos, "不能对 %s 指定类型实参", base)
	return tInvalid, nil
}

func (c *Checker) typeApp(e *TypeApp) Type {
	bt := c.expr(e.X, nil)
	if isInvalid(bt) {
		return tInvalid
	}
	t, types := c.instantiate(bt, e.Args, e.Pos)
	e.Types = types
	return t
}

func (c *Checker) index(e *Index, want Type) Type {
	xt := c.expr(e.X, nil)
	if isInvalid(xt) {
		c.exprQuiet(e.Idx)
		return tInvalid
	}
	switch xt.(type) {
	case *GenericT, *FuncRefT, *MetaT:
		args, ok := indexTypeArgs(e.Idx)
		if !ok {
			c.errorf(e.Idx.P(), "这里需要类型实参")
			return tInvalid
		}
		e.IsTypeApp = true
		t, _ := c.instantiate(xt, args, e.Pos)
		return t
	}
	switch x := xt.(type) {
	case *ListT:
		c.exprTo(&e.Idx, tInt, "列表下标")
		return x.Elem
	case *DictT:
		c.exprTo(&e.Idx, x.Key, "字典的键")
		return x.Val
	case *TupleT:
		v, ok := c.constInt(e.Idx)
		c.expr(e.Idx, tInt)
		if !ok {
			c.errorf(e.Idx.P(), "元组的下标必须是整数常量")
			return tInvalid
		}
		if v < 0 {
			v += int64(len(x.Elems))
		}
		if v < 0 || int(v) >= len(x.Elems) {
			c.errorf(e.Idx.P(), "元组下标 %d 超出范围（元组有 %d 个元素）", v, len(x.Elems))
			return tInvalid
		}
		e.TupleIdx = int(v)
		return x.Elems[v]
	case *Basic:
		if x.Kind == BStr {
			c.exprTo(&e.Idx, tInt, "字符串下标")
			return tChar
		}
	case *NamedT:
		if m, sig := c.operatorMethod(xt, "__getitem__"); m != nil {
			e.Ovl = m
			c.exprTo(&e.Idx, sig.Params[0].T, "下标")
			return sig.Ret
		}
	case *OptT:
		c.exprQuiet(e.Idx)
		c.errorf(e.Pos, "值可能为 null，不能直接下标；请先判断或使用 !")
		return tInvalid
	}
	c.exprQuiet(e.Idx)
	c.errorf(e.Pos, "类型 %s 不支持下标", xt)
	return tInvalid
}

func (c *Checker) exprQuiet(e Expr) {
	if e != nil && e.Typ() == nil {
		c.expr(e, nil)
	}
}

func (c *Checker) slice(e *SliceExpr) Type {
	xt := c.expr(e.X, nil)
	for _, p := range []*Expr{&e.Lo, &e.Hi, &e.Step} {
		if *p != nil {
			c.exprTo(p, tInt, "切片下标")
		}
	}
	if isInvalid(xt) {
		return tInvalid
	}
	switch x := xt.(type) {
	case *ListT:
		return x
	case *Basic:
		if x.Kind == BStr {
			return tStr
		}
	case *OptT:
		c.errorf(e.Pos, "值可能为 null，不能直接切片")
		return tInvalid
	}
	c.errorf(e.Pos, "类型 %s 不支持切片", xt)
	return tInvalid
}

// ===== await / as / is =====

func (c *Checker) await(e *Await) Type {
	t := c.expr(e.X, nil)
	if isInvalid(t) {
		return tInvalid
	}
	if n, ok := t.(*NamedT); ok && n.D == c.taskTD && len(n.Args) == 1 {
		return n.Args[0]
	}
	if l, ok := t.(*ListT); ok {
		if n, ok := l.Elem.(*NamedT); ok && n.D == c.taskTD {
			if isVoid(n.Args[0]) {
				return tVoid
			}
			return &ListT{n.Args[0]}
		}
	}
	c.errorf(e.Pos, "await 需要 Task（async 函数的调用结果）或 list[Task]，得到 %s", t)
	return tInvalid
}

const (
	castNoop = iota
	castNumeric
	castAssert
	castEnumToInt
	castIntToEnum
	castOptUnwrapAssert
	castCharInt
)

func (c *Checker) cast(e *Cast) Type {
	xt := c.expr(e.X, nil)
	to := c.resolveType(e.Type, c.scope)
	e.To = to
	if isInvalid(xt) || isInvalid(to) {
		return tInvalid
	}
	result := to
	if e.Safe {
		if isOpt(to) || isAny(to) {
			result = to
		} else {
			result = &OptT{to}
		}
	}
	from := xt
	if o, ok := xt.(*OptT); ok && !isOpt(to) {
		from = o.Elem
		e.Mode = castOptUnwrapAssert
	}
	switch {
	case identical(from, to):
		if e.Mode == castOptUnwrapAssert {
			return result
		}
		c.warnf(e.Pos, "多余的转换：值已经是 %s", to)
		e.Mode = castNoop
		return result
	case isNumeric(from) && isNumeric(to):
		if e.Safe {
			c.errorf(e.Pos, "数字转换不需要 as?，请使用 as")
		}
		if e.Mode == castOptUnwrapAssert {
			c.errorf(e.Pos, "请先判断值是否为 null 再做数字转换")
		}
		e.Mode = castNumeric
		return to
	case (basicKind(from) == BChar && isInteger(to)) || (isInteger(from) && basicKind(to) == BChar):
		e.Mode = castCharInt
		return to
	case isAny(from):
		e.Mode = castAssert
		return result
	}
	fd, td := namedDecl(from), namedDecl(to)
	if fd != nil && fd.Kind == DEnum && !fd.ADT && isInteger(to) {
		e.Mode = castEnumToInt
		return to
	}
	if td != nil && td.Kind == DEnum && !td.ADT && isInteger(from) {
		e.Mode = castIntToEnum
		return to
	}
	if fd != nil && td != nil {
		// 向上转换
		if _, ok := c.assignable(from, to); ok {
			if e.Mode != castOptUnwrapAssert {
				e.Mode = castNoop
			}
			return result
		}
		// 向下转换或接口之间
		if (fd.Kind == DClass || fd.Kind == DInterface) && (td.Kind == DClass || td.Kind == DInterface || td.Kind == DStruct || td.Kind == DEnum) {
			if fd.Kind == DClass && td.Kind == DClass && !isSubclassDecl(td, fd) {
				c.errorf(e.Pos, "%s 不是 %s 的子类，转换一定会失败", to, from)
				return tInvalid
			}
			if e.Mode != castOptUnwrapAssert {
				e.Mode = castAssert
			} else {
				e.Mode = castAssert
			}
			return result
		}
	}
	if fd != nil && fd.Kind == DInterface {
		e.Mode = castAssert
		return result
	}
	d := c.errorf(e.Pos, "不能把 %s 转换为 %s", xt, to)
	if isStr(from) && isNumeric(to) {
		d.Notes = append(d.Notes, "解析字符串请用 int(s) 或 float(s)")
	}
	if isNumeric(from) && isStr(to) {
		d.Notes = append(d.Notes, "转换为字符串请用 str(x)")
	}
	return tInvalid
}

func (c *Checker) isExpr(e *IsExpr) Type {
	xt := c.expr(e.X, nil)
	if isInvalid(xt) {
		return tBool
	}
	if e.Null {
		if !isOpt(xt) && !isAny(xt) {
			c.warnf(e.Pos, "%s 不可能为 null", xt)
		}
		return tBool
	}
	to := c.resolveType(e.Type, c.scope)
	e.To = to
	if isInvalid(to) {
		return tBool
	}
	from := optElem(xt)
	if isAny(from) {
		return tBool
	}
	fd, td := namedDecl(from), namedDecl(to)
	if fd != nil && td != nil {
		if _, ok := c.assignable(from, to); ok {
			c.warnf(e.Pos, "%s 一定是 %s，这个判断总是成立", xt, to)
			return tBool
		}
		if fd.Kind == DClass && td.Kind == DClass && !isSubclassDecl(td, fd) {
			c.errorf(e.Pos, "%s 不可能是 %s", from, to)
			return tBool
		}
		if fd.Kind == DClass || fd.Kind == DInterface {
			return tBool
		}
	}
	if identical(from, to) {
		c.warnf(e.Pos, "这个判断总是成立")
		return tBool
	}
	c.errorf(e.Pos, "is 只能用于 any、类和接口类型的值，不能用于 %s", xt)
	return tBool
}

// ===== 成员访问 =====

func (c *Checker) privateOK(name string, owner *Module) bool {
	if !strings.HasPrefix(name, "_") || strings.HasPrefix(name, "__") {
		return true
	}
	return owner == c.mod || (owner != nil && owner.IsPrelude && c.mod.IsPrelude)
}

func (c *Checker) attr(e *Attr, want Type) Type {
	if _, ok := e.X.(*SuperExpr); ok {
		c.errorf(e.Pos, "super() 只能用来调用基类的方法")
		return tInvalid
	}
	xt := c.expr(e.X, nil)
	if isInvalid(xt) {
		return tInvalid
	}
	switch x := xt.(type) {
	case *ModT:
		return c.moduleMember(e, x.M)
	case *MetaT:
		return c.staticMember(e, x.T)
	case *GenericT:
		c.errorf(e.Pos, "访问 %s 的成员需要先给出类型实参", x.Sym.Name)
		return tInvalid
	case *OptT:
		if !e.Safe {
			d := c.errorf(e.Pos, "值可能为 null，不能直接访问 .%s", e.Name)
			d.Notes = append(d.Notes, "使用 ?. 安全访问，或先判断 if x != null，或用 x! 断言非空")
			return tInvalid
		}
		mt := c.member(e, x.Elem)
		if isInvalid(mt) {
			return tInvalid
		}
		e.RecvT = x.Elem
		if isOpt(mt) || isVoid(mt) || isAny(mt) {
			return mt
		}
		if _, ok := mt.(*FuncT); ok && e.Kind == AttrMethodValue {
			return mt
		}
		return &OptT{mt}
	}
	if e.Safe {
		c.warnf(e.Pos, "%s 不可能为 null，?. 可以改成 .", xt)
		e.Safe = false
	}
	e.RecvT = xt
	return c.member(e, xt)
}

func (c *Checker) moduleMember(e *Attr, m *Module) Type {
	if strings.HasPrefix(e.Name, "_") {
		c.errorf(e.NamePos, "'%s' 是模块 %s 的私有名字", e.Name, m.Name)
		return tInvalid
	}
	sym := m.Scope.Syms[e.Name]
	if sym == nil || sym.Mod != m && sym.Kind != SFunc && sym.Kind != SType {
		if sym == nil {
			c.errorf(e.NamePos, "模块 %s 中没有 '%s'", m.Name, e.Name)
			return tInvalid
		}
	}
	e.Kind = AttrModule
	e.Sym = sym
	id := &Ident{exprBase: exprBase{Pos: e.Pos}, Name: e.Name}
	saveScope := c.scope
	c.scope = m.Scope
	t := c.ident(id, nil)
	c.scope = saveScope
	if sym.Kind == SVar && sym.Pending {
		c.errorf(e.NamePos, "模块 %s 的变量 %s 还没有初始化", m.Name, e.Name)
		return tInvalid
	}
	return t
}

func (c *Checker) staticMember(e *Attr, t Type) Type {
	nt, ok := t.(*NamedT)
	if !ok {
		c.errorf(e.Pos, "类型 %s 没有静态成员 %s", t, e.Name)
		return tInvalid
	}
	d := nt.D
	e.Owner = nt
	if d.Kind == DEnum {
		if v := d.VariantMap[e.Name]; v != nil {
			e.Variant = v
			if v.HasPayload {
				e.Kind = AttrVariantCtor
				ft := &FuncT{Ret: nt}
				m := makeSubst(d.TParams, nt.Args)
				for _, f := range v.Fields {
					ft.Params = append(ft.Params, subst(f.T, m))
				}
				return ft
			}
			if d.ADT && len(d.TParams) > 0 && len(nt.Args) == 0 {
				c.errorf(e.Pos, "泛型枚举成员需要类型实参，例如 %s[int].%s", d.Name, e.Name)
			}
			e.Kind = AttrEnumCase
			return nt
		}
	}
	mem, owner, m := c.findMember(nt, e.Name)
	if mem == nil {
		d := c.errorf(e.NamePos, "%s 没有静态成员 %s", nt, e.Name)
		if nt.D.Kind == DEnum {
			var names []string
			for _, v := range nt.D.Variants {
				names = append(names, v.Name)
			}
			d.Notes = append(d.Notes, "枚举成员有："+strings.Join(names, "、"))
		}
		return tInvalid
	}
	if !c.privateOK(e.Name, nt.D.Mod) {
		c.errorf(e.NamePos, "'%s' 是私有成员，只能在模块 %s 中访问", e.Name, nt.D.Mod.Name)
	}
	e.Owner = owner
	switch x := mem.(type) {
	case *Field:
		if !x.Static {
			c.errorf(e.NamePos, "%s 是实例字段，需要通过对象访问", e.Name)
			return tInvalid
		}
		e.Kind = AttrStaticField
		e.Field = x
		if x.T == nil {
			return tInvalid
		}
		return x.T
	case *Method:
		if !x.Static {
			c.errorf(e.NamePos, "%s 是实例方法，需要通过对象调用", e.Name)
			return tInvalid
		}
		e.Kind = AttrStaticMethod
		e.Method = x
		if len(x.Sig.TParams) > 0 {
			return &FuncRefT{Sig: x.Sig, Name: e.Name}
		}
		return c.sigFuncType(substSig(x.Sig, m))
	case *Prop:
		if !x.Static {
			c.errorf(e.NamePos, "%s 是实例属性，需要通过对象访问", e.Name)
			return tInvalid
		}
		e.Kind = AttrProp
		e.Prop = x
		return x.T
	}
	return tInvalid
}

// member 在值类型 t 上查找成员 e.Name。
func (c *Checker) member(e *Attr, t Type) Type {
	switch x := t.(type) {
	case *NamedT:
		mem, owner, m := c.findMember(x, e.Name)
		if mem != nil {
			if !c.privateOK(e.Name, x.D.Mod) {
				c.errorf(e.NamePos, "'%s' 是私有成员，只能在模块 %s 中访问", e.Name, x.D.Mod.Name)
			}
			e.Owner = owner
			switch mm := mem.(type) {
			case *Field:
				if mm.Static {
					c.errorf(e.NamePos, "静态字段 %s 请通过类型访问：%s.%s", e.Name, x.D.Name, e.Name)
					return tInvalid
				}
				e.Kind = AttrField
				e.Field = mm
				if mm.T == nil {
					c.errorf(e.NamePos, "字段 %s 的类型尚未确定", e.Name)
					return tInvalid
				}
				return subst(mm.T, m)
			case *Prop:
				e.Kind = AttrProp
				e.Prop = mm
				return subst(mm.T, m)
			case *Method:
				if mm.Static {
					c.errorf(e.NamePos, "静态方法 %s 请通过类型调用：%s.%s(...)", e.Name, x.D.Name, e.Name)
					return tInvalid
				}
				e.Kind = AttrMethodValue
				e.Method = mm
				if len(mm.Sig.TParams) > 0 {
					return &FuncRefT{Sig: substSig(mm.Sig, m), Name: e.Name}
				}
				return c.sigFuncType(substSig(mm.Sig, m))
			}
		}
		if x.D.Kind == DEnum && !x.D.ADT {
			switch e.Name {
			case "name":
				e.Kind = AttrBuiltinProp
				return tStr
			case "value":
				e.Kind = AttrBuiltinProp
				return tInt
			}
		}
		if x.D.Kind == DEnum {
			if v := x.D.VariantMap[e.Name]; v != nil {
				c.errorf(e.NamePos, "枚举成员请通过类型访问：%s.%s", x.D.Name, e.Name)
				return tInvalid
			}
		}
	case *TupleT:
		if len(e.Name) > 0 && e.Name[0] >= '0' && e.Name[0] <= '9' {
			var idx int
			fmt.Sscanf(e.Name, "%d", &idx)
			if idx >= len(x.Elems) {
				c.errorf(e.NamePos, "元组只有 %d 个元素", len(x.Elems))
				return tInvalid
			}
			e.Kind = AttrTupleField
			e.Field = &Field{Name: e.Name}
			return x.Elems[idx]
		}
	case *TParamT:
		if x.P.Bound == BoundIface {
			return c.member(e, x.P.Iface)
		}
	case *Basic:
		if x.Kind == BAny {
			d := c.errorf(e.NamePos, "any 类型的值没有成员 %s", e.Name)
			d.Notes = append(d.Notes, "先用 as 转换为具体类型，例如 (x as str)."+e.Name)
			return tInvalid
		}
	}
	// 扩展方法与扩展属性
	if cands := c.extCandidates(t, e.Name); len(cands) > 0 {
		cd := cands[0]
		if cd.prop != nil {
			e.Kind = AttrProp
			e.Prop = cd.prop
			return subst(cd.prop.T, cd.subst)
		}
		e.Kind = AttrMethodValue
		e.Method = cd.method
		sig := substSig(cd.method.Sig, cd.subst)
		if len(sig.TParams) > 0 {
			return &FuncRefT{Sig: sig, Name: e.Name}
		}
		return c.sigFuncType(sig)
	}
	if bad := c.extUnsatisfied(t, e.Name); bad != "" {
		c.errorf(e.NamePos, "%s", bad)
		return tInvalid
	}
	d := c.errorf(e.NamePos, "类型 %s 没有成员 %s", t, e.Name)
	if s := c.suggestMember(t, e.Name); s != "" {
		d.Notes = append(d.Notes, "是不是想写 ."+s+"？")
	}
	return tInvalid
}

type extCand struct {
	method *Method
	prop   *Prop
	block  *ExtBlock
	subst  Subst
}

// extCandidates 查找适用于类型 t 的扩展成员。
func (c *Checker) extCandidates(t Type, name string) []extCand {
	var out []extCand
	key := typeHead(t)
	for _, blk := range c.exts[key] {
		ms := blk.Methods[name]
		p := blk.Props[name]
		if len(ms) == 0 && p == nil {
			continue
		}
		m := Subst{}
		free := map[*TypeParam]bool{}
		for _, tp := range blk.TParams {
			free[tp] = true
		}
		if !c.unify(blk.Target, t, m, free) {
			continue
		}
		ok := true
		for _, tp := range blk.TParams {
			if m[tp] == nil {
				ok = false
				break
			}
			if good, _ := c.satisfies(m[tp], tp); !good {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if !c.privateOK(name, blk.Mod) {
			continue
		}
		if p != nil {
			out = append(out, extCand{prop: p, block: blk, subst: m})
		}
		for _, mt := range ms {
			out = append(out, extCand{method: mt, block: blk, subst: m})
		}
	}
	return out
}

// extUnsatisfied 若存在同名扩展但约束不满足，返回说明。
func (c *Checker) extUnsatisfied(t Type, name string) string {
	key := typeHead(t)
	for _, blk := range c.exts[key] {
		if len(blk.Methods[name]) == 0 && blk.Props[name] == nil {
			continue
		}
		m := Subst{}
		free := map[*TypeParam]bool{}
		for _, tp := range blk.TParams {
			free[tp] = true
		}
		if !c.unify(blk.Target, t, m, free) {
			continue
		}
		for _, tp := range blk.TParams {
			if m[tp] == nil {
				continue
			}
			if good, why := c.satisfies(m[tp], tp); !good {
				return fmt.Sprintf("%s 的方法 %s 要求元素满足 %s：%s", t, name, tp.BoundName(), why)
			}
		}
	}
	return ""
}

func (c *Checker) suggestMember(t Type, name string) string {
	var names []string
	if nt, ok := t.(*NamedT); ok {
		for _, anc := range supertypes(nt) {
			for n := range anc.D.FieldMap {
				names = append(names, n)
			}
			for n := range anc.D.Methods {
				names = append(names, n)
			}
			for n := range anc.D.Props {
				names = append(names, n)
			}
		}
	}
	for _, blk := range c.exts[typeHead(t)] {
		for n := range blk.Methods {
			names = append(names, n)
		}
		for n := range blk.Props {
			names = append(names, n)
		}
	}
	aliases := map[string]string{
		"length": "len(x)", "size": "len(x)", "count": "len(x)", "push": "append", "add": "append",
		"toUpperCase": "upper", "toLowerCase": "lower", "ToUpper": "upper", "ToLower": "lower",
		"trim": "strip", "Trim": "strip", "toString": "str(x)", "ToString": "str(x)",
		"contains": "in 运算符", "Contains": "in 运算符", "indexOf": "index", "IndexOf": "index",
		"forEach": "for 循环", "Length": "len(x)", "Count": "len(x)", "substring": "切片 s[a:b]",
		"charAt": "下标 s[i]", "equals": "==", "Equals": "==", "isEmpty": "len(x) == 0",
	}
	best, bestD := "", 3
	for _, n := range names {
		if d := editDistance(name, n); d < bestD {
			best, bestD = n, d
		}
	}
	if best == "" {
		if a, ok := aliases[name]; ok {
			return a
		}
	}
	return best
}

// ===== 合一（泛型推断） =====

// unify 把含自由类型参数的模式 p 与实际类型 a 合一，结果写入 m。
func (c *Checker) unify(p, a Type, m Subst, free map[*TypeParam]bool) bool {
	if p == nil || a == nil {
		return false
	}
	if isInvalid(a) {
		return true
	}
	switch x := p.(type) {
	case *TParamT:
		if !free[x.P] {
			if y, ok := a.(*TParamT); ok && y.P == x.P {
				return true
			}
			if bound := m[x.P]; bound != nil {
				if identical(bound, a) {
					return true
				}
				_, ok := c.assignable(a, bound)
				return ok
			}
			return identical(p, a)
		}
		if cur := m[x.P]; cur != nil {
			if identical(cur, a) {
				return true
			}
			if isNull(a) {
				return true
			}
			if j := c.join(cur, a); j != nil {
				m[x.P] = j
				return true
			}
			return false
		}
		if isNull(a) {
			return true
		}
		m[x.P] = a
		return true
	case *ListT:
		if y, ok := a.(*ListT); ok {
			return c.unify(x.Elem, y.Elem, m, free)
		}
	case *SetT:
		if y, ok := a.(*SetT); ok {
			return c.unify(x.Elem, y.Elem, m, free)
		}
	case *ChanT:
		if y, ok := a.(*ChanT); ok {
			return c.unify(x.Elem, y.Elem, m, free)
		}
	case *IterT:
		switch y := a.(type) {
		case *IterT:
			return c.unify(x.Elem, y.Elem, m, free)
		case *ListT:
			return c.unify(x.Elem, y.Elem, m, free)
		case *SetT:
			return c.unify(x.Elem, y.Elem, m, free)
		case *DictT:
			return c.unify(x.Elem, y.Key, m, free)
		case *ChanT:
			return c.unify(x.Elem, y.Elem, m, free)
		case *Basic:
			if y.Kind == BStr {
				return c.unify(x.Elem, tChar, m, free)
			}
		}
	case *DictT:
		if y, ok := a.(*DictT); ok {
			return c.unify(x.Key, y.Key, m, free) && c.unify(x.Val, y.Val, m, free)
		}
	case *OptT:
		if y, ok := a.(*OptT); ok {
			return c.unify(x.Elem, y.Elem, m, free)
		}
		if isNull(a) {
			return true
		}
		return c.unify(x.Elem, a, m, free)
	case *TupleT:
		if y, ok := a.(*TupleT); ok && len(x.Elems) == len(y.Elems) {
			for i := range x.Elems {
				if !c.unify(x.Elems[i], y.Elems[i], m, free) {
					return false
				}
			}
			return true
		}
	case *FuncT:
		if y, ok := a.(*FuncT); ok && len(x.Params) == len(y.Params) {
			for i := range x.Params {
				if !c.unify(x.Params[i], y.Params[i], m, free) {
					return false
				}
			}
			if x.Ret != nil && y.Ret != nil {
				if isVoid(x.Ret) {
					return true
				}
				return c.unify(x.Ret, y.Ret, m, free)
			}
			return true
		}
	case *NamedT:
		if y, ok := a.(*NamedT); ok {
			if y.D == x.D {
				for i := range x.Args {
					if i >= len(y.Args) || !c.unify(x.Args[i], y.Args[i], m, free) {
						return false
					}
				}
				return true
			}
			// 子类：沿祖先查找
			for _, anc := range supertypes(y)[1:] {
				if anc.D == x.D {
					return c.unify(x, anc, m, free)
				}
			}
			if x.D.Kind == DInterface {
				return c.unifyIface(x, a, m, free)
			}
		}
		if x.D.Kind == DInterface {
			return c.unifyIface(x, a, m, free)
		}
	default:
		if identical(p, a) {
			return true
		}
		if isNumeric(p) && isNumeric(a) {
			return true // 交给后续的转换检查
		}
		if isAny(p) {
			return true
		}
		_, ok := c.assignable(a, p)
		return ok
	}
	return false
}

// unifyIface 通过结构化实现推断接口的类型实参：把接口方法的签名与实际类型的同名方法合一。
func (c *Checker) unifyIface(x *NamedT, a Type, m Subst, free map[*TypeParam]bool) bool {
	if !containsTParam(x, free) {
		return true // 是否真的实现由之后的可赋值检查决定
	}
	if an, ok := a.(*NamedT); ok && an.D.Kind == DInterface && an.D == x.D {
		for i := range x.Args {
			if i < len(an.Args) && !c.unify(x.Args[i], an.Args[i], m, free) {
				return false
			}
		}
		return true
	}
	methods, _, propT := c.ifaceMembers(x)
	ok := true
	for name, want := range methods {
		got := c.methodSigOf(a, name)
		if got == nil || len(got.Params) != len(want.Params) {
			return false
		}
		for i := range want.Params {
			if !c.unify(want.Params[i].T, got.Params[i].T, m, free) {
				ok = false
			}
		}
		if !c.unify(want.Ret, got.Ret, m, free) {
			ok = false
		}
	}
	for name, pt := range propT {
		if got, _ := c.propTypeOf(a, name); got != nil {
			c.unify(pt, got, m, free)
		}
	}
	return ok
}
