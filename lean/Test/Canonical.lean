import AetherCert
open AetherCert

def x : Term := .var "x"
def y : Term := .var "y"
def z : Term := .var "z"

-- 交换律（排序 + 合并后应判出）
example : canonical (Term.add x y) = canonical (Term.add y x) := by native_decide

-- 三数交换
example : canonical (Term.add (Term.add x y) z) = canonical (Term.add (Term.add y x) z) := by
  native_decide

-- 分配律
example : canonical (Term.mul x (Term.add y z)) =
    canonical (Term.add (Term.mul x y) (Term.mul x z)) := by native_decide

-- 平方展开
example : canonical ((x.add y).pow 2) =
    canonical (((x.mul x).add (x.mul y)).add ((y.mul x).add (y.mul y))) := by native_decide

-- 单项式内原子的交换
example : canonical (Term.mul x y) = canonical (Term.mul y x) := by native_decide

-- 幂展开 vs 重复乘
example : canonical ((x.pow 3)) = canonical ((x.mul x).mul x) := by native_decide

-- 合并同类项
example : canonical (Term.add (Term.mul (Term.num 2) x) (Term.mul (Term.num 3) x)) =
    canonical (Term.mul (Term.num 5) x) := by native_decide

-- 反例：确实不应相等
example : canonical x ≠ canonical y := by native_decide