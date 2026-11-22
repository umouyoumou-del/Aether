package compiler

import (
	"strings"
)

// ===== 语句 =====

func (c *Checker) stmts(list []Stmt) {
	warned := false
	wasDead := false
	for _, s := range list {
		if c.flow.dead {
			if !warned {
				if _, ok := s.(*PassStmt); !ok {
					c.warnf(s.P(), "这行代码永远不会执行")
				}
				warned = true
			}
			// 仍然检查后续代码，但不让死代码状态引起级联错误
			c.flow.dead = false
			wasDead = true
		}
		c.stmt(s)
	}
	if wasDead {
		c.flow.dead = true
	}
}

func (c *Checker) block(list []Stmt) {
	c.pushScope()
	c.stmts(list)
	c.popScope()
}

func (c *Checker) stmt(s Stmt) {
	switch s := s.(type) {
	case *ExprStmt:
		c.exprStmt(s)
	case *AssignStmt:
		if s.Op == TAssign {
			c.assign(s)
		} else {
			c.augAssign(s)
		}
	case *VarDecl:
		c.varDecl(s)
	case *ConstDecl:
		c.constDecl(s)
	case *IfStmt:
		c.ifStmt(s)
	case *WhileStmt:
		c.whileStmt(s)
	case *ForStmt:
		c.forStmt(s)
	case *BreakStmt, *ContinueStmt:
		if len(c.loops) == 0 {
			if _, ok := s.(*BreakStmt); ok {
				c.errorf(s.P(), "break 只能用在循环中")
			} else {
				c.errorf(s.P(), "continue 只能用在循环中")
			}
		} else {
			lc := c.loops[len(c.loops)-1]
			if _, ok := s.(*BreakStmt); ok {
				lc.breaks = append(lc.breaks, c.flow.copy())
				if lc.info != nil {
					lc.info.HasBreak = true
				}
			}
		}
		c.flow.dead = true
	case *PassStmt:
	case *ReturnStmt:
		c.returnStmt(s)
	case *RaiseStmt:
		c.raiseStmt(s)
	case *TryStmt:
		c.tryStmt(s)
	case *WithStmt:
		c.withStmt(s)
	case *DeferStmt:
		c.deferStmt(s)
	case *GoStmt:
		c.goStmt(s)
	case *AssertStmt:
		c.cond(&s.Cond)
		if s.Msg != nil {
			c.expr(s.Msg, tStr)
		}
	case *DelStmt:
		c.delStmt(s)
	case *MatchStmt:
		c.matchStmt(s)
	case *SelectStmt:
		c.selectStmt(s)
	case *YieldStmt:
		c.yieldStmt(s)
	case *FuncDeclStmt:
		c.nestedFunc(s)
	case *GlobalStmt:
		if s.Nonlocal {
			return
		}
		if c.fn == nil || c.fn.IsModInit {
			c.warnf(s.Pos, "global 只在函数中有意义")
			return
		}
		for _, n := range s.Names {
			sym := c.mod.Scope.Syms[n]
			if sym == nil || sym.Kind != SVar {
				if c.mod.pendingGlobals[n] {
					continue
				}
				c.errorf(s.Pos, "模块中没有全局变量 %s", n)
			}
		}
	case *FuncDecl, *ClassDecl, *EnumDecl, *ExtendDecl, *ImportStmt, *TestDecl:
		// 顶层声明已在收集阶段处理
	default:
		c.errorf(s.P(), "未知的语句")
	}
}

func (c *Checker) exprStmt(s *ExprStmt) {
	t := c.expr(s.X, nil)
	switch x := s.X.(type) {
	case *Call, *Await:
		return
	case *StrLit:
		return // 文档字符串
	case *Attr:
		if x.Kind == AttrProp {
			return
		}
	case *Compare:
		if len(x.Ops) == 1 && x.Ops[0] == TEq {
			c.warnf(s.Pos, "比较结果没有被使用；赋值请用 =")
			return
		}
	case *Ident:
		if x.Sym != nil && (x.Sym.Kind == SFunc || x.Sym.Kind == SBuiltin) {
			c.warnf(s.Pos, "函数没有被调用；是不是漏了 ()？")
			return
		}
	}
	if _, ok := t.(*MetaT); ok {
		return
	}
	if !isInvalid(t) && !isVoid(t) {
		c.warnf(s.Pos, "表达式的值没有被使用")
	}
}

// ===== 赋值 =====

func (c *Checker) isShadowable(sym *Symbol) bool {
	if sym.Universe {
		return true
	}
	// prelude 中的名字（例如 counter、repeat）可以被用户变量遮蔽
	if sym.Mod != nil && sym.Mod.IsPrelude && c.mod != nil && !c.mod.IsPrelude && sym.Kind != SVar {
		if c.mod.Scope.Syms[sym.Name] != sym {
			return true
		}
	}
	return false
}

// assignableTargetSym 查找赋值目标符号；返回 nil 表示需要声明新变量。
func (c *Checker) targetSym(id *Ident) *Symbol {
	sym := c.scope.lookup(id.Name)
	if sym == nil {
		return nil
	}
	if c.isShadowable(sym) {
		return nil
	}
	if c.globalNeedsDecl(sym) {
		// 函数中给模块级变量赋值：没有 global 声明时创建局部变量（与 Python 相同）
		if p, ok := c.fn.globalReads[id.Name]; ok {
			d := c.errorf(id.Pos, "%s 在第 %d 行作为全局变量读取，这里又赋值，会产生同名的局部变量", id.Name, p.Line)
			d.Notes = append(d.Notes, "如需修改全局变量，请在函数开头写 global "+id.Name)
		}
		return nil
	}
	return sym
}

// globalNeedsDecl 判断在当前函数中给 sym 赋值是否需要 global 声明。
func (c *Checker) globalNeedsDecl(sym *Symbol) bool {
	if !sym.Global || sym.Kind != SVar || sym.Field != nil {
		return false
	}
	f := c.fn
	if f == nil || f.IsModInit {
		return false
	}
	return !f.globals[sym.Name]
}

func (c *Checker) assign(s *AssignStmt) {
	if len(s.Targets) > 1 {
		// 链式赋值 a = b = v
		var want Type
		if id, ok := s.Targets[0].(*Ident); ok {
			if sym := c.targetSym(id); sym != nil && sym.Kind == SVar {
				want = sym.T
			}
		}
		vt := c.expr(s.Value, want)
		if isVoid(vt) {
			c.errorf(s.Value.P(), "这个表达式没有值，不能用于赋值")
			return
		}
		for _, t := range s.Targets {
			c.assignTarget(t, vt, nil, s.Pos)
		}
		return
	}
	target := s.Targets[0]
	if tl, ok := target.(*TupleLit); ok {
		c.assignTuple(tl, &s.Value, s.Pos)
		return
	}
	if ll, ok := target.(*ListLit); ok {
		c.assignTuple(&TupleLit{exprBase: ll.exprBase, Elems: ll.Elems}, &s.Value, s.Pos)
		s.Targets[0] = &TupleLit{exprBase: ll.exprBase, Elems: ll.Elems}
		return
	}
	c.assignTarget(target, nil, &s.Value, s.Pos)
}

// assignTarget 给单个目标赋值。valueSlot 非空时检查并转换值；否则值的类型为 vt（链式/解构赋值）。
func (c *Checker) assignTarget(target Expr, vt Type, valueSlot *Expr, pos Pos) {
	switch t := target.(type) {
	case *Ident:
		if t.Name == "_" {
			if valueSlot != nil {
				c.expr(*valueSlot, nil)
			}
			t.T = vt
			return
		}
		sym := c.targetSym(t)
		if sym == nil {
			// 声明新变量
			if valueSlot != nil {
				vt = c.expr(*valueSlot, nil)
			}
			vt = c.declType(vt, (func() Pos {
				if valueSlot != nil {
					return (*valueSlot).P()
				}
				return pos
			})(), t.Name)
			sym = c.declareLocal(t.Name, vt, t.Pos, false)
			sym.Assigns = 1
			t.Sym = sym
			t.T = vt
			c.flow.assigned[sym] = true
			return
		}
		t.Sym = sym
		switch sym.Kind {
		case SVar:
			if sym.Pending {
				// 模块级变量第一次赋值
				if valueSlot != nil {
					vt = c.expr(*valueSlot, nil)
				}
				vt = c.declType(vt, pos, t.Name)
				sym.T = vt
				sym.Pending = false
				t.T = vt
				sym.Assigns++
				c.flow.assigned[sym] = true
				return
			}
			if valueSlot != nil {
				c.exprTo(valueSlot, sym.T, "赋值给 "+t.Name)
			} else if _, ok := c.assignable(vt, sym.T); !ok && !isInvalid(vt) {
				c.errorf(t.Pos, "不能把 %s 类型的值赋给 %s 类型的变量 %s", vt, sym.T, t.Name)
			}
			t.T = sym.T
			sym.Assigns++
			if sym.Fn != nil && sym.Fn != c.fn && !sym.Global {
				sym.ClosureAssigned = true
				sym.Captured = true
			}
			c.flow.assigned[sym] = true
			delete(c.flow.narrow, sym)
			if sym.IsSelf {
				c.errorf(t.Pos, "不能给 self 赋值")
			}
		case SConst:
			c.errorf(t.Pos, "不能给常量 %s 赋值", t.Name)
		case SFunc:
			c.errorf(t.Pos, "不能给函数 %s 赋值", t.Name)
		case SType:
			c.errorf(t.Pos, "不能给类型 %s 赋值", t.Name)
		case SModule:
			c.errorf(t.Pos, "不能给模块 %s 赋值", t.Name)
		default:
			c.errorf(t.Pos, "不能给 %s 赋值", t.Name)
		}
	case *Attr:
		ft := c.attrTarget(t, valueSlot, vt)
		if valueSlot != nil && ft != nil {
			c.exprTo(valueSlot, ft, "赋值给 "+t.Name)
		} else if ft != nil && vt != nil {
			if _, ok := c.assignable(vt, ft); !ok {
				c.errorf(t.Pos, "不能把 %s 赋给 %s 类型的成员 %s", vt, ft, t.Name)
			}
		}
	case *Index:
		et := c.indexTarget(t)
		if valueSlot != nil && et != nil {
			c.exprTo(valueSlot, et, "下标赋值")
		} else if et != nil && vt != nil {
			if _, ok := c.assignable(vt, et); !ok {
				c.errorf(t.Pos, "不能把 %s 赋给 %s 类型的元素", vt, et)
			}
		}
	case *TupleLit:
		if valueSlot != nil {
			c.assignTuple(t, valueSlot, pos)
			return
		}
		tt, ok := vt.(*TupleT)
		if !ok || len(tt.Elems) != len(t.Elems) {
			c.errorf(t.Pos, "不能把 %s 解构为 %d 个变量", vt, len(t.Elems))
			return
		}
		for i, e := range t.Elems {
			c.assignTarget(e, tt.Elems[i], nil, pos)
		}
		t.T = vt
	default:
		c.errorf(target.P(), "不能给这个表达式赋值")
	}
}

// declType 校验用于推断新变量类型的值类型。
func (c *Checker) declType(vt Type, pos Pos, name string) Type {
	switch {
	case isInvalid(vt):
		return tInvalid
	case isVoid(vt):
		c.errorf(pos, "这个表达式没有值，不能赋给 %s", name)
		return tInvalid
	case isNull(vt):
		d := c.errorf(pos, "无法从 null 推断 %s 的类型", name)
		d.Notes = append(d.Notes, "请写出类型，例如 "+name+": str? = null")
		return tInvalid
	}
	switch x := vt.(type) {
	case *MetaT:
		c.errorf(pos, "%s 是类型，不能作为值赋给变量", x.T)
		return tInvalid
	case *ModT:
		c.errorf(pos, "模块不能赋值给变量")
		return tInvalid
	case *FuncRefT:
		c.errorf(pos, "泛型函数 %s 不能直接作为值使用；请用 lambda 包装", x.Name)
		return tInvalid
	}
	return vt
}

// assignTuple 处理 a, b = ... 形式的解构赋值。
func (c *Checker) assignTuple(tl *TupleLit, valueSlot *Expr, pos Pos) {
	if vl, ok := (*valueSlot).(*TupleLit); ok && len(vl.Elems) == len(tl.Elems) {
		// 并行赋值 a, b = b, a：右侧先全部求值
		var wants []Type
		for _, e := range tl.Elems {
			var w Type
			if id, ok := e.(*Ident); ok {
				if sym := c.targetSym(id); sym != nil && sym.Kind == SVar && !sym.Pending {
					w = sym.T
				}
			}
			wants = append(wants, w)
		}
		types := make([]Type, len(vl.Elems))
		for i := range vl.Elems {
			if wants[i] != nil {
				c.exprTo(&vl.Elems[i], wants[i], "赋值")
				types[i] = wants[i]
			} else {
				types[i] = c.expr(vl.Elems[i], nil)
			}
		}
		vl.T = &TupleT{Elems: types}
		for i, e := range tl.Elems {
			c.assignTarget(e, types[i], nil, pos)
		}
		tl.T = vl.T
		return
	}
	vt := c.expr(*valueSlot, nil)
	if isInvalid(vt) {
		for _, e := range tl.Elems {
			if id, ok := e.(*Ident); ok && id.Name != "_" && c.targetSym(id) == nil {
				sym := c.declareLocal(id.Name, tInvalid, id.Pos, false)
				id.Sym = sym
				c.flow.assigned[sym] = true
			}
		}
		return
	}
	switch x := vt.(type) {
	case *TupleT:
		if len(x.Elems) != len(tl.Elems) {
			c.errorf(pos, "元组有 %d 个元素，但要解构为 %d 个变量", len(x.Elems), len(tl.Elems))
			return
		}
		for i, e := range tl.Elems {
			c.assignTarget(e, x.Elems[i], nil, pos)
		}
	case *ListT:
		for _, e := range tl.Elems {
			c.assignTarget(e, x.Elem, nil, pos)
		}
	default:
		c.errorf(pos, "不能把 %s 解构为多个变量", vt)
		return
	}
	tl.T = vt
}

// attrTarget 检查 obj.name 作为赋值目标，返回成员类型。
func (c *Checker) attrTarget(t *Attr, valueSlot *Expr, vt Type) Type {
	if t.Safe {
		c.errorf(t.Pos, "?. 不能用于赋值目标")
		return nil
	}
	// __init__ 中的字段推断：self.x = v
	if id, ok := t.X.(*Ident); ok && id.Name == "self" && c.fn != nil && c.fn.IsInit && c.initPhase && c.fn.Owner != nil {
		td := c.fn.Owner
		if mem, _, _ := c.findMember(&NamedT{D: td, Args: tparamTypes(td.TParams)}, t.Name); mem == nil {
			c.expr(t.X, nil)
			var ft Type
			if valueSlot != nil {
				ft = c.expr(*valueSlot, nil)
			} else {
				ft = vt
			}
			ft = c.declType(ft, t.Pos, "self."+t.Name)
			f := &Field{Name: t.Name, T: ft, Owner: td, Pos: t.NamePos, Inferred: true}
			td.Fields = append(td.Fields, f)
			td.FieldMap[t.Name] = f
			t.Kind = AttrField
			t.Field = f
			t.Owner = &NamedT{D: td, Args: tparamTypes(td.TParams)}
			t.RecvT = t.Owner
			t.T = ft
			if valueSlot != nil {
				// 值已检查；类型一致无需转换
			}
			return nil
		}
	}
	at := c.expr(t, nil)
	if isInvalid(at) {
		if valueSlot != nil {
			c.expr(*valueSlot, nil)
		}
		return nil
	}
	switch t.Kind {
	case AttrField:
		f := t.Field
		if f.Const {
			c.errorf(t.NamePos, "不能给常量 %s 赋值", t.Name)
		}
		c.noteMutation(t)
		return at
	case AttrStaticField:
		if t.Field.Const {
			c.errorf(t.NamePos, "不能给常量 %s 赋值", t.Name)
		}
		return at
	case AttrProp:
		p := t.Prop
		if !p.HasSet {
			c.errorf(t.NamePos, "属性 %s 是只读的", t.Name)
			return nil
		}
		c.noteMutation(t)
		return at
	case AttrModule:
		if t.Sym != nil && t.Sym.Kind == SVar {
			if t.Sym.Mod != c.mod {
				c.errorf(t.NamePos, "不能给其他模块的变量 %s 赋值", t.Name)
			}
			return at
		}
		c.errorf(t.NamePos, "不能给 %s 赋值", t.Name)
	case AttrTupleField:
		c.errorf(t.NamePos, "元组是不可变的")
	case AttrMethodValue:
		c.errorf(t.NamePos, "不能给方法 %s 赋值", t.Name)
	default:
		c.errorf(t.NamePos, "不能给 %s 赋值", t.Name)
	}
	return nil
}

// noteMutation 记录结构体方法中对 self 的修改。
func (c *Checker) noteMutation(t *Attr) {
	if c.fn == nil || c.fn.Owner == nil || c.fn.Owner.Kind != DStruct {
		return
	}
	// 沿着值类型字段向上，看是否到达 self
	var x Expr = t.X
	for {
		switch e := x.(type) {
		case *Ident:
			if e.Sym != nil && e.Sym.IsSelf {
				if c.fn.Method != nil {
					c.fn.Method.Mutating = true
				}
				if c.fn.Prop != nil && c.fn.Setter {
					c.fn.Prop.Mutating = true
				}
				if c.fn.Prop != nil && !c.fn.Setter {
					c.errorf(t.Pos, "属性的 get 不能修改结构体 self")
				}
			}
			return
		case *Attr:
			if e.Kind != AttrField {
				return
			}
			if d := namedDecl(e.T); d == nil || d.Kind != DStruct {
				return
			}
			x = e.X
		default:
			return
		}
	}
}

// indexTarget 检查 x[i] 作为赋值目标，返回元素类型。
func (c *Checker) indexTarget(t *Index) Type {
	xt := c.expr(t.X, nil)
	if isInvalid(xt) {
		c.expr(t.Idx, nil)
		return nil
	}
	switch x := xt.(type) {
	case *ListT:
		c.exprTo(&t.Idx, tInt, "列表下标")
		t.T = x.Elem
		return x.Elem
	case *DictT:
		c.exprTo(&t.Idx, x.Key, "字典的键")
		t.T = x.Val
		return x.Val
	case *NamedT:
		mem, _, m := c.findMember(x, "__setitem__")
		mt, ok := mem.(*Method)
		if !ok {
			c.errorf(t.Pos, "类型 %s 不支持下标赋值（需要 __setitem__）", xt)
			return nil
		}
		sig := substSig(mt.Sig, m)
		t.Ovl = mt
		c.exprTo(&t.Idx, sig.Params[0].T, "下标")
		t.T = sig.Params[1].T
		return sig.Params[1].T
	case *Basic:
		if x.Kind == BStr {
			c.errorf(t.Pos, "字符串是不可变的，不能修改其中的字符")
			return nil
		}
	case *TupleT:
		c.errorf(t.Pos, "元组是不可变的")
		return nil
	case *OptT:
		c.errorf(t.Pos, "值可能为 null，请先判断或使用 !")
		return nil
	}
	c.errorf(t.Pos, "类型 %s 不支持下标赋值", xt)
	return nil
}

func augBinOp(op TokKind) TokKind {
	switch op {
	case TPlusEq:
		return TPlus
	case TMinusEq:
		return TMinus
	case TStarEq:
		return TStar
	case TSlashEq:
		return TSlash
	case TDSlashEq:
		return TDSlash
	case TPercentEq:
		return TPercent
	case TDStarEq:
		return TDStar
	case TAmpEq:
		return TAmp
	case TPipeEq:
		return TPipe
	case TCaretEq:
		return TCaret
	case TShlEq:
		return TShl
	case TShrEq:
		return TShr
	}
	return op
}

// augAssign 检查 x op= v。
func (c *Checker) augAssign(s *AssignStmt) {
	target := s.Targets[0]
	var tt Type
	switch t := target.(type) {
	case *Ident:
		sym := c.scope.lookup(t.Name)
		if sym == nil {
			c.undefined(t.Pos, t.Name)
			c.expr(s.Value, nil)
			return
		}
		if c.globalNeedsDecl(sym) {
			d := c.errorf(t.Pos, "不能在函数中直接修改全局变量 %s", t.Name)
			d.Notes = append(d.Notes, "请在函数开头写 global "+t.Name)
			c.expr(s.Value, nil)
			return
		}
		tt = c.expr(t, nil)
		if sym.Kind != SVar {
			c.errorf(t.Pos, "不能修改 %s", t.Name)
		}
		sym.Assigns++
		if sym.Fn != nil && sym.Fn != c.fn && !sym.Global {
			sym.ClosureAssigned = true
		}
	case *Attr:
		tt = c.expr(t, nil)
		switch t.Kind {
		case AttrField, AttrStaticField:
			if t.Field != nil && t.Field.Const {
				c.errorf(t.NamePos, "不能修改常量 %s", t.Name)
			}
			c.noteMutation(t)
		case AttrProp:
			if !t.Prop.HasSet {
				c.errorf(t.NamePos, "属性 %s 是只读的", t.Name)
			}
			c.noteMutation(t)
		case AttrModule:
		default:
			c.errorf(t.NamePos, "不能修改 %s", t.Name)
		}
	case *Index:
		tt = c.indexTarget(t)
		if tt == nil {
			c.expr(s.Value, nil)
			return
		}
		// 需要同时可读
		if n, ok := t.X.Typ().(*NamedT); ok {
			if mem, _, _ := c.findMember(n, "__getitem__"); mem == nil {
				c.errorf(t.Pos, "类型 %s 不支持下标读取（需要 __getitem__）", n)
			}
		}
	default:
		c.errorf(target.P(), "不能对这个表达式使用复合赋值")
		return
	}
	if isInvalid(tt) {
		c.expr(s.Value, nil)
		return
	}
	op := augBinOp(s.Op)
	// 列表 += 可迭代对象：原地扩展
	if lt, ok := tt.(*ListT); ok && op == TPlus {
		c.exprTo(&s.Value, tt, "列表扩展")
		_ = lt
		return
	}
	// 用二元运算的规则检查 target op value
	bin := &Binary{exprBase: exprBase{Pos: s.Pos}, Op: op, X: &typedPlaceholder{exprBase{Pos: target.P(), T: tt}}, Y: s.Value}
	rt := c.binary(bin, tt)
	s.Value = bin.Y
	s.OpMethod = bin.Op2
	if isInvalid(rt) {
		return
	}
	if bin.Op2 == nil {
		if _, ok := c.assignable(rt, tt); !ok {
			c.errorf(s.Pos, "%s %s 的结果类型是 %s，不能赋回 %s 类型的目标", tt, op, rt, tt)
		} else if !identical(rt, tt) {
			// 例如 int 变量 /= 2 得到 float
			c.errorf(s.Pos, "%s %s= 的结果类型是 %s，与目标类型 %s 不一致", tt, op, rt, tt)
		}
	} else if !identical(rt, tt) {
		if _, ok := c.assignable(rt, tt); !ok {
			c.errorf(s.Pos, "运算符 %s 返回 %s，不能赋回 %s", op, rt, tt)
		}
	}
	if b, ok := bin.Y.(*ConvExpr); ok {
		s.Value = b
	}
}

// typedPlaceholder 是复合赋值中代表目标的占位表达式。
type typedPlaceholder struct{ exprBase }

func (c *Checker) varDecl(s *VarDecl) {
	switch t := s.Target.(type) {
	case *Ident:
		var typ Type
		if s.Sym != nil && s.Sym.Global {
			// 模块级注解变量：类型已在收集阶段解析
			typ = s.Sym.T
		} else {
			typ = c.resolveType(s.Type, c.scope)
			if isVoid(typ) {
				c.errorf(s.Type.P(), "变量类型不能是 void")
			}
			fs := c.funcScope()
			if old := fs.Syms[t.Name]; old != nil && old.Kind == SVar && !fs.IsModule {
				c.errorf(t.Pos, "变量 %s 已经声明过（在第 %d 行）", t.Name, old.Pos.Line)
			}
			sym := c.declareLocal(t.Name, typ, t.Pos, false)
			s.Sym = sym
			t.Sym = sym
		}
		t.T = typ
		if s.Value != nil {
			c.exprTo(&s.Value, typ, "变量初始值")
			c.flow.assigned[s.Sym] = true
			s.Sym.Assigns++
		} else if isOpt(typ) {
			// 可空类型默认为 null
			c.flow.assigned[s.Sym] = true
			s.Sym.Assigns++
		}
	case *Attr:
		// self.x: T = v（在 __init__ 中声明字段）
		id, ok := t.X.(*Ident)
		if !ok || id.Name != "self" || c.fn == nil || !c.fn.IsInit || c.fn.Owner == nil {
			c.errorf(s.Pos, "只能在 __init__ 中用 self.字段: 类型 = 值 声明字段")
			return
		}
		td := c.fn.Owner
		typ := c.resolveType(s.Type, c.scope)
		if mem, _, _ := c.findMember(&NamedT{D: td, Args: tparamTypes(td.TParams)}, t.Name); mem != nil {
			if c.initPhase {
				if f, ok := mem.(*Field); ok && !identical(f.T, typ) && !isInvalid(f.T) {
					c.errorf(s.Pos, "字段 %s 已声明为 %s", t.Name, f.T)
				}
			}
		} else if c.initPhase {
			f := &Field{Name: t.Name, T: typ, Owner: td, Pos: t.NamePos, Inferred: true}
			td.Fields = append(td.Fields, f)
			td.FieldMap[t.Name] = f
		}
		c.expr(t, nil)
		s.Field = t.Field
		if s.Value != nil {
			c.exprTo(&s.Value, typ, "字段初始值")
		}
	}
}

func (c *Checker) constDecl(s *ConstDecl) {
	if s.Sym != nil && s.Sym.Global {
		// 模块级常量已在解析阶段检查
		c.flow.assigned[s.Sym] = true
		return
	}
	var typ Type
	if s.Type != nil {
		typ = c.resolveType(s.Type, c.scope)
		c.exprTo(&s.Value, typ, "常量初始值")
	} else {
		typ = c.declType(c.expr(s.Value, nil), s.Pos, s.Name)
	}
	if fs := c.funcScope(); fs.Syms[s.Name] != nil {
		c.errorf(s.Pos, "名字 %s 已经声明过", s.Name)
	}
	sym := c.declareLocal(s.Name, typ, s.Pos, false)
	sym.Kind = SConst
	sym.GoConst = isConstExpr(s.Value)
	sym.Assigns = 1
	s.Sym = sym
	c.flow.assigned[sym] = true
}

// ===== 控制流 =====

func (c *Checker) applyNarrow(n map[*Symbol]Type) {
	for k, v := range n {
		c.flow.narrow[k] = v
	}
}

func (c *Checker) ifStmt(s *IfStmt) {
	thenN, elseN := c.cond(&s.Cond)
	entry := c.flow
	c.flow = entry.copy()
	c.applyNarrow(thenN)
	if lit, ok := s.Cond.(*BoolLit); ok && !lit.Val {
		// if false: 分支不可达，但不报警告
	}
	c.block(s.Then)
	thenFlow := c.flow
	c.flow = entry.copy()
	c.applyNarrow(elseN)
	if s.Else != nil {
		c.block(s.Else)
	}
	elseFlow := c.flow
	c.flow = mergeFlows(thenFlow, elseFlow)
}

// assignedNames 收集语句列表中被赋值的名字（用于循环与 try 中的窄化失效）。
func assignedNames(list []Stmt, out map[string]bool) {
	var target func(e Expr)
	target = func(e Expr) {
		switch t := e.(type) {
		case *Ident:
			out[t.Name] = true
		case *TupleLit:
			for _, x := range t.Elems {
				target(x)
			}
		case *ListLit:
			for _, x := range t.Elems {
				target(x)
			}
		}
	}
	for _, st := range list {
		switch s := st.(type) {
		case *AssignStmt:
			for _, t := range s.Targets {
				target(t)
			}
		case *VarDecl:
			target(s.Target)
		case *IfStmt:
			assignedNames(s.Then, out)
			assignedNames(s.Else, out)
		case *WhileStmt:
			assignedNames(s.Body, out)
		case *ForStmt:
			assignedNames(s.Body, out)
		case *TryStmt:
			assignedNames(s.Body, out)
			for _, h := range s.Handlers {
				assignedNames(h.Body, out)
			}
			assignedNames(s.Else, out)
			assignedNames(s.Finally, out)
		case *WithStmt:
			assignedNames(s.Body, out)
		case *MatchStmt:
			for _, cs := range s.Cases {
				assignedNames(cs.Body, out)
			}
		case *SelectStmt:
			for _, cs := range s.Cases {
				if cs.Target != nil {
					target(cs.Target)
				}
				assignedNames(cs.Body, out)
			}
		case *FuncDeclStmt:
			// 嵌套函数可能修改外层变量
			assignedNames(s.Fn.Body, out)
		}
	}
}

func (c *Checker) invalidate(body []Stmt) {
	if len(c.flow.narrow) == 0 {
		return
	}
	names := map[string]bool{}
	assignedNames(body, names)
	for sym := range c.flow.narrow {
		if names[sym.Name] {
			delete(c.flow.narrow, sym)
		}
	}
}

func isTrueLit(e Expr) bool {
	b, ok := e.(*BoolLit)
	return ok && b.Val
}

func (c *Checker) whileStmt(s *WhileStmt) {
	s.Loop = &LoopInfo{}
	c.invalidate(s.Body)
	infinite := isTrueLit(s.Cond)
	thenN, elseN := c.cond(&s.Cond)
	entry := c.flow.copy()
	c.applyNarrow(thenN)
	lc := &loopCtx{info: s.Loop, tryDepth: c.tryDepth}
	c.loops = append(c.loops, lc)
	c.block(s.Body)
	c.loops = c.loops[:len(c.loops)-1]
	if infinite {
		c.flow = mergeFlows(lc.breaks...)
		if len(lc.breaks) == 0 {
			c.flow.dead = true
		}
		return
	}
	after := entry.copy()
	for k, v := range elseN {
		after.narrow[k] = v
	}
	c.flow = mergeFlows(append([]*flowState{after}, lc.breaks...)...)
}

func (c *Checker) forStmt(s *ForStmt) {
	info := &LoopInfo{}
	s.Loop = info
	c.invalidate(s.Body)
	entry := c.flow.copy()
	c.pushScope()
	c.iterate(&s.Iter, s.Target, info)
	lc := &loopCtx{info: info, tryDepth: c.tryDepth}
	c.loops = append(c.loops, lc)
	c.block(s.Body)
	c.loops = c.loops[:len(c.loops)-1]
	c.popScope()
	c.flow = mergeFlows(append([]*flowState{entry}, lc.breaks...)...)
}

// bindLoopTarget 为循环变量声明符号（块作用域）。
func (c *Checker) bindLoopTarget(target Expr, t Type, info *LoopInfo) {
	switch x := target.(type) {
	case *Ident:
		x.T = t
		if x.Name == "_" {
			return
		}
		sym := c.declareLocal(x.Name, t, x.Pos, true)
		sym.LoopVar = true
		sym.Assigns = 1
		x.Sym = sym
		c.flow.assigned[sym] = true
		if info != nil {
			info.Vars = append(info.Vars, sym)
		}
	case *TupleLit:
		x.T = t
		tt, ok := t.(*TupleT)
		if !ok {
			if !isInvalid(t) {
				c.errorf(x.Pos, "不能把 %s 解构为 %d 个循环变量", t, len(x.Elems))
			}
			for _, e := range x.Elems {
				c.bindLoopTarget(e, tInvalid, info)
			}
			return
		}
		if len(tt.Elems) != len(x.Elems) {
			c.errorf(x.Pos, "元素是 %d 元组，但给了 %d 个循环变量", len(tt.Elems), len(x.Elems))
			for _, e := range x.Elems {
				c.bindLoopTarget(e, tInvalid, info)
			}
			return
		}
		for i, e := range x.Elems {
			c.bindLoopTarget(e, tt.Elems[i], info)
		}
	default:
		c.errorf(target.P(), "循环变量必须是名字")
	}
}

// iterate 检查 for 循环的可迭代对象，确定迭代方式并绑定循环变量。
func (c *Checker) iterate(iterSlot *Expr, target Expr, info *LoopInfo) {
	if call, ok := (*iterSlot).(*Call); ok {
		if id, ok := call.Fn.(*Ident); ok {
			if sym := c.scope.lookup(id.Name); sym != nil && sym.Kind == SBuiltin && sym.Builtin == "range" {
				id.Sym = sym
				et := c.rangeArgs(call)
				call.Kind = CallBuiltin
				call.Builtin = "range"
				call.T = &IterT{et}
				info.Kind = loopRange
				for _, a := range call.Args {
					info.Range = append(info.Range, a.X)
				}
				info.ElemT = et
				c.bindLoopTarget(target, et, info)
				return
			}
		}
	}
	t := c.expr(*iterSlot, nil)
	if isInvalid(t) {
		c.bindLoopTarget(target, tInvalid, info)
		return
	}
	if call, ok := (*iterSlot).(*Call); ok {
		switch {
		case call.Kind == CallBuiltin && call.Builtin == "enumerate":
			inner := call.Args[0].X
			sub := &LoopInfo{}
			et := c.elemOf(inner.Typ(), inner.P(), sub)
			info.Kind = loopEnumerate
			info.InnerKind = sub.Kind
			info.Inner = inner
			info.Method = sub.Method
			if len(call.Args) > 1 {
				info.Range = []Expr{call.Final[1]}
			}
			pair := &TupleT{Elems: []Type{tInt, et}}
			info.ElemT = pair
			c.bindLoopTarget(target, pair, info)
			return
		case call.Kind == CallBuiltin && call.Builtin == "zip" && len(call.Args) == 2:
			la, okA := call.Args[0].X.Typ().(*ListT)
			lb, okB := call.Args[1].X.Typ().(*ListT)
			if okA && okB {
				info.Kind = loopZip
				info.Inner = call.Args[0].X
				info.Inner2 = call.Args[1].X
				pair := &TupleT{Elems: []Type{la.Elem, lb.Elem}}
				info.ElemT = pair
				c.bindLoopTarget(target, pair, info)
				return
			}
		case (call.Kind == CallExt || call.Kind == CallMethod) && call.Method != nil && call.Recv != nil && len(call.Args) == 0:
			if dt, ok := call.Recv.Typ().(*DictT); ok {
				switch call.Method.Name {
				case "items":
					info.Kind = loopDictItems
					info.Inner = call.Recv
					pair := &TupleT{Elems: []Type{dt.Key, dt.Val}}
					info.ElemT = pair
					c.bindLoopTarget(target, pair, info)
					return
				case "keys":
					info.Kind = loopDict
					info.Inner = call.Recv
					info.ElemT = dt.Key
					c.bindLoopTarget(target, dt.Key, info)
					return
				case "values":
					info.Kind = loopDictValues
					info.Inner = call.Recv
					info.ElemT = dt.Val
					c.bindLoopTarget(target, dt.Val, info)
					return
				}
			}
		}
	}
	et := c.elemOf(t, (*iterSlot).P(), info)
	info.ElemT = et
	c.bindLoopTarget(target, et, info)
}

// elemOf 返回可迭代类型的元素类型；info 非空时记录迭代方式。
func (c *Checker) elemOf(t Type, pos Pos, info *LoopInfo) Type {
	if info == nil {
		info = &LoopInfo{}
	}
	switch x := t.(type) {
	case *ListT:
		info.Kind = loopList
		return x.Elem
	case *DictT:
		info.Kind = loopDict
		return x.Key
	case *SetT:
		info.Kind = loopSet
		return x.Elem
	case *IterT:
		info.Kind = loopIter
		return x.Elem
	case *ChanT:
		info.Kind = loopChan
		return x.Elem
	case *Basic:
		if x.Kind == BStr {
			info.Kind = loopStr
			return tChar
		}
	case *OptT:
		c.errorf(pos, "值可能为 null，不能直接遍历；请先判断是否为 null")
		return tInvalid
	case *NamedT:
		mem, _, m := c.findMember(x, "__iter__")
		if mt, ok := mem.(*Method); ok {
			info.Kind = loopIterMethod
			info.Method = mt
			rt := subst(mt.Sig.Ret, m)
			if it, ok := rt.(*IterT); ok {
				return it.Elem
			}
			return tInvalid
		}
	case *TupleT:
		c.errorf(pos, "不能遍历元组（元素类型可能不同）；请改用列表")
		return tInvalid
	}
	if !isInvalid(t) {
		c.errorf(pos, "类型 %s 不能被遍历", t)
	}
	return tInvalid
}

func (c *Checker) rangeArgs(call *Call) Type {
	if len(call.Args) < 1 || len(call.Args) > 3 {
		c.errorf(call.Pos, "range 需要 1 到 3 个参数")
		for _, a := range call.Args {
			c.expr(a.X, nil)
		}
		return tInt
	}
	var et Type = tInt
	// 先检查非字面量参数以确定整数类型
	for i, a := range call.Args {
		if a.Name != "" {
			c.errorf(a.Pos, "range 不接受关键字参数")
		}
		if !isUntypedLit(a.X) {
			t := c.expr(a.X, nil)
			if isInteger(t) {
				if i == 0 || !identical(t, et) {
					if numWidens(et, t) || i == 0 {
						et = t
					}
				}
			}
		}
	}
	for i := range call.Args {
		if isUntypedLit(call.Args[i].X) {
			c.exprTo(&call.Args[i].X, et, "range 参数")
		} else {
			t := call.Args[i].X.Typ()
			if !isInteger(t) && !isInvalid(t) {
				c.errorf(call.Args[i].X.P(), "range 的参数必须是整数，得到 %s", t)
			} else {
				c.convert(&call.Args[i].X, t, et, "range 参数")
			}
		}
	}
	if len(call.Args) == 3 {
		if v, ok := c.constInt(call.Args[2].X); ok && v == 0 {
			c.errorf(call.Args[2].X.P(), "range 的步长不能为 0")
		}
	}
	return et
}

func (c *Checker) returnStmt(s *ReturnStmt) {
	fn := c.fn
	defer func() { c.flow.dead = true }()
	if fn == nil || fn.IsModInit {
		c.errorf(s.Pos, "return 只能用在函数中")
		if s.Value != nil {
			c.expr(s.Value, nil)
		}
		return
	}
	if fn.IsGen {
		if s.Value != nil {
			c.errorf(s.Pos, "生成器中的 return 不能带返回值")
			c.expr(s.Value, nil)
		}
		return
	}
	if isVoid(fn.Ret) {
		if s.Value != nil {
			t := c.expr(s.Value, nil)
			if !isVoid(t) && !isInvalid(t) {
				d := c.errorf(s.Value.P(), "%s 没有声明返回类型，不能返回值", fn.Name)
				d.Notes = append(d.Notes, "在参数列表后写 -> "+t.String())
			}
		}
		return
	}
	if s.Value == nil {
		c.errorf(s.Pos, "%s 需要返回 %s 类型的值", fn.Name, fn.Ret)
		return
	}
	c.exprTo(&s.Value, fn.Ret, "返回值")
}

func (c *Checker) raiseStmt(s *RaiseStmt) {
	defer func() { c.flow.dead = true }()
	if s.X == nil {
		if c.handlerDepth() == 0 {
			c.errorf(s.Pos, "单独的 raise 只能在 except 中重新抛出当前异常")
		}
		return
	}
	t := c.expr(s.X, nil)
	if mt, ok := t.(*MetaT); ok {
		if n, ok := mt.T.(*NamedT); ok && n.D.IsExc {
			// raise ValueError  =>  raise ValueError()
			call := &Call{exprBase: exprBase{Pos: s.X.P()}, Fn: s.X}
			s.X = call
			t = c.expr(call, nil)
		}
	}
	if isInvalid(t) {
		return
	}
	n, ok := t.(*NamedT)
	if !ok || !n.D.IsExc {
		d := c.errorf(s.X.P(), "只能抛出 Exception 的子类实例，不能抛出 %s", t)
		if isStr(t) {
			d.Notes = append(d.Notes, "写成 raise ValueError(\"...\") 或 raise Exception(\"...\")")
		}
	}
	c.UsesTraceback = true
}

func (c *Checker) handlerDepth() int { return c.handlers }

func (c *Checker) tryStmt(s *TryStmt) {
	if c.fn != nil {
		c.fn.NeedFlow = true
	}
	c.invalidate(s.Body)
	entry := c.flow.copy()
	c.tryDepth++
	c.block(s.Body)
	if s.Else != nil {
		// else 在 try 主体正常结束后执行（其中的异常不被本 try 捕获）
		c.tryDepth--
		c.block(s.Else)
		c.tryDepth++
	}
	flows := []*flowState{c.flow}
	seenTypes := map[*TypeDecl]Pos{}
	for _, h := range s.Handlers {
		c.flow = entry.copy()
		c.pushScope()
		var ht Type
		if len(h.Types) == 0 {
			if c.excTD != nil {
				ht = &NamedT{D: c.excTD}
			}
		}
		for _, tx := range h.Types {
			t := c.resolveType(tx, c.scope)
			if isInvalid(t) {
				continue
			}
			n, ok := t.(*NamedT)
			if !ok || !n.D.IsExc {
				c.errorf(tx.P(), "except 只能捕获 Exception 的子类，%s 不是异常类型", t)
				continue
			}
			if len(n.Args) > 0 {
				c.errorf(tx.P(), "不能捕获泛型异常类型")
			}
			for d, p := range seenTypes {
				if isSubclassDecl(n.D, d) {
					c.warnf(tx.P(), "%s 已经被前面第 %d 行的 except %s 捕获，这里永远不会执行", n.D.Name, p.Line, d.Name)
				}
			}
			h.TDs = append(h.TDs, n)
			if ht == nil {
				ht = n
			} else {
				ht = c.join(ht, n)
				if ht == nil {
					ht = &NamedT{D: c.excTD}
				}
			}
		}
		for _, t := range h.TDs {
			seenTypes[t.(*NamedT).D] = h.Pos
		}
		if h.Name != "" && ht != nil {
			sym := c.declareLocal(h.Name, ht, h.Pos, true)
			sym.Assigns = 1
			h.Sym = sym
			c.flow.assigned[sym] = true
		}
		c.handlers++
		c.stmts(h.Body)
		c.handlers--
		c.popScope()
		flows = append(flows, c.flow)
	}
	c.tryDepth--
	after := mergeFlows(flows...)
	if s.Finally != nil {
		c.flow = entry.copy()
		c.tryDepth++
		c.loopsSave(func() {
			c.block(s.Finally)
		})
		c.tryDepth--
		if c.flow.dead {
			after.dead = true
		}
		c.checkNoJumps(s.Finally, "finally")
	}
	c.flow = after
}

// loopsSave 在不允许 break/continue 跳出的上下文中执行 fn。
func (c *Checker) loopsSave(fn func()) {
	save := c.loops
	c.loops = nil
	fn()
	c.loops = save
}

func (c *Checker) checkNoJumps(list []Stmt, where string) {
	for _, st := range list {
		switch s := st.(type) {
		case *ReturnStmt:
			c.errorf(s.Pos, "%s 中不能使用 return", where)
		case *IfStmt:
			c.checkNoJumps(s.Then, where)
			c.checkNoJumps(s.Else, where)
		case *TryStmt:
			c.checkNoJumps(s.Body, where)
			for _, h := range s.Handlers {
				c.checkNoJumps(h.Body, where)
			}
		case *WithStmt:
			c.checkNoJumps(s.Body, where)
		case *MatchStmt:
			for _, cs := range s.Cases {
				c.checkNoJumps(cs.Body, where)
			}
		}
	}
}

func (c *Checker) withStmt(s *WithStmt) {
	if c.fn != nil {
		c.fn.NeedFlow = true
	}
	c.pushScope()
	for _, item := range s.Items {
		t := c.expr(item.X, nil)
		if isInvalid(t) {
			if item.Name != "" {
				c.declareLocal(item.Name, tInvalid, item.Pos, true)
			}
			continue
		}
		item.EnterT = t
		nt, ok := t.(*NamedT)
		if !ok {
			c.errorf(item.X.P(), "with 需要一个实现了 __enter__/__exit__ 或 close() 的对象，而 %s 不是", t)
			continue
		}
		enter, _, m1 := c.findMember(nt, "__enter__")
		exit, _, _ := c.findMember(nt, "__exit__")
		em, ok1 := enter.(*Method)
		xm, ok2 := exit.(*Method)
		if ok1 && ok2 {
			item.Enter = em
			item.Exit = xm
			if rt := subst(em.Sig.Ret, m1); !isVoid(rt) {
				item.EnterT = rt
			}
		} else if cl, _, _ := c.findMember(nt, "close"); cl != nil {
			if cm, ok := cl.(*Method); ok && len(cm.Sig.Params) == 0 {
				item.Exit = cm
			} else {
				c.errorf(item.X.P(), "%s 的 close 方法不能有参数", t)
			}
		} else {
			c.errorf(item.X.P(), "类型 %s 没有 __enter__/__exit__ 或 close()，不能用于 with", t)
		}
		if item.Name != "" {
			sym := c.declareLocal(item.Name, item.EnterT, item.Pos, true)
			sym.Assigns = 1
			item.Sym = sym
			c.flow.assigned[sym] = true
		}
	}
	c.tryDepth++
	c.stmts(s.Body)
	c.tryDepth--
	c.popScope()
}

func (c *Checker) deferStmt(s *DeferStmt) {
	if c.fn == nil || c.fn.IsModInit {
		c.errorf(s.Pos, "defer 只能用在函数中")
	} else if c.tryDepth > 0 {
		c.errorf(s.Pos, "defer 不能写在 try/with 内部；请使用 finally")
	} else if c.fn.IsGen {
		c.errorf(s.Pos, "生成器中不能使用 defer")
	}
	if c.fn != nil {
		c.fn.HasDefer = true
	}
	if _, ok := s.X.(*Call); !ok {
		c.errorf(s.X.P(), "defer 后面需要一个函数调用")
	}
	c.expr(s.X, nil)
}

func (c *Checker) goStmt(s *GoStmt) {
	if _, ok := s.X.(*Call); !ok {
		c.errorf(s.X.P(), "go 后面需要一个函数调用，例如 go worker(1)")
	}
	c.expr(s.X, nil)
}

func (c *Checker) delStmt(s *DelStmt) {
	for _, t := range s.Targets {
		idx, ok := t.(*Index)
		if !ok {
			c.errorf(t.P(), "del 只能删除列表元素或字典的键，例如 del d[key]")
			continue
		}
		xt := c.expr(idx.X, nil)
		switch x := xt.(type) {
		case *ListT:
			c.exprTo(&idx.Idx, tInt, "列表下标")
			idx.T = x.Elem
		case *DictT:
			c.exprTo(&idx.Idx, x.Key, "字典的键")
			idx.T = x.Val
		default:
			if !isInvalid(xt) {
				c.errorf(t.P(), "del 不能用于 %s", xt)
			}
			c.expr(idx.Idx, nil)
		}
	}
}

func (c *Checker) selectStmt(s *SelectStmt) {
	entry := c.flow.copy()
	var flows []*flowState
	defaults := 0
	for _, sc := range s.Cases {
		c.flow = entry.copy()
		c.pushScope()
		switch sc.Kind {
		case selDefault:
			defaults++
			if defaults > 1 {
				c.errorf(sc.Pos, "select 只能有一个 default")
			}
		case selRecv, selSend:
			ct := c.expr(sc.Chan, nil)
			ch, ok := ct.(*ChanT)
			if !ok {
				if !isInvalid(ct) {
					c.errorf(sc.Chan.P(), "select 的 case 需要通道，得到 %s", ct)
				}
				ch = &ChanT{tInvalid}
			}
			if sc.Kind == selSend {
				c.exprTo(&sc.Value, ch.Elem, "发送到通道的值")
			} else if sc.Target != nil {
				id, ok := sc.Target.(*Ident)
				if !ok {
					c.errorf(sc.Target.P(), "接收结果只能绑定到变量")
				} else if id.Name != "_" {
					sym := c.targetSym(id)
					if sym == nil {
						sym = c.declareLocal(id.Name, ch.Elem, id.Pos, true)
						sc.Decl = true
					} else if sym.Kind == SVar {
						if _, ok := c.assignable(ch.Elem, sym.T); !ok {
							c.errorf(id.Pos, "不能把 %s 赋给 %s 类型的变量 %s", ch.Elem, sym.T, id.Name)
						}
						delete(c.flow.narrow, sym)
					}
					sym.Assigns++
					id.Sym = sym
					id.T = ch.Elem
					sc.Sym = sym
					c.flow.assigned[sym] = true
				}
			}
		}
		c.stmts(sc.Body)
		c.popScope()
		flows = append(flows, c.flow)
	}
	if len(s.Cases) == 0 {
		c.flow = entry
		c.flow.dead = true
		return
	}
	c.flow = mergeFlows(flows...)
}

func (c *Checker) yieldStmt(s *YieldStmt) {
	fn := c.fn
	if fn == nil || !fn.IsGen {
		c.errorf(s.Pos, "yield 只能用在生成器函数中（返回类型为 iter[T] 的 def）")
		c.expr(s.Value, nil)
		return
	}
	if s.From {
		t := c.expr(s.Value, nil)
		if isInvalid(t) {
			return
		}
		info := &LoopInfo{}
		et := c.elemOf(t, s.Value.P(), info)
		if !isInvalid(et) && !isInvalid(fn.YieldT) {
			if _, ok := c.assignable(et, fn.YieldT); !ok {
				c.errorf(s.Value.P(), "yield from 的元素类型 %s 与生成器的元素类型 %s 不匹配", et, fn.YieldT)
			}
		}
		info.ElemT = et
		s.Loop = info
		return
	}
	c.exprTo(&s.Value, fn.YieldT, "yield 的值")
}

// containsYield 判断函数体中是否有 yield（不进入嵌套函数）。
func containsYield(list []Stmt) bool {
	for _, st := range list {
		switch s := st.(type) {
		case *YieldStmt:
			return true
		case *IfStmt:
			if containsYield(s.Then) || containsYield(s.Else) {
				return true
			}
		case *WhileStmt:
			if containsYield(s.Body) {
				return true
			}
		case *ForStmt:
			if containsYield(s.Body) {
				return true
			}
		case *TryStmt:
			if containsYield(s.Body) || containsYield(s.Else) || containsYield(s.Finally) {
				return true
			}
			for _, h := range s.Handlers {
				if containsYield(h.Body) {
					return true
				}
			}
		case *WithStmt:
			if containsYield(s.Body) {
				return true
			}
		case *MatchStmt:
			for _, cs := range s.Cases {
				if containsYield(cs.Body) {
					return true
				}
			}
		case *SelectStmt:
			for _, cs := range s.Cases {
				if containsYield(cs.Body) {
					return true
				}
			}
		}
	}
	return false
}

// ===== 函数体 =====

func (c *Checker) checkFuncDecl(fd *FuncDecl, owner *TypeDecl) {
	if !fd.HasBody {
		return
	}
	c.checkBody(fd, fd.Sig, owner, nil, nil)
}

func (c *Checker) checkMethodBody(td *TypeDecl, m *Method) {
	fd := m.Decl
	if fd == nil || !fd.HasBody || fd.Fn != nil {
		return
	}
	c.mod = td.Mod
	c.checkBody(fd, m.Sig, td, m, nil)
}

func (c *Checker) checkTypeBodies(td *TypeDecl) {
	c.mod = td.Mod
	for _, m := range td.MethodList {
		c.checkMethodBody(td, m)
	}
	for _, p := range td.PropList {
		c.checkPropBodies(td, nil, p, td.TScope)
	}
}

func (c *Checker) checkExtBodies(x *ExtBlock) {
	c.mod = x.Mod
	for _, fd := range x.Decl.Methods {
		if fd.HasBody && fd.Method != nil {
			c.checkBody(fd, fd.Sig, nil, fd.Method, x)
		}
	}
	for _, p := range x.Props {
		c.checkPropBodies(nil, x, p, x.Scope)
	}
}

func (c *Checker) selfType(owner *TypeDecl, ext *ExtBlock) Type {
	if ext != nil {
		return ext.Target
	}
	return &NamedT{D: owner, Args: tparamTypes(owner.TParams)}
}

func (c *Checker) checkPropBodies(td *TypeDecl, x *ExtBlock, p *Prop, sc *Scope) {
	pd := p.Decl
	if pd == nil || p.Abstract || pd.GoExtern != "" {
		return
	}
	if pd.HasGet {
		fn := &FuncCtx{Name: "属性 " + p.Name, Mod: c.mod, Ret: p.T, Owner: td, Ext: x, Prop: p, Body: pd.Getter}
		pd.GetFn = fn
		c.withFunc(fn, sc, func() {
			c.declareSelf(fn, td, x, pd.Static)
			c.stmts(pd.Getter)
			if !c.flow.dead {
				c.errorf(pd.Pos, "属性 %s 的 get 缺少 return", p.Name)
			}
		})
	}
	if pd.HasSet {
		fn := &FuncCtx{Name: "属性 " + p.Name + " 的 set", Mod: c.mod, Ret: tVoid, Owner: td, Ext: x, Prop: p, Setter: true, Body: pd.Setter}
		pd.SetFn = fn
		c.withFunc(fn, sc, func() {
			c.declareSelf(fn, td, x, pd.Static)
			sym := &Symbol{Kind: SVar, Name: pd.SetParam, T: p.T, Pos: pd.Pos, Fn: fn, Param: true, Assigns: 1}
			c.scope.insert(sym)
			fn.Params = append(fn.Params, sym)
			c.flow.assigned[sym] = true
			c.stmts(pd.Setter)
		})
	}
}

func (c *Checker) declareSelf(fn *FuncCtx, td *TypeDecl, x *ExtBlock, static bool) {
	if static || (td == nil && x == nil) {
		return
	}
	st := c.selfType(td, x)
	sym := &Symbol{Kind: SVar, Name: "self", T: st, Fn: fn, Param: true, IsSelf: true, Assigns: 1}
	c.scope.insert(sym)
	fn.Self = sym
	c.flow.assigned[sym] = true
}

// checkBody 检查函数或方法的函数体。
func (c *Checker) checkBody(fd *FuncDecl, sig *FuncSig, owner *TypeDecl, m *Method, ext *ExtBlock) *FuncCtx {
	name := fd.Name
	if owner != nil {
		name = owner.Name + "." + fd.Name
	}
	fn := &FuncCtx{Name: name, Mod: c.mod, Sig: sig, Ret: sig.Ret, IsAsync: sig.Async, Decl: fd, Owner: owner, Method: m, Ext: ext, TParams: sig.TParams, Body: fd.Body}
	fn.IsInit = fd.Name == "__init__" && owner != nil
	fd.Fn = fn
	if containsYield(fd.Body) {
		fn.IsGen = true
		sig.Gen = true
		it, ok := sig.Ret.(*IterT)
		if !ok {
			d := c.errorf(fd.NamePos, "含有 yield 的函数是生成器，返回类型必须是 iter[T]")
			if isVoid(sig.Ret) {
				d.Notes = append(d.Notes, "在参数列表后写 -> iter[int] 之类的类型")
			}
			fn.YieldT = tInvalid
		} else {
			fn.YieldT = it.Elem
		}
		if sig.Async {
			c.errorf(fd.NamePos, "生成器不能是 async")
		}
	}
	parent := sig.Scope
	if parent == nil {
		parent = c.mod.Scope
	}
	c.withFunc(fn, parent, func() {
		if owner != nil || ext != nil {
			c.declareSelf(fn, owner, ext, fd.Static)
		}
		for i, p := range sig.Params {
			pt := p.T
			if p.Variadic {
				pt = &ListT{p.T}
			}
			var pos Pos
			var prm *Param
			idx := i
			if len(fd.Params) > 0 && fd.Params[0].Name == "self" && fd.Params[0].Type == nil {
				idx++
			}
			if idx < len(fd.Params) {
				prm = fd.Params[idx]
				pos = prm.Pos
			}
			sym := &Symbol{Kind: SVar, Name: p.Name, T: pt, Pos: pos, Fn: fn, Param: true, Assigns: 1}
			if prm != nil {
				prm.Sym = sym
			}
			c.scope.insert(sym)
			fn.Params = append(fn.Params, sym)
			c.flow.assigned[sym] = true
		}
		c.stmts(fd.Body)
		if !isVoid(fn.Ret) && !fn.IsGen && !c.flow.dead && !isInvalid(fn.Ret) {
			c.errorf(fd.NamePos, "函数 %s 缺少 return：并非所有执行路径都返回了 %s", fd.Name, fn.Ret)
		}
	})
	return fn
}

func (c *Checker) nestedFunc(s *FuncDeclStmt) {
	fd := s.Fn
	if len(fd.TypeParams) > 0 {
		c.errorf(fd.NamePos, "嵌套函数不能是泛型的")
	}
	sig := c.resolveSig(fd, c.scope, nil)
	fd.Sig = sig
	if !fd.HasBody {
		c.errorf(fd.NamePos, "函数 %s 缺少函数体", fd.Name)
		return
	}
	if fs := c.funcScope(); fs.Syms[fd.Name] != nil && !fs.IsModule {
		c.errorf(fd.NamePos, "名字 %s 已经声明过", fd.Name)
	}
	ft := sig.FuncType()
	if sig.Async && c.taskTD != nil {
		ft.Ret = &NamedT{D: c.taskTD, Args: []Type{taskArg(sig.Ret)}}
	}
	sym := c.declareLocal(fd.Name, ft, fd.NamePos, false)
	sym.LocalFunc = true
	sym.Func = fd
	sym.Sig = sig
	sym.Assigns = 1
	fd.Sym = sym
	c.flow.assigned[sym] = true
	if c.fn != nil {
		c.fn.LocalFuncs = append(c.fn.LocalFuncs, fd)
	}
	c.checkBody(fd, sig, nil, nil, nil)
}

func taskArg(t Type) Type {
	if isVoid(t) {
		return tVoid
	}
	return t
}

// ===== 结构体可变方法分析 =====

func (c *Checker) analyzeMutating() {
	// 传递闭包：调用了 self 上可变方法的方法也是可变的
	changed := true
	for changed {
		changed = false
		for _, fn := range c.AllFuncs {
			if fn.Owner == nil || fn.Owner.Kind != DStruct || fn.Method == nil || fn.Method.Mutating {
				continue
			}
			if callsMutatingOnSelf(fn.Body) {
				fn.Method.Mutating = true
				changed = true
			}
		}
	}
}

func callsMutatingOnSelf(list []Stmt) bool {
	found := false
	var visitExpr func(e Expr)
	visitExpr = func(e Expr) {
		if found || e == nil {
			return
		}
		if call, ok := e.(*Call); ok {
			if call.Kind == CallMethod && call.Method != nil && call.Method.Mutating {
				if id, ok := call.Recv.(*Ident); ok && id.Sym != nil && id.Sym.IsSelf {
					found = true
					return
				}
			}
			for _, a := range call.Final {
				visitExpr(a)
			}
			visitExpr(call.Recv)
		}
	}
	walkStmts(list, func(s Stmt) {
		switch x := s.(type) {
		case *ExprStmt:
			visitExpr(x.X)
		case *AssignStmt:
			visitExpr(x.Value)
		}
	})
	return found
}

// walkStmts 遍历语句（不进入嵌套函数）。
func walkStmts(list []Stmt, fn func(Stmt)) {
	for _, st := range list {
		fn(st)
		switch s := st.(type) {
		case *IfStmt:
			walkStmts(s.Then, fn)
			walkStmts(s.Else, fn)
		case *WhileStmt:
			walkStmts(s.Body, fn)
		case *ForStmt:
			walkStmts(s.Body, fn)
		case *TryStmt:
			walkStmts(s.Body, fn)
			for _, h := range s.Handlers {
				walkStmts(h.Body, fn)
			}
			walkStmts(s.Else, fn)
			walkStmts(s.Finally, fn)
		case *WithStmt:
			walkStmts(s.Body, fn)
		case *MatchStmt:
			for _, cs := range s.Cases {
				walkStmts(cs.Body, fn)
			}
		case *SelectStmt:
			for _, cs := range s.Cases {
				walkStmts(cs.Body, fn)
			}
		}
	}
}

// checkStructCalls 检查对结构体可变方法的调用是否作用在可寻址的值上。
func (c *Checker) checkStructCalls() {
	for _, sc := range c.structCalls {
		if !sc.method.Mutating {
			continue
		}
		if !addressable(sc.recv) {
			c.errorf(sc.pos, "方法 %s 会修改结构体，但这里的接收者是临时值（函数返回值、字典元素等），修改不会生效；请先赋给变量", sc.method.Name)
		}
	}
}

func addressable(e Expr) bool {
	switch x := e.(type) {
	case *Ident:
		return x.Sym != nil && x.Sym.Kind == SVar && x.Narrow == NarrowNone
	case *Attr:
		if x.Kind != AttrField && x.Kind != AttrStaticField {
			return false
		}
		if x.Kind == AttrStaticField {
			return true
		}
		if d := namedDecl(x.X.Typ()); d != nil && d.Kind == DClass {
			return true
		}
		return addressable(x.X)
	case *Index:
		_, ok := x.X.Typ().(*ListT)
		return ok
	}
	return false
}

func init() {
	_ = strings.TrimSpace
}
