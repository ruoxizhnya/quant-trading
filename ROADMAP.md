# Quant Trading System - Roadmap

> Last updated: 2026-03-23
> Version: 0.1.0

---

## 🎯 Vision

**Build a professional quantitative trading platform where:**
- Strategies are **hot-swappable plugins** — users can add new strategies without rebuilding
- Users can **write strategies themselves** or **with AI Copilot assistance**
- Multiple strategies can run in parallel; **AI (龙少) helps select the best** strategy or portfolio of strategies
- The platform learns from backtests and improves over time

---

## 🛤️ Roadmap

### Phase 1: Foundation (Now → 1 month)
**Goal: Make the backtest engine reliable and usable**

- [x] Fix API URL mismatches between services
- [x] Add currentPrice to risk service (fix hardcoded 100.0)
- [x] Slippage modeling (0.01% per trade)
- [x] Commission modeling
- [x] Equity curve calculation fix (short positions)
- [x] Metrics calculation fix (annual return/Sharpe using actual dates)
- [x] **UI Dashboard** — browser-based backtesting interface at http://localhost:8085
- [x] Optimize momentum strategy — weekly rebalancing (not daily) to avoid position accumulation
- [ ] ATR-based dynamic stop loss
- [ ] Max drawdown monitoring and alerts

### Phase 2: Factor System + Smart Stock Selection (1-3 months)
**Goal: Intelligent stock screening + multi-factor strategies**

#### Factor Library
- [ ] **Value Factors**: PE, PB, PS, PCFR, EV/EBITDA
- [ ] **Momentum Factors**: 1M/3M/6M/12M momentum + momentum reversal (falling knife detection)
- [ ] **Quality Factors**: ROE, ROA, debt-to-equity, operating cash flow
- [ ] **Growth Factors**: revenue growth, profit growth, PEG
- [ ] **Analyst Factors**: rating upgrades/downgrades, price target deviation
- [ ] **Sentiment Factors**: news情绪, social media (雪球/东财股吧)
- [ ] **Macro Factors**: interest rates, CPI, FX, geopolitical events

#### Stock Selector
- [ ] Screen stocks by multiple factors simultaneously
- [ ] Rank stocks by composite factor score
- [ ] Select top N stocks for strategy deployment

#### News & Sentiment AI
- [ ] Crawl financial news (东方财富, 同花顺, Reuters, Bloomberg)
- [ ] AI sentiment analysis → sentiment factor score
- [ ] Geopolitical event impact assessment → macro shock factor

### Phase 3: Strategy Framework (Plugin Architecture) (2-4 months)
**Goal: Hot-swappable strategy plugins**

- [ ] **Strategy Plugin Interface**
  - Define `Strategy` interface with `GenerateSignals()`, `Name()`, `Parameters()`
  - Strategies are loaded dynamically (no rebuild needed to add new strategy)
  - Strategy registry — list available strategies via API

- [ ] **Strategy UI**
  - View all available strategies
  - Configure strategy parameters
  - Enable/disable strategies

- [ ] **Strategy Copilot (AI-assisted coding)**
  - User describes strategy in natural language
  - AI generates strategy code
  - User can edit and refine
  - Syntax highlighting + strategy validation

- [ ] **Built-in Strategy Library**
  - Momentum (needs fixing — monthly rebalancing)
  - Value Momentum (needs financial data)
  - Mean Reversion
  - Breakout
  - Pair Trading

### Phase 4: Strategy Selection & Portfolio (3-6 months)
**Goal: AI helps pick best strategies**

- [ ] **Backtest All Strategies**
  - Run backtest for every strategy in the library
  - Compare performance metrics

- [ ] **Strategy Selector**
  - AI (龙少) analyzes backtest results
  - Recommends best strategy for current market conditions
  - Or: recommends portfolio of multiple strategies

- [ ] **Strategy Combination**
  - Combine multiple strategies in one portfolio
  - Weight allocation optimization

### Phase 5: Live Trading (6+ months)
**Goal: Real money**

- [ ] **Paper Trading Mode**
  - Simulated execution, real market data
  - No real orders placed

- [ ] **Broker Integration**
  - Futu (港美股) — https://open.futunn.com/
  - Tiger (港美股) — https://quant.tigerbrokers.com/
  - Snowball (港股) — https://www.snowball.com/

- [ ] **Real-time Risk Management**
  - Live ATR stop loss
  - Drawdown-based position reduction
  - Position size limits

---

## 🏗️ Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        UI (Browser)                          │
│   Dashboard / Strategy Copilot / Stock Selector              │
└─────────────────────┬───────────────────────────────────────┘
                      │ HTTP API
┌─────────────────────▼───────────────────────────────────────┐
│                 Analysis Service (Port 8085)                  │
│   Backtest Engine / Report Generator / Strategy Selector     │
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

---

## 📝 Notes

- **2026-03-23 Morning**: System launched. Backtest engine working with momentum strategy. Slippage and commission modeled. UI dashboard built.
- **2026-03-23 Morning**: Identified key issues: (1) momentum strategy generates signals daily causing position accumulation; (2) news/sentiment pipeline not yet built; (3) strategy plugin system not yet implemented.
- **2026-03-23 Afternoon**: Fixed momentum strategy — weekly rebalancing is now default (not daily). Results much more realistic: 5 trades (weekly) vs 21 trades (daily). UI pushed to http://localhost:8085. ROADMAP.md created.
- **2026-03-23 Evening**: Momentum strategy weekly rebalancing verified: 600000.SH (Mar-Apr 2024) = +0.27%, 5 weekly trades, Sharpe 5.66, MaxDD 0.04%.

---

## 🎯 Success Metrics

| Metric | Target |
|--------|--------|
| Backtest Sharpe Ratio | > 1.5 |
| Max Drawdown | < 15% |
| Strategy Count | 5+ pluggable strategies |
| Backtest Runs | < 5 seconds for 1 year |
| Paper Trading Accuracy | > 80% vs backtest |
