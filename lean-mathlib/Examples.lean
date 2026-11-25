/-
  Mathlib 独立校验 Aether 证书
  ================================

  本目录是一个独立的最小 Lean 工程，唯一目的：用 **Mathlib**（而不是本仓库
  自带的 `certcheck`）再核一遍 Aether 证书系统导出的每一步，给出「第二见证人」
  的签名。

  ## 这里的每一条 example 都对应一份证书的一步

    证书                          命题                                    tactic
    ----------------------------  --------------------------------------  -------------
    t1   (x+1)^2 = x^2+2x+1       ℚ 上恒等                                ring
    t12  x(x+2) = x^2+2x = 2x+x^2  ℚ 上两步恒等                            ring
    t2   d/dx(x^2+3x) = 2x+3      ℝ 上求导                                simp → ring
    t5   d/dx sin x = cos x       ℝ 上求导                                simp → ring
    t6   d/dx cos x = -sin x      ℝ 上求导                                simp → ring
    t8   d/dx sin(x^2)            ℝ 上求导 + 链式法则                     simp → ring
    t10  d/dx(x/2) = 1/2          ℝ 上求导 + 商                           simp → ring

  ## 为什么这算「独立」

  `ring` / `deriv` 来自 Mathlib，本目录**不 import 本仓库的任何模块**。
  它们不认识 Aether 的 `canonical`、不认识那 158 行手写引理、不认识
  `Deriv.dtable`。所以这些 example 全部编译通过，就等于说：

      「一个完全无关的实现（Mathlib）也认为这些等式成立。」

  这正是自带 `certcheck` 给不了的东西——`certcheck` 和生成它的代码是同一批人
  写的，万一某条引理写错，它会「自洽地错」；Mathlib 不会跟着一起错。

  ## 怎么跑

    # 1) 拉取 Mathlib 预编译缓存（几百 MB，数分钟）
    #    没有缓存也能跑，但会从源码编译 Mathlib，需要数小时
    lake exe cache get

    # 2) 编译 = 校验。全部通过即代表每一步都被 Mathlib 认可
    lake build
    # 或者只查这一份文件：
    lake env lean Examples.lean

  任意一条被改坏，都会在对应行报错——与 certcheck 的行为一致，
  只是换了一个更值得信任的裁判。

  ## 这些内容怎么来的

  由 `cert2lean` 从 `../lean/Test/certs/*.cert` 直接生成，并非手抄：

    cd ../lean && lake build cert2lean
    .lake/build/bin/cert2lean Test/certs/t1.cert

  ## 已知边界

  - 只覆盖证书实际表达的四则 / 整数幂 / 12 个初等函数；`sin²+cos²=1` 这类
    需要三角恒等式的、或 `x/x=1` 这类有定义域前提的，证书本身就证不出，
    这里自然也验不了（两套实现的能力边界一致）。
  - 若某条 deriv 步在当前 Mathlib 版本上 `simp` 未能完全化简，可把该条的
    tactic 换成 `fun_prop` 或 `by simpa using HasDerivAt.sin ...` 手写。
    `ℚ` 上的恒等（t1 / t12）用 `ring`，跨版本极稳。
-/
import Mathlib

namespace AetherCertGenerated

/-! ## 代数恒等式（ℚ 上，`ring`） -/

/-- t1：(x+1)^2 = x² + 2x + 1 -/
example (x : ℚ) : ((x + (1 : ℚ)) ^ 2) = (((x ^ 2) + ((2 : ℚ) * x)) + (1 : ℚ)) := by
  ring

/-- t12 第 1 步：x(x+2) = x² + 2x -/
example (x : ℚ) : (x * (x + (2 : ℚ))) = ((x ^ 2) + ((2 : ℚ) * x)) := by
  ring

/-- t12 第 2 步：x² + 2x = 2x + x² -/
example (x : ℚ) : ((x ^ 2) + ((2 : ℚ) * x)) = (((2 : ℚ) * x) + (x ^ 2)) := by
  ring

/-! ## 求导（ℝ 上，`deriv` + `ring`） -/

/-- t2：d/dx (x² + 3x) = 2x + 3 -/
example (x : ℝ) : deriv (fun _t : ℝ => ((_t ^ 2) + ((3 : ℝ) * _t))) x = (((2 : ℝ) * x) + (3 : ℝ)) := by
  simp <;> ring

/-- t5：d/dx sin x = cos x -/
example (x : ℝ) : deriv (fun _t : ℝ => Real.sin _t) x = Real.cos x := by
  simp <;> ring

/-- t6：d/dx cos x = -sin x -/
example (x : ℝ) : deriv (fun _t : ℝ => Real.cos _t) x = (-Real.sin x) := by
  simp <;> ring

/-- t8：链式法则 d/dx sin(x²) = cos(x²) · 2x -/
example (x : ℝ) :
    deriv (fun _t : ℝ => Real.sin (_t ^ 2)) x = (Real.cos (x ^ 2) * ((2 : ℝ) * x)) := by
  simp <;> ring

/-- t10：商 d/dx (x/2) = 1/2 -/
example (x : ℝ) : deriv (fun _t : ℝ => (_t / (2 : ℝ))) x = ((1 : ℝ) / (2 : ℝ)) := by
  simp <;> ring

end AetherCertGenerated
