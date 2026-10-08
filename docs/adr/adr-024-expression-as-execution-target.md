# ADR-024: 策略执行载体是表达式，LLM 生成的代码只是 artifact

> **Status**: Accepted —— **Superseded (部分) by [ADR-029](adr-029-ai-layer-2026-agent-practice-alignment.md)**（2026-10-06）
> **Date**: 2026-09-17
> **Category**: Architecture
> **Related**: [ADR-023](adr-023-ai-experimenter-lab.md) · [ADR-001](adr-001-plugin-loading.md) · [ADR-015](adr-015-ai-agent-architecture.md) · **[ADR-029](adr-029-ai-layer-2026-agent-practice-alignment.md)** · [TASKS.md](../TASKS.md) P0-5
> **Upstream**: 用户（若曦）2026-09-17 决策（P0-5 修法三选一）

> ### ⚠️ 2026-10-06 部分被取代（ADR-029 §2）
>
> **被取代的一条**：「表达式是**唯一**执行载体」→ 改为**双轨**。
> 轨道 A 仍是表达式；轨道 B 是 **WASM（wazero）策略插件**，用于 ADR-028 附录 B
> 量化的 5 类硬缺口（EWMA/RMA/IIR 递推、OBV 等累积量、最大回撤、任意长度 streak、
> 线性离散状态方程）—— 这些在表达式 DAG 里**无法表达**（DAG 不能引用自身历史输出）。
>
> **完整保留的三条**：
> - **「起点确定 / 可审阅 / 可复现」三条理由** —— 轨道 B 用 **host API 能力隔离**满足
>   （WASM 实例只能调宿主显式导入的函数，`get_bar(t_offset > 0)` 直接 trap，
>   前视在**物理上不可能**）。这比本 ADR 依赖的静态审阅**更强**。见 ADR-029 §3
> - **「不做 `plugin.Open`」** —— 完整保留。wazero 是纯 Go、无 CGO、**支持 Windows**，
>   恰好绕开本 ADR 拒绝 plugin 的核心理由（Windows 不支持 `-buildmode=plugin`）。
>   本 ADR 对 plugin 的三条批评（平台限制 / 依赖版本逐字节一致 / 符号冲突难排查）
>   **对 WASM 全部不成立**
> - **「LLM 生成代码是 artifact」在轨道 A 下不变** —— 轨道 A 跑的仍是表达式，
>   代码只真编译校验不执行
>
> **判定规则**：默认走轨道 A；仅当意图属于轨道 B 适用域**且**轨道 A 已证明无法表达
> （parse 失败或需要附录 B 的硬缺口能力）才走 B。见 ADR-029 §2。
>
> **同时收口**：本 ADR 与 [ADR-007](adr-007-ai-sandbox.md) Phase 3（「Optional: WASM
> sandbox via `wazero`」）悬置半年的矛盾 —— ADR-007 状态 Accepted 且其 Context 针对
> 「compiles+**runs** it」，与本 ADR「不加载不执行」冲突，但两者都活着。
> ADR-029 把 Phase 3 转为轨道 B 主路径，矛盾收口。
>
> **下列正文保持原样，作为 2026-09-17 时点的判断记录。**

> ### 🔗 2026-10-08 对齐：与「四层表达力」模型的关系（D2 裁决）
>
> 上文（2026-10-06 注记）的「双轨」需与 2026-10-08 拍板的**四层表达力模型**对齐（见 [design/kernel/target-architecture-modular-kernel.md](../design/kernel/target-architecture-modular-kernel.md) §6）：
>
> - **轨道 A 不是只有 L1 表达式** —— 它内部含三层：L0 固定模板 / L1 表达式 DSL / **L2 确定性有状态算子**（EWMA / RMA / IIR / Kalman，作为 DSL 扩展算子，可枚举、warmup 可静态推导、可白盒校验）。
> - **轨道 B = L3（WASM）**，仅在 **L2 确定性算子也无法表达**时（自定义状态机、非标准滤波）才启用。
> - **对上文「附录 B 的 5 类硬缺口全部划给轨道 B」的修正**：其中 EWMA / RMA / IIR / Kalman（线性离散状态方程）**能用 L2 确定性算子表达**，按 D2「能用 L2 表达的不允许上 L3」应**优先做 L2 算子**，不直接上 WASM；只有 L2 也表达不了的才落到轨道 B。
>
> 即：判定规则细化为 **L0 → L1 → L2 → L3（轨道 B）逐层升级**，每层都先问「上一层能不能做」。这维持本 ADR「自由度是负债」的原始精神——表达力逐层放大，但每层都有前置闸口。

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

## 补充（2026-09-17，P1-2b）：参数如何进到执行载体

本 ADR 解决了「执行载体是什么」，但没说**搜索采出来的参数怎么作用到它上面**。
P1-2 落地后这个洞暴露出来：控制器在认认真真采样，而 `defaultSignalExpression`
的回看窗口写死 20 —— 真库取证显示一轮 6 次探索的 expression 一字不差，
等于同一个策略跑了 6 遍，**搜索是假的**。

**决策**

1. `defaultSignalExpression` 改为读意图参数（`lookback_days`）；意图没给就退回默认，
   非法值（<= 0）同样退回。长窗口沿用原先 3 倍的比例。
2. 新增 `pipeline.WithParameterOverrides(ctx, params)`：覆盖发生在**意图解析之后、
   YAML 生成之前**。解析得出的参数不丢，同名的替换，意图里没有的新增。

**为什么走 context 而不是给 Execute 加形参**：参数每次执行都不同，而 pipeline
实例是复用的（同一个实例要服务一轮里的每一次尝试）。这与 `ExperimentContext`
是同一套理由，两者也确实是并列关系。

**代价 / 限制**

- 搜索空间的参数名**必须**与意图参数同名（`lookback_days`）。写成 `lookback`
  之类的别名**不报错、不警告，只是静默失效** —— 整轮退化成同一个策略跑 N 遍。
  这是本设计里最阴的地方，由「一轮下来必须出现不同的表达式」这条断言兜住
  （见 `pkg/ai/loop` 的 `TestRun_DifferentParamsProduceDifferentConfig`）。
- 表达式的字符串拼接决定了参数只能是数值型。要支持结构化参数（如行业中性化），
  得先把表达式 DSL 扩出对应的算子。

---

_2026-09-17：P0-5 / P0-6 落地时补记。P1-2b 追加「参数如何进到执行载体」一节。_
