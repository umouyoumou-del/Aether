package compiler

// walkExpr 先序遍历表达式树；fn 返回 false 时不进入该节点的子表达式。
// 会进入 lambda 的主体（需要时由 fn 剪枝）。
func walkExpr(e Expr, fn func(Expr) bool) {
	if e == nil || !fn(e) {
		return
	}
	walkList := func(xs []Expr) {
		for _, x := range xs {
			walkExpr(x, fn)
		}
	}
	switch x := e.(type) {
	case *FString:
		for _, p := range x.Parts {
			if p.IsExpr {
				walkExpr(p.X, fn)
			}
		}
	case *ListLit:
		walkList(x.Elems)
	case *SetLit:
		walkList(x.Elems)
	case *TupleLit:
		walkList(x.Elems)
	case *DictLit:
		walkList(x.Keys)
		walkList(x.Vals)
	case *Comp:
		for _, cl := range x.Clauses {
			walkExpr(cl.Iter, fn)
			walkExpr(cl.Cond, fn)
		}
		walkExpr(x.Elem, fn)
		walkExpr(x.Key, fn)
		walkExpr(x.Val, fn)
	case *Unary:
		walkExpr(x.X, fn)
	case *Binary:
		walkExpr(x.X, fn)
		walkExpr(x.Y, fn)
	case *Compare:
		walkList(x.Xs)
	case *Logical:
		walkExpr(x.X, fn)
		walkExpr(x.Y, fn)
	case *Ternary:
		walkExpr(x.Cond, fn)
		walkExpr(x.Then, fn)
		walkExpr(x.Else, fn)
	case *NullCo:
		walkExpr(x.X, fn)
		walkExpr(x.Y, fn)
	case *Lambda:
		walkExpr(x.Body, fn)
	case *Call:
		walkExpr(x.Fn, fn)
		for _, a := range x.Args {
			walkExpr(a.X, fn)
		}
	case *Index:
		walkExpr(x.X, fn)
		walkExpr(x.Idx, fn)
	case *TypeApp:
		walkExpr(x.X, fn)
	case *SliceExpr:
		walkExpr(x.X, fn)
		walkExpr(x.Lo, fn)
		walkExpr(x.Hi, fn)
		walkExpr(x.Step, fn)
	case *Attr:
		walkExpr(x.X, fn)
	case *Unwrap:
		walkExpr(x.X, fn)
	case *Await:
		walkExpr(x.X, fn)
	case *Cast:
		walkExpr(x.X, fn)
	case *IsExpr:
		walkExpr(x.X, fn)
	case *ConvExpr:
		walkExpr(x.X, fn)
	case *NamedExpr:
		walkExpr(x.Target, fn)
		walkExpr(x.Value, fn)
	}
}

// stmtExprs 返回语句自身直接包含的表达式（不含子语句块中的表达式）。
func stmtExprs(st Stmt) []Expr {
	switch s := st.(type) {
	case *ExprStmt:
		return []Expr{s.X}
	case *AssignStmt:
		return append(append([]Expr(nil), s.Targets...), s.Value)
	case *VarDecl:
		return []Expr{s.Target, s.Value}
	case *ConstDecl:
		return []Expr{s.Value}
	case *IfStmt:
		return []Expr{s.Cond}
	case *WhileStmt:
		return []Expr{s.Cond}
	case *ForStmt:
		return []Expr{s.Iter}
	case *ReturnStmt:
		return []Expr{s.Value}
	case *RaiseStmt:
		return []Expr{s.X}
	case *WithStmt:
		var out []Expr
		for _, it := range s.Items {
			out = append(out, it.X)
		}
		return out
	case *DeferStmt:
		return []Expr{s.X}
	case *GoStmt:
		return []Expr{s.X}
	case *AssertStmt:
		return []Expr{s.Cond, s.Msg}
	case *DelStmt:
		return s.Targets
	case *MatchStmt:
		out := []Expr{s.Subject}
		for _, cs := range s.Cases {
			out = append(out, cs.Guard)
		}
		return out
	case *SelectStmt:
		var out []Expr
		for _, cs := range s.Cases {
			out = append(out, cs.Chan, cs.Value)
		}
		return out
	case *YieldStmt:
		return []Expr{s.Value}
	}
	return nil
}

// walkStmtExprs 遍历语句列表（含子语句块，不进入嵌套函数）中的所有表达式。
func walkStmtExprs(list []Stmt, fn func(Expr) bool) {
	walkStmts(list, func(s Stmt) {
		for _, e := range stmtExprs(s) {
			walkExpr(e, fn)
		}
	})
}
