# ADR-009: Speed Target Revision — Phase 1 Exit Criteria

**Date:** 2026-03-25
**Status:** Decided
**Decider:** 龙少（CEO）
**Reviewer:** 如侠（董事长）

---

## Context

Phase 1 exit criteria (VISION.md) states:

> **Speed:** 5-year, 500-stock backtest completes in ≤ 5 seconds (requires Redis caching)

After running a real-DB benchmark (`engine_realbench_test.go`), the measured results are:

| Stocks | Years | Measured Time |
|--------|-------|---------------|
| 10 | 1 | 1.9s |
| 50 | 1 | 79s |
| 100 | 1 | 52.9s |
| 200 | 1 | 111.8s |
| 500 | 1 | 272s |
| 500 | 5 | ~(extrapolated) ~1500-3000s |

**Gap:** Phase 1 target (5s) vs current reality (~1500s+) — approximately **3 orders of magnitude**.

### Root Cause Analysis

The bottleneck is **data loading**, not engine computation:

1. Each backtest run calls `store.GetOHLCV()` per stock per run — sequential DB queries
2. No in-memory data pre-loading across backtest runs
3. TimescaleDB chunking not yet benchmarked
4. Redis cache layer exists but hasn't been measured for the full hot path

The "Redis caching" mentioned in the target was never actually verified to achieve the 5s target.

---

## Decision

**Remove "Speed" from Phase 1 exit criteria.**

Phase 1 will focus exclusively on **accuracy** (drift vs vnpy ≤ 5%). Speed optimization becomes a **Phase 2 P0 deliverable**.

**Rationale:**
- Speed without accuracy is meaningless — wrong numbers fast are worse than right numbers slow
- Accuracy requires vnpy comparison, which takes time to set up — don't complicate Phase 1 with speed work that may become irrelevant if accuracy fails
- Speed optimization is a well-understood engineering problem (pre-loading, chunking, parallel queries) — it can be scoped and solved in Phase 2 once accuracy is proven

---

## Consequences

### Positive
- Phase 1 stays focused: accuracy is the only gate
- Speed work won't block Phase 1 completion
- vnpy drift comparison gets full attention

### Negative
- Speed target is deferred — no "5-second backtest" in Phase 1
- Competitive differentiation (vs Python systems) is delayed

---

## Action Items

1. Update VISION.md Phase 1 exit criteria — remove speed row
2. Add Speed Optimization as Phase 2 P0
3. Design Phase 2 speed architecture (ADR-010): in-memory pre-load, TimescaleDB chunking, parallel warmCache

---

## Alternative Considered

**Keep speed in Phase 1 and defer vnpy comparison.**

Rejected: If vnpy drift > 5%, the speed work is wasted — we've optimized the wrong model. Accuracy first.

---

_Next review: After Phase 1 drift test results are available_
