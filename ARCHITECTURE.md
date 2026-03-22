# Quant Trading System - Architecture

## System Overview

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                              QUANT TRADING SYSTEM                                │
├─────────────────────────────────────────────────────────────────────────────────┤
│                                                                                  │
│  ┌──────────────────────────────────────────────────────────────────────────┐   │
│  │                         EXTERNAL DATA SOURCES                              │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐     │   │
│  │  │ tushare.pro │  │  Wind API    │  │  Bloomberg  │  │  Custom     │     │   │
│  │  │  (A-share)  │  │  (China)     │  │  (Global)   │  │  Feeds      │     │   │
│  │  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘     │   │
│  └─────────┼────────────────┼────────────────┼────────────────┼────────────┘   │
│            │                │                │                │                 │
│  ┌─────────▼────────────────▼────────────────▼────────────────▼────────────┐   │
│  │                           DATA SERVICE (Port 8081)                        │   │
│  │  ┌─────────────────────────────────────────────────────────────────┐      │   │
│  │  │  • Data Fetching & Normalization                                   │      │   │
│  │  │  • TimescaleDB Storage                                             │      │   │
│  │  │  • Data Validation & Quality Checks                                │      │   │
│  │  │  • Caching Layer (Redis)                                           │      │   │
│  │  └─────────────────────────────────────────────────────────────────┘      │   │
│  │                                      │                                      │   │
│  │                    ┌──────────────────┼──────────────────┐                   │   │
│  │                    │                  │                  │                   │   │
│  │                    ▼                  ▼                  ▼                   │   │
│  │              ┌──────────┐      ┌──────────┐      ┌──────────┐               │   │
│  │              │TimescaleDB│      │  Redis   │      │  Disk    │               │   │
│  │              │(OHLCV, Fnds│      │ (Cache)  │      │ (Backup) │               │   │
│  │              └──────────┘      └──────────┘      └──────────┘               │   │
│  └─────────────────────────────────────┬──────────────────────────────────────────┘   │
│                                        │                                              │
│  ┌─────────────────────────────────────▼──────────────────────────────────────────┐   │
│  │                                                                                  │   │
│  │    ┌─────────────────────────────────────────────────────────────────────┐     │   │
│  │    │                      STRATEGY SERVICE (Port 8082)                     │     │   │
│  │    │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐   │     │   │
│  │    │  │ value_      │  │ mean_       │  │ trend_      │  │ custom_     │   │     │   │
│  │    │  │ momentum    │  │ reversion   │  │ following   │  │ strategy    │   │     │   │
│  │    │  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘   │     │   │
│  │    │         │               │               │               │           │     │   │
│  │    │         └───────────────┴───────────────┴───────────────┘           │     │   │
│  │    │                               │                                       │     │   │
│  │    │                    ┌──────────▼──────────┐                           │     │   │
│  │    │                    │   Strategy Loader    │◄── Hot-Swap Interface     │     │   │
│  │    │                    │   (Plugin-based)     │                           │     │   │
│  │    │                    └──────────┬──────────┘                           │     │   │
│  │    │                               │                                       │     │   │
│  │    │                    ┌──────────▼──────────┐                           │     │   │
│  │    │                    │   Signal Generator │                           │     │   │
│  │    │                    │   (Multi-factor)   │                           │     │   │
│  │    │                    └─────────────────────┘                           │     │   │
│  │    └─────────────────────────────────────────────────────────────────────────┘     │   │
│  │                                      │                                             │   │
│  └──────────────────────────────────────┼─────────────────────────────────────────────────┘   │
│                                         │                                                   │
│  ┌──────────────────────────────────────┼─────────────────────────────────────────────────┐   │
│  │                           RISK SERVICE (Port 8083)                                    │   │
│  │                                                                                        │   │
│  │   ┌──────────────────────────────────────────────────────────────────────────────┐    │   │
│  │   │  ┌────────────────┐  ┌────────────────┐  ┌────────────────┐  ┌─────────────┐ │    │   │
│  │   │  │ Volatility    │  │ Position       │  │ Stop-Loss      │  │ Market      │ │    │   │
│  │   │  │ Targeting     │  │ Sizing         │  │ Manager        │  │ Regime      │ │    │   │
│  │   │  └───────┬────────┘  └───────┬────────┘  └───────┬────────┘  └──────┬──────┘ │    │   │
│  │   │          │                 │                  │                  │        │    │   │
│  │   │          └─────────────────┼──────────────────┴──────────────────┘        │    │   │
│  │   │                            │                                                  │    │   │
│  │   │                 ┌──────────▼──────────┐                                      │    │   │
│  │   │                 │   Risk Aggregator   │                                      │    │   │
│  │   │                 │   (Final Position)  │                                      │    │   │
│  │   │                 └─────────────────────┘                                      │    │   │
│  │   └───────────────────────────────────────────────────────────────────────────────┘    │   │
│  │                                      │                                               │   │
│  └──────────────────────────────────────┼───────────────────────────────────────────────────┘   │
│                                         │                                                       │
│  ┌──────────────────────────────────────┼───────────────────────────────────────────────────┐   │
│  │                         EXECUTION SERVICE (Port 8084)                                  │   │
│  │                                                                                          │   │
│  │   ┌────────────────┐  ┌────────────────┐  ┌────────────────┐                              │   │
│  │   │ Order Manager  │  │ Position       │  │ Execution     │                              │   │
│  │   │                │  │ Tracker        │  │ Simulator     │                              │   │
│  │   └────────────────┘  └────────────────┘  └────────────────┘                              │   │
│  │                                                                                          │   │
│  └────────────────────────────────────────────────────────────────────────────────────────────┘   │
│                                                                                                │
│  ┌────────────────────────────────────────────────────────────────────────────────────────────┐   │
│  │                         ANALYSIS SERVICE (Port 8085)                                     │   │
│  │                                                                                            │   │
│  │   ┌────────────────┐  ┌────────────────┐  ┌────────────────┐  ┌────────────────┐           │   │
│  │   │ Backtest       │  │ Performance   │  │ Report        │  │ Factor        │           │   │
│  │   │ Engine         │  │ Calculator    │  │ Generator     │  │ Analyzer      │           │   │
│  │   └────────────────┘  └────────────────┘  └────────────────┘  └────────────────┘           │   │
│  │                                                                                            │   │
│  └────────────────────────────────────────────────────────────────────────────────────────────┘   │
│                                                                                                │
└────────────────────────────────────────────────────────────────────────────────────────────┘
```

## Interface Definitions

### Core Domain Interfaces

```go
// MarketData is the primary interface for accessing market data
type MarketData interface {
    // GetOHLCV returns candlestick data for a symbol in a date range
    GetOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]OHLCV, error)
    
    // GetFundamental returns fundamental data for a symbol on a specific date
    GetFundamental(ctx context.Context, symbol string, date time.Time) (*Fundamental, error)
    
    // GetStocks returns stocks matching the given filter
    GetStocks(ctx context.Context, filter StockFilter) ([]Stock, error)
    
    // GetLatestPrice returns the most recent close price for a symbol
    GetLatestPrice(ctx context.Context, symbol string) (float64, error)
}

// StockFilter defines filtering criteria for stock queries
type StockFilter struct {
    Exchange   string
    Sector     string
    Status     string
    MinMCap    float64
    MaxMCap    float64
    Limit      int
}

// Strategy is the core interface for all trading strategies
type Strategy interface {
    // Name returns the unique name of the strategy
    Name() string
    
    // Description returns a human-readable description
    Description() string
    
    // Version returns the strategy version
    Version() string
    
    // Configure loads strategy-specific configuration
    Configure(config map[string]interface{}) error
    
    // Signals generates trading signals for the given universe and market data
    Signals(ctx context.Context, universe []Stock, data MarketData, date time.Time) ([]Signal, error)
    
    // Weight calculates the position weight for a signal
    Weight(signal Signal) float64
    
    // Cleanup releases any resources held by the strategy
    Cleanup()
}

// Signal represents a trading signal generated by a strategy
type Signal struct {
    Symbol      string
    Date        time.Time
    Direction   Direction    // Long, Short, Close
    Strength    float64      // Confidence: 0.0 - 1.0
    Factors     map[string]float64  // Factor contributions
    CompositeScore float64    // Overall composite factor score
    Metadata    map[string]interface{}
}

// RiskManager defines the interface for risk management
type RiskManager interface {
    // CalculatePosition calculates the optimal position size
    CalculatePosition(ctx context.Context, params PositionParams) (*PositionResult, error)
    
    // GetRiskMetrics calculates current portfolio risk metrics
    GetRiskMetrics(ctx context.Context, portfolio *Portfolio) (*RiskMetrics, error)
    
    // CheckStopLoss checks if any positions hit stop-loss thresholds
    CheckStopLoss(ctx context.Context, positions []Position, prices map[string]float64) ([]StopLossEvent, error)
    
    // DetectRegime detects the current market regime
    DetectRegime(ctx context.Context, data MarketData) (*MarketRegime, error)
}

// PositionParams contains parameters for position calculation
type PositionParams struct {
    PortfolioValue float64
    Signal         Signal
    MarketRegime   *MarketRegime
    CurrentVolatility float64
    StockVolatility float64
    PortfolioBeta  float64
}

// PositionResult contains the calculated position size
type PositionResult struct {
    Size       float64
    Weight     float64
    StopLoss   float64
    TakeProfit float64
    RiskScore  float64
}

// MarketRegime represents the current market regime
type MarketRegime struct {
    Trend      string    // "bull", "bear", "sideways"
    Volatility string    // "low", "medium", "high"
    Sentiment  float64   // -1.0 to 1.0
    Timestamp  time.Time
}

// RiskMetrics contains various risk measurements
type RiskMetrics struct {
    PortfolioVolatility float64
    PortfolioBeta       float64
    SharpeRatio         float64
    MaxDrawdown         float64
    VaR                 float64  // Value at Risk (95%)
    CVaR                float64  // Conditional VaR
}

// Order represents a trading order
type Order struct {
    ID        string
    Symbol    string
    Side      string    // "buy", "sell"
    Type      string    // "market", "limit", "stop"
    Quantity  float64
    Price     float64   // 0 for market orders
    FilledQty float64
    AvgPrice  float64
    Status    string    // "pending", "filled", "cancelled", "rejected"
    CreatedAt time.Time
    FilledAt  *time.Time
}

// Portfolio holds the current portfolio state
type Portfolio struct {
    Cash        float64
    Positions   map[string]Position
    TotalValue  float64
    DailyReturn float64
    UpdatedAt   time.Time
}

// Position represents a single position
type Position struct {
    Symbol        string
    Quantity      float64
    AvgCost       float64
    CurrentPrice  float64
    MarketValue   float64
    UnrealizedPnL float64
    RealizedPnL   float64
    Weight        float64
}
```

## Service Communication

### Synchronous (HTTP/gRPC)
- Strategy → Data: Fetch market data
- Risk → Data: Historical volatility
- Execution → Data: Price queries
- Analysis → Data: Historical backtest data

### Asynchronous (Message Queue - Future)
- Event-driven signals
- Real-time price updates
- Order fill notifications

## Data Flow

### Signal Generation Flow
```
1. Strategy Service receives signal request
2. Load strategy instance from registry
3. Fetch stock universe from Data Service
4. For each stock in universe:
   a. Fetch OHLCV data (20 days + today)
   b. Fetch fundamental data
   c. Calculate factor values
   d. Apply filters (market cap, status, etc.)
   e. Calculate composite score
   f. Generate Signal if score > threshold
5. Apply risk adjustments via Risk Service
6. Return ranked signals
```

### Backtest Flow
```
1. Analysis Service receives backtest request
2. Initialize backtest engine with parameters
3. Load strategy and historical data
4. For each trading day:
   a. Detect market regime
   b. Generate signals for universe
   c. Calculate positions via Risk Service
   d. Simulate execution at close price
   e. Update portfolio state
   f. Record daily metrics
5. Calculate final performance metrics
6. Generate report
```

## Deployment Architecture

### Development (Docker Compose)
```
┌─────────────────────────────────────────────────┐
│              Docker Network: quant-dev           │
├─────────────────────────────────────────────────┤
│  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐  │
│  │  Data   │ │Strategy │ │  Risk   │ │Analysis │  │
│  │ Service │ │ Service │ │ Service │ │ Service │  │
│  │  :8081  │ │  :8082  │ │  :8083  │ │  :8085  │  │
│  └────┬────┘ └────┬────┘ └────┬────┘ └────┬────┘  │
│       │           │           │           │        │
│  ┌────▼───────────▼───────────▼───────────▼────┐  │
│  │              Service Mesh / Reverse Proxy     │  │
│  └────────────────────────┬────────────────────┘  │
│                           │                      │
│  ┌────────────────────────▼────────────────────┐  │
│  │              PostgreSQL + TimescaleDB        │  │
│  └────────────────────────┬────────────────────┘  │
│                           │                      │
│  ┌────────────────────────▼────────────────────┐  │
│  │                   Redis                     │  │
│  └─────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────┘
```

### Production (Kubernetes)
```
┌─────────────────────────────────────────────────────────────────────┐
│                        Kubernetes Cluster                           │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐               │
│  │  Namespace   │  │  Namespace    │  │  Namespace    │               │
│  │  data-svc    │  │  strategy-svc │  │  risk-svc    │               │
│  ├──────────────┤  ├──────────────┤  ├──────────────┤               │
│  │ Deployment   │  │ Deployment   │  │ Deployment   │               │
│  │ HPA: 1-5     │  │ HPA: 1-3     │  │ HPA: 1-2     │               │
│  │ Resources    │  │ Resources    │  │ Resources    │               │
│  └──────────────┘  └──────────────┘  └──────────────┘               │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │                     Data Layer                               │   │
│  │  ┌─────────────────┐  ┌─────────────────┐                     │   │
│  │  │ TimescaleDB     │  │ Redis Cluster   │                     │   │
│  │  │ (StatefulSet)   │  │ (Deployment)     │                     │   │
│  │  │ Volume: 100Gi   │  │ Replicas: 3      │                     │   │
│  │  └─────────────────┘  └─────────────────┘                     │   │
│  └──────────────────────────────────────────────────────────────┘   │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

## Technology Stack

| Component | Technology | Purpose |
|-----------|------------|---------|
| Language | Go 1.21+ | Primary language for all services |
| Web Framework | Gin | HTTP API server |
| Database | PostgreSQL + TimescaleDB | Time-series storage |
| Cache | Redis | Data caching, session storage |
| Logging | zerolog | Structured logging |
| Tracing | OpenTelemetry | Distributed tracing |
| Metrics | Prometheus | Metrics collection |
| Container | Docker | Containerization |
| Orchestration | Kubernetes | Production deployment |
| Config | Viper | Configuration management |

## Security

### Network Policies
- Services communicate only with required dependencies
- Database accessible only from data service
- External API access only from data service

### Secrets Management
- All secrets via Kubernetes Secrets or Vault
- Environment variable substitution in configs
- No hardcoded credentials

### Data Protection
- Sensitive fields encrypted at rest
- API authentication via JWT
- Rate limiting on public endpoints
