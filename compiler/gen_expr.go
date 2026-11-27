package compiler

import (
	"fmt"
	"strconv"
	"strings"
)

func (g *Gen) expr(e Expr) string {
	switch x := e.(type) {
	case *goExpr:
		return x.code
	case *Ident:
		return g.ident(x)
	case *IntLit:
		return g.intLit(x, false)
	case *FloatLit:
		if basicKind(x.T) == BF32 {
			return "float32(" + x.Text + ")"
		}
		if tp, ok := x.T.(*TParamT); ok {
			return g.tpName(tp.P) + "(" + x.Text + ")"
		}
		return x.Text
	case *StrLit:
		return strconv.Quote(x.Val)
	case *CharLit:
		return g.rt() + ".Char(" + strconv.QuoteRune(x.Val) + ")"
	case *BoolLit:
		if x.Val {
			return "true"
		}
		return "false"
	case *NullLit:
		return "nil"
	case *FString:
		return g.fstring(x)
	case *ListLit:
		return g.listLit(x)
	case *SetLit:
		et := x.T.(*SetT).Elem
		if hasStar(x.Elems) {
			return g.starLit(x.T, et, x.Elems, "Add", "UpdateIter")
		}
		var parts []string
		for _, el := range x.Elems {
			parts = append(parts, g.expr(el))
		}
		return fmt.Sprintf("%s.SetOf[%s](%s)", g.rt(), g.typ(et), strings.Join(parts, ", "))
	case *DictLit:
		return g.dictLit(x)
	case *TupleLit:
		var parts []string
		for _, el := range x.Elems {
			parts = append(parts, g.expr(el))
		}
		return g.typ(x.T) + "{" + strings.Join(parts, ", ") + "}"
	case *Comp:
		return g.comp(x)
	case *Unary:
		return g.unary(x)
	case *Binary:
		if x.Op2 != nil {
			if x.Rev {
				return g.methodOn(x.Y, x.Op2, g.expr(x.Y), []string{g.expr(x.X)}, nil)
			}
			return g.methodOn(x.X, x.Op2, g.expr(x.X), []string{g.expr(x.Y)}, nil)
		}
		xc := g.expr(x.X)
		if !g.inConst && isGoConst(x.X) && isGoConst(x.Y) && isNumeric(x.X.Typ()) {
			xc = g.rt() + ".Val[" + g.typ(x.X.Typ()) + "](" + xc + ")"
		}
		return g.binCode(x.Op, xc, x.X.Typ(), g.expr(x.Y), x.Y.Typ(), x.T)
	case *Compare:
		return g.compare(x)
	case *Logical:
		op := "&&"
		if x.Op == KOr {
			op = "||"
		}
		return "(" + g.expr(x.X) + " " + op + " " + g.expr(x.Y) + ")"
	case *Ternary:
		return g.ternary(x)
	case *NullCo:
		return g.nullCo(x)
	case *Lambda:
		return g.lambda(x)
	case *Call:
		return g.call(x)
	case *Index:
		return g.index(x)
	case *SliceExpr:
		lo, hi, step := "rt.NoIdx", "rt.NoIdx", "1"
		rt := g.rt()
		lo, hi = rt+".NoIdx", rt+".NoIdx"
		if x.Lo != nil {
			lo = g.expr(x.Lo)
		}
		if x.Hi != nil {
			hi = g.expr(x.Hi)
		}
		if x.Step != nil {
			step = g.expr(x.Step)
		}
		if isStr(x.X.Typ()) {
			return fmt.Sprintf("%s.StrSlice(%s, %s, %s, %s)", rt, g.expr(x.X), lo, hi, step)
		}
		return fmt.Sprintf("%s.Slice(%s, %s, %s)", g.expr(x.X), lo, hi, step)
	case *Attr:
		return g.attr(x)
	case *Unwrap:
		if isAny(x.X.Typ()) {
			return g.rt() + ".NotNull(" + g.expr(x.X) + ")"
		}
		if !isOpt(x.X.Typ()) {
			return g.expr(x.X)
		}
		return g.expr(x.X) + ".Must()"
	case *Await:
		t := x.X.Typ()
		if l, ok := t.(*ListT); ok {
			if n, ok := l.Elem.(*NamedT); ok && len(n.Args) == 1 && isVoid(n.Args[0]) {
				return g.rt() + ".AwaitAllVoid(" + g.expr(x.X) + ")"
			}
			return g.rt() + ".AwaitAll(" + g.expr(x.X) + ")"
		}
		return g.expr(x.X) + ".Await()"
	case *Cast:
		return g.cast(x)
	case *IsExpr:
		return g.isExpr(x)
	case *ConvExpr:
		return g.convCode(g.expr(x.X), x.Kind, x.From, x.T)
	case *TypeApp:
		return g.expr(x.X)
	case *NamedExpr:
		// 变量已提升到函数开头声明，闭包给它赋值后返回它
		name := g.symName(x.Target.Sym)
		return fmt.Sprintf("func() %s { %s = %s; return %s }()", g.typ(x.T), name, g.expr(x.Value), name)
	}
	return "nil /* ? */"
}

// isGoConst 判断生成的 Go 表达式是否会被 Go 当作常量（从而以任意精度折叠）。
func isGoConst(e Expr) bool {
	if isConstExpr(e) {
		return true
	}
	switch x := e.(type) {
	case *Call:
		if x.Kind == CallConv && len(x.Final) == 1 && isNumeric(x.ConvTo) && isNumeric(x.Final[0].Typ()) {
			return isGoConst(x.Final[0])
		}
	case *Cast:
		if x.Mode == castNumeric {
			return isGoConst(x.X)
		}
	case *ConvExpr:
		if x.Kind == ConvNumeric {
			return isGoConst(x.X)
		}
	case *Unary:
		if x.Op == TMinus || x.Op == TPlus || x.Op == TTilde {
			return x.Op2 == nil && isGoConst(x.X)
		}
	case *Binary:
		return x.Op2 == nil && isGoConst(x.X) && isGoConst(x.Y)
	}
	return false
}

func (g *Gen) intLit(x *IntLit, neg bool) string {
	text := x.Text
	if neg != x.Neg {
		text = "-" + text
	}
	t := x.T
	if tp, ok := t.(*TParamT); ok {
		return g.tpName(tp.P) + "(" + text + ")"
	}
	switch basicKind(t) {
	case BInt, BInvalid:
		return text
	case BFloat:
		return "float64(" + text + ")"
	}
	return g.typ(t) + "(" + text + ")"
}

func (g *Gen) ident(x *Ident) string {
	if x.Lam != nil {
		return g.lambda(x.Lam)
	}
	sym := x.Sym
	if sym == nil {
		return x.Name
	}
	switch sym.Kind {
	case SVar, SConst:
		name := g.symName(sym)
		switch x.Narrow {
		case NarrowUnwrap:
			return name + ".V"
		case NarrowCast:
			base := name
			if isOpt(x.From) {
				base += ".V"
				if identical(optElem(x.From), x.T) {
					return base
				}
			}
			return base + ".(" + g.typ(x.T) + ")"
		}
		return name
	case SFunc:
		if sym.LocalFunc {
			return g.local(sym)
		}
		return g.symName(sym)
	}
	return g.symName(sym)
}

// ===== 字面量 =====

// toStr 把任意值转换为字符串表达式。
func (g *Gen) toStr(code string, t Type) string {
	switch {
	case isStr(t):
		return code
	case basicKind(t) == BInt:
		return g.pkg("strconv") + ".Itoa(" + code + ")"
	case basicKind(t) == BFloat:
		return g.rt() + ".FloatStr(" + code + ")"
	case isBool(t):
		return g.rt() + ".BoolStr(" + code + ")"
	case basicKind(t) == BChar:
		return g.rt() + ".CharStr(" + code + ")"
	}
	return g.rt() + ".Str(" + code + ")"
}

func (g *Gen) fstring(x *FString) string {
	var parts []string
	for _, p := range x.Parts {
		if !p.IsExpr {
			if p.Lit != "" {
				parts = append(parts, strconv.Quote(p.Lit))
			}
			continue
		}
		code := g.expr(p.X)
		t := p.X.Typ()
		var s string
		switch {
		case p.Spec != "":
			s = fmt.Sprintf("%s.Fmt(%s, %q)", g.rt(), code, p.Spec)
		case p.Conv == 'r' || (p.Debug && p.Conv != 's'):
			s = g.rt() + ".Repr(" + code + ")"
		default:
			s = g.toStr(code, t)
		}
		if p.Debug {
			parts = append(parts, strconv.Quote(p.Lit))
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return `""`
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, " + ") + ")"
}

func (g *Gen) listLit(x *ListLit) string {
	lt, ok := x.T.(*ListT)
	if !ok {
		return "nil"
	}
	if len(x.Elems) == 0 {
		return fmt.Sprintf("%s.NewList[%s](0)", g.rt(), g.typ(lt.Elem))
	}
	if hasStar(x.Elems) {
		return g.starLit(lt, lt.Elem, x.Elems, "Append", "Extend")
	}
	var parts []string
	for _, el := range x.Elems {
		parts = append(parts, g.expr(el))
	}
	return fmt.Sprintf("%s.ListOf[%s](%s)", g.rt(), g.typ(lt.Elem), strings.Join(parts, ", "))
}

func (g *Gen) dictLit(x *DictLit) string {
	rt := g.rt()
	switch t := x.T.(type) {
	case *SetT:
		return fmt.Sprintf("%s.NewSet[%s](0)", rt, g.typ(t.Elem))
	case *DictT:
		kt, vt := g.typ(t.Key), g.typ(t.Val)
		if len(x.Keys) == 0 {
			return fmt.Sprintf("%s.NewDict[%s, %s](0)", rt, kt, vt)
		}
		if hasStar(x.Keys) {
			// {**a, k: v, **b}：按顺序写入，后面的覆盖前面的
			r := g.tmp()
			var b strings.Builder
			fmt.Fprintf(&b, "func() %s { %s := %s.NewDict[%s, %s](0)", g.typ(t), r, rt, kt, vt)
			for i, k := range x.Keys {
				if st, ok := k.(*StarExpr); ok {
					fmt.Fprintf(&b, "; %s.Update(%s)", r, g.expr(st.X))
				} else {
					fmt.Fprintf(&b, "; %s.Set(%s, %s)", r, g.expr(k), g.expr(x.Vals[i]))
				}
			}
			fmt.Fprintf(&b, "; return %s }()", r)
			return b.String()
		}
		var ks, vs []string
		for i := range x.Keys {
			ks = append(ks, g.expr(x.Keys[i]))
			vs = append(vs, g.expr(x.Vals[i]))
		}
		return fmt.Sprintf("%s.DictLit[%s, %s]([]%s{%s}, []%s{%s})", rt, kt, vt, kt, strings.Join(ks, ", "), vt, strings.Join(vs, ", "))
	}
	return "nil"
}

// starLit 生成含有 *展开 的列表/集合字面量：逐个加入元素，展开的部分整体加入。
func (g *Gen) starLit(t Type, et Type, elems []Expr, add, addAll string) string {
	r := g.tmp()
	ctor := "NewList"
	if _, ok := t.(*SetT); ok {
		ctor = "NewSet"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "func() %s { %s := %s.%s[%s](0)", g.typ(t), r, g.rt(), ctor, g.typ(et))
	for _, el := range elems {
		if st, ok := el.(*StarExpr); ok {
			fmt.Fprintf(&b, "; %s.%s(%s)", r, addAll, g.expr(st.X))
		} else {
			fmt.Fprintf(&b, "; %s.%s(%s)", r, add, g.expr(el))
		}
	}
	fmt.Fprintf(&b, "; return %s }()", r)
	return b.String()
}

// inline 把生成语句的函数捕获为单行代码。
func (g *Gen) inline(fn func()) string {
	save, saveInd := g.w, g.ind
	var b strings.Builder
	g.w = &b
	g.ind = 0
	saveNoLines := g.Options.NoLines
	g.Options.NoLines = true
	fn()
	g.Options.NoLines = saveNoLines
	g.w, g.ind = save, saveInd
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "//line") {
			continue
		}
		out = append(out, l)
	}
	// 合并为一行：在 { 之后和 } 之前不需要分号
	var sb strings.Builder
	for i, l := range out {
		if i > 0 {
			prev := out[i-1]
			if strings.HasSuffix(prev, "{") || strings.HasPrefix(l, "}") {
				sb.WriteString(" ")
			} else {
				sb.WriteString("; ")
			}
		}
		sb.WriteString(l)
	}
	return sb.String()
}

func (g *Gen) comp(x *Comp) string {
	rt := g.rt()
	r := g.tmp()
	saveFn := g.fn
	if g.fn == nil {
		g.fn = &genFn{retT: tVoid}
	}
	inner := g.fn
	_ = inner
	body := g.inline(func() {
		saveLoops, saveTry := g.fn.loops, g.fn.tryDepth
		g.fn.loops = nil
		g.fn.tryDepth = 0
		opened := 0
		var emit func(i int)
		emit = func(i int) {
			if i == len(x.Clauses) {
				switch x.Kind {
				case CompList:
					g.line("%s.Append(%s)", r, g.expr(x.Elem))
				case CompSet:
					g.line("%s.Add(%s)", r, g.expr(x.Elem))
				case CompDict:
					g.line("%s.Set(%s, %s)", r, g.expr(x.Key), g.expr(x.Val))
				case CompGen:
					g.line("if !%s(%s) {", r, g.expr(x.Elem))
					g.line("return")
					g.line("}")
				}
				return
			}
			cl := x.Clauses[i]
			if cl.IsIf {
				g.line("if %s {", g.cond(cl.Cond))
				emit(i + 1)
				g.line("}")
				return
			}
			lp := g.pushLoop()
			header, pre := g.loopHead(cl.Target, cl.Iter, cl.Loop, nil)
			g.line("%s {", header)
			if pre != nil {
				pre()
			}
			emit(i + 1)
			g.line("}")
			g.popLoop()
			_ = lp
			opened++
		}
		emit(0)
		g.fn.loops, g.fn.tryDepth = saveLoops, saveTry
	})
	g.fn = saveFn
	switch t := x.T.(type) {
	case *ListT:
		return fmt.Sprintf("func() %s { %s := %s.NewList[%s](0); %s; return %s }()", g.typ(t), r, rt, g.typ(t.Elem), body, r)
	case *SetT:
		return fmt.Sprintf("func() %s { %s := %s.NewSet[%s](0); %s; return %s }()", g.typ(t), r, rt, g.typ(t.Elem), body, r)
	case *DictT:
		return fmt.Sprintf("func() %s { %s := %s.NewDict[%s, %s](0); %s; return %s }()", g.typ(t), r, rt, g.typ(t.Key), g.typ(t.Val), body, r)
	case *IterT:
		return fmt.Sprintf("func(%s func(%s) bool) { %s }", r, g.typ(t.Elem), body)
	}
	return "nil"
}

// ===== 运算符 =====

func (g *Gen) unary(x *Unary) string {
	if x.Op2 != nil {
		return g.methodOn(x.X, x.Op2, g.expr(x.X), nil, nil)
	}
	switch x.Op {
	case KNot:
		return "!" + g.paren(g.expr(x.X))
	case TMinus:
		if il, ok := x.X.(*IntLit); ok {
			return g.intLit(il, true)
		}
		return "(-" + g.expr(x.X) + ")"
	case TPlus:
		return g.expr(x.X)
	case TTilde:
		return "(^" + g.expr(x.X) + ")"
	}
	return g.expr(x.X)
}

func (g *Gen) paren(s string) string {
	if isIdentLike(s) || (strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") && balanced(s[1:len(s)-1])) {
		return s
	}
	return "(" + s + ")"
}

func isIdentLike(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r == '_' || r == '.' || r == 'ꞏ' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r > 127) {
			return false
		}
	}
	return true
}

// binCode 生成二元运算（操作数已转换为合适的类型）。
func (g *Gen) binCode(op TokKind, x string, xt Type, y string, yt Type, rt Type) string {
	r := g.rt()
	switch op {
	case TPlus:
		if _, ok := xt.(*ListT); ok {
			return x + ".Concat(" + y + ")"
		}
		if isStr(xt) && basicKind(yt) == BChar {
			return "(" + x + " + " + r + ".CharStr(" + y + "))"
		}
		if basicKind(xt) == BChar && isStr(yt) {
			return "(" + r + ".CharStr(" + x + ") + " + y + ")"
		}
		if basicKind(xt) == BChar && !isNumeric(xt) {
			return "(" + x + " + " + r + ".Char(" + y + "))"
		}
		return "(" + x + " + " + y + ")"
	case TMinus:
		if basicKind(xt) == BChar {
			if basicKind(yt) == BChar {
				return "int(" + x + " - " + y + ")"
			}
			return "(" + x + " - " + r + ".Char(" + y + "))"
		}
		if _, ok := xt.(*SetT); ok {
			return x + ".Diff(" + y + ")"
		}
		return "(" + x + " - " + y + ")"
	case TStar:
		if isStr(xt) {
			return r + ".StrRepeat(" + x + ", " + y + ")"
		}
		if isStr(yt) {
			return r + ".StrRepeat(" + y + ", " + x + ")"
		}
		if basicKind(xt) == BChar && isInteger(yt) {
			return r + ".StrRepeat(" + r + ".CharStr(" + x + "), " + y + ")"
		}
		if basicKind(yt) == BChar && isInteger(xt) {
			return r + ".StrRepeat(" + r + ".CharStr(" + y + "), " + x + ")"
		}
		if _, ok := xt.(*ListT); ok {
			return x + ".Repeat(" + y + ")"
		}
		return "(" + x + " * " + y + ")"
	case TSlash:
		return "(" + x + " / " + y + ")"
	case TDSlash:
		switch {
		case isInteger(xt):
			return r + ".FloorDiv(" + x + ", " + y + ")"
		case basicKind(xt) == BF32:
			return r + ".FFloorDiv32(" + x + ", " + y + ")"
		}
		return r + ".FFloorDiv(" + x + ", " + y + ")"
	case TPercent:
		switch {
		case isInteger(xt):
			return r + ".Mod(" + x + ", " + y + ")"
		case basicKind(xt) == BF32:
			return r + ".FMod32(" + x + ", " + y + ")"
		}
		return r + ".FMod(" + x + ", " + y + ")"
	case TDStar:
		switch {
		case basicKind(xt) == BInt && basicKind(yt) == BInt:
			return r + ".IPowInt(" + x + ", " + y + ")"
		case isInteger(xt):
			return r + ".IPow(" + x + ", " + y + ")"
		case basicKind(xt) == BF32:
			return r + ".Pow32(" + x + ", " + y + ")"
		}
		return g.pkg("math") + ".Pow(" + x + ", " + y + ")"
	case TAmp, TPipe, TCaret:
		if isBool(xt) {
			return r + map[TokKind]string{TAmp: ".BoolAnd(", TPipe: ".BoolOr(", TCaret: ".BoolXor("}[op] + x + ", " + y + ")"
		}
		if _, ok := xt.(*SetT); ok {
			return x + map[TokKind]string{TAmp: ".Intersect(", TPipe: ".Union(", TCaret: ".SymDiff("}[op] + y + ")"
		}
		if _, ok := xt.(*DictT); ok {
			return x + ".Merge(" + y + ")"
		}
		return "(" + x + " " + op.String() + " " + y + ")"
	case TShl, TShr:
		return "(" + x + " " + op.String() + " " + y + ")"
	}
	return "(" + x + " " + op.String() + " " + y + ")"
}

// eqCode 生成相等比较。
func (g *Gen) eqCode(a, b string, t Type, optSubj bool) string {
	if optSubj {
		if g.goEqOK(t) {
			return "(" + a + ".Ok && " + a + ".V == " + b + ")"
		}
		return "(" + a + ".Ok && " + g.rt() + ".Equal(" + a + ".V, " + b + "))"
	}
	if g.goEqOK(t) {
		return "(" + a + " == " + b + ")"
	}
	return g.rt() + ".Equal(" + a + ", " + b + ")"
}

func (g *Gen) cmp1(e *Compare, i int, a, b string) string {
	op := e.Ops[i]
	xa, xb := e.Xs[i], e.Xs[i+1]
	at, bt := xa.Typ(), xb.Typ()
	if m := e.Ovl[i]; m != nil {
		var code string
		if op == KIn || op == KNotIn {
			code = g.methodOn(xb, m, b, []string{a}, nil)
			if op == KNotIn {
				return "!" + code
			}
			return code
		}
		if e.Swap[i] {
			code = g.methodOn(xb, m, b, []string{a}, nil)
		} else {
			code = g.methodOn(xa, m, a, []string{b}, nil)
		}
		neg := e.Neg[i]
		if op == TNe && m.Name == "__eq__" {
			neg = true
		}
		if neg {
			return "!" + code
		}
		return code
	}
	switch op {
	case TEq, TNe:
		_, an := unconv(xa).(*NullLit)
		_, bn := unconv(xb).(*NullLit)
		if an || bn {
			other, ot := a, at
			if an {
				other, ot = b, bt
			}
			if an && bn {
				return map[TokKind]string{TEq: "true", TNe: "false"}[op]
			}
			var isNull string
			switch {
			case isOpt(ot):
				isNull = "!" + other + ".Ok"
			case isAny(ot):
				isNull = "(" + other + " == nil)"
			default:
				isNull = "false"
			}
			if op == TNe {
				if isNull == "false" {
					return "true"
				}
				if strings.HasPrefix(isNull, "!") {
					return isNull[1:]
				}
				return "!" + isNull
			}
			return isNull
		}
		t := at
		if isAny(at) || isAny(bt) {
			code := g.rt() + ".Equal[any](" + a + ", " + b + ")"
			if op == TNe {
				return "!" + code
			}
			return code
		}
		if !identical(at, bt) && (isPolyType(at) || isPolyType(bt) || isClassType(at) || isClassType(bt)) {
			// 类与接口之间按身份比较
			code := "(" + a + " == " + b + ")"
			if op == TNe {
				return "(" + a + " != " + b + ")"
			}
			return code
		}
		code := g.eqCode(a, b, t, false)
		if op == TNe {
			if strings.HasPrefix(code, "(") && strings.Contains(code, " == ") && g.goEqOK(t) {
				return "(" + a + " != " + b + ")"
			}
			return "!" + code
		}
		return code
	case TLt, TGt, TLe, TGe:
		if _, ok := at.(*TupleT); ok {
			return fmt.Sprintf("(%s.Compare(%s, %s) %s 0)", g.rt(), a, b, op)
		}
		if tp, ok := at.(*TParamT); ok && tp.P.Bound == BoundSortable {
			return fmt.Sprintf("(%s.Compare(%s, %s) %s 0)", g.rt(), a, b, op)
		}
		code := "(" + a + " " + op.String() + " " + b + ")"
		if e.Neg[i] {
			return "!" + code
		}
		return code
	case KIn, KNotIn:
		var code string
		switch ct := bt.(type) {
		case *ListT:
			if g.goEqOK(ct.Elem) {
				code = g.rt() + ".ListHas(" + b + ", " + a + ")"
			} else {
				code = g.rt() + ".ListContains(" + b + ", " + a + ")"
			}
		case *SetT, *DictT:
			code = b + ".Has(" + a + ")"
		case *Basic:
			if basicKind(at) == BChar {
				code = g.pkg("strings") + ".ContainsRune(" + b + ", rune(" + a + "))"
			} else {
				code = g.pkg("strings") + ".Contains(" + b + ", " + a + ")"
			}
		default:
			code = "false"
		}
		if op == KNotIn {
			return "!" + code
		}
		return code
	}
	return "false"
}

func (g *Gen) compare(e *Compare) string {
	if len(e.Ops) == 1 {
		return g.cmp1(e, 0, g.expr(e.Xs[0]), g.expr(e.Xs[1]))
	}
	// 链式比较：每个操作数只求值一次，并且短路
	var sb strings.Builder
	sb.WriteString("func() bool { ")
	prev := g.tmp()
	fmt.Fprintf(&sb, "%s := %s; ", prev, g.expr(e.Xs[0]))
	for i := range e.Ops {
		cur := g.tmp()
		fmt.Fprintf(&sb, "%s := %s; ", cur, g.expr(e.Xs[i+1]))
		test := g.cmp1(e, i, prev, cur)
		if i == len(e.Ops)-1 {
			fmt.Fprintf(&sb, "return %s }()", test)
		} else {
			fmt.Fprintf(&sb, "if !%s { return false }; ", g.paren(test))
		}
		prev = cur
	}
	return sb.String()
}

func isSimpleExpr(e Expr) bool {
	switch x := e.(type) {
	case *IntLit, *FloatLit, *StrLit, *BoolLit, *CharLit, *NullLit:
		return true
	case *Ident:
		return x.Narrow != NarrowCast
	case *ConvExpr:
		switch x.Kind {
		case ConvNumeric, ConvWrap, ConvNull, ConvToAny, ConvIface:
			return isSimpleExpr(x.X)
		}
	case *Unary:
		return x.Op == TMinus && isSimpleExpr(x.X) && x.Op2 == nil
	}
	return false
}

func (g *Gen) ternary(x *Ternary) string {
	c := g.expr(x.Cond)
	a := g.expr(x.Then)
	b := g.expr(x.Else)
	t := g.typ(x.T)
	if isSimpleExpr(x.Then) && isSimpleExpr(x.Else) {
		return fmt.Sprintf("%s.If[%s](%s, %s, %s)", g.rt(), t, c, a, b)
	}
	return fmt.Sprintf("func() %s { if %s { return %s }; return %s }()", t, c, a, b)
}

func (g *Gen) nullCo(x *NullCo) string {
	xc := g.expr(x.X)
	yc := g.expr(x.Y)
	t := g.typ(x.T)
	if isAny(x.X.Typ()) {
		tmp := g.tmp()
		return fmt.Sprintf("func() any { if %s := %s; %s != nil { return %s }; return %s }()", tmp, xc, tmp, tmp, yc)
	}
	if !isOpt(x.X.Typ()) {
		return xc
	}
	if isOpt(x.T) {
		if isSimpleExpr(x.Y) {
			return xc + ".OrOpt(" + yc + ")"
		}
		tmp := g.tmp()
		return fmt.Sprintf("func() %s { if %s := %s; %s.Ok { return %s }; return %s }()", t, tmp, xc, tmp, tmp, yc)
	}
	if isSimpleExpr(x.Y) {
		return xc + ".Or(" + yc + ")"
	}
	tmp := g.tmp()
	return fmt.Sprintf("func() %s { if %s := %s; %s.Ok { return %s.V }; return %s }()", t, tmp, xc, tmp, tmp, yc)
}

func (g *Gen) lambda(x *Lambda) string {
	fn := x.Fn
	var ps []string
	for _, p := range fn.Params {
		ps = append(ps, g.local(p)+" "+g.typ(p.T))
	}
	head := "func(" + strings.Join(ps, ", ") + ")"
	body := g.expr(x.Body)
	if fn.Ret == nil || isVoid(fn.Ret) {
		if bt := x.Body.Typ(); bt != nil && !isVoid(bt) {
			return head + " { _ = " + body + " }"
		}
		return head + " { " + body + " }"
	}
	return head + " " + g.typ(fn.Ret) + " { return " + body + " }"
}

// ===== 下标与成员 =====

func (g *Gen) index(x *Index) string {
	if x.Ovl != nil {
		return g.methodOn(x.X, x.Ovl, g.expr(x.X), []string{g.expr(x.Idx)}, nil)
	}
	xc := g.expr(x.X)
	switch x.X.Typ().(type) {
	case *ListT:
		return xc + ".At(" + g.expr(x.Idx) + ")"
	case *DictT:
		return xc + ".Get(" + g.expr(x.Idx) + ")"
	case *TupleT:
		return fmt.Sprintf("%s.V%d", xc, x.TupleIdx)
	case *Basic:
		return g.rt() + ".StrAt(" + xc + ", " + g.expr(x.Idx) + ")"
	}
	return xc
}

// addrExpr 返回可寻址形式的表达式代码：列表中的结构体元素使用 Ptr(i)。
func (g *Gen) addrExpr(e Expr) string {
	switch x := e.(type) {
	case *Index:
		if _, ok := x.X.Typ().(*ListT); ok && x.Ovl == nil {
			if d := namedDecl(x.T); d != nil && d.Kind == DStruct {
				return g.expr(x.X) + ".Ptr(" + g.expr(x.Idx) + ")"
			}
		}
	case *Attr:
		if x.Kind == AttrField && !x.Safe {
			if d := namedDecl(x.T); d != nil && d.Kind == DStruct {
				return g.fieldRefOnCode(g.addrExpr(x.X), x.X, x)
			}
		}
	}
	return g.expr(e)
}

func (g *Gen) isConcreteSelf(e Expr) bool {
	id, ok := e.(*Ident)
	return ok && id.Sym != nil && id.Sym.IsSelf && g.self != nil
}

// fieldRef 返回字段访问的 Go 代码。
func (g *Gen) fieldRef(a *Attr) string {
	return g.fieldRefOn(a.X, a)
}

func (g *Gen) fieldRefOn(recv Expr, a *Attr) string {
	rc := g.expr(recv)
	if d := namedDecl(recv.Typ()); d != nil && d.Kind == DStruct {
		rc = g.addrExpr(recv)
	}
	return g.fieldRefOnCode(rc, recv, a)
}

func (g *Gen) fieldRefOnCode(rc string, recv Expr, a *Attr) string {
	f := a.Field
	rt := a.RecvT
	if rt == nil {
		rt = recv.Typ()
	}
	if g.isConcreteSelf(recv) {
		return rc + "." + f.GoName
	}
	if isPolyType(rt) && f.Owner != nil && f.Owner.Kind == DClass {
		return rc + "." + f.Owner.GoName + "ꞏ()." + f.GoName
	}
	return rc + "." + f.GoName
}

func (g *Gen) extArgs(x *ExtBlock, recvT Type) []Type {
	if len(x.TParams) == 0 {
		return nil
	}
	m := Subst{}
	free := map[*TypeParam]bool{}
	for _, tp := range x.TParams {
		free[tp] = true
	}
	g.c.unify(x.Target, subst(recvT, g.subst), m, free)
	var out []Type
	for _, tp := range x.TParams {
		out = append(out, m[tp])
	}
	return out
}

func (g *Gen) propGet(a *Attr, recv string) string {
	p := a.Prop
	rt := a.RecvT
	if rt == nil {
		rt = a.X.Typ()
	}
	switch {
	case p.Extern != "":
		var targs []Type
		if p.Ext != nil {
			targs = g.extArgs(p.Ext, rt)
		}
		return g.externCall(p.Extern, recv, true, nil, targs)
	case p.Ext != nil:
		return p.GoName + g.targs(g.extArgs(p.Ext, rt)) + "(" + recv + ")"
	case p.Owner != nil && p.Owner.Kind == DEnum && p.Owner.ADT:
		var args []Type
		if nt, ok := rt.(*NamedT); ok {
			args = nt.Args
		}
		return p.Owner.GoName + "ꞏget_" + p.Name + g.targs(args) + "(" + recv + ")"
	}
	return recv + ".ꞏget_" + p.Name + "()"
}

func (g *Gen) propSet(a *Attr, recv, v string) string {
	return recv + ".ꞏset_" + a.Prop.Name + "(" + v + ")"
}

func (g *Gen) safeWrap(recvExpr Expr, memberT Type, resT Type, inner func(recv string) string) string {
	tmp := g.tmp()
	rc := g.expr(recvExpr)
	body := inner(tmp + ".V")
	if resT == nil || isVoid(resT) {
		return fmt.Sprintf("func() { if %s := %s; %s.Ok { %s } }()", tmp, rc, tmp, body)
	}
	rt := g.typ(resT)
	if isOpt(memberT) || isAny(memberT) {
		zero := rt + "{}"
		if isAny(memberT) {
			zero = "nil"
		}
		return fmt.Sprintf("func() %s { if %s := %s; %s.Ok { return %s }; return %s }()", rt, tmp, rc, tmp, body, zero)
	}
	return fmt.Sprintf("func() %s { if %s := %s; %s.Ok { return %s.Some[%s](%s) }; return %s{} }()", rt, tmp, rc, tmp, g.rt(), g.typ(memberT), body, rt)
}

func (g *Gen) attr(a *Attr) string {
	if a.Lam != nil {
		return g.lambda(a.Lam)
	}
	if a.Safe {
		ot := a.T
		memberT := optElem(ot)
		if isOpt(ot) && a.Kind == AttrField && a.Field != nil && isOpt(a.Field.T) {
			memberT = ot
		}
		return g.safeWrap(a.X, memberT, ot, func(recv string) string {
			cp := *a
			cp.Safe = false
			cp.X = &goExpr{exprBase{Pos: a.X.P(), T: a.RecvT}, recv}
			return g.attr(&cp)
		})
	}
	switch a.Kind {
	case AttrField:
		return g.fieldRef(a)
	case AttrProp:
		if a.Prop.Static {
			return a.Owner.D.GoName + "ꞏget_" + a.Prop.Name + "()"
		}
		return g.propGet(a, g.expr(a.X))
	case AttrModule:
		if a.Sym != nil {
			switch a.Sym.Kind {
			case SVar, SConst, SFunc:
				return g.symName(a.Sym)
			}
		}
		return a.Name
	case AttrStaticField:
		return a.Field.GoName
	case AttrStaticMethod:
		return a.Method.GoName
	case AttrEnumCase:
		v := a.Variant
		if v.Owner.ADT {
			var args []Type
			if nt, ok := a.T.(*NamedT); ok {
				args = nt.Args
			}
			return g.typ(a.T) + "(" + v.GoName + g.targs(args) + "{})"
		}
		return v.GoName
	case AttrVariantCtor:
		ft := a.T.(*FuncT)
		var ps, ns []string
		for i, p := range ft.Params {
			n := fmt.Sprintf("ꞏp%d", i)
			ps = append(ps, n+" "+g.typ(p))
			ns = append(ns, n)
		}
		var args []Type
		if a.Owner != nil {
			args = a.Owner.Args
		}
		return fmt.Sprintf("func(%s) %s { return %s%s{%s} }", strings.Join(ps, ", "), g.typ(ft.Ret), a.Variant.GoName, g.targs(args), strings.Join(ns, ", "))
	case AttrTupleField:
		return fmt.Sprintf("%s.V%s", g.expr(a.X), a.Name)
	case AttrBuiltinProp:
		if a.Name == "name" {
			return g.expr(a.X) + ".EnumNameꞏ()"
		}
		return "int(" + g.expr(a.X) + ")"
	case AttrMethodValue:
		return g.methodValue(a)
	}
	return g.expr(a.X) + "." + a.Name
}

// methodValue 生成绑定方法值 obj.method（不调用）。
func (g *Gen) methodValue(a *Attr) string {
	m := a.Method
	ft, ok := a.T.(*FuncT)
	if !ok {
		return "nil"
	}
	direct := m.Extern == "" && m.Ext == nil && len(m.Sig.TParams) == 0 && m.Owner != nil && !(m.Owner.Kind == DEnum && m.Owner.ADT)
	if direct {
		return g.expr(a.X) + "." + m.GoName
	}
	recv := g.tmp()
	var ps, ns []string
	for i, p := range ft.Params {
		n := fmt.Sprintf("ꞏp%d", i)
		ps = append(ps, n+" "+g.typ(p))
		ns = append(ns, n)
	}
	rx := &goExpr{exprBase{Pos: a.X.P(), T: a.X.Typ()}, recv}
	call := g.methodOn(rx, m, recv, ns, nil)
	ret := ""
	body := call
	if ft.Ret != nil && !isVoid(ft.Ret) {
		ret = " " + g.typ(ft.Ret)
		body = "return " + call
	}
	return fmt.Sprintf("func() %s { %s := %s; return func(%s)%s { %s } }()", g.typ(ft), recv, g.expr(a.X), strings.Join(ps, ", "), ret, body)
}

// ownerArgs 返回接收者类型中方法所属类型的类型实参。
func (g *Gen) ownerArgs(recvT Type, owner *TypeDecl) []Type {
	nt, ok := optElem(recvT).(*NamedT)
	if !ok {
		return nil
	}
	if owner == nil {
		return nil
	}
	for _, anc := range supertypes(nt) {
		if anc.D == owner {
			return anc.Args
		}
	}
	return nt.Args
}

// methodOn 生成对方法 m 的调用（接收者代码已给出）。
func (g *Gen) methodOn(recvExpr Expr, m *Method, recv string, args []string, targs []Type) string {
	rt := recvExpr.Typ()
	if m.Extern != "" {
		var all []Type
		if m.Ext != nil {
			all = append(all, g.extArgs(m.Ext, rt)...)
		}
		all = append(all, targs...)
		if m.Owner != nil && m.Owner.Kind == DExtern && strings.Contains(m.Extern, "$T") {
			all = append(g.ownerArgs(rt, m.Owner), targs...)
		}
		return g.externCall(m.Extern, recv, true, args, all)
	}
	if m.Ext != nil {
		all := append(g.extArgs(m.Ext, rt), targs...)
		return m.GoName + g.targs(all) + "(" + joinArgs(recv, args) + ")"
	}
	owner := m.Owner
	if owner != nil && ((owner.Kind == DEnum && owner.ADT) || len(m.Sig.TParams) > 0) {
		var all []Type
		all = append(all, g.ownerArgs(rt, owner)...)
		all = append(all, targs...)
		return m.GoName + g.targs(all) + "(" + joinArgs(recv, args) + ")"
	}
	return recv + "." + m.GoName + "(" + strings.Join(args, ", ") + ")"
}

func joinArgs(first string, rest []string) string {
	if len(rest) == 0 {
		return first
	}
	return first + ", " + strings.Join(rest, ", ")
}

// externCall 根据 @go 说明生成调用。
func (g *Gen) externCall(spec string, recv string, hasRecv bool, args []string, targs []Type) string {
	if strings.Contains(spec, "$") {
		s := spec
		for i := len(targs); i >= 1; i-- {
			s = strings.ReplaceAll(s, "$T"+strconv.Itoa(i), g.typ(targs[i-1]))
		}
		s = strings.ReplaceAll(s, "$*", strings.Join(args, ", "))
		for i := len(args); i >= 1; i-- {
			s = strings.ReplaceAll(s, "$"+strconv.Itoa(i), args[i-1])
		}
		s = strings.ReplaceAll(s, "$r", recv)
		g.importsIn(s)
		return s
	}
	if strings.HasPrefix(spec, ".") {
		return recv + spec + "(" + strings.Join(args, ", ") + ")"
	}
	if hasRecv && !strings.Contains(spec, ".") {
		return recv + "." + spec + "(" + strings.Join(args, ", ") + ")"
	}
	name := g.externName(spec)
	all := args
	if hasRecv {
		all = append([]string{recv}, args...)
	}
	return name + g.targs(targs) + "(" + strings.Join(all, ", ") + ")"
}

// ===== 调用 =====

func (g *Gen) argList(e *Call) []string {
	var out []string
	if e.Sig == nil {
		for _, a := range e.Final {
			out = append(out, g.expr(a))
		}
		return out
	}
	for i := range e.Sig.Params {
		var a Expr
		if i < len(e.Final) {
			a = e.Final[i]
		}
		if a != nil {
			out = append(out, g.expr(a))
			continue
		}
		out = append(out, g.defaultArg(e, i))
	}
	return out
}

func (g *Gen) defaultArg(e *Call, i int) string {
	p := e.Sig.Params[i]
	if p.Default == nil {
		return g.zero(p.T)
	}
	save := g.subst
	ns := Subst{}
	for k, v := range g.subst {
		ns[k] = v
	}
	for k, v := range composeSubst(p.DefaultSubst, e.Subst) {
		ns[k] = subst(v, save)
	}
	g.subst = ns
	saveSelf := g.self
	g.self = nil
	code := g.expr(p.Default)
	g.subst = save
	g.self = saveSelf
	return code
}

// zero 返回类型的零值表达式。
func (g *Gen) zero(t Type) string {
	switch x := subst(t, g.subst).(type) {
	case *Basic:
		switch x.Kind {
		case BStr:
			return `""`
		case BBool:
			return "false"
		case BAny, BNull:
			return "nil"
		}
		return g.typ(x) + "(0)"
	case *OptT, *TupleT:
		return g.typ(x) + "{}"
	case *NamedT:
		if x.D.Kind == DStruct {
			return g.typ(x) + "{}"
		}
	}
	return "*new(" + g.typ(t) + ")"
}

func (g *Gen) call(e *Call) string {
	switch e.Kind {
	case CallFunc, CallExtern:
		sym := e.Func
		args := g.argList(e)
		if sym != nil && sym.Func != nil && sym.Func.GoExtern != "" {
			return g.externCall(sym.Func.GoExtern, "", false, args, e.TArgs)
		}
		name := ""
		if sym != nil {
			name = g.symName(sym)
		} else {
			name = g.expr(unwrapTypeAppX(e.Fn))
		}
		return name + g.targs(e.TArgs) + "(" + strings.Join(args, ", ") + ")"
	case CallLocalFunc:
		return g.local(e.Func) + "(" + strings.Join(g.argList(e), ", ") + ")"
	case CallValue:
		fn := g.expr(e.Fn)
		if _, ok := e.Fn.(*Lambda); ok {
			fn = "(" + fn + ")"
		}
		return fn + "(" + strings.Join(g.argList(e), ", ") + ")"
	case CallMethod, CallExt:
		if e.ViaCall {
			// obj(...) → obj.__call__(...)
			rc := g.expr(e.Recv)
			if e.Method != nil && e.Method.Mutating {
				rc = g.addrExpr(e.Recv)
			}
			return g.methodOn(e.Recv, e.Method, rc, g.argList(e), e.TArgs)
		}
		fa, _ := unwrapTypeAppX(e.Fn).(*Attr)
		if fa != nil && fa.Safe {
			memberT := optElem(e.T)
			if e.Method != nil && isOpt(substSig(e.Method.Sig, e.Subst).Ret) {
				memberT = e.T
			}
			resT := e.T
			if e.Method != nil && isVoid(e.Method.Sig.Ret) {
				resT = nil
			}
			return g.safeWrap(e.Recv, memberT, resT, func(recv string) string {
				rx := &goExpr{exprBase{Pos: e.Recv.P(), T: fa.RecvT}, recv}
				return g.methodOn(rx, e.Method, recv, g.argList(e), e.TArgs)
			})
		}
		recv := e.Recv
		rc := g.expr(recv)
		if e.Method != nil && e.Method.Mutating {
			rc = g.addrExpr(recv)
		}
		rx := recv
		if fa != nil && fa.RecvT != nil && !identical(fa.RecvT, recv.Typ()) {
			rx = &goExpr{exprBase{Pos: recv.P(), T: fa.RecvT}, rc}
		}
		return g.methodOn(rx, e.Method, rc, g.argList(e), e.TArgs)
	case CallStatic:
		return e.Method.GoName + g.targs(e.TArgs) + "(" + strings.Join(g.argList(e), ", ") + ")"
	case CallSuper:
		name := g.superName(e.Method.Owner, e.Method)
		return "self." + name + "(" + strings.Join(g.argList(e), ", ") + ")"
	case CallCtor:
		td := e.Ctor.D
		args := g.argList(e)
		if td.Kind == DExtern {
			if td.GoCtor == "" {
				return "nil"
			}
			return g.externCall(td.GoCtor, "", false, args, e.Ctor.Args)
		}
		return td.GoName + "ꞏnew" + g.targs(e.Ctor.Args) + "(" + strings.Join(args, ", ") + ")"
	case CallVariant:
		var args []Type
		if e.Ctor != nil {
			args = e.Ctor.Args
		}
		lit := e.Variant.GoName + g.targs(args) + "{" + strings.Join(g.argList(e), ", ") + "}"
		return g.typ(e.T) + "(" + lit + ")"
	case CallConv:
		return g.convCall(e)
	case CallBuiltin:
		return g.builtin(e)
	case CallEmptyColl:
		return g.emptyColl(e)
	}
	return "nil /* call */"
}

func (g *Gen) emptyColl(e *Call) string {
	rt := g.rt()
	var arg string
	if len(e.Final) > 0 && e.Final[0] != nil {
		arg = g.expr(e.Final[0])
	}
	switch t := e.ConvTo.(type) {
	case *ListT:
		if arg == "" {
			return fmt.Sprintf("%s.NewList[%s](0)", rt, g.typ(t.Elem))
		}
		return fmt.Sprintf("%s.ListFrom(%s)", rt, arg)
	case *SetT:
		if arg == "" {
			return fmt.Sprintf("%s.NewSet[%s](0)", rt, g.typ(t.Elem))
		}
		return fmt.Sprintf("%s.SetFrom(%s)", rt, arg)
	case *DictT:
		if arg == "" {
			return fmt.Sprintf("%s.NewDict[%s, %s](0)", rt, g.typ(t.Key), g.typ(t.Val))
		}
		if e.Extra == "dict" {
			return arg + ".Copy()"
		}
		return fmt.Sprintf("%s.DictFrom(%s)", rt, arg)
	case *ChanT:
		if arg == "" {
			arg = "0"
		}
		return fmt.Sprintf("make(chan %s, %s)", g.typ(t.Elem), arg)
	}
	return "nil"
}

func (g *Gen) convCall(e *Call) string {
	rt := g.rt()
	to := e.ConvTo
	if len(e.Final) == 0 {
		return g.zero(to)
	}
	a := e.Final[0]
	x := g.expr(a)
	from := a.Typ()
	if e.Extra == "base" {
		return fmt.Sprintf("%s.ParseIntBase(%s, %s)", rt, x, g.expr(e.Final[1]))
	}
	switch basicKind(to) {
	case BStr:
		return g.toStr(x, from)
	case BBool:
		switch {
		case isBool(from):
			return x
		case isNumeric(from):
			return "(" + x + " != 0)"
		case isStr(from):
			return "(" + x + " != \"\")"
		case isOpt(from):
			return x + ".Ok"
		}
		return g.convCode(x, ConvTruthy, from, tBool)
	case BChar:
		switch {
		case isStr(from):
			return rt + ".StrToChar(" + x + ")"
		case basicKind(from) == BChar:
			return x
		}
		return rt + ".Char(" + x + ")"
	}
	goT := g.typ(to)
	switch {
	case isStr(from):
		if isFloat(to) {
			if basicKind(to) == BFloat {
				return rt + ".ParseFloat(" + x + ")"
			}
			return goT + "(" + rt + ".ParseFloat(" + x + "))"
		}
		if basicKind(to) == BInt {
			return rt + ".ParseInt(" + x + ")"
		}
		// u64 需要无符号解析：走 ParseInt 会把 >MaxInt64 的合法值误判成溢出。
		if basicKind(to) == BU64 {
			return rt + ".ParseUint(" + x + ")"
		}
		return goT + "(" + rt + ".ParseInt(" + x + "))"
	case isBool(from):
		return goT + "(" + rt + ".B2I(" + x + "))"
	case isFloat(from) && isInteger(to):
		if basicKind(to) == BInt {
			return rt + ".FloatToInt(float64(" + x + "))"
		}
		return goT + "(" + x + ")"
	}
	if identical(from, to) {
		return x
	}
	return goT + "(" + x + ")"
}

func (g *Gen) builtin(e *Call) string {
	rt := g.rt()
	arg := func(i int) string {
		if i < len(e.Final) && e.Final[i] != nil {
			return g.expr(e.Final[i])
		}
		return ""
	}
	argT := func(i int) Type {
		if i < len(e.Final) && e.Final[i] != nil {
			return e.Final[i].Typ()
		}
		return nil
	}
	elemOfIter := func(i int) Type {
		if it, ok := argT(i).(*IterT); ok {
			return it.Elem
		}
		return tInvalid
	}
	switch e.Builtin {
	case "print":
		kw, _ := e.Extra.(map[string]*Arg)
		var args []string
		spread := false
		for _, a := range e.Final {
			if st, ok := a.(*StarExpr); ok {
				spread = true
				args = append(args, g.expr(st.X))
				continue
			}
			args = append(args, g.expr(a))
		}
		sep, end := `" "`, `"\n"`
		if a := kw["sep"]; a != nil {
			sep = g.expr(a.X)
		}
		if a := kw["end"]; a != nil {
			end = g.expr(a.X)
		}
		if kw["file"] != nil || kw["flush"] != nil {
			// print(..., file=f, flush=...)
			file, flush := "nil", "false"
			if a := kw["file"]; a != nil {
				file = g.expr(a.X)
			}
			if a := kw["flush"]; a != nil {
				flush = g.expr(a.X)
			}
			if spread {
				r := g.tmp()
				var b strings.Builder
				fmt.Fprintf(&b, "func() { var %s []any", r)
				for i, a := range e.Final {
					if _, ok := a.(*StarExpr); ok {
						fmt.Fprintf(&b, "; for v := range %s { %s = append(%s, v) }", args[i], r, r)
					} else {
						fmt.Fprintf(&b, "; %s = append(%s, %s)", r, r, args[i])
					}
				}
				fmt.Fprintf(&b, "; %s.PrintTo(%s, %s, %s, %s, %s...) }()", rt, file, sep, end, flush, r)
				return b.String()
			}
			return rt + ".PrintTo(" + joinArgs(file+", "+sep+", "+end+", "+flush, args) + ")"
		}
		if spread {
			// print(a, *xs)：先收集所有参数
			r := g.tmp()
			var b strings.Builder
			fmt.Fprintf(&b, "func() { var %s []any", r)
			for i, a := range e.Final {
				if _, ok := a.(*StarExpr); ok {
					fmt.Fprintf(&b, "; for v := range %s { %s = append(%s, v) }", args[i], r, r)
				} else {
					fmt.Fprintf(&b, "; %s = append(%s, %s)", r, r, args[i])
				}
			}
			fmt.Fprintf(&b, "; %s.PrintSE(%s, %s, %s...) }()", rt, sep, end, r)
			return b.String()
		}
		if len(kw) == 0 {
			if len(e.Final) == 1 && isStr(e.Final[0].Typ()) {
				return rt + ".PrintStr(" + args[0] + ")"
			}
			return rt + ".Print(" + strings.Join(args, ", ") + ")"
		}
		return rt + ".PrintSE(" + joinArgs(sep+", "+end, args) + ")"
	case "len":
		x := arg(0)
		switch t := argT(0).(type) {
		case *ListT, *DictT, *SetT:
			return x + ".Len()"
		case *ChanT:
			return "len(" + x + ")"
		case *TupleT:
			return strconv.Itoa(len(t.Elems))
		case *Basic:
			return rt + ".StrLen(" + x + ")"
		case *NamedT:
			return g.methodOn(e.Final[0], e.Method, x, nil, nil)
		}
		return "0"
	case "range":
		var start, stop, step string
		switch len(e.Final) {
		case 1:
			start, stop, step = g.zeroOf(e.T.(*IterT).Elem), arg(0), "1"
		case 2:
			start, stop, step = arg(0), arg(1), "1"
		default:
			start, stop, step = arg(0), arg(1), arg(2)
		}
		return fmt.Sprintf("%s.Range[%s](%s, %s, %s)", rt, g.typ(e.T.(*IterT).Elem), start, stop, step)
	case "abs":
		return rt + ".Abs(" + arg(0) + ")"
	case "min", "max":
		cap := "Min"
		if e.Builtin == "max" {
			cap = "Max"
		}
		et := e.T
		switch ex := e.Extra.(type) {
		case string:
			if ex == "iter" {
				def := g.typ(&OptT{et}) + "{}"
				if d := arg(2); d != "" {
					def = rt + ".Some[" + g.typ(et) + "](" + d + ")"
				}
				if k := arg(1); k != "" {
					return fmt.Sprintf("%s.%sIterBy(%s, %s, %s)", rt, cap, arg(0), k, def)
				}
				if isOrdered(et) {
					return fmt.Sprintf("%s.%sIter(%s, %s)", rt, cap, arg(0), def)
				}
				if g.c.hasLess(et) {
					return fmt.Sprintf("%s.%sIterLess(%s, %s)", rt, cap, arg(0), def)
				}
				return fmt.Sprintf("%s.%sIterCmp(%s, %s)", rt, cap, arg(0), def)
			}
			var args []string
			for _, a := range e.Final {
				args = append(args, g.expr(a))
			}
			if isOrdered(et) {
				return fmt.Sprintf("%s.%s[%s](%s)", rt, cap, g.typ(et), strings.Join(args, ", "))
			}
			if g.c.hasLess(et) {
				return fmt.Sprintf("%s.%sLess[%s](%s)", rt, cap, g.typ(et), strings.Join(args, ", "))
			}
			return fmt.Sprintf("%s.%sCmp[%s](%s)", rt, cap, g.typ(et), strings.Join(args, ", "))
		case Expr:
			var args []string
			for _, a := range e.Final {
				args = append(args, g.expr(a))
			}
			return fmt.Sprintf("%s.%sBy(%s, %s)", rt, cap, g.expr(ex), strings.Join(args, ", "))
		}
		return "0"
	case "sum":
		et := elemOfIter(0)
		start := g.zero(et)
		if s := arg(1); s != "" {
			start = s
		}
		return fmt.Sprintf("%s.Sum(%s, %s)", rt, arg(0), start)
	case "sorted":
		rev := "false"
		if r := arg(2); r != "" {
			rev = r
		}
		if k := arg(1); k != "" {
			return fmt.Sprintf("%s.SortedBy(%s, %s, %s)", rt, arg(0), k, rev)
		}
		if isOrdered(elemOfIter(0)) {
			return fmt.Sprintf("%s.Sorted(%s, %s)", rt, arg(0), rev)
		}
		if g.c.hasLess(elemOfIter(0)) {
			return fmt.Sprintf("%s.SortedLess(%s, %s)", rt, arg(0), rev)
		}
		return fmt.Sprintf("%s.SortedCmp(%s, %s)", rt, arg(0), rev)
	case "reversed":
		return rt + ".Reversed(" + arg(0) + ")"
	case "enumerate":
		start := "0"
		if s := arg(1); s != "" {
			start = s
		}
		return fmt.Sprintf("%s.Enumerate(%s, %s)", rt, arg(0), start)
	case "zip":
		if e.Extra == "star" {
			if lt, ok := argT(0).(*ListT); ok && isStr(lt.Elem) {
				return rt + ".ZipStrs(" + arg(0) + ")"
			}
			return rt + ".ZipLists(" + arg(0) + ")"
		}
		if len(e.Final) == 3 {
			return fmt.Sprintf("%s.Zip3(%s, %s, %s)", rt, arg(0), arg(1), arg(2))
		}
		return fmt.Sprintf("%s.Zip2(%s, %s)", rt, arg(0), arg(1))
	case "any":
		return rt + ".AnyOf(" + arg(0) + ")"
	case "all":
		return rt + ".AllOf(" + arg(0) + ")"
	case "input":
		p := arg(0)
		if p == "" {
			p = `""`
		}
		return rt + ".Input(" + p + ")"
	case "repr":
		return rt + ".Repr(" + arg(0) + ")"
	case "type_name":
		return rt + ".TypeName(" + arg(0) + ")"
	case "ord":
		if isStr(argT(0)) {
			return rt + ".Ord(" + arg(0) + ")"
		}
		return "int(" + arg(0) + ")"
	case "chr":
		return rt + ".Chr(" + arg(0) + ")"
	case "hex":
		return rt + ".Hex(int(" + arg(0) + "))"
	case "bin":
		return rt + ".Bin(int(" + arg(0) + "))"
	case "oct":
		return rt + ".Oct(int(" + arg(0) + "))"
	case "round":
		if len(e.Final) > 1 {
			return rt + ".RoundN(" + arg(0) + ", " + arg(1) + ")"
		}
		return rt + ".Round(" + arg(0) + ")"
	case "divmod":
		if isFloat(argT(0)) {
			return rt + ".FDivmod(float64(" + arg(0) + "), float64(" + arg(1) + "))"
		}
		return rt + ".Divmod(int(" + arg(0) + "), int(" + arg(1) + "))"
	case "open":
		mode := rt + ".Opt[string]{}"
		if m := arg(1); m != "" {
			mode = rt + ".Some(" + m + ")"
		}
		return rt + ".Open(" + arg(0) + ", " + mode + ")"
	case "exit":
		c := arg(0)
		if c == "" {
			c = "0"
		}
		return rt + ".Exit(" + c + ")"
	case "isinstance":
		if ie, ok := e.Extra.(*IsExpr); ok {
			return g.isExpr(ie)
		}
		return "false"
	case "enum_values":
		return e.Ctor.D.GoName + "ꞏvalues()"
	case "enum_from_value":
		return e.Ctor.D.GoName + "ꞏfrom_value(" + arg(0) + ")"
	}
	return "nil /* builtin " + e.Builtin + " */"
}

// ===== as / is =====

func (g *Gen) cast(x *Cast) string {
	xc := g.expr(x.X)
	to := x.To
	switch x.Mode {
	case castImplicit:
		return xc
	case castNoop:
		if x.Safe {
			return g.rt() + ".Some[" + g.typ(to) + "](" + g.convCode(xc, ConvIface, x.X.Typ(), to) + ")"
		}
		return g.typ(to) + "(" + xc + ")"
	case castNumeric:
		if isFloat(x.X.Typ()) && basicKind(to) == BInt {
			return g.rt() + ".FloatToInt(float64(" + xc + "))"
		}
		return g.typ(to) + "(" + xc + ")"
	case castCharInt, castEnumToInt, castIntToEnum:
		return g.typ(to) + "(" + xc + ")"
	case castAssert, castOptUnwrapAssert:
		src := xc
		if isOpt(x.X.Typ()) && !isOpt(to) {
			if x.Safe {
				tmp := g.tmp()
				gt := g.typ(to)
				return fmt.Sprintf("func() %s.Opt[%s] { if %s := %s; %s.Ok { return %s.TryCast[%s](%s.V) }; return %s.Opt[%s]{} }()", g.rt(), gt, tmp, xc, tmp, g.rt(), gt, tmp, g.rt(), gt)
			}
			src = xc + ".Must()"
		}
		if identical(optElem(x.X.Typ()), to) {
			return src
		}
		if x.Safe {
			return g.rt() + ".TryCast[" + g.typ(to) + "](" + src + ")"
		}
		return g.rt() + ".Cast[" + g.typ(to) + "](" + src + ", " + strconv.Quote(to.String()) + ")"
	}
	return xc
}

func (g *Gen) isExpr(x *IsExpr) string {
	xc := g.expr(x.X)
	xt := x.X.Typ()
	var code string
	if x.Null {
		switch {
		case isOpt(xt):
			code = "!" + xc + ".Ok"
		case isAny(xt):
			code = "(" + xc + " == nil)"
		default:
			code = "false"
		}
	} else if x.To != nil {
		code = g.typeTest(xc, xt, x.To)
	} else {
		code = "false"
	}
	if x.Neg {
		return "!" + g.paren(code)
	}
	return code
}

// ===== 隐式转换 =====

func (g *Gen) convCode(code string, k ConvKind, from, to Type) string {
	rt := g.rt()
	switch k {
	case ConvNone:
		return code
	case ConvNumeric:
		return g.typ(to) + "(" + code + ")"
	case ConvWrap:
		elem := optElem(to)
		inner, _ := g.c.assignable(from, elem)
		return rt + ".Some[" + g.typ(elem) + "](" + g.convCode(code, inner, from, elem) + ")"
	case ConvNull:
		if isAny(to) {
			return "nil"
		}
		return g.typ(to) + "{}"
	case ConvToAny:
		return "any(" + code + ")"
	case ConvOptAny:
		return code + ".Any()"
	case ConvIface:
		return g.typ(to) + "(" + code + ")"
	case ConvOptMap:
		fe, te := optElem(from), optElem(to)
		inner, _ := g.c.assignable(fe, te)
		return fmt.Sprintf("%s.OptMap(%s, func(v %s) %s { return %s })", rt, code, g.typ(fe), g.typ(te), g.convCode("v", inner, fe, te))
	case ConvFunc:
		ff, _ := from.(*FuncT)
		tf, _ := to.(*FuncT)
		if ff == nil || tf == nil {
			return code
		}
		f := g.tmp()
		var ps, ns []string
		for i, p := range tf.Params {
			n := fmt.Sprintf("ꞏa%d", i)
			ps = append(ps, n+" "+g.typ(p))
			ns = append(ns, n)
		}
		call := f + "(" + strings.Join(ns, ", ") + ")"
		var body, ret string
		if tf.Ret == nil || isVoid(tf.Ret) {
			body = call
		} else {
			inner, _ := g.c.assignable(ff.Ret, tf.Ret)
			body = "return " + g.convCode(call, inner, ff.Ret, tf.Ret)
			ret = " " + g.typ(tf.Ret)
		}
		return fmt.Sprintf("func() %s { %s := %s; return func(%s)%s { %s } }()", g.typ(to), f, code, strings.Join(ps, ", "), ret, body)
	case ConvIter:
		switch from.(type) {
		case *ListT, *SetT:
			return code + ".All()"
		case *DictT:
			return code + ".KeysIter()"
		case *ChanT:
			return rt + ".ChanIter(" + code + ")"
		case *Basic:
			return rt + ".StrIter(" + code + ")"
		case *NamedT:
			if m, _ := g.c.operatorMethod(from, "__iter__"); m != nil {
				return g.methodOn(&goExpr{exprBase{T: from}, code}, m, code, nil, nil)
			}
		}
		return code
	case ConvTruthy:
		switch x := from.(type) {
		case *OptT:
			return code + ".Ok"
		case *ListT, *DictT, *SetT:
			return "(" + code + ".Len() > 0)"
		case *Basic:
			if x.Kind == BStr {
				return "(" + code + " != \"\")"
			}
			if x.Kind == BAny {
				return "(" + code + " != nil)"
			}
		case *NamedT:
			if m, _ := g.c.operatorMethod(from, "__bool__"); m != nil {
				return g.methodOn(&goExpr{exprBase{T: from}, code}, m, code, nil, nil)
			}
			if m, _ := g.c.operatorMethod(from, "__len__"); m != nil {
				return "(" + g.methodOn(&goExpr{exprBase{T: from}, code}, m, code, nil, nil) + " != 0)"
			}
		}
		return code
	}
	return code
}

// exprTo 生成表达式并转换为目标类型（用于字段默认值等未经检查器转换的位置）。
func (g *Gen) exprTo(e Expr, to Type) string {
	return g.coerce(g.expr(e), e.Typ(), to)
}
