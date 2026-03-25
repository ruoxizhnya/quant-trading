# ADR-008: Synchronous vs. Async Inter-Service Communication

**Date:** 2026-03-24
**Status:** PARTIAL — resolved for OHLCV, OPEN for regime/risk calls

## Context

All inter-service communication is synchronous HTTP blocking.

## Decision (OHLCV path — RESOLVED, 2026-03-25)

**Bulk endpoint + in-memory cache** — `POST /api/v1/ohlcv/bulk` returns all OHLCV for the universe in one call. Engine stores result in `e.inMemoryOHLCV`. Subsequent `getOHLCV` calls are zero-HTTP. Eliminates the per-symbol HTTP round-trip bottleneck.

## Decision (Regime/risk path — still OPEN)

**Regime/risk path — still synchronous** — regime detection makes per-day HTTP calls (252 calls for 5yr backtest). This is a remaining bottleneck.

## Recommendation for Regime Path

**Keep synchronous for Phase 1/2** — regime detection overhead is not the primary bottleneck (OHLCV was). Revisit when Phase 3 parallel backtests are needed.
