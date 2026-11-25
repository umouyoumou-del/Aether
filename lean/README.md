/-
  AetherCert —— 证书系统的 Lean 4 侧独立校验器

  ## 这个包做什么

  Aether 的数学库（`sym`/`calc`）做符号计算，把每一步推导导出成文本证书
  （`.cert` 文件）。本包提供 `certcheck` 可执行文件，**独立地**逐行校验这份
  证书：不信任 Aether 侧的任何结论，只承认「规范化后两边多项式恒等」。

  校验全部用 Lean 4 核心库完成，**不依赖 Mathlib** —— 可信性靠「假设最少」
  换来，而不是靠某个大库的正确性。

  ## 用法

      certcheck my.cert        # 校验文件
      certcheck -              # 从 stdin 读
      echo "0"                 # 退出码：0 通过 / 1 拒绝 / 2 用法或读取错误

      cert2lean my.cert        # 把证书翻译成一份可由 Mathlib 独立核对的 .lean 源码
      cert2lean - < my.cert    # 同样支持 stdin

  ## 与 Mathlib 的交叉验证（第二见证人）

  本包的 `certcheck` 是「自己证自己」——它和生成证书的代码出自同一批人，
  万一某条引理写错，它会自洽地错。为补上这一点，`cert2lean` 把证书的每一步
  翻译成一条 **Mathlib** 命题：

      same 步  →  example (x : ℚ) : lhs = rhs := by ring
      diff 步  →  example (x : ℝ) : deriv (fun t => lhs) x = rhs := by simp <;> ring

  `ring` / `deriv` 来自 Mathlib，不认识本仓库的 `canonical`、手写引理或
  `dtable`。把这些 example 编译通过，就等于「一个完全无关的实现也认为它们成立」。
  现成的校验工程在 `../lean-mathlib/`（见其 `README.md`，一条 `lake exe cache get`
  + `lake env lean Examples.lean` 即可）。

  > 注意：`cert2lean` 本身**不依赖 Mathlib**（本包编译不需要它），只有它生成
  > 的 `.lean` 文件才 `import Mathlib`。所以没装 Mathlib 也能在本机跑翻译、
  > 检查生成的源码。

  ## 证书格式（前缀式，逐行）

      aethercert 1                      # 魔数 + 版本
      vars x y                          # 变量表（可选，用于文档）
      start <term>                      # 起始项
      same <term>                       # 一步等价变换：与当前项恒等
      diff x <term>                     # 一步求导：与内核 d/dx(当前项) 恒等
      claim <term>                      # 可选：最终项应与它恒等
      end

  项（term）的语法：

      (num 1/2)  (num 3)  (num -1.5)
      (var x)
      (add a b)  (mul a b)  (div a b)  (neg a)  (pow a 2)
      (fn sin a)                        # 函数名限定在白名单内

  ## 信任边界（重要）

  校验分两种步，各自的假设写在这里，一目了然：

  - `same` 步：**零假设**。只判 `canonical 上一项 = canonical 声称项`，
    而 `canonical_sound`（见 `AetherCert/Sort.lean`）已证「canonical 保持语义」。

  - `diff` 步：**唯一假设是 `Deriv.dtable` 那张 12 行导数表**
    （`sin'=cos`、`cos'=-sin` … 见 `AetherCert/Deriv.lean`）。内核把
    `deriv x 上一项` 算出来，再判它与声称项恒等。除这张表外不假设任何东西。

  换句话说：想审计这套系统，只需逐行看 `dtable` 那 12 行是否与你想承认的
  微积分一致，其余全是机器验证的代数变形。

  ## 目录结构

      AetherCert/Term.lean    项的定义、渲染、函数白名单
      AetherCert/Basic.lean   手写列表/Rat 引理（不引 Mathlib 的代价）
      AetherCert/Eval.lean    参数化语义：eval Φ σ，函数符号当作不透明原子
      AetherCert/Monom.lean   单项式 = 原子列表，及其语义
      AetherCert/Poly.lean    多项式 = (单项式, 系数) 表，及其语义
      AetherCert/Norm.lean    规范化 norm，及可靠性 norm_sound
      AetherCert/Sort.lean    排序 + 合并同类项 → canonical，及 canonical_sound
      AetherCert/Parse.lean   证书文本解析（前缀语法）
      AetherCert/Deriv.lean   形式导数 + 12 行导数表 dtable
      AetherCert/Check.lean   逐步校验、runSteps、checkCert
      AetherCert/ToMathlib.lean  项 → Lean/Mathlib 表达式文本（不依赖 Mathlib）
      AetherCert/Gen.lean     证书 → Mathlib 证明源码
      Main.lean               certcheck 命令行入口
      GenMainCert.lean        cert2lean 命令行入口

  ## 设计取舍（为什么这么小）

  只证「规范化保持语义」（可靠性），**不证**「规范形唯一」（完备性）。

      canonical a = canonical b  ⟹  eval a = eval b      -- 这是能判等的原因

  所以 `norm` 不合并同类项、不排序都不影响**正确性**，只影响**能判出多少**。
  完备性靠 `Sort.lean` 的排序 + 合并同类项补上一部分，但刻意留缺口：
  `sin²+cos²=1` 判不出来（要判它就得引入三角恒等式，那就不是「零假设」了）。

  ## 测试

      export PATH="/e/Apps/elan/bin:$PATH" && export ELAN_HOME='E:\Apps\elan'
      lake env lean --run Test/Certs.lean       # 证书端到端回归，期望全 PASS
      lake env lean Test/Canonical.lean         # canonical 完备性断言（native_decide）

  `Test/certs/` 下是手工证书样本：`t1`…`t12`（应通过）与 `*_bad`（应被拒）。
  `t3/t4/t7/t9_bad` 测「校验期拒绝」，`t11_bad` 测「解析期拒绝」，两条失败路径都覆盖。
-/
