# 用 Mathlib 独立校验

本目录用 **Mathlib**（一个与本仓库完全无关的数学库）重新核对 Aether 证书系统
导出的每一步等式，作为自带 `certcheck` 之外的「第二见证人」。

| 文件 | 作用 |
|---|---|
| `Examples.lean` | 从 `../lean/Test/certs/*.cert` 生成的 Mathlib 证明（本目录的核心） |
| `lakefile.lean` | 依赖 Mathlib，锁定到 `v4.24.0`（与本仓库 Lean 版本一致） |
| `lean-toolchain` | `leanprover/lean4:v4.24.0` |

## 一步到位

```bash
cd lean-mathlib
lake exe cache get      # 拉 Mathlib 预编译缓存（几百 MB，数分钟；没缓存则源码编，数小时）
lake env lean Examples.lean   # 编译通过 = 每一步都被 Mathlib 认可
```

`lake env lean Examples.lean` 无任何输出即代表全部通过；任一行报错 = 证书在
那一步被改坏。

## 这些证明从哪来

不是手抄的，是 `cert2lean` 直接从证书文件生成的：

```bash
cd ../lean
lake build cert2lean
.lake/build/bin/cert2lean Test/certs/t1.cert        # 打印生成的 Mathlib 源码
.lake/build/bin/cert2lean - < my.cert               # 也支持 stdin
```

## 为什么这算「独立」

`ring`、`deriv` 都来自 Mathlib，本目录不 import 本仓库的任何模块。
Mathlib 不认识 Aether 的 `canonical`、那 158 行手写引理、或 `Deriv.dtable`。
所以这些 example 全过 = 一个无关实现也认为它们成立——这正是 `certcheck`
本身给不了的（它和生成它的代码是同一批人写的）。

## 能力边界

证书能表达的（四则、整数幂、12 个初等函数）这里都能验；证书表达不了的
（`sin²+cos²=1` 这类需三角恒等式的、`x/x=1` 这类有定义域前提的）这里也验不了，
两套实现边界一致。细节见 `Examples.lean` 头注释。
