# 内核可插拔 + 外围 NT-ready：需求与设计

> **状态**：Accepted / 分阶段实施（阶段 1–2 已授权开工，阶段 3 待决策门，见 §6）
> **日期**：2026-10-12
> **作者**：若曦 × WorkBuddy
> **定位**：**新需求固化文档**（需求 + 设计细节）。决策理由在 ADR-031（`quant-trading-docs/docs/adr/adr-031-kernel-pluggable-nt-ready.md`），可行性证据在 [nautilus-trader-kernel-evaluation.md](nautilus-trader-kernel-evaluation.md)（下称「调研报告」），模块化内核基础在 [target-architecture-modular-kernel.md](target-architecture-modular-kernel.md)。本文不重复论证，只定义**要什么（需求）**与**怎么落（设计）**。
> **与旧文档的冲突裁决**：与既有未完成规划冲突处**以本文为准**（ADR-031 §关系）；模块化内核蓝图 D1–D5 与 K0–K7 成果全部继承，不推翻。

---

## 0. 一页结论

把回测内核从「平台的器官」降级为**可替换的执行器**：外围（dashboard / AI 层 / 策略层 / 编排层）只依赖四条冻结契约（**RunSpec / SignalSpec / ResultSpec / EngineRunner**），不感知内核实现。NautilusTrader（NT）以**侧车 Python 服务**经「信号—权重回放适配器」接入为第二实现——Go 策略资产与外围服务零重写，A 股规则自建收缩到适配器层。分三阶段推进，阶段 1 不依赖 NT 任何决策即独立成立。

---

## 1. 需求（功能需求）

> 需求描述产品/系统行为，不绑定实现。设计细节见 §2–§5。

### FR-1 内核可替换（核心）

- **FR-1.1** 一次回测运行请求可以指定由哪个内核执行；未指定时走 Go 内核，**现有全部行为逐字节不变**。
- **FR-1.2** 外部引擎（如 NT 侧车）只要实现 EngineRunner 契约并产出等价 ResultSpec，即可作为执行器接入，**不修改任何外围服务**。
- **FR-1.3** 路由到不可用/未知内核时**拒绝运行并明确报错**（fail-closed），不得静默回退到 Go 内核（防止「以为在测 NT 其实在测 Go」）。

### FR-2 信号供给接口化（SignalSpec）

- **FR-2.1** 「策略产出信号」从引擎进程内调用抽象为 `SignalProvider` 接口；本地策略注册表实现为**默认且行为不变**。
- **FR-2.2** 信号供给可以来自进程外（外部模型 / 远端服务），消费语义与 K7 信号表一致：**按 `as_of` 拉取、未来信号物理不可见**（防前视语义不因接口化而放松）。
- **FR-2.3** 同一份信号流可以同时喂给多个内核（双跑对齐的前提）。

### FR-3 结果语义全链路一致（ResultSpec）

- **FR-3.1** 回测结果 schema（收益率 / 夏普 / 最大回撤 / 交易明细 / 组合净值）冻结为 canonical 定义；指标口径（封闭系统日收益，见 `pkg/backtest/metrics/performance.go`）写入规范，内核实现各自对齐。
- **FR-3.2** 结果的 `status` 语义（`completed` / `invalid` / `failed`）在**所有**呈现路径一致：API、报告、对比、**UI**。无效运行（空票池 / 零成交 / 垃圾指标，OBS-01）不得按成功呈现。

### FR-4 规则配置声明式（RunSpec）

- **FR-4.1** A 股交易规则（T+1、涨跌停、费率）以**声明式配置**随运行请求传递，由各内核/适配器解释；Go 内核沿用现有内建规则，行为不变。
- **FR-4.2** 运行请求可携带内核路由字段与引擎特有参数（引擎不识别的参数拒绝而非忽略）。

### FR-5 NT 侧车执行（阶段 2）

- **FR-5.1** NT 侧车以独立 Python 进程提供回测执行服务，只经 EngineRunner 与 Go 侧交互，不直连数据库、不对外暴露端口。
- **FR-5.2** 权重回放适配器：每个调仓日从信号流读目标权重 → 生成调仓订单；**卖单不超过昨仓可卖量（T+1）**、**触及涨跌停不追单**；印花税/过户费/最低佣金映射到费率模型。
- **FR-5.3** 侧车结果映射回 ResultSpec（含 status 语义），落库与 Go 内核产物同表同口径，调用方无感。

### FR-6 双跑对齐（阶段 2）

- **FR-6.1** 同一信号流、同一时间窗、同一费率配置喂两个内核，产出可逐项对比的运行对；差异超阈必须被显式报告（阈值 [TBD]，阶段 2 实测定）。
- **FR-6.2** 对齐实验可复现（固定种子 / 确定性回放），结论可归因到「执行机制差异」而非随机性。

### FR-7 编排不变

job / batch / walk-forward 编排**留在 Go 侧**，对内核的调用一律经 EngineRunner；参数网格、走查、并行度语义不因内核替换而变化。

---

## 2. 四契约设计

### 2.1 总表（现状基础 → 补全 → NT 对接形态）

| 契约 | 现状基础 | 需要做的 | NT 对接形态 |
|---|---|---|---|
| **RunSpec** | `BacktestRequest`（`pkg/backtest/contracts/contracts.go`）+ `TradingConfig` | 增 `kernel` 路由字段 + 引擎参数块；规则配置保持声明式 | 映射 `BacktestRunConfig` |
| **SignalSpec** | `strategy.Signal`（`pkg/strategy/strategy.go`）+ `quant.external_signals` + `SignalStore`（`pkg/strategy/signal_store.go`） | `SignalProvider` 接口化；本地/远程实现可插拔 | 适配器消费同一流 |
| **ResultSpec** | `BacktestResponse` / `domain.BacktestResult` | 冻结 canonical schema + status 语义收口（消灭硬编码 Status） | pandas 报告 → ResultSpec 映射层 |
| **EngineRunner** | `contracts.BacktestRunner`（`pkg/backtest/contracts/runner.go`） | 可插拔 runner 注册 + 路由 | NT 侧车客户端实现该接口 |

### 2.2 SignalProvider（阶段 1 核心新增）

```go
// pkg/strategy（草案，落地时以 K8 任务的冻结契约为准）
type SignalProvider interface {
    // 返回 as_of <= asOf 的信号（未来信号物理不可见，语义与 SignalStore.ListFor 一致）。
    Signals(ctx context.Context, scope Scope, asOf time.Time) ([]Signal, error)
}
```

- **本地实现** `LocalSignalProvider`：包装现有注册表 + `GenerateSignals` 调用点，行为逐字节不变（回测引擎 `pkg/backtest/engine.go` 的 `getSignalsFromLocalStrategy` 路径改为经接口调用）。
- **远程实现**（阶段 2）：HTTP / Redis 读 `quant.external_signals` 同款语义；K7 的 `SignalStrategy` + `SignalStore`（as_of 拉取、注入→成交 e2e 已验证）是现成雏形，**复用而非重造**。
- 防前视是**契约级语义**：任何实现返回 `as_of > asOf` 的信号即违约（K7 `rejectFutureSignals` 的拒绝语义上提为接口约定）。

### 2.3 ResultSpec 冻结要点

- canonical 字段集 = `BacktestResponse` 现有字段（收益率 / 夏普 / sortino / 最大回撤 / 交易明细 / 净值曲线），**冻结后新增字段须走变更评审**；
- `status` 三值语义（OBS-01 已定义 `CheckValidity` 与 `invalid` 原因码）成为契约的一部分：`completed`（有效）/ `invalid`（跑完但无效，reasons 必填）/ `failed`（未跑完）；
- 消灭「各处硬编码 Status」：`pkg/backtest/reporting/compare.go` 与 UI 渲染判据统一到 `buildResponseFromState` 同源口径。

### 2.4 EngineRunner 可插拔

- `BacktestRunner` 窄接口不动签名；新增**执行器注册与路由**：`RunSpec.kernel`（如 `"go"` / `"nautilus"`）→ 查注册表 → 取 runner；
- Go 内核 runner = 现有实现包装，默认注册、默认选中；
- NT runner = 侧车客户端（HTTP/gRPC，[TBD: 通道选型，阶段 2 裁决]），超时/错误透传为 `failed`，不得吞错。

---

## 3. 信号—权重回放适配器（NT 侧，阶段 2 核心设计）

通用 Python Strategy，**唯一职责**：把 SignalSpec 信号流翻译成 NT 订单流。

1. **消费**：每个调仓日读 `as_of <= 当日` 的目标权重信号（与 Go 侧 `SignalStrategy` 同一消费语义——回测=重放历史、实盘=读到今天为止）；
2. **下单**：目标权重 → 目标仓位 → 调仓订单（`order_factory`）；
3. **A 股最小规则集**（适配器层自建，不扩展 NT venue 模型）：
   - T+1：**卖单量 ≤ 昨仓可卖量**（今仓不可卖）；
   - 涨跌停：**触及涨停不追买、触及跌停不追卖**；
   - 费率：印花税（卖出）/ 过户费 / 最低佣金映射 `MakerTakerFeeModel` 或自定义 fee model；
   - [TBD: ST/新股价格限制、集合竞价是否进入最小集，阶段 2 按双跑需要裁决]；
4. **对齐友好**：适配器不做任何 Go 侧没有的执行增强（不用冰山单 / 部分成交模型的默认增强），保证差异可归因。

**代价声明**：策略表达被限定为「日频目标权重」——这是现有信号—权重体系的表达能力上限，对存量策略无损失；需要 L2/L3 微观结构保真的研究（执行算法、冲击成本）走 NT 原生策略的将来扩展，不在本路线范围。

---

## 4. NT 侧车服务（阶段 2）

- **形态**：独立 Python 进程（K13 裁决：**HTTP/1.1 + JSON**，标准库 `http.server` 包装 `BacktestNode`/`BacktestRunConfig`，见 §8.1），仅监听回环，Go 侧经 EngineRunner 客户端调用（安全约束见 ADR-031 D5）；
- **数据馈送**：Postgres `ohlcv_daily_qfq` → Parquet 导出（`ParquetDataCatalog`，纳秒 UTC 时间戳口径，**离线 ETL 脚本**；侧车服务本身不直连 DB）；复权口径映射见 §8.2；
- **结果回流**：pandas 报告 → ResultSpec 映射 → 落库（与 Go 内核同表）；
- **许可合规**：LGPL-3.0 服务化使用（独立进程、不链接），合规面收敛为零传染；
- **版本**：PoC 钉 NT 版本（v2.0.0rc 系列），升级跟随策略 [TBD]。

---

## 5. 外围服务 NT-ready 改造清单

| 外围服务 | 耦合现状 | 改造量 | 具体动作 |
|---|---|---|---|
| Dashboard | 仅经 REST API | **近零** | 只需 FR-3.2 的 status 呈现收口（本就要做） |
| AI 层 | 消费数据/结果/分析 API | **近零** | 依赖 canonical schema，无改动 |
| 策略层 | 执行经进程内 `GenerateSignals` | **主要工作** | `SignalProvider` 接口化（默认实现行为不变） |
| 编排层 | 经 `EngineRunner` | **小** | 路由字段透传；编排留 Go |
| 数据层 | market data provider | **小** | 增量 Parquet 导出 |

---

## 6. 分阶段、验收与任务落点

| 阶段 | 内容 | 人周 | 验收总纲 | 任务（docs/TASKS.md） |
|---|---|---|---|---|
| 1 契约化与解耦 | SignalProvider / RunSpec+ResultSpec / EngineRunner 可插拔 / 边界护栏 | 6–10 | 现有全量测试绿（默认行为零变化）+ 双 runner 注册路由正反证据 + status 全链路一致 | K8–K12 |
| 2 NT 侧车 PoC | 侧车骨架 / 权重回放适配器 / 双跑对齐实验 | 10–16 | 单策略双跑对齐报告（差异归因可读）+ A 股规则最小集反证腿 | K13–K15 |
| 3 双内核生产化 | 路由生产化 / 对齐回归 / CI/CD / 监控运维 | 6–10 | 对齐回归进 CI 门禁 + Python 流水线 + 运维文档 | K16（决策门通过后） |

**决策门（阶段 2 完成后，见 ADR-031 D4）**：凭对齐数据 + NT v2.0.0 正式版裁决阶段 3 推进 / 长期双内核 / 回退；VeighNa 并列评估；K4「impact 默认滑点」一并裁决。

---

## 7. 非目标（明确不做）

- ❌ 整体替换 Go 内核（ADR-031 已否决）；
- ❌ 重写策略层 / expression DSL / 插件体系（Go 策略资产零重写是本路线的核心收益）；
- ❌ 为 NT 改造 dashboard / AI 层（近零改造是验收项，出现改造需求 = 契约泄漏内核字段，先修契约）；
- ❌ 真实券商对接（D1 不变）；
- ❌ 把 walk-forward / 批量参数网格迁到 NT 侧（NT 无内置编排）；
- ❌ L2/L3 微观结构保真研究（NT 原生策略路径，超出「信号—权重」表达上限，另立议题）；
- ❌ K7 遗留③（`SignalStrategy` O(N²) 消费优化）——正确性已有游标去重保证，性能优化不阻塞本路线，另立小任务。

---

## 8. 未决问题 [TBD]

| # | 问题 | 关闭时机 | 状态 |
|---|---|---|---|
| 1 | 双跑对齐容差阈值（分指标） | 阶段 2 实验（K15） | 未决 |
| 2 | 侧车通道选型（HTTP vs gRPC vs Redis Streams） | K13 | ✅ 已关闭（见 §8.1） |
| 3 | Parquet 导出复权口径与 `ohlcv_daily_qfq` 映射 | K13 | ✅ 已关闭（见 §8.2） |
| 4 | NT 版本锁定 / 升级跟随策略 | 阶段 2 前 | PoC 已钉 `2.0.0rc6`；升级跟随策略未决 |
| 5 | ST/新股限制、集合竞价是否进适配器最小规则集 | K14 按双跑需要裁决 | ✅ 已关闭（见 §8.4） |
| 6 | 侧车部署形态（compose 服务 vs 独立进程）与监控口径 | 决策门后（K16） | 未决 |

### 8.1 裁决：侧车通道 = HTTP/1.1 + JSON（回环）

| 候选 | 性能 | 生态/成熟度 | 学习成本 | 运维成本 | 团队匹配 | 结论 |
|---|---|---|---|---|---|---|
| **HTTP/1.1 + JSON** | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ | 低 | 低 | 高（Go `net/http` + Python 标准库，零新依赖） | ✅ **选定** |
| gRPC | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | 中高（protobuf + 双端 codegen） | 中 | 中 | 现阶段无收益：仅需一个同步请求/响应端点 |
| Redis Streams | ⭐⭐⭐ | ⭐⭐⭐ | 中 | 中高（多一个 broker + 运维面） | 低 | 否决：K13 无异步扇出需求，且与 D5「不额外暴露」相悖 |

- **决策**：侧车以 **HTTP/1.1 + JSON** 提供单一同步端点 `POST /v1/backtest`，Python 侧用标准库 `http.server` 实现（运行时依赖只剩 NT 及其数据栈）。请求/响应 DTO 逐字段镜像 Go 的 `contracts.BacktestRequest`，Go 客户端映射是一次直拷。
- **理由**：通道是**内部实现细节**（两端都是我们自己的代码），价值在「契约能不能通」而非「协议多先进」。gRPC 要引 protobuf 工具链 + 两端 codegen，换来的流式/强类型能力在 K13 用不上；纯 JSON 可直接 curl 复现正/反证腿，排障成本最低。
- **代价**：无 schema 强制（改用文档 + 双端校验兜底）、无 HTTP/2 多路复用、无流式进度。
- **切换触发条件**：K14/K15 若出现「多 run 编排 / 流式进度 / 需要强类型双向契约」，再评估 gRPC —— 换协议只动 Go `NautilusRunner` 客户端与 Python 服务两端，属内部改动，不破外围契约。

### 8.2 裁决：Parquet 复权口径 = `ohlcv_daily_qfq` 原样消费 + UTC 午夜纳秒

| 项 | 映射 |
|---|---|
| 复权口径 | **不做二次复权**：`ohlcv_daily_qfq` 已是前复权序列，原样写入 catalog（Go 内核读的也是同一序列） |
| symbol → instrument | `{symbol}.{venue}`（venue 默认 `SIM`），`Equity`、`CNY`、tick `0.01` |
| bar_type | `{symbol}.{venue}-1-DAY-LAST-EXTERNAL` |
| 时间戳 | `trade_date` → **UTC 午夜纳秒**，`ts_event = ts_init` |
| volume | 表内原值取整（tushare 手数口径）直传 |

- **决策**：**原样消费前复权序列**，NT 侧不叠加任何 dividend/split 调整。
- **理由**：K15 的双跑对齐要求两内核价源同口径。前复权是以最新日为基准重构的历史序列，若 NT 侧再做调整，价差将无法归因到「执行机制差异」这一受控变量上。
- **代价 / 观察项**：catalog 是**物化快照**，基表重灌后需重导；若 K15 出现系统性价差，须复核两侧取数时点是否一致（同一基表快照）——**列为 K15 观察项**。

### 8.3 K13 附带记录：NT 无内置 buy-and-hold

调研报告与源码核对结论：NT **不提供内置 buy-and-hold 策略**（其示例策略在 `examples` cargo feature 之后，非稳定 API）。K13 因此自带一个最小 demo 策略（首根 bar 买入、持有到窗口结束，无信号/指标/风控逻辑）作为链路验证载体——这不改变「本任务无策略逻辑」的定位，真实策略行为在 K14 的权重回放适配器落地。

### 8.4 裁决：适配器最小规则集（K14）

适配器（`nt-sidecar/sidecar/weight_replay.py` + `ashare.py`）落地的最小集，逐项裁决如下。
**入集** = 与 Go 内核口径逐字对齐，K15 双跑必须一致；**不入集** = 刻意不做，理由入档（不是遗忘）。

| 规则 | 入最小集 | 口径来源 / 理由 |
|---|---|---|
| 主板涨跌停 ±10%（四舍五入到分） | ✅ | `pkg/backtest/pricelimit.go` `LimitPrices`/`roundToCent`（half-away-from-zero）。直接影响「触及涨停不追买 / 触及跌停不追卖」与成交价 |
| T+1（今仓不可当日卖出） | ✅ | 卖单量 ≤ 昨仓可卖量（`ashare.sellable_quantity`）。A 股结算语义，K15 不可回避 |
| 费率（印花税**卖出单边** / 过户费 / 最低佣金） | ✅ | `pkg/portfolio/portfolio.go` `ComputeFees` 逐字对拍（容差 0，见 `tests/fixtures/ashare_fees_parity.json`） |
| 每日成交占比 ≤ D_max（外生参数） | ✅ | 参与率硬约束：成交股数 ≤ `floor(D_max × bar volume)`。RunSpec 新增可选 `d_max`（默认 0.10） |
| 整手（买入 100 股整数倍） | ✅ | `ashare.round_down_to_lot` |
| **ST / \*ST 价格限制** | ❌ | 需「板块 + 名称 + 日期分段」三重判定（`resolvePriceLimit` 完整口径，含 2026-07-06 ST 幅度变更）；引入即多一个不受控变量，K15 归因断掉。真需要另立任务 |
| **ChiNext/STAR ±20% / BSE ±30%** | ❌ | 同上（需 `marketdata.Board.DailyPriceLimit` 板级口径）。最小集只做主板 ±10% 一档 |
| **新股上市初期价格限制** | ❌ | 依赖上市日，样本窗口内无新股；且 Go 侧该分支本身有已知不精确（`PriceLimitInput.TradeDays` 注释） |
| **集合竞价** | ❌ | catalog 只有日线（`1-DAY-LAST-EXTERNAL`），无分时/竞价数据；「日频目标权重」表达上限之外的撮合机制不建模（设计文档 §3 代价声明） |

**K15 观察项（新增）**：Go 回测撮合路径（`pkg/backtest/execution` → `pkg/execution` CostModel）**只计佣金**（`max(notional×CommissionRate, MinCommission)`），不建模印花税/过户费——这是代码注释里写明的「两侧共同已知口径边界」。而 K14 适配器的 NT fee model 计**全三项**。K15 对齐时该差异必须**显式归因**（是口径边界而非执行机制差异），不得被容差掩盖。

