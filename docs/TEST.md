# Test Plan & Quality Assurance (TEST.md)

> **Location:** Quality assurance and testing methodology for the quant trading system
> **Owner:** 龙少 (Longshao) — AI Assistant
> **Version:** 1.0.0
> **Created:** 2026-03-24
> **Related:** VISION.md (product), ADR.md (architecture)

---

## 1. Testing Philosophy

We apply the **FIRST** principles to all testing:
- **Fast** — Unit tests run in < 1s; backtest regression suite in < 30s
- **Independent** — No test depends on another; tests can run in any order
- **Repeatable** — Same results every time (deterministic, fixed seeds)
- **Self-validating** — Tests pass or fail automatically; no manual inspection required
- **Timely** — Tests written alongside code, not after

**Core invariant tests (never allowed to fail):**
```
cash ≥ 0                          // Never spend more than you have
position_quantity ≥ 0            // Never hold negative shares
nav = cash + Σ(position_value)   // NAV is always traceable
all_trades.have_fee > 0          // Every trade has fees recorded
all_trades.have_timestamp        // Every trade is time-stamped
```

---

## 2. Test Categories

### 2.1 Unit Tests — `pkg/*`

**Strategy Layer:**
- Register mock strategy → resolve by name → verify `GenerateSignals` signature
- Feed known bar series to momentum/value/multi-factor → verify signal direction
- Property: `cash ≥ 0` after every signal generation
- Property: signal weights sum to ≤ 1.0 (no leverage)

**Execution Layer (Tracker):**
- `Signal → Order → Tracker → Position` pipeline
  - Buy 1000 shares → `Position.Quantity == 1000`, `AvgCost` correct
  - Sell 500 → remainder correct
- **T+1 settlement (P0, critical):**
  - Buy on D → attempt sell on D → **blocked** ✅
  - Buy on D → sell on D+1 → **succeeds** ✅
  - Buy on D → sell portion on D+1 → only YD bucket depleted ✅
  - Sell 100 of 500 held (YD: 300, TD: 200) → YD depleted first ✅
- **涨跌停 detection (P0, critical):**
  - Inject mock bar: `(high - prev_close)/prev_close >= 0.10` → buy **blocked** ✅
  - Inject mock bar: `(low - prev_close)/prev_close <= -0.10` → sell **blocked** ✅
  - ST stock with `(high - prev_close)/prev_close >= 0.05` → buy **blocked** ✅
  - Next day after limit-up: open at limit price → verify gap model ✅
- **Commission + stamp tax:**
  - 3 trades (buy, sell, sell) with various sizes → stamp tax **only on sell legs** ✅
  - Very small buy trade → minimum 5 CNY floor applied ✅
  - Transfer fee on both sides verified ✅
- **Slippage:** configured rate applied to every fill ✅
- **Buying power:** order value > available cash → **rejected** ✅
- **Integer rounding:** 183 shares → rounds to 100; 99 shares → **rejected** ✅
- **Partial fill:** available cash covers 50% of order → 50% fill ✅

**Analytics Layer:**
- Feed fixed OHLCV fixture → compute Sharpe, max drawdown, win rate → compare to expected values
- Equity curve: NAV monotonically related to positions + cash ✅

**Data Layer:**
- OHLCV continuity across corporate actions (前复权)
- `stock_fundamentals` schema: required fields non-null
- Trading calendar: CNY 2024 holiday → `is_open=false`

---

### 2.2 Integration Tests

**Service Communication:**
- `analysis-service → data-service`: HTTP call returns OHLCV for known date range
- `analysis-service → risk-service`: signal weight adjustment returns within timeout
- Health endpoints return 200 on all services

**Data Pipeline:**
- Tushare sync → PostgreSQL → query returns same data (end-to-end integrity)
- Factor cache: pre-computed z-scores match on-the-fly computation

**Backtest Full Pipeline:**
```
Fixed seed + known universe → backtest → equity curve + trade log
↓
Compare to stored fixture ( fixture.json )
↓
Any deviation > 0.01% → FAIL
```

---

### 2.3 Regression Suite

**Purpose:** Ensure backtest results are deterministic. Same inputs → same outputs forever.

**Mechanism:**
- `backtest_runs` table stores `seed` column
- Golden fixtures stored in `testdata/backtest-fixtures/` (JSON files with known-good NAV curves)
- CI runs: `go test ./pkg/backtest/... -update-fixtures=false` against fixtures

**Fixtures to create (Phase 1 before exit):**
- [ ] `fixture-5yr-500stock-momentum.json` — expected NAV curve, total return, Sharpe, max drawdown
- [ ] `fixture-1yr-single-stock-value.json` — single stock, value strategy
- [ ] `fixture-t+1-enforcement.json` — T+1 edge cases
- [ ] `fixture-zhangting-detection.json` — 涨跌停 boundary cases

**Running the suite:**
```bash
# Run all tests
go test ./...

# Run regression suite only
go test ./pkg/backtest/... -run Regression

# Update fixtures (after verifying new results are correct)
go test ./pkg/backtest/... -run Regression -update-fixtures=true
```

---

### 2.4 Property-Based Tests

Using `testing/quick` or `golang/mock`:

| Property | Test |
|----------|------|
| Cash never negative | Generate random trades; cash always ≥ 0 |
| Position never negative | Generate random buy/sell sequences; quantity always ≥ 0 |
| NAV traceable | NAV = cash + Σ(position_value) at every step |
| Fees always positive | Every trade has fee > 0 |
| T+1 enforced always | 1000 random buy-sell sequences; same-day sells always blocked |

---

### 2.5 Chaos / Reliability Tests

**What to test (Phase 2+):**
- Tushare API fails mid-sync → partial data handled gracefully; retry with backoff
- Redis goes down mid-backtest → fallback to PostgreSQL query (slower, but works)
- Risk service timeout → circuit breaker opens; backtest continues without risk adjustment (fail-safe)
- Docker service crash → health check detects; restart triggered

---

## 3. Phase Gate Tests

These are the **acceptance tests** that must pass before advancing phases. Recorded in `docs/phase-gate-reviews.md`.

### Phase 1 Gate (before Phase 2)

| Test | Pass Criterion |
|------|----------------|
| T+1 unit tests | All 5 T+1 cases (above) pass |
| 涨跌停 unit tests | All boundary cases (ST ±5%, gap model) pass |
| Determinism regression | 3 fixtures produce identical results across 3 consecutive runs |
| vnpy drift comparison | 5yr/500stock backtest vs vnpy: < 5% drift on same universe/dates/rebalancing |
| Backtest speed | 5yr/500stock backtest: ≤ 5 seconds |
| Test coverage | `go test -cover ./pkg/backtest/...` > 80% |

### Phase 2 Gate (before Phase 3)

| Test | Pass Criterion |
|------|----------------|
| Factor cache integration | Multi-factor 100stock/3yr backtest: ≤ 30 seconds |
| Strategy Copilot E2E | Human submits Chinese → receives code → backtest runs → results displayed (manual test) |
| Copilot acceptance rate | ≥ 30% of generated strategies compile and pass smoke test |
| Walk-forward framework | Framework operational; 3 candidate strategies pass train/validate split |
| Background worker | `POST /backtest` returns `job_id` immediately; worker processes async; results queryable |
| Strategy DB CRUD | Create/read/update/delete strategy via API; YAML round-trip preserved |

---

## 4. Backtest Accuracy Validation (vs vnpy)

### Methodology for 5% Drift Comparison

**Step 1 — Select reference universe:**
- 50 stocks from CSI 300 constituents (as of 2023-01-01)
- 5-year backtest: 2018-01-01 to 2023-01-01

**Step 2 — Configure identical inputs:**
- Same initial capital: 1,000,000 CNY
- Same strategy: 20-day momentum (top 10 by score, equal weight)
- Same rebalancing: monthly (last trading day of each month)
- Same commission: 0.03% + 0.1% stamp tax (sell-only) + 0.001% transfer fee
- Same slippage: 0% (disable for clean comparison)
- Same T+1 enforcement
- Same 涨跌停 handling

**Step 3 — Run both systems:**
```bash
# Our system
go run ./cmd/analysis/main.go \
  --strategy momentum \
  --universe csi300-2018 \
  --start 2018-01-01 \
  --end 2023-01-01 \
  --capital 1000000 \
  --output json > our-result.json

# vnpy: run equivalent backtest and export results
```

**Step 4 — Compare:**
```python
# compare_backtests.py
import json

with open('our-result.json') as f:
    ours = json.load(f)
with open('vnpy-result.json') as f:
    theirs = json.load(f)

total_return_diff = abs(ours['total_return'] - theirs['total_return'])
annual_return_diff = abs(ours['annual_return'] - theirs['annual_return'])
sharpe_diff = abs(ours['sharpe_ratio'] - theirs['sharpe_ratio'])
max_dd_diff = abs(ours['max_drawdown'] - theirs['max_drawdown'])

print(f"Total return diff: {total_return_diff:.4f}")
print(f"Annual return diff: {annual_return_diff:.4f}")
print(f"Sharpe diff: {sharpe_diff:.4f}")
print(f"Max drawdown diff: {max_dd_diff:.4f}")
```

**Step 5 — Pass criterion:**
- `total_return_diff < 0.05` (5%)
- `max_drawdown_diff < 0.05` (5%)
- `sharpe_diff < 0.3` (0.3 is lenient givenvnpy's approximation)

> ⚠️ **Note:** vnpy itself has approximations (e.g., T+1 handling, 前复权 continuity). The 5% target is a proxy for "results are in the same ballpark." True ground-truth validation requires comparing against a real brokerage account's actual P&L.

---

## 5. AI Evolution Validation Tests

For Phase 3 candidate strategies generated by AI:

| Validation Step | Tool | Pass Criterion |
|----------------|------|----------------|
| Syntax check | `go build -o /dev/null` | Compiles without error |
| Dangerous pattern scan | `go vet` + custom linter | No `os.Open`, `os.Remove`, infinite loop patterns |
| Logic guardrails | AI code review (separate LLM call) | No leverage > 1.0, no position limits > 40% |
| Walk-forward validation | Backtest engine (train/validate split) | Must beat benchmark in BOTH train AND validate windows |
| Minimum backtest window | Backtest engine | ≥ 3 years of data required |
| Resource limits | Subprocess with timeout + memory cap | `GenerateSignals()` completes in ≤ 5s; no memory > 100MB |

---

## 6. Coverage Targets

| Package | Target |
|---------|--------|
| `pkg/backtest` | > 85% line coverage |
| `pkg/strategy/plugins` | > 80% line coverage |
| `pkg/tracker` | > 90% line coverage |
| `pkg/data` | > 70% line coverage |
| Overall | > 80% line coverage |

Run: `go test ./... -coverprofile=coverage.out && go tool cover -html=coverage.out`

---

_Last updated by: 龙少 (AI Assistant) — 2026-03-24_
