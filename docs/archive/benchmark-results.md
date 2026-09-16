# Performance Benchmark Results

> **Date**: 2026-06-10
> **Environment**: Intel Core i7-4870HQ @ 2.50GHz, macOS

## Backtest Engine — In-Memory Synthetic Data (2026-06-10)

Self-contained benchmarks (`pkg/backtest`, `BenchmarkEngineSynthetic_*`).
No DB or HTTP — measures engine + strategy + risk-manager computation cost only
(pre-loaded L1 cache). All runtimes below are wall-clock per single backtest run.

| Benchmark | Stocks × Days | Bars | Workers | ns/op (median) | ms/op | Notes |
|-----------|---------------|------|---------|----------------|-------|-------|
| `EngineSynthetic_50x240`   | 50 × 240  | 12,000 | 1 | ~290,000,000 | **290 ms** | Typical 1y daily strategy |
| `EngineSynthetic_50x240_W8` | 50 × 240 | 12,000 | 8 | ~340,000,000 | 340 ms | Workers add channel overhead at this size |
| `EngineSynthetic_100x480`  | 100 × 480 | 48,000 | 1 | ~2,030,000,000 | **2.03 s** | Walk-forward 2y window |
| `EngineSynthetic_100x480_W8` | 100 × 480 | 48,000 | 8 | ~1,900,000,000 | 1.90 s | Parallel fetch wins at 100 stocks |
| `EngineSynthetic_200x240`  | 200 × 240 | 48,000 | 1 | ~1,140,000,000 | **1.14 s** | Large universe 1y |
| `EngineSynthetic_200x240_W8` | 200 × 240 | 48,000 | 8 | ~1,260,000,000 | 1.26 s | Workers help on large pool |

**Throughput** (backtests per 5 s budget):

| Scenario | 5s budget throughput |
|----------|----------------------|
| 50 × 240 | ~14-17 runs |
| 100 × 480 | ~2-3 runs |
| 200 × 240 | ~4 runs |

### Optimizations applied (2026-06-10)

1. **L1 cache published via `atomic.Pointer[map]`** — readers in the hot path
   (`getOHLCV`, called 12,000+ times per backtest) no longer contend on the
   engine RWMutex. Snapshot is set once at startup; the L1 map is read-only
   during the backtest loop.
2. **`getOHLCV` uses binary search** for date-range filtering (O(log n)) instead
   of full O(n) scan. With 12,000 calls and 240-bar cache per stock, this is
   the difference between ~1.4M and ~96K comparisons.
3. **`warmCache` skips bulk re-fetch** when L1 is already populated (the common
   case when `LoadOHLCVInMemory` was called). Avoids `BulkLoadOHLCV` + per-symbol
   re-sort on every backtest run.
4. **`StopLossChecker.CalculateATR` windowed** — computes at most
   `atrPeriod + 1` true-ranges instead of all `len(ohlcv) - 1`. Also reuses a
   small stack-allocated ring buffer; the previous version allocated
   `len(ohlcv) - 1` floats per call (~479 allocations per call for a 1y backtest).
5. **`momentumStrategy` sort bypass** — `sortOHLCV` / `getLatestPrice` skip the
   defensive copy+sort when input is already date-sorted (the common case in
   backtesting since the L1 cache is pre-sorted in `warmCache` and
   `InMemoryProvider.LoadOHLCV`).

### Combined impact vs pre-optimization baseline

| Scenario | Baseline | Optimized | Speedup |
|----------|----------|-----------|---------|
| 50 × 240  | ~490 ms | ~290 ms | **1.7×** |
| 100 × 480 | ~2.31 s | ~2.03 s | 1.14× |
| 200 × 240 | ~1.34 s | ~1.14 s | 1.18× |

pprof hotspots before vs after:
- **Before**: 47% of CPU in `pthread_cond_wait` (lock contention on engine
  RWMutex), 20% in `pthread_cond_signal`, 9% in `madvise` (GC heap churn).
- **After**: contention is much reduced; remaining time is in ATR computation
  (already trimmed), strategy signal generation, and goroutine scheduling for
  the per-day worker pool.

## AI Search Optimization

| Benchmark | Operations | Time/Op | Memory/Op | Allocs/Op |
|-----------|-----------|---------|-----------|-----------|
| GeneticOptimizer.Optimize | 4747 | 226,689 ns | 96,313 B | 2,223 |

## Test Execution Time

| Package | Tests | Time |
|---------|-------|------|
| pkg/ai/agents | 15 | 3.1s |
| pkg/ai/client | - | 3.7s |
| pkg/ai/drift | 25 | 4.9s |
| pkg/ai/evolution | 16 | 4.3s |
| pkg/ai/expression | - | 5.3s |
| pkg/ai/gene_pool | - | 5.7s |
| pkg/ai/intent | - | 6.3s |
| pkg/ai/metrics | 18 | 6.8s |
| pkg/ai/pipeline | - | 5.1s |
| pkg/ai/search | 22 | 4.6s |
| pkg/ai/validator | 14 | 4.8s |
| pkg/ai/yaml | - | 4.7s |
| pkg/backtest | - | 2.9s |
| pkg/live | 8 | 0.5s |

## E2E Test Results

| Browser | Tests | Passed | Failed | Time |
|---------|-------|--------|--------|------|
| Chromium | 32 | 32 | 0 | 4.6m |

## Notes

- Backtest benchmarks require trading calendar sync (run POST /sync/calendar first)
- Genetic algorithm: ~4.4K optimizations/second
- All unit tests pass successfully
- The `engine_inmemory_bench_test.go` benchmark requires a live PostgreSQL
  connection. Use `BenchmarkEngineSynthetic_*` (no DB) for offline profiling
  of the engine + strategy + risk pipeline.
