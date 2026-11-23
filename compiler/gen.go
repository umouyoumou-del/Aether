package compiler

import (
	"fmt"
	"go/format"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// RTPath 是生成程序中运行时包的导入路径。
// 生成的程序使用模块路径 aether/assets，使运行时及其子包的导入路径
// 与 Aether 源码树中一致（子包可以导入 aether/assets/rt）。
const RTPath = "aether/assets/rt"

var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true, "default": true,
	"defer": true, "else": true, "fallthrough": true, "for": true, "func": true, "go": true,
	"goto": true, "if": true, "import": true, "interface": true, "map": true, "package": true,
	"range": true, "return": true, "select": true, "struct": true, "switch": true, "type": true, "var": true,
}

var goPredeclared = []string{
	"bool", "byte", "complex64", "complex128", "error", "float32", "float64", "int", "int8", "int16",
	"int32", "int64", "rune", "string", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
	"any", "comparable", "true", "false", "iota", "nil", "append", "cap", "clear", "close", "complex",
	"copy", "delete", "imag", "len", "make", "max", "min", "new", "panic", "print", "println", "real",
	"recover", "main", "init", "_",
}

// 生成代码可能导入的包名。
var knownPkgs = map[string]string{
	"rt": RTPath, "rtjson": RTPath + "/rtjson", "rthttp": RTPath + "/rthttp", "rtre": RTPath + "/rtre", "rtenc": RTPath + "/rtenc", "rtcsv": RTPath + "/rtcsv", "fmt": "fmt", "math": "math", "strings": "strings", "strconv": "strconv",
	"unicode": "unicode", "utf8": "unicode/utf8", "os": "os", "time": "time", "sort": "sort",
	"slices": "slices", "maps": "maps", "cmp": "cmp", "iter": "iter", "sync": "sync", "atomic": "sync/atomic",
	"runtime": "runtime", "errors": "errors", "reflect": "reflect", "regexp": "regexp",
	"json": "encoding/json", "rand": "math/rand/v2", "filepath": "path/filepath", "bufio": "bufio",
	"io": "io", "bytes": "bytes", "http": "net/http", "url": "net/url", "exec": "os/exec", "bits": "math/bits",
	"big": "math/big", "hex": "encoding/hex", "base64": "encoding/base64", "sha256": "crypto/sha256",
	"md5": "crypto/md5", "path": "path", "unsafe": "unsafe", "context": "context", "log": "log",
	"cmplx": "math/cmplx", "csv": "encoding/csv", "user": "os/user", "signal": "os/signal",
}

var goReserved = func() map[string]bool {
	m := map[string]bool{}
	for k := range goKeywords {
		m[k] = true
	}
	for _, k := range goPredeclared {
		m[k] = true
	}
	for k := range knownPkgs {
		m[k] = true
	}
	return m
}()

// 成员名（字段、方法）需要避开的名字。
var memberReserved = map[string]bool{"String": true, "Error": true}

// Gen 把类型检查后的程序生成为 Go 源码。
type Gen struct {
	c        *Checker
	testMode bool
	imports  map[string]bool
	used     map[string]bool // 包级 Go 名字
	globals  strings.Builder
	decls    strings.Builder
	w        *strings.Builder
	ind      int
	fn       *genFn
	subst    Subst
	self     *NamedT // 方法副本中 self 的具体类型
	curFile  *SourceFile
	lastLine int
	tmpN     int
	tpNames  map[*TypeParam]string
	ifaceProps map[string]bool
	superReq map[string]bool
	work     []methodWork
	srcPath  map[*SourceFile]string
	Options  GenOptions
	inConst  bool
}

type GenOptions struct {
	NoLines bool // 不生成 //line 指令（用于查看生成代码）
}

type genFn struct {
	ctx      *FuncCtx
	retT     Type
	needFlow bool
	isGen    bool
	tryDepth int
	loops    []*genLoop
	trys     []*genTry
	handlers []string
	tparams  []*TypeParam
}

type genLoop struct {
	label     string
	tryDepth  int
	selDepth  int
	labelUsed bool
}

type genTry struct {
	usedReturn, usedBreak, usedContinue bool
	loopDepth                           int
}

type methodWork struct {
	cls    *TypeDecl
	m      *Method
	def    *TypeDecl
	goName string
	prop   *Prop
	setter bool
}

// Generate 生成整个程序的 Go 源码。
func Generate(c *Checker, testMode bool, opts GenOptions) (string, error) {
	g := &Gen{c: c, testMode: testMode, imports: map[string]bool{}, used: map[string]bool{}, tpNames: map[*TypeParam]string{},
		ifaceProps: map[string]bool{}, superReq: map[string]bool{}, srcPath: map[*SourceFile]string{}, Options: opts}
	g.assignNames()
	g.collectIfaceProps()
	g.w = &g.decls
	for _, td := range c.Types {
		g.genTypeDecl(td)
	}
	for _, m := range c.Mods {
		for _, fd := range m.Funcs {
			g.genFunc(fd)
		}
		for _, x := range m.Exts {
			g.genExt(x)
		}
	}
	for _, m := range c.Mods {
		g.genModuleInit(m)
	}
	if testMode {
		g.genTests()
	}
	g.genHooks()
	g.genMain()
	g.genGlobals()

	var out strings.Builder
	out.WriteString("// 由 Aether 编译器生成，请勿手动修改。\n\npackage main\n\n")
	paths := make([]string, 0, len(g.imports))
	for p := range g.imports {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if len(paths) > 0 {
		out.WriteString("import (\n")
		for _, p := range paths {
			alias := ""
			for name, path := range knownPkgs {
				if path == p && name != pathBase(p) {
					alias = name + " "
				}
			}
			fmt.Fprintf(&out, "\t%s%q\n", alias, p)
		}
		out.WriteString(")\n\n")
	}
	out.WriteString(g.globals.String())
	out.WriteString(g.decls.String())
	return out.String(), nil
}

func pathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		b := p[i+1:]
		if b == "v2" {
			return pathBase(p[:i])
		}
		return b
	}
	return p
}

// FormatGo 用 gofmt 格式化生成的代码（仅用于展示）。
func FormatGo(src string) string {
	b, err := format.Source([]byte(src))
	if err != nil {
		return src
	}
	return string(b)
}

// ===== 输出 =====

func (g *Gen) line(format string, args ...any) {
	g.w.WriteString(strings.Repeat("\t", g.ind))
	if len(args) > 0 {
		fmt.Fprintf(g.w, format, args...)
	} else {
		g.w.WriteString(format)
	}
	g.w.WriteByte('\n')
}

func (g *Gen) raw(s string) { g.w.WriteString(s) }

func (g *Gen) pos(p Pos) {
	if g.Options.NoLines || p.File == nil || p.Line <= 0 {
		return
	}
	if p.File == g.curFile && p.Line == g.lastLine {
		return
	}
	g.curFile = p.File
	g.lastLine = p.Line
	path, ok := g.srcPath[p.File]
	if !ok {
		path = p.File.Path
		if path == "" {
			path = p.File.Name
		}
		g.srcPath[p.File] = path
	}
	fmt.Fprintf(g.w, "//line %s:%d\n", path, p.Line)
}

// posAgain 再次输出当前位置的 //line 指令（用于多行生成代码中的后续行）。
func (g *Gen) posAgain(p Pos) {
	g.lastLine = 0
	g.pos(p)
}

// resetLine 让之后生成的辅助代码不再映射到 .ae 源文件。
func (g *Gen) resetLine() {
	if g.Options.NoLines || g.curFile == nil {
		return
	}
	g.curFile = nil
	g.lastLine = 0
	g.w.WriteString("//line aether-generated.go:1\n")
}

func (g *Gen) tmp() string {
	g.tmpN++
	return "ꞏ" + strconv.Itoa(g.tmpN)
}

func (g *Gen) pkg(name string) string {
	path, ok := knownPkgs[name]
	if !ok {
		path = name
	}
	g.imports[path] = true
	return name
}

func (g *Gen) rt() string { return g.pkg("rt") }

// ===== 命名 =====

func (g *Gen) alloc(name string) string {
	if goReserved[name] {
		name += "ꞏ"
	}
	base := name
	for i := 1; g.used[name]; i++ {
		name = base + "ꞏ" + strconv.Itoa(i)
	}
	g.used[name] = true
	return name
}

func modPrefix(m *Module) string {
	if m.IsMain || m.IsPrelude {
		return ""
	}
	return strings.ReplaceAll(m.Name, ".", "ꞏ") + "ꞏꞏ"
}

func memberName(name string) string {
	if goKeywords[name] || memberReserved[name] {
		return name + "ꞏ"
	}
	return name
}

func (g *Gen) assignNames() {
	// 先为 prelude 分配，再为主模块与其他模块
	order := append([]*Module(nil), g.c.Mods...)
	sort.SliceStable(order, func(i, j int) bool {
		ri, rj := 2, 2
		if order[i].IsPrelude {
			ri = 0
		} else if order[i].IsMain {
			ri = 1
		}
		if order[j].IsPrelude {
			rj = 0
		} else if order[j].IsMain {
			rj = 1
		}
		return ri < rj
	})
	for _, m := range order {
		pre := modPrefix(m)
		for _, td := range m.Types {
			td.GoName = g.alloc(pre + td.Name)
		}
	}
	for _, m := range order {
		pre := modPrefix(m)
		for _, fd := range m.Funcs {
			if fd.Sym != nil {
				if fd.GoExtern != "" {
					continue
				}
				fd.Sym.GoName = g.alloc(pre + fd.Name)
			}
		}
		for _, sym := range m.Globals {
			if sym.GoName == "" {
				if cd, ok := sym.DeclStmt.(*ConstDecl); ok && cd.GoName != "" {
					continue
				}
				sym.GoName = g.alloc(pre + sym.Name)
			}
		}
	}
	for _, td := range g.c.Types {
		for _, f := range td.Statics {
			f.GoName = g.alloc(td.GoName + "ꞏ" + f.Name)
			if f.Sym != nil {
				f.Sym.GoName = f.GoName
			}
		}
		for _, m := range td.MethodList {
			switch {
			case m.Static:
				m.GoName = g.alloc(td.GoName + "ꞏ" + m.Name)
			case len(m.Sig.TParams) > 0 || td.Kind == DEnum && td.ADT:
				m.GoName = g.alloc(td.GoName + "ꞏ" + m.Name)
			case m.Name == "__str__":
				m.GoName = "String"
			case m.Name == "__init__":
				m.GoName = "ꞏinit"
			default:
				m.GoName = memberName(m.Name)
			}
		}
		for _, f := range td.Fields {
			f.GoName = memberName(f.Name)
		}
		for _, v := range td.Variants {
			if td.ADT {
				v.GoName = g.alloc(td.GoName + "ꞏ" + v.Name)
			} else {
				v.GoName = g.alloc(td.GoName + "ꞏ" + v.Name)
			}
			for _, f := range v.Fields {
				f.GoName = memberName(f.Name)
			}
		}
		for _, p := range td.PropList {
			p.GoName = memberName(p.Name)
		}
	}
	for _, m := range g.c.Mods {
		for _, x := range m.Exts {
			for _, fd := range x.Decl.Methods {
				if fd.Method != nil && fd.GoExtern == "" {
					fd.Method.GoName = g.alloc(modPrefix(m) + "extꞏ" + sanitize(x.Key) + "ꞏ" + fd.Name)
				}
			}
			for _, p := range x.Props {
				if p.Extern == "" {
					p.GoName = g.alloc(modPrefix(m) + "extꞏ" + sanitize(x.Key) + "ꞏget_" + p.Name)
				}
			}
		}
	}
}

func sanitize(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r > 127 {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('ꞏ')
		}
	}
	return sb.String()
}

func (g *Gen) collectIfaceProps() {
	for _, td := range g.c.Types {
		if td.Kind == DInterface {
			for _, p := range td.PropList {
				g.ifaceProps[p.Name] = true
			}
		}
	}
}

// local 返回局部变量的 Go 名字。
func (g *Gen) local(sym *Symbol) string {
	if sym.GoName != "" {
		return sym.GoName
	}
	if sym.IsSelf {
		sym.GoName = "self"
		return "self"
	}
	if sym.Name == "_" {
		return "_"
	}
	name := sym.Name
	if goReserved[name] || g.used[name] || name == "self" {
		name += "ꞏ"
	}
	sym.GoName = name
	return name
}

func (g *Gen) symName(sym *Symbol) string {
	if sym.Global {
		if sym.GoName == "" {
			if cd, ok := sym.DeclStmt.(*ConstDecl); ok && cd.GoName != "" {
				return g.externRef(cd.GoName)
			}
			if sym.Field != nil {
				return sym.Field.GoName
			}
			sym.GoName = g.alloc(modPrefix(sym.Mod) + sym.Name)
		}
		return sym.GoName
	}
	if sym.Kind == SFunc && sym.Func != nil {
		if sym.Func.GoExtern != "" {
			return g.externName(sym.Func.GoExtern)
		}
		return sym.GoName
	}
	return g.local(sym)
}

// externName 解析 @go("pkg.Name") 形式的名字并登记导入。
func (g *Gen) externName(spec string) string {
	if i := strings.LastIndex(spec, "."); i > 0 {
		path := spec[:i]
		name := spec[i+1:]
		if strings.Contains(path, "/") {
			alias := pathBase(path)
			g.imports[path] = true
			return alias + "." + name
		}
		return g.pkg(path) + "." + name
	}
	return spec
}

// externRef 返回 @go 常量引用的 Go 表达式。
func (g *Gen) externRef(spec string) string {
	if strings.ContainsAny(spec, "()* +-$") {
		g.importsIn(spec)
		return spec
	}
	return g.externName(spec)
}

var tmplPkgRe = regexp.MustCompile(`\b([a-z][a-z0-9]*)\.[A-Za-z_]`)

// importsIn 登记模板中引用到的包。
func (g *Gen) importsIn(s string) {
	for _, m := range tmplPkgRe.FindAllStringSubmatch(s, -1) {
		if _, ok := knownPkgs[m[1]]; ok {
			g.pkg(m[1])
		}
	}
}

func (g *Gen) tpName(tp *TypeParam) string {
	if n, ok := g.tpNames[tp]; ok {
		return n
	}
	n := tp.Name
	if goReserved[n] || g.used[n] {
		n += "ꞏ"
	}
	g.tpNames[tp] = n
	return n
}

// ===== 类型 =====

func (g *Gen) typ(t Type) string {
	t = subst(t, g.subst)
	switch x := t.(type) {
	case *Basic:
		switch x.Kind {
		case BInt:
			return "int"
		case BI8:
			return "int8"
		case BI16:
			return "int16"
		case BI32:
			return "int32"
		case BU8:
			return "uint8"
		case BU16:
			return "uint16"
		case BU32:
			return "uint32"
		case BU64:
			return "uint64"
		case BFloat:
			return "float64"
		case BF32:
			return "float32"
		case BStr:
			return "string"
		case BChar:
			return g.rt() + ".Char"
		case BBool:
			return "bool"
		case BAny, BNull:
			return "any"
		case BVoid:
			return "struct{}"
		}
		return "any"
	case *ListT:
		return "*" + g.rt() + ".List[" + g.typ(x.Elem) + "]"
	case *DictT:
		return "*" + g.rt() + ".Dict[" + g.typ(x.Key) + ", " + g.typ(x.Val) + "]"
	case *SetT:
		return "*" + g.rt() + ".Set[" + g.typ(x.Elem) + "]"
	case *TupleT:
		var parts []string
		for _, e := range x.Elems {
			parts = append(parts, g.typ(e))
		}
		return fmt.Sprintf("%s.T%d[%s]", g.rt(), len(x.Elems), strings.Join(parts, ", "))
	case *OptT:
		return g.rt() + ".Opt[" + g.typ(x.Elem) + "]"
	case *FuncT:
		return g.funcType(x)
	case *ChanT:
		return "chan " + g.typ(x.Elem)
	case *IterT:
		return "func(func(" + g.typ(x.Elem) + ") bool)"
	case *NamedT:
		return g.namedType(x)
	case *TParamT:
		return g.tpName(x.P)
	}
	return "any"
}

func (g *Gen) funcType(x *FuncT) string {
	var ps []string
	for _, p := range x.Params {
		ps = append(ps, g.typ(p))
	}
	s := "func(" + strings.Join(ps, ", ") + ")"
	if x.Ret != nil && !isVoid(x.Ret) {
		s += " " + g.typ(x.Ret)
	}
	return s
}

func (g *Gen) targs(args []Type) string {
	if len(args) == 0 {
		return ""
	}
	var parts []string
	for _, a := range args {
		parts = append(parts, g.typ(a))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func (g *Gen) namedType(x *NamedT) string {
	d := x.D
	switch d.Kind {
	case DExtern:
		return g.externType(d.GoType, x.Args)
	case DClass:
		if d.Poly() {
			return d.GoName + "ꞏI" + g.targs(x.Args)
		}
		return "*" + d.GoName + g.targs(x.Args)
	}
	return d.GoName + g.targs(x.Args)
}

func (g *Gen) externType(tmpl string, args []Type) string {
	s := tmpl
	for i := len(args); i >= 1; i-- {
		s = strings.ReplaceAll(s, "$T"+strconv.Itoa(i), g.typ(args[i-1]))
	}
	g.importsIn(s)
	if !strings.Contains(tmpl, "$") && len(args) > 0 {
		s += g.targs(args)
	}
	return s
}

// structType 返回类/结构体的 Go 结构体类型名（不含指针）。
func (g *Gen) structType(nt *NamedT) string {
	return nt.D.GoName + g.targs(nt.Args)
}

// tparamDecl 生成类型参数声明 [T any, K comparable]。
func (g *Gen) tparamDecl(tps []*TypeParam) string {
	if len(tps) == 0 {
		return ""
	}
	var parts []string
	for _, tp := range tps {
		parts = append(parts, g.tpName(tp)+" "+g.constraint(tp))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func (g *Gen) constraint(tp *TypeParam) string {
	switch tp.Bound {
	case BoundHashable:
		return "comparable"
	case BoundOrdered:
		return g.pkg("cmp") + ".Ordered"
	case BoundNumber:
		return g.rt() + ".Number"
	case BoundInteger:
		return g.rt() + ".Integer"
	case BoundIface:
		return g.typ(tp.Iface)
	}
	return "any"
}

func tparamUse(g *Gen, tps []*TypeParam) string {
	if len(tps) == 0 {
		return ""
	}
	var parts []string
	for _, tp := range tps {
		parts = append(parts, g.tpName(tp))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// isPolyType 判断类型在 Go 中是否以接口表示。
func isPolyType(t Type) bool {
	if n, ok := t.(*NamedT); ok {
		return n.D.Kind == DClass && n.D.Poly() || n.D.Kind == DInterface || (n.D.Kind == DEnum && n.D.ADT)
	}
	return false
}

// goEqOK 判断能否直接用 Go 的 == 比较该类型的值（且语义与 Aether 一致）。
func (g *Gen) goEqOK(t Type) bool {
	t = subst(t, g.subst)
	switch x := t.(type) {
	case *Basic:
		return x.Kind != BAny && x.Kind != BNull
	case *OptT:
		return g.goEqOK(x.Elem)
	case *TupleT:
		for _, e := range x.Elems {
			if !g.goEqOK(e) {
				return false
			}
		}
		return true
	case *ChanT:
		return true
	case *NamedT:
		d := x.D
		switch d.Kind {
		case DClass:
			mem, _, _ := g.c.findMember(x, "__eq__")
			return mem == nil
		case DInterface:
			return true
		case DExtern:
			return strings.HasPrefix(d.GoType, "*")
		case DEnum:
			if !d.ADT {
				return true
			}
			return false
		case DStruct:
			if d.Methods["__eq__"] != nil {
				return false
			}
			m := makeSubst(d.TParams, x.Args)
			for _, f := range d.Fields {
				if !g.goEqOK(subst(f.T, m)) {
					return false
				}
			}
			return true
		}
	case *TParamT:
		return x.P.Bound == BoundHashable || x.P.Bound == BoundOrdered || x.P.Bound == BoundNumber || x.P.Bound == BoundInteger
	}
	return false
}

// ===== 程序组装 =====

func (g *Gen) genGlobals() {
	w := &g.globals
	var vars, consts []string
	for _, m := range g.c.Mods {
		for _, sym := range m.Globals {
			if sym.T == nil || isInvalid(sym.T) {
				continue
			}
			if cd, ok := sym.DeclStmt.(*ConstDecl); ok && cd.GoName != "" {
				continue
			}
			name := g.symName(sym)
			if sym.Kind == SConst && sym.GoConst {
				if cd, ok := sym.DeclStmt.(*ConstDecl); ok {
					saveW := g.w
					var b strings.Builder
					g.w = &b
					g.inConst = true
					val := g.expr(cd.Value)
					g.inConst = false
					g.w = saveW
					consts = append(consts, fmt.Sprintf("const %s %s = %s", name, g.typ(sym.T), val))
					continue
				}
			}
			vars = append(vars, fmt.Sprintf("var %s %s", name, g.typ(sym.T)))
		}
	}
	for _, td := range g.c.Types {
		for _, f := range td.Statics {
			if f.T == nil {
				continue
			}
			vars = append(vars, fmt.Sprintf("var %s %s", f.GoName, g.typ(f.T)))
		}
	}
	for _, s := range consts {
		w.WriteString(s + "\n")
	}
	for _, s := range vars {
		w.WriteString(s + "\n")
	}
	if len(consts)+len(vars) > 0 {
		w.WriteString("\n")
	}
}

func initName(m *Module) string {
	return "ꞏinit_" + sanitize(m.Name)
}

func (g *Gen) genModuleInit(m *Module) {
	g.w = &g.decls
	g.line("func %s() {", initName(m))
	g.ind++
	fn := m.InitFn
	g.fn = &genFn{ctx: fn, retT: tVoid, needFlow: fn.NeedFlow}
	g.subst = nil
	g.self = nil
	g.curFile = nil
	// 静态字段初始化
	for _, td := range m.Types {
		var statics []*Field
		for _, f := range td.Statics {
			statics = append(statics, f)
		}
		sort.Slice(statics, func(i, j int) bool { return statics[i].Pos.Line < statics[j].Pos.Line })
		for _, f := range statics {
			if f.Default != nil {
				g.pos(f.Pos)
				g.line("%s = %s", f.GoName, g.exprTo(f.Default, f.T))
			}
		}
	}
	g.funcPrologue(fn)
	g.stmts(m.TopStmts)
	g.ind--
	g.line("}")
	g.resetLine()
	g.line("")
	g.fn = nil
}

func (g *Gen) genHooks() {
	g.w = &g.decls
	g.resetLine()
	exc := g.c.excTD
	if exc == nil {
		return
	}
	rt := g.rt()
	g.line("func ꞏexc_convert(e *%s.Error) any {", rt)
	g.ind++
	g.line("switch e.Kind {")
	byName := map[string]*TypeDecl{}
	for _, td := range g.c.Prelude.Types {
		if td.IsExc && !td.Abstract && td.Kind == DClass {
			byName[td.Name] = td
		}
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		td := byName[n]
		g.line("case %q:", n)
		g.line("\treturn %sꞏnew(e.Msg)", td.GoName)
	}
	g.line("}")
	if rtErr := byName["RuntimeError"]; rtErr != nil {
		g.line("return %sꞏnew(e.Kind + \": \" + e.Msg)", rtErr.GoName)
	} else {
		g.line("return %sꞏnew(e.Msg)", exc.GoName)
	}
	g.ind--
	g.line("}")
	g.line("")
	g.line("func ꞏexc_describe(x any) (string, string) {")
	g.line("\tif e, ok := x.(%sꞏI); ok {", exc.GoName)
	g.line("\t\treturn e.ExcNameꞏ(), e.String()")
	g.line("\t}")
	g.line("\treturn \"Exception\", %s.Str(x)", rt)
	g.line("}")
	g.line("")
	g.line("func ꞏexc_is(x any) bool {")
	g.line("\t_, ok := x.(%sꞏI)", exc.GoName)
	g.line("\treturn ok")
	g.line("}")
	g.line("")
}

func (g *Gen) genMain() {
	g.w = &g.decls
	g.resetLine()
	rt := g.rt()
	g.line("func main() {")
	g.ind++
	if g.c.excTD != nil {
		g.line("%s.ConvertHook = ꞏexc_convert", rt)
		g.line("%s.DescribeHook = ꞏexc_describe", rt)
		g.line("%s.IsExcHook = ꞏexc_is", rt)
	}
	g.line("%s.SetArgs(%s.Args[1:])", rt, g.pkg("os"))
	if mm := g.c.Main; mm != nil && mm.Src != nil {
		g.line("%s.SetScript(%q)", rt, mm.Src.Name)
	}
	inits := func() {
		for _, m := range g.c.Mods {
			g.line("%s()", initName(m))
		}
	}
	if g.testMode {
		g.line("%s.RunTests(func() {", rt)
		g.ind++
		inits()
		g.ind--
		g.line("}, []%s.TestCase{", rt)
		g.ind++
		for _, m := range g.c.Mods {
			if !m.IsMain {
				continue
			}
			for i, t := range m.Tests {
				g.line("{Name: %q, File: %q, Line: %d, Fn: ꞏtest%d},", t.Name, t.Pos.File.Name, t.Pos.Line, i+1)
			}
		}
		g.ind--
		g.line("})")
	} else {
		g.line("%s.Run(func() {", rt)
		g.ind++
		inits()
		if mm := g.c.Main; mm != nil && mm.MainFn != nil {
			g.line("%s()", mm.MainFn.GoName)
		}
		g.ind--
		g.line("})")
	}
	g.ind--
	g.line("}")
}

func (g *Gen) genTests() {
	for _, m := range g.c.Mods {
		if !m.IsMain {
			continue
		}
		for i, t := range m.Tests {
			g.w = &g.decls
			g.line("func ꞏtest%d() {", i+1)
			g.ind++
			g.fn = &genFn{ctx: t.Fn, retT: tVoid, needFlow: t.Fn != nil && t.Fn.NeedFlow}
			g.subst = nil
			g.self = nil
			if t.Fn != nil {
				g.funcPrologue(t.Fn)
			}
			g.stmts(t.Body)
			g.ind--
			g.line("}")
			g.resetLine()
			g.line("")
			g.fn = nil
		}
	}
}

func displayName(f *SourceFile) string {
	if f == nil {
		return "?"
	}
	return filepath.Base(f.Name)
}
