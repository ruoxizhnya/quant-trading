---
status: evergreen
last-verified: 2026-10-12
verified-by: 模块化内核任务重构（2026-10-08）—— 旧 TASKS（2427 行，基本已完成）归档至 archive/TASKS-2026-10-08-archived.md；本文件按 AGENTS.md §8.5 任务定义规范重新生成。2026-10-12 内核可插拔 + NT-ready 新主线重构（ADR-031）—— 新增 K8–K16（阶段 1–3），K7 遗留①② / OBS-01 遗留①② / K0-P2-1 / K1-P2-1~4 并入新任务，「impact 默认滑点」并入 NT 决策门；同日补登记 K17（K4 遗留 exec-algo 主路径接线）+ K16 ③-⑤ 认领 K5 告警接线遗留
---

# TASKS — 未完成项

> **只放未完成项**，完成即移入 `archive/`（旧任务文档：archive/TASKS-2026-10-08-archived.md（`quant-trading-docs:docs/archive/TASKS-2026-10-08-archived.md`），含 Sprint 1–8 与 AUD-01~62 记录）。
> 任务定义遵循 [AGENTS.md §8.5 任务定义规范](../../AGENTS.md)——**每个 task 必须让独立 coding agent 不被打扰地完成并自证**（六字段 + 派工三律）。
>
> 状态标记：`⬜ 待做` / `🔶 进行中` / `⛔ 阻塞（有未解除前置）` / `✅ 完成（附日期）`
> 派工三律（每个 task 都适用，见 §8.5.3）：**一 task 一 commit · 护栏必破坏验证 · 要正证据不靠失败**。

---

## 一、主线：模块化内核（目标架构）✅ 全部完成（K0–K7，2026-10-08~10）

> 详细蓝图：[design/kernel/target-architecture-modular-kernel.md](../design/kernel/target-architecture-modular-kernel.md)（D1–D5 已拍板，2026-10-08）。
> **依赖主线：K0 → K1 → K2 → {K3, K5, K6, K7}；K4 独立（只依赖 K1 的 exec-engine 挂点）。**
> K0 是所有并行开发的**硬前提**（契约先行冻结，见蓝图 §5.1）。K0 完成后，K3/K4/K5/K6/K7 可各自交一个独立 agent 并行开发。
> **本主线已收官，遗留项按 ADR-031 并入新主线（§二）**：K7 遗留①②→K8、OBS-01 遗留①②→K9、K0-P2-1→K11、K1-P2-1~4→K12、K4 遗留（exec-algo 接线）→K17、K5 遗留（告警接线）→K16 ③-⑤。

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

### K1 · 内核骨架（P1）✅ 完成（2026-10-10，三切片）（前置：K0）

> **切片 1 ✅（`b33afd7`）**：clock / msgbus / eventstore 三模块实现（63 测试含签名变更）。
> **切片 2 ✅（2026-10-08）**：`StandardKernel` 真实现（Boot 按 BootOrder / 失败逆序回滚 / Shutdown 逆序幂等 + 关停消息先发再停 / Module 检索）+ 适配包装 `pkg/kernel/adapters.go`（Clock/MsgBus/EventStore 三真模块）+ **`main.go` 影子启动**（3 真模块 + 7 占位，**现有装配一行未改**）+ 真环境验证（服务起 + `/health` 200 + `audit.message_log` 实测 kernel.boot/shutdown 落库）。
> **待做（高风险，单独切片）**：把现有组件（backtest.Engine / risk / live / alert / store …）包装成 Module，让 Kernel 真正接管 `setup.go` 的装配顺序；接管后影子启动的「失败不阻断」改为 fail-fast。
> **切片 3 ✅（2026-10-10）**：接管完成。四个业务模块包装（`pkg/kernel/modules_analysis.go`）：data-engine(3) ← provider+DataAdapter；risk-engine(5) ← RiskManager；exec-engine(6) ← MockTrader；strategy-runtime(7) ← Engine 构造 + 全部 Set* 注入（第 7 位是顺序裁决：此刻 3/5/6 产物全就位）。装配迁移到 `cmd/analysis/kernel_wiring.go`（`assembleKernel`，Boot 失败 `logger.Fatal` 拒绝启动——fail-fast）；影子启动文件删除；三个构造 shim 随调用者消失而删（死代码零残留）。**偏差登记**：① portfolio(4)/indicators(8)/exec-algo(9) 三槽位现无对应可接线组件（portfolio 状态活在 MockTrader/engine 内；K3 算子是 per-run 实例化；K4 算法是 per-run 挂点）⇒ Noop 占位 + 理由注释（BootOrder 宪法级冻结，槽位必须占住保 Shutdown 逆序完整）；② JobService/WF/Batch/FactorAttributor/StrategyDB/auth/alert 不在蓝图 10 模块矩阵（无槽位），仍由 cmd 层构造——加槽位 = 改宪法，须走变更评审。**验收证据**：模块层正证（按 BootOrder 序 Init 产物链完整）+ 两条破坏腿（缺 data-engine / 缺 exec-engine 各 fail-fast 点名）；结构护栏 `wiring_guard_test.go`（main 必须经 assembleKernel+fail-fast；不得直接调三个 builder/Set*；影子不得复活）破坏验证红点名「被抢回装配代码，BootOrder 失去顺序控制权」；`kernel_wiring.go` 槽位完整性护栏。全量回归 67 包全绿。
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
> **✅ 已裁决（2026-10-14，随 NT 决策门一并拍板）**：**维持默认 `fixed`，`"impact"` 保留为显式选项（`ExecutionConfig.ImpactSigma`），不设默认。** 理由：① 现阶段 ADV 用当根 `quote.Volume` 代理，证据不足以支撑默认（真实口径应为多日均量）；② K15 双跑基线以滑点=0 跑出，现在翻默认会使两侧基线失效需整体重跑；③ **待有真实成交回执数据后单独再裁**（届时可用实测滑点校准模型参数）。
> **未做（后续）**：~~把 exec-algo 接进下单主路径~~（**✅ 已由 K17 落地，2026-10-12，`6205c64`**——原写「归 K5」但 K5 切片未含、代码核实 `pkg/execalgo` 零生产引用，补 K17 认领）；真实券商算法单（D1 不做）；VWAP 的成交量预测（现只用给定 profile，沿用 K4 登记不做）。

- **① 目标**：exec-algo 模块落地 TWAP/VWAP 执行算法（与策略同构 trait），撮合引擎加市场冲击模型（D5：模拟真实大单）。
- **② 上下文**：蓝图 UC4（执行算法拆单）；nautilus `crates/trading/src/algorithm/`（TWAP 同构）；K1 的 exec-engine 挂点。
- **③ 要求**：`ExecAlgorithm` 接口（`OnBar`/`OnFill`/`Schedule()`）；TWAP/VWAP 实现（父订单拆子订单，按时间片）；撮合市场冲击模型（滑点随订单量占盘口/成交量比例增大，大单分层成交）。
- **④ 约束与非目标**：执行算法与策略同构但**不取代**策略接口；真实券商算法单直连不做（D1）。
- **⑤ 验收**：TWAP 拆单后子订单时间分布均匀；市场冲击下大单滑点 > 小单滑点；破坏验证——去掉冲击模型 → 大小单滑点相同（反证）。
- **⑥ 边界**：只做执行算法与冲击模型，不做策略、不做真实券商。

### K5 · 实盘 paper（P5）切片 1 ✅ / 切片 2 ✅（前置：K2）

> **切片 1（2026-10-09）**：读法 A 的 paper 回放链路 —— 回放与回测**同一批**日线 bar（不造 tick→bar 假聚合层），用**同一段代码**的执行成本核撮合。若曦拍板：paper 与回测必须吃同一批输入，差异才只可能来自执行机制 ⇒ 对账才可归因。
> **切片 2（2026-10-09 完成）**：**信号对账**（若曦拍板「信号为核心」）—— 同一策略、同一批 bar、同一窗口语义，paper 与回测两侧独立产出的信号序列**逐条一致**（`ReconcileSignals` 零差异）。对**信号**而非**成交**，因为成交差异会被 T+1/现金约束/风控污染（两侧本就不同），信号在执行机制之前、只由喂给策略的 bar 序列决定，是「同构验证」里最干净、最可证伪的一层。

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

**切片 2 交付明细（2026-10-09）**：
- 新增 `pkg/live/signal_reconcile.go`：纯函数 `ReconcileSignals(paper, backtest []domain.Signal, cfg)`，比对键=(date,symbol)，比 direction/strength/order_type/limit_price；**刻意忽略 portfolio**（纯 bar 驱动才是信号的可对账层，依赖 portfolio 的差异属执行机制）
- 两侧信号暴露钩子（默认 nil 零行为变化）：paper 侧 `PaperSessionConfig.SignalCollector`（撮合前回调）；回测侧 `Engine.SetSignalObserver`（getSignals 后、撮合前回调）
- 端到端测试 `pkg/backtest/signal_reconcile_e2e_test.go`：同一批合成 bar，两侧独立跑 → 91 条信号逐条一致（正证据）；截断窗口 → 5 条差异被点名（反证腿）
- 对账器单测 8 条（一致/缺失/方向/强度/容差/顺序无关/确定性输出）
- ✅ 告警接线**已收口（2026-10-13，K16 ⑤，`9502ec2`）**：原遗留「`ReconciliationWorker`/`AlertDispatcher` 零生产构造点、信号差异只以列表 + fail-loud 兑现」——已由 K16 ⑤ 认领并落地：`pkg/alert` 的 `ReconciliationDispatcher`/`SignalDispatcher` 生产实现（一条差异一条告警）+ `cmd/analysis` reconciliation worker 首个生产构造点 + `cmd/dualrun -gate` 违例进告警通道，验收「注入对账差异 → 告警触发点名」兑现

### K6 · L3a WASM（P6）切片 1 ✅ / 切片 2 ✅ / 切片 3 ✅（前置：K2）

> **切片 1（2026-10-09 完成）**：wazero 入 go.mod + `WazeroRuntime` 落地 + `InProcessRuntime` 退役。
> **切片 2（2026-10-09 完成）**：host API 白名单 + `get_bar(t_offset>0)` trap + import section 扫描器。
> **切片 3（2026-10-09 完成）**：协议改造 `generate_signals` → 逐 bar `on_bar(t)` + `finalize`（堵前视漏洞）+ 状态断点续跑一致性。

- **① 目标**：wazero 沙箱落地，host API 受限（`get_bar` 防前视 + `get_state`/`set_state`），仅在 L2 无法表达时启用（D2）。
- **② 上下文**：蓝图 UC2；ADR-029 §3（WASM host API 契约）+ ADR-007 Phase 3（2026-10-08 对齐：轨道 B=L3a）；ADR-024（不做 plugin.Open）；`internal/sandbox/wasm/sandbox.go`（骨架，现用 InProcessRuntime fallback）。
- **③ 要求**：`wazero` 入 go.mod，`InProcessRuntime` 退役；host API 白名单（`get_bar(t_offset≤0)` 越界 trap、`get_state`/`set_state`）；协议从一次性写全部 bars 改逐 bar `on_bar(t)`（堵前视漏洞，ADR-007 2026-10-06 注记）。
- **④ 约束与非目标**：**仅 L2 无法表达时启用**（D2）；沙箱无文件/网络/时钟/随机源/goroutine。
- **⑤ 验收**：`get_bar(t_offset>0)` → trap（防前视物理不可能）；WASM 策略断点 `SaveState`/`LoadState` 续跑一致；破坏验证——去掉 t_offset 检查 → 能读到未来数据（反证）。
- **⑥ 边界**：只做 WASM 沙箱与 host API，不做 L2 算子（K3）、不做 L3b（K7）。

**切片 1 交付明细（2026-10-09）**：
- `go.mod` 加 `github.com/tetratelabs/wazero v1.12.0`
- 新增 `internal/sandbox/wasm/wazero_runtime.go`：`WazeroRuntime`（Runtime/CompiledModule/Instance 三接口的真实隔离实现）
  - 内存限制语义变更（已裁决）：wazero 是 **runtime 级**上限（`WithMemoryLimitPages`），不是 per-instance；现有 `Instantiate(memoryLimitBytes)` 参数本就是冗余（总是传同一个 `cfg.MaxMemoryBytes`），故收敛到 runtime 级，实例化时校验请求 ≤ 上限
- 退役 `InProcessRuntime`（删除注入式 Go handler 模拟执行，约 220 行）+ `PluginHandler`/`inProcessModule`/`inProcessInstance`
- 重写 `sandbox_test.go`（23 条全绿）：因本机无 wasm 编译工具链（无 tinygo/wat2wasm），测试用 `wasm_builder_test.go` 的极简 builder **结构化构造** wasm 字节（参考 wazero 官方 encodeModule），而非手写十六进制
- 破坏验证：`Compile` 对无效字节不报错 → `TestWazeroRuntime_Compile_Invalid` 红 → 还原 → 绿 → 零残留

**切片 2 交付明细（2026-10-09）**：
- 新增 `internal/sandbox/wasm/host_api.go`：host API 白名单（`get_bar`/`get_series_len`/`get_symbol_count`/`get_cross_section`/`get_state`/`set_state`/`emit_signal`/`log`），模块名固定 `env`
  - **`get_bar(t_offset>0)` / `get_cross_section(t_offset>0)` 直接 panic → wazero 转 wasm trap**（前视物理不可能）
  - 数据模型（per-symbol 实例）：ADR-029 §3 的 get_bar 签名无 symbol 参数 ⇒ 一个 WASM 实例绑定「当前 symbol 序列」（`BarContext.Series`），跨 symbol 用 `get_cross_section(t_offset, idx)`
  - `BarContext` 经 `context.WithValue` 注入（wazero host 函数 ctx 是调用链透传，实测确认）
  - `get_state`/`set_state` 宿主管理持久化状态（切片 3 的 SaveState/LoadState 续跑一致性依赖它）
- 新增 `ValidateImports`（import section 扫描器）：扫描 `CompiledModule.ImportedFunctions()`，拒绝任何「非 env 模块」或「非白名单函数」的 import；接入 `WazeroRuntime.Compile`（fail-closed）
- 测试（`host_api_test.go`）：get_bar 过去/当前/负数偏移正确返回、**未来偏移 trap**、get_state/set_state 往返一致、import 扫描器拒绝越权 import（含非 env 模块）
- 破坏验证 3 条：去掉 t_offset 检查 → `TestHostGetBar_FutureTraps` 红（读到未来值）→ 还原绿；去掉 import 扫描 → `TestValidateImports_Forbidden` 红 → 还原绿；ReadMemory 去 copy → 红 → 还原绿

**切片 3 交付明细（2026-10-09）**：
- 协议改造 `generate_signals` → 逐 bar `on_bar(t)` + `finalize`（ADR-029 §3.3 / ADR-007 2026-10-06 注记）：新增 `internal/sandbox/wasm/on_bar_session.go`（`OnBarSession`：`Initialize`（写 params@0、调 `initialize`、期望返回 0）→ `OnBar(ctx, bc)`（`withBarContext` 注入、调 `on_bar` 传 `uint64(uint32(bc.T))`）→ `Finalize(ctx, bc)`（调 finalize、解 ptr/len）→ `Close`）
- 退役旧协议：删除 `sandbox.go` 末尾 `StrategyPluginSession`（generate_signals）块 + `sandbox_test.go` 里的 `wasmStrategy`/`TestStrategyPluginSession_InitializeAndGenerate`；`wasm_builder_test.go` 增 `opDrop`（0x1a）清 get_bar 返回值残留
- 测试（`on_bar_session_test.go`，3 条全绿）：`TestOnBarSession_Protocol`（5 bar 累积、空 finalize）、`TestOnBarSession_NoBarContext`（fail-loud）、`TestStateCheckpointResume`（BarContext.State 快照/恢复 ⇒ get_state 读回 "30"）
- 破坏验证：去掉 `OnBar` 里的 `withBarContext` 注入 → `TestOnBarSession_Protocol` 红 → 还原 → 绿 → 零残留
- 全量 `go build ./...` + `go vet` + 4 包测试（wasm/repoguard/live/backtest）全绿，gofmt 干净

### K7 · L3b 信号注入（P7）✅ 完成（2026-10-09，两切片）（前置：K2）

- **① 目标**：外部模型（进程外，任意 Python/ML 框架）产出信号注入内核，由策略消费（Actor 模式）。
- **② 上下文**：蓝图 §6.4；nautilus `Actor.on_signal`；K2 的策略运行时。
- **③ 要求**：外部模型信号表（信号契约：符号/方向/强度/as_of）；Actor 消费契约（策略 `on_signal` 读信号）；信号落库可追溯。
- **④ 约束与非目标**：模型在进程外（自由度挡在内核外）；内核只认信号契约，仍确定可审阅。
- **⑤ 验收**：外部模型信号注入 → 策略按信号下单；信号可追溯（content_hash 坐标）；破坏验证——注入未来日期的信号 → 拒绝。
- **⑥ 边界**：只做信号注入与消费契约，不做模型训练、不做 L3a（K6）。

> **消费形态（若曦 2026-10-09 拍板）**：**信号表 + 按 as_of 拉取**（非运行时回调推送）——外部模型只写 `quant.external_signals`，内核 `SignalStrategy` 逐 bar 按 `as_of <= bar.Date` 拉取；回测=重放历史、实盘=读到今天为止，同一段消费代码同构。**Actor 形态**：`SignalStrategy` 纯消费者（外部信号本身即策略，读信号转 `domain.Signal`），复用 K2 的 `BarHandler` 同构桥，零新执行模式。

**切片 1 交付明细（2026-10-09）**：
- `pkg/strategy/external_signal.go`：`ExternalSignal` 契约（ModelID/Symbol/Direction/Strength/AsOf/Factors）+ `SignalContentHash`（sha256 规范 JSON，**只哈希可执行字段** model_id/symbol/direction/strength/as_of，Factors 不入哈希——诊断非身份）+ `ValidateExternalSignal`（fail-loud 拒 ModelID/Symbol 空、Direction 非 long/short/close、Strength NaN/±Inf、AsOf 零值）+ `SignalStore` 接口（Save 幂等 / ListFor 按 as_of≤upTo 拉取）
- `pkg/strategy/signal_store.go`：`PGSignalStore` + `SignalSchemaDDL`（`quant.external_signals`，UNIQUE content_hash）+ `EnsureSchema` + `Save`（`ON CONFLICT (content_hash) DO NOTHING` 幂等）+ `ListFor`（`WHERE model_id/symbol AND as_of<=$3 ORDER BY as_of`，**未来信号物理不可见**）
- **信号对齐以 UTC 自然日为粒度**（`dayOf` 归一）：内核是日线驱动的，as_of 的时分秒无语义，统一归一到 00:00 UTC 消除「同一日不同时刻」的比较歧义（K5 signalKey 归一化的同源教训）
- `contracts/kernel_modules.schema.sql` + `docs/SPEC.md` 追加 `quant.external_signals`（第 9 张表，归属 strategy-runtime）
- 测试：content_hash 确定性/字段敏感性/Factors 不入哈希/时区归一 + ValidateExternalSignal 全分支 + 真库 roundtrip/幂等去重/ListFor 未来排除/排序/scope/fail-loud 拒绝
- **破坏验证（切片 1）**：去掉 `ListFor` 的 `as_of <= $3` → `TestPGSignalStoreListForExcludesFuture` 红（读到未来信号）→ 还原绿

**切片 2 交付明细（2026-10-09）**：
- `pkg/strategy/signal_strategy.go`：`SignalStrategy`（**同时**实现 Strategy + BarHandler，引擎双模式接入据此走流式路径）。`OnBar` 拉取 as_of≤当日 → `rejectFutureSignals` 防前视拒绝 → 游标去重消费 → 游标推进；`SaveState`/`LoadState` 序列化游标 + pending（断点续跑）
- **信号对齐语义（裁决）**：每条信号在「首个 bar 日期 >= as_of」那根 bar 消费一次——as_of 命中交易日→当天、非交易日→下一交易日、早于窗口首日→跳过（不倾倒历史）、晚于当前 bar→拒绝
- 测试（`signal_strategy_test.go` 8 条）：信号→Signal 转换/首 bar 跳历史/周末顺延/游标去重/未来拒绝（含纯函数）/SaveLoad 续跑/LoadState 拒坏输入/接线 fail-loud
- 端到端（`pkg/backtest/signal_inject_e2e_test.go`）：注入 buy+close 信号 → **2 条 domain.Signal → 2 笔成交**（注入即下单闭环）；未来信号反证腿 → 零成交且被 OBS-01 裁为 `invalid`（zero_trades）
- **破坏验证（切片 2）**：去掉 `OnBar` 里的 `rejectFutureSignals` → `TestSignalStrategyRejectsFutureSignal` 红（未来信号被静默接受）→ 还原绿
- 全量 `go build ./...` + `go vet` + strategy/repoguard/backtest 全绿；gofmt 干净

> **⚠️ 已知边界（2026-10-12 更新归宿）**：① 信号表 DDL 并入 `migrate()` → **已并入 K8**（§二）；② 外部模型 HTTP 写入入口 → **已并入 K8**（`POST /api/signals`）；③ `SignalStrategy` O(N²) 消费优化 → 不阻塞、另立小任务（K8 明确列为非目标）。

---

## 二、主线（新）：内核可插拔 + 外围 NT-ready（阶段 1–3，ADR-031）

> **决策**：[ADR-031](https://github.com/ruoxizhnya/quant-trading-docs/blob/main/docs/adr/adr-031-kernel-pluggable-nt-ready.md)（buy vs build：内核降级为可替换执行器，NT 侧车「信号—权重回放适配器」，Go 策略资产零重写）。
> **需求与设计**：[design/kernel/kernel-pluggable-nt-ready.md](../design/kernel/kernel-pluggable-nt-ready.md)（四契约 RunSpec/SignalSpec/ResultSpec/EngineRunner、适配器规则、FR-1~FR-7）。
> **依赖主线**：阶段 1 = {K8, K9} 并行 → K10；K11 / K12 独立可并行（K12 是阶段 3 前置）。阶段 2 = K13 → K14 → K15（前置 K8–K10）。阶段 3 = K16（前置 K15 + **NT 决策门通过**，见 §五）。K17 独立（K4 遗留收口，不在 NT 阶段链上）✅ 已完成（2026-10-12，`6205c64`；审查遗留 K17-R1 `547564b` / K17-R2 `4aab98c` / RV1–RV4 均已 2026-10-13 收口，K18 `7a3576e`）。
> **总原则**：阶段 1 全程「默认行为逐字节不变」——不带新字段的存量请求走 Go 内核，数值、语义、API 形状零变化（回归腿钉住）。

### K8 · SignalSpec：`SignalProvider` 接口化 + K7 信号通路收口（阶段 1）✅ 完成（2026-10-12，`3c09ed4`）

> **收口记录 ✅（2026-10-12）**：六条全落地，破坏腿均实测红后还原。① `SignalProvider` 契约（`Signals(ctx, scope, asOf) ([]Signal, error)`）+ `LocalSignalProvider` 本地实现：批式 `GenerateSignals` 与流式 `OnBar`/`Signals()` 双路径统一收口到接口，默认行为逐字节不变（错误消息/错误码/op 名原样保留，既有双模式等价、信号对账、注入 e2e 测试全过）。② 契约级防前视：`RejectFutureSignals` fail-loud（`DayOf` 日粒度），「未来信号物理不可见」从事后校验升格为接口语义；`dayOf` 导出为 `DayOf`，「未来」判定唯一口径。③ 引擎改经接口取信号（`getSignalsFromLocalStrategy` 退役，逻辑移入 `LocalSignalProvider`；新增 `EngineOption.WithSignalProvider`）。④ K7 遗留①：`quant.external_signals` DDL 并入 `migrate()`（Migration 048，DDL 文本 `storage.ExternalSignalsSchemaDDL`）+ `docs/migrations/048` 镜像 + 一致性测试守 `migrate() ≡ EnsureSchema`。⑤ K7 遗留②：`POST /api/signals` HTTP 写入门（批量 `ExternalSignal`；逐条 `ValidateExternalSignal` + 未来 as_of 拒绝、全批校验先于落库；`content_hash` 幂等去重；RBAC `requireTrader`）+ openapi.yaml 同步。⑥ e2e 正证据：`POST /api/signals` 写入 → `SignalStrategy` 消费 → 引擎下单闭环一条通过。破坏腿：`LocalSignalProvider` 人为返回未来信号 → 契约拒绝测试红；删接口方法 → 编译期合规测试红；migrate DDL 与 `SignalSchemaDDL` 不一致 → 一致性测试红（均已还原）。远程 `SignalProvider` 实现（HTTP/Redis 消费）按非目标留阶段 2。

- **① 目标**：引擎取信号改为经 `SignalProvider` 接口（本地实现行为逐字节不变），且外部信号通路（信号表进 `migrate()` + HTTP 写入入口）收口，成为可插拔信号供给的默认远程形态。
- **② 上下文**：设计文档 §2.2（接口草案）；K7 交付物 `pkg/strategy/external_signal.go`（`ExternalSignal`/`SignalContentHash`/`ValidateExternalSignal`/`SignalStore`）、`pkg/strategy/signal_store.go`（`PGSignalStore` + `EnsureSchema`）、`pkg/strategy/signal_strategy.go`（`SignalStrategy`，as_of 消费 + `rejectFutureSignals`）；引擎调用点 `pkg/backtest/engine.go`（`getSignalsFromLocalStrategy`，批式 `GenerateSignals` 与流式 `OnBar`/`Signals()` 双路径）；K7「⚠️ 已知边界」①②。ADR-031 D1/D2。
- **③ 要求**：
  1. `pkg/strategy` 新增 `SignalProvider` 接口：`Signals(ctx, scope, asOf) ([]Signal, error)`——**返回 `as_of <= asOf` 的信号，未来信号物理不可见是契约级语义**（任何实现违反即违约）；
  2. `LocalSignalProvider` 包装现有注册表 + 批式/流式双模式调用，**行为逐字节不变**；引擎两条取信号路径改经接口（默认注入 LocalSignalProvider）；
  3. K7 遗留①：`quant.external_signals` DDL 并入 `pkg/storage/postgres.go` 的 `migrate()`（**内联 DDL**，`docs/migrations/` 加镜像副本、编号顺延；`EnsureSchema` 保留为幂等兜底，与 migrate 语义一致）；
  4. K7 遗留②：外部模型 HTTP 写入入口 `POST /api/signals`（批量 `ExternalSignal`；`SignalContentHash` 幂等去重；`ValidateExternalSignal` fail-loud 拒坏输入与未来 as_of；敏感写操作走 `RequireRole` RBAC）+ `docs/openapi.yaml` 同步。
- **④ 约束与非目标**：不改 `SignalStore`/`SignalStrategy` 既有行为与语义（含 UTC 自然日归一）；K7 遗留③（`SignalStrategy` O(N²) 消费优化）**不做**（正确性已有游标去重保证）；`quant.strategy_state` 不并入 migrate（另案）；远程 `SignalProvider` 实现（HTTP/Redis 消费）**不做**（阶段 2）；不动 NT、不动 ResultSpec/RunSpec。
- **⑤ 验收**：`go build ./...` + 全量 `go test ./...` 全绿（行为零变化正证据：K2 双模式等价、K5 信号对账、K7 注入 e2e 既有测试全过）；真库 `migrate()` 后 `\d quant.external_signals` 存在且与 `EnsureSchema` 结果一致；`POST /api/signals` 写入 → `SignalStrategy` 消费下单 e2e 一条通过；**破坏验证**：① `LocalSignalProvider` 人为返回 `as_of > asOf` 的信号 → 契约拒绝测试变红；② 删接口一个方法 → 编译期合规测试变红；③ migrate 的 DDL 与 `SignalSchemaDDL` 不一致 → 一致性测试红。
- **⑥ 边界**：只做 SignalProvider 接口 + 本地实现 + 引擎接线 + 信号表 migrate + HTTP 写入门，一个 commit。

### K9 · RunSpec/ResultSpec 冻结 + status 全链路收口（阶段 1）✅ 完成（2026-10-12）

> **收口记录 ✅（2026-10-12）**：四条全落地，破坏腿均实测红后还原。① RunSpec：`BacktestRequest` 增 `kernel`（默认 `"go"`，`EffectiveKernel()` 默认不回填字段）+ `engine_params`（`ValidateRunSpec` 未知名点名拒绝「rejected, not ignored」，POST 旧格式同闸门 400）；存量请求序列化逐字节不变（golden 断言）。② ResultSpec 冻结：`docs/SPEC.md`「ResultSpec 冻结」节（27 字段表 + 指标口径封闭系统日收益 + status 三值语义含不变量/判据唯一/同源声明）+ 三层护栏（字段集反射比对 / 三值常量冻结 / SPEC.md 文档互锁），字段集破坏腿实测红。③ 同源口径：state→response 唯一透传点上提为 `Engine.ResponseFromState`（`cmd/analysis.buildResponseFromState` 保名薄包装），compare engine 路径改同源读 status/reasons；`CompareReport` 增 `invalid` 列表带 `invalid_reasons`，invalid 不混入 Entries（混合集断言：invalid TotalReturn 故意设最大仍不评 Best）、Resolved 不蒸发、failed fail-closed 归 Missing 带真实错误；闸门改回只认 completed → 混合集断言红（已验）。④ UI（quant-trading-ui 仓，另 commit）：`result.status` 三值判据、invalid 显式警示 + reasons 列表、failed 显式呈现，废除 `result.id` 存在判成功；vitest 5 腿 + Playwright 1 条全绿，判据改回 `result.id` → vitest 红（已验）。metrics 口径与 `omitempty` 语义未动，EngineRunner 路由留 K10。

- **① 目标**：RunSpec 增加内核路由字段（未知引擎参数 fail-closed 拒绝）；ResultSpec canonical schema 冻结为契约；`status` 三值语义（`completed`/`invalid`/`failed`）在 API、compare、UI 全链路一致呈现。
- **② 上下文**：设计文档 §2.3/FR-3/FR-4；`pkg/backtest/contracts/contracts.go`（`BacktestRequest`/`BacktestResponse`）；`pkg/backtest/contracts/validity.go`（`CheckValidity`，OBS-01 切片 1）；`buildResponseFromState`（OBS-01 切片 2 的唯一透传点）；OBS-01 遗留①（`pkg/backtest/reporting/compare.go` 第三处硬编码 Status，其闸门只认 completed ⇒ 读不到 reasons）②（UI 不读 `BacktestResult.status`，渲染判据是 `result.id` 存在——UI 代码在 **quant-trading-ui 仓** `src/`，跨仓改）。
- **③ 要求**：
  1. `BacktestRequest` 增 `kernel` 字段（默认 `"go"`）+ `engine_params`（map，**未知名拒绝**而非忽略）；不带新字段的存量请求序列化行为不变；
  2. ResultSpec 冻结：`BacktestResponse` 字段集 + 指标口径（封闭系统日收益）+ status 三值语义写入 `docs/SPEC.md`，加合规测试（字段集变更须破坏该测试，走变更评审）；
  3. `compare.go` 改从 `buildResponseFromState` 同源口径读 status/reasons（invalid 运行不混入成功集、且 compare 能呈现 reasons）；
  4. UI（quant-trading-ui 仓）读 `result.status`：`invalid` 显式呈现（警示样式 + `invalid_reasons` 列表），`failed` 显式呈现，不再以 `result.id` 存在判成功。
- **④ 约束与非目标**：默认请求（无新字段）API 响应逐字节不变；不改 metrics 计算口径；不改 `omitempty` 零值语义（P1-25 另案）；不做 EngineRunner 路由实现（K10）；不做 openapi 大版本化（P2-16 已有机制）。
- **⑤ 验收**：core：契约测试（`kernel` 透传 / 未知 `engine_params` 键 → 400 / ResultSpec 字段集护栏破坏验证）+ compare 对含 invalid 的混合集断言；ui：`npm run typecheck` + `npm run lint` + `npm test` 全绿 + Playwright 一条（invalid 结果页呈现警示与 reasons）。**破坏验证**：UI 渲染判据改回 `result.id` → 对应 vitest 变红；compare 闸门改回只认 completed → reasons 断言变红。
- **⑥ 边界**：RunSpec/ResultSpec/status 三件事，跨 core + quant-trading-ui 两仓（各自 atomic commit，core 先、ui 后）。

### K10 · EngineRunner 可插拔 + 内核路由（阶段 1，前置：K9）✅ 完成（2026-10-12）

> **收口记录 ✅（2026-10-12）**：四条全落地，破坏腿实测红后还原。① `contracts.EngineRegistry`（`Register` 装配期 / Must 风格 misuse panic、`Resolve` fail-closed 哨兵 `ErrKernelNotRegistered`，message 点名被拒内核 + 已注册集合 + 「不回退」语义；零值可用、RWMutex 并发安全）。**接口选型偏差（已裁决）**：注册表承载 `contracts.EngineRunner`（四家消费方共同窄接口）而非任务原文所写 `contracts.BacktestRunner` —— 后者是 AI 管线的五参端口，签名不同，强行承载需给每条调用链加适配层，违背「编排零改动」；`runner.go` 的 BacktestRunner 一字未动，两条接口刻意分离（裁决记入 SPEC.md「RunSpec 内核路由」节）。② 三处接线：同步 handler `POST /api/backtest` 经 `Resolve(req.EffectiveKernel())`（未注册 → 400 点名内核，解析先于执行与落库）；异步 job 改持注册表执行期解析（`CreateJobRequest` 增 `kernel`/`engine_params`，经 params JSONB 保留键落库往返、非空才写 —— 存量作业 params 序列化不变；K9 的异步整体拒绝闸门退役，改走与同步同一道 `ValidateRunSpec` 400）；batch 经注册表取 go runner；walk-forward 保留每窗口全新 Engine 工厂（缓存隔离优先，编排包零改动）。③ fake runner 全链路：`kernel=fake` 同步 200 带 fake marker 并经 SaveSyncResult 落库、异步 job completed 且 result 同 schema（`TestBacktestPost_FakeKernelRunsAndPersists` / `TestBacktestPost_AsyncKernelFakeCompletes` / `TestJobService_KernelFakeRunsFakeAndPersistsSameSchema`）。④ 三层破坏验证：`Resolve` 改「未知名回退 go」→ contracts 4 红 + job 1 红 + handler 2 红（均已还原）。回归腿：不带 kernel 请求穿过路由进 go 引擎（日历 409 为证）、默认值不回填请求字段、存量 params 不多键。`go build ./...` + 全量 `go test ./...` 67 包全绿；openapi.yaml 补 kernel/engine_params（BacktestRequest + CreateJobRequest）；SPEC.md 增「RunSpec 内核路由」节。**审查修复（`285d7d5`，2026-10-12）**：① `Register` 重复注册 panic 先解锁再抛（recover 后注册表不得带永久写锁，回归腿实测）；② job 侧门畸形保留键 fail-closed（kernel 非字符串不得静默跑 go / engine_params 非对象不得静默丢弃）；③ 文档补内核名大小写敏感口径。

- **① 目标**：执行器注册表 + 按 `RunSpec.kernel` 路由落地：Go runner 默认选中、行为不变；未知/不可用内核 **fail-closed 明确报错、零静默回退**；用 fake runner 证明「外部引擎可插拔」全链路成立。
- **② 上下文**：设计文档 §2.4/FR-1；`pkg/backtest/contracts/runner.go`（`BacktestRunner` 窄接口，签名不动）；两条调用链：同步 `cmd/analysis/handlers_backtest.go`（`POST /api/backtest`）与异步 `pkg/backtest/job/job.go`（job worker）；ADR-031 D1。
- **③ 要求**：
  1. `EngineRegistry`（`Register(name string, runner contracts.BacktestRunner)` / `Resolve(name) (BacktestRunner, error)`），装配期注册 `go` runner（现有实现包装，默认名）；
  2. 同步与 job 异步两条路径都改经 `Resolve`；`kernel` 字段（K9）为路由键；
  3. 路由失败语义：未注册/侧车不可达 → 显式错误（同步 4xx/5xx 且 message 点名内核名；job → `failed` + error_msg），**不得回退 go**；
  4. walk-forward / batch 经同一注册表取 runner（编排逻辑零改动）。
- **④ 约束与非目标**：`BacktestRunner` 签名不改；不实现 NT runner（K13）；不改 job/batch/walkforward 的编排语义与并行度；不加内核热插拔（注册在装配期，运行期不改）。
- **⑤ 验收**：测试注册 fake runner 为 `"fake"` → 请求 `kernel=fake` 走 fake 并落库同 schema（正证据）；`kernel=nautilus`（未注册）→ 明确报错、**断言响应与库内均无 go 内核产物**（反证腿：证明没有静默回退）；不带 `kernel` → 与现状逐字节一致（回归腿）。**破坏验证**：路由改成「未知名回退 go」→ fail-closed 测试红。
- **⑥ 边界**：注册表 + 路由 + 两条调用链接线，一个 commit。

### K11 · 三条内核不变量 AST 护栏（阶段 1，原 K0-P2-1 并入）✅ 完成（2026-10-12）

> **收口记录 ✅（2026-10-12，`06b97b2`）**：三条护栏全落地，位置随被约束代码所在包。① `pkg/live/exec_submit_risk_check_test.go` —— `ExecEngine.Submit` 必先过 `risk.CheckOrder`：调用存在 + 先于 broker 出口（`SubmitOrder` / `Broker().…`）+ 裁决不得被丢弃（裸调用、全赋 `_`）；HEAD 上唯一实现仍是 K1 契约 stub，stub 走显式登记表，`panic` 被换成任意实现即刻变红。② `pkg/msgbus/publish_record_before_dispatch_test.go` —— BusTap「先记录后分发」：`Tap.Append` 必须早于 handler 分发、且落库失败即刻 return（认不出分发调用时 fail closed，免「先后」在无分发代码上永真）。③ `pkg/strategy/onbar_no_lookahead_test.go` —— `OnBar` 函数体禁正偏移索引/切片、未来语义取数名（`Next`/`Peek`/`Future`/`BarsAfter`…，精确匹配，不误伤 `rejectFutureSignals` 这类反前视名）、遍历 bar 序列容器、给 bar 形参赋值。三者均为 `scanInside` 方法体扫描（不扫原文：三个包里这几句不变量在注释中出现几十处，字符串扫描两头都不可信），各配反瞎腿 + 合成源码正反例腿（证明判据非永真）。破坏腿：Submit 绕过风控 / 先报送后补检 → 红；Publish 反序 / 拆掉落库失败闸门 → 红；`BatchAdapter.OnBar` 注入正偏移索引 → 红（均已还原）。`go test ./internal/repoguard/ ./pkg/...` 61 ok / 0 FAIL；契约签名一行未动。
>
> **遗留**：`StandardExecEngine.Submit` 仍是 K1 契约 stub —— 三条判据当前没有活体对象，护栏真正保护的是「K1 填上实现时必须先过风控」；K1 落地时需删掉对应登记表条目。

- **① 目标**：把三条**只在注释里**的核心不变量变成机器可验证的护栏。
- **② 上下文**：K0 契约的三条关键语义约束目前只有注释——`ExecEngine.Submit` 必须先过 `risk.RiskEngine.CheckOrder`（`pkg/live/interfaces.go:87`）；`msgbus.Publish` 必须先经 EventStore 记录、再分发（BusTap 语义，D4）；`BarHandler.OnBar` 不得回看未来 bar（`pkg/strategy/streaming.go:52`）。项目先例：`internal/repoguard/calendar_seed_isolation_test.go` 的 AST 扫、`cmd/analysis/auth_public_paths_test.go` 的 AST 双向对齐。与 K8 的 SignalProvider 防前视契约同族（未来数据不可见）。
- **③ 要求**：用 AST 护栏钉住三条不变量（每条一护栏，位置随被约束代码所在包）。
- **④ 约束与非目标**：只加护栏、不改契约签名；护栏本身必须破坏验证（改坏→红→还原）；**防「扫原文被注释误报」**——用 `scanInside(FuncName)` 限定函数体，别扫全文。
- **⑤ 验收**：三条不变量各有一条破坏腿（如 Submit 去掉 CheckOrder 调用 → 该护栏红），且反证腿证明护栏不是永真；`go test ./internal/repoguard/ ./pkg/...` 全绿。
- **⑥ 边界**：只做这三条护栏，不扩到其他语义约束，一个 commit。

### K12 · 内核装配治理收口 K1-P2-1~4（阶段 3 前置，独立）✅ 完成（2026-10-12）

> **收口记录 ✅（2026-10-12）**：四条全落地，各配正测试/护栏 + 破坏腿（改坏→红→还原验证）。① Boot ctx 裁决「**ctx 仅限调用期间**」（选契约钉住而非内核传长寿 ctx，保住超时中断能力）——契约注释 `pkg/kernel/interfaces.go` + `TestLifecycleCtxIsCallScoped`；② 回滚期 Stop 失败并进返回 error 的 message（根因为主体、回滚失败并列 %w）——`bootFailedLocked` + `TestBootErrorCarriesRollbackStopFailures`；③ **裁决 pool = 应用层资源（保持现状，谁构造谁释放）**——`adapters.go` 注释 + 蓝图 §4.2 资源归属 + ADR-031（quant-trading-docs）+ `TestEventStoreModuleStopLeavesPoolOpen`；④ 「生命周期方法内禁回调 `Module()`」契约注释 + AST 护栏 `internal/repoguard/module_lifecycle_callback_test.go`（含防注释误报对照腿）。BootOrder 与模块契约签名未动，业务模块逻辑未动。

- **① 目标**：K1/K2 审查登记的四条装配缺陷收口，让 Kernel 装配达到「可托管可插拔执行器」的稳定度。
- **② 上下文**：本文件 §六「K1/K2 实现审查发现」表 K1-P2-1~4（Boot ctx 生命周期 / 回滚 error 静默丢弃 / eventstore 资源归属未定 / 模块回调内核死锁）；`pkg/kernel/kernel.go`（`rollbackLocked`）；`cmd/analysis/kernel_wiring.go`（`assembleKernel`）；ADR-031 D1（EngineRunner 可插拔的稳定性基础）。
- **③ 要求**：① Boot 的 ctx 生命周期修正——内核传长生命周期 ctx，或契约注释显式「ctx 仅限 Boot 期间」并加测试钉住（二选一，裁决理由写注释）；② `rollbackLocked` 的 Stop error 至少 log 并包进返回 error 的 message；③ **裁决** pool 归属（内核资源：模块 Stop 关 / 应用层资源：保持现状）并同步到代码注释 + 设计文档；④ 「模块不得在生命周期方法内回调 `Module()`」固化为契约注释 + 护栏（AST 或编译期约束，破坏验证一条）。
- **④ 约束与非目标**：不改 BootOrder 与模块契约签名（要改须走变更评审）；不动业务模块逻辑；不做内核功能扩展。
- **⑤ 验收**：四条各有一条正测试或护栏 + 各一条破坏腿（改坏→红→还原）；全量 `go test ./...` 绿。
- **⑥ 边界**：只做 K1-P2-1~4 四条，一个 commit。

### K13 · NT 侧车骨架：RunSpec API + 数据馈送（阶段 2，前置：K8–K10）✅ 完成（2026-10-12）

> **收口记录 ✅（2026-10-12）**：四条全落地。① `nt-sidecar/` 独立 Python 项目——`requirements.txt` 钉死 `nautilus_trader==2.0.0rc6`（cp313 Windows wheel 实测可装）+ 服务入口 `python -m sidecar` + README（一条命令启动）；**通道选型裁决 = HTTP/1.1 + JSON**（设计文档 §8.1，标准库 `http.server`，仅监听 127.0.0.1，绑定非回环地址直接拒绝）。② 数据馈送 `tools/export_bars.py`（离线 ETL：`ohlcv_daily_qfq` → Parquet catalog，纳秒 UTC；**侧车服务不直连 DB**，守 ADR-031 D5）；**复权口径裁决 = 前复权原样消费 + UTC 午夜纳秒**（设计文档 §8.2）。③ Go 侧 `pkg/backtest/nautilus.NautilusRunner` 实现注册表要求的 `contracts.EngineRunner`，装配期注册名 `"nautilus"`（`cmd/analysis/setup.go`；地址取 `$NT_SIDECAR_URL`，缺省 `127.0.0.1:9100`）；超时/传输错/非 2xx/`status=failed` 一律透传为 error，**所有失败路径返回 nil response**（不吞错、零静默回退）。④ 链路用最小 buy-and-hold demo 验证（NT **无**内置 buy-and-hold，见 §8.3）。

> **证据**：真装 NT `2.0.0rc6` + 真库导出 2024 年 `600000.SH`/`000001.SZ` 日线（各 242 根）→ 真侧车：`curl POST /v1/backtest` 返回 `status=completed`（484 iterations / 2 orders / total_return −0.1347 / Sharpe 1.4267）；Go E2E（`NT_SIDECAR_URL` 环境门，默认 skip）经真 handler → NautilusRunner → 侧车 → 真 `PostgresStore` **落库并清理**（同数值，确定性复现）。**反证腿**：侧车未启动 → 503 UNAVAILABLE + 库内零行 + go 内核零调用（`TestBacktestPost_NautilusSidecarDownFailClosed`）。**破坏腿**：把 runner 失败分支改成「空 completed + nil error」→ nautilus 包 2 条红、handler 反证腿红（"200" not ≥ "500"），已还原。Python 侧 8/8 绿。
>
> **接口选型澄清**：派工卡写「实现 `contracts.BacktestRunner`」，但 K10 注册表实际承载的是 **`contracts.EngineRunner`**（`RunBacktest(ctx, BacktestRequest) → *BacktestResponse`，见 `registry.go`「接口选型说明」）；`BacktestRunner` 是 AI 管线五参端口，与内核路由无关。本任务按注册表要求实现 `EngineRunner`。
>
> **边界遵守**：只新增 `nt-sidecar/` + `pkg/backtest/nautilus/` + 两个 handler 层测试文件，并改 `cmd/analysis/setup.go` 装配点 + 命名文档；未碰 `pkg/execalgo` / `engine.go` 日循环 / `pkg/live` / `pkg/data`。一个 commit（Python + Go 同 commit）。
> **自审跟进**：K13 交付后按评审清单**自审**，发现 1 条 🔴（`total_return` 口径与 Go 市值口径不一致，判别实验可复现符号相反）+ 1 条 🟠（HTTP/1.1 早退分支不读 body → keep-alive 连接错位）+ 2 条 🟡（`initial_capital` 校验缺口 / `null` 吞成 0），已立 **K13-R1** 工单（紧随本节）。

- **① 目标**：独立 Python 进程（NT 侧车）经回环提供「RunSpec 执行」服务跑通最小链路（NT 内置 demo 策略即可），Go 侧 `NautilusRunner` 注册进 K10 注册表，证明「外部引擎经契约可插拔」在真实 NT 上成立。
- **② 上下文**：调研报告 §3.4/§5.3（唯一干净形态 = 独立 Python 服务；无 Go 绑定、无 gRPC 控制 API、cgo 不可行）；设计文档 §4；NT 现状 v2.0.0rc6（pip wheel，Python 3.12–3.14）；`ohlcv_daily_qfq`（390 万行日线）；ADR-031 D5 安全约束。
- **③ 要求**：
  1. core 仓新增 `nt-sidecar/`（独立 Python 项目：`requirements.txt` **钉死 NT 版本** + 服务入口 + README）：HTTP 或 gRPC（**本任务裁决通道选型，写入设计文档 §8 表第 2 项**）包装 `BacktestNode`/`BacktestRunConfig`，**只监听 127.0.0.1**；
  2. 数据馈送：`ohlcv_daily_qfq` → NT 可消费形态（Parquet 导出，纳秒 UTC 时间戳口径，或低层 `add_data()` 馈送脚本）；**裁决复权口径映射并写入设计文档 §8 表第 3 项**；
  3. Go 侧 `NautilusRunner` 实现 `contracts.BacktestRunner`（调侧车 API；超时/错误 → `failed` 透传，不吞错）+ 装配注册名 `"nautilus"`；
  4. 无策略逻辑——用 NT 内置 buy-and-hold demo 验证链路（RunSpec → 侧车 → 结果 JSON → 落库）。
- **④ 约束与非目标**：ADR-031 D5 全部安全约束（不直连 DB、回环 only、不对外暴露）；不做权重回放适配器（K14）；不做结果指标映射对齐（K15，本任务返回最小字段集即可）；不进生产 compose 主路径（可加 docker-compose.nt.yml 独立样例）。
- **⑤ 验收**：Go 集成测试：`kernel=nautilus` 跑通 demo 回测并落库（正证据）；侧车未启动 → `kernel=nautilus` 显式 failed 且**无 go 内核产物**（反证腿，复用 K10 fail-closed 断言）；`nt-sidecar/` 有可复现启动文档（一条命令起服务）。**破坏验证**：客户端错误吞掉改静默回退 → K10 fail-closed 测试红。
- **⑥ 边界**：侧车骨架 + 数据馈送 + Go 客户端注册，一个 commit（Python 与 Go 同 commit 属同一任务交付）。

### K13-R1 · 侧车结果口径与传输健壮性收口（2026-10-12 自审立单）✅ 完成（2026-10-13，`db3a127`）

> 来源：K13 交付（`c48a2c7`）后的**自审**——按代码评审清单逐项过 + 每条问题做最小复现。K13 的契约链路与失败语义（正/反证/破坏三腿）经实测成立，**本工单不推翻 K13**，只收口 1 条 🔴 口径错误 + 1 条 🟠 传输健壮性缺陷 + 2 条 🟡 校验/映射缺口。**🔴 必须在 K15 双跑对齐之前清掉**：否则对齐基线与 UI 呈现都建立在一个已知错误的字段上。

- **① 目标**：修掉自审发现的 A/B/C/D 四条，使侧车返回的结果与 Go 内核**同口径可比**，传输在误配下不再产生误导性错误。
- **② 上下文**：`nt-sidecar/sidecar/engine.py`（`_map_result`）、`nt-sidecar/sidecar/server.py`（`do_POST` / `_read_body`）、`nt-sidecar/sidecar/runspec.py`（`from_json`）、`pkg/backtest/nautilus/runner.go`（`runResponse` / `toResponse`）。Go 侧口径权威：`pkg/backtest/metrics/performance.go:453-455`（`totalReturn = (endValue-startValue)/startValue`）+ `pkg/backtest/tracker/tracker.go:948`（`totalValue = cash + positionsValue` → **市值口径**）。
- **③ 要求**（按优先级，逐条给修法）：
  1. **🔴 A · `total_return` 口径错误（数据错误）**。现状把 NT `stats_pnls[ccy]['PnL% (total)'] / 100` 当总收益率映射。**判别实验（已跑通、决定性）**：合成日线价格 10.00 → 20.00（持仓翻倍），该值仍为 **−0.10**，恰等于「买入现金流出占本金比」，而 Go 的 `TotalReturn` 是**含持仓市值**的净值口径（tracker.go:948）——两者**符号都可能相反**。该字段经 `SaveSyncResult` **落库**并进 API / UI。修法二选一（**不得保留现有映射**）：**(a) 最小正确**：K13 不冒充该字段——侧车不返回 `total_return`，把余额口径值改名 `nt_balance_pnl_pct` 只作原始统计暴露在 `nt_stats`，`total_return` 留给 K15 用市值口径净值序列计算；**(b) 一步到位**：本工单即按市值口径算（NT 侧取 mark-to-market 净值：`node.generate_account_report` + 持仓市值，或 NT `Portfolio` 的未实现盈亏）。
  2. **🟠 B · 早退分支不读 body → HTTP/1.1 keep-alive 连接错位**。`server.py` 的 404（路径不匹配）/ 413（超大 body）/ 非法 `Content-Length` 三条路径在**未消费请求体**时就返回，而 `protocol_version = "HTTP/1.1"` 维持长连接 → 残留字节被当作下一个请求行解析。**最小复现（已跑通）**：同一连接 `POST /nt/v1/backtest`（带 body）→ 404；紧接着 `GET /healthz` → 实际返回 **400 乱码页**（对照腿：已读 body 的 400 路径下第二个请求正常 200）。**触发条件真实**：`NT_SIDECAR_URL` 若带路径前缀（如 `http://127.0.0.1:9100/nt`），Go 客户端连接池被污染后，后续调用会返回与真实原因无关的次级错误。修法：这三条路径置 `self.close_connection = True`（或读完/丢弃 body 后再回）。
  3. **🟡 C · `initial_capital` 校验缺口**。`inf` / `nan` 通过校验（`> 0` 为真）后在引擎内 `int()` 抛 `OverflowError` / `ValueError` → **500（应 400）**；显式 `0` 被 `or` 短路**静默替换**为默认 1,000,000（与「拒绝而非忽略」的 fail-closed 原则不符）。修法：校验 `math.isfinite(capital) and capital > 0`；「0 = 未指定」写成注释或改 `is None` 判定。
  4. **🟡 D · `null` 被吞成 0**。侧车对非有限指标输出 `null`，Go 端 `runResponse` 用 `float64` 解码 → `null` 变 `0.0`，「未上报」与「真的是 0」不可区分。修法：`runResponse` 的浮点字段改 `*float64`（或 `json.Number`），`toResponse` 显式处理「未上报」。
- **④ 约束与非目标**：不改契约与失败语义（正/反证/破坏三腿必须继续绿）；不扩到 K14 的权重回放范围；不引入新依赖；**不把 🔴 洗成「K15 再说」**——K15 负责做**全**指标对齐，但**不能**让一个已落库字段继续使用已知错误的映射。
- **⑤ 验收**：① A 项：以「价 10→20」判别语境固化断言，证明 `total_return` 不再等于现金流出率（走 (a) 则断言侧车不返回该字段）；② B 项：上述最小复现脚本作为回归腿固化进 `nt-sidecar/tests/`，断言**第二个请求必须 200**；③ C 项：`inf` / `nan` / `0` 三条表驱动断言（400 或显式文档化默认）；④ D 项：非有限指标经 Go 端不再变 0；⑤ K13 三腿 + 全量 `go test ./...` + Python 套件继续全绿。**破坏验证**：把 B 的修复还原 → 回归腿红。
- **⑥ 边界**：只碰 `nt-sidecar/` 与 `pkg/backtest/nautilus/`；一个 commit。

### K13-R1-R1 · 侧车收口残余（2026-10-13 自审立单）✅ 完成（2026-10-13）

> 来源：K13-R1（`db3a127`）交付后的**自审**——对照评审清单逐项过 + 每条最小复现。K13-R1 的 A/B/C/D 与 3 项增项经实测成立，**本工单不推翻 K13-R1**，只收口自审新发现的残余（1 🟠 + 3 🟡）。

- **① 目标**：清掉「静默错值」1 处与传输/守门残余 3 处，使 `completed` 语义与落库字段不再可能承载已知错误值；测试守门不因工作目录/环境而静默失效。
- **② 上下文**：`pkg/backtest/nautilus/runner.go`（`deref`/`toResponse`/响应体读取）、`nt-sidecar/sidecar/engine.py`（`_end_equity`）、`nt-sidecar/sidecar/server.py`（`do_GET`）、`pkg/backtest/nautilus/k14_acceptance_test.go`（fixture 读取路径）。
- **③ 要求**：
  1. **🟠 A · `completed` + `total_return` 缺失仍被折叠成 0 并落库（静默错值）**。链路：`engine._end_equity` 是 best-effort（任一异常 → `total_return=None`，而 run 仍报 `completed`）→ `runner.deref(nil) = 0.0` → `SaveSyncResult` 落库 → API/UI 呈现「0% 收益」。这与 K13-R1 A 要修的「静默错值」是同一类：**读不到的口径被呈现成一个像真的数字**。注意当前 `TestRunner_AbsentMetricFoldsToZeroInDTO` **正在断言这个折叠**，即把该行为固化了。修法二选一（不得继续静默折叠）：**(a) fail-closed**：`status=completed` 但 `total_return` 缺失 → `RunBacktest` 返回显式错误（点名 kernel +「metric not reported」）；**(b) 源头兜底**：`_end_equity` 失败即让该 run `failed`，使「缺字段」在源头不可能发生。建议 **(b) 为主 + (a) 作纵深防御**，并同步改掉那条固化折叠的断言。
  2. **🟡 B · `do_GET` 404 不 drain body**。与 K13-R1 B 同类的 keep-alive 错位（GET 带 body 虽罕见，但部分客户端/代理确实会发）。修法：与 `do_POST` 一致——`_drain_body()` 失败则 `close_connection = True`。
  3. **🟡 C · 侧车响应体读取无上限**。`io.ReadAll(resp.Body)` 无界（K13 遗留，不属 K13-R1 范围）。恶意/故障侧车可让 Go 端内存膨胀。修法：`io.LimitReader` + 明确上限；**注意**上限要与 K15 可能返回的净值曲线体量匹配，不可过小。
  4. **🟡 D · 对拍 fixture 读不到时静默 Skip**。`k14_acceptance_test.go` 用相对路径读 `ashare_fees_parity.json`，失败即 `t.Skipf` → 换工作目录守门即失效（CI 里等同没有）。修法：改 `t.Fatal`，或把 fixture 放进 `testdata/`（Go 惯例路径）。
- **④ 约束与非目标**：不改 K13-R1 已冻结的失败语义（正/反证/破坏三腿必须继续绿）；不扩到 K15 全指标对齐；不引入新依赖。
- **⑤ 验收**：① A 项：构造「`completed` + `total_return` 缺失」的 stub 侧车 → 断言 Go 端**不返回** `TotalReturn=0` 的成功响应（走 (a) 断言报错；走 (b) 在 Python 侧断言 equity 读失败 → `status=failed`）；② B 项：GET 带 body → 404 后**同连接**第二请求必须 200（扩 K13-R1 回归腿）；③ C 项：超上限响应体 → 显式错误、不无界读；④ D 项：在「仓库根」与「包目录」两种工作目录下跑该测试，均不得 Skip。**破坏验证**：还原 A 的修法 → 该腿红。
- **⑥ 边界**：只碰 `nt-sidecar/` 与 `pkg/backtest/nautilus/`；一个 commit。

### K14 · 信号—权重回放适配器（阶段 2，前置：K13）✅ 完成（2026-10-13，`d76cf3d`）

- **① 目标**：NT 侧通用 Python Strategy（权重回放适配器）落地——从 SignalSpec 信号流读目标权重生成调仓订单，含 A 股最小规则集（T+1 / 涨跌停 / 费率），Go 策略资产零改动即能在 NT 上执行。
- **② 上下文**：设计文档 §3（适配器四步：消费→下单→规则→对齐友好）；K7 信号表消费语义（as_of ≤ 当日、首 bar 消费、未来拒绝——与 `pkg/strategy/signal_strategy.go` 一致）；Go 侧规则参照 `pkg/backtest/tracker/tracker.go`（T+1 今昨仓）、`pkg/backtest/pricelimit.go`（涨跌停）、`pkg/fees/ashare.go`（印花税/过户费/最低佣金）；ADR-031 D2。
- **③ 要求**：
  1. 适配器消费：每调仓日读 `as_of <= 当日` 目标权重信号（与 `SignalStrategy` 同一消费语义），目标权重 → 目标仓位 → 调仓订单（`order_factory`）；
  2. A 股最小规则集：**卖单量 ≤ 昨仓可卖量**（今仓不可卖）；**触及涨停不追买、触及跌停不追卖**；费率映射（印花税卖出单边 / 过户费 / 最低佣金 → NT fee model）；
  3. 对齐友好：不做 Go 侧没有的执行增强（禁用冰山单/部分成交默认增强），差异可归因；
  4. ST/新股价格限制、集合竞价是否入最小集：按双跑需要**裁决并写入设计文档 §8 表第 5 项**（可裁「不做」，理由入档）。
- **④ 约束与非目标**：不扩展 NT venue 模型（规则全在适配器下单逻辑）；不做 L2/L3 微观结构策略（超「信号—权重」表达上限，非目标）；不做实盘。
- **⑤ 验收**：合成信号流单测：T+1 反证腿（今仓卖出被拒/截断）、涨停追买反证腿（触板不追单）、费率与 `pkg/fees/ashare.go` 同输入对拍（容差 0 或实测记录）；**破坏验证**：去掉 T+1 检查 → 反证腿红；去掉触板不追单 → 反证腿红。
- **⑥ 边界**：只做适配器与最小规则集（`nt-sidecar/` 内），一个 commit。

### K14-R1 · 权重回放适配器自审收口（2026-10-13 自审立单）✅ 完成（2026-10-13）

> 来源：K14（`d76cf3d`）交付后的**自审**——对照评审清单逐项过 + 每条最小复现。K14 的 T+1 / 涨跌停 / 费率 / D_max 四条反证腿经实测成立，**本工单不推翻 K14**，只收口自审新发现：**1 🔴（多标的资金超配 → 腿静默消失）+ 2 🟠（回执记账 / 整手）+ 4 🟡**。

- **① 目标**：让适配器在**多标的**与**订单被拒**两种情形下不再静默产出错误结果；补齐整手、记账、参数下发、复杂度四处口径。
- **② 上下文**：`nt-sidecar/sidecar/weight_replay.py`（`_buy` / `_sell` / `_submit` / `_record` / `_participation_remaining`）、`nt-sidecar/sidecar/engine.py`（每 symbol 一个策略实例、逐实例传 `initial_capital`）、`nt-sidecar/sidecar/strategies.py`（sample 权重 0.5）、`nt-sidecar/sidecar/signals.py`、`runspec.py`。
- **③ 要求**：
  1. **🔴 A · 多标的资金超配 → 腿静默消失、结果仍报 `completed`（已复现）**。`engine.run_backtest` 对**每个 symbol** 各建一个策略实例，且每实例都拿到**全额** `initial_capital`；sample 权重 0.5 × N 标的 ⇒ 合计承诺 `N × 0.5 × 本金`。**2 标的实测**：`total_orders=7`、`total_positions=1`——`000001.SZ` 5 次买单全被 NT 以现金不足拒绝，只有 `600000.SH` 成交，而响应仍是 `completed` + 一个像真的 `total_return`（−0.00056）。修法：把权重解释为**组合总权重**并在适配器内做组合级资金预算（正解），或最小修 = 按标的数分摊 `capital_base`（`initial_capital / len(stock_pool)`）。**必须同时**补 B 的回执处理，否则分摊后仍会静默退化。
  2. **🟠 B · 订单提交无回执记账（被拒当成交 + 每 bar 重试）（已复现）**。`_submit` 无条件 `self._sellable -= qty` / `self._traded_today += qty` 并记 `"submitted"`，不看 NT 是否接受/成交；目标仓位又取自 `_net_position()` ⇒ 被拒后**每根 bar 重复提交**（实测 `000001.SZ` 连续 5 次）。修法：接 `on_order_rejected` / `on_order_filled`（或与 `cache` 对账）回填真实成交量；`_traded_today` 口径改「已**成交**」而非「已提交」（参与率本就是成交口径）。**注**：同一条路径也覆盖「NT 侧拒单」（现金不足、非交易时段）——拒单必须有可观测记录，不能只进日志。
  3. **🟠 C · 参与率 cap 未按整手再取整（已复现）**。`_buy` 的 `qty = min(delta, self._participation_remaining(bar))`：`delta` 已整手对齐，但 cap 可以是任意股数。实测 `participation_cap(1_050, 0.10) = 105` ⇒ 提交 **105 股**买单，违反本单已声明的「买入整手」。修法：买入侧 `qty = ashare.round_down_to_lot(min(...))`（卖出保留零股——A 股允许卖出零股）。
  4. **🟡 D · `ReplayDecision.submitted` 恒为 0（死字段）**。`_record` 硬编码 `submitted=0.0`，测试只能解析 reason 字符串 `"submitted 100"` ⇒ 结构化字段与测试脱钩、改文案即碎。修法：`_submit` 填 `submitted=qty`，测试改读字段。
  5. **🟡 E · `d_max` 无法从 Go 侧下发**。「外生参数」当前只能吃侧车默认 0.10（Go `runRequest` 无该字段，K14 边界内未改 Go）⇒ 实际上**不可外在设置**。修法：在 `contracts.BacktestRequest` 增可选字段并透传（走契约变更评审），或明确记为「侧车默认值、暂不可外置」并写入设计文档 §8.4。
  6. **🟡 F · `SignalStream.as_of` 每 bar O(N) 全扫**。真实信号表（K7 `quant.external_signals`）量级下为 O(N×B) 热点（与 K7 遗留③ 的游标去重同源）。修法：预排序 + 游标/二分。**无数据不报 🟠**——修前先量样本量级，量级小则维持现状并记档。
  7. **🟡 G · 目标仓位用期初本金 ⇒ 恒定名义额再平衡**。`capital_base × weight / price` 在价格变动时每 bar 向「恒定名义额」再平衡（涨则减仓、跌则加仓），与「目标权重占实时净值」的直觉语义不同，会产生非预期换手并影响 K15 归因。需**明确裁决**（改实时权益 / 显式声明为恒定名义额）并写入设计文档 §8.4。
- **④ 约束与非目标**：不改已验证的四条反证腿（T+1 / 涨跌停 / 费率 / D_max 必须继续绿）；不做 K15 全指标对齐；不做实盘。
- **⑤ 验收**：① A 项：2 标的 end-to-end 断言**两腿都有仓位**，且不再出现「腿被拒但报 completed」；② B 项：注入一次拒单 → 断言不重复提交、`_traded_today` 只计成交；③ C 项：`cap=105` 场景断言提交量为整手（100）；④ D 项：测试读 `submitted` 字段而非字符串；⑤ E/F/G 项：结论（含「维持现状」的裁决）落设计文档 §8.4。**破坏验证**：还原 B 的回执记账 → 重复提交腿红；还原 C 的整手取整 → 105 股腿红。
- **⑥ 边界**：只碰 `nt-sidecar/`（以及 `pkg/backtest/nautilus/` 里对应的测试），一个 commit。

### K14-R1-R1 · 回执记账的两处复查（2026-10-13 K14-R1 自审立单）✅ 完成（2026-10-13）

> 来源：K14-R1 交付后的自审——用探针实测「fill 回调是否真到达 handler」时发现的两点。

- **① 目标**：给「成交喂参与率预算」补**正证据**测试腿；把「在途子单不预留预算」的口径记档。
- **② 现状**：
  - **R1（🟡 测试缺口，正证据缺失）**：K14-R1 对 B 的验收只有**反证**（注入拒单 → `_traded_today == 0`）。**没有**任何腿证明「正常成交后 `_traded_today` 按成交累加」。探针实测：`on_order_filled` 确实被调用且量正确（两次 50,000），**但**若将来 NT 的 `OrderFilled` 属性名漂移（`last_qty` 改名等），handler 会抛异常而 **NT 会吞掉回调异常只记 ERROR 日志**——`_traded_today` 恒 0 ⇒ 参与率 cap 与 T+1 可售量**静默失效**，而现有全部测试仍绿（现有 cap 腿是单日单 bar，traded 恒 0 也能过）。
  - **R2（🟡 口径记档）**：`_traded_today` 改「已成交」口径后，`_participation_remaining` 只减 `_traded_today`、**不预留在途（`_pending`）子单**。实测当前 NT bar 撮合语义下 order 在同一 bar 内即结清（submit → fill 相邻），故**不可达超额**；但该依赖未写进注释，未来换撮合模型（或引入跨 bar 未结清）时会成为静默超额点。
- **③ 要求**：
  1. **R1**：新增正证据腿——**同一日内两根分钟 bar**（volume=1,000/bar，`d_max=0.10` ⇒ cap=100）：首根提交 100 并成交；第二根 `_participation_remaining` 必须已被成交消耗 ⇒ 决策为 `participation cap reached`、**当日总提交仅 1 笔**。若 fill 未喂预算（回调抛错/属性漂移），第二根会再提交 100 ⇒ 本腿红。
  2. **R2**：在 `_participation_remaining` 注释里写明「只减已成交、不预留在途」的口径与「同 bar 结清」的前提（改撮合模型需重估）。
- **④ 约束与非目标**：不改参与率口径（本单只补测试与注释）；不改回执记账逻辑本身。
- **⑤ 验收**：新腿绿；破坏腿——把 `on_order_filled` 的 `self._traded_today += qty` 注释掉 → 新腿红（当日变成 2 笔提交）；Python 全套绿。
- **⑥ 边界**：只碰 `nt-sidecar/`；一个 commit。
- **⑦ 收口记录（2026-10-13）**：新增 `TradedBudgetFedByFillsTest.test_fills_consume_the_daily_participation_budget`（同日两根分钟 bar，cap=100：首根提交并成交 100，第二根判「cap 已达」，当日仅 1 笔）；`_participation_remaining` 补 R2 口径注释。实测结论：fill 回调**确实到达** handler 且**同 bar 结清**（探针：submit→fill 相邻），故 R2 只需记档、不必改法——新腿同时是该前提的回归护栏。破坏腿：注释掉 `self._traded_today += qty` → 新腿红（`1 != 2`，当日 2 笔，实测）。Python 全套 44 绿。

### K15 · 结果映射 + 双跑对齐实验（阶段 2，前置：K14）✅ 完成（2026-10-13）

> **⑦ 收口记录（2026-10-13）**：三件套一次交付——① **映射层**：侧车 `engine.py` 的 `_map_result` 扩全数据面（`equity_curve` 日净值合成 = cash(d)+Σ qty×close，与 Go tracker `totalValue` 同构；`trades` 逐笔 fills 映射；status 三值；completed 但曲线合成失败 → failed fail-loud）+ `runspec.py` 的 `signals` wire 解析（fail-closed，与 Go `ValidateRunSpec` 逐字同口径）；Go `runner.go` 数据面落入冻结字段 `PortfolioValues/Trades`（无 schema 新增）。② **对齐工具** `cmd/dualrun`：Go 侧权重回放执行器（`wrreplay.go`，决策规则与 `weight_replay.py` 逐字对拍——恒定名义额分摊 capital/N / 涨跌停不追 / T+1 / 整手 / D_max；撮合记账走 Tracker+ComputeFees 内核既有实现）+ 同一 `metrics.GenerateBacktestResult` 算两侧指标 + 逐指标/逐笔 diff 报告。③ **实验**：2024 全年两标的（主板）× 2 信号流（bh/quarterly），确定性腿两侧 ✅，total_return 差 -0.086%/-0.076%、sharpe 差 ≤0.0023、maxDD 差 ≤0.35%，matched 成交 363/328、无机制性缺笔；报告落 [docs/design/kernel/k15-dualrun-report.md](../design/kernel/k15-dualrun-report.md)（+ 两份机器生成数据文件），容差阈值实测回写设计文档 §8 表 1 ✅。**破坏腿实测红**：① wrReplay 整手 round→Round（`1 != 3` 红）② 涨停 no-chase 短路（`1 != 2` 红）③ diff 报告删 NT 一笔成交 → go_only 点名（单测钉住）④ 侧车 equity 合成缺 account 报告 → failed（单测）。**正证据**：Python 59 绿 / Go 全量绿。**归因与遗留**：价源精度（PG 浮点 vs catalog tick）为路径分叉主因；NT fills 费用 ≈ 纯佣金（过户费分量未体现）→ 立 **K15-R1**（复查结论：为对账展示 bug，NT 费用实际全项正确，见 K15-R1 ⑦）；win_rate 配对敏感性如实记档不设硬阈；费率口径裁决 b 项执行（Tracker+ComputeFees 路径，§8.4 记档）。

- **① 目标**：NT 报告 → ResultSpec 映射层落地；产出**同信号流、双内核**的对齐报告（逐指标 diff、差异归因可读），容差阈值实测确定并回写设计文档。
- **② 上下文**：设计文档 §2.3/FR-6；K5 信号对账方法论（`pkg/live/signal_reconcile.go` 的「同输入对差异归因」思路）；指标口径 `pkg/backtest/metrics/performance.go`（封闭系统日收益）；K4 冲击/费率基线。
- **③ 要求**：
  1. 映射层：NT pandas 报告（orders/fills/positions/equity/stats）→ ResultSpec（收益率/夏普/最大回撤/交易明细/净值 + status 语义）；
  2. 对齐工具：同一信号流（K7 信号表）+ 同一时间窗 + 同一费率配置 → Go 内核与 NT 侧车各跑一次 → 逐指标 diff 报告（含交易明细级对账）；
  3. 实验：**至少 2 只策略 × 1 个年度窗口**（含一只含 T+1/涨跌停触发的）；差异逐项归因（费率 / T+1 / 涨跌停 / 撮合机制）；
  4. 容差阈值按实测分指标确定，**写回设计文档 §8 表第 1 项**（不拍脑袋）。
  5. **口径遗留认领（2026-10-12，K13-R1/K14 收口）**：Go 回测**撮合路径只计佣金**（`pkg/execution.CostModel` / `pkg/backtest/execution`，代码注释写明的已知口径边界），**不建模印花税/过户费**；而 K14 的 NT fee model（`nt-sidecar/sidecar/ashare.py::AShareFeeModel`）计**全三项**。双跑 diff 若在费率上系统性偏离，必须**显式归因到该口径边界**（设计文档 §8.4 已记录），**不得被容差掩盖**；裁决：要么先把两侧费率口径对齐再比，要么在报告里单列该差异并说明。
- **④ 约束与非目标**：实验可复现（确定性回放/固定输入，双跑各跑两遍结果一致）；不改 Go 内核任何数值；容差不得放宽到「掩盖真实差异」——差异解释不了就如实记录为开放问题；不做生产化（K16）。
- **⑤ 验收**：对齐报告落 `docs/design/kernel/`（或 `docs/archive/`，含数据、命令、结论）；映射层单测（含 status 映射）；**反证腿**：人为注入一处差异（如改费率）→ diff 报告捕获并点名。
- **⑥ 边界**：映射层 + 对齐工具 + 实验报告，一个 commit。

### K15-R1 · NT fills 费用链路复查（K15 实验发现，2026-10-13 登记）✅ 完成（2026-10-13）

- **① 目标**：定位并修复「NT 侧车 fills 报告的 commission ≈ 纯佣金、过户费分量缺失」的链路疑点，使 NT 侧费用与 `ashare.compute_fees(...).total` 逐笔一致，对齐阈值可收紧到 total_return |Δ| ≤ 0.1%。
- **② 上下文**：K15 实验（[k15-dualrun-report.md](../design/kernel/k15-dualrun-report.md) 归因②）实测：Go 侧每笔费用 = ComputeFees 全三项精确对上（例 7.5662×33000 → 77.40 = 佣金 74.91 + 过户 2.50）；NT 同一笔 fills 报告 commission = 74.91（= 纯佣金，`ashare.make_fee_model` 声明返回 `compute_fees(...).total` 但实际未含过户费）。疑点在 FeeModel 返回的 Money → NT 撮合 → `generate_fills_report` 的 commission 列之间的链路（被覆盖 / 只取部分 / Money 精度截断）。工具：`nt-sidecar/.venv/Scripts/python.exe` + NT 2.0.0rc6 源码（site-packages 内 Rust/PyO3）。
- **③ 要求**：
  1. 最小复现：单笔已知 fill（qty/px 已知）断言 NT fills 报告 commission == `compute_fees(v, is_sell).total`（当前实测不等，差一笔过户费）；
  2. 定位根因（NT FeeModel 覆盖 / fills 报告取值口径 / Money 精度），修复落代码——若根因在 NT 库内部且不可修，则改用**对账层补偿**（Go diff 工具按 is_sell 单侧加回过户费）并在注释与报告里写明是补偿不是修复；
  3. 重跑 K15 两实验（bh/quarterly），费用列逐笔对齐，报告数据文件更新，总对齐差收紧记录。
- **④ 约束与非目标**：不改 `ashare.compute_fees` 公式与 parity fixture（那是与 Go ComputeFees 的冻结对拍）；不追求与 NT 内部账户余额路径对账（只对齐 fills 报告这一数据面）；不扩到 K16 CI 门禁。
- **⑤ 验收**：单笔复现测试绿；重跑两实验的 fills 费用列 matched 行 fees: go == nt（或补偿后差值 = 0）；`go test ./...` 绿 + Python 全套绿；K15 报告数据文件与阈值备注更新。
- **⑥ 边界**：`nt-sidecar/sidecar/`（或 diff 补偿在 `cmd/dualrun`）+ 报告数据文件更新，一个 commit。
- **⑦ 收口记录（2026-10-13）**：**根因不在 NT FeeModel，在对账报告的展示层**——`cmd/dualrun/diff.go` matched 行的 `nt=` 费用误用**循环变量（Go trade）的 Commission 分量**（ComputeFees 的纯佣金 74.905），而非 NT trade 的全项 total（77.44）；「NT 缺过户费」是假象。三层实测定位：① FeeModel 单元（`get_commission` 返回 total ✓）② 单笔真 run（直调 + HTTP 双路径 fills commission = 77.44 全项 ✓，且账户 cash 与 K15 首轮报告一字不差——实际扣费从来是全项）③ server 端 RAW FILLS dump（fills 原始值正确）。**修复**：diff.go 用 `ntBy[k]` 取 NT trade 自身费用对账；重跑 bh/quarterly 费用列逐笔对齐（77.40/77.44、40.24/40.24、5.03/5.03——残差 = 价源精度差）。**回归护栏**：`tests/test_k15r1_fees.py`（FeeModel 单元 2 腿 + 端到端 `FillReportFeeRegressionTest`：fills commission == compute_fees.total 逐笔断言）。**附带发现（运维教训）**：排查中本机 9100 端口曾同时存在两个 sidecar 监听进程（Windows 允许端口复用绑定、连接随机分配），已清理；「同请求不同结果」先查端口占用者。**排查辅助代码已清理**（DUALRUN_DUMP_REQ / SIDECAR_DUMP_FILL_FEES）。Python 63 绿 / Go 全量绿。K15 报告 §3/§5 与设计文档 §8.4 记档同步更新；阈值维持 §8 表 1（total_return |Δ| ≤ 0.2%——主导因素是价源精度，费用对齐后不再有收紧空间，除非两侧价源精度先统一）。

### K16 · 双内核生产化（阶段 3）✅ 完成（2026-10-13，NT 决策门裁决：基于 2.0.0rc6 先行开工，正式版发布后按升级预案收口）

> **进度（2026-10-13）**：① CI 对齐回归门禁✅（`522c11b`）② compose+健康检查+监控✅（`7ee2626`，§8.6 部署形态裁决已回写设计文档）③ 运维文档✅（docs 仓 `5adda21`，`guides/nt-sidecar-operations.md`：升级流程首节三条纪律+三个 API 盯防面+对齐回归验收门；故障回退「零回退是结构属性」+临时退 go 内核路径；微坑 5 条）④ 生产路由开关✅（`34621c6`）⑤ **生产告警通道接线✅（`9502ec2`，K5 遗留认领收口）**——`pkg/alert` 增 `ReconciliationDispatcher`/`SignalDispatcher`（一条差异一条告警，severity 同名词表映射、未知 fail-closed 到 warning；`DispatchDualRunBreaches` 对齐 `GateViolation` 信息面全 critical），`cmd/analysis` 首个 reconciliation worker 生产构造点（`buildReconciliation`，自检形态对账、`FeeSnap` 恒零缺口记档、陌生账户 fail-loud），`cmd/dualrun -gate` 违例经 `dispatchGateAlerts` 进告警通道（`$DUALRUN_ALERT_WEBHOOK`）；验收「人为注入对账差异 → 告警触发点名」由装配演示测试兑现（现金/持仓逐条点名 account/symbol/delta），破坏验证（不转发 → 红）闭环。

- **① 目标**：决策门通过后，双内核形态进生产：kernel 路由可控、对齐回归进 CI 门禁、Python 运行时进 CI/CD、监控与运维文档齐备。
- **② 上下文**：ADR-031 D4 阶段 3；§五「NT 决策门」；K12–K15 交付物（K12 装配治理是「可托管可插拔执行器」的稳定性前提）；CI 现状（Go `-race` + 前端 typecheck/lint/test）。
- **③ 要求**：① 对齐回归测试进 CI 门禁（Go + Python，双跑 diff 超阈即红）；② 侧车部署形态（compose 服务 vs 独立进程，裁决并回写设计文档 §8 表第 6 项）+ 健康检查 + 监控指标（侧车延迟/失败率/回退次数=0 断言）；③ 运维文档（NT 版本升级流程、故障回退流程）落 `quant-trading-docs/docs/guides/`；④ 生产路由开关（默认 `go`，`nautilus` 显式开启）；⑤ **K5 遗留认领（2026-10-12）**：生产告警通道接线——对账/双跑差异超阈经 `AlertDispatcher` 告警（现有 `ReconciliationWorker`/`AlertDispatcher` 零生产构造点，K5 切片 2 只以差异列表 + fail-loud 兑现）。
  ⑥ **部署形态遗留认领（2026-10-12，K13-R1 交付）**：`docker-compose.nt.yaml` + `nt-sidecar/Dockerfile` 是 K13-R1 的**独立样例**，**未在任何 Docker 环境实测**（交付时本机 Docker 未运行），且 ADR-031 D5 回环约束下容器内 127.0.0.1 跨网络不可达（样例用 `network_mode: host`，Linux/Windows 差异已注明）。裁决 §8 表第 6 项前**必须先实测该 compose**（起容器 + 跑通一条 `kernel=nautilus` 回测）。
- **④ 约束与非目标**：ADR-031 D5 安全约束全部有效；不迁移编排到 NT；不接真实券商（D1）。**NT 版本纪律（2026-10-13 开工裁决，详见设计文档 §8.5）**：① `nautilus_trader==2.0.0rc6` 精确 pin，不追 nightly；② 「NT 版本升级流程」进运维文档首节（升级工单以对齐回归为验收门）；③ 升级时只盯三个 API 面（Backtest 配置族签名 / fee model 接口 / 三个报告 API）。
- **⑤ 验收**：CI 全绿含对齐回归腿；部署文档按文档起侧车 + 跑通一条 `kernel=nautilus` 回测；监控面板/告警演示（人为停侧车 → failed 显式、零回退；人为注入对账差异 → 告警触发点名）。
- **⑥ 边界**：生产化收尾，一个 commit。
- **⑦ 收口记录（2026-10-13）**：六件套全部收口（①`522c11b` ②`7ee2626` ③docs 仓 `5adda21` ④`34621c6` ⑤`9502ec2` ⑥含于 ②）。**⑤ 交付面**：`pkg/alert/reconcile.go`（`ReconciliationDispatcher`/`SignalDispatcher`，一条差异一条告警、severity 同名词表映射、未知 fail-closed 到 warning、`RuleDualRunGate`）+ `cmd/analysis/setup_reconciliation.go`（reconciliation worker **首个生产构造点**，此前零构造点；自检形态对账 = MockTrader 双接口同体，真实券商落地只换 broker 侧一个类型；`FeeSnap` 恒零缺口文件头记档；陌生账户 fail-loud）+ `cmd/dualrun` `dispatchGateAlerts`（`-gate` 违例 → 告警通道，`$DUALRUN_ALERT_WEBHOOK`）。**验收证据**：`TestReconciliation_InjectedDiscrepancyFiresNamedAlert`（注入现金 −500 / 持仓 −100 → 告警逐条点名 account/symbol/Value=+500/+100，Delta=local−broker 口径）+ `TestGateAlerts_ProductionFireDeliversAlerts`（真 webhook 送达，rule=dualrun_gate 全 critical）；破坏验证（`dispatchGateAlerts` 不转发 → 双腿红）闭环；`go test ./...` 全量绿。**③ 交付面**：`guides/nt-sidecar-operations.md`（升级流程首节 = 三条纪律 + 三个 API 盯防面 + 对齐回归验收门；故障回退 = 「零回退是结构属性」+ `NT_SIDECAR_ENABLED` 临时退 go 是唯一合法回退路径；微坑 5 条含 Windows 端口复用）。**测试环境已清理（2026-10-13）**：`quant_ci_test` 库 DROP、`%TEMP%\ci_catalog` 删除、nt-sidecar 测试容器 stop+rm、9101 残留侧车进程（PID 6544/28020）kill 后端口确认释放——呼应 §8.5.3/§8.6 的「停侧车后查端口」运维教训。**遗留**：无 K16 内遗留；升级预案已按 §8.5.2 交付（运维文档首节），正式版发布后立升级工单按其执行。

### K17 · exec-algo 接进下单主路径（独立，K4 遗留收口）✅ 完成（2026-10-12）

> **收口记录 ✅（2026-10-12，`6205c64`）**：`pkg/execalgo`（TWAP/VWAP，此前全仓零生产引用）接进两条下单主路径，11 文件 +1851/−4，一个 atomic commit。落点：`pkg/execalgo/dispatch.go`（多父单派发器 Dispatcher：Attach/OnBar/OnFill/Unfinished + `ResolveExecWindow`）+ `pkg/backtest/exec_algo.go` + `pkg/live/paper_exec_algo.go`；契约侧 `domain.Order/Signal`、`strategy.Signal` 增可选 `exec_algo`/`exec_slices`/`exec_profile`（omitempty，零值=整单直下）。关键裁决：父单号 `EA-日期-symbol-方向` 纯函数（禁时间戳）；窗口 = n 个自然日、步长恰 1 日；会话结束未报完子单**显式裁决=撤销**（强撮=以最后 bar 虚构成交）两路径一致；超额 fail-loud 在 Dispatcher 层补（算法 planState 只钳制）；`ExecAlgorithm`/`LiveTrader` 冻结签名未动。**正证据**：回测 e2e TWAP 7 片 → 撮合层 7 笔子单、Σ=10000=同策略整单量、7 个不同交易日、总冲击 189.1 vs 整单 500.0（**占比 0.3783 ≈ 1/√7**，D5 正证据）；paper e2e 同构（占比 0.3783）；VWAP 按 profile 加权；混合运行中无算法信号仍 1 笔整单。**破坏验证**：① 强制整单 → 「7 笔子单」红；② OnFill 不回填 → 会话结束 CANCELLED 警告红；③ 子单号时间戳派生 → K4 幂等测试红（均改坏→红→还原→绿）。默认路径既有全量测试零改动全过（execalgo/backtest/live/domain/strategy/portfolio）。
>
> **审查与遗留**：对照代码评审清单自审通过（API 真实性/并发/确定性/vet+gofmt）；🟠 发现「窗口内外部仓位变动（止损/强平/拆股）不取消活跃父单 → 拆单续买」已按六字段立为 **K18**（`7e5a963`）——K17 边界内不做，属拆单固有语义（业界解法 cancel-on-stop）。🟡 三件（OnBar 出错丢弃已取子单 / PaperSession 重复 Run 撞父单号 / paper 侧 attach 未贯通 ctx）已按六字段立为 **K17-R2**（随 K17-R1 落库后执行）。本机无 gcc，`-race` 未跑（环境限制）。

- **① 目标**：把 K4 交付的 `pkg/execalgo`（TWAP/VWAP/Schedule 拆单）接进下单主路径（回测撮合 + paper），使 D5「模拟真实大单」真正闭环；**未指定执行算法的订单行为逐字节不变**。
- **② 上下文**：K4「未做」条（本文件 K4 区块，原写「归 K5」但 K5 切片未含，2026-10-12 代码核实 `pkg/execalgo` 全仓零生产引用，仅自身测试）；`pkg/execalgo/`（`ExecAlgorithm`：`OnBar`/`OnFill`/`Schedule()`/`DueChildOrders()` 取走即清空；TWAP `Σ子单量=父单`、子单号 `(父单号,片序号)` 纯函数幂等；VWAP profile 退化 → 等分 + `Degraded` 标记）；下单路径：`pkg/backtest` 撮合（`BacktestExecutionService`）与 `pkg/live`（`MockTrader`/`SimulatedBroker`/`OrderManager`、`PaperSession` 日循环）；K5 共享成本核 `pkg/execution.CostModel`（fixed/variable/impact/none）。
- **③ 要求**：
  1. 订单/下单请求增加**可选**执行算法指定（如 `exec_algo: twap|vwap` + `slices` / `profile` 参数）；未指定 → 整单直下，默认路径数值逐字节不变；
  2. 回测与 paper 两条下单路径在「订单送撮合前」经 `ExecAlgorithm` 拆单：按 bar 到点取 `DueChildOrders()` 逐笔送撮合、`OnFill` 回填进度；子单走同一 `CostModel`（含 impact——大单拆细后单笔冲击下降，正是 D5 要模拟的效果）；
  3. 父单状态一致性：`父单量 = Σ子单成交量 + 待报子单量`，超额 fail-loud；会话/回测结束仍有未报完子单的处置（撤销或强制撮完）必须显式裁决并写入实现注释；
  4. 确定性：同一输入两次拆单结果一致（复用 K4 幂等语义，禁止时间戳派生 ID）。
- **④ 约束与非目标**：默认（无 `exec_algo`）行为与数值零变化（回归腿钉住）；真实券商算法单不做（D1）；VWAP 成交量预测不做（沿用给定 profile，K4 登记项）；不改 `ExecAlgorithm` 既有接口签名；不动 NT 侧车（K14 的权重回放适配器是另一条转化路径，互不影响）。
- **⑤ 验收**：e2e——指定 TWAP 拆 7 片的大单 → 撮合收到 7 笔子单、`Σ=父单`、时间片均匀、大单总冲击成本低于同量整单（D5 正证据）；默认路径回归腿全绿（无 `exec_algo` 的既有回测/paper 测试零改动全过）；**破坏验证**：① 指定 `exec_algo` 后强制整单 → 拆单断言红；② `OnFill` 不回填 → 进度/超额 fail-loud 断言红；③ 子单号改时间戳派生 → 幂等断言红。
- **⑥ 边界**：只做下单主路径接线（回测 + paper 两条），一个 commit。

### K18 · exec-algo 外部仓位变动取消活跃父单（独立，K17 审查发现，2026-10-12 登记）✅ 完成（2026-10-12，`7a3576e`）

- **① 目标**：exec-algo 父单执行窗口内发生**外部仓位变动**（止损/止盈平仓、退市强平、拆股改仓）时，取消该 symbol 活跃父单的剩余未报送子单，使「止损后应空仓」的策略语义不被拆单续买破坏。
- **② 上下文**：K17（commit `6205c64`）审查发现的已知边界：`pkg/backtest/exec_algo.go` 的 `processExecAlgoChildren` 与 `pkg/live/paper_exec_algo.go` 的 `dispatchExecAlgoChildren` 按计划独立报送子单，**不感知**日循环里后置的 `processStopLosses` / `forceCloseDelisted` / `processCorporateActions` 对仓位的改动——第 k 天止损平仓后，第 k+1..n 天的子单仍照报（把刚被风控平掉的仓位重新买回）。整单路径无此问题（当日一次成交，无跨日残留委托）；真实 TWAP 的业界解法是 cancel-on-stop 钩子。现有可复用基础设施：`execalgo.Dispatcher.Unfinished()`（会话结束裁决输入）+ 两条路径的撤销裁决（K17 注释），缺的只是「窗口中途取消」入口。
- **③ 要求**：
  1. `execalgo.Dispatcher` 增加**窗口中途取消** API（如 `Cancel(parentID, reason)`）：剩余**未报送**子单丢弃（不再产出）、**已报送已成交**部分保留（不回滚）；被取消父单不再进 `Unfinished()` 的会话结束 CANCELLED 告警（或带 cancel-reason 显式区分「窗口中取消」与「会话结束撤销」）；
  2. 回测侧：`processStopLosses` / `forceCloseDelisted` / `processCorporateActions` 实际改仓（有成交落账）后，对受影响 symbol 的活跃父单触发取消（改动点在 engine.go 日循环与 processStopLosses 的成交分支）；
  3. paper 侧同一语义、同一裁决、同一 cancel-reason 日志；
  4. 取消后 `TargetPosition`（ActualQty / PendingQty）与实际持仓一致（对齐 AUD-55 的「引擎外改仓」教训）；
  5. 不改 `ExecAlgorithm` / `LiveTrader` 冻结签名。
- **④ 约束与非目标**：默认路径（无 `exec_algo`）行为与数值零变化（回归腿钉住）；不做部分成交回滚；已报送未成交子单的 broker 侧撤单不做（回测/paper 中子单即时全额成交，无此状态）；不接真实券商算法单（D1）。
- **⑤ 验收**：e2e——TWAP 7 片窗口中段注入止损触发 → 后续子单不再报送、最终持仓与「整单 + 止损」对照路径一致；日志带 cancel-reason、会话结束不再对已取消父单报 CANCELLED；paper 侧同构断言；**破坏验证**：去掉取消钩子 → 「止损后不再续买」断言红。
- **⑥ 边界**：只做「外部仓位变动 → 取消活跃父单」钩子（回测 + paper 两条），一个 commit；不动 K17 的拆单/成本/会话结束裁决逻辑。

---

## 三、最高优先级独立工单

### OBS-01 · 空票池假成功必须 fail-loud ✅ 完成（2026-10-09，两切片）

> **切片 1 ✅（2026-10-09）**：裁定 + 报告层 + 自动化层落地 —— `pkg/backtest/contracts/validity.go`（纯函数 `CheckValidity`，4 个稳定 Code：`empty_universe` / `zero_trades` / `zero_start_date` / `garbage_metric`，垃圾阈值 `|x|>1e12` 网住 MaxFloat64/NaN/±Inf）；`domain.BacktestResult` 增 `UniverseMaxSize` + `InvalidReasons`（引擎算一次、下游只读不重算）；引擎**正常路径行为不变**、无效时日志不再无条件报 completed 且同步响应 `Status="invalid"`；`pipeline`×2 / `job` 不再宣告 successfully（job 走 failed + error_msg）；**`loop` 的 `a.OK` 加裁定前置 ⇒ 无效运行按 failed 计、伪造观测不再进 `history`/`observed`**（否则 `adjP=1-(1-rawP)^N` 算出的 p 值是假的）；验证器新增 `universe` 维（空 → 进 Dimensions 且 0；非空 → 不进 map）。真库证据（只读）：空票池 `status=invalid`、reasons 三条；反证腿（真库 2024 全年 3 票）`status=completed`、25 笔成交不受影响。
> **切片 2 ✅（2026-10-09）**：读路径收口 —— `state.SetStatus` 按裁定置 `"invalid"`（有效仍 `"completed"`）；`GetBacktestResult` 状态闸门放行 `{"completed","invalid"}`（**无效运行必须可读**，否则「为什么无效」在 UI/报告层不可见，调用方只拿到 409 会把「跑完但无效」误报成「还没跑完」）；抽出 **`buildResponseFromState`** 作为「引擎内存态 → API 响应」的**唯一**透传点，`GET /:id/report` 与 `lookupBacktestResponse` 都改调它 —— 消灭「两处各自内联重建、各自硬编码 Status」的病根结构（这才是 OBS-01 的成因）。handler 层 httptest + engine 断言 + 真库集成三处取证；15 包全绿。
> **遗留（2026-10-12 已并入 K9，§二）**：① `pkg/backtest/reporting/compare.go:130` 第三处硬编码 `Status`（invalid 读不到 reasons）；② **UI 仍按成功呈现指标**（无人读 `BacktestResult.status`，渲染判据是 `result.id`）——两项均归 K9「status 全链路收口」。

- **① 目标**：回测/验证在「0 交易 / 空票池」时必须 **fail-loud**（报错或显式标记），不得报成功并产出伪造观测值。
- **② 上下文**：ADR-030 OBS-01（`docs/adr/adr-030-draft-observed-improvements.md`）；实测：`total_trades=0` + `start_date=0001-01-01`（零值）+ `sortino_ratio=1.797e308`（MaxFloat64 垃圾值）仍报成功；根因 `index_constituents`/`sectors`/`stock_sector_map` 全 0 行 ⇒ YAML 默认 `universe: csi300` 解析为空票池；`pkg/ai/loop/loop.go` 会把这些伪造观测计入 `NumTrials` ⇒ **多重检验校正（`pkg/validation/statistical.go` `adjP=1-(1-rawP)^N`）算出的 p 值是假的**。
- **③ 要求**：回测完成时校验「票池非空 ∧ 成交数 > 0 ∧ 日期非零值」，任一不满足 → 标记为**无效运行**（非成功）；验证器新增「票池非空」维（ADR-028 把验证器从 5 维涨到 8 维但仍无此维）；伪造观测不得计入 `NumTrials`。
- **④ 约束与非目标**：改动小、独立；不改变正常回测的行为；不趁机重构验证器链。
- **⑤ 验收**：空票池场景 → 明确报「无效运行」而非成功 + 垃圾指标；正常回测不受影响；**正证据**：造一个「只有票池为空才触发」的场景 + 反证腿（票池非空时不触发）；伪造观测不再进 `NumTrials`。
- **⑥ 边界**：只做「空票池 fail-loud」这一件事，不动验证器其他维度、不动 loop 其他逻辑。

---

## 四、OBS 独立工单（未被设计吸收，源自 ADR-030）

> 完整描述见 ADR-030（`quant-trading-docs:docs/adr/adr-030-draft-observed-improvements.md`）。按依赖与价值排序，开工前各自按 §8.5 补全六字段。

| 工单 | 一句话 | 优先级 |
|---|---|---|
| OBS-06 ✅ 完成（2026-10-09，随 K3 切片 2 合并） | DSL 语法闸门不校验算子名 —— 已在 `Expression.Validate()` 补算子/字段/参数个数闸门（fail-closed，报错带位置与可用清单），`validate_factor` 解析后必过闸门 | — |
| OBS-07 ✅ 完成（2026-10-09，`d209ae2`） | tokenizer 不支持 `>=`/`<=`/`AND`/`OR`/`NOT` —— 已补齐：tokenizer 两字符算子（`>=`/`<=` 优先于单字符识别）+ 关键字大小写不敏感归一大写；优先级链 **OR → AND → NOT → 比较 → 加减 → 乘除 → 幂 → 一元**（两条裁决：比较**高于** AND/OR，`a>1 AND b>2`=`(a>1)AND(b>2)`；NOT **低于**比较，`NOT a>b`=`NOT(a>b)` 而非 `(NOT a)>b`——后者把逐元素取反的序列再比较，几乎从不是本意）；**AND/OR/NOT 的 NaN 硬性传播**（Go `NaN!=0` 恒 true，不特判则「谓词 AND NaN」组合出 1 = 假信号）。注册表 31→36 算子，自洽护栏 sampleExpr 同步 5 条；提示词派生自动携带（AvailableOperators 全集）。测试：纯函数 NaN 语义 + 优先级结构断言（astShape）+ 大小写归一 + 端到端求值 + 组合表达式过闸门。破坏验证 2 条：NOT 提到比较之上 → 优先级测试红；去掉 AND 的 NaN 特判 → `AND(NaN,1)=1` 假信号红。均还原绿 | 中 |
| OBS-08 切片 1 ✅ 完成（2026-10-09） | 字段注册表收敛为**单一事实源** + 与 provider 对齐：`DataProvider` 加 `Fields()` 能力声明（5 处实现同步：1 生产 + 4 假）；注册表升级为**带来源**（market / fundamentals / group）；**修误拒**（`ps`/`roa` 现被闸门接受）、**补实现**（`revenue`/`profit` 由 `domain.Fundamental` 的 `Revenue`/`NetProfit` 接线 —— 此前只有声明没有实现）、**清假合法**（`market_cap`/`roe_ttm`/`eps` 从注册表移除：`domain.Fundamental` 无此三字段 ⇒ 永不可求值，留在闸门里就是让 AI 反复撞墙）；求值报错区分「字段不在语言里」vs「字段属于 X 源但本 provider 不供应」；新增**跨包防漂移护栏**（注册表非 group 字段 ≡ `OHLCVDataProvider.Fields()`，双向点名）。**并**：研究提示词 `research.go` 的字段/算子清单改为**从注册表派生**（此前硬编码，广告幻影 `market_cap` 且漏掉 9 个算子与 4 个字段）+ 新增护栏 | — |
| OBS-08 切片 2 ✅ 完成（2026-10-09，`e97f0cd` + `ec33ec3`） | **序列注册表 + 可用性声明**（`SeriesSpec` 此前只存在于 `registry.go:126` 一句注释里）：声明每个字段/序列的**当前可用性**（例如 `stock_sector_map` 0 行 ⇒ `sector` 不可用），让 AI 不再对着空数据静默产垃圾。**并**（本轮新发现）：`pkg/ai/prompts/factor_research.txt` 与 `strategy_generate.txt` 是**零引用的死资产**（Go / 最新文档 / 脚本 / CI 全无引用，仅 `docs/archive/` 提到），且 `factor_research.txt` 的字段清单**同样漂移**（广告 `vwap` / `turnover_rate` / `returns` / `volatility`）。**待裁决：接线它**（当作研究提示词，字段从注册表派生）**还是删除它** | **可用性声明已做**（`e97f0cd` + `ec33ec3`）：见下方交付明细。**prompts/\*.txt 已清理**（`6455566`）：`factor_research.txt` 删除（漂移死资产，被 `research.go` 派生版超越）、`strategy_generate.txt` 的领域知识合并进 `generate.go` 活提示词后删除（消除双真相） |
> **OBS-08 切片 2 交付明细（2026-10-09）**
>
> **取证（决定性事实）**：真库 `stock_fundamentals` **0 行**、`stock_sector_map` **0 行** ⇒ 15 个注册字段里**只有 6 个行情字段真能用**，7 个基本面字段 + `sector` 全是空数据的坑。而 `research.go` 此前把 14 个数据字段**全部广告给 AI**，且提示词示例里还有 `- Quality: roe / pe` —— **提示词自己在示范用零数据字段**。
>
> **裁决：可用性是「能力层」之上的第三层，不混为一谈**——能力层 = provider 代码认不认（切片 1 已做，被跨包护栏 `AvailableDataFields() ≡ OHLCVDataProvider.Fields()` 钉住）；可用性层 = 那张表里**当前有没有数据**。「认」不等于「有」。故 `AvailableDataFields()` 语义**一字未改**（护栏不破），新增的是它之上的运行时判定。
> - `fieldRegistry` 值从 `FieldSource` 扩为 `FieldDef{Source, DependsOn}`；表名收成常量（`TableOHLCV`/`TableFundamentals`/`TableSectorMap`），防拼写漂移
> - `AvailabilityProbe func(ctx, table) (bool, error)` —— **注入式**，expression 包不连 DB（纯计算层不该知道数据库）
> - `FieldAvailability(ctx, probe)`：同表只探一次；**探测失败 fail-loud 整体报错**，不降级成「不可用」（库挂了若被当成「字段没数据」，AI 会以为本来就没有，静默降级比报错危险）
> - `AvailableDataFieldsWith(avail)`：`avail==nil` 明确语义为「未做可用性过滤」而非「全部可用」
> - `FieldsDependingOn(table)`：表 ↔ 字段反查（数据补上/清空时一眼看出影响面）
> - **提示词接线**（`ec33ec3`）：`factorDSLSyntax(avail)` 只把有数据的字段列进「可用」，并**单列一段 `UNUSABLE NOW` 点名禁用**空数据字段；`roe / pe` 示例在基本面不可用时换成明确跳过说明。只删不标注不够——AI 会因「记得有这些字段」反复尝试
> - **为什么单列「不可用」而不是干脆不提**：删掉会让 AI 重复撞墙；标注才能一次绕开
> - **测试**：纯函数（fake probe）5 条 + 注册表自洽（DependsOn 必须是已知表常量）+ **真库核对**（判定 ≡ 真库行数，不把「业务现状」钉成不变式以免数据补上后假红）+ 提示词护栏（可用清单不含空数据字段、UNUSABLE 段点名禁用）
> - **破坏验证 2 条**：探测结果强制 true → 3 条红（含真库核对点名 `pb` 与真实 0 行不一致）→ 还原绿；`unavailableDataFields` 恒空 → 提示词护栏红 → 还原绿
> - **真库现状输出**：可用字段 = `[close high low open turnover volume]`（仅 6 个）
>
> **⚠️ 踩坑（第 N 次脆弱断言）**：`strings.Contains(dataLine, "pe")` 命中了 **"open"**（o-**pe**-n）⇒ 把正确输出误判成错。改按逗号解析成**精确字段集合**比对。与记忆里「隔离判断别按行位置」同类：**断言要做精确匹配，子串 Contains 在字段名这种短词上必然踩雷。**
>
> **登记未做**：① 完整 `SeriesSpec` + `Provider.GetSeries`（ADR-028 §5/§5.1）——那是**大工程**（要动 5 个 Provider 实现），本切片只做了可用性这一层；② `pkg/ai/agents` 提示词里的**示例表达式**（`research.go` 的算子清单已派生，示例仍是硬编码，同 K3b 登记项）；③ prompts/*.txt 已清理（`6455566`：factor_research.txt 删、strategy_generate.txt 知识合并进 generate.go 后删）。

| OBS-11 ✅ 完成（2026-10-09，`d2ccdc6`） | per-day HTTP 反模式 `getSignalsFromStrategyService` 待删（策略服务只传定义不传信号）—— **已两端同拆**：① 引擎侧删 `getSignalsFromStrategyService` + getSignals 的 fallback 分支 + `strategyServiceURL` 字段/配置读取（唯一消费者），未注册策略改 **fail-loud**（报错点名策略名 + 可用清单 + OBS-11 标注）；② 服务端删 `POST /:name/signals` 端点（ADR-012 后恒回 503 的死哨兵）+ SignalRequest/Response/Detail 类型；③ 清理 `strategy_service.url` 配置段 + compose `STRATEGY_SERVICE_URL` env。**决定性事实**：ADR-012 后该路径是**必然失败**的死路径（本地注册表 miss → HTTP → 恒 503）——比「建了没接」更糟，删除零风险。测试：未注册 fail-loud（错误无 HTTP/503 痕迹）+ 已注册仍走本地路径（反证腿）。httpClient 字段保留（detect_regime/position/stop-loss 等其它服务调用仍在用——**是否同属反模式属 risk-service 议题，不在 OBS-11 范围**） | 中（随 K1/K2） |
| OBS-12 ✅ 完成（2026-10-09，core `cbef8a6` / ui `166d7bc`，跨两仓） | Copilot 对外 API 契约与前端脱节（四层缺陷叠加）—— **取证重映射后修复**：① 四层缺陷的**现状**与 ADR-030 记载已不同——`handlers_copilot.go` 里的同步版 `generateStrategyHandler`（description 契约 + OpenAI 直调）是**零引用死代码**（~150 行），活契约自始就是**异步 job**（POST 202 `{job_id,status}` + `GET /generate/:job_id` 轮询端点**已存在**）；② core 侧：删死代码块（含 5 个失效 import）；③ ui 侧：`CopilotRequest` 对齐 `GenerateParams`（`description`，修复「发 `{prompt}` 输入被吞」）、删从未对上过的 `CopilotResponse`、`copilot.ts` 补 `pollCopilotJob`（白名单终态集 `generated/backtest_complete/llm_failed/sandbox_rejected/build_failed/backtest_failed`，刻意不用「非 pending 即终态」——误判终态会把半成品当结果展示，白名单的错误方向是超时可见）、`Copilot.vue` 轮询到终态**如实展示**产物/`build_error`/`backtest_error`（build_failed 仍展示代码供审阅；sandbox_rejected 提示需配 `copilot.working_dir`），删除「策略已生成」谎言兜底。验证：ui typecheck 0 错 / lint 0 error / **208 测试全过**；缺陷④（working_dir 空 ⇒ sandbox_rejected）是**部署态配置**，后端 fail-closed 行为正确，前端已能如实呈现 | 治理建议「契约同源校验」登记为后续（openapi→TS 类型生成 + CI 断言） |
| OBS-09 | ✅ 2026-10-09：027/028/029 成环引用澄清为**单向依赖**——028/029 的能力部分（K3/K6/K7）已随内核线解耦落地，服务边界条目显式标注「待 027 服务拆分落地后重评」 | 低（治理） |
| OBS-10 | ✅ 2026-10-09：ADR-027 头部补 **Reconcile（ADR-019）**——019 的合并判断全部保留，反转前提是内核模块化已冻结边界，服务拆分是可选部署形态 | 低（治理） |

---

## 五、遗留待裁决（已全部收口）+ NT 决策门裁决记录

| 项 | 现状 | 裁决记录与内容 |
|---|---|---|
| **AUD-61** | ✅ 已迁出主线（2026-10-14，用户拍板：前端正处设计系统重构、e2e 将大改，留 core 无执行意义） | **归属地：`quant-trading-ui/docs/TASKS.md` AUD-61**（六字段已写全，两阶段式：先 `test.fixme` + 显式登记恢复「全绿」可信度，设计系统重构落地后再按页重对齐）。核心红线随迁：不得把红洗成「已验证通过」，不得放宽断言消红。本行仅存迁移指针，勿在此更新状态 |
| **P2-8** | ✅ 已裁决关闭（2026-10-09，若曦授权「按你的建议走」）：**不落 hfq 表** | **裁决：不落 hfq，回测一律用 qfq。**取证与理由：① hfq 价 = qfq 价 × k（k = 每股累积复权因子，**因股而异**），**单位不是「元」** ⇒ 固定资金下股数计算错误 ⇒ 改写票池成分与权重；② **资金无法按同一基准缩放**——不同股票 k 不同，组合层面不存在统一基准，这是几何事实不是实现问题；③ **hfq 与 qfq 的收益率序列逐点相同**（已验证）⇒ hfq 不提供 qfq 没有的 alpha 信息；④ 实测 `splits` 0 行、`dividends` 0 行、`ohlcv_daily_qfq` **无复权因子列** ⇒ 本地无法自行推导 hfq，只能靠数据源且无法自校验。⇒ hfq 唯一增量价值是「长期走势的直观展示（复权净值曲线）」，而**回测必须用 qfq**（价格真实 ⇒ 股数/权重正确）。落 hfq 是纯风险无收益：存储翻倍（390 万行 ×2）+ 引入「拿 hfq 跑固定资金回测」的误用风险（错误结论极难察觉，与 OBS-01 同源）。**若将来确需 hfq（如 UI 展示复权净值）**：由数据源直接提供 hfq 序列或复权因子列，本地不自行推导；落库必须在 schema 注释与文档标注「单位非元，禁止用于资金/股数计算」，且引擎侧要有护栏拒绝 hfq 作为回测价格源 |
| **P2-1** | ✅ 已完成（2026-10-12，`02058b9`：Migration 050 + tushare 宏观 adapter + 只读 API；**真库 `quant.macro_series` 6168 行实测**） | 收口记录见表格下方 **P2-1 · 宏观数据源** 小节；台账关闭见 **P2-1-R1**；代码侧自审收尾见 **P2-1-R2** |
| **P2-2** | ✅ 已完成（2026-10-12，`8c922c9`/`3bc0995`/`fd10a7f`，四任务深度审查判定达标；审查收尾三件由 P2-2-R1 清账） | 收口记录见表格下方 **P2-2 收口记录**；收尾执行见 **P2-2-R1** 小节 |
| **NT 决策门** | ✅ 已裁决（2026-10-14，若曦拍板）：**① 推进生产化**（阶段 3 于 2026-10-13 以 K16 落地，本行是其事后正式化） | **裁决一（三选一 → ①）**：凭 K15 双跑数据（total_return \|Δ\| ≤ 0.086%、容差 ≤0.2%、CI `align-gate` 门禁上线）+ K16 六件套落地，正式化为「推进生产化」；③ 回退（废弃侧车、落「借鉴设计增强 Go 内核」）降级为「NT 正式版升级失败」的兜底路径，不再是并列选项。**裁决二（VeighNa 并列评估：取消）**：其触发条件是「诉求确认为 Python+中国市场」，从头未成立——生产后端必须 Go（ADR-031 D5），Python 仅侧车且已被 NT 以双跑对齐验证占住；VeighNa 主场是期货 CTA+A 股柜台网关接入，非日频股票回测内核，且评估需为第二个 Python 引擎复刻 K14+K15 全套对齐资产，无基线可比。其真正价值（A 股柜台网关）属将来**实盘接入阶段**的选型问题，届时以网关候选单独评估。**裁决三（K4 impact）**：见 K4 区块——维持默认 `fixed`。**遗留（唯一）**：NT v2.0.0 正式版发布后按运维文档升级预案收口（双跑对齐回归为验收门）；版本锁定策略 = 钉 rc6、不追 nightly（ADR-031 未决问题随之关闭） |

> **✅ P2-2 收口记录（2026-10-12）**：交付落点 `8c922c9`/`3bc0995`/`fd10a7f` —— Migration 049 产业链四表（`quant.sw_industry_tree` / `supply_chain_edge` / `industry_vitality` / `etf_constituent`，内联 DDL + 溯源列 + DDL 一致性测试）、`pkg/data` 三 tushare 接口导入（`index_classify` 全量行业树 / `index_member` 行业成分入边表 / `fund_portfolio` ETF 持仓）+ seed CSV 导入（错误向上传播不吞、幂等 ON CONFLICT DO UPDATE）、`/api/industry/*` 只读查询 + openapi 同步。验收要点：四任务深度审查判定达标（无 🔴）、core 提交态全量 `go test ./...` 67 包 0 FAIL（含真库）、ETL 错误传播与同批重导不重复插行均有测试腿。遗留（审查收尾三件 🟠 seed BOM / 🟡 台账 / 🟡 真实喂数）→ **P2-2-R1** 工单（seed BOM 修复与台账关闭随该工单首 commit 清账；真实喂数验证与逐接口额度登记写进该工单收口记录）。

### ✅ P2-1 · 宏观数据源（六字段工单，2026-10-12 补全，实现 `02058b9`）

- **① 目标**：补上**国内宏观**数据（汇率 / 利率 / 物价 / 货币）的数据源接入——新增宏观 DataType + Tushare 宏观 adapter 落库，带溯源列、幂等重导、提供只读查询。跨境那一半已完成（`global_ohlcv: yahoo_finance primary + alpha_vantage secondary`），不在本任务。
- **② 上下文**：quant-trading-docs 仓 `docs/guides/data-dependencies.md` §P2-1（结论「**宏观是真空白**」——`pkg/data/source/adapter.go` 的 DataType 常量里只有行情/资金流/财务/板块/公告/新闻，没有任何宏观类型，**要新增 DataType + adapter 不是填 key**；候选源：Tushare 宏观接口复用 `TUSHARE_TOKEN` 0 新 key 但吃积分 / FRED 1 免费 key / Alpha Vantage 免费档 25 req/day 只够探测 / World Bank 0 key；**待核实**：除 `cn_cpi`（doc_id=228）外各宏观接口名称与积分未逐条核对）；adapter 注册与 kill switch 模式 `cmd/data/registry_init.go`（缺依赖注册 disabled 降级、不阻断启动）；现有 call/normalize/save 模式 `pkg/data/tushare.go`；P2-2 刚落地的同族先例（postgres 内联 DDL + 溯源列 + DDL 一致性测试 + 只读 handler 样板）——`quant.macro_series` 与 `quant.industry_vitality` 并列不重复（后者是行业景气，本任务是国家层宏观）。
- **③ 要求**：
  1. **先核对再动手**：查 Tushare 官方文档逐条核对宏观接口清单与积分要求，结果回写指南 §「待核实」——权限/积分不够的指标登记「不做」；不许凭接口文档臆断（P2-8 曾据文档推断 `stk_factor_pro` 可用，被 40203 实测推翻）；
  2. 路线**首版 Tushare 单路线**（复用 `TUSHARE_TOKEN`，0 新 key）；目标指标集以第 1 步核对为准，含 CPI / PPI / PMI / M2 / 社融 / LPR / 汇率；
  3. Schema：Migration **050**（当前最新 049=P2-2；若并行期间被占用则以 `migrate()` 内当时最大编号顺延）内联 DDL + `docs/migrations/050_*` 镜像；表 `quant.macro_series`（indicator、freq、obs_date、value、unit、source、as_of；唯一键 indicator+freq+obs_date+source）+ DDL 一致性测试（照 `TestSignalSchemaDDLSyncedWithMigrate` 先例）；
  4. domain 类型 + storage 读写；`adapter.go` 增宏观 DataType 常量 + tushare 宏观 adapter 注册进 `registry_init.go`（遵循 disabled 降级与 `DATA_DISABLE_*` kill switch）；**ETL 错误向上传播不吞**；重导幂等（同唯一键不重复插行）；
  5. 只读查询 API（照 P2-2 handler 样板：struct + Option + New + RegisterRoutes）+ `docs/openapi.yaml` 同步——⚠️ `deps_test.go` 断言 `ServerDeps` 字段总数，**不得新增字段**。
- **④ 约束与非目标**：不做跨境（已完）；不消费宏观数据产因子/策略（消费方另案）；首版不接 FRED / World Bank / Alpha Vantage（key 增量为零）；不动 P2-2 四表与行情链路；不碰内核线目录 `pkg/kernel`/`pkg/backtest` 撮合/`contracts`（K10、K17 并行区域）；mootdx/实时行情不在范围。
- **⑤ 验收**：`go build ./...` + 全量 `go test ./...` 绿（本地无 -race，CI 覆盖）；真库 migrate 后 `\d quant.macro_series` 与 EnsureSchema 一致；导入幂等测试（同批重导行数不变）；查询 API httptest 契约测试（正常 / 空结果 / 非法参数 400）；`TUSHARE_TOKEN` 可用时真拉 ≥1 条宏观指标入库取证（无 token 则 fixture 全绿 + commit message 登记「真拉未实测」）。**破坏验证**（改坏→红→还原，留证 commit message）：① 删 source 的 NOT NULL → 溯源约束测试红；② 导入器改吞错（log 后 continue 返 nil）→ 错误传播测试红；③ 撤唯一键 → 幂等测试红（重导行数翻倍）。
- **⑥ 边界**：静态宏观数据层 + 导入 + 只读查询，core 仓一个 atomic commit（`feat(data): P2-1 宏观数据源 …`）；指南 §待核实 回写在 quant-trading-docs 仓单独 commit（跨仓各自一 commit）。

> **✅ 收口记录（2026-10-12，P2-1-R1 台账关闭）**：交付落点 `02058b9` —— Migration 050 建 `quant.macro_series`（唯一键 indicator+freq+obs_date+source + source/as_of 溯源列 + DDL 三方一致性测试）、`pkg/data/macro.go` 指标目录 / normalize / 导入面（同接口多 indicator 共用一次请求）、Tushare 宏观 adapter（registry disabled 降级 + `DATA_DISABLE_TUSHARE_MACRO` kill switch）、只读 `GET /api/macro/series` + openapi 同步（复用 deps.Store，未新增 ServerDeps 字段）。
> 实测结论：9 个 indicator（7 个 tushare 接口）真 token 逐个 POST 全部 code:0，**三处推翻文档假设**（`cn_pmi` 响应字段实测大写 / `shibor_lpr` 实测限流与官方标称冲突 / `fx_daily` 实测 2410 行不截断），清单见 quant-trading-docs 仓 `34255d9`（数值纠错 `dd35ab9`）。
> 真库取证：`quant.macro_series` **6168 行**（8 指标；`LPR_5Y` 受 `shibor_lpr` 流控未补齐，重跑一次分组导入即可，见 `02058b9` commit「已知限制」）。
> 遗留：**大宗 / 境外宏观未接**（后续工单，不在 P2-1 范围）；代码侧自审收尾（7 项）见 **P2-1-R2**。

### ✅ P2-1-R1 · P2-1 台账关闭（2026-10-12 审查立单，同日关闭）

> 来源：2026-10-12 对 K10/K17/P2-1 三任务深度审查。P2-1 **实现质量高**（DDL 三方一致测试 / 真库 catalog 测试 / 批量事务语义 / 错误传播 / 未知指标拒绝 / 探测不落库 / 共享一次请求，全绿；docs 仓 `34255d9` 逐个 POST 实测 9 个接口并推翻文档三处假设——正是工单「不许凭接口文档臆断」的落实），**唯一缺口是台账未关**。

- **① 目标**：关闭 P2-1 台账（§五 表行 + 工单小节收口记录），让已落地实现在台账上有完成态。
- **② 上下文**：实现 `02058b9` + 指南实测结论 `34255d9`（quant-trading-docs 仓）均已完成——2026-10-12 审查实测：core 提交态全量 `go test ./...` **67 包 0 FAIL**；真库 `quant.macro_series` **6168 行**、`source` NOT NULL；指南 §P2-1 已由「宏观是真空白」改为「已落地」+ 9 接口实测清单。但本文件 §五 表 P2-1 行仍 ⬜、工单小节无 ✅ 收口记录（对比 K10/K17 均已打 ✅）。
- **③ 要求**：
  1. §五 表 P2-1 行打 ✅（含实现提交号 + 真库行数取证 + 指向本工单）；
  2. 工单小节标题打 ✅ + 补 3–4 行收口记录：交付落点（Migration 050 / adapter / 只读 API）、实测结论（9 接口 + 三处推翻文档）、真库行数、**遗留：大宗 / 境外宏观未接**；
  3. 遗留项（大宗 / 境外宏观）按既有口径登记为「待裁决 / 后续工单」，不混进本工单范围。
- **④ 约束与非目标**：不改 P2-1 已落地代码、DDL 与 openapi；不扩界接新数据源。
- **⑤ 验收**：TASKS.md 内 P2-1 两处（表行 + 小节标题）均 ✅；收口记录事实与代码、真库、docs 仓指南逐条对照一致（不许出现「文档说已落地、代码未落地」的错位）。
- **⑥ 边界**：core 仓 `docs/TASKS.md` 一个 docs commit（指南实测结论已在 docs 仓 `34255d9`，不重复）。

> **✅ 关闭记录（2026-10-12）**：§五 行与小节收口记录已更新（本 commit）；收口记录与代码、真库（6168 行复测）、docs 仓指南逐条对照通过。

### ✅ P2-1-R2 · 宏观数据层提交后自审收尾（7 项，2026-10-12 作者自审立单，同日落地）

> 来源：2026-10-12 作者对 P2-1（`02058b9`）的**提交后自审**。判定：**实现可用，代码侧无 🔴 阻塞缺陷**；发现 **4 🟠 + 3 🟡**。另有 **3 处事实错误在 docs 仓当场修正（`dd35ab9`）**，不进本工单范围。
>
> **已当场修正的 3 处（docs 仓 `dd35ab9`，逐个 curl 官方页核实）**：指南 §P2-1 实测清单表里 ① `TSF_INC` 数值列 `sf_inc` → **`inc_month`**（实测 `sf_month` 返回 `month/inc_month/inc_cumval/stk_endval`，代码一直是对的，**是文档写错**）；② `sf_month` 积分 600 → **2000**（doc_id=310）；③ `shibor_lpr` 积分 600 → **120**（doc_id=151「用户积累120积分可以调取，正式权限需要5000积分」）。其余五格复核与官方页一致。教训已写进该 commit：**实测清单的价值在于每个数字都有出处，凭印象补的格子比留空更危险**。

- **① 目标**：清掉 P2-1 自审遗留 7 项（4 🟠 + 3 🟡），让宏观层从「现在能用」到「上游变故不静默、未来加指标不踩坑」。
- **② 上下文**：
  - **R1（🟠 探测放大）**：`pkg/data/macro.go:320` `ProbeMacro` 遍历 `macroFetchWindows` 的**全部**窗口逐个发请求。当前 `HealthCheck` 探 `CPI_YOY`（1 段）没触发；若换成 `USDCNY_CLOSE` 就是 **8 次请求/次探测**（2019–2026 分段），未来任何带 `SinceYear` 的日频指标同理。**探测的语义是连通性，不该按数据量放大**——这会白吃 `fx_daily`（2000 积分档）配额。
  - **R2（🟠 0 行静默，与自述纪律自相矛盾）**：`normalizeMacroSeries` 静默跳过「日期解析失败」与「数值为 nil」的行；`FetchMacroIndicator` 在 `len(all)==0` 时直接 `return nil, nil`（成功）。若上游改日期格式或整列变 nil ⇒ **全批跳过、0 行入库、对外仍是成功**。这与同文件头自述纪律「绝不 log 后 continue——那会让『上游挂了』伪装成『导入了 0 行』」**直接矛盾**（normalize 的 continue 正是同类），也是 P2-8「静默丢数据」同款 hazard。当前实测正常，**未触发**。
  - **R3（🟠 分组不变量未约束）**：`FetchMacroIndicators` 分组后用 `specs[0].Fields` 发一次请求、响应喂给组内所有 spec；但 `sameWindows` 只比**窗口**、不比 `Fields`/`DateLayout`。同接口两 spec 的 `Fields` 一旦不同（例：未来给 `cn_pmi` 加 `PMI_PROD` 只取 `pmi010100`）⇒ 另一 spec 的列不在响应里，报「响应形状不符契约」——**错误信息误导**（真因是指标目录配置不自洽，不是上游变形状）。当前 `M2`/`M2_YOY`、`LPR_1Y`/`LPR_5Y` 恰好 Fields 一致，**未触发**。
  - **R4（🟠 无上限查询）**：`pkg/storage/macro.go:116` `ListMacroSeries` 无 `LIMIT`，`handlers_macro.go` 也无分页。当前单指标最大 2410 行（`USDCNY_CLOSE`），日频每年 +250 行；且是**无分页**形态，客户端无法增量取。
  - **R5–R7（🟡）**：① 查询路径用裸 `fmt.Errorf` 未走 `apperrors`（同文件 Save 路径走了，风格不一致）；② `sameWindows` 内部重复取 `time.Now()`，与调用方的 now 在跨年边界瞬间可能不一致；③ `idx_macro_series_lookup (indicator,freq,obs_date)` 与唯一键 `(indicator,freq,obs_date,source)` 前缀重叠，对当前查询形态基本冗余（写放大 + 存储），但**是否删需先 EXPLAIN，不凭猜**。
- **③ 要求**：
  1. **R1**：`ProbeMacro` 只取窗口序列的**第一个**发一次请求；补测试钉住「`USDCNY_CLOSE` 探测只发 1 次请求」（破坏腿：改回遍历全部 → 红）。
  2. **R2**：`normalizeMacroSeries` 返回 `skipped` 计数（或带跳过原因）；`FetchMacro*` 在「响应有行但入库 0 行」时 **fail-loud**（报错，或至少 Warn 带计数 + 指标名）。破坏腿：造一份全 nil 值的响应 → 应红/应告警 → 还原绿。
  3. **R3**：把 `Fields`/`DateLayout` 一致性纳入分组校验（扩展 `sameWindows`，或加目录自检测试断言「同 `APIName` 的 spec 必须 `Fields`/`DateLayout`/`Params` 一致」）；不一致时**报错点名**（说清是指标目录配置不自洽），而非退化为「形状不符契约」。
  4. **R4**：`ListMacroSeries` 加 `Limit`（默认 + 上限，如默认 1000 / 上限 5000）；`handlers_macro.go` 与 `docs/openapi.yaml` 同步参数与超限语义。
  5. **R5–R7**：查询路径错误改 `apperrors` 包装；`sameWindows` 的 `now` 改为入参；先 `EXPLAIN` 确认唯一索引能否覆盖该查询，再决定 `idx_macro_series_lookup` 去留（**不凭猜删索引**）。
- **④ 约束与非目标**：不改 `quant.macro_series` 的唯一键与 `source`/`as_of` 溯源列语义（破坏腿①②③的护栏不破）；不新增 `ServerDeps` 字段（`deps_test.go` 钉住总数）；不接新数据源（**大宗 / 境外宏观仍属后续**）；不动 P2-2 四表与行情链路；不碰 `pkg/kernel`/`pkg/backtest` 撮合/`contracts`（K10/K17 并行区域）。
- **⑤ 验收**：全量 `go test ./...` 绿；R1 测试钉住探测请求数且破坏腿红→还原绿；R2 破坏腿（全 nil 响应）红→还原绿；R3 目录自检测试能捕获人为造的不一致 spec；R4 加 LIMIT 后 `EXPLAIN` 仍走索引、openapi 与 handler 参数一致。
- **⑥ 边界**：core 仓一个 atomic commit（`fix(data): P2-1-R2 宏观层自审收尾 …`）；不跨仓（docs 仓纠错已在 `dd35ab9`）；与 P2-1-R1（台账关闭）**互不依赖**，可并行。

> **✅ 收口记录（2026-10-12，P2-1-R2 落地，7 项全清）**：
> - **R1（🟠 探测放大）**：`ProbeMacro` 只发首窗口一次请求（`macroWindowsOrErr` + `windows[0]`，空窗口 fail-loud）；USDCNY_CLOSE 探测由 8 次请求降为 1 次。测试 `TestProbeMacro_ChunkedIndicatorSendsSingleRequest` 钉住请求数；破坏腿：改回遍历全窗口 → 红 → 还原绿。
> - **R2（🟠 0 行静默）**：`normalizeMacroSeries` 返回 `normalizedMacroRows{Rows, SkippedBadDate, SkippedNilValue}`；`macroAllSkippedError` 闸门在「响应有行但全部被跳过」时 fail-loud（点名 indicator + 分解计数），部分跳过留 Info 日志。破坏腿：闸门失效 → `TestFetchMacroIndicator_FailsLoudWhenAllRowsSkipped` 红 → 还原绿；共享响应腿点名 M2_YOY。
> - **R3（🟠 分组不变量）**：`specsCanShareResponse(specs, now)` 校验 Fields / DateField / DateLayout / Params / 窗口五组不变量，不一致报「宏观指标目录配置不自洽」并点名（不再静默退化逐条拉）；三件测试：目录自检（钉住恰好 2 个共享组）/ 四分歧人为捕获 / 运行时拒绝（报错先于发请求落库）。破坏腿：改 M2_YOY fields → 自检红 → 还原绿。
> - **R4（🟠 无上限查询）**：`ListMacroSeries` 加 `Limit`（默认 1000 / 上限 5000，越界 INVALID_INPUT）+ `ORDER BY obs_date DESC`（截断只丢最旧）；handler 解析 limit（1..5000，显式 0 / 非数 / 超限 → 400）+ 响应 `limit`/`truncated`/`hint`（OPTION-A：不做多取 1 行嗅探）；openapi 同步（参数 + 400 语义 + 200 字段）。EXPLAIN 实测（6168 行真库）：ind / ind+freq / ind+freq+range 三形态均走 `idx_macro_series_lookup`。破坏腿 2 条：去 DESC → 红；去 LIMIT → 红；均还原绿。
> - **R5–R7（🟡）**：查询 / 扫描 / 迭代三处错误改 `apperrors.Wrap(INTERNAL)`；`sameWindows` / `specsCanShareResponse` 的 now 改入参（同一次导入 as_of 与窗口共用一个时间点）；**`idx_macro_series_lookup` 保留**——EXPLAIN 实测三形态均走该索引（非冗余，不凭猜删）。
> - **验证**：全量 `go test ./...` **68 包 ok / 0 FAIL**；5 组破坏腿全部「红 → 还原绿」；`ServerDeps` 未新增字段（deps_test 护栏未触）、唯一键与溯源列语义未动、未接新数据源。
> - **遗留**：无（7 项全清）；大宗 / 境外宏观仍属后续工单，不在本收尾范围。

### ✅ P2-2-R1 · 产业链数据底座审查收尾（三件套，2026-10-12 深度审查立单）

> 来源：2026-10-12 对 K9/K11/K12/P2-2 四任务深度审查（四任务判定**达标**，无 🔴；发现 1 🟠 + 2 🟡，全部归属 P2-2，本工单为其可执行化）。审查报告：WorkBuddy 会话工作区 `quant-review-report.md`。审查基准（实测）：core 提交态全量 `go test ./...` **67 包 0 FAIL**（含真库）；`./internal/repoguard/ ./pkg/...` **61 ok**；UI 仓 `b28d5b6` typecheck 0 错 / lint 0 error / vitest **221/221** / Playwright K9 invalid e2e **1 绿**；UI 破坏腿复现 **2 红→还原**。

- **① 目标**：清掉 P2-2（`8c922c9`/`3bc0995`/`fd10a7f`）审查遗留三件事——seed CSV 可导入性缺陷（🟠）、本文件台账关闭（🟡）、真实数据入库验证（🟡）——完成后 §五 P2-2 行关闭。
- **② 上下文**：
  - **R1（🟠）seed CSV BOM**：`testdata/industry_vitality_seed.csv` 带 UTF-8 BOM（实测首三字节 `ef bb bf`）；`ImportIndustryVitalityCSV` 表头归一化仅 `strings.TrimSpace`，Go 1.25 下 U+FEFF 非空白字符（`TrimSpace("\ufeffindustry_code") != "industry_code"` 已实证）→ 直接导入该文件必报「缺列 industry_code」。且全仓无任何代码/脚本/测试引用该文件（grep 无命中，测试全用无 BOM 的 inline CSV）——**seed 文件可导入性零证明**（项目自己的标准：未证明的声明等于没有）。
  - **R2（🟡）台账未关**：本文件 §五 P2-2 行仍 ⬜ 且无收口记录（K9/K11/K12/K10 均有 ✅ 收口记录）；该任务的定义就是关闭此行。
  - **R3（🟡）底座未喂数**：真库 `quant.sw_industry_tree` / `supply_chain_edge` / `industry_vitality` / `etf_constituent` 四表 0 行（2026-10-12 实测）；tushare `index_classify` / `index_member` / `fund_portfolio` 免费额度下真实可用性未验证——`8c922c9` 所称「导入幂等（真库自灌自证）」是代码构造行自导，非真实拉取（与 stock_fundamentals 空表同风险面）。
- **③ 要求**：
  1. R1：`pkg/data/supply_chain.go` 表头循环加 `h = strings.TrimPrefix(h, "\ufeff")` 后再 TrimSpace（防 Windows 编辑器另存 BOM 的普遍场景）；补一条直接读 `testdata/industry_vitality_seed.csv` **真文件**的测试腿，让 seed 从此有消费方；
  2. R2：本工单收口时同步——§五 P2-2 行打 ✅ + 补 P2-2 简短收口记录（提交号 + 验收要点 + 遗留指向本工单）；
  3. R3：真实导入验证——依次真拉 `index_classify`（全量行业树）、`index_member`（≥1 个一级行业成分）、`fund_portfolio`（≥1 只 ETF）+ seed CSV 导入，逐接口记录额度/权限结果与四表行数；任一接口额度不可用 → 把结论登记进本工单（对齐「stock_fundamentals 等三表空属设计行为」的既有口径），不阻塞 R1/R2。
- **④ 约束与非目标**：不改 DDL 与唯一键语义、不动 P2-2 既有存储/导入/API 行为；不做 K60 因子/传导计算、不做 P2-1 宏观、不做 UI；不碰 K9–K12 已冻结契约与 K10 注册表。
- **⑤ 验收**：R1——真文件测试腿绿，且破坏腿（去掉 TrimPrefix → 红，还原绿）；全量 `go test ./...` 绿。R3——四表行数 > 0 或逐接口额度结论已登记。R1+R2 完成后 §五 P2-2 行 ✅。
- **⑥ 边界**：R1（代码+测试+台账关行）core 仓一个 atomic commit（`fix(data): P2-2-R1 seed CSV BOM …`）；R3 结果登记可为后续 docs commit；逐接口真拉记录写进本工单收口记录。

> **✅ 收口记录（2026-10-12，R1+R2 commit `3ceb69d`，R3 登记随 docs commit）**：
> - **R1（🟠 seed CSV BOM）**：`ImportIndustryVitalityCSV` 表头归一化改为 `strings.TrimPrefix(h, "\ufeff")` 后再 `TrimSpace`；新增 `TestImportIndustryVitalityCSV_RealSeedFile` 直读 `testdata/industry_vitality_seed.csv` 真文件（断言钉住「文件带 BOM」为该腿存在理由），seed 文件从此有消费方。破坏腿留证：去掉 TrimPrefix → 红（`缺列 "industry_code"`，实际表头首列带 U+FEFF）→ 还原绿；全量 `go test ./...` **68 包 0 FAIL**（含真库）。
> - **R2（🟡 台账）**：§五 P2-2 行已打 ✅ + 补 P2-2 简短收口记录（同 `3ceb69d`）。
> - **R3（🟡 真实喂数实测，2026-10-12，真库 `quant_trading` + 真 `TUSHARE_TOKEN` 逐接口真拉）**：`index_classify` **OK — 359 节点**（全量申万行业树，source=tushare）；`index_member`（801010.SI 一级行业）**OK — 归一化 184 条边 → 唯一键折叠 160 行**（同股多期 in/out_date 记录按 (upstream,downstream,relation) 收敛；折叠的 PIT 语义损失——历史/当前成员混同——由审查立单 **P2-2-R2** 清账）；`fund_portfolio`（510300.SH）**不可用 — tushare 40203「您没有接口(fund_portfolio)访问权限」**（token 权限/积分档位不足，对齐「stock_fundamentals 等空表属设计行为」口径，登记不阻塞）；seed CSV 导入 **OK — 5 行**（走生产导入面，BOM 修复真文件自证）。四表行数：`quant.sw_industry_tree` **359** / `quant.supply_chain_edge` **160** / `quant.industry_vitality` **5** / `quant.etf_constituent` **0**（唯一空表，40203 权限结论在案）。喂数后复跑全量：**67 ok / 1 FAIL**，唯一红为并行在途文件 `cmd/analysis/handlers_backtest_nautilus_test.go:77`（K13 线 `gin.SetMode` 违反守卫，非本任务文件、与喂数无关）——如实登记不洗绿，归属 K13 线自行处理。遗留：`fund_portfolio` 待 token 权限升档后按 R3 步骤补拉 ETF 成分，届时 `etf_constituent` 转 > 0（数据源升级事项，不属本收尾范围）。

### ✅ P2-2-R2 · P2-2-R1 收尾审查发现（两件，2026-10-12 自审立单，同日落地）

> 来源：2026-10-12 对 P2-2-R1 两 commit（`3ceb69d`/`8deda87`）的代码审查（结构化审查 + 双验证者独立交叉验证 2/2 确认）。审查取证（实测）：raw tushare `index_member(801010.SI)` 184 行 / 160 唯一 con_code，其中 **56 个（35%）全部成分段 out_date 非空（已出表历史成员）**、104 个当前在表；`quant.sw_industry_tree` level 分布 **28/104/227 = 359**（SW 三级全在，收口记录「全量树」措辞复核成立）。

- **① 目标**：清掉 P2-2-R1 审查发现两件——R1（🟡）BOM 剥离覆盖单点、R2（🟠）`industry_member` 成分边 PIT 语义缺失。
- **② 上下文**：
  - **R1（🟡）覆盖单点**：`strings.TrimPrefix(h, "\ufeff")`（`pkg/data/supply_chain.go` 表头循环）的唯一测试覆盖是 `TestImportIndustryVitalityCSV_RealSeedFile`（`pkg/data/supply_chain_test.go` L346-L362），其存在理由依赖 `testdata/industry_vitality_seed.csv` 自带 BOM，且断言提示「有意去 BOM 时同步删本断言」——按提示删后 BOM 剥离零覆盖（全测试文件无 inline BOM 表头用例）。真文件腿是回归腿，不是唯一腿。
  - **R2（🟠）PIT 语义**：`normalizeSWIndustryMembers`（`pkg/data/supply_chain.go` L266-L295）向 tushare 请求了 `in_date/out_date` 却全部丢弃；`domain.SupplyChainEdge`（`pkg/domain/supply_chain.go` L36-L49）无有效期字段（`AsOf` 是导入时刻溯源，不是成分有效期）；`pkg/storage/supply_chain.go` 唯一键 `ON CONFLICT (upstream, downstream, relation)` 把多期记录折叠成一行。净效果：**已出表历史成员与当前成员同形态落库**——R3 真喂数的 160 行里 56 个是历史成员，K60 产业链/传导按当前成分消费将引入陈旧成员。此为 P2-2 既有设计（`8c922c9`），非 P2-2-R1 引入，但 R3 使其成为真库真实风险。
- **③ 要求**：
  1. **R1**：`supply_chain_test.go` 补一条 inline BOM 表头用例（表头串以 `"\ufeffindustry_code,metric,…"` 构造，断言导入成功），与真文件腿互为独立覆盖；真文件腿的 BOM 存在理由断言保留不动；
  2. **R2 先裁决口径（二选一，裁决理由写收口记录）**：① **只导当前成员**——`normalizeSWIndustryMembers` 仅保留 out_date 为空的段（当前在表），历史段不入边表，契约注释 + 设计文档明示「`industry_member` = 当前成分快照，非 PIT」；② **保留全历史**——给成分关系补有效期（新 relation 值或边表有效期列；涉及 DDL 与唯一键变更，须按「DDL 内联 postgres.go + 一致性测试」口径**单独变更评审后**再动手）；
  3. 裁决后落地 + 真库重导验证（`801010.SI` 行数与所选口径一致：① 应为 104、② 应保留 184 条段记录）+ 破坏腿一条（改坏→红→还原）。
- **④ 约束与非目标**：不动 `sw_industry_tree` / `industry_vitality` / `etf_constituent` 三表；不做 K60 消费方改造（消费侧过滤另案）；不改 P2-2-R1 已收口记录的事实部分（只可加交叉引用）；R2 口径②的 DDL 变更不许混进 R1 的 commit。
- **⑤ 验收**：R1——inline BOM 用例绿 + 破坏腿（删 TrimPrefix → 不依赖真文件的独立红）；R2——口径注释/文档与实现一致、真库重导行数符合所选口径、全量 `go test ./...` 绿。
- **⑥ 边界**：R1 一个独立 commit（`test(data): …`）；R2 按裁决结果另起 commit（若选口径②，先过变更评审）。数据线工单，与内核线零交集。

> **✅ 收口记录（2026-10-12，P2-2-R2 落地，两件全清）**：
> - **R1（🟡 BOM 覆盖单点）**：`supply_chain_test.go` 新增 `TestImportIndustryVitalityCSV_InlineBOMHeader`——表头串以 `"\ufeff"` 开头 inline 构造、不依赖 testdata 真文件，与真文件腿互为独立覆盖；真文件腿「BOM 存在理由」断言保留不动。破坏腿：注释移除 `strings.TrimPrefix(h, "\ufeff")` → 只跑 inline 用例独立红（缺列 "industry_code"，实际表头首列带 U+FEFF）→ 还原绿；还原后 `pkg/data/supply_chain.go` 与 HEAD 零差异（`git diff --quiet` 通过）。commit `b845f10`（生产代码零改动）。
> - **R2（🟠 成分边 PIT 语义）**：**裁决口径①「只导当前成员」**——理由：轻量（无 DDL/唯一键变更，立即可落地）、契约明示「当前成分快照，非 PIT」即消除 K60 误用面；口径②（补有效期保留全历史）涉边表 DDL 与唯一键语义变更，按「DDL 内联 postgres.go + 一致性测试」口径须**单独变更评审**，本单不做、按需另案。落地：`normalizeSWIndustryMembers` 仅保留 `out_date` 为空的段（当前在表），历史出表段过滤；`out_date` 列缺失 fail-loud（请求固定含该列，缺列 = 当前/历史不可判定，静默全保留会悄然回退旧行为——与缺 con_code 列同处理）；契约注释（`FetchSWIndustryMembers` / `normalizeSWIndustryMembers` 双 doc）+ `docs/SPEC.md` 四表登记段与导入段同步明示。
> - **真库重导验证（口径①，真库 `quant_trading` + 真 `TUSHARE_TOKEN`）**：BEFORE 801010.SI 旧边 **160 行** → 清边重导 → AFTER **104 行**；raw 对账 184 行 / 唯一 con_code 160 / out_date 空行 104 / out_date 空唯一 104——与审查「56 个（35%）全段出表」结论精确一致；抽查全段出表历史成员 `000061.SZ` 库中 **0 行** ✅。
> - **破坏腿（R2）**：注释移除过滤 `continue` → `TestNormalizeSWIndustryMembers_SkipsOutOfDateSegments` 红（5 条边、3 条历史段混入）→ 还原绿、无残留（BREAKLEG 清零）。
> - **测试与提交**：新增 2 用例（历史段过滤 / 缺 out_date 列拒绝）+ 更新 1 用例（`FallsBackToRequestedIndex` body 补 out_date 列，测试意图不变）；全量 `go test ./...` **68 包 ok / 0 FAIL**。R2 commit（`fix(data): …`，`docs/SPEC.md` + 本台账同 commit）。
> - **遗留**：口径②（PIT 全历史 + 有效期列，须先过 DDL 变更评审）按需另案；K60 消费侧以契约「当前成分快照，非 PIT」为准，若需 PIT 语义另案表达。

### P2-1-R3 · 宏观层复检发现（2 项，2026-10-13 自审立单）✅ 完成（2026-10-13）

> 来源：2026-10-13 对 P2 数据线 4 commit（`69889f4` / `eb689ab` / `b845f10` / `ffb7388`）的结构化复检（对照 code-review checklist 逐节过，含 API 真实性核查）。判定：**改动整体达标，无 🔴 阻塞缺陷**；发现 3 🟠 + 1 🟡，按线拆分落 **P2-1-R3**（宏观面）与 **P2-2-R3**（产业链面）。

- **① 目标**：清掉宏观面复检两项——`fieldToFloat` 不可解析值静默折 0（R2 闸门的漏网面）、`interface{}` 用 `!=` 比较的 panic 隐患。
- **② 上下文**：
  - **R1（🟠 静默折 0，与自述纪律矛盾）**：`pkg/data/tushare.go` `fieldToFloat` 的 `string` 分支 `f, _ := strconv.ParseFloat(val, 64); return f`（err 丢弃）、`default` 分支 `return 0`。消费点 `pkg/data/macro.go`（`normalizeMacroSeries` 的 `Value: tushareCellFloat(item, iVal)`，L512 区）。闸门只拦 `item[iVal] == nil`（L504 区）——「**非 nil 但不可解析**」（上游 value 列从 JSON number 改字符串 `"N/A"` / `"13,42"` / `"--"`，或改非标量类型）**静默入库 0**。与同函数注释自述「缺失值折成 0 等于编数据（同 tushare.go 的 P2-10 教训）」**直接矛盾**；`SkippedNilValue` 计数亦不暴露（只计 nil）。当前 tushare 返回 JSON number，**未触发**。**同洞波及 P2-2**：`normalizeETFPortfolio` 的 `Weight: tushareCellFloat(item, iRatio)`（ETF 权重静默 0，交叉见 P2-2-R3）。
  - **R2（🟠 interface 比较 panic 隐患）**：`specsCanShareResponse` 的 `equalMacroParams`（`macro.go` L346 区 `bv != av`）与 `sameWindows`（L368 区 `ov != v`）对 `interface{}` 用 `!=` —— 值若为非可比较类型（slice/map/func）将 runtime panic（"comparing uncomparable type"），绕过 apperrors 链路。已核实当前目录 Params 仅 1 处标量（`USDCNY_CLOSE` 的 `ts_code: "USDCNH.FXCM"`），共享组（cn_m / shibor_lpr）均无 Params，**当前不可达**；未来任一 spec 的 `Params` 引入非标量即触发，且触发点在导入路径。
- **③ 要求**：
  1. **R1**：normalize 层显式判定可解析性——新增 `tushareFloatOK(any) (float64, bool)`（或 `tushareCell` + `strconv.ParseFloat` 判 err）→ `SkippedBadValue` 计数分列，纳入 `macroAllSkippedError` 报错文案与 `logMacroSkipped` 字段；补测试（不可解析串 / bool / array 三种输入 → 计入跳过不进库；全坏 → fail-loud 红→还原）。**不改 `fieldToFloat` 共享签名**（波及行情链路，另案评估）。
  2. **R2**：`equalMacroParams` / `sameWindows` 的 map value 比较改 `reflect.DeepEqual`；配一条「非标量 Params」目录自检用例（改前 panic → 改后不 panic 且能判不等）。
- **④ 约束与非目标**：不动 `fieldToFloat` / `fieldToStr` 既有签名与共享行为（行情/链路消费方不受影响）；不动 `quant.macro_series` 唯一键与 `source`/`as_of` 溯源列；不新增 ServerDeps 字段；ETF weight 面由 P2-2-R3 处理（本单只做交叉引用）。
- **⑤ 验收**：R1——三种不可解析输入的测试腿绿 + 全坏响应 fail-loud 腿红→还原；R2——非标量 Params 用例绿（改前 panic → 改后不 panic）+ 破坏腿；全量 `go test ./...` 绿。
- **⑥ 边界**：core 仓一个 atomic commit（`fix(data): P2-1-R3 …`）；`pkg/data/macro.go` + `pkg/data/macro_test.go` + `pkg/data/tushare.go`（仅新增函数，不改既有）+ 台账。

### P2-1-R3-R1 · `fieldToFloat` 成为死代码（2026-10-13 P2-1-R3 自审立单）✅ 完成（2026-10-13）

> 来源：P2-1-R3 交付后的自审——全仓 grep 核查「④/⑥ 的『行情/链路另有消费方』前提」。

- **① 目标**：处置 `fieldToFloat` 的孤立死代码，避免后续 normalize 误拾「静默折 0」助手。
- **② 现状（已证）**：P2-2-R3 侧删掉 `tushareCellFloat`（静默折 0 的 ETF weight 路径）后，`fieldToFloat` **全仓零调用点**（`grep -rn fieldToFloat --include=*.go` 仅命中其定义）。即 P2-1-R3 ④/⑥ 所依据的「不改既有的共享签名，波及行情/链路消费方」**前提不成立**：它的**唯一**调用者一直是 `tushareCellFloat`，而该助手本身也是静默折 0 的同一类反模式。留着它 = 未来写 normalize 的人 grep 到「一个现成的 float 助手」再犯 P2-10 教训。
- **③ 要求**：删除 `fieldToFloat`（保留 `fieldToStr`——`tushareCell` 仍在用；`tushareFloatOK` 是可判定失败的替代）；在 `tushareFloatOK` 注释里把「fieldToFloat 的既有签名不动」改为「已随 P2-2-R3 删除，见本单」。
- **④ 约束与非目标**：不动 `fieldToStr`；不改 `tushareFloatOK` 语义；不新增函数。
- **⑤ 验收**：`go build ./...` + 全量 `go test ./...` 绿（零引用删除，行为零变化——以 build/测试为证）。破坏腿不适用（纯删死代码）。
- **⑥ 边界**：core 仓一个 commit；`pkg/data/tushare.go` + 台账。
- **⑦ 收口记录（2026-10-13）**：`fieldToFloat` 已删（`go build ./...` + 全量 `go test ./...` 绿证零行为变化）；`tushareFloatOK` 注释改为「曾与 fieldToFloat/tushareCellFloat 并存，后两者已随 P2-2-R3 删除」。`fieldToStr` 保留（`tushareCell` 在用）。

### P2-2-R3 · 产业链层复检发现（2 项，2026-10-13 自审立单）✅ 完成（2026-10-13）

> 来源：同上（2026-10-13 P2 数据线 4 commit 复检，P2-1-R3 的姊妹单）。判定：产业链面 2 项（1 🟠 + 1 🟡）。

- **① 目标**：清掉产业链面复检两项——口径①落地后的「全过滤静默」（无计数 / 无日志 / 无闸门）、口径①新行为的测试缺口。
- **② 上下文**：
  - **R1（🟠 全过滤静默）**：`FetchSWIndustryMembers`（`pkg/data/supply_chain.go` L111-113 区）`if len(edges) == 0 { return nil, nil }` —— 口径①后「响应有行但**全部段** out_date 非空（被过滤）」与「上游返回 0 行」**同形不可分**，两者都静默成功：无 raw 行数计数、无过滤计数、0 行路径无日志。上游 out_date 空值形态变故（填 `"0"` / `"-"` / `"00000000"`）→ 当前成员被全判历史段 → **静默 0 行入边表**，K60 消费方拿到空产业链而无信号。对比 P2-1-R2 R2 已在宏观层为同类风险立 fail-loud 闸门（`macroAllSkippedError`）——**同仓两条数据线纪律不一致**。
  - **R2（🟡 测试缺口）**：口径①新增的过滤有混合形态用例（`TestNormalizeSWIndustryMembers_SkipsOutOfDateSegments`），但「**全部**段都是历史 → 0 条边」形态**无用例**；`FetchSWIndustryMembers` 的 `len(edges)==0 → nil,nil` 路径亦无测试。另：`normalizeETFPortfolio` 的 `Weight` 走 `tushareCellFloat` → `fieldToFloat` 同 **P2-1-R3 R1** 静默 0 洞（本单做专项确认：weight 不可解析 / 非有限时是否该拒绝该行）。
- **③ 要求**：
  1. **R1 口径已拍板（2026-10-13 若曦裁决）**：选 **① fail-loud**（`ErrCodeDataQuality`，文案对齐 `macroAllSkippedError`，注明「若确为全部出表属上游数据形态判断，需人工确认」）——与宏观层纪律统一，防静默错误。落地：`normalizeSWIndustryMembers` 返回过滤计数（raw 行数 `len(resp.Data.Items)` 调用方已可得），`FetchSWIndustryMembers` 增闸门；「raw == 0」保持 `nil, nil`（真真空合法）。
  2. **R2**：补 `TestNormalizeSWIndustryMembers_AllSegmentsOutOfDate`（全历史段 → 0 条边）+ `TestFetchSWIndustryMembers_FailsLoudWhenAllFiltered`（R1 已拍板 fail-loud）；`normalizeETFPortfolio` weight 不可解析 / 非有限的处置确认（口径与 P2-1-R3 R1 对齐）。
- **④ 约束与非目标**：不动 `sw_industry_tree` / `industry_vitality` / `etf_constituent` 三表；不动口径①「仅当前成员」语义本身（本单只补闸门 / 计数 / 测试）；不改 P2-2-R2 收口记录的事实部分（只可加交叉引用）。
- **⑤ 验收**：R1——闸门 / 告警测试腿红→还原绿 + 真库重导不受影响（`801010.SI` 仍 104 行）；R2——新用例绿；全量 `go test ./...` 绿。
- **⑥ 边界**：core 仓一个 atomic commit（`fix(data): P2-2-R3 …`）；`pkg/data/supply_chain.go` + 其测试 + 台账。

### P2-2-R3-R1 · ETF 成分行跳行无计数/日志（2026-10-13 P2-2-R3 自审立单）✅ 完成（2026-10-13）

> 来源：P2-2-R3 交付后的自审——对照本仓两条数据线的「跳行必须可观测」纪律复查 ETF 面。

- **① 目标**：让 ETF 成分行的跳过可观测——口径与宏观侧（`SkippedBadValue` + `logMacroSkipped`）和申万侧（`filtered` 计数进错文案）一致。
- **② 现状（🟠）**：P2-2-R3 把 ETF weight 的「空 / 不可解析 / 非有限」改为**跳过该行**（不编 0，与 P2-1-R3 R1 对齐），但 `normalizeETFPortfolio` 只返回 `rows`，**没有任何跳过计数或日志**。后果：**部分**成分行静默消失（无信号）——`len(rows)==0` 的全跳场景有闸门兜住，但「10 行里 3 行 weight 为空」这种**部分**丢失完全无声，K60 消费方拿到残缺成分表却看不到任何证据。这正是本档 P2 线反复要治的「静默丢数据」，只是从「全过滤」缩到了「部分过滤」。
- **③ 要求**：`normalizeETFPortfolio` 增返回「跳过行数」（weight 空 / 不可解析 / 非有限三类合计）；`FetchETFPortfolio` 在 `skipped > 0` 时记 Info 日志（`etf` / `rows` / `skipped`），并把该数纳入全跳闸门的报错文案。
- **④ 约束与非目标**：不动 `etf_constituent` 表结构与唯一键；不改 weight 跳过口径本身（本单只补可观测）；不引入 logger 到 normalize（保持纯函数）。
- **⑤ 验收**：`TestNormalizeETFPortfolio_SkipsUnparseableWeight` 增断言（跳过计数 == 3）；新增「部分跳行记日志 / 全跳闸门文案带计数」断言；全量 `go test ./...` 绿。
- **⑥ 边界**：core 仓一个 commit；`pkg/data/supply_chain.go` + 其测试 + 台账。
- **⑦ 收口记录（2026-10-13）**：`normalizeETFPortfolio` 增返回 `weightSkipped`；`FetchETFPortfolio` 在 `weightSkipped > 0` 时记 Info（`skipped_bad_weight`），全跳闸门文案带该计数。测试：`SkipsUnparseableWeight` 增 `skipped == 3` 断言 + 新增 `TestFetchETFPortfolio_LogsPartialWeightSkips`（同包注入 client.logger，避开全局 logger）。破坏腿：把返回计数改成 0 → 计数腿与日志腿均红（实测）。**注**：闸门断言一度写成 `Contains "1 行"`，而该文案里行数总量那处也是「1 行」→ 断言失效；已收紧为 `"不可解析 1 行"` 并用同一破坏腿复验。

---

## 六、K0 契约审查发现（2026-10-08）

> 审查通过项（记录以免重复审）：依赖方向无环且层次干净（clock/msgbus 零依赖 → eventstore → kernel；`live→risk`、`execalgo→portfolio` 别名方向均与 BootOrder 同向）；11 模块全覆盖；8 张表齐全且归属清楚；编译期守卫经双方独立破坏验证有效；零现有实现改动。

### K0-P2-1 · 核心语义约束需 AST 护栏 ✅ → 已并入 K11（随 K11 于 2026-10-12 完成）

> ⚠️ **2026-10-12 墓碑注记**：本任务内容已整体移入 §二 **K11**（内核三条不变量 AST 护栏，作为新主线的独立任务保留），此处不再重复维护。后续执行与验收以 K11 六字段为准。

### K0-P2-2 · `strategy → ai` 循环已破 ✅（2026-10-09 完成 ADR-027 §5 第 3 步）

- **做了什么**：`git mv pkg/ai/expression pkg/expression`（6 生产 + 6 测试，零逻辑改动）；`pkg/ai/contracts/contracts.go` → `pkg/backtest/contracts/runner.go`（并入已存在的 LEAF 包，包名同 `contracts`）；改 18 个文件的 import/注释；`contracts_test.go` 拆两处落点（解掉「core 测试依赖 AI」）。
- **破环证据**：`go list -deps ./pkg/strategy/ | grep pkg/ai` → **空**（改前 4 处）；core 侧（backtest / strategy / indicator / validation / risk / marketdata / portfolio / execalgo / kernel / expression / live / storage 及全部子包）**零 `pkg/ai` 依赖**。
- **新增护栏**：`internal/repoguard/ai_boundary_test.go` 的 `TestPkgAIIsOnlyImportedByAllowedLayers` —— **白名单式 fail-closed**（仅 `pkg/ai|pkg/tools|cmd|e2e` 可 import `pkg/ai`；只扫生产代码）。破坏验证双向：既有包加 import → 红；**全新包加 import → 红**（默认拒绝性质）。
- **边界定义**：见 ADR-027 §「执行记录：§5 第 3 步已落地 + AI / core 边界定义」（含两个待裁决点：`pkg/validation` 与 `pkg/tools/builtin` 的归属）。
- **✅ 2026-10-09 AI 拆仓两阶段全部落地**：阶段 1（`94f2b78`）ai-service 独立进程（:8086，`cmd/ai`），共享装配上收 `internal/bootstrap`（后随阶段 2 提升 `pkg/bootstrap` + `pkg/sandbox` + `pkg/httpserver`，因 Go internal 可见性规则跨模块不可 import），analysis 对 copilot/pipeline/explore/tools 四族路由改**纯反代**（前端与 openapi 契约零改动）。阶段 2（本 commit）`pkg/ai`(141 文件) + `pkg/copilot`(原 strategy/copilot.go) + `cmd/ai` + AI 装配 builder 物理迁入 `quant-trading-agent`（独立 go module，require core + replace，**单向依赖**）。core 的 ai_boundary 护栏改写为新不变量：pkg/ai 目录不许回来 + core 不许 import pkg/ai/agent 模块（破坏验证两条腿红）。compose 的 ai-service 经 additional_contexts 传 core 上下文构建。
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

**本条新登记的两条待办（已于 2026-10-13 收口，见各节 ⑦）**：

#### K0-P2-3a · `Verify` 依赖内存计数器，实例重建即归零 ✅ 完成（2026-10-13，`b639270`）

- **① 目标**：把 `Verify` 的「写入账」基准从进程内 `atomic.Int64` 换成库内计数。
- **② 现状**：`PGEventStore.Verify` 用本实例成功 Append 的次数（`appended`）与库里 `count(*)` 对账。**实例重建（进程重启、断点续跑）后计数器归零**，而库里的行还在 ⇒ Verify 误报「落库 N 行 vs Append 0 次」，红灯是假的；假红灯会训练人忽略 Verify。
- **③ 要求**：改为基于 `run_id` 的库内计数（例如按 run_id 聚合条数并与期望值对账），使「重开一个 store 实例」不再影响结论。
- **④ 约束与非目标**：不改 `audit.message_log` 的 DDL 与列名；不改 `Verify` 的方法签名。
- **⑤ 验收**：构造 store → Append 5 条 → 丢弃实例 → 用同一 (publisher, run_id) 新建实例 → `Verify` 仍绿。
- **⑥ 边界**：只改 Verify 的计数口径，不做哈希链/checksum、不做 id 连续性检查。
- **⑦ 收口（2026-10-13，`b639270`）**：口径落为**基线+增量**（比 ③ 的「期望值对账」更简单且不需外部传期望值）：`baseline`（首次需要时捕获的库内计数，`ensureBaseline` 懒建立 + mutex 一次性）+ `appended`（本实例增量）== 当前库内计数。懒建立是因为构造期表可能尚不存在（测试先构造后 EnsureSchema，生产 kernel_wiring 在 Boot 前构造），捕获点必在本实例首条 INSERT 之前。验收 ⑤ 由 `TestVerifySurvivesInstanceRebuild` 落地（含重建后续写对账与「删行仍红」红灯腿）；破坏验证：口径改回 `== appended` → 验收 ⑤ 假红场景变红。约束守住（DDL/列名/Verify 签名未动）。**已知边界**（注释记档）：同 `(publisher, run_id)` 双实例并发写入会误红，属「一 run 一实例」用法之外的罕见场景，不为此加复杂度。

#### K0-P2-3b · `pkg/msgbus/registry.go` 的 topicSet 与 `topics.go` 常量是两份真相 ✅ 完成（2026-10-13，`9f6dfbc`）

- **① 目标**：把注册表收敛为一处真相后删除 `registry.go`。
- **② 现状**：`topics.go` 的 16 个常量是编译期符号，运行期拿不到「全体已注册 topic」集合，于是 `registry.go` 里又手工镜像了一份 `topicSet` map——**加常量忘了登记**是静默失效（新 topic 会被 Publish 拒掉，错误信息清楚但没人提前知道）。现状靠 `registry_internal_test.go` 与 `interfaces_compliance_test.go` 各引用一次全部常量来钉，属于测试兜底而非结构消除。
- **③ 要求**：把 `All`（或等价集合）迁进 `topics.go`，`IsRegisteredTopic` / `RegisteredTopics` 基于它实现，然后删除 `registry.go` 并同步测试引用。
- **④ 约束与非目标**：topic 数量与取值不得变更（仍是 16 个、值同 K0 冻结）；`IsRegisteredTopic` / `RegisteredTopics` 的对外语义不变。
- **⑤ 验收**：删除 `registry.go` 后 `go build ./...` 与 `pkg/msgbus` 全部测试仍绿，且「新增常量未登记」会编译失败（不再只是测试红）。
- **⑥ 边界**：只做这份双真相收敛，不动 topic 取值与 `Publish/Subscribe` 的校验语义。
- **⑦ 收口（2026-10-13，`9f6dfbc`）**：验收 ⑤ 以**声明即登记**落地，比原验收的「编译失败」更强——Go 里「常量声明了但忘登记进列表」无法成为编译错误（声明与列表是两个语法位点），故改为 `TopicX = topic("x.y")`：`topic()` 在包初始化期校验命名正则、查重、登记进 `registry` map，「新增 topic 未登记」**结构上不可能发生**。16 个 topic const 改 var（名与值不变，K0 冻结面由 `interfaces_compliance_test.go` 的 `frozenTopicValues` 继续钉值；var 的篡改风险由 `IsRegisteredTopic` 当场拒掉）；`IsRegisteredTopic`/`RegisteredTopics` 迁入 `topics.go` 基于 `registry` 实现，`registry.go` 删除。绕过 `topic()` 的违规写法（裸字符串/const）由 `topics_internal_test.go` 的 AST 护栏兜底。破坏验证：把 `TopicKernelShutdown` 改为裸字符串赋值 → AST 护栏 + 注册表护栏双红；还原后全量 `go test ./...` 绿。

### K1/K2 实现审查发现（2026-10-08，设计层审查）

> 审查通过与亮点记录：`kernel.go` 全生命周期逻辑正确（回滚只装成功的、回滚 error 丢弃理由已论证、Shutdown 先发后停时序）、`adapters.go` 包装设计论证完整、`kernel_shadow.go` 失败策略与关停时序清楚、`runtime.go` 失败立即停 + universe 过滤 + 横截面就绪门正确。**P1-1 已当场修复**（Boot 防重复闸门 + `ErrAlreadyBooted` + 测试 + 破坏验证，随审查提交）。

| # | 问题 | 处理时机 |
|---|---|---|
| K1-P2-1 | **Boot 的 ctx 在 Boot 后立即 cancel**（kernel_shadow 的 20s timeout + defer cancel）：当前模块不保存 ctx 无影响，但未来模块若在 Start 里起 goroutine 依赖该 ctx 存活，会被抢先 Cancel | 接管时（内核应传长生命周期 ctx，或契约明示 ctx 仅限于 Boot 期间） |
| K1-P2-2 | **回滚 error 被静默丢弃**：`rollbackLocked` 的 Stop error 直接丢弃（注释论证「避免掩盖根因」），但 Stop 失败=资源泄漏，无人知晓 | 接管时（至少 log 或包进返回 error 的 message） |
| K1-P2-3 | **eventstore 模块的资源归属未定**：`EventStoreModule.Stop` 不关 pool（归 pkg/storage 统一 Close）——影子期正确，但接管后必须裁决「pool 是内核资源（模块 Stop 关）还是应用层资源（保持现状）」 | 接管时（与 setup.go 的 gracefulShutdown 归属一起裁决） |
| K1-P2-4 | **模块回调内核会死锁**：`Boot/Shutdown` 全程持 `k.mu`；`Clock()/Bus()/Store()` 不加锁安全，但模块若在 Init/Start/Stop 里调 `Module()`（加锁）会死锁。注释已声明「模块拿不到内核引用」 | 作为**未来模块约束**登记：模块不得在生命周期方法内回调 `Module()` |

> ⚠️ **2026-10-12 归宿注记**：K1-P2-1~4 四条已整体并入 §二 **K12**（内核装配治理收口，阶段 3 前置）。「接管时」即 K12 执行时；上表保留作为问题登记与出处，后续状态与验收以 K12 六字段为准。

### K3 切片 2（+OBS-06）审查发现（2026-10-09，含真库核对）

> **通过项**：注册表（31 算子）结构清晰，`selfWarmup/selfLookback` 由实参推导，闸门递归覆盖四类节点，`DeriveWarmup/Lookback` 与 ADR-028 §8 及附录 A 对齐。**真库核对**（把 `factor_genes.formula` / `experiments.expression` / `strategy_genes.params` / `strategies.params` 的存量表达式全部喂给新闸门）：**可解析 1 条、被拒 0 条** ⇒ 无「既有表达式被判非法」的回归。`pow` 不在注册表（历史 `IsMathOp` 有它但无实现）——复查用它的三处（agent 提示词 / 基因池名单 / yaml 生成器）都没用，无现网影响。

| # | 问题 | 影响与处理 |
|---|---|---|
| K3a ✅ 已修（2026-10-09，`810f151`） | **闸门不校验「标量参数位」的类型**：`ts_mean(close, volume)`、`ts_mean(close, ts_delay(volume,1))` 参数个数与节点都合法 ⇒ 过闸；但求值时 `firstScalar` 取到非标量（或 `NaN`）⇒ `int(NaN)` 未定义 ⇒ 窗口变荒谬值 ⇒ **整条序列 NaN**（假合法残留） | **已修**：`OperatorDef` 加 `ScalarParams []int`（13 个时序算子按 `firstScalar` 实参位声明：`ts_*`→[1]、`ts_corr`→[2]、`ts_kalman`→[1,2]），闸门在解析期拦。**裁决：判定标准是「常量表达式」而非「字面量」** —— 只认 `*LiteralNode` 会误拒 `ts_ewma(close, 1/20)` 这类合法常量写法（误拒比假合法更危险，K3c 教训）。新增 `isConstantExpr`（子树不引用序列数据即算标量）+ `describeNode`（报错点名参数位）。测试：10 条病根被拦 + 13 条合法未被误拒（含**真库存量唯一表达式** `cs_rank(ts_pct_change(close,20))>0.8` 作永久回归护栏）+ 纯函数 13 条 + 注册表自洽护栏。破坏验证：闸门校验改恒真 → 10 条病根全红 → 还原绿 |
| K3b ✅ 已修（2026-10-09，`37339f4`） | **防漂移护栏只覆盖 `pkg/expression`**：`pkg/ai/gene_pool/mutation.go` 两处局部名单写死算子名；`pkg/ai/yaml/generator.go` 用字符串模板生成表达式；`pkg/ai/agents/*` 提示词里也写死算子名 | **已修（修法选「把护栏扩到产出侧」而非改模板结构）**：`generator.go` 的算子名是**完整表达式模板**的一部分（含嵌套与参数关系），无法从扁平算子清单派生——硬改等于为护栏重写业务。真正危害是「产出过不了闸门」，故护栏钉在这一层：① `yaml/generator_gate_test.go` 遍历全部 7 个 StrategyType × 4 个 lookback，断言产出 Parse+Validate 通过（顺带覆盖参数个数/字段/标量位）；② `mutation.go` 两处局部 slice 提成包级变量 `wrapCandidates`/`unwrapCandidates`（**行为一字不变**），断言 ⊆ `AvailableOperators()`。破坏验证两条：模板算子名改坏 → 2 条测试红并点名 `cs_ran`；名单加不存在算子 → 名单护栏红并点名。**登记未覆盖**：`pkg/ai/agents` 提示词里的**示例表达式**（`research.go:94-95` 等）——示例是文字示范不是产出路径，且 `research.go` 的算子清单已由 OBS-08 切片 1 改为注册表派生 |
| K3c | **闸门的字段白名单与 provider 真实能力互相错配**（两个方向）：闸门放行 `market_cap`/`roe_ttm`/`eps`/`revenue`/`profit`/`sector`，而 `OHLCVDataProvider.GetField` 不认（**求值时报 unknown field，fail-loud**）；反过来 provider 支持 `ps`/`roa`，闸门**不认** ⇒ 用它们的表达式会被 fail-closed 闸门**误拒**（比前者更危险：挡住正经用法） | **并入 OBS-08**（序列可用性声明）：字段白名单必须由 provider/`SeriesSpec` 注册表**派生**，并声明「有数据 / 表为空」，把「不可用」在**闸门期**说清而不是留到求值期 |


### K17-R1 · exec-algo 挂载失败不得静默跳过（2026-10-12 审查发现，🟠）✅ 完成（2026-10-13，`547564b`）

> 来源：2026-10-12 对 K10/K17/P2-1 三任务深度审查。K17 实现与 D5 正证据（TWAP 7 片 / 冲击占比 0.3783 ≈ 1/√7 / paper 同构）判定达标；本工单承其**唯一 🟠**——挂载失败的处置与项目已确立的 fail-closed 口径不一致。

- **① 目标**：让「信号显式指定 `exec_algo` 但挂载失败」不再静默消失——回测侧与 paper 侧口径统一（fail-loud 或运行诊断可见），并补测试钉住。
- **② 上下文**：
  - 回测侧 `pkg/backtest/engine_daily.go:521-529`：`attachExecAlgoSignal` 返回 error → 仅 `logger.Warn()` + `tp.PendingQty` 回填 + `return`，**该信号既不拆单也不整单直下**，交易静默消失、回测照常跑完出结果（只留一条易被淹没的 Warn）；
  - paper 侧 `pkg/live/paper_exec_algo.go:82-84`：同一 `Attach` 失败是 `return err`（**fail-loud**）——**两条下单主路径口径相反**，同一次误配置在回测里静默、在 paper 里报错；
  - 触发条件（`Dispatcher.Attach` 的 fail-loud 四类，全部为调用方**静态错误**）：未知 Style（如拼错 `"twp"`）/ 父单号重复 / Σ子单计划量 ≠ 父单量 / Schedule 非法输入；
  - 口径冲突：同一系统内，HTTP 请求拼错 `engine_params` 被 K9 以 400 点名拒绝、`kernel` 未注册被 K10 拒绝且不回退 go，**唯独策略内拼错 `exec_algo` 静默跳过交易**——与 OBS-01 同族的「静默失真面」；
  - **零测试覆盖**：`exec_algo_e2e_test.go` 仅 TWAP / VWAP / 默认路径三条正路径，全仓 grep 无挂载失败用例（未证明的行为等于没有）。
- **③ 要求**：
  1. 回测侧：未知算法名（静态判据）→ 在信号校验期 **fail-loud**（对齐 K9「rejected, not ignored」）；形态二选一并写明理由：该 run 走 `invalid` + reason code，或直接 fail 该信号；
  2. 其余挂载失败（父单号重复 / Σ不等 / Schedule 非法）保留「单信号失败不中断整场」的既有约定，但**累计计数进运行诊断并在结果可见**——禁止「跑完没人知道少了交易」；
  3. 与 paper 侧口径统一：同一判定同一处置，补测试钉住两侧一致；
  4. 四类挂载失败各补一条测试；**破坏验证**：把「未知算法名」改回静默跳过 → 对应断言红（改坏→红→还原）。
- **④ 约束与非目标**：默认路径（无 `exec_algo`）行为逐字节不变；不改 `ExecAlgorithm` / `Dispatcher` 冻结签名与 `AttachSpec` 语义；不动 K18（外部仓位变动取消活跃父单）范围；不新增 ResultSpec 字段（需呈现先走既有 `invalid_reasons` / 诊断路径）。
- **⑤ 验收**：四类挂载失败各有测试（含破坏腿）；两侧口径一致性测试；运行诊断计数在结果可见的断言；默认路径既有 e2e 三条零改动全过；`go build ./...` + 全量 `go test ./...` 绿。
- **⑥ 边界**：只改挂载失败的处置与可见性 + 测试，core 仓一个 atomic commit。

### K17-R2 · exec-algo 接线鲁棒性收尾（作者自审 🟡 三件，2026-10-12 登记）✅ 完成（2026-10-13，`4aab98c`）

> 来源：K17 作者自审（对照代码评审清单）登记为「挂技术债不修」的三件 🟡，按本工单可执行化。三件均不阻塞 D5 实验；**执行时点：K17-R1 落库后**——R1/R3 与其改同一批文件（dispatch.go / paper_exec_algo.go），先做会撞车。K18 已落库（`7a3576e`），不与本工单冲突。

- **① 目标**：把 exec-algo 接线的三处 🟡 鲁棒性缺口收掉——错误路径不丢已取子单、会话不可重入、ctx 全链贯通。
- **② 上下文**（2026-10-12 于 K18 落库后复核，三项均仍成立）：
  - R1（🟡）`pkg/execalgo/dispatch.go` `OnBar`（现 `:281`）：遍历父单中途遇错即 `return out, err`——**已取走（计入 submitted）的子单被调用方丢弃**（调用方 `continue`），父单进度与报送事实从此不一致。该错误路径当前不可达（algo.OnBar 仅在未 Schedule 时报错，Attach 后不可能），但结构上是「错误吞掉已发生的状态变更」；
  - R2（🟡）`pkg/live/paper_session.go`：`PaperSession` 语义为一次性回放，但 `Run` 无重入护栏——第二次 `Run` 撞父单号（`Dispatcher.Attach` duplicate → warn+skip），表现为「信号静默消失」而非显式拒绝（与 K17-R1 同族的静默失真面）；
  - R3（🟡）`pkg/live/paper_exec_algo.go` `attachExecAlgo`（现 `:105`）内部用 `context.Background()`——`Run` 的 ctx 没贯通到 Schedule（回测侧 `attachExecAlgoSignal` 已带 ctx，两侧不一致）。
- **③ 要求**：
  1. R1：`OnBar` 遍历**完全部**父单后再返回（错误累积或取首错，**已取子单照常返回**给调用方处置）；或等价方案（错误时已取子单仍交还调用方），取舍写进注释；
  2. R2：`Run` 增加重入护栏（`used` 标记或等价判据），第二次调用返回 error 显式拒绝（fail-loud，非 warn+skip）；
  3. R3：`attachExecAlgo` 增 ctx 参数，`Run` 传入；
  4. 三件各补测试；**破坏验证**：R1 注入会报错的算法 stub → 「已取子单仍可达」断言红；R2 二次 Run → 现状 warn+skip 红、改后显式 error；R3 ctx 取消传播断言。
- **④ 约束与非目标**：默认路径与 K17 已冻结语义（父单号 / 窗口 / 撤销裁决 / K18 的 Cancel）零变化；不改 `ExecAlgorithm` 冻结签名；不做错误重试/补偿；不动 K17-R1（挂载失败口径）范围。
- **⑤ 验收**：三件测试绿 + 破坏腿红→还原；全量 `go test ./...` 绿；`go vet` + gofmt 干净。
- **⑥ 边界**：只做三件鲁棒性收尾，core 仓一个 atomic commit；⚠️ R1/R3 与 K17-R1 改同一批文件，**必须在其落库后执行**。

### K17-R1/R2 落库后复查发现（2026-10-13，作者复查）✅ 全部完成（RV1–RV4 四条均于 2026-10-13 收口，详见各 #### 小节⑦）

> 来源：`547564b`（K17-R1）+ `4aab98c`（K17-R2）落库后按代码评审清单复查。复查**通过项**：engine.go 的 exec-algo Code 挂载点位于 CheckValidity 之后、invalid 判定日志块之前（engine.go:924-933 行序正确）；OnBar 快路径零新增分配；四 sentinel + `errors.Is` 双侧穿透（回测 invalid_reasons / paper Run error）均有测试钉住；五条破坏腿全部可复现红。以下四条为复查新发现，按六字段立单。

| # | 级别 | 问题 | 位置 |
|---|---|---|---|
| K17-RV1 | 🟠 | OnBar 新契约（错误时已取子单照常交还）与两个调用方错误分支矛盾——err != nil 时 `continue` 把 due 连同错误一起丢弃，而 Dispatcher 已计入 submitted | `pkg/backtest/exec_algo.go:163-166`、`pkg/live/paper_exec_algo.go:143-147` |
| K17-RV2 | 🟡 | `execAlgoRunDiag.record` 无 nil 接收者防护（同组另三法均有） | `pkg/backtest/exec_algo.go:339` |
| K17-RV3 | 🟡 | 文档同步欠账：SPEC.md 稳定 Code 清单缺两条新 Code；paper Run 文档注释未提挂载失败 fail-loud；新公开 API 是否入 SPEC 待核对 | `docs/SPEC.md:1618`、`pkg/live/paper_session.go` Run 注释 |
| K17-RV4 | 🟡 | ctx 取消当日已到点子单仍报送（两侧同构）——**已拍板口径 A（维持现状，补注释）**，已落实现与测试 | 两侧 `processExecAlgoChildren` / `dispatchExecAlgoChildren` |

#### K17-RV1 · OnBar 错误分支丢弃已取子单——R2-R1 的病移到了调用方层（🟠）✅ 完成（2026-10-13）

- **① 目标**：让 `Dispatcher.OnBar` 的「错误时已取子单照常交还调用方处置」契约在调用方落地，消除「submitted 已计、实际未报」的第二次分叉。
- **② 现状**：R2-R1 改完 OnBar（遍历完全部父单、`errors.Join` 聚合、已取子单随错误一并返回），但两个调用方的错误分支仍是 `if err != nil { Warn; continue }`——把 due 连同错误一起丢掉。派发器侧 `run.submitted += c.Qty` 已记账 ⇒ 父单进度账与报送事实再次不一致，正是 R2-R1 要治的病，只是从派发器层移到了调用方层。
- **③ 触发条件与定级**：algo.OnBar 报错（当前真算法不可达——Attach 后 `planState.onBar` 不报错，保护的是未来算法实现），故 🟠 非 🔴。
- **④ 要求**：两侧错误分支改为「Warn 后仍逐笔报送 due」（报送语义与成功路径一致，错误降级为诊断信号，不丢已取子单）；补测试——约束：Engine 的 dispatcher 是 run 内部构造（engine.go runBacktestInternal），接线级注入需最小测试钩子，或断言落在 execalgo 层 + 调用方错误分支静态护栏（二选一，取舍写注释）。
- **⑤ 验收**：两侧错误分支不再丢弃 due；测试红→还原；全量 `go test ./...` 绿。
- **⑥ 边界**：不改 OnBar 契约与冻结签名；不动 K17-R1 挂载口径。
- **⑦ 收口记录（2026-10-13）**：两侧错误分支改为「Warn 后仍逐笔报送 due」。测试取 ④ 的二选一之**「execalgo 层断言 + 调用方静态护栏」**：行为判据沿用 `pkg/execalgo/dispatch_onbar_test.go` 的 `TestDispatcher_OnBarErrorReturnsTakenChildren`；调用方侧新增 AST 形态护栏 `TestCallers_NeverDropOnBarDueChildren`（`pkg/backtest/exec_algo_review_test.go`，扫两侧文件——OnBar 取值后的 `err != nil` 分支不得含 continue/return）。**不**为测试给 Engine 加生产测试缝：该错误路径正常接线不可达（Attach 后 `planState.onBar` 只在「未 Schedule」时报错，而 Attach 已拒绝空父单号）。破坏腿：还原 `continue` → 护栏红（实测）。

#### K17-RV2 · `execAlgoRunDiag.record` 缺 nil 接收者防护（🟡）✅ 完成（2026-10-13）

- **① 目标**：`record` 与同组 `empty() / invalidReasonCodes() / logSummary()` 的 nil 语义对齐（后三者均 nil-safe，唯独 record 不是）。
- **② 现状**：`record` 首行 `if dg.fails == nil` 只防**零值结构体**、不防 **nil 指针**——nil 接收者直接 panic。当前两条穿参路径（batch / fallback）都保证非 nil，属未来调用点的隐患。
- **③ 要求**：`record` 首行加 `if dg == nil { return }`；补一条 nil 接收者调用不 panic 的测试。
- **④ 验收**：nil 调用静默返回；全量绿。
- **⑤ 边界**：只加防护，不改诊断口径与计数语义。

#### K17-RV3 · 文档同步欠账（🟡）✅ 完成（2026-10-13）

- **① 目标**：把 R1 引入的可观测面登记进契约文档，消除「代码先行、文档漂移」。
- **② 现状**：`docs/SPEC.md:1618` 枚举 invalid_reasons 稳定 Code（`empty_universe` / `zero_trades` / ...），缺 `exec_algo_unknown_style` / `exec_algo_attach_failed`；`pkg/live/paper_session.go` Run 的文档注释仍写「单笔撮合失败（如现金不足、T+1 违规）记日志跳过，不中断整段回放」，未提 K17-R1 新语义「exec-algo 挂载失败 → Run 显式 error（rejected, not ignored）」；pkg/execalgo 新公开 API（`IsKnownStyle` / `ClassifyAttachError` / 四 sentinel / `StyleTWAP`·`StyleVWAP` / `AttachFailureKind`）是否需入 SPEC 待核对。
- **③ 要求**：SPEC.md 补两条 Code（各一句话语义即可）；Run 注释补一句挂载失败 fail-loud；核对新公开 API 是否需入 SPEC 清单。
- **④ 协调约束**：docs/SPEC.md 当前有并行会话未提交改动——执行时先取最新版再改，避免撞车。
- **⑤ 验收**：SPEC 与代码枚举一致（grep 两条 Code 可命中）；Run 注释与行为一致。
- **⑥ 收口记录（2026-10-13）**：SPEC.md 稳定 Code 清单已补 `exec_algo_unknown_style` / `exec_algo_attach_failed`；`pkg/live/paper_session.go` 的 Run 注释已补挂载失败 fail-loud 例外。**公开 API 核对结论：不入 SPEC** —— SPEC 登记的是**结果契约**（响应字段 / status 三值 / 稳定 Code），不枚举内部 Go 包 API；`pkg/execalgo` 的 `IsKnownStyle` / `ClassifyAttachError` / 四 sentinel / `StyleTWAP`·`StyleVWAP` / `AttachFailureKind` 属包内契约，其 wire 可见面就是那两条 Code（已登记）。

#### K17-RV4 · ctx 取消当日已到点子单仍报送——口径已拍板 A（🟡，2026-10-13 若曦裁决）✅ 完成（2026-10-13）

- **① 目标**：按已拍板口径 A 落实现与测试——取消当日已到点未报送的子单**仍照常报送**。
- **② 现状**：R2-R3 只在**挂载点**判 `ctx.Err`；当日已挂父单的到点子单在 `dispatchExecAlgoChildren` / `processExecAlgoChildren` 报送时不判 ctx（两侧同构）——取消当日仍真实成交，次日循环才中止。
- **③ 裁决（2026-10-13 若曦拍板）**：**选 A · 维持现状**——取消 = 「尽快停」，当日 bar 已真实发生，片已到点照发，与撮合语义一致；不选 B（报送前判 ctx，取消即刻零成交）。
- **④ 要求**：两侧报送点补注释写明「取消当日已到点子单仍报送」是**有意行为**（口径 A，2026-10-13 拍板），防止未来被当缺陷重报；补一条测试钉住该行为（取消注入 → 当日已到点子单仍报送）；两侧注释与测试口径必须一致。
- **⑤ 验收**：注释与测试就位；删除注释或改为判 ctx 时测试红；全量 `go test ./...` 绿。
- **⑥ 边界**：不改冻结签名；只补注释与测试，不改报送逻辑本身。
- **⑦ 收口记录（2026-10-13）**：两侧报送点已补「本报送点不判 ctx 是有意行为」注释；测试 `TestExecAlgo_CancelSameDayDueSliceStillSubmitted`（回测侧 `pkg/backtest/exec_algo_review_test.go`，取消注入当日到点片仍报送、末笔日期 == 取消日）+ `TestPaperExecAlgo_CancelSameDayDueSliceStillSubmitted`（paper 侧 `pkg/live/paper_exec_algo_review_test.go`，入场量为对照的 3/5——当日即停会掉到 2/5）。破坏腿：两侧报送前加 `if ctx.Err() != nil { continue }` → 两条腿均红（实测）。

---

---

## 七、AI / core 边界归属（2026-10-09 若曦拍板，DDD 判定）

判据（若曦定的）：**「AI 层每次都要用、而 core 层没有用它的地方 ⇒ 放进 AI 层」**，并用 DDD 复核。

| 包 | 判定 | 依据 | 状态 |
|---|---|---|---|
| `expression`（原 `pkg/ai/expression`） | → **core**（`pkg/expression`） | 零 AI 概念、零非确定性、只依赖 core ⇒ 一个核心层包住错了目录 | ✅ `55ea33f` |
| `contracts`（原 `pkg/ai/contracts`） | → **core**（并入 `pkg/backtest/contracts`） | 1 个接口 `BacktestRunner`；6 个消费方里 4 个本就是 core | ✅ `55ea33f` |
| `validation`（原 `pkg/validation`，4920 行） | → **AI**（`pkg/ai/validation`） | core 侧零引用；它是**实验员对假设的判断**（6 维 → `Verdict` 概率），不是回测·执行域的仪器 | ✅ `cff0a0f` |
| `tools`（原 `pkg/tools`，10716 行，含 `builtin/`） | → **AI**（`pkg/ai/tools`） | core 侧零引用；MCP 桥 = 开放主机服务 / **ACL，属边界消费侧** | ✅ `cff0a0f` |
| `strategy/copilot.go` 的 `CopilotService`（518 行） | → **AI** | 自然语言 → 生成代码 → 沙箱编译 → 回测 = 实验员的活，却住在仪器包里 | ✅ 2026-10-09 随 AI 拆仓阶段 2 迁入 agent 仓 `pkg/copilot` |
| `pkg/ai/prompts/{factor_research,strategy_generate}.txt` | **✅ 已清理（`6455566`）** | 零引用的孤儿；`factor_research.txt` 严重漂移（广告 7 个幻影字段）被 `research.go` 派生版超越 ⇒ 删；`strategy_generate.txt` 的领域知识（负 PE 是亏损、用 neg(pe) 等）合并进 `generate.go` 活提示词 ⇒ 删文件。消除双真相 | ✅ |
| `pkg/backtest/marketimpact` | → **core**（`pkg/marketimpact`） | 共享成本模型却住在 `backtest` 下；K5 的 paper 侧也要用它 ⇒ 提到顶层与 `pkg/fees` 平级（`pkg/fees` 同为共享成本模型，被 `backtest`/`live`/`portfolio`/`ai` 共用） | ✅ K5 切片 1 |

**契约变更（随 `validation` 归 AI 生效）**：**凡存入策略库的策略，均已由 AI 层验证通过**；core 侧的策略存储不做也不应做独立验证。详见 ADR-027 追加节。

**不变量**（`internal/repoguard/ai_boundary_test.go` 强制）：只有 `pkg/ai/*`、`cmd/*`、`e2e/*` 可以 import `pkg/ai/*` —— 白名单式 **fail-closed**（新包默认拒绝），只扫生产代码。

---

## 八、本地栈拉起 + A-05 前端实测发现（2026-10-14）

起因：给 A-05（六维验证器组件）补浏览器真实数据实测，需要先拉起本地栈。
`tools/local-stack.sh start` 从头跑了一遍，抓出 4 条**只在 compose 形态下存在**的缺口 ——
共同根因是「本地一直 `go run`、CI 不构建镜像」，所以编译期不检查的东西从未被跑过。
前 4 条已修（两个仓的 feature branch，未 push），后 2 条待裁决。

| # | 发现 | 症状 | 状态 |
|---|---|---|---|
| 1 | `Dockerfile.service` 末行多了一个 `\\` | Docker 把最后一个反斜杠当行继续，留下字面 `\` 交给 shell ⇒ `go build` 收到额外参数解析成 `/` ⇒ `no Go files in /`，**三个镜像全建不出来**。迷惑性在于 `docker compose ps` 显示三个服务 healthy —— 跑的是这个 typo 落地**之前**构建的旧镜像 | ✅ core `fix/a05-env-stack-startup` |
| 2 | compose 里 analysis-service 缺 `AI_SERVICE_URL` | `handlers_ai_proxy.go` 的 `ai_service.url` 缺省 `http://localhost:8086`，宿主机直跑恰好命中，容器里那是空地址 ⇒ **copilot / pipeline / explore / tools 四族全 503**，而 ai-service 自身 healthcheck 一直绿。症状是「AI 服务活着但前端全红」 | ✅ core `fix/a05-env-stack-startup` |
| 3 | agent 仓 `go.mod` 缺 15 条 indirect | 拆仓阶段 2 迁入 `cmd/ai` 时只手工列了直接依赖；`-mod=readonly` 下 import graph 与 go.mod 不平直接拒绝构建 ⇒ ai-service 镜像 build 失败并让整条 `docker compose up` 中断，**连带另外三个服务也起不来** | ✅ agent `fix/a05-env-ai-service-image` |
| 4 | agent 镜像缺 `wait-for-deps.sh` 与 `config/ai-service.yaml` | 前者 compose entrypoint 引用它 ⇒ exit 2；后者 `cmd/ai/main.go` 按相对路径读，文件留在 core 没迁 ⇒ `logger.Fatal`。两样都经命名上下文 `core` 取，不在 agent 仓复制第二份真相 | ✅ agent `fix/a05-env-ai-service-image` |
| 5 | 实测看到 `Status=invalid` —— **那是护栏在正常工作，不是新缺口** | 真跑一轮 `/explore`（run `c46fa2ad`），两次尝试的回测都是 `universe_max_size=0 / zero_trades / garbage_metric` ⇒ 被标 `invalid` 而非报成功。**这条本表 §四 已跟踪**（OBS-01 / ADR-030，见 `docs/TASKS.md` 「① 目标：回测/验证在「0 交易 / 空票池」时必须 fail-loud」），我看到的就是它落地的效果。本轮只补一个**机制更正**，见下方「§八 的 5 号条目复查」 | 🔵 已跟踪（OBS-01），本轮补机制 |
| 6 | 状态轮询要不要进 `RateLimitExemptPaths` | **已裁决：不进**，理由见下方「§八 的 6 号条目裁决」 | ✅ 已裁决（不改豁免表） |
| 7 | **偏差维把没测过的数字打印成实测结果** | 见下方独立工单 OBS-01-R1 | 🔴 新立 |
| 8 | **AI 链路把 K9 三值丢在客户端边界上** | 见下方独立工单 OBS-01-R2（A-07 收口时逐字核出） | 🔴 新立 |

**§八 的 5 号条目复查（2026-10-14 当日自查，结论反转）**

登记时我写的是「股票池恒为 0 只，是 G 组新缺口」，并且把裁决里那句
「股票池按**今天**的上市名单取（0 只）」当成引擎量出来的票池规模。**两句都不准确**，
逐字读码后更正：

1. **那个「0 只」不是测量值。** `cmd/ai/handlers_explore.go:50` 只设了
   `PoolSource: PoolSourceCurrent` 而**从不设 `PoolSize`**（只有 `:96` 那条
   预热成功的路径才 `in.PoolSize = len(windows)`），于是 `bias.go:303` 的
   `fmt.Sprintf("...（%d 只）...", in.PoolSize)` 打印的是 Go 零值。
   它既不是引擎的票池规模，也和 `universe_max_size=0` 只是**碰巧同为 0**。
   ⇒ 这条本身是新缺陷，另立 **OBS-01-R1**（下表）。
2. **票池为空的根因本表早已登记**（`index_constituents` / `sectors` /
   `stock_sector_map` 全 0 行 ⇒ YAML 默认 `universe: csi300` 解析为空票池）。
   今日实测三张表**仍然各 0 行**，缺口没动过。
3. 本轮新查清的是**它为什么解析为空**：`pkg/backtest/engine.go:556` 的
   `resolveStockPool` 只在 `len(StockPool)==0 && req.IndexCode != ""` 时才展开指数，
   而探索链路（`intent/parser.go:117` → `yaml/generator.go:198` →
   `pipeline.go:868 parseUniverse` → `cmd/ai/builders.go:47`）把字面量
   `"csi300"` 放进 `StockPool`、**从不填 `IndexCode`** ⇒ 展开分支**结构上不可达**，
   `"csi300"` 被当成一只股票代码，进不了上市日历（`engine.go:1125` 以
   **Debug** 级丢弃），票池归零。
   ⇒ 对 OBS-01 的增量：**光把三张表灌满数据还不够**，还得让 `"csi300"` 走
   `IndexCode` 而不是 `StockPool`，否则展开分支照样不可达。这是给那张工单的
   「③ 要求」补的一条验收，不另开工单。

| # | 工单 | 根因（已逐字核对） | 修法 |
|---|---|---|---|
| 7 | **OBS-01-R1 · 偏差维输出一个它没量过的数字** | `cmd/ai/handlers_explore.go:50` 设 `PoolSource=PoolSourceCurrent` 但不设 `PoolSize`；`pkg/ai/validation/bias.go:296-303` 无条件把 `in.PoolSize` 插进 blocking 文案 ⇒ 屏幕上出现「股票池按今天的上市名单取（**0 只**）」。0 是零值，不是测量结果。**违反愿景原则 4（每个数字可被当场质问）**：主任若追问「哪来的 0 只」，诚实答案是「没量过」——而这正是 F5 这块组件存在的意义 | 池子规模未知时**不要打印数**，改成不带数量的措辞（`PoolSize<=0` 已有先例：`bias.go:202` 与 `:314` 都按「未测量」分支处理，只有 `PoolSourceCurrent` 这一支漏了）。**注意概率侧没有这个 bug**：`biasProbability` 对 `PoolSourceCurrent` 提前返回 `BiasProbFloor`，不看 `PoolSize`，所以低分是有意为之；错的只有那句话。**非目标**：不借机重构偏差维，不改 `PoolSourceCurrent` 的概率 |
| 8 | **OBS-01-R2 · AI 链路把 K9 三值丢在客户端边界上** | `pkg/ai/client/backtest_client.go:47` 的 `RunBacktest` 明明**解出了** `BacktestResponse.Status`（同文件 `:43`），返回值却只有 `*domain.BacktestResult` ⇒ 三值在这一步被丢弃；而 `pkg/domain/backtest.go:5-39` 的 `BacktestResult` **没有 `Status` 字段**（只有 `InvalidReasons`，`:38`）。于是任何 AI 侧消费者（Copilot 播报、实验日志、`pkg/ai/loop` 的候选评估）从 payload 里**读不到 completed / invalid / failed**，唯一活下来的信号是 `invalid_reasons`。⚠️ **这不是用户可见的假成功**：`pkg/backtest/engine.go:924` 每次返回前都会跑 `CheckValidity` 并挂上这组 Code（有效 = 空数组），前端 A-07 已把它当判据接住（`web/src/utils/runStatus.ts` 的 `backtestValidityNote`：status 到了按三值，没到就退回 `invalid_reasons` **并把判据说出来**）。登记的是**契约残缺**，不是渲染缺陷 | 两条路，**属 Ask First**（要动 `pkg/domain` 核心类型或 AI 客户端公开签名）：**(a)** `domain.BacktestResult` 加 `Status` 字段，由引擎在 `runBacktestInternal` 末尾按 `contracts.Status*` 常量填；**(b)** `RunBacktest` 改返回一个带 `Status`/`Error` 的小结构（不动 domain）。倾向 (b) —— 三值在现有规格里本来就属于**响应层**（`contracts.go:250-252` 把 Status 与 Error/InvalidReasons 的搭配写在响应语义上），把它塞进"一次运行的指标集合"会让同一个概念有两处真相。**验收**：(1) 新增单测断言 `RunBacktest` 的返回能区分 completed / invalid / failed 三值，且 invalid 时 `InvalidReasons` 非空；(2) 跑一次 `POST /api/backtest` 真请求确认线上 JSON 的 `status` 与结构体字段一致。**非目标**：不改前端（它已自救并有 7 条单测钉住）；不借机重构 pipeline 的结果结构 |

**§八 的 6 号条目裁决（2026-10-14，本轮定）**

**不把 `/api/explore/runs/:id` 加进 `RateLimitExemptPaths`。** 三条理由，第一条是硬事实：

1. **机制上做不到**：`IsRateLimitExempt` 是**逐字相等**匹配（`server.go` 里明写
   「不做前缀匹配 —— 前缀匹配会让 `/api/auth/status/../login` 这类写法有机会绕过限流」）。
   而这条路径带 run_id 动态段，逐字表**永远匹配不上**。要放开就得改成前缀匹配，
   那是拿一条有明确安全理由的不变量去换一个便利 —— 不划算。
2. **判据本来也不满足**：该表的入选条件是「限流它会不会让正常客户端分不清 429 与 401」。
   状态轮询 429 了不会把用户弹去登录页，与 `/api/auth/status` 那种「一次 429 毁掉整站」不同级。
3. **正确的修法在后端另处，且不急**：观察页的真解是 **SSE**（本仓已有先例
   `/api/sync/stream`，前端也已接 `EventSource`），或给只读状态端点单列预算。
   两者都是后端设计，不在前端手里改。

前端已在自己那一层自救完毕（`POLL_MS=2500` + 连续 6 次失败才认断线 + 停下时明说
「屏幕上的是最后一次成功拿到的进展」），并有 e2e 钉住。所以这条**关闭**，
SSE 化留作 P3 待办，不占裁决位。

**给「拉起环境」这条路的通用教训**（与 `Dockerfile.service` 里已记录的那条 `COPY cmd/analysis/static`
事故同型）：`go build ./...` 与单元测试都不检查 Dockerfile 的 COPY/RUN 目标是否存在，
`docker compose ps` 的 healthy 也不区分「新镜像」与「上次构建的旧镜像」。
**判据只能是带 `--build` 的完整 up + 逐服务 HTTP 探针**，也就是 `local-stack.sh` 现在做的事。

证据全文见前端仓
`quant-trading-ui/docs/design/stitch_quantlab_ux_redesign/TASKS.md` 的 A-05 节
（屏幕逐项核对表 + 三个前端缺陷及各自的破坏验证）。此处只留指针，不双写。

---

## 九、ODR-067 改判带来的两条登记（2026-10-14）

### DOCS-01 · AGENTS.md 的文档指针在文档拆仓后集体失效（🟠）

- **① 目标**：`AGENTS.md` 里每条 `docs/...` 引用都能在某个工作树命中，且由一条**机器断言**守住（新增死链当场变红）。
- **② 上下文**（2026-10-14 实测，非估计）：`AGENTS.md` 共 **114** 处 `docs/*.md` 引用，其中 **34 个不同目标在本仓不存在**。逐个查过去向：

  | 落点 | 目标数 | 代表（括号内为引用处数） |
  |---|---|---|
  | 已迁 `quant-trading-docs` 仓 | 28 | `docs/PRODUCT.md`(9) · `docs/VISION.md`(6) · `docs/ADR.md`(6) · `docs/TEST.md`(3) · `docs/adr/adr-023-*`(7) · `docs/adr/adr-024-*`(5) · `docs/archive/superseded-adr/adr-022-*`(5) · `docs/archive/tasks-phase-2.md`(3) · `docs/archive/odr/*`(9 条) · `docs/ROADMAP.md`(2) · `docs/AGENTS_TEMPLATE.md` |
  | 已迁 `quant-trading-ui` 仓 | 3 | `docs/design/index.md` · `visual.md` · `components.md`（§11「前端设计系统」那行三条全死） |
  | **三仓历史里从未存在（幻影引用）** | 3 | `docs/E2E_TEST_GUIDE.md`（`git log --all -- <path>` 在 core / docs / ui 均 **0 命中** ⇒ 这个文件从未有过；它承诺的内容实际在 docs 仓 `docs/TEST.md` §7.2「E2E Tests — Playwright」）· `docs/archive/README.md` · `docs/archive/RESEARCH-equitydeep-legacy.md`（后两条按同法复核，命中历史即归上表） |

  本仓 `docs/` 现只剩 `SPEC.md`/`ARCHITECTURE.md`/`TASKS.md`/`migrations/`/`design/kernel/`/`openapi.yaml`/`embed.go`，且 **`docs/archive/odr/` 是空目录（0 个文件）** —— 而 `AGENTS.md` §3 目录树、§10 Rule 2、§11 导航仍按「ADR/ODR 都在本仓」描述。
- **③ 要求**：先做**归因不改链**（= ② 已完成）；再三选一并在工单里写理由 —— **(a)** core `docs/` 放转发存根（每个死链一个文件，正文只写「已迁 X 仓 yyy.md」）／**(b)** 改显式跨仓措辞（`quant-trading-docs:docs/PRODUCT.md`，不带可解析相对链接）／**(c)** 建一份「文档归属表」，正文只引用该表。**排除条件**：三个仓不保证在同一目录布局下被检出（CI、他人机器），故**任何依赖 `../quant-trading-docs/...` 相对路径的方案先排除** —— 动之前先确认这一条。无论选哪条，**必须同时落守卫**：扫 `AGENTS.md`（及 core `docs/*.md`）的相对链接，命中不了即红；可抄 ui 仓 A-02/A-03 那组「CSS 变量引用完整性」棘轮基线（悬空集合必须等于登记集，双向红）。
- **④ 约束与非目标**：不改任何文档正文的判断内容（只处理指针）；不重排 `AGENTS.md` 章节；不改 `docs/archive/` 归档内容（AGENTS.md §4 规定归档不可变，**唯一例外正是失效相对链接可修** —— 本工单落在这个例外内）；不顺手修「指向仍存在文件」的措辞问题。
- **⑤ 验收**：守卫存在且默认随检查跑；**首次上线必须当场抓到这 34 条**（抓不到 = 假护栏，须做「改坏→变红→还原」）；三条幻影引用要么补出文件、要么从 `AGENTS.md` 删除，不留悬空链接。
- **⑥ 边界**：只做指针 + 守卫；文档内容重写与 `AGENTS.md` 版本史整理另立。

### ODR-067 的补偿依赖（是一条前提，不是工单）

直推 `main` 之后，「这条提交弄坏了」与「本来就坏了」只能靠**红登记在案**来区分。而当日实测
全量 e2e 是 **61 failed / 116 passed / 2 skipped**（退出码 1）。所以 ui 仓 **AUD-61 阶段一**
（61 条逐条 `test.fixme` + 登记表一行，并要求「去掉 fixme 必须重新变红」的破坏验证）从
「测试债清理」升格为**主干工作流的前置条件** —— 不做，直推 main 等于把红灯与提交历史解耦。
实测分布、四类根因与逐条证据都在 ui 仓 `docs/TASKS.md` 的 AUD-61 节，此处只留指针，不双写。

---

## 维护本文件

- 新增 task 必须按 [AGENTS.md §8.5](../../AGENTS.md) 写全六字段（目标/上下文/要求/约束与非目标/验收/边界）。
- 完成即标 ✅（附日期）并定期移入 `archive/`；本文件只留未完成项。
- 大任务（K0–K7）开工前，若六字段不够细，先补细再派工——**独立可运行判据达不到 = task 没写好，不是 agent 的问题**。
