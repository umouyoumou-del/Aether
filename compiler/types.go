package compiler

import (
	"strings"
)

// Type 是五行的类型。
type Type interface {
	String() string
}

type BasicKind int

const (
	BInvalid BasicKind = iota
	BVoid
	BBool
	BInt
	BI8
	BI16
	BI32
	BU8
	BU16
	BU32
	BU64
	BFloat
	BF32
	BStr
	BChar
	BAny
	BNull
)

type Basic struct {
	Kind BasicKind
	Name string
}

func (b *Basic) String() string { return b.Name }

var (
	tInvalid = &Basic{BInvalid, "<无效>"}
	tVoid    = &Basic{BVoid, "void"}
	tBool    = &Basic{BBool, "bool"}
	tInt     = &Basic{BInt, "int"}
	tI8      = &Basic{BI8, "i8"}
	tI16     = &Basic{BI16, "i16"}
	tI32     = &Basic{BI32, "i32"}
	tU8      = &Basic{BU8, "u8"}
	tU16     = &Basic{BU16, "u16"}
	tU32     = &Basic{BU32, "u32"}
	tU64     = &Basic{BU64, "u64"}
	tFloat   = &Basic{BFloat, "float"}
	tF32     = &Basic{BF32, "f32"}
	tStr     = &Basic{BStr, "str"}
	tChar    = &Basic{BChar, "char"}
	tAny     = &Basic{BAny, "any"}
	tNull    = &Basic{BNull, "null"}
)

var basicByName = map[string]*Basic{
	"void": tVoid, "bool": tBool, "int": tInt, "i64": tInt, "i8": tI8, "i16": tI16, "i32": tI32,
	"u8": tU8, "byte": tU8, "u16": tU16, "u32": tU32, "u64": tU64,
	"float": tFloat, "f64": tFloat, "f32": tF32, "str": tStr, "char": tChar, "any": tAny,
}

type ListT struct{ Elem Type }
type DictT struct{ Key, Val Type }
type SetT struct{ Elem Type }
type TupleT struct{ Elems []Type }
type OptT struct{ Elem Type }
type ChanT struct{ Elem Type }
type IterT struct{ Elem Type }

type FuncT struct {
	Params   []Type
	Ret      Type
	Variadic bool
}

// NamedT 是类、结构体、接口、枚举或外部类型的（可能已实例化的）引用。
type NamedT struct {
	D    *TypeDecl
	Args []Type
}

type TParamT struct{ P *TypeParam }

// MetaT 表示“类型本身”作为表达式的值（例如 Dog、Color、list[int]）。
type MetaT struct{ T Type }

// GenericT 表示尚未给出类型实参的泛型类型名（list、dict、Stack……）。
type GenericT struct{ Sym *Symbol }

func (t *GenericT) String() string { return "泛型类型 " + t.Sym.Name }

// ModT 表示模块作为表达式的值。
type ModT struct{ M *Module }

// FuncRefT 表示对（可能是泛型的）函数或方法的直接引用，尚未调用。
type FuncRefT struct {
	Sig  *FuncSig
	Name string
}

func (t *ListT) String() string { return "list[" + t.Elem.String() + "]" }
func (t *DictT) String() string { return "dict[" + t.Key.String() + ", " + t.Val.String() + "]" }
func (t *SetT) String() string  { return "set[" + t.Elem.String() + "]" }
func (t *ChanT) String() string { return "chan[" + t.Elem.String() + "]" }
func (t *IterT) String() string { return "iter[" + t.Elem.String() + "]" }
func (t *OptT) String() string {
	if _, ok := t.Elem.(*FuncT); ok {
		return "(" + t.Elem.String() + ")?"
	}
	return t.Elem.String() + "?"
}
func (t *TupleT) String() string {
	var parts []string
	for _, e := range t.Elems {
		parts = append(parts, e.String())
	}
	if len(parts) == 1 {
		return "(" + parts[0] + ",)"
	}
	return "(" + strings.Join(parts, ", ") + ")"
}
func (t *FuncT) String() string {
	var parts []string
	for i, p := range t.Params {
		s := p.String()
		if t.Variadic && i == len(t.Params)-1 {
			s = "*" + s
		}
		parts = append(parts, s)
	}
	s := "fn(" + strings.Join(parts, ", ") + ")"
	if t.Ret != nil && t.Ret != tVoid {
		s += " -> " + t.Ret.String()
	}
	return s
}
func (t *NamedT) String() string {
	if len(t.Args) == 0 {
		return t.D.Name
	}
	var parts []string
	for _, a := range t.Args {
		parts = append(parts, a.String())
	}
	return t.D.Name + "[" + strings.Join(parts, ", ") + "]"
}
func (t *TParamT) String() string  { return t.P.Name }
func (t *MetaT) String() string    { return "类型 " + t.T.String() }
func (t *ModT) String() string     { return "模块 " + t.M.Name }
func (t *FuncRefT) String() string { return "函数 " + t.Name }

// ===== 声明 =====

type DeclKind int

const (
	DClass DeclKind = iota
	DStruct
	DInterface
	DEnum
	DExtern
)

func (k DeclKind) String() string {
	switch k {
	case DClass:
		return "类"
	case DStruct:
		return "结构体"
	case DInterface:
		return "接口"
	case DEnum:
		return "枚举"
	}
	return "外部类型"
}

type BoundKind int

const (
	BoundAny BoundKind = iota
	BoundHashable
	BoundOrdered
	BoundNumber
	BoundInteger
	BoundIface
	BoundSortable // 能比较大小：Ordered、实现了 Comparable 的类型，以及由它们组成的元组
)

type TypeParam struct {
	Name   string
	GoName string
	Bound  BoundKind
	Iface  *NamedT
	Pos    Pos
	Index  int
}

func (tp *TypeParam) BoundName() string {
	switch tp.Bound {
	case BoundHashable:
		return "Hashable"
	case BoundOrdered:
		return "Ordered"
	case BoundNumber:
		return "Number"
	case BoundInteger:
		return "Integer"
	case BoundIface:
		return tp.Iface.String()
	case BoundSortable:
		return "Sortable"
	}
	return "any"
}

type ParamInfo struct {
	Name     string
	T        Type
	Default  Expr
	Variadic bool
	KwOnly   bool
	Mod      *Module
	Pos      Pos
	// DefaultSubst 把默认值表达式中的类型参数映射到本签名所在的上下文
	// （例如继承来的字段默认值使用基类的类型参数）。
	DefaultSubst Subst
}

// composeSubst 先应用 a，再应用 b。
func composeSubst(a, b Subst) Subst {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	r := Subst{}
	for k, v := range b {
		r[k] = v
	}
	for k, v := range a {
		r[k] = subst(v, b)
	}
	return r
}

type FuncSig struct {
	TParams []*TypeParam
	Params  []*ParamInfo
	Ret     Type
	Async   bool
	Gen     bool
	Decl    *FuncDecl
	Scope   *Scope // 含类型参数的作用域
}

// FuncType 返回签名对应的函数值类型。
func (s *FuncSig) FuncType() *FuncT {
	ft := &FuncT{Ret: s.Ret}
	for _, p := range s.Params {
		pt := p.T
		if p.Variadic {
			ft.Variadic = true
		}
		ft.Params = append(ft.Params, pt)
	}
	if s.Async {
		ft.Ret = s.Ret
	}
	return ft
}

type Field struct {
	Name     string
	GoName   string
	T        Type
	Default  Expr
	Owner    *TypeDecl
	Pos      Pos
	Static   bool
	Const    bool
	Inferred bool
	Decl     *FieldDecl
	Sym      *Symbol // 静态字段对应的全局符号
}

type Method struct {
	Name     string
	GoName   string
	Sig      *FuncSig
	Decl     *FuncDecl
	Owner    *TypeDecl
	Ext      *ExtBlock
	Static   bool
	Abstract bool
	Override bool
	Mutating bool
	Extern   string
	Pos      Pos
	Builtin  string // 由编译器特殊处理
}

type Prop struct {
	Name     string
	GoName   string
	T        Type
	Owner    *TypeDecl
	Ext      *ExtBlock
	HasGet   bool
	HasSet   bool
	Abstract bool
	Static   bool
	Decl     *PropDecl
	Extern   string
	GetSig   *FuncSig
	SetSig   *FuncSig
	Mutating bool
}

type Variant struct {
	Name       string
	GoName     string
	Fields     []*Field
	Value      int64
	Owner      *TypeDecl
	Index      int
	Pos        Pos
	HasPayload bool
}

type TypeDecl struct {
	Name       string
	GoName     string
	Kind       DeclKind
	Mod        *Module
	Pos        Pos
	TParams    []*TypeParam
	Base       *NamedT
	Ifaces     []*NamedT
	Fields     []*Field
	FieldMap   map[string]*Field
	Statics    map[string]*Field
	Methods    map[string]*Method
	MethodList []*Method
	Props      map[string]*Prop
	PropList   []*Prop
	Variants   []*Variant
	VariantMap map[string]*Variant
	Subs       []*TypeDecl
	Abstract   bool
	ADT        bool
	Class      *ClassDecl
	Enum       *EnumDecl
	GoType     string // 外部类型的 Go 类型模板，例如 *rt.Mutex
	GoCtor     string
	Resolved   int // 0 未开始，1 进行中，2 完成
	BodiesDone bool
	Init       *Method
	IsExc      bool
	Sym        *Symbol
	Builtin    string // 内置泛型容器：list dict set chan iter
	HasStr     bool
	Hashable   int // 0 未知 1 是 2 否
	TScope     *Scope
	Ctor       *FuncSig
	CtorInit   *Method // 构造时调用的 __init__（可能继承自基类）
	AutoCtor   bool
	CtorFields []*Field
	Iter       *Method // __iter__
}

func (d *TypeDecl) IsClass() bool  { return d.Kind == DClass }
func (d *TypeDecl) IsStruct() bool { return d.Kind == DStruct }
func (d *TypeDecl) IsIface() bool  { return d.Kind == DInterface }
func (d *TypeDecl) IsEnum() bool   { return d.Kind == DEnum }

// Poly 表示该类在 Go 中需要以接口形式表示（有子类或是抽象类）。
func (d *TypeDecl) Poly() bool {
	return d.Kind == DClass && (len(d.Subs) > 0 || d.Abstract)
}

// ExtBlock 是一个 extend 块。
type ExtBlock struct {
	TParams []*TypeParam
	Target  Type
	Methods map[string][]*Method
	Props   map[string]*Prop
	Mod     *Module
	Decl    *ExtendDecl
	Key     string
	Scope   *Scope
}

// ===== 类型工具函数 =====

func isInvalid(t Type) bool { return t == nil || t == tInvalid }

func basicKind(t Type) BasicKind {
	if b, ok := t.(*Basic); ok {
		return b.Kind
	}
	return BInvalid
}

func isInteger(t Type) bool {
	switch basicKind(t) {
	case BInt, BI8, BI16, BI32, BU8, BU16, BU32, BU64:
		return true
	}
	if tp, ok := t.(*TParamT); ok {
		return tp.P.Bound == BoundInteger
	}
	return false
}

func isUnsigned(t Type) bool {
	switch basicKind(t) {
	case BU8, BU16, BU32, BU64:
		return true
	}
	return false
}

func isFloat(t Type) bool {
	k := basicKind(t)
	return k == BFloat || k == BF32
}

func isNumeric(t Type) bool {
	if isInteger(t) || isFloat(t) {
		return true
	}
	if tp, ok := t.(*TParamT); ok {
		return tp.P.Bound == BoundNumber || tp.P.Bound == BoundInteger
	}
	return false
}

func isOrdered(t Type) bool {
	if isNumeric(t) {
		return true
	}
	switch basicKind(t) {
	case BStr, BChar:
		return true
	}
	if tp, ok := t.(*TParamT); ok {
		return tp.P.Bound == BoundOrdered || tp.P.Bound == BoundNumber || tp.P.Bound == BoundInteger
	}
	return false
}

func isStr(t Type) bool  { return basicKind(t) == BStr }
func isBool(t Type) bool { return basicKind(t) == BBool }
func isAny(t Type) bool  { return basicKind(t) == BAny }
func isVoid(t Type) bool { return basicKind(t) == BVoid }
func isNull(t Type) bool { return basicKind(t) == BNull }

func isOpt(t Type) bool {
	_, ok := t.(*OptT)
	return ok
}

func optElem(t Type) Type {
	if o, ok := t.(*OptT); ok {
		return o.Elem
	}
	return t
}

func namedDecl(t Type) *TypeDecl {
	if n, ok := t.(*NamedT); ok {
		return n.D
	}
	return nil
}

func isClassType(t Type) bool {
	d := namedDecl(t)
	return d != nil && d.Kind == DClass
}

func isIfaceType(t Type) bool {
	d := namedDecl(t)
	return d != nil && d.Kind == DInterface
}

// numRank 用于数值隐式拓宽。
func numRank(t Type) int {
	switch basicKind(t) {
	case BI8:
		return 1
	case BU8:
		return 1
	case BI16:
		return 2
	case BU16:
		return 2
	case BI32:
		return 3
	case BU32:
		return 3
	case BInt:
		return 4
	case BU64:
		return 4
	case BF32:
		return 5
	case BFloat:
		return 6
	}
	return 0
}

// numWidens 判断 from 能否隐式拓宽为 to。
func numWidens(from, to Type) bool {
	if identical(from, to) {
		return true
	}
	if !isNumeric(from) || !isNumeric(to) {
		return false
	}
	if _, ok := from.(*TParamT); ok {
		return false
	}
	if _, ok := to.(*TParamT); ok {
		return false
	}
	if isFloat(to) {
		if isFloat(from) {
			return numRank(from) <= numRank(to)
		}
		return true // 整数 -> 浮点
	}
	if isFloat(from) {
		return false
	}
	// 整数之间
	if isUnsigned(from) == isUnsigned(to) {
		return numRank(from) < numRank(to)
	}
	if isUnsigned(from) && !isUnsigned(to) {
		return numRank(from) < numRank(to)
	}
	return false
}

func identical(a, b Type) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	switch x := a.(type) {
	case *Basic:
		y, ok := b.(*Basic)
		return ok && x.Kind == y.Kind
	case *ListT:
		y, ok := b.(*ListT)
		return ok && identical(x.Elem, y.Elem)
	case *SetT:
		y, ok := b.(*SetT)
		return ok && identical(x.Elem, y.Elem)
	case *ChanT:
		y, ok := b.(*ChanT)
		return ok && identical(x.Elem, y.Elem)
	case *IterT:
		y, ok := b.(*IterT)
		return ok && identical(x.Elem, y.Elem)
	case *OptT:
		y, ok := b.(*OptT)
		return ok && identical(x.Elem, y.Elem)
	case *DictT:
		y, ok := b.(*DictT)
		return ok && identical(x.Key, y.Key) && identical(x.Val, y.Val)
	case *TupleT:
		y, ok := b.(*TupleT)
		if !ok || len(x.Elems) != len(y.Elems) {
			return false
		}
		for i := range x.Elems {
			if !identical(x.Elems[i], y.Elems[i]) {
				return false
			}
		}
		return true
	case *FuncT:
		y, ok := b.(*FuncT)
		if !ok || len(x.Params) != len(y.Params) || x.Variadic != y.Variadic {
			return false
		}
		for i := range x.Params {
			if !identical(x.Params[i], y.Params[i]) {
				return false
			}
		}
		return identical(retOrVoid(x.Ret), retOrVoid(y.Ret))
	case *NamedT:
		y, ok := b.(*NamedT)
		if !ok || x.D != y.D || len(x.Args) != len(y.Args) {
			return false
		}
		for i := range x.Args {
			if !identical(x.Args[i], y.Args[i]) {
				return false
			}
		}
		return true
	case *TParamT:
		y, ok := b.(*TParamT)
		return ok && x.P == y.P
	}
	return false
}

func retOrVoid(t Type) Type {
	if t == nil {
		return tVoid
	}
	return t
}

type Subst map[*TypeParam]Type

func subst(t Type, m Subst) Type {
	if len(m) == 0 || t == nil {
		return t
	}
	switch x := t.(type) {
	case *TParamT:
		if r, ok := m[x.P]; ok && r != nil {
			return r
		}
		return t
	case *ListT:
		return &ListT{subst(x.Elem, m)}
	case *SetT:
		return &SetT{subst(x.Elem, m)}
	case *ChanT:
		return &ChanT{subst(x.Elem, m)}
	case *IterT:
		return &IterT{subst(x.Elem, m)}
	case *OptT:
		e := subst(x.Elem, m)
		if o, ok := e.(*OptT); ok {
			return o
		}
		return &OptT{e}
	case *DictT:
		return &DictT{subst(x.Key, m), subst(x.Val, m)}
	case *TupleT:
		n := &TupleT{}
		for _, e := range x.Elems {
			n.Elems = append(n.Elems, subst(e, m))
		}
		return n
	case *FuncT:
		n := &FuncT{Ret: subst(x.Ret, m), Variadic: x.Variadic}
		for _, p := range x.Params {
			n.Params = append(n.Params, subst(p, m))
		}
		return n
	case *NamedT:
		if len(x.Args) == 0 {
			return x
		}
		n := &NamedT{D: x.D}
		for _, a := range x.Args {
			n.Args = append(n.Args, subst(a, m))
		}
		return n
	case *MetaT:
		return &MetaT{subst(x.T, m)}
	}
	return t
}

// containsTParam 判断类型中是否含有（给定集合中的）类型参数。
func containsTParam(t Type, set map[*TypeParam]bool) bool {
	switch x := t.(type) {
	case *TParamT:
		return set == nil || set[x.P]
	case *ListT:
		return containsTParam(x.Elem, set)
	case *SetT:
		return containsTParam(x.Elem, set)
	case *ChanT:
		return containsTParam(x.Elem, set)
	case *IterT:
		return containsTParam(x.Elem, set)
	case *OptT:
		return containsTParam(x.Elem, set)
	case *DictT:
		return containsTParam(x.Key, set) || containsTParam(x.Val, set)
	case *TupleT:
		for _, e := range x.Elems {
			if containsTParam(e, set) {
				return true
			}
		}
	case *FuncT:
		for _, p := range x.Params {
			if containsTParam(p, set) {
				return true
			}
		}
		return containsTParam(x.Ret, set)
	case *NamedT:
		for _, a := range x.Args {
			if containsTParam(a, set) {
				return true
			}
		}
	}
	return false
}

func makeSubst(tps []*TypeParam, args []Type) Subst {
	if len(tps) == 0 {
		return nil
	}
	m := Subst{}
	for i, tp := range tps {
		if i < len(args) {
			m[tp] = args[i]
		}
	}
	return m
}

// supertypes 返回 nt 及其所有祖先类（已代入类型实参）。
func supertypes(nt *NamedT) []*NamedT {
	var out []*NamedT
	cur := nt
	for cur != nil && len(out) < 64 {
		out = append(out, cur)
		b := cur.D.Base
		if b == nil {
			break
		}
		cur = subst(b, makeSubst(cur.D.TParams, cur.Args)).(*NamedT)
	}
	return out
}

// isSubclass 判断 sub 是否为 sup 本身或其子类。
func isSubclassDecl(sub, sup *TypeDecl) bool {
	for d := sub; d != nil; {
		if d == sup {
			return true
		}
		if d.Base == nil {
			return false
		}
		d = d.Base.D
	}
	return false
}

// typeHead 返回用于查找扩展方法的类型键。
func typeHead(t Type) string {
	switch x := t.(type) {
	case *Basic:
		return x.Name
	case *ListT:
		return "list"
	case *DictT:
		return "dict"
	case *SetT:
		return "set"
	case *TupleT:
		return "tuple"
	case *ChanT:
		return "chan"
	case *IterT:
		return "iter"
	case *OptT:
		return "opt"
	case *FuncT:
		return "fn"
	case *NamedT:
		return "decl:" + x.D.Mod.Name + "." + x.D.Name
	}
	return ""
}

// valueTypeStr 用于错误信息中的类型描述。
func typeStr(t Type) string {
	if t == nil {
		return "<未知>"
	}
	return t.String()
}
