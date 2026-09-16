# ADR-010: Speed Optimization Architecture — Phase 2

**Date:** 2026-03-25
**Status:** Draft
**Decider:** 龙少（CEO）

---

## Context

Phase 1 exit criteria removed the ≤5s speed target (ADR-009). Speed optimization is now a **Phase 2 P0**.

**Current measured baseline:**
- 500 stocks × 1 year → **272 seconds**
- Extrapolated 500 stocks × 5 years → **~1500 seconds**
- Target: **≤ 5 seconds**
- Gap: **~300x too slow**

**Root cause identified (from `engine_realbench_test.go`):**
The bottleneck is **NOT just data loading**:
1. WarmCache sequential: 112ms/symbol → 500×5yr ≈ 4.6 min
2. WarmCache parallel: actually slower than sequential (Redis single-connection write bottleneck)
3. **NEW: Engine computation (data already in memory)**: 49 symbols × 1yr = **31.3 seconds** (0 trades)
   - Data is pre-loaded in `inMemoryOHLCV` → no DB queries
   - Still takes 31s → bottleneck is **strategy service HTTP calls + engine computation**
   - Extrapolated: 500 symbols × 5yr (in-memory) ≈ **~1,300 seconds**

**Revised understanding:**
- Even with perfect WarmCache, strategy service HTTP calls dominate the runtime
- Engine computation itself (signals, order tracking) is also significant
- **5s target requires: in-process signal computation + parallel engine + pre-warmed data**

---

## Options

### Option 1: In-Process Signal Computation (NEW Recommended)

**Approach:**
- Move signal generation INSIDE the backtest engine (no HTTP calls to strategy-service)
- Compute signals directly from OHLCV data in-memory
- Parallelize per-stock signal computation with goroutines
- Pre-warm all data on startup

**Expected improvement:**
- Removes strategy-service HTTP round-trip latency (~2-5ms per call × 12,000 calls = 24-60s)
- With in-memory signals + goroutines: potentially < 5s for 500×5yr

**Implementation:**
- Add `GenerateSignalsInProcess()` method to engine
- Load strategy logic directly (no HTTP) for momentum/mean-reversion
- Keep HTTP strategy-service as fallback for custom strategies

### Option 2: Warm Cache with TimescaleDB Chunking

**Approach:**
1. At backtest start, call `POST /api/v1/ohlcv/bulk` — fetches all symbols' OHLCV in **one Redis bulk write**
2. TimescaleDB with chunking by time range (e.g., yearly chunks) — queries only hit relevant chunks
3. On subsequent runs, check Redis first → if miss, fall back to TimescaleDB with chunk pruning

**Expected improvement:** 1500s → ~30-60s (10-25x improvement)

**Implementation:**
- `warmCache` in engine calls bulk endpoint once per backtest
- Bulk endpoint returns all data, engine populates `inMemoryOHLCV`
- Subsequent `getOHLCV` calls hit in-memory map (zero latency)
- Redis TTL = 1 hour, so repeated backtests are instant

### Option 2: Background Pre-warm

**Approach:**
- When user loads the dashboard, asynchronously pre-warm cache for top 500 stocks
- Backtest starts with warm cache already in memory

**Expected improvement:** Reduces perceived latency but doesn't eliminate cold-cache problem

**Implementation:**
- Background goroutine runs `warmCache` on app startup
- Dashboard shows "cache warming..." status

### Option 3: Incremental / Streaming Data Load

**Approach:**
- Instead of loading all 5 years upfront, load only the data needed for the current lookback window
- Stream data per trading day (lazy loading)

**Expected improvement:** Reduces initial load time for long backtests

**Risk:** Increases per-day latency if lookback is large (e.g., 250-day lookback = 250 queries)

---

## Decision

**Proceed with Option 1 (In-Process Signal Computation) in Phase 2 Sprint 1.**

**Key insight (2026-03-25):**
- In-memory benchmark: 49 symbols × 1yr = **31.3 seconds** (0 trades, data already loaded)
- Bottleneck is **strategy service HTTP calls**, NOT data loading
- This fundamentally changes optimization direction

**Revised approach:**
1. Move signal generation in-process (remove HTTP overhead)
2. Pre-warm data on startup (background)
3. Parallelize engine computation
4. Target: in-process backtest ≤ 5s

---

## Action Items

1. **Measure pure engine computation (no HTTP)**
   - Confirm: is the 31s for 49 symbols due to HTTP latency or engine computation?
   - Add logging/timing to isolate HTTP time from computation time

2. **In-process signal generation (P0)**
   - Move momentum/mean-reversion signal computation inside engine
   - Remove HTTP calls to strategy-service during backtest
   - Expected: remove 24-60s of HTTP latency

3. **Background pre-warm (always useful)**
   - Pre-warm inMemoryOHLCV on system startup
   - Keep Redis warm with TTL

4. **Parallel engine computation** (if still insufficient after step 2)
   - Partition per-stock computation across goroutines
   - Expected: Nx speedup, N = number of cores

5. **TimescaleDB chunking** (long-term)
   - Enable yearly chunks on ohlcv_daily_qfq
   - Reduces DB query time for large universes
   - Reduces cold-start latency to zero for active stocks

---

## Metrics

| Stage | Target | Measurement |
|-------|--------|-------------|
| Bulk fetch 500×5yr | < 1s | Benchmark `warmCache` directly |
| In-memory backtest | < 4s | `BenchmarkBacktest` with pre-loaded cache |
| **Total** | **≤ 5s** | Full end-to-end benchmark |

---

_Next: Implement step 1 (verify bulk endpoint at scale)_
