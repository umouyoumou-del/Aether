/-
  AetherCert.Sort —— 排序与合并同类项

  ## 这一层补的是「完备性」

  `Norm.lean` 只保证「规范化保持语义」，但它不排序、不合并，所以
  `x + y` 与 `y + x` 得到的多项式不同，判不出相等。

  这一层补上。关键观察：**排序与合并都不改变「和」这个结构**，只改变
  列表顺序与分组。于是全部证明都落在已证好的

    - `perm_sum_eq` —— 排列不改变求和
    - `perm_lprod_eq` —— 排列不改变求积
    - `List.mergeSort_perm` —— 排序结果是原列表的排列

  上。这里**没有**引入任何新的数学假设。

  ## 排序键

  `Term` 的序由 `Term.render` 的字典序给出。用渲染串而不是结构做键，是为了让
  「谁在前」有一个双方都能独立算出的定义：Aether 侧与 Lean 侧对同一项调用同一
  份渲染规则，必然得出同一个顺序。证书因此不需要交换排序结果，只需交换排序键
  的定义 —— 那已经在两侧的代码里了。
-/
import AetherCert.Norm

namespace AetherCert

/-! ### 排序键 -/

/-- 项的排序键：渲染串的字典序。 -/
def termLt (a b : Term) : Bool :=
  compare a.render b.render == Ordering.lt

/-- 单项式的排序键：逐原子比较渲染串。 -/
def monomLt (a b : Monom) : Bool :=
  compare (a.map Term.render) (b.map Term.render) == Ordering.lt

/-! ### 合并同类项

`groupSame` 把相邻且单项式相同的项加起来。写成「带着累加器往下走」的形式，
这样是结构递归（每次丢掉至少一个元素），不会卡在终止性证明上。 -/

/-- 把 `acc` 加到 `q` 上（要求单项式相同）。 -/
def addCoeff (q acc : Monom × Rat) : Monom × Rat :=
  (q.1, q.2 + acc.2)

/-- 合并相邻同类项：`acc` 是当前正在累积的项，`None` 表示还没开始。 -/
def groupSameAux : List (Monom × Rat) → Option (Monom × Rat) → List (Monom × Rat)
  | [], acc => match acc with
    | none => []
    | some a => [a]
  | q :: rest, none => groupSameAux rest (some q)
  | q :: rest, some a =>
      if q.1 = a.1 then groupSameAux rest (some (addCoeff q a))
      else a :: groupSameAux rest (some q)

/-- 合并相邻同类项。 -/
def groupSame (p : List (Monom × Rat)) : List (Monom × Rat) := groupSameAux p none

/-! ### 语义保值

`groupSameAux` 的语义：它输出的求和 = 「当前累积值 + 剩下各项之和」。

证明的核心是：把 `a :: rest` 这段的和按 `sum_append` 拆成 `[a] 的和 + rest 的和`，
再对 `addCoeff` 用乘法分配律（`Rat.add_mul`）。 -/

/-- 一项的值。 -/
def itemDenote (Φ : Phi) (σ : Env) (q : Monom × Rat) : Rat :=
  q.2 * monomDenote Φ σ q.1

/-- 单项式相同时，合并系数等于两项各自的值相加。 -/
theorem itemDenote_addCoeff (Φ : Phi) (σ : Env) {q a : Monom × Rat}
    (h : q.1 = a.1) :
    itemDenote Φ σ (addCoeff q a) = itemDenote Φ σ q + itemDenote Φ σ a := by
  simp only [itemDenote, addCoeff, h]
  exact Rat.add_mul q.2 a.2 (monomDenote Φ σ a.1)

/-- 交换加法中间两项：`(x + y) + s = y + (x + s)`。 -/
theorem add_swap_mid (x y s : Rat) : (x + y) + s = y + (x + s) :=
  (Rat.add_assoc x y s).trans (Rat.add_left_comm x y s)

/-- 累积值 + 剩余之和。 -/
theorem groupSameAux_sound (Φ : Phi) (σ : Env) (l : List (Monom × Rat))
    (acc : Option (Monom × Rat)) :
    (List.map (itemDenote Φ σ) (groupSameAux l acc)).sum =
      match acc with
      | none => (List.map (itemDenote Φ σ) l).sum
      | some a => itemDenote Φ σ a + (List.map (itemDenote Φ σ) l).sum := by
  induction l generalizing acc with
  | nil =>
    cases acc with
    | none => simp only [groupSameAux, List.map_nil, List.sum_nil]
    | some a => simp only [groupSameAux, List.map_cons, List.map_nil, List.sum_cons,
        List.sum_nil, Rat.add_zero]
  | cons q rest ih =>
    have ih' : ∀ a : Monom × Rat,
        (List.map (itemDenote Φ σ) (groupSameAux rest (some a))).sum =
          itemDenote Φ σ a + (List.map (itemDenote Φ σ) rest).sum := by
      intro a
      simpa using ih (some a)
    cases acc with
    | none =>
      show (List.map (itemDenote Φ σ) (groupSameAux (q :: rest) none)).sum =
        (List.map (itemDenote Φ σ) (q :: rest)).sum
      simp only [groupSameAux, List.map_cons, List.sum_cons, ih' q]
    | some a =>
      show (List.map (itemDenote Φ σ) (groupSameAux (q :: rest) (some a))).sum =
        itemDenote Φ σ a + (List.map (itemDenote Φ σ) (q :: rest)).sum
      by_cases h : q.1 = a.1
      · simp only [groupSameAux, if_pos h, List.map_cons, List.sum_cons,
          ih' (addCoeff q a), itemDenote_addCoeff Φ σ h]
        exact add_swap_mid _ _ _
      · simp only [groupSameAux, if_neg h, List.map_cons, List.sum_cons, ih' q]

/-- 合并同类项不改变多项式的值。 -/
theorem groupSame_sound (Φ : Phi) (σ : Env) (p : List (Monom × Rat)) :
    (List.map (itemDenote Φ σ) (groupSame p)).sum =
      (List.map (itemDenote Φ σ) p).sum := by
  simp only [groupSame, groupSameAux_sound Φ σ p none]

theorem polyDenote_groupSame (Φ : Phi) (σ : Env) (p : Poly) :
    polyDenote Φ σ (groupSame p) = polyDenote Φ σ p :=
  groupSame_sound Φ σ p

/-! ### 排序 -/

/-- 排序单项式内部：原子的重排不改变单项式的值。 -/
def sortMonom (m : Monom) : Monom := m.mergeSort termLt

theorem monomDenote_sortMonom (Φ : Phi) (σ : Env) (m : Monom) :
    monomDenote Φ σ (sortMonom m) = monomDenote Φ σ m := by
  unfold sortMonom
  exact monomDenote_perm Φ σ (List.mergeSort_perm m termLt)

/-- 排序多项式：先排每项的原子，再按单项式排项。 -/
def sortPoly (p : Poly) : Poly :=
  (p.map (fun q => (sortMonom q.1, q.2))).mergeSort
    (fun a b => monomLt a.1 b.1)

theorem sum_map_perm {α : Type} (f : α → Rat) {l₁ l₂ : List α} (h : l₁.Perm l₂) :
    (List.map f l₁).sum = (List.map f l₂).sum :=
  perm_sum_eq (h.map f)

theorem polyDenote_sortPoly (Φ : Phi) (σ : Env) (p : Poly) :
    polyDenote Φ σ (sortPoly p) = polyDenote Φ σ p := by
  unfold sortPoly polyDenote
  -- 1) 排序：mergeSort 是原列表的排列，求和不变
  rw [sum_map_perm (fun q : Monom × Rat => q.2 * monomDenote Φ σ q.1)
        (List.mergeSort_perm (p.map (fun q => (sortMonom q.1, q.2)))
          (fun a b => monomLt a.1 b.1))]
  -- 2) 逐项：把 sortMonom 删掉（值不变）
  rw [List.map_map]
  rw [List.map_congr_left
        (f := (fun q : Monom × Rat => q.2 * monomDenote Φ σ q.1) ∘
              fun q : Monom × Rat => (sortMonom q.1, q.2))
        (g := fun q : Monom × Rat => q.2 * monomDenote Φ σ q.1)]
  intro q _
  simp only [Function.comp]
  rw [monomDenote_sortMonom]

/-- 规范化并标准化：排序 + 合并同类项。 -/
def canonical (t : Term) : Poly := groupSame (sortPoly (norm t))

theorem canonical_sound (Φ : Phi) (σ : Env) (t : Term) :
    polyDenote Φ σ (canonical t) = Term.eval Φ σ t := by
  unfold canonical
  rw [polyDenote_groupSame, polyDenote_sortPoly, norm_sound]

/-- **最终判等**：标准形相同 ⟹ 求值相等。 -/
theorem eq_of_canonical_eq (Φ : Phi) (σ : Env) {a b : Term}
    (h : canonical a = canonical b) : Term.eval Φ σ a = Term.eval Φ σ b := by
  rw [← canonical_sound Φ σ a, ← canonical_sound Φ σ b, h]

end AetherCert