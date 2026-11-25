/-
  AetherCert.Check —— 逐步校验

  ## 校验器的形状

  校验是一条链：每一行用上一行的结果作为前提，所以任何一行被篡改，
  后续全断。具体到每种步：

    `same t`      ：`canonical 上一项 = canonical t`，否则报错
    `diff x t`    ：内核算出 `deriv x 上一项`，再判 `canonical 它 = canonical t`

  两种情况都落到同一个判等函数上。也就是说 `diff` 步并不比 `same` 步多需要
  什么公理 —— 它只是把「证书声称的结果」与「内核算出的结果」相比，而内核算
  导数用的是 `Deriv.deriv` 那张表（唯一的假设，见 `Deriv.lean` 头注释）。

  ## 为什么错误要带行号

  一份 40 行的推导，只说「不成立」等于没说。报出「第 17 行：`diff` 步不成立，
  内核算得 ...，证书声称 ...」才能定位。诊断信息里放两侧的渲染串，
  是因为人眼对表达式比对照树形结构敏感得多。

  ## `end` 之后的 `claim` 行

  证书可以（可选地）以一行 `claim <term>` 结尾，声明「最终结果与这个项恒等」。
  这是最常用的一步：`sym.equals` 就是判这个。单独列出来是为了让
  「验到哪一步」有明确的终点，而不是靠「最后一行碰巧相等」。
-/
import AetherCert.Parse
import AetherCert.Deriv

namespace AetherCert

/-- 校验结果。`ok` 附带最终项与已验过的步数；`err` 带行号与诊断。 -/
inductive Result where
  | ok (final : Term) (steps : Nat)
  | err (msg : String)
  deriving Repr

def Result.isOk : Result → Bool
  | .ok _ _ => true
  | .err _ => false

/-- 判两个项是否代数恒等。这是 `same` 步的全部内容。 -/
def sameTerm (a b : Term) : Bool :=
  decide (canonical a = canonical b)

/-- 诊断串：说明两侧标准形不同。 -/
def sameFailMsg (no : Nat) (a b : Term) : String :=
  s!"第 {no} 行：`same` 不成立。上一项 {a} 与声称的 {b} 标准形不同"

/-- 走一步。返回新的当前项，或错误。 -/
def applyStep (cur : Term) (ln : Line) : Except String Term :=
  match ln.step with
  | .same t =>
      if sameTerm cur t then .ok t
      else .error (sameFailMsg ln.no cur t)
  | .diff x t =>
      match deriv x cur with
      | none =>
          .error s!"第 {ln.no} 行：对 {x} 求导时遇到没有导数规则的函数，无法校验"
      | some d =>
          if sameTerm d t then .ok t
          else .error s!"第 {ln.no} 行：`diff` 不成立。内核把 {cur} 对 {x} 求导得 {d}，证书声称 {t}"

/-- 走完全部步。 -/
def runSteps : Term → List Line → Except String (Term × Nat)
  | cur, [] => .ok (cur, 0)
  | cur, ln :: rest => do
      let cur' ← applyStep cur ln
      let (fin, n) ← runSteps cur' rest
      return (fin, n + 1)

/-- 校验整份证书：从 `start` 起，逐步走到最后，最后检查 `claim`（若有）。 -/
def checkCert (cert : Cert) (claim : Option Term) : Result :=
  match runSteps cert.start cert.steps with
  | .error e => .err e
  | .ok (fin, n) =>
      match claim with
      | none => .ok fin n
      | some t =>
          if sameTerm fin t then .ok fin n
          else .err s!"末步：`claim` 不成立。最终项 {fin} 与声称的 {t} 标准形不同"

/-- 从文本直接校验（`claim` 由文本末尾的 `claim` 行给出）。 -/
def checkText (text : String) : Except String Result := do
  let cert ← parseCert text
  return checkCert cert cert.claim

end AetherCert