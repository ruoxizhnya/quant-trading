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
```go
type OHLCV struct {
    Symbol   string
    Date     time.Time
    Open     float64
    High     float64
    Low      float64
    Close    float64
    Volume   float64
    Turnover float64 // in CNY
}
```

### Fundamental Data
```go
type Fundamental struct {
    Symbol       string
    Date         time.Time
    PE           float64  // Price-to-Earnings
    PB           float64  // Price-to-Book
    PS           float64  // Price-to-Sales
    ROE          float64  // Return on Equity (%)
    ROA          float64  // Return on Assets (%)
    DebtToEquity float64  // Debt to Equity ratio
    GrossMargin  float64  // Gross margin (%)
    NetMargin    float64  // Net profit margin (%)
    Revenue      float64  // Total revenue
    NetProfit    float64  // Net profit
    TotalAssets  float64
    TotalLiab    float64
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
GET  /stocks/count                 - Get total stock count
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
GET  /api/stocks/count            - Get stock count
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

### Backtest Report Metrics
- Total return (annualized)
- Sharpe ratio
- Max drawdown
- Calmar ratio
- Win rate
- Profit factor
- Number of trades
- Average holding period
- Turnover

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
