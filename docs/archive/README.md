# Archive — Archived Documents

> This directory contains documents that have been superseded, completed, or are no longer actively referenced.
> These files are retained for audit trails and historical reference only.

## Directory Structure

```
archive/
├── reports-2026-Q2/              ← Archived on 2026-04-09
│   ├── CLEANUP_REPORT.md         ← Doc cleanup operation record (superseded by ODR-001)
│   ├── DOC_AUDIT_REPORT.md       ← Design doc audit report (superseded by ODR-002)
│   └── MIGRATION_REPORT.md       ← AGENTS.md migration report (superseded by ODR-003)
│
└── research-2026-Q2/             ← Archived on 2026-04-11
    ├── CACHE.md                  ← Redis cache design (integrated into ARCHITECTURE.md)
    ├── CODE_REVIEW_REPORT.md     ← Full code review findings (tasks migrated to TASKS.md)
    ├── DOC_MGMT_RESEARCH.md      ← AGENTS.md best practices research (applied to AGENTS.md v3.0)
    ├── PHASE3-PLAN.md            ← Phase 3 implementation plan (tasks migrated to TASKS.md)
    ├── phase-gate-reviews.md     ← Phase Gate 1 audit records (tasks migrated to TASKS.md)
    ├── QUANT_SOFTWARE_DESIGN_ANALYSIS.md ← Quant software design analysis (reference)
    └── REPORT_ASSESSMENT_AND_GOVERNANCE_PLAN.md ← ODR governance design (integrated into AGENTS.md)
```

## How to Find Archived Content

1. Check the corresponding **ODR file** in `docs/odr/` first — it contains the distilled decision record
2. If you need the full operational details, find the original report here
3. Use `git log -- docs/archive/` to see when files were archived

## Archival Policy

- Files are archived when their **active reference value drops below daily-use threshold**
- Archived files are **never deleted** — only moved here
- Archived files may have **outdated information** — always cross-reference with current code/docs
- Quarterly review: consider removing files archived > 12 months ago (requires maintainer approval)

---
_Last updated: 2026-04-11_
