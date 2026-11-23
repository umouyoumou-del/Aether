package compiler

// ===== 表达式 =====

type Expr interface {
	P() Pos
	Typ() Type
	setTyp(Type)
}

type exprBase struct {
	Pos Pos
	T   Type
}

func (e *exprBase) P() Pos        { return e.Pos }
func (e *exprBase) Typ() Type     { return e.T }
func (e *exprBase) setTyp(t Type) { e.T = t }

// NarrowKind 描述在某处对变量做了何种窄化（智能转换）。
type NarrowKind int

const (
	NarrowNone   NarrowKind = iota
	NarrowUnwrap            // T? -> T
	NarrowCast              // 基类/接口/any -> 具体类型
)

type Ident struct {
	exprBase
	Name   string
	Sym    *Symbol
	Narrow NarrowKind
	From   Type    // 窄化前的类型
	Lam    *Lambda // 内置函数、类型名或泛型函数被当作函数值使用时，等价的 lambda
}

type IntLit struct {
	exprBase
	Text string
	Val  uint64
	Neg  bool // 由常量折叠产生的负数
}

type FloatLit struct {
	exprBase
	Text string
	Val  float64
}

type StrLit struct {
	exprBase
	Val string
}

type CharLit struct {
	exprBase
	Val rune
}

type BoolLit struct {
	exprBase
	Val bool
}

type NullLit struct{ exprBase }

type FString struct {
	exprBase
	Parts []*FPart
}

type ListLit struct {
	exprBase
	Elems []Expr
}

type SetLit struct {
	exprBase
	Elems []Expr
}

type TupleLit struct {
	exprBase
	Elems []Expr
	Paren bool // 写成了 (a, b)；用于区分 chan[(int, int)] 与 dict[str, int]
}

type DictLit struct {
	exprBase
	Keys []Expr
	Vals []Expr
}

type CompKind int

const (
	CompList CompKind = iota
	CompSet
	CompDict
	CompGen
)

type CompClause struct {
	Pos    Pos
	IsIf   bool
	Target Expr // Ident 或 TupleLit（由 Ident 组成）
	Iter   Expr
	Cond   Expr
	Loop   *LoopInfo
}

type Comp struct {
	exprBase
	Kind    CompKind
	Elem    Expr
	Key     Expr
	Val     Expr
	Clauses []*CompClause
}

type Unary struct {
	exprBase
	Op TokKind // TMinus TPlus TTilde KNot
	X  Expr
	Op2 *Method // 运算符重载
}

type Binary struct {
	exprBase
	Op  TokKind
	X   Expr
	Y   Expr
	Op2 *Method // 运算符重载
	Rev bool    // 使用了反向运算符 __rxxx__
}

// Compare 表示（可能链式的）比较：a < b <= c
type Compare struct {
	exprBase
	Ops  []TokKind // TEq TNe TLt TGt TLe TGe KIn KNotIn
	Xs   []Expr
	Ovl  []*Method // 每个比较对应的重载方法（可为 nil）
	Swap []bool    // 重载时交换了操作数
	Neg  []bool    // 结果需要取反
}

type Logical struct {
	exprBase
	Op TokKind // KAnd KOr
	X  Expr
	Y  Expr
}

type Ternary struct {
	exprBase
	Cond Expr
	Then Expr
	Else Expr
}

type NullCo struct {
	exprBase
	X Expr
	Y Expr
}

type Lambda struct {
	exprBase
	Params []*Param
	Ret    TypeExpr
	Body   Expr
	Fn     *FuncCtx
}

type Arg struct {
	Pos  Pos
	Name string
	X    Expr
	Star bool
}

type CallKind int

const (
	CallUnresolved CallKind = iota
	CallFunc                // 普通函数（全局或模块）
	CallMethod              // 实例方法
	CallStatic              // 静态方法
	CallCtor                // 构造
	CallValue               // 调用函数值（lambda、变量）
	CallBuiltin             // 内置函数
	CallConv                // 类型转换 int(x) 等
	CallExtern              // 外部 Go 函数
	CallSuper               // super().m(...)
	CallExt                 // 扩展方法
	CallVariant             // 枚举变体构造 Shape.Circle(1.0)
	CallLocalFunc           // 嵌套 def
	CallEmptyColl           // list[int]() 等
)

type Call struct {
	exprBase
	Fn       Expr
	Args     []*Arg
	Kind     CallKind
	Recv     Expr     // 方法接收者
	Method   *Method  // 被调用的方法
	Func     *Symbol  // 被调用的函数符号
	Sig      *FuncSig // 签名（已实例化之前）
	Final    []Expr   // 按形参顺序排列的实参（含默认值，nil 表示使用默认值）
	Spread   bool     // 最后一个实参是 *xs 展开
	TArgs    []Type   // 类型实参
	Builtin  string
	Ctor     *NamedT
	Variant  *Variant
	DefaultEnv *Module // 默认值表达式所在模块
	ConvTo   Type
	Extra    any
	Subst    Subst // 调用时的类型代换（含接收者的类型实参）
	ViaCall  bool  // obj(...) 调用的是 obj.__call__(...)（Recv 是 obj）
}

type Index struct {
	exprBase
	X    Expr
	Idx  Expr
	Ovl  *Method // __getitem__
	TupleIdx int
	IsTypeApp bool
}

// TypeApp 是带类型实参的表达式：Stack[int]、max_of[float]
type TypeApp struct {
	exprBase
	X     Expr
	Args  []TypeExpr
	Types []Type
}

type SliceExpr struct {
	exprBase
	X    Expr
	Lo   Expr
	Hi   Expr
	Step Expr
}

type AttrKind int

const (
	AttrUnresolved AttrKind = iota
	AttrField
	AttrProp
	AttrMethodValue // 绑定方法值
	AttrModule      // 模块成员
	AttrStaticField
	AttrStaticMethod
	AttrEnumCase    // Color.Red
	AttrVariantCtor // Shape.Circle（作为函数）
	AttrTupleField
	AttrBuiltinProp // 例如 enum 的 .name/.value
	AttrType        // 模块中的类型
	AttrExtProp
)

type Attr struct {
	exprBase
	X       Expr
	Name    string
	NamePos Pos
	Safe    bool // ?.
	Kind    AttrKind
	Field   *Field
	Prop    *Prop
	Method  *Method
	Sym     *Symbol // 模块成员
	Variant *Variant
	Owner   *NamedT // 成员所属类型（已实例化）
	RecvT   Type    // 接收者类型（?. 时为解包后的类型）
	Lam     *Lambda // 类型.方法 被当作函数值使用时（例如 key=str.lower），等价的 lambda
}

type Unwrap struct {
	exprBase
	X Expr
}

// StarExpr 是 *x：解构赋值中收集其余元素的目标（a, *rest = xs），或列表/集合字面量中的展开
// （[*a, *b]）。Double 表示字典字面量中的 **d（此时它出现在 DictLit.Keys 中，对应的值为 nil）。
type StarExpr struct {
	exprBase
	X      Expr
	Double bool
}

// NamedExpr 是赋值表达式（海象运算符）：(名字 := 值)，值同时也是整个表达式的值。
type NamedExpr struct {
	exprBase
	Target *Ident
	Value  Expr
}

type Await struct {
	exprBase
	X Expr
}

type Cast struct {
	exprBase
	X    Expr
	Type TypeExpr
	Safe bool // as?
	To   Type
	Mode int // 由类型检查器设置
}

type IsExpr struct {
	exprBase
	X    Expr
	Type TypeExpr
	Null bool // is null
	Neg  bool
	To   Type
}

// SuperExpr 是 super() 本身；只能出现在 super().xxx 中。
type SuperExpr struct{ exprBase }

// ConvKind 是类型检查器插入的隐式转换种类。
type ConvKind int

const (
	ConvNone    ConvKind = iota
	ConvNumeric          // 数值拓宽
	ConvWrap             // T -> T?
	ConvNull             // null -> T?
	ConvToAny            // -> any
	ConvOptAny           // T? -> any
	ConvIface            // 类 -> 基类/接口（Go 中隐式）
	ConvOptMap           // A? -> B?（内部需要转换）
	ConvFunc             // 函数类型适配
	ConvIter             // list/set/dict/str/chan -> iter[T]
	ConvTruthy           // 条件中的真值判断
)

type ConvExpr struct {
	exprBase
	X    Expr
	Kind ConvKind
	From Type
	Inner ConvKind // ConvOptMap / ConvWrap 时内部的转换
}

// ===== 类型表达式 =====

type TypeExpr interface {
	P() Pos
	typeNode()
}

type NamedTypeX struct {
	Pos  Pos
	Mod  string // 模块前缀（可选）
	Name string
	Args []TypeExpr
}

type OptTypeX struct {
	Pos  Pos
	Elem TypeExpr
}

type TupleTypeX struct {
	Pos   Pos
	Elems []TypeExpr
}

type FuncTypeX struct {
	Pos    Pos
	Params []TypeExpr
	Ret    TypeExpr
}

func (t *NamedTypeX) P() Pos { return t.Pos }
func (t *OptTypeX) P() Pos   { return t.Pos }
func (t *TupleTypeX) P() Pos { return t.Pos }
func (t *FuncTypeX) P() Pos  { return t.Pos }
func (*NamedTypeX) typeNode() {}
func (*OptTypeX) typeNode()   {}
func (*TupleTypeX) typeNode() {}
func (*FuncTypeX) typeNode()  {}

// ===== 模式 =====

type Pattern interface {
	P() Pos
	patNode()
}

type WildPat struct{ Pos Pos }

// NamePat 是单个名字或点分名字：x、Red、Color.Red、Dog
type NamePat struct {
	Pos   Pos
	Names []string
	// 由类型检查器决定：
	Kind    int // patCapture / patValue / patType / patVariant
	Sym     *Symbol
	Variant *Variant
	Value   Expr
	Type    Type
}

type LitPat struct {
	Pos Pos
	X   Expr
}

type OrPat struct {
	Pos  Pos
	Alts []Pattern
}

type TuplePat struct {
	Pos   Pos
	Elems []Pattern
	TT    *TupleT
}

type ListPat struct {
	Pos      Pos
	Elems    []Pattern
	RestIdx  int // -1 表示没有 *rest
	RestName string
	RestSym  *Symbol
	Elem     Type
}

// CallPat：Circle(r)、Shape.Rect(w, h)、Point(x=0, y=yy)
type CallPat struct {
	Pos     Pos
	Names   []string
	Args    []Pattern
	KwNames []string // 与 Args 对齐，""表示位置参数
	Variant *Variant
	TD      *NamedT
	Fields  []*Field // 每个参数对应的字段
	Total   bool     // 主体类型恰好是该类型（不需要类型判断）
}

// TypePat：Dog d、str s、int n
type TypePat struct {
	Pos   Pos
	Type  TypeExpr
	Name  string
	Sym   *Symbol
	To    Type
	Total bool
}

type AsPat struct {
	Pos  Pos
	Pat  Pattern
	Name string
	Sym  *Symbol
}

const (
	patCapture = iota + 1
	patValue
	patType
	patVariant
)

func (p *WildPat) P() Pos  { return p.Pos }
func (p *NamePat) P() Pos  { return p.Pos }
func (p *LitPat) P() Pos   { return p.Pos }
func (p *OrPat) P() Pos    { return p.Pos }
func (p *TuplePat) P() Pos { return p.Pos }
func (p *ListPat) P() Pos  { return p.Pos }
func (p *CallPat) P() Pos  { return p.Pos }
func (p *TypePat) P() Pos  { return p.Pos }
func (p *AsPat) P() Pos    { return p.Pos }
func (*WildPat) patNode()  {}
func (*NamePat) patNode()  {}
func (*LitPat) patNode()   {}
func (*OrPat) patNode()    {}
func (*TuplePat) patNode() {}
func (*ListPat) patNode()  {}
func (*CallPat) patNode()  {}
func (*TypePat) patNode()  {}
func (*AsPat) patNode()    {}

// ===== 语句 =====

type Stmt interface {
	P() Pos
	stmtNode()
}

type ExprStmt struct {
	Pos Pos
	X   Expr
}

type AssignStmt struct {
	Pos     Pos
	Targets []Expr // 链式赋值 a = b = 0 时有多个
	Value   Expr
	Op      TokKind // TAssign 或复合赋值运算符
	Decl    []*Symbol // 本语句新声明的变量
	OpMethod *Method  // 复合赋值的运算符重载
}

// VarDecl：x: T = v 或 x: T
type VarDecl struct {
	Pos     Pos
	Target  Expr // Ident 或 self.x
	Type    TypeExpr
	Value   Expr
	Sym     *Symbol
	Field   *Field // self.x: T = v
}

type ConstDecl struct {
	Pos     Pos
	NamePos Pos
	Name    string
	Type   TypeExpr
	Value  Expr
	Sym    *Symbol
	GoName string // @go 常量
}

type IfStmt struct {
	Pos  Pos
	Cond Expr
	Then []Stmt
	Else []Stmt
	// 窄化信息（供代码生成参考）
}

type WhileStmt struct {
	Pos  Pos
	Cond Expr
	Body []Stmt
	Loop *LoopInfo
}

type ForStmt struct {
	Pos    Pos
	Target Expr // Ident 或 TupleLit
	Iter   Expr
	Body   []Stmt
	Loop   *LoopInfo
}

// LoopInfo 记录循环的迭代方式（由类型检查器填写）。
type LoopInfo struct {
	Kind     int // loopXXX
	Vars     []*Symbol
	ElemT    Type
	KeyT     Type
	Range    []Expr // range(a, b, step) 的参数
	Inner    Expr   // enumerate/zip/items 的内部可迭代对象
	Inner2   Expr
	Method    *Method // __iter__
	HasBreak  bool
	InnerKind int // enumerate 内部的迭代方式
}

const (
	loopList = iota + 1
	loopRange
	loopStr
	loopDict
	loopDictItems
	loopDictValues
	loopSet
	loopIter
	loopChan
	loopEnumerate
	loopZip
	loopIterMethod
	loopTupleList // for a, b in list_of_tuples
)

type BreakStmt struct{ Pos Pos }
type ContinueStmt struct{ Pos Pos }
type PassStmt struct{ Pos Pos }

type ReturnStmt struct {
	Pos   Pos
	Value Expr
}

type RaiseStmt struct {
	Pos Pos
	X   Expr // nil 表示重新抛出
}

type Handler struct {
	Pos   Pos
	Types []TypeExpr
	Name  string
	Body  []Stmt
	Sym   *Symbol
	TDs   []Type
}

type TryStmt struct {
	Pos      Pos
	Body     []Stmt
	Handlers []*Handler
	Else     []Stmt
	Finally  []Stmt
}

type WithItem struct {
	X    Expr
	Name string
	Pos  Pos
	Sym  *Symbol
	Enter *Method
	Exit  *Method
	EnterT Type
}

type WithStmt struct {
	Pos   Pos
	Items []*WithItem
	Body  []Stmt
}

type DeferStmt struct {
	Pos Pos
	X   Expr
}

type GoStmt struct {
	Pos Pos
	X   Expr
}

type AssertStmt struct {
	Pos  Pos
	Cond Expr
	Msg  Expr
	Src  string // 断言表达式的源码文本
}

type DelStmt struct {
	Pos     Pos
	Targets []Expr
}

type Case struct {
	Pos   Pos
	Pat   Pattern
	Guard Expr
	Body  []Stmt
}

type MatchStmt struct {
	Pos     Pos
	Subject Expr
	Cases      []*Case
	SubjT      Type
	Exhaustive bool
}

const (
	selRecv = iota + 1
	selSend
	selDefault
)

type SelCase struct {
	Pos    Pos
	Kind   int
	Target Expr // 接收结果绑定（Ident 或 nil）
	Chan   Expr
	Value  Expr
	Body   []Stmt
	Sym    *Symbol
	Decl   bool
}

type SelectStmt struct {
	Pos   Pos
	Cases []*SelCase
}

type YieldStmt struct {
	Pos   Pos
	Value Expr
	From  bool // yield from 可迭代对象
	Loop  *LoopInfo
}

// GlobalStmt：global x, y（在函数中修改模块级变量）
type GlobalStmt struct {
	Pos      Pos
	Names    []string
	Nonlocal bool
}

// ===== 声明 =====

type Decorator struct {
	Pos  Pos
	Name string
	Args []*Arg
}

type Param struct {
	Pos      Pos
	Name     string
	Type     TypeExpr
	Default  Expr
	Variadic bool
	KwOnly   bool // 位于单独的 * 之后，只能用关键字传递
	Sym      *Symbol
}

type TypeParamX struct {
	Pos        Pos
	Name       string
	Constraint TypeExpr
}

type FuncDecl struct {
	Pos        Pos
	Name       string
	NamePos    Pos
	TypeParams []*TypeParamX
	Params     []*Param
	Ret        TypeExpr
	Body       []Stmt // nil 表示没有函数体（extern/abstract/接口）
	HasBody    bool
	Decorators []*Decorator
	Async      bool
	Static     bool
	Abstract   bool
	Override   bool
	Nested     bool
	// 解析结果
	Sym    *Symbol
	Sig    *FuncSig
	Fn     *FuncCtx
	Method *Method
	GoExtern string
	// @cache / @lru_cache：缓存结果（CacheMax <= 0 表示不限大小）
	Cache    bool
	CacheMax int
	CachePos Pos
}

type FieldDecl struct {
	Pos     Pos
	Name    string
	Type    TypeExpr
	Default Expr
	Static  bool
	Const   bool
	Field   *Field
}

type PropDecl struct {
	Pos      Pos
	NamePos  Pos
	Name     string
	Type     TypeExpr
	Getter   []Stmt
	Setter   []Stmt
	SetParam string
	HasGet   bool
	HasSet   bool
	Static   bool
	Abstract bool
	Prop     *Prop
	GetFn    *FuncCtx
	SetFn    *FuncCtx
	Decorators []*Decorator
	GoExtern string
}

type ClassKind int

const (
	KindClass ClassKind = iota
	KindStruct
	KindInterface
)

type ClassDecl struct {
	Pos        Pos
	NamePos    Pos
	Name       string
	Kind       ClassKind
	TypeParams []*TypeParamX
	Bases      []TypeExpr
	Fields     []*FieldDecl
	Methods    []*FuncDecl
	Props      []*PropDecl
	Abstract   bool
	Decorators []*Decorator
	TD         *TypeDecl
}

type VariantDecl struct {
	Pos    Pos
	Name   string
	Fields []*Param
	HasParens bool
	Value  Expr
}

type EnumDecl struct {
	Pos        Pos
	NamePos    Pos
	Name       string
	TypeParams []*TypeParamX
	Bases      []TypeExpr
	Variants   []*VariantDecl
	Methods    []*FuncDecl
	Props      []*PropDecl
	TD         *TypeDecl
}

type ExtendDecl struct {
	Pos        Pos
	TypeParams []*TypeParamX
	Target     TypeExpr
	Methods    []*FuncDecl
	Props      []*PropDecl
	Ext        *ExtBlock
}

// TypeAliasDecl：type 名字[类型参数] = 类型
type TypeAliasDecl struct {
	Pos        Pos
	Name       string
	TypeParams []*TypeParamX
	Type       TypeExpr
	Sym        *Symbol
}

type ImportName struct {
	Pos   Pos
	Name  string
	Alias string
}

type ImportStmt struct {
	Pos   Pos
	Path  []string
	Alias string
	From  bool
	Names []*ImportName
	Star  bool
	Mod   *Module
}

type TestDecl struct {
	Pos  Pos
	Name string
	Body []Stmt
	Fn   *FuncCtx
}

type FuncDeclStmt struct {
	Pos Pos
	Fn  *FuncDecl
}

func (s *ExprStmt) P() Pos     { return s.Pos }
func (s *AssignStmt) P() Pos   { return s.Pos }
func (s *VarDecl) P() Pos      { return s.Pos }
func (s *ConstDecl) P() Pos    { return s.Pos }
func (s *IfStmt) P() Pos       { return s.Pos }
func (s *WhileStmt) P() Pos    { return s.Pos }
func (s *ForStmt) P() Pos      { return s.Pos }
func (s *BreakStmt) P() Pos    { return s.Pos }
func (s *ContinueStmt) P() Pos { return s.Pos }
func (s *PassStmt) P() Pos     { return s.Pos }
func (s *ReturnStmt) P() Pos   { return s.Pos }
func (s *RaiseStmt) P() Pos    { return s.Pos }
func (s *TryStmt) P() Pos      { return s.Pos }
func (s *WithStmt) P() Pos     { return s.Pos }
func (s *DeferStmt) P() Pos    { return s.Pos }
func (s *GoStmt) P() Pos       { return s.Pos }
func (s *AssertStmt) P() Pos   { return s.Pos }
func (s *DelStmt) P() Pos      { return s.Pos }
func (s *MatchStmt) P() Pos    { return s.Pos }
func (s *SelectStmt) P() Pos   { return s.Pos }
func (s *YieldStmt) P() Pos    { return s.Pos }
func (s *GlobalStmt) P() Pos   { return s.Pos }
func (s *FuncDecl) P() Pos     { return s.Pos }
func (s *ClassDecl) P() Pos    { return s.Pos }
func (s *EnumDecl) P() Pos     { return s.Pos }
func (s *ExtendDecl) P() Pos   { return s.Pos }
func (s *TypeAliasDecl) P() Pos { return s.Pos }
func (s *ImportStmt) P() Pos   { return s.Pos }
func (s *TestDecl) P() Pos     { return s.Pos }
func (s *FuncDeclStmt) P() Pos { return s.Pos }

func (*ExprStmt) stmtNode()     {}
func (*AssignStmt) stmtNode()   {}
func (*VarDecl) stmtNode()      {}
func (*ConstDecl) stmtNode()    {}
func (*IfStmt) stmtNode()       {}
func (*WhileStmt) stmtNode()    {}
func (*ForStmt) stmtNode()      {}
func (*BreakStmt) stmtNode()    {}
func (*ContinueStmt) stmtNode() {}
func (*PassStmt) stmtNode()     {}
func (*ReturnStmt) stmtNode()   {}
func (*RaiseStmt) stmtNode()    {}
func (*TryStmt) stmtNode()      {}
func (*WithStmt) stmtNode()     {}
func (*DeferStmt) stmtNode()    {}
func (*GoStmt) stmtNode()       {}
func (*AssertStmt) stmtNode()   {}
func (*DelStmt) stmtNode()      {}
func (*MatchStmt) stmtNode()    {}
func (*SelectStmt) stmtNode()   {}
func (*YieldStmt) stmtNode()    {}
func (*GlobalStmt) stmtNode()   {}
func (*FuncDecl) stmtNode()     {}
func (*ClassDecl) stmtNode()    {}
func (*EnumDecl) stmtNode()     {}
func (*ExtendDecl) stmtNode()   {}
func (*TypeAliasDecl) stmtNode() {}
func (*ImportStmt) stmtNode()   {}
func (*TestDecl) stmtNode()     {}
func (*FuncDeclStmt) stmtNode() {}

// File 是一个已解析的模块源文件。
type File struct {
	Src   *SourceFile
	Stmts []Stmt
}
