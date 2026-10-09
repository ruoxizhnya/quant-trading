# ADR-030（草稿）: 观察清单 —— 跨 ADR 交付缺口与横向框架对比暴露的改进项

> **Status**: **Draft** —— 观察与建议登记，**尚未裁决**。本文件不取代任何 ADR，只把散落在对话里的、
> 现有 ADR 尚未覆盖的改进项集中成可引用、可派工的条目（编号 `OBS-xx`）。
> **路径注记（2026-10-09）**：本文出现的 `pkg/ai/expression` 现为 **`pkg/expression`**、`pkg/ai/contracts` 现为 **`pkg/backtest/contracts`**（ADR-027 §5 第 3 步已执行）；正文保留 2026-10-07 时点的取证记录。
> **Date**: 2026-10-07
>
> **吸收状态（2026-10-08）**：本清单是 [design/kernel/target-architecture-modular-kernel.md](../design/kernel/target-architecture-modular-kernel.md)（模块化内核目标架构，D1–D5 已拍板）的直接输入。**已被吸收 / 裁决**：OBS-02 / 03 / 04 → 数据契约与 msgbus（同步分发）；OBS-05 → D2（能用 L2 不上 L3）；OBS-13 → D1（实盘-ready）；OBS-15 → eventstore（先记录后分发）。**未吸收、留作独立工单**：OBS-01（空票池假成功，最高优先级）/ OBS-06 / 07 / 08 / 09 / 10 / 11 / 12；OBS-14 为外部先例参考（非工单）。
> **Category**: Architecture / Governance
> **Related**: [ADR-023](adr-023-ai-experimenter-lab.md) · [ADR-024](adr-024-expression-as-execution-target.md) ·
> [ADR-027](adr-027-modular-decomposition-to-independent-services.md) ·
> [ADR-028](adr-028-tiered-extension-of-strategy-expression-capability.md) ·
> [ADR-029](adr-029-ai-layer-2026-agent-practice-alignment.md) · [ADR-019](adr-019-service-merge-ai-copilot.md)
> **Upstream**: 用户（若曦）2026-10-07 三轮对话 ——
> ①「回测到数据服务不应该是 event driven 么」②「我期待的回测数据框架类似于 vn.py」
> ③「把看到的应该 improve 的地方写下来，写进一个 draft ADR」

---

## Context

### 触发

本轮对话从「回测读数据为什么是同步的」开始，一路核到三个框架的源码级对比，暴露出两类问题：

1. **交付缺口** —— 现有 ADR（尤其 027/028/029）覆盖了大量架构议题，但有几条**实测撞出来的缺陷
   没有任何一份 ADR 提及**。这类缺口的特点是：不影响架构自洽，但会让「跑起来的结论」不可信。
2. **表达力定位未定** —— 与 vn.py / nautilus_trader 对比后，发现「随机方程 / 状态空间类建模」在本项目
   的落点（走 ADR-028 L2 递推算子，还是 ADR-029 L3 WASM）**没有裁决**。

本 ADR 只做一件事：把这两类写成可派工的条目。

### 实测现状（全部基于源码取证，非印象）

| # | 事实 | 证据 |
|---|---|---|
| ⑬a | 回测 `total_trades=0` + `start_date=0001-01-01`（零值）+ `sortino_ratio=1.797e308`（MaxFloat64）时，日志仍打印成功 | `/api/pipeline/run` 实测（2026-10-07） |
| ⑬b | 根因：`index_constituents` / `sectors` / `stock_sector_map` 全 **0 行** ⇒ YAML 默认 `universe: csi300` 解析为**空集** | DB count 实测 |
| ⑬c | **`空票池` / `total_trades` / `ts_event` 三个关键词在 `docs/adr/` 全仓 grep 零命中** | 本轮核验 |
| ⑬d | 回测对 store 的 6 处直连**全是 per-run 批量预取**：`GetTradingDates` / `GetListingWindows` / `GetFundamentalsPITBulk` / `GetDividendsInRange` / `GetSplitsInRange` / `GetFactorCacheRange` | `pkg/backtest/engine.go` |
| ⑬e | `provider.BulkLoadOHLCV(ctx, symbols, start, end)` —— 一次性拉整个区间，非 per-bar | `pkg/marketdata` |
| ⑬f | `NewEventBus` 起 **4 个并发 `go dispatchLoop()`** 消费同一 channel | `pkg/marketdata/eventbus.go` |
| ⑬g | ADR-027 §2.6.3 判 `EventBus`「保留代码、明确挂起」，但 **`suspended` 注记未落地**：全仓 grep `suspended` 在 `pkg/` `cmd/` 只命中 `pkg/ai/factor/capital_flow.go` 的**业务注释**（停牌日） | 本轮核验 |
| ⑬h | `cmd/analysis/setup.go:385` 仍为 `marketdata.NewDataAdapter(nil, pgProvider, httpProvider, logger)` | 本轮核验 |
| ⑬i | repoguard 的未接线白名单是**包级**粒度（`unwiredPackages map[string]string`），`pkg/marketdata` 未登记（因为 `Provider` 在用），catch 不到「包在用、包内类型零接线」 | `internal/repoguard/package_wiring_test.go:68` |
| ⑬j | DSL 共 **28 个运算** = 14 序列函数（10 `ts_*` + 4 `cs_*`）+ 6 一元（`neg abs log sqrt sign exp`）+ 8 二元（`+ - * / ^ > < ==`）。**无 `>=` `<=` `!=` `AND` `OR` `NOT`** | `pkg/ai/expression/evaluator.go:180-384` |
| ⑬k | `POST /api/tools/validate_factor` 对 `CROSS(MA(close,5), MA(close,20))` 返回 `valid=true` —— 两个算子都不存在 | 实测（2026-10-07） |
| ⑬l | `close >= ts_mean(close,20)` → `false`；`... AND volume > 0` → `false` | 实测 |
| ⑬m | 数据广度：只有 `ohlcv_daily_qfq` 有矿（3,926,510 行 / 5671 票 / 727 交易日零缺口）；`stock_fundamentals` / `index_constituents` / `sectors` / `stock_sector_map` / `capital_flow` / `top_list` / `limit_up_pool` / `news` / `announcements` / `dividends` / `splits` **全 0 行** | DB count 实测 |
| ⑬n | nautilus 仓库已重构为 **Rust-native v2**：默认分支 `develop`，顶层 `crates/` + `python/` + `examples/`，**不再有顶层 `nautilus_trader/`** —— 旧路径 `nautilus_trader/examples/strategies` 已 404 | `api.github.com/repos/nautechsystems/nautilus_trader`（2026-10-07 核） |
| ⑬o | nautilus `MessageBus` 是 **`thread_local!` + `Rc<RefCell<MessageBus>>`**，注释原话：「designed for single-threaded use within each async runtime. Each thread gets its own `MessageBus` instance... **eliminating the need for unsafe Send/Sync implementations**」 | `crates/common/src/msgbus/mod.rs` |
| ⑬p | nautilus `Indicator` trait 是 **`&mut self` 有状态**契约：`name()` / `has_inputs()` / `initialized()` / `handle_bar()` / `handle_quote()` / `handle_trade()` / `handle_book()` / `handle_depth()` / `reset()`；未实现的 `handle_*` 默认 **panic**（fail-loud，非静默） | `crates/indicators/src/indicator.rs` |
| ⑬q | nautilus `crates/analysis/src/statistics/` 共 **34 个绩效统计量**（sharpe / sortino / calmar / omega / VaR / expected_shortfall / max_drawdown / ulcer / alpha / beta / information_ratio / tracking_error / treynor / up-down capture / returns_skewness / returns_kurtosis …）—— **零 GARCH、零 Kalman、零 HMM、零 OU** | `crates/analysis/src/statistics/` 目录列举 |

---

## 一、横向对比：策略表达能力的四层模型

这是回答「他们支持什么类型的策略」的统一框架，也是后面 `OBS-05` 的依据。

| 层 | 含义 | vn.py | nautilus_trader | 本项目 |
|---|---|---|---|---|
| **L0** 固定模板 + 参数 | 骨架由框架定，只填参数 | `CtaTemplate` 的 `parameters` / `variables` | 无（不提供模板） | 策略配置 + `FactorType` 枚举（ADR-028 §6 要改成注册表） |
| **L1** 表达式 DSL | 声明式公式，非图灵完备 | ✅ `vnpy.alpha.dataset` 表达式引擎（内置 **Alpha158**，源自微软 Qlib，支持自定义表达式函数注册） | ❌ 无 | ✅ `pkg/ai/expression`（28 个运算） |
| **L2** 通用代码 | 图灵完备，任意逻辑 | ✅ 任意 Python（`on_bar` / `on_tick`） | ✅ 任意 Python（`Actor` / `Strategy` + 自定义 `Indicator`；`on_save` / `on_load` 提供状态持久化钩子） | ⚠️ 当前无。**ADR-029 轨道 B = WASM**（`get_state` / `set_state` host API） |
| **L3** 外部模型注入 | 模型产信号，框架消费 | ✅ `vnpy.alpha.strategy`（Lasso / LightGBM / MLP 预测 → **截面多标的**与**时序单标的**两种策略类型） | ✅ `on_signal` / `SignalStrategy` / `Actor` 发布自定义 `Data`（官方 pattern 含 HMM 隐马尔可夫 regime 推断） | ❌ 无 |

**关键分野**：vn.py 与 nautilus 都是 **L2 图灵完备兜底** —— 表达式只是「方便」，不是「边界」。
本项目恰好相反：**L1 是边界，L2 是例外**（ADR-024 明确把「自由度是负债」写进判据）。

### 「随机方程 / 状态空间建模」能不能做？（用户提问）

| 框架 | 能吗 | 靠什么 | 框架提供什么保障 |
|---|---|---|---|
| **vn.py** | ✅ 能 | L2 任意 Python —— 自己写 Kalman / GARCH / OU / GBM / HMM（numpy / scipy / statsmodels） | **几乎为零**：无防前视检查、无 warmup 推导、无三路一致性、无可复现保证。自由度和风险一起给用户 |
| **nautilus_trader** | ✅ 能 | L2 任意 Python；官方 pattern 明确写了 `RegimeActor` 跑 HMM、`ML Model → Signal → Strategy`；`on_save` / `on_load` 提供状态持久化 | 有确定性保障（**虚拟时钟** ✅源码确认 + **单线程同步分发** ✅源码确认 + `ts_event` 排序 ⚠️文档来源），但**不校验你的模型有没有前视** |
| **本项目** | ⚠️ **当前不能** | —— | —— |

本项目不能的数学原因：DSL 是**窗口纯函数的 DAG**，DAG 无法引用自身历史输出。
ADR-028 附录 B 已逐条判为「真能力缺口」：

> `EWMA / RMA / IIR` — ❌ 不可能 · `OBV 等累积量` — ❌ · `最大回撤 / running peak` — ❌ ·
> `任意长度 streak / ts_since` — ❌ · **`Kalman / GARCH` — ❌ 不可能**

**但这不是「要上 WASM 就完事」** —— 见 `OBS-05`。

---

## 二、改进清单

分组：**G1 契约缺口（P0，决定后续一切自动化结论的可信度）** ·
**G2 表达力（P1）** · **G3 治理与文档（P2）**

### G1 — 契约缺口

#### OBS-01 · 空票池假成功（建议最高优先级，独立立项）

- **现象**：`total_trades=0`、`start_date=0001-01-01`（零值）、`sortino_ratio=1.797e308`（MaxFloat64 垃圾值），
  回测仍报 `Backtest completed successfully`。（⑬a ⑬b）
- **为什么严重**：后续所有自动化（AI 挖因子、TPE 参数搜索、多重检验校正 `adjP = 1-(1-rawP)^N`）
  都建立在「回测结果是有效观测」这个前提上。空票池产生的全是**伪造观测**，
  而 `loop.go` 会把它们当成真实尝试计入 `NumTrials`，**校正出来的 p 值是假的**。
- **三份新 ADR（027/028/029）零提及**（⑬c）。
- **建议契约**（三条，全部 fail-loud）：
  1. `universe` 解析为空集 ⇒ **拒绝进入回测**，不是跑完报成功；
  2. `total_trades == 0` ⇒ 结果标记 `inconclusive`，**不得**进入验证器链与基因池；
  3. 比率类指标在分母为 0 时返回 `NaN`，**不得**返回 `MaxFloat64`。
- **归属**：ADR-023 验证器链 + 新工单。改动小、独立、不牵动架构 —— 建议先做。

#### OBS-02 · 「物化边界」未成契约

- **现象**：异步数据生产（sync worker 的 `pending→running→completed` 状态机）与
  同步回测消费（date-loop）之间，必须存在一道**快照冻结**的接缝。实现里已经有了
  （`BulkLoadOHLCV` + 6 处 per-run 批量预取，⑬d ⑬e），**但没有任何 ADR 显式定义这条契约**。
- **取证**：grep「物化」在 `docs/adr/` 只命中 ADR-028 一处且语义不同；
  grep「快照」在 ADR-027 里指 `ai.experiment_runs` 的**状态图快照**，不是数据快照。（⑬c）
- **为什么必须显式**：这是「回测读数据为什么是同步的」这个问题的答案所在。
  不写成契约，下一个 agent（或 AI）很容易把它改回 per-bar / per-day 拉取而不触发任何门禁。
- **建议契约**：回测开跑前必须完成快照冻结并生成 `snapshot_id`（内容指纹），
  冻结失败 ⇒ fail-loud。回测期间只读该快照。
- **归属**：ADR-027 补一节，或本条转正为独立契约 ADR。

#### OBS-03 · `DataEvent` 缺双时间戳

- **现象**：ADR-027 §2.6.3 判定「不复用 EventBus」的**唯一理由**是语义缺口 ——
  `DataEvent{Type, Symbol, Timestamp, Payload}` 没有字段标注数据来自**历史回放**还是**实时**，
  而这是防前视的必需语义。
- **现成解法**：nautilus 的双时间戳 —— `ts_event`（领域发生时刻）/ `ts_init`（系统创建时刻），
  且**按 `ts_event` 排序后再投递**（timers 存 `BTreeMap` 保证遍历序稳定，clock 强制时间非递减）。
- **建议**：抄这个**字段设计**（不必抄总线）。这是我们唯一该从 nautilus 拿的东西。

#### OBS-04 · 现有 `EventBus` 与确定性直接冲突，且「挂起」注记未落地

- **现象 1**：`NewEventBus` 起 **4 个并发 `go dispatchLoop()`** 消费同一 channel（⑬f）。
  nautilus **源码级**的反证（⑬o）：MessageBus 是 `thread_local!` + `Rc<RefCell<MessageBus>>`，
  注释原话 ——

  > "MessageBus is designed for **single-threaded use** within each async runtime. Each thread gets
  > its own `MessageBus` instance, avoiding synchronization overhead... **eliminating the need for
  > unsafe Send/Sync implementations**."

  ⇒ **哪天接线都不能照搬现有实现**。Rust 侧是**类型系统强制**的单线程（没有 `Send`/`Sync` 实现），
  我们是 4 个 goroutine 抢同一个 channel —— 照搬会立刻破坏确定性。
- **现象 2**：ADR-027 判「保留代码、明确挂起」，但注记**没落地到代码**（⑬g），
  `setup.go:385` 仍传 `nil`（⑬h）。
- **现象 3**：repoguard 的白名单是**包级**粒度，`pkg/marketdata` 因 `Provider` 在用而未登记，
  ⇒ catch 不到「包在用、包内类型零接线」（⑬i）。
- **建议**：
  1. 落地 `// suspended: see ADR-027 §2.6.3` 注记 + `unwiredPackages` 登记；
  2. repoguard 补**符号级**登记能力，或加一条「生产路径不得引用 `EventBus` / `BackpressureBus` /
     `SimulatedDataFeed`」的断言；
  3. 在 `eventbus.go` 顶部写明「本实现为并发消费，与回测确定性不兼容，接线前须重写」。

### G2 — 表达力

#### OBS-05 · 随机 / 状态空间建模的落点未裁决（本轮对比暴露）

- **矛盾**：ADR-028 附录 B 把 `Kalman / GARCH` 判为「非线性，§11 明确不做」（那时 §11 说 L3 推迟）；
  但 ADR-029 落地后 L3（WASM）**是图灵完备的**，且提供 `get_state` / `set_state`。
  ⇒ 递推类算子（EWMA / RMA / IIR / Kalman / GARCH）到底走哪条路，**没有裁决**。
- **建议裁决**：**L2 优先，L3 兜底**，并写死判据 ——

  | 判据 | L2 递推算子 | L3 WASM |
  |---|---|---|
  | 能否用有限算子组合表达 | ✅ 必须走 L2 | 仅当不能 |
  | warmup 可否静态推导 | ✅ 算子声明即可 | ❌ 不可判定 |
  | 前视防护 | AST 递归推导 | host trap（`t_offset > 0`）—— 更强，但仅此一招 |
  | 算子集合可枚举 ⇒ 校准 | ✅ | ❌ |
  | 三路一致性（Batch ≡ Step） | ✅ 可属性测试 | 需 `get_state` 版本管理 |

  理由沿用 ADR-028 §2 已判过的那句：**「多几个算子不是造仪器，是给仪器加刻度」**。
  `ts_rma` / `ts_ewma` / `ts_kalman` / `ts_garch_vol` 全是**确定性算子** —— 给定输入必有唯一输出，
  可单测、可审阅、warmup 可推导。  **能用 L2 表达的，不允许上 L3。**
- **nautilus 的源码给了这个裁决一个反面对照**（⑬p）：它的 `Indicator` trait 是
  **`&mut self` 的有状态契约** ——

  ```rust
  pub trait Indicator {
      fn name(&self) -> String;
      fn has_inputs(&self) -> bool;      // ← warmup 语义
      fn initialized(&self) -> bool;     // ← warmup 语义
      fn handle_bar(&mut self, bar: &Bar);   // ← 有状态：可变借用
      fn handle_quote(&mut self, quote: &QuoteTick) -> anyhow::Result<()>;
      fn reset(&mut self);
  }
  ```

  ⇒ 在 nautilus 里，**「有 state 的流数据处理」是一等公民**，自定义指标就是实现这个 trait。
  而它的代价也正是我们拒绝的：trait 实现是**代码**，不可枚举 ⇒ 验证器只能事后统计检验
  （对应 ADR-028 §2 判 L3 时说的「校准会更难」）。
  我们的 `SeriesSpec` + 算子契约是**注册表数据**，可枚举 ⇒ warmup 可静态推导、可三路属性测试。
- **另注意一条值得抄的细节**：nautilus 未实现的 `handle_*` 默认 `panic!`（fail-loud），
  不是静默返回 —— 与 OBS-01 要的 fail-loud 是同一种工程取向。

- **注意**：ADR-028 §4 的算子声明契约已有 `state` 字段 —— 说明设计上已预留，缺的只是裁决。

#### OBS-06 · DSL 语法闸门不校验算子名

- **现象**：`CROSS(MA(close,5), MA(close,20))` → `valid=true`（两个算子都不存在）（⑬k）。
  当前 L1 校验器只做语法解析，**不查算子名**。
- **后果**：闸门给的是**假合法**。AI 挖因子时会把不存在的算子一路带到求值阶段才炸，
  而且炸的位置离出错点很远。
- **建议**：算子白名单 + 签名校验（arity / 参数类型 / 返回类型），与 ADR-028 §3 类型系统对齐。

#### OBS-07 · tokenizer 不支持比较与逻辑运算符

- **现象**：`close >= ts_mean(close,20)` → false；`... AND volume > 0` → false（⑬l）。
  当前二元算子只有 `+ - * / ^ > < ==`（⑬j）。
- **关系**：ADR-028 §L1.5a 把**布尔三值逻辑**列为「真能力缺口 · 优先级最高」，
  本条是它的**实现前提** —— 先有 token，再有语义。
- **建议**：`>=` `<=` `!=` 与 `AND` `OR` `NOT` 一起补；语义按 ADR-028 §3 的 Kleene 三值逻辑
  （`false AND unknown = false`，这一点算术硬凑不出来，`0 * NaN = NaN` 是 IEEE 754 强制）。

#### OBS-08 · 序列注册表缺「可用性声明」

- **现象**：11 张表全 0 行，只有价量有矿（⑬m）。而 ADR-028 §5 的 `SeriesSpec` 只描述
  「序列是什么」，不描述「序列有没有数据」。
- **后果**：AI 挖因子在空表上**静默产出垃圾**，与 OBS-01 同源。
- **建议**：`SeriesSpec` 增加 `availability`（行数 / 标的覆盖率 / 最后更新日期），
  DAG 求值前对不可用序列 **fail-loud**；`Provider.GetSeries` 在缺失时返回明确错误而非空切片。

### G3 — 治理与文档

#### OBS-09 · 三份 ADR 引用成环且全为 Proposed

- **现象**：027 → 028 → 029 → 027 互相引用，且三份状态都是 `Proposed`。
- **建议**：027 先独立落地；028 / 029 中指向 027 的引用降级为「待 027 落地后重评」，
  并显式标注。成环的 ADR 无法分派，也无法做破坏验证。

#### OBS-10 · 服务数 5 → 7 与 ADR-019 的方向相反

- **现象**：ADR-027 把服务从 5 拆到 7；而 ADR-019（`Service 合并 + AI Copilot Sandbox 重构`，
  **Status: Accepted**，2026-06-11）的方向是**合并服务**。
- **建议**：ADR-019 仍为 `Accepted`，故 ADR-027 落地时应显式 `Supersedes: ADR-019` 或写明
  reconcile 理由。方向反转本身不是错（前提变了），但**不留痕就会被当成自相矛盾**。

#### OBS-11 · per-day HTTP 反模式待删除（登记，防遗漏）

- `getSignalsFromStrategyService`（定义 `engine.go:1233`，调用点 `engine.go:1124`）在
  **日循环内部**发 HTTP，body 带整个股票池的
  完整 K 线 —— per-day 而非 per-run。ADR-027 已判「**应当删除，而不是优化**」。
- 本条仅登记为待办，确保 OBS-02 落地时不遗漏。

#### OBS-12 · 对外 API 契约与前端脱节

- **现象**：Copilot 四层缺陷叠加 ——
  ① 前端发 `{prompt}` 而后端字段是 `description`（输入被吞）；
  ② `CopilotResponse` 是旧契约遗留 ⇒ `res.explanation` 恒 `undefined` ⇒ 兜底文案「策略已生成」是谎言；
  ③ 前端只 POST 一次，**不轮询** `GET /api/copilot/generate/{job_id}`；
  ④ 后端真实状态是 `sandbox_rejected`（`copilot.working_dir is not configured`）。
- **治理建议**：对外 API 契约版本化；前后端契约**同源校验**（由 `docs/openapi.yaml` 生成 TS 类型
  + 一条 CI 断言，字段不一致即 fail）。

#### OBS-13 · 「不做实盘」这个前提需在 ADR 层显式确认

- **为什么重要**：nautilus 的核心卖点是「**回测-实盘同构**」（同一套 Strategy 代码，回测与实盘
  只换 DataClient / ExecClient）。这个收益对我们**等于零**，因为我们不做实盘
  （VISION：在不上实盘的前提下验证投资逻辑）。
- **建议**：在 ADR-027（或 VISION）里显式写下「前提：无实盘 ⇒ 不采用 event-driven 回测」，
  使该结论**可被推翻而不是被默认**。哪天要做实盘，这条就是第一个要重评的地方。

#### OBS-14 · 可引用外部先例：nautilus 的 `AI_POLICY.md` 与本项目哲学一致

- **发现**：nautilus 仓库根目录有 `AI_POLICY.md`（另有 `AGENTS.md` / `CLAUDE.md`），
  原话摘录 ——

  > "AI tools may assist with project work, but **active human thinking and judgment remain
  > essential**."
  > "**A human must choose the work, direct the implementation, and verify the result.** ...
  > **Fully autonomous contributions, where an agent acts without meaningful human direction and
  > review, are not accepted.**"
  > "AI-assisted workflows should **reduce** the total engineering effort needed to produce a
  > review-ready contribution. They must **not shift** design, implementation, debugging, or
  > validation work onto maintainers."
  > "Maintainers have seen higher-quality pull requests when AI workflows **strengthen quality
  > assurance and thoroughness** ... instead of stopping at an unrefined first pass."

- **为什么重要**：这与本项目的「**AI 是操作仪器的实验员，人是实验室主任**」以及
  ADR-024 的「自由度是负债」是**同一套判断**，而且是一个成熟开源项目写进仓库的成文政策。
- **建议**：ADR-029（AI 层 2026 agent 实践对齐）引用它作为**外部佐证**，
  并把其中两条落到我们的门禁：
  ① 「不得把验证工作推给下游」⇒ 对应我们的「假护栏比没护栏更糟 / 必须破坏验证」；
  ② 「AI 工作流应加强 QA 而非停在初稿」⇒ 对应 ADR-023 的验证器链。

#### OBS-15 · 可观测性：nautilus 的 `BusTap` 模式值得抄

- **发现**：nautilus 的 MessageBus 有一等钩子 `trait BusTap`，在**每条消息派发给订阅者之前**
  被调用（`on_publish` / `on_send` / `on_response`），注释原话 ——

  > "The bus invokes the registered tap (when present) **before each publish, send, or correlation
  > response fanout, so subscribers cannot observe a message that has not yet been handed to the
  > tap**. ... it must not re-enter the bus (the bus is single-threaded...)"

  用途是**持久化事件存储**（durable event store，`crates/event_store`）。
- **对应我们的缺口**：当前有 `rid` 无 `span`（ADR-029 §8 已计划补轻量 span + 日志异步落库）。
- **建议**：ADR-029 落地 span 时参考这个「**派发前 tap**」语义 —— 它保证了
  「先记录、后投递」的顺序，**不会出现「订阅者已处理、日志还没写」的窗口**。
  这是我们不引入 event bus 也能拿到的一条设计。

---

## 三、派工建议（按依赖性排序）

| 顺序 | 条目 | 理由 |
|---|---|---|
| **1** | **OBS-01** 空票池 fail-loud | 改动小、独立；不做它，后面所有自动化的结论都不可信 |
| 2 | OBS-06 + OBS-07 | DSL 闸门是 L1.5a 的实现前提，且互相独立 |
| 3 | OBS-02 + OBS-03 | 契约层，与 ADR-027 落地绑一起 |
| 4 | OBS-04 | 注记落地 + repoguard 增强；属门禁，可独立 |
| 5 | **OBS-05** | 需要用户裁决（L2 优先 vs L3 兜底），裁完才派工 |
| 6 | OBS-08 | 依赖 ADR-028 §5 `SeriesSpec` 落地 |
| 7 | OBS-09 / OBS-10 / OBS-13 | 纯文档，一次性处理 |
| 8 | OBS-11 / OBS-12 | 分别与 ADR-027、前端契约绑定 |

**需要用户裁决的两条**：`OBS-05`（表达力落点）、`OBS-13`（实盘前提是否仍成立）。

---

## Consequences

### 正面

- 把「跑起来才撞得出来」的缺陷（OBS-01）与「架构图上好看但没定义」的契约（OBS-02）
  变成**可派工、可做破坏验证**的条目 —— 符合本项目「假护栏比没护栏更糟」的规矩。
- 表达力落点一旦裁决（OBS-05），ADR-028 与 ADR-029 的边界就清楚了：
  **L2 是能力扩展，L3 是兜底**，不再出现「028 说不做、029 又能做」的观感矛盾。
- 横向对比的结论落到纸面后，「要不要 event-driven 回测」这个问题**有据可查地结案**，
  而不是每次重开。

### 代价 / 限制

- 本 ADR 是**观察登记**，不是决策。除 OBS-05 / OBS-13 外的条目仍需各自的设计与破坏验证。
- OBS-01 的三条 fail-loud 会让**现有空数据环境下「回测成功」的演示全部变红** ——
  这是**预期行为**（本来就是假成功），但会让 UI 演示暂时更难看，需要提前与用户同步。
- OBS-08 的 `availability` 字段会让「加一种数据源」的成本略增（要填元数据），
  但换来的是不再静默产出垃圾。

### 未解决

- `NewTPEProposer`（`loop/proposer.go:40`）**未找到调用点** —— TPE 疑似已实现但未接入，
  需单独确认（属既有台账，不在本 ADR 范围）。
- Copilot 的 `sandbox_rejected`（`copilot.working_dir is not configured`）当前按 ADR-024
  判定为不阻断，但 UI 显示为失败态 —— 表现层与语义不一致，需另立工单。

---

## 四、对比结论修订记录（2026-10-07 拿到 nautilus 仓库后逐条回查）

前几轮的三框架对比有几处来自文档与二手来源。拿到仓库后逐条回查，结论分三类。

### A. 需要修正（我此前说错的 / 过时的）

| # | 此前说法 | 修订 | 证据 |
|---|---|---|---|
| **R-1** | nautilus 示例策略在 `nautilus_trader/examples/strategies/` | ❌ **路径过时**。仓库已重构为 Rust-native v2，该路径 404。现为顶层 `examples/{backtest,live,other,quickstarts,tutorials}` | `api.github.com/.../contents/` 逐层列举 |
| **R-2** | vn.py 回测引擎在 `vnpy_ctastrategy/backtesting/engine.py` | ❌ **路径过时**。4.x 起是**单文件** `vnpy_ctastrategy/backtesting.py`（1294 行 / 42KB），不再是包 | `api.github.com/repos/vnpy/vnpy_ctastrategy/contents/vnpy_ctastrategy` |
| **R-3** | 「vn.py 在 `new_bar` 旁写了『防止未来函数』动机注释」 | ❌ **这条引用是错的** —— 当前源码 grep「未来」**零命中**。结论仍成立，但**机制不是注释，是调用顺序**（见 R-4） | `backtesting.py` 全文 grep |
| **R-4** | （承上）vn.py 的防前视机制 | ✅ **改为**：`new_bar()` 内部顺序是 `cross_limit_order()` → `cross_stop_order()` → `strategy.on_bar(bar)` → `update_daily_close()`。**先用这根 bar 撮合，再把 bar 交给策略** —— 结构上使策略不可能「看到当根 K 线的成交后再下单」。这比一句注释硬得多 | `backtesting.py:666-674` |

**R-1 / R-2 的教训（写给自己）**：前几轮连续 404 时，我应该立刻怀疑「路径过时」并重新列目录，
而不是转向二手来源（DeepWiki / 搜索摘要）去拼结论。**404 是信号，不是噪音。**

### B. 源码级加固（结论不变，证据升级）

| 论断 | 新证据 |
|---|---|
| **vn.py 回测不用 event bus** | `backtesting.py`（1294 行）中 **`EventEngine` 出现 0 次**；`run_backtesting` 是 `for ix, i in enumerate(range(0, total_size, batch_size)):` + `for data in batch_data: func(data)` 的**纯同步嵌套循环**；`func = self.new_bar` 或 `self.new_tick`，直接函数调用 |
| **nautilus 单线程分发** | `crates/common/src/msgbus/mod.rs`：`thread_local!` + `Rc<RefCell<MessageBus>>`，注释「designed for **single-threaded use**... eliminating the need for unsafe Send/Sync implementations」——**类型系统强制**，不是纪律 |
| **nautilus 虚拟时钟** | `crates/system/src/clock_factory.rs`：`Environment::Backtest ⇒ VirtualClock::new()`，`Environment::Live \| Sandbox ⇒ LiveClock`；并有测试 `test_for_environment_backtest_uses_test_clock` 断言 |
| **回测-实盘同构** | 三环境枚举 `Environment::{Backtest, Live, Sandbox}`；`crates/backtest/` 与 `crates/live/` 是**两套对偶 crate** |
| **`BacktestDataIterator` 仍在** | `crates/backtest/src/data_iterator.rs`（36KB）存在 |

### C. 需要补充（此前漏画的）

| 补充 | 说明 |
|---|---|
| **nautilus 官方一句话定位** | 「**Production-grade Rust-native trading engine with deterministic event-driven architecture**」—— *deterministic* 被写进一句话定位 |
| **回测侧 crate 远不止两个替换点** | `crates/backtest/src/`：`accumulator.rs`（bar 聚合）· `data_batch.rs` · `data_iterator.rs` · `data_client.rs` · `execution_client.rs` · `exchange.rs`（85KB，模拟撮合）· `engine.rs`（164KB）· `node.rs` · `result.rs` · `modules/` · **`defi/`**（v2 还支持 DeFi）。此前我只画了 DataClient / ExecClient 两处 |
| **系统侧组件** | `crates/system/src/`：`kernel.rs`（82KB）· `trader.rs`（204KB）· `builder.rs` · `controller.rs` · **`event_store.rs`** · `clock_factory.rs` · `registration.rs` |
| **`event_store` 是一等 crate** | 顶层 `crates/` 里有独立的 `event_store`（配合 OBS-15 的 `BusTap`）—— 此前我的图里完全没有这一层 |
| **vn.py 4.x 的「分批」是进度条** | `batch_size = max(int(total_size / 10), 1)`，**仅用于** `progress_bar` 输出与进度打印，**不是**异步/流式分批处理。容易误读，记一笔 |

### 结论：哪些没被推翻

- **三方都不在回测日循环里发网络请求** —— 依旧成立（vn.py `history_data` 内存数组；nautilus `data_iterator` + Catalog；我们 `BulkLoadOHLCV`）。
- **不采用 event-driven 回测** —— 依旧成立，三条理由（无实盘 / 横截面 / 顺序执行天然确定）未被削弱。
- **该抄的两件**：双时间戳 `ts_event` / `ts_init`；`BusTap` 派发前钩子。
- ⚠️ 其中 **`ts_event` 排序**一条仍是**文档来源**，v2 源码未逐条核 —— 引用时应标注证据等级。

---

## 附录：三框架策略类型对照（回答「他们支持什么策略」）

| 策略类型 | vn.py | nautilus_trader | 本项目 |
|---|---|---|---|
| 单标的时序（CTA：双均线 / 突破 / ATR 止损） | ✅ 原生（`CtaTemplate`） | ✅ 原生 | ✅ 表达式（ATR 走 Wilder 要等 `ts_rma`，ADR-028 ⑤） |
| 套利 / 价差（SpreadTrading） | ✅ 原生模块 | ✅ 多腿订单 + 组合 | ❌ |
| 做市 / 订单簿微观结构 | ❌ | ✅ 原生（L1/L2/L3 book、`OrderBook` 对象、`OrderBookImbalance` 官方示例） | ❌（无 tick 数据） |
| 执行算法（TWAP / VWAP / 冰山） | ❌（有 `AlgoTrading` 模块） | ✅ 原生（ExecAlgorithm） | ❌ |
| 期权（Greek / 波动率曲面） | ✅ `OptionMaster` | ✅ `OptionGreeks` / `OptionChainSlice` | ❌ |
| **截面多因子选股** | ✅ `vnpy.alpha`（**Alpha158** + 截面策略类型） | ⚠️ 可做但需自己拼（无内建 `cs_rank` 语义） | ✅ **这是本项目的主场**（`cs_rank` / `cs_neutralize`） |
| **机器学习预测 → 信号** | ✅ `vnpy.alpha.model`（Lasso / LightGBM / MLP） | ✅（`Actor` 托管模型 + `on_signal` 注入信号） | ⚠️ 有 gene pool / TPE，但信号回注链路待确认 |
| **有 state 的流数据处理**（自定义累积指标） | ✅（`ArrayManager` 或自己写 Python） | ✅ **一等公民** —— 实现 `Indicator` trait，`&mut self` 有状态，带 `has_inputs()` / `initialized()` warmup 契约（⑬p） | ❌ 当前不能（DAG 无法引用自身历史输出）；落点见 OBS-05 |
| **随机方程 / 状态空间**（Kalman / GARCH / OU / HMM） | ✅ 靠 L2 任意 Python，**框架无保障** | ✅ 靠 L2 任意 Python + `on_save` / `on_load` 状态持久化；但**框架内零实现** —— `crates/analysis/src/statistics/` 34 个统计量全是**绩效指标**，无一个随机过程（⑬q） | ❌ 当前不能；落点见 OBS-05 |

**nautilus 现状补充（2026-10-07 核）**：仓库已重构为 **Rust-native v2**（默认分支 `develop`，
顶层 `crates/` / `python/` / `examples/`）。`examples/backtest/` 下的官方示例是
`architect_ax_book_imbalance` / `architect_ax_mean_reversion` / `crypto_orderbook_imbalance` /
`tardis_option_chain` / `signal_reentrancy` / `synthetic_data_pnl_test` / `model_configs_example` ——
**全部是微观结构、均值回归、期权、信号注入类，没有经典 CTA 双均线示例**。
这印证了它的定位：多 venue / 多资产的执行与撮合同构，而非 A 股横截面选股。

**一句话总结**：vn.py 和 nautilus 的边界都是「**你能写什么 Python**」——
框架给的是执行与风控基础设施，策略表达力本身**不设限，也不设防**。
本项目把边界设在表达式（L1），换来的是可审阅、可复现、可校准 ——
代价正是 OBS-05 要裁决的那件事。
