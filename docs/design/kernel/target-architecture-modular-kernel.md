# 目标架构设计：模块化内核 + 四层表达力

> **状态**：Draft（设计蓝图；**2026-10-08 若曦已拍板 D1–D5**，见 §11）
> **日期**：2026-10-08
> **作者**：若曦 × WorkBuddy
> **输入**：nautilus_trader（本地源码 `C:\Users\ruoxi\Workspace\nautilus_trader`，develop 分支）· vnpy（本地源码 `C:\Users\ruoxi\Workspace\vnpy`）· 本仓库现状盘点 · ADR-023/024/027/028/029/030
> **定位**：这不是 ADR（不记决策理由），是**目标架构蓝图**。拍板后从中抽 ADR-031。本文与 ADR-030 的关系：ADR-030 是「观察到的改进点清单」，本文是「这些改进点收敛成的目标形态」。

---

## 0. 一页结论

把回测与实盘改造成 **nautilus-like 的模块化内核**，并支持 **L0–L3 四层表达力**。

三个核心设计判断：

1. **抄 nautilus 的「结构」，不抄它的「并发模型」**。抄的是：内核装配（Kernel）、可替换边界（Clock / DataClient / ExecClient）、策略流式 trait、消息命名注册表（switchboard）、事件存储（先记录后分发）。不抄：event-driven 日循环——我们的主场是 5900 票 × 727 天的**横截面**，不是单标的事件流。
2. **双模式策略接口**。现有 `SignalGenerator`（批式：一次给全量 bars 返回信号）保留服务 L0/L1 与横截面；新增 `BarHandler`（流式：`OnBar` 逐 bar 回调 + `SaveState/LoadState`）服务 L2/L3 有状态策略。**回测-实盘同构的桥**是：批式策略在流式引擎里也能跑（引擎攒够窗口再回调），流式策略在回测里用 VirtualClock 驱动。
3. **每个模块四件套**：接口定义 / DB 归属 / 数据流定义 / 功能设计定义。11 个模块全部登记在 §5 的矩阵里，没有「黑盒模块」。
4. **核心部件按实盘-ready 设计（D1 已拍板）**：Broker / DataFeed / Clock / ExecEngine / Reconciliation 这些核心抽象**本着将来做实盘去设计、留足冗余**，但当前只实现 paper / 模拟撮合；实盘**专属**部件（真实券商对接、合规报送）不做，留作将来。回测-实盘同构从「可选项」升级为「设计目标」。

> **2026-10-08 拍板（若曦）**：D1 保留实盘能力、核心部件实盘-ready；D2 能用 L2 不用 L3；D3 先做核心部件模块化、且模块要有独立定义以便独立 coding agent 并行完成；D4 EventStore 全量落 `audit.message_log`；D5 P4 执行算法按「模拟真实大单」做。五条全部采纳、无重大错误。评估与分寸提醒见 §11。

---

## 1. 两个想法的设计翻译

| 你的想法 | 翻译成架构语言 | 落点 |
|---|---|---|
| **想法 1**：回测/实盘改造成 nautilus-like 结构，每个大功能模块化，每个模块有自己的接口、DB（如有）、数据流定义、功能设计定义 | 引入 **Kernel 装配层** 取代 `setup.go` 的 831 行硬编码；模块间只经**命名消息注册表**通信；每模块在 §5 矩阵登记四件套，**且定义细到「独立 coding agent 能不被打扰地独立完成」（D3）** | §4 目标架构 + §5 模块矩阵 + §5.1 边界契约 + 组件连接图 |
| **想法 2**：回测/实盘支持全部 4 层表达力 | 新增**流式策略接口** `BarHandler` 补 L2/L3；L0/L1 沿用现有批式；L3 分 WASM 沙箱（L3a）与外部模型信号注入（L3b） | §6 四层表达力 + 双模式接口 |

---

## 2. 现状：我们离 nautilus 有多远

先说结论：**边界基础比想象的厚，缺的是「装配」和「流式」两块**。

### 2.1 已有的接口资产（源码取证）

`pkg/` 下已有 **60+ 个 Go interface**，关键的几个已经长得像 nautilus 的对应物：

| 我们的接口 | 位置 | nautilus 对应物 | 差距 |
|---|---|---|---|
| `marketdata.Provider` | `pkg/marketdata/provider.go` | `DataClient` | ✅ 已够用（`BulkLoadOHLCV` 批量预取） |
| `live.Broker` | `pkg/live/engine.go` | `ExecutionClient` | ✅ 已够用（Connect/SubmitOrder/GetPositions） |
| `live.DataFeed` | `pkg/live/engine.go` | `DataEngine` 订阅侧 | ⚠️ 只有 `Subscribe/GetQuote/SetCallback`，无 bar 聚合 |
| `domain.RiskManager` | `pkg/domain/types.go` | `RiskEngine` | ⚠️ 方法少（CalculatePosition/DetectRegime/CheckStopLoss），无订单前置风控 |
| `strategy.Strategy`（4 子接口） | `pkg/strategy/interfaces.go` | `Strategy` trait | ❌ **形态不同**：我们是批式，nautilus 是流式（见 §2.2） |
| `live/reconciliation/*`（4 接口） | `pkg/live/reconciliation/` | `execution/reconciliation` | ✅ 方向已对（BrokerQuerier/LocalSnapshotter/AlertDispatcher/ReportPersister） |
| `tools.Tool` + `Registry` | `pkg/tools/` | — | ✅ MCP 工具层，nautilus 无此概念（它没有 AI 实验员） |
| `validation` 六维验证器 | `pkg/validation/` | — | ✅ 我们没有的 nautilus 也没有：证伪链是我们的独有能力 |

### 2.2 三处关键差距

**差距 ①：策略接口是「批式快照」，nautilus 是「流式回调」。**

我们的 `SignalGenerator`（`pkg/strategy/interfaces.go:75`）：

```go
GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]Signal, error)
```

一次性给**全量历史 bar**，返回信号列表。这是**横截面批式**——对 `cs_rank` 这类「同日全市场排序」是高效的，一次向量化算完。

nautilus 的 `Strategy`（`crates/trading/src/strategy/core.rs`）是**流式**：`on_bar(&mut self, bar)` 逐 bar 回调，`&mut self` 持有内部状态，配 `OrderFactory` 下单、`save/load` 状态持久化。

**这个形态差异决定了 L2/L3 能不能落地**：Kalman / GARCH / 任意 IIR 滤波器都需要「逐 bar 更新内部状态、且状态依赖上一根 bar 的输出」——批式接口的 DAG 无法引用自身历史输出（ADR-028 附录 B 已逐条判死）。**要支持 L2/L3，必须引入流式策略接口**，这不是优化，是前提。

**差距 ②：无时钟抽象。** 回测的「现在」是数据驱动的（date-loop 的当前交易日），实盘的「现在」是墙钟。没有 `Clock` 接口，同一份策略代码无法在两种时间观下跑。nautilus 的 `clock_factory.rs` 明确：`Backtest ⇒ VirtualClock`，`Live|Sandbox ⇒ LiveClock`——这是回测-实盘同构的**第一替换点**。

**差距 ③：无内核装配，生命周期硬编码。** `cmd/analysis/setup.go` 831 行手工编排实例化顺序，`setup.go:771-831` 硬编码 4 阶段关停。nautilus 的 `NautilusKernel`（`crates/system/src/kernel.rs:97`）把 8 个组件声明为结构体字段（`cache / clock / portfolio / data_engine / risk_engine / exec_engine / order_emulator / trader`），装配与生命周期由内核统一管理。

### 2.3 nautilus 与 vnpy 的架构定性（源码取证，供 §3 取舍）

| 维度 | vnpy | nautilus_trader | 我们（现状） |
|---|---|---|---|
| 定位 | 单标的 CTA 全流程 | 多资产多 venue，研究到实盘 | AI 造策略 + 全市场横截面选股 |
| 回测驱动 | 同步 `for` 循环（`backtesting.py`，`EventEngine` 出现 0 次） | MessageBus + VirtualClock 单线程同步分发 | date-loop（`engine.go:669` `for i, date := range tradingDays`） |
| 实盘驱动 | EventEngine（`Queue`+`Thread`，真异步） | 同一内核，换 LiveClock + 真实 Client | 无实盘（`pkg/live` 零接线） |
| 策略形态 | `on_bar` 回调（`vnpy/alpha` 另有截面批式） | `on_bar` 流式 + Actor | 批式 `GenerateSignals` |
| 执行算法 | 无 | `ExecutionAlgorithm`（TWAP 与 Strategy 同构 trait） | 无 |
| 事件存储 | 无 | `crates/event_store`（先记录后分发） | 无 |
| 确定性机制 | 顺序执行 | thread_local + `Rc<RefCell>` 无 Send/Sync + VirtualClock + ts_event 排序 | 顺序执行 |

---

## 3. nautilus 抄什么、不抄什么

### 3.1 抄（7 件，全部源码取证）

| # | 模式 | nautilus 出处 | 我们的落点 |
|---|---|---|---|
| C1 | **Kernel 装配**：8 组件声明为内核字段，统一生命周期 | `crates/system/src/kernel.rs:97` | 新增 `pkg/kernel`，取代 setup.go 硬编码 |
| C2 | **可替换边界**：回测/实盘只换 Clock + Client，内核不变 | `crates/system/src/clock_factory.rs` | Clock / DataSource / Broker 三处替换点 |
| C3 | **消息命名注册表**：所有端点/主题名集中一处定义 | `crates/common/src/msgbus/switchboard.rs` | 新增 `pkg/kernel/topics.go` |
| C4 | **流式策略 trait**：`on_bar(&mut self)` 有状态回调 | `crates/trading/src/strategy/core.rs:100` | 新增 `strategy.BarHandler` 接口 |
| C5 | **执行算法与策略同构**：TWAP 是实现 `ExecutionAlgorithm` trait 的可插拔组件 | `crates/trading/src/algorithm/` | 新增 `pkg/execalgo`（TWAP/VWAP/拆单） |
| C6 | **事件存储先记录后分发**：`BusTap` 在消息派发给订阅者**之前**落库 | `crates/common/src/msgbus/`（`trait BusTap`）+ `crates/event_store/` | 新增 `pkg/eventstore`，`audit.message_log` |
| C7 | **有状态指标契约**：`Indicator` trait `&mut self`，`handle_bar/has_inputs/initialized/reset`，未实现的 handle 默认 panic（fail-loud） | `crates/indicators/src/indicator.rs` | L2 算子契约（`pkg/indicator`） |

### 3.2 不抄（3 件，都有硬理由）

| # | 不抄 | 理由 |
|---|---|---|
| N1 | **event-driven 日循环** | 我们的主场是横截面（`cs_rank` 要「同日全部 5900 票」）。事件流是「逐票」的，硬套等于把截面拆散再拼回去。回测内循环保持 date-loop。 |
| N2 | **MessageBus 并发分发** | nautilus 用 Rust 类型系统强制单线程（`thread_local!` + `Rc<RefCell>`，无 `Send`/`Sync`）。我们现有 `marketdata.EventBus` 是 **4 个 goroutine 抢同一 channel**——照搬进回测会立刻破坏确定性。进程内用**同步分发**（单线程语义），跨进程用 HTTP。EventBus 不接线（维持 ADR-027/030 结论）。 |
| N3 | **Rust 单进程内核** | nautilus 是一个进程内一个内核。我们是多服务（data/analysis/web 已拆分），内核落在 analysis 进程内，data-service 保留为独立数据源进程。不为了「像 nautilus」把已拆的服务并回去。 |

---

## 4. 目标架构：模块化内核

### 4.1 一句话形态

**一个内核（Kernel）装配 11 个模块，跑在 analysis 进程内；回测与实盘共享同一内核，只在三处替换边界换实现；AI 编排层（Hermes）不进内核，走 MCP 工具层操作内核。核心部件的抽象按实盘-ready 设计（D1），当前只实现 paper / 模拟。**

```
                        ┌─────────────────────────────────────────────┐
                        │              AI 编排层 (L3, Hermes)          │
                        │   经 MCP 工具层操作内核 —— 禁止直接读库       │
                        └──────────────────┬──────────────────────────┘
                                           │ MCP tools (21+)
   ┌───────────────────────────────────────┼───────────────────────────────────────┐
   │              KERNEL（装配 + 生命周期 + 消息注册表）                              │
   │                                                                               │
   │  ┌─────────┐  ┌──────────┐  ┌───────────┐  ┌────────┐  ┌──────────────────┐  │
   │  │  Clock  │  │ DataEngine│  │ Portfolio │  │  Risk  │  │   ExecEngine     │  │
   │  │ (替换点①)│  │          │  │           │  │ Engine │  │ (订单/成交/对账)  │  │
   │  └─────────┘  └──────────┘  └───────────┘  └────────┘  └──────────────────┘  │
   │  ┌──────────────────────────────────────────────────────────────────────┐   │
   │  │              Strategy Runtime（批式 + 流式 双模式）                     │   │
   │  │   SignalGenerator(L0/L1)  ·  BarHandler(L2/L3)  ·  ExecAlgorithm      │   │
   │  └──────────────────────────────────────────────────────────────────────┘   │
   │  ┌──────────────┐  ┌──────────────┐  ┌────────────────────────────────┐    │
   │  │  Indicators   │  │  EventStore  │  │  MsgBus（命名注册表+同步分发）  │    │
   │  │ (L2 有状态算子)│  │ (先记录后分发)│  │                                │    │
   │  └──────────────┘  └──────────────┘  └────────────────────────────────┘    │
   └───────────────────────────────────────────────────────────────────────────┘
        │ 替换点② DataSource          │ 替换点③ Broker
   ┌────┴─────────────┐         ┌────┴──────────────┐
   │ Backtest: PG快照  │         │ Backtest: 模拟撮合  │
   │ Live:     实时feed │         │ Live:     真实券商  │
   └──────────────────┘         └───────────────────┘
```

**三个替换边界**（回测/实盘同构的全部秘密）：

| 替换点 | 回测实现 | 实盘实现 |
|---|---|---|
| ① Clock | `VirtualClock`（数据驱动推进，确定性） | `LiveClock`（墙钟） |
| ② DataSource | `SnapshotSource`（物化边界后的 PG 快照，`BulkLoadOHLCV`） | `RealtimeFeed`（`pkg/live.DataFeed`，推送式） |
| ③ Broker | `SimulatedBroker`（模拟撮合 + 滑点 + 费用） | 真实券商 `Broker`（Connect/SubmitOrder） |

### 4.1.1 实盘-ready 的分寸（D1 拍板，必须守住）

D1 的本意是「**留能力，不留包袱**」。执行时最容易犯的错是把「实盘-ready」读成「把实盘复杂度全搬进来」。划一条线：

| 维度 | 要实盘-ready（现在就做，留冗余） | 实盘专属（现在不做，留作将来） |
|---|---|---|
| **接口抽象** | `Broker` / `DataFeed` / `Clock` / `ExecEngine` 的方法签名按真实券商设计（支持部分成交、撤单、订单状态机、断线重连语义） | — |
| **实现** | `SimulatedBroker`（含滑点 + 费用 + 市场冲击） / `MockTrader` | 真实券商 SDK 对接、FIX 协议 |
| **风控** | 订单前置风控的**挂点**（`CheckOrder` 在报送前调用） | 实盘级合规规则、监管报送 |
| **对账** | `Reconciliation` 接口 + paper 对账（broker 快照 vs 本地） | 与真实券商的日终对账文件解析 |
| **订单模型** | `domain.Order` 支持 `type/limit/stop`、`time_in_force`、部分成交填充 | 融资融券、期权、算法单直连 |

**一句话**：抽象的形状对着真实世界设计，但实例只造「模拟」那一个。这样将来接真实券商时，只换实现、不动内核——这正是 nautilus「research-to-live parity」的精髓，也是 D1 要的「冗余」。

### 4.2 内核生命周期（取代 setup.go 硬编码）

```
Kernel.Boot()
  ├─ 1.  EventStore.Init       (先开记录，否则后续消息无审计)
  ├─ 2.  Clock.Init            (决定时间观)
  ├─ 3.  DataEngine.Init       (建快照 / 连 feed)
  ├─ 4.  Portfolio.Init        (恢复持仓)
  ├─ 5.  RiskEngine.Init       (风控就位后 exec 才敢收单)
  ├─ 6.  ExecEngine.Init       (连 broker)
  ├─ 7.  StrategyRuntime.Init  (加载策略，恢复 L2/L3 状态)
  ├─ 8.  Indicators.Init       (L2 有状态算子就位)
  ├─ 9.  ExecAlgo.Init         (执行算法挂点)
  └─ 10. MsgBus.Start          (最后开分发，此前消息只记录不派发)

Kernel.Shutdown()  // 逆序，先停分发再落库
```

> **已冻结为代码契约**：本顺序即 `pkg/kernel.BootOrder`（K0 切片 1，10 项）。**11 模块中 kernel 自身是装配者、不被自己装配**，故被装配的是其余 10 个——这与本文 §5 矩阵的 11 模块不矛盾，是同一集合的两种视角。改顺序 = 改契约，须走变更评审；断言见 `TestBootOrderContract`。

**为什么这个顺序是契约**：EventStore 必须第一个起（否则启动期消息丢失），MsgBus 必须最后开（否则策略在状态未恢复完就收到 bar）。nautilus 的 Kernel 就是这么排的我们直接对齐。

---

## 5. 模块矩阵（四件套 × 11 模块）

> 「DB 归属」遵循 §7 数据归属铁律（禁止双写）：可重建数据唯一于 PG 分区，不可重建叙事唯一于 vault。

| 模块 | 职责（功能设计定义） | 接口 | DB 归属 | 数据流定义 |
|---|---|---|---|---|
| **kernel** | 装配 11 模块、管生命周期（Boot/Shutdown 顺序）、持有消息注册表 | `Kernel.Boot/Shutdown/Module(name)` | 无（内存装配） | 启动时单向装配，运行时不收消息 |
| **clock** | 提供「现在」；回测=数据驱动，实盘=墙钟 | `Clock.Now()/Advance(ts)/Mode()` | 无 | VirtualClock 被数据迭代器推进；LiveClock 自走 |
| **msgbus** | 模块间消息的**命名注册表 + 同步分发**；不并发 | `Publish(ctx,topic,msg)/Subscribe(topic,handler)`（同步；handler 返回 error，分发不中断、错误聚合上报） | 无（内存）；消息经 eventstore 落 `audit.message_log` | 单线程语义：handler 同步执行，按注册优先级；时钟构造期强制注入（`NewSyncBus(tap,clk)` / `NewLiveBus(tap)`），无墙钟兜底 |
| **eventstore** | 所有模块间消息**先落库后分发**（BusTap 语义）；回放与审计 | `Append(ctx,msg)/Replay(ctx,range)/Verify()` | `audit.message_log`（新） | 写：msgbus 派发前钩子；读：审计/回放/调试 |
| **data-engine** | 数据入口唯一闸口；回测建快照，实盘管订阅 | `Provider`（沿用）+ `Snapshot(range)/Subscribe(symbols)` | 读 `market.*`；写 `ingest.raw`（同步）；`factor_cache`（Redis） | 回测：`BulkLoadOHLCV` 一次性物化；实盘：feed→bar 聚合→发布 |
| **portfolio** | 持仓/现金/净值；成交后更新；快照持久化 | `ApplyFill(f)/Value()/Snapshot()` | `quant.portfolio_snapshot` / `quant.positions`（新） | 收 `exec.fill` 消息 → 更新 → 发 `portfolio.updated` |
| **risk-engine** | 订单前置风控（仓位/止损/制度）+ 盘后 regime | `RiskManager`（扩）+ `CheckOrder(order)→verdict` | `quant.risk_events`（新） | 收 `exec.order_intent` → 裁决 → 放行/拒绝 |
| **exec-engine** | 订单生命周期、撮合（回测）/报送（实盘）、对账 | `Broker`（沿用）+ `SubmitOrder/CancelOrder/Reconcile()` | `quant.orders` / `quant.fills` / `quant.recon_report`（新） | 收策略 order → 风控 → broker → 发 `exec.fill` |
| **strategy-runtime** | 加载策略、按模式（批/流）驱动、L2/L3 状态持久化 | `SignalGenerator`（现）+ `BarHandler`（新）+ `SaveState/LoadState` | `quant.strategies` / `quant.strategy_state`（新） | 批式：攒窗口→`GenerateSignals`；流式：逐 bar→`OnBar` |
| **indicators** | L2 有状态算子（Kalman/EWMA/IIR）+ 无状态因子库 | `Indicator.Update(bar)/Value()/Reset()/Warmup()`（fail-loud） | `quant.factor_cache` + Redis | 被策略调用；warmup 静态推导 |
| **exec-algo** | 执行算法（TWAP/VWAP/拆单），与策略同构 trait | `ExecAlgorithm.OnBar/OnFill/Schedule()` | 无（状态在内存+orders） | 收父订单 → 拆子订单 → 交给 exec-engine |

**AI 编排层（Hermes）不在矩阵里**——它不是内核模块，是内核的**操作员**，经 MCP 工具层（21+ 工具）调用内核能力。这条边界维持 ADR-023：编排者只有一个，且不直接摸数据。

### 5.1 模块边界契约（D3 拍板：独立 agent 并行开发的硬前提）

D3 要的「独立 coding agent 不被打扰地完成模块」，有一个**不成立就会翻车的前提**：**契约必须先冻结，再开并行**。接口没定死就并行，agent A 一改签名，agent B 的实现立刻作废——这不叫并行，叫互相拆台。

所以落地顺序是：**先把「模块边界契约」写死、冻结，再让多个 agent 各自拿一个模块去实现**。契约必须包含四类、缺一不可：

| 契约 | 内容 | 落在哪 | 为什么独立 agent 需要它 |
|---|---|---|---|
| **接口契约** | 每模块的 Go interface 签名（方法名/参数/返回/错误） | `pkg/<module>/interfaces.go` + `contracts/` | agent 只管实现这个 interface，不用看别人怎么调用 |
| **消息契约** | 所有 topic 名 + payload 结构（集中定义） | `pkg/kernel/topics.go`（switchboard 模式） | agent 发布/订阅的是「名字+结构」，不是「对方的包」 |
| **DB 契约** | 每模块的表 schema（DDL）+ 读写归属 | `contracts/schema.sql` + `migrations/` | agent 知道自己能写哪张表、哪张表只能读 |
| **测试契约** | 每模块必须过的接口合规测试 + 破坏验证 | `pkg/<module>/interfaces_compliance_test.go` | agent 自证「我做完了」，不依赖集成环境 |

**判断独立开发是否可行的一条硬标准**：一个 agent 拿着某个模块的这四份契约，**不需要读任何其他模块的实现、不需要问任何人**，就能把模块写完并通过它的测试契约。达不到这条，就是契约没写细——问题在契约，不在 agent。

> 已有基础可复用：`pkg/strategy/interfaces.go` 已经是「4 子接口 + 编译期合规检查（`var _ Interface = (*T)(nil)`）+ `interfaces_compliance_test.go`」的样板，`contracts/` 目录已存在。D3 是把这套样板推广到全部 11 个模块。

---

## 6. 四层表达力落地

### 6.1 四层 × 双模式矩阵

| 层 | 定义 | 载体 | 策略接口 | 状态 | DB | 现状 |
|---|---|---|---|---|---|---|
| **L0** 固定模板+参数 | 预置策略模板，填参数即用 | YAML/策略配置 | 批式 `SignalGenerator` | 无 | `quant.strategies` | ✅ 已有 |
| **L1** 表达式 DSL | 28 运算的声明式表达式 | `ExpressionStrategy` | 批式 `SignalGenerator` | 无（窗口纯函数 DAG） | `quant.strategies` | ✅ 已有（ADR-024） |
| **L2** 确定性有状态算子 | Kalman/EWMA/IIR/累积量——可枚举、warmup 可静态推导 | DSL 扩展算子 + `Indicator` trait | **流式 `BarHandler`**（新增） | 有，`SaveState/LoadState` | `quant.strategy_state`（新） | ❌ 缺接口与算子 |
| **L3** 通用代码/外部模型 | 图灵完备 | L3a: WASM 沙箱（wazero）· L3b: 外部模型信号注入 | **流式 `BarHandler`** | 有 | `quant.strategy_state` + 信号表 | ❌ 缺（ADR-029 轨道 B 待签） |

### 6.2 双模式策略接口（核心设计）

```go
// 批式（现有，保留）—— 服务 L0/L1 + 横截面
type SignalGenerator interface {
    GenerateSignals(ctx, bars map[string][]OHLCV, portfolio) ([]Signal, error)
    Weight(signal, portfolioValue) float64
}

// 流式（新增）—— 服务 L2/L3
type BarHandler interface {
    OnBar(ctx, bar Bar) error          // 逐 bar 回调，&mut self 语义
    Warmup() int                        // 声明需要多少根历史 bar（静态推导）
    SaveState() ([]byte, error)         // 状态持久化（对应 nautilus save/load）
    LoadState([]byte) error
}
```

**模式选择与同构桥**：

- 引擎启动时检测策略实现了哪个接口，自动选执行模式。
- **批式策略 → 流式引擎**：引擎按 `Warmup()` 攒够窗口，每逢 bar 到达调 `GenerateSignals`——批式策略不做改动就能上实盘。
- **流式策略 → 回测**：VirtualClock 驱动数据迭代器逐 bar 喂 `OnBar`，与实盘逐字同一段代码。
- **这就是回测-实盘同构**：同一策略对象，回测换 VirtualClock+SnapshotSource+SimulatedBroker，实盘换 LiveClock+RealtimeFeed+真实 Broker，**策略代码一行不动**。

### 6.3 L2 与 L3 的落点裁决（沿用 ADR-030 OBS-05 建议）

**能用 L2 表达的，不允许上 L3。**（**D2 已拍板采纳**） L2 算子（`ts_kalman`/`ts_ewma`/`ts_garch_vol`/`ts_rma`）是确定性、可枚举、warmup 可静态推导、可三路属性测试的——验证器能白盒校验。L3（WASM/外部模型）是图灵完备兜底，只用于 L2 表达不了的（如自定义状态机、非标准滤波）。这维持 ADR-024 的「自由度是负债」：表达力逐层放大，但每层都先问「上一层能不能做」。

### 6.4 L3b：外部模型信号注入（Actor 模式）

外部模型（如对未来 20 日收益的预测）不直接下单，而是产出**信号数据**注入内核，由策略消费。这对应 nautilus 的 `Actor.on_signal` / 我们的 GenePool 回注链路。模型在进程外（可用任意 Python/ML 框架），内核只认信号契约——**把「自由度」挡在内核之外**，内核内仍然确定、可审阅。

---

## 7. 数据流图

> 实线 = REST/同步调用；虚线 = 异步/事件/消息。消息一律先经 eventstore 落库（图中简化为一条「记录」注记）。

### 7.1 主数据流：回测（批式 + 流式双模式）

```
AI/Hermes ──MCP──► Kernel.RunBacktest(req)
                        │
                        ▼
              EventStore.Append(ctx, run.start)          ─ ─ ─ 先记录
                        │
                        ▼
              DataEngine.Snapshot(range)            ─ ─ ─ 物化边界
                        │ BulkLoadOHLCV (1 次/回测, REST)
                        ▼
              ┌─────────────────────┐
              │  VirtualClock 推进    │
              │  for date in dates:   │
              └─────────────────────┘
                   │           │
        ┌──────────┘           └──────────┐
        ▼ 批式路径                        ▼ 流式路径
  攒窗口→GenerateSignals            逐 bar→OnBar(bar)
        │                                 │
        └──────────┬──────────────────────┘
                   ▼
              signals / orders
                   │
                   ▼
              RiskEngine.CheckOrder      ─ ─ ─ 订单前置风控
                   │ verdict
                   ▼
              ExecEngine(SimulatedBroker)撮合
                   │ fill
                   ▼
              Portfolio.ApplyFill → snapshot
                   │
                   ▼
              EventStore.Append(ctx, run.done)  ─ ─ ─ 落 audit.message_log
                   │
                   ▼
              validation 六维证伪（被调用，不循环）
```

### 7.2 主数据流：实盘（paper / 真实）

```
RealtimeFeed ──推送──► DataEngine(bar聚合)
                            │ Bar 到达
                            ▼
                      EventStore.Append(ctx, bar)   ─ ─ ─ 先记录
                            │
                            ▼
                      StrategyRuntime.OnBar(bar)   （流式；批式由引擎攒窗口）
                            │ order intent
                            ▼
                      RiskEngine.CheckOrder
                            │ verdict
                            ▼
                      ExecEngine ──► Broker(真实/Mock)
                            │ fill 回报（异步）
                            ▼
                      Portfolio.ApplyFill
                            │
                            ▼
                      Reconciliation（定时对账，BrokerQuerier vs LocalSnapshotter）
                            │ 差异
                            ▼
                      AlertDispatcher → 若曦
```

---

## 8. User Case 数据流图（4 个）

### UC1：AI 实验员提交 L1 表达式策略跑回测（现状链路）

```
Hermes ──MCP:run_backtest──► Kernel
  │                            │ 1. ExpressionStrategy 注册（批式）
  │                            │ 2. DataEngine 物化快照
  │                            │ 3. VirtualClock 逐日推进
  │                            │ 4. 攒窗口→GenerateSignals（cs_rank 横截面）
  │                            │ 5. Risk→Exec(Sim)→Portfolio
  │                            │ 6. EventStore 全程落库
  │◄─── result(trades, metrics)─┘
  │
  ▼ validation 六维 → 概率 + 质疑清单
```

### UC2：L3a WASM 策略（Kalman 滤波）回测（新增链路）

```
Hermes ──MCP:run_backtest(wasm_strategy)──► Kernel
  │ 1. WASM 模块加载（wazero 沙箱，host API 受限：get_bar(t_offset≤0) / get_state / set_state）
  │ 2. DataEngine 物化快照
  │ 3. VirtualClock 推进 → 逐 bar 喂 BarHandler.OnBar
  │      └─ WASM 内 Kalman 状态逐 bar 更新（L2 表达不了的状态机才到这层）
  │ 4. 策略经 host API 读历史 bar（t_offset≤0 强制，防前视）
  │ 5. order intent → Risk → Exec(Sim) → Portfolio
  │ 6. SaveState 定期落 quant.strategy_state（断点可续跑）
  │◄─── result + state_checkpoint ─┘
```

### UC3：实盘 paper trading（新增链路，验证回测-实盘同构）

```
若曦 ──UI:启动paper──► Kernel.Boot(mode=live, broker=mock)
  │ 1. Clock=LiveClock, DataSource=RealtimeFeed, Broker=MockTrader
  │ 2. 加载与回测**同一策略对象**（代码一行不动）
  │ 3. feed 推送 → bar 聚合 → OnBar（与回测同一回调）
  │ 4. order → Risk → MockBroker 撮合 → fill
  │ 5. Portfolio 实时更新 → UI 推送
  │ 6. EventStore 落库 → 每日与「同策略同日回测」对账
  │      └─ 同构验证：paper 成交 vs 回测成交，偏差超阈 → AlertDispatcher 告警
```

### UC4：执行算法拆单（TWAP，新增链路）

```
Strategy ──父订单(买 10万股)──► ExecEngine
                                  │ 路由到 TWAPAlgorithm（与策略同构 trait）
                                  ▼
                          TWAP.OnBar: 按时间片拆成 N 笔子订单
                                  │ 逐片子订单
                                  ▼
                          RiskEngine.CheckOrder（每片都过风控）
                                  │
                                  ▼
                          Broker 报送 → fill 回报 → TWAP.OnFill 更新进度
                                  │
                                  ▼
                          全部成交 → Portfolio.ApplyFill
```

---

## 9. 与现有 ADR 的落点关系

| 本文设计 | 消费/修订的 ADR | 关系 |
|---|---|---|
| Kernel 装配 + 模块矩阵 | ADR-027（模块化拆服务） | **落点**：ADR-027 拆到「服务边界」，本文落到「进程内模块边界」——先进程内核模块化，再谈拆服务 |
| 双模式策略接口 + L2 算子 | ADR-028（表达力分层扩展） | **落点**：L2 有状态算子走流式 `BarHandler` + `Indicator` trait，实现 ADR-028 的 `state` 字段预留 |
| L3a WASM + L3b 信号注入 | ADR-029（AI 层 2026 对齐） | **落点**：轨道 B 的 WASM 主路径；L3b 补外部模型注入 |
| EventStore / 消息注册表 / Clock | ADR-030（OBS-02/03/04/15） | **关闭**：这四条 OBS 在本文收敛为 kernel/msgbus/eventstore/clock 四模块 |
| 回测-实盘同构 | ADR-023（AI 实验员实验室） | **补条款（D1 已拍板）**：ADR-023 的「不做实盘」**不推翻**，补一条「核心部件按实盘-ready 设计；paper trading 是同构验证手段，真实券商对接与合规报送留作将来」 |

---

## 10. 分期路线（D1–D5 已拍板后）

| 期 | 内容 | 前置 | 产出 |
|---|---|---|---|
| **P0 契约冻结**（D3 硬前提） | 把 11 个模块的四类契约写死：接口签名（`pkg/<module>/interfaces.go`）+ 消息名（`pkg/kernel/topics.go`）+ DB schema（`contracts/schema.sql`）+ 测试契约 | 无 | **冻结的契约集**，作为后续所有并行开发的「宪法」；此后改契约要走变更评审 |
| **P1 内核骨架** | kernel + clock + msgbus(命名注册表+同步分发) + eventstore | P0 | setup.go 硬编码被 Kernel.Boot 取代；`audit.message_log` 落库（D4） |
| **P2 流式接口** | strategy-runtime 加 `BarHandler` + VirtualClock 驱动 + 同构桥 | P1 | L2/L3 的前提就绪；批式策略可无改动上 paper |
| **P3 L2 算子** | indicators 模块 + `ts_kalman/ts_ewma/ts_rma` + warmup 静态推导 | P2 | L2 落地（表达力到第 2 层，D2） |
| **P4 执行算法 + 市场冲击**（D5 已拍板做） | exec-algo 模块 + TWAP/VWAP；**撮合引擎加市场冲击模型**（滑点随订单量占盘口/成交量比例增大，大单分层成交） | P1 | 模拟真实大单：拆单 + 冲击成本，贴近真实成交 |
| **P5 实盘 paper**（D1：不接真实券商） | DataSource=RealtimeFeed + Broker=Mock + 同构对账；核心抽象按 §4.1.1 实盘-ready | P2 | UC3 跑通，回测-实盘同构可验证 |
| **P6 L3a WASM** | wazero 沙箱 + host API（get_bar 防前视 + state） | P2 | L3a 落地（表达力到第 3 层，ADR-029 轨道 B） |
| **P7 L3b 信号注入** | 外部模型信号表 + Actor 消费契约 | P2 | 机器学习预测→信号→内核 |

**依赖主线**：**P0（契约冻结）→ P1（内核）→ P2（流式）→ {P3, P5, P6, P7}**；P4 独立于主线（只依赖 P1 的 exec-engine 挂点）。

**D3 的并行红利从 P1 之后开始释放**：契约冻结后，P3 / P4 / P5 / P6 / P7 这五个模块可以**各自交给一个独立 coding agent 并行开发**——它们只依赖 P2 冻结的接口，互不依赖彼此。这正是 D3 那句「不被打扰地完成模块」的兑现方式。

---

## 11. 决策记录（2026-10-08 若曦拍板）与我的评估

**总体结论：五条决定全部合理、互相自洽，无重大错误。** 仅 D1 需守分寸（§4.1.1）、D3 需补机制（§5.1 契约先行）。

| # | 决策点 | 若曦的决定 | 我的评估 | 落实处 |
|---|---|---|---|---|
| D1 | 实盘能力 | **保留所有核心部件将来做实盘的能力**；核心部件本着做实盘去设计、留冗余；实盘专属部件不做，留作将来 | ✅ **最成熟的一条**。没推翻「不做实盘」，而是把前提升级为「实盘-ready 但不实盘」——正是 nautilus 的 research-to-live parity。**唯一风险是过度设计**（把真实券商复杂度搬进来），已用 §4.1.1 的「抽象实盘-ready / 实现只 paper」划线 | §0 判断4、§4.1.1、§9 ADR-023 补条款、P5 |
| D2 | 表达力落点 | **采纳**：能用 L2 就不用 L3 | ✅ 采纳。维持 ADR-024「自由度是负债」；L2 可枚举可白盒校验，L3 仅兜底 | §6.3 |
| D3 | 模块化宗旨 | **采纳**先内核模块化；**新增宗旨**：模块要有独立定义，以便独立 coding agent 不被打扰地完成 | ✅ **好洞察**，把模块化从「代码整洁」升级为「并行开发协作协议」。**硬前提是契约先行冻结**——接口/消息/DB/测试四类契约不定死就并行 = 互相拆台。已补 §5.1 + 路线加 P0 | §5.1、P0、§10 并行红利 |
| D4 | EventStore 成本 | **采纳建议**：全量落 `audit.message_log`，量大再加 retention | ✅ 采纳。单人自托管量级可承受；retention 机制留作将来（可后抄 `crates/event_store/retention.rs`） | P1 |
| D5 | 执行算法 | **按「模拟真实大单」做 P4** | ✅ 采纳。**补一句**：模拟真实大单不只是 TWAP 拆单，撮合引擎还要加**市场冲击模型**（滑点随量增大、大单分层成交），否则「真实」只真实在拆单、不真实在成本 | P4 |

---

## 附录 A：nautilus 源码取证锚点（本文论断的依据）

| 论断 | 文件 |
|---|---|
| Kernel 8 组件字段 | `crates/system/src/kernel.rs:97`（cache/clock/portfolio/data_engine/risk_engine/exec_engine/order_emulator/trader） |
| 回测/实盘时钟替换 | `crates/system/src/clock_factory.rs`（`Backtest⇒VirtualClock`, `Live\|Sandbox⇒LiveClock`） |
| 消息命名注册表 | `crates/common/src/msgbus/switchboard.rs`（端点/主题集中定义，queue_execute vs execute 双入口） |
| MessageBus 单线程强制 | `crates/common/src/msgbus/`（`thread_local!` + `Rc<RefCell>`，无 Send/Sync） |
| 流式策略 trait | `crates/trading/src/strategy/core.rs:100`（`StrategyNative`，`order_factory`/`portfolio` 注入） |
| 执行算法同构 | `crates/trading/src/algorithm/`（`ExecutionAlgorithm`, `TwapAlgorithm`） |
| 事件存储先记录后分发 | `crates/event_store/`（backend/capture/reader/replay/snapshot/verifier）+ `trait BusTap` |
| 有状态指标契约 | `crates/indicators/src/indicator.rs`（`&mut self`, handle_* 默认 panic = fail-loud） |
| vnpy 回测同步循环 | `vnpy_ctastrategy/backtesting.py`（`EventEngine` 出现 0 次；`new_bar` 先撮合后回调防前视） |

## 附录 B：我们现状取证锚点

| 论断 | 文件 |
|---|---|
| 批式策略接口 | `pkg/strategy/interfaces.go:75`（`SignalGenerator.GenerateSignals(bars map)`） |
| 回测 date-loop | `pkg/backtest/engine.go:669`（`for i, date := range tradingDays`） |
| 实盘抽象已存在 | `pkg/live/engine.go`（`Broker`/`DataFeed`）、`pkg/live/reconciliation/`（4 接口） |
| 数据抽象已存在 | `pkg/marketdata/provider.go`（`Provider.BulkLoadOHLCV`） |
| 风控抽象已存在 | `pkg/domain/types.go`（`RiskManager` 3 方法） |
| 表达式策略 | `pkg/strategy/expression/strategy.go:80`（`ExpressionStrategy`） |
| EventBus 零接线且并发 | `pkg/marketdata/eventbus.go`（4 `dispatchLoop` worker，仅 test 调用） |
| 60+ interface | `grep -rn "type \w+ interface {" pkg/` 实测 |
