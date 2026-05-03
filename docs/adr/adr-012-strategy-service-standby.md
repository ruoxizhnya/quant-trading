# ADR-012: Strategy-Service Standby Decision

> **Status**: Accepted
> **Date**: 2026-04-11
> **Deciders**: Longshao

## Context

The project has a `strategy-service` (:8082) defined in docker-compose.yml with a complete implementation in `cmd/strategy/main.go`. However, analysis-service currently handles all strategy operations in-process via `strategy.DefaultRegistry` (populated by `pkg/strategy/plugins/` init functions). The strategy-service is never actually called because the local registry always finds strategies first.

Key issues:
1. **Two incompatible strategy registries**: `cmd/strategy/main.go` uses the old `strategy.Register()` factory pattern, while `cmd/analysis/main.go` uses `strategy.DefaultRegistry` with the canonical Strategy interface.
2. **AGENTS.md data flow diagram incorrectly shows** `GET /strategies → strategy-service :8082 (proxy)`, but the actual `/strategies` route queries local `StrategyDB` directly.
3. **Engine fallback logic** in `engine.go` tries local registry first, then falls back to strategy-service via HTTP — but the fallback never executes because all strategies are registered locally.

## Decision

**Keep strategy-service in standby status** with the following clarifications:

1. **Current state**: Strategy-service is not required for normal operation. All strategies run in-process within analysis-service.
2. **Future activation criteria**: Strategy-service will be activated when Phase 3 D3 (Go Plugin hot-swap) is implemented, which requires a separate process for dynamic strategy loading.
3. **Required work before activation**:
   - Migrate `cmd/strategy/main.go` to use `strategy.DefaultRegistry` instead of the old `strategy.Register()` factory
   - Ensure both services share the same canonical Strategy interface
   - Add proper proxy routes in `handlers_proxy.go` if external strategy lookups are needed
4. **Deprecation path**: If D3 is replaced by a simpler in-process plugin mechanism, strategy-service can be fully deprecated and removed from docker-compose.yml.

## Consequences

### Positive
- No unnecessary service to maintain or monitor
- Simpler deployment topology (2 services instead of 3)
- No latency from inter-service HTTP calls for strategy lookups

### Negative
- Strategy hot-swap requires analysis-service restart until D3 is implemented
- `cmd/strategy/main.go` code drifts if not kept in sync with canonical interface

### Mitigation
- Document the standby status clearly in ARCHITECTURE.md and AGENTS.md
- Correct the data flow diagram to reflect actual architecture
- Track the interface alignment as a prerequisite for D3 activation
