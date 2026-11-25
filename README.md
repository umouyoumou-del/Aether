<div align="center">

# Aether（五行）

**Python 的写法，静态类型的安全，原生代码的速度。**

一门融合 Python、C++、Go、Java、C# 优点的编程语言：Python 的简洁语法与推导式、静态类型与类型推断、
编译为不依赖运行时的原生可执行文件、Go 的协程与通道、Java/C# 的类/接口/异常/属性与空安全，
并自带可用 GPU 的深度学习框架与纯 Aether 实现的符号数学库。

[![Go 1.23+](https://img.shields.io/badge/Go-1.23%2B-00ADD8?logo=go)](https://go.dev/dl/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Windows%20%7C%20Linux%20%7C%20macOS-lightgrey)](https://github.com/umouyoumou-del/Aether)
[![Stars](https://img.shields.io/github/stars/umouyoumou-del/Aether?style=social)](https://github.com/umouyoumou-del/Aether/stargazers)

[快速开始](#快速开始) · [特性](#特性) · [深度学习](#深度学习) · [性能](#性能) · [文档](#文档) · [贡献](#贡献)

</div>

---

## 为什么是 Aether

Python 写起来舒服，但类型错误要到运行时才发现，发布还要带一整套解释器；
C++/Rust 快且安全，但写起来远没有 Python 轻快。Aether 的目标是把这条裂缝补上：

- **写起来像 Python**：缩进、推导式、切片、f-字符串、`with`、`match`、生成器、装饰器。
- **查起来像 Java/C#**：编译期类型推断、空安全窄化、确定赋值、泛型约束、穷尽性检查，中文报错带修改建议。
- **跑起来像 Go/C++**：生成 Go 代码后编译为单个原生可执行文件，不需要 Python 解释器；比 CPython 快 2～34 倍。
- **并发像 Go**：`go`、`chan`、`select`、`defer`、`async`/`await` 一套齐全。
- **自带深度学习**：张量、自动求导、常见网络层与优化器，GPU 走 OpenCL，**不需要装 CUDA/ROCm**。

```python
# hello.ae
def fib(n: int) -> int:
    return n if n < 2 else fib(n - 1) + fib(n - 2)

scores = {"小明": 92, "小红": 88, "小刚": 75}
good = scores.items().filter(kv => kv[1] >= 80).map(kv => kv[0])
print(f"fib(30) = {fib(30)}，80 分以上：{good}")
```

```
$ aether run hello.ae
fib(30) = 832040，80 分以上：["小明", "小红"]
```

## 名字的由来

**Aether**（以太 / 五行之“气”）是古希腊哲学中的第五元素——土、水、气、火之外构成天空与星辰的元素。
Aether 吸收了五门语言各自最好的部分：

| 来源 | Aether 吸收了什么 |
|---|---|
| **Python** | 缩进语法、推导式、切片与负下标、f-字符串、生成器 `yield`、关键字参数与默认值、`with`、`match` |
| **C++** | 编译为原生代码、值类型 `struct`、运算符重载（`__add__` 等）、泛型、编译期常量 `const` |
| **Go** | `go` 协程、`chan` 通道、`select`、`defer`、结构化接口、一体化工具链、单个可执行文件 |
| **Java** | 静态类型检查、类与单继承、抽象类、接口、异常体系、`enum`、确定赋值分析 |
| **C#** | 属性 `prop`、LINQ 式链式方法、`?.` `??` 空安全、`async`/`await`、模式匹配、扩展方法（包括接口的扩展方法） |

## 快速开始

需要 [Go 1.23+](https://go.dev/dl/)（国内可用 [golang.google.cn](https://golang.google.cn/dl/)）。
Aether 把 `.ae` 源码翻译成 Go，再由 Go 编译为原生程序。

### Windows（PowerShell）

**方式一：MSI 安装包（推荐）**——下载 Release 中的 `Aether-0.2.0-x64.msi`，双击安装。
它会把编译器装到 `Program Files\Aether`、自动加入系统 PATH、并创建开始菜单快捷方式。
安装后打开任意终端即可使用 `aether run hello.ae`（编译程序仍需本机有 Go 工具链）。

**方式二：源码构建**

```powershell
git clone https://github.com/umouyoumou-del/Aether.git
cd Aether
powershell -ExecutionPolicy Bypass -File install.ps1             # 构建 bin\aether.exe 并运行 hello 示例
powershell -ExecutionPolicy Bypass -File install.ps1 -AddToPath  # 同时把 bin 加入用户 PATH
```

### Linux / macOS

```sh
git clone https://github.com/umouyoumou-del/Aether.git
cd Aether
sh install.sh                 # 构建 bin/aether，运行 hello 示例，并检查 GPU 环境
sh install.sh --add-to-path   # 同时把 bin 加入 PATH（写入 ~/.bashrc）
```

在 Linux 上用 GPU 还需要 C 编译器与 OpenCL 加载器（Ubuntu/Debian：`apt install build-essential ocl-icd-libopencl1`），详见 [深度学习文档](docs/深度学习.md)。

也可以手动构建：`go build -o bin/aether ./cmd/aether`（Windows 为 `bin\aether.exe`）。
第一次运行程序时 Go 需要编译标准库，会多花几秒钟。

### 命令一览

| 命令 | 作用 |
|---|---|
| `aether run 文件.ae [参数...]` | 编译并运行（同一程序第二次运行会直接使用缓存的可执行文件） |
| `aether build 文件.ae [-o 输出]` | 编译为原生可执行文件（不依赖任何运行时） |
| `aether check 文件.ae` | 只做语法与类型检查 |
| `aether test 文件.ae` | 运行文件中的 `test "名字":` 测试块 |
| `aether emit 文件.ae` | 查看生成的 Go 代码 |
| `aether repl` | 交互模式：输入表达式立即看到结果 |
| `aether lsp` | 语言服务器（供 VS Code 等编辑器使用） |

## 特性

### 类型推断与静态检查

```python
name = "Aether"          # 推断为 str
nums = [1, 2, 3]         # list[int]
ages: dict[str, int] = {}
ratio: float = 1         # int 可以隐式拓宽为 float

nums.append("4")         # 编译错误：参数 x：类型不匹配，需要 int，实际是 str
```

错误信息是中文的，并指出位置与修改建议：

```
nulls.ae:8:8: 错误: 值可能为 null，不能直接调用 .upper()
    print(r.upper())
           ^
    提示: 使用 ?. 安全调用，或先判断 if x != null，或用 x!.upper()
```

### 函数、lambda 与闭包

```python
def greet(name: str, greeting: str = "你好", *, punct: str = "！") -> str:
    return f"{greeting}，{name}{punct}"

def total(*nums: int, scale: int = 1) -> int:
    return sum(nums) * scale

double = (x: int) => x * 2
print(greet("世界", punct="~"), total(1, 2, 3, scale=10), double(21))
```

### 熟悉的 Python 写法

```python
type Graph = dict[str, list[(str, int)]]       # 类型别名（也可以带类型参数）
nums = list(map(int, input().split()))        # 内置函数、类型名可以直接当作函数传递
longest = max(words, key=len)
first, *rest = sorted(nums)                    # 星号解包
xs[1:3] = [0, 0, 0]                            # 切片赋值；del xs[::2]
config = {**defaults, **overrides}
while (line := f.read_line()) != null:         # 赋值表达式，并且 line 被窄化为 str
    print(line)

@cache                                         # 缓存结果；也支持 @lru_cache(maxsize=...)
def fib(n: int) -> int:
    return n if n < 2 else fib(n - 1) + fib(n - 2)
```

### 集合、推导式与链式查询

```python
squares = {n: n * n for n in range(5)}
evens = [x for x in range(20) if x % 2 == 0]
top = students.filter(s => s.score >= 85).sorted_by(s => -s.score).map(s => s.name)
by_city = students.group_by(s => s.city)
print(sum(x * x for x in evens), "第一行"[:2], [1, 2, 3][-1])
```

### 类、继承、接口、属性

```python
abstract class Shape:
    abstract def area(self) -> float
    def describe(self) -> str:
        return f"面积 {self.area():.2f}"        # 虚方法：运行时分派到子类

class Circle(Shape):
    r: float
    override def area(self) -> float:
        return 3.14159 * self.r * self.r

interface Drawable:                             # 结构化接口：有 draw() 就满足
    def draw(self) -> str

class Account:
    _balance: float = 0.0
    prop balance -> float:                      # C# 风格的属性
        return self._balance
```

没有 `__init__`、只声明了字段的类会自动生成构造函数：`Point(1, 2)`、`Point(x=1, y=2)`。

### 值类型与运算符重载

```python
struct Vec:
    x: float
    y: float
    def __add__(self, o: Vec) -> Vec:
        return Vec(self.x + o.x, self.y + o.y)

v = Vec(1.0, 2.0) + Vec(3.0, 4.0)      # struct 按值复制、按字段比较，可以作为字典的键
```

### 扩展方法与 Comparable

```python
extend str:                                     # 为已有类型（包括内置类型）添加方法
    def shout(self) -> str:
        return self.upper() + "!"

interface Shape:
    def area(self) -> float

extend Shape:                                   # 接口的扩展：所有实现了 Shape 的类型都能用
    def describe(self) -> str:
        return f"面积 {self.area():.1f}"

struct Version:
    major: int
    minor: int
    def __lt__(self, other: Version) -> bool:   # 定义 __lt__ 就实现了 Comparable[Version]
        return (self.major, self.minor) < (other.major, other.minor)

vs = [Version(1, 2), Version(0, 9)]
print(sorted(vs), max(vs), vs.min())            # 可以直接排序、求最值
def smallest[T: Comparable[T]](xs: list[T]) -> T: ...
```

### 枚举、代数数据类型与模式匹配

```python
enum Shape:
    Circle(r: float)
    Rect(w: float, h: float)
    Empty

def area(s: Shape) -> float:
    match s:
        case Circle(r):
            return 3.14159 * r * r
        case Rect(w, h) if w == h:
            return w * w
        case Rect(w, h):
            return w * h
        case Empty:
            return 0.0
```

`match` 还支持字面量、`|`、守卫 `if`、元组、列表 `[first, *rest]`、类型模式 `case int n`；没有覆盖所有枚举成员时会给出警告。

### 空安全与异常

```python
def find(xs: list[str], p: str) -> str?:      # T? 表示可能为 null
    ...
r = find(langs, "J")
print(r?.upper() ?? "没找到")
if r != null:
    print(len(r))                              # 这里 r 自动窄化为 str

class InsufficientFunds(Exception):
    pass

try:
    withdraw(500.0)
except InsufficientFunds as e:
    print("失败：", e)
finally:
    ...
```

未捕获的异常会打印 Python 风格的回溯（含源码行），列表越界、除以零、`null` 解引用等运行时错误都会转换为对应的异常类。

### 并发：协程、通道、select、async/await

```python
def worker(jobs: chan[int], results: chan[int]):
    for n in jobs:
        results.send(n * n)

jobs = chan[int](10)
results = chan[int](10)
for _ in range(4):
    go worker(jobs, results)

select:
    case v = results.recv():
        print(v)
    case time.after(1.0).recv():
        print("超时")

async def fetch(url: str) -> int:
    ...
sizes = await [fetch(u) for u in urls]          # 并发执行，等待全部完成
```

### 泛型、模块与测试

```python
def naturals() -> iter[int]:                   # 生成器
    n = 1
    while true:
        yield n
        n += 1

def max_of[T: Ordered](xs: list[T]) -> T: ...
class Stack[T]: ...
import geo.shapes as gs                        # 导入 geo/shapes.ae

test "加法":                                   # aether test 文件.ae
    assert add(1, 2) == 3
```

### 标准库

内置：`print` `input` `len` `range` `enumerate` `zip` `sorted` `reversed` `sum` `min` `max` `any` `all` `abs` `round` `open` 等；
模块：`math` `random` `time` `datetime` `sys` `os` `os.path` `json` `re` `csv` `http` `heapq` `bisect` `collections` `itertools` `functools` `statistics` `string` `textwrap` `base64` `hashlib`，以及深度学习的 `tensor` `tensor.nn` `tensor.optim`。完整列表见 [docs/标准库.md](docs/标准库.md)。

```python
import http
server = http.Server()
server.get("/hello/{name}", req => http.text(f"你好，{req.param("name") ?? "世界"}！"))
server.listen(":8080")
```

## 深度学习

Aether 自带深度学习框架：多维张量、自动求导、常见网络层、损失函数与优化器，接口与 PyTorch 基本一致。
GPU 后端使用显卡驱动自带的 **OpenCL**（NVIDIA / AMD / Intel 均可），**不用安装 CUDA 或 ROCm**；Windows 与 Linux 都可用。

```python
import tensor
import tensor.nn as nn
import tensor.optim as optim

tensor.set_default_device("gpu")
x, y = tensor.randn(256, 2), tensor.randint(0, 3, [256])
model = nn.Sequential(nn.Linear(2, 64), nn.ReLU(), nn.Linear(64, 3))
opt = optim.Adam(model.parameters(), lr=0.01)
for step in range(100):
    loss = nn.CrossEntropyLoss()(model(x), y)
    opt.zero_grad()
    loss.backward()                           # 自动求导
    opt.step()
```

- **层与优化器**：`Linear` / `Conv2d` / `BatchNorm` / `LayerNorm` / `RMSNorm` / `SiLU` / `Embedding` / `Dropout` / `LSTM` / `GRU` / `MultiheadAttention` / `TransformerEncoderLayer`；SGD / Adam / AdamW；学习率调度；模型保存与读取（含 safetensors、int8 权重）。
- **示例**：`examples/deep_learning.ae`（多层感知机）、`examples/cnn.ae`（卷积网络）、`examples/gpt.ae`（会“写唐诗”的小 GPT）。
- **MiniJev / QwenJev**：`examples/minijev/` 是仿照 [TypeSafe Jev](https://docs.typesafe.ai) 接口的小巧 System One 决策模型；其中 QwenJev 用 **LoRA 把 Qwen3-1.7B 微调成同接口的模型**（分词器、模型、训练全部用 Aether 实现），并提供与 TypeSafe System One API 兼容的 HTTP 服务。详见 [examples/minijev/README.md](examples/minijev/README.md)。

完整文档见 [docs/深度学习.md](docs/深度学习.md)。

## 符号数学

`sym` 用纯 Aether 实现：同一份表达式既能符号推导、又能编译成原生代码求值、还能检查恒等。

```python
import sym

x = sym.var("x")
f = (x + 1) ** 2

print(sym.expand(f))                        # x^2 + 2*x + 1
print(sym.diff(f, "x"))                     # 2*x + 2
print(sym.equals(f, x ** 2 + 2 * x + 1))    # true

g = sym.compile_expr(f, ["x"])              # 编译成闭包树，不再按节点种类解释
print(g([3.0]))                             # 16.0
```

- 表达式带**值域与可微性**元数据：整数系数的展开保持精确，含 `abs` 的表达式求导会直接报错而不是给出错误结果。
- 构造时即归到**规范形式**（合并同类项、同底数幂合并、`sin^2+cos^2=1`），因此 `equals` 是确定的判定而不是碰运气。
- 支持展开、求导（含链式法则与商法则）、代入、编译求值、一元二次方程与数值积分。
- 另有 `egraph`：基于并查集与同余闭包的**相等饱和**，能证明需要“绕路”的恒等式（如 `(a+b)^2 = a^2+2ab+b^2`），
  并通过代价提取把 `x*y + x*z` 因式分解成 `x*(y + z)`。
- 还有 `codegen`：把表达式生成 Aether 源码（自动提取公共子表达式、`x^2` 换成 `x*x`、连偏导一起生成），
  再编译成原生可执行文件——同一份表达式既可符号推导，也可编译执行，结果由 `verify` 逐点比对。
- 还有 `latex`：**直接读 LaTeX 源码**参与运算，结果再写回 LaTeX。论文里粘来一条公式就能求导、
  证明恒等（`latex.prove_equal_latex`）、求值、生成原生函数；从 PDF 复制的 `πr²`、`x³ − 2x` 也能读。
- 还有 `calc`：在表达式树上做真正的计算——**求和/连乘的闭式公式**（`Σk² = n(n+1)(2n+1)/6`，
  任意次幂和走 Faulhaber + 伯努利数，`Σk·2^k` 这类「多项式乘等比」也有闭式）、
  **极限**（洛必达 + 数值外推 + 左右极限判定 + 振荡检测）、**符号积分**（规则表 + 三角幂 + 一般换元 +
  分部积分 + 二次不可约分式，定积分自动处理瑕点）、**泰勒展开**、**解方程**（三次 Cardano、四次 Ferrari、
  更高次数值求根）、**线性方程组**、**矩阵**（行列式、逆、乘、转置、迹，数值行列式不限阶）。
  `\sum \prod \lim \int`、`\frac{\partial}{\partial x}`、`\begin{pmatrix}…\end{pmatrix}`、
  `\begin{cases}…\end{cases}` 这些 LaTeX 记号都能直接读进来算。
- **不定函数的符号微分**：`f(x)`、`g(x, y)`、`f'(x)`、`f^{(3)}(x)`、`f_{1,2}(x, y)` 都是不带定义的记号，
  求导按链式法则得到偏导记号（`d/dx f(g(x)) = f'(g(x))·g'(x)`，混合偏导 `g_{12} = g_{21}` 是同一个节点）；
  `subs_fn` 再把函数换成具体表达式，偏导记号自动换成相应的偏导。
- **证书与独立校验**：`cert` 把 `sym` 的展开、求导、判等推导导出成纯文本「证书」（`.cert`），交给
  `lean/` 下一个**独立的 Lean 4 校验器**（`certcheck`）核对。校验器不复用 Aether 的代码、也不相信证书里
  写的结论——它只按证书声明的每一步自己重推一遍，因此推导被篡改时会精确报到出错的第几行、内核实算得什么。
  信任边界只有一张 12 行的初等导数表；`same` 声明更是零假设（只有两侧独立规范化后完全一致才算通过）。
  为避免「自己证自己」，`cert2lean` 还能把每一步翻成 **Mathlib** 命题（`ring` / `deriv`），
  由完全无关的数学库做**第二见证人**——见 `lean-mathlib/`。
  详见 [lean/README.md](lean/README.md) 与 [数学库第 15 节](docs/数学库.md)。

```python
import latex

latex.diff_latex(r"\exp\left(-\frac{x^2}{2}\right)", "x")          # -x \, \exp\left(-0.5x^{2}\right)
latex.prove_equal_latex(r"(a+b)^3", r"a^3 + 3a^2b + 3ab^2 + b^3")     # true
latex.to_latex(sym.expand(latex.parse(r"(x+y)^2")))                   # x^{2} + 2x \, y + y^{2}

latex.sum_latex(r"\sum_{k=1}^{n} k^2")             # \frac{1}{6}n \, \left(2n + 1\right) \cdot \left(n + 1\right)
latex.sum_latex(r"\sum_{k=0}^{\infty} k \left(\frac{1}{2}\right)^k")   # 2
latex.limit_latex(r"\lim_{x\to 0} \frac{e^x - 1 - x}{x^2}")   # 0.5（洛必达）
latex.limit_latex(r"\lim_{x\to 0} \sin\left(\frac{1}{x}\right)")  # 抛错：振荡，极限不存在
latex.integrate_latex(r"\int x e^x \, dx")            # x \, \exp\left(x\right) - \exp\left(x\right)
latex.diff_latex(r"f(g(x))", "x")                     # f'\left(g\left(x\right)\right) \cdot g'\left(x\right)
latex.subs_fn_latex(r"f'(x) + f(x)", "f", r"\sin u", ["u"])   # \cos\left(x\right) + \sin\left(x\right)
latex.integrate_latex(r"\int x e^{x^2} \, dx")        # \frac{1}{2}\exp\left(x^{2}\right)（换元）
latex.integrate_latex(r"\int \sin^2 x \cos^3 x \, dx")  # \frac{1}{5}\sin\left(x\right)^{5} + \frac{1}{3}\sin\left(x\right)^{3}
latex.definite_latex(r"\int_0^{\pi} \sin x \, dx")    # 2.0
latex.taylor_latex(r"\sin x", "x", 0.0, 7)            # -\frac{1}{5040}x^{7} + \frac{1}{120}x^{5} - \frac{1}{6}x^{3} + x
latex.solve_latex(r"x^3 - 3x + 2 = 0", "x")            # x = -2, x = 1
```

见 [docs/数学库.md](docs/数学库.md)。

## 性能

Aether 编译为原生代码。以下是同一份程序在 Windows 11 上与 Python 3.13 的对比（单位：秒）：

| 测试 | Aether | Python 3.13 | 倍数 |
|---|---:|---:|---:|
| 递归 fib(32) | 0.010 | 0.325 | 32× |
| 质数筛（20 万以内） | 0.006 | 0.204 | 34× |
| 字典计数（100 万次） | 0.019 | 0.202 | 11× |
| 字符串拼接（20 万次） | 0.015 | 0.033 | 2× |
| 浮点累加（500 万次） | 0.015 | 0.387 | 26× |

`examples/mandelbrot.ae` 中一百万个点、约 1.1 亿次迭代用时约 0.3 秒。

## 工作原理

```
.ae 源码 → 词法分析（缩进 → INDENT/DEDENT）→ 语法分析（AST）
        → 类型检查（推断、泛型、空安全窄化、确定赋值、穷尽性检查）
        → 生成 Go 代码（带 //line 指令，回溯可以定位到 .ae 源码行）
        → go build → 原生可执行文件
```

运行时库（列表、有序字典、集合、可空值、格式化、异常回溯等）是一个 Go 包，嵌入在 `aether` 可执行文件中。
详见 [docs/实现原理.md](docs/实现原理.md)。

## 项目结构

```
cmd/aether/         命令行工具
compiler/           编译器：lexer、parser、checker、代码生成、构建驱动、测试
assets/rt/          运行时库（Go），含 tensor 的 CPU/GPU 内核
assets/lib/         预导入模块 prelude.ae 与标准库（.ae）
examples/           示例程序（计算器、银行、并发、Web 服务、生命游戏、深度学习……）
examples/minijev/   MiniJev 与 QwenJev：完整的训练与推理示例
tests/run/          黄金测试（程序 + 期望输出）
tests/errors/       编译错误测试
docs/               语言参考、标准库、深度学习、实现原理
lean/               独立的 Lean 4 证书校验器（certcheck），核对 cert 模块导出的 .cert
lean-mathlib/       用 Mathlib 独立交叉验证证书的示例工程（可选，需拉 Mathlib）
editors/vscode/     VS Code 插件（语法高亮 + 语言服务器客户端）
```

运行全部测试：

```sh
cd compiler && go test .
```

## 编辑器支持

`editors/vscode` 提供语法高亮，以及通过 `aether lsp` 实现的**实时中文诊断、悬停显示类型、跳转到定义、成员补全和大纲**。
其它支持 LSP 的编辑器也可以直接使用 `aether lsp`。详见 [editors/vscode/README.md](editors/vscode/README.md)。

## 文档

| 文档 | 内容 |
|---|---|
| [语言参考](docs/语言参考.md) | 语法、类型系统、语句与表达式、模块、泛型的完整说明 |
| [标准库](docs/标准库.md) | 内置函数与所有标准库模块的 API |
| [深度学习](docs/深度学习.md) | 张量、自动求导、nn / optim、GPU 与性能 |
| [实现原理](docs/实现原理.md) | 编译器管线、运行时与代码生成 |
| [数学库](docs/数学库.md) | 符号表达式、化简展开、求导、e-graph 相等饱和、生成原生代码、LaTeX 读写、证书导出与校验 |
| [Lean 证书校验器](lean/README.md) | 独立校验 `cert` 导出的推导证书：用法、证书格式、信任边界 |
| [MiniJev / QwenJev](examples/minijev/README.md) | 用 Aether 从零训练决策模型与微调 Qwen3 |

## 当前限制

- 整数是 64 位，溢出时回绕（不像 Python 那样自动变成大整数）。
- 方法不能重载（扩展方法可以按参数个数区分）；装饰器只支持 `@cache` / `@lru_cache`；不支持多重继承、`**kwargs`。
- 无限递归会因栈空间耗尽而终止（`aether run` 会显示中文说明），不能被 `try` 捕获。
- 编译和运行需要本机安装 Go 工具链；`aether build` 生成的可执行文件可以在没有 Go 的机器上运行。

## 贡献

欢迎提交 issue 和 pull request。贡献前请：

1. 在 issue 中说明你想改动的内容（大改动尤其如此）；
2. 保持错误信息为中文，并遵循现有代码风格；
3. 修改编译器后运行 `cd compiler && go test .`，确认全部测试通过；
4. 新增语法特性时，同时补上 `tests/run/` 的黄金测试或 `tests/errors/` 的错误测试，并更新 [语言参考](docs/语言参考.md)。

## 许可

本项目使用 [MIT 许可证](LICENSE)，可自由使用、修改与分发，包括商业用途。
