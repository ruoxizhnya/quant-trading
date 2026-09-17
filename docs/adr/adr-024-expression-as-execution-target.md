# ADR-024: 策略执行载体是表达式，LLM 生成的代码只是 artifact

> **Status**: Accepted
> **Date**: 2026-09-17
> **Category**: Architecture
> **Related**: [ADR-023](adr-023-ai-experimenter-lab.md) · [ADR-001](adr-001-plugin-loading.md) · [ADR-015](adr-015-ai-agent-architecture.md) · [TASKS.md](../TASKS.md) P0-5
> **Upstream**: 用户（若曦）2026-09-17 决策（P0-5 修法三选一）

---

## Context

P0-5 登记的现象是「AI 编译后不加载：`go build` 真跑但产物从未 `plugin.Open`」。
实际排查下来，**根因比登记的两层**：

1. **浅层**：`Pipeline.Execute` 在生成 YAML 之后，**从未把任何策略注册进 registry**。
   Stage 5 拿 `parsedIntent.StrategyName` 去回测，而回测引擎是按名字查 registry 的
   —— 名字没注册，必然 `strategy not found`。编译出来的产物又被 `defer os.RemoveAll`
   删掉，所以"加载"这件事压根不存在。

2. **深层**：即使补上注册，`yamlgen.Generate` 产出的配置也**加载不成策略**。
   生成器只在 intent 自带 `signal_expr` 参数时才写 `expression:` 段，而规则解析
   （`ruleBasedExtract`）出来的 intent 只有一个语义标签（`momentum` / `breakout` / …）；
   `LoadStrategy` 只认 expression 类型，于是直接拒绝。

3. 另外 `Execute` 与 `ExecuteAsync` 是**复制粘贴的两份五段逻辑** —— 只修一边必漏另一边。

所以真正缺的是：**意图 → 可执行配置之间的一层映射**。

同时有一个反直觉的观察：ADR-001 遗留的「策略插件动态加载」心智模型（编译 Go 代码 →
`plugin.Open`）在这里并不适用。

---

## Decision

### 1. 执行载体 = YAML → ExpressionStrategy（确定性引擎）

一条意图的落地路径固定为：

```
自然语言 → intent（语义类型）→ YAML 配置 → ExpressionStrategy → GlobalRegister → 回测
```

回测跑的是**表达式引擎**算出来的信号，不是 LLM 写的 Go 代码。

### 2. LLM 生成的 Go 代码降级为「可审阅 artifact」

继续生成、继续**真编译校验**，结果留在 `result.GeneratedCode` / `result.BuildError`
里给人看，但**不加载、不执行**。

推论：它失败时**不阻断**实验。LLM 写不出能编译的代码，不该导致整个实验跑不了 ——
何况回测压根不用这段代码。此前这一步会让整个 pipeline fail。

### 3. 意图类型 → 确定性默认表达式

能用价量表达的意图类型，在 `defaultSignalExpression()` 里给一个默认表达式：

| 意图类型 | 默认信号表达式 |
|---|---|
| `momentum` | `cs_rank(ts_pct_change(close, 20)) > 0.8` |
| `mean_reversion` | `cs_rank(ts_mean(close, 20) - close) > 0.8` |
| `trend_following` | `cs_rank(ts_mean(close, 20) - ts_mean(close, 60)) > 0.8` |
| `breakout` | `cs_rank(close - ts_max(high, 20)) > 0.8` |
| `multi_factor` | `cs_rank(ts_pct_change(close, 20)) + cs_rank(neg(ts_std(close, 20))) > 1.6` |

**为什么不交给 LLM 现编**：映射写在生成的 YAML 里，人能审阅能改；同样的输入永远
得到同样的起点，实验可复现；AI 调的是旋钮（窗口 / 阈值 / 权重），不是每次重新发明
一个策略。这与 ADR-023「AI 是操作仪器的实验员，不是造仪器的生成器」一致。

`value` / `quality` **给不出默认表达式**：它们要的是 PE / PB / ROE，而表达式引擎
目前只暴露 OHLCV。这里**明确失败**，而不是套一个无关的价格表达式 —— 那会跑出一堆
看起来像样、实则答非所问的回测数字，比失败更糟。已登记为 P2-12。

### 4. 不做 `plugin.Open`

评估后放弃（这是本 ADR 最主要的"不做"记录）：

- **Windows 根本不支持** Go plugin（`-buildmode=plugin` 仅 Linux / macOS / FreeBSD），
  而开发环境就是 Windows —— 本地无法验证的加载路径不配当主链路。
- **依赖版本必须逐字节一致**：主程序与插件重复 import 的包（这里是 `pkg/strategy`）
  符号一冲突就 `plugin was built with a different version of package`。这类失败与业务
  无关、难复现、难排查。
- **违背定位**：让 LLM 写代码再动态加载 = AI 造仪器，与 ADR-023 冲突。
- 收益为零：表达式引擎已经能表达目标策略空间，动态加载只增加可执行的自由度，
  而**自由度在这里是负债**（不可审阅、不可复现、不可校准）。

ADR-001 的插件机制本身保留 —— 它服务的是人工编写的、需要热插拔的策略，与本决策
针对的"AI 生成代码"是两回事。

---

## Consequences

**正面**

- 「一条意图 → 回测出结果」端到端打通（P0-5 验收达成）。
- 每次实验的起点确定、可审阅、可复现；参数在 YAML 里，人能直接改。
- 少一次 LLM 调用也能跑完整条链（LLM 只影响 artifact 有无）。
- 校验不再撒谎：编译校验换成真 `go build`（P0-6）。

**代价 / 限制**

- 策略表达力受限于表达式 DSL。要新算子就得扩 DSL —— 这是**有意**的收窄，
  换来可审阅与可校准。
- `value` / `quality` 类意图目前直接失败，直到 P2-12 把基本面列接进表达式引擎。
- LLM 生成 Go 代码的通道名存实亡：它现在只是 artifact。若确认无人审阅，
  应考虑整条退役（并入 P2-5 的 DEPRECATED 模块清理）。

---

_2026-09-17：P0-5 / P0-6 落地时补记。_
