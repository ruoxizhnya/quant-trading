# Architecture Decision Records (ADR)

> **Location:** `docs/adr/` — individual ADR files
> **Owner:** 龙少 (Longshao) — AI Assistant
> **Version:** 1.1.0
> **Created:** 2026-03-24

---

## Index

| ADR | Title | Status | Date |
|-----|-------|--------|------|
| [ADR-001](adr/adr-001-plugin-loading.md) | Dynamic Plugin Loading vs. Compiled Strategies | Accepted | 2026-03-24 |
| [ADR-002](adr/adr-002-timescaledb.md) | TimescaleDB vs. Vanilla PostgreSQL for OHLCV Storage | Accepted | 2026-03-24 |
| [ADR-003](adr/adr-003-background-worker.md) | In-Process Backtest vs. Background Worker | Accepted | 2026-03-24 |
| [ADR-004](adr/adr-004-scoring-method.md) | Rank-Based Composite Scoring vs. Portfolio Optimization | Accepted | 2026-03-24 |
| [ADR-005](adr/adr-005-strategy-config.md) | YAML Strategy Config vs. Database-Driven Strategy Config | Accepted | 2026-03-24 |
| [ADR-006](adr/adr-006-job-queue.md) | Job Queue Technology Selection | OPEN | 2026-03-24 |
| [ADR-007](adr/adr-007-ai-sandbox.md) | AI Evolution Layer — Sandbox & Safety | OPEN | 2026-03-24 |
| [ADR-008](adr/adr-008-inter-service-comm.md) | Synchronous vs. Async Inter-Service Communication | PARTIAL | 2026-03-24 |
| [ADR-009](adr/adr-009-speed-target-revision.md) | Speed Target Revision — Phase 1 Exit Criteria | Decided | 2026-03-25 |
| [ADR-010](adr/adr-010-speed-architecture.md) | Speed Optimization Architecture — Phase 2 | Draft | 2026-03-25 |

---

## Status Legend

| Status | Meaning |
|--------|---------|
| Draft | Under discussion, not yet decided |
| OPEN | Needs decision before a specific phase |
| PARTIAL | Partially resolved, some aspects still open |
| Accepted | Decided and implemented |
| Decided | Decided but not yet implemented |
| Superseded | Replaced by a later ADR |

---

## Future ADRs

The following decisions are anticipated but not yet written:

| ADR | Topic | Phase |
|-----|-------|-------|
| ADR-011 | Data freshness SLA and Tushare outage fallback | Phase 2 |
| ADR-012 | Schema migration tooling (Flyway/goose) | Phase 2 |
| ADR-013 | API authentication and access control | Phase 2 |
| ADR-014 | Docker networking → Kubernetes service discovery | Phase 4 |

---

_Last updated by: 龙少 (AI Assistant) — 2026-03-25_
