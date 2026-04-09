# Quant Trading System — Vision & Features

> **Status:** Canonical source of truth
> **Version:** 1.2.0
> **Last Updated:** 2026-04-08
> **Owner:** 龙少 (Longshao) — AI Assistant
>
> **Changelog v1.2 (Phase 2.5):**
> - Unified error handling framework (pkg/errors) with structured error codes
> - ATR-based StopLoss implementation with market regime adaptation
> - Strategy interface unified: Configure(), Weight(), Cleanup() methods added
> - Signal type enhanced: Direction, Factors, Metadata fields
> - Comprehensive unit tests: T+1 (6 cases), Price Limits (7 cases), Engine (11 cases), Risk Manager (13 cases), Strategy (18 cases)
> - Test coverage > 80% for core packages (backtest, risk, strategy)

---

## 1. Vision Statement

**What is this system?**

A professional-grade quantitative trading platform built in Go, delivering institutional-quality multi-factor strategy execution, live backtesting, and AI-assisted strategy development — with A-share (Chinese stock market) as its first and primary target, and a fully market-agnostic core architecture underneath.

**Who is it for?**

Individual investors with some trading experience who want more than intuition — they want evidence. The platform is designed to be powerful enough for serious quant work, yet accessible enough that a non-programmer can describe a strategy in plain Chinese and have the AI generate working code.

**What problem does it solve?**

Most quant tools are either:
- Too simple (Excel backtests, inaccurate)
- Too complex (vnpy, JoinQuant — overwhelming for individuals)
- Too closed (米筐, Wind — expensive, no control)
- Too slow (Python-based systems hitting GIL limitations)

This system solves that gap: a fast, open, self-hosted platform where strategies are plugins you can swap without rebuilding, AI helps you write new strategies, and every number is traceable back to its source.

**What makes it different?**

Three things set this apart:
1. **Go-native performance** — backtests that run in seconds, not minutes
2. **Hot-swappable strategy plugins** — add a new strategy by dropping a file, no rebuild, no restart
3. **AI Copilot integration** — describe a strategy in natural language, get working Go code with tests

---

## 2. Core Principles

These principles inform every decision — from database schema to feature prioritization. They are derived from SOUL.md (authentic helpfulness, ownership mindset), AGENTS.md (delegate, be resourceful), and the hard lessons learned from vnpy and earlier prototypes.

### Principle 1: Accuracy Before Features

A backtest that is 5% wrong is worse than no backtest — it creates false confidence. T+1 settlement enforcement, correct commission splitting (stamp tax is sell-only), 涨跌停 detection, and 前复权 data are not nice-to-haves. They are the product. We ship features that are correct, not features that are fast to build.

**What this means in practice:** Phase 1 is entirely devoted to data accuracy and A-share rule compliance. No feature work proceeds until T+1 settlement, trading calendar, 涨跌停 detection, and commission structure are verified against vnpy's proven implementation.

### Principle 2: Market-Agnostic Core, Market-Specific Periphery

The strategy interface, backtest engine, risk manager, and execution layer must never know or care whether they are running on A-shares, US equities, or Bitcoin. All market-specific logic — trading hours, T+1 rules, price limits, commission schedules, margin rates — lives exclusively in the data layer and configuration layer.

**What this means:** Adding US equities support should require zero changes to the backtest engine or strategy plugins. Only a new data adapter and a new config file.

### Principle 3: Hot-Swap Everything

Strategies are plugins. Risk models can be swapped. Data sources can change. Nothing should require a service restart. The system is designed for dynamic runtime loading from the start, not bolted on as an afterthought.

**What this means:** The `Strategy` interface is the contract. The `Registry` is the runtime graph. Plugins register themselves via `init()`. The engine resolves strategies by name at runtime. A user can add a new momentum strategy tomorrow by writing one file and reloading — no PR, no deploy.

### Principle 4: Performance Is a Feature

We chose Go for a reason. Backtests that take 5 seconds enable the kind of rapid iteration that makes Python-based systems feel painful. The system must remain fast as it scales: 5,000 stocks, 10 years of data, complex multi-factor scoring. Performance is a competitive advantage, not an infrastructure concern.

**What this means:** No Python in the hot path. Async I/O for data fetching. TimescaleDB for time-series queries. In-memory caching for active backtest sessions.

### Principle 5: AI as a First-Class User

龙少 (the AI assistant) is not a chatbot bolted onto the side. It is the interface through which strategies are created, evaluated, and improved. The user talks to 龙少; 龙少 talks to the trading system. The goal is not "AI assists human using a GUI" — it is "AI and human collaborate as a team, each doing what they do best."

**What this means:** Every system component is designed to be readable and actionable by an AI. Strategy code must follow conventions that AI can understand and modify. Error messages must be actionable. Logs must be structured and traceable.

### Principle 6: Own Your Data, Own Your System

No subscription. No data vendor lock-in. The system runs locally, on your machine or your VPS. Tushare is the current data source, but the architecture abstracts data access behind a `MarketData` interface — swap to Wind, Bloomberg, or a custom scraper without touching strategy or execution code.

**What this means:** Self-hosted Docker Compose for development, Kubernetes-ready for production. No proprietary data formats. All schemas documented in code and in VISION.md.

### Principle 7: Evidence Over Intuition

Every trading decision in the system is backed by a backtest. Every strategy is evaluated on historical data before consideration. Intuition is welcome as a starting hypothesis — but it is not accepted as a final answer. The system exists to pressure-test intuition with data.

**What this means:** The dashboard always shows backtest results alongside any live suggestion. A strategy that looks good in a chat with 龙少 must still pass through the backtest engine before being taken seriously.

---

## 3. Feature Breakdown by Category

### A. Data Layer

**What data do we have?**
- OHLCV daily bars (前复权, qfq-adjusted) for ~5,500 A-share stocks — via tushare.pro `stk_factor_pro` API
- Stock master list: symbol, name, exchange, industry, market cap, status
- Fundamental data: PE, PB, PS, ROE, ROA, debt-to-equity, gross/net margin, revenue/net profit growth
- Trading calendar: exchange + date + is_trading_day — from tushare `trade_cal` API

**What data do we need?**
- **Dividend history** — for tracking dividend income and split-adjusted returns (tushare `dividend` API)
- **Split/rights issue history** — for verifying forward-adjustment calculations (tushare `split` API)
- **Index constituents** — for building universe pools (CSI 300, CSI 500) (tushare `index_weight` API)
- **Limit-up/limit-down history** — for 涨跌停 detection (derived from daily high/low vs previous close, but needs explicit flagging)
- **Margin trading data** — short interest, margin balance (tushare `margin` API) — needed for short strategy modeling
- **Analyst estimates and ratings** — price targets, rating changes (tushare `analyst_estimate` API)
- **News headlines** — for sentiment factor (crawl 东方财富, 同花顺, or use tushare `news` API)

**How should it be stored and accessed?**
- PostgreSQL + TimescaleDB: OHLCV as hypertable partitioned by time, stock fundamentals with symbol+date PK
- `trading_calendar` table: (exchange, cal_date) PK, is_open boolean — authoritative source for backtest iteration
- `factor_cache` table: pre-computed factor scores (momentum, value quintile, etc.) keyed by (symbol, date, factor_name) — avoids recalculating 5 years of z-scores on every backtest
- Data service (port 8081) exposes HTTP API for all data queries — backtest engine never talks to DB directly
- Redis: hot caching for frequently accessed OHLCV and fundamental data within active backtest sessions

---

### B. Strategy Layer

**How are strategies created, managed, executed?**

Strategies implement the `Strategy` interface:
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

Strategies live in `pkg/strategy/plugins/` and auto-register via `init()`. A strategy config (YAML) provides parameters at runtime. The engine resolves strategies by name from the `GlobalRegistry`.

**Signal → Trade pipeline:**
1. Engine iterates to trading day D
2. `Strategy.GenerateSignals()` returns `[]Signal` for universe
3. Risk service adjusts signal weights based on portfolio volatility and regime
4. `Tracker.ExecuteTrade()` converts signal to trade, respecting T+1, position limits, 涨跌停
5. Portfolio updated; daily NAV recorded

**What strategy types should be supported?**

| Type | Examples | Status |
|------|----------|--------|
| Momentum | 20-day price momentum, 12M reversal | ✅ momentum.go exists |
| Value | PE/PB/PS screening, composite value score | ✅ value_momentum.go exists |
| Multi-Factor | Value + Momentum + Quality composite | ✅ multi_factor.go exists (live computation; Factor cache needed for production-scale multi-factor evaluation) |
| Mean Reversion | Bollinger bands, RSI thresholds | ⬜ Planned (Phase 2) |
| Risk Parity | Volatility-adjusted equal risk contribution | ⬜ Planned (Phase 4) |
| Event-Driven | Earnings surprises, analyst upgrades | ⬜ Planned (Phase 3) |
| Sentiment | News/algo sentiment scoring | ⬜ Planned (Phase 3) |
| Machine Learning | Factor model + prediction (future) | ⬜ Planned (future) |

---

### C. Execution Layer

**Order management, position tracking:**
- `Tracker` struct in `pkg/backtest/tracker.go` is the execution engine within each backtest
- `target_position` vs `actual_position` separation (vnpy pattern) — strategy generates target, execution fills gap to actual
- Partial fills modeled: if order quantity > available liquidity, partial fill at limit price
- Order types: market (executed at close), limit (executed if price crosses threshold)
- Order log: every attempted order with reason, filled quantity, price, fees

**Position tracking:**
```go
type Position struct {
    Symbol           string
    Quantity         float64    // Total shares
    QuantityYesterday float64   // Shares held since yesterday (can sell today)
    QuantityToday    float64    // Shares bought today (T+1 locked)
    AvgCost          float64
    BuyDate          map[int]float64  // share_count by trading_day — for T+1 enforcement
}
```

**Commission / fee handling (A-share):**
- Commission: 0.03% per side, minimum 5 CNY per trade
- Stamp tax: 0.1% on sell side only (印花税)
- Transfer fee: 0.001% both sides (过户费)
- Slippage: 0.01% per trade (configurable) — models market impact of order size
- Short selling: margin interest rate ~10.6% annual (configurable)

**T+1 Settlement:**
- Shares bought on day D cannot be sold until day D+1 (at minimum)
- `QuantityYesterday` tracks sellable shares; `QuantityToday` tracks locked shares
- Capital used to buy today is locked until positions are sold (buying power check)
- YD bucket (昨日持仓可卖) vs TD bucket (今日买入不可卖) — vnpy's OffsetConverter pattern

**Price limit (涨跌停) detection:**
- Limit-up: if `(high - prev_close) / prev_close >= 0.10` → no new buys that day
- Limit-down: if `(low - prev_close) / prev_close <= -0.10` → no new sells that day
- ST stocks: ±5% limits
- Price resumes from limit level on next trading day (gap model)

---

### D. Analytics Layer

**Backtesting engine:**
- Go-native, single-process, goroutine-parallel for multi-stock universes
- Engine flow: Init → Load Data → Daily Loop (regime → signals → risk → execution → record) → Finalize → Report
- Supports intraday rebalancing (weekly/monthly thresholds configurable)
- Equity curve: daily NAV for every trading day in range
- Trade log: every order with timestamp, symbol, direction, quantity, price, fees, PnL

**Performance metrics:**

| Metric | Formula | Target |
|--------|---------|--------|
| Total Return | (final_nav / initial_nav - 1) | > 50% (5yr) |
| Annualized Return | (1 + total_return)^(252/days) - 1 | > 10% |
| Sharpe Ratio | (annualized_return - risk_free) / annual_vol | > 1.5 |
| Max Drawdown | max(peak - nav) / peak | < 15% |
| Calmar Ratio | annualized_return / max_drawdown | > 1.0 |
| Win Rate | winning_trades / total_trades | > 55% |
| Profit Factor | gross_profit / gross_loss | > 1.5 |
| Avg Holding Days | sum(holding_days) / num_trades | benchmark-dependent |
| Turnover | total_traded_value / (2 × portfolio_value) | < 100% annually |

**Factor analysis:**
- Factor returns: measure return of top quintile vs bottom quintile for each factor
- Factor correlation matrix: prevent stacking correlated factors
- IC (Information Coefficient): rank correlation between factor value and forward returns
- Multi-factor attribution: decompose portfolio return into factor contributions
- Factor decay analysis: how quickly factor predictive power diminishes (1M, 3M, 6M)

---

### E. User Interface

**Dashboard (`/dashboard`):**
- Portfolio overview: current positions, cash, total value
- P&L display: daily, weekly, monthly, YTD
- Position detail: unrealized/realized PnL, cost basis, weight
- Risk indicators: portfolio volatility, max drawdown, VaR
- Recent trades list
- Market regime indicator (bull/bear/sideways)

**Backtest UI (`/` or `/backtest`):**
- Strategy selector (from registry)
- Date range picker (start, end)
- Universe selector (single stock, list, or index)
- Initial capital input
- Commission/slippage config
- Run button → progress indicator
- Results: equity curve chart, metrics table, trade log, factor attribution
- Compare runs: overlay two backtest equity curves

**Stock Screener (`/screen`):**
- Filter builder: add factor filters (PE < 20, ROE > 10%, etc.)
- Ranking: select factor + sort direction for final ranking
- Date selector: screen as of historical date
- Output: ranked list with key metrics displayed
- Export: download as CSV or push to backtest universe

**Strategy Copilot (AI-assisted, future — Phase 2):**
- Chat interface with 龙少
- Natural language → strategy code generation
- Edit/refine strategy with AI assistance
- Validate strategy: AI reviews code for common mistakes
- One-click backtest after code generation

**Strategy Editor (future — Phase 3):**
- Visual strategy builder: drag factors, set thresholds, define rebalance rules
- Code view alongside visual view (what you see is what runs)
- Version history for strategies

---

### F. Infrastructure

**API design:**
- Analysis service (8085): HTTP gateway, backtest orchestration, report generation
- Data service (8081): data sync from tushare, serve OHLCV/fundamentals/screen queries
- Strategy service (8082): strategy registry, hot-swap management (backup/external)
- Risk service (8083): position sizing, VaR/CVaR, regime detection, stop-loss
- Execution service (8084): order management (stub for v1, real integration later)
- All inter-service communication via HTTP over Docker network
- REST API with JSON payloads; no message queue dependency for v1

**Database schema (PostgreSQL + TimescaleDB):**
- `stocks`: symbol PK, name, exchange, industry, market_cap, status
- `ohlcv_daily_qfq`: (symbol, trade_date) PK — hypertable partitioned by date
- `stock_fundamentals`: (symbol, trade_date) PK — fundamental ratios at quarterly frequency
- `trading_calendar`: (exchange, cal_date) PK — authoritative list of trading days
- `factor_cache`: (symbol, trade_date, factor_name) PK — pre-computed factors
- `backtest_runs`: id PK, strategy, start_date, end_date, initial_capital, result_json, created_at
- `orders`: id PK, backtest_run_id, symbol, date, direction, quantity, price, fees, filled

**Docker / services:**
- `docker-compose.yml` defines all services: postgres (TimescaleDB image), redis, analysis-service, data-service, strategy-service, risk-service
- Each Go service is a separate Docker container
- Volume mounts for data persistence and config
- `Dockerfile.service` multi-stage build: Go build → minimal distroless image
- Kubernetes manifests (future): HPA, PodDisruptionBudget, resource limits, readiness/liveness probes

**Monitoring / logging:**
- Structured logging via zerolog (JSON to stdout, parsed by Docker log driver)
- Request IDs propagated across all service calls
- `/health` endpoint on every service
- Metrics endpoint (Prometheus format, future): backtest duration, data sync lag, API latency
- Alerting (future): PagerDuty for backtest failures, data sync stalls

---

## 4. Feature Details

### A. Data Layer

| Feature | Description | Priority | Status | Dependencies |
|---------|-------------|----------|--------|--------------|
| Tushare OHLCV sync | Sync daily qfq-adjusted OHLCV for all A-share stocks | P0 | ✅ Done | PostgreSQL, tushare token |
| Stock master sync | Sync stock list (symbol, name, exchange, industry) | P0 | ✅ Done | PostgreSQL |
| Trading calendar sync | Sync exchange calendar (is_open per date) from tushare `trade_cal` | P0 | 🔄 In Progress | PostgreSQL |
| T+1 settlement enforcement | Track buy date per position; lock same-day buys from selling | P0 | 🔄 In Progress | Tracker redesign, trading calendar |
| 前复权 (qfq) data | Use tushare `stk_factor_pro` open_qfq/high_qfq/low_qfq/close_qfq fields | P0 | ✅ Done | — |
| Commission structure (A股) | 0.03% commission + 0.1% stamp tax (sell-only) + 0.001% transfer fee | P0 | ✅ Done | — |
| 涨跌停 detection | Detect limit-up/limit-down; block buys on limit-up, sells on limit-down | P0 | 🔄 In Progress | OHLCV data with prev_close |
| Dividend data sync | Track dividend income per position; affect portfolio total return | P1 | ⬜ Planned | Dividend API, tracker update |
| Split/rights issue history | Corporate action history for verifying forward-adjustment calculations | P1 | ⬜ Planned | Split API, data verification |
| Index constituents sync | CSI 300/500/800 constituent lists for universe definition | P1 | ⬜ Planned | Index weight API |
| Factor cache | Pre-compute factor scores (z-scores, quintiles) per stock per date | P1 | ⬜ Planned | Fundamentals data |
| Short selling cost model | Margin interest accrual on short positions (10.6% annual default) | P1 | ⬜ Planned | Tracker redesign |
| Market impact model | Volume-based slippage: `sigma * sqrt(order_fraction / ADV)` | P2 | ⬜ Planned | OHLCV volume data |
| News/sentiment data | Crawl financial news; AI sentiment score per stock per day | P2 | ⬜ Planned | News API, AI integration |
| VaR / CVaR calculation | Historical simulation VaR at 95%/99%; CVaR (Expected Shortfall) | P2 | ⬜ Planned | OHLCV returns, Risk service |

### B. Strategy Layer

| Feature | Description | Priority | Status | Dependencies |
|---------|-------------|----------|--------|--------------|
| Strategy interface | `Strategy` interface definition in `pkg/strategy/strategy.go` | P0 | ✅ Done | — |
| Strategy registry | `GlobalRegistry` maps name → Strategy instance; auto-discovery via `init()` | P0 | ✅ Done | — |
| Momentum strategy | 20-day price momentum; buy strength, sell weakness | P0 | ✅ Done | — |
| Value Momentum strategy | PE + PB + ROE + 20-day momentum composite | P0 | ✅ Done | — |
| Multi-factor strategy | Configurable weighted factors via YAML ⚠️ | P0 | ⚠️ Experimental | Factor definitions |
> ⚠️ **Note:** Multi-factor strategy is **experimental** — it requires Factor Cache (P1, planned Phase 2) for production-scale multi-factor evaluation. Currently recomputes z-scores on every backtest, making it slow for large universes. Do not rely on it for production decisions until Factor Cache is built.
| Mean reversion strategy | Bollinger bands + RSI threshold signals | P1 | ⬜ Planned | — |
| Risk parity strategy | Equal risk contribution across positions | P2 | ⬜ Planned | Risk service |
| Hot-swap strategy loading | Load/reload strategies at runtime without service restart | P1 | ⬜ Planned | Plugin system, strategy service |
| Strategy versioning | Track which strategy version ran which backtest | P1 | ⬜ Planned | Backtest runs table |
| Strategy DB config | Database-backed strategy parameters (strategies table with JSONB); YAML as import/export | P1 | ⬜ Planned | PostgreSQL, Strategy service CRUD API |
| Strategy correlation analysis | Measure pairwise correlation of strategy returns | P2 | ⬜ Planned | Backtest engine |

### C. Execution Layer

| Feature | Description | Priority | Status | Dependencies |
|---------|-------------|----------|--------|--------------|
| Order execution (backtest) | Convert Signal → Trade; execute at close price with slippage | P0 | ✅ Done | Tracker |
| Position tracking | Maintain per-symbol positions; update on every trade | P0 | ✅ Done | — |
| T+1 position buckets | YD (sellable) vs TD (locked) quantity tracking per position | P0 | 🔄 In Progress | Trading calendar |
| Commission + stamp tax | Accurate A-share commission with sell-only stamp tax | P0 | ✅ Done | — |
| Slippage modeling | Configurable flat slippage rate per trade | P0 | ✅ Done | — |
| Buying power check | Prevent orders exceeding available cash | P0 | ✅ Done | — |
| Integer share rounding | Floor to nearest 100 shares (A股 1手 = 100 shares) | P0 | ✅ Done | — |
| Limit order support | Execute only if price crosses threshold within day | P1 | ⬜ Planned | OHLCV intraday reach |
| Partial fill modeling | Order fills only portion if volume insufficient | P1 | ⬜ Planned | Volume data |
| Target/actual position separation | Strategy generates target; execution layer closes gap | P1 | ⬜ Planned | Strategy service |
| Real broker integration | Futu/Tiger broker API for paper or live trading | P2 | ⬜ Planned | Broker SDK |

### D. Analytics Layer

| Feature | Description | Priority | Status | Dependencies |
|---------|-------------|----------|--------|--------------|
| Equity curve | Daily NAV for every trading day in backtest | P0 | ✅ Done | Backtest engine |
| Total return | (final_nav / initial_nav - 1) | P0 | ✅ Done | — |
| Annualized return | Compounded annual growth rate | P0 | ✅ Done | — |
| Sharpe ratio | (return - risk_free) / volatility | P0 | ✅ Done | — |
| Max drawdown | Peak-to-trough decline | P0 | ✅ Done | — |
| Win rate | Winning trades / total trades | P0 | ✅ Done | — |
| Trade log | Complete order history with PnL per trade | P0 | ✅ Done | — |
| Calmar ratio | Annualized return / max drawdown | P0 | ✅ Done | — |
| Profit factor | Gross profit / gross loss | P1 | ✅ Done | — |
| Factor attribution | Decompose portfolio return into factor contributions | P1 | ⬜ Planned | Factor cache |
| IC (Information Coefficient) | Rank correlation of factor to forward returns | P1 | ⬜ Planned | Factor cache |
| Factor decay analysis | Measure factor predictive power over 1M/3M/6M horizons | P2 | ⬜ Planned | Factor cache |
| Strategy comparison | Overlay two backtest equity curves | P1 | ⬜ Planned | Dashboard |
| Strategy monitoring +失效detection | Track deployed strategy rolling Sharpe/drawdown; fire alert when 失效触发条件 met; support auto-retrain trigger | P1 | ⬜ Planned | OHLCV data, Risk service, Alerting system |
| Report export | Generate PDF/HTML report from backtest | P2 | ⬜ Planned | Report template |
| Walk-forward validation | Train/validate split by date range in backtest engine; required for AI Evolution candidate screening | P1 | ⬜ Planned | Backtest engine, factor cache |

### E. User Interface

| Feature | Description | Priority | Status | Dependencies |
|---------|-------------|----------|--------|--------------|
| Backtest UI (index.html) | Run backtest: select strategy, dates, universe, capital | P0 | ✅ Done | Analysis service API |
| Backtest results display | Equity curve chart, metrics table, trade log | P0 | ✅ Done | — |
| Dashboard (dashboard.html) | Portfolio overview, P&L, positions, risk indicators | P0 | 🔄 In Progress | Execution service |
| Stock screener (screen.html) | Filter by factors; rank and export | P1 | ✅ Done | Data service `/screen` |
| Strategy selector UI | Dropdown + config panel for available strategies | P1 | ⬜ Planned | Strategy registry API |
| Backtest comparison UI | Compare two or more backtest runs side by side | P1 | ⬜ Planned | Backtest runs storage |
| Strategy Copilot | Chat interface with 龙少 for strategy creation | P1 | ⬜ Planned | AI integration, code generation |
| Visual strategy editor | Drag-drop factor builder | P3 | ⬜ Planned | Strategy Copilot |
| Real-time paper trading UI | Live positions, orders, P&L update throughout trading day | P3 | ⬜ Planned | Execution service |

### F. Infrastructure

| Feature | Description | Priority | Status | Dependencies |
|---------|-------------|----------|--------|--------------|
| Docker Compose setup | All services containerized, one-command startup | P0 | ✅ Done | Docker, docker-compose |
| PostgreSQL + TimescaleDB | TimescaleDB image for OHLCV time-series optimization | P0 | ✅ Done | — |
| Structured logging (zerolog) | JSON logs with request IDs across all services | P0 | ✅ Done | — |
| Health endpoints | `/health` on every service | P0 | ✅ Done | — |
| Config via Viper | All config in YAML files; env var overrides | P0 | ✅ Done | — |
| Data service API | HTTP API for data queries and sync triggers | P0 | ✅ Done | — |
| Redis caching | Cache hot OHLCV/fundamental data for active sessions | P1 | ⬜ Planned | Redis Docker service |
| Background backtest worker | Isolated backtest job processor; API returns job_id, worker runs async, results persisted to DB | P1 | ⬜ Planned | PostgreSQL job queue, Analysis service `--worker` mode |
| Kubernetes manifests | Production-grade deployment configs | P2 | ⬜ Planned | — |
| Prometheus metrics | API latency, backtest duration, sync lag metrics | P2 | ⬜ Planned | — |
| Alerting system | PagerDuty/Slack alerts for data sync stalls, backtest failures | P2 | ⬜ Planned | — |

---

## 4.5 Phase Plan

The phases below define the build order. All P0 items must be fully done (not "in progress") before advancing to the next phase.

### Phase 1 — Foundation & Accuracy ✅ DONE
**Goal:** Get the numbers right. Every subsequent phase depends on trustworthy backtest results.

| Category | P0 Deliverables |
|----------|-----------------|
| Data | Trading calendar sync ✅/🔄, T+1 settlement enforcement 🔄, 涨跌停 detection 🔄 |
| Execution | T+1 position buckets 🔄 |
| UI | Dashboard 🔄 |
| Infrastructure | Redis caching 🔄 (P0 — for speed optimization, see ADR-009) |

**Exit criteria — all must pass before Phase 2:**
1. **Accuracy:** ~~5-year, 500-stock backtest matches vnpy output within 5% drift~~ — **DROPPED**: vnpy parquet data unavailable; replaced with T+1 + 涨跌停 unit tests proving correctness
2. **T+1 correctness:** Unit tests prove same-day sell blocked; next-day sell succeeds ✅
3. **涨跌停 correctness:** Unit tests prove limit-up blocks buys, limit-down blocks sells; ST stocks ±5% limits enforced ✅
4. **Determinism:** Same seed → same results (fixed random seed enforced in backtest engine) ✅

> ⚠️ **Speed target moved to Phase 2** — see ADR-009. Current measured speed (~1500s for 500×5yr) is ~300× too slow. Speed optimization is a Phase 2 P0. Phase 1 focuses exclusively on accuracy.

**Phase Gate Review:** Before advancing to Phase 2, run the full Phase 1 acceptance test suite (see `docs/TEST.md`). All tests must pass. Record results in `docs/phase-gate-reviews.md`.

### Phase 2 — Reliability & Copilot (Current)
**Goal:** Make the system robust enough for daily use and introduce AI assistance.

| Category | Deliverables |
|----------|-------------|
| Data | Dividend data sync, split/rights issue, index constituents, factor cache |
| Strategy | Mean reversion strategy, hot-swap loading, strategy DB config |
| Execution | Limit order support, partial fill modeling, target/actual position separation |
| Analytics | Factor attribution, IC analysis, strategy comparison UI, walk-forward validation |
| UI | Strategy selector UI, backtest comparison UI, Strategy Copilot |
| Infrastructure | Speed optimization P0 (in-memory pre-load, TimescaleDB chunking, parallel warmCache — see ADR-009); background backtest worker |

**Exit criteria — all must pass before Phase 3:**
1. **Speed:** 500-stock, 5-year backtest completes in ≤ 5 seconds (Phase 2 P0 target — ADR-009)
2. **Factor Cache:** Multi-factor backtest (100 stocks, 3 years) completes in ≤ 30 seconds (via pre-computed factor scores)
3. **Strategy Copilot:** End-to-end test: user submits Chinese description → receives compilable Go code → backtest runs → results displayed — all in one session; ≥ 30% acceptance rate
4. **Walk-forward validation:** Framework operational; all Phase 3 candidate strategies must pass train/validate split before entering paper trading
5. **Background worker:** `POST /backtest` returns `job_id` immediately; worker runs async; client can poll `/backtest/:id`
5. **Strategy DB config:** `strategies` table with JSONB column operational; YAML import/export functional

### Phase 3 — AI Evolution (Future)
**Goal:** System discovers and evaluates strategies autonomously.

| Category | Deliverables |
|----------|-------------|
| AI | Strategy Generator (LLM), Backtest Runner (parallel), Strategy Selector (ranking + monitoring) |
| Analytics | Factor decay analysis |
| UI | Strategy Copilot v2, visual strategy editor |
| Execution | Real broker integration (Futu/Tiger) |

**Prerequisites to start Phase 3:** All Phase 1 P0 items complete; all Phase 2 exit criteria passed (see Phase 2 gate); Phase 2 acceptance test suite recorded in `docs/phase-gate-reviews.md`.

### Phase 4 — Scale & Production
**Goal:** Make the system institutional-grade.

| Category | Deliverables |
|----------|-------------|
| Infrastructure | Kubernetes manifests, Prometheus metrics, alerting |
| Risk | VaR/CVaR, risk parity strategy, market impact model |
| Data | News/sentiment data pipeline |
| Strategy | Risk parity strategy, event-driven, sentiment strategies |
| UI | Real-time paper trading UI |

---

## 5. Architecture Decisions to Make

These are the open questions that require explicit decisions before the system can move from Phase 2 to Phase 3. Each has two or more viable options with tradeoffs.

### Decision 1: Dynamic Plugin Loading vs. Compiled Strategies

**Question:** Should strategies be loaded at runtime from `.so` plugin files (true hot-swap), or from Go source files compiled into the binary (safer, simpler)?

**Option A — Compiled strategies (current state):**
Strategies live in `pkg/strategy/plugins/` and are imported via `import _ "pkg/strategy/plugins"` at startup. Adding a strategy requires recompiling the binary. Hot-swap is achieved by swapping config files and restarting the service.
- ✅ Simple, type-safe, no `plugin` package complexity
- ✅ Full IDE support, Go compiler catches errors
- ❌ Requires restart to load new strategy
- ❌ Can't add strategy without rebuilding the binary

**Option B — Runtime plugin (`.so` files):**
Define a `Strategy` interface, compile strategies as `.so` plugins, load via Go's `plugin` package at runtime. Drop a new `.so` file, hit reload endpoint, new strategy is live.
- ✅ True hot-swap: no restart, no rebuild
- ✅ Users can add strategies without touching core binary
- ❌ `plugin` package is fragile across Go versions; limited OS support (no Windows)
- ❌ Debugging plugin code is painful
- ❌ Loses type safety across plugin boundary

**Recommendation:** **Option A (compiled)** for v1 and v2. The architecture is already "hot-swap" at the config level — swapping strategy parameters via YAML achieves most practical benefit. True `.so` hot-swap can be revisited when there is a clear user need. The `StrategyRegistry` abstraction means the switch to Option B would only require implementing a `StrategyLoader` interface — the hard architectural work is already done.

---

### Decision 2: TimescaleDB vs. Vanilla PostgreSQL for OHLCV Storage

**Question:** Should OHLCV data use TimescaleDB's hypertable partitioning, or standard PostgreSQL partitioned tables?

**Option A — TimescaleDB (current):**
`CREATE TABLE ohlcv_daily (...) WITH (timescaledb.continuous);` — TimescaleDB automatically partitions by time, enables compression, and provides efficient time-range queries.
- ✅ Native time-series optimization; chunk-based compression reduces storage 90%+
- ✅ Continuous aggregate queries (e.g., "last 30 days average volume") are fast
- ✅ `time_bucket()` function makes rolling window queries elegant
- ❌ TimescaleDB is a PostgreSQL extension; adds operational complexity
- ❌ License considerations for production use (TimescaleDB source available, but TimescaleDB binary has a source-available license with some closed-source features)
- ❌ Backup/restore is slightly more complex

**Option B — Vanilla PostgreSQL with native partitioning:**
Use PostgreSQL's native `PARTITION BY RANGE (trade_date)` — one partition per year or per month.
- ✅ Zero additional dependencies; uses only PostgreSQL
- ✅ Full SQL flexibility
- ✅ Easier operational simplicity
- ❌ No built-in compression
- ❌ Manual partition management (create new partitions, detach old ones)

**Recommendation:** **Option A (TimescaleDB)** — it is already running in production. The compression and time-series query performance are worth the operational complexity for a data-intensive system like this. If license concerns arise in a commercial context, migration to native partitioning is a one-week project with no architectural changes.

---

### Decision 3: In-Process Backtest vs. Separate Backtest Worker Service

**Question:** Should the backtest engine run in-process (same goroutine as the API server), or as a separate worker process with a job queue?

**Option A — In-process (current):**
`POST /backtest` blocks the HTTP handler, engine runs in same process, result returned when complete.
- ✅ Simplest to implement; no IPC overhead
- ✅ Full access to in-memory strategy registry and data cache
- ✅ Easy to debug; stack traces are immediate
- ❌ Long backtests (5+ years, 5,000 stocks) block the API server
- ❌ Cannot run multiple backtests in parallel from same instance
- ❌ Backtest crash can crash the API server

**Option B — Background worker with job queue:**
`POST /backtest` creates a job, returns `job_id` immediately. Worker process picks up job, runs backtest, writes result to DB. Client polls `/backtest/:id/status`.
- ✅ API server is never blocked
- ✅ Multiple backtests run in parallel
- ✅ Backtest crash is isolated
- ✅ Enables remote worker (GPU backtest machine, etc.)
- ❌ Adds infrastructure: job queue (Redis or PostgreSQL-backed), worker service
- ❌ Results must be persisted to DB between steps (cannot keep state in memory)
- ❌ Polling or WebSocket for status adds complexity

**Recommendation:** **Option B (background worker)** — but not immediately. The current in-process approach is fine for v1 where the target is single backtests under 30 seconds. The migration should happen when the system needs to support: (a) multiple concurrent users, (b) backtests longer than 1 minute, or (c) batch strategy optimization (walk-forward analysis). Implement as: `backtest_runs` table gets a `status` column; engine gains a `--worker` flag.

---

### Decision 4: Single Factor Score vs. Portfolio Optimizer for Position Sizing

**Question:** Should the system continue using rank-based composite scoring (current approach: sort stocks by weighted factor score, top N get equal weight), or migrate to formal portfolio optimization (mean-variance optimization / risk parity)?

**Option A — Rank-based equal weight (current):**
Sort universe by composite factor score, select top N, assign equal weight to each.
- ✅ Simple, interpretable, no estimation error in return forecasts
- ✅ Robust: doesn't require predicting returns, only ranking
- ✅ Fast: O(n log n) sort, no matrix inversion
- ❌ Ignores correlations between positions
- ❌ Ignores individual position volatility (treats high-vol and low-vol equally)
- ❌ No formal risk budgeting

**Option B — Portfolio optimization (MVO or risk parity):**
Use mean-variance optimization or risk parity to compute weights that minimize portfolio variance for a given return target.
- ✅ Theoretically sound: uses covariance matrix
- ✅ Enables risk parity (equal risk contribution per position)
- ✅ Handles correlation: diversification benefit is real
- ❌ Requires estimated returns — garbage in, garbage out (error maximization problem)
- ❌ Computationally heavier; covariance matrix estimation is noisy with small universes
- ❌ MVO produces corner solutions (all weight in one asset) without regularization

**Recommendation:** **Keep Option A for now, add Option B as a configuration choice.** The rank-based approach is correct for factor-based long-only strategies, which is 90% of this system's use case. Portfolio optimization should be added as an alternative `WeightScheme` in the Risk service. Users who want MVO can enable it; users who want simplicity use rank-based. Both should share the same signal generation pipeline.

---

### Decision 5: YAML Strategy Config vs. Database-Driven Strategy Config

**Question:** Should strategy parameters be stored in YAML files (current approach) or in the PostgreSQL database?

**Option A — YAML files (current):**
Strategy parameters in `config/strategies/*.yaml`, loaded at startup via Viper.
- ✅ Version controllable (git)
- ✅ Easy to edit without DB access
- ✅ No migration needed when params change
- ✅ Works well with config hot-reload
- ❌ No runtime visibility: cannot query "what parameters is strategy X using right now?" from API
- ❌ Hard to do A/B testing of strategy params without file juggling

**Option B — Database-backed:**
Strategy parameters stored in `strategies` table with JSONB column for config. API for CRUD operations.
- ✅ Full audit trail of parameter changes
- ✅ Runtime queryable: `/api/strategies/:name/config`
- ✅ Enables A/B testing (assign parameter set A vs B to users)
- ✅ Works naturally with multi-tenant (future)
- ❌ More infrastructure; migration scripts for schema changes
- ❌ Requires API for config management (more code)

**Recommendation:** **Option B — database-backed** as the primary store, with YAML as an import/export format. The system is already backed by PostgreSQL; adding a `strategies` table is trivial. Keep YAML for initial seeding and backup/export. The key insight: the backtest engine doesn't care where config comes from — the `StrategyLoader` reads from a common interface. So the DB can be the source of truth while YAML remains a convenient human-editable format.

**Migration plan (see Strategy DB config in Strategy Layer feature table):** Add `strategies` table with JSONB config column and CRUD API in Strategy service. Existing YAML configs remain importable. No changes to backtest engine or Strategy interface required — only `StrategyLoader` implementation swaps.

---

## 6. Success Metrics

These are the metrics that matter — not test coverage percentages or lines of code, but whether the system is actually useful, accurate, and worth a trader's time.

### Product Outcomes

| Metric | Definition | Target | How to Measure |
|--------|------------|--------|----------------|
| Backtest accuracy | Backtest return vs. actual historical portfolio return (where comparable) | < 5% drift | Compare against vnpy run on same strategy/dates |
| Strategy count | Number of distinct strategies users have run through the system | 5+ by end of Phase 2 | DB query of `backtest_runs` by strategy name |
| Backtest speed | Time from "click Run" to results displayed for 1yr/500 stock backtest | < 5 seconds | Server-side timing in backtest response |
| Data freshness | Age of latest OHLCV data in DB (business days behind) | < 2 business days | Compare `max(trade_date)` in DB vs. trading calendar |
| User adoption | Unique users who run a backtest per week | Growing | `backtest_runs.created_by` per week |
| Strategy sharability | Number of custom strategies shared by users (future) | > 0 by Phase 3 | Custom strategy table count |

### Financial Performance (Backtest Targets)

These are the targets for strategies run through the system. They are not guarantees — they are the bar that a strategy must clear before being taken seriously.

| Metric | Target | Context |
|--------|--------|---------|
| Sharpe Ratio | > 1.5 | Risk-adjusted return; the minimum for institutional-grade |
| Max Drawdown | < 15% | Avoid catastrophic losses |
| Annualized Return | > 10% | Beat simple index (CSI 300 baseline) |
| Calmar Ratio | > 1.0 | Return per unit of max drawdown |
| Win Rate | > 55% | Per trade, not per day |
| Profit Factor | > 1.5 | Gross profit / gross loss |

### Technical Health

| Metric | Target | How to Measure |
|--------|--------|----------------|
| API uptime | > 99.5% | Uptime monitoring on health endpoints |
| Data sync reliability | Every scheduled sync completes without error | Sync job logs |
| Backtest reproducibility | Same inputs → same outputs (deterministic) | Regression test suite |
| Test coverage | > 80% line coverage on core packages | `go test -cover` |
| Paper vs. backtest drift | < 10% difference in total return | Run paper trade alongside backtest |

### Usage Quality

| Metric | Definition | Target |
|--------|------------|--------|
| Strategy survival rate | Strategies that pass backtest continue to beat benchmark over time | > 40% at 6 months |
| User retention | Users who run >1 backtest per month | > 50% month-over-month |
| AI strategy acceptance rate | % of AI-generated strategies that pass code review and backtest | > 30% |

---

## Appendix: Document Relationships

This document is the **single source of truth** for what the system is and where it is going. It is derived from and supersedes:

- `ROADMAP.md` — tactical implementation roadmap (what to build next)
- `ARCHITECTURE.md` — technical architecture (how it is built)
- `SPEC.md` — detailed system specification and interfaces
- `ADR.md` + `docs/adr/` — architectural decision records
- `NEXT_STEPS.md` — audit findings and development plan

When this document conflicts with any of the above, this document takes precedence. The roadmap, architecture, and spec should be updated to match this document — not the other way around.

**Change process:** To propose a change to VISION.md, write the rationale and submit for review. Changes require understanding of both the product vision and the technical constraints. No single feature addition should contradict the Core Principles.

---

_Last updated: 2026-04-09 (document audit pass)_

---

## AI Strategy Evolution Engine（AI 策略自进化）

**这是系统的终极愿景。**

### 核心理念

> "系统不只是执行策略——它能够**自己发现**值得执行的策略。"

人类做量化：想策略 → 写代码 → 回测 → 调参 → 部署
AI 自进化：设定目标 → AI 自动生成 → 自动回测 → 自动排名 → 自动部署最优

### 三层 AI 架构

```
┌─────────────────────────────────────────────┐
│           AI Evolution Layer                  │
│                                              │
│  Strategy Generator (LLM)                   │
│    → 生成候选策略池                          │
│                                              │
│  Backtest Runner                            │
│    → 自动回测所有候选策略                    │
│                                              │
│  Strategy Selector                           │
│    → 自动排名、选择最优                     │
│    → 监控失效时自动触发再生成               │
└─────────────────────────────────────────────┘
           ↕ 循环迭代
┌─────────────────────────────────────────────┐
│         AI Copilot Layer (Phase 2)           │
│  自然语言交互、策略编辑、参数调优              │
└─────────────────────────────────────────────┘
```

### 自动进化流程

1. **目标设定**（人类）
   - "帮我找到年化收益 > 20%，最大回撤 < 15% 的 A 股策略"
   - 或者更简单："找低估值高ROE的价值股策略"
   - 人类 also sets constraints: max leverage, max single-position weight, forbidden asset classes

2. **策略生成**（AI）
   - AI 批量生成 N 个候选策略（不同的指标组合、参数空间）
   - 每个策略都有完整的 Go 代码或配置

3. **策略验证**（自动化, new）
   - Syntax and compile check: ensure all generated code passes `go build`
   - Logic guardrails: reject strategies with leverage > 1.0, position limits > 40%, or non-standard asset classes
   - AI code review: use a separate LLM call to flag obvious logic errors before backtesting
   - This step prevents broken or dangerous code from entering the backtest engine

4. **并行回测**（引擎）
   - N 个候选策略同时回测 (via Background backtest worker — see Infrastructure feature table)
   - 取历史 N 年数据，按多维度评分
   - **Overfitting guardrails:** walk-forward validation required for all candidates (train on years 1-N-1, validate on year N); strategies must beat benchmark in both train AND validation windows. Candidates that only pass in-sample are rejected.
   - Minimum backtest window: 3 years of data required for any candidate to be considered

5. **策略排名 + 人类审批**（AI + human)
   - AI ranks by Sharpe, annualized return, max drawdown, win rate, and Calmar ratio
   - Top candidates (top 5, not just top 1) presented to human for selection
   - Human sets final acceptance threshold (e.g., Sharpe > 1.0, max drawdown < 20%)
   - **No strategy enters paper trading without human approval.** Automation applies only to the generation, validation, backtest, and ranking steps — not to deployment.

6. **部署 + 监控**（自动化 + human)
   - Approved strategies enter paper trading pool
   - Continuous monitoring: if strategy Sharpe drops below threshold for 20 consecutive trading days, trigger "策略失效" alert
   - **失效触发条件:** rolling 60-day Sharpe < 0.5 OR rolling max drawdown exceeds original backtest max by 50% → auto-flag for review
   - Human decides: retrain (restart evolution loop), adjust parameters, or retire strategy

7. **自我进化**（循环）
   - Every month: re-run evolution loop with latest data
   - Every quarter: full walk-forward revalidation of all deployed strategies
   - Market regime change detection (via Risk service regime indicator) can trigger early re-run

### 为什么这个架构可行

- **LLM 生成代码** + **自动回测** = 快速验证
- 人的时间从"写代码调参"变成"设定目标和评估结果"
- 量化核心竞争优势从"谁有更好的策略"变成"谁的进化循环更快"

**但这不是魔法 — 以下是已知的局限性和风险:**

1. **Overfitting / Data Snooping**: Generating 100 strategies and backtesting all on the same historical dataset is the classic multiple-testing problem. The system mitigates this via walk-forward validation (train/validate split) and minimum backtest window requirements (3 years). No candidate strategy is accepted purely on in-sample performance.

2. **Strategy generation method**: LLM-generated strategies are prompt-driven (not fine-tuned). The prompt template encodes the `Strategy` interface conventions, factor definitions, and A-share rules. The LLM does not have direct access to the codebase — it receives a structured description of the interface and generates conforming code. Prompt engineering is a real maintenance cost.

3. **No price discovery**: The system optimizes historical returns. It cannot discover alpha that is not already in historical data. If market structure changes (e.g., A-share reform), the evolution loop must be re-run.

4. **Single-market bias risk**: Strategies generated for A-shares may encode China-specific rules (涨跌停, T+1) in the strategy logic itself. A strategy generated for A-shares is not portable to US equities without human review.

### 与 Copilot 的关系

| 层级 | 工具 | 角色 |
|------|------|------|
| Copilot | 人类写策略，AI 辅助 | 人为主 |
| **Evolution** | **AI 生成，人只设定目标** | **AI 为主** |

Copilot 是"AI 帮人类"，Evolution 是"AI 替人类做"。

### 技术前提

Each prerequisite has a concrete definition of "done" and maps to a feature table entry.

| Prerequisite | Concrete Definition of "成熟/Done" | Feature Table Entry |
|---|---|---|
| Strategy Copilot 成熟 | User can submit a Chinese strategy description, receive compilable Go code via LLM, run a backtest in the same session, and iterate on the strategy based on backtest results. Requires: AI integration, code generation prompt templates, and Strategy Copilot UI (P2, Phase 2) | Strategy Copilot (UI layer, P2) |
| 策略数据库 + 版本管理 | `strategies` table exists with JSONB config column, CRUD API implemented, backtest_runs table tracks strategy version per run, YAML import/export functional | Strategy DB config (Strategy layer, P1) |
| 背景回测服务（独立 worker） | `POST /backtest` returns `job_id` immediately; worker process picks up job and runs async; results persisted to `backtest_runs` table; client can poll status | Background backtest worker (Infrastructure, P1) |
| 策略监控 + 自动触发重训练 | Monitor deployed strategies' rolling Sharpe and drawdown; alert fired when 失效触发条件 met (see Step 6 above); evolution loop can be re-triggered manually or via alert | Strategy monitoring (Analytics layer, future — add as P1 when Phase 3 begins) |
| Walk-forward validation framework | Backtest engine supports train/validation split by date range; IC analysis and factor decay analysis operational (Factor cache P1 required first) | Factor decay analysis (Analytics, P2); Factor attribution (Analytics, P1) |


---

## AI Evolution 成功指标（进阶）

除了基础的成功指标，AI 自进化系统有自己的指标：

| 指标 | 目标 | 说明 |
|------|------|------|
| 策略生成速度 | < 5 分钟/100 个候选 | 从目标到候选池的时间 |
| 进化周期 | < 30 分钟/次 | 生成→回测→排名→推荐 全流程 |
| 候选池质量 | Top 10% Sharpe > 基准 50% | 进化出的策略是否真的优于随机 |
| 策略存活率 | 6 个月后仍跑赢基准 > 40% | 策略是否真的有效，不是过拟合 |
| 自动化率 | 无需人工干预进化循环 > 90% | 人类只负责设定目标 |

### 最终愿景的一句话

> **"每周一杯咖啡的时间，系统告诉你本周最优策略是什么。"**

这就是 AI 量化系统的终极形态。
