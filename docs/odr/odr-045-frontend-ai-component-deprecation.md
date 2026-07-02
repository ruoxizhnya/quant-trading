# ODR-045: Frontend AI Component Deprecation

> **Status**: Accepted
> **Date**: 2026-07-02
> **Category**: Cleanup
> **Related ADRs**: [adr-015](../adr/adr-015-ai-agent-architecture.md)
> **Supersedes**: None

## Context

ADR-015 (AI Agent Architecture) planned 9 frontend AI components for the
"AI Research Platform" UI layer:

| Component | Purpose | Planned File |
|-----------|---------|-------------|
| FactorLab | Factor discovery via natural language | `web/src/components/ai/FactorLab.vue` |
| StrategyWorkshop | Strategy generation from factors | `web/src/components/ai/StrategyWorkshop.vue` |
| EvolutionObs | Strategy population monitoring | `web/src/components/ai/EvolutionObs.vue` |
| GenealogyTree | Strategy lineage visualization | `web/src/components/ai/GenealogyTree.vue` |
| FitnessChart | Fitness evolution charts | `web/src/components/ai/FitnessChart.vue` |
| StrategyCard | Strategy display card | `web/src/components/ai/StrategyCard.vue` |
| FactorCard | Factor display card | `web/src/components/ai/FactorCard.vue` |
| PipelineDashboard | AI pipeline visualization | `web/src/components/ai/PipelineDashboard.vue` |
| AIResearch | Main AI research page | `web/src/pages/AIResearch.vue` |

**These components were created, then deleted as dead code.** The full history:

1. **Created** in P1-13 (commit `4354409`, [ODR-017](odr-017-p1-13-p1-14-ai-hardening.md))
   as the "AI L5 人工审查 UI — Approve / Reject / Edit YAML" feature.
   11 Vue components + 2 test files + `api/factor.ts` + `types/pipeline.ts`
   were implemented (3406 lines total).

2. **Deleted** in S7-P2-7 (commit `d7c2a38`, ODR-043) as unreachable dead code.
   The audit found `AIResearch.vue` (36 lines) was never registered in the
   Vue router, making all 11 components unreachable. The user decided to
   delete all 15 files (-3406 lines).

3. **Deprecated** in this ODR (2026-07-02): with Hermes Agent adoption
   ([ODR-046](odr-046-hermes-agent-integration-decision.md)), the autonomous
   research capability is delivered via MCP tools + natural language
   interaction, making a Vue-based AI UI unnecessary.

> **Correction note**: An earlier draft of this ODR claimed the components
> were "never created." Git history (`git log --all --oneline --
> web/src/components/ai/PipelineDashboard.vue`) disproves this — the files
> were created in `4354409` and deleted in `d7c2a38`. This ODR has been
> corrected to reflect the accurate timeline.

Despite the deletion, three design documents continued to claim the frontend
was "complete" or "implemented" (documentation drift):

1. **ADR-015** (4 locations):
   - Line 9-12: "exercised by the AIResearch.vue page"
   - Line 165: "New frontend: web/src/components/ai/, web/src/pages/AIResearch.vue"
   - Line 224: "PipelineDashboard.vue 已实现"
   - Line 248: "PipelineDashboard.vue 增补"

2. **VISION.md** (5 lines):
   - Line 23: "AI Research UI: FactorLab, StrategyWorkshop, EvolutionObs, PipelineDashboard"
   - Line 376: "AI Research Platform (Phase 4 — ✅ Implemented)"
   - Lines 377-381: Per-component descriptions with ✅ marks

3. **tasks-phase-2.md** (8 task rows):
   - S8-8, S8-9, S9-6, S9-7, S9-8, S10-11, S10-12, S10-13 all marked ✅
   - Header claims "95% Complete"
   - Footer claims "All tasks completed - Phase 4 ready for release"

The backend Phase 4 work (Go packages: `pkg/ai/agents/`, `pkg/ai/expression/`,
`pkg/ai/gene_pool/`, etc.) **was genuinely completed and retained** — only
the frontend UI layer was deleted and is now deprecated.

## Decision

**Deprecate (cancel) all 9 planned frontend AI components.** The autonomous
research capability will instead be delivered through the **Hermes Agent
Integration** (see [ODR-046](odr-046-hermes-agent-integration-decision.md)),
which replaces the need for a dedicated Vue-based AI research UI.

Rationale for cancellation (not deferral):
1. **Architecture shift**: Hermes Agent (LLM-driven) replaces the
   human-in-the-loop Vue UI approach. The agent autonomously drives the
   research loop — no UI interaction needed.
2. **Maintenance burden**: The 11 components were already deleted as
   unreachable dead code (S7-P2-7, -3406 lines). Rebuilding them against
   an evolving backend API is high cost for low marginal value when
   Hermes provides the same capability via MCP tools.
3. **Consistency**: The `web/src/components/ai/` directory was deleted
   in S7-P2-7 but design docs continued to reference it as "complete"
   for months. Canceling removes the false expectation and the
   documentation drift.

## Consequences

### Positive
- Eliminates 9 components' worth of frontend development debt
- Removes documentation drift in 3 files (17 false claims)
- Aligns project scope with the Hermes-based autonomous approach
- Frees frontend development capacity for core trading UI improvements

### Negative
- Users who expected a visual AI research interface will not get one
  (mitigated: Hermes provides natural-language interaction instead)
- `pkg/ai/agents/` Go code (research.go, generate.go) loses its
  intended consumer — these are deprecated in [ODR-046](odr-046-hermes-agent-integration-decision.md)

## Artifacts

### Files affected (documentation fixes — to be applied in Phase 3.3-3.6):
- `docs/adr/adr-015-ai-agent-architecture.md` — remove/correct 4 false claims
- `docs/VISION.md` — remove/correct 5 false claims
- `docs/tasks-phase-2.md` — correct 8 task rows + header + footer

### Files NOT affected:
- No Go code deleted — `pkg/ai/agents/*.go` retained but deprecated
- No frontend code deleted in this ODR — the 15 files were already deleted
  in S7-P2-7 (commit `d7c2a38`, ODR-043)

## Metrics

| Metric | Before | After |
|--------|--------|-------|
| Planned frontend AI components | 9 | 0 (deprecated) |
| False "complete" claims in docs | 17 | 0 |
| `web/src/components/ai/` files | 0 (deleted in S7-P2-7) | 0 (deprecated, not rebuilt) |

## Lessons Learned

1. **Documentation drift compounds silently**: ADR-015's false claims
   propagated to VISION.md and tasks-phase-2.md over months. Each doc
   cross-referenced the others, creating a self-reinforcing illusion of
   completion. Regular doc-code consistency checks (AGENTS.md §10 Rule 5)
   should catch this earlier.

2. **Deletions must update task/doc status**: The frontend components were
   created (P1-13) and legitimately marked ✅. They were later deleted as
   dead code (S7-P2-7), but the task status in tasks-phase-2.md was never
   reverted to reflect the deletion. **When code is deleted, all docs/tasks
   referencing it must be updated in the same commit.** A `git log --oneline
   -- <file>` check should be part of the doc-sync checklist.

3. **Architecture pivots need explicit ODRs**: The shift from Vue-based AI
   UI to Hermes Agent was a significant architectural pivot that should
   have been recorded immediately, not deferred until documentation drift
   accumulated. This ODR (and ODR-046) retroactively document the decision.

4. **Verify claims against git history**: The initial draft of this ODR
   incorrectly stated the components were "never created." A simple
   `git log --all -- <file>` check would have revealed the creation
   (commit `4354409`) and deletion (commit `d7c2a38`). Decision records
   must verify historical claims against git evidence, not rely on
   current filesystem state alone.
