# ODR-045: Frontend AI Component Deprecation

> **Status**: Accepted
> **Date**: 2026-07-02
> **Category**: Cleanup
> **Related ADRs**: [adr-015](../adr/adr-015-ai-agent-architecture.md)
> **Supersedes**: None

## Context

ADR-015 (AI Agent Architecture) planned 8 frontend AI components for the
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

**None of these were ever created.** The directory `web/src/components/ai/`
does not exist in the codebase.

Despite this, three design documents falsely claimed the frontend was
"complete" or "implemented":

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
`pkg/ai/gene_pool/`, etc.) **was genuinely completed** — only the frontend
UI layer was never built.

## Decision

**Deprecate (cancel) all 9 planned frontend AI components.** The autonomous
research capability will instead be delivered through the **Hermes Agent
Integration** (see [ODR-046](odr-046-hermes-agent-integration-decision.md)),
which replaces the need for a dedicated Vue-based AI research UI.

Rationale for cancellation (not deferral):
1. **Architecture shift**: Hermes Agent (LLM-driven) replaces the
   human-in-the-loop Vue UI approach. The agent autonomously drives the
   research loop — no UI interaction needed.
2. **Maintenance burden**: Building 9 Vue components + maintaining them
   against an evolving backend API is high cost for low marginal value
   when Hermes provides the same capability via MCP tools.
3. **Consistency**: The `web/src/components/ai/` directory was referenced
   in docs for months without existing. Canceling removes the false
   expectation and the documentation drift.

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
- No frontend code deleted — nothing existed to delete

## Metrics

| Metric | Before | After |
|--------|--------|-------|
| Planned frontend AI components | 9 | 0 (deprecated) |
| False "complete" claims in docs | 17 | 0 |
| `web/src/components/ai/` files | 0 | 0 (confirmed: never existed) |

## Lessons Learned

1. **Documentation drift compounds silently**: ADR-015's false claims
   propagated to VISION.md and tasks-phase-2.md over months. Each doc
   cross-referenced the others, creating a self-reinforcing illusion of
   completion. Regular doc-code consistency checks (AGENTS.md §10 Rule 5)
   should catch this earlier.

2. **"Planned" ≠ "Complete"**: The tasks-phase-2.md marked frontend tasks
   as ✅ without corresponding code review or file existence verification.
   Task status should require a `git log --oneline -- <file>` verification
   before marking ✅.

3. **Architecture pivots need explicit ODRs**: The shift from Vue-based AI
   UI to Hermes Agent was a significant architectural pivot that should
   have been recorded immediately, not deferred until documentation drift
   accumulated. This ODR (and ODR-046) retroactively document the decision.
