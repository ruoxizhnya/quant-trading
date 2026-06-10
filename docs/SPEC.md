# Quant Trading System - System Specification

> **Status**: Active (Canonical)
> **Version:** 1.4.2 (Phase 4 AI-Native + Documentation Sync)
> **Last Updated:** 2026-06-10
> **Owner:** 龙少 (Longshao) — AI Assistant
> **Related:** [VISION.md](VISION.md) (design), [ARCHITECTURE.md](ARCHITECTURE.md) (layout), [TEST.md](TEST.md) (quality)
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

## Core Domain Models

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

> **Canonical definition** — matches [pkg/strategy/strategy.go](../pkg/strategy/strategy.go)

```go
type Strategy interface {
    Name() string
    Description() string
    Parameters() []Parameter
    Configure(params map[string]interface{}) error
    GenerateSignals(ctx context.Context,
        bars map[string][]domain.OHLCV,
        portfolio *domain.Portfolio) ([]domain.Signal, error)
    Weight(signal domain.Signal, portfolioValue float64) float64
    Cleanup()
}
```

### Hot-Swap Mechanism
- Strategies are loaded from YAML + Go plugin
- `StrategyLoader` interface allows runtime replacement
- Context cancellation triggers graceful strategy unload
- Version tracking for audit trail

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
| `TushareProvider` | tushare.pro API | External API (200 req/min) | `pkg/marketdata/tushare_provider.go` |
| `AkShareProvider` | AkShare (Python) | Free alternative to Tushare | `pkg/marketdata/akshare_provider.go` |
| `HttpProvider` | Generic HTTP | Generic REST API adapter | `pkg/marketdata/http_provider.go` |
| `InMemoryProvider` | In-memory | Testing and caching | `pkg/marketdata/inmemory_provider.go` |

### DataAdapter (Three-Layer Architecture)

The `DataAdapter` implements a primary/fallback pattern with health checking:

```go
type DataAdapter struct {
    primary  Provider    // e.g., PostgresProvider
    fallback Provider    // e.g., TushareProvider
    logger   zerolog.Logger
}
```

- **Primary**: Local PostgreSQL for fast, reliable data
- **Fallback**: External API (Tushare/AkShare) when local data is missing
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
CREATE TABLE fundamentals (
    symbol TEXT,
    date TIMESTAMPTZ,
    pe FLOAT,
    pb FLOAT,
    ps FLOAT,
    roe FLOAT,
    roa FLOAT,
    debt_to_equity FLOAT,
    gross_margin FLOAT,
    net_margin FLOAT,
    revenue FLOAT,
    net_profit FLOAT,
    total_assets FLOAT,
    total_liab FLOAT,
    PRIMARY KEY (symbol, date)
);

SELECT create_hypertable('fundamentals', 'date');

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

### 3. Risk Service (port 8083) ⚠️ *Planned — Phase 3*
> **Status**: Not yet implemented. Endpoints below are design targets.

**Responsibilities**:
- Calculate position sizes
- Monitor portfolio risk metrics
- Apply dynamic stop-losses
- Market regime detection

**Endpoints**:
```
GET  /health                      - Health check
POST /calculate_position          - Calculate position size
     {"portfolio_value": 1000000, "signal": {...}, "market_regime": {...}}
GET  /risk_metrics                - Get current risk metrics
     ?portfolio_value=1000000
POST /stop_loss                   - Check stop-loss triggers
     {"positions": [...], "current_prices": {...}}
GET  /regime                      - Get current market regime
```

### 4. Execution Service (port 8084) ✅ *Implemented — Phase 3*
> **Status**: LiveTrader interface defined, MockTrader implemented with A-share rules, AdvancedTrader with batch operations and quote streaming. Real broker integration planned for Phase 4.

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

**Endpoints**:
```
GET  /health                      - Health check
POST /orders                      - Create new order
     {"symbol": "000001.SZ", "quantity": 100, "side": "buy", "type": "market"}
GET  /orders                      - List orders
GET  /orders/:id                  - Get order status
POST /orders/:id/cancel            - Cancel order
GET  /positions                   - Get current positions
GET  /account                     - Get account summary
```

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
   - Calculate positions via risk service
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
  risk:
    port: 8083
  execution:
    port: 8084
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
