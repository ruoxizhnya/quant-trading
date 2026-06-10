# Architecture Decision Records (ADR) & Operational Decision Records (ODR)

> **Location:** `docs/adr/` — architectural ADR files | `docs/odr/` — operational ODR files
> **Owner:** 龙少 (Longshao) — AI Assistant
> **Version:** 2.4.0
> **Created:** 2026-03-24
> **Updated:** 2026-06-08

---

## ADR Index — Architectural Decisions

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
| [ADR-011](adr/adr-011-vue-spa-frontend.md) | Vue 3 SPA as Official Frontend (Replacing Legacy HTML) | Accepted | 2026-04-11 |
| [ADR-012](adr/adr-012-strategy-service-standby.md) | Strategy-Service Standby Decision | Accepted | 2026-04-11 |
| [ADR-013](adr/adr-013-data-sync-enhancement.md) | Data Synchronization Enhancement | Proposed | 2026-05-03 |
| [ADR-014](adr/adr-014-strategy-framework-refactor.md) | Strategy Framework Refactor & Unified Interface | Proposed | 2026-05-04 |
| [ADR-015](adr/adr-015-ai-agent-architecture.md) | AI Agent Quantitative Research Architecture | Proposed | 2026-05-04 |
| [ADR-016](adr/adr-016-multi-source-data-architecture.md) | Multi-Source Data Architecture | Proposed | 2026-05-17 |

---

## ODR Index — Operational Decisions

| ODR | Title | Status | Category | Date |
|-----|-------|--------|----------|------|
| [ODR-001](odr/odr-001-document-cleanup.md) | Document Cleanup Operation | Completed | Cleanup | 2026-04-09 |
| [ODR-002](odr/odr-002-design-doc-audit.md) | Design Document Audit Framework | Completed | Audit | 2026-04-09 |
| [ODR-003](odr/odr-003-agents-md-adoption.md) | AGENTS.md Adoption Decision | Completed | Migration | 2026-04-09 |
| [ODR-004](odr/odr-004-verification-standard.md) | Verification Standard Definition | Completed | Process | 2026-04-09 |
| [ODR-005](odr/odr-005-agents-md-v3-migration.md) | AGENTS.md v3.0 Migration to Template v2.0 | Completed | Migration | 2026-04-11 |
| [ODR-006](odr/odr-006-document-consolidation.md) | Document Consolidation & TASKS.md Creation | Completed | Cleanup | 2026-04-11 |
| [ODR-007](odr/odr-007-task-consolidation.md) | Task Consolidation & Document Migration | Completed | Migration | 2026-04-11 |
| [ODR-008](odr/odr-008-next-steps-archive.md) | NEXT_STEPS.md Archival to docs/archive/ | Completed | Cleanup | 2026-05-03 |
| [ODR-009](odr/odr-009-code-doc-audit.md) | 代码与文档对齐全面审查 | Completed | Audit | 2026-05-06 |
| [ODR-010](odr/odr-010-code-doc-audit-2026-05-17.md) | 2026-05-17 全项目代码与文档一致性审查 | Completed | Audit | 2026-05-17 |
| [ODR-011](odr/odr-011-multi-source-integration.md) | Multi-Source Data Integration (ashare-data-source-fetchers 整合) | Completed | Migration | 2026-05-17 → 2026-06-08 |
| [ODR-012](odr/odr-012-comprehensive-code-review.md) | Sprint 5 — 全项目综合代码审查 (代码质量/测试/文档一致性 4 维度) | Completed | Audit | 2026-06-08 |

---

## Status Legend

### ADR Status
| Status | Meaning |
|--------|---------|
| Draft | Under discussion, not yet decided |
| OPEN | Needs decision before a specific phase |
| PARTIAL | Partially resolved, some aspects still open |
| Accepted | Decided and implemented |
| Decided | Decided but not yet implemented |
| Superseded | Replaced by a later ADR |

### ODR Status
| Status | Meaning |
|--------|---------|
| Proposed | Draft, pending review |
| Accepted | Approved, execution pending |
| Completed | Execution finished, outcomes verified |
| Deprecated | No longer relevant, kept for history |
| Superseded | Replaced by a later ODR (link to replacement) |

---

## Future ADRs

| ADR | Topic | Phase |
|-----|-------|-------|
| ADR-017 | API authentication and access control | Phase 5 |
| ADR-018 | Docker networking → Kubernetes service discovery | Phase 5 |

---

## ODR Creation Guide

When to create an ODR:
- After completing a document cleanup, audit, or migration operation
- When making a process/tooling decision that affects how the team works
- When archiving or restructuring project documentation

ODR template: see `docs/odr/odr-001-document-cleanup.md` for the canonical example.

---

_Last updated by: AI Assistant — 2026-06-10 (ODR-012 P1 20 项修复完成并入索引; index version 2.4.2)_
_ODR 累计 12 条: Cleanup 3 (ODR-001/006/008) | Audit 4 (ODR-002/009/010/012) | Migration 4 (ODR-003/005/007/011) | Process 1 (ODR-004)_
