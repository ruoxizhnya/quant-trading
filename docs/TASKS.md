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

### K0 · 契约冻结（P0）⬜ —— 当前最优先

- **① 目标**：把 11 个内核模块的四类契约写死并冻结，作为后续所有并行开发的「宪法」。
- **② 上下文**：蓝图 §5 模块矩阵 + §5.1 模块边界契约（`docs/design/kernel/`）；现有接口样板 `pkg/strategy/interfaces.go`（4 子接口 + 编译期合规检查 + `interfaces_compliance_test.go`）、`pkg/marketdata/provider.go`、`pkg/live/engine.go`（Broker/DataFeed）、`pkg/domain/types.go`（RiskManager）；契约落点 `contracts/`（已存在）。
- **③ 要求**：为 11 个模块（kernel / clock / msgbus / eventstore / data-engine / portfolio / risk-engine / exec-engine / strategy-runtime / indicators / exec-algo）各产出四类契约——**接口契约** `pkg/<module>/interfaces.go`（签名 + 编译期合规 `var _ Iface = (*T)(nil)`）；**消息契约** `pkg/kernel/topics.go`（topic 名 + payload，集中定义，switchboard 模式）；**DB 契约** `contracts/schema.sql`（表 DDL + 读写归属，新表见 SPEC §模块化内核新表 8 张）；**测试契约** `pkg/<module>/interfaces_compliance_test.go`（合规 + 破坏验证）。
- **④ 约束与非目标**：**只写契约（接口/类型/DDL/测试签名），不写实现逻辑**（实现属 K1+）；不改任何现有模块实现；冻结后改契约须在 task 显式登记变更评审。
- **⑤ 验收**：`go build ./...` 通过；每模块合规测试通过；**独立可运行判据自证**（任取一模块，其四类契约能让不了解其他模块的 agent 说清「实现什么接口 / 写哪张表 / 发收什么消息 / 过什么测试」）；破坏验证——删某模块一个接口方法 → 其合规测试变红。
- **⑥ 边界**：只做 11 个模块 × 四类契约定义，不做任何实现。

### K1 · 内核骨架（P1）⬜（前置：K0）

- **① 目标**：kernel + clock + msgbus（命名注册表 + 同步分发）+ eventstore 落地，`cmd/analysis/setup.go` 的 831 行硬编码装配被 `Kernel.Boot/Shutdown` 取代，`audit.message_log` 落库（D4）。
- **② 上下文**：蓝图 §4.1 形态 + §4.2 生命周期（Boot/Shutdown 顺序契约）+ §5 矩阵；`cmd/analysis/setup.go`（当前硬编码）；K0 冻结的契约。
- **③ 要求**：`pkg/kernel`（装配 + 生命周期，EventStore 先起 / MsgBus 最后开）；`pkg/clock`（`Clock` 接口 + `VirtualClock`/`LiveClock`）；`pkg/kernel/topics.go` 同步分发（**不并发**，单线程语义）；`pkg/eventstore`（消息**先记录后分发**，BusTap 语义，落 `audit.message_log`）。
- **④ 约束与非目标**：MsgBus **不并发**（现有 `marketdata.EventBus` 是 4 goroutine 抢 channel，**不接线、不照搬**，见 ADR-030 OBS-04）；真实券商对接不做（D1）。
- **⑤ 验收**：`Kernel.Boot` 装配顺序符合契约（EventStore 第一个、MsgBus 最后）；关停逆序；`audit.message_log` 有启动期消息；破坏验证——打乱 Boot 顺序 → 对应测试变红。
- **⑥ 边界**：只做 4 个模块的骨架 + 装配替换，不动策略/回测逻辑。

### K2 · 流式接口（P2）⬜（前置：K1）

- **① 目标**：strategy-runtime 加流式 `BarHandler` + VirtualClock 驱动 + 同构桥，使批式策略（L0/L1）无改动可上 paper、流式策略（L2/L3）可回测。
- **② 上下文**：蓝图 §6.2 双模式接口；SPEC §Streaming Strategy Interface（`docs/SPEC.md`）；`pkg/strategy/interfaces.go`（现有批式 SignalGenerator）；`pkg/backtest/engine.go:669`（date-loop）。
- **③ 要求**：`BarHandler` 接口（`OnBar`/`Warmup`/`SaveState`/`LoadState`）；引擎按策略实现接口自动选执行模式；批式→流式桥（引擎按 Warmup 攒窗口调 `GenerateSignals`）；流式→回测（VirtualClock 逐 bar 喂 `OnBar`）；`quant.strategy_state` 状态持久化。
- **④ 约束与非目标**：批式 `SignalGenerator` **保留不动**（向后兼容）；不改横截面 `cs_rank` 路径（date-loop 保留）。
- **⑤ 验收**：同一批式策略对象在回测（VirtualClock+快照）与 paper（LiveClock+feed）跑，**代码一行不动**、信号一致；流式策略 `SaveState`/`LoadState` 断点续跑结果一致；破坏验证——`get_bar(t_offset>0)` → trap。
- **⑥ 边界**：只做策略运行时的双模式与同构桥，不做具体 L2 算子（K3）或 WASM（K6）。

### K3 · L2 算子（P3）⬜（前置：K2）

- **① 目标**：indicators 模块落地 L2 确定性有状态算子（`ts_kalman`/`ts_ewma`/`ts_rma`），warmup 可静态推导（D2：能用 L2 不上 L3）。
- **② 上下文**：蓝图 §6.3（L2 优先）；ADR-028 §2（L2 递推算子）+ §4 算子声明契约（含 `state` 字段）+ §8 warmup 推导；K2 的 `BarHandler`。
- **③ 要求**：`Indicator` trait 契约（`Update(bar)`/`Value()`/`Reset()`/`Warmup()`，未实现 fail-loud）；`ts_kalman`/`ts_ewma`/`ts_rma` 实现；warmup 静态推导；三路一致性（`Batch ≡ Step ≡ Step-from-persisted`，ADR-028 §7）。
- **④ 约束与非目标**：算子集**有限可枚举**（不允许 LLM 动态新增）；只用 L2，不提前引 WASM（K6）。
- **⑤ 验收**：`ts_rma(tr,14)` 与手算 Wilder ATR 逐点一致；三路一致性测试通过；warmup 推导与实际消耗 bar 数一致；破坏验证——改递推系数 → 属性测试变红。
- **⑥ 边界**：只做 L2 算子与 warmup 推导，不做 WASM / 外部模型。

### K4 · 执行算法 + 市场冲击（P4）⬜（前置：K1，独立）

- **① 目标**：exec-algo 模块落地 TWAP/VWAP 执行算法（与策略同构 trait），撮合引擎加市场冲击模型（D5：模拟真实大单）。
- **② 上下文**：蓝图 UC4（执行算法拆单）；nautilus `crates/trading/src/algorithm/`（TWAP 同构）；K1 的 exec-engine 挂点。
- **③ 要求**：`ExecAlgorithm` 接口（`OnBar`/`OnFill`/`Schedule()`）；TWAP/VWAP 实现（父订单拆子订单，按时间片）；撮合市场冲击模型（滑点随订单量占盘口/成交量比例增大，大单分层成交）。
- **④ 约束与非目标**：执行算法与策略同构但**不取代**策略接口；真实券商算法单直连不做（D1）。
- **⑤ 验收**：TWAP 拆单后子订单时间分布均匀；市场冲击下大单滑点 > 小单滑点；破坏验证——去掉冲击模型 → 大小单滑点相同（反证）。
- **⑥ 边界**：只做执行算法与冲击模型，不做策略、不做真实券商。

### K5 · 实盘 paper（P5）⬜（前置：K2）

- **① 目标**：DataSource=RealtimeFeed + Broker=Mock 的 paper trading 跑通，并与同策略同日回测做同构对账（D1）。
- **② 上下文**：蓝图 §4.1.1 实盘-ready 分寸 + UC3；`pkg/live/`（Broker/MockTrader/OrderManager/reconciliation）；`live-trading.md`；K2 的同构桥。
- **③ 要求**：RealtimeFeed 接入 + bar 聚合；Mock 撮合（滑点+费用+冲击）；每日 paper 成交 vs 同策略同日回测成交对账，偏差超阈 → AlertDispatcher 告警；`pkg/live/reconciliation/` 对账（BrokerQuerier vs LocalSnapshotter）。
- **④ 约束与非目标**：**只做 paper，不接真实券商**（D1）；核心抽象按实盘-ready 设计留冗余，实现只 paper。
- **⑤ 验收**：同一策略 paper 与回测信号/成交一致（容差内）；对账差异超阈触发告警；破坏验证——paper 与回测用不同时钟 → 对账报差异。
- **⑥ 边界**：只做 paper trading 与同构对账，不做真实券商、不做合规报送。

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

### OBS-01 · 空票池假成功必须 fail-loud ⬜ —— 强烈建议最先做

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
| OBS-06 | DSL 语法闸门不校验算子名（`CROSS(MA(...))` 两个不存在算子判 `valid=true`）——L1 补算子白名单 | 高（假合法） |
| OBS-07 | tokenizer 不支持 `>=`/`<=`/`AND`/`OR`/`NOT` | 中 |
| OBS-08 | 序列注册表缺「可用性声明」（11 张表全 0 行，AI 在空表上静默产垃圾） | 高（与 OBS-01 同源） |
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

## 维护本文件

- 新增 task 必须按 [AGENTS.md §8.5](../AGENTS.md) 写全六字段（目标/上下文/要求/约束与非目标/验收/边界）。
- 完成即标 ✅（附日期）并定期移入 `archive/`；本文件只留未完成项。
- 大任务（K0–K7）开工前，若六字段不够细，先补细再派工——**独立可运行判据达不到 = task 没写好，不是 agent 的问题**。
