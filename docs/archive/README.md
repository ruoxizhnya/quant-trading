# Archive — Archived Documents

> This directory contains documents that have been superseded, completed, or are no longer actively referenced.
> These files are retained for audit trails and historical reference only.

## Directory Structure

```
archive/
└── reports-2026-Q2/          ← Archived on 2026-04-09
    ├── CLEANUP_REPORT.md     ← Doc cleanup operation record (superseded by ODR-001)
    ├── DOC_AUDIT_REPORT.md   ← Design doc audit report (superseded by ODR-002)
    └── MIGRATION_REPORT.md   ← AGENTS.md migration report (superseded by ODR-003)
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
_Last updated: 2026-04-09_
