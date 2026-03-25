# Architecture Decision Records (ADR)

> **Location:** Canonical record of significant architectural decisions
> **Owner:** 龙少 (Longshao) — AI Assistant
> **Version:** 1.0.0
> **Created:** 2026-03-24

---

## ADR-001: Dynamic Plugin Loading vs. Compiled Strategies

**Date:** 2026-03-24
**Status:** Accepted

### Context
Should strategies be loaded at runtime from `.so` plugin files (true hot-swap), or from Go source files compiled into the binary (safer, simpler)?

### Decision
**Option A — Compiled strategies (current)** for v1 and v2.

The architecture is already "hot-swap" at the config level — swapping strategy parameters via YAML achieves most practical benefit. True `.so` hot-swap requires implementing a `StrategyLoader` interface and can be revisited when there is clear user need.

### Consequences
- Adding a new strategy requires rebuilding the binary
- Type safety and IDE support are preserved
- Config-level hot-reload provides most of the practical benefit

### Review
Revisit when: user demand for runtime plugin loading emerges.

---

## ADR-002: TimescaleDB vs. Vanilla PostgreSQL for OHLCV Storage

**Date:** 2026-03-24
**Status:** Accepted

### Context
Should OHLCV data use TimescaleDB's hypertable partitioning, or standard PostgreSQL partitioned tables?

### Decision
**Option A — TimescaleDB (current).**

Compression and time-series query performance are worth the operational complexity for a data-intensive system. If license concerns arise in a commercial context, migration to native partitioning is a one-week project with no architectural changes.

### Consequences
- TimescaleDB extension adds operational complexity
- Chunk-based compression reduces storage ~90%
- License considerations for production use (source-available license)

---

## ADR-003: In-Process Backtest vs. Background Worker

**Date:** 2026-03-24
**Status:** Accepted — migrate to Background Worker

### Context
Should the backtest engine run in-process (same goroutine as the API server), or as a separate worker process with a job queue?

### Decision
**Migrate to Option B — Background worker with job queue.**

Implement via: `backtest_runs` table gets a `status` column; engine gains a `--worker` flag. Redis as job queue backend (see ADR-006).

**Current state (Phase 1):** In-process is acceptable for single-user, single-backtest scenarios.

**Trigger for migration:** When any of these conditions are met:
- Multiple concurrent users
- Backtests longer than 1 minute
- Batch strategy optimization (walk-forward analysis)

### Consequences
- API server is never blocked
- Multiple backtests can run in parallel
- Backtest crash is isolated
- Infrastructure adds Redis dependency and worker service

---

## ADR-004: Rank-Based Composite Scoring vs. Portfolio Optimization

**Date:** 2026-03-24
**Status:** Accepted — keep both, user-selectable

### Context
Should the system use rank-based composite scoring (current: equal weight to top-N), or migrate to formal portfolio optimization (mean-variance optimization / risk parity)?

### Decision
**Keep Option A (rank-based) as default; add Option B as a configuration choice.**

Rank-based approach is correct for factor-based long-only strategies. Portfolio optimization should be added as an alternative `WeightScheme` in the Risk service. Users who want MVO can enable it; users who want simplicity use rank-based.

### Consequences
- Both approaches share the same signal generation pipeline
- Risk service `WeightScheme` interface allows pluggable weight computation

---

## ADR-005: YAML Strategy Config vs. Database-Driven Strategy Config

**Date:** 2026-03-24
**Status:** Accepted — migrate to DB-backed

### Context
Should strategy parameters be stored in YAML files (current approach) or in the PostgreSQL database?

### Decision
**Option B — Database-backed** as primary store, with YAML as import/export format.

Add `strategies` table with JSONB config column and CRUD API in Strategy service. Backtest engine `StrategyLoader` reads from a common interface — DB can be source of truth while YAML remains a convenient human-editable format.

### Migration Plan
1. Add `strategies` table with JSONB config column and CRUD API in Strategy service
2. Existing YAML configs remain importable
3. No changes to backtest engine or Strategy interface required

### Consequences
- Full audit trail of parameter changes
- Runtime queryable strategy config via API
- Enables A/B testing of strategy parameters

---

## ADR-006: Job Queue Technology Selection

**Date:** 2026-03-24
**Status:** **OPEN — needs decision before Phase 2**

### Context
Decision 3 says "implement as job queue" but does not specify the technology. Options:
- **Redis-backed queue** (using LIST/Streams) — leverages existing Redis dependency, lightweight
- **PostgreSQL-backed queue** — no new dependencies, uses `backtest_runs` table with status
- **Dedicated queue** (RabbitMQ, NATS) — more robust but adds operational complexity

### Decision
**Pending.** Recommendation: Redis-backed queue using Redis Streams, given Redis is already a Phase 1 infrastructure dependency.

### Consequences (when decided)
- Worker service implementation depends on this choice
- Queue reliability guarantees (at-least-once vs exactly-once delivery)
- Operational complexity

---

## ADR-007: AI Evolution Layer — Sandbox & Safety

**Date:** 2026-03-24
**Status:** **OPEN — needs decision before Phase 3**

### Context
AI Evolution Layer generates Go strategy code via LLM and compiles+runs it in the same process as the backtest engine. This creates two risks:
1. **Security:** Generated code could contain malicious operations (`os.RemoveAll`, infinite loops, memory exhaustion)
2. **Execution safety:** Compiled strategy could crash the backtest engine

### Options

**Option A — Process Isolation (recommended)**
Run generated strategy code in a separate goroutine/process with:
- Resource limits: CPU time, memory, max iterations
- No filesystem access
- Timeout on `GenerateSignals()` call (e.g., 5 seconds max)
- Compile to separate binary, execute as subprocess, communicate via stdin/stdout

**Option B — Static Analysis Gate**
Before running any LLM-generated code:
- `go vet` + custom linter for dangerous patterns
- AI code review via separate LLM call
- Syntax check via `go build -o /dev/null`

**Option C — Managed Strategy Library**
Only allow strategies from a curated library. AI Copilot helps modify existing strategies, not generate from scratch.

### Recommendation
**Option A (Process Isolation) + Option B (Static Analysis Gate)** layered approach:
1. LLM generates code
2. Static analysis rejects dangerous patterns
3. Code compiles to isolated subprocess
4. Subprocess has resource limits
5. Signals returned via IPC

### Consequences
- Higher implementation complexity for Phase 3
- Better safety guarantees
- Enables truly untrusted strategy generation

### Review
Must be decided before Phase 3 begins. See Phase 2 exit criteria.

---

## ADR-008: Synchronous vs. Async Inter-Service Communication

**Date:** 2026-03-24
**Status:** **PARTIAL — resolved for OHLCV, OPEN for regime/risk calls**

### Context
All inter-service communication is synchronous HTTP blocking.

### Decision (OHLCV path — RESOLVED, 2026-03-25)
**Bulk endpoint + in-memory cache** — `POST /api/v1/ohlcv/bulk` returns all OHLCV for the universe in one call. Engine stores result in `e.inMemoryOHLCV`. Subsequent `getOHLCV` calls are zero-HTTP. Eliminates the per-symbol HTTP round-trip bottleneck.

**Regime/risk path — still synchronous** — regime detection makes per-day HTTP calls (252 calls for 5yr backtest). This is a remaining bottleneck.

### Recommendation for Regime Path
**Keep synchronous for Phase 1/2** — regime detection overhead is not the primary bottleneck (OHLCV was). Revisit when Phase 3 parallel backtests are needed.

---

## Future ADRs

The following decisions are anticipated but not yet written:

| ADR | Topic | Phase |
|-----|-------|-------|
| ADR-009 | Data freshness SLA and Tushare outage fallback | Phase 2 |
| ADR-010 | Schema migration tooling (Flyway/goose) | Phase 2 |
| ADR-011 | API authentication and access control | Phase 2 |
| ADR-012 | Docker networking → Kubernetes service discovery | Phase 4 |

---

_Last updated by: 龙少 (AI Assistant) — 2026-03-24_
