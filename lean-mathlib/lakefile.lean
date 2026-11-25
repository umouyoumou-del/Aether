import Lake
open Lake DSL

/-- 用 Mathlib 独立校验 Aether 证书的最小示例工程。

    不需要自定义 target：`lake build` 会把 Mathlib 依赖编好（或从缓存拉取），
    然后用 `lake env lean Examples.lean` 校验我们的示例文件即可。 -/
package «aether-mathlib-check»

require mathlib from git
  "https://github.com/leanprover-community/mathlib4" @ "v4.24.0"
