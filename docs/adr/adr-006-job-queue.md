# ADR-006: Job Queue Technology Selection

**Date:** 2026-03-24
**Status:** OPEN — needs decision before Phase 2

## Context

Decision 3 says "implement as job queue" but does not specify the technology. Options:
- **Redis-backed queue** (using LIST/Streams) — leverages existing Redis dependency, lightweight
- **PostgreSQL-backed queue** — no new dependencies, uses `backtest_runs` table with status
- **Dedicated queue** (RabbitMQ, NATS) — more robust but adds operational complexity

## Decision

**Pending.** Recommendation: Redis-backed queue using Redis Streams, given Redis is already a Phase 1 infrastructure dependency.

## Consequences (when decided)

- Worker service implementation depends on this choice
- Queue reliability guarantees (at-least-once vs exactly-once delivery)
- Operational complexity
