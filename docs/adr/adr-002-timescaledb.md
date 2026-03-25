# ADR-002: TimescaleDB vs. Vanilla PostgreSQL for OHLCV Storage

**Date:** 2026-03-24
**Status:** Accepted

## Context

Should OHLCV data use TimescaleDB's hypertable partitioning, or standard PostgreSQL partitioned tables?

## Decision

**Option A — TimescaleDB (current).**

Compression and time-series query performance are worth the operational complexity for a data-intensive system. If license concerns arise in a commercial context, migration to native partitioning is a one-week project with no architectural changes.

## Consequences

- TimescaleDB extension adds operational complexity
- Chunk-based compression reduces storage ~90%
- License considerations for production use (source-available license)
