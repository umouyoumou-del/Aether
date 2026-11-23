# 五行 · Aether

**一门融合 Python、C++、Go、Java、C# 优点的编程语言。**

Python 的简洁语法 + 静态类型与类型推断 + 编译为原生可执行文件 + Go 的协程与通道 + Java/C# 的类、接口与异常 + C# 的空安全与 LINQ。

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

“五行”取金、木、水、火、土五种元素，对应五门语言各自最好的部分；英文名 **Aether**（以太）是古希腊的“第五元素”。

| 元素 | 来源 | 五行吸收了什么 |
|---|---|---|
| 木 · 生长 | **Python** | 缩进语法、推导式、切片与负下标、f-字符串、生成器 `yield`、关键字参数与默认值、`with`、`match` |
| 金 · 坚硬 | **C++** | 编译为原生代码、值类型 `struct`、运算符重载（`__add__` 等）、泛型、编译期常量 `const` |
| 水 · 流动 | **Go** | `go` 协程、`chan` 通道、`select`、`defer`、结构化接口、一体化工具链、单个可执行文件 |
| 土 · 稳固 | **Java** | 静态类型检查、类与单继承、抽象类、接口、异常体系、`enum`、确定赋值分析 |
| 火 · 灵动 | **C#** | 属性 `prop`、LINQ 式链式方法、`?.` `??` 空安全、`async`/`await`、模式匹配、扩展方法（包括接口的扩展方法） |

## 快速开始

需要 [Go 1.23+](https://go.dev/dl/)（五行把程序编译为 Go 代码，再由 Go 编译为原生可执行文件）。

```powershell
cd E:\Aether
powershell -ExecutionPolicy Bypass -File install.ps1             # 构建 bin\aether.exe 并运行 hello 示例
powershell -ExecutionPolicy Bypass -File install.ps1 -AddToPath  # 同时把 bin 加入用户 PATH
```

也可以手动构建：`go build -o bin\aether.exe .\cmd\aether`。第一次运行程序时 Go 需要编译标准库，会多花几秒钟。

| 命令 | 作用 |
|---|---|
| `aether run 文件.ae [参数...]` | 编译并运行（同一程序第二次运行会直接使用缓存的可执行文件） |
| `aether build 文件.ae [-o 输出]` | 编译为原生可执行文件（不依赖任何运行时） |
| `aether check 文件.ae` | 只做语法与类型检查 |
| `aether test 文件.ae` | 运行文件中的 `test "名字":` 测试块 |
| `aether emit 文件.ae` | 查看生成的 Go 代码 |
| `aether repl` | 交互模式：输入表达式立即看到结果 |
| `aether lsp` | 语言服务器（供 VS Code 等编辑器使用） |

## 特性一览

### 类型推断与静态检查

```python
name = "五行"            # 推断为 str
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

### 空安全

```python
def find(xs: list[str], p: str) -> str?:      # T? 表示可能为 null
    ...
r = find(langs, "J")
print(r?.upper() ?? "没找到")
if r != null:
    print(len(r))                              # 这里 r 自动窄化为 str
```

### 异常

```python
class InsufficientFunds(Exception):
    pass

try:
    withdraw(500.0)
except InsufficientFunds as e:
    print("失败：", e)
except (ValueError, KeyError):
    ...
else:
    ...
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

### 生成器、泛型、模块

```python
def naturals() -> iter[int]:
    n = 1
    while true:
        yield n
        n += 1

def max_of[T: Ordered](xs: list[T]) -> T: ...
class Stack[T]: ...

import geo.shapes as gs          # 导入 geo/shapes.ae
from math import sqrt, pi
```

### 测试

```python
test "加法":
    assert add(1, 2) == 3
```

```
$ aether test calc.ae
  ✓ 加法 (2µs)
  ✗ 故意失败
      AssertionError: 断言失败：add(2, 2) == 5
        左边：4
        右边：5
```

### 标准库

内置：`print` `input` `len` `range` `enumerate` `zip` `sorted` `reversed` `sum` `min` `max` `any` `all` `abs` `round` `open` 等；
模块：`math` `random` `time` `datetime` `os` `json` `re` `csv` `http` `heapq` `bisect` `collections` `itertools` `functools` `statistics` `string` `textwrap` `base64` `hashlib`。完整列表见 [docs/标准库.md](docs/标准库.md)。

```python
import http
server = http.Server()
server.get("/hello/{name}", req => http.text(f"你好，{req.param("name") ?? "世界"}！"))
server.listen(":8080")
```

## 性能

五行编译为原生代码。以下是同一份程序在 Windows 11 上与 Python 3.13 的对比（单位：秒）：

| 测试 | 五行 | Python 3.13 | 倍数 |
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

运行时库（列表、有序字典、集合、可空值、格式化、异常回溯等）是一个 Go 包，嵌入在 `aether` 可执行文件中。详见 [docs/实现原理.md](docs/实现原理.md)。

## 目录结构

```
cmd/aether/      命令行工具
compiler/        编译器：lexer、parser、checker、代码生成、构建驱动、测试
assets/rt/       运行时库（Go）
assets/lib/      预导入模块 prelude.ae 与标准库 math/random/time/os/json/re/http（.ae）
examples/        示例程序（计算器、银行、并发、Web 服务、生命游戏……）
tests/run/       黄金测试（程序 + 期望输出）
tests/errors/    编译错误测试
docs/            语言参考、标准库、实现原理
editors/vscode/  VS Code 插件（语法高亮，以及语言服务器的客户端）
```

运行全部测试：`cd compiler && go test .`

## 编辑器支持

`editors/vscode` 是 VS Code 插件：语法高亮，以及通过语言服务器 `aether lsp` 提供的**实时中文诊断、悬停显示类型、跳转到定义、成员补全和大纲**。把该目录复制到 `%USERPROFILE%\.vscode\extensions\aether-0.2.0` 后重启 VS Code 即可（需要 `aether` 在 PATH 中，或在设置中指定 `aether.path`），详见 [editors/vscode/README.md](editors/vscode/README.md)。其它支持 LSP 的编辑器也可以直接使用 `aether lsp`。

## 当前限制

- 整数是 64 位，溢出时回绕（不像 Python 那样自动变成大整数）。
- 方法不能重载（扩展方法可以按参数个数区分）；装饰器只支持 `@cache` / `@lru_cache`；不支持多重继承、`**kwargs`。
- 无限递归会因栈空间耗尽而终止（`aether run` 会显示中文说明），不能被 `try` 捕获。
- 运行需要本机安装 Go 工具链；`aether build` 生成的可执行文件可以在没有 Go 的机器上运行。
