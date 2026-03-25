# Sprint 2 Plan — Test Suite + Speed + Dashboard

> **Sprint:** 2 / Phase 1
> **Duration:** 7 days (2026-03-25 to 2026-04-01)
> **Depends on:** Sprint 1 ✅ (Phase Gate 1 approved)
> **Owner:** Backend Dev (primary), Frontend Dev (secondary), QA, PM/DevOps

---

## 1. Sprint 2 Goal & Scope

**Goal:** Ship a fully tested, fast, and usable quant backtesting system. Phase 1 is feature-complete after this sprint; the only remaining work is vnpy accuracy validation (Sprint 3).

**Out of Scope for Sprint 2:**
- vnpy comparison (deferred to Sprint 3 — requires vnpy environment)
- Dividend/split/index data sync (Phase 2, Sprint 4+)
- Strategy Copilot (Phase 2, Sprint 6)

---

## 2. Task Breakdown

### 2.1 Backend Dev

#### Task B1: Phase 1 Test Suite — Coverage Improvement
**Days: 1–2 | Depends on: nothing**

Increase coverage to exit-criteria levels. Prioritize by gap vs target.

| Package | Current Cov (est.) | Target | Delta | Notes |
|---------|-------------------|--------|-------|-------|
| `pkg/tracker/` | ~80% | >90% | +10pp | Add tests for boundary cases in T+1 + 涨跌停 |
| `pkg/backtest/` | ~60% | >85% | +25pp | Engine loop, order fill, NAV calculation, margin |
| `pkg/strategy/plugins/` | ~50% | >80% | +30pp | Strategy-specific signal generation |

**Done definition:**
```
go test -cover ./pkg/tracker/...    # > 90%
go test -cover ./pkg/backtest/...   # > 85%
go test -cover ./pkg/strategy/...   # > 80%
go test ./...                       # ALL PASS, FIRST principles
```

**Additional requirements:**
- Property-based edge cases: cash never negative, position quantities never negative, NAV monotonically traceable through every trade
- `t+1_enforcement_test.go`: cover sell-before-buy, same-day buy→sell blocked, partial sell depletes yesterday bucket first
- `zhangting_test.go`: add ST boundary (exactly ±5%), next-day gap reset, intra-day reversal off limit

---

#### Task B2: Speed Optimization to ≤5s
**Days: 2–4 | Depends on: B1 (coverage baseline stable)**

Speed target: `time go run ./cmd/analysis/main.go --universe 500 --start 2018 --end 2023` ≤ **5 seconds**

**Optimization stack (apply in order, measure after each):**

1. **Tracker goroutine parallelization** — each symbol's Position tracker runs in its own goroutine; channel-based aggregation back to engine. Expected: 3–4x speedup on 500-stock universe.

2. **Batched DB reads** — replace N single-row queries with batched `SELECT ... WHERE symbol IN (...)` and `WHERE trade_date BETWEEN`. Use single round-trip per batch instead of N round-trips.

3. **Pre-allocated slice buffers** — `make([]float64, 0, 500)` instead of `append` in hot loops (OHLCV bar aggregation).

4. **Redis cache integration** — confirm warmCache() covers the 5yr/500stock universe before timing run; cache TTL ≥ 24h. If cache miss >5% on benchmark run, investigate cold-start before reporting.

5. **Query prefetching** — on engine init, prefetch all OHLCV data for the backtest window into Redis (see warmCache coverage).

**Profiling before shipping:**
```bash
# CPU profile to identify remaining bottlenecks
go test -bench=. -benchmem -cpuprofile=cpu.prof ./pkg/backtest/...
go tool pprof cpu.prof
```

**Done definition:**
```bash
time go run ./cmd/analysis/main.go --universe 500 --start 2018 --end 2023
# Output: real ≤ 5.0s (use GNU time for accurate measurement: /usr/bin/time -f "%e")
```
> ⚠️ Requires real 5yr data in DB. If DB is empty, use fixture data. **Report actual data size used in test.**

---

#### Task B3: Regression Fixtures — 3 Fixtures, Zero Drift
**Days: 4–5 | Depends on: B2 (speed stable)**

All 3 fixtures must pass 3 consecutive runs with **0.00% drift**.

| Fixture | File | Universe | Period | Strategy | Acceptable Drift |
|---------|------|----------|--------|----------|-----------------|
| A | `fixture-5yr-500stock-momentum.json` | 500 stocks | 5yr | Momentum | 0.00% |
| B | `fixture-1yr-single-stock-value.json` | 1 stock | 1yr | Value | 0.00% |
| C | `fixture-t+1-enforcement.json` | 50 stocks | 1yr | Momentum | 0.00% |

**Fixture runner:**
```bash
go test ./pkg/backtest/... -run Regression -v
# Must print PASS for all 3 fixtures
# CI: exit code 1 on any fixture failure
```

**Drift check methodology:**
- Compare `total_return`, `annual_return`, `max_drawdown`, `sharpe_ratio` to fixture golden values
- Tolerance: exactly 0.00% for all — this is a determinism guarantee

---

#### Task B4: vnpy Setup + Comparison Methodology (support PM)
**Days: 5–7 | Depends on: B3 (fixtures stable)**

This is primarily a PM/DevOps task (see 2.3) but Backend Dev assists with:
- Writing the `compare_backtests.py` script skeleton (Python side: calls vnpy, extracts equity curve)
- Defining the Go-side output format so it matches what the Python script expects
- Providing a deterministic `backtest_output.json` that the Python script can diff against

---

### 2.2 Frontend Dev

#### Task F1: Dashboard UI Completion
**Days: 1–7 (parallel with other work) | Depends on: open items from Sprint 1**

> ⚠️ Before starting, Frontend Dev must resolve the **static file path issue** from Phase Gate 1:
> `./static/` vs `./cmd/analysis/static/` — pick one, document it, and commit.

**Dashboard panels to build:**

| # | Panel | Endpoint | Notes |
|---|-------|----------|-------|
| F1.1 | Portfolio overview (positions, cash, NAV) | `GET /api/v1/portfolio` | NAV chart over time |
| F1.2 | P&L display (daily, weekly, monthly, YTD) | `GET /api/v1/pnl?period=daily\|weekly\|monthly\|ytd` | Line chart + table |
| F1.3 | Position detail (unrealized/realized PnL, cost basis, weight) | `GET /api/v1/positions/:symbol` | Drilldown from overview |
| F1.4 | Recent trades list | `GET /api/v1/trades?limit=50` | Timestamp, symbol, side, qty, price |
| F1.5 | Risk indicators (volatility, max drawdown) | `GET /api/v1/risk` | Drawdown chart, Sharpe, volatility |
| F1.6 | Cache status panel (cache hit rate, warming status) | `GET /api/v1/cache/stats` | Backend Dev must expose this endpoint |

**Done definition:**
- All 6 panels render with real data (not stub JSON)
- No dead UI elements, no "coming soon" placeholders
- Responsive layout (min 1024px width)
- Phase Gate checklist item #8: **manual E2E test passes**

---

### 2.3 QA

#### Task Q1: Test Suite Expansion (mirrors B1)
**Days: 1–2 | Depends on: nothing (parallel with B1)**

QA independently writes and runs the invariant tests and boundary tests to cross-verify B1 results.

**Scope:**
- Cash invariant: `assert cash >= 0` after every trade
- Position invariant: `assert position.qty_yesterday >= 0 && position.qty_today >= 0`
- NAV trace: reconstruct NAV from trade log → compare to stored NAV at each step
- T+1 boundary: exactly 0 shares held yesterday, buy today → sell today must be blocked
- 涨跌停 boundary: exactly prev_close × 1.10 = limit_up_price (float equality check)

**Done:** QA reports coverage numbers independently. B1 + Q1 combined = full coverage.

---

#### Task Q2: Regression Fixture Validation
**Days: 5–6 | Depends on: B3**

QA runs the fixture suite and verifies zero drift across 3 consecutive runs. Reports actual drift numbers (not just pass/fail).

**Done:**
```bash
# Run 1
go test ./pkg/backtest/... -run Regression -v > fixture-run1.log
# Run 2
go test ./pkg/backtest/... -run Regression -v > fixture-run2.log
# Run 3
go test ./pkg/backtest/... -run Regression -v > fixture-run3.log
diff run1.log run2.log  # must be identical
diff run2.log run3.log  # must be identical
```

---

### 2.4 PM / DevOps

#### Task D1: vnpy Environment Setup
**Days: 5–7 | Depends on: B2 (speed stable)**

vnpy comparison is for Sprint 3, but the environment must be ready before Sprint 3 starts. Do not start until B2 confirms the speed is stable.

**Setup steps:**
1. Docker environment for vnpy (Python 3.9+, vnpy conda/pip install, or use `vnpy/docker/`)
2. CSI 300 reference data: obtain via tushare or other data provider
3. Run a reference backtest in vnpy with the same universe/period/strategy as the Go engine
4. Export equity curve to `vnpy_reference.csv`

**Done:** `python scripts/vnpy_reference_backtest.py --universe csi300 --start 2018 --end 2023` runs without error and produces `vnpy_reference.csv`.

---

#### Task D2: Speed Comparison Methodology
**Days: 5–7 | Depends on: B2**

Document the exact methodology for the speed test in `docs/SPEED_METRICS.md`:

1. **Environment:** macOS/Linux, M-series or equivalent, 16GB RAM minimum
2. **Data:** Actual 5yr OHLCV bars for 500 stocks in PostgreSQL
3. **Warm-up:** Run `POST /api/v1/cache/warm` before timing; confirm via cache stats endpoint
4. **Timing command:**
   ```bash
   /usr/bin/time -f "%e" go run ./cmd/analysis/main.go --universe 500 --start 2018 --end 2023
   ```
5. **Measurement:** Take best of 3 consecutive runs (exclude cold-start anomalies)
6. **Report format:** `{ "duration_seconds": X.XX, "data_size": "5yr×500stock", "cache_hit_rate": "XX%" }`

**Done:** `docs/SPEED_METRICS.md` exists and is referenced in `SPRINT2_PLAN.md` exit criteria.

---

#### Task D3: Phase Gate Checklist Update
**Days: 7**

Update `docs/phase-gate-reviews.md` with Sprint 2 results. Mark items green or document blockers.

---

## 3. Dependencies

```
B1 (Coverage) ──────────────┐
                            ├── B2 (Speed) ─── B3 (Fixtures) ─── D1 (vnpy setup)
F1 (Dashboard) ─────────────┤                  │
                            │                  └─ D2 (Speed Method)
Q1 (QA Coverage) ───────────┤
                            │
Q2 (Fixture Validation) ────┴── (depends on B3)
                              │
D3 (Phase Gate Update) ───────┴── (end of sprint)
```

**Critical path:** B1 → B2 → B3 → Sprint 3 (vnpy)
**Parallel tracks:**
- Track A (Backend): B1 → B2 → B3 → D1
- Track B (Frontend): F1 (independent after path resolution)
- Track C (QA): Q1 (parallel with B1) → Q2 (after B3)

**No blocking dependencies for F1** — Frontend Dev starts Day 1 after confirming static file path resolution with Backend Dev.

---

## 4. Sprint 2 Exit Criteria

| # | Criterion | Method | Target | Owner |
|---|-----------|--------|--------|-------|
| 1 | All unit tests pass | `go test ./...` | 0 failures | Backend Dev |
| 2 | Tracker coverage | `go test -cover ./pkg/tracker/...` | > 90% | Backend Dev |
| 3 | Backtest coverage | `go test -cover ./pkg/backtest/...` | > 85% | Backend Dev |
| 4 | Strategy plugins coverage | `go test -cover ./pkg/strategy/...` | > 80% | Backend Dev |
| 5 | Speed benchmark | `time go run ./cmd/analysis/main.go --universe 500 --start 2018 --end 2023` | ≤ 5.0s | Backend Dev |
| 6 | Regression fixtures | `go test ./pkg/backtest/... -run Regression` | 3/3 PASS, 0.00% drift | QA |
| 7 | Dashboard E2E | Manual test by PM | All 6 panels wired + data visible | Frontend Dev + PM |
| 8 | Cache stats endpoint | `GET /api/v1/cache/stats` returns JSON | 200 OK, valid JSON | Backend Dev |
| 9 | vnpy environment | `python scripts/vnpy_reference_backtest.py` | Runs without error | PM/DevOps |
| 10 | Speed methodology doc | `docs/SPEED_METRICS.md` exists | Complete | PM/DevOps |
| 11 | Phase Gate review updated | `docs/phase-gate-reviews.md` Sprint 2 entry | Dated entry added | PM |

**Exit gate:** All 11 criteria green → Sprint 3 can begin.
**If any red:** Block Sprint 3 kickoff until resolved.

---

## 5. Risks & Mitigations

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| Speed target <5s not met | Medium | High | Parallelization is the main lever; if still >5s after B2, escalate to PM immediately — vnpy comparison can still start with a note |
| Dashboard static path not resolved | Low | Medium | Frontend Dev picks `./cmd/analysis/static/` on Day 1; if conflict, PM arbitrates |
| vnpy data access fails | Medium | Medium | Use synthetic data or subset universe (10-stock) to validate methodology before full run |
| Regression fixtures drift on CI | Low | High | Ensure CI environment matches dev (same seed, same Go version) |

---

## 6. Daily Cadence

| Day | Focus | Key Milestone |
|-----|-------|---------------|
| Day 1 | Coverage (B1/Q1) + Dashboard F1 | Static path resolved; tracker tests at target |
| Day 2 | Coverage (B1) + Dashboard F1 | Coverage targets met |
| Day 3 | Speed optimization (B2) | Speed profiling complete; warmCache validated |
| Day 4 | Speed optimization (B2) | First ≤5s run achieved |
| Day 5 | Fixtures (B3) + vnpy setup (D1) | All 3 fixtures passing; vnpy environment running |
| Day 6 | Fixture validation (Q2) + speed method (D2) | Zero-drift confirmed; methodology doc drafted |
| Day 7 | Buffer + Phase Gate update (D3) | Sprint 2 signed off; Sprint 3 kickoff |

---

_Plan authored by: 龙少 (PM) — 2026-03-25_
