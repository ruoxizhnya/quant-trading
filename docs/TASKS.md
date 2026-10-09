---
status: evergreen
last-verified: 2026-10-08
verified-by: 模块化内核任务重构（2026-10-08）—— 旧 TASKS（2427 行，基本已完成）归档至 archive/TASKS-2026-10-08-archived.md；本文件按 AGENTS.md §8.5 任务定义规范重新生成
---

# TASKS — 未完成项

> **只放未完成项**，完成即移入 `archive/`（旧任务文档：[archive/TASKS-2026-10-08-archived.md](archive/TASKS-2026-10-08-archived.md)，含 Sprint 1–8 与 AUD-01~62 记录）。
> 任务定义遵循 [AGENTS.md §8.5 任务定义规范](../AGENTS.md)——**每个 task 必须让独立 coding agent 不被打扰地完成并自证**（六字段 + 派工三律）。
>
> 状态标记：`⬜ 待做` / `🔶 进行中` / `⛔ 阻塞（有未解除前置）` / `✅ 完成（附日期）`
> 派工三律（每个 task 都适用，见 §8.5.3）：**一 task 一 commit · 护栏必破坏验证 · 要正证据不靠失败**。

---

## 一、主线：模块化内核（目标架构）

> 详细蓝图：[design/kernel/target-architecture-modular-kernel.md](design/kernel/target-architecture-modular-kernel.md)（D1–D5 已拍板，2026-10-08）。
> **依赖主线：K0 → K1 → K2 → {K3, K5, K6, K7}；K4 独立（只依赖 K1 的 exec-engine 挂点）。**
> K0 是所有并行开发的**硬前提**（契约先行冻结，见蓝图 §5.1）。K0 完成后，K3/K4/K5/K6/K7 可各自交一个独立 agent 并行开发。

### K0 · 契约冻结（P0）✅ 完成（2026-10-08，两切片均独立 agent 派工 + 验收方独立复核）

> **切片 1 ✅**：kernel / clock / msgbus / eventstore 四包契约 + topics 命名注册表（9 topic）+ `audit.message_log` DDL + 合规测试（10 文件 905 行）。
> **切片 2 ✅**：data-engine / portfolio / risk-engine / exec-engine / strategy-runtime / indicators / exec-algo 七模块契约 + 7 个新 topic + `quant.*` 7 张表 DDL + 合规测试（14 新增 / 3 修改，30 条合规用例）。
> 关键裁决（已写入代码注释）：BootOrder 为 **10 元素**（kernel 不自装配）；topics.go 落位 `pkg/msgbus`（防 import 环）；`OrderManager` 接口改名 **`OrderLifecycle`**（与现有 struct 撞名）；`Fill` 新定义不复用 `domain.Trade`（费用因果倒置 + 无 OrderID）；`domain.Bar` 不存在→用 `domain.OHLCV`；`OrderIntent` 单一事实源在 `pkg/risk`，`pkg/live` 走别名。
> **K0 已完成，K1+ 可开工**（K1 内核骨架直接前置）。

- **① 目标**：把 11 个内核模块的四类契约写死并冻结，作为后续所有并行开发的「宪法」。
- **② 上下文**：蓝图 §5 模块矩阵 + §5.1 模块边界契约（`docs/design/kernel/`）；现有接口样板 `pkg/strategy/interfaces.go`（4 子接口 + 编译期合规检查 + `interfaces_compliance_test.go`）、`pkg/marketdata/provider.go`、`pkg/live/engine.go`（Broker/DataFeed）、`pkg/domain/types.go`（RiskManager）；契约落点 `contracts/`（已存在）。
- **③ 要求**：为 11 个模块（kernel / clock / msgbus / eventstore / data-engine / portfolio / risk-engine / exec-engine / strategy-runtime / indicators / exec-algo）各产出四类契约——**接口契约** `pkg/<module>/interfaces.go`（签名 + 编译期合规 `var _ Iface = (*T)(nil)`）；**消息契约** `pkg/kernel/topics.go`（topic 名 + payload，集中定义，switchboard 模式）；**DB 契约** `contracts/schema.sql`（表 DDL + 读写归属，新表见 SPEC §模块化内核新表 8 张）；**测试契约** `pkg/<module>/interfaces_compliance_test.go`（合规 + 破坏验证）。
- **④ 约束与非目标**：**只写契约（接口/类型/DDL/测试签名），不写实现逻辑**（实现属 K1+）；不改任何现有模块实现；冻结后改契约须在 task 显式登记变更评审。
- **⑤ 验收**：`go build ./...` 通过；每模块合规测试通过；**独立可运行判据自证**（任取一模块，其四类契约能让不了解其他模块的 agent 说清「实现什么接口 / 写哪张表 / 发收什么消息 / 过什么测试」）；破坏验证——删某模块一个接口方法 → 其合规测试变红。
- **⑥ 边界**：只做 11 个模块 × 四类契约定义，不做任何实现。

### K1 · 内核骨架（P1）🔶 切片 1+2 完成，接管 setup.go 待做（前置：K0）

> **切片 1 ✅（`b33afd7`）**：clock / msgbus / eventstore 三模块实现（63 测试含签名变更）。
> **切片 2 ✅（2026-10-08）**：`StandardKernel` 真实现（Boot 按 BootOrder / 失败逆序回滚 / Shutdown 逆序幂等 + 关停消息先发再停 / Module 检索）+ 适配包装 `pkg/kernel/adapters.go`（Clock/MsgBus/EventStore 三真模块）+ **`main.go` 影子启动**（3 真模块 + 7 占位，**现有装配一行未改**）+ 真环境验证（服务起 + `/health` 200 + `audit.message_log` 实测 kernel.boot/shutdown 落库）。
> **待做（高风险，单独切片）**：把现有组件（backtest.Engine / risk / live / alert / store …）包装成 Module，让 Kernel 真正接管 `setup.go` 的装配顺序；接管后影子启动的「失败不阻断」改为 fail-fast。
> 关键裁决（已写入代码注释）：适配包装放 `pkg/kernel` 而非 cmd 层；**Shutdown 先发 kernel.shutdown 再逆序停**（否则关停事件落不了库）；影子关停放 `gracefulShutdown` **之前**（pool 要先活着才能落库）；服务级内核用 LiveClock（VirtualClock 是 per-回测-run 的）。

- **① 目标**：kernel + clock + msgbus（命名注册表 + 同步分发）+ eventstore 落地，`cmd/analysis/setup.go` 的 831 行硬编码装配被 `Kernel.Boot/Shutdown` 取代，`audit.message_log` 落库（D4）。
- **② 上下文**：蓝图 §4.1 形态 + §4.2 生命周期（Boot/Shutdown 顺序契约）+ §5 矩阵；`cmd/analysis/setup.go`（当前硬编码）；K0 冻结的契约。
- **③ 要求**：`pkg/kernel`（装配 + 生命周期，EventStore 先起 / MsgBus 最后开）；`pkg/clock`（`Clock` 接口 + `VirtualClock`/`LiveClock`）；`pkg/kernel/topics.go` 同步分发（**不并发**，单线程语义）；`pkg/eventstore`（消息**先记录后分发**，BusTap 语义，落 `audit.message_log`）。
- **④ 约束与非目标**：MsgBus **不并发**（现有 `marketdata.EventBus` 是 4 goroutine 抢 channel，**不接线、不照搬**，见 ADR-030 OBS-04）；真实券商对接不做（D1）。
- **⑤ 验收**：`Kernel.Boot` 装配顺序符合契约（EventStore 第一个、MsgBus 最后）；关停逆序；`audit.message_log` 有启动期消息；破坏验证——打乱 Boot 顺序 → 对应测试变红。
- **⑥ 边界**：只做 4 个模块的骨架 + 装配替换，不动策略/回测逻辑。

### K2 · 流式接口（P2）✅ 完成（2026-10-08，三切片）（前置：K1）

> **切片 1 ✅（`532af55`）**：流式运行时基座落地 `pkg/strategy/runtime.go` —— `StreamRunner`（Clock 逐 bar 驱动 BarHandler，时间倒退立即停）+ `BatchAdapter`（批式→流式攒窗口桥：每 symbol 滚动窗口 + **横截面对齐就绪门**）+ 三路一致性落测（Batch ≡ Step ≡ Step-from-persisted）+ `LoadState` 原子提交 fail-loud。
> **切片 2 ✅（`a43c011`）**：`BarHandler` 加 **`Signals() []Signal`**（取走即清空——OnBar 无返回、信号无处可出是切片 1 暴露的契约缺口，走 K0-P2-3 同款变更流程）；引擎 `getSignalsFromLocalStrategy` **双模式接入**（检测 BarHandler → 流式分支：喂序按 symbol 字典序 + 只喂当日 bar（停牌不喂陈旧 bar）+ 取 Signals；批式路径零改动，**机制是类型系统保证**——`strategy.Strategy` 不嵌入 BarHandler，断言恒 false）；**双模式等价性正证据**：同一回测请求批式 vs 流式各产 127 条信号逐条一致。
> **切片 3 ✅（2026-10-08）**：`quant.strategy_state` 落库接线 —— `pkg/strategy/state_store.go`（`StateStore` + `PGStateStore`，UPSERT 只保留最新，幂等 `EnsureSchema`，Save 拒绝空 state/零值 ts）+ `pkg/strategy/checkpoint.go`（`RunCheckpointed`：周期/终局/异常退出保存 + 续跑恢复）。**核心裁决：保存点必须落在日期边界**——否则续跑「跳过 `Date <= 存储 ts` 前缀」会误跳同日期块未处理 bar（多 symbol 平铺流下静默丢 bar）；边界对齐让跳过规则精确成立。**续跑等价性正证据**：故障点 f=10/11/12 三点覆盖，库内 ts=最后边界、续跑处理序列精确 `bars[f:]`、终态逐字节一致。另一裁决：contracts「历史版本按 created_at 取」与 PK/upsert 矛盾 → 以 PK/upsert 为准。`Run` 委托 `RunCheckpointed` 零值 Checkpoint（单一驱动体，防两条路径确定性分叉）。
> **paper 侧接线归 K5**（本任务不含）。

- **① 目标**：strategy-runtime 加流式 `BarHandler` + VirtualClock 驱动 + 同构桥，使批式策略（L0/L1）无改动可上 paper、流式策略（L2/L3）可回测。
- **② 上下文**：蓝图 §6.2 双模式接口；SPEC §Streaming Strategy Interface（`docs/SPEC.md`）；`pkg/strategy/interfaces.go`（现有批式 SignalGenerator）；`pkg/backtest/engine.go:669`（date-loop）。
- **③ 要求**：`BarHandler` 接口（`OnBar`/`Warmup`/`SaveState`/`LoadState`）；引擎按策略实现接口自动选执行模式；批式→流式桥（引擎按 Warmup 攒窗口调 `GenerateSignals`）；流式→回测（VirtualClock 逐 bar 喂 `OnBar`）；`quant.strategy_state` 状态持久化。
- **④ 约束与非目标**：批式 `SignalGenerator` **保留不动**（向后兼容）；不改横截面 `cs_rank` 路径（date-loop 保留）。
- **⑤ 验收**：同一批式策略对象在回测（VirtualClock+快照）与 paper（LiveClock+feed）跑，**代码一行不动**、信号一致；流式策略 `SaveState`/`LoadState` 断点续跑结果一致；破坏验证——`get_bar(t_offset>0)` → trap。
- **⑥ 边界**：只做策略运行时的双模式与同构桥，不做具体 L2 算子（K3）或 WASM（K6）。

### K3 · L2 算子（P3）✅ 完成（2026-10-09，两切片）（前置：K2）

> **切片 1 ✅（2026-10-08）**：L2 算子核心落地 `pkg/indicator/` —— **契约变更**（走 K0-P2-3 同款流程）：`Indicator.Update(bar)` → **`Update(x float64)`**（对齐 ADR-028 §7 的 `Step(x float64)`；bar 级抽取归调用方，「RMA over TrueRange」在旧签名下无法表达）+ 新增 **`SaveState`/`LoadState`**（ADR-028 §7 原文：「接口不预留状态序列化，后面补不进去」）。三算子：`RMA`（Wilder，init=前 N 根 SMA）/ `EWMA`（init=首值，warmup=`ceil(ln(1e-6)/ln(1-α))`）/ `Kalman`（标量线性局部水平模型，p0=r）＋ `RMABatch`/`EWMABatch`/`KalmanBatch` **双实现对偶（共享同一递推核）** ＋ `TrueRange` 助手。**33 顶层用例**：手算 fixture（抓公式错）＋ 三路一致性属性测试（抓路径分裂，容差 1e-12）＋ warmup 边界＋原子性 fail-loud；**ATR 验收**：20 根含跳空 bar，`RMABatch(tr,14)` 与手算 Wilder ATR 逐点一致（容差 1e-12）。
> **ADR-028 §4 订正记录**：`ts_ewma` 样例数字与公式矛盾（α=0.3「约 20」实算 39；α=0.05「约 60」实算 269）——以公式 + tol=1e-6 为准，ADR 内已加订正注记。
> **切片 2 ✅（2026-10-09，与 OBS-06 合并做）**：三算子**进 DSL** —— 新增 `pkg/expression/registry.go`（2026-10-09 从 `pkg/ai/expression` 归位 core）作**算子名字的单一事实源**（31 个算子：13 ts + 4 cs + 6 一元 + 8 二元，声明 + 求值绑定在一处），`ast.go` 的四个硬编码列表与 evaluator 的三处 switch（含 `applyCrossSectionalOp` 的 `default: return values` 静默直通）全部改为查表；`ts_rma/ts_ewma/ts_kalman` 可解析、可求值（**复用 `pkg/indicator` 的 Batch**：每 symbol 一次算完整条再取下标）、被白名单认可、有 `Spec()`；新增 `DeriveWarmup`/`DeriveLookback`（ADR-028 §8：并行取 max、串行累加；任一 `State=true` ⇒ infinite），对齐附录 A（19 / max(19,14)=19 / 串行 18）。**契约变更**：`indicator.OperatorSpec.Init` `float64 → string`（受控词表），理由同 `Indicator.Update` 那次改型。护栏：注册表自洽测试 + **防漂移 AST 护栏**（算子名字面量只许出现在 `registry.go`）。

- **① 目标**：indicators 模块落地 L2 确定性有状态算子（`ts_kalman`/`ts_ewma`/`ts_rma`），warmup 可静态推导（D2：能用 L2 不上 L3）。
- **② 上下文**：蓝图 §6.3（L2 优先）；ADR-028 §2（L2 递推算子）+ §4 算子声明契约（含 `state` 字段）+ §8 warmup 推导；K2 的 `BarHandler`。
- **③ 要求**：`Indicator` trait 契约（`Update(bar)`/`Value()`/`Reset()`/`Warmup()`，未实现 fail-loud）；`ts_kalman`/`ts_ewma`/`ts_rma` 实现；warmup 静态推导；三路一致性（`Batch ≡ Step ≡ Step-from-persisted`，ADR-028 §7）。
- **④ 约束与非目标**：算子集**有限可枚举**（不允许 LLM 动态新增）；只用 L2，不提前引 WASM（K6）。
- **⑤ 验收**：`ts_rma(tr,14)` 与手算 Wilder ATR 逐点一致；三路一致性测试通过；warmup 推导与实际消耗 bar 数一致；破坏验证——改递推系数 → 属性测试变红。
- **⑥ 边界**：只做 L2 算子与 warmup 推导，不做 WASM / 外部模型。

### K4 · 执行算法 + 市场冲击（P4）✅ 完成（2026-10-09）（前置：K1，独立）

> **已完成**：`pkg/execalgo/{schedule,twap,vwap}.go` 落地 K0 冻结的 `ExecAlgorithm`（替换 stub）——`Schedule` 拆单（**Σ 子单量精确等于父单**，余数并入末片；`SubmitAt` 严格等差；子单号是 `(父单号, 片序号)` 的**纯函数**⇒幂等）、`OnBar` 到点产出 + `DueChildOrders` **取走即清空**（防重复报送，对齐 `BarHandler.Signals` 语义）、`OnFill` 进度累计；VWAP 按成交量 profile 加权，profile 退化（空/全 0/长度不符/负值）→ 等分并置 `Degraded` 标记。
> **冲击接入**：`pkg/backtest/marketimpact` 的平方根模型此前**全仓零引用（死代码，与 `EventBus` 同类现象）**，现接入 `BacktestExecutionService` 的 `"impact"` 分支（滑点随 `orderQty/ADV` 增大；ADV 用当根 `quote.Volume` 代理；`Volume<=0` → 退化 fixed，不把「无数据」误读成「零成本」）。`ExecutionConfig` 加可选 `ImpactSigma`/`ImpactLiquidityFactor`。
> **默认行为零变化**：engine 默认仍 `"fixed"`、默认滑点率不变，`fixed/variable/none` 分支逐字未动（回归腿钉住）。
> **正证据**：TWAP `1000/7 → [143×6,142]` 且 `Σ=1000`、`SubmitAt` 相邻间隔全等；VWAP `profile[1,2,3,4] → 占比 0.1/0.2/0.3/0.4`；**大单滑点 0.0632 > 小单 0.0200（10:1 单量，比值 3.1623 = √10）**。
> **审查加固（验收方做）**：子单 ID 幂等用例原本会被「时间戳派生 ID」骗过 —— Windows `time.Now()` 毫秒级粒度下背靠背两次调用取同一值 ⇒ **假绿（实测 PASS）**；已在 TWAP/VWAP 幂等用例中插 20ms 延迟，重放同一破坏**变红**（灵敏度平台无关）。
> **待裁决（只登记）**：是否把 `"impact"` 设为**默认**滑点模型（会改变所有回测数值）。
> **未做（后续）**：把 exec-algo 接进下单主路径（引擎/实盘按算法拆单 → 归 K5）；真实券商算法单；VWAP 的成交量预测（现只用给定 profile）。

- **① 目标**：exec-algo 模块落地 TWAP/VWAP 执行算法（与策略同构 trait），撮合引擎加市场冲击模型（D5：模拟真实大单）。
- **② 上下文**：蓝图 UC4（执行算法拆单）；nautilus `crates/trading/src/algorithm/`（TWAP 同构）；K1 的 exec-engine 挂点。
- **③ 要求**：`ExecAlgorithm` 接口（`OnBar`/`OnFill`/`Schedule()`）；TWAP/VWAP 实现（父订单拆子订单，按时间片）；撮合市场冲击模型（滑点随订单量占盘口/成交量比例增大，大单分层成交）。
- **④ 约束与非目标**：执行算法与策略同构但**不取代**策略接口；真实券商算法单直连不做（D1）。
- **⑤ 验收**：TWAP 拆单后子订单时间分布均匀；市场冲击下大单滑点 > 小单滑点；破坏验证——去掉冲击模型 → 大小单滑点相同（反证）。
- **⑥ 边界**：只做执行算法与冲击模型，不做策略、不做真实券商。

### K5 · 实盘 paper（P5）切片 1 ✅ / 切片 2 ⬜（前置：K2）

> **切片 1（本步完成，2026-10-09）**：读法 A 的 paper 回放链路 —— 回放与回测**同一批**日线 bar（不造 tick→bar 假聚合层），用**同一段代码**的执行成本核撮合。若曦拍板：paper 与回测必须吃同一批输入，差异才只可能来自执行机制 ⇒ 对账才可归因。
> **切片 2（待做）**：同构对账比对 + 超阈告警（D1）。

- **① 目标**：DataSource=RealtimeFeed + Broker=Mock 的 paper trading 跑通，并与同策略同日回测做同构对账（D1）。
- **② 上下文**：蓝图 §4.1.1 实盘-ready 分寸 + UC3；`pkg/live/`（Broker/MockTrader/OrderManager/reconciliation）；`live-trading.md`；K2 的同构桥。
- **③ 要求**：RealtimeFeed 接入 + bar 聚合；Mock 撮合（滑点+费用+冲击）；每日 paper 成交 vs 同策略同日回测成交对账，偏差超阈 → AlertDispatcher 告警；`pkg/live/reconciliation/` 对账（BrokerQuerier vs LocalSnapshotter）。
- **④ 约束与非目标**：**只做 paper，不接真实券商**（D1）；核心抽象按实盘-ready 设计留冗余，实现只 paper。
- **⑤ 验收**：同一策略 paper 与回测信号/成交一致（容差内）；对账差异超阈触发告警；破坏验证——paper 与回测用不同时钟 → 对账报差异。
- **⑥ 边界**：只做 paper trading 与同构对账，不做真实券商、不做合规报送。

**切片 1 交付明细（2026-10-09）**：
- `pkg/backtest/marketimpact` → `pkg/marketimpact`（提到顶层，与 `pkg/fees` 平级 —— 共享成本模型归位）
- 新增 `pkg/execution`（共享执行成本核 `CostModel`：fixed/variable/impact/none 四种滑点 + 佣金，**回测与 paper 走同一段代码**）
- 新增 `pkg/live/bar_replay_feed.go`（`BarReplayFeed`：按「交易日升序 × symbol 字典序」回放 bar 包成 Quote，同步 pull、`VirtualClock` 推进、时间倒退 fail-loud）
- 新增 `pkg/live/paper_session.go`（`PaperSession`：日循环编排，窗口 `Date<=d` 防前视，产出确定性 `PaperFill`）
- `MockTrader` / `SimulatedBroker` 换掉伪随机滑点（`order.ID[0]%10`），走共享成本核；`OrderResult` 加 `FillPrice`/`Fee` 字段
- 测试：回放确定性 / 时间倒退护栏 / 防前视（批式+流式探针）/ 成本同构（fixed+impact，含独立解析锚点）/ 真库只读冒烟

### K6 · L3a WASM（P6）⬜（前置：K2）

- **① 目标**：wazero 沙箱落地，host API 受限（`get_bar` 防前视 + `get_state`/`set_state`），仅在 L2 无法表达时启用（D2）。
- **② 上下文**：蓝图 UC2；ADR-029 §3（WASM host API 契约）+ ADR-007 Phase 3（2026-10-08 对齐：轨道 B=L3a）；ADR-024（不做 plugin.Open）；`internal/sandbox/wasm/sandbox.go`（骨架，现用 InProcessRuntime fallback）。
- **③ 要求**：`wazero` 入 go.mod，`InProcessRuntime` 退役；host API 白名单（`get_bar(t_offset≤0)` 越界 trap、`get_state`/`set_state`）；协议从一次性写全部 bars 改逐 bar `on_bar(t)`（堵前视漏洞，ADR-007 2026-10-06 注记）。
- **④ 约束与非目标**：**仅 L2 无法表达时启用**（D2）；沙箱无文件/网络/时钟/随机源/goroutine。
- **⑤ 验收**：`get_bar(t_offset>0)` → trap（防前视物理不可能）；WASM 策略断点 `SaveState`/`LoadState` 续跑一致；破坏验证——去掉 t_offset 检查 → 能读到未来数据（反证）。
- **⑥ 边界**：只做 WASM 沙箱与 host API，不做 L2 算子（K3）、不做 L3b（K7）。

### K7 · L3b 信号注入（P7）⬜（前置：K2）

- **① 目标**：外部模型（进程外，任意 Python/ML 框架）产出信号注入内核，由策略消费（Actor 模式）。
- **② 上下文**：蓝图 §6.4；nautilus `Actor.on_signal`；K2 的策略运行时。
- **③ 要求**：外部模型信号表（信号契约：符号/方向/强度/as_of）；Actor 消费契约（策略 `on_signal` 读信号）；信号落库可追溯。
- **④ 约束与非目标**：模型在进程外（自由度挡在内核外）；内核只认信号契约，仍确定可审阅。
- **⑤ 验收**：外部模型信号注入 → 策略按信号下单；信号可追溯（content_hash 坐标）；破坏验证——注入未来日期的信号 → 拒绝。
- **⑥ 边界**：只做信号注入与消费契约，不做模型训练、不做 L3a（K6）。

---

## 二、最高优先级独立工单

### OBS-01 · 空票池假成功必须 fail-loud ✅ 完成（2026-10-09，两切片）

> **切片 1 ✅（2026-10-09）**：裁定 + 报告层 + 自动化层落地 —— `pkg/backtest/contracts/validity.go`（纯函数 `CheckValidity`，4 个稳定 Code：`empty_universe` / `zero_trades` / `zero_start_date` / `garbage_metric`，垃圾阈值 `|x|>1e12` 网住 MaxFloat64/NaN/±Inf）；`domain.BacktestResult` 增 `UniverseMaxSize` + `InvalidReasons`（引擎算一次、下游只读不重算）；引擎**正常路径行为不变**、无效时日志不再无条件报 completed 且同步响应 `Status="invalid"`；`pipeline`×2 / `job` 不再宣告 successfully（job 走 failed + error_msg）；**`loop` 的 `a.OK` 加裁定前置 ⇒ 无效运行按 failed 计、伪造观测不再进 `history`/`observed`**（否则 `adjP=1-(1-rawP)^N` 算出的 p 值是假的）；验证器新增 `universe` 维（空 → 进 Dimensions 且 0；非空 → 不进 map）。真库证据（只读）：空票池 `status=invalid`、reasons 三条；反证腿（真库 2024 全年 3 票）`status=completed`、25 笔成交不受影响。
> **切片 2 ✅（2026-10-09）**：读路径收口 —— `state.SetStatus` 按裁定置 `"invalid"`（有效仍 `"completed"`）；`GetBacktestResult` 状态闸门放行 `{"completed","invalid"}`（**无效运行必须可读**，否则「为什么无效」在 UI/报告层不可见，调用方只拿到 409 会把「跑完但无效」误报成「还没跑完」）；抽出 **`buildResponseFromState`** 作为「引擎内存态 → API 响应」的**唯一**透传点，`GET /:id/report` 与 `lookupBacktestResponse` 都改调它 —— 消灭「两处各自内联重建、各自硬编码 Status」的病根结构（这才是 OBS-01 的成因）。handler 层 httptest + engine 断言 + 真库集成三处取证；15 包全绿。
> **遗留（后续任务）**：① `pkg/backtest/reporting/compare.go:130` 是**第三处**硬编码 `Status`（其闸门只认 completed ⇒ invalid 不会混进成功集，但 compare 也读不到 reasons），建议用同一共用函数思路收口；② **UI 仍按成功呈现指标**（`web/src` 无人读 `BacktestResult.status`，渲染判据是 `result.id` 存在）⇒「UI 呈现无效运行」待做。

- **① 目标**：回测/验证在「0 交易 / 空票池」时必须 **fail-loud**（报错或显式标记），不得报成功并产出伪造观测值。
- **② 上下文**：ADR-030 OBS-01（`docs/adr/adr-030-draft-observed-improvements.md`）；实测：`total_trades=0` + `start_date=0001-01-01`（零值）+ `sortino_ratio=1.797e308`（MaxFloat64 垃圾值）仍报成功；根因 `index_constituents`/`sectors`/`stock_sector_map` 全 0 行 ⇒ YAML 默认 `universe: csi300` 解析为空票池；`pkg/ai/loop/loop.go` 会把这些伪造观测计入 `NumTrials` ⇒ **多重检验校正（`pkg/validation/statistical.go` `adjP=1-(1-rawP)^N`）算出的 p 值是假的**。
- **③ 要求**：回测完成时校验「票池非空 ∧ 成交数 > 0 ∧ 日期非零值」，任一不满足 → 标记为**无效运行**（非成功）；验证器新增「票池非空」维（ADR-028 把验证器从 5 维涨到 8 维但仍无此维）；伪造观测不得计入 `NumTrials`。
- **④ 约束与非目标**：改动小、独立；不改变正常回测的行为；不趁机重构验证器链。
- **⑤ 验收**：空票池场景 → 明确报「无效运行」而非成功 + 垃圾指标；正常回测不受影响；**正证据**：造一个「只有票池为空才触发」的场景 + 反证腿（票池非空时不触发）；伪造观测不再进 `NumTrials`。
- **⑥ 边界**：只做「空票池 fail-loud」这一件事，不动验证器其他维度、不动 loop 其他逻辑。

---

## 三、OBS 独立工单（未被设计吸收，源自 ADR-030）

> 完整描述见 [ADR-030](adr/adr-030-draft-observed-improvements.md)。按依赖与价值排序，开工前各自按 §8.5 补全六字段。

| 工单 | 一句话 | 优先级 |
|---|---|---|
| OBS-06 ✅ 完成（2026-10-09，随 K3 切片 2 合并） | DSL 语法闸门不校验算子名 —— 已在 `Expression.Validate()` 补算子/字段/参数个数闸门（fail-closed，报错带位置与可用清单），`validate_factor` 解析后必过闸门 | — |
| OBS-07 | tokenizer 不支持 `>=`/`<=`/`AND`/`OR`/`NOT` | 中 |
| OBS-08 切片 1 ✅ 完成（2026-10-09） | 字段注册表收敛为**单一事实源** + 与 provider 对齐：`DataProvider` 加 `Fields()` 能力声明（5 处实现同步：1 生产 + 4 假）；注册表升级为**带来源**（market / fundamentals / group）；**修误拒**（`ps`/`roa` 现被闸门接受）、**补实现**（`revenue`/`profit` 由 `domain.Fundamental` 的 `Revenue`/`NetProfit` 接线 —— 此前只有声明没有实现）、**清假合法**（`market_cap`/`roe_ttm`/`eps` 从注册表移除：`domain.Fundamental` 无此三字段 ⇒ 永不可求值，留在闸门里就是让 AI 反复撞墙）；求值报错区分「字段不在语言里」vs「字段属于 X 源但本 provider 不供应」；新增**跨包防漂移护栏**（注册表非 group 字段 ≡ `OHLCVDataProvider.Fields()`，双向点名）。**并**：研究提示词 `research.go` 的字段/算子清单改为**从注册表派生**（此前硬编码，广告幻影 `market_cap` 且漏掉 9 个算子与 4 个字段）+ 新增护栏 | — |
| OBS-08 切片 2 ⬜ | **序列注册表 + 可用性声明**（`SeriesSpec` 目前只存在于 `registry.go:105` 一句注释里）：声明每个字段/序列的**当前可用性**（例如 `stock_sector_map` 0 行 ⇒ `sector` 不可用），让 AI 不再对着空数据静默产垃圾。**并**（本轮新发现）：`pkg/ai/prompts/factor_research.txt` 与 `strategy_generate.txt` 是**零引用的死资产**（Go / 最新版文档 / 脚本 / CI 全无引用，仅 `docs/archive/` 提到），且 `factor_research.txt` 的字段清单**同样漂移**（广告 `vwap` / `turnover_rate` / `returns` / `volatility`）。**待裁决：接线它**（当作研究提示词，字段从注册表派生）**还是删除它** | 高（与 OBS-01 同源） |
| OBS-11 | per-day HTTP 反模式 `getSignalsFromStrategyService` 待删（策略服务只传定义不传信号） | 中（随 K1/K2） |
| OBS-12 | Copilot 对外 API 契约与前端脱节（四层缺陷叠加） | 中 |
| OBS-09 | 三份 ADR 引用成环且全 Proposed（027→028→029→027） | 低（治理） |
| OBS-10 | 服务数 5→7 与 ADR-019（Service 合并，仍 Accepted）方向相反，需显式 supersede | 低（治理） |

---

## 四、遗留待裁决（源自旧 TASKS，未关闭）

| 项 | 现状 | 待裁决内容 |
|---|---|---|
| **AUD-61** | ⬜ e2e UI 套件与前端结构严重脱节（干净环境实测 127 passed / 34 failed / 2 skipped；34 条全归因、与代码改动无关） | ① 按页面分批重对齐选择器与期望值（工作量大）；② 先 `test.fixme` + 显式登记恢复「全绿」可信度再消债。⚠️ 不得把「34 条红」洗成「已验证通过」，也不得放宽断言消红 |
| **P2-8** | ⬜ hfq 落库（AUD-55 已修，资金侧敏感度塌掉；价格侧差异=绝对金额约束，非引擎缺陷） | 已得结论「hfq 价不能直接用固定资金跑回测」（hfq 价=qfq 价×每股常数，单位非元）。**裁决：资金要不要按同一基准缩放？** 见旧 TASKS 文末「P2-8 取证说明」 |
| **P2-1** | ⬜ 宏观数据源（跨境那一半已做完，见 `guides/data-dependencies.md`） | 补宏观数据源 |
| **P2-2** | ⬜ 产业链数据底座 | 补产业链数据底座 |

---

## 五、K0 契约审查发现（2026-10-08）

> 审查通过项（记录以免重复审）：依赖方向无环且层次干净（clock/msgbus 零依赖 → eventstore → kernel；`live→risk`、`execalgo→portfolio` 别名方向均与 BootOrder 同向）；11 模块全覆盖；8 张表齐全且归属清楚；编译期守卫经双方独立破坏验证有效；零现有实现改动。

### K0-P2-1 · 核心语义约束需 AST 护栏 ⬜（建议 K1/K2 同步做）

- **① 目标**：把三条**只在注释里**的核心不变量变成机器可验证的护栏。
- **② 上下文**：K0 契约的三条关键语义约束目前只有注释，K1/K2 实现后无从自动验证——
  - `ExecEngine.Submit` 必须先过 `risk.RiskEngine.CheckOrder`（`pkg/live/interfaces.go:87`）
  - `msgbus.Publish` 必须先经 EventStore 记录、再分发（BusTap 语义，D4）
  - `BarHandler.OnBar` 不得回看未来 bar（`pkg/strategy/streaming.go:52`）
- **③ 要求**：用 AST 护栏钉住（项目先例：`internal/repoguard/calendar_seed_isolation_test.go` 的 AST 扫、`cmd/analysis/auth_public_paths_test.go` 的 AST 双向对齐）。
- **④ 约束与非目标**：只加护栏、不改契约签名；护栏本身必须破坏验证（改坏→红→还原）；**防「扫原文被注释误报」**——用 `scanInside(FuncName)` 限定函数体，别扫全文。
- **⑤ 验收**：三条不变量各有一条破坏腿（如 Submit 去掉 CheckOrder 调用 → 该护栏红），且反证腿证明护栏不是永真。
- **⑥ 边界**：只做这三条护栏，不扩到其他语义约束。

### K0-P2-2 · `strategy → ai` 循环已破 ✅（2026-10-09 完成 ADR-027 §5 第 3 步）

- **做了什么**：`git mv pkg/ai/expression pkg/expression`（6 生产 + 6 测试，零逻辑改动）；`pkg/ai/contracts/contracts.go` → `pkg/backtest/contracts/runner.go`（并入已存在的 LEAF 包，包名同 `contracts`）；改 18 个文件的 import/注释；`contracts_test.go` 拆两处落点（解掉「core 测试依赖 AI」）。
- **破环证据**：`go list -deps ./pkg/strategy/ | grep pkg/ai` → **空**（改前 4 处）；core 侧（backtest / strategy / indicator / validation / risk / marketdata / portfolio / execalgo / kernel / expression / live / storage 及全部子包）**零 `pkg/ai` 依赖**。
- **新增护栏**：`internal/repoguard/ai_boundary_test.go` 的 `TestPkgAIIsOnlyImportedByAllowedLayers` —— **白名单式 fail-closed**（仅 `pkg/ai|pkg/tools|cmd|e2e` 可 import `pkg/ai`；只扫生产代码）。破坏验证双向：既有包加 import → 红；**全新包加 import → 红**（默认拒绝性质）。
- **边界定义**：见 ADR-027 §「执行记录：§5 第 3 步已落地 + AI / core 边界定义」（含两个待裁决点：`pkg/validation` 与 `pkg/tools/builtin` 的归属）。
- **仍未做**：`pkg/strategy/copilot.go`（`CopilotService`，518 行）**归位到 AI 侧**属 ADR-027 §5 第 8 步（ai-service）—— 破环不需要它（修完上两项后它只 import core，边自然消失）。
- **副产品**：新护栏自己报出 K4 遗漏的过时白名单条目（`pkg/backtest/marketimpact`），已单独提交（`bd39868`）。

---

### K0-P2-3 · 三项签名级契约变更 ✅（2026-10-08 已裁决并完成）

> K0 审查（依赖/文档层）未发现、K1 实现时才暴露的签名级缺陷（外加审查时新发现的时钟静默陷阱）。K1 曾用 workaround 绕开，已在 K2 前裁决落地。

**最终形态（已落地，`go build ./...` + 三包测试全绿，破坏验证 a/b 各红一次后逐字节还原）**：

| 项 | 变更前 | 变更后（当前） |
|---|---|---|
| `Handler` | `func(ctx, msg)`（无 error，失败只能 panic 传播） | `func(ctx, msg) error`；**逐 handler 调用、遇 error 继续**（一个订阅者失败不拖累其他），全部 error 由 `errors.Join` 聚合上报；**panic 仍不 recover** |
| `Publish` | `Publish(topic, payload)`（总线构造时自持一个 ctx，全消息共享） | `Publish(ctx, topic, payload) error`；ctx **逐消息**透传给落库与 handler；`SyncBus.ctx` 字段**已删除**；ctx 已取消/超时 ⇒ 返回 `ErrContextCanceled` 且**零分发**（连落库都不试） |
| `Append` | `Append(msg)`（`context.Background()` + 固定 `appendTimeout`） | `Append(ctx, msg)`；沿用 `mergeTimeout(ctx, appendTimeout)` 保留超时兜底 |
| 时钟 | `NewSyncBus(tap)`，clk 为 nil 时**兜底墙钟** | `NewSyncBus(tap, clk)` —— **构造期强制**，nil 直接 panic；实盘便捷入口 `NewLiveBus(tap)`；`NewSyncBusWithClock` **已删除** |

- **① `Handler` 改返回 error**：裁决为「**继续分发 + 聚合上报**」。反向（遇错即中断）会把单个订阅者的故障扩散成全总线静默丢消息——那正是 BusTap 要防的缺口。聚合选 `errors.Join`（`errors.Is/As` 天然穿透到每个原始 error，不必自定聚合类型多维护一份真相）。
- **② `Publish` 显式接收 ts（原提案）**：**不采纳**，改为 K1 已有的 Clock 注入 + **构造期强制注入**。理由：ts 由内核/装配决定、每条消息都一样，塞进 `Publish` 参数会让每个调用点都能改写业务时间（ts 是回测锚点）；强制注入 clk 既堵住「回测忘了注入虚拟时钟」的静默陷阱，又不给调用方改 ts 的权力。
- **③ 审查新发现并已修的静默陷阱**：旧 `NewSyncBus(tap)` 在 clk 为 nil 时兜底墙钟——回测装配漏注入 VirtualClock 时编译照过、运行照跑，但 `audit.message_log` 落的是真实世界时刻，两次回放无从比对，**BusTap 审计价值归零且全程不报错**。现改为构造期 panic（故障钉在离错误最近处），墙钟只能由 `NewLiveBus` 显式声明。
- **已解决的架构模式（记录复用）**：`msgbus` ↔ `eventstore` 的 import 环（eventstore 必须 import msgbus 的 `Message`）用**消费方定义窄接口**绕开 —— `msgbus.Tap { Append(ctx, Message) error }`，`eventstore.EventStore` 结构化满足，并由 `var _ msgbus.Tap = (*eventstore.PGEventStore)(nil)` 钉住。这是 Go 惯用法，值得在其他跨模块依赖处复用（与 topics.go 落位修正是同一类问题）。

**本条新登记的两条待办（本次不做，仅记录）**：

#### K0-P2-3a · `Verify` 依赖内存计数器，实例重建即归零 ⬜

- **① 目标**：把 `Verify` 的「写入账」基准从进程内 `atomic.Int64` 换成库内计数。
- **② 现状**：`PGEventStore.Verify` 用本实例成功 Append 的次数（`appended`）与库里 `count(*)` 对账。**实例重建（进程重启、断点续跑）后计数器归零**，而库里的行还在 ⇒ Verify 误报「落库 N 行 vs Append 0 次」，红灯是假的；假红灯会训练人忽略 Verify。
- **③ 要求**：改为基于 `run_id` 的库内计数（例如按 run_id 聚合条数并与期望值对账），使「重开一个 store 实例」不再影响结论。
- **④ 约束与非目标**：不改 `audit.message_log` 的 DDL 与列名；不改 `Verify` 的方法签名。
- **⑤ 验收**：构造 store → Append 5 条 → 丢弃实例 → 用同一 (publisher, run_id) 新建实例 → `Verify` 仍绿。
- **⑥ 边界**：只改 Verify 的计数口径，不做哈希链/checksum、不做 id 连续性检查。

#### K0-P2-3b · `pkg/msgbus/registry.go` 的 topicSet 与 `topics.go` 常量是两份真相 ⬜

- **① 目标**：把注册表收敛为一处真相后删除 `registry.go`。
- **② 现状**：`topics.go` 的 16 个常量是编译期符号，运行期拿不到「全体已注册 topic」集合，于是 `registry.go` 里又手工镜像了一份 `topicSet` map——**加常量忘了登记**是静默失效（新 topic 会被 Publish 拒掉，错误信息清楚但没人提前知道）。现状靠 `registry_internal_test.go` 与 `interfaces_compliance_test.go` 各引用一次全部常量来钉，属于测试兜底而非结构消除。
- **③ 要求**：把 `All`（或等价集合）迁进 `topics.go`，`IsRegisteredTopic` / `RegisteredTopics` 基于它实现，然后删除 `registry.go` 并同步测试引用。
- **④ 约束与非目标**：topic 数量与取值不得变更（仍是 16 个、值同 K0 冻结）；`IsRegisteredTopic` / `RegisteredTopics` 的对外语义不变。
- **⑤ 验收**：删除 `registry.go` 后 `go build ./...` 与 `pkg/msgbus` 全部测试仍绿，且「新增常量未登记」会编译失败（不再只是测试红）。
- **⑥ 边界**：只做这份双真相收敛，不动 topic 取值与 `Publish/Subscribe` 的校验语义。

### K1/K2 实现审查发现（2026-10-08，设计层审查）

> 审查通过与亮点记录：`kernel.go` 全生命周期逻辑正确（回滚只装成功的、回滚 error 丢弃理由已论证、Shutdown 先发后停时序）、`adapters.go` 包装设计论证完整、`kernel_shadow.go` 失败策略与关停时序清楚、`runtime.go` 失败立即停 + universe 过滤 + 横截面就绪门正确。**P1-1 已当场修复**（Boot 防重复闸门 + `ErrAlreadyBooted` + 测试 + 破坏验证，随审查提交）。

| # | 问题 | 处理时机 |
|---|---|---|
| K1-P2-1 | **Boot 的 ctx 在 Boot 后立即 cancel**（kernel_shadow 的 20s timeout + defer cancel）：当前模块不保存 ctx 无影响，但未来模块若在 Start 里起 goroutine 依赖该 ctx 存活，会被抢先 Cancel | 接管时（内核应传长生命周期 ctx，或契约明示 ctx 仅限于 Boot 期间） |
| K1-P2-2 | **回滚 error 被静默丢弃**：`rollbackLocked` 的 Stop error 直接丢弃（注释论证「避免掩盖根因」），但 Stop 失败=资源泄漏，无人知晓 | 接管时（至少 log 或包进返回 error 的 message） |
| K1-P2-3 | **eventstore 模块的资源归属未定**：`EventStoreModule.Stop` 不关 pool（归 pkg/storage 统一 Close）——影子期正确，但接管后必须裁决「pool 是内核资源（模块 Stop 关）还是应用层资源（保持现状）」 | 接管时（与 setup.go 的 gracefulShutdown 归属一起裁决） |
| K1-P2-4 | **模块回调内核会死锁**：`Boot/Shutdown` 全程持 `k.mu`；`Clock()/Bus()/Store()` 不加锁安全，但模块若在 Init/Start/Stop 里调 `Module()`（加锁）会死锁。注释已声明「模块拿不到内核引用」 | 作为**未来模块约束**登记：模块不得在生命周期方法内回调 `Module()` |

### K3 切片 2（+OBS-06）审查发现（2026-10-09，含真库核对）

> **通过项**：注册表（31 算子）结构清晰，`selfWarmup/selfLookback` 由实参推导，闸门递归覆盖四类节点，`DeriveWarmup/Lookback` 与 ADR-028 §8 及附录 A 对齐。**真库核对**（把 `factor_genes.formula` / `experiments.expression` / `strategy_genes.params` / `strategies.params` 的存量表达式全部喂给新闸门）：**可解析 1 条、被拒 0 条** ⇒ 无「既有表达式被判非法」的回归。`pow` 不在注册表（历史 `IsMathOp` 有它但无实现）——复查用它的三处（agent 提示词 / 基因池名单 / yaml 生成器）都没用，无现网影响。

| # | 问题 | 影响与处理 |
|---|---|---|
| K3a | **闸门不校验「标量参数位」的类型**：`ts_mean(close, volume)`、`ts_mean(close, ts_delay(volume,1))` 参数个数与节点都合法 ⇒ 过闸；但求值时 `firstScalar` 取到非标量（或 `NaN`）⇒ `int(NaN)` 未定义 ⇒ 窗口变荒谬值 ⇒ **整条序列 NaN**（假合法残留） | 危害被 OBS-01 兜住（判无效运行，不产出假结论），但浪费 AI 试验且报错无用。修法：`OperatorDef` 加 `ScalarParams []int`，闸门对这些位置要求 `*LiteralNode`（ADR-028 §4 的 `Series × Scalar` 签名本就该在闸门体现） |
| K3b | **防漂移护栏只覆盖 `pkg/expression`**（2026-10-09 前为 `pkg/ai/expression`；护栏按包内相对路径扫描，搬家不破）：`pkg/ai/gene_pool/mutation.go:196` 有 `functions := []string{"abs","log","sqrt","sign","cs_rank","cs_zscore"}`；`pkg/ai/yaml/generator.go` 用字符串模板生成表达式；`pkg/ai/agents/*` 提示词里也写死算子名 | 现状**无实害**（名单内名字都在注册表），但注册表改名/删名会让生成器产出「过不了闸门」的表达式。修法：生成器改查 `AvailableOperators()`，或把护栏扩到生成器侧 |
| K3c | **闸门的字段白名单与 provider 真实能力互相错配**（两个方向）：闸门放行 `market_cap`/`roe_ttm`/`eps`/`revenue`/`profit`/`sector`，而 `OHLCVDataProvider.GetField` 不认（**求值时报 unknown field，fail-loud**）；反过来 provider 支持 `ps`/`roa`，闸门**不认** ⇒ 用它们的表达式会被 fail-closed 闸门**误拒**（比前者更危险：挡住正经用法） | **并入 OBS-08**（序列可用性声明）：字段白名单必须由 provider/`SeriesSpec` 注册表**派生**，并声明「有数据 / 表为空」，把「不可用」在**闸门期**说清而不是留到求值期 |

---

## 六、AI / core 边界归属（2026-10-09 若曦拍板，DDD 判定）

判据（若曦定的）：**「AI 层每次都要用、而 core 层没有用它的地方 ⇒ 放进 AI 层」**，并用 DDD 复核。

| 包 | 判定 | 依据 | 状态 |
|---|---|---|---|
| `expression`（原 `pkg/ai/expression`） | → **core**（`pkg/expression`） | 零 AI 概念、零非确定性、只依赖 core ⇒ 一个核心层包住错了目录 | ✅ `55ea33f` |
| `contracts`（原 `pkg/ai/contracts`） | → **core**（并入 `pkg/backtest/contracts`） | 1 个接口 `BacktestRunner`；6 个消费方里 4 个本就是 core | ✅ `55ea33f` |
| `validation`（原 `pkg/validation`，4920 行） | → **AI**（`pkg/ai/validation`） | core 侧零引用；它是**实验员对假设的判断**（6 维 → `Verdict` 概率），不是回测·执行域的仪器 | ✅ `cff0a0f` |
| `tools`（原 `pkg/tools`，10716 行，含 `builtin/`） | → **AI**（`pkg/ai/tools`） | core 侧零引用；MCP 桥 = 开放主机服务 / **ACL，属边界消费侧** | ✅ `cff0a0f` |
| `strategy/copilot.go` 的 `CopilotService`（518 行） | → **AI** | 自然语言 → 生成代码 → 沙箱编译 → 回测 = 实验员的活，却住在仪器包里 | ⬜ 属 ADR-027 §5 第 8 步（ai-service） |
| `pkg/ai/prompts/{factor_research,strategy_generate}.txt` | **保留 + 待接线** | 零引用的孤儿，但内容是**两处 prompt 契约**（DSL 层策略生成 / 因子研究）；`factor_research.txt` 与 `research.go` 内联提示词构成**双真相**（与 OBS-06/08 同病） | ⬜ 接线 + 词汇表从注册表派生 |
| `pkg/backtest/marketimpact` | → **core**（`pkg/marketimpact`） | 共享成本模型却住在 `backtest` 下；K5 的 paper 侧也要用它 ⇒ 提到顶层与 `pkg/fees` 平级（`pkg/fees` 同为共享成本模型，被 `backtest`/`live`/`portfolio`/`ai` 共用） | ✅ K5 切片 1 |

**契约变更（随 `validation` 归 AI 生效）**：**凡存入策略库的策略，均已由 AI 层验证通过**；core 侧的策略存储不做也不应做独立验证。详见 ADR-027 追加节。

**不变量**（`internal/repoguard/ai_boundary_test.go` 强制）：只有 `pkg/ai/*`、`cmd/*`、`e2e/*` 可以 import `pkg/ai/*` —— 白名单式 **fail-closed**（新包默认拒绝），只扫生产代码。

---

## 维护本文件

- 新增 task 必须按 [AGENTS.md §8.5](../AGENTS.md) 写全六字段（目标/上下文/要求/约束与非目标/验收/边界）。
- 完成即标 ✅（附日期）并定期移入 `archive/`；本文件只留未完成项。
- 大任务（K0–K7）开工前，若六字段不够细，先补细再派工——**独立可运行判据达不到 = task 没写好，不是 agent 的问题**。
