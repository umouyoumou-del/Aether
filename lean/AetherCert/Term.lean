/-
  AetherCert.Term —— 证书里的项语言

  Aether 的 `sym` 库用一棵 `Expr` 树表示数学表达式，节点种类在
  `assets/lib/sym.ae` 的类注释里有完整列表。这里给出证书侧的**重构**：
  只保留 Lean 能给出语义的部分，并且刻意做得比 `sym.Expr` 更窄——
  证书只声明「能证明的东西」，声明不了的一律拒绝。

  与 `sym.Expr` 的对应关系：

    sym.Expr.kind     Term
    ----------------  ---------------------------------------------
    "num"             .num（有理数，精确；浮点常量不入证书）
    "var"             .var（自由变量，名字是字符串）
    "add"             .add（二元，右结合；多元和由 Aether 侧折叠成树）
    "mul"             .mul（同上）
    "pow"             .pow（指数必须是有理数的整数：.num k 且 k 的分母为 1）
    "neg"             .neg（sym 里 neg 是 mul(-1, e)，证书里单列，便于化简）
    "div"             .div（分母非零时才可作为域运算使用）
    "fn"              .fn（只允许有精确导数公式的初等函数，见 Term.fn?）

  不收录：`ufunc` / `df`（不定函数没有语义，无法在 Lean 里证明任何东西）、
  `sum` / `prod` / `lim` / `int`（绑定变量，语义依赖极限与积分理论，
  超出本内核范围）、`matrix` / `cases` / `rel`。

  这个取舍是证书可信性的来源：证书说什么，Lean 就证什么；不在 Term 里的
  东西，证书根本表达不出来，也就不会被「假装证明」。
-/

namespace AetherCert

/-- 证书项。所有运算都在 `Rat` 上，因此每一步变换都能用有理数环/域的
    公理精确验证，不涉及浮点误差。 -/
inductive Term where
  | num (q : Rat)
  | var (name : String)
  | add (a b : Term)
  | mul (a b : Term)
  | pow (a : Term) (k : Int)
  | neg (a : Term)
  | div (a b : Term)
  | fn (name : String) (a : Term)
  deriving Repr, DecidableEq

namespace Term

/-- 允许出现在证书里的初等函数。每个都必须在内核里有对应的导数规则，
    否则 `HasDerivAt` 那一侧会缺一条路。 -/
def fnNames : List String :=
  ["sin", "cos", "tan", "exp", "log", "sqrt",
   "sinh", "cosh", "tanh", "atan", "asin", "acos"]

def isFn (s : String) : Bool := fnNames.contains s

/-- 项的字符串形式，用于报错和调试。语法与 `sym.render` 一致，
    便于把 Lean 的报错和 Aether 的打印对照着看。 -/
def render : Term → String
  | .num q => if q.den = 1 then toString q.num else toString q.num ++ "/" ++ toString q.den
  | .var s => s
  | .add a b => "(" ++ render a ++ " + " ++ render b ++ ")"
  | .mul a b => "(" ++ render a ++ " * " ++ render b ++ ")"
  | .pow a k => "(" ++ render a ++ "^" ++ toString k ++ ")"
  | .neg a => "(-" ++ render a ++ ")"
  | .div a b => "(" ++ render a ++ " / " ++ render b ++ ")"
  | .fn f a => f ++ "(" ++ render a ++ ")"

instance : ToString Term := ⟨render⟩

end Term

end AetherCert