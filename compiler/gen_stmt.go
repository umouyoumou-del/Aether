package compiler

import (
	"fmt"
	"strconv"
	"strings"
)

// goExpr 是代码生成时使用的、已经生成好的 Go 表达式。
type goExpr struct {
	exprBase
	code string
}

func (g *Gen) stmts(list []Stmt) {
	for _, s := range list {
		g.stmt(s)
	}
}

func (g *Gen) stmt(s Stmt) {
	g.pos(s.P())
	switch s := s.(type) {
	case *ExprStmt:
		g.exprStmt(s.X)
	case *AssignStmt:
		if s.Op == TAssign {
			g.assign(s)
		} else {
			g.augAssign(s)
		}
	case *VarDecl:
		g.varDecl(s)
	case *ConstDecl:
		if s.Sym == nil || s.GoName != "" {
			return
		}
		if s.Sym.Global && s.Sym.GoConst {
			return
		}
		g.line("%s = %s", g.symName(s.Sym), g.expr(s.Value))
	case *IfStmt:
		g.ifStmt(s)
	case *WhileStmt:
		g.whileStmt(s)
	case *ForStmt:
		g.forStmt(s)
	case *BreakStmt:
		g.jump(2)
	case *ContinueStmt:
		g.jump(3)
	case *PassStmt:
	case *ReturnStmt:
		g.returnStmt(s)
	case *RaiseStmt:
		if s.X == nil {
			if n := len(g.fn.handlers); n > 0 {
				g.line("panic(%s)", g.fn.handlers[n-1])
			}
			return
		}
		g.line("panic(%s.Throw(%s))", g.rt(), g.expr(s.X))
	case *TryStmt:
		g.tryStmt(s)
	case *WithStmt:
		g.withStmt(s, s.Items)
	case *DeferStmt:
		g.goDefer(s.X, "defer")
	case *GoStmt:
		g.goDefer(s.X, "go")
	case *AssertStmt:
		g.assertStmt(s)
	case *DelStmt:
		for _, t := range s.Targets {
			idx := t.(*Index)
			switch idx.X.Typ().(type) {
			case *ListT:
				g.line("%s.RemoveAt(%s)", g.expr(idx.X), g.expr(idx.Idx))
			case *DictT:
				g.line("%s.Delete(%s)", g.expr(idx.X), g.expr(idx.Idx))
			}
		}
	case *MatchStmt:
		g.matchStmt(s)
	case *SelectStmt:
		g.selectStmt(s)
	case *YieldStmt:
		if s.From {
			v := g.tmp()
			sym := &Symbol{Kind: SVar, Name: v, GoName: v, T: s.Loop.ElemT}
			target := &Ident{exprBase: exprBase{Pos: s.Pos, T: s.Loop.ElemT}, Name: v, Sym: sym}
			header, pre := g.loopHead(target, s.Value, s.Loop, nil)
			flowRet := "return"
			if g.fn.tryDepth > 0 {
				flowRet = "ꞏflow = 1; return"
				g.markTrys(1)
			}
			g.line("%s {", header)
			g.ind++
			if pre != nil {
				pre()
			}
			g.line("if !yieldꞏ(%s) { %s }", g.coerce(v, s.Loop.ElemT, g.fn.ctx.YieldT), flowRet)
			g.ind--
			g.line("}")
			return
		}
		if g.fn.tryDepth > 0 {
			g.line("if !yieldꞏ(%s) {", g.expr(s.Value))
			g.line("\tꞏflow = 1")
			g.line("\treturn")
			g.line("}")
			g.markTrys(1)
		} else {
			g.line("if !yieldꞏ(%s) {", g.expr(s.Value))
			g.line("\treturn")
			g.line("}")
		}
	case *FuncDeclStmt:
		g.nestedFunc(s.Fn)
	}
}

func (g *Gen) exprStmt(e Expr) {
	code := g.expr(e)
	t := e.Typ()
	if t == nil || isVoid(t) {
		g.line("%s", code)
		return
	}
	g.line("_ = %s", code)
}

// ===== 赋值 =====

// lhs 返回可赋值目标的 Go 代码（仅用于标识符、字段、静态字段）。
func (g *Gen) lhs(e Expr) string {
	switch t := e.(type) {
	case *Ident:
		if t.Name == "_" {
			return "_"
		}
		return g.symName(t.Sym)
	case *Attr:
		switch t.Kind {
		case AttrField:
			return g.fieldRef(t)
		case AttrStaticField:
			return t.Field.GoName
		case AttrModule:
			return g.symName(t.Sym)
		}
	}
	return g.expr(e)
}

// store 生成把 value 写入目标的语句。
func (g *Gen) store(target Expr, value string) {
	switch t := target.(type) {
	case *Ident:
		if t.Name == "_" {
			g.line("_ = %s", value)
			return
		}
		g.line("%s = %s", g.symName(t.Sym), value)
	case *Attr:
		switch t.Kind {
		case AttrProp:
			g.line("%s", g.propSet(t, g.expr(t.X), value))
		default:
			g.line("%s = %s", g.lhs(t), value)
		}
	case *Index:
		x := g.expr(t.X)
		if t.Ovl != nil {
			g.line("%s", g.methodOn(t.X, t.Ovl, x, []string{g.expr(t.Idx), value}, nil))
			return
		}
		switch t.X.Typ().(type) {
		case *ListT, *DictT:
			g.line("%s.Set(%s, %s)", x, g.expr(t.Idx), value)
		}
	case *TupleLit:
		tmp := g.tmp()
		g.line("%s := %s", tmp, value)
		g.destructure(t, tmp, t.T)
	}
}

// destructure 把元组/列表值（已在临时变量中）解构到多个目标。
func (g *Gen) destructure(t *TupleLit, tmp string, vt Type) {
	switch x := vt.(type) {
	case *TupleT:
		for i, el := range t.Elems {
			g.store(el, g.coerce(fmt.Sprintf("%s.V%d", tmp, i), x.Elems[i], el.Typ()))
		}
	case *ListT:
		g.line("%s.CheckLen(%s, %d)", g.rt(), tmp, len(t.Elems))
		for i, el := range t.Elems {
			g.store(el, g.coerce(fmt.Sprintf("%s.S[%d]", tmp, i), x.Elem, el.Typ()))
		}
	}
}

// coerce 在代码生成阶段做简单的隐式转换（数值拓宽、包装为可空）。
func (g *Gen) coerce(code string, from, to Type) string {
	if from == nil || to == nil || identical(from, to) || isInvalid(to) {
		return code
	}
	k, _ := g.c.assignable(from, to)
	return g.convCode(code, k, from, to)
}

func (g *Gen) assign(s *AssignStmt) {
	if len(s.Targets) > 1 {
		tmp := g.tmp()
		g.line("%s := %s", tmp, g.expr(s.Value))
		for _, t := range s.Targets {
			g.store(t, g.coerce(tmp, s.Value.Typ(), t.Typ()))
		}
		return
	}
	target := s.Targets[0]
	if tl, ok := target.(*TupleLit); ok {
		if vl, ok := s.Value.(*TupleLit); ok && len(vl.Elems) == len(tl.Elems) {
			var tmps, vals []string
			for _, e := range vl.Elems {
				tmps = append(tmps, g.tmp())
				vals = append(vals, g.expr(e))
			}
			g.line("%s := %s", strings.Join(tmps, ", "), strings.Join(vals, ", "))
			for i, el := range tl.Elems {
				g.store(el, g.coerce(tmps[i], vl.Elems[i].Typ(), el.Typ()))
			}
			return
		}
		tmp := g.tmp()
		g.line("%s := %s", tmp, g.expr(s.Value))
		g.destructure(tl, tmp, s.Value.Typ())
		return
	}
	g.store(target, g.expr(s.Value))
}

func (g *Gen) varDecl(s *VarDecl) {
	switch t := s.Target.(type) {
	case *Ident:
		if s.Value != nil {
			g.line("%s = %s", g.symName(s.Sym), g.expr(s.Value))
		}
	case *Attr:
		if s.Value != nil {
			g.line("%s = %s", g.lhs(t), g.expr(s.Value))
		}
	}
}

func isSimpleRecv(e Expr) bool {
	switch x := e.(type) {
	case *Ident:
		return true
	case *Attr:
		return x.Kind == AttrField && isSimpleRecv(x.X)
	}
	return false
}

func (g *Gen) augAssign(s *AssignStmt) {
	target := s.Targets[0]
	op := augBinOp(s.Op)
	tt := target.Typ()
	val := s.Value
	// 读写同一目标：必要时先把接收者存入临时变量
	var cur string
	var put func(v string)
	switch t := target.(type) {
	case *Ident:
		name := g.symName(t.Sym)
		if t.Narrow != NarrowNone {
			name = g.symName(t.Sym)
		}
		cur = name
		put = func(v string) { g.line("%s = %s", name, v) }
	case *Attr:
		recv := g.expr(t.X)
		if !isSimpleRecv(t.X) {
			tmp := g.tmp()
			g.line("%s := %s", tmp, recv)
			recv = tmp
		}
		rx := &goExpr{exprBase{Pos: t.X.P(), T: t.X.Typ()}, recv}
		switch t.Kind {
		case AttrProp:
			cur = g.propGet(t, recv)
			put = func(v string) { g.line("%s", g.propSet(t, recv, v)) }
		case AttrStaticField:
			cur = t.Field.GoName
			put = func(v string) { g.line("%s = %s", cur, v) }
		case AttrModule:
			cur = g.symName(t.Sym)
			put = func(v string) { g.line("%s = %s", cur, v) }
		default:
			ref := g.fieldRefOn(rx, t)
			cur = ref
			put = func(v string) { g.line("%s = %s", ref, v) }
		}
	case *Index:
		x := g.expr(t.X)
		if !isSimpleRecv(t.X) {
			tmp := g.tmp()
			g.line("%s := %s", tmp, x)
			x = tmp
		}
		k := g.tmp()
		g.line("%s := %s", k, g.expr(t.Idx))
		if t.Ovl != nil {
			getM, _ := g.c.operatorMethod(t.X.Typ(), "__getitem__")
			cur = g.methodOn(t.X, getM, x, []string{k}, nil)
			setM := t.Ovl
			put = func(v string) { g.line("%s", g.methodOn(t.X, setM, x, []string{k, v}, nil)) }
		} else if _, ok := t.X.Typ().(*ListT); ok {
			cur = fmt.Sprintf("%s.At(%s)", x, k)
			put = func(v string) { g.line("%s.Set(%s, %s)", x, k, v) }
		} else {
			cur = fmt.Sprintf("%s.Get(%s)", x, k)
			put = func(v string) { g.line("%s.Set(%s, %s)", x, k, v) }
		}
	default:
		return
	}
	v := g.expr(val)
	if s.OpMethod != nil {
		put(g.methodOn(target, s.OpMethod, cur, []string{v}, nil))
		return
	}
	// 列表 += ：原地扩展
	if _, ok := tt.(*ListT); ok && op == TPlus {
		g.line("%s.ExtendList(%s)", cur, v)
		return
	}
	if st, ok := tt.(*SetT); ok {
		_ = st
		switch op {
		case TPipe:
			g.line("%s.Update(%s)", cur, v)
			return
		}
	}
	// 能用 Go 复合赋值的情形
	if (isNumeric(tt) || (isStr(tt) && isStr(val.Typ()))) && (op == TPlus || op == TMinus || op == TStar || (op == TSlash && isFloat(tt))) {
		if _, isIdent := target.(*Ident); isIdent || (func() bool { a, ok := target.(*Attr); return ok && a.Kind == AttrField })() {
			if isStr(tt) && op != TPlus {
			} else {
				g.line("%s %s= %s", cur, op, v)
				return
			}
		}
	}
	if isStr(tt) && op == TPlus && basicKind(val.Typ()) == BChar {
		put(fmt.Sprintf("(%s + %s.CharStr(%s))", cur, g.rt(), v))
		return
	}
	if basicKind(tt) == BChar && (op == TPlus || op == TMinus) {
		put(fmt.Sprintf("(%s %s %s.Char(%s))", cur, op, g.rt(), v))
		return
	}
	put(g.binCode(op, cur, tt, v, val.Typ(), tt))
}

// ===== if / while / for =====

func (g *Gen) ifStmt(s *IfStmt) {
	g.line("if %s {", g.cond(s.Cond))
	g.ind++
	g.stmts(s.Then)
	g.ind--
	if len(s.Else) > 0 {
		g.line("} else {")
		g.ind++
		g.stmts(s.Else)
		g.ind--
	}
	g.line("}")
}

func (g *Gen) cond(e Expr) string {
	c := g.expr(e)
	if strings.HasPrefix(c, "(") && strings.HasSuffix(c, ")") && balanced(c[1:len(c)-1]) {
		return c[1 : len(c)-1]
	}
	return c
}

func balanced(s string) bool {
	depth := 0
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

func (g *Gen) pushLoop() *genLoop {
	lp := &genLoop{tryDepth: g.fn.tryDepth}
	g.fn.loops = append(g.fn.loops, lp)
	return lp
}

func (g *Gen) popLoop() { g.fn.loops = g.fn.loops[:len(g.fn.loops)-1] }

// loopBody 把循环体生成到临时缓冲区，返回代码与是否需要标签。
func (g *Gen) loopBody(lp *genLoop, pre func(), body []Stmt) string {
	save := g.w
	var b strings.Builder
	g.w = &b
	g.ind++
	if pre != nil {
		pre()
	}
	g.stmts(body)
	g.ind--
	g.w = save
	return b.String()
}

func (g *Gen) emitLoop(lp *genLoop, header string, body string) {
	if lp.labelUsed {
		g.line("%s:", lp.label)
	}
	g.line("%s {", header)
	g.raw(body)
	g.line("}")
}

func (g *Gen) whileStmt(s *WhileStmt) {
	lp := g.pushLoop()
	lp.label = "Lꞏ" + strconv.Itoa(g.tmpN+1)
	g.tmpN++
	body := g.loopBody(lp, nil, s.Body)
	g.popLoop()
	header := "for"
	if !isTrueLit(s.Cond) {
		header = "for " + g.cond(s.Cond)
	}
	g.emitLoop(lp, header, body)
}

// bindTarget 生成循环变量的绑定语句。
func (g *Gen) bindTarget(target Expr, val string, t Type) {
	switch x := target.(type) {
	case *Ident:
		if x.Name == "_" || x.Sym == nil {
			return
		}
		name := g.local(x.Sym)
		g.line("%s := %s", name, val)
		g.line("_ = %s", name)
	case *TupleLit:
		tt, ok := t.(*TupleT)
		if !ok {
			return
		}
		tmp := g.tmp()
		g.line("%s := %s", tmp, val)
		for i, el := range x.Elems {
			g.bindTarget(el, fmt.Sprintf("%s.V%d", tmp, i), tt.Elems[i])
		}
	}
}

func identName(g *Gen, e Expr) string {
	if id, ok := e.(*Ident); ok {
		if id.Name == "_" || id.Sym == nil {
			return "_"
		}
		return g.local(id.Sym)
	}
	return ""
}

// loopHead 返回 for 循环的头部（不含 "{"）以及循环体开头需要的绑定。
func (g *Gen) loopHead(target Expr, iter Expr, info *LoopInfo, body []Stmt) (string, func()) {
	rt := g.rt()
	simple := identName(g, target)
	elemT := info.ElemT
	bindVia := func(val string) func() {
		return func() { g.bindTarget(target, val, elemT) }
	}
	switch info.Kind {
	case loopRange:
		var start, stop, step string
		args := info.Range
		et := elemT
		switch len(args) {
		case 1:
			start = g.zeroOf(et)
			stop = g.expr(args[0])
		case 2:
			start = g.expr(args[0])
			stop = g.expr(args[1])
		default:
			start = g.expr(args[0])
			stop = g.expr(args[1])
			step = g.expr(args[2])
		}
		v := simple
		if v == "" {
			v = "_"
		}
		counter := v
		var pre func()
		assigned := map[string]bool{}
		assignedNames(body, assigned)
		tid, _ := target.(*Ident)
		if v == "_" || (tid != nil && assigned[tid.Name]) {
			counter = g.tmp()
			if v != "_" {
				vv := v
				pre = func() {
					g.line("%s := %s", vv, counter)
					g.line("_ = %s", vv)
				}
			}
		}
		stopV := stop
		stopTmp := g.tmp()
		if step == "" {
			return fmt.Sprintf("for %s, %s := %s, %s; %s < %s; %s++", counter, stopTmp, start, stopV, counter, stopTmp, counter), pre
		}
		if v, ok := g.c.constInt(args[2]); ok {
			cmpOp := "<"
			if v < 0 {
				cmpOp = ">"
			}
			return fmt.Sprintf("for %s, %s := %s, %s; %s %s %s; %s += %s", counter, stopTmp, start, stopV, counter, cmpOp, stopTmp, counter, step), pre
		}
		stepTmp := g.tmp()
		return fmt.Sprintf("for %s, %s, %s := %s, %s, %s.CheckStep(%s); (%s > 0 && %s < %s) || (%s < 0 && %s > %s); %s += %s",
			counter, stopTmp, stepTmp, start, stopV, rt, step, stepTmp, counter, stopTmp, stepTmp, counter, stopTmp, counter, stepTmp), pre
	case loopList:
		x := g.expr(iter)
		if simple != "" {
			return fmt.Sprintf("for _, %s := range %s.S", simple, x), func() {
				if simple != "_" {
					g.line("_ = %s", simple)
				}
			}
		}
		v := g.tmp()
		return fmt.Sprintf("for _, %s := range %s.S", v, x), bindVia(v)
	case loopStr:
		v := g.tmp()
		return fmt.Sprintf("for _, %s := range %s", v, g.expr(iter)), bindVia(rt + ".Char(" + v + ")")
	case loopDict:
		x := g.expr(info.Inner)
		if info.Inner == nil {
			x = g.expr(iter)
		}
		if simple != "" {
			return fmt.Sprintf("for %s := range %s.KeysIter()", simple, x), func() {
				if simple != "_" {
					g.line("_ = %s", simple)
				}
			}
		}
		v := g.tmp()
		return fmt.Sprintf("for %s := range %s.KeysIter()", v, x), bindVia(v)
	case loopDictValues:
		x := g.expr(info.Inner)
		v := g.tmp()
		return fmt.Sprintf("for %s := range %s.ValuesIter()", v, x), bindVia(v)
	case loopDictItems:
		x := g.expr(info.Inner)
		if tl, ok := target.(*TupleLit); ok && len(tl.Elems) == 2 {
			k, vv := g.tmp(), g.tmp()
			tt := elemT.(*TupleT)
			return fmt.Sprintf("for %s, %s := range %s.All()", k, vv, x), func() {
				g.bindTarget(tl.Elems[0], k, tt.Elems[0])
				g.bindTarget(tl.Elems[1], vv, tt.Elems[1])
			}
		}
		v := g.tmp()
		return fmt.Sprintf("for %s := range %s.ItemsIter()", v, x), bindVia(v)
	case loopSet:
		v := g.tmp()
		return fmt.Sprintf("for %s := range %s.All()", v, g.expr(iter)), bindVia(v)
	case loopIter:
		v := g.tmp()
		return fmt.Sprintf("for %s := range %s", v, g.expr(iter)), bindVia(v)
	case loopChan:
		v := g.tmp()
		return fmt.Sprintf("for %s := range %s", v, g.expr(iter)), bindVia(v)
	case loopIterMethod:
		v := g.tmp()
		x := g.methodOn(iter, info.Method, g.expr(iter), nil, nil)
		return fmt.Sprintf("for %s := range %s", v, x), bindVia(v)
	case loopEnumerate:
		tt := elemT.(*TupleT)
		start := ""
		if len(info.Range) > 0 && info.Range[0] != nil {
			start = g.expr(info.Range[0])
		}
		idx, val := g.tmp(), g.tmp()
		bind := func() {
			iv := idx
			if start != "" && info.InnerKind == loopList {
				iv = "(" + idx + " + " + start + ")"
			}
			if tl, ok := target.(*TupleLit); ok && len(tl.Elems) == 2 {
				g.bindTarget(tl.Elems[0], iv, tInt)
				g.bindTarget(tl.Elems[1], val, tt.Elems[1])
			} else {
				g.bindTarget(target, fmt.Sprintf("%s{%s, %s}", g.typ(tt), iv, val), tt)
			}
		}
		inner := g.expr(info.Inner)
		switch info.InnerKind {
		case loopList:
			return fmt.Sprintf("for %s, %s := range %s.S", idx, val, inner), bind
		}
		// 其余情况使用计数器
		cnt := g.tmp()
		if start == "" {
			start = "0"
		}
		var src string
		switch info.InnerKind {
		case loopStr:
			src = rt + ".StrIter(" + inner + ")"
		case loopDict:
			src = inner + ".KeysIter()"
		case loopSet:
			src = inner + ".All()"
		case loopIterMethod:
			src = g.methodOn(info.Inner, info.Method, inner, nil, nil)
		default:
			src = inner
		}
		return fmt.Sprintf("for %s := range %s.Enumerate(%s, %s)", cnt, rt, src, start), func() {
			g.line("%s, %s := %s.V0, %s.V1", idx, val, cnt, cnt)
			bind()
		}
	case loopZip:
		a, b := g.expr(info.Inner), g.expr(info.Inner2)
		ta, tb, i := g.tmp(), g.tmp(), g.tmp()
		tt := elemT.(*TupleT)
		return fmt.Sprintf("for %s, %s, %s := %s, %s, 0; %s < min(%s.Len(), %s.Len()); %s++", ta, tb, i, a, b, i, ta, tb, i), func() {
			if tl, ok := target.(*TupleLit); ok && len(tl.Elems) == 2 {
				g.bindTarget(tl.Elems[0], fmt.Sprintf("%s.S[%s]", ta, i), tt.Elems[0])
				g.bindTarget(tl.Elems[1], fmt.Sprintf("%s.S[%s]", tb, i), tt.Elems[1])
			} else {
				g.bindTarget(target, fmt.Sprintf("%s{%s.S[%s], %s.S[%s]}", g.typ(tt), ta, i, tb, i), tt)
			}
		}
	}
	return "for false", nil
}

func isConstLike(e Expr) bool {
	switch x := e.(type) {
	case *IntLit:
		return true
	case *Ident:
		return x.Sym != nil && x.Sym.Kind == SConst
	}
	return false
}

func (g *Gen) zeroOf(t Type) string {
	switch basicKind(t) {
	case BInt:
		return "0"
	}
	return g.typ(t) + "(0)"
}

func (g *Gen) forStmt(s *ForStmt) {
	lp := g.pushLoop()
	g.tmpN++
	lp.label = "Lꞏ" + strconv.Itoa(g.tmpN)
	header, pre := g.loopHead(s.Target, s.Iter, s.Loop, s.Body)
	body := g.loopBody(lp, pre, s.Body)
	g.popLoop()
	g.emitLoop(lp, header, body)
}

// jump 生成 break（kind=2）或 continue（kind=3）。
func (g *Gen) jump(kind int) {
	if len(g.fn.loops) == 0 {
		return
	}
	lp := g.fn.loops[len(g.fn.loops)-1]
	if g.fn.tryDepth > lp.tryDepth {
		g.line("ꞏflow = %d", kind)
		g.line("return")
		for i := lp.tryDepth; i < g.fn.tryDepth; i++ {
			tr := g.fn.trys[i]
			if kind == 2 {
				tr.usedBreak = true
			} else {
				tr.usedContinue = true
			}
		}
		return
	}
	word := "break"
	if kind == 3 {
		word = "continue"
	}
	if lp.selDepth > 0 {
		lp.labelUsed = true
		g.line("%s %s", word, lp.label)
		return
	}
	g.line("%s", word)
}

func (g *Gen) markTrys(kind int) {
	for _, tr := range g.fn.trys {
		if kind == 1 {
			tr.usedReturn = true
		}
	}
}

func (g *Gen) returnStmt(s *ReturnStmt) {
	if g.fn.tryDepth > 0 {
		if s.Value != nil {
			g.line("ꞏret = %s", g.expr(s.Value))
		}
		g.line("ꞏflow = 1")
		g.line("return")
		g.markTrys(1)
		return
	}
	if g.fn.isGen || s.Value == nil {
		g.line("return")
		return
	}
	g.line("return %s", g.expr(s.Value))
}

// dispatch 在 try/with 闭包之后处理被闭包截获的 return/break/continue。
func (g *Gen) dispatch(tr *genTry) {
	inner := g.fn.tryDepth > 0
	if tr.usedReturn {
		if inner {
			g.line("if ꞏflow == 1 {")
			g.line("\treturn")
			g.line("}")
			g.fn.trys[g.fn.tryDepth-1].usedReturn = true
		} else if g.fn.isGen || isVoid(g.fn.retT) {
			g.line("if ꞏflow == 1 {")
			g.line("\treturn")
			g.line("}")
		} else {
			g.line("if ꞏflow == 1 {")
			g.line("\treturn ꞏret")
			g.line("}")
		}
	}
	for _, k := range []int{2, 3} {
		used := (k == 2 && tr.usedBreak) || (k == 3 && tr.usedContinue)
		if !used {
			continue
		}
		var lp *genLoop
		if len(g.fn.loops) > 0 {
			lp = g.fn.loops[len(g.fn.loops)-1]
		}
		if lp != nil && lp.tryDepth == g.fn.tryDepth {
			word := "break"
			if k == 3 {
				word = "continue"
			}
			if lp.selDepth > 0 {
				lp.labelUsed = true
				word += " " + lp.label
			}
			g.line("if ꞏflow == %d {", k)
			g.line("\tꞏflow = 0")
			g.line("\t%s", word)
			g.line("}")
		} else if inner {
			g.line("if ꞏflow == %d {", k)
			g.line("\treturn")
			g.line("}")
			if k == 2 {
				g.fn.trys[g.fn.tryDepth-1].usedBreak = true
			} else {
				g.fn.trys[g.fn.tryDepth-1].usedContinue = true
			}
		}
	}
}

func (g *Gen) enterTry() *genTry {
	tr := &genTry{}
	g.fn.trys = append(g.fn.trys, tr)
	g.fn.tryDepth++
	return tr
}

func (g *Gen) exitTry() {
	g.fn.tryDepth--
	g.fn.trys = g.fn.trys[:len(g.fn.trys)-1]
}

// ===== try / with =====

func (g *Gen) tryStmt(s *TryStmt) {
	rt := g.rt()
	okVar := ""
	if s.Else != nil {
		okVar = g.tmp()
		g.line("%s := false", okVar)
	}
	tr := g.enterTry()
	g.line("func() {")
	g.ind++
	if s.Finally != nil {
		g.line("defer func() {")
		g.ind++
		saveLoops := g.fn.loops
		g.fn.loops = nil
		g.stmts(s.Finally)
		g.fn.loops = saveLoops
		g.ind--
		g.line("}()")
	}
	if len(s.Handlers) > 0 {
		g.line("func() {")
		g.ind++
		g.fn.trys = append(g.fn.trys, tr)
		g.fn.tryDepth++
		rv := g.tmp()
		ev := g.tmp()
		g.line("defer func() {")
		g.ind++
		g.line("if %s := recover(); %s != nil {", rv, rv)
		g.ind++
		g.line("%s := %s.Catch(%s)", ev, rt, rv)
		g.line("_ = %s", ev)
		for i, h := range s.Handlers {
			var conds []string
			for _, t := range h.TDs {
				conds = append(conds, fmt.Sprintf("%s.Is[%s](%s)", rt, g.typ(t), ev))
			}
			cond := "true"
			if len(conds) > 0 {
				cond = strings.Join(conds, " || ")
			}
			if i == 0 {
				g.line("if %s {", cond)
			} else {
				g.line("} else if %s {", cond)
			}
			g.ind++
			if h.Sym != nil {
				name := g.local(h.Sym)
				g.line("%s := %s.(%s)", name, ev, g.typ(h.Sym.T))
				g.line("_ = %s", name)
			}
			g.fn.handlers = append(g.fn.handlers, rv)
			g.pos(h.Pos)
			g.stmts(h.Body)
			g.fn.handlers = g.fn.handlers[:len(g.fn.handlers)-1]
			g.ind--
		}
		g.line("} else {")
		g.line("\tpanic(%s)", rv)
		g.line("}")
		g.ind--
		g.line("}")
		g.ind--
		g.line("}()")
		g.stmts(s.Body)
		if okVar != "" {
			g.line("%s = true", okVar)
		}
		g.fn.tryDepth--
		g.fn.trys = g.fn.trys[:len(g.fn.trys)-1]
		g.ind--
		g.line("}()")
		// 内层闭包截获的控制流传播到外层闭包
		if tr.usedReturn || tr.usedBreak || tr.usedContinue {
			g.line("if ꞏflow != 0 {")
			g.line("\treturn")
			g.line("}")
		}
	} else {
		g.stmts(s.Body)
		if okVar != "" {
			g.line("%s = true", okVar)
		}
	}
	if s.Else != nil {
		g.line("if %s {", okVar)
		g.ind++
		g.stmts(s.Else)
		g.ind--
		g.line("}")
	}
	g.ind--
	g.line("}()")
	g.exitTry()
	g.dispatch(tr)
}

func (g *Gen) withStmt(s *WithStmt, items []*WithItem) {
	if len(items) == 0 {
		g.stmts(s.Body)
		return
	}
	item := items[0]
	g.line("{")
	g.ind++
	w := g.tmp()
	g.posAgain(item.Pos)
	g.line("%s := %s", w, g.expr(item.X))
	wx := &goExpr{exprBase{Pos: item.X.P(), T: item.X.Typ()}, w}
	val := w
	if item.Enter != nil {
		call := g.methodOn(wx, item.Enter, w, nil, nil)
		if isVoid(item.Enter.Sig.Ret) {
			g.line("%s", call)
		} else {
			val = call
		}
	}
	if item.Sym != nil {
		name := g.local(item.Sym)
		g.line("%s := %s", name, val)
		g.line("_ = %s", name)
	} else if val != w {
		g.line("_ = %s", val)
	}
	tr := g.enterTry()
	g.line("func() {")
	g.ind++
	if item.Exit != nil {
		g.line("defer %s", g.methodOn(wx, item.Exit, w, nil, nil))
	}
	g.withStmt(s, items[1:])
	g.ind--
	g.line("}()")
	g.exitTry()
	g.dispatch(tr)
	g.ind--
	g.line("}")
}

// goDefer 生成 go / defer 语句：参数立即求值（与 Go 相同）。
func (g *Gen) goDefer(e Expr, word string) {
	call, ok := e.(*Call)
	if !ok {
		return
	}
	// 把接收者与实参存入临时变量
	var pre []string
	saveFinal := append([]Expr(nil), call.Final...)
	saveRecv := call.Recv
	for i, a := range call.Final {
		if a == nil || isConstLike(a) {
			continue
		}
		switch a.(type) {
		case *StrLit, *IntLit, *FloatLit, *BoolLit, *CharLit, *Lambda:
			continue
		}
		tmp := g.tmp()
		pre = append(pre, fmt.Sprintf("%s := %s", tmp, g.expr(a)))
		call.Final[i] = &goExpr{exprBase{Pos: a.P(), T: a.Typ()}, tmp}
	}
	if call.Recv != nil && !isSimpleRecv(call.Recv) {
		tmp := g.tmp()
		pre = append(pre, fmt.Sprintf("%s := %s", tmp, g.expr(call.Recv)))
		call.Recv = &goExpr{exprBase{Pos: call.Recv.P(), T: call.Recv.Typ()}, tmp}
	}
	code := g.expr(call)
	call.Final = saveFinal
	call.Recv = saveRecv
	if len(pre) > 0 {
		g.line("{")
		g.ind++
		g.posAgain(e.P())
		for _, p := range pre {
			g.line("%s", p)
		}
	}
	stmt := code
	if t := call.Typ(); t != nil && !isVoid(t) {
		stmt = "_ = " + code
	}
	if word == "go" {
		g.line("go func() {")
		g.line("\tdefer %s.GoGuard()", g.rt())
		g.line("\t%s", stmt)
		g.line("}()")
	} else {
		g.line("defer func() {")
		g.line("\t%s", stmt)
		g.line("}()")
	}
	if len(pre) > 0 {
		g.ind--
		g.line("}")
	}
}

func (g *Gen) assertStmt(s *AssertStmt) {
	rt := g.rt()
	msg := `""`
	if s.Msg != nil {
		msg = g.expr(s.Msg)
	}
	cond := unconv(s.Cond)
	if cmp, ok := cond.(*Compare); ok && len(cmp.Ops) == 1 && (cmp.Ops[0] == TEq || cmp.Ops[0] == TNe) && cmp.Ovl[0] == nil {
		if _, isNull := unconv(cmp.Xs[1]).(*NullLit); !isNull {
			if _, isNull := unconv(cmp.Xs[0]).(*NullLit); !isNull {
				l, r := g.tmp(), g.tmp()
				a, b := cmp.Xs[0], cmp.Xs[1]
				la := &goExpr{exprBase{Pos: a.P(), T: a.Typ()}, l}
				rb := &goExpr{exprBase{Pos: b.P(), T: b.Typ()}, r}
				save := cmp.Xs
				cmp.Xs = []Expr{la, rb}
				test := g.expr(cmp)
				cmp.Xs = save
				g.line("if %s, %s := %s, %s; !%s {", l, r, g.expr(a), g.expr(b), test)
				g.posAgain(s.Pos)
				g.line("\t%s.AssertEqFail(%q, %q, %s.Repr(%s), %s.Repr(%s), %s)", rt, s.Src, cmp.Ops[0].String(), rt, l, rt, r, msg)
				g.line("}")
				return
			}
		}
	}
	g.line("if !(%s) {", g.cond(s.Cond))
	g.posAgain(s.Pos)
	g.line("\t%s.AssertFail(%q, %s)", rt, s.Src, msg)
	g.line("}")
}

func (g *Gen) nestedFunc(fd *FuncDecl) {
	if fd.Fn == nil || fd.Sym == nil {
		return
	}
	name := g.local(fd.Sym)
	sig := fd.Sig
	g.line("%s = func(%s)%s {", name, g.paramList(fd.Fn, sig), g.retSig(sig))
	g.ind++
	saveFn := g.fn
	g.funcBody(fd.Fn, fd.Body, sig)
	g.fn = saveFn
	g.ind--
	g.line("}")
}

// ===== select =====

func (g *Gen) selectStmt(s *SelectStmt) {
	var lp *genLoop
	if len(g.fn.loops) > 0 {
		lp = g.fn.loops[len(g.fn.loops)-1]
		lp.selDepth++
	}
	g.line("select {")
	for _, sc := range s.Cases {
		switch sc.Kind {
		case selDefault:
			g.line("default:")
		case selSend:
			g.line("case %s <- %s:", g.expr(sc.Chan), g.expr(sc.Value))
		case selRecv:
			v, ok := g.tmp(), g.tmp()
			g.line("case %s, %s := <-%s:", v, ok, g.expr(sc.Chan))
			g.ind++
			g.line("if !%s {", ok)
			g.line("\tpanic(%s.ClosedErr())", g.rt())
			g.line("}")
			if sc.Sym != nil {
				name := g.local(sc.Sym)
				if sc.Decl {
					g.line("%s := %s", name, v)
					g.line("_ = %s", name)
				} else {
					g.line("%s = %s", g.symName(sc.Sym), g.coerce(v, sc.Chan.Typ().(*ChanT).Elem, sc.Sym.T))
				}
			} else {
				g.line("_ = %s", v)
			}
			g.ind--
		}
		g.ind++
		g.stmts(sc.Body)
		g.ind--
	}
	g.line("}")
	if lp != nil {
		lp.selDepth--
	}
}

// ===== match =====

type patStep struct {
	cond string
	bind string
}

func (g *Gen) matchStmt(s *MatchStmt) {
	g.line("{")
	g.ind++
	subj := g.tmp()
	g.posAgain(s.Pos)
	g.line("%s := %s", subj, g.expr(s.Subject))
	g.line("_ = %s", subj)
	done := ""
	if len(s.Cases) > 1 {
		done = g.tmp()
		g.line("%s := false", done)
		g.line("_ = %s", done)
	}
	for i, cs := range s.Cases {
		steps := g.patSteps(cs.Pat, subj, s.SubjT)
		opened := 0
		if done != "" && i > 0 {
			g.line("if !%s {", done)
			g.ind++
			opened++
		}
		for _, st := range steps {
			if st.cond != "" {
				g.line("if %s {", st.cond)
				g.ind++
				opened++
			}
			if st.bind != "" {
				g.line("%s", st.bind)
			}
		}
		if cs.Guard != nil {
			g.line("if %s {", g.cond(cs.Guard))
			g.ind++
			opened++
		}
		if done != "" {
			g.line("%s = true", done)
		}
		g.pos(cs.Pos)
		g.stmts(cs.Body)
		for ; opened > 0; opened-- {
			g.ind--
			g.line("}")
		}
	}
	g.ind--
	g.line("}")
}

// patSteps 把模式编译为一系列条件与绑定。
func (g *Gen) patSteps(p Pattern, subj string, t Type) []patStep {
	rt := g.rt()
	switch x := p.(type) {
	case *WildPat:
		return nil
	case *NamePat:
		switch x.Kind {
		case patCapture:
			if x.Sym == nil {
				return nil
			}
			name := g.local(x.Sym)
			return []patStep{{bind: fmt.Sprintf("%s := %s; _ = %s", name, subj, name)}}
		case patVariant:
			return []patStep{{cond: g.variantTest(x.Variant, subj, t)}}
		case patType:
			return []patStep{{cond: g.typeTest(subj, t, x.Type)}}
		case patValue:
			return []patStep{{cond: g.eqCode(subj, g.expr(x.Value), optElem(t), isOpt(t))}}
		}
	case *LitPat:
		lit := unconv(x.X)
		if _, ok := lit.(*NullLit); ok {
			if isOpt(t) {
				return []patStep{{cond: "!" + subj + ".Ok"}}
			}
			return []patStep{{cond: subj + " == nil"}}
		}
		if isAny(t) {
			return []patStep{{cond: fmt.Sprintf("%s.Equal[any](%s, %s)", rt, subj, g.expr(x.X))}}
		}
		return []patStep{{cond: g.eqCode(subj, g.expr(x.X), optElem(t), isOpt(t))}}
	case *OrPat:
		var alts []string
		for _, a := range x.Alts {
			steps := g.patSteps(a, subj, t)
			var cs []string
			for _, st := range steps {
				if st.cond != "" {
					cs = append(cs, st.cond)
				}
			}
			if len(cs) == 0 {
				alts = append(alts, "true")
			} else {
				alts = append(alts, "("+strings.Join(cs, " && ")+")")
			}
		}
		return []patStep{{cond: strings.Join(alts, " || ")}}
	case *TuplePat:
		var out []patStep
		base := subj
		if isOpt(t) {
			out = append(out, patStep{cond: subj + ".Ok"})
			base = subj + ".V"
		}
		tt := x.TT
		for i, e := range x.Elems {
			out = append(out, g.patSteps(e, fmt.Sprintf("%s.V%d", base, i), tt.Elems[i])...)
		}
		return out
	case *ListPat:
		var out []patStep
		n := len(x.Elems)
		if x.RestIdx < 0 {
			out = append(out, patStep{cond: fmt.Sprintf("%s.Len() == %d", subj, n)})
		} else {
			out = append(out, patStep{cond: fmt.Sprintf("%s.Len() >= %d", subj, n)})
		}
		for i, e := range x.Elems {
			var idx string
			if x.RestIdx >= 0 && i >= x.RestIdx {
				idx = fmt.Sprintf("%s.S[%s.Len()-%d]", subj, subj, n-i)
			} else {
				idx = fmt.Sprintf("%s.S[%d]", subj, i)
			}
			out = append(out, g.patSteps(e, idx, x.Elem)...)
		}
		if x.RestSym != nil {
			name := g.local(x.RestSym)
			after := n - x.RestIdx
			out = append(out, patStep{bind: fmt.Sprintf("%s := %s.Slice(%d, %s.Len()-%d, 1); _ = %s", name, subj, x.RestIdx, subj, after, name)})
		}
		return out
	case *CallPat:
		var out []patStep
		v := g.tmp()
		if x.Variant != nil {
			vt := x.Variant.GoName + g.targs(x.TD.Args)
			src := subj
			if isOpt(t) {
				out = append(out, patStep{cond: subj + ".Ok"})
				src = subj + ".V"
			}
			out = append(out, patStep{cond: fmt.Sprintf("%s.Is[%s](%s)", rt, vt, src)})
			out = append(out, patStep{bind: fmt.Sprintf("%s := %s.(%s); _ = %s", v, src, vt, v)})
		} else {
			src := subj
			if isOpt(t) {
				out = append(out, patStep{cond: subj + ".Ok"})
				src = subj + ".V"
			}
			if identical(optElem(t), x.TD) {
				out = append(out, patStep{bind: fmt.Sprintf("%s := %s; _ = %s", v, src, v)})
			} else {
				gt := g.typ(x.TD)
				out = append(out, patStep{cond: fmt.Sprintf("%s.Is[%s](%s)", rt, gt, src)})
				out = append(out, patStep{bind: fmt.Sprintf("%s := %s.(%s); _ = %s", v, src, gt, v)})
			}
		}
		for i, a := range x.Args {
			f := x.Fields[i]
			if f == nil {
				continue
			}
			var ref string
			if x.Variant == nil && isPolyType(x.TD) {
				ref = fmt.Sprintf("%s.%sꞏ().%s", v, f.Owner.GoName, f.GoName)
			} else {
				ref = v + "." + f.GoName
			}
			ft := f.T
			if x.TD != nil {
				m := Subst{}
				for _, anc := range supertypes(x.TD) {
					for k, vv := range makeSubst(anc.D.TParams, anc.Args) {
						m[k] = vv
					}
				}
				ft = subst(ft, m)
			}
			out = append(out, g.patSteps(a, ref, ft)...)
		}
		return out
	case *TypePat:
		src := subj
		var out []patStep
		if isOpt(t) {
			out = append(out, patStep{cond: subj + ".Ok"})
			src = subj + ".V"
		}
		if identical(optElem(t), x.To) {
			if x.Sym != nil {
				name := g.local(x.Sym)
				out = append(out, patStep{bind: fmt.Sprintf("%s := %s; _ = %s", name, src, name)})
			}
			return out
		}
		gt := g.typ(x.To)
		out = append(out, patStep{cond: fmt.Sprintf("%s.Is[%s](%s)", rt, gt, src)})
		if x.Sym != nil {
			name := g.local(x.Sym)
			out = append(out, patStep{bind: fmt.Sprintf("%s := %s.(%s); _ = %s", name, src, gt, name)})
		}
		return out
	case *AsPat:
		out := g.patSteps(x.Pat, subj, t)
		if x.Sym != nil {
			name := g.local(x.Sym)
			val := subj
			if !identical(x.Sym.T, t) {
				if isOpt(t) && identical(optElem(t), x.Sym.T) {
					val = subj + ".V"
				} else if isPolyType(t) || isAny(optElem(t)) || isPolyType(optElem(t)) {
					src := subj
					if isOpt(t) {
						src = subj + ".V"
					}
					val = fmt.Sprintf("%s.(%s)", src, g.typ(x.Sym.T))
				}
			}
			out = append(out, patStep{bind: fmt.Sprintf("%s := %s; _ = %s", name, val, name)})
		}
		return out
	}
	return nil
}

func (g *Gen) variantTest(v *Variant, subj string, t Type) string {
	td := v.Owner
	src := subj
	prefix := ""
	if isOpt(t) {
		prefix = subj + ".Ok && "
		src = subj + ".V"
	}
	if !td.ADT {
		return prefix + src + " == " + v.GoName
	}
	nt, _ := optElem(t).(*NamedT)
	var args []Type
	if nt != nil {
		args = nt.Args
	}
	return fmt.Sprintf("%s%s.Is[%s%s](%s)", prefix, g.rt(), v.GoName, g.targs(args), src)
}

func (g *Gen) typeTest(subj string, from, to Type) string {
	src := subj
	prefix := ""
	if isOpt(from) {
		prefix = subj + ".Ok && "
		src = subj + ".V"
	}
	if identical(optElem(from), to) {
		if prefix != "" {
			return subj + ".Ok"
		}
		return "true"
	}
	return fmt.Sprintf("%s%s.Is[%s](%s)", prefix, g.rt(), g.typ(to), src)
}
