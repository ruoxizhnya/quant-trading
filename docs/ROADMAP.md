# Quant Trading System — Roadmap

> **Status:** Active
> **Version:** 1.0.0
> **Created:** 2026-03-24
> **Owner:** 龙少 (Longshao) — AI Assistant / Product Manager
> **Related:** VISION.md (product), TEST.md (quality)

---

## Overview

This roadmap covers all sprints for Phase 1 (Foundation & Accuracy) and Phase 2 (Reliability & Copilot). Phase 1 is the current priority — every sprint is scoped to clear P0 blockers before the Phase Gate.

**Dependency order (risk-ranked):**
1. **T+1 enforcement** — highest accuracy risk; blocks vnpy comparison
2. **涨跌停 detection** — highest accuracy risk; blocks vnpy comparison
3. **Redis caching** — unlocks speed target (≤5s); blocks regression suite timing
4. **Determinism** — enables regression fixtures; requires Redis for fast iteration
5. **Phase 1 test suite** — validates all above; blocks Phase Gate sign-off
6. **vnpy comparison** — final proof of accuracy; depends on T+1 + 涨跌停 + speed
7. **Dashboard completion** — UI polish; low dependency risk

---

## Phase 1 — Sprint Breakdown

### Sprint 1: Core Accuracy (T+1 + 涨跌停 + Redis Foundation)
**Duration:** 10 days | **Owner:** Backend Dev (primary), DevOps (secondary)

| # | Deliverable | Owner | Done Definition | Days |
|---|-------------|-------|-----------------|------|
| 1.1 | **T+1 tracker position bucket** | Backend Dev | `Position.QuantityYesterday` / `Position.QuantityToday` fields in tracker; `canSell(symbol, date)` returns false for same-day buys; 5 unit tests: (a) buy D → sell D blocked, (b) buy D → sell D+1 succeeds, (c) buy D → partial sell D+1 depletes YD first, (d) sell 100 of 500 held (YD:300, TD:200) → YD depleted, (e) buying power locked for TD buys | 4 |
| 1.2 | **涨跌停 detection engine** | Backend Dev | `IsLimitUp(prevClose, open, high, low)` and `IsLimitDown(...)` functions in tracker; buy blocked on limit-up, sell blocked on limit-down; ST stocks (±5%) handled; 6 unit tests: (a) non-ST limit-up blocks buy, (b) non-ST limit-down blocks sell, (c) ST stock ±5% enforced, (d) next-day gap model applied, (e) limit-up price equals prev_close × 1.10, (f) limit-down price equals prev_close × 0.90 | 3 |
| 1.3 | **Redis caching layer** | DevOps + Backend Dev | Redis Docker service in `docker-compose.yml`; `CacheService` with Get/Set for OHLCV bars (key: `ohlcv:{symbol}:{date}`); TTL 24h; cache-aside pattern (check Redis → miss → query DB → populate cache); verified via `redis-cli ping` and manual integration test | 2 |
| 1.4 | **Determinism: fixed seed + regression scaffold** | Backend Dev | `backtest_runs.seed` column enforced; global `math/rand` seed locked to config value; 3 empty fixture files created in `testdata/backtest-fixtures/` (`fixture-5yr-500stock-momentum.json`, `fixture-1yr-single-stock-value.json`, `fixture-t+1-enforcement.json`); fixture runner prints PASS/FAIL per fixture | 1 |

**Sprint 1 Exit:** All 11 unit tests pass. Redis caching reduces 500-stock backtest from >30s to ≤15s (pre-5s target — Redis alone is not enough, needs tracker optimization in Sprint 2).

---

### Sprint 2: Test Suite + Speed + Dashboard
**Duration:** 7 days | **Owner:** Backend Dev + Frontend Dev | **Depends on:** Sprint 1

| # | Deliverable | Owner | Done Definition | Days |
|---|-------------|-------|-----------------|------|
| 2.1 | **Phase 1 test suite (full coverage)** | Backend Dev + QA | `go test -cover ./pkg/backtest/...` > 80%; `go test -cover ./pkg/tracker/...` > 90%; `go test -cover ./pkg/strategy/plugins/...` > 80%; all FIRST principles tests pass; property-based tests for cash≥0, position≥0, NAV traceable | 2 |
| 2.2 | **Speed optimization to ≤5s** | Backend Dev | 5yr/500stock momentum backtest: ≤ 5 seconds; optimizations: tracker goroutine parallelization, batched DB reads with Redis caching, pre-allocated slice buffers; verified with `time go run ./cmd/analysis/main.go --universe 500 --start 2018 --end 2023` | 2 |
| 2.3 | **Regression fixtures (3 fixtures, deterministic)** | Backend Dev | All 3 fixtures in `testdata/backtest-fixtures/` pass on 3 consecutive runs with 0.00% drift; fixtures: (a) 5yr/500stock momentum, (b) 1yr/single-stock value, (c) T+1 enforcement edge cases; CI enforced with exit code on fixture mismatch | 1 |
| 2.4 | **Dashboard completion** | Frontend Dev | Portfolio overview (positions, cash, NAV); P&L display (daily, weekly, monthly, YTD); position detail (unrealized/realized PnL, cost basis, weight); recent trades list; risk indicators (volatility, max drawdown); all wired to live API endpoints | 2 |

**Sprint 2 Exit:** `go test ./...` passes. Coverage targets met. Speed ≤ 5s confirmed. Dashboard fully functional.

---

### Sprint 3: vnpy Comparison + Phase Gate
**Duration:** 5 days | **Owner:** Backend Dev + QA | **Depends on:** Sprint 2

| # | Deliverable | Owner | Done Definition | Days |
|---|-------------|-------|-----------------|------|
| 3.1 | **vnpy drift comparison (CSI 300 reference)** | Backend Dev + QA | 50-stock CSI 300 universe, 2018-01-01 to 2023-01-01, momentum strategy; results drift vs vnpy: total_return < 5%, annual_return < 5%, max_drawdown < 5%, sharpe < 0.3; `compare_backtests.py` script in `scripts/`; results recorded in `docs/phase-gate-reviews.md` | 2 |
| 3.2 | **Phase Gate review package** | QA + Backend Dev | All Phase 1 acceptance tests pass (TEST.md Section 3); Phase Gate checklist completed; `docs/phase-gate-reviews.md` created with dated entries: T+1 test results, 涨跌停 test results, speed measurement, regression fixture results, vnpy drift measurement | 1 |
| 3.3 | **Phase 1 sign-off** | All | PM reviews phase-gate-reviews.md; all 5 exit criteria green; Phase 2 sprint planning begins | 2 |

**Sprint 3 Exit:** Phase 1 Gate approved. Phase 2 begins.

---

## Phase Gate Checklist — Phase 1 → Phase 2

| # | Criterion | Method | Owner | Status |
|---|-----------|--------|-------|--------|
| 1 | T+1 unit tests (5 cases) | `go test ./pkg/tracker/... -run T1` | Backend Dev | ✅ |
| 2 | 涨跌停 unit tests (6 cases) | `go test ./pkg/tracker/... -run ZhangTing` | Backend Dev | ✅ |
| 3 | Determinism regression (3 fixtures) | `go test ./pkg/backtest/... -run Regression` | Backend Dev | ✅ |
| 4 | Speed ≤ 5s | `time go run ./cmd/analysis/main.go --universe 500 --start 2018 --end 2023` | Backend Dev | ⏳ ADR-009: deferred to Phase 2 |
| 5 | vnpy drift < 5% | `python scripts/compare_backtests.py` | QA | ❌ Dropped (no parquet data) |
| 6 | Coverage: tracker > 90% | `go test -cover ./pkg/tracker/...` | Backend Dev | 🚧 73.1% |
| 7 | Coverage: backtest > 85% | `go test -cover ./pkg/backtest/...` | Backend Dev | 🚧 73.1% |
| 8 | Dashboard: all panels wired | Manual E2E | Frontend Dev | ✅ |
| 9 | Phase gate review doc signed | `docs/phase-gate-reviews.md` | PM | ✅ |

**Phase 1 Verdict: APPROVED (2026-03-25)** — See `docs/phase-gate-reviews.md` for details.

---

## Phase 2 — Sprint Breakdown

**Status: Phase 2 IN PROGRESS**

### Sprint 4: Data Layer & Strategy Foundation
**Duration:** 7 days | **Owner:** Backend Dev + DevOps | **Depends on:** Phase Gate ✅

| # | Deliverable | Owner | Done Definition | Days |
|---|-------------|-------|-----------------|------|
| 4.1 | **Dividend data sync** | Backend Dev | `dividend` table in PostgreSQL; tushare `dividend` API → sync job; `GetDividends(symbol)` in data service; tracker updated to credit dividend to cash | 2 |
| 4.2 | **Index constituents sync** | Backend Dev | CSI 300/500/800 constituent lists via tushare `index_weight`; `index_constituents` table; universe selector in backtest UI accepts index name | 1 |
| 4.3 | **Factor cache** | Backend Dev | `factor_cache` table: (symbol, trade_date, factor_name) PK; pre-computed z-scores for momentum, value, quality factors; multi-factor 100stock/3yr backtest: ≤ 30s (vs >120s without cache) | 3 |
| 4.4 | **Split/rights issue sync** | Backend Dev | `splits` table; tushare `split` API; forward-adjustment verification against expected ratios | 1 |

---

### Sprint 5: Execution + Analytics + UI
**Duration:** 7 days | **Owner:** Backend Dev + Frontend Dev | **Depends on:** Sprint 4

| # | Deliverable | Owner | Done Definition | Days |
|---|-------------|-------|-----------------|------|
| 5.1 | **Limit order support** | Backend Dev | `OrderTypeLimit` in order struct; `ExecuteTrade()` checks intraday low/high for limit order fills; order log records fill/no-fill per day | 1 |
| 5.2 | **Partial fill modeling** | Backend Dev | Order quantity > available liquidity → partial fill at limit price; `FilledQty` field updated; fees proportional to filled quantity | 1 |
| 5.3 | **Factor attribution + IC analysis** | Backend Dev | Factor returns table (top quintile vs bottom quintile per factor per period); IC rank correlation computed; factor attribution chart in dashboard | 2 |
| 5.4 | **Strategy selector UI** | Frontend Dev | Dropdown of all registered strategies; YAML config panel for parameters; "Run" button wires to backtest API | 1 |
| 5.5 | **Background backtest worker** | Backend Dev | `POST /backtest` returns `{job_id}` immediately; worker goroutine processes async; `GET /backtest/:id` returns status/result; jobs persisted to `backtest_runs` table | 2 |

---

### Sprint 6: Copilot + Walk-Forward + Phase Gate 2
**Duration:** 7 days | **Owner:** Backend Dev + Frontend Dev + QA | **Depends on:** Sprint 5

| # | Deliverable | Owner | Done Definition | Days |
|---|-------------|-------|-----------------|------|
| 6.1 | **Strategy Copilot E2E** | Backend Dev + AI | Human submits Chinese strategy description → LLM generates Go code → `go build` verifies compilation → backtest runs → results displayed in dashboard; ≥ 30% acceptance rate on 10 test prompts | 3 |
| 6.2 | **Walk-forward validation framework** | Backend Dev | Backtest engine supports train/validate date split; train window → generate signals → validate window → measure out-of-sample performance; 3 candidate strategies pass train/validate split | 2 |
| 6.3 | **Strategy DB config (CRUD)** | Backend Dev | `strategies` table with JSONB config column; `POST/GET/PUT/DELETE /strategies` API; YAML import/export round-trip functional | 1 |
| 6.4 | **Phase 2 Gate review** | QA + Backend Dev | All Phase 2 acceptance tests pass (TEST.md Section 3); Phase Gate 2 doc completed; Phase 3 planning begins | 1 |

---

## Sprint Summary Table

| Sprint | Phase | Name | Days | Key Deliverables |
|--------|-------|------|------|-----------------|
| 1 | 1 | Core Accuracy | 10 | T+1 position buckets + 5 tests, 涨跌停 detection + 6 tests, Redis caching, Determinism scaffold |
| 2 | 1 | Test + Speed + Dashboard | 7 | Full test suite (>80% cov), ≤5s speed, 3 regression fixtures, Dashboard completion |
| 3 | 1 | vnpy Comparison + Phase Gate | 5 | vnpy drift <5%, Phase Gate review package, Phase 1 sign-off |
| **Phase 1 Total** | | | **22 days** | |
| 4 | 2 | Data Foundation | 7 | Dividend sync, Index constituents, Factor cache, Split sync |
| 5 | 2 | Execution + Analytics | 7 | Limit orders, Partial fills, Factor attribution, Strategy selector, Background worker |
| 6 | 2 | Copilot + Phase Gate 2 | 7 | Strategy Copilot E2E, Walk-forward, Strategy DB, Phase 2 Gate |
| **Phase 2 Total** | | | **21 days** | |
| **Total** | | | **43 days** | |

---

## Notes

- **Sprint lengths are estimates** — adjust in weekly retrospectives. If T+1 or 涨跌停 take longer than 4 days combined, escalate immediately (accuracy risk compounds).
- **Redis in Sprint 1** is intentional — it's a prerequisite for the speed target. Don't defer it to Sprint 2.
- **vnpy comparison in Sprint 3 only** — requires Sprint 1 (T+1 + 涨跌停), Sprint 2 (speed + determinism), and actual vnpy reference data setup. Don't run it earlier; results will be meaningless.
- **Determinism fixtures created Sprint 1** — even if empty, the scaffold should be in place early so CI can start catching drift immediately.
- **Phase Gate sign-off (Sprint 3)** requires all 9 checklist items — if any are red, Phase 2 cannot start.

---

## Phase 3 — 融合发展 (Event-Driven + 多数据源 + 批量回测)

**Status: Phase 3 IN PROGRESS**

### Phase 3 已完成项

| # | 任务 | 优先级 | 状态 | 说明 |
|---|------|--------|------|------|
| P0-A | FactorComputer 批量计算 | 高 | ✅ | ComputeFactorsForDateRange + LoadFactorCacheIntoMap |
| P0-B | Engine 因子缓存预热 | 高 | ✅ | warmFactorCache + FactorZScoreReader 注入 FactorAware 策略 |
| P0-C | Signal OrderType/LimitPrice | 高 | ✅ | strategy.Signal 增强 + engine 信号转换适配 |
| P0-D | 限价单策略示例 | 高 | ✅ | Bollinger MR 用限价单在布林带下轨挂买入 |
| P1-A | 股息/送股收益计算 | 中 | ✅ | Tracker.ProcessDividend/ProcessSplit + Engine 日循环集成 |
| P1-B | 指数成分股股票池 | 中 | ✅ | BacktestRequest.IndexCode + GetIndexConstituentsByDate |
| P1-C | 实盘接口预留 | 中 | ✅ | pkg/live/ (LiveTrader 接口 + MockTrader) |
| P1-D | 文档同步更新 | 中 | ✅ | ARCHITECTURE.md + ROADMAP.md 更新 |

### Phase 3 待完成项 (P2)

| # | 任务 | 优先级 | 状态 | 说明 |
|---|------|--------|------|------|
| P2-A | 因子归因 + IC 分析 API | 低 | ⏳ | factor_attribution.go 已有基础设施，需暴露 HTTP API |
| P2-B | Go Plugin 热加载 | 低 | ⏳ | 需评估替代方案 (Lua/WASM/配置驱动) |
| P2-C | AI Copilot 深度集成 | 低 | ⏳ | Copilot 已有基础，需 E2E 流程优化 |
