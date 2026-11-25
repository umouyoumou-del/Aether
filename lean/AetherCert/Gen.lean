/-
  AetherCert.Gen —— 把一份 .cert 证书翻译成一份可被 Mathlib 独立核对的 .lean 文件

  ## 产出什么

  对证书里的每一步，生成一条 **独立于本仓库** 的 Lean 命题并给出证明：

    - `same` 步  →  `example (x : ℚ) : lhs = rhs := by ring`
    - `diff` 步  →  `example (x : ℝ) : deriv (fun t => <lhs>) x = <rhs> := by simp; ring`

  `ring` / `deriv` 都来自 Mathlib，不认识我们的 `canonical`、`Sort.lean` 或
  `Deriv.dtable`，所以它们给出的「成立」是本仓库之外的第二份见证。

  ## 为什么生成「逐步」而不是「整体」

  证书的价值在于「每一步都可定位」。逐条生成，一方面与 `certcheck` 的报错
  行号一一对应；另一方面，任何一条 `ring` 编译失败，就精确指向证书里被改坏
  的那一步——和自带校验器的行为完全一致。

  ## 域的选择

  - 全程无初等函数、无求导：`ℚ` + `ring`（最稳，跨 Mathlib 版本不易坏）；
  - 出现 `sin` 等函数或 `diff` 步：`ℝ`，函数写成 `Real.sin`。
-/
import AetherCert.ToMathlib

namespace AetherCert

/-- 把项里的变量 `src` 改名成 `dst`（用于把被求导变量换成 `fun` 的形参）。 -/
def Term.rename (src dst : String) : Term → Term
  | .num q => .num q
  | .var s => if s = src then .var dst else .var s
  | .add a b => .add (a.rename src dst) (b.rename src dst)
  | .mul a b => .mul (a.rename src dst) (b.rename src dst)
  | .pow a k => .pow (a.rename src dst) k
  | .neg a => .neg (a.rename src dst)
  | .div a b => .div (a.rename src dst) (b.rename src dst)
  | .fn f a => .fn f (a.rename src dst)

/-- 项里是否出现除法（决定用 `ring` 还是 `ring_nf`）。 -/
def Term.hasDiv : Term → Bool
  | .num _ | .var _ => false
  | .add a b | .mul a b | .div a b => a.hasDiv || b.hasDiv
  | .pow a _ | .neg a => a.hasDiv
  | .fn _ a => a.hasDiv

/-- 一条待证义务。 -/
inductive Oblig where
  /-- `lhs = rhs`（对应一个 `same` 步或 `claim`）。 -/
  | eq (no : Nat) (lhs rhs : Term)
  /-- `deriv (fun t => lhs) x = rhs`（对应一个 `diff` 步）。 -/
  | dvd (no : Nat) (x : String) (lhs rhs : Term)
  deriving Repr

/-- 遍历证书，把每一步展开成义务（用上一项作为下一项的左端）。
    末尾 `claim` 若与最终项**完全相同**（导出器常见情形），跳过——
    那是一条自反断言，不值得生成一条 `ring`。 -/
def collect (c : Cert) : List Oblig :=
  let rec go : Term → List Line → List Oblig
    | cur, [] =>
        match c.claim with
        | some t => if t = cur then [] else [.eq 0 cur t]
        | none => []
    | cur, ln :: rest =>
        match ln.step with
        | .same t => .eq ln.no cur t :: go t rest
        | .diff x t => .dvd ln.no x cur t :: go t rest
  go c.start c.steps

/-- 一份证书是否涉及实数域（含初等函数或求导）。 -/
def needsReal (c : Cert) (obs : List Oblig) : Bool :=
  c.start.hasFn || obs.any (fun o => match o with
      | .eq _ l r => l.hasFn || r.hasFn
      | .dvd _ _ _ _ => true)

/-- 渲染一条义务为 Lean 代码。`ty` 是域名（"ℚ"/"ℝ"）。 -/
def renderOblig (ty : String) (real : Bool) (c : Cert) : Oblig → String
  | .eq no lhs rhs =>
      let bind := if c.vars.isEmpty then "" else " (" ++ String.intercalate " " c.vars ++ " : " ++ ty ++ ")"
      let tac := if lhs.hasDiv || rhs.hasDiv then "ring_nf" else "ring"
      let lbl := if no = 0 then "末步断言" else s!"第 {no} 步"
      s!"/-- {lbl}：代数恒等 -/\nexample{bind} : {toLean ty real lhs} = {toLean ty real rhs} := by\n  {tac}\n"
  | .dvd no x lhs rhs =>
      let lhs' := lhs.rename x "_t"
      let bind := " (" ++ x ++ " : ℝ)"
      -- `simp` 化简 `deriv`（用 Mathlib 的求导引理，含链式法则），
      -- 余下若有目标再用 `ring` 收盘；用 `<;>` 是因为 `simp` 可能已经证完，
      -- 顺序写 `ring` 会报 "No goals to be solved"。
      let tac := if rhs.hasDiv || lhs.hasDiv then "simp <;> ring_nf" else "simp <;> ring"
      s!"/-- 第 {no} 步：对 {x} 求导 -/\nexample{bind} : deriv (fun _t : ℝ => {toLean "ℝ" true lhs'}) {x} = {toLean "ℝ" true rhs} := by\n  {tac}\n"

/-- 证书 → 一份 .lean 源文件。 -/
def certToLean (c : Cert) (source : String) : String :=
  let obs := collect c
  let real := needsReal c obs
  let ty := if real then "ℝ" else "ℚ"
  let header :=
    "/-\n  自动生成，请勿手改。来源证书：" ++ source ++ "\n\n" ++
    "  这份文件用 Mathlib 的 ring / deriv **独立**核对证书里的每一步。\n" ++
    "  它不引用 Aether 的任何代码，因此与自带 certcheck 构成两个互相独立的见证。\n\n" ++
    "  校验方式（需已安装 Mathlib）：\n" ++
    "    lake env lean <本文件>\n" ++
    "  编译通过 = 每一步都被 Mathlib 认可；任一步失败 = 证书在该步被篡改。\n-/\n" ++
    "import Mathlib\n\nnamespace AetherCertGenerated\n\n"
  let body := String.intercalate "\n" (obs.map (renderOblig ty real c))
  header ++ body ++ "\n\nend AetherCertGenerated\n"

/-! ### 命令行入口 -/

/-- 从文件或标准输入读取证书，打印生成的 Lean 源码。 -/
def genMain (args : List String) : IO UInt32 := do
  let (text, src) ←
    match args with
    | [] => do
        let s ← (← IO.getStdin).readToEnd
        pure (s, "<stdin>")
    | "-" :: _ => do
        let s ← (← IO.getStdin).readToEnd
        pure (s, "<stdin>")
    | p :: _ => do
        let s ← IO.FS.readFile p
        pure (s, p)
  match parseCert text with
  | .error e =>
      IO.eprintln s!"解析失败：{e}"
      pure 2
  | .ok cert =>
      IO.print (certToLean cert src)
      pure 0

end AetherCert
