# Quant Trading System - System Specification

> **Status**: Active (Canonical)
> **Version:** 1.5.0 (Unified Research Platform — ADR-022)
> **Last Updated:** 2026-09-15
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

## Unified Research Platform (ADR-022, Proposed)

> **Status**: Proposed — 顶层定义已落盘，实现待执行（阶段 P1~P5）。
> **Canonical 定义**: [PRODUCT.md](PRODUCT.md)（顶层产品）→ [ADR-022](adr/adr-022-unified-research-platform.md)（架构决策）。
> 本节仅摘录对 API/数据模型有约束力的部分；冲突时以 PRODUCT.md / ADR-022 为准。

### 四层架构（L0-L3，单向依赖）

```
L3 体验面   Obsidian Vault（工作面1） | Vue SPA（工作面2） | Hermes Agent（编排）
L2 编排面   Research Pipeline（纵向） | Research Engine（横截面） | MCP Tool Bridge
L1 计算面   因子引擎 | 回测引擎 | 验证门禁 L1-L5 | 风控·执行
L0 数据面   ingest.raw | market.* | quant.* | research.* | Evidence API   ← 唯一事实源
```

原则：L0 唯一数据面 + 单一写者 + 单向依赖（L3→L2→L1→L0）+ 可重建性标注 + 契约优先。

### 双对等工作面

| 工作面 | 形态 | 频次 | 产出 |
|---|---|---|---|
| 工作面 1 纵向深研（EquityDeep） | 1 股 × N 季度 | 季度 | 研究档案（vault markdown） |
| 工作面 2 横截面选股（原 Quant Lab 能力） | N 股 × 1 因子 | 日 | 交易信号 |

二者通过**飞轮闭环**互联：纵向深挖产假设 → 横截面回测验证 → 结果回流修正理解 → 异常触发深挖。

### 数据归属（A-E 分区，不重复存储）

| 类别 | 内容 | 唯一权威位置 | 其他侧副本 |
|---|---|---|---|
| A | 原始源响应 | PG `ingest.raw`（`content_hash` 唯一键） | ❌ 仅持 `content_hash` |
| B | 规范化数据（OHLCV / 财报 / 日历 / 公司行为） | PG `market.*` | ❌ 只读证据 API |
| C | 派生计算结果（因子 / 回测 / IC） | PG `quant.*` + Redis `factor_cache` | ❌ 只读计算 API |
| D | 研究叙事（人的判断） | **Vault markdown**（事实源） | — |
| E | 研究结构化状态 | PG `research.*`（D 的确定性投影，可 DROP 重建） | 权威在 D |

### Evidence API（已实现 — L0-3，见 [ODR-050](odr/odr-050-p1-base-contract-landing.md)）

证据坐标从"文件路径 + 模糊字符串"升级为**不可变内容坐标**：

```
citation = { source, dataset, key, as_of, content_hash }
```

**摄取入口**（已实现 — L0-1，见 [ODR-051](odr/odr-051-l0-1-single-ingest-entry.md)）：

| Method | Path | 说明 |
|---|---|---|
| POST | `/api/ingest/raw` | 外部生产者（akshare 侧 / 同步 executor）上报原始响应，落 `ingest.raw`；同 `content_hash` 幂等 |
| POST | `/api/ingest/equitydeep` | 归一化已归档的 EquityDeep 快照（ndjson，每行一条 `contracts/snapshot.schema.json` 记录）→ `fundamentals_detail`（类 B）；须先经 `/api/ingest/raw` 归档并携带其 `content_hash`（见 [ODR-055](odr/odr-055-eqd-p1-2-vertical-factors.md)） |
| GET | `/api/evidence/{content_hash}` | 返回该哈希对应的**唯一原始记录**（类 A），404 表示未摄取 |

约束：
- `content_hash` 由 L0 摄取时计算并作为 `ingest.raw` 唯一键，全平台跨工作面共享；
- `POST /api/ingest/raw` 是类 A 数据的**唯一写入口**，`source`/`dataset`/`key` 三者构成人类可读坐标，`content_hash` 为机器坐标；
- `POST /api/ingest/equitydeep` 是 EquityDeep 快照进入类 B（`fundamentals_detail`）的唯一门：它**拒绝** `ingest.raw` 不认识的 `content_hash`，并把该哈希盖在每一行 `snapshot_uri` 上 —— 没有可解析的原始响应在背后，任何数字都进不了 `fundamentals_detail`；
- 工作面 1 不得自建数据副本，运行期经本 API 只读取数；
- 该设计在架构层面消除 ODR-047 记录的 P0 假阳性缺陷（校验对象由"文本"变为"citation 元组 + JSON Pointer"）。

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

> **External-source direct providers retired** ([ODR-058](odr/odr-058-p5-1-retire-direct-providers.md)): the former `TushareProvider` / `AkShareProvider` (which called tushare.pro / AkShare directly) have been removed. Per [ADR-022](adr/adr-022-unified-research-platform.md) §1, external data may only enter through the L0 single ingest door (`POST /api/ingest/raw` → `ingest.raw`); read-only consumers go through `http` / `postgres`. The adapter factory now rejects `type: tushare` / `type: akshare` explicitly.

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
> 详见 [Hermes Agent Integration System Design](.trae/documents/hermes-agent-integration-system-design.md) §6。

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

### 5. Paper Trading Service (port 8085 /api/paper)
**Responsibilities**:
- Simulated trading with real-time market data
- Order lifecycle management (submit, cancel, query)
- Position tracking with T+1 settlement
- Portfolio PnL calculation
- Trade history recording

**Endpoints**:
```
POST /api/paper/start              - Start paper trading session
     {"symbols": ["000001.SZ", ...], "initial_capital": 1000000}
POST /api/paper/stop               - Stop paper trading session
GET  /api/paper/status             - Get session status
POST /api/paper/orders             - Submit new order
     {"symbol": "000001.SZ", "direction": "long", "quantity": 100, "order_type": "market"}
GET  /api/paper/orders             - List all orders
GET  /api/paper/orders/:id         - Get order by ID
DELETE /api/paper/orders/:id       - Cancel order
GET  /api/paper/positions          - Get current positions
GET  /api/paper/portfolio          - Get portfolio summary
GET  /api/paper/trades             - Get trade history
```

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
```
GET  /ohlcv/:symbol               - Get OHLCV data for symbol (legacy, no /api prefix)
     ?start_date=2024-01-01
     &end_date=2024-12-31
GET  /api/ohlcv/:symbol            - Same as above, with /api prefix (preferred)
POST /screen                      - Screen stocks by criteria (proxied, legacy)
POST /api/screen                  - Same as above, with /api prefix
GET  /stocks/count                - Get stock count (proxied, legacy)
GET  /api/stocks/count            - Same as above, with /api prefix
GET  /market/index                - Get market index data (proxied, legacy)
     ?symbol=000001.SH&date=2024-01-01
GET  /api/market/index            - Same as above, with /api prefix
POST /sync/calendar               - Sync trading calendar (proxied, legacy)
POST /api/sync/calendar           - Same as above, with /api prefix
GET  /api/v1/trading/calendar     - Get trading calendar (proxied)
```

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
POST /api/datasource/switch       - Switch primary data source
     {"source": "tushare|akshare|local|..."}
GET  /api/datasource/health       - Health check of all registered adapters
```

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
3. **Code Generation**: Use LLM to generate Go strategy code
4. **Compilation Validation**: Verify generated code compiles successfully
5. **Backtest Execution**: Run quick backtest to validate strategy performance

### 6. AI Research Service (port 8086) ✅ *Implemented — Phase 4*
> **Status**: Core implementation complete. Services: factor discovery, strategy generation, optimization, evolution, drift detection.
> **Design Principle**: AI acts as a senior quantitative researcher, using existing backtest infrastructure for validation.

**Responsibilities**:
- Factor discovery via LLM-driven hypothesis generation
- Strategy code generation from natural language
- Multi-layer validation (L1 syntax → L2 quick backtest → L3 standard → L4 Walk-Forward)
- AutoML parameter optimization (TPE + Genetic Algorithm)
- Strategy population evolution with drift detection
- Gene pool management (factor/strategy persistence)

**Endpoints**:

#### Health & Info
```
GET  /health                      - Health check
GET  /api/v1                      - API info and endpoint listing
```

#### Factor Research
```
POST /api/factors/discover        - Discover factors from natural language prompt
     {"prompt": "Find a factor for price-volume divergence", "universe": ["000001.SZ", ...]}
GET  /api/factors                 - List discovered factors
GET  /api/factors/:id             - Get factor details (expression, IC, performance)
POST /api/factors/:id/validate    - Run validation pipeline on factor
GET  /api/factors/:id/ic          - Get IC time series
DELETE /api/factors/:id           - Archive factor
```

#### Strategy Generation
```
POST /api/strategies/generate     - Generate strategy from factors
     {"factor_ids": ["f_001", "f_002"], "style": "multi_factor", "constraints": {...}}
GET  /api/strategies/generated    - List generated strategies
GET  /api/strategies/:id/code     - Get strategy source code
POST /api/strategies/:id/compile  - Validate compilation
POST /api/strategies/:id/backtest - Run quick backtest validation
```

#### Optimization
```
POST /api/optimize                - Run parameter optimization
     {"strategy_id": "s_001", "objectives": ["sharpe", "max_drawdown"], "method": "tpe"}
GET  /api/optimize/:job_id        - Get optimization progress/result
```

#### Evolution
```
GET  /api/evolution/population    - Get current strategy population
POST /api/evolution/evolve        - Trigger evolution cycle
GET  /api/evolution/drift         - Get drift detection status
GET  /api/evolution/genealogy/:id - Get strategy genealogy tree
```

#### Gene Pool
```
GET  /api/gene-pool/factors       - Browse factor gene pool
GET  /api/gene-pool/strategies    - Browse strategy gene pool
POST /api/gene-pool/archive       - Archive generation to gene pool
```

#### Data Source Management
```
GET  /api/datasource/status       - Get current data source status
POST /api/datasource/switch       - Switch active data source
GET  /api/datasource/health       - Check data source connectivity
```

#### Factor Analysis
```
GET  /api/factor/returns/:factor  - Get factor quintile returns time series
GET  /api/factor/ic/:factor       - Get factor IC time series
POST /api/factor/compute-returns  - Compute factor quintile returns for date
POST /api/factor/compute-ic       - Compute factor IC for date
GET  /api/factor/list             - List available factor types
```

#### Data Synchronization (Phase 3)
```
GET  /api/sync/status             - Get current sync status and active job
GET  /api/sync/jobs               - List sync jobs (supports ?status=&type= filters)
GET  /api/sync/jobs/:id           - Get sync job details
POST /api/sync/jobs               - Create and enqueue a new sync job
     {
       "type": "ohlcv|fundamental|stock_list|calendar",
       "params": {
         "symbols": ["000001.SZ", ...],
         "start_date": "2024-01-01",
         "end_date": "2024-12-31",
         "source": "tushare"
       }
     }
POST /api/sync/jobs/:id/cancel    - Cancel a running sync job
POST /api/sync/jobs/:id/retry     - Retry a failed sync job
GET  /api/sync/stream             - SSE endpoint for real-time progress updates
     Event: progress
     Data: {
       "job_id": "uuid",
       "type": "ohlcv",
       "status": "running",
       "progress": 45,
       "total": 100,
       "message": "Processing symbol 000001.SZ"
     }
```

#### Batch Backtest (Phase 3)
```
POST /api/batch/backtest          - Run batch backtest on multiple parameter sets
     {
       "strategy": "momentum",
       "base_config": {
         "start_date": "2020-01-01",
         "end_date": "2024-12-31",
         "initial_capital": 1000000
       },
       "parameter_sets": [
         {"lookback_days": 10, "top_n": 5},
         {"lookback_days": 20, "top_n": 10},
         {"lookback_days": 30, "top_n": 15}
       ],
       "stock_pool": ["000001.SZ", "000002.SZ", ...],
       "parallel": true,
       "max_workers": 4
     }
GET  /api/batch/backtest/:id      - Get batch backtest status and results
GET  /api/batch/backtest/:id/report - Get comprehensive comparison report
     Response: {
       "summary": {
         "total_runs": 9,
         "best_sharpe": 1.85,
         "best_config": {"lookback_days": 20, "top_n": 10},
         "avg_return": 0.15
       },
       "results": [...],
       "rankings": {
         "by_sharpe": [...],
         "by_return": [...],
         "by_max_drawdown": [...]
       }
     }
POST /api/batch/backtest/csv      - Upload CSV file with parameter sets
     Content-Type: multipart/form-data
     File: parameters.csv (columns: param1, param2, ...)
```

#### Walk-Forward Analysis (Phase 3)
```
POST /api/walkforward             - Run walk-forward optimization
     {
       "strategy": "momentum",
       "config": {
         "start_date": "2020-01-01",
         "end_date": "2024-12-31",
         "initial_capital": 1000000,
         "stock_pool": ["000001.SZ", ...]
       },
       "optimization_params": {
         "lookback_days": {"min": 5, "max": 60, "step": 5},
         "top_n": {"min": 3, "max": 20, "step": 2}
       },
       "window_config": {
         "train_days": 252,
         "test_days": 63,
         "step_days": 63
       }
     }
GET  /api/walkforward/:id         - Get walk-forward result
     Response: {
       "windows": [
         {
           "train_start": "2020-01-01",
           "train_end": "2020-12-31",
           "test_start": "2021-01-01",
           "test_end": "2021-03-31",
           "best_params": {"lookback_days": 20, "top_n": 10},
           "in_sample_sharpe": 2.1,
           "out_of_sample_sharpe": 1.6
         }
       ],
       "aggregate_metrics": {
         "avg_is_sharpe": 1.9,
         "avg_oos_sharpe": 1.5,
         "overfit_score": 0.21
       }
     }
```

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

### Global Config (config/global.yaml)
```yaml
app:
  name: "quant-trading"
  env: "development"  # development, production

database:
  host: "localhost"
  port: 5432
  user: "postgres"
  password: "${DB_PASSWORD}"
  name: "quant_trading"
  sslmode: "disable"
  max_connections: 20

redis:
  host: "localhost"
  port: 6379
  password: "${REDIS_PASSWORD}"
  db: 0

logging:
  level: "info"
  format: "json"
  output: "stdout"

tushare:
  token: "${TUSHARE_TOKEN}"
  base_url: "https://api.tushare.pro"

services:
  data:
    port: 8081
  strategy:
    port: 8082
  # ODR-021 (P1-15): risk + execution are in-process under analysis-service,
  # no separate service config / port needed.
  analysis:
    port: 8085
```

### Strategy Config (config/strategies/value_momentum.yaml)
```yaml
name: "value_momentum"
description: "Multi-factor strategy combining value, momentum, and quality factors"
version: "1.0.0"

factors:
  - name: "value_pe"
    enabled: true
    weight: 0.25
    params:
      percentile: 30
      direction: "lower_is_better"

  - name: "value_pb"
    enabled: true
    weight: 0.20
    params:
      percentile: 30
      direction: "lower_is_better"

  - name: "momentum"
    enabled: true
    weight: 0.30
    params:
      lookback: 20
      direction: "higher_is_better"

  - name: "quality_roe"
    enabled: true
    weight: 0.25
    params:
      threshold: 15.0
      direction: "higher_is_better"

filters:
  market_cap:
    enabled: true
    quantile: 80
  status:
    enabled: true
    values: ["active"]
  price:
    enabled: true
    min: 1.0
  liquidity:
    enabled: true
    min_turnover: 10000000

risk:
  max_position_pct: 0.05
  max_portfolio_beta: 0.5
  target_volatility: 0.15
  base_stop_loss_atr: 2.0
```

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
