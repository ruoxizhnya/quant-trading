---
status: evergreen
last-verified: 2026-09-22
verified-by: AUD-30 定位校准 + AUD-41 `## Configuration` 整段订正（2026-09-22）；其余段落沿用 2026-09-16 代码审查
---

# Quant Trading System - System Specification

> **Version:** 1.6.0 (AI 实验员实验室 — ADR-023 / ADR-024)
> **Owner:** 龙少 (Longshao) — AI Assistant
> **Related:** [PRODUCT.md](PRODUCT.md) (top-level), [VISION.md](VISION.md) (design), [ARCHITECTURE.md](ARCHITECTURE.md) (layout), [TEST.md](TEST.md) (quality)
>
> **Changelog v1.3 (Migration):**
> - 添加标准元数据头部（Status, Owner, Related）
> - 标注未实现服务 (Risk/Execution) 为 "Planned"
> - 统一 Strategy Interface 为实际代码签名
> - 添加文档导航链接
>
> **Changelog v1.4.1 (Documentation Sync, ODR-012, 2026-06-08 + 2026-06-10 P1 follow-up):**
> - CR-11: Data Source Management endpoints moved from AI Research section
>   to Analysis Service section (matches handlers_datasource.go)
> - CR-12: Backtest endpoints documented with `/api/backtest/*` prefix;
>   legacy `/backtest/*` aliases added as redirect section
> - CR-13: Data Service endpoints expanded to include registry views and
>   the multi-source sync routes (sync/ohlcv, sync/fundamental, etc.)
> - CR-16: AI Pipeline endpoints updated to `/pipeline/*` (not `/api/pipeline/*`)
> - Added `mode: 'sync'|'async'` discriminator note for backtest client
> - CR-29 (2026-06-10): Added 4 sections to Analysis Service API
>   (Batch Backtest, Walk-Forward, Data Source Management, Factor Analysis)
>   and the `/api`-prefixed Data Proxy variants
> - CR-33 (2026-06-10): `Signal` → `domain.Signal` consistency in Vision/SPEC
>
> **Changelog v1.6.1 (§Configuration 整段订正, AUD-41, 2026-09-22):**
> - `## Configuration` 段此前是一份**设计草图** —— 描述了一个并不存在的
>   `config/global.yaml`，且键名与实际不符（`database.name` / `redis.host|port|password` /
>   `app.env` / `services.*.port` 全无读取点）。现已改为**如实描述**：三份真实配置
>   文件及其定位方式、键名 → env 名规则、主要配置段导航、`${...}` 禁令、
>   策略 YAML 的真实 schema
> - 该段还描述了另一个**不存在**的文件 `config/strategies/value_momentum.yaml`
>   —— `value_momentum` 实际是 Go 实现 `pkg/strategy/examples/value_momentum.go`。
>   已改为 `pkg/ai/yaml.Config` 的真实 schema + `LoadStrategy` 的加载条件
> - `tools/check_doc_links.py` 新增第二项检查：文档里引用的 `config/` · `deploy/`
>   路径必须存在（按仓库根**或文档自身目录**解析）。这类引用**不是 Markdown 链接**，
>   原检查看不见它 —— 这正是本段能烂掉而无人发现的原因
>
> **Changelog v1.6.0 (定位校准到 ADR-023/024, AUD-30, 2026-09-22):**
> - 顶层定位从 ADR-022（Proposed，**从未实施**，已被 ADR-023 取代）切到
>   [ADR-023](adr/adr-023-ai-experimenter-lab.md)（AI 实验员 + 人类监督者实验室）
> - §Unified Research Platform → **§AI 实验员实验室**：四层（L0-L3）改为 ADR-023 的
>   三层（**L1 数据 / L2 能力 / L3 AI 编排**）；「双对等工作面 + 飞轮闭环」取消，
>   EquityDeep 降为 L1 数据底座；新增验证器链（5 确定性 + 1 因果）与
>   「**可校准，不是准确**」目标
> - 新增 §策略执行载体（ADR-024）：执行载体是 YAML → `ExpressionStrategy`，
>   LLM 生成的 Go 代码只是 artifact（不加载、不执行），不做 `plugin.Open`
> - 保留 ADR-022 中**经 ADR-023 明确承接**的三件事：单一数据面、内容坐标证据、
>   按数据性质分区（A-E）
> - ⚠️ **层号已改**：旧版用 ADR-022 的 `L0 = 数据面`；ADR-023 是 `L1 = 数据层`。
>   本文件与 `docs/archive/` 旧任务号中的「L0」一律读作 **L1**
>
> **Changelog v1.5.0 (Unified Research Platform, ADR-022, 2026-09-15):**
> - 新增 §Unified Research Platform：四层架构（L0-L3）、双对等工作面、数据归属（A-E 分区）
> - 新增 Evidence API 规格草案（`GET /api/evidence/{content_hash}`，Proposed 未实现）
> - 顶层定位变更：Quant Lab 降维为共享底座，EquityDeep 升级为工作面 1 — 见 [PRODUCT.md](PRODUCT.md)
> - 关联文档新增 [PRODUCT.md](PRODUCT.md)（顶层 canonical，优先级高于本文件）

---

## Overview

A production-grade quantitative trading system targeting A-share markets with market-agnostic core services. The system implements a microservices architecture with hot-swappable multi-factor strategies, dynamic risk management, and comprehensive backtesting capabilities.

**Phase 2.5 Changes:**
- Unified error handling: `pkg/errors` with structured error codes (ErrorCode, AppError)
- ATR StopLoss: Market regime-adaptive stop loss (bull/bear/sideways multipliers)
- Strategy interface finalized: Configure(), Weight(), GenerateSignals(), Cleanup()
- Signal type enhanced: Direction enum, Factors map, Metadata map
- Test coverage: 55+ unit tests across core packages

**Phase 4 Changes (AI-Native Evolution):**
- AI Research Service (port 8086): Factor discovery, strategy generation, optimization
- Execution Service abstraction: BacktestExecutionService with pluggable slippage models
- Paper Trading API: Full order lifecycle management with simulated broker
- AI Components: FactorLab, StrategyWorkshop, EvolutionObs, GenealogyTree, FitnessChart
- Gene Pool: Factor and strategy gene pool with PostgreSQL persistence
- Metrics: IC/RankIC calculator, Turnover calculator

> ⚠️ **上表是 Phase 4 的「历史交付清单」，不是现状**（2026-09-22 复核）：
> `:8086` AI Research Service **已删除**（2026-09-18，P2-5）；
> `/api/paper/*` Paper Trading API **已删除**（AUD-19 —— 而且它**从未挂载过**，
> 前端页面打开就是 404）；前端 AI Components（FactorLab / StrategyWorkshop /
> EvolutionObs / GenealogyTree / FitnessChart）**已删除**（ODR-045）；
> Gene Pool 端点全仓零注册。**现行状态以下面的 Microservices 一节为准。**
- Search: TPE Bayesian optimization, Genetic Algorithm, Walk-Forward validation
- Drift Detection: Mean shift, variance shift, distribution shift detection
- Evolution: Population management with selection, crossover, mutation operators

---

## Architecture Principles

1. **Market-Agnostic Core**: Core services (strategy, risk, execution, analysis) operate on abstract interfaces, making them reusable across markets (A-share, US equities, crypto, etc.)
2. **A-Share Specifics**: Data layer and some configurations are A-share specific (tushare.pro, Chinese market conventions)
3. **Declarative Strategies**: Strategies are defined via YAML configuration, loaded dynamically at runtime
4. **Hot-Swap Capability**: Strategies can be loaded, replaced, and unloaded without service restart

---

## AI 实验员实验室（ADR-023 / ADR-024）

> **Status**: Accepted — 顶层定义已落盘，实现按阶段推进。
> **Canonical 定义**: [PRODUCT.md](PRODUCT.md)（顶层产品）→ [ADR-023](adr/adr-023-ai-experimenter-lab.md)（架构决策）。
> 本节仅摘录对 API/数据模型有约束力的部分；冲突时以 PRODUCT.md / ADR-023 为准。
>
> ⚠️ **本节上一版按 [ADR-022](archive/superseded-adr/adr-022-unified-research-platform.md)
> 陈述**（四层 L0-L3 / 双对等工作面 / 飞轮闭环）—— 该 ADR 在 Proposed 期间即被
> ADR-023 取代、**从未实施**（见 [ADR.md](ADR.md) 索引）。ADR-023 明确保留了
> ADR-022 中方向正确的三件事：**单一数据面、内容坐标证据、按数据性质分区**，
> 下面照录；被否掉的是「两个对等工作面 + 飞轮闭环」。
>
> ⚠️ **层号变了，别照抄旧文**：ADR-022 用 `L0 = 数据面`，ADR-023 用
> **`L1 = 数据层`**。本文件及 `docs/archive/` 里旧任务号中的「L0」一律读作
> **L1 数据层**（例：原「L0-1 单一摄取门」= 现在的 L1 摄取门）。

### 三角色

| 角色 | 谁 | 职责 |
|---|---|---|
| **实验室主任** | 人（用户） | 给方向、批阅、拍板。**不操作仪器** |
| **AI 实验员** | Hermes agent | **操作底座**：挖因子、调系数、跑回测、看结果、决定下一步 |
| **验证器**（validation subagent） | 验证方 | **主动证伪**；被调用、不自主循环；输出概率估计 + 质疑清单 |

**AI 是操作员，不是生成器。** 底座是仪器，已存在且可用；缺的是「操作它的人」和「看见他在干什么的窗」。

### 三层模型（L1-L3，编排者唯一）

```
L3  AI 编排层    AI 实验员（唯一编排者，会循环） · 因果审查（被调用，不循环）   ← 唯一需要人在环
L2  能力层       [MCP Tool Bridge] → 因子计算 | 回测 | 产业链查询 | 证据查询 | 验证器链×5   ← 禁止 AI 直接摸数据
L1  数据层       行情 | 财务 | 产业链图谱 | 研究洞察 | 实验日志
```

对 API 的约束：

- **分界线是「能否无人值守」**，不是「静态 vs 动态」。L1/L2 给定输入必有确定输出、
  可自动测试；L3 会失败，必须可监督。
- **能力层存在的意义是拦住 AI 直接读库/读文件。** 现状仍有违反
  （`pkg/tools/builtin/research_tool.go` 直接 `os.ReadFile` 读 vault），须收敛。
- **编排者只有一个**：禁止 EquityDeep 成为第二个 agent。直接后果是
  **不存在「EquityDeep 自己的工作流端点」** —— 它没有独立编排，只有被调用的查询面。
- **验证器不是第二个编排者**：它是被调用的（输入一份提案 → 运行一次 → 输出
  概率 + 质疑清单 → 结束），因此它的 API 形态是**同步的、无会话状态的**。
  **拒绝多 agent 投票与 arbitrator 仲裁**（错误相关会放大偏差；仲裁层不可解释）。

### 验证器链：5 个确定性 + 1 个因果

| 维度 | 性质 | 归属 | 内容 |
|---|---|---|---|
| 统计 / 经济 / 稳健 / 偏差 / 冗余 | 确定性 | **L2** | 多重检验校正 / 净收益（费·滑·冲击）/ 参数敏感度与分组 / 前视·幸存者·复权 / 与已有策略相关性 |
| **因果** | 需语义理解 | **L3** | 讲得出它为什么有效吗 |

> **六个维度里五个是纯计算，不需要 LLM。** 因此验证器实现于 `pkg/validation`，
> 入口 `ValidateProposal`；只有「因果」一项需要模型（`AttachCausal` 事后补）。

**信息隔离（这是 API 契约的一部分）**：

| | 挖掘 agent | 验证器 |
|---|---|---|
| 能看到 | 训练期数据 + 全部工具 | **保留期数据** + 提案 + **完整实验日志** |
| 看不到 | 保留期数据（防偷看） | 挖掘 agent 的推理过程（防锚定） |
| 输出 | 候选 + 因果来源 | **概率估计 + 质疑清单**（不是通过/不通过） |

两条不能破的实现约束：**给验证器 LLM 的请求不含回测结果**（`blind.Result = nil`
物理剜掉 —— 知道答案后的「预测」只是复述）；**没查就说没查**（未评估的维度进
`Unassessed`，绝不拿剩下几维凑一个完整的数字）。

### EquityDeep：从「工作面」降为数据底座

| 块 | 性质 | 归属 | 能否当因子 |
|---|---|---|---|
| **① 产业链图谱** | 公司-环节归属、上下游映射、传导指标 | L1 数据层，与行情/财务并列 | **能** |
| **② 研究洞察** | 「为什么值得关注」的自然语言 | L1，供 AI 生成假设时检索 | 不能 |

- 纵向深度研究从「**研究的前置步骤**」变为「**被异常触发的按需动作**」——
  不再要求验证一个想法前先给几十家公司各写完整档案。
- 产业链数据最有价值的形态是**传导信号**（上游涨价 → 下游毛利受压），而非行业标签。
- 每个因子增加 `hypothesis_source` 字段：**从产业链假设出发挖出的因子天然带因果
  解释**，这解决「AI 挖的因子不敢用」问题。

### 目标：可校准，不是准确

系统输出「未来有效的概率 X%」，考核指标是**校准误差**（声称 60% 的应实际命中
50–70%），而非推荐准确率。理由：校准良好的信心才能用于配仓位；不准却表现得有
把握会导致重仓受伤。

### 数据归属（A-E 分区，不重复存储）

| 类别 | 内容 | 唯一权威位置 | 其他侧副本 |
|---|---|---|---|
| A | 原始源响应 | PG `ingest.raw`（`content_hash` 唯一键） | ❌ 仅持 `content_hash` |
| B | 规范化数据（OHLCV / 财报 / 日历 / 公司行为） | PG `market.*` | ❌ 只读证据 API |
| C | 派生计算结果（因子 / 回测 / IC） | PG `quant.*` + Redis `factor_cache` | ❌ 只读计算 API |
| D | 研究叙事（人的判断） | **Vault markdown**（事实源） | — |
| E | 研究结构化状态 | PG `research.*`（D 的确定性投影，可 DROP 重建） | 权威在 D |

### 证据服务（已实现 — 原「L0-3」，见 [ODR-050](archive/odr/odr-050-p1-base-contract-landing.md)）

证据坐标从"文件路径 + 模糊字符串"升级为**不可变内容坐标**：

```
citation = { source, dataset, key, as_of, content_hash }
```

**摄取入口**（已实现 — 原「L0-1」，见 [ODR-051](archive/odr/odr-051-l0-1-single-ingest-entry.md)）：

| Method | Path | 说明 |
|---|---|---|
| POST | `/api/ingest/raw` | 外部生产者（akshare 侧 / 同步 executor）上报原始响应，落 `ingest.raw`；同 `content_hash` 幂等 |
| POST | `/api/ingest/equitydeep` | 归一化已归档的 EquityDeep 快照（ndjson，每行一条 `contracts/snapshot.schema.json` 记录）→ `fundamentals_detail`（类 B）；须先经 `/api/ingest/raw` 归档并携带其 `content_hash`（见 [ODR-055](archive/odr/odr-055-eqd-p1-2-vertical-factors.md)） |
| GET | `/api/evidence/{content_hash}` | 返回该哈希对应的**唯一原始记录**（类 A），404 表示未摄取 |

约束：

- `content_hash` 由 L1 摄取时计算并作为 `ingest.raw` 唯一键，全平台共享；
- `POST /api/ingest/raw` 是类 A 数据的**唯一写入口**，`source`/`dataset`/`key` 三者
  构成人类可读坐标，`content_hash` 为机器坐标；
- `POST /api/ingest/equitydeep` 是 EquityDeep 快照进入类 B（`fundamentals_detail`）
  的唯一门：它**拒绝** `ingest.raw` 不认识的 `content_hash`，并把该哈希盖在每一行
  `snapshot_uri` 上 —— 没有可解析的原始响应在背后，任何数字都进不了
  `fundamentals_detail`；
- **EquityDeep（数据底座）不得自建数据副本**，运行期经本 API 只读取数；
- 该设计在架构层面消除 ODR-047 记录的 P0 假阳性缺陷（校验对象由"文本"变为"citation 元组 + JSON Pointer"）。

### 策略执行载体（ADR-024）

**一条意图的落地路径是固定的，回测跑的是表达式算出来的信号，不是 LLM 写的 Go 代码**：

```
自然语言 → intent（语义类型）→ YAML 配置 → ExpressionStrategy → GlobalRegister → 回测
```

| 约束 | 内容 |
|---|---|
| **执行载体** | YAML → `ExpressionStrategy`（确定性引擎）。人能审阅能改；同样输入永远得到同样的起点 |
| **LLM 生成的 Go 代码** | 降级为**可审阅 artifact** —— 继续生成、继续真编译校验，留在 `result.GeneratedCode` / `result.BuildError` 里给人看，但**不加载、不执行** |
| **代码生成失败不阻断实验** | 它失败时 pipeline 不再 fail（回测压根不用那段代码）。此前会让整个 pipeline 失败 |
| **不做 `plugin.Open`** | Windows 不支持 `-buildmode=plugin`；依赖版本须逐字节一致；且「让 LLM 写代码再动态加载」= AI 造仪器，与 ADR-023 冲突。**ADR-001 的插件机制保留** —— 它服务人工编写、需要热插拔的策略，与「AI 生成代码」是两回事 |
| **给不出默认表达式的意图** | `value` / `quality` 要的是 PE / PB / ROE，而表达式引擎只暴露 OHLCV → **明确失败**，不套一个无关的价格表达式（那会跑出看似像样、实则答非所问的回测数字，比失败更糟）。见 TASKS P2-12 |
| **参数如何进入载体** | 见 [ADR-024](adr/adr-024-expression-as-execution-target.md) §补充（2026-09-17，P1-2b） |

> **估值字段按可用日接入**：倍数非正一律 NaN（PE 为负是亏损不是便宜）；
> 无财报数据时用 pe **必须报错**，不能返 0。

---

## Core Domain Models

> **Canonical Path (S7-P3-4)**: 市场数据类型（OHLCV / Stock / Fundamental /
> FundamentalData / IndexConstituent / Split / Dividend / Provider）的 canonical
> 定义位于 [`pkg/domain/market/`](../pkg/domain/market/)。
> `pkg/domain/types.go` 通过 Go type alias（`type OHLCV = market.OHLCV`）重导出
> 这些类型以保持向后兼容 — 旧代码 `domain.OHLCV` 仍可用，新代码应直接 import
> `pkg/domain/market`。详见
> [ARCHITECTURE.md — Domain Market 软分层架构](ARCHITECTURE.md#domain-market-软分层架构-pkgdomainmarket-s7-p3-4)。

### Stock
```go
type Stock struct {
    Symbol         string    // e.g., "000001.SZ", "600000.SH"
    Name           string    // e.g., "平安银行"
    Exchange       string    // "SZ", "SH", "BJ"
    Market         string    // "A-share", "US", "Crypto"
    Sector         string    // e.g., "金融", "科技"
    MarketCap      float64   // Total market cap in CNY
    FloatMarketCap float64   // Float market cap in CNY
    Status         string    // "active", "suspended", "delisted"
}
```

### OHLCV (Candlestick Data)

> **规范层 schema（ADR-031 / D1）**：字段全集、时间语义与聚合口径见
> [规范层 Schema（Canonical Layer）](#规范层-schemacanonical-layer) 节
> （防漂移测试钉住字段表）。相对旧定义的变更：**频率是记录自带字段**
> （Frequency，不靠来源表隐式区分）；每条 bar **自带可见时间点**
> （VisibleAt，PIT 语义）。
```go
type OHLCV struct {
    Symbol    string
    Date      time.Time // bar 周期起点：1m=分钟起点；15m=bucket 起点；1d=交易日（UTC 零点）
    Open      float64
    High      float64
    Low       float64
    Close     float64
    Volume    float64
    Turnover  float64   // in CNY
    TradeDays int
    Frequency market.Frequency // "1m"/"15m"/"1d"；零值 "" = 未声明（遗留取数路径不填）
    VisibleAt time.Time         // 可见时间点（PIT）：1m/15m=周期结束；1d=当日 15:00 收盘 CST
    LimitUp   bool              // 涨停（日线路径有值；聚合 bar 恒 false，不推导）
    LimitDown bool              // 跌停（同上）
}
```

### Fundamental Data

> **规范层 schema（ADR-031 / D1）**：数值字段一律 `*float64`——
> **缺失（nil）与等于 0 是两件不同的事**（PE=0 会被读成"白送的股票"）。
> 字段全集与时间语义（Date=可用日 / AnnDate=披露日即可见时间点）见
> [规范层 Schema（Canonical Layer）](#规范层-schemacanonical-layer) 节。
```go
type Fundamental struct {
    Symbol       string
    Date         time.Time  // 可用日：从哪天起这条数据才存在（COALESCE(ann_date, trade_date)）
    AnnDate      *time.Time // 真实披露日（nil = 未知）；披露日即可见时间点
    PE           *float64   // Price-to-Earnings；nil = 缺失
    PB           *float64   // Price-to-Book
    PS           *float64   // Price-to-Sales
    ROE          *float64   // Return on Equity (%)
    ROA          *float64   // Return on Assets (%)
    DebtToEquity *float64   // Debt to Equity ratio
    GrossMargin  *float64   // Gross margin (%)
    NetMargin    *float64   // Net profit margin (%)
    Revenue      *float64   // Total revenue
    NetProfit    *float64   // Net profit
    TotalAssets  *float64
    TotalLiab    *float64
}
```

### Market Data Aggregator
```go
type MarketData interface {
    GetOHLCV(symbol string, start, end time.Time) ([]OHLCV, error)
    GetFundamental(symbol string, date time.Time) (*Fundamental, error)
    GetStocks(filter StockFilter) ([]Stock, error)
}
```

> **规范层取数入口**：新代码使用 `pkg/marketdata.Router.Bars(ctx, freq,
> symbol, start, end)`——声明频率（1m/15m/1d）、返回自带 VisibleAt 的
> bar，不感知底层表。本接口的 GetOHLCV 是遗留路径（无频率声明、无
> VisibleAt，护栏基线登记在册），消费方按内核迁移时逐个切 Router。
> 路由与聚合口径见 [规范层 Schema（Canonical Layer）](#规范层-schemacanonical-layer)。

### Signal

> **Canonical definition** — matches [pkg/strategy/strategy.go](../pkg/strategy/strategy.go)

```go
type Signal struct {
    Symbol      string             `json:"symbol"`
    Action      string             `json:"action"`
    Strength    float64            `json:"strength"`
    Price       float64            `json:"price"`
    Date        interface{}        `json:"date"`
    Direction   domain.Direction   `json:"direction"`
    Factors     map[string]float64 `json:"factors"`
    Metadata    map[string]interface{} `json:"metadata"`
    OrderType   domain.OrderType   `json:"order_type"`
    LimitPrice  float64            `json:"limit_price"`
}

type Direction int
const (
    DirectionLong Direction = 1
    DirectionShort Direction = -1
    DirectionClose Direction = 0
)
```

### Position & Portfolio
```go
type Position struct {
    Symbol      string
    Quantity    float64
    AvgCost     float64
    CurrentPrice float64
    UnrealizedPnL float64
    RealizedPnL float64
}

type Portfolio struct {
    Cash        float64
    Positions   map[string]Position
    TotalValue  float64
    DailyReturn float64
}
```

## 规范层 Schema（Canonical Layer）

> **定位**（ADR-031 蓝图 §4，2026-10-14 D1 落定）：全系统数据取用只认
> 一套规范层 schema——三类字段（OHLCV / Fundamental / FactorOutput），
> **每条记录自带频率与可见时间点**；connector 之外无人感知底层表
> （ohlcv_daily_qfq / ohlcv_minute 只归 pkg/marketdata 的 Router 与
> 登记的遗留基线）。
>
> - 本节是**文档真相**；代码真相在 `pkg/domain/market`（OHLCV /
>   Fundamental / Frequency / 时间语义）、`pkg/domain`（FactorOutput）、
>   `pkg/marketdata`（Router 透明路由 + 聚合口径）。
> - **防漂移**：`pkg/domain/spec_drift_test.go` 以反射比对下文三张字段表
>   与 Go 类型（字段名 + json tag，双向）及频率词表；对不上即红。
> - **棘轮护栏**：`internal/repoguard/canonical_layer_test.go` 禁止非测试
>   Go 文件新开直连物理表的路径（只减不增，见下文「护栏」）。

### 频率词表

<!-- drift:frequency -->
| 频率 | Go 常量 | 取数路径 | 说明 |
| --- | --- | --- | --- |
| 1m | market.Freq1m | ohlcv_minute 原样 | 日内 bar；含集合竞价打点（开盘 09:15–09:25 / 收盘 14:57–15:00） |
| 15m | market.Freq15m | 由 1m 聚合产出（不物化） | 日内 bar；bucket 对齐见「聚合口径」 |
| 1d | market.Freq1d | ohlcv_daily_qfq（Tushare 前复权） | 日频 bar；唯一进回测的口径 |
<!-- /drift:frequency -->

**扩展频率 = 四件套同步**：加常量 + Router 路由分支 + 聚合口径 + 本节
词表，缺一即漂移（测试会红）。**未知频率 fail-loud**：`ParseFrequency`
报错、`Router.Bars` 拒请求，不静默回退任何「默认」频率——回测里
「默认」就是泄漏。

### 三类字段（规范层 schema）

三类字段的共同纪律：**缺失 ≠ 0**。无 bar = 缺失（停牌 / 未上市 /
当日无成交，不零填充）；可空数值字段（`*float64`）nil = 缺失，
消费前必须显式判空。

#### OHLCV — `market.OHLCV`

<!-- drift:ohlcv -->
| 字段 | Go 类型 | json tag | 语义 |
| --- | --- | --- | --- |
| Symbol | string | symbol | 股票代码，如 "000001.SZ" |
| Date | time.Time | date | bar 周期起点：1m=分钟起点；15m=bucket 起点；1d=交易日（UTC 零点，PG DATE 扫描形态） |
| Open | float64 | open | 开盘价（聚合 bar = 首笔开） |
| High | float64 | high | 最高价（聚合 bar = 期间最高） |
| Low | float64 | low | 最低价（聚合 bar = 期间最低） |
| Close | float64 | close | 收盘价（聚合 bar = 末笔收） |
| Volume | float64 | volume | 成交量（股；聚合 bar = 期间合计） |
| Turnover | float64 | turnover | 成交额（CNY；聚合 bar = 期间合计）。分钟表 amount 可空，NULL 时记 0——字段级缺失唯一折零处（见「已知边界」） |
| TradeDays | int | trade_days | 成交日数（日线路径有值；聚合 bar 恒 0，不发明语义） |
| Frequency | market.Frequency | frequency | 记录自带频率："1m"/"15m"/"1d"；零值 "" = 未声明（遗留取数路径不填，规范层路径必须填） |
| VisibleAt | time.Time | visible_at | 可见时间点（PIT）：1m/15m = 周期结束时刻；1d = 当日 15:00 收盘（Asia/Shanghai）。零值 = 未声明 |
| LimitUp | bool | limit_up | 涨停标记（日线路径有值；聚合 bar 恒 false，不推导） |
| LimitDown | bool | limit_down | 跌停标记（同上） |
<!-- /drift:ohlcv -->

#### Fundamental — `market.Fundamental`

<!-- drift:fundamental -->
| 字段 | Go 类型 | json tag | 语义 |
| --- | --- | --- | --- |
| Symbol | string | symbol | 股票代码 |
| Date | time.Time | date | 可用日：从哪天起这条数据才存在（读取侧 COALESCE(ann_date, trade_date)） |
| AnnDate | *time.Time | ann_date | 真实披露日（nil = 不知道何时披露）。**披露日即可见时间点**，故本类型不设 VisibleAt |
| PE | *float64 | pe | 市盈率；nil = 缺失（≠ 0） |
| PB | *float64 | pb | 市净率 |
| PS | *float64 | ps | 市销率 |
| ROE | *float64 | roe | 净资产收益率（%） |
| ROA | *float64 | roa | 总资产收益率（%） |
| DebtToEquity | *float64 | debt_to_equity | 产权比率 |
| GrossMargin | *float64 | gross_margin | 毛利率（%） |
| NetMargin | *float64 | net_margin | 净利率（%） |
| Revenue | *float64 | revenue | 营业总收入 |
| NetProfit | *float64 | net_profit | 净利润 |
| TotalAssets | *float64 | total_assets | 总资产 |
| TotalLiab | *float64 | total_liab | 总负债 |
<!-- /drift:fundamental -->

频率为类别级声明：按日消费（1d）；公告驱动的事件语义由 AnnDate 承载
（connector 侧「事件流」声明属 D2）。

#### FactorOutput — `domain.FactorOutput`

<!-- drift:factor_output -->
| 字段 | Go 类型 | json tag | 语义 |
| --- | --- | --- | --- |
| Factor | string | factor | 因子名（命名空间与 domain.FactorType 冻结清单同域） |
| Date | time.Time | date | 分数对应日（按日分数序列） |
| Symbol | string | symbol | 股票代码 |
| Value | *float64 | value | 分数；nil = 缺失（输入未齐那天不产分，≠ 0） |
| Caliber | FactorCaliber | caliber | 口径标签随分数走："z-score" / "rank" / "raw"——量纲不同，禁止混用加权 |
| Frequency | market.Frequency | frequency | 产出序列频率；当前恒为 1d（D6 引入日内特征时扩展） |
| VisibleAt | time.Time | visible_at | 可见时间点：由输入数据的可见性推导（输入未齐 = 分数不可见） |
<!-- /drift:factor_output -->

因子定义入库 / 版本指纹属 D3；消费属 D6 / D8——D1 只冻结形状。

### 时间语义（PIT 红线）

蓝图 §4.2：每条数据必须自带「什么时候才可见」，否则即数据泄漏，且
极难事后发现（回测曲线照样漂亮）。

| 频率 | Date 语义 | VisibleAt 口径 |
| --- | --- | --- |
| 1m | 分钟起点 | 周期结束：stamped 10:00 → 10:01 可见 |
| 15m | bucket 起点 | bucket 结束：stamped 09:30 → 09:45 可见 |
| 1d | 交易日（UTC 零点） | 当日 15:00 收盘（Asia/Shanghai）——Tushare 日更落库更晚，15:00 是「收盘后可见」的保守口径（不早于真实可见时刻） |

时区：Asia/Shanghai = 固定 +8 偏移（`time.FixedZone`），不依赖运行时
tzdata——Windows 开发机与精简 CI 容器默认没有 zoneinfo，
`LoadLocation("Asia/Shanghai")` 会直接失败。

### connector 注册契约（时间语义 + 频率口径，D2）

蓝图 §4.2 红线：时间语义漏声明 = 数据泄漏，且回测曲线照样漂亮、事后极难
发现。因此它不是注释约定，而是**注册期的结构性闸门**——connector 注册时
必须显式声明下两项，缺失 / 非法 / 与数据形态矛盾一律 fail-closed 拒收，
不静默放行、不降级、不猜默认值。

| 声明字段 | 枚举 | 说明 |
| --- | --- | --- |
| 时间语义 `TimeSemantics` | `announcement_day` / `after_close` / `next_open` / `custom` | 何时可见；`custom` 必须附判定表达 `VisibleExpr` |
| 频率口径 `FrequencySpec` | `daily` / `intraday` / `event` | 数据源的产出节奏（与记录自带 `Frequency` 是两个正交的轴） |

Go 侧词表是唯一真相：`pkg/domain/market`（`TimeSemantics` / `FrequencySpec`
+ `Parse*` fail-loud，零值 `""` = 未声明、不算合法）。connector 契约在
`pkg/data/source`：`ConnectorDeclaration`（`DataType` / `TimeSemantics` /
`VisibleExpr` / `VisibleField` / `Frequency`）+ `DataSourceAdapter.Declarations()`
（在接口上，新 adapter 漏声明 = 编译错误）+ `Registry.Register` 的
`validateDeclarations`（校验在改状态之前，被拒 adapter 不污染 Registry）。

`VisibleField` 是记录中承载可见时间锚点的字段名：`DataSchema.Fields` 里的
date/datetime 字段名，或**保留锚点记号**——`trade_time`（OHLCV 类数据锚点在
`DataItem.TradeTime`，不在 schema 字段里）、`ann_date`（财报类数据锚点是规范
层 `market.Fundamental.AnnDate`，披露日即可见时间点，见「规范层 Schema」；
connector 的 wire schema 与 `Data` map 都不承载它，故列为保留记号）。

**fail-closed 三类拒收**（错误信息点名 adapter + 数据类型 + 缺什么）：

| 类别 | 触发 | 哨兵 |
| --- | --- | --- |
| 未声明 | `SupportedTypes` 某成员没有对应声明 | `ErrDeclarationMissing` |
| 声明非法 | 时间语义/频率口径枚举非法、`custom` 缺表达式、重复声明、声明了未支持的类型 | `ErrDeclarationInvalid` |
| 形态矛盾 | 可见时间锚点 `VisibleField` 不在该数据类型 schema 里，或其类型不是 date/datetime | `ErrDeclarationMismatch` |

**高危样例（蓝图 §4.2 点名场景）**：市场活跃度 / 拥挤度类数据「收盘后才
可得」，盘中用当日值即偷看——其声明必须是 `TimeSemantics: after_close`
（保守口径，不早于真实可见时刻），**不得**声明任何盘中可见语义。D5 接入
该源时直接照此声明。

**存量 connector 声明一览**（`pkg/data/source/declarations.go`，「声明即登记」；
新增 connector 必须在此为每个 `SupportedTypes` 成员补声明）：

| adapter | 数据类型 | 时间语义 | 频率口径 |
| --- | --- | --- | --- |
| tushare | `ohlcv_daily` | `after_close` | `daily` |
| tushare | `fundamentals` | `announcement_day`（锚点=`ann_date` 保留记号，**披露日**，非报告期截止日） | `event` |
| tushare_macro | `macro_series` | `announcement_day`（锚点=`obs_date`，见已知边界④） | `event` |
| mootdx | `realtime_quote` | `custom`（观察时刻即可见） | `intraday` |
| mootdx | `ohlcv_minute` | `custom`（bar 周期结束即可见） | `intraday` |
| mootdx | `ohlcv_daily` | `after_close` | `daily` |
| eastmoney | `capital_flow` | `after_close` | `daily` |
| eastmoney_sectors | `sectors` | `custom`（盘中快照，观察即可见） | `intraday` |
| eastmoney_sectors | `stock_sector` | `custom`（参考数据，观察即可见） | `event` |
| eastmoney_toplist | `top_list` / `limit_up_pool` | `after_close` | `daily` |
| juchao | `announcements` | `announcement_day`（`ann_time`） | `event` |
| xueqiu | `hot_search` | `custom`（观察时刻即可见） | `intraday` |
| xueqiu | `news` | `custom`（`publish_time` 即可见） | `event` |
| alpha_vantage / yahoo_finance | `global_ohlcv` | `after_close` | `daily` |

**护栏与测试**（`pkg/data/source/contract_test.go`）：三类拒收逐条负例、
全部存量 connector 的声明完整（`SupportedTypes` ⇄ 声明一一对应）且整批
注册成功、被拒 adapter 不污染 Registry（注册原子性）。**破坏验证（改坏→
红→还原→绿）**：① 注册一个不声明时间语义的 connector → 拒收且报错点名；
② 删掉任一 adapter 的某条声明 → 集成测试红；③ 把「收盘后可见」改成词表
外的「盘中即可见」→ 集成测试红。

**已知边界（D2 如实登记）**：① `mootdx` 分钟表 `amount` 为 NULL 时
`Turnover` 记 0 —— 字段级折零由 D2 在契约文档收编（D1 已点名），本单不改
ETL 行为；② 产业链 fetcher（`pkg/data/supply_chain.go`）不走 Registry，
其时间语义在此成文登记为已知边界，建 adapter 属 D5；③ `tushare` 财报
connector 的 wire schema / `Data` map **未透出 `ann_date`** —— 真实披露日
由底层 `data.TushareClient` 直写 `stock_fundamentals.ann_date`（规范层
`market.Fundamental.AnnDate` 承载可见语义），故锚点取保留记号 `ann_date`；
补齐透出链路（让 connector 的 `DataItem` 也带 `ann_date`）属单独立项
（TASKS D2-R1）；④ `tushare_macro` 的可见锚点声明为 `obs_date`，但
`obs_date` 是**观测日**（月频取当月首日），真实发布日**晚于**它——声明
口径对齐 P2-1 既有「obs_date 为 PIT」约定，该缺口（潜在前视）已登记为
跟进项（TASKS D2-R1），是否改判宏观层 PIT 门不在 D2 范围。

### 透明路由

调用方**声明频率**，不感知存在哪张表：

```go
Router.Bars(ctx, freq, symbol, start, end) ([]market.OHLCV, error)
```

| 声明频率 | 取数路径 | 数值口径 |
| --- | --- | --- |
| 1d | ohlcv_daily_qfq 直查 | 与遗留 storage.GetOHLCV 逐字段相等（回归红线：集成测试对拍，D1 ③5） |
| 1m | ohlcv_minute 直查 | 原样返回（volume 由表内 BIGINT 承载；amount → Turnover） |
| 15m | 1m 直查 + 聚合 | 见「聚合口径」；不物化 |
| 其他 | **fail-loud 报错** | 拒绝静默回退 |

start/end 语义：1d 按交易日闭区间（trade_date BETWEEN）；1m/15m 按
时刻闭区间（ts BETWEEN）。

**日内跨度边界（REV-01）**：1m/15m 单次请求的跨度上限 `maxIntradaySpan` = **366 自然日**
—— 单标的约 243 交易日 × 240 根/日 = **58,320 行**，× 实测 `unsafe.Sizeof(market.OHLCV)`
= 144 B/行 ⇒ 切片本体 **8.4MB**（含 symbol 字符串背板约 9.3MB）。超限 **fail-loud 报错**，
报错里点名已支持上限、当前请求跨度（两者都换算成自然日，因为纯 Duration 串读不出
「超了多少、该切几片」）与下一步动作「切片分次取」；不静默截断，也不做无上限全量加载。
1d 不受该上限约束（日线单票量级可接受）。

三点别读混：① 上限按**自然日**而非按行数 —— 每日 bar 数不同的市场同一跨度行数不同
（A 股 240 根/日、美股 390 根/日，D7/D9 国外线到位后同一跨度约差 1.6 倍），所以它是
**内存量级护栏**，不是"最多 N 行"的保证；② 与上表 15m 那行的「不物化」不矛盾 —— 那句说
15m 结果**不落库**，本边界管的是「分钟行先读进内存再聚合」这一步；③ 判定在**库层**，
今天 `Router.Bars` 还没有 HTTP 消费面（全仓零生产调用方，实测），将来接线方要负责把这个
error 映射成 400。

### 聚合口径（1m → 15m / 1d）

**主口径**：开 = 首笔开，高 = 期间最高，低 = 期间最低，收 = 末笔收，
量 = 期间合计（turnover 同比合计）。

**边界三细则**（蓝图 §9 #2 的归属地，2026-10-14 落定在此）：

1. **午休断点**：15m bucket 按会话内时钟对齐，**不跨午休**。每日 16 个：
   上午 09:30 / 09:45 / 10:00 / 10:15 / 10:30 / 10:45 / 11:00 / 11:15，
   下午 13:00 / 13:15 / 13:30 / 13:45 / 14:00 / 14:15 / 14:30 / 14:45。
2. **集合竞价**：开盘竞价打点（09:15–09:25；实现口径为早于 09:30 的
   打点）并入首个上午 bucket（09:30，开价取竞价成交价）；收盘竞价打点
   （14:57–15:00，含 15:00:00 收盘价打点）并入末 bucket（14:45）。
   上午收盘打点（11:30:00）对称并入上午末 bucket（11:15）——下午收盘
   打点接纳、上午收盘打点拒收没有口径理由；且 Tushare 1m 普遍含 11:30
   整点 bar（mootdx 导出不带），两种分钟形态在入站处兼容（REV-04，
   2026-10-15）。
3. **停牌日**：当日无 1m bar → **不产出**聚合 bar（缺失，不零填充）；
   半日停牌 → 已覆盖 bucket 照常产出、未覆盖 bucket 缺失。

另外两条硬规则：

- **时段外 bar fail-loud**：早于 09:15、落在午休（11:30–13:00）、晚于
  15:00 的打点 → 聚合器报错，不静默丢弃（静默丢 bar = 悄悄改口径）。
- **秒级精度**（REV-04，2026-10-15）：时段边界按秒判定——整点收盘打点
  （11:30:00 上午收盘 / 15:00:00 下午收盘）合法，分别并入 11:15 /
  14:45 bucket；边界带秒/亚秒的打点（11:30:01+、15:00:01+，含
  15:00:00.000000001 这类亚秒越界）拒收。分钟粒度判定会把 15:00:30 与
  15:00:00 混同，静默放宽口径边界——标准分钟 bar 秒恒 0 使该路径实际
  不可达，但非标数据不该靠「恰好没出现」活着。
- **不发明语义**：聚合 bar 的 TradeDays 恒 0、LimitUp/LimitDown 恒
  false（前者无消费方，后者需日线参考价，属 D6 按需计算）；1d 聚合的
  Date 用 UTC 零点，与日线表扫描形态一致，保证两种 1d 口径可比。

### 按内核翻译层

- **Go 侧直吃规范层**：`pkg/marketdata.Router` 是唯一读路径（见
  「透明路由」）。
- **NT 侧**（nautilus_trader 内核）：`nt-sidecar/tools/export_bars.py`
  是第一条实现，经 `nt-sidecar/sidecar/mapping.py` 的映射表把规范层行
  数据翻译成 NT `Bar()` 构造参数——**加一种数据类 = 映射加一行**
  （`nt-sidecar/tests/test_d1_mapping.py` 钉住）。
- **跨语言字面量契约**：Python `FREQ_1D = "1d"` 等常量与 Go
  `market.Freq1d` 等**逐字相同**，翻译层测试双向钉住（改一侧即红）。
- export_bars 换算：trade_date → UTC 午夜纳秒（与 daily 表 DATE 扫描形态
  一致）；volume 取规范层的**「股」**（tushare 的「手」由 pkg/data 同步侧
  按 sharesPerLot 换算，存量由 migration 052 对齐），导出层只要求它是整数
  （as_quantity 的整数纪律），不再做单位换算。

### 护栏（棘轮 + 防漂移）

1. **物理表直连棘轮**（`internal/repoguard/canonical_layer_test.go`）：
   非测试 Go 文件中出现 `ohlcv_daily_qfq` / `ohlcv_minute` 字符串字面量
   （SQL / DDL / 表名常量）的，只允许 `pkg/marketdata`（Router）与
   `directTableAccessBaseline` 登记的遗留文件。**只减不增**：新开直连
   路径 = 红；基线文件被清理 = 红（逼着删条目——白名单不许替已消失的
   违规打掩护）。
2. **防漂移测试**（`pkg/domain/spec_drift_test.go`）：上文三张字段表 +
   频率词表与 Go 类型反射比对，双向缺失都红。
3. **翻译层测试**（`nt-sidecar/tests/test_d1_mapping.py`）：Python 映射
   表与 Go 常量逐字一致；`bar_kwargs_for_row` 产出与 export_bars 消耗
   自洽。

## 因子定义 Schema（Factor Definition，D3）

> ADR-031 蓝图 §5.2/§6 三期①：因子定义从 Go 代码迁入「库里公式」，因子名
> 注册表从「代码冻结」改「库管理 + fail-closed」。**语义不放松**：名册
> 本身仍是冻结协议，变的是定义的存放与分派方式。

### 定义骨架（一个样，三种内核 + pending）

<!-- drift:factor_definition -->
| 字段 | JSON | 含义 |
|---|---|---|
| Name | name | 因子名。必须落在冻结名册（`domain.ParseFactorType`）内——D3 不放开「谁可以是新因子」 |
| Kernel | kernel | 计算内核：leaf / formula / code / pending |
| Formula | formula | leaf/formula 型的公式文本（`pkg/expression` DSL）。pending 型为空 |
| Validity | validity | 可选域过滤公式：与 Formula 同形求值，逐点非零（且非 NaN）才产出分数——输入落在数学定义域外（PE<=0、价格<=0）= 缺失，不是 0 分 |
| CodeRef | code_ref | code 型的 Go 出口标识（`pkg/data/factor_registry.go` 绑定表）。未知标识 fail-closed |
| Caliber | caliber | 口径标签：z-score / rank / raw。随分数走，苹果橘子不混用 |
| InputFields | input_fields | 消耗的规范层字段名（D1 字段词表 / expression fieldRegistry / equitydeep 契约字段码）。pending 型为空 |
| EffectiveFrom | effective_from | 生效日（YYYY-MM-DD，字符串契约——格式校验在 Go 侧） |
| Lookahead | lookahead | 前视纪律声明：横截面行情因子 = `trade_date`；纵向财报因子 = `ann_date<=trade_date`（PIT 在 storage 层强制） |
| Lookback | lookback | 公式型求值窗口（自然日）。code 型留 0 |
<!-- /drift:factor_definition -->

**产出 schema**（策略看到的那一半）不变，仍是 D1 冻结的 `FactorOutput`：
因子名 / 日期 / 股票 / 值（`*float64`，nil=缺失）/ 口径 / 频率 / 可见时间点。

### 内核四态

| 态 | 求值路径 | 现状（11 个冻结因子） |
|---|---|---|
| leaf | 表达式引擎（字段直取） | quality（`roe`） |
| formula | 表达式引擎（叶子 + 算子） | momentum（`close / ts_delay(close, 20) - 1`）、value（`1 / pe`） |
| code | Go 出口（`CodeRef` 绑定闭包） | 纵向 5 因子——单季差分 / TTM 桥接 / 跨期趋势需要财报期坐标，表达引擎的求值模型是日频数字序列、没有「季」维度，**不硬凑公式**（D3 判据） |
| pending | 不计算，fail-closed 报「已登记未定义」 | size / volatility / growth——名册有名字、计算线从未实现，**不发明数学定义** |

### 版本哈希纪律（可复现的铁律）

- `VersionHash = SHA-256(canonical JSON)`：字段全量进哈希（InputFields
  排序后），**改公式 = 新哈希天然成立**；方法是派生的，结构体里没有
  hash 字段——没有可篡改的存储入口。
- 库里存的哈希（`factor_definitions.version_hash`）写入时以派生值为准，
  加载时重新 derive 比对：不符即拒绝加载（`pkg/storage/factor_definition.go`，
  有测试钉住）。
- 同一次回测全程持同一 `Registry` 快照：定义不可能在计算中途被换掉
  （`Registry` 构造时定稿、之后只读）。

### 计算分派（registry 驱动）

```
FactorComputer.ComputeFactorByName(ctx, name, date)
  → Registry.Definition(name)        未登记 → ErrFactorNotRegistered（带可用清单）
  → code    → CodeRef 闭包（原 Compute*Factor 方法）
  → formula → 表达式引擎现算（Formula + Validity）
  → leaf    → 同上（公式即单 identifier）
  → pending → ErrFactorKernelUndefined
```

`ComputeAllFactors` 的因子清单 = 注册表投影（定义序），报告顺序与原
硬编码列表一致（可观测行为不变）；pending 因子显式进
`FactorBatchReport.Skipped`——「没算」必须说出口（AUD-15 同款纪律）。
公式型因子的窗口由定义 Formula 钉死：改窗口 = 改定义 = 新哈希，这是
版本纪律，不是退化。

### 数值回归红线

公式库版 vs 原代码版**同输入同输出**（`pkg/data/factor_formula_test.go`
对拍测试）：momentum / value / quality 三因子 raw / z-score / percentile
三列全等，域过滤（PE>0、ROE>0、两端价格>0）由 Validity 复现，历史不足
（动量不足 20 根 bar）由 `ts_delay` 窗口不足给 NaN → 缺失复现。

### 护栏（棘轮 + 防漂移）

1. **因子注册表护栏**（`internal/repoguard/factor_registry_test.go`）：
   非测试 Go 文件引用 `Compute*Factor` 内核方法（定义/绑定表三文件除外）
   = 绕过 registry 直调内核 → 红；同一 composite literal 内聚集 ≥3 个
   名册常量 = 库外重建清单 → 红。**只减不增**（基线带裁决理由）。
2. **种子词表测试**（`pkg/data/factor_registry_test.go`）：种子定义的
   财报字段码字面量与 `equitydeep.FieldX` 契约常量逐一比对，防漂移。
3. **防漂移测试**（`pkg/domain/spec_drift_test.go`）：本节目定义骨架表
   与 `domain.FactorDefinition` 反射比对，双向缺失都红。

### 已知边界（D3 如实登记）

- size / volatility / growth 三因子**无计算线**（全仓无 Compute* 出口、
  无数学定义）：D3 登记为 pending，不发明定义。查询它们得到的是
  fail-closed 报错而非静默 0——定义补齐属后续工单。
- 公式型因子的 universe 规则：全 market 字段 → OHLCV 出现过的股票；
  全 fundamentals 字段 → 财报快照股票；混源 → 并集（缺侧 NaN → 缺失）。
  三个公式型因子均不混源。
- `pkg/backtest/cache/factor_cache.go` 的回测预热清单是存量库外清单
  （基线登记），收敛到 Registry 读取留待 D6。

### 已知边界（D1 如实登记）

- 分钟表 `amount` 可空，NULL → Turnover 记 0：字段级缺失唯一折零处，
  D2 接 connector 契约时收编（bar 级「无 bar = 缺失」不受影响）。
- 会话边界秒级精度与 11:30 bar 形态（REV-04，2026-10-15）：
  `checkInSession` 按秒判定，11:30:00 并入 11:15 bucket、15:00:00
  并入 14:45 bucket（收盘打点对称接纳）；11:30:01 / 15:00:01 及
  亚秒越界打点拒收。Tushare 1m 的 11:30 整点 bar 与 mootdx（无该
  bar）两种分钟形态在聚合入站处兼容，无需同步侧 normalize——D6 灌
  Tushare 1m 前无需为此加处理。
- 15m 不物化：每次取数实时聚合；分钟表数据量上来后的物化 / 预聚合属
  后续性能工单，不在 D1 范围。
- 既有两表明细数据不迁移（D1 ④ 非目标）：迁移 = 消费方逐个切 Router，
  由棘轮基线逐条销号。

---

## 因子注册表治理细则（Factor Registration，D4）

> ADR-031 蓝图 §5.2 三条铁律、§6 协议变更纪律、§9 #3（参考外部门户信号
> `content_hash` 幂等先例）。D3 把因子定义从代码搬进库（「库里公式」），
> **D4 定「进库之后的纪律」**：谁在什么时候把哪一版定义接进来、哪一版被
> 回滚。语义不放松——名册仍是冻结协议，登记不是「写个字符串进库」。

### 登记记录（一条登记 = 定义快照 + 登记人）

<!-- drift:factor_registration -->
| 字段 | JSON | 含义 |
|---|---|---|
| Definition | definition | 登记时的**定义快照**（`FactorDefinition`）。改公式必须新登记一份（新哈希），老快照原样留存——「回滚后老版本仍可复现」的载体，自包含、不依赖别处还留着旧定义对象 |
| VersionHash | version_hash | 登记的版本指纹（四样之「哈希」）。**记录而非权威**：写入/加载都以 `Definition.VersionHash()` 重新 derive 比对，不符即拒收（与 D3 `factor_definitions.version_hash` 同款纪律） |
| Registrar | registrar | 登记人（四样之「登记人」）：谁把这一版定义接进来的。非空（纯空白不算） |
| Status | status | 生命周期态：active（生效，参与新引用解析）/ rolled_back（已回滚） |
| RegisteredAt | registered_at | 登记时刻（审计「何时」） |
| RolledBackAt | rolled_back_at | 回滚时刻。仅 Status=rolled_back 时有值 |
| RolledBackBy | rolled_back_by | 回滚人。仅 Status=rolled_back 时有值 |
| RollbackReason | rollback_reason | 回滚缘由（审计「为什么」）。仅 Status=rolled_back 时有值 |
<!-- /drift:factor_registration -->

### 四样齐全（fail-closed，缺一拒收）

一条登记**四样齐全**方可生效：**公式、哈希、登记人、生效日**。

| 四样 | 载体 | 校验 |
|---|---|---|
| 公式 | `Definition` 的内核体（leaf/formula 的 `Formula`、code 的 `CodeRef`） | `Definition.Validate()` 逐型校验；pending 态无内核体 → 拒收（`four-required: formula`） |
| 哈希 | `VersionHash` | 非空且 == `Definition.VersionHash()`（同一算法：canonical JSON 的 SHA-256，见 D3） |
| 登记人 | `Registrar` | `strings.TrimSpace` 非空 |
| 生效日 | `Definition.EffectiveFrom` | `YYYY-MM-DD`（由 `Definition.Validate()` 覆盖） |

校验在**两道关卡**共同守住（`pkg/domain/factor_registration.go` 的
`FactorRegistration.Validate` + Migration 053 的 DB CHECK）：连「绕过写入
路径直接 INSERT」也被库拒绝（registrar 非空 + 内核体存在 + 状态枚举）。

登记（`pkg/data/factor_governance.go` 的 `RegistrationBook.Register`）在四样
之外**再过一道 D3 注册表闸门**（公式可解析 / 过 L1 / identifier↔输入字段
双向相等 / 输入字段 ⊆ 规范层字段词表 / code_ref 已绑定）——登记不是「写个
字符串进库」，是「这一版定义真的能算」。同一 `(因子名, 版本哈希)` 重复登记
= 幂等 no-op；改内容必然改哈希 → 新行（`ErrRegistrationDuplicate`）。

### 回滚语义（钉死 —— 可复现性红线）

回滚 = **新引用不再命中该版本，历史回测仍能引到老版本**。两种引用解析：

| 引用 | 入口 | 语义 |
|---|---|---|
| 新引用（按名） | `RegistrationBook.ResolveEffective(name)` | 命中该因子最新的 **active** 版本；被回滚的版本不参与。无在册版本 → miss（`ok=false`，不凑默认定义） |
| 历史引用（按哈希） | `RegistrationBook.ResolvePinned(name, hash)` | 按 `(名, 哈希)` 精确锁定，**无论是否已回滚都命中**——历史回测钉着「当时那一版哈希」跑 |

因此「登记 v1 → 登记 v2（新引用抬到 v2）→ 回滚 v2（新引用回落到 v1，
钉 v2 的历史引用仍命中）」是可演练、可测试的确定性行为
（`pkg/data/factor_governance_test.go`）。回滚**不删版本行**，只把该行
`status` 置 `rolled_back` 并留痕——老定义快照原样在库，复现链不断。

### 审计留痕（谁 / 何时 / 改了什么 / 为什么）

每次登记与回滚各留一条 `RegistrationAuditEntry`（`factor_registration_audit`
表）：「谁」=`Actor`、「何时」=`At`、「改了什么」=`VersionHash`（内容指纹）
+ `Detail`、「为什么」=`Reason`。登记/回滚缺 actor 一律拒收（审计「谁」不可缺）。

### 并发语义（进程内账本是并发安全类型，REV-03）

`RegistrationBook` 是**可变**账本，与 D3 `Registry` 有别——后者是构造时定稿的
**不可变快照**（读侧无需加锁），前者写路径直接改索引 map、登记序 slice、审计
slice，故自持 `sync.RWMutex` 成为**并发安全类型**：

| 路径 | 方法 | 锁 |
|---|---|---|
| 写 | `Register` / `Rollback` | `mu.Lock`（整个方法是一个写事务：校验 + 去重 + 落账 + 留痕原子完成） |
| 读 | `ResolveEffective` / `ResolvePinned` / `Registrations` / `Audit` | `mu.RLock` |

读路径在写事务边界上取到自洽快照——不会看到「map 已写一半、审计还没补」的
撕裂中间态。锁只护**进程内**账本：签名与治理语义（四样校验、回滚保复现、审计
字段）均不变；DB 侧落库的并发由调用方事务管理（另立），HTTP 接入面另立。将来
接「登记 / 回滚」的 HTTP 或后台入口时，并发写不再退化成 map 并发写（panic 或
静默丢登记与审计留痕）。

### 演练记录（D4 ⑤1：登记 → 被公式引用 → 回滚）

可执行形态在 `pkg/data/factor_governance_test.go`
（`TestRegistrationBook_Drill_RegisterReferenceRollback`）：逐步断言等价于
逐步截图——① 登记 v1（审计 +1，新引用命中 v1）；② 解析出的定义过计算闸门
（`NewRegistry`，即「被引用」）+ 登记 v2 抬新引用、钉 v1 历史引用仍命中；
③ 回滚 v2（审计 +1，新引用回落 v1、钉 v2 历史引用仍命中）。

### 护栏与测试

1. **四样拒收**（`pkg/domain/factor_registration_test.go`）：8 例表驱动
   （缺公式/空公式/空哈希/哈希不符/空登记人/纯空白登记人/空生效日/生效日
   格式错）+ 未知状态 + 陈旧哈希。
2. **登记与回滚 fail-closed**：重复登记、回滚未登记/已回滚/缺 actor。
3. **破坏验证**（`pkg/storage/factor_registration_test.go`）：绕过写入路径
   直接 INSERT（缺 registrar / 缺内核体 / 未知状态）→ DB CHECK 拒收（护栏
   红）；走正常写入路径 → 绿。另钉「库行被绕过改坏 → 加载时哈希校验拒载」。
4. **防漂移测试**（`pkg/domain/spec_drift_test.go`）：上表
   `factor_registration` 锚点与 `domain.FactorRegistration` 反射比对，双向
   缺失都红。
5. **并发语义**（`pkg/data/factor_governance_test.go`，REV-03）：
   `TestRegistrationBook_ConcurrentRegisterResolveRollback` 在 `-race` 下把写侧
   （Register/Rollback）与读侧（Resolve*/Registrations/Audit）并发交叠，钉住
   写侧锁——并发下不撕裂；且同因子双版本并发登记**去重封闭**（恰好 1 成功、
   其余 `ErrRegistrationDuplicate`），并发回滚同样恰好 1 成功。

### 实现指针

- 治理核心类型与校验：`pkg/domain/factor_registration.go`
- 进程内账本（登记 / 回滚 / 版本解析）：`pkg/data/factor_governance.go`
- DB 侧持久化与审计：`pkg/storage/factor_registration.go`
- DDL：`pkg/storage/postgres.go` Migration 053（`factor_registrations` /
  `factor_registration_audit`）+ `docs/migrations/053_add_factor_registrations.sql`

---

## AI 因子挖掘流水线 + 治理层（D8）

> ADR-031 蓝图 §5.5 / §6 三期③。**产物是公式文本不是代码**——LLM/MCTS
> 只负责提出公式假设，公式带哈希入库、策略即用，全程零代码改动（「即挖即测」）。
> 挖掘算法本体不在流水线内：`Proposer`（提候选）/ `Evaluator`（在快照上测 IC）
> 是两个接口，LLM/MCTS/基因池是实现细节，躲在其后。

### 闭环流水线

```
Candidate（公式文本 + 来源留痕）
  → Candidate.Definition()（内核固定 formula，InputFields 由公式 identifier 派生）
  → 沙箱验证（解析 + L1 闸门 + 字段词表 + Validity + 有界求值）→ Attestation（绑定版本哈希）
  → 治理层（过拟合检测 + 样本外验证）
  → D4 登记（四样齐全，凭证必须绑定该版本）
  → 策略按名引用（Pipeline.Effective / EvaluateOnSnapshot）
```

单条候选任一环节不过**只拒收该条**并记录 `RejectReason`，不中断整轮——「挖一批、
坏的挡在外面、好的进库」是流水线的正常形态。

### 三条命根子（可复现 / 防偷看 / 可比）

| 命根子 | 结构性落法 | 钉住它的测试 |
|---|---|---|
| ① 可复现 | 版本哈希由定义内容派生（canonical JSON 的 SHA-256，同 D3）——改一个字符即变；同一挖掘输入重跑哈希一致 | `TestCandidate_VersionHash_LocksContent` / `TestPipeline_Run_Reproducible` |
| ② 防偷看 | 沙箱与治理求值只读**预加载快照**（`Snapshot`，只读适配器，无运行时查询入口）；快照缺字段一律 NaN（缺失≠0） | `TestSnapshot_MissingFieldIsNaN` |
| ③ 可比 | 同一公式 + 同一快照两次求值逐点相等（NaN 感知） | `TestEvaluateOnSnapshot_BilateralComparability` |

沙箱的隔离边界就是表达式 DSL 本身（无 IO、无循环、算子白名单）——不复用
`pkg/sandbox/runner`（那是跑 Go **代码**的进程隔离，公式不编译不执行任意代码）。
安全约束不放宽：闸门一条不少、`Lookback ≤ DefaultMaxEvalBars(4096)`、求值 panic 兜底。
挖掘产物是「已有因子名的新公式版本」（改公式=新哈希），**不动态新增因子名**
——名册仍是冻结协议（`ParseFactorType`）。

### 治理层三件

**① 过拟合检测**（`DetectOverfit`，多重检验校正）：

```
z   = |IC| × sqrt(期数)         该 IC 的 t 近似（H0: 真实 IC = 0）
raw = 2·(1 − Φ(z))             单次检验的 p 值
adj = 1 − (1 − raw)^NumTrials  族系误差（试 N 次至少中一次的概率）
```

`adj ≥ alpha(0.05)` ⇒ 判过拟合（拒收）。**`NumTrials` 必须含失败**——只统计成功率
等于把选择偏差藏起来（先例 `pkg/ai/validation` 的 `StatisticalInput.NumTrials` 教训）；
失败占比过半额外附因。输入不合法（期数/尝试次数非正、IC NaN/Inf）→ fail-closed 判过拟合
（「说不清就不放行」）。正例：弱 IC 试 500 次被标记；反例：强信号 IC=0.30×252 期×只试 1 次不误伤。

**② 样本外验证**（`ValidateOutOfSample`）：样本内外 IC **同号**且幅度保留比
`|Out|/|In| ≥ 0.5`（默认）才通过。fail-closed：样本内 IC 为 0/NaN 或样本外缺失（NaN）
一律判不通过——「没测」不等于「通过」。

**③ 审计留痕**（`FactorMiningRecord`，以 `(factor_name, version_hash)` 为键）：

<!-- drift:factor_mining_record -->
| 字段 | JSON | 含义 |
|---|---|---|
| FactorName | factor_name | 因子名（须在冻结名册内） |
| VersionHash | version_hash | 该定义版本的指纹（sha-256 hex，64 字符） |
| SourceKind | source_kind | 来源分类：ai_hypothesis / literature / supply_chain / ad_hoc（「有机制支撑」与「试出来的」可信度不同一个量级） |
| Generator | generator | 生成器标识（审计「谁生成」）。非空（纯空白不算） |
| Generation | generation | 第几代（挖掘迭代代数） |
| Rationale | rationale | 生成依据（可空） |
| NumTrials | num_trials | 本轮共试次数（**含失败**，过拟合校正的输入）。为正 |
| FailedTrials | failed_trials | 其中失败次数（≥0 且 ≤ NumTrials） |
| SandboxHash | sandbox_hash | 沙箱凭证哈希（= 该定义版本哈希，绑定内容）。**Admitted 时必须 == VersionHash** |
| Admitted | admitted | 是否登记入库 |
| RejectReason | reject_reason | 拒收原因。**未入库时必须非空** |
| OverfitRawP | overfit_raw_p | 过拟合检测原始 p 值 |
| OverfitAdjP | overfit_adj_p | 多重检验校正后 p 值 |
| OverfitFlag | overfit_flag | 是否被判过拟合 |
| OOSInSample | oos_in_sample | 样本内 IC |
| OOSOutSample | oos_out_sample | 样本外 IC |
| OOSPass | oos_pass | 样本外是否通过 |
| CreatedAt | created_at | 留痕创建时刻 |
<!-- /drift:factor_mining_record -->

校验在**两道关卡**共同守住（`FactorMiningRecord.Validate` + Migration 055 的 DB CHECK）：
生成器非空 / 来源枚举 / 尝试次数自洽 / **admitted ⇒ sandbox_hash==version_hash** /
被拒必须写明原因——连「绕过写入路径直接 INSERT 缺样」也被库拒绝（fail-closed）。

### 破坏验证（D8 ⑤3，实跑闭环：改坏→红→还原→绿→`git diff` 零残留）

1. 禁用 `Pipeline.admit` 的「凭证非空」闸门（跳过沙箱直接入库）→
   `TestPipeline_Admit_RejectsWhenSandboxNotRun` 红（`ErrSandboxNotRun`）。
2. `domain.FactorDefinition.VersionHash` 改为内容无关（去掉哈希锁版本）→
   `TestCandidate_VersionHash_LocksContent` 红。
3. 库侧兜底：`FactorMiningRecord{Admitted:true, SandboxHash:""}` / 凭证不绑定版本
   → `TestFactorMiningRecord_AdmittedRequiresSandboxAttestation` 红。

### 实现指针

- 流水线 / 候选 / 来源留痕：`pkg/mining/pipeline.go`、`pkg/mining/mining.go`
- 沙箱与只读求值：`pkg/mining/sandbox.go`（`Attestation`、`EvaluateOnSnapshot`）
- 只读预加载快照：`pkg/mining/snapshot.go`
- 治理层两件：`pkg/mining/governance.go`
- 留痕类型与校验：`pkg/domain/mining.go`
- DB 持久化：`pkg/storage/mining.go`
- DDL：`pkg/storage/postgres.go` Migration 055（`factor_mining_records`）+
  `docs/migrations/055_add_factor_mining_records.sql`

---

## Strategy Interface

> **Canonical definition** — matches [pkg/strategy/interfaces.go](../pkg/strategy/interfaces.go)
> (single source of truth, P1-24 ADR-020 §6 ODR-013 CQ-006)

The `Strategy` interface is a **composite** of 4 ISP single-responsibility
sub-interfaces. Concrete strategies implement the composite by embedding
`*strategy.BaseStrategy` (which provides `Configure`/`Cleanup`/`Parameters`
defaults) and adding `GenerateSignals`/`Weight`.

```go
// 4 个 single-responsibility 子接口
type StrategyCore interface {
    Name() string
    Description() string
}
type Configurable interface {
    Parameters() []Parameter
    Configure(params map[string]interface{}) error
}
type SignalGenerator interface {
    GenerateSignals(ctx context.Context,
        bars map[string][]domain.OHLCV,
        portfolio *domain.Portfolio) ([]domain.Signal, error)
    Weight(signal domain.Signal, portfolioValue float64) float64
}
type ResourceManaged interface {
    Cleanup()
}

// 复合接口 (向后兼容, 7 方法 surface 不变)
type Strategy interface {
    StrategyCore
    Configurable
    SignalGenerator
    ResourceManaged
}
```

**Sub-interface responsibilities**:

| Sub-interface | Required? | Default in `*BaseStrategy` | Purpose |
|---|---|---|---|
| `StrategyCore` | ✅ | yes | Identity (Name/Description) |
| `Configurable` | ⬜ optional | yes | Runtime parameters |
| `SignalGenerator` | ✅ | ❌ (no default) | Signal generation + position sizing |
| `ResourceManaged` | ⬜ optional | yes (no-op) | Resource release |

**Type-assertion helpers** (accept `any`, not `Strategy`):
- `strategy.AsConfigurable(s)` — runtime check + safe downcast
- `strategy.AsSignalGenerator(s)` — runtime check + safe downcast
- `strategy.AsResourceManaged(s)` — runtime check + safe downcast

> **P1-24 migration note**: External API unchanged. The composite
> `Strategy` interface still embeds 4 sub-interfaces whose union is
> the original 7 methods. All 30+ existing strategy implementations,
> tests, and plugin loaders work without modification.

### Hot-Swap Mechanism
- Strategies are loaded from YAML + Go plugin
- `StrategyLoader` interface allows runtime replacement
- Context cancellation triggers graceful strategy unload
- Version tracking for audit trail

---

## Streaming Strategy Interface — BarHandler（2026-10-08，目标形态）

> **Canonical definition（目标形态）** — 模块化内核新增，服务 L2/L3 有状态策略。与批式 `SignalGenerator`（§Strategy Interface）**并存**，引擎按策略实现的接口自动选执行模式。详细设计见 [design/kernel/target-architecture-modular-kernel.md](design/kernel/target-architecture-modular-kernel.md) §6。

```go
// 流式策略接口（新增）—— 服务 L2/L3 有状态策略
type BarHandler interface {
    // OnBar 逐 bar 回调，&mut self 语义（持有内部状态）
    // ⚠️ 2026-10-08 订正：此处原写 domain.Bar，但该类型并不存在
    //（pkg/domain 与 pkg/domain/market 均无；全仓零引用）——它是目标形态
    // 里的未建类型。K0 冻结实现用 domain.OHLCV（pkg/domain/market/types.go:6，
    // 本项目唯一的 bar 载体）。将来若真引入 domain.Bar（如给 bar 加复权
    // 标记）属**改冻结契约**，须走变更评审并同步 pkg/strategy/streaming.go
    // 及全部调用方。
    OnBar(ctx context.Context, bar domain.OHLCV) error
    // Warmup 声明需要多少根历史 bar 才能产出首个有效信号（静态推导）
    Warmup() int
    // SaveState / LoadState 状态持久化（断点续跑 / 回测-实盘迁移）
    SaveState() ([]byte, error)
    LoadState([]byte) error
    // Signals 取走自上次调用以来 OnBar 产生的全部信号（**取走即清空**）。
    // 引擎在每根 bar 喂完后调用并消费；重复调用不应拿到重复信号——
    // 防重复消费是接口属性（2026-10-08 K2 切片 2 新增的信号出口）。
    Signals() []Signal
}
```

**双模式与回测-实盘同构**：

| | 批式 `SignalGenerator` | 流式 `BarHandler` |
|---|---|---|
| 服务层 | L0 / L1（横截面） | L2 / L3（有状态） |
| 回测驱动 | VirtualClock 攒窗口 → `GenerateSignals` | VirtualClock 逐 bar → `OnBar` |
| 实盘驱动 | feed 攒窗口 → `GenerateSignals` | feed bar 到达 → `OnBar` |
| 状态 | 无（窗口纯函数 DAG） | 有，`SaveState`/`LoadState` 落 `quant.strategy_state` |

**同一份策略代码，回测与实盘只换三个实现**：Clock（Virtual/Live）、DataSource（PG 快照/推送 feed）、Broker（模拟撮合/真实券商）——策略本身一行不动。

---

## ExpressionStrategy (S7-P3-1)

> **Package**: `pkg/strategy/expression/`
> **Source**: [ADR-015](adr/adr-015-ai-agent-architecture.md) AI-Native Evolution

`ExpressionStrategy` lets a DSL expression run as a `strategy.Strategy`
inside the backtest engine. It composes four config-driven components
into the canonical Strategy interface:

| Component | File | Responsibility |
|-----------|------|----------------|
| `SignalGenerator` | `signal.go` | DSL comparison expression → `[]Signal` (truthy filter) |
| `PositionSizer` | `sizing.go` | Signals → target weights (`equal` / `strength_prop` / `fixed`) |
| `RiskController` | `risk.go` | Weights → risk-checked weights (per-position cap, max open, cash buffer) |
| `OHLCVDataProvider` | `data_provider.go` | `map[string][]domain.OHLCV` → `aiexpr.DataProvider` adapter |

### GenerateSignals Pipeline

```
bars → OHLCVDataProvider → aiexpr.Evaluator
                                ↓
        SignalGenerator.Generate(bars, evaluator) → raw signals
                                ↓
        PositionSizer.Size(signals, portfolioValue) → weights
                                ↓
        RiskController.Check(weights, portfolio) → filtered weights
                                ↓
        signals with Strength=weight, Metadata["raw_strength"]=DSL value
```

`Weight()` returns the precomputed `signal.Strength` — risk checks are
set-level (MaxOpenPositions, MinCashBuffer) and cannot be decomposed into
per-signal `Weight()` calls.

### Configuration Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `signal_expr` | string | `cs_rank(close) > 0.8` | DSL formula for signal generation |
| `action` | string | `buy` | Signal action: `buy` or `sell` |
| `direction` | string | `long` | Trade direction: `long`/`short`/`close` |
| `min_strength` | float | `0.0` | Minimum signal strength to emit |
| `sizing_method` | string | `equal` | `equal` / `strength_prop` / `fixed` |
| `fixed_weight` | float | `0.05` | Per-signal weight (fixed method) |
| `max_per_stock` | float | `0.10` | Max weight per single position |
| `max_total` | float | `1.0` | Max total exposure |
| `max_position_pct` | float | `0.10` | Risk: max weight per position |
| `max_open_positions` | int | `20` | Risk: max concurrent positions |
| `min_cash_buffer` | float | `0.05` | Risk: min cash buffer fraction |
| `lookback` | int | `60` | Evaluator lookback window (trading days) |

### AI Pipeline Integration (S7-P3-2)

The default `expression_template` strategy self-registers via `init()`.
For custom expression strategies, the AI pipeline emits a YAML config
that `pkg/ai/yaml.LoadStrategy` parses into an `ExpressionStrategy`,
bypassing the LLM Go-codegen + compile path entirely:

```
Intent → Generator → YAML → LoadStrategy → ExpressionStrategy → GlobalRegister → backtest
```

This closes the loop from natural-language strategy intent to executable
backtest without generating Go code (per ADR-015 §3 "AI as quant researcher").

#### YAML Schema

The `expression:` section of a strategy YAML maps 1:1 to
`ExpressionStrategyConfig`. Example:

```yaml
strategy:
  name: my_expr_strat
  type: expression
  description: top-decile by close-price rank
expression:
  signal:
    expression: "cs_rank(close) > 0.8"
    action: buy
    direction: long
    min_strength: 0
    lookback: 60
  sizing:
    method: equal           # equal | strength_prop | fixed
    fixed_weight: 0.05
    max_per_stock: 0.10
    max_total: 1.0
  risk:
    max_position_pct: 0.10
    max_open_positions: 20
    min_cash_buffer: 0.05
```

| Section | Field | Maps to | Default |
|---------|-------|---------|---------|
| `expression.signal` | `expression` | `SignalConfig.Expression` | (required if section present) |
| | `action` | `SignalConfig.Action` | `buy` |
| | `direction` | `SignalConfig.Direction` | `long` (long/short/close/hold) |
| | `min_strength` | `SignalConfig.MinStrength` | `0` |
| | `lookback` | `SignalConfig.Lookback` | `60` |
| `expression.sizing` | `method` | `SizingConfig.Method` | `equal` |
| | `fixed_weight` | `SizingConfig.FixedWeight` | `0.05` |
| | `max_per_stock` | `SizingConfig.MaxPerStock` | `0.10` |
| | `max_total` | `SizingConfig.MaxTotal` | `1.0` |
| `expression.risk` | `max_position_pct` | `RiskConfig.MaxPositionPct` | `0.10` |
| | `max_open_positions` | `RiskConfig.MaxOpenPositions` | `20` |
| | `min_cash_buffer` | `RiskConfig.MinCashBuffer` | `0.05` |

> **Note**: The top-level `risk:` section (max_positions/stop_loss/take_profit) is
> engine-level risk control applied during backtest execution. The
> `expression.risk:` section is post-signal weight risk control applied
> inside `ExpressionStrategy.GenerateSignals`. They are independent.

#### Loader API (`pkg/ai/yaml/loader.go`)

| Function | Purpose |
|----------|---------|
| `yaml.ParseConfig(yamlStr) (*Config, error)` | Parse YAML into Config struct (validates strategy + name) |
| `yaml.LoadStrategy(yamlStr) (strategy.Strategy, error)` | Parse + build ExpressionStrategy (not registered) |
| `yaml.LoadAndRegister(yamlStr) (strategy.Strategy, error)` | Load + GlobalRegister (convenience wrapper) |

**Detection logic**: `LoadStrategy` builds an ExpressionStrategy when
either (a) the `expression:` section is present with a non-empty
`signal.expression`, or (b) `strategy.type == "expression"`. In case
(b) with no expression section, package defaults are used
(`cs_rank(close) > 0.8`, equal sizing, 10% per stock, 20 positions).

**Reserved name**: `expression_template` is rejected by `LoadStrategy`
to avoid collision with the self-registered default.

#### Direct Execution via ExecuteFromYAML

`Pipeline.ExecuteFromYAML` runs a backtest directly from a YAML strategy
config, **bypassing the LLM Go-codegen + compile path entirely**. This is
the execution path for expression-type strategies once the AI has
emitted the YAML:

```
YAML → ParseConfig + LoadStrategy → registerOrConfigure → RunBacktest
```

| Method | Signature |
|--------|-----------|
| `Pipeline.ExecuteFromYAML` | `(ctx, yamlStr string, runner BacktestRunner) (*Result, error)` |

**Behavior**:

- **No codegen**: `Result.GeneratedCode` and `Result.BuildError` are left
  empty; `Result.YAMLConfig` is set to the input YAML.
- **Registration collision**: if a strategy with the same name is already
  registered and is also an `*expression.ExpressionStrategy`, the existing
  one is reconfigured in place via `Configure` (partial-update semantics).
  If the existing strategy is a different concrete type, an error is
  returned. If the name is not registered, `GlobalRegister` is called.
- **Universe / dates**: taken from the YAML `data.universe` and
  `backtest.start_date` / `backtest.end_date` fields. When absent, they
  fall back to `nil` (all stocks) and `2022-01-01` → `2024-01-01`
  (matching `Pipeline.runBacktest` defaults).
- **Nil runner**: when `runner` is nil, backtest is skipped and the
  result reaches `StageComplete` after registration — supports
  "load and register without running" callers.

This method is synchronous (like `Execute`). An async variant
`ExecuteFromYAMLAsync` may be added in a follow-up if needed.

### cs_neutralize Fix (S7-P3-1 Phase 1)

The `cs_neutralize(x, group)` cross-sectional operator was declared in
`IsCrossSectionalOp` and advertised in the LLM prompt as 2-arg, but the
parser enforced 1-arg arity and the evaluator had no implementation.
Fixed across all 4 layers: AST (`CrossSectionalNode.Group` field),
parser (2-arg special case), evaluator (group evaluation + signature
change), operators (`csNeutralize` + `globalDemean` helpers).

---

## Multi-Factor Strategy: value_momentum

### Factor Definitions

| Factor | Description | Threshold | Weight |
|--------|-------------|-----------|--------|
| value_pe | PE < 30th percentile of stock's historical PE | < Percentile(30) | 0.25 |
| value_pb | PB < 30th percentile of stock's historical PB | < Percentile(30) | 0.20 |
| momentum | 20-day momentum > 0 | > 0 | 0.30 |
| quality | ROE > 15% | > 15% | 0.25 |

### Filters
- Market cap: Top 80% by float market cap
- Status: Only "active" stocks
- Price: > 1 CNY (avoid penny stocks)
- Liquidity: Average daily turnover > 10M CNY

### Composite Score
```
composite_score = 0.25*z_value + 0.20*z_pb + 0.30*z_momentum + 0.25*z_quality
```

### Weight Calculation
- Base weight: `signal.Strength * composite_score`
- Volatility adjustment: Reduce by `1 / (1 + volatility)`
- Final weight capped at 5% per position

---

## Dynamic Risk Management

### Volatility Targeting
- Target portfolio volatility: 15% annualized
- Current volatility calculated from 20-day returns
- If `actual_vol > target_vol`: reduce position by `target_vol / actual_vol`
- If `actual_vol < target_vol * 0.8`: can increase position by `min(1.25, target_vol / actual_vol)`

### Market Regime Detection
```go
type MarketRegime struct {
    Trend      string  // "bull", "bear", "sideways"
    Volatility string  // "low", "medium", "high"
    Sentiment  float64 // -1.0 to 1.0
}
```

### Dynamic Stop-Loss
- Base stop-loss: 2x ATR (Average True Range)
- In high volatility regime: 2.5x ATR
- In low volatility regime: 1.5x ATR
- Trailing stop: Activated after 5% profit, trails at 3x ATR

### Position Sizing
```
position_size = min(
    portfolio_value * risk_fraction / stock_volatility,
    portfolio_value * 0.05,  // Max 5% per position
    portfolio_value * 0.5 * (1 - portfolio_beta)  // Beta-adjusted
)
```

---

## Data Layer

### Provider Interface

The system uses a unified `Provider` interface for all data sources, enabling seamless switching and fallback:

```go
type Provider interface {
    Name() string
    CheckConnectivity(ctx context.Context) error
    GetOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]domain.OHLCV, error)
    GetFundamental(ctx context.Context, symbol string, date time.Time) (*domain.Fundamental, error)
    GetStocks(ctx context.Context, exchange string) ([]domain.Stock, error)
    GetLatestPrice(ctx context.Context, symbol string) (float64, error)
    GetIndexConstituents(ctx context.Context, indexCode string) ([]string, error)
    GetTradingDays(ctx context.Context, start, end time.Time) ([]time.Time, error)
    GetStock(ctx context.Context, symbol string) (domain.Stock, error)
    BulkLoadOHLCV(ctx context.Context, symbols []string, start, end time.Time) (map[string][]domain.OHLCV, error)
    CheckCalendarExists(ctx context.Context, start, end time.Time) (bool, error)
}
```

### Provider Implementations

| Provider | Source | Use Case | File |
|----------|--------|----------|------|
| `PostgresProvider` | Local PostgreSQL/TimescaleDB | Primary data source (zero latency) | `pkg/marketdata/postgres_provider.go` |
| `HttpProvider` | Generic HTTP | Generic REST API adapter (e.g., L0 data service) | `pkg/marketdata/http_provider.go` |
| `InMemoryProvider` | In-memory | Testing and caching | `pkg/marketdata/inmemory_provider.go` |

> **External-source direct providers retired** ([ODR-058](archive/odr/odr-058-p5-1-retire-direct-providers.md)): the former `TushareProvider` / `AkShareProvider` (which called tushare.pro / AkShare directly) have been removed. Per [ADR-023](adr/adr-023-ai-experimenter-lab.md) (which carried this forward from ADR-022 §1), external data may only enter through the **L1** single ingest door (`POST /api/ingest/raw` → `ingest.raw`); read-only consumers go through `http` / `postgres`. The adapter factory now rejects `type: tushare` / `type: akshare` explicitly.

### DataAdapter (Three-Layer Architecture)

The `DataAdapter` implements a primary/fallback pattern with health checking:

```go
type DataAdapter struct {
    primary  Provider    // e.g., PostgresProvider
    fallback Provider    // e.g., HttpProvider (L0 data service)
    logger   zerolog.Logger
}
```

- **Primary**: Local PostgreSQL for fast, reliable data
- **Fallback**: A read-only L0-facing provider (e.g., `HttpProvider` → data service) when local data is missing
- **Auto-switching**: Health checks on every request, automatic fallback on failure

### CachedProvider (Redis Cache Decorator)

Wraps any Provider with Redis caching:

```go
type CachedProvider struct {
    inner  Provider
    redis  *redis.Client
    config CacheConfig
}
```

- Cache TTL: 1 hour for OHLCV, 24 hours for fundamentals
- Cache key format: `ohlcv:{symbol}:{start}:{end}`

### DataEventBus (Pub/Sub)

Event-driven data updates for real-time synchronization:

```go
type DataEventBus struct {
    mu        sync.RWMutex
    listeners map[EventType][]EventListener
}
```

Event types: `EventOHLCVUpdated`, `EventFundamentalUpdated`, `EventStockListUpdated`

### TimescaleDB Schema

```sql
-- Stocks table
CREATE TABLE stocks (
    symbol TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    exchange TEXT NOT NULL,
    market TEXT NOT NULL,
    sector TEXT,
    market_cap FLOAT,
    float_market_cap FLOAT,
    status TEXT DEFAULT 'active',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Daily OHLCV (hypertable)
CREATE TABLE ohlcv_daily (
    symbol TEXT,
    date TIMESTAMPTZ,
    open FLOAT,
    high FLOAT,
    low FLOAT,
    close FLOAT,
    volume FLOAT,
    turnover FLOAT,
    PRIMARY KEY (symbol, date)
);

SELECT create_hypertable('ohlcv_daily', 'date');

-- Fundamentals
-- 原独立的 fundamentals 表已在 EQD-P3-1（C-8）中并入 stock_fundamentals 并 DROP：
-- 两表 12 个指标列同名同义，读写路径统一为 stock_fundamentals（见 ODR-056 / migration 025）。

-- Factor cache for faster computation
CREATE TABLE factor_cache (
    symbol TEXT,
    date TIMESTAMPTZ,
    factor_name TEXT,
    factor_value FLOAT,
    PRIMARY KEY (symbol, date, factor_name)
);

SELECT create_hypertable('factor_cache', 'date');
```

### 模块化内核新表（2026-10-08，目标形态）

> 以下表为模块化内核的目标 schema，遵循单一事实源、禁止双写。**尚未实施**者见表（`quant.strategy_state` 结构可由 `EnsureSchema` 幂等兜底建，但未并入 `migrate()`、生产零调用，待另案）；`quant.external_signals` **已随 K8 实施**（2026-10-12，见下节），不再列入本表。

| 表 | 归属模块 | 用途 |
|---|---|---|
| `quant.portfolio_snapshot` | portfolio | 组合净值 / 持仓快照 |
| `quant.positions` | portfolio | 当前持仓 |
| `quant.risk_events` | risk-engine | 风控裁决记录 |
| `quant.orders` | exec-engine | 订单生命周期 |
| `quant.fills` | exec-engine | 成交回报 |
| `quant.recon_report` | exec-engine | 对账差异报告 |
| `quant.strategy_state` | strategy-runtime | L2/L3 流式策略状态（`SaveState`）——**尚未实施**（另案） |
| `audit.message_log` | eventstore | 模块间消息（先记录后分发，BusTap 语义） |

### 外部模型信号注入（K8，已实施 — 2026-10-12）

> 引擎取信号改经 `SignalProvider` 接口（`pkg/strategy/signal_provider.go`）；外部模型（进程外，任意 Python/ML 框架）不直接下单，产出信号经 HTTP 写入门注入 `quant.external_signals`，由 `SignalStrategy` 按 `as_of` 拉取消费。契约级防前视：`Signals(ctx, scope, asOf)` 只返回 `as_of <= asOf` 的信号——未来信号物理不可见是接口语义（`RejectFutureSignals` fail-loud，`DayOf` 日粒度为唯一判定口径），写入门对未来的 `as_of` 直接 400。

| Method | Path | 说明 |
|---|---|---|
| POST | `/api/signals` | 批量写入 `ExternalSignal`（RBAC `trader`/`admin`）；逐条 `ValidateExternalSignal` + 未来 as_of 拒绝，全批校验先于落库；`content_hash`（model_id/symbol/direction/strength/as_of 的 sha256）幂等去重，重复注入安全 |

`quant.external_signals` 表（Migration 048，`storage.ExternalSignalsSchemaDDL`，镜像 `docs/migrations/048`）：`content_hash` UNIQUE 既是幂等键也是证据坐标的 source 位；`factors` 为诊断快照（JSONB，不入哈希）。消费侧 `SignalStrategy.ListFor` 按 (model_id, symbol) + `as_of <= upTo` 拉取。

### 产业链静态数据层（P2-2，已实施 — quant schema 四表）

> ADR-023 数据层 L1「产业链图谱」的静态底座（TASKS.md §五 P2-2，2026-10-12）。取代手抄快照：四表统一带 `source`（来源标识）+ `as_of`（观察时点）溯源列，各有幂等唯一约束（同键重导刷新行，不插新行）。加表唯一执行路径为 `pkg/storage/postgres.go` 内联 `migrate()`（Migration 049，DDL 文本 `SupplyChainSchemaDDL`，镜像 `docs/migrations/049_add_supply_chain_tables.sql`）。

| 表 | 唯一键（幂等） | 用途 |
|---|---|---|
| `quant.sw_industry_tree` | `code` | 申万行业树（`parent_code` 一级行业为空串哨兵；`is_leaf` 由父子关系导入时推导） |
| `quant.supply_chain_edge` | `upstream+downstream+relation` | 组件→整机 / 上下游映射；`relation=industry_member` 为申万行业成分（index_member）——**当前成分快照，非 PIT**：仅 `out_date` 为空的段（当前在表成员）入表，历史出表段导入时过滤（P2-2-R2 裁决①） |
| `quant.industry_vitality` | `industry_code+metric+obs_date+source` | 行业景气指标（`obs_date` 为 DATE，PIT；唯一键含 source——不同来源各行其行） |
| `quant.etf_constituent` | `etf_code+symbol+obs_date` | ETF 成分（`obs_date` 为报告期，PIT；`weight` 为占净值比例百分数） |

**导入**（`pkg/data/supply_chain.go`，ETL 错误一律向上传播、幂等落库）：tushare `index_classify` → 行业树；`index_member` → 成分边（仅 `out_date` 为空的当前在表段，历史出表段过滤，见上表注）；`fund_portfolio` → ETF 成分；行业景气指标首版为 seed CSV 导入器（无现成 API，`source` 取自 CSV 列，人工维护）。

**只读查询 API**（`cmd/analysis/handlers_industry.go`，不设 RBAC；空结果为 200 + `[]`；非法参数 400 先于库访问；未接库 503）：

| Method | Path | 参数 | 说明 |
|---|---|---|---|
| GET | `/api/industry/tree` | `level`、`parent_code`（均可选） | 行业树节点，按 code 排序 |
| GET | `/api/industry/edges` | `upstream`/`downstream`/`relation`（至少一个） | 上下游/成分映射；无过滤全表扫描 400 |
| GET | `/api/industry/vitality` | `industry_code`（必填）、`metric`、`start_date`、`end_date`（可选） | 景气观测，按 (obs_date, metric) 排序 |
| GET | `/api/industry/etf/{etf_code}/constituents` | `obs_date`（可选） | ETF 成分；省略 obs_date 取最新报告期 |

契约详情（请求/响应 schema）以 `docs/openapi.yaml`（`/api/industry/*`，tag `Industry`）为准。

### tushare.pro API Adapter
- REST API with token authentication
- Rate limiting: 200 requests/minute
- Data normalization to unified format
- Local cache with TTL

---

## Tools API (S7-P3-3)

> 架构方向：本服务作为对外的工具提供方，外部 agent 通过 HTTP 调用工具。

### GET /api/tools

列出所有已注册工具及其 schema。

**响应**:
```json
{
  "tools": [
    {
      "name": "backtest.run",
      "description": "Run a backtest for a strategy",
      "parameters": [...],
      "output_schema": {...}
    }
  ],
  "count": 16
}
```

### GET /api/tools/:name

获取单个工具的 schema。

**响应**: `ToolInfo` 对象（name + description + parameters + output_schema）

**错误**:
- `404` `{"error": "...", "code": "TOOL_NOT_REGISTERED"}`

### POST /api/tools/:name

执行工具。

**请求体**:
```json
{
  "args": {
    "strategy_name": "momentum",
    "stock_pool": ["000001.SZ"],
    "start_date": "2022-01-01",
    "end_date": "2024-01-01"
  }
}
```

**响应**: `{"result": <任意 JSON 值>}`

**错误**:
- `404` `TOOL_NOT_REGISTERED` — 工具名未注册
- `400` `INVALID_ARGS` — 参数缺失或类型错误
- `400` `INVALID_BODY` — 请求体非合法 JSON
- `500` `TOOL_EXECUTION_FAILED` — 工具执行失败

### 内置工具

> S7-P3-3 注册了原始 8 个工具；Hermes Phase 1 (S7-P3-4) 新增 8 个工具；
> Hermes Phase 2 新增 2 个；EQD-P2-1 (2026-09-15, ODR-057) 新增 1 个
> （`research.profile`），当前共 19 个，分为 10 组。详见 `cmd/analysis/setup.go` `buildToolsRegistry()`。

#### 原始工具 (S7-P3-3, 8 个)

| 工具名 | 参数 | 输出 | 验证门禁 |
|--------|------|------|---------|
| `backtest.run` | strategy_name, stock_pool, start_date, end_date | BacktestResult | L3 |
| `factor.compute` | formula, symbols, start_date, end_date | map[string][]float64 | — |
| `factor.evaluate` | formula, symbols, start_date, end_date | FactorMetrics | — |
| `data.ohlcv` | symbol, start_date, end_date | []OHLCV | — |
| `data.stocks` | symbol? | []Stock | — |
| `data.fundamentals` | symbol, start_date?, end_date? | []Fundamental | — |
| `strategy.list` | (无) | []StrategyInfo | — |
| `strategy.get` | name | StrategyInfo | — |

#### Hermes Phase 1 新增工具 (S7-P3-4, 8 个)

| 工具名 | 参数 | 输出 | 验证门禁 |
|--------|------|------|---------|
| `validate_factor` | expression | {valid, inputs, ast, error, level, passed, reason, recommendation} | L1 (<1s) |
| `compute_factor_ic` | expression, symbols, start_date, end_date | computeFactorICResult {ic, ir, level, passed, reason, recommendation} | L2 (<10s) |
| `walk_forward_validate` | strategy_name, stock_pool, start_date, end_date, train_days?, test_days? | walkForwardSummary {..., level, passed, reason, recommendation} | L4 (<10min) |
| `list_factors` | category?, min_ic?, limit? | []FactorGene | — |
| `save_factor` | name, category, formula, description, rationale?, ic, turnover, sharpe? | {factor_id} | — |
| `list_strategies` | strategy_type?, min_fitness?, limit? | []StrategyGene | — |
| `save_strategy` | name, strategy_yaml, factor_ids?, sharpe, max_drawdown, total_return | {strategy_id} | — |
| `summarize_backtest` | result_json | backtestSummary {..., level, passed, reason, recommendation} | L3 |

#### Hermes Phase 2 新增工具

| 工具名 | 参数 | 输出 | 验证门禁 |
|--------|------|------|---------|
| `get_strategy_lineage` | strategy_id, max_depth? (default 5) | strategyLineageResult {root, depth_reached, max_depth, cycles_detected?, missing_parent_ids?} | — |
| `get_market_regime` | start_date, end_date, symbol? (default 000300.SH) | marketRegimeResult {symbol, start_date, end_date, bars_analyzed, trend, volatility, sentiment, as_of} | — |

> **GetStrategyLineageTool (Phase 2.2)**: 递归查询策略的系谱树（祖先链）。
> 输入 `strategy_id`，沿 `ParentIDs` 向上遍历，构造嵌套树形结构。
> 每个节点携带 id/name/strategy_type/factor_ids/parent_ids/sharpe/fitness/
> generation/status；丢弃重型字段（Code/Params/Description/timestamps）以保持
> LLM 友好的 JSON 大小（< 500 tokens）。
>
> 遍历使用标准 DFS 白/灰/黑三色算法区分两种情况：
> - **BLACK**（已完全展开的共享祖先）— 静默跳过，避免重复子树
> - **GRAY**（递归栈上的祖先，构成真环）— 记录到 `cycles_detected`
>
> 缺失父策略（Get 返回错误）记录到 `missing_parent_ids`，分支终止但兄弟继续展开。
> 同一缺失 ID 经多条路径到达时去重。
>
> `max_depth` 限制递归深度（默认 5，上限 10）。0 = 只返回根节点。
> 根节点本身找不到时返回错误（区别于"找到但树中有缺失"）。

> **GetMarketRegimeTool (Phase 2.3)**: 分析指定时间段的市场状态
> （牛市/熊市/震荡 + 波动率水平 + 情绪分数）。包装
> `risk.RiskManager.DetectRegime()`，输入 OHLCV 序列，输出
> `*domain.MarketRegime`。
>
> 工具内部完成 OHLCV 获取 → 类型转换 → 状态检测的完整流水线：
> 1. 通过 `DataSourceClient.FetchOHLCV` 从 data-service 获取原始 OHLCV 记录
> 2. `recordsToOHLCV` 将 `[]map[string]interface{}` 转换为 `[]domain.OHLCV`
>    （按日期升序排序，防御性处理多种日期格式）
> 3. 调用 `DetectRegime` 进行 MA 交叉 + 波动率分类
>
> 默认 symbol 为 `000300.SH`（沪深 300 指数，A 股市场基准）。
> `DetectRegime` 需要至少 `SlowMAPeriod`（通常 200）根 K 线，不足时返回错误。
> 结果扁平化为 `marketRegimeResult`（避免嵌套，LLM 友好）。

> **验证门禁架构**: L1 语法检查 → L2 快速 IC → L3 标准回测 → L4 Walk-Forward 过拟合检测。
>
> **GateDecision (Phase 2.1)**: 所有 L1-L4 门禁工具的返回值都嵌入统一的
> `GateDecision { level, passed, reason, recommendation }` 结构。
> - `level`: 门禁标识 ("L1"/"L2"/"L3"/"L4")
> - `passed`: 是否通过门禁阈值（机器可读）
> - `reason`: 失败原因代码（`passed`/`syntax_error`/`low_ic`/`low_sharpe`/
>   `excessive_drawdown`/`sharpe_gap_exceeded`/`low_oos_sharpe`）
> - `recommendation`: LLM 可读的中文建议
>
> Hermes 只需读取 `passed` 字段即可决定是否进入下一层，无需解析工具特定字段。
> 门禁阈值在 `pkg/tools/builtin/gate.go` 集中定义（`GateL2MinIC=0.02`、
> `GateL3MinSharpe=0.50`、`GateL3MaxDrawdown=-0.30`、`GateL4MaxSharpeGap=0.30`、
> `GateL4MinOOSSharpe=0.30`）。L4 的 `passed` 可能与引擎的 `overall_pass`
> 不同——前者使用更严格的门禁阈值，是 Hermes 决定是否保存到基因池的权威依据。
> 因子/策略必须按序通过 L1→L2→L3→L4 才能保存到基因池。
> 详见 Hermes Agent Integration System Design §6。
> ⚠️ 该设计文档原位于 `.trae/documents/`，已随目录删除而遗失（2026-09-16 CI 文档校验发现）。
> 现行行为以 `docs/hermes/` 下的配置与 prompts 为准；如需补回该设计文档，见 TASKS P2-11。

#### EQD-P2-1 新增工具 (2026-09-15, 1 个)

| 工具名 | 参数 | 输出 | 验证门禁 |
|--------|------|------|---------|
| `research.profile` | ticker (required), sections? | profileResult {ticker, name, schema_version, source, stale, stale_reason?, generated_at, last_researched, needs_review, review_reason?, conclusions[], questions[]} | — |

> **ResearchProfileTool (EQD-P2-1, 桥 B2, ODR-057)**: 读取某标的的研究档案
> （EquityDeep 侧的结论 + 疑点），实现于 `pkg/tools/builtin/research_tool.go`。
> 来源解析为「PG `research.*` 投影优先 → vault `_profile.json` 镜像回退 →
> 404 `NOT_FOUND`」；输出逐字对齐契约 C2（citations 原样透传，不做 LLM 二次加工），
> 并附 `source` / `stale` 字段供调用方判断来源与可信度。

#### Hermes Agent Configuration & Skill (Phase 2.4-2.5)

Hermes 端的配置和工作流定义存放在 `docs/hermes/` 下，Go 后端不读取这些文件
（per System Design §7 边界划分 — Hermes 是唯一工作流存储）。三组文件协同：

| 文件 | 角色 | 部署路径 | 关注点 |
|------|------|---------|--------|
| [docs/hermes/prompts/quant-research.md](hermes/prompts/quant-research.md) | System Prompt | `~/.hermes/prompts/quant-research.md` | 研究员角色 + 能力描述 + 约束规则 |
| [docs/hermes/skills/autonomous_factor_mining.md](hermes/skills/autonomous_factor_mining.md) | Skill 定义 | `~/.hermes/skills/autonomous_factor_mining.md` | 10 步自主挖掘循环 + 终止条件 |
| [docs/hermes/config/hermes.yaml](hermes/config/hermes.yaml) | Agent 配置 | `~/.hermes/config/hermes.yaml` | 模型 + 内存 + 预算 + 安全护栏 |
| [docs/hermes/tools-quant-backtest.yaml](hermes/tools-quant-backtest.yaml) | MCP 工具镜像 | `~/.hermes/tools/quant-backtest.yaml` | 19 个工具的静态 schema |

**autonomous_factor_mining Skill (Phase 2.4)** 定义 10 步循环：

1. `list_factors` + `get_strategy_lineage` + `get_market_regime` — 查询已有因子 + 系谱路径 + 市场状态
2. LLM 推理生成因子假设（DSL 表达式）
3. `validate_factor` — L1 语法门禁
4. `compute_factor_ic` — L2 快速 IC 门禁
5. 生成策略 YAML（根据 market regime 调整 risk 参数）
6. `backtest.run` + `summarize_backtest` — L3 标准回测
7. `walk_forward_validate` — L4 过拟合检测
8. `save_factor` + `save_strategy` — 沉淀到基因池
9. `get_strategy_lineage` 反思 — 为下一轮提供上下文
10. 检查终止条件（成功 / 预算耗尽 / 迭代上限 / 收敛停滞）

**Budget Controller (Phase 2.5)** 采用两层预算模型：

- **Layer 1 — Session-wide hard cap** (`hermes.yaml` `budget.*`):
  `max_cost_per_session=5.0` USD, `max_iterations=50`, `max_llm_calls=200`。
  Hermes 启动时读取，整个会话期间不可超过；即使 Skill 传入更大的值也会被截断。
- **Layer 2 — Per-skill-invocation soft default** (Skill 输入 `max_iterations`):
  默认 30，比 session cap 保守，留出 headroom 给其他 Skill。调用方可显式传入
  更小的值（如验收测试用 20）。

成本模型：`cost = (tokens_in + tokens_out) / 1000 * rate`，其中
Ollama 本地 `rate=0`，Together.ai hermes-3-8b `rate=$0.0002`，
Together.ai nous-hermes-3-70b `rate=$0.0006`（高质量可选模式）。

**安全护栏** (per System Design §附录A 风险登记):
- R1 过拟合幻觉 → 强制 L4 Walk-Forward (`require_l4_before_save: true`)
- R2 LLM 幻觉因子 → 强制 L1 语法门禁 (`require_l1_before_l2: true`)
- R3 成本爆炸 → 预算控制器 (`budget.max_cost_per_session`)
- R4 数据泄露 → 优先 Ollama 本地部署 (`prefer_local_model: true`)

**验收标准** (Phase 2): 给定 `{target_ic: 0.04, category: "momentum", budget: 2.0,
max_iterations: 20}`，Hermes 在 $2 预算内自主完成 L1-L4 验证并保存到基因池。
详见 [docs/hermes/e2e-acceptance-test.md](hermes/e2e-acceptance-test.md)。

#### Hermes Integration Tests (Phase 2.6-2.7)

| 测试层 | 文件 | 覆盖范围 |
|--------|------|---------|
| Go 单元测试 | `pkg/tools/builtin/*_test.go` | 每个工具的 Execute 方法（含 mock 依赖） |
| Go HTTP 集成测试 | `cmd/analysis/handlers_tools_integration_test.go` | 6 个测试：L1 门禁成功/失败 HTTP 往返、发现 schema 与执行一致性、save→list 往返、L1→save 链式、GateDecision 全字段 JSON 序列化 |
| Go 注册测试 | `cmd/analysis/setup_test.go` | 19 个工具全部注册、无重名 |
| Go HTTP plumbing | `cmd/analysis/handlers_tools_test.go` | 假工具测试 HTTP 层（状态码、错误分类、JSON 结构） |
| E2E 验收测试 | `docs/hermes/e2e-acceptance-test.md` | Hermes + Ollama + 全栈基础设施下的自主挖掘验收（手动执行） |

Go 集成测试 (Phase 2.7) 使用真实 builtin 工具 + mock 依赖，验证 L1 GateDecision
结构体通过 HTTP JSON 序列化往返后保持完整（level/passed/reason/recommendation 四
字段全部存在且类型正确）。这是 Hermes 自主循环的关键契约 — 如果任何字段缺失或类型
错误，自主循环会中断。

E2E 验收测试 (Phase 2.6) 需要 Hermes + Ollama + 全栈基础设施运行，包含 3 个冒烟
测试（工具发现、L1 门禁、市场状态）+ 完整自主循环 + 结果验证脚本。

---

## Microservices

### 1. Data Service (port 8081)
**Responsibilities**:
- Fetch data from multi-source adapters (Tushare, AkShare, Eastmoney, local Postgres)
- Normalize and store in TimescaleDB
- Serve data queries via HTTP API
- Expose adapter registry and fallback chains

**Endpoints** (registered in `cmd/data/main.go`):
```
GET  /health                       - Health check
GET  /stocks                       - List stocks (with filters)
GET  /stocks/:symbol               - Get single stock
GET  /stocks/count                 - Stock records + status split (DATA-02, 见下方响应形状)
GET  /market/index                 - Get market index data
     ?symbol=000001.SH&date=2024-01-01
GET  /ohlcv/:symbol                - Get OHLCV data for symbol
     ?start_date=2024-01-01
     &end_date=2024-12-31
POST /api/v1/ohlcv/bulk            - Bulk OHLCV query
GET  /fundamental/:symbol          - Get fundamental data
GET  /index/:code/constituents     - Get index constituent stocks
POST /sync/index-constituents/:index_code - Sync index constituents
GET  /api/v1/trading/calendar      - Get trading calendar
POST /sync/calendar                - Sync trading calendar
POST /sync/stocks                  - Trigger stock-list sync
POST /sync/ohlcv                   - Trigger OHLCV sync (specific symbols)
POST /sync/ohlcv/all               - Trigger OHLCV sync for all stocks
POST /sync/fundamental             - Trigger fundamental sync

# Adapter registry & fallback chain (added in ODR-011)
GET  /api/datasource/registry/status    - Snapshot of all adapters and chains
GET  /api/datasource/registry/health    - Run HealthCheck on every adapter
GET  /api/datasource/registry/chains    - List configured fallback chains
```

#### `GET /stocks/count` 的响应形状（DATA-02，2026-10-14）

```json
{"count": <总行数>, "by_status": {"<status>": <行数>, "…": 0}}
```

- `count` **含已退市**，且由各档**求和**导出（同一条 GROUP BY 的产物，不是第二次
  `COUNT(*)`）—— 所以「各档加起来不等于总数」这个中间态在这条端点上结构上不存在。
  把 `count` 读成"在市股票数"是错的；要在市数就取 `by_status.active`。
- `by_status` 逐值等于 `SELECT status, COUNT(*) FROM stocks GROUP BY status`。
- status 为 NULL 或空串的行折成 `"unknown"` 一档（该列可空：DDL 只有
  `DEFAULT 'active'`，无 NOT NULL）。这是一档**显式**读数，不是被静默丢弃的行。
- 票池为空时 `by_status` 序列化成 `{}` 而不是 `null`（前端类型按对象读，
  `null` 会被当成"字段不存在"而不是"票池是空的"）。
- `latest_date` 仍可能缺席 —— 那是 **DATA-03** 登记的形状问题，本单不动。
- 网关 `GET /api/stocks/count` 是**字段透传**（`proxyRequest` 解码成 map 再编码），
  所以这条形状不需要网关侧改动，也不需要在网关再登记一遍。

### 2. Strategy Service (port 8082)
**Responsibilities**:
- Load/unload strategies dynamically
- Execute strategy signals
- Hot-swap support

**Endpoints**:
```
GET  /health                      - Health check
GET  /strategies                  - List available strategies
GET  /strategies/:name            - Get strategy details
POST /strategies/load             - Load a strategy
     {"name": "value_momentum", "config": {...}}
POST /strategies/unload/:name     - Unload a strategy
POST /signals                     - Generate signals
     {"strategy": "value_momentum", "universe": ["000001.SZ", ...], "date": "2024-06-01"}
GET  /signals/:date               - Get signals for date
```

### 3. Risk API (in-process, /api/risk/* on :8085) ✅ *Implemented — P1-15 (ODR-021)*
> **Status**: Implemented as in-process `risk.RiskManager` in analysis-service.
> Merged from standalone risk-service(8083) per ODR-021 (P1-15, 2026-06-12).

**Responsibilities**:
- Calculate position sizes
- Monitor portfolio risk metrics
- Apply dynamic stop-losses
- Market regime detection

**Endpoints** (all on analysis-service :8085):
```
POST /api/risk/calculate_position   - Calculate position size
     {"portfolio_value": 1000000, "signal": {...}, "market_regime": {...}}
POST /api/risk/detect_regime        - Detect current market regime
POST /api/risk/check_stoploss       - Check stop-loss triggers
     {"positions": [...], "current_prices": {...}}
GET  /api/risk/metrics              - Get current risk metrics
     ?portfolio_value=1000000

# Legacy aliases (no /api/risk prefix, backward compat):
POST /calculate_position            - legacy alias for /api/risk/calculate_position
POST /detect_regime                 - legacy alias
POST /check_stoploss                - legacy alias
GET  /risk_metrics                  - legacy alias
```

> **Source**: `cmd/analysis/handlers_risk.go` — `RiskHandler.RegisterRoutes`

### 4. Execution API (in-process, /api/execution/* on :8085) ✅ *Implemented — P1-15 (ODR-021)*
> **Status**: LiveTrader interface defined, MockTrader implemented with A-share rules, AdvancedTrader with batch operations and quote streaming. Real broker integration planned for Phase 4. Merged from standalone execution-service(8084) per ODR-021 (P1-15, 2026-06-12).

**Responsibilities**:
- Order management (submit, cancel, query)
- Simulated order execution with A-share trading rules
- Position tracking with T+1 settlement
- Account summary and PnL tracking
- Batch order submission
- Quote subscription (mock streaming)

**LiveTrader Interface** (`pkg/live/trader.go`):
```go
type LiveTrader interface {
    SubmitOrder(ctx context.Context, symbol string, direction domain.Direction, orderType domain.OrderType, quantity float64, price float64) (*OrderResult, error)
    CancelOrder(ctx context.Context, orderID string) error
    GetOrder(ctx context.Context, orderID string) (*OrderResult, error)
    GetPositions(ctx context.Context) ([]PositionInfo, error)
    GetAccount(ctx context.Context) (*AccountInfo, error)
    Name() string
    HealthCheck(ctx context.Context) error
}
```

**A-Share Trading Rules** (enforced by MockTrader):
- T+1 settlement: shares bought today cannot be sold today
- Stamp tax: 0.1% on sell trades only
- Commission: 0.03% with minimum 5 CNY per trade
- Transfer fee: 0.001% of trade value
- Slippage: 0.01% applied to execution price

**Endpoints** (all on analysis-service :8085):
```
POST /api/execution/orders            - Create new order
     {"symbol": "000001.SZ", "quantity": 100, "side": "buy", "type": "market"}
GET  /api/execution/orders            - List orders
GET  /api/execution/orders/:id        - Get order status
POST /api/execution/orders/:id/cancel - Cancel order
GET  /api/execution/positions         - Get current positions
GET  /api/execution/account          - Get account summary

# Legacy aliases (root-level, backward compat):
POST /orders                          - legacy alias
GET  /orders                          - legacy alias
GET  /orders/:id                      - legacy alias
POST /orders/:id/cancel               - legacy alias
GET  /positions                       - legacy alias
GET  /account                        - legacy alias
```

> **Source**: `cmd/analysis/handlers_execution.go` — `ExecutionHandler.RegisterRoutes`

### 5. ~~Paper Trading Service~~ — 已删除（AUD-19, 2026-09-22）

**这一节描述的 `/api/paper/*` 端点从未挂载过**，已整条删除。保留标题是为了让
查过旧版文档的人能看到「它去哪了」，而不是以为文档漏了一节。

删它的依据（逐项核实过，不是按「零调用方」一笔带过）：

- `registerPaperTradingRoutes` **零调用方** —— 10 个端点从未挂到任何 router。
- 前端确实有一个「模拟交易」页面在调这 10 个路径，**但那个页面打开就是 404**：
  功能从未上线，不是「坏了」。该页面（`PaperTrading.vue`）与它的导航入口
  已随本次删除。
- 能力上 7/10 已被**活的** `/api/execution/*` 覆盖（见上一节），且那套带 RBAC：
  orders / orders/:id / orders/:id/cancel / positions 一一对应，
  `/api/execution/account` 对应 portfolio。剩下的 start / stop / status 是
  「模拟会话生命周期」，没有生产用途。
- 底层的 `SimulatedDataFeed` 文件头自己写着 `for testing` —— 它是测试脚手架，
  不是行情接入。即使挂载起来也没有真实数据源。
- 顺带关掉了两个缺陷：`LiveEngine.GetPortfolio()` 恒返回「初始资金 + 空持仓」
  （字段从构造后从未被写入，AUD-31），且无锁返回内部指针（AUD-23）。
  两者都随消费方消失而删除。

**仍然有效的部分**：下面这些是 `pkg/live` 的订单类型与校验规则。`LiveEngine` /
`OrderManager` 仍在仓库中（当前**无生产接线** —— `/api/execution/*` 走的是
`MockTrader`）。文档保留，免得下次需要时无从查起。

**Order types (P1-3 / ODR-016)** — `pkg/live/engine.go:tryFillOrder`:
- `market` — 立即按 Ask/Bid 成交
- `limit` — 限价单: `Buy: Ask <= LimitPrice` 时成交, `Sell: Bid >= LimitPrice` 时成交; 成交价为限价或更优
- `stop` — 止损单: `Buy: Ask >= StopPrice` 触发后转市价; `Sell: Bid <= StopPrice` 触发后转市价
- `trailing` — 跟踪止损: HWM 单调递增, 触发价 = `HWM - TrailOffset` (TrailAmount 优先于 TrailPercent); 触发后转市价

字段要求 (server-side validation in `OrderManager.SubmitOrder`):
- `limit`     → `LimitPrice > 0`
- `stop`      → `StopPrice > 0`
- `trailing`  → `TrailAmount > 0` 或 `TrailPercent > 0`

### 6. Analysis Service (port 8085)
**Responsibilities**:
- API Gateway (proxies to data-service and strategy-service)
- Backtesting engine
- Strategy CRUD management
- AI Copilot strategy generation
- Data source management
- Factor analysis (IC, quintile returns)
- Performance metrics calculation
- Report generation

**Endpoints**:

#### Health & Info
```
GET  /health                      - Health check
GET  /api/v1                      - API info and endpoint listing
```

#### Backtest
```
GET  /backtest?limit=20           - List recent backtest jobs
POST /backtest                    - Run backtest (sync or async)
     {
       "strategy": "value_momentum",
       "start_date": "2020-01-01",
       "end_date": "2024-12-31",
       "stock_pool": ["000001.SZ", ...],
       "initial_capital": 1000000,
       "commission_rate": 0.0003,
       "slippage_rate": 0.0001
     }
GET  /backtest/:id                - Get backtest job status/result (async)
GET  /backtest/:id/report         - Get backtest report (checks DB if not in memory)
GET  /backtest/:id/trades         - Get backtest trades (checks DB if not in memory)
GET  /backtest/:id/equity         - Get equity curve data (checks DB if not in memory)
```

#### Data Proxies (→ data-service :8081)

All of these are registered by `registerProxyRoutes`
(`cmd/analysis/handlers_proxy.go`). **The SPA reaches them through the `/api`
prefix only** — the no-prefix mirrors are gone (see "Removed" below).

```
GET  /api/ohlcv/:symbol           - Get OHLCV data for symbol
     ?start_date=2024-01-01&end_date=2024-12-31
POST /api/screen                  - Screen stocks by criteria
GET  /api/stocks/count            - Stock count + status split (passthrough;
                                    形状见 §1「`GET /stocks/count` 的响应形状」)
GET  /api/market/index            - Get market index data
     ?symbol=000001.SH&date=2024-01-01
GET  /api/factors/:factor_name    - Read a factor_cache row (citation 五元组，见 §AI 实验员实验室)
     ?symbol=&date=
ANY  /api/sync/*path              - Streaming reverse proxy to data-service
                                    `/api/sync/*` (job CRUD, workers, schedules).
                                    SSE at /api/sync/jobs/:id/progress is
                                    deliberately unbuffered.
```

**Removed — do not re-document these as live:**

- `GET /ohlcv/:symbol`, `POST /screen`, `GET /stocks/count`, `GET /market/index`
  — the 4 no-prefix mirrors, removed by **AUD-33 (2026-09-22)**. They existed
  only to serve the legacy HTML pages in `cmd/analysis/static/`, retired in the
  same change. `cmd/analysis/deps_test.go` asserts they stay unregistered.
- `POST /sync/calendar`, `POST /api/sync/calendar`, `GET /api/v1/trading/calendar`
  — removed as dead code in **ODR-062 (S-C)**. Note the `/api/sync/*path`
  wildcard does **not** resurrect `/api/sync/calendar`: data-service serves
  calendar sync only at the bare `/sync/calendar` (`cmd/data/main.go:112`),
  which is outside that prefix.

#### Data Synchronization (Phase 3 — gateway to data-service)

`/api/sync/*` on **this** service is a **streaming reverse proxy** to
data-service's `/api/sync/*` (`registerProxyRoutes`, ODR-062 S-B). The SPA talks
to these and never to `:8081` directly.

```
GET  /api/sync/jobs               - List sync jobs (supports ?status=&type= filters)
GET  /api/sync/jobs/:id           - Get sync job details
POST /api/sync/jobs               - Create and enqueue a sync job (typed door)
     {"type": "<JobType>", "params": {...}}
POST /api/sync/jobs/:id/cancel    - Cancel a running sync job
POST /api/sync/jobs/:id/retry     - Retry a failed sync job
GET  /api/sync/jobs/:id/progress  - SSE stream of job progress
GET  /api/sync/workers            - Worker stats
POST /api/sync/schedules          - Create a schedule
```

> **类型清单以代码为准**：`pkg/sync/types/types.go` 的 `JobType` 常量
> （`stocks` / `ohlcv` / `ohlcv_all` / `fundamental` / `fundamentals` /
> `dividends` / `splits` / `calendar` / `factor` / `factors` /
> `factor_attribution` / `factor_ic`）。`POST /api/sync/jobs` 是**类型化创建门**
> —— params 在门口校验，打错类型当场 400，而不是排一个注定失败的后台任务。
>
> ⚠️ **SSE 路径是 `GET /api/sync/jobs/:id/progress`**（按 job 订阅），
> **不是**旧文档里的 `/api/sync/stream`。反代必须 `proxy_buffering off`，
> 否则进度事件会被攒到最后一次性下发（前端表现：进度条一动不动，跑完才跳到 100%）。

#### Batch Backtest (Phase 3)
```
POST /api/batch                   - Run batch of backtests (multi-strategy × multi-period)
     {"runs": [{"strategy": "...", "start_date": "...", "end_date": "..."}, ...]}
GET  /api/batch/:batch_id         - Get aggregated batch report
GET  /api/batch/:batch_id/export/:format - Export batch report (json/csv)
```

#### Walk-Forward Analysis (Phase 3)
```
POST /api/walkforward             - Run walk-forward optimization
     {"strategy_id": "value_momentum", "in_sample_months": 12, "out_of_sample_months": 3}
GET  /api/walkforward             - List all walk-forward reports
GET  /api/walkforward/:strategy_id - Get walk-forward report for strategy
```

#### Data Source Management (Phase 3 — ODR-011 multi-source)
```
GET  /api/datasource/status       - Current data adapter status (primary, stopped, mode)
GET  /api/datasource/health       - Health check of all registered adapters
```
> 运行时切换门 `POST /api/datasource/switch` 已于 [ODR-059](archive/odr/odr-059-p5-1-retire-datasource-switch.md) 退役（任意 URL 冲突 [ADR-023](adr/adr-023-ai-experimenter-lab.md) 的单一数据面原则，承 ADR-022 §1；读源由启动期 `data_service.url` 固定）。

#### Factor Analysis
```
GET  /api/factor/returns/:factor  - Get factor returns time series
     ?start_date=2024-01-01&end_date=2024-12-31
GET  /api/factor/ic/:factor       - Get factor IC time series
POST /api/factor/compute-returns  - Compute factor returns for given universe
POST /api/factor/compute-ic       - Compute IC for factor × returns cross-section
GET  /api/factor/list             - List all available factors
```

#### Strategy Management
```
GET    /api/strategies            - List strategies (supports ?type=&active= filters)
POST   /api/strategies            - Create/update strategy config
GET    /api/strategies/:id        - Get strategy details
PUT    /api/strategies/:id        - Update strategy config
DELETE /api/strategies/:id        - Soft-delete strategy
```

#### Plugin Management (Phase 3 — Hot-Reload)
```
GET    /api/plugins                - List all loaded plugins
GET    /api/plugins/active         - List active plugins only
GET    /api/plugins/:name          - Get plugin details by name
POST   /api/plugins/load           - Load plugin from file path
       {"path": "/path/to/strategy.so"}
POST   /api/plugins/:name/unload   - Unload plugin by name
POST   /api/plugins/:name/reload   - Reload plugin by name
POST   /api/plugins/reload         - Reload plugin from file path
       {"path": "/path/to/strategy.so"}
POST   /api/plugins/load-all       - Load all .so files from watch directory
POST   /api/plugins/watch-dir      - Set plugin watch directory
       {"dir": "./plugins"}
GET    /api/plugins/watch-dir      - Get current watch directory
```

**Plugin System Architecture**:
- Plugins are compiled as Go shared libraries (`.so` files) using `-buildmode=plugin`
- Each plugin must export a `Strategy` symbol implementing `strategy.Strategy` interface
- The `PluginLoader` manages plugin lifecycle: Load → Active → Unload → Reload
- Directory watching auto-detects new/modified plugins and loads/reloads them
- Go plugin limitation: true unload requires process restart; we mark as unloaded in registry

**Build Plugin**:
```bash
cd pkg/strategy/plugins
go build -buildmode=plugin -o my_strategy.so ./my_strategy.go
```

#### AI Copilot (Legacy — Phase 3)
```
POST /api/copilot/generate        - Start AI strategy generation task
GET  /api/copilot/generate/:job_id - Poll generation task result
GET  /api/copilot/stats           - Get Copilot usage statistics
POST /api/copilot/save            - Save generated strategy code to file
```

#### AI Pipeline (Phase 4 — Intent-to-Strategy)
```
POST /api/pipeline/run            - Run full AI strategy generation pipeline
     {"description": "20日动量策略，在沪深300中选出最强10只股票"}
     Response: {
       "id": "uuid",
       "status": "complete|failed",
       "intent": { "strategy_type": "momentum", "parameters": [...] },
       "yaml_config": "...",
       "generated_code": "...",
       "build_error": "...",
       "duration_ms": 12345,
       "logs": ["..."]
     }
GET  /api/pipeline/jobs           - List all pipeline jobs
GET  /api/pipeline/jobs/:id       - Get specific pipeline job result
```

**Pipeline Stages**:
1. **Intent Parsing**: Extract structured parameters from natural language (Chinese/English)
2. **YAML Generation**: Generate strategy configuration from parsed intent
3. **Code Generation**: Use LLM to generate Go strategy code — **an artifact, not the execution target**
4. **Compilation Validation**: Verify the generated code compiles — the result is recorded, and **failure does not block the run**
5. **Backtest Execution**: Backtest the **YAML → `ExpressionStrategy`** signal, not the generated code

> ⚠️ 第 3~5 步的语义由 [ADR-024](adr/adr-024-expression-as-execution-target.md) 定：
> 回测跑的是**表达式引擎算出来的信号**，不是 LLM 写的 Go 代码；代码继续生成、
> 继续真编译校验，但**不加载、不执行**，失败也不让整个 pipeline 变 `failed`。
> 详见 §策略执行载体。`value` / `quality` 类意图给不出默认表达式 →
> **明确失败**，不套一个无关的价格表达式。

### ~~AI Research Service (port 8086)~~ — 已删除（2026-09-18，TASKS P2-5）

**这一节描述的 `:8086` 服务已删除**（零调用方，且建在废弃的交互层上；见
[ARCHITECTURE.md](ARCHITECTURE.md) 的服务表）。本文件此前仍把它标为
"✅ *Implemented — Phase 4*" 并列出约 100 行端点 —— 那是**服务删除时没清理的陈旧副本**。

它列的三类内容，实际归属是：

| 旧内容 | 现状 |
|---|---|
| Factor Research / Strategy Generation / Optimization / Evolution / Gene Pool | **端点全部不存在** —— `/api/factors/discover`、`/api/strategies/generate`、`/api/optimize`、`/api/evolution/*`、`/api/gene-pool/*` 全仓零注册。AI 生成策略的现行路径是上面的 **AI Pipeline**，执行载体见 §策略执行载体（ADR-024） |
| Data Source Management / Factor Analysis / Batch Backtest / Walk-Forward | 这些**活着**，但一直属于 **§6 Analysis Service (8085)**；此处那份是**路径写错的副本**（例如它写 `/api/batch/backtest`，实际是 `/api/batch`）。**以上面 §6 的版本为准** |
| Data Synchronization | 已上移到 §6 Analysis Service（`/api/sync/*` 网关） |

> **为什么留这一节而不是直接删干净**：删掉之后，查过旧版文档的人会以为
> 「文档漏了一节」，而不是「它被删了」。留一个带裁决的墓碑，比留一个空洞好。
> 同理见 §5 ~~Paper Trading Service~~。
>
> ⚠️ **节号 `6.` 已被 §6 Analysis Service 占用**，此处不再复用编号。

---

## Backtesting

### Backtest Engine Flow
1. **Initialization**: Load strategy, set date range, initialize portfolio
2. **Data Loading**: Load OHLCV and fundamental data for universe
3. **Daily Loop**:
   - Update market regime
   - Generate signals from strategy
   - Calculate positions via in-process risk.RiskManager (ODR-021)
   - Execute "orders" at close price
   - Update portfolio
4. **Daily Rebalance**: At month-end or threshold breach

### Live Trading Bridge

The backtest engine supports seamless transition from simulation to live/paper trading via the `LiveTrader` interface:

```go
// Attach a LiveTrader to the engine
trader := live.NewMockTrader(live.MockTraderConfig{InitialCash: 1e6}, logger)
engine.SetLiveTrader(trader)

// Execute a single signal through the live trader
result, err := engine.ExecuteSignalViaLiveTrader(ctx, signal, currentPrice)

// Execute multiple signals (daily rebalancing)
results := engine.ExecuteSignalsViaLiveTrader(ctx, signals, prices)

// Health check the trader
err := engine.HealthCheckLiveTrader(ctx)
```

**Engine Live Trading Methods**:
| Method | Purpose |
|--------|---------|
| `SetLiveTrader(trader)` | Attach/detach a LiveTrader |
| `GetLiveTrader()` | Get currently attached trader |
| `ExecuteSignalViaLiveTrader(ctx, signal, price)` | Execute one signal |
| `ExecuteSignalsViaLiveTrader(ctx, signals, prices)` | Batch execute signals |
| `HealthCheckLiveTrader(ctx)` | Verify trader connectivity |

> **CR-49 (ODR-012) verification** — re-grepped `pkg/backtest/engine.go`
> on 2026-06-10: all 5 methods above are present in
> `pkg/backtest/engine.go` with matching signatures (lines 1168, 1201,
> 1208, 1273, 1310). The interface definition in `pkg/live/trader.go`
> is the canonical source; this table mirrors it. Any future drift
> must be caught by `go doc pkg/backtest.Engine` diff in CI.

When a LiveTrader is attached, the engine can run in "paper trading" mode where signals are executed through the trader while still tracking performance internally. This enables:
- **Backtest → Paper Trading**: Same strategy code, different execution backend
- **Paper Trading → Live Trading**: Swap MockTrader for real broker implementation
- **Hybrid Mode**: Backtest with live execution for validation
5. **Finalization**: Calculate metrics, generate report

### RunSpec 内核路由（K10，2026-10-12）：EngineRunner 可插拔

一次回测运行请求可以声明由哪个内核执行（FR-1.1/FR-1.3，ADR-031 D1）：

- **路由键**：`RunSpec.kernel`（`pkg/backtest/contracts.BacktestRequest.Kernel`）。空值 =
  默认内核 `"go"`（`EffectiveKernel()` 语义，默认值**不**回填字段 —— 存量请求序列化不变）。
- **注册表**：`contracts.EngineRegistry`（`Register` / `Resolve`）是内核路由的唯一实现。
  注册仅发生在**装配期**（无热插拔）；`Resolve` 可并发。同步 handler（`POST /api/backtest`
  旧格式）、异步 job（`pkg/backtest/job`）、batch 编排统一经 `Resolve` 取 runner。
- **fail-closed（零静默回退）**：未注册/空名内核 → `Resolve` 返回
  `ErrKernelNotRegistered`，message 点名被拒内核与已注册集合。同步路径映射 400；异步 job
  终态 `failed` + `error_msg` 点名内核；两者都不得回退 go —— 防「以为在测 NT 其实在测 Go」。
- **接口边界**：注册表承载 `contracts.EngineRunner`
  （`RunBacktest(ctx, BacktestRequest) → *BacktestResponse`）。`runner.go` 的
  `BacktestRunner`（五参签名，返回 `*domain.BacktestResult`）是 AI 管线端口，**签名不动**，
  与内核路由无关。walk-forward 编排每窗口构造全新 Engine（引擎级缓存 per-instance，
  共享会跨窗口污染），不经注册表解析共享实例。
- **护栏**：`pkg/backtest/contracts/registry_test.go`（含「未知名回退 go → 红」的破坏腿）、
  `pkg/backtest/job/job_kernel_routing_test.go`、`cmd/analysis/handlers_backtest_kernel_test.go`。

### ResultSpec 冻结（K9，2026-10-12）：BacktestResponse canonical schema

回测结果的 **canonical schema**，四契约（ADR-031）里 ResultSpec 的落地形态，实现于
`pkg/backtest/contracts.BacktestResponse`。**字段集、指标口径、status 三值语义三者都是契约**：
任何变更必须先破坏护栏测试 `pkg/backtest/contracts/resultspec_test.go` 再走契约变更评审。
护栏含文档互锁 —— 改字段不改本节会直接红（本节前身列着根本不存在的 Profit factor /
Turnover，就是文档与实现静默漂移的标本）。

#### status 三值语义

`status` 字段本身是调用方的**唯一判据** —— 禁止用 `id` 是否存在、HTTP 200 之类间接信号推断成功。

| 值 | 含义 | 不变量 |
|---|---|---|
| `completed` | 跑完且有效（`contracts.CheckValidity` 无 reasons） | `invalid_reasons` 空、`error` 空 |
| `invalid` | 跑完但结果无效（OBS-01） | `invalid_reasons` **必填**、`error` 必空 |
| `failed` | 没跑完 | `error` **必填**、`invalid_reasons` 空 |

- `invalid_reasons` 的取值是稳定 Code：`empty_universe` / `zero_trades` /
  `zero_start_date` / `garbage_metric`（`contracts.CheckValidity`），
  以及 exec-algo 挂载类 `exec_algo_unknown_style`（信号指定的算法名不受支持，
  校验期拦下）/ `exec_algo_attach_failed`（挂载期失败：父单号重复 / Σ子单量不等 /
  计划非法）。无效是**结果属性**不是错误，所以不进 `error` —— `error` 只装真实
  错误（取不到日历、参数非法等）。
- `invalid` 的指标字段照常给出（如实呈现），但调用方**不得**把它当成功观测。
- 读路径同源：state → response 的唯一透传点是 `Engine.ResponseFromState`
  （`cmd/analysis.buildResponseFromState` 是其薄包装）。API 读路径与多策略对比
  （compare）都从它取 status / reasons，不允许第三处硬编码。

#### 字段集（27 项，冻结）

| JSON key | 类型 | 语义 |
|---|---|---|
| `id` | string | 回测 ID（UUID），恒在 |
| `status` | string | 三值之一（见上），恒在 |
| `strategy` | string | 策略名 |
| `strategy_git_hash` | string | 产出该结果的策略插件 git hash，可空 |
| `start_date` / `end_date` | string | 请求区间，`YYYY-MM-DD` |
| `total_return` | float | 区间总收益 = (V_end − V_start) / V_start |
| `annual_return` | float | 年化收益，见指标口径 |
| `sharpe_ratio` | float | 见指标口径 |
| `sortino_ratio` | float | 见指标口径 |
| `max_drawdown` | float | 峰谷最大回撤，**负值** |
| `max_drawdown_date` | string | 回撤最深日，`YYYY-MM-DD` |
| `win_rate` | float | 盈利平仓交易占比，0–1 |
| `total_trades` | int | 平仓交易总数 |
| `win_trades` / `lose_trades` | int | 盈利 / 亏损平仓数 |
| `avg_holding_days` | float | 平均持有天数 |
| `calmar_ratio` | float | = annual_return / \|max_drawdown\| |
| `started_at` / `completed_at` | string | RFC3339 |
| `error` | string | 仅 `failed` 有；真实错误 |
| `portfolio_values` | []PortfolioValue | 逐日净值序列 |
| `trades` | []Trade | 成交明细 |
| `stock_pool` | []string | 本轮票池 |
| `initial_capital` | float | 初始资金 |
| `invalid_reasons` | []string | 仅 `invalid` 有；稳定 Code 列表 |
| `universe_max_size` | int? | 票池规模峰值；`null` = 未评估（≠ 0） |

#### 指标口径（封闭系统日收益，冻结；实现 `pkg/backtest/metrics`）

- **日收益** r_t = (V_t − V_{t−1}) / V_{t−1}，V 为 `portfolio_values` 当日 TotalValue。
  **不做外部资金流修正** —— 回测是封闭系统，现金变动 100% 来自买卖。历史教训：
  申赎式 TWR 修正在每个交易日注入 ±成交额/总资产 的脉冲并污染 Sharpe 全链路（废止，
  见 `metrics.CalculateReturns` 注释）。
- **sharpe_ratio** = (mean(r) − rf/252) / std_sample(r) × √252。
- **sortino_ratio** = (mean(r) − rf/252) / downside_std × √252，下行只取 r < 0 的样本。
- **max_drawdown** = 峰谷法最大回撤，负值；`max_drawdown_date` 为最深日。
- **annual_return** = (1 + total_return)^(1/years) − 1，years 按净值序列**实际**首尾
  日历时间（365.25 天/年）。与 Sharpe 的 252 交易日年化并存且有意不同：前者按日历
  时间复利，后者按交易日波动。
- **calmar_ratio** = annual_return / |max_drawdown|（分母 0 → 0）。
- **win_rate / total_trades / win_trades / lose_trades / avg_holding_days**：按 symbol
  分组的平仓配对统计（加仓按均价合并，反手算一平一开）。

---

## Configuration

**本仓的配置就是 `config/` 下的三份 YAML。** 没有 `config/global.yaml`，也没有
把多份合成一份的加载器 —— 每个服务只读自己那一份。

| 文件 | 谁读 | 定位方式 | 默认端口 |
|---|---|---|---|
| `config/analysis-service.yaml` | `cmd/analysis` | `CONFIG_PATH` env（默认 `config/analysis-service.yaml`） | 8085 |
| `config/data-service.yaml` | `cmd/data` | viper `SetConfigName("data-service")`，搜索 `./config` `../config` `../../config` | 8081 |
| `config/strategy-service.yaml` | `cmd/strategy` | viper `SetConfigName("strategy-service")`，同上三个路径 | 8082 |

⚠️ **定位方式不统一是有意保留的历史差异，不是设计。** analysis 走 `CONFIG_PATH`
（便于容器里挂不同文件），另两个走 viper 的搜索路径。改启动逻辑时注意别把
`CONFIG_PATH` 当成三个服务的通用开关 —— 它只对 `cmd/analysis` 生效。

### 键名 → env 名

三个服务都接了 `viper.AutomaticEnv()` + `SetEnvKeyReplacer(".", "_")`，所以
**配置键里的 `.` 换成 `_` 就是 env 名**：`database.host` → `DATABASE_HOST`、
`server.gin_mode` → `SERVER_GIN_MODE`、`logging.level` → `LOGGING_LEVEL`、
`redis.url` → `REDIS_URL`。这是本仓**唯一**的重命名规则，不要再发明第二种 ——
AUD-35 的两个死键（`LOG_LEVEL` / `LOG_FORMAT`）就是照抄了并不存在的规则。

`tools/check_deploy_consistency.py` 的检查 6 会把「部署里注入的 env」与
「Go 源码里真的被读到的键」对账：注入一个没人读的 env 会报错。

### 主要配置段

| 段 | 键 | 读到哪 |
|---|---|---|
| `server` | `host` / `port` / `gin_mode` / `cors.allowed_origins` | `internal/httpserver` |
| `auth` | `jwt_secret`（env `JWT_SECRET` 或 `AUTH_JWT_SECRET`）/ `allow_insecure`（env `AUTH_INSECURE`）/ `insecure_exposure`（env `AUTH_INSECURE_EXPOSURE`，只认 `loopback-published`）/ `issuer` / `access_token_ttl` / `refresh_token_ttl` | `cmd/analysis` 的 `decideAuthStartup`（P0-4 启动门）+ `pkg/auth` |
| `rate_limit` | `per_minute`（env `RATE_LIMIT_PER_MINUTE`） | 网关限流中间件 |
| `database` | `url`（整串 DSN 覆盖点）/ `host` / `port` / `user` / `password` / `database` / `sslmode` | `pkg/storage.BuildDSN` |
| `redis` | `url` | `pkg/storage.NewCache` |
| `data_service` / `strategy_service` / `risk_service` | `url` | 服务间 HTTP 调用（`risk_service` 仅 analysis，legacy 回落） |
| `risk_manager` | `target_volatility` / `max_position_weight` / `min_position_weight` / `stoploss.*` / `take_profit.*` / `volatility.*` / `regime.*` | 回测引擎（进程内，无 HTTP 跳） |
| `alert` | `enabled` / `interval_sec` / `history_limit` / `recorder_capacity` / `max_position_weight` / `max_sector_weight` / `max_drawdown` / `daily_loss_limit` / `failure_rate_limit` / `webhook_url` / `webhook_timeout_sec` | `pkg/alert` |
| `backtest` | `initial_capital` / `commission_rate` / `slippage_rate` / `risk_free_rate` | 回测引擎 |
| `copilot` | `working_dir` | AI 沙箱（空 = 拒绝构建生成的策略，fail closed） |
| `trading` | `stamp_tax_rate` / `stamp_tax_rate_before` / `min_commission` / `transfer_fee_rate` / `price_limit.{normal,st,st_before,new}` / `new_stock_days` / `emergency_token` | 回测引擎（`UnmarshalKey("trading")`）+ `cmd/analysis` 实盘费率 |
| `logging` | `level` / `format` | zerolog 初始化 |
| `tushare`（仅 data） | `token`（空 = 未配置）/ `base_url` / `max_retries` | `pkg/data` |

**上表是导航，不是 schema —— 以文件和 Go 侧的读取点为准。** 加键前先 grep
有没有人读它；`pkg/backtest` 的 `TestTradingBlockInServiceConfigHasNoDeadKeys`
会把 `trading.*` 里没人读的键报出来。

### ⚠️ YAML 里不要写 `${...}`

本仓**没有** env 展开器（全仓无 `os.ExpandEnv` / `envsubst`）。写
`password: "${DATABASE_PASSWORD}"` 时，那串字符**就是密码本身**，会被交给驱动；
失败点在 TCP 连上之后，日志里只有 `password authentication failed` ——
读起来像「密码错了」，实际是「没填密码」（AUD-39）。

凭据类字段一律留**空**，由启动期校验要求 env 必填：

- `database.password` 空 → `pkg/storage.BuildDSN` 返回 `ErrEmptyDBPassword`，
  调用方 `Fatal` 并指向 `DATABASE_PASSWORD`（`cmd/analysis` / `cmd/data` 都是）。
- `tushare.token` 空 → **不 Fatal**，这是有意的：只读端点不依赖 token，只有
  sync 端点会在调用时失败并打明确 warn。
- `database.url` 非空且不含 `${` → 原样使用（嵌入方直接给整串 DSN 的逃生口）。

`tools/check_deploy_consistency.py` 的检查 7 钉住「`config/*.yaml` 里不得出现
`${`」；检查 5 钉住 compose ↔ k8s 的库名 / 用户名 / 密码变量不漂移。

### 策略 YAML

**`value_momentum` 不是一个 YAML 文件** —— 它是 Go 实现
`pkg/strategy/examples/value_momentum.go`。运行时真正加载的策略 YAML 由
`pkg/ai/yaml.Generator` 产出、由 `pkg/ai/yaml.LoadStrategy(yamlStr)` 加载，
schema 是 `pkg/ai/yaml.Config`：

```yaml
strategy:                 # → StrategyConfig
  name: "value_momentum"          # 必填，且不得是保留名 expression_template
  type: "momentum"                # 自由字符串，见下面的加载条件
  description: "…"
  indicators: ["pe", "pb"]        # 可选
  parameters: {}                  # map，自由键值

backtest:                 # → BacktestConfig
  start_date: "2020-01-01"
  end_date: "2024-01-01"
  initial_capital: 1000000
  commission_rate: 0.0003
  slippage_rate: 0.0001
  rebalance_frequency: "daily"

data:                     # → DataConfig
  universe: "hs300"
  timeframe: "1d"
  providers: ["postgres", "tushare"]   # 可选
  adjust_price: true

risk:                     # → RiskConfig（引擎级，可省）
  max_positions: 10
  max_drawdown: 0.15
  stop_loss: 0.08
  take_profit: 0.20
  position_sizing: "volatility_target"

execution:                # → ExecutionConfig（可省）
  order_type: "market"
  price_tolerance: 0.01

optimization:             # → OptimizationConfig（可省）
  enabled: false
  method: "grid_search"
  max_iterations: 100
  parameters: ["lookback"]

# ── 执行载体（ADR-024）──────────────────────────────────────────
# expression 段映射到 expression.ExpressionStrategyConfig。有它，
# LoadStrategy 才产出 ExpressionStrategy —— 这是「YAML → 确定性底座」
# 那条路的入口；LLM 生成的 Go 代码只是可审阅 artifact，不加载、不执行。
expression:
  signal:
    expression: "cs_rank(close) > 0.8"   # 有非空值即触发 ExpressionStrategy
    action: "buy"
    direction: "long"                   # 只认 long / short / close / hold
    min_strength: 0.5
    lookback: 20
  sizing:
    method: "equal_weight"
    fixed_weight: 0.05
    max_per_stock: 0.05
    max_total: 0.95
  risk:
    max_position_pct: 0.05
    max_drawdown: 0.15
    max_open_positions: 20
    min_cash_buffer: 0.05
```

**`LoadStrategy` 的加载条件**（三者之一，否则报错）：

1. `expression.signal.expression` 非空 → 用该段的值构建 `ExpressionStrategy`
   （省略的子字段由下游补默认值）；
2. 否则 `strategy.type == "expression"` → 用包级默认（`cs_rank(close) > 0.8`、
   equal sizing、单票 10%、20 个持仓、5% 现金缓冲）构建；
3. 否则 **返回错误** —— `LoadStrategy` 只支持 expression 型策略。

⚠️ **`expression.risk` 与顶层 `risk` 不是一回事。** 顶层 `risk` 是**引擎级**
止损止盈（`RiskConfig`，由回测引擎消费）；`expression.risk` 是
`ExpressionStrategy` 的**信号后权重**风控（`RiskYAML`）。两者同名不同层，
改一个不影响另一个。

`LoadStrategy` 只做「YAML → 对象」，**不注册**；要进全局 registry 用
`LoadAndRegister`。

---

## Error Handling

### Error Types
```go
type ErrorCode int
const (
    ErrCodeValidation ErrorCode = 1000 + iota
    ErrCodeNotFound
    ErrCodeDataNotAvailable
    ErrCodeStrategyError
    ErrCodeRiskLimitExceeded
    ErrCodeExecutionFailed
    ErrCodeInternal
)

type APIError struct {
    Code    ErrorCode
    Message string
    Details map[string]interface{}
}
```

### Logging
- Structured logging with zerolog
- Request ID propagation
- Log levels: debug, info, warn, error, fatal
- Sensitive data redaction

---

## Testing Strategy

1. **Unit Tests**: Core logic (factor calculation, signal generation, risk calculations)
2. **Integration Tests**: Service communication, database operations
3. **Backtesting Tests**: Strategy performance on historical data
4. **Mock Data**: Comprehensive mock datasets for reproducible testing

---

## Deployment

### Docker Compose (Development)
All services containerized with docker-compose for local development.

### Kubernetes (Production)
Each service as separate deployment with:
- Horizontal Pod Autoscaler
- PodDisruptionBudget
- Resource limits
- Health probes

---

## Future Enhancements
- [ ] Real broker integration (interactive brokers,证券)
- [ ] Options data and derivatives strategies
- [ ] Machine learning factor models
- [ ] Real-time data streaming (WebSocket)
- [ ] Multi-asset support (futures, options)
- [ ] Portfolio optimization (mean-variance, risk parity)
