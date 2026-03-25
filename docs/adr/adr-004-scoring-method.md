# ADR-004: Rank-Based Composite Scoring vs. Portfolio Optimization

**Date:** 2026-03-24
**Status:** Accepted — keep both, user-selectable

## Context

Should the system use rank-based composite scoring (current: equal weight to top-N), or migrate to formal portfolio optimization (mean-variance optimization / risk parity)?

## Decision

**Keep Option A (rank-based) as default; add Option B as a configuration choice.**

Rank-based approach is correct for factor-based long-only strategies. Portfolio optimization should be added as an alternative `WeightScheme` in the Risk service. Users who want MVO can enable it; users who want simplicity use rank-based.

## Consequences

- Both approaches share the same signal generation pipeline
- Risk service `WeightScheme` interface allows pluggable weight computation
