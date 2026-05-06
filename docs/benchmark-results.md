# Performance Benchmark Results

> **Date**: 2026-05-05
> **Environment**: Intel Core i7-4870HQ @ 2.50GHz, macOS

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
