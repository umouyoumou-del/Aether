/-
  AetherCert.Parse —— 证书文本的解析

  ## 为什么证书要有自己的语法

  证书是 Aether 与 Lean 之间唯一的信道。这条信道必须满足两件事：

    1. **可独立解析**：Lean 侧不调用 Aether 的代码，只读文本。
    2. **无法夹带**：文本里写不出的东西，就不能被「声称证明」。

  第 2 点决定了语法要极窄。这里用前缀括号式：

    (num 3)            整数
    (num -1/2)         有理数，分数形式
    (num 0.5)          有理数，小数形式（按十进制精确转成分数）
    (var x)            自由变量
    (add a b)          二元和
    (mul a b)          二元积
    (pow a 2)          整数次幂
    (neg a)            一元负号
    (div a b)          商
    (fn sin a)         初等函数（名字必须在 Term.fnNames 白名单里）

  前缀式而非中缀式，是因为中缀要处理优先级与结合性，而「解析器的优先级 bug」
  恰恰是证书系统最不该有的东西 —— 一个把 `a - b - c` 读成 `a - (b - c)` 的
  解析器会让两侧对同一份文本得出不同的树，于是「验过了」变成一句空话。
  前缀式没有优先级，每个括号只对应一种树。

  ## 证书的整体形状

    aethercert 1                ← 版本头，防止旧格式被当新格式读
    vars x y                    ← 自由变量声明（可重复，累积）
    start <term>                ← 起点
    same <term>                 ← 断言行 k：上一项与本项标准形相同
    diff x <term>               ← 断言行 k：本项是上一项对 x 的导数
    same <term>
    end                         ← 结束

  逐行校验，每一行都是一次独立的判等。出错时报出行号与失败原因，
  所以「哪一步被篡改」可以直接定位到行。

  `#` 开头是注释，空行忽略。
-/
import AetherCert.Sort

namespace AetherCert

/-! ### 词法 -/

/-- 记号：左括号、右括号、原子串。 -/
inductive Tok where
  | lp
  | rp
  | atom (s : String)
  deriving Repr, DecidableEq

namespace Tok

def render : Tok → String
  | .lp => "("
  | .rp => ")"
  | .atom s => s

end Tok

/-- 把累积中的原子串收进结果（`acc` 是逆序累积）。 -/
def lexFlush (cur : List Char) (acc : List Tok) : List Tok :=
  match cur with
  | [] => acc
  | _ => Tok.atom (String.mk cur.reverse) :: acc

def lexGo : List Char → List Char → List Tok → List Tok
  | [], cur, acc => (lexFlush cur acc).reverse
  | c :: rest, cur, acc =>
      if c = '(' then lexGo rest [] (Tok.lp :: lexFlush cur acc)
      else if c = ')' then lexGo rest [] (Tok.rp :: lexFlush cur acc)
      else if c.isWhitespace then lexGo rest [] (lexFlush cur acc)
      else lexGo rest (c :: cur) acc

/-- 切词。 -/
def lex (s : String) : List Tok := lexGo s.toList [] []

/-! ### 数值 -/

/-- 十进制下 `10 ^ k`。 -/
def tenPow : Nat → Nat
  | 0 => 1
  | k + 1 => 10 * tenPow k

/-- 指数记法 `d.ddd[eE][+-]k` → `(有效数字串, 指数)`。

    例如 `1.5e-3` → `("15", -4)`（因为 `1.5e-3 = 15 × 10⁻⁴`）。
    接收科学计数法是必要的：`str(1e-7)` 会输出 `1e-07`，若不认这种写法，
    Aether 导出的证书会在解析阶段就失败。 -/
def splitExp (s : String) : String × Int :=
  let parts := s.splitOn "e"
  let parts := match parts with
    | [a] => a.splitOn "E"
    | _ => parts
  match parts with
  | [mant, ex] =>
      match ex.toInt? with
      | some e => (mant, e)
      | none => (s, 0)
  | _ => (s, 0)

/-- 把一个不带符号的串精确转成有理数。

    支持：整数、`分子/分母`、`整数.小数`、以及带 `e/E` 指数的科学计数法。
    全部走 `Int`/`Nat` 精确运算，不经过浮点，所以 `0.1` 严格等于 `1/10`
    而不是二进制近似值 —— 这是「证书里的数必须精确」的底线。 -/
def parseRatBody (s : String) : Option Rat :=
  match s.splitOn "/" with
  | [a, b] => do
      let na ← a.toInt?
      let nb ← b.toInt?
      if nb = 0 then none else some (Rat.divInt na nb)
  | _ =>
      let (mant, ex) := splitExp s
      let frac := mant.splitOn "."
      match frac with
      | [a, b] => do
          let ip ← if a = "" then some (0 : Int) else a.toInt?
          let fp ← b.toNat?
          let d := tenPow b.length
          let digits := (if ip < 0 then ip * (d : Int) - (fp : Int) else ip * (d : Int) + (fp : Int))
          -- 值 = digits/d × 10^ex：正指数进分子，负指数进分母
          let nn := if ex ≥ 0 then digits * (tenPow ex.toNat : Int) else digits
          let dd := if ex ≥ 0 then d else d * tenPow (-ex).toNat
          some (Rat.divInt nn (dd : Int))
      | _ => do
          let n ← mant.toInt?
          let nn := if ex ≥ 0 then n * (tenPow ex.toNat : Int) else n
          let dd := if ex ≥ 0 then 1 else tenPow (-ex).toNat
          some (Rat.divInt nn (dd : Int))

/-- 有理数字面量：可选负号，其余交给 `parseRatBody`。 -/
def parseRatStr (s : String) : Option Rat :=
  if s.startsWith "-" then
    match parseRatBody (s.drop 1) with
    | some q => some (-q)
    | none => none
  else parseRatBody s

/-! ### 项 -/

/-- 从记号流里读一个项，返回项与剩余的记号。`fuel` 保证递归可终止：
    前缀记号的个数有限，每读一个子项至少消耗一次 fuel。 -/
def parseTermFuel : Nat → List Tok → Option (Term × List Tok)
  | 0, _ => none
  | fuel + 1, toks =>
      match toks with
      | Tok.lp :: Tok.atom head :: rest =>
          match head with
          | "num" =>
              match rest with
              | Tok.atom v :: Tok.rp :: rest =>
                  match parseRatStr v with
                  | some q => some (.num q, rest)
                  | none => none
              | _ => none
          | "var" =>
              match rest with
              | Tok.atom v :: Tok.rp :: rest => some (.var v, rest)
              | _ => none
          | "neg" =>
              match parseTermFuel fuel rest with
              | some (a, Tok.rp :: rest) => some (.neg a, rest)
              | _ => none
          | "add" =>
              match parseTermFuel fuel rest with
              | some (a, rest) =>
                match parseTermFuel fuel rest with
                | some (b, Tok.rp :: rest) => some (.add a b, rest)
                | _ => none
              | none => none
          | "mul" =>
              match parseTermFuel fuel rest with
              | some (a, rest) =>
                match parseTermFuel fuel rest with
                | some (b, Tok.rp :: rest) => some (.mul a b, rest)
                | _ => none
              | none => none
          | "div" =>
              match parseTermFuel fuel rest with
              | some (a, rest) =>
                match parseTermFuel fuel rest with
                | some (b, Tok.rp :: rest) => some (.div a b, rest)
                | _ => none
              | none => none
          | "pow" =>
              match parseTermFuel fuel rest with
              | some (a, Tok.atom k :: Tok.rp :: rest) =>
                  match k.toInt? with
                  | some n => some (.pow a n, rest)
                  | none => none
              | _ => none
          | "fn" =>
              match rest with
              | Tok.atom name :: rest =>
                  if Term.isFn name then
                    match parseTermFuel fuel rest with
                    | some (a, Tok.rp :: rest) => some (.fn name a, rest)
                    | _ => none
                  else none
              | _ => none
          | _ => none
      | _ => none

/-- 读一个完整的项（记号流必须恰好被吃干净）。fuel 取记号个数，
    每次递归至少消耗一个记号，所以足够。 -/
def parseTermExact (toks : List Tok) : Option Term :=
  match parseTermFuel (toks.length + 1) toks with
  | some (t, []) => some t
  | _ => none

/-! ### 步 -/

/-- 一步变换。两种都归结为「两侧的标准形相同」：
    - `same t`：上一项与 `t` 代数恒等；
    - `diff x t`：`t` 是上一项对 `x` 的导数（把 `deriv` 插在左边再判等）。 -/
inductive Step where
  | same (t : Term)
  | diff (x : String) (t : Term)
  deriving Repr

/-- 带行号的步，便于报错定位。 -/
structure Line where
  no : Nat
  step : Step
  deriving Repr

/-- 解析后的证书。 -/
structure Cert where
  vars : List String
  start : Term
  steps : List Line
  /-- 可选断言：最终项应与本项恒等。 -/
  claim : Option Term
  deriving Repr

/-! ### 逐行解析 -/

/-- 一行的第一条记号。 -/
def headOf (toks : List Tok) : Option String :=
  match toks with
  | Tok.atom s :: _ => some s
  | _ => none

/-- 解析一行（已切词）。`none` 表示这行语法上不成立。 -/
def parseLineKind (toks : List Tok) : Option (String × List Tok) :=
  match toks with
  | Tok.atom s :: rest => some (s, rest)
  | _ => none

/-- 逐行累积的解析状态。 -/
structure PState where
  vars : List String := []
  start : Option Term := none
  steps : List Line := []
  claim : Option Term := none
  closed : Bool := false

/-- 把 `vars` 行的字面量收进来。 -/
def collectVars (vs : List String) (toks : List Tok) : Except String (List String) := do
  let mut out := vs
  for t in toks do
    match t with
    | Tok.atom v => out := out ++ [v]
    | _ => throw "`vars` 后面只能跟变量名"
  return out

/-- 处理单行，返回新的状态。 -/
def stepLine (st : PState) (no : Nat) (raw : String) : Except String PState := do
  let line := raw.trim
  if line = "" then return st
  if line.startsWith "#" then return st
  let toks := lex line
  match toks with
  | [] => throw s!"第 {no} 行：空行却读不出记号"
  | Tok.atom key :: rest =>
    match key with
    | "aethercert" =>
      match rest with
      | [Tok.atom v] =>
        if v = "1" then return st else throw s!"第 {no} 行：不支持的证书版本 {v}"
      | _ => throw s!"第 {no} 行：版本头格式应为 `aethercert 1`"
    | "vars" =>
      let vs ← collectVars st.vars rest
      return { st with vars := vs }
    | "start" =>
      match parseTermExact rest with
      | some t => return { st with start := some t }
      | none => throw s!"第 {no} 行：`start` 后面不是一个完整的项"
    | "same" =>
      match parseTermExact rest with
      | some t => return { st with steps := st.steps ++ [{ no := no, step := .same t }] }
      | none => throw s!"第 {no} 行：`same` 后面不是一个完整的项"
    | "diff" =>
      match rest with
      | Tok.atom x :: rest =>
        match parseTermExact rest with
        | some t => return { st with steps := st.steps ++ [{ no := no, step := .diff x t }] }
        | none => throw s!"第 {no} 行：`diff` 后面不是一个完整的项"
      | _ => throw s!"第 {no} 行：`diff` 需要先写一个变量名"
    | "claim" =>
      match parseTermExact rest with
      | some t => return { st with claim := some t }
      | none => throw s!"第 {no} 行：`claim` 后面不是一个完整的项"
    | "end" =>
      if rest.isEmpty then return { st with closed := true }
      else throw s!"第 {no} 行：`end` 后面不应有内容"
    | other => throw s!"第 {no} 行：未知关键字「{other}」"
  | _ => throw s!"第 {no} 行：每行必须以关键字或注释开头"

/-- 解析整份证书。 -/
def parseCert (text : String) : Except String Cert := do
  let mut st : PState := {}
  let mut no := 0
  for raw in text.splitOn "\n" do
    no := no + 1
    st ← stepLine st no raw
  if !st.closed then throw "证书缺少 `end` 行（可能被截断）"
  match st.start with
  | none => throw "证书缺少 `start` 行"
  | some s => .ok { vars := st.vars, start := s, steps := st.steps, claim := st.claim }

end AetherCert