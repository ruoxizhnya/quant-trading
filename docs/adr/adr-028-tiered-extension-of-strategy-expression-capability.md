# ADR-028: 策略与因子表达能力的分层扩展

> **Status**: Proposed —— **定位由 [ADR-029](adr-029-ai-layer-2026-agent-practice-alignment.md) 调整为「轨道 A 的能力扩展」**（2026-10-06）；**与四层表达力对齐（2026-10-08 确认一致）**：本 ADR 的 L2 递推算子 = [design/kernel/target-architecture-modular-kernel.md](../design/kernel/target-architecture-modular-kernel.md) 四层模型的 L2（确定性有状态算子，优先级最高）；轨道 A 内部为 L0 / L1 / L2 三层，能用 L2 表达的不上轨道 B（D2）。
> **路径注记（2026-10-09）**：本文出现的 `pkg/ai/expression` 现为 **`pkg/expression`**（core 侧，ADR-027 §5 第 3 步已执行）；正文保留 2026-10-06 时点的记录。
> **Date**: 2026-10-06
> **Category**: Architecture
> **Related**: [ADR-023](adr-023-ai-experimenter-lab.md)（三层模型 · 验证器链 · 校准优于准确）· [ADR-024](adr-024-expression-as-execution-target.md)（表达式作为执行载体）· [ADR-027](adr-027-modular-decomposition-to-independent-services.md)（模块化拆分 · 策略服务）· **[ADR-029](adr-029-ai-layer-2026-agent-practice-alignment.md)（双轨执行载体 · WASM host API）** · [PRODUCT.md](../PRODUCT.md)
> **Extends**: ADR-024 —— **不取代**。ADR-024 定的是「执行载体是表达式，不是 LLM 生成的代码」；本 ADR 定的是「这个表达式载体的表达能力如何分层扩展，且不丢失 ADR-024 的三条理由」
> **Upstream**: 用户（若曦）2026-10-06 三轮头脑风暴的六项裁决 ——
> ① 延迟目标「两者都要，分层解决」 ② 反对表达式的理由「表达力不够」
> ③ 因子注册表住策略服务 ④ 序列注册表 config 驱动、动态定义
> ⑤ 类型系统「静态类型」 ⑥ 防前视「算子级前视声明」
> ⑦ warmup「算子声明 + 引擎自动多取」 ⑧ 停牌「= unknown」 ⑨ 落文档「新建 ADR-028」

> ### ⚠️ 2026-10-06 定位调整（ADR-029 §2）—— 本 ADR 的内容大部分保留，一条被取代
>
> 本 ADR 写完后，用户裁决策略执行载体改为**双轨**（轨道 A 表达式 / 轨道 B WASM），
> 理由是 ②「表达力不够」。这对本 ADR 的影响如下：
>
> | 本 ADR 的内容 | 双轨下的状态 |
> |---|---|
> | **§3 类型系统**（`Series` / `BoolSeries` / `Scalar` + Kleene 三值逻辑） | **完整保留** —— 是数据语义，与载体无关。轨道 B 的 WASM 代码同样要遵守停牌 = `unknown` |
> | **§4 算子声明契约**（7 项：signature / lookback / causal / state / warmup / init / nan_policy） | **完整保留** —— 轨道 A 的算子契约；轨道 B 的 host API 是它的对应物（ADR-029 §3.2） |
> | **§5 SeriesSpec + §5.1 `Provider.GetSeries`** | **完整保留且更重要** —— 它就是轨道 B 的 `get_bar` / `get_cross_section` host import 的数据来源 |
> | **§6 `FactorType` 收敛为注册表数据** | **完整保留** |
> | **§7 三路一致性**（`Batch ≡ Step ≡ Step-from-persisted`） | **完整保留** —— 轨道 B 用 host 的 `get_state`/`set_state` 实现（ADR-029 §3.5） |
> | **§8 warmup 推导** · **§9 停牌/一字板语义** | **完整保留** |
> | **§10 验证器三维** | **前视维需分流** —— 轨道 A 走 AST 递归推导；轨道 B 静态分析**不可判定**（图灵完备），改走 **host API 能力隔离**（`t_offset > 0` 直接 trap，前视物理上不可能，比 AST 推导更强）。递推稳定性与 warmup 两维**两轨都需要** |
> | **§11「L3 任意代码（WASM）明确不做」** | ❌ **被取代** —— 见下方更正 |
> | **附录 A Dry-run** · **附录 B 能力矩阵** | **完整保留且成为双轨判定规则的依据** —— 附录 B 标记为「硬缺口」的 5 类正是轨道 B 的适用域（ADR-029 §2 判定规则第 2 条直接引用它） |
>
> **§11 那行的成本估算原判断有误，更正如下**：原文写「L3 = 引入 WASM / 嵌入脚本，
> 代价高（沙箱、跨平台、AI 生成代码可信度），明确推迟」。但 ADR-029 §Context② ⑬ 取证发现：
> `internal/sandbox/wasm/sandbox.go` **505 行骨架已实现且测试齐全**（内存上限 / 超时 /
> 越界保护 / 编译复用），**插件协议已定义**，`rlimit_windows.go` 200 行证明**跨平台已解决**，
> 编译链路（`codeChecker` + `buildExecutor` + fail-closed + 三个成功率计数器）**已在生产跑**。
> 缺的只是 `wazero` 一个纯 Go 依赖 + 协议的前视漏洞修正。**所以不是「引入新技术栈」，
> 是「补一个依赖 + 修一个协议」。**
>
> **下列正文保持原样**（含 §11 那一行），作为 2026-10-06 早先时点的判断记录。

---

## Context

### 触发点

用户要支持 **price action 类回测策略**，并想实验**用纯数学的离散状态方程做策略**，
输入是**多条命名时间序列**（price / volume / 热点新闻权重…）。取证后发现当前表达式
DSL 表达不了，且缺口不是单一的一处，而是**分层的**。

顺带查出一条已经在影响真实决策的缺陷：**ATR 止损用的不是 ATR**。

### 实测现状（全部基于源码取证）

**① 全部 14 个算子都是无状态 FIR 形式。**

`pkg/ai/expression/operators.go` 里 14 个算子的签名全是同一形状：

```go
func tsMean(data []float64, window int) []float64
func tsStd (data []float64, window int) []float64
func tsCorr(data1, data2 []float64, window int) []float64
func csRank(values []float64) []float64
```

**进整条序列、出整条序列、无状态、无输出反馈** —— 信号处理里的 **FIR（有限冲激响应）系统**。
10 个时序算子（`ts_mean/std/sum/max/min/delay/delta/pct_change/corr/rank`）
+ 4 个横截面算子（`cs_rank/zscore/percentile/neutralize`）。

**② 函数名与字段名都是封闭白名单。**

- 函数名：`evaluator.go:143` 的 `isUnaryOp`（6 个）+ `evaluator.go:300` 的
  `applyTimeSeriesOp`（9 个），`default` 报错
- 字段名：`strategy/expression/data_provider.go:13-34` 两个硬编码 map ——
  **6 个行情字段**（open/high/low/close/volume/turnover）+ **5 个基本面字段**
  （pe/pb/ps/roe/roa），`default: return nil, fmt.Errorf("unknown field %q", field)`

后果：**「因子引用因子」在当前 AST 下不可能** —— 没有 `FactorRefNode` 这种节点类型。

**③ 无逻辑算子，无布尔类型。**

`parser.go` 的比较符只有 `tokenGT/LT/EQ`，**没有 `and`/`or`/`not` token**。
`applyBinaryOp`（`evaluator.go:248-262`）里 `>` `<` `==` 返回 **1 或 0（float64）**。

**④ `x/0` 返回 NaN，A 股一字板会让比例类因子失效。**

`evaluator.go:242-244`：

```go
case "/":
    if b == 0 { return math.NaN() }
```

一字涨停/跌停时 `high == low`，于是 `(high-low) == 0` → **实体比例、上下影线比例、
振幅全部 NaN**。NaN 沿表达式传播，`cs_rank` 会排除它 —— 结果是
**最需要信号的极端形态那天没有信号**。

**⑤ `==` 用浮点精确相等。**

`evaluator.go:258-262` 是 `if a == b`。「十字星 = 开盘等于收盘」写成 `open == close`
会因浮点误差**几乎永远 false**；「N 日新高」写成 `high == ts_max(high,N)` 同理。

**⑥ ATR 实为 `SMA(TrueRange, N)`，不是 Wilder 递推。**

`pkg/risk/stoploss.go:113-119`：

```go
// Compute the simple average over the most-recent `need` true ranges.
// (Original behavior — see doc comment above.)
sum := 0.0
for _, tr := range buf { sum += tr }
atr := sum / float64(len(buf))
```

标准 ATR（Wilder 1978）是 `ATR[t] = (ATR[t-1]×(N-1) + TR[t]) / N` —— **IIR 递推**。
这里是硬截断的 N 日等权平均。两者数值不同：波动率突变时 SMA-ATR 会在第 N 天
**突然忘掉**那次冲击，Wilder 平滑衰减。

它直接喂给 `CalculateStopLossPrice(entryPrice, atr, regime)` = `entry − multiplier × atr`
（`stoploss.go` / `boundary.go`），即**止损位与所有主流平台和教科书定义不一致**。

**⑦ 全仓零递推计算能力。**

grep `EWMA|EMA|GARCH|Kalman|HoltWinters|Wilder` → **3 个命中全是 prometheus buckets
与 retry backoff**，与金融计算无关。不是「实现了没接线」，是**根本没有**。

**⑧ ⑥ 与 ⑦ 与 ① 是同一条因果链，不是三个独立问题。**

```
① DSL 缺递推算子（FIR only）
   ↓ 所以 ATR 无法用表达式表达
⑦ ATR 只能在 pkg/risk 里用 Go 硬编码
   ↓ 硬编码时没有递推基础设施可用
⑥ stoploss.go 用了 SMA 而非 Wilder
   ↓
   止损位偏离行业标准，且回测结果里看不出来
```

**补 L2 的 `ts_rma` 一次性解决全部三层。**

**⑨ `FactorType` 是 12 个名字的封闭枚举，且门禁卡在 AI 自己的工具上。**

`pkg/domain/factor.go:8-54`：`type FactorType string` + 12 个 const
（momentum/value/quality/size/volatility/growth + 6 个纵向基本面因子），
`ParseFactorType` 是白名单 switch，**`default` 返回 false**。

调用点 5 处，其中关键的一处是 **AI 的 MCP 工具** ——
`pkg/tools/builtin/factor_hypothesis_tool.go:127`：

```go
ft, known := domain.ParseFactorType(name)
if !known {
    return &factorHypothesisResult{KnownFactor: false,
        Hypothesis: "未知因子名 —— 无法查到假设来源。请确认拼写..."}, nil
}
```

**AI 挖出一个新因子，连假设都登记不进去。**

**⑩ 因子有两条互不相通的路径。**

| | 路径甲 | 路径乙 |
|---|---|---|
| 载体 | `domain.FactorType` 12 个 const | expression AST |
| 扩展 | ❌ 白名单 switch | ⚠️ 受限于 11 个字段 |
| 消费 | `GetFactorZScore(FactorType, date, symbol)` | `Evaluator.Evaluate(node)` |
| 缓存 | `engine.LoadFactorCache` + Redis `factor_cache` | 无 |

**⑪ 算子优先级链是正确的（本研究一度怀疑倒置，核实后撤回）。**

`parser.go` 的**函数定义顺序**是 additive → multiplicative → comparison → power，
看起来像比较符优先级高于加减。但**调用关系**才是真优先级：

```
parseExpression → parseComparison → parseAdditive → parseMultiplicative → parsePower → parseUnary
```

`parseComparison` 在最外层、其操作数是 `parseAdditive`，所以 `a + b > c`
**正确**解析为 `(a+b) > c`。**此项无缺陷，记录在此避免后人重复怀疑。**

另有一个小的语义提示：`parseComparison` 用 `for p.match(...)` 左结合，
`a > b > c` 会解析成 `(a>b)>c` 而非 `a>b && b>c`。引入 `and` 后应在文档里明确禁止链式比较。

**⑫ dry-run 结论：三值逻辑无法用 float64 硬凑，其余「便利算子」可以。**

详见附录 B 的能力矩阵。关键一条：

```
需要：  false AND unknown = false        （Kleene 三值逻辑，false 吸收 unknown）
硬凑：  0 * NaN = NaN                    （IEEE 754 强制，无法绕过）
```

`NaN` 传染一切算术运算，所以**布尔三值逻辑是真能力缺口**，而
`max/min/if/near` 只是语法糖（`(a+b+abs(a-b))/2` 可算 max）。

**⑬ 存量算子全部 causal，新的前视检查不会让存量表达式失效。**

逐个核对全部 **28 个运算**（`operators.go` 的 14 个具名算子 = 10 时序 + 4 横截面，
加 `evaluator.go` 的 6 个一元、8 个二元）：全部只看过去或当日截面。
`cs_*` 是**同日横截面**，不看未来。所以 §10 的前视检查是**纯增量约束**，
不会让现有 `defaultSignalExpression()` 的 5 个默认表达式失效。

---

## Decision

### 1. 继承 ADR-024 的三条理由，作为本 ADR 的判据

ADR-024 拒绝 `plugin.Open` 的理由是「**起点确定、可审阅、可复现**」，并明确写下
「**自由度在这里是负债**」。本 ADR 的每一层扩展都必须通过同一个判据：

> **加了它之后，表达式是否仍然起点确定、可审阅、可复现？**

这条判据把候选方案自动分成两类（§2）。

### 2. 分层扩展模型

| 层 | 内容 | 性质 | 通过 ADR-024 判据？ | 优先级 |
|---|---|---|---|---|
| **L1** | 因子 DAG —— 因子引用因子，拓扑序求值 + 循环检测 | 组合与复用 | ✅ | 中 |
| **L1.5a** | **布尔三值逻辑** —— `BoolSeries` + Kleene `and/or/not` | **真能力缺口**（不可硬凑，⑫） | ✅ | **高** |
| **L1.5b** | 便利算子 —— `max/min/if/near/max3` | 语法糖（可硬凑但不该凑） | ✅ 正是为了「可审阅」 | 高（随 L1.5a） |
| **L2** | **递推算子** —— 指数平滑类 / 路径依赖类 / 线性状态空间 | **真能力缺口** | ✅ | **最高** |
| **L3** | 任意代码（WASM / 嵌入脚本） | 图灵完备 | ❌ **正面冲突** | **明确推迟** |

**关键判断：L2 不违反 ADR-024 的任何一条理由。**

`ts_rma(tr, 14)` 与 `ts_mean(close, 20)` 一样是**确定性算子** —— 给定输入序列必有
唯一输出，可单元测试，可在配置里一眼看懂，可复现。ADR-024 真正反对的是
「让 LLM 写代码再动态加载」= **AI 造仪器**；多几个算子不是造仪器，是**给仪器加刻度**。

**L2 对 ADR-023「校准优于准确」有额外好处**：算子集合**有限可枚举**，验证器链能对
算子组合做**先验**检查（前视、递推发散、除零风险都能在跑之前否决）。
L3 的代码搜索空间不可枚举，验证器只能事后统计检验，**校准会更难**。

**L3 明确推迟**，触发重评的条件写进 §11。

### 3. 类型系统：三类型 + 显式算子签名

```
Series      每标的的 float64 时间序列
BoolSeries  每标的的三值布尔序列（true / false / unknown）
Scalar      编译期确定的数值参数
```

**`BoolSeries` 用 Kleene 三值逻辑**，`unknown` 是 NaN 的布尔对应物：

| `and` | true | false | unknown |
|---|---|---|---|
| **true** | true | false | unknown |
| **false** | false | false | **false** ← 关键，false 吸收 unknown |
| **unknown** | unknown | false | unknown |

| `or` | true | false | unknown |
|---|---|---|---|
| **true** | true | true | **true** ← true 吸收 unknown |
| **false** | true | false | unknown |
| **unknown** | true | unknown | unknown |

`not(unknown) = unknown`。

**为什么必须是三值而不是 bool**：数据缺失（停牌、未披露财报、上市不足 N 日）
与「条件为假」是两件不同的事。二值逻辑会把「不知道」当成「不成立」，
产出**看起来正常但实际基于缺失数据**的信号 —— 与
`data_provider.go:113-117` 那条注释反对的是同一类错误：

> 「没有就是没有，给一串 0 只会产出看似能跑的假信号」

**静态类型的价值**：`lt(vol_ratio, 0.7)` 的签名是 `Series × Scalar → BoolSeries`。
若 AI 写出 `lt(vol_ratio, ma20)`（拿序列当阈值），**编译期拦下**。
float64 单类型只能在运行时炸，且炸法通常是静默的错误数值。

### 4. 算子声明契约

每个算子必须声明 7 项，这是本 ADR 的核心契约：

```
signature   类型签名           lt : Series × Scalar → BoolSeries
lookback    最大回看窗口        int | ∞
causal      是否只看过去        bool（false 者一律拒绝注册）
state       是否有状态          bool（true 者必须提供 Batch + Step 双实现）
warmup      预热期             int | f(params)   ← 可为参数的函数
init        初始化规则          如 Wilder ATR = 前 N 根 SMA
nan_policy  NaN / unknown 传播规则
```

**`warmup` 可以是参数的函数**：`ts_ewma(x, α)` 的 warmup ≈ `ln(tol)/ln(1-α)`，
α=0.3 时约 20 根，α=0.05 时约 60 根。**不能硬编码成一个常数。**

**存量算子的声明**（全部 `causal=true`、`state=false`，见 ⑬）：

| 算子 | lookback | 算子 | lookback |
|---|---|---|---|
| `ts_mean/std/sum/max/min(x,N)` | N−1 | `ts_corr/ts_rank(x,N)` | N−1 |
| `ts_delay/ts_delta/ts_pct_change(x,d)` | d | `cs_rank/zscore/percentile/neutralize` | 0（同日截面） |
| 一元 `neg/abs/log/sqrt/sign/exp` | 0 | 二元 `+ - * / ^ > < ==` | 0 |

**L2 新增算子的声明**：

| 算子 | lookback | state | warmup | init |
|---|---|---|---|---|
| `ts_ewma(x, α)` | ∞ | ✅ | `ln(1e-6)/ln(1-α)` | 首值 = x[0] |
| `ts_rma(x, N)` | ∞ | ✅ | N | 前 N 根 SMA（= Wilder 定义） |
| `ts_iir(x, b[], a[])` | ∞ | ✅ | `len(a)+len(b)` | 零初始状态 |
| `ts_streak(cond)` | ∞ | ✅ | 1 | 0 |
| `ts_since(cond)` | ∞ | ✅ | 1 | ∞（从未发生） |
| `ts_drawdown(x)` | ∞ | ✅ | 1 | peak = x[0] |
| `ts_cumsum(x)` | ∞ | ✅ | 1 | 0 |
| `ts_count(cond, N)` | N−1 | ❌ | N | 滑窗即可，无需状态 |

> **订正记录（2026-10-08，K3 切片 1 实现时发现）**：`ts_ewma` 行的样例数字与公式对不上——
> 上文「α=0.3 约 20 根」按公式 `ln(1e-6)/ln(1-α)` 实算应为 **39** 根（约 20 对应 α=0.5）；
> 「α=0.05 约 60 根」实算应为 **269** 根（约 60 对应 tol≈5e-2）。**实现以公式 + tol=1e-6 为准**
> （`pkg/indicator/ewma.go` 的 `ewmaWarmup`），样例数字是笔误；本表数字待后续修订时一并更正。

**`ts_count` 是唯一 `lookback` 有限的路径依赖算子** —— 它可以用滑窗实现，
不需要状态。这个区分很重要：不是所有「路径依赖」都需要 `state=true`。

### 5. 序列注册表 SeriesSpec（config 驱动，动态定义）

用户裁决：**因子输入必须可动态定义，基于 config 映射，初始可以只有日线报价。**
这精确对应把 `data_provider.go:13-34` 那两个硬编码 map 换成注册表：

```
SeriesSpec {
  name        "close" | "main_inflow_5d" | "hot_news_weight" | ...
  domain      行情 | 基本面 | 资金 | 情绪 | 派生
  source      数据来源引用（表 / 外部 API / 另一个序列的派生）
  transform   降维或聚合规则（可选）
  frequency   日 | 分钟
  as_of       ★ PIT 语义：这个值在哪一天才可用
  nan_policy  ★ 缺失时的确定行为（含停牌语义，见 §9）
}
```

**`as_of` 是这张表里最重要的字段**，因为它是防前视的数据层保障：

- 财报的 `as_of` 是**公告日**，不是报告期截止日 —— 项目已这么做
  （`available_date = COALESCE(ann_date, trade_date)`，`data_provider.go:62-64`
  的注释明确警告「对齐错一格就是前视偏差」）
- 新闻的 `as_of` 是发布时间
- 资金流 / 龙虎榜的 `as_of` 是**收盘后**（盘中不可得）

把 `as_of` 做成一等字段后，**验证器链的偏差维可以机械检查对齐**，纯计算、不需要模型。

初始注册表 = 6 条日线行情字段（+ 5 条基本面），后续**加条目不改代码**。

**与 ADR-027 §5「11 张只写不读表先停写入」裁决的关系**：本 ADR **不推翻**该裁决。
初始注册表不含那 11 张表，所以它们在初始阶段仍然没有读者，「先停写入」依然成立。
但 SeriesSpec 让**未来接入它们成为配置变更而非代码变更** —— 这是本 ADR 给那条裁决
留的出口。何时接入由后续按域逐个裁决（资金流/龙虎榜/涨停池是结构化的、接入成本最低，
且与 price action 天然契合；文本类需先有 reduction-service）。

#### 5.1 序列供给契约：`Provider.GetSeries`（本节为初稿遗漏，2026-10-06 补）

§5 定义了 SeriesSpec 说明「序列是什么」，但**没有定义「谁按 SeriesSpec 去读数据」**。
这是个真实的断点，不补上则 §5 只是纸面注册表。

**为什么不能沿用现有 `Provider` 接口**：`marketdata.Provider` 的 11 个方法
（`provider.go:10-22`）全部是行情 / 基本面 / 日历 / 股票列表 ——
`GetOHLCV` / `BulkLoadOHLCV` / `GetFundamental` / `GetStocks` / `GetStock` /
`GetLatestPrice` / `GetIndexConstituents` / `GetTradingDays` / `CheckCalendarExists` /
`Name` / `CheckConnectivity`。**没有任何「通用序列」或「因子」入口。**

于是「加一种新数据」= 加一个 Provider 方法，而 Provider 有 **5 个实现**
（`httpProvider` / `postgresProvider` / `cachedProvider` / `inmemoryProvider` /
`RealtimeProvider`）—— **改一次接口要动 5 处**。这不是可扩展性，是线性成本。

⚠️ 注意 [ADR-027](adr-027-modular-decomposition-to-independent-services.md) §5 第 5 步
列的「Provider 补 5 个方法」（`GetListingWindows` / `GetFundamentalsPITBulk` /
`GetDividendsInRange` / `GetSplitsInRange` / `GetFactorCacheRange`）是**收编 engine
直连 store 的那 6 处**，它们都是既有的具体数据类型，**不解决「新增一种数据」的问题**。

**裁决：加一个通用方法，而不是每种数据加一个方法。**

```
GetSeries(ctx context.Context, ref SeriesRef, symbols []string,
          start, end time.Time) (map[string][]float64, error)
```

`SeriesRef` 是 SeriesSpec 的引用（name + version + as_of 语义），住在 Tier 1 契约层。

这样：

| 关注点 | 效果 |
|---|---|
| 加新数据源 | 加一条 SeriesSpec + data-service 端实现该 `source` 的读取器。**Provider 接口不再变** |
| 5 个实现的改动面 | 只有 `httpProvider` / `postgresProvider` 要真实现；`cachedProvider` 是装饰器可通用转发；`inmemoryProvider` / `RealtimeProvider` 是测试与实时用 |
| `GetField` 的硬编码白名单 | `data_provider.go:13-34` 的两个 map **退化为查 SeriesSpec 注册表**，`default` 分支语义不变（查不到就报错，不给假数字） |
| 引擎的数据预取 | 可从表达式 AST **静态提取所需序列名**，一次性批量拉取 —— 沿用现有 per-run 批量预取模式（`fetchMarketDataForDay` / `warmFundamentalsIfNeeded`），**不是 per-day** |

**最后一条顺带解决了 ADR-027 §Context⑬ 的 per-day HTTP 陷阱**：一旦引擎能从 AST
静态推导所需序列，就不存在「每天把全市场数据 POST 出去算信号」的必要 ——
数据按 run 批量取一次，定义按 run 取一次，**信号在引擎内 in-process 算 N 天**。

**同时收敛掉现状的三套注入约定。** 现在策略拿额外数据有三种互不相干的方式：

| 方式 | 位置 | 问题 |
|---|---|---|
| `strategy.FactorAware.SetFactorCache` | `engine.go:1129` | 类型断言注入，且 `GetFactorZScore` 吃封闭的 `FactorType` |
| `strategy.FundamentalAware.SetFundamentals` | `engine.go:1133` | 类型断言注入 |
| `sentimentStrategy.SetAggregator` | `plugins/sentiment.go:40` | **具体类型 setter**，最弱的一种 |

第三种最能说明问题：`GenerateSignals(ctx, bars, portfolio)` 的签名里**看不到**情绪数据，
它从注入的 `aggregator` 字段自己拿，靠注释「This must be called before GenerateSignals」
的**调用顺序**保证。四条代价：

1. **引擎无法知道策略需要什么数据** → 不能预热、不能做 `as_of` 前视检查、不能在缺失时明确失败
2. 每加一种数据源 = 加一个字段 + 一个 setter + 一个 `if nil` 分支
3. 三套约定并存，无统一契约
4. **AI 生成策略时无法静态校验「这个策略要的数据有没有」**

改成 SeriesSpec + `GetSeries` 后，数据依赖**显式出现在表达式里**（用了哪些序列名，
AST 可静态提取），四条代价同时消失。

### 6. `FactorType` 从封闭枚举收敛为注册表数据

**因子定义（AST）住策略服务**（ADR-027 已定），**因子求值在消费点 in-process 做**。

理由：回测引擎的 date-loop 已经拿到全市场当日数据（`fetchMarketDataForDay`），
横截面算子需要的截面就在手上。所以：

| 关注点 | 归属 |
|---|---|
| 因子**定义** | 策略服务（版本化，与策略定义同级资产） |
| 因子**求值** | 回测引擎 in-process |
| 因子**缓存**（`quant.*` + Redis） | **纯性能优化，不是架构必需** |

**收敛动作**：`domain.FactorType` 不再是类型系统的封闭集合，而是**注册表里的 12 条
预置记录**。`ParseFactorType` 从白名单 switch 改成查注册表。这样：

- AI 挖出的新因子可以登记（解开 ⑨ 的门禁）
- 预计算缓存路径保留（性能优化）
- **表达式成为唯一的定义载体**，缓存只是它的物化结果
- 路径甲/乙 的分裂（⑩）消失

**明确不做「因子服务」**：那会把 in-process 的横截面求值变成跨服务调用，
且回测引擎已经持有全部所需数据。这是过度设计。

### 7. 三路一致性：Batch ≡ Step ≡ Step-from-persisted

回测走 date-loop（批量），实盘走 event-loop（增量）。用户裁决「两者都要，分层解决」，
但**共用同一份策略定义与同一段算子实现**。

每个 `state=true` 的算子必须提供两个实现：

```
Batch(data []float64) []float64              回测用
Step(x float64, state *S) float64            实盘用
```

**一致性属性测试必须覆盖三条路径，不是两条**：

```
Batch(全序列)[t]  ≡  Step 从零累积到 t  ≡  Step 从持久化状态恢复到 t
                                        （容差 1e-12）
```

**第三条是本 ADR 发现的真实不对称点**：`Batch` 无状态，`Step` 必须能把
`RmaState{count, sum, prev}` **序列化持久化**，否则实盘进程重启后 EWMA/ATR 从头算，
**与回测漂移** —— 这是量化系统最隐蔽最致命的 bug 类别（回测漂亮、实盘不一样、且查不出来）。

**这个不对称必须在算子接口设计时就承认**：如果接口不预留状态序列化，后面补不进去。

### 8. warmup 推导：算子声明 + 引擎自动多取

用户裁决：**算子声明 warmup，引擎自动向前多取数据，信号从 warmup 之后开始。**

```
总 warmup = 对表达式 AST 递归推导
          = 各并行链 warmup 的最大值（不是相加）
          + 各串行链 warmup 的累加
```

Dry-run 策略的推导（附录 A）：`buy` 链 warmup = 19（`ts_mean(close,20)`），
`atr` 链 warmup = 14（`ts_rma(tr,14)`），两条链**并行** → 总 warmup = max(19,14) = **19**。

**回测结果必须如实记录用了多长预热**，且预热段的信号标为 `unknown` 而非丢弃 ——
否则「回测区间 2020-01-01 起」这句话会与「信号从 2020-01-28 起」静默不一致。

### 9. 数据语义：停牌与一字板

#### 停牌 = `unknown`（用户裁决）

停牌日 `volume=0`、价格不变。若按数值处理：

```
vol_ratio = 0 / ts_mean(volume,20) = 0  →  shrink = lt(0, 0.7) = true
```

但**停牌日根本不能交易**，`buy=true` 是假信号。

裁定：**停牌日的行情/成交量序列一律为 `unknown`（不是 0）**，由 `SeriesSpec.nan_policy`
声明。三值逻辑的 `and` 会正确吸收它 —— 若其他子条件为 `false`，结果 `false`；
若其他为 `true`，结果 `unknown`（= 不出信号）。**两种情况都不会产生假买入。**

引擎已有的在市信息（`eligibleUniverse` / `ListingWindow` / `loadListingCalendar`）
**下沉到序列层**，让表达式能看到「这天能不能交易」，而不是只在引擎层事后过滤。

#### 一字板：约定确定值（用户裁决），但范围比初判窄

Dry-run 纠正了一处过度概括：**一字板不影响 true range**。
一字涨停时 `high == low`，但 `tr = max(high-low, |high-prev_close|, |low-prev_close|)`
的后两项**非零**，所以 ATR 正常计算。

受影响的只有**除以振幅 `(high-low)` 的比例类因子**。约定值：

| 表达式 | `high == low` 时 | 理由 |
|---|---|---|
| 实体比例 `abs(close-open)/(high-low)` | **1.0** | 无波动区间 = 实体占满全部，是连续延拓的极限 |
| 上影线 / 下影线比例 | **0.0** | 没有影线 |
| 振幅 `(high-low)/prev_close` | **0.0** | 确实没有振幅 |
| true range / ATR | **正常计算，不触发** | max 的后两项非零 |
| 其他 `x/0` | **NaN（保持现状）** | 真缺失就是真缺失，不给假数字 |

**原则：只有「几何上有明确极限」的比值才约定确定值，其余保持 NaN。**

约定值必须写进**算子/序列的 `nan_policy` 声明**，不能散落在实现里 ——
否则同一个「除零」在不同算子里行为不一致。

#### `==` 的浮点问题

加 `near(a, b, tol)` / `eq(a, b, tol)` 做容差比较。
`==` 在**序列语境**下标记为 deprecated（保留但不推荐）：
「N 日新高」写成 `high == ts_max(high,N)` 会因浮点误差失效。

**同时禁止链式比较**（`a > b > c`）：`parseComparison` 是左结合的，
它会解析成 `(a>b)>c` 而不是 `a>b && b>c`（⑪）。引入 `and` 后这条必须写进文档与 lint。

### 10. 验证器链新增三维（全部纯计算，符合 ADR-023）

ADR-023 的验证器链是「五维确定性 + 一维因果（唯一需要模型）」。本 ADR 新增的三维
**全部是纯计算**，因此归入确定性侧，**不增加模型不确定性**：

| 新维度 | 检查内容 | 为什么必须确定性 |
|---|---|---|
| **前视** | 对 AST 递归推导 `lookback`，若任一算子 `causal=false` 或推导出触及 `t+1`，**跑之前就否决** | AI 挖因子最大的隐形杀手；必须是机械检查才能拦得住 |
| **递推稳定性** | `ts_iir(x,b,a)` 的特征根模长必须 < 1，否则递推指数发散 | AR(p) 参数拟合历史时看不出问题，**实盘会爆** |
| **warmup 充分性** | 回测区间长度必须 > 推导出的总 warmup，否则结果无意义 | 静默产生「基于未收敛状态」的信号 |

**前视检查是纯增量的**：存量 28 个运算全部 `causal=true`（⑬），所以
`defaultSignalExpression()` 的 5 个默认表达式不会失效。

**递推稳定性这一维是 L2 引入的新风险的对冲** —— 用户想实验「纯数学离散状态方程」，
而线性差分方程若特征根落在单位圆外会指数发散。**没有这一维，L2 就是个上膛的枪。**

### 11. 明确不做的事

| 不做 | 理由 | 重评触发条件 |
|---|---|---|
| **L3 任意代码（WASM / 嵌入脚本）** | 违反 ADR-024 三条理由中的「可审阅」「起点确定」；且验证器只能事后统计检验，与 ADR-023「校准优于准确」冲突 | L1+L1.5+L2 全部落地后，**仍有具体因子无法表达**时开新 ADR |
| **非线性状态空间（Kalman / GARCH / EKF / 粒子滤波）** | 用户想做的「离散状态方程」若为**线性**，`ts_iir` 一个算子就够（§2 附注）；非线性版本实现与测试成本高一档，且参数估计本身需要拟合 | 线性 `ts_iir` 落地并证明不够用时 |
| **「因子服务」** | 因子求值应在消费点 in-process（§6）；独立成服务会把横截面求值变成跨服务调用 | 出现「多消费者需要同一份重计算因子且缓存命中率低」的实测证据时 |
| **把 `ts_count` 做成有状态算子** | 它 `lookback` 有限（N−1），滑窗即可，`state=false`（§4） | — |
| **链式比较 `a > b > c`** | 左结合语义与数学习惯相反（⑪） | — |
| **在初始注册表里接入那 11 张只写不读的表** | 不推翻 ADR-027 §5 的「先停写入」裁决（§5） | 按域逐个裁决，结构化的资金流/龙虎榜/涨停池优先 |
| **不为每种新数据加一个 `Provider` 方法** | §5.1：Provider 有 5 个实现，改一次接口动 5 处，是线性成本而非可扩展性 | — （已裁决用通用 `GetSeries`） |
| **不保留三套并存的数据注入约定** | §5.1：`FactorAware` / `FundamentalAware` / `SetAggregator` 都是隐式依赖，引擎无法静态知道策略要什么数据 | 迁移到 SeriesSpec 后逐个删除 |
| **不让策略服务承载信号计算** | ADR-027 §Context⑬：`getSignalsFromStrategyService` 在日循环内 POST 全市场 K 线，是 per-day 不是 per-run | 策略服务只传定义；该路径应删除而非优化 |

**关于「离散状态方程算不算状态空间」的澄清**（用户提问）：

标量 p 阶线性差分方程与 n=p 维线性状态空间模型**数学等价**（伴随矩阵变换）：

```
y[t] = a₁y[t-1] + ... + aₚy[t-p] + b₀x[t] + ... + b_q x[t-q]     ← 「离散状态方程」
                    ⟺
s[t] = A·s[t-1] + B·x[t] ；  y[t] = C·s[t] + D·x[t]              ← 「状态空间」
```

**分水岭是「线性 vs 非线性」，不是「状态空间 vs 非状态空间」。**
线性情形 `ts_iir(x, b[], a[])` 一个算子覆盖全部 ARMA(p,q)。

**专业提醒（已写入 §10）**：线性差分方程直接当**交易信号**在量化里少见且危险 ——
特征根在单位圆外时递推指数发散，回测看不出来（参数是拟合历史的），实盘会爆。
更常见的正确用法是当**工具**：滤波去噪、平滑、波动率估计、均值回复半衰期。

---

## 附录 A：Dry-run 全过程（概念验证证据）

策略：**缩量回踩 20 日均线 + 阳包阴吞没 → 买入，ATR(14) 止损**

### 用当前 DSL 能写到哪一步

```
① 缩量
   shrink = volume / ts_mean(volume, 20) < 0.7              ✅ 现有算子可写

② 回踩 20 日均线（容差 2%）
   touch = abs(close - ts_mean(close,20)) / ts_mean(close,20) < 0.02
                                                            ✅ 现有算子可写

③ 阳包阴吞没（四个子条件同时成立）
   engulf = (close > open)                                  今天阳线
          * (ts_delay(close,1) < ts_delay(open,1))          昨天阴线
          * (close > ts_delay(open,1))                      今收 > 昨开
          * (open  < ts_delay(close,1))                     今开 < 昨收
                                                            ⚠️ 乘法当 and，能硬凑
                                                            ❌ NaN 语义是错的

④ 组合
   buy = shrink * touch * engulf                            ⚠️ 同上

⑤ ATR 止损
   tr  = max(high-low, abs(high-ts_delay(close,1)), abs(low-ts_delay(close,1)))
       = 三层嵌套的 (a+b+abs(a-b))/2                         ⚠️ 能硬凑，完全不可读
   atr = ts_rma(tr, 14)                                     ❌ 硬缺口，无法表达
```

**结论**：① ② 能写，③ ④ 能硬凑但语义有坑，**⑤ 完全写不出来**。

### 用目标 DSL（本 ADR 设计）重写

```
# 序列声明（查注册表，非硬编码）
close, open, high, low, volume : Series
    # domain=行情, freq=日, as_of=收盘, nan_policy=停牌→unknown

# 缩量
ma20       : Series     = ts_mean(close, 20)               # lookback 19
vol_ratio  : Series     = div(volume, ts_mean(volume,20))  # lookback 19
shrink     : BoolSeries = lt(vol_ratio, 0.7)               # Series×Scalar→BoolSeries ✓

# 回踩
dev        : Series     = div(abs(sub(close, ma20)), ma20)
touch      : BoolSeries = lt(dev, 0.02)

# 阳包阴吞没
today_bull     : BoolSeries = gt(close, open)                          # lookback 0
yesterday_bear : BoolSeries = lt(ts_delay(close,1), ts_delay(open,1))  # lookback 1
engulf_high    : BoolSeries = gt(close, ts_delay(open,1))              # lookback 1
engulf_low     : BoolSeries = lt(open,  ts_delay(close,1))             # lookback 1
engulf : BoolSeries = and(and(today_bull, yesterday_bear),
                          and(engulf_high, engulf_low))

# 组合
buy : BoolSeries = and(and(shrink, touch), engulf)

# ATR 止损
prev_close : Series = ts_delay(close, 1)
tr   : Series = max3(sub(high,low),
                     abs(sub(high, prev_close)),
                     abs(sub(low,  prev_close)))            # lookback 1
atr  : Series = ts_rma(tr, 14)                              # state=✅ warmup=14
stop_long : Series = sub(entry_price, mul(2.0, atr))
```

### 前视检查（验证器递归推导，纯计算）

```
shrink → vol_ratio → ts_mean(volume,20)          lookback 19   causal ✅
touch  → dev → ma20 → ts_mean(close,20)          lookback 19   causal ✅
engulf → ts_delay(·,1)                           lookback  1   causal ✅
buy    = max(19, 19, 1)                          lookback 19   causal ✅ 无前视

atr    → tr → ts_delay(close,1)                  lookback  1   causal ✅
       → ts_rma(·,14)                            lookback  ∞   causal ✅
```

**warmup 推导**：`buy` 链 19，`atr` 链 14，两条链并行 → 总 warmup = max(19,14) = **19**。
引擎自动向前多取 19 个交易日，信号从第 20 天开始，预热段标 `unknown`。

### Dry-run 暴露的 4 个概念问题（全部已在 §4/§7/§8/§9 处理）

1. `lookback=∞` 需要 `warmup` 成为一等字段，且可为参数的函数 → **§4 + §8**
2. 实盘需状态持久化、回测不需要 → **§7 三路一致性**
3. 一字板影响范围比初判窄（不影响 true range）→ **§9 已精确化**
4. `unknown` 与停牌语义冲突 → **§9 停牌 = unknown**

---

## 附录 B：能力矩阵（哪些能硬凑，哪些不能）

用现有 28 个运算（⑬）+ 算术技巧能否模拟目标能力：

| 目标能力 | 硬凑写法 | 可行？ | 判定 |
|---|---|---|---|
| `and(a,b)` | `a * b` | ⚠️ 数值上可行，**NaN 语义错** | L1.5a 必需 |
| `or(a,b)` | `a + b - a*b` | ⚠️ 同上 | L1.5a 必需 |
| `not(a)` | `1 - a` | ⚠️ 同上 | L1.5a 必需 |
| **`false AND unknown = false`** | **不可能** —— `0 * NaN = NaN`（IEEE 754 强制） | ❌ | **真能力缺口** |
| `if(c,x,y)` | `c*x + (1-c)*y` | ✅ | L1.5b 语法糖 |
| `max(a,b)` | `(a + b + abs(a-b)) / 2` | ✅ | L1.5b 语法糖 |
| `min(a,b)` | `(a + b - abs(a-b)) / 2` | ✅ | L1.5b 语法糖 |
| `max3(a,b,c)` | 两层嵌套上式 | ✅ 但**完全不可读** | L1.5b（为可审阅性） |
| `near(a,b,tol)` | `abs(a-b) < tol` | ✅ | L1.5b 语法糖 |
| 固定 N 天连续 | N 个 `ts_delay` 相乘 | ✅ | L1.5b |
| **EWMA / RMA / IIR** | **不可能** —— DAG 无法引用自身历史输出 | ❌ | **真能力缺口** |
| **OBV 等累积量** | 不可能 | ❌ | **真能力缺口** |
| **最大回撤 / running peak** | 不可能 | ❌ | **真能力缺口** |
| **任意长度 streak / `ts_since`** | 不可能（固定 N 可以，任意 N 不行） | ❌ | **真能力缺口** |
| **Kalman / GARCH** | 不可能 | ❌ | 非线性，§11 明确不做 |

**这张表是 §2 分层优先级的依据**：L1.5a（三值逻辑）与 L2（递推）是**不可替代**的，
L1.5b 是为了满足 ADR-024 的「可审阅」。

---

## Consequences

### 正面

- **price action 策略从「几乎写不出来」变成「大部分能写」** —— Dry-run 的 ① ② 现在就能写，
  ③ ④ 有了正确语义，⑤ 由 `ts_rma` 解决
- **ATR 的三层因果链（⑧）一次性断开** —— 补 `ts_rma` 后，止损可以用标准 Wilder 定义重写，
  且表达式可审阅
- **AI 挖因子的天花板抬高两个量级** —— 从「12 个封闭枚举 + 11 个硬编码字段」
  变成「开放式序列注册表 + 因子 DAG + 递推算子」
- **前视偏差从「靠人审」变成「机械否决」** —— 算子级 `lookback`/`causal` 声明 + AST 递归推导，
  纯计算，直接进 ADR-023 验证器链，**不增加模型不确定性**
- **递推发散风险有对冲** —— 特征根检查是 L2 的必要配套，没有它 L2 就是上膛的枪
- **回测/实盘一致性从人工纪律变成机器可验证** —— Batch ≡ Step ≡ Step-from-persisted
  三路属性测试
- **序列扩展成为配置变更而非代码变更** —— SeriesSpec 注册表 + §5.1 的通用 `GetSeries`，
  加一种数据源**不用改 Provider 接口**（否则改一次动 5 个实现）；且为那 11 张只写不读的表
  留了出口而不推翻 ADR-027 的裁决
- **三套并存的数据注入约定收敛成一套** —— `FactorAware` / `FundamentalAware` /
  `SetAggregator`（§5.1）都是隐式依赖，收敛后**数据依赖显式出现在表达式里**，
  引擎可静态推导所需序列 → 自动预热 + `as_of` 对齐检查 + 缺失时明确失败
- **顺带解掉 ADR-027 §Context⑬ 的 per-day HTTP 陷阱** —— 引擎能从 AST 静态推导所需序列后，
  就没有「每天把全市场数据 POST 出去算信号」的必要（§5.1）
- **不违反 ADR-024** —— L1/L1.5/L2 全部保持「起点确定、可审阅、可复现」，
  且算子集合可枚举这一点**强化**了 ADR-023 的校准目标

### 代价 / 限制

- **类型系统是一次性的大改** —— `Series`/`BoolSeries`/`Scalar` 三类型 + 全部算子签名
  要重新声明。存量 28 个运算的表达式需要迁移（可用别名与隐式提升降低成本，但仍是面广的机械改动）
- **每个 `state=true` 算子要写三份东西** —— `Batch` + `Step` + 状态序列化，
  外加三路一致性属性测试。L2 的 8 个算子就是 8×3 份实现 + 8 组测试
- **warmup 让回测的实际数据需求变大** —— 引擎必须自动向前多取，
  且「回测区间」与「信号起始日」不再重合，需要在结果里如实披露
- **三值逻辑会改变现有表达式的行为** —— 现在 NaN 传染一切，改后 `false` 能吸收 `unknown`。
  这是**修正**，但意味着历史回测结果不可比，需要标注
- **`==` deprecated 会触动存量表达式** —— `defaultSignalExpression()` 里若有依赖精确相等的，
  需要逐个改为 `near`
- **验证器链从五维变八维** —— 前视 / 递推稳定性 / warmup 充分性三个新维度都是纯计算，
  但都要实现、测试、并接入 `pkg/validation` 的门禁
- **ADR-024 的边界被推远了** —— 虽然没跨过（L3 仍明确不做），但「表达式」这个词
  现在涵盖有状态递推，比 ADR-024 写作时的含义宽。需要在 ADR-024 加一条注记指向本 ADR

### 未解决 / 需后续 ADR

- **算子清单的完整定义** —— 本 ADR 定了契约（§4 的 7 项声明）与 L2 的 8 个算子，
  但**每个算子的精确语义、边界条件、NaN 行为**需要一份独立的算子规格文档
- **因子 DAG 的求值调度** —— 拓扑序 + 循环检测 + 公共子表达式消除 + 缓存策略。
  L1 的具体设计
- **`ts_iir` 的稳定性检查怎么算特征根** —— 高阶 AR 的特征根求解在数值上不稳定，
  需要定判据（如 Schur-Cohn 判据而非直接求根）
- **非线性状态空间的取舍** —— GARCH 是波动率建模的标准工具，但 §11 明确推迟。
  若风险管理真的需要它，要么开新 ADR，要么在 `pkg/risk` 里继续 Go 硬编码（延续现状）
- **文本降维（reduction-service）与 SeriesSpec 的对接契约** —— 服务归属已在
  [ADR-027](adr-027-modular-decomposition-to-independent-services.md) §2.6.2 裁定
  （data-service 的 7 个限界上下文里唯一物理独立的一个）。**仍未定的是语义对接**：
  `Hotness` / `DecayHalfLife` / `WeightSeries` 如何映射到 SeriesSpec 的 `as_of` 与
  `nan_policy` —— 一条新闻的可用时刻是**发布时间**还是**次日开盘**？这个选择直接决定
  用新闻因子回测有没有前视偏差
- **`SeriesRef` 的字段与批量语义** —— §5.1 定了「一个通用方法而非每种数据一个方法」的原则，
  但 `SeriesRef` 具体带哪些字段、一次能否请求多个序列、与既有 `BulkLoadOHLCV` 的关系
  （并存 / 收编 / 废弃）需在 ADR-027 §5 第 5 步之前定稿
- **实盘状态持久化的存储位置** —— 属于 B 类业务状态，但住回测服务还是 ai-service，
  取决于 §7 的 event-loop 归谁。ADR-027 §2.6.3 已裁定「实盘 event 路径新建，不复用
  `EventBus` / `DataFeed`」，但**归哪个进程仍未定**
- **ATR 的过渡期处置** —— 本 ADR 裁定「挂起重写 + 立刻加诚实性标注」
  （回测结果标注 `atr_method: "sma_tr"`），但**标注本身要改 `pkg/risk` 与回测结果 DTO**，
  这是一条独立的 P1 任务
- **AGENTS.md §2 与 ADR-024 的 `value`/`quality` 说法已过时** —— 两处都写
  「`value` / `quality` 给不出默认表达式，明确失败」，但 `ai/yaml/generator.go:295-313`
  实证：**两者都有默认表达式**（`cs_rank(neg(pe)) + cs_rank(neg(pb)) > 1.6` /
  `cs_rank(roe) + cs_rank(roa) > 1.6`），只有 `custom` 返回 `ok=false`。这是 P2-12
  接入 pe/pb/roe/roa 之后未同步的漂移，按 AGENTS.md Rule 1 / Rule 4 该修
  （已同时登记在 ADR-027 的「未解决」表）

---

## 落地检查清单

Proposed → Accepted 的判据：

- [ ] 用户（实验室主任）批阅分层模型（§2）与算子声明契约（§4）
- [ ] 用户批阅三值逻辑语义（§3）与停牌 = `unknown`（§9）
- [ ] 算子清单的完整规格文档产出（§未解决 第 1 项）
- [ ] L1.5a 三值逻辑落地，且 Dry-run 策略的 ③ ④ 步能正确表达
- [ ] L2 的 `ts_rma` 落地，且 ATR 用它重写、`Batch ≡ Step ≡ Step-from-persisted` 三路测试通过
- [ ] SeriesSpec 注册表落地，初始只含 11 条序列（6 行情 + 5 基本面），扩展不改代码
- [ ] **`Provider.GetSeries` 落地（§5.1）**，且 `data_provider.go:13-34` 的两个硬编码 map
      改为查注册表；`FactorAware` / `FundamentalAware` / `SetAggregator` 三套注入约定
      逐个退役
- [ ] `FactorType` 收敛为注册表数据，`factor_hypothesis_tool.go:127` 的白名单门禁解除
- [ ] 验证器新增三维（前视 / 递推稳定性 / warmup 充分性）落地并接入 `pkg/validation`
- [ ] warmup 递归推导落地，回测结果如实披露预热长度
- [ ] ATR 诚实性标注落地（`atr_method: "sma_tr"`）
- [x] 文档同步（ADR-027 部分）：图 B 已补策略服务与 reduction-service、§2.6 已写
      DDD 限界上下文与数据面裁决、§Context⑫⑬⑭ 已补录 —— 见 ADR-027 同日追加
- [ ] 文档同步（其余）：ADR-024 加注记指向本 ADR；AGENTS.md §1/§2/§11
      （含 `value`/`quality` 漂移修正）；SPEC.md 的算子与序列契约；ARCHITECTURE.md
- [ ] 创建 ODR 记录本次头脑风暴与裁决过程（Rule 2：变更开发工具或流程 → Tooling/Process）

---

_Date: 2026-10-06（§5.1 为同日补，初稿遗漏了「谁按 SeriesSpec 读数据」这一环）_
_Author: AI 实验员（Hermes）+ 实验室主任（若曦）三轮头脑风暴_
_Evidence: 全部结论基于源码取证，关键位置 —— `pkg/ai/expression/{operators.go, evaluator.go, parser.go}` ·
`pkg/strategy/expression/data_provider.go:13-34, 62-64, 113-117` ·
`pkg/risk/stoploss.go:57-128, 336-350` · `pkg/domain/factor.go:8-54` ·
`pkg/tools/builtin/factor_hypothesis_tool.go:127` ·
`pkg/marketdata/provider.go:10-22（11 方法，无通用序列入口）` ·
`pkg/strategy/plugins/sentiment.go:30-44（SetAggregator 隐式注入）` ·
`pkg/ai/yaml/generator.go:277-313（7 个意图类型全部有默认表达式）` ·
`pkg/backtest/engine.go:669（date-loop）, 1118-1135（两级分派 + 三套注入约定）, 1233-1272（per-day HTTP）`_
_Supersedes: 无。Extends ADR-024（表达能力分层），与 [ADR-027](adr-027-modular-decomposition-to-independent-services.md)（服务边界）正交且互相引用_
