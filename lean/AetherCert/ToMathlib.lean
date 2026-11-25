/-
  AetherCert.ToMathlib —— 把证书里的项翻译成 Lean / Mathlib 表达式文本

  ## 为什么要有这个模块

  自带的 `certcheck` 证明的是「在它自己的内核里成立」。它的信任基础是本仓库
  手写的一套东西：`canonical` 的规范化、158 行手写引理、`Deriv.dtable` 的
  12 行导数表。这些都对，但**是同一批人写的**——万一某条引理本身写错，校验器
  会「自洽地错」。

  要真正「独立」，就得让**另一个、完全无关的实现**来核对同一件事。Mathlib 的
  `ring` 策略正是这样的东西：它不认识我们的 `canonical`，只认交换半环公理；
  它的导数库也不认识我们的 `dtable`。所以把证书翻成 Mathlib 命题再证一遍，
  得到的是一份「第二见证人」的签名。

  ## 本模块的边界

  只做「项 → 文本」，**不依赖 Mathlib**（本包的编译不需要 Mathlib）。真正
  `import Mathlib` 的是它生成的 `.lean` 文件。这样即便没装 Mathlib，也能在本机
  把翻译跑起来、肉眼检查生成的源码。

  两种数值域：
  - `ℚ`：纯四则与整数幂，交给 `ring`（最稳，跨 Mathlib 版本不变）；
  - `ℝ`：出现初等函数时，交给 `deriv` + `ring`（依赖具体函数引理名）。
-/
import AetherCert.Parse

namespace AetherCert

namespace Term

/-- 项里是否含初等函数节点。含则不能走 `ring`（`ℚ` 上没有 `sin`）。 -/
def hasFn : Term → Bool
  | .num _ => false
  | .var _ => false
  | .add a b => a.hasFn || b.hasFn
  | .mul a b => a.hasFn || b.hasFn
  | .pow a _ => a.hasFn
  | .neg a => a.hasFn
  | .div a b => a.hasFn || b.hasFn
  | .fn _ _ => true

end Term

/-- 函数名 → Mathlib 的 `Real.xxx`（仅用于 `ℝ` 语境）。 -/
def realFnName : String → String
  | "sin" => "Real.sin"
  | "cos" => "Real.cos"
  | "tan" => "Real.tan"
  | "exp" => "Real.exp"
  | "log" => "Real.log"
  | "sqrt" => "Real.sqrt"
  | "sinh" => "Real.sinh"
  | "cosh" => "Real.cosh"
  | "tanh" => "Real.tanh"
  | "atan" => "Real.arctan"
  | "asin" => "Real.arcsin"
  | "acos" => "Real.arccos"
  | s => s

/-- 把项翻成 Lean 表达式文本。
    `ty` 是数值域字面名（"ℚ" / "ℝ"）；`real=true` 时函数符号映射到 `Real.*`，
    为 `false` 时不应出现 `fn`（调用方先查 `hasFn`）。 -/
def toLean (ty : String) (real : Bool) : Term → String
  | .num q =>
      if q.den = 1 then
        "(" ++ toString q.num ++ " : " ++ ty ++ ")"
      else
        "((" ++ toString q.num ++ " : " ++ ty ++ ") / (" ++ toString q.den ++ " : " ++ ty ++ "))"
  | .var s => s
  | .add a b => "(" ++ toLean ty real a ++ " + " ++ toLean ty real b ++ ")"
  | .mul a b => "(" ++ toLean ty real a ++ " * " ++ toLean ty real b ++ ")"
  | .pow a k =>
      let e := toLean ty real a
      if k ≥ 0 then "(" ++ e ++ " ^ " ++ toString k.toNat ++ ")"
      else "(" ++ e ++ " ^ (" ++ toString k ++ " : ℤ))"
  | .neg a => "(-" ++ toLean ty real a ++ ")"
  | .div a b => "(" ++ toLean ty real a ++ " / " ++ toLean ty real b ++ ")"
  | .fn f a =>
      if real then realFnName f ++ " (" ++ toLean ty real a ++ ")"
      else f ++ "(" ++ toLean ty real a ++ ")"

end AetherCert
