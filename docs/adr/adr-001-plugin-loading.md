# ADR-001: Dynamic Plugin Loading vs. Compiled Strategies

**Date:** 2026-03-24
**Status:** Accepted

## Context

Should strategies be loaded at runtime from `.so` plugin files (true hot-swap), or from Go source files compiled into the binary (safer, simpler)?

## Decision

**Option A — Compiled strategies (current)** for v1 and v2.

The architecture is already "hot-swap" at the config level — swapping strategy parameters via YAML achieves most practical benefit. True `.so` hot-swap requires implementing a `StrategyLoader` interface and can be revisited when there is clear user need.

## Consequences

- Adding a new strategy requires rebuilding the binary
- Type safety and IDE support are preserved
- Config-level hot-reload provides most of the practical benefit

## Review

Revisit when: user demand for runtime plugin loading emerges.
