# MiniJev：用 Aether 写的小巧 System One 决策模型

MiniJev 仿照 [TypeSafe AI](https://typesafe.ai) 的 **Jev**（[文档](https://docs.typesafe.ai)、[中文指南](https://jev.org.cn)）的接口：
给出一个**状态**和若干**类型化的问题**，返回**类型化的决策**和**校准的概率**，而不是生成一段文字——
代码可以直接拿这些值分支、排序、按阈值分流。

> 这是一个概念演示，**不是** TypeSafe 的 Jev，也与 TypeSafe AI 无关。真正的 Jev 是大规模预训练的模型，
> 能理解任意领域的问题；MiniJev 只有 64 万个参数，在合成数据上从零训练，只懂得训练数据中的三类场景
> （客服工单、Agent 操作、内容审核）。

另见最后一节的 **QwenJev**：用 LoRA 把预训练的 Qwen3 微调成接口相同的模型，同样全部用 Aether 实现。

```python
import minijev as jev

model = jev.MiniJev.load("minijev.bin", "gpu")
r = model.system_one("我的信用卡被扣了两次钱，急需处理。", {
    "topic": jev.choice("这条消息属于哪个主题？", {"billing": "账单或扣费问题", "account": "账号或登录问题", "technical": "产品故障或技术问题"}),
    "priority": jev.score("紧急程度如何？", ["不急", "一般", "非常紧急"]),
    "needs_human": jev.noul("是否需要人工处理？"),
})
r.choices["topic"].choice          # "billing"（类型是 str）
r.choices["topic"].probabilities   # {"billing": 0.99, "account": 0.00, "technical": 0.01}
r.scores["priority"].score         # 1.97：在 0（不急）到 2（非常紧急）之间的位置
r.nouls["needs_human"].noul        # 0.21：答案为“是”的概率
jev.route(0.98)                    # "act"：≥ 0.85 自动执行，0.5～0.85 请求确认，否则转人工
```

## 运行

```
aether run examples/minijev/demo.ae      # 演示：客服分流、Agent 风险、内容审核
aether run examples/minijev/train.ae     # 训练、校准并评估，模型保存到临时目录的 minijev.bin
aether test examples/minijev/minijev.ae  # 置信度公式的测试（与 Jev 文档中的例子对照）
```

第一次运行 `demo.ae` 时会先训练模型：在 RTX 3050 上约 2 分钟，只用 CPU 约 10 分钟。训练是确定的：
同样的设备上两次训练得到完全相同的模型。

## 与 Jev 相同的部分

| | |
|---|---|
| 三种问题 | **Choice**：从选项中选一个，返回 `choice`、各选项的 `probabilities`、`confidence`；**Score**：在有序的等级上定位，返回位置 `score`（可以落在两级之间）、`legend`、`probabilities`、`confidence`；**Noul**：是非问题，返回 `noul`（“是”的概率） |
| 置信度 | 与 [Jev 文档](https://docs.typesafe.ai/confidence)给出的公式相同：Choice 为 `(p_max - 1/n) / (1 - 1/n)`；Score 按离最可能等级的平均距离计算；Noul 没有单独的置信度（可以用 `|2p - 1|`） |
| 问题相互独立 | 同一个状态上的所有问题一起计算、互不影响；状态只编码一次，所以多问几个问题几乎不增加时间 |
| 校准的概率 | 训练使用交叉熵（恰当评分规则），训练后再做温度缩放，让概率可以直接与阈值比较 |
| 类型安全 | 答案按问题类型分开存放（`r.choices`、`r.scores`、`r.nouls`），字段的类型在编译时检查 |

## 模型

字符级的 Transformer，共 64 万个参数（`minijev.ae`）：

1. **状态编码器**：字的嵌入 + 相邻两个字（二元组）的嵌入 + 位置嵌入，两层 Transformer。每个状态只编码一次。
2. **问题编码器**：每个问题的每个选项是一段文字“问题｜选项的描述”，单独编码（一层 Transformer）。
3. **打分**：两部分相加——
   - 交叉注意力：问题的每个字读取状态中相关的位置，再对问题的各个字取平均后打分；
   - 双线性匹配：状态的摘要直接与问题的摘要比较。
4. Choice、Score 对各选项的分数做 softmax，Noul 对分数做 sigmoid；再除以校准得到的温度。

开发过程中的几个发现（都可以用 `train.ae` 复现）：

- 只有交叉注意力时，模型要先“发现”应该关注状态中的哪些字，训练开始阶段会停滞很久；有的问题在多任务训练中
  一直停留在随机猜测的水平。加上双线性匹配后，从第一步起就有清楚的梯度。
- 只看单个字时，模型要自己学会“闪退”“扣款”“登录”这样的词；加上二元组的嵌入后，客服工单的主题分类从约 85%
  提高到 97.8%，同时出现两个主题的工单也从 85% 提高到 93%。
- `nn.Embedding` 默认用标准正态分布初始化，比注意力汇集来的信息大得多；改为较小的初始值后才学得动。

## 数据与评估

训练数据由 `data.ae` 生成：9000 个场景（客服工单、Agent 操作、内容审核各三分之一），约 2.5 万个问题。
每类问题有几种说法，其中一种只在测试集中出现。一部分答案是故意不确定的（例如工单同时提到两个主题，
两个主题各占一半；“是否需要人工处理”按一定的概率决定），生成器知道每个答案的真实概率。

测试集上的结果（`train.ae` 的输出）：

| | 问题数 | 决策正确 | 概率误差 | 校准误差（温度缩放前 → 后） |
|---|---:|---:|---:|---:|
| Choice | 2083 | 99.1% | 0.041 | 0.047 → 0.024 |
| Score | 2091 | 99.7% | 0.005 | 0.003 → 0.003 |
| Noul | 4116 | 90.2% | 0.109 | 0.061 → 0.044 |
| 训练中没见过的说法 | 2504 | 85.6% | 0.162 | 0.094 → 0.069 |

- **决策正确**：最可能的答案与真实分布中概率最大的答案一致的比例。Noul 中有一部分问题的真实概率本身就接近
  0.5（例如 0.4、0.6），所以不可能接近 100%。
- **概率误差**：预测的概率与真实概率的平均绝对误差。
- **校准误差**（ECE）：按预测的概率分成 10 段，比较每段的平均概率与实际发生的频率。

## 局限

- 只懂三类训练场景。对训练数据之外的输入，Choice 仍然会选一个选项，而且可能给出很高的概率
  （演示的最后一个例子）——小模型无法知道自己“不懂”。真正的 Jev 文档建议为 Choice 配一个“是否有选项符合”的 Noul。
- 对训练中没见过的说法，准确率和校准都明显变差（85.6%，校准误差 0.069）。
- 同时提到两个主题的工单，真实分布是各占一半，但模型往往偏向其中一个（例如 0.80 对 0.19）：训练数据中每种组合
  出现的次数很少，而且标签是按概率抽出的一个结果，而不是真实的分布。整体的校准仍然很好，但单个少见的情况不一定。
- 状态只能是文字（Jev 还支持结构化的对象和图像）；没有 Box 问题。

## QwenJev：把 Qwen3 用 LoRA 微调成 Jev

MiniJev 从零训练，只认识训练数据中的说法。`qwen*.ae` 换一种做法：用预训练的语言模型 Qwen3（0.6B 或 1.7B），
通过 LoRA 微调成接口相同的决策模型——`system_one`、Choice/Score/Noul、校准的概率都与 MiniJev 一样。
**全部用 Aether 实现**，不依赖 Python 或 PyTorch：

| 文件 | 内容 |
|---|---|
| `qwen_tokenizer.ae` | Qwen 的字节级 BPE 分词器，读取 `tokenizer.json` |
| `qwen_model.ae` | Qwen3 的结构（RMSNorm、旋转位置编码、分组查询注意力、SwiGLU），读取 safetensors 权重；所有线性层可以加 LoRA，原模型的权重可以量化为 int8 |
| `qwenjev.ae` | Jev 的接口：把问题写成对话、读取答案标记的概率、LoRA 训练、温度缩放 |
| `qwen_train.ae` | 评估零样本 → LoRA 微调 → 温度缩放 → 再评估，保存 LoRA 参数 |
| `qwen_demo.ae` | 与 `demo.ae` 相同的问题，外加一个训练数据之外的例子 |

**做法**：每个问题写成一段对话（系统提示、状态、问题、标上字母或数字的选项；Qwen3 的非思考模式）。模型不生成文字，
只看下一个词元的分数：Choice 读选项字母 A、B、C……，Score 读等级数字 0、1、2……，Noul 读“是”“否”。在这个问题
自己的几个标记之间做 softmax，就是各个答案的概率。LoRA 微调就是在这些标记上最小化交叉熵，训练后再按问题类型做温度缩放。

### 运行

先从 [ModelScope](https://www.modelscope.cn/models/Qwen/Qwen3-1.7B)（或 Hugging Face）下载模型目录中的
`config.json`、`tokenizer.json` 和 `*.safetensors`：

```
aether run examples/minijev/qwen_train.ae D:/models/Qwen3-0.6B 600 qwen06-lora.bin
aether run examples/minijev/qwen_train.ae D:/models/Qwen3-1.7B 1200 qwen17-lora.bin int8
aether run examples/minijev/qwen_demo.ae D:/models/Qwen3-1.7B qwen17-lora.bin int8
```

最后的 `int8` 把原模型的线性层按行量化为 int8 放在显存中，Qwen3-1.7B 要这样才能放进 4 GB 的显存；LoRA 的参数仍然是
float32。词嵌入（0.6B 的有 6 亿字节）留在内存中，每次只取出用到的几行；训练时每一层使用激活检查点（`tensor.checkpoint`）。

在 RTX 3050 Laptop（4 GB 显存）上：

| | Qwen3-0.6B | Qwen3-1.7B（int8） |
|---|---|---|
| 读取模型 | 2 秒 | 21～31 秒 |
| 读取后正在使用的显存 | 1684 MB（float32） | 1351 MB |
| LoRA（秩 8，每层的 7 个线性层） | 505 万个参数 | 872 万个参数 |
| LoRA 训练（每步 4 个问题） | 2.6 秒/步 | 4.2～5.6 秒/步（1200 步 85 分钟） |
| 评估 634 个问题（零样本 / 加上 LoRA） | 87 秒 / 225 秒 | 160～210 秒 / 175～280 秒 |
| 一次 `system_one`（2～3 个问题） | 0.6～1.1 秒 | 0.6～2.3 秒 |

0.6B 的训练速度是 30 步的测试；下面结果中 0.6B 的 LoRA 是用更早的代码训练的（显存回收与矩阵乘法优化之前），
600 步用了 62 分钟。

进程的提交内存（任务管理器中的“提交大小”）在 1.7B 训练时最多 7 GB：Go 的堆约 1.5 GB（主要是 float32 的词嵌入），
其余是 OpenCL 驱动为显存分配的。这是设置了环境变量 `GOGC=50 GOMEMLIMIT=2500MiB`（让 Go 更早回收内存）时测得的，
不设置时最多约 9 GB。

没有 GPU 时自动使用 CPU：0.6B 的 `qwen_demo.ae` 在 Ryzen 5 5600H 上每次 `system_one` 1.3～2.5 秒，答案与 GPU 上
相同（CPU 上的训练没有测过）。

### 结果

测试集与 `qwen_train.ae` 中的相同：634 个问题。它与 MiniJev 的测试集用同一个生成器和种子，是其中的前 240 个场景，
MiniJev 一栏是 `train.ae` 训练的模型在这 634 个问题上的结果。表中是决策正确的比例，括号中是概率误差；LoRA 的各栏
是温度缩放之后的。

| | MiniJev | 0.6B 零样本 | 0.6B + LoRA 600 步 | 1.7B 零样本 | 1.7B + LoRA 600 步 | 1.7B + LoRA 1200 步 |
|---|---:|---:|---:|---:|---:|---:|
| Choice | 98.2%（0.044） | 91.7%（0.105） | 98.2%（0.065） | 96.4%（0.080） | 98.2%（0.058） | **100.0%**（0.043） |
| Score | 99.4%（0.007） | 54.8%（0.339） | 87.7%（0.132） | 40.6%（0.406） | 91.6%（0.098） | **98.7%**（0.011） |
| Noul | 89.1%（0.116） | 31.5%（0.668） | 89.4%（0.089） | 69.1%（0.310） | 92.6%（0.074） | **97.4%**（0.046） |
| 训练中没见过的说法 | 84.0%（0.164） | 52.4%（0.443） | 90.3%（0.110） | 68.0%（0.289） | 94.2%（0.086） | **99.5%**（0.043） |

- **零样本**：0.6B 对是非题几乎总是回答“是”（Noul 只有 31.5%），概率也过于自信；1.7B 的 Choice、Noul 好得多，
  Score 反而更差。
- **600 步只用了 2400 个训练问题**（每步 4 个；MiniJev 训练了 8 遍，每遍约 2.5 万个问题）：Choice 的正确率已经与
  MiniJev 相同，Noul 和训练中没见过的说法超过 MiniJev——预训练的模型本来就懂同一个意思的不同说法。1.7B 在每一类
  问题上都不比 0.6B 差。
- **600 步时 Score 不如 MiniJev**。1.7B 答错的 13 个 Score 问题中有 9 个是 Agent 操作的风险等级：生成器按一张固定的表
  打标签，有时与常识不一致，例如“调用支付接口向商户转账5000元（只涉及一个文件）”标为中风险，模型答高风险；“读取配置
  文件config.yaml（影响全部3万条记录）”标为低风险，模型答中风险。MiniJev 没有先入之见，能把这张表学得分毫不差。
  等级倒序排列（四分之一的 Score 问题）不是原因：倒序的 28 个问题只错了 2 个。
- **练习量翻一倍（1200 步，4800 个问题）**之后，Score 只错 2 个（其中一个仍是上面“转账5000元”那道题），正确率 98.7%，
  接近 MiniJev 的 99.4%；其余三类都超过 MiniJev（Noul 和没见过的说法超过得最多）。对这个任务，多练习比换更大的模型更有用：0.6B → 1.7B
  让 Score 提高了 3.9 个百分点，1.7B 从 600 步到 1200 步提高了 7.1 个百分点。
- **温度缩放**：600 步的模型温度在 1.47～1.69 之间（概率偏于自信），校准误差因此下降，例如 1.7B 的 Noul 从 0.049
  降到 0.031；但 Score 的概率误差从 0.069 升到 0.098：Score 的真实答案是确定的，温度大于 1 让概率变平。1200 步的
  模型 Score 的温度是 0.77（概率反而偏于保守），校准后两项误差都很小。
- **训练数据之外**：演示的最后一个例子“附近有没有好吃的川菜馆？”，各个模型都答 food（0.98～0.99），不是投诉（0.00）；
  MiniJev 在同样的问题上答 weather（0.64）。

### API 服务

`qwen_server.ae` 把 QwenJev 做成 HTTP 服务，请求与响应的格式与 TypeSafe 的 System One API 相同（按它公开的
OpenAPI 规范实现）：`POST /v1/systemone`、`GET /v1/models`，以及不需要密钥的 `GET /health`。TypeSafe 的 Python SDK
（`typesafe-sdk` 0.7.5）只要把 `base_url` 指向这个服务就能直接使用（测试过）。

```
QWENJEV_API_KEY=密钥 aether run examples/minijev/qwen_server.ae D:/models/Qwen3-1.7B qwen17-lora.bin int8 :8460
```

```python
from typesafe_sdk import Choice, Noul, Score, TypeSafeClient

client = TypeSafeClient(api_key="密钥", base_url="http://服务器:8460")
r = client.system_one(state="我的信用卡被扣了两次钱，急需处理。", questions={
    "topic": Choice(instructions="这条消息属于哪个主题？", criteria={"billing": "账单或扣费问题", "account": "账号或登录问题"}),
    "priority": Score(instructions="紧急程度如何？", criteria=["不急", "一般", "非常紧急"]),
    "needs_human": Noul(instructions="是否需要人工处理？"),
})
r.answers["topic"].choice          # "billing"
```

与 TypeSafe 的 API 的差别：

- 只支持文字。`state`、`instructions`、`criteria` 也可以是 JSON 对象或数组（转换为 JSON 文字），但模型是用纯文字训练的，
  写成纯文字效果最好。没有 Box 问题和图像（`assets`）。
- Choice 最多 10 个选项（模型用字母 A～J 表示选项），Score 最多 10 级；每个问题（状态 + 问题）最多 4096 个词元，一个请求
  最多 32 个问题。超出时返回 400 和说明。
- `model` 可以写 `qwenjev`、`jev-latest`（SDK 的默认值）或 `GET /v1/models` 返回的名字。
- 同一时刻只处理一个请求：3～4 个问题在 RTX 3050 或 NVIDIA A2 上约 1～2 秒。

### 局限

- 慢：每个问题都要算一遍整个语言模型，一次 `system_one` 要 0.6～2.3 秒，MiniJev 只要几毫秒。
- 需要 4 GB 显存和约 7 GB 的提交内存；GPU 可以在 Windows 与 Linux 上使用（见 `docs/深度学习.md`）。在 Linux（NVIDIA A2）上，
  0.6B 的 `qwen_demo.ae`（int8 权重）给出的概率与 Windows 上完全相同。
- 只在三类合成场景上微调过：训练数据之外的问题靠的是预训练模型原有的理解，这部分的概率没有经过校准。
- 同样**不是** TypeSafe 的 Jev，也与 TypeSafe AI 无关。
