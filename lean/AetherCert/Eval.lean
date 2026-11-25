/-
  AetherCert.Eval —— 证书项的语义

  ## 为什么语义要参数化

  `Term.fn f a` 是「把函数符号 `f` 作用到 `a`」。证书不打算规定 `sin` 到底是
  哪个函数 —— 那是 Aether 的事，Lean 不必采信。这里只把解释 `Φ : String → Rat → Rat`
  当作**任意**参数。

  于是所有定理都是 `∀ Φ σ, ...` 形式：不论你把 `sin` 解释成正弦、解释成常数、
  还是解释成胡说八道，只要两边多项式恒等，等式就成立。

  这样做的收益是证书的**可信性不依赖任何函数库**：checker 不需要知道 `sin` 的
  级数展开，也永远不会因为「函数语义没对齐」而误判。

  ## 同样的道理用于原子

  一个项如果不是四则运算，就整块当作**原子**参与多项式运算（见 `Norm.lean`）。
  原子在 `monomDenote` 里被求值成 `Term.eval Φ σ t`，两边同源，于是证明不需要
  知道它的值。
-/
import AetherCert.Term
import AetherCert.Basic

namespace AetherCert

/-- 函数符号的解释。证书不固定它，checker 对任意解释都成立。 -/
abbrev Phi := String → Rat → Rat

/-- 变量赋值。 -/
abbrev Env := String → Rat

namespace Term

/-- 项的语义。

    `pow` 的指数是 `Int`：非负走 `Nat` 幂，负走倒数。
    注意 `0 ^ (-1) = 0⁻¹ = 0`，所以负指数**不满足** `x^(m+n) = x^m * x^n`
    （当 `x = 0`）。规范化器因此把负指数整块当原子处理，见 `Norm.normPow`。 -/
def eval (Φ : Phi) (σ : Env) : Term → Rat
  | .num q => q
  | .var s => σ s
  | .add a b => eval Φ σ a + eval Φ σ b
  | .mul a b => eval Φ σ a * eval Φ σ b
  | .pow a k =>
      if 0 ≤ k then eval Φ σ a ^ k.toNat else (eval Φ σ a ^ (-k).toNat)⁻¹
  | .neg a => - eval Φ σ a
  | .div a b => eval Φ σ a / eval Φ σ b
  | .fn f a => Φ f (eval Φ σ a)

end Term

end AetherCert