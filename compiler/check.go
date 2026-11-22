package compiler

import (
	"sort"
	"strings"
)

type SymKind int

const (
	SVar SymKind = iota
	SConst
	SFunc
	SType
	SModule
	SBuiltin
	STParam
)

type Symbol struct {
	Kind      SymKind
	Name      string
	GoName    string
	T         Type
	Pos       Pos
	Mod       *Module
	Global    bool
	Param     bool
	IsSelf    bool
	Fn        *FuncCtx
	Func      *FuncDecl
	Sig       *FuncSig
	TD        *TypeDecl
	Alias     Type // 内置类型（int、str……）
	Builtin   string
	TP        *TypeParam
	ModRef    *Module
	ConstVal  any
	Assigns   int // 赋值次数（含声明）
	Captured  bool
	ClosureAssigned bool
	LoopVar   bool
	LocalFunc bool
	Pending   bool // 全局变量：类型尚未确定
	Universe  bool
	Field     *Field // 静态字段
	DeclStmt  Stmt
	Used      bool
	GoConst   bool
}

type Scope struct {
	Parent   *Scope
	Syms     map[string]*Symbol
	Fn       *FuncCtx
	IsModule bool
	Mod      *Module
	Universe bool
}

func newScope(parent *Scope, fn *FuncCtx) *Scope {
	return &Scope{Parent: parent, Syms: map[string]*Symbol{}, Fn: fn}
}

func (s *Scope) lookup(name string) *Symbol {
	for sc := s; sc != nil; sc = sc.Parent {
		if sym, ok := sc.Syms[name]; ok {
			return sym
		}
	}
	return nil
}

func (s *Scope) lookupWithScope(name string) (*Symbol, *Scope) {
	for sc := s; sc != nil; sc = sc.Parent {
		if sym, ok := sc.Syms[name]; ok {
			return sym, sc
		}
	}
	return nil, nil
}

func (s *Scope) insert(sym *Symbol) { s.Syms[sym.Name] = sym }

// Module 是一个五行模块（一个 .wx 文件）。
type Module struct {
	Name      string
	Path      string
	File      *File
	Src       *SourceFile
	Scope     *Scope
	IsMain    bool
	IsPrelude bool
	IsStd     bool
	Imports   []*Module
	Funcs     []*FuncDecl
	Types     []*TypeDecl
	Exts      []*ExtBlock
	Globals   []*Symbol
	Consts    []*ConstDecl
	TopStmts  []Stmt
	Tests     []*TestDecl
	InitFn    *FuncCtx
	MainFn    *Symbol
	pendingGlobals map[string]bool
	deferredFrom   []*ImportStmt
	GoPrefix  string
	Index     int
}

// FuncCtx 是正在检查的函数（或 lambda、模块初始化代码、属性访问器）的上下文。
type FuncCtx struct {
	Name      string
	Parent    *FuncCtx
	Mod       *Module
	Sig       *FuncSig
	Ret       Type
	IsGen     bool
	YieldT    Type
	IsAsync   bool
	IsLambda  bool
	IsInit    bool
	IsModInit bool
	IsTest    bool
	Owner     *TypeDecl
	Method    *Method
	Self      *Symbol
	Locals    []*Symbol
	Params    []*Symbol
	Scope     *Scope
	NeedFlow  bool
	Decl      *FuncDecl
	Lambda    *Lambda
	TParams   []*TypeParam
	Prop      *Prop
	Setter    bool
	Ext       *ExtBlock
	Body      []Stmt
	HasDefer  bool
	narrowedCaptures []*Symbol
	LocalFuncs []*FuncDecl
	Test      *TestDecl
	globals     map[string]bool // global 声明
	globalReads map[string]Pos  // 作为全局变量读取过的名字
}

// ===== 流分析状态 =====

type flowState struct {
	assigned map[*Symbol]bool
	narrow   map[*Symbol]Type
	dead     bool
}

func newFlow() *flowState {
	return &flowState{assigned: map[*Symbol]bool{}, narrow: map[*Symbol]Type{}}
}

func (f *flowState) copy() *flowState {
	n := &flowState{assigned: make(map[*Symbol]bool, len(f.assigned)), narrow: make(map[*Symbol]Type, len(f.narrow)), dead: f.dead}
	for k, v := range f.assigned {
		n.assigned[k] = v
	}
	for k, v := range f.narrow {
		n.narrow[k] = v
	}
	return n
}

func mergeFlows(fs ...*flowState) *flowState {
	var live []*flowState
	for _, f := range fs {
		if f != nil && !f.dead {
			live = append(live, f)
		}
	}
	if len(live) == 0 {
		r := newFlow()
		r.dead = true
		if len(fs) > 0 && fs[0] != nil {
			r = fs[0].copy()
			r.dead = true
		}
		return r
	}
	r := live[0].copy()
	for _, f := range live[1:] {
		for k := range r.assigned {
			if !f.assigned[k] {
				delete(r.assigned, k)
			}
		}
		for k, v := range r.narrow {
			if fv, ok := f.narrow[k]; !ok || !identical(fv, v) {
				delete(r.narrow, k)
			}
		}
	}
	return r
}

type loopCtx struct {
	breaks   []*flowState
	info     *LoopInfo
	tryDepth int
}

// ===== 检查器 =====

type Checker struct {
	diag      *Diagnostics
	Mods      []*Module
	Prelude   *Module
	Main      *Module
	universe  *Scope
	exts      map[string][]*ExtBlock
	Types     []*TypeDecl
	TestMode  bool

	fn       *FuncCtx
	scope    *Scope
	mod      *Module
	flow     *flowState
	loops    []*loopCtx
	tryDepth int
	handlers int

	excTD   *TypeDecl
	taskTD  *TypeDecl
	implCache map[string]bool
	initPhase bool
	structCalls []structCall
	AllFuncs  []*FuncCtx
	Lambdas   []*Lambda
	constBusy map[*ConstDecl]bool
	UsesTraceback bool
}

type structCall struct {
	recv   Expr
	method *Method
	pos    Pos
}

func (c *Checker) errorf(pos Pos, format string, args ...any) *Diag {
	return c.diag.Errorf(pos, format, args...)
}

func (c *Checker) warnf(pos Pos, format string, args ...any) *Diag {
	return c.diag.Warnf(pos, format, args...)
}

// Check 对按依赖顺序排列的模块（prelude 在最前，主模块在最后）做类型检查。
func Check(mods []*Module, diag *Diagnostics, testMode bool) (c *Checker) {
	c = &Checker{diag: diag, Mods: mods, exts: map[string][]*ExtBlock{}, implCache: map[string]bool{}, TestMode: testMode, constBusy: map[*ConstDecl]bool{}}
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(tooManyErrors); ok {
				return
			}
			if diag.HasErrors() {
				// 已有错误时，后续阶段的崩溃通常是连锁反应，忽略即可
				return
			}
			panic(r)
		}
	}()
	c.setupUniverse()
	for i, m := range mods {
		m.Index = i
		if m.IsPrelude {
			c.Prelude = m
		}
		if m.IsMain {
			c.Main = m
		}
	}
	for _, m := range mods {
		c.collect(m)
		if m.IsPrelude {
			// prelude 的公开名字进入全局作用域
			for name, sym := range m.Scope.Syms {
				if !strings.HasPrefix(name, "_") {
					c.universe.Syms[name] = sym
				}
			}
			m.Scope.Parent = c.universe
		}
	}
	if c.Prelude != nil {
		if s := c.Prelude.Scope.Syms["Exception"]; s != nil {
			c.excTD = s.TD
		}
		if s := c.Prelude.Scope.Syms["Task"]; s != nil {
			c.taskTD = s.TD
		}
	}
	c.resolveAll()
	c.checkFieldDefaults()
	// __init__ 方法体（字段推断）
	c.initPhase = true
	for _, td := range c.sortedClasses() {
		if td.Init != nil && td.Init.Decl != nil && td.Init.Decl.HasBody {
			c.checkMethodBody(td, td.Init)
		}
	}
	c.initPhase = false
	for _, td := range c.Types {
		c.finishCtor(td)
	}
	// 模块顶层语句
	for _, m := range mods {
		c.checkModuleInit(m)
	}
	// 其余函数体
	for _, m := range mods {
		c.mod = m
		for _, fd := range m.Funcs {
			c.checkFuncDecl(fd, nil)
		}
		for _, td := range m.Types {
			c.checkTypeBodies(td)
		}
		for _, x := range m.Exts {
			c.checkExtBodies(x)
		}
		for _, t := range m.Tests {
			c.checkTest(t)
		}
	}
	c.analyzeMutating()
	c.checkStructCalls()
	return c
}

// sortedClasses 返回所有类型声明，基类在前。
func (c *Checker) sortedClasses() []*TypeDecl {
	var out []*TypeDecl
	seen := map[*TypeDecl]bool{}
	var visit func(td *TypeDecl)
	visit = func(td *TypeDecl) {
		if seen[td] {
			return
		}
		seen[td] = true
		if td.Base != nil {
			visit(td.Base.D)
		}
		out = append(out, td)
	}
	for _, td := range c.Types {
		visit(td)
	}
	return out
}

var builtinFuncs = []string{
	"print", "len", "range", "abs", "min", "max", "sum", "sorted", "reversed",
	"enumerate", "zip", "all", "input", "repr", "ord", "chr", "round", "open",
	"hex", "bin", "oct", "divmod", "hash", "isinstance", "type_name", "exit", "panic",
}

func (c *Checker) setupUniverse() {
	u := &Scope{Syms: map[string]*Symbol{}, Universe: true}
	c.universe = u
	for name, b := range basicByName {
		u.insert(&Symbol{Kind: SType, Name: name, Alias: b, Universe: true})
	}
	for _, name := range []string{"list", "dict", "set", "chan", "iter"} {
		u.insert(&Symbol{Kind: SType, Name: name, Builtin: name, Universe: true})
	}
	for _, name := range builtinFuncs {
		u.insert(&Symbol{Kind: SBuiltin, Name: name, Builtin: name, Universe: true})
	}
	// any 既是类型也是 any(iterable) 函数
	u.Syms["any"].Builtin = "any"
}

// ===== 收集阶段 =====

func (c *Checker) collect(m *Module) {
	c.mod = m
	m.Scope = &Scope{Parent: c.universe, Syms: map[string]*Symbol{}, IsModule: true, Mod: m}
	m.InitFn = &FuncCtx{Name: "<模块>", Mod: m, IsModInit: true, Ret: tVoid}
	m.InitFn.Scope = m.Scope
	m.pendingGlobals = map[string]bool{}
	declare := func(sym *Symbol, pos Pos) bool {
		if old := m.Scope.Syms[sym.Name]; old != nil {
			c.errorf(pos, "名字 '%s' 重复定义（之前定义于 %s）", sym.Name, old.Pos)
			return false
		}
		sym.Pos = pos
		sym.Mod = m
		sym.Global = true
		m.Scope.insert(sym)
		return true
	}
	for _, st := range m.File.Stmts {
		switch s := st.(type) {
		case *FuncDecl:
			sym := &Symbol{Kind: SFunc, Name: s.Name, Func: s}
			s.Sym = sym
			declare(sym, s.NamePos)
			m.Funcs = append(m.Funcs, s)
			if s.Name == "main" && m.IsMain {
				m.MainFn = sym
			}
		case *ClassDecl:
			td := &TypeDecl{Name: s.Name, Mod: m, Pos: s.Pos, Class: s, Abstract: s.Abstract}
			switch s.Kind {
			case KindClass:
				td.Kind = DClass
			case KindStruct:
				td.Kind = DStruct
			case KindInterface:
				td.Kind = DInterface
			}
			for _, d := range s.Decorators {
				if d.Name == "go" && len(d.Args) >= 1 {
					if sl, ok := d.Args[0].X.(*StrLit); ok {
						td.Kind = DExtern
						td.GoType = sl.Val
					}
					for _, a := range d.Args[1:] {
						if a.Name == "ctor" {
							if sl, ok := a.X.(*StrLit); ok {
								td.GoCtor = sl.Val
							}
						}
					}
				}
			}
			s.TD = td
			sym := &Symbol{Kind: SType, Name: s.Name, TD: td}
			td.Sym = sym
			declare(sym, s.Pos)
			m.Types = append(m.Types, td)
			c.Types = append(c.Types, td)
		case *EnumDecl:
			td := &TypeDecl{Name: s.Name, Mod: m, Pos: s.Pos, Enum: s, Kind: DEnum}
			s.TD = td
			sym := &Symbol{Kind: SType, Name: s.Name, TD: td}
			td.Sym = sym
			declare(sym, s.Pos)
			m.Types = append(m.Types, td)
			c.Types = append(c.Types, td)
		case *ExtendDecl:
			x := &ExtBlock{Mod: m, Decl: s, Methods: map[string][]*Method{}, Props: map[string]*Prop{}}
			s.Ext = x
			m.Exts = append(m.Exts, x)
		case *ImportStmt:
			c.collectImport(m, s, declare)
		case *TestDecl:
			m.Tests = append(m.Tests, s)
		case *ConstDecl:
			sym := &Symbol{Kind: SConst, Name: s.Name, DeclStmt: s, Pending: true}
			s.Sym = sym
			if declare(sym, s.Pos) {
				m.Globals = append(m.Globals, sym)
			}
			m.Consts = append(m.Consts, s)
			m.TopStmts = append(m.TopStmts, s)
		case *VarDecl:
			if id, ok := s.Target.(*Ident); ok {
				sym := &Symbol{Kind: SVar, Name: id.Name, DeclStmt: s, Pending: true}
				s.Sym = sym
				id.Sym = sym
				if declare(sym, id.Pos) {
					m.Globals = append(m.Globals, sym)
				}
			}
			m.TopStmts = append(m.TopStmts, s)
		default:
			collectAssignedNames(st, m.pendingGlobals)
			m.TopStmts = append(m.TopStmts, st)
		}
	}
}

// collectAssignedNames 收集顶层语句中被赋值的名字（不深入函数）。
func collectAssignedNames(st Stmt, out map[string]bool) {
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
	switch s := st.(type) {
	case *AssignStmt:
		for _, t := range s.Targets {
			target(t)
		}
	case *IfStmt:
		for _, x := range s.Then {
			collectAssignedNames(x, out)
		}
		for _, x := range s.Else {
			collectAssignedNames(x, out)
		}
	case *WhileStmt:
		for _, x := range s.Body {
			collectAssignedNames(x, out)
		}
	case *ForStmt:
		for _, x := range s.Body {
			collectAssignedNames(x, out)
		}
	case *TryStmt:
		for _, x := range s.Body {
			collectAssignedNames(x, out)
		}
		for _, h := range s.Handlers {
			for _, x := range h.Body {
				collectAssignedNames(x, out)
			}
		}
		for _, x := range s.Finally {
			collectAssignedNames(x, out)
		}
	case *WithStmt:
		for _, x := range s.Body {
			collectAssignedNames(x, out)
		}
	}
}

func (c *Checker) collectImport(m *Module, s *ImportStmt, declare func(*Symbol, Pos) bool) {
	if s.Mod == nil {
		return
	}
	m.Imports = append(m.Imports, s.Mod)
	if !s.From {
		name := s.Alias
		if name == "" {
			name = s.Path[len(s.Path)-1]
		}
		declare(&Symbol{Kind: SModule, Name: name, ModRef: s.Mod}, s.Pos)
		return
	}
	target := s.Mod
	if s.Star {
		names := make([]string, 0, len(target.Scope.Syms))
		for name := range target.Scope.Syms {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			sym := target.Scope.Syms[name]
			if strings.HasPrefix(name, "_") || sym.Kind == SModule {
				continue
			}
			if m.Scope.Syms[name] == nil {
				m.Scope.Syms[name] = sym
			}
		}
		m.deferredFrom = append(m.deferredFrom, s)
		return
	}
	for _, in := range s.Names {
		alias := in.Alias
		if alias == "" {
			alias = in.Name
		}
		if strings.HasPrefix(in.Name, "_") {
			c.errorf(in.Pos, "'%s' 是模块 %s 的私有名字，不能导入", in.Name, target.Name)
			continue
		}
		sym := target.Scope.Syms[in.Name]
		if sym == nil {
			if target.pendingGlobals[in.Name] {
				// 等目标模块的顶层语句检查完再绑定
				continue
			}
			c.errorf(in.Pos, "模块 %s 中没有名字 '%s'", target.Name, in.Name)
			continue
		}
		if old := m.Scope.Syms[alias]; old != nil {
			c.errorf(in.Pos, "名字 '%s' 重复定义", alias)
			continue
		}
		m.Scope.Syms[alias] = sym
	}
	m.deferredFrom = append(m.deferredFrom, s)
}

// bindDeferredImports 在模块顶层语句检查前绑定依赖模块中普通全局变量的 from-import。
func (c *Checker) bindDeferredImports(m *Module) {
	for _, s := range m.deferredFrom {
		target := s.Mod
		if s.Star {
			for name, sym := range target.Scope.Syms {
				if strings.HasPrefix(name, "_") || sym.Kind == SModule {
					continue
				}
				if m.Scope.Syms[name] == nil {
					m.Scope.Syms[name] = sym
				}
			}
			continue
		}
		for _, in := range s.Names {
			alias := in.Alias
			if alias == "" {
				alias = in.Name
			}
			if m.Scope.Syms[alias] != nil {
				continue
			}
			sym := target.Scope.Syms[in.Name]
			if sym == nil {
				c.errorf(in.Pos, "模块 %s 中没有名字 '%s'", target.Name, in.Name)
				continue
			}
			m.Scope.Syms[alias] = sym
		}
	}
}

// ===== 作用域与符号工具 =====

func (c *Checker) pushScope() *Scope {
	s := newScope(c.scope, c.fn)
	c.scope = s
	return s
}

func (c *Checker) popScope() { c.scope = c.scope.Parent }

// funcScope 返回当前函数的函数级作用域（局部变量提升到这里）。
func (c *Checker) funcScope() *Scope {
	if c.fn != nil && c.fn.Scope != nil {
		return c.fn.Scope
	}
	return c.mod.Scope
}

// declareLocal 在当前函数作用域中声明新变量。
func (c *Checker) declareLocal(name string, t Type, pos Pos, blockScoped bool) *Symbol {
	sym := &Symbol{Kind: SVar, Name: name, T: t, Pos: pos, Fn: c.fn, Mod: c.mod}
	var sc *Scope
	if blockScoped {
		sc = c.scope
	} else {
		sc = c.funcScope()
	}
	if sc.IsModule {
		sym.Global = true
		sym.Fn = nil
		c.mod.Globals = append(c.mod.Globals, sym)
	} else if !blockScoped && c.fn != nil {
		c.fn.Locals = append(c.fn.Locals, sym)
	}
	if name != "_" {
		sc.insert(sym)
	}
	return sym
}

func (c *Checker) suggest(name string) string {
	best := ""
	bestD := 3
	seen := map[string]bool{}
	for sc := c.scope; sc != nil; sc = sc.Parent {
		for n := range sc.Syms {
			if seen[n] {
				continue
			}
			seen[n] = true
			d := editDistance(name, n)
			if d < bestD || (d == bestD && n < best && best != "") {
				bestD = d
				best = n
			}
		}
	}
	return best
}

func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if abs(len(ra)-len(rb)) > 2 {
		return 99
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func (c *Checker) undefined(pos Pos, name string) {
	d := c.errorf(pos, "未定义的名字 '%s'", name)
	if s := c.suggest(name); s != "" && s != name {
		d.Notes = append(d.Notes, "是不是想写 '"+s+"'？")
	}
	switch name {
	case "True", "False":
		d.Notes = append(d.Notes, "五行的布尔值写作 true / false")
	case "None", "nil", "nullptr":
		d.Notes = append(d.Notes, "五行的空值写作 null")
	case "this":
		d.Notes = append(d.Notes, "五行用 self 表示当前对象")
	case "lambda":
		d.Notes = append(d.Notes, "五行的 lambda 写作 x => 表达式，例如 xs.map(x => x * 2)")
	case "__name__":
		d.Notes = append(d.Notes, "五行不需要 if __name__ == \"__main__\"：定义 def main() 即可作为程序入口")
	case "self":
		d.Notes = append(d.Notes, "self 只能在方法中使用")
	case "printf", "println", "console", "System", "fmt":
		d.Notes = append(d.Notes, "输出请使用 print(...)")
	case "string":
		d.Notes = append(d.Notes, "字符串类型写作 str")
	case "boolean":
		d.Notes = append(d.Notes, "布尔类型写作 bool")
	}
}

// ===== 模块初始化与测试 =====

func (c *Checker) checkModuleInit(m *Module) {
	c.mod = m
	c.bindDeferredImports(m)
	fn := m.InitFn
	c.fn = fn
	c.scope = m.Scope
	c.flow = newFlow()
	c.loops = nil
	c.tryDepth = 0
	for _, g := range m.Globals {
		if g.Kind == SConst || (g.Kind == SVar && !g.Pending) {
			c.flow.assigned[g] = true
		}
	}
	c.stmts(m.TopStmts)
	c.finishFunc(fn)
	c.fn = nil
}

func (c *Checker) checkTest(t *TestDecl) {
	fn := &FuncCtx{Name: "测试 " + t.Name, Mod: c.mod, Ret: tVoid, IsTest: true, Test: t}
	t.Fn = fn
	c.withFunc(fn, c.mod.Scope, func() {
		c.stmts(t.Body)
	})
}

// withFunc 在新的函数上下文中执行 body。
func (c *Checker) withFunc(fn *FuncCtx, parent *Scope, body func()) {
	saveFn, saveScope, saveFlow, saveLoops, saveTry := c.fn, c.scope, c.flow, c.loops, c.tryDepth
	if fn.Parent == nil && saveFn != nil && !saveFn.IsModInit {
		fn.Parent = saveFn
	} else if fn.Parent == nil && saveFn != nil && (fn.IsLambda || (fn.Decl != nil && fn.Decl.Nested)) {
		fn.Parent = saveFn
	}
	c.fn = fn
	fn.Scope = newScope(parent, fn)
	c.scope = fn.Scope
	parentFlow := c.flow
	c.flow = newFlow()
	if (fn.IsLambda || (fn.Decl != nil && fn.Decl.Nested)) && parentFlow != nil {
		// 闭包继承外层的窄化与已赋值信息
		for k, v := range parentFlow.narrow {
			c.flow.narrow[k] = v
		}
		for k, v := range parentFlow.assigned {
			c.flow.assigned[k] = v
		}
	}
	c.loops = nil
	c.tryDepth = 0
	fn.globals = map[string]bool{}
	fn.globalReads = map[string]Pos{}
	scanGlobals(fn.Body, fn.globals)
	body()
	c.finishFunc(fn)
	c.AllFuncs = append(c.AllFuncs, fn)
	c.fn, c.scope, c.flow, c.loops, c.tryDepth = saveFn, saveScope, saveFlow, saveLoops, saveTry
}

// scanGlobals 收集函数体中的 global 声明（不进入嵌套函数）。
func scanGlobals(list []Stmt, out map[string]bool) {
	walkStmts(list, func(s Stmt) {
		if gs, ok := s.(*GlobalStmt); ok && !gs.Nonlocal {
			for _, n := range gs.Names {
				out[n] = true
			}
		}
	})
}

func (c *Checker) finishFunc(fn *FuncCtx) {
	for _, s := range fn.narrowedCaptures {
		if s.Assigns > 1 || s.ClosureAssigned {
			c.errorf(s.Pos, "变量 '%s' 在闭包中以窄化后的类型使用，但它被重新赋值过；请在闭包内使用 ! 或 ??", s.Name)
		}
	}
}
