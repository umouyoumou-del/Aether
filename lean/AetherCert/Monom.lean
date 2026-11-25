/-
  AetherCert.Monom —— 单项式：原子列表

  ## 表示选择

  单项式就是**一串原子**，例如 `x^2 * y` 记作 `[x, x, y]`。

  另一种常见做法是「原子 → 指数」的关联表（`[(x,2), (y,1)]`）。这里没有采用，
  理由是证明成本：关联表要额外证「插入时指数相加」「排序后查表一致」等一堆引理，
  而原子列表把幂直接拆成重复，于是：

    - `x^2 * x^3` 的语义 = `lprod ([x,x] ++ [x,x,x])` = `lprod [x,x] * lprod [x,x,x]`
      —— 只要 `lprod_append`；
    - `(x^2)^3` 的语义 = `lprod (List.flatten [[x,x],[x,x],[x,x]])`
      —— 只要 `lprod_append` + 归纳。

  两条引理都已证。原子列表唯一的问题是长度会膨胀（`x^100` 存 100 个 `x`），
  但证书里的指数受 `MAX_EXPAND_EXPONENT = 64` 约束，膨胀上限是可控的。

  ## 原子是什么

  「原子」= 不再拆分的项。构造多项式时，凡不是 `add`/`mul`/`pow`/`div` 的项都
  整块当原子；`pow` 的指数为负时也整块当原子（负指数不满足 `x^(m+n)=x^m*x^n`，
  见 `Term.eval`）。`div` 在分母非零前提下会转成「乘倒数」，也是原子。
-/
import AetherCert.Basic
import AetherCert.Eval

namespace AetherCert

/-- 单项式：原子的有序列表。 -/
abbrev Monom := List Term

/-- 单项式的语义：把原子逐个相乘。 -/
def monomDenote (Φ : Phi) (σ : Env) (m : Monom) : Rat :=
  lprod (m.map (Term.eval Φ σ))

@[simp] theorem monomDenote_nil (Φ : Phi) (σ : Env) :
    monomDenote Φ σ [] = 1 := rfl

/-- 单项式拼接 = 语义相乘。 -/
theorem monomDenote_append (Φ : Phi) (σ : Env) (m₁ m₂ : Monom) :
    monomDenote Φ σ (m₁ ++ m₂) = monomDenote Φ σ m₁ * monomDenote Φ σ m₂ := by
  simp only [monomDenote, List.map_append, lprod_append]

/-- 单项式的原子可以按任意顺序排列。 -/
theorem monomDenote_perm (Φ : Phi) (σ : Env) {m₁ m₂ : Monom} (h : m₁.Perm m₂) :
    monomDenote Φ σ m₁ = monomDenote Φ σ m₂ :=
  perm_lprod_eq (h.map _)

/-- 单个原子构成的单项式。 -/
def monomOf (t : Term) : Monom := [t]

@[simp] theorem monomDenote_monomOf (Φ : Phi) (σ : Env) (t : Term) :
    monomDenote Φ σ (monomOf t) = Term.eval Φ σ t := by
  simp only [monomOf, monomDenote, List.map_cons, List.map_nil, lprod, Rat.mul_one]

/-- 单原子单项式的语义。 -/
@[simp] theorem monomDenote_single (Φ : Phi) (σ : Env) (t : Term) :
    monomDenote Φ σ [t] = Term.eval Φ σ t := by
  simp only [monomDenote, List.map_cons, List.map_nil, lprod, Rat.mul_one]

/-- 重复一个原子 `k` 次，即「`t^k` 的幂部分」。 -/
def monomRepeat (t : Term) : Nat → Monom
  | 0 => []
  | k + 1 => t :: monomRepeat t k

theorem monomDenote_repeat (Φ : Phi) (σ : Env) (t : Term) (k : Nat) :
    monomDenote Φ σ (monomRepeat t k) = Term.eval Φ σ t ^ k := by
  induction k with
  | zero => simp only [monomRepeat, monomDenote, List.map_nil, lprod, Rat.pow_zero]
  | succ k ih =>
    simp only [monomRepeat, monomDenote, List.map_cons, lprod] at ih ⊢
    rw [ih, Rat.pow_succ, Rat.mul_comm]

end AetherCert