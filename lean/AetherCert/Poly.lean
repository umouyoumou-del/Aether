/-
  AetherCert.Poly —— 多项式：单项式的带系数和

  ## 表示

  `Poly = List (Monom × Rat)`，语义是「各单项式乘上系数，再全加起来」。

  不要求规范化（不去零、不合并同类项、不排序）。原因见 `Norm.lean` 文件头：
  checker 判两边相等靠的是「两边各跑一遍同一个 `norm`，再比结果」，
  所以 `norm` 的**输出**不必是唯一规范形；只要 `norm` 保持语义即可。

  这样一来，本文件要证的东西就只剩「拼接/相乘/求和」的语义相容性，
  全部依赖 `Monom.lean` 与 `Basic.lean` 里已经证好的引理。
-/
import AetherCert.Monom

namespace AetherCert

/-- 多项式：单项式与系数。 -/
abbrev Poly := List (Monom × Rat)

/-- 多项式的语义：系数乘单项式，再求和。 -/
def polyDenote (Φ : Phi) (σ : Env) (p : Poly) : Rat :=
  (p.map (fun q => q.2 * monomDenote Φ σ q.1)).sum

@[simp] theorem polyDenote_nil (Φ : Phi) (σ : Env) :
    polyDenote Φ σ [] = 0 := by
  simp only [polyDenote, List.map_nil, List.sum_nil]

/-- 单一项构成的多项式。语义就是「系数 × 单项式」。 -/
def polySingle (m : Monom) (c : Rat) : Poly := [(m, c)]

@[simp] theorem polyDenote_single (Φ : Phi) (σ : Env) (m : Monom) (c : Rat) :
    polyDenote Φ σ (polySingle m c) = c * monomDenote Φ σ m := by
  simp only [polyDenote, polySingle, List.map_cons, List.map_nil, List.sum_cons,
    List.sum_nil, Rat.add_zero]

/-- 多项式相加 = 拼接。 -/
theorem polyDenote_append (Φ : Phi) (σ : Env) (p₁ p₂ : Poly) :
    polyDenote Φ σ (p₁ ++ p₂) = polyDenote Φ σ p₁ + polyDenote Φ σ p₂ := by
  simp only [polyDenote, List.map_append, sum_append]

/-- 多项式乘一个标量：系数全乘。 -/
def polyScale (c : Rat) (p : Poly) : Poly :=
  p.map (fun q => (q.1, c * q.2))

theorem polyDenote_scale (Φ : Phi) (σ : Env) (c : Rat) (p : Poly) :
    polyDenote Φ σ (polyScale c p) = c * polyDenote Φ σ p := by
  unfold polyScale polyDenote
  rw [List.map_map]
  rw [sum_map_congr (f := (fun q : Monom × Rat => q.2 * monomDenote Φ σ q.1) ∘
                          fun q : Monom × Rat => (q.1, c * q.2))
        (g := fun q : Monom × Rat => c * (q.2 * monomDenote Φ σ q.1))]
  · exact sum_map_const_mul c (fun q => q.2 * monomDenote Φ σ q.1) p
  · intro q _
    simp only [Function.comp]
    exact Rat.mul_assoc c q.2 (monomDenote Φ σ q.1)

/-- 每项都拼上同一个单项式：`p * m`（右乘）。 -/
def polyMulMonom (p : Poly) (m : Monom) : Poly :=
  p.map (fun q => (q.1 ++ m, q.2))

theorem polyDenote_mulMonom (Φ : Phi) (σ : Env) (p : Poly) (m : Monom) :
    polyDenote Φ σ (polyMulMonom p m) = polyDenote Φ σ p * monomDenote Φ σ m := by
  unfold polyMulMonom polyDenote
  rw [List.map_map]
  rw [sum_map_congr (f := (fun q : Monom × Rat => q.2 * monomDenote Φ σ q.1) ∘
                          fun q : Monom × Rat => (q.1 ++ m, q.2))
        (g := fun q : Monom × Rat => q.2 * monomDenote Φ σ q.1 * monomDenote Φ σ m)]
  · exact sum_map_mul_const (fun q => q.2 * monomDenote Φ σ q.1) (monomDenote Φ σ m) p
  · intro q _
    simp only [Function.comp, monomDenote_append]
    exact (Rat.mul_assoc q.2 (monomDenote Φ σ q.1) (monomDenote Φ σ m)).symm

end AetherCert
