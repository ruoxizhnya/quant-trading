# Quant Trading System - System Specification

> **Version:** 1.2.0 (Phase 2.5 Updated)
> **Last Updated:** 2026-04-08

## Overview

A production-grade quantitative trading system targeting A-share markets with market-agnostic core services. The system implements a microservices architecture with hot-swappable multi-factor strategies, dynamic risk management, and comprehensive backtesting capabilities.

**Phase 2.5 Changes:**
- Unified error handling: `pkg/errors` with structured error codes (ErrorCode, AppError)
- ATR StopLoss: Market regime-adaptive stop loss (bull/bear/sideways multipliers)
- Strategy interface finalized: Configure(), Weight(), GenerateSignals(), Cleanup()
- Signal type enhanced: Direction enum, Factors map, Metadata map
- Test coverage: 55+ unit tests across core packages

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
```go
type Signal struct {
    Symbol      string
    Date        time.Time
    Direction   Direction  // Long, Short, Close
    Strength    float64    // 0.0 - 1.0, confidence in signal
    Factors     map[string]float64  // Factor contributions
    Metadata    map[string]interface{}
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
        portfolio *domain.Portfolio) ([]Signal, error)
    Weight(signal Signal, portfolioValue float64) float64
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
- Fetch data from tushare.pro
- Normalize and store in TimescaleDB
- Serve data queries via HTTP API

**Endpoints**:
```
GET  /health              - Health check
GET  /stocks              - List stocks (with filters)
GET  /stocks/:symbol      - Get single stock
GET  /ohlcv               - Get OHLCV data
     ?symbol=000001.SZ
     &start=2024-01-01
     &end=2024-12-31
GET  /fundamentals        - Get fundamental data
     ?symbol=000001.SZ
     &date=2024-03-31
POST /sync                - Trigger data sync from tushare
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

### 4. Execution Service (port 8084) ⚠️ *Planned — Phase 3*
> **Status**: Interface defined (`pkg/live/trader.go`), mock implementation exists, real broker integration not started.

**Responsibilities**:
- Order management (stub for now)
- Simulated order execution
- Position tracking

**Endpoints**:
```
GET  /health                      - Health check
POST /orders                      - Create new order
     {"symbol": "000001.SZ", "quantity": 100, "side": "buy", "type": "market"}
GET  /orders                      - List orders
GET  /orders/:id                  - Get order status
POST /orders/:id/cancel            - Cancel order
GET  /positions                   - Get current positions
```

### 5. Analysis Service (port 8085)
**Responsibilities**:
- Backtesting engine
- Performance metrics calculation
- Report generation

**Endpoints**:
```
GET  /health                      - Health check
GET  /backtest?limit=20           - List recent backtest jobs
POST /backtest                    - Run backtest
     {
       "strategy": "value_momentum",
       "start_date": "2020-01-01",
       "end_date": "2024-12-31",
       "universe": "all",  // or ["000001.SZ", ...]
       "initial_capital": 1000000,
       "commission": 0.0003
     }
GET  /backtest/:id                - Get backtest job status (async)
GET  /backtest/:id/report         - Get backtest report (checks DB if not in memory)
GET  /backtest/:id/trades         - Get backtest trades (checks DB if not in memory)
GET  /backtest/:id/equity         - Get equity curve data (checks DB if not in memory)
POST /analyze                     - Analyze existing portfolio
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
