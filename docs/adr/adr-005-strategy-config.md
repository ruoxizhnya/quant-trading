# ADR-005: YAML Strategy Config vs. Database-Driven Strategy Config

**Date:** 2026-03-24
**Status:** Accepted — migrate to DB-backed

## Context

Should strategy parameters be stored in YAML files (current approach) or in the PostgreSQL database?

## Decision

**Option B — Database-backed** as primary store, with YAML as import/export format.

Add `strategies` table with JSONB config column and CRUD API in Strategy service. Backtest engine `StrategyLoader` reads from a common interface — DB can be source of truth while YAML remains a convenient human-editable format.

## Migration Plan

1. Add `strategies` table with JSONB config column and CRUD API in Strategy service
2. Existing YAML configs remain importable
3. No changes to backtest engine or Strategy interface required

## Consequences

- Full audit trail of parameter changes
- Runtime queryable strategy config via API
- Enables A/B testing of strategy parameters
