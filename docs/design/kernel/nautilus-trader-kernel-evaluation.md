# 用 NautilusTrader 替换自研 Go 回测内核：利弊、可行性与工作量研究

> 研究日期：2026-10-12。研究对象名称按 **NautilusTrader**（nautechsystems/nautilus_trader）理解——用户原文写作 "neutulis"，检索范围内无其他同名量化回测框架，此为唯一合理匹配；若实际所指为其他框架，本报告结论需重新评估。
>
> **⚠️ 后续裁决更新（2026-10-14，若曦拍板）**：本报告的**决策门建议已被实际裁决取代**——① 裁决为「推进生产化」（阶段 3 随 K16 落地）；**§8.6 第 3 条与执行摘要中的「VeighNa 并列评估」建议已取消**（触发条件未成立，理由见 ADR-031 D4 末尾裁决二）。本报告作为调研取证保留原文，后续以 ADR-031 裁决记录为准。

## 执行摘要

NautilusTrader（下称 NT）是 Rust 内核 + Python 控制面的事件驱动回测/实盘一体框架，回测保真度（L3 逐单撮合、12 种成交模型、多市场组合账务）显著高于当前自研内核，社区活跃（约 2.98 万 stars、双周发版）。[cite:1][cite:2] 但对本项目而言存在三个硬障碍：

1. **中国市场零覆盖**：官方无 A 股交易日历、T+1、涨跌停、公司行为（分红/拆股/复权）支持；维护者在 GitHub issue #4599 中明确表示"近期路线图不含中国市场"[cite:21][cite:22]，且未检索到任何公开的 NT 做 A 股回测的先例。
2. **与 Go 技术栈正交**：NT 无 Go 绑定、无对外 gRPC/REST 控制 API，C FFI 仅覆盖数据模型层而非回测引擎[cite:16]，只能以独立 Python 服务的形态接入——这与项目"后端必须使用 Go"的硬约束直接冲突，需要显式修改该约束才能推进。
3. **时机风险**：当前 v2.0.0 处于 RC 阶段、破坏性变更频繁，官方自己声明 release notes 仅 best-effort[cite:1][cite:24]。

**总体判断：不建议现在整体替换自研回测内核。** 推荐路线是"保留 Go 内核为默认引擎 + 以侧车 Python 服务做小范围 PoC 验证 + 借鉴 NT 的执行模拟设计反哺 Go 内核"，待 v2.0.0 正式版发布且 PoC 通过 A 股规则对齐验证后再做替换决策。若真实诉求是"Python 生态 + 中国市场覆盖"，VeighNa（vnpy）是比 NT 更贴合的候选，应并列评估[cite:29]。

**工作量量级**（估算假设见 §6）：整体替换约 **25–45 人周**的初次投入，另加长期跟随 NT 2.x 破坏性变更的维护成本；混合双内核路线约 **10–20 人周**；不替换、仅借鉴设计增强自研内核约 **2–5 人周**。

**动机修正（2026-10-12 补充）**：委托方明确替换动机是"降低内核维护成本、集中精力于上层开发"（buy vs build），而非回测保真度。在此动机下推荐路线调整为**内核可插拔 + 外围服务 NT-ready 化**：先把策略层、dashboard、AI 层与内核解耦、以内核边界契约为稳定接口，再以"信号—权重回放适配器"形态接入 NT 侧车——现有 Go 策略资产与外围服务零重写，A 股规则自建范围收缩到适配器层。修正后的路线与工作量见 §8。

---

## 1. 研究对象与方法

### 1.1 名称澄清

用户提出的 "neutulis" 在检索范围内无直接匹配的量化回测框架。NautilusTrader（官网 nautilustrader.io，GitHub nautechsystems/nautilus_trader）是发音与拼写上最接近的知名项目，且是"回测内核替换"场景下的高频候选，故按此展开研究。[cite:1][cite:4]

### 1.2 方法与证据口径

研究由三部分构成：本项目代码库盘点（回测内核规模、契约面、调用链）、NT 官方文档与 GitHub 仓库一手资料检索、第三方评测与社区反馈交叉验证。来源分级：官方文档/仓库（P0）> GitHub issues/代码（P1）> 社区评测（P2）> 论坛（P3）。关键结论尽量由 2–3 个独立来源交叉验证；检索不到的信息在文中明确标注"未找到"。外部事实以 `[cite:N]` 标注，文末附来源清单；代码库事实以文件链接标注。

---

## 2. 现状：自研 Go 回测内核盘点

### 2.1 执行模型与 A 股特化

当前内核是**按交易日推进的信号驱动回测引擎**：策略产出信号与目标权重，引擎按日拉取股票池行情，处理涨跌停阻塞后将目标权重转化为调仓交易（[engine.go](file:///c:/Users/ruoxi/Workspace/quant-trading/pkg/backtest/engine.go#L53-L174)、[engine_daily.go](file:///c:/Users/ruoxi/Workspace/quant-trading/pkg/backtest/engine_daily.go#L242-L335)）。与 NT 的"事件驱动、逐订单撮合"是两种不同的回测范式（详见 §5.2）。

A 股市场规则深度内建，这是自研内核最核心的资产：

| 机制 | 位置 | 说明 |
|---|---|---|
| 涨跌停价限制 | [engine_daily.go#L152-L240](file:///c:/Users/ruoxi/Workspace/quant-trading/pkg/backtest/engine_daily.go#L152-L240)、[pricelimit.go](file:///c:/Users/ruoxi/Workspace/quant-trading/pkg/backtest/pricelimit.go) | 涨跌停阻塞交易、ST/新股价格限制 |
| T+1 结算 | [tracker](file:///c:/Users/ruoxi/Workspace/quant-trading/pkg/backtest/tracker/tracker.go)（含 T+1 专项测试） | 今昨仓、可卖数量追踪 |
| 集合竞价 | [auction.go](file:///c:/Users/ruoxi/Workspace/quant-trading/pkg/backtest/auction/auction.go) | 开盘/收盘竞价撮合 |
| 费率模型 | [fees/ashare.go#L41-L59](file:///c:/Users/ruoxi/Workspace/quant-trading/pkg/fees/ashare.go#L41-L59)、[contracts.go#L29-L63](file:///c:/Users/ruoxi/Workspace/quant-trading/pkg/backtest/contracts/contracts.go#L29-L63) | 印花税（含 2023 减半沿革）、最低佣金、过户费 |
| 收益口径 | [metrics/performance.go#L76-L107](file:///c:/Users/ruoxi/Workspace/quant-trading/pkg/backtest/metrics/performance.go#L76-L107) | 封闭系统日收益口径，规避"现金流水调整收益"的历史缺陷 |
| 防前视偏差 | [engine.go#L656-L699](file:///c:/Users/ruoxi/Workspace/quant-trading/pkg/backtest/engine.go#L656-L699) 及确定性回放测试 | 独立引擎实例、确定性回放、walk-forward 隔离 |

### 2.2 代码规模

| 包 | 生产代码 | 测试代码 | 说明 |
|---|---|---|---|
| pkg/backtest | 约 9,485 行 | 约 13,399 行 | 引擎本体 + tracker/batch/job/walkforward/metrics/auction/reporting/cache/state/execution |
| pkg/strategy | 约 10,126 行 | 约 8,836 行 | 策略接口、注册表、表达式 DSL、插件加载 |
| cmd/analysis | 约 4,098 行 | 约 3,380 行 | HTTP 层、回测/走查/批量接线 |
| pkg/domain + pkg/fees | 约 1,564 行 | 约 1,599 行 | 领域对象、A 股费率 |

回测相关的**总替换面约 2.5 万行生产代码、2.7 万行测试代码**。测试规模（1.4:1 的测试/代码比）意味着替换不仅要重建实现，还要重建验证体系（确定性回放、不变量测试、性质测试、规模不变性探针等）。

### 2.3 契约面与调用链

- **同步路径**：HTTP `POST /api/backtest`（10 分钟超时）内同步调用 `engine.RunBacktest` 并落库（[handlers_backtest.go#L36-L83](file:///c:/Users/ruoxi/Workspace/quant-trading/cmd/analysis/handlers_backtest.go#L36-L83)）。
- **异步路径**：job 服务（[job.go](file:///c:/Users/ruoxi/Workspace/quant-trading/pkg/backtest/job/job.go#L16-L51)）基于 Postgres job store 管理回测任务生命周期，含优雅停机；另有 batch 批量评分与 walk-forward（[walkforward.go](file:///c:/Users/ruoxi/Workspace/quant-trading/pkg/backtest/walkforward/walkforward.go)）。
- **窄接口缝合点**：[runner.go#L40-L56](file:///c:/Users/ruoxi/Workspace/quant-trading/pkg/backtest/contracts/runner.go#L40-L56) 的 `BacktestRunner`（`RunBacktest(ctx, strategyName, stockPool, startDate, endDate) → *domain.BacktestResult`）是跨模块最窄契约——外部引擎只要能实现该接口并产出等价 `BacktestResult`，即可在接缝处替换。
- **API 契约**：`BacktestRequest`/`BacktestResponse`（[contracts.go#L153-L208](file:///c:/Users/ruoxi/Workspace/quant-trading/pkg/backtest/contracts/contracts.go#L153-L208)）定义了收益率、夏普、最大回撤、交易明细、组合净值等输出字段，替换后必须保持等价输出或做显式映射。
- **策略模型**：策略实现 `Strategy` 组合接口（`StrategyCore + Configurable + SignalGenerator + ResourceManaged`，[interfaces.go#L106-L111](file:///c:/Users/ruoxi/Workspace/quant-trading/pkg/strategy/interfaces.go#L106-L111)），核心是 `GenerateSignals` / `Weight` 的**信号—权重**模型；另有表达式 DSL（pkg/strategy/expression）与插件加载机制。

---

## 3. NautilusTrader 能力评估

### 3.1 定位与架构

NT 是"Rust-native 的多资产多市场交易引擎，单一事件驱动架构覆盖研究、确定性模拟与实盘执行，Python 作为策略/配置/编排的控制面"。[cite:1][cite:5] 热路径（数据引擎、执行引擎、风控、消息总线）在 Rust，Python 绑定经 PyO3（v1 的 Cython 架构已进入维护模式）。回测与实盘共享同一内核（`NautilusKernel`），官方称之为 "research-to-live parity as architecture"，但同时诚实声明实盘仍有模拟无法复现的行为（场所规则、网络故障、对账等）。[cite:6][cite:23]

**版本与活跃度**（2026-10-12 实测）：当前 v2.0.0rc6（2026-10-04 发布，预发布）；GitHub 29,758 stars / 3,954 forks / 仅 122 open issues，最近提交 2026-10-09。[cite:14][cite:2] 官方目标约双周发版，README 明确警告 breaking changes 可能发生且 release notes 仅 best-effort。[cite:1]

### 3.2 回测能力

| 维度 | 能力 | 来源 |
|---|---|---|
| 订单与执行指令 | IOC/FOK/GTC/GTD/DAY/AT_THE_OPEN/AT_THE_CLOSE、post-only、reduce-only、冰山单、OCO/OUO/OTO、模拟单、部分成交 | [cite:1] |
| 成交模拟 | 约 12 种内置 fill model（概率成交、一/多 tick 滑点、成交量敏感、队列消耗等），可注入自定义模型 | [cite:7] |
| 数据粒度 | L3 MBO 逐单 / L2 MBP 逐档 / L1 报价 / Trade tick / Bar / 自定义数据，纳秒时间戳；L2/L3 下撮合直接走真实盘口 | [cite:8] |
| 账户与组合 | CASH/MARGIN/BETTING 账户、杠杆/标准保证金模型、多币种组合 PnL、资金费与合约到期结算 | [cite:9] |
| 批量运行 | `BacktestNode` + `BacktestRunConfig` 高层 API（catalog-backed、流式分块），支持重复运行与 reset；参数网格需外层循环自行编排 | [cite:18] |
| 结果导出 | orders/fills/positions/account 报告（pandas DataFrame）、stats 统计字典、equity curve；持久化为 Parquet/Arrow catalog | [cite:20] |
| 性能 | 官方基准：约 2 万条消息的回放工作负载 20–40ms 量级（Rust 引擎，2026-08 测量）；Databento 解码 25.1M 条/秒 | [cite:13][cite:35] |

### 3.3 数据与市场覆盖（关键缺口）

内置适配器集中于加密交易所（Binance、Bybit、Coinbase、Kraken、OKX、Deribit 等）、Interactive Brokers、Betfair 及数据商 Databento/Tardis。[cite:11] **中国 A 股（上交所/深交所）无任何官方适配器**；GitHub issue #4599（中国资本市场支持 RFC）由维护者于 2026-07 正式回复："当前聚焦 v2 稳定化，不接新市场，RFC 无时间表"。[cite:21][cite:22]

公司行为方面，**未找到**面向股票回测的通用分红/拆股/复权模型；仅 Bybit 适配器有执行历史层面的拆股/分红风险事件。[cite:12] 意味着 A 股复权因子、除权除息对回测的影响需在数据预处理层自建。

自定义数据机制（`register_custom_data_class`，走 Arrow 桥进 Rust，与内置数据同享路由与持久化）理论上可承载涨跌停价、T+1 可卖标记等 A 股特色数据，但全部自理。[cite:19]

### 3.4 策略模型与集成接口

策略用 Python 编写：继承 `Strategy`，以 `on_bar / on_order_filled / on_position_opened` 等约 30 个生命周期/数据/订单回调驱动，配套 Clock/Cache/Portfolio/order_factory。[cite:10] 性能敏感场景可用 Rust 写策略（`nautilus_strategy!` 宏）。[cite:1]

**与 Go 后端集成的接口现实**：

- 未找到官方或社区的 Go 绑定；不存在对外 gRPC/REST 控制 API（gRPC 仅是个别交易所适配器的客户端）。[cite:1]
- C FFI 仅从 `nautilus-core` / `nautilus-model` 两个 crate 暴露（数据模型对象），**回测引擎没有 C ABI，无法 cgo 直调**。[cite:16]
- 官方支持的跨进程通道是 Message Bus external egress/ingress（Redis Streams，≥6.2）与 Parquet 数据目录。[cite:17]
- headless 批量回测的正规入口是 `BacktestNode`/`BacktestRunConfig` 程序化 API。[cite:18]
- 跨语言"Python 服务化 + RPC 调用"在量化工程中有成熟先例（如 Uber 的 Python 提交 + Go 回测服务架构）。[cite:28]

### 3.5 许可与成熟度

- **许可**：LGPL-3.0（GitHub API、LICENSE 全文、README 三重确认）。[cite:3] 内部使用约束小；独立进程/服务化调用不触发传染义务，进程内嵌入/链接需评估合规——这是不走 cgo 嵌入的又一理由。
- **商业生态**：NautilusTrader Pro（自托管监控/执行算法/风控组件，订阅制、价格未公开）、Cloud Platform（Coming Soon）、机构支持渠道；开源范围明确收敛于"单节点回测 + 实盘"，UI/分布式编排不在开源范围。[cite:1]
- **已知批评**：学习曲线陡峭[cite:27]；v1→v2 过渡期 API 不稳定、迁移成本显式存在（MIGRATION_V2 文档）[cite:24][cite:26]；无捆绑行情数据，tick 级数据自采是独立工程[cite:27]；源码构建依赖重（仅用 wheel 则无此问题）[cite:26]；事件驱动逐笔回测在参数扫描场景慢于向量化方案（vectorbt 千组参数 <30 秒）[cite:32]。

---

## 4. 利弊分析

### 4.1 收益

| 收益 | 说明 | 依据 |
|---|---|---|
| 回测保真度跃升 | L3 逐单撮合、队列消耗、12 种 fill model、真实盘口驱动的滑点——当前 Go 内核（日线信号—权重模型）不具备的能力 | [cite:7][cite:8] |
| 回测/实盘同引擎 | 同一策略代码可从研究直接走向实盘执行，消除双实现偏差（有边界，见 §3.1） | [cite:6] |
| Rust 热路径性能 | 官方基准显示 2 万条消息级工作负载毫秒完成；对 tick 级或大规模回测有实质优势 | [cite:13] |
| 工程生态 | 2.98 万 stars、文档完善、双周发版、机构背书；订单/账务/风控模型久经打磨 | [cite:1][cite:2] |
| 框架资产复用 | 多市场多资产账务、保证金、资金费、合约到期等模型无需自研 | [cite:9] |
| LGPL 约束小 | 内部服务化使用无开源传染义务 | [cite:3] |

### 4.2 代价与风险

| 代价/风险 | 严重度 | 说明 | 依据 |
|---|---|---|---|
| A 股规则全部自建 | **高** | T+1、涨跌停、ST/新股、印花税/过户费、复权/公司行为、交易日历均无官方支持，且无公开先例可抄 | [cite:21][cite:22] |
| 与 Go 硬约束冲突 | **高** | 引擎只能跑在 Python/Rust 进程，Go 后端只能进程间集成；与项目"后端必须 Go"约束矛盾 | [cite:1][cite:16] |
| 策略范式不兼容 | **高** | 现有策略是信号—权重日频调仓模型，NT 是事件—订单回调模型；策略层不是"移植"而是"重写" | [cite:10] |
| API 不稳定期 | **中高** | v2.0.0 RC、破坏性变更频繁、1.x 只接受安全修复；现在迁移将被迫跟随 v2 变更 | [cite:1][cite:24] |
| 运维复杂度 | **中** | 引入 Python 运行时（3.12–3.14）+ 新服务形态，CI/CD、监控、Windows 开发环境支持需重建 | [cite:15] |
| 参数扫描性能 | **中** | 事件驱动逐笔模拟在批量参数搜索场景慢于向量化方案，walk-forward/批量编排需自行在外层搭建 | [cite:18][cite:32] |
| 数值口径迁移风险 | **中** | 夏普/回撤/收益口径需与现有 metrics 层对齐，历史上本项目已踩过"现金流水调整收益"类口径坑 | [cite:20] |
| 验证体系重建 | **中** | 现有 2.7 万行测试（确定性回放、不变量、规模不变性）需在新架构上等价重建 | 代码库盘点 §2.2 |

---

## 5. 可行性评估

### 5.1 集成架构选项

**选项 A：整体替换（NT 侧车服务）**——新建独立 Python 回测服务（FastAPI/gRPC 包装 `BacktestNode`），analysis-service 经 `BacktestRunner` 窄接口调用，结果经 pandas → Postgres 回流；数据经 ParquetDataCatalog 或低层 `add_data()` 喂入。技术上可行（服务化模式有成熟先例[cite:28]，官方批量 API 支持 headless 运行[cite:18]），但 §4.2 的全部代价都要支付。

**选项 B：混合双内核**——保留 Go 内核处理现有日频信号—权重策略（覆盖 A 股规则），NT 侧车仅用于需要 L2/L3 微观结构保真的专项研究（如执行算法、冲击成本）。两个内核各自擅长，`BacktestRunner` 窄接口让路由层可以按策略类型分发。风险分散，但双栈长期共存。

**选项 C：借鉴设计、不替换**——将 NT 的 fill model 设计、队列消耗、确定性回放/事件溯源架构移植为 Go 内核的增强模块。无跨语言成本，A 股规则完全可控，LGPL 对参考实现细节无碍。

**选项 D：其他路线**——若诉求本质是"中国市场 + Python 生态"，VeighNa（国内期货/股票网关、CTP 生态、回测与参数优化模块）覆盖度显著更好[cite:29]；QuantConnect Lean（Apache-2.0，可自托管）是另一工程化同级候选，但其 A 股规则支持未获证据[cite:30]；向量化方案（vectorbt）适合研究层参数扫描但成交真实性弱[cite:31]；自研 Rust 内核 + cgo/UniFFI 可完全掌控规则但等于第三次造轮子[cite:36]。

### 5.2 硬性障碍逐项判定

| 障碍 | 判定 | 依据 |
|---|---|---|
| 项目硬约束"后端必须 Go" | **冲突**。引入 Python 回测服务需显式修改该约束，或将 NT 严格限定为"可替换的实验性侧车"而 Go 仍是唯一生产后端 | 项目工程约束 |
| A 股交易规则 | **自建可行但无先例**。custom data + venue 配置 + 自定义 fill model 在架构上留有扩展点[cite:19]，但 T+1/涨跌停/复权/交易日历全部自理，且 #4599 表明官方不会接手 | [cite:19][cite:22] |
| 策略迁移 | **重写而非移植**。信号—权重模型与事件—订单模型之间没有机械映射；expression DSL 与插件体系需要桥接层或放弃 | [cite:10]、§2.3 |
| 结果契约 | **可映射**。NT 报告层输出 DataFrame 类指标[cite:20]，可映射到 `BacktestResponse` 字段，但口径对齐需专项验证（尤其收益/回撤/夏普） | [cite:20] |
| 部署 | **可行**。官方 wheel 免编译、Docker 官方镜像、Python 3.12–3.14；Windows 开发环境可用 wheel，源码构建复杂但通常不需要 | [cite:15] |
| 许可 | **可行**。LGPL-3.0 服务化使用约束小；进程隔离同时规避了传染与 FFI 缺口两个问题 | [cite:3][cite:16] |
| 时机 | **不利**。v2.0.0 RC 期，任何迁移都要承受后续破坏性变更 | [cite:1][cite:24] |

### 5.3 可行性结论

技术可行性成立的唯一干净形态是：**NT 作为独立 Python 微服务（HTTP/gRPC 或 Redis Streams 解耦），经 `BacktestRunner`/`EngineRunner` 窄接口与 Go 平台对接，Parquet/pandas 做数据与结果交换**。cgo 嵌入路线不可行（引擎无 C ABI）[cite:16]。业务可行性则取决于"A 股规则自建"和"策略重写"两项的接受度——这两项占整体工作量的约一半（§6），且是纯增量成本：它们复用不了 NT 的任何现成能力，因为 NT 在这两个方向上恰好是空白。[cite:21][cite:22]

---

## 6. 工作量估算

### 6.1 估算假设

- 人力按"熟悉本项目代码库的全职工程师"计，单位为人周，给出区间反映不确定性；
- **不做**新功能开发，仅达到"现有回测能力在 NT 上等价复现"；
- 现有 A 股日频策略数量与复杂度按 pkg/strategy 的 1 万行生产代码体量折算（含 expression DSL 与插件体系）；
- 不含数据采集体系重建（假设沿用现有 Postgres 行情存储）。

### 6.2 选项 A：整体替换工作分解

| # | 工作流 | 内容 | 人周（区间） |
|---|---|---|---|
| 1 | Python 回测服务 | `BacktestNode`/`BacktestRunConfig` 封装、HTTP/gRPC API、容器化、日志/监控、CI | 2–4 |
| 2 | 数据管线 | Postgres OHLCV → ParquetDataCatalog（纳秒 UTC 时间戳约束[cite:19]）、复权数据口径、custom data 类定义 | 3–6 |
| 3 | **A 股规则自建** | T+1 今昨仓、涨跌停阻塞、ST/新股限制、印花税/过户费/最低佣金、集合竞价、A 股交易日历 | **6–12** |
| 4 | **策略层重写/桥接** | 信号—权重模型 → NT Strategy 事件模型；expression DSL 移植或桥接；插件体系处置 | **6–12** |
| 5 | 结果契约与落库 | fills/positions/equity 报告 → `BacktestResponse` 等价映射、指标口径对齐、job/batch/walk-forward 对接 | 3–5 |
| 6 | 双跑对齐验证 | Go 内核 vs NT 数值一致性回归、确定性回放/不变量/规模不变性测试等价重建 | 3–6 |
| 7 | 运维与开发环境 | Python 运行时进 CI/CD、Windows 开发支持、部署文档、监控告警 | 2–4 |
| | **合计** | | **25–45 人周** |

另有长期成本：跟随 NT 2.x 破坏性变更（双周发版节奏下的升级与回归）[cite:1]、A 股规则变更（费率、交易制度）在新栈上的维护、Python/Rust 双栈人才培养。

### 6.3 选项 B：混合双内核

侧车服务（工作流 1 简化版）+ 数据管线（工作流 2）+ 路由分发与结果对齐（工作流 5 简化版）+ 一个代表性专项场景 PoC（不做全量策略重写、不做全量规则对齐）：约 **10–20 人周**。后续按需增量投入。walk-forward/批量编排仍由 Go 侧掌控，不迁移。

### 6.4 选项 C：借鉴设计增强自研

研读移植 NT 的 fill model/队列消耗/事件回放设计到 Go 内核（如限价单部分成交概率、成交量敏感滑点）：约 **2–5 人周**/模块，无跨语言与迁移风险。

### 6.5 三方案对比

| 方案 | 初次投入 | 回测保真度提升 | A 股适配 | 与 Go 约束 | 风险 |
|---|---|---|---|---|---|
| A 整体替换 | 25–45 人周 | 大 | 需全部自建 | **冲突** | 高（API 不稳定、双跑偏差、策略重写） |
| B 混合双内核 | 10–20 人周 | 专项场景大 | Go 侧保留 | 可控 | 中 |
| C 借鉴设计 | 2–5 人周/模块 | 渐进 | 完全可控 | 无冲突 | 低 |
| （参考）VeighNa 路线 | 未评估 | 中 | **最好**[cite:29] | 冲突（Python） | 中 |

---

## 7. 结论与建议

**结论：NautilusTrader 是优秀的回测框架，但当前不是本项目的正确替换对象。** 其核心价值（L3 撮合、fill model、回测/实盘 parity、多市场账务）与本项目的核心资产（A 股规则内建、信号—权重策略体系、Go 单栈运维）恰好互补而不重叠——替换等于放弃自己的护城河去换取对方的长板，同时为对方的空白（A 股、公司行为、策略重写）支付约一半的总成本。[cite:21][cite:22]

**建议路径（三步走）：**

1. **先立性能/保真度基线**：明确想换内核的真实动机——是回测太慢、保真度不够、还是想要实盘 parity？用现有 Go 内核基准（[engine_bench_test.go](file:///c:/Users/ruoxi/Workspace/quant-trading/pkg/backtest/engine_bench_test.go) 体系）量化瓶颈，避免为"感觉更快"的框架支付 25–45 人周。
2. **小成本验证**：以选项 B 形态做一个 2–4 个场景的 PoC（侧车服务 + 一个 A 股规则最小集 + 一只策略双跑对齐），用实测数据回答"A 股规则自建的真实成本"和"NT 的速度/保真度收益是否兑现"这两个最大不确定性。
3. **决策门**：待 NT v2.0.0 正式版发布（消除 RC 期破坏性变更风险[cite:1]）且 PoC 数据在手后，再做"整体替换 / 长期双内核 / 停留在借鉴设计"的正式决策。若届时诉求确认为"Python + 中国市场"，将 VeighNa 纳入同一 PoC 并列评测[cite:29]。

同时提示一项低成本高回报的行动：无论是否迁移，NT 的 fill model 分类学（12 种模型的参数化设计）[cite:7] 与确定性回放架构[cite:6] 都值得在 Go 内核的执行模拟层直接借鉴。

---

## 8. 动机修正与外围服务 NT-ready 化路线图

### 8.1 策略层现状：定义在外、执行在内

代码库确认的现状是混合形态：

- **策略定义/配置在外部**：策略配置存 Postgres（`StrategyDB`），由 analysis-service 的 `/api/strategies` CRUD 管理（[handlers_strategy.go#L13-L64](file:///c:/Users/ruoxi/Workspace/quant-trading/cmd/analysis/handlers_strategy.go#L13-L64)）；独立的 strategy-service（cmd/strategy，:8082）按 ADR-012 处于 standby，不参与信号生成。
- **策略执行在内核进程内**：pkg/strategy 是编译进 analysis-service 的 Go 库，策略实例经 `DefaultRegistry` + 插件加载器装载（[setup.go#L228-L237](file:///c:/Users/ruoxi/Workspace/quant-trading/cmd/analysis/setup.go#L228-L237)），回测引擎每个交易日**进程内直调** `strat.GenerateSignals(ctx, marketData, portfolio)`（[engine.go#L1280](file:///c:/Users/ruoxi/Workspace/quant-trading/pkg/backtest/engine.go#L1280)），流式策略走 `OnBar`/`Signals()` 路径（[engine.go#L1246-L1273](file:///c:/Users/ruoxi/Workspace/quant-trading/pkg/backtest/engine.go#L1246-L1273)）。

含义：策略资产（约 1 万行生产代码，含 expression DSL 与插件体系）与内核的耦合点只有一个——**"引擎调用策略生成信号"这一进程内调用**。把这个调用点契约化，策略层即与内核解耦；反之若直接整体替换内核，这 1 万行全部重写（§6.2 工作流 4）。

### 8.2 内核边界：四个契约缝合点

NT-ready 化的本质是把"内核"降级为**可替换的执行器**，外围只依赖稳定契约。现有 contracts 包已提供大部分基础，需要的是固化与补全：

| 契约 | 现状基础 | 需要做的 | NT 对接形态 |
|---|---|---|---|
| **RunSpec**（输入） | `BacktestRequest`（[contracts.go#L153-L208](file:///c:/Users/ruoxi/Workspace/quant-trading/pkg/backtest/contracts/contracts.go#L153-L208)）+ `TradingConfig` 规则配置 | 增加 kernel 路由字段；规则配置保持声明式（NT 侧由适配器解释） | 映射为 `BacktestRunConfig`[cite:18] |
| **SignalSpec**（信号） | `strategy.Signal`（symbol/direction/weight，[strategy.go#L45](file:///c:/Users/ruoxi/Workspace/quant-trading/pkg/strategy/strategy.go#L45)） | 把"引擎进程内调 GenerateSignals"抽象为 `SignalProvider` 接口：本地注册表实现为默认，远程实现（HTTP/Redis）可插拔 | NT 侧通用"权重回放策略"消费同一信号流 |
| **ResultSpec**（结果） | `BacktestResponse`/`domain.BacktestResult`（收益率、夏普、回撤、交易明细、净值） | 冻结为 canonical 结果 schema，指标口径（含日收益口径）写入规范 | pandas 报告 → ResultSpec 映射层[cite:20] |
| **EngineRunner**（执行） | `BacktestRunner`/`EngineRunner` 窄接口（[runner.go#L40-L56](file:///c:/Users/ruoxi/Workspace/quant-trading/pkg/backtest/contracts/runner.go#L40-L56)） | 内核选择做成可插拔 runner（Go 内核实现 + NT 客户端实现并存） | NT 侧车客户端实现该接口 |

### 8.3 各外围服务的 NT-ready 改造量

| 外围服务 | 与内核的耦合现状 | NT-ready 改造量 |
|---|---|---|
| **Dashboard**（web/ Vue3） | 仅经 analysis-service REST API 交互，无直接内核耦合 | **近零**。只要 API 层保持 ResultSpec/RunSpec 口径、不泄露内核特有字段，前端完全不用改 |
| **AI 层**（analysis-service 内 ai_proxy 等 handler） | 消费数据/结果/分析 API，无直接内核耦合 | **近零**。同上，依赖 canonical 结果 schema |
| **策略层** | 定义已在外部；执行经进程内 `GenerateSignals` 调用与内核耦合（§8.1） | **主要工作**：`SignalProvider` 接口化（本地实现保持默认行为不变），为远程信号供给留出 HTTP/Redis 实现位 |
| **编排层**（job/batch/walk-forward） | 经 `EngineRunner` 窄接口调内核 | **小**。job/batch/walk-forward 留在 Go 侧作为编排器，内核成为可替换执行器；NT 无内置参数网格/走查编排[cite:18]，留在 Go 侧反而正确 |
| **数据层**（data-service/Postgres） | 引擎经 market data provider 取数 | **小**。增量提供 Parquet 导出（NT `ParquetDataCatalog` 要求纳秒 UTC 时间戳[cite:19]），或低层 `add_data()` 馈送 |

### 8.4 信号—权重回放适配器：策略资产零重写的关键

在 NT 侧实现一个通用 Python Strategy（"权重回放适配器"）：每个调仓日从 SignalSpec 信号流读取目标权重，用 `order_factory` 生成目标仓位调整订单。这样：

- **现有 Go 策略（含 expression DSL、插件）全部保留**，策略层继续在 Go 侧开发维护，NT 只接管撮合/账务/费用内核——§6.2 工作流 4（6–12 人周）整体消除；
- **A 股规则自建范围收缩**：T+1 可卖数量、涨跌停阻塞等规则在适配器下单逻辑中直接实现（卖单不超过昨仓可卖量、触板不追单），不必扩展 NT venue 模型——比 §5.2 预估的"规则全部自建"轻一个量级；印花税/过户费映射到 `MakerTakerFeeModel` 费率参数或自定义 fee model[cite:7]；
- **双内核可比性天然成立**：同一信号流喂两个内核，数值对齐验证（§6.2 工作流 6）变为受控实验而非迁移阻塞项。

代价：策略被抽象为"日频目标权重"，NT 的逐订单事件模型（部分成交、冰山单、L2/L3 撮合）对权重回放策略仅部分发挥——但对现有信号—权重策略体系而言，这本来就是其表达能力上限，无损失。

### 8.5 修正后的分阶段工作量

| 阶段 | 内容 | 人周（区间） | 独立价值 |
|---|---|---|---|
| 1. 契约化与解耦 | `SignalProvider` 接口化、RunSpec/ResultSpec 固化、EngineRunner 可插拔 | 6–10 | **即使不接 NT 也成立**：降低内核耦合、明确模块边界（与 docs/design/kernel 的模块化内核方向一致） |
| 2. NT 侧车 PoC | RunSpec API 服务、数据馈送、权重回放适配器（含 T+1/涨跌停最小规则）、结果映射、双跑对齐实验 | 10–16 | 用实测数据回答"A 股规则在 NT 上的真实成本" |
| 3. 双内核生产化 | kernel 路由、对齐回归、CI/CD（Python 运行时进流水线）、监控、运维文档 | 6–10 | Go 内核降级为可退役的备选实现 |
| | **合计** | **22–36 人周** | 分阶段交付，风险逐段释放 |

对比 §6.2 的整体替换（25–45 人周）：总量相当或略低，但**省掉策略重写（6–12 人周）与外围服务改造**，A 股规则成本从"venue 级自建"收缩为"适配器级自建"，且阶段 1 不依赖 NT 的任何决策。长期看，若双跑对齐验证通过且 NT v2 趋于稳定（[cite:1][cite:24]），Go 内核可退役，内核维护成本转移给上游开源社区——即委托方 buy vs build 目标的达成形态。

**修正后的行动顺序**（替代 §7 三步走的第 1–2 步）：① 契约化与策略层解耦（阶段 1）→ ② 权重回放适配器 + 双跑 PoC（阶段 2）→ ③ 决策门：对齐数据 + NT v2.0.0 正式版 → 生产化或回退。

### 8.6 与既有未完成任务（docs/TASKS.md）的关系

TASKS.md 主线 K0–K7（模块化内核）已全部完成，遗留未完成项与本路线的关系分三类：

- **并入阶段 1 一起做（直接相关）**：K7 遗留①②（`quant.external_signals` DDL 并入 migrate、外部模型 HTTP 写入入口）、OBS-01 遗留②（UI 不读 `BacktestResult.status` 仍按成功呈现）、K0-P2-1（三条不变量 AST 护栏）。其中 **K7 的"信号表 + as_of 拉取"模式（`SignalStrategy` + `SignalStore`，含注入→成交 e2e 验证）正是 SignalSpec/SignalProvider 的现成雏形**——外部进程写信号、内核消费的通路已跑通，阶段 1 的策略层解耦可复用，工作量可下调。
- **照旧独立推进，不阻塞本路线（正交）**：AUD-61（e2e UI 套件对齐）、P2-1/P2-2（宏观/产业链数据底座）、K0-P2-3a/b（Verify 计数口径、topicSet 双真相）。
- **建议推迟裁决**：K4 遗留的"impact 是否设为默认滑点"——待阶段 2 双跑对比数据在手后再定；K1-P2-1~4（内核装配治理）可在阶段 3 前收掉，是 EngineRunner 可插拔的稳定性基础。

战略提示：K0–K7 模块化内核主线与 buy vs build 目标同向（都在降低内核耦合与维护成本）；K3/K4/K6 的能力虽与 NT 部分重叠，但本路线决定策略层保留 Go，故该投入全部保值，且 K4 的执行算法/冲击模型恰是双内核对比的基线资产。

---

## Sources

[cite:1] NautilusTrader GitHub README（架构、整合表、版本策略、许可）— https://github.com/nautechsystems/nautilus_trader
[cite:2] GitHub API 仓库数据（stars/forks/issues，2026-10-12 实测）— https://api.github.com/repos/nautechsystems/nautilus_trader
[cite:3] NautilusTrader LICENSE 全文（LGPL-3.0）— https://raw.githubusercontent.com/nautechsystems/nautilus_trader/master/LICENSE
[cite:4] NautilusTrader 官网首页 — https://nautilustrader.io/
[cite:5] 文档 Overview（Rust 核心 / Python 控制面）— https://nautilustrader.io/docs/latest/concepts/overview/
[cite:6] 官方博客 Why NautilusTrader Exists（parity 架构论证）— https://nautilustrader.io/blog/why-nautilustrader-exists/
[cite:7] 文档 Fill Models（约 12 种成交模型）— https://nautilustrader.io/docs/latest/concepts/backtesting/fill-models/
[cite:8] 文档 Backtest Data and Venues（L1/L2/L3 数据粒度）— https://nautilustrader.io/docs/latest/concepts/backtesting/data-and-venues/
[cite:9] 文档 Accounts and Margin（账户/保证金/资金费）— https://nautilustrader.io/docs/latest/concepts/backtesting/accounts-and-margin/
[cite:10] 文档 Strategies（Strategy/回调 API）— https://nautilustrader.io/docs/latest/concepts/strategies/
[cite:11] 文档 Integrations（适配器总表）— https://nautilustrader.io/docs/latest/integrations/
[cite:12] 文档 Bybit 适配器（公司行为风险事件）— https://nautilustrader.io/docs/latest/integrations/bybit/
[cite:13] 仓库 Backtest Engine Benchmarks（BENCHMARKS.md）— https://raw.githubusercontent.com/nautechsystems/nautilus_trader/develop/crates/backtest/benches/BENCHMARKS.md
[cite:14] GitHub Releases（v2.0.0rc6，2026-10-04）— https://github.com/nautechsystems/nautilus_trader/releases
[cite:15] 文档 Installation（Python 版本、wheel、Docker 镜像）— https://nautilustrader.io/docs/latest/getting_started/installation/
[cite:16] 文档 FFI Memory Contract（C FFI 覆盖范围）— https://nautilustrader.io/docs/latest/developer_guide/ffi/
[cite:17] 文档 Message Bus（external egress/ingress、Redis Streams）— https://nautilustrader.io/docs/nightly/concepts/message_bus/
[cite:18] 文档 Backtest APIs and Repeated Runs（BacktestNode/BacktestRunConfig）— https://nautilustrader.io/docs/latest/concepts/backtesting/apis-and-runs/
[cite:19] 文档 Custom Data（自定义数据机制与 Arrow/Parquet 约束）— https://nautilustrader.io/docs/nightly/concepts/custom_data/
[cite:20] 文档 Reports（报告/指标导出）— https://nautilustrader.io/docs/latest/concepts/reports/
[cite:21] GitHub Issue #4599 [RFC] Support for Chinese Capital Markets — https://github.com/nautechsystems/nautilus_trader/issues/4599
[cite:22] 维护者 cjdsellers 对 #4599 的回复（2026-07）— https://github.com/nautechsystems/nautilus_trader/issues/4599#issuecomment-5112952574
[cite:23] 文档 Live Trading（回测/实盘差异声明）— https://nautilustrader.io/docs/nightly/concepts/live/
[cite:24] 仓库 MIGRATION_V2.md — https://github.com/nautechsystems/nautilus_trader/blob/master/MIGRATION_V2.md
[cite:25] 仓库 ROADMAP.md（开源范围界定）— https://github.com/nautechsystems/nautilus_trader/blob/develop/ROADMAP.md
[cite:26] MrKeyoor：nautilus_trader 实测评测（构建/过渡期批评）— https://mrkeyoor.com/repos/nautilus-trader/
[cite:27] BacktestScore：NautilusTrader Review 2026 — https://www.backtestscore.com/tools/nautilus-trader
[cite:28] Uber Engineering：Building a Backtesting Service at Uber-scale — https://www.uber.com/en-AU/blog/backtesting-at-scale/
[cite:29] VeighNa（vnpy）官方功能介绍 — https://www.vnpy.com/docs/cn/community/info/introduction.html
[cite:30] QuantConnect Lean v2 发布公告 — https://www.quantconnect.com/lean/15474/lean-backtesting-engine-v2-released/
[cite:31] VectorBT PRO — https://vectorbt.pro/
[cite:32] TierZero：Best Backtesting Frameworks 2025（参数扫描性能对比）— https://tierzero.dev/blog/best-backtesting-frameworks-crypto-algo-trading-2025
[cite:33] BullAlert：Best Python Backtest Engines 2026 — https://bullalert.ai/blog/best-python-backtest-engines-2026
[cite:34] PyPI nautilus_trader（版本与安装）— https://pypi.org/project/nautilus_trader/
[cite:35] NautilusTrader Databento 集成页（解码基准）— https://nautilustrader.io/databento/
[cite:36] AGNTCY：Integrating Rust and Go with UniFFI — https://blogs.agntcy.org/technical/2026/01/27/integrating-rust-and-go-with-uniffi.html

**未找到的信息（明确标注）**：NT 用于 A 股回测的公开案例；NT 的 Go/Java/C++ 绑定或对外 gRPC/REST 控制 API；NT 官方股票分红/拆股/复权的通用回测机制；NT 官方统一的"回测每秒 X 消息"吞吐宣称；Lean 对 A 股规则支持的证据。
