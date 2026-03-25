# ADR-003: In-Process Backtest vs. Background Worker

**Date:** 2026-03-24
**Status:** Accepted — migrate to Background Worker

## Context

Should the backtest engine run in-process (same goroutine as the API server), or as a separate worker process with a job queue?

## Decision

**Migrate to Option B — Background worker with job queue.**

Implement via: `backtest_runs` table gets a `status` column; engine gains a `--worker` flag. Redis as job queue backend (see ADR-006).

**Current state (Phase 1):** In-process is acceptable for single-user, single-backtest scenarios.

**Trigger for migration:** When any of these conditions are met:
- Multiple concurrent users
- Backtests longer than 1 minute
- Batch strategy optimization (walk-forward analysis)

## Consequences

- API server is never blocked
- Multiple backtests can run in parallel
- Backtest crash is isolated
- Infrastructure adds Redis dependency and worker service
