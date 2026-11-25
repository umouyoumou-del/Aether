/-
  AetherCert 回归测试（不属于库，仅探针）

  跑法（在 `/e/Aether/lean` 下）：

      export PATH="/e/Apps/elan/bin:$PATH" && export ELAN_HOME='E:\Apps\elan'
      lake env lean --run Test/Certs.lean

  每行输出 `PASS` 或 `FAIL`。任何一条 `FAIL` 都说明有回归。

  为什么要这个：`Test/certs/*.cert` 是手工样本，只跑一遍看不出「以后改坏了」。
  把「哪份该过、几倍该过、哪份该拒、拒绝理由里该出现什么词」写成断言，
  改动 `norm`/`canonical`/`deriv` 时就能立刻发现行为漂移。
-/
import AetherCert.Check

open AetherCert

/-- 断言：该证书通过，且步数恰为 `steps`。 -/
def expectPass (path : String) (steps : Nat) : IO Unit := do
  let text ← IO.FS.readFile path
  match checkText text with
  | .error e => IO.println s!"FAIL {path}：解析错误 {e}"
  | .ok (.err e) => IO.println s!"FAIL {path}：应通过却被拒 —— {e}"
  | .ok (.ok _ n) =>
      if n = steps then IO.println s!"PASS {path}（{n} 步）"
      else IO.println s!"FAIL {path}：步数 {n} ≠ 期望 {steps}"

/-- 断言：该证书被拒，且诊断里含 `needle`（用来区分「解析期拒」与「校验期拒」）。 -/
def expectReject (path : String) (needle : String) : IO Unit := do
  let text ← IO.FS.readFile path
  let okMsg (e : String) : IO Unit :=
    if (e.splitOn needle).length > 1 then IO.println s!"PASS {path}（已拒）"
    else IO.println s!"FAIL {path}：拒绝理由不含「{needle}」—— {e}"
  match checkText text with
  | .error e => okMsg e
  | .ok (.err e) => okMsg e
  | .ok (.ok _ _) => IO.println s!"FAIL {path}：应被拒却通过"

def main : IO Unit := do
  -- 该通过的
  expectPass "Test/certs/t1.cert" 1
  expectPass "Test/certs/t2.cert" 1
  expectPass "Test/certs/t5.cert" 1
  expectPass "Test/certs/t6.cert" 1
  expectPass "Test/certs/t8.cert" 1
  expectPass "Test/certs/t10.cert" 1
  expectPass "Test/certs/t12_multistep.cert" 2
  -- 该被拒的：`diff` 校验期
  expectReject "Test/certs/t3_bad.cert" "`diff` 不成立"
  expectReject "Test/certs/t4_bad.cert" "`diff` 不成立"
  expectReject "Test/certs/t7_bad.cert" "`diff` 不成立"
  expectReject "Test/certs/t9_bad.cert" "`diff` 不成立"
  -- 该被拒的：解析期（白名单外函数名）
  expectReject "Test/certs/t11_bad.cert" "不是一个完整的项"
  IO.println "—— 全部断言执行完毕 ——"
