# ADR-008: Synchronous vs. Async Inter-Service Communication

**Date:** 2026-03-24
**Status:** **Accepted** (2026-06-11 — see Decision)
**Supersedes:** —

## Context

All inter-service communication is synchronous HTTP blocking.

## Decision (OHLCV path — RESOLVED, 2026-03-25)

**Bulk endpoint + in-memory cache** — `POST /api/v1/ohlcv/bulk` returns all OHLCV for the universe in one call. Engine stores result in `e.inMemoryOHLCV`. Subsequent `getOHLCV` calls are zero-HTTP. Eliminates the per-symbol HTTP round-trip bottleneck.

## Decision (Regime/risk path — RESOLVED, 2026-06-11)

**Status updated PARTIAL → Accepted** based on 2026-06-11 comprehensive audit ([ODR-013](../odr/odr-013-comprehensive-audit-2026-06-11.md) AR-002/AR-009 findings).

**Decision: Merge risk-service and execution-service into analysis-service as in-process libraries** (rationale in [ADR-019](adr-019-service-merge-ai-copilot.md)):

| Component | Current | New (Sprint 6+) |
|---|---|---|
| Regime detection | HTTP `POST /detect_regime` per trading day | Direct in-process call to `pkg/risk/regime.Detector` |
| Risk manager | HTTP `POST /risk/check` | Direct in-process call to `pkg/risk.NewRiskManager` |
| Position sizing | HTTP `POST /position/calculate` | Direct in-process call to `pkg/risk.RiskManager.CalculatePosition` |
| Order execution (live) | HTTP `POST /execution/submit` | Direct in-process call to `pkg/live.LiveEngine` |

**Rationale**:
1. `pkg/risk` is already a complete Go library (no need for IPC overhead)
2. `pkg/live` is already an in-process mock; merging just removes 2 indirection layers
3. Cross-service HTTP adds 1-6s latency per 5-year backtest (1260 regime calls)
4. Configuration drift between risk-service and analysis-service is real production risk
5. Deployment complexity: 7 services → 3 services (analysis/data/ai)

**Trade-off acknowledged**: Loss of independent scaling for risk/execution. But: (a) regime detection is bounded by OHLCV data already loaded in analysis-service, so independent scaling is moot; (b) live trading is low-frequency by design; (c) for institutional Phase 5 we can re-extract via gRPC if needed.

## Consequences

- **Positive**:
  - 1-6s backtest latency reduction
  - 50% deployment complexity reduction
  - Single source of truth for risk config
  - Eliminates HTTP timeout/retry complexity in hot path
- **Negative**:
  - Loss of independent risk-service scaling (acceptable per trade-off analysis)
  - Docker compose file requires rewrite (tracked in Sprint 6 P1-15)
  - Monitoring/alerting endpoints need refactor
- **Migration risk**:
  - `cmd/risk/`, `cmd/execution/` directories preserved as thin shims during transition (service names `risk-service`/`execution-service` retained in docker-compose for backward compat)
  - HTTP endpoints still exposed for external observability

## Review

Implementation tracked in [TASKS.md §Sprint 6](../../TASKS.md) tasks P1-15 (Service Merge).

## Related

- [ODR-013 AR-002/AR-009 findings](../odr/odr-013-comprehensive-audit-2026-06-11.md)
- [ADR-019 Service Merge & AI Copilot Sandbox](adr-019-service-merge-ai-copilot.md)
- [ADR-012 Strategy Service Standby](adr-012-strategy-service-standby.md) (precedent for service consolidation)
