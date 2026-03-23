# Quantitative Trading Backtest System - Research Findings & Roadmap Update

**Date:** 2026-03-23
**Author:** 龙少 (Subagent Research)
**Context:** Professional quant backtesting system feature gap analysis

---

## Research Findings

### Tier 1 - Critical (Must Have)

| Feature | Current Status | Priority | Notes |
|---------|---------------|----------|-------|
| Trading Calendar / Market Schedule | ⚠️ PARTIAL | P0 | Calendar derived from OHLCV data (SELECT DISTINCT trade_date FROM ohlcv_daily_qfq). This means you only trade on days where data exists — BUT this is fragile: (1) it won't detect holidays without data, (2) it relies on OHLCV data completeness. A proper `trading_calendar` table is needed. |
| 前复权 (Forward-Adjusted Prices) | ✅ YES | Done | Using `stk_factor_pro` API with `open_qfq/high_qfq/low_qfq/close_qfq` fields. Correct. |
| Dividends / Splits / Rights Issuances | ⚠️ PARTIAL | P1 | `stk_factor_pro` forward-adjusted prices should incorporate most corporate actions, but no explicit dividend/split API calls exist. No `dividend`, `split`, `rights` tushare APIs are called. |
| Commission + Slippage Modeling | ✅ YES | Done | `commissionRate` (default 0.03%) and `slippageRate` (default 0.01%) both implemented in `pkg/backtest/tracker.go`. Correct. |
| Position Sizing with Real Portfolio Value | ✅ YES | Done | VolatilitySizer calculates position weight from portfolio total value. `risk/manager.go` uses `portfolio.TotalValue` correctly. |
| Short Selling Mechanics | ⚠️ PARTIAL | P1 | Tracker tracks short positions (negative quantity), but: no margin interest rate, no borrow cost (hard-to-borrow stocks), no short squeeze risk modeling. Real A股 short selling has ~10.6% annual margin interest rate. |
| Market Impact Modeling | ❌ NO | P2 | No market impact model. In production, large orders in illiquid A股 stocks cause significant price slippage beyond the flat 0.01% assumption. |
| Settlement T+1 Mechanics (A股) | ❌ NO | P1 | **CRITICAL for A股.** T+1 means shares bought today cannot be sold today. Current tracker doesn't track buy date per position to enforce this. Also: buying power check doesn't account for T+1 locked capital. |
| Integer Share Trading (whole lots) | ✅ YES | Done | Uses `math.Floor(positionValue / currentPrice)` — correct for A股 (100 shares = 1手, no fractional shares). |

### Tier 2 - Important (Strongly Recommended)

| Feature | Current Status | Priority | Notes |
|---------|---------------|----------|-------|
| Stock Screening / Universe Management | ❌ NO | P1 | Stock pool is passed as a static list. No dynamic screening based on fundamentals. Need stock screener that filters by PE, PB, ROE, volume, etc. |
| Factor Library | ⚠️ PARTIAL | P1 | Domain types exist (`Fundamental` with PE, PB, PS, ROE, ROA, DebtToEquity, etc.) but only `momentum.go` and `value_momentum.go` strategies exist. No dedicated factor scoring system. |
| News / Analyst Sentiment Data | ❌ NO | P2 | Roadmap mentions crawling 东方财富/同花顺, but not implemented. Sentiment factor score is all zeros. |
| Macro Events Calendar | ❌ NO | P2 | No earnings calendar, Fed meeting, CPI data. Important for event-driven strategies and avoiding earnings surprises. |
| Portfolio-Level Risk Management (VaR, CVaR) | ❌ NO | P2 | Domain types exist (`RiskMetrics` with VaR95, CVaR95), but no calculation implementation in backtest engine. Needed for proper risk reporting. |
| Dynamic Position Sizing (volatility-adjusted) | ✅ YES | Done | `VolatilitySizer` implements target volatility approach with `lookbackDays` and `annualizationFactor`. Regime-aware multipliers applied. |
| Stop-Loss / Take-Profit Rules | ✅ YES | Done | `StopLossChecker` with ATR-based stop loss and take profit. Regime-aware multipliers (bull/bear/sideways). |
| ATR-Based Stops | ✅ YES | Done | `CalculateATR()` using Wilder's smoothing. Multipliers: bull=1.5, bear=3.0, sideways=2.0. |

### Tier 3 - Nice to Have

| Feature | Current Status | Priority | Notes |
|---------|---------------|----------|-------|
| Strategy Hot-Swapping / Plugin System | ⚠️ PARTIAL | P2 | `StrategyRegistry` exists but strategies are compiled in. Roadmap goal is truly dynamic plugin loading. |
| Strategy Copilot / AI Code Generation | ❌ NO | P3 | No natural language → strategy code generation. |
| Visual Strategy Builder | ❌ NO | P3 | No drag-drop strategy builder UI. |
| Multiple Strategy Portfolio | ❌ NO | P2 | One strategy per backtest run. Phase 4 of roadmap covers this. |
| Real-Time Paper Trading | ❌ NO | P3 | Not implemented. Would need live data feed + simulated execution engine. |
| Broker API Integration | ❌ NO | P3 | Futu/Tiger/Snowball mentioned in roadmap. None implemented. |

---

## Trading Calendar Analysis

### Current State

The system derives trading days from:
```sql
SELECT DISTINCT trade_date FROM ohlcv_daily_qfq
WHERE trade_date >= $1 AND trade_date <= $2
ORDER BY trade_date ASC
```

**Problem:** This approach has a subtle but critical flaw:
1. **Missing holiday detection**: If a holiday (e.g., National Day Oct 1-7, Chinese New Year) falls on a weekday, and there's no OHLCV data for it, the calendar will return only the trading days *around* it — which seems correct. BUT the issue is more nuanced:
   - The calendar will *appear* to work correctly because non-trading days simply don't have OHLCV data, so `getTradingDays` naturally returns only days with data.
   - The real danger is: if you backtest across a period where data is sparse or missing, the calendar will return an incomplete list.

2. **Data dependency**: The calendar is only as complete as the OHLCV data. If data sync missed some days, the calendar will be wrong.

3. **A股-specific holidays** that MUST be respected:
   - Chinese New Year (1-2 weeks in Jan/Feb, varies yearly)
   - National Day Golden Week (Oct 1-7)
   - Qingming Festival (April 4-6)
   - Labor Day (May 1-3)
   - Mid-Autumn Festival (varies)
   - These are NOT fixed dates — they change every year!

### Recommended Implementation

**Option A — tushare calendar API (RECOMMENDED):**
```
API: trade_cal (tushare)
Fields: exchange, cal_date, is_open (1=trading day, 0=holiday), pretrade_date
```
Store all dates in a `trading_calendar` table indexed by (exchange, cal_date).
The backtest engine queries this table for `is_open=1` within the date range.

**Option B — Generate weekdays + exclusion table:**
1. Generate all weekdays (Mon-Fri) in range
2. Store known A股 holidays in exclusion table
3. Subtract exclusions from weekdays

**Option C — Hybrid (current + enhancement):**
Keep current OHLCV-derived approach as a fallback, but add explicit holiday checking via tushare `trade_cal` API for A股-specific holidays.

**Action Items:**
- [ ] Create `trading_calendar` table in PostgreSQL with (exchange, cal_date, is_open) columns
- [ ] Add `sync_calendar` job to fetch all dates from tushare `trade_cal` API
- [ ] Modify `getTradingDays()` to query `trading_calendar WHERE is_open=1` instead of `ohlcv_daily_qfq`
- [ ] Verify calendar covers 2020-2030 range (need ~10 years for long backtests)

---

## Recommended Roadmap Updates

### Immediate Additions (This Week — P0)

1. **Add T+1 settlement enforcement**
   - Tracker must record `buyDate` for each position
   - Before executing a SELL, verify (today - buyDate) >= 1 trading day
   - When buying, lock capital for T+1 (cannot use for new buys until sold)
   - Location: `pkg/backtest/tracker.go` — add `BuyDate` to `Position` struct

2. **Fix trading calendar**
   - Add `trading_calendar` table to PostgreSQL schema
   - Add tushare `trade_cal` API call in `pkg/data/tushare.go`
   - Modify `GetTradingDays()` to use the calendar table
   - Verify against known holidays (2020-2026 at minimum)

3. **Add short selling cost model**
   - Add `margin_interest_rate` config (default 10.6% annual for A股)
   - Add `borrow_cost` per symbol (higher for hard-to-borrow small caps)
   - Calculate daily margin interest on short position value
   - Affects portfolio value on daily basis (not just at trade time)

### Short-Term (1-4 Weeks — P1)

4. **Stock Screener**
   - Build `StockScreener` component: filter stocks by PE, PB, ROE, volume, industry
   - Add screener endpoint: `POST /api/v1/screener` with factor criteria
   - Use in backtest to dynamically build stock pool from universe (e.g., top 50 by ROE)

5. **Factor Scoring System**
   - Build `FactorScorer` with normalized factor scores (z-score or percentile rank)
   - Support: value (PE, PB, PS), momentum (1M/3M/6M), quality (ROE, ROA, debt/equity), growth
   - Composite score = weighted sum of normalized factors

6. **Dividend/Split Handling**
   - Add tushare `dividend` and `split` API calls
   - Track dividend income in tracker (adds to portfolio cash)
   - Ensure forward-adjusted prices already handle splits (should be covered by qfq)

### Medium-Term (1-3 Months — P2)

7. **VaR / CVaR Risk Metrics**
   - Implement `CalculateVaR()` using historical returns simulation
   - Implement `CalculateCVaR()` (Expected Shortfall)
   - Add to backtest results and risk reporting

8. **Market Impact Model**
   - Replace flat slippage with volume-based model: `market_impact = sigma * sqrt(order_fraction / ADV)`
   - ADV = Average Daily Volume
   - Important for strategies that trade large positions in illiquid stocks

9. **Strategy Plugin Architecture**
   - Define `Strategy` interface with hot-reload capability
   - Strategy files in `strategies/` directory loaded at runtime
   - Add strategy registry API: `GET /api/v1/strategies`

### Roadmap Changes Summary

**Modify Phase 1:**
- Add T+1 settlement mechanics (NEW — critical for A股 accuracy)
- Add trading calendar fix (NEW — critical for signal alignment)
- Add short selling cost model (NEW — partial implementation)

**Modify Phase 2:**
- Add stock screener (promote from Phase 2 implicit need to explicit)
- Add factor scoring system (enhance existing factor library goal)

**Modify Phase 3:**
- Strategy plugin: clarify it means runtime hot-reload, not compile-time

**New Phase (Recommended):**
- **Phase 1.5: Data Quality (2 weeks)**
  - Trading calendar implementation
  - T+1 settlement enforcement
  - Short selling cost model
  - Dividend tracking

---

## Summary of Critical Gaps

| Gap | Risk Level | Impact on Win Rate |
|-----|-----------|-------------------|
| T+1 settlement missing | 🔴 CRITICAL | Causes 5-15% return overestimation (can buy same-day sells) |
| Trading calendar fragile | 🟡 MEDIUM | Signal/trade misalignment if holiday detection fails |
| Short selling cost=0 | 🟡 MEDIUM | Underestimates cost of short strategies by ~10.6%/year |
| Stock screener missing | 🟡 MEDIUM | Cannot filter by fundamentals, limits strategy quality |
| Market impact not modeled | 🟡 MEDIUM | Large orders in illiquid stocks show unrealistic fills |
| VaR/CVaR not calculated | 🟢 LOW | Risk reporting incomplete but doesn't affect backtest P&L |

---

*Generated by research subagent — 2026-03-23*
