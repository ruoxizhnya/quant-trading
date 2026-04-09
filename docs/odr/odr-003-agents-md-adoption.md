# ODR-003: AGENTS.md Adoption Decision

> **Status**: Completed
> **Date**: 2026-04-09
> **Category**: Migration
> **Related ADRs**: —
> **Supersedes**: —
> **Archived Source**: [MIGRATION_REPORT.md](../archive/reports-2026-Q2/MIGRATION_REPORT.md)

## Context

The project lacked any AI-agent-specific configuration. Every new AI coding session (Trae/Cursor/Claude) started with zero context about project commands, code style, boundaries, or document locations. Research (DOC_MGMT_RESEARCH.md) identified AGENTS.md as the industry standard, adopted by 60,000+ repositories and supported by 25+ tools.

## Decision

Adopt the AGENTS.md standard and implement a "two-layer, four-zone" document architecture:

1. **Phase 1**: Create AGENTS.md as the single source of truth for AI agents (~229 lines)
2. **Phase 2**: Create tool-specific adapters (CLAUDE.md, .cursorrules, .windsurfrules)
3. **Phase 3**: Create .session/ directory for dynamic task tracking + update .gitignore

AGENTS.md content sourced exclusively from existing project artifacts (code, docs, docker-compose) — zero fabrication.

## Consequences

**Positive**:
- AI agents now have immediate project context on session start
- Consistent behavior across Trae/Cursor/Claude/Windsurf tools
- .session/ mechanism enables cross-session state persistence
- Follows industry standard (AGENTS.md spec by OpenAI/AAIF)

**Negative**:
- AGENTS.md must be maintained when project conventions change
- .cursorrules/.windsurfrules are full copies (not symlinks) — must be updated alongside AGENTS.md
- Additional 4 files added to project root

## Artifacts

- Created: AGENTS.md, CLAUDE.md, .cursorrules, .windsurfrules
- Created: .session/task-current.md.template
- Updated: .gitignore (+4 lines for .session/)

## Metrics

| Metric | Before | After |
|--------|--------|-------|
| AI agent config files | 0 | 4 |
| Session state mechanism | None | .session/ with template |
| Time to productive AI session | ~15-30 min context setup | ~0 min (AGENTS.md auto-loaded) |

## Lessons Learned

- AGENTS.md should be the first file created in any new project — retrofitting is more expensive
- Tool-specific adapters (CLAUDE.md) should be minimal — just @AGENTS.md import + tool-specific additions
- .session/ must be gitignored to prevent cross-contamination of task state

---
_ODR created: 2026-04-09 by AI Assistant_
