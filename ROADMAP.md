# Quant Trading System - Roadmap

> Last updated: 2026-03-23
> Version: 0.2.0

---

## 🎯 Vision

**Build a professional quantitative trading platform where:**
- Strategies are **hot-swappable plugins** — users can add new strategies without rebuilding
- Users can **write strategies themselves** or **with AI Copilot assistance**
- Multiple strategies can run in parallel; **AI (龙少) helps select the best** strategy or portfolio of strategies
- The platform learns from backtests and improves over time

---

## 📊 Current Status (as of 2026-03-23)

### What We Have
- Go backtest engine (high performance, not Python)
- PostgreSQL + TimescaleDB for OHLCV storage
- Momentum + Value Momentum strategies
- Slippage + Commission modeling
- ATR-based stop loss
- Weekly rebalancing
- Browser-based Web UI

### What We Learned from vnpy
- vnpy is mature but Python-based (slow)
- Target/Actual position separation is a best practice
- T+1 settlement must be enforced at OMS level (TD/YD buckets)
- Trading calendar is essential for A-share accuracy
- Stamp tax (0.1% sell only) must be separate from commission
- Price limit (涨跌停) detection needed

### What We're Fixing
- **OHLCV data**: Switching to 前复权 (qfq) from Tushare stk_factor_pro API
- Old 不复权 data deleted → must re-sync all stocks

---

## 🛤️ Roadmap

### Phase 1: Accuracy & Data Foundation (NOW → 1 week)
**Goal: Fix critical accuracy issues before any live use**

- [ ] **T+1 Settlement (P0)**
  - Track buyDate per position (TD = today, YD = yesterday)
  - Cannot sell same-day bought shares
  - vnpy pattern: OffsetConverter with TD/YD buckets
  - Impact: without this, returns overstated 5-15%

- [ ] **Trading Calendar (P0)**
  - Create `trade_calendar` table (date + is_trading_day)
  - Sync from Tushare trade_cal API
  - A股 holidays: CNY, National Day, Memorial days
  - Use calendar to iterate trading days in backtest engine

- [ ] **Commission Structure Fix (P1)**
  - A股: commission ~0.03% + stamp tax 0.1% (SELL ONLY) + transfer fee 0.001%
  - Separate stamp tax from commission
  - vnpy shows these must be split

- [ ] **涨跌停 Detection (P1)**
  - Detect daily limit-up / limit-down in backtest
  - Cannot buy on limit-up day, cannot sell on limit-down day
  - Price continues from limit for next valid day

- [ ] **Data Sync (BACKGROUND, ~4-5 hours)**
  - ohlcv_daily_qfq table exists (600519.SH test done)
  - Need to sync all 5,491 stocks from 2000-01-01 to 2026-03-20
  - Year-by-year chunks (~27 requests per stock)
  - Rate limited: 250ms between requests

---

### Phase 2: Core Architecture (2-4 weeks)
**Goal: Add professional quant system features**

- [ ] **Target/Actual Position Separation**
  - vnpy pattern: `target_position` vs `actual_position`
  - Strategy generates signals → target position
  - Execution layer fills gap to actual
  - Enables partial fills, position limits

- [ ] **Strategy Plugin System (Hot-swap)**
  - Define `Strategy` interface with `GenerateSignals()`, `Name()`, `Parameters()`
  - Strategies loaded dynamically from `plugins/` directory
  - No rebuild needed to add new strategy
  - Strategy registry via API

- [ ] **Signal-driven Strategy Interface**
  - vnpy pattern: `get_signal()` returns pre-computed signals
  - `execute_trading()` handles target vs actual gap
  - Cleaner separation of signal generation vs execution

- [ ] **Strategy Copilot (AI-assisted coding)**
  - User describes strategy in natural language
  - AI generates Go strategy code
  - User can edit and refine with validation

---

### Phase 3: Factor System + Stock Selection (1-2 months)
**Goal: Intelligent stock screening + multi-factor strategies**

#### Factor Library
- [ ] **Value Factors**: PE, PB, PS, PCFR, EV/EBITDA
- [ ] **Momentum Factors**: 1M/3M/6M/12M momentum + reversal detection
- [ ] **Quality Factors**: ROE, ROA, debt-to-equity, operating cash flow
- [ ] **Growth Factors**: revenue growth, profit growth, PEG
- [ ] **Analyst Factors**: rating changes, price target deviation

#### Stock Selector
- [ ] Screen stocks by multiple factors simultaneously
- [ ] Rank by composite factor score
- [ ] Select top N for strategy deployment

#### News & Sentiment AI
- [ ] Crawl financial news (东方财富, 同花顺, Reuters)
- [ ] AI sentiment analysis → sentiment score
- [ ] Geopolitical event impact → macro shock factor

---

### Phase 4: Portfolio & Risk (2-3 months)
**Goal: Multi-strategy portfolio with professional risk management**

- [ ] **Multi-Strategy Portfolio**
  - Run multiple strategies simultaneously
  - Weight allocation optimization (equal weight, risk parity, etc.)
  - Strategy correlation analysis

- [ ] **Portfolio Risk Metrics**
  - VaR / CVaR calculation
  - Portfolio-level drawdown monitoring
  - Strategy diversification metrics

- [ ] **Dynamic Position Sizing**
  - Volatility-adjusted position sizing (risk parity)
  - ATR-based dynamic stops
  - Max drawdown → position reduction

---

### Phase 5: Live Trading (3-6 months)
**Goal: Real money (paper trading first)**

- [ ] **Paper Trading Mode**
  - Simulated execution, real market data
  - No real orders placed
  - Compare live vs backtest

- [ ] **Broker Integration**
  - Futu (港美股) — https://open.futunn.com/
  - Tiger (港美股) — https://quant.tigerbrokers.com/

- [ ] **Real-time Risk Alerts**
  - Push notifications on drawdown thresholds
  - Daily/weekly performance summaries

---

## 🏗️ Architecture (Reference)

```
┌─────────────────────────────────────────────────────────────┐
│                        UI (Browser)                          │
│   Dashboard / Strategy Copilot / Stock Selector              │
└─────────────────────┬───────────────────────────────────────┘
                      │ HTTP API
┌─────────────────────▼───────────────────────────────────────┐
│                 Analysis Service (Port 8085)                   │
│   Backtest Engine / Report Generator / Strategy Selector      │
└──────┬──────────────────┬───────────────────┬───────────────┘
       │                  │                   │
┌──────▼──────┐  ┌───────▼─────┐  ┌────────▼────────┐
│ Risk Service │  │Strategy Svc  │  │  Data Service   │
│   (8083)     │  │   (8082)    │  │    (8081)      │
│              │  │             │  │                 │
│ ATR Stop     │  │ Momentum    │  │ OHLCV Storage   │
│ Position Siz │  │ Value Momen │  │ Tushare Sync    │
│ Regime Detec │  │ Copilot Gen │  │ Stock Master    │
└──────────────┘  └─────────────┘  └─────────────────┘
                                          │
                              ┌────────────▼────────────┐
                              │ PostgreSQL + TimescaleDB  │
                              └───────────────────────────┘
```

### Key Lessons from vnpy
1. **T+1 = TD/YD buckets** (OffsetConverter pattern)
2. **Target vs Actual positions** (execute_trading fills gap)
3. **Signal-driven** (get_signal → execute_trading)
4. **Commission must be split** (stamp tax is sell-only)

---

## 📝 Implementation Notes

### A-share Trading Rules
- **Trading hours**: 9:30-11:30, 13:00-15:00 (T+1 settlement)
- **Commission**: ~0.03% (min 5元 per trade)
- **Stamp tax**: 0.1% on sell only
- **Transfer fee**: 0.001% both sides
- **Price limits**: ±10% (ST stocks ±5%)
- **T+1**: Cannot sell shares bought same day

### Tushare Data
- **API**: stk_factor_pro for qfq data
- **Rate limit**: 200 req/min (use 250ms sleep = 240 req/min)
- **Record limit**: ~1000 records per request (use year-by-year chunks)

---

## 🎯 Success Metrics

| Metric | Target |
|--------|--------|
| Backtest Sharpe Ratio | > 1.5 |
| Max Drawdown | < 15% |
| Strategy Count | 5+ pluggable strategies |
| Backtest Speed | < 5 seconds for 1 year |
| Paper vs Backtest Drift | < 10% |
