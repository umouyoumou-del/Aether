/-
  AetherCert.Basic —— 列表与 `Rat` 的基础引理

  Lean core 没有 Mathlib 的 `List.sum_append` / `List.prod_append` / `Finset` 那一套，
  所以规范化器需要的东西得自己证。这个文件只放**与具体数学对象无关**的通用引理，
  让后面的 `Norm.lean` 能聚焦在数学上。

  取舍说明：core 里 `List.prod` 不存在（`List.sum` 存在，因为 `Add`/`Zero` 是标准类，
  而乘积对应的函数没随 core 提供），所以乘积由本文件的 `lprod` 承担。
-/
import AetherCert.Term

namespace AetherCert

/-! ### 求和 -/

/-- 求和按追加可拆。 -/
theorem sum_append (l₁ l₂ : List Rat) :
    (l₁ ++ l₂).sum = l₁.sum + l₂.sum := by
  induction l₁ with
  | nil => simp only [List.nil_append, List.sum_nil, Rat.zero_add]
  | cons h t ih =>
    simp only [List.cons_append, List.sum_cons]
    exact congrArg (fun x => h + x) ih |>.trans (Rat.add_assoc _ _ _).symm

/-- 三项相邻交换。 -/
theorem sum_swap (a b : Rat) (l : List Rat) :
    (a :: b :: l).sum = (b :: a :: l).sum := by
  simp only [List.sum_cons]
  show a + (b + l.sum) = b + (a + l.sum)
  calc a + (b + l.sum) = (a + b) + l.sum := (Rat.add_assoc a b l.sum).symm
    _ = (b + a) + l.sum := by rw [Rat.add_comm a b]
    _ = b + (a + l.sum) := Rat.add_assoc b a l.sum

/-- 求和与加法可交换到排列上：排列不改变和。 -/
theorem perm_sum_eq {l₁ l₂ : List Rat} (h : l₁.Perm l₂) :
    l₁.sum = l₂.sum := by
  induction h with
  | nil => rfl
  | cons x _ ih => exact congrArg (fun t => x + t) ih
  | swap x y l => exact (sum_swap x y l).symm
  | trans _ _ ih₁ ih₂ => exact ih₁.trans ih₂

/-- 常数可以从求和里提出来。 -/
theorem sum_map_mul_left (c : Rat) (l : List Rat) :
    (l.map (fun x => c * x)).sum = c * l.sum := by
  induction l with
  | nil => simp only [List.map_nil, List.sum_nil, Rat.mul_zero]
  | cons h t ih =>
    simp only [List.map_cons, List.sum_cons, ih]
    exact (Rat.mul_add c h t.sum).symm

/-- `flatMap` 的求和 = 各段求和再求和。 -/
theorem sum_flatMap {α : Type} (f : α → List Rat) (l : List α) :
    (l.flatMap f).sum = (l.map (fun x => (f x).sum)).sum := by
  induction l with
  | nil => rfl
  | cons h t ih =>
    simp only [List.flatMap_cons, List.map_cons, List.sum_cons]
    rw [sum_append, ih]

/-! ### 乘积 -/

/-- `List Rat` 的乘积。core 无 `List.prod`。 -/
def lprod : List Rat → Rat
  | [] => 1
  | x :: xs => x * lprod xs

theorem lprod_append (l₁ l₂ : List Rat) :
    lprod (l₁ ++ l₂) = lprod l₁ * lprod l₂ := by
  induction l₁ with
  | nil => simp only [List.nil_append, lprod, Rat.one_mul]
  | cons h t ih => simp only [List.cons_append, lprod, ih, Rat.mul_assoc]

theorem lprod_swap (a b : Rat) (l : List Rat) :
    lprod (a :: b :: l) = lprod (b :: a :: l) := by
  simp only [lprod]
  show a * (b * lprod l) = b * (a * lprod l)
  calc a * (b * lprod l) = (a * b) * lprod l := (Rat.mul_assoc a b (lprod l)).symm
    _ = (b * a) * lprod l := by rw [Rat.mul_comm a b]
    _ = b * (a * lprod l) := Rat.mul_assoc b a (lprod l)

/-- 乘积对排列不变。 -/
theorem perm_lprod_eq {l₁ l₂ : List Rat} (h : l₁.Perm l₂) :
    lprod l₁ = lprod l₂ := by
  induction h with
  | nil => rfl
  | cons x _ ih => exact congrArg (fun t => x * t) ih
  | swap x y l => exact (lprod_swap x y l).symm
  | trans _ _ ih₁ ih₂ => exact ih₁.trans ih₂

/-- 常数可以从乘积里提出去。 -/
theorem lprod_map_mul_left (c : Rat) (l : List Rat) :
    lprod (l.map (fun x => c * x)) = c ^ l.length * lprod l := by
  induction l with
  | nil => simp only [List.map_nil, List.length_nil, lprod, Rat.pow_zero, Rat.one_mul]
  | cons h t ih =>
    simp only [List.map_cons, List.length_cons, lprod, ih, Nat.add_one, Rat.pow_succ]
    show (c * h) * (c ^ t.length * lprod t) = c ^ t.length * c * (h * lprod t)
    calc (c * h) * (c ^ t.length * lprod t)
        = c * (h * (c ^ t.length * lprod t)) := Rat.mul_assoc c h _
      _ = c * ((h * c ^ t.length) * lprod t) := by rw [Rat.mul_assoc h]
      _ = c * ((c ^ t.length * h) * lprod t) := by rw [Rat.mul_comm h]
      _ = c * (c ^ t.length * (h * lprod t)) := by rw [Rat.mul_assoc (c ^ t.length)]
      _ = (c * c ^ t.length) * (h * lprod t) := (Rat.mul_assoc c _ _).symm
      _ = (c ^ t.length * c) * (h * lprod t) := by rw [Rat.mul_comm c]

/-- 求和右乘常数。 -/
theorem sum_map_mul_right (l : List Rat) (c : Rat) :
    (l.map (fun x => x * c)).sum = l.sum * c := by
  induction l with
  | nil => simp only [List.map_nil, List.sum_nil, Rat.zero_mul]
  | cons h t ih =>
    simp only [List.map_cons, List.sum_cons]
    rw [ih, Rat.add_mul]

/-- 先 `flatMap` 再逐项求和 = 各段分别求和后再求和。 -/
theorem sum_map_flatMap {α β : Type} (g : β → Rat) (f : α → List β) (l : List α) :
    (List.map g (l.flatMap f)).sum = (l.map (fun x => (List.map g (f x)).sum)).sum := by
  induction l with
  | nil => rfl
  | cons h t ih =>
    simp only [List.flatMap_cons, List.map_append, List.map_cons, List.sum_cons, ih,
      sum_append]

/-- 逐点相等的两个函数，求和相同。 -/
theorem sum_map_congr {α : Type} {f g : α → Rat} {l : List α}
    (h : ∀ a, a ∈ l → f a = g a) : (l.map f).sum = (l.map g).sum :=
  congrArg List.sum (List.map_congr_left h)

/-- 求和时左侧因子可以提出来（对任意投影函数）。 -/
theorem sum_map_const_mul {α : Type} (c : Rat) (f : α → Rat) (l : List α) :
    (l.map (fun x => c * f x)).sum = c * (l.map f).sum := by
  induction l with
  | nil => simp only [List.map_nil, List.sum_nil, Rat.mul_zero]
  | cons h t ih =>
    simp only [List.map_cons, List.sum_cons, ih]
    exact (Rat.mul_add c (f h) (List.map f t).sum).symm

/-- 求和时右侧因子可以提出来（对任意投影函数）。 -/
theorem sum_map_mul_const {α : Type} (f : α → Rat) (c : Rat) (l : List α) :
    (l.map (fun x => f x * c)).sum = (l.map f).sum * c := by
  induction l with
  | nil => simp only [List.map_nil, List.sum_nil, Rat.zero_mul]
  | cons h t ih =>
    simp only [List.map_cons, List.sum_cons, ih]
    exact (Rat.add_mul (f h) (List.map f t).sum c).symm

/-! ### 指数 -/

/-- 自然数指数的加法律。 -/
theorem pow_add_nat (a : Rat) (m n : Nat) : a ^ (m + n) = a ^ m * a ^ n := by
  induction n with
  | zero => simp only [Nat.add_zero, Rat.pow_zero, Rat.mul_one]
  | succ n ih =>
    rw [Nat.add_succ, Rat.pow_succ, ih, Rat.pow_succ]
    exact Rat.mul_assoc (a ^ m) (a ^ n) a

end AetherCert