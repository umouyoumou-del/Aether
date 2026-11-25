/-
  AetherCert.Deriv —— 形式导数

  ## 求导这一类步为什么特殊

  化简与展开的每一步，验法都是「两边标准形相同」，`Sort.lean` 已经把这件事
  证到无假设。求导不行：`d/dx (x²) = 2x` 两边**不**恒等，硬判等只会失败。

  更麻烦的是求导最典型的错误恰好不改变「形状相似度」：
  漏乘链式法则的内层导数、`cos` 的符号写错、商法则分子顺序写反。
  这类错误必须被抓住，而它们抓不住就等于没验。

  ## 采取的办法：Lean 自己重算一遍

  `diff` 步不验「两边相等」，而是验：

      给出的结果  ≡  内核自己算出的导数      （标准形意义下相同）

  也就是 Lean 侧实现一份形式导数 `deriv`，把上一项代进去、自己算一遍，
  再把算出的结果与证书声称的结果都过 `canonical` 判等。

  这样 `diff` 步就**归约成 `same` 步**：`Sort.lean` 的 `eq_of_canonical_eq`
  直接复用，一行新定理都不需要。抓错能力完全来自「内核算的和证书写的不一样」，
  与两边长得像不像无关。

  ## 信任边界（唯一一处，写在这里）

  `Term.fn f u` 的语义是 `Φ f (eval u)`，`Φ` 在证书里是**任意**函数
  （见 `Term.eval`）。于是「`sin` 的导数是 `cos`」无法从语义推出，
  它是下面 `dtable` 这张表**声明**的：

      sin' = cos         cos' = -sin        tan'  = 1/cos²
      exp' = exp         log' = 1/u         sqrt' = 1/(2√u)
      sinh' = cosh       cosh' = sinh       tanh' = 1 - tanh²
      atan' = 1/(1+u²)   asin' = 1/√(1-u²)  acos' = -1/√(1-u²)

  这张表就是这个系统里唯一的数学假设。九行、可逐行目视检查、与
  `assets/lib/sym.ae` 的 `_fn_derivative` 一一对应。

  对比一下两种做法的可信度差异：
  与其把「导数正确」塞进「两边恒等」里含混过关（那是真的没验），
  不如把假设摊开成一页纸。**知道自己在假设什么，比多证几条定理更重要。**
-/
import AetherCert.Norm
import AetherCert.Sort

namespace AetherCert

namespace Term

/-- 单一占位变量名。导数表里的 `_` 表示「函数的自变量」。 -/
def placeholder : String := "_"

/-- 把 `body` 里的占位变量换成 `arg`。导数表用它把 `cos _` 变成 `cos u`。 -/
def subst1 (body : Term) (arg : Term) : Term :=
  match body with
  | .var s => if s = placeholder then arg else .var s
  | .num q => .num q
  | .add a b => .add (a.subst1 arg) (b.subst1 arg)
  | .mul a b => .mul (a.subst1 arg) (b.subst1 arg)
  | .pow a k => .pow (a.subst1 arg) k
  | .neg a => .neg (a.subst1 arg)
  | .div a b => .div (a.subst1 arg) (b.subst1 arg)
  | .fn f a => .fn f (a.subst1 arg)

/-- 整数转有理数系数（`Rat.divInt` 是 core 里现成的构造）。 -/
def ratOfInt (k : Int) : Rat := Rat.divInt k 1

/-- 有理数常量助手。 -/
def c (n : Int) : Term := .num (ratOfInt n)

/-- 占位变量。 -/
def ph : Term := .var placeholder

end Term

open Term

/-- 函数导数表。`none` 表示「这个函数没有导数规则」，对应 `diff` 步直接失败 ——
    证书里写不出来的东西不能被声称证明。 -/
def dtable (f : String) : Option Term :=
  match f with
  | "sin" => some (.fn "cos" ph)
  | "cos" => some (.neg (.fn "sin" ph))
  | "tan" => some (.div (c 1) (.pow (.fn "cos" ph) 2))
  | "exp" => some (.fn "exp" ph)
  | "log" => some (.div (c 1) ph)
  | "sqrt" => some (.div (c 1) (.mul (c 2) (.fn "sqrt" ph)))
  | "sinh" => some (.fn "cosh" ph)
  | "cosh" => some (.fn "sinh" ph)
  | "tanh" => some (.add (c 1) (.neg (.pow (.fn "tanh" ph) 2)))
  | "atan" => some (.div (c 1) (.add (c 1) (.pow ph 2)))
  | "asin" => some (.div (c 1) (.fn "sqrt" (.add (c 1) (.neg (.pow ph 2)))))
  | "acos" => some (.neg (.div (c 1) (.fn "sqrt" (.add (c 1) (.neg (.pow ph 2))))))
  | _ => none

/-- 形式导数。`none` 只在「遇到没有导数规则的函数」时出现 —— 这是唯一可能
    失败的情形，其余分支都是构造性的。 -/
def deriv (x : String) : Term → Option Term
  | .num _ => some (c 0)
  | .var s => some (if s = x then c 1 else c 0)
  | .add a b => do
      let da ← deriv x a
      let db ← deriv x b
      some (.add da db)
  | .mul a b => do
      let da ← deriv x a
      let db ← deriv x b
      some (.add (.mul da b) (.mul a db))
  | .neg a => do
      let da ← deriv x a
      some (.neg da)
  | .pow a k =>
      if k = 0 then some (c 0)
      else do
        let da ← deriv x a
        some (.mul (.mul (.num (ratOfInt k)) (.pow a (k - 1))) da)
  | .div a b => do
      let da ← deriv x a
      let db ← deriv x b
      some (.div (.add (.mul da b) (.neg (.mul a db))) (.mul b b))
  | .fn f a => do
      let rule ← dtable f
      let da ← deriv x a
      some (.mul (rule.subst1 a) da)

end AetherCert