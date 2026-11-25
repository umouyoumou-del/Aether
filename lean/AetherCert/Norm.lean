/-
  AetherCert.Norm —— 把项规范化成多项式，并证明「规范化保持语义」

  ## 一条定理就够了

  证书要判的是 `lhs = rhs`。checker 的做法是：

      norm lhs == norm rhs  ⟹  lhs = rhs

  这个推理的**全部**依据是下面这条可靠性定理：

      polyDenote Φ σ (norm t) = Term.eval Φ σ t        -- norm_sound

  它说明 `norm` 只是换了种写法，没改值。于是

      Term.eval lhs
        = polyDenote (norm lhs)      -- norm_sound
        = polyDenote (norm rhs)      -- norm lhs = norm rhs（结构相等）
        = Term.eval rhs              -- norm_sound

  注意这里**没有**要求 `norm` 输出唯一规范形。`norm` 不合并同类项、不排序、
  不去零项都无所谓 —— 那些只影响「两份不同写法的相等式子能不能被判成相等」
  （**完备性**），不影响「判成相等时是否真相等」（**可靠性**）。

  这个取舍把证明规模压小了一个量级：不必证「规范形唯一」，只证「保持语义」。

  ## 处理不了的项怎么办

  不是四则运算的项（`fn`、负指数 `pow`、`div`）整块当**原子**。原子在
  `monomDenote` 里按原样求值，所以 `norm_sound` 对它们照样成立 —— 证书不需要
  理解 `sin`，只需要知道「同一个 `sin(x)` 前后是同一个值」。

  代价是**完备性**受限：`sin(x)^2 + cos(x)^2 = 1` 判不出来（两边成了不同的
  原子）。这是刻意的 —— 要判它就得知道三角恒等式，那就得引 Mathlib，而
  checker 的可信性正是靠「什么都不假设」换来的。
-/
import AetherCert.Poly

namespace AetherCert

/-! ### 多项式运算 -/

/-- 剔除系数为 0 的项。

    这不是纯粹的「洁癖」：求导会产生 `x * 0` 这类零系数项，它们会让
    `(1*2) + -(x*0)` 变成两块多项式 `[((),2), ([x],0)]`，从而匹配不上
    `normDiv` 的「纯常数」形状，导致 `d/dx (x/2) = 1/2` 被误拒。
    顺手也让 `x + 0 = x` 这类式子判得出来。 -/
def stripZero : Poly → Poly
  | [] => []
  | q :: rest => if q.2 = 0 then stripZero rest else q :: stripZero rest

/-- 剔除零项保持语义：被扔掉的项系数是 0，对和的贡献本来就是 0。 -/
theorem polyDenote_stripZero (Φ : Phi) (σ : Env) (p : Poly) :
    polyDenote Φ σ (stripZero p) = polyDenote Φ σ p := by
  induction p with
  | nil => rfl
  | cons q rest ih =>
    unfold stripZero
    by_cases hq : q.2 = 0
    · rw [if_pos hq]
      have hr : polyDenote Φ σ (q :: rest) = polyDenote Φ σ rest := by
        simp only [polyDenote, List.map_cons, List.sum_cons, hq, Rat.zero_mul,
          Rat.zero_add]
      rw [hr, ih]
    · rw [if_neg hq]
      have ih' : (List.map (fun q : Monom × Rat => q.2 * monomDenote Φ σ q.1)
            (stripZero rest)).sum
          = (List.map (fun q : Monom × Rat => q.2 * monomDenote Φ σ q.1) rest).sum := ih
      simp only [polyDenote, List.map_cons, List.sum_cons]
      rw [ih']

/-- 多项式加法。顺手剔除零系数项，见 `stripZero`。 -/
def polyAdd (p₁ p₂ : Poly) : Poly := stripZero (p₁ ++ p₂)

theorem polyDenote_add (Φ : Phi) (σ : Env) (p₁ p₂ : Poly) :
    polyDenote Φ σ (polyAdd p₁ p₂) = polyDenote Φ σ p₁ + polyDenote Φ σ p₂ := by
  unfold polyAdd
  rw [polyDenote_stripZero, polyDenote_append]

/-- 两项相乘：`p₂` 的每一项去乘 `p₁` 的每一项。 -/
def polyMul (p₁ p₂ : Poly) : Poly :=
  p₂.flatMap (fun q => polyMulMonom (polyScale q.2 p₁) q.1)

theorem polyDenote_mul (Φ : Phi) (σ : Env) (p₁ p₂ : Poly) :
    polyDenote Φ σ (polyMul p₁ p₂) = polyDenote Φ σ p₁ * polyDenote Φ σ p₂ := by
  show (List.map (fun q => q.2 * monomDenote Φ σ q.1)
        (p₂.flatMap (fun q => polyMulMonom (polyScale q.2 p₁) q.1))).sum
      = polyDenote Φ σ p₁ * polyDenote Φ σ p₂
  rw [sum_map_flatMap]
  rw [show (List.map
        (fun q : Monom × Rat =>
          (List.map (fun r : Monom × Rat => r.2 * monomDenote Φ σ r.1)
            (polyMulMonom (polyScale q.2 p₁) q.1)).sum) p₂)
      = List.map (fun q : Monom × Rat =>
          q.2 * monomDenote Φ σ q.1 * polyDenote Φ σ p₁) p₂ from by
        apply List.map_congr_left
        intro q _
        show polyDenote Φ σ (polyMulMonom (polyScale q.2 p₁) q.1)
            = q.2 * monomDenote Φ σ q.1 * polyDenote Φ σ p₁
        rw [polyDenote_mulMonom, polyDenote_scale]
        calc q.2 * polyDenote Φ σ p₁ * monomDenote Φ σ q.1
            = q.2 * (polyDenote Φ σ p₁ * monomDenote Φ σ q.1) :=
                Rat.mul_assoc q.2 _ _
          _ = q.2 * (monomDenote Φ σ q.1 * polyDenote Φ σ p₁) := by
                rw [Rat.mul_comm (polyDenote Φ σ p₁) (monomDenote Φ σ q.1)]
          _ = q.2 * monomDenote Φ σ q.1 * polyDenote Φ σ p₁ :=
                (Rat.mul_assoc q.2 _ _).symm]
  rw [sum_map_mul_const (fun q => q.2 * monomDenote Φ σ q.1) (polyDenote Φ σ p₁) p₂]
  exact Rat.mul_comm (polyDenote Φ σ p₂) (polyDenote Φ σ p₁)

/-- 多项式的 `k` 次幂。对 `Nat` 递归，正好对上 `Rat.pow_succ`。 -/
def polyPow (p : Poly) : Nat → Poly
  | 0 => polySingle [] 1
  | k + 1 => polyMul (polyPow p k) p

theorem polyDenote_pow (Φ : Phi) (σ : Env) (p : Poly) (k : Nat) :
    polyDenote Φ σ (polyPow p k) = polyDenote Φ σ p ^ k := by
  induction k with
  | zero => simp only [polyPow, polyDenote_single, monomDenote_nil, Rat.mul_one,
      Rat.pow_zero]
  | succ k ih =>
    simp only [polyPow, polyDenote_mul, ih, Rat.pow_succ]

/-- 多项式取负。 -/
def polyNeg (p : Poly) : Poly := polyScale (-1) p

theorem polyDenote_neg (Φ : Phi) (σ : Env) (p : Poly) :
    polyDenote Φ σ (polyNeg p) = - polyDenote Φ σ p := by
  unfold polyNeg
  rw [polyDenote_scale]
  -- -1 * x = -(1 * x) = -x
  rw [Rat.neg_mul, Rat.one_mul]

/-! ### 商的常数折叠

`norm` 把 `div` 整块当原子，因为一般商不是多项式。但**常数除以常数**是：
`1/2` 应该折成有理数 `1/2`，否则 `d/dx (x/2)` 算出的 `(1*2 + -(x*0))/(2*2)`
就永远化简不到 `1/2`，`diff` 步会误拒正确答案。

这里只折「分子分母都是纯常数」这一种情形，其余仍当原子。折得越多越容易出错，
而恰好这一种是导数表与化简里最常出现、也最容易判断的。 -/

/-- 常数商折叠。`pa`/`pb` 是 `norm a`/`norm b`，由调用方传入以保持结构递归。 -/
def normDiv (a b : Term) (pa pb : Poly) : Poly :=
  match pa, pb with
  | [([], c₁)], [([], c₂)] =>
      if c₂ = 0 then polySingle [Term.div a b] 1
      else polySingle [] (c₁ * c₂⁻¹)
  | _, _ => polySingle [Term.div a b] 1

/-- 单项式为空、系数为 `c` 的单项，其值就是 `c`。 -/
theorem monomDenote_nil_mul (Φ : Phi) (σ : Env) (c : Rat) :
    c * monomDenote Φ σ [] = c := by
  simp only [monomDenote_nil, Rat.mul_one]

/-- 纯常数多项式的值就是它的系数。 -/
@[simp] theorem polyDenote_const (Φ : Phi) (σ : Env) (c : Rat) :
    polyDenote Φ σ [([], c)] = c := by
  unfold polyDenote
  simp only [List.map_cons, List.map_nil, List.sum_cons, List.sum_nil,
    monomDenote_nil_mul, Rat.add_zero]

/-- 常数商折叠的语义：折出来的多项式值仍是「分子 / 分母」。

    注意这条定理的陈述方式：它不引用 `norm`，只要求「`pa`/`pb` 的值分别是
    `a`/`b` 的值」。这样它就可以在 `norm_sound` 之前证出来，不构成循环。 -/
theorem normDiv_sound (Φ : Phi) (σ : Env) (a b : Term) (pa pb : Poly)
    (ha : polyDenote Φ σ pa = Term.eval Φ σ a)
    (hb : polyDenote Φ σ pb = Term.eval Φ σ b) :
    polyDenote Φ σ (normDiv a b pa pb) = Term.eval Φ σ a / Term.eval Φ σ b := by
  unfold normDiv
  split
  · rename_i c₁ c₂
    have ha' : c₁ = Term.eval Φ σ a := by simpa using ha
    have hb' : c₂ = Term.eval Φ σ b := by simpa using hb
    split
    · -- 分母为 0：`a / b` 保持为一个原子
      simp only [polyDenote_single, monomDenote_single, Rat.one_mul, Term.eval]
    · -- 分母非零：折成有理数
      unfold polySingle polyDenote
      simp only [List.map_cons, List.map_nil, List.sum_cons, List.sum_nil,
        monomDenote_nil_mul, Rat.add_zero]
      rw [ha', hb', Rat.div_def]
  · -- 至少一侧不是纯常数：保持原子
    simp only [polyDenote_single, monomDenote_single, Rat.one_mul, Term.eval]

/-! ### 规范化 -/

/-- 把一个项规范化成多项式。

    结构递归，不用 fuel：`Term` 是归纳类型，子项严格变小。
    `normDiv` 接收已经算好的 `norm a`/`norm b`，所以递归不跨函数边界。 -/
def norm : Term → Poly
  | .num q => polySingle [] q
  | t@(.var _) => polySingle [t] 1
  | .add a b => polyAdd (norm a) (norm b)
  | .mul a b => polyMul (norm a) (norm b)
  | .neg a => polyNeg (norm a)
  | .pow a k =>
      if 0 ≤ k then polyPow (norm a) k.toNat
      else polySingle [.pow a k] 1
  | .div a b => normDiv a b (norm a) (norm b)
  | t@(.fn _ _) => polySingle [t] 1

/-! ### 可靠性 -/

/-- `Term.eval` 在非负指数上的取值。 -/
theorem Term.eval_pow_pos {Φ : Phi} {σ : Env} {a : Term} {k : Int} (h : 0 ≤ k) :
    Term.eval Φ σ (.pow a k) = Term.eval Φ σ a ^ k.toNat := by
  simp only [Term.eval, if_pos h]

/-- `Term.eval` 在负指数上的取值。 -/
theorem Term.eval_pow_neg {Φ : Phi} {σ : Env} {a : Term} {k : Int} (h : ¬ 0 ≤ k) :
    Term.eval Φ σ (.pow a k) = (Term.eval Φ σ a ^ (-k).toNat)⁻¹ := by
  simp only [Term.eval, if_neg h]

/-- **可靠性**：规范化不改变语义。

    这是整个 checker 的支点 —— 有了它，「`norm lhs` 与 `norm rhs` 结构相等」
    才能升级成「`lhs` 与 `rhs` 求值相等」。 -/
theorem norm_sound (Φ : Phi) (σ : Env) (t : Term) :
    polyDenote Φ σ (norm t) = Term.eval Φ σ t := by
  induction t with
  | num q => simp only [norm, polyDenote_single, monomDenote_nil, Rat.mul_one, Term.eval]
  | var s => simp only [norm, polyDenote_single, monomDenote_single, Term.eval,
      Rat.one_mul]
  | add a b iha ihb =>
    simp only [norm, polyDenote_add, iha, ihb, Term.eval]
  | mul a b iha ihb =>
    simp only [norm, polyDenote_mul, iha, ihb, Term.eval]
  | neg a iha =>
    simp only [norm, polyDenote_neg, iha, Term.eval]
  | pow a k iha =>
    by_cases h : 0 ≤ k
    · rw [norm, if_pos h, polyDenote_pow, iha, Term.eval_pow_pos h]
    · rw [norm, if_neg h, polyDenote_single, monomDenote_single, Rat.one_mul,
        Term.eval_pow_neg h]
  | div a b iha ihb =>
    show polyDenote Φ σ (normDiv a b (norm a) (norm b)) = Term.eval Φ σ a / Term.eval Φ σ b
    exact normDiv_sound Φ σ a b (norm a) (norm b) iha ihb
  | fn f a _ => simp only [norm, polyDenote_single, monomDenote_single, Rat.one_mul,
      Term.eval]

/-! ### 判等

规范化到同一个多项式，就意味着原项求值相等。这是 checker 的最终判据，
也是「证书导出」能被独立校验的形式化依据。 -/

/-- **判等**：`norm` 结果相同 ⟹ 两项在所有解释下求值相等。 -/
theorem eq_of_norm_eq (Φ : Phi) (σ : Env) {a b : Term}
    (h : norm a = norm b) : Term.eval Φ σ a = Term.eval Φ σ b := by
  rw [← norm_sound Φ σ a, ← norm_sound Φ σ b, h]

/-- 判等的「无解释」形式：不需要指定 `Φ`/`σ`，直接说「恒等」。 -/
theorem ident_of_norm_eq {a b : Term} (h : norm a = norm b) :
    ∀ (Φ : Phi) (σ : Env), Term.eval Φ σ a = Term.eval Φ σ b :=
  fun Φ σ => eq_of_norm_eq Φ σ h

end AetherCert