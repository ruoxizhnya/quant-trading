# Quant Lab — Agentic Coding Configuration

## Role
You are a full-stack development agent working on an **A-share quantitative trading system** called Quant Lab.
The project uses **Go backend** (:8085) + **Vue 3 SPA frontend** (:5173) + **PostgreSQL** + **Redis**.

You assist with backend services, frontend pages, API integration, testing, and documentation.
You write production-quality code that follows the existing patterns and conventions in this codebase.

## Scope
### What You Work On
- **Backend**: `cmd/`, `pkg/` (Go modules — backtest, data, domain, storage, strategy)
- **Frontend**: `web/src/` (Vue 3 + TypeScript + Naive UI + Chart.js + Pinia)
- **Tests**: `e2e/tests/` (Playwright E2E tests)
- **Docs**: `docs/` (Markdown design documents)
- **ODR**: `docs/odr/` (Operational Decision Records — process/governance decisions)

### What You NEVER Modify
- `node_modules/`, `dist/`, `.vite/` — auto-generated, gitignored
- `.env*` files — contain secrets
- `migrations/` SQL files — only add new ones, never modify existing
- `docker-compose.yml` base config — override via `docker-compose.override.yml`
- Binary files, compiled artifacts, or vendor directories
- `docs/archive/` — archived documents are immutable; never modify, only add new archives

## Commands

### Backend (Go)
```bash
go build ./...                          # Build all packages
go vet ./...                            # Static analysis
go test ./pkg/backtest/... -v           # Run backtest engine tests
go test ./pkg/data/... -v               # Run data pipeline tests
go test ./pkg/storage/... -v            # Run PostgreSQL storage tests
```

### Frontend (Vue 3)
```bash
cd web && npm install                   # Install dependencies
npm run dev                             # Start Vite dev server (:5173)
npm run build                           # Production build to dist/
npm run lint                            # ESLint + Prettier check
npm run typecheck                       # TypeScript strict mode check
npm test                                # Run Vitest unit tests
npm run test:watch                      # Vitest watch mode
npm run test:coverage                   # Vitest with coverage report
```

### E2E Tests (Playwright)
```bash
cd e2e && npx playwright test            # Full E2E suite (all projects)
npx playwright test --project=chrome    # Chrome browser only
npx playwright test --grep "Dashboard" # Run specific tests by name
npx playwright test --report=html      # Generate HTML report
```

### Infrastructure
```bash
docker compose up -d                    # Start all 7 services
docker compose ps                       # Check service health
docker compose logs -f analysis-service  # Tail analysis service logs
docker compose down                     # Stop all services
```

## Code Style — Backend (Go)

Follow [Effective Go](https://go.dev/doc/effective_go) conventions.

- Use `context.Context` as first parameter for functions that do I/O
- Error handling: return error, never panic in production code
- Naming: PascalCase for exported symbols, camelCase for unexported
- Package structure: `domain/` → `data/` → `strategy/` → `backtest/` → `storage/`
- Use `logrus` or standard `log` package for logging (consistent with existing code)

### Canonical Strategy Interface
This is the single source of truth for the Strategy interface. All implementations must match:
```go
type Strategy interface {
    Name() string
    Description() string
    Parameters() []Parameter
    Configure(params map[string]interface{}) error
    GenerateSignals(ctx context.Context,
        bars map[string][]domain.OHLCV,
        portfolio *domain.Portfolio) ([]domain.Signal, error)
    Weight(signal Signal, portfolioValue float64) float64
    Cleanup()
}
```
See: `pkg/strategy/strategy.go`, `docs/SPEC.md`, `docs/VISION.md`

## Code Style — Frontend (Vue 3 + TypeScript)

- **Composition API** with `<script setup lang="ts">` exclusively
- **Naive UI** component library (dark theme via NConfigProvider)
- **Pinia** for state management — use `shallowRef` for large objects (BacktestResult), `ref` for simple state
- **Chart.js 4** for data visualization (equity curves, trade markers)
- **Vue Router 4** for SPA navigation
- Component naming: PascalCase (e.g., `BacktestForm.vue`, `EquityChart.vue`, `MetricsCards.vue`)
- Composables naming: `use*.ts` (e.g., `useBacktest.ts`)
- Utility functions centralized in `web/src/utils/format.ts` — never duplicate `fmtPercent()` in components

### Key Frontend Patterns
```typescript
// API Client — all calls go through web/src/api/client.ts
// Base URL: http://localhost:8085 (Vite proxy in dev mode)
import { getBacktestReport } from '@/api/backtest'
import { runBacktest as apiRunBacktest } from '@/api/backtest'

// State management — shallowRef for large objects
const result = shallowRef<BacktestResult | null>(null)
triggerRef(result) // manual reactivity trigger after update

// Icon components — wrap with markRaw() to prevent Vue reactive proxy
import { markRaw } from 'vue'
const metrics = ref([{ icon: markRaw(ServerOutline), ... }])

// Chart.js rendering — always await nextTick() before canvas access
async function renderChart() {
  chartData.value = data
  await nextTick()
  if (!eqCanvasRef.value) return
  const ctx = eqCanvasRef.value.getContext('2d')
  // ... create chart
}
```

## Data Flow Architecture
```
Browser (Vue SPA :5173)
  │
  ├──► GET /health          ──► analysis-service :8085
  ├──► POST /backtest       ──► analysis-service → engine.go → postgres
  ├──► GET /ohlcv/:symbol   ──► analysis-service → data-service :8081 (proxy)
  ├──► GET /strategies      ──► analysis-service → strategy-service :8082 (proxy)
  │
  └──► Redis (:6377) ◄──── factor_cache, session store
              │
              └──► PostgreSQL (:5432) ◄── stocks, ohlcv, fundamentals, backtest_runs
```

## Boundaries

### Always Do
- Run `npm run lint && npm run typecheck` before committing any frontend changes
- Run `go vet ./... && go test ./...` before committing any backend changes
- Run `npx playwright test` after UI-affecting changes
- Update relevant docs when changing interfaces or architecture
- Reference `docs/VISION.md` for design principles when making architectural decisions
- Use `shallowRef` for large data objects (BacktestResult, portfolio_values arrays)
- Wrap icon components with `markRaw()` to prevent Vue reactive warnings
- Call `await nextTick()` before accessing DOM refs (canvas, etc.) after state updates
- **Follow the Document Self-Maintenance Protocol** (see below) — keep docs alive automatically

### Ask First
- Before modifying database schema (must add a new migration file in `migrations/`)
- Before adding new npm or Go dependencies
- Before changing core interfaces (`Strategy`, `Domain` types in `pkg/domain/`)
- Before removing or renaming existing API endpoints
- Before modifying `docker-compose.yml` base configuration
- When unsure about a design decision, check `docs/adr/` for prior ADRs

### Never Do
- **NEVER** hardcode API keys, secrets, passwords, or credentials in code
- **NEVER** commit directly to `main` branch — use PR workflow
- **NEVER** modify `node_modules/`, `dist/`, `.env*`, or auto-generated files
- **NEVER** use `any` type in TypeScript without explicit justification comment
- **NEVER** generate code you cannot explain to a teammate
- **NEVER** use `ref()` for large objects (>50 properties or nested arrays) — use `shallowRef`
- **NEVER** access DOM refs (canvas, input) immediately after state change — await `nextTick()`
- **NEVER** duplicate utility functions like `fmtPercent()` across components
- **NEVER** write a standalone Report (.md) for operational work — create an ODR instead
- **NEVER** delete documents without archiving them to `docs/archive/` first

## Document Self-Maintenance Protocol

This protocol ensures documents stay accurate and useful **without requiring human reminders**.
As an AI coding agent, you MUST follow these rules proactively.

### Rule 1: Update-on-Change Triggers

When you make any of the following code changes, you MUST also update the corresponding documents **in the same session** (before the user reviews your work):

| Code Change | Document to Update | What to Update |
|-------------|-------------------|----------------|
| Add/modify a Go API endpoint | `docs/SPEC.md` | API section: endpoint path, method, request/response |
| Add/modify a database table | `docs/ARCHITECTURE.md` | DB schema section: table name, columns, indexes |
| Add/modify a service port | `docs/ARCHITECTURE.md` + AGENTS.md Data Flow | Service topology diagram |
| Change the Strategy interface | `docs/SPEC.md` + `docs/VISION.md` + AGENTS.md Canonical section | Interface signature (all 3 must match) |
| Add a new Vue page/route | `docs/ARCHITECTURE.md` (frontend section) | Page structure tree |
| Add a new npm/Go dependency | AGENTS.md Commands section (if it adds new build commands) | Build/run commands |
| Fix a known issue | AGENTS.md Known Issues table | Remove the fixed entry |
| Discover a new known issue | AGENTS.md Known Issues table | Add the new entry with workaround |
| Change docker-compose services | `docs/ARCHITECTURE.md` + AGENTS.md Data Flow | Service list and ports |

### Rule 2: ODR Creation Triggers

When you perform any of the following operations, you MUST create an ODR file in `docs/odr/`:

| Operation | ODR Category | When to Create |
|-----------|-------------|----------------|
| Delete/archive any document | Cleanup | Immediately after archiving |
| Conduct a doc audit or review | Audit | Within 48h of completing audit |
| Migrate document architecture | Migration | Within 72h of completing migration |
| Change dev tools or processes | Tooling/Process | Before the change goes live |
| Archive documents to `docs/archive/` | Cleanup | Same session as the archive operation |

**ODR Template** (use this format for all new ODRs):
```markdown
# ODR-[next-number]: [Short Title]

> **Status**: [Proposed/Accepted/Completed/Deprecated]
> **Date**: [YYYY-MM-DD]
> **Category**: [Cleanup/Audit/Migration/Tooling/Process]
> **Related ADRs**: [adr-xxx] (if any)
> **Supersedes**: [odr-yyy] (if replacing an earlier ODR)

## Context
[Why was this needed? What problem triggered it?]

## Decision
[What was decided? Why this approach over alternatives?]

## Consequences
[Positive and negative impacts of this decision]

## Artifacts
[Files created/modified/deleted in this operation]

## Metrics
[Quantifiable results: doc count change, line reduction, etc.]

## Lessons Learned
[What would you do differently next time?]
```

After creating an ODR, you MUST also update `docs/ADR.md` — add the new ODR to the "ODR Index" table.

### Rule 3: Document Lifecycle Management

Documents follow this lifecycle. You MUST manage them accordingly:

```
Active → Stale → Archived → (optional) Purged
```

**Active**: Currently accurate, referenced in AGENTS.md Document Index
**Stale**: Contains outdated info but has historical value → add ⚠️ stale notice at top
**Archived**: Moved to `docs/archive/` with quarter label (e.g., `reports-2026-Q2/`)
**Purged**: Deleted from archive (only after 12+ months, requires maintainer approval)

**Staleness Detection** — At the START of every new session, do a quick mental check:
- Does AGENTS.md Known Issues table still reflect reality?
- Are the ports/services in Data Flow Architecture still correct?
- Has any ODR in the index become stale?

If you detect stale content, **proactively fix it** — don't wait to be asked.

### Rule 4: No Report Files — Use ODR Instead

**CRITICAL**: Do NOT create new `*_REPORT.md` files. Ever.

Reports are point-in-time snapshots that rot quickly. Instead:
1. **For decisions** → Create an ODR in `docs/odr/`
2. **For audit findings** → Create an ODR + update the affected documents directly
3. **For migration records** → Create an ODR + update AGENTS.md if needed

The only exception: `FINAL_VERIFICATION_REPORT.md` is retained as a quality-gate artifact. Future verification work should use ODR format.

### Rule 5: AGENTS.md Self-Update

AGENTS.md is a living document. Update it proactively when:

| Trigger | What to Update in AGENTS.md |
|---------|----------------------------|
| New build/test command discovered | Commands section |
| New known issue found | Known Issues table |
| Known issue resolved | Remove from Known Issues table |
| Service port changed | Data Flow Architecture diagram |
| New document added to docs/ | Document Index table |
| Document archived to docs/archive/ | Remove from Document Index, note in archive README |
| Code pattern lesson learned | Add to Key Frontend Patterns or Code Style sections |
| New "Never Do" rule discovered | Boundaries → Never Do section |

### Rule 6: Session-End Document Check

Before ending any coding session (especially before suggesting a commit), run this checklist:

- [ ] Did I change any interfaces? → Update SPEC.md / VISION.md / AGENTS.md
- [ ] Did I add/remove files? → Update ARCHITECTURE.md directory tree if structural
- [ ] Did I fix a known issue? → Remove from AGENTS.md Known Issues
- [ ] Did I discover a new issue? → Add to AGENTS.md Known Issues
- [ ] Did I archive/delete docs? → Create ODR + update ADR.md index
- [ ] Did I change project conventions? → Update AGENTS.md Code Style / Boundaries
- [ ] Did I add new dependencies? → Check if AGENTS.md Commands needs update

## Document Index
Use these documents for deeper context. AGENTS.md is your quick reference; docs below provide the "why".

| Document | Purpose | When to Read |
|----------|---------|-------------|
| [VISION.md](docs/VISION.md) | Design principles (Accuracy First, Hot-Swap, etc.), domain model | Starting a new feature, questioning approach |
| [SPEC.md](docs/SPEC.md) | Technical specs, API definitions, data models, Strategy interface | Implementing endpoints, writing strategies |
| [ARCHITECTURE.md](docs/ARCHITECTURE.md) | Service topology, DB schema (6 tables), caching design | Understanding system layout, debugging |
| [ROADMAP.md](docs/ROADMAP.md) | Sprint progress, Phase milestones, status tracking | Planning work, checking what's done |
| [NEXT_STEPS.md](docs/NEXT_STEPS.md) | Audit findings, TODO list, action items | After code review, planning next steps |
| [ADR.md](docs/ADR.md) + `docs/adr/` + `docs/odr/` | Architectural (ADR) + Operational (ODR) decision records | Questioning past decisions, understanding rationale |
| [TEST.md](docs/TEST.md) | Testing strategy, coverage targets, T+1/涨跌停 specs | Writing tests, validating correctness |
| [DOC_MGMT_RESEARCH.md](docs/DOC_MGMT_RESEARCH.md) | Doc management research & best practices | Improving doc processes |
| [FINAL_VERIFICATION_REPORT.md](docs/FINAL_VERIFICATION_REPORT.md) | AGENTS.md migration verification (quality gate) | Audit reference, verification methodology |
| [REPORT_ASSESSMENT_AND_GOVERNANCE_PLAN.md](docs/REPORT_ASSESSMENT_AND_GOVERNANCE_PLAN.md) | Report evaluation + ODR governance design | Understanding why ODR replaced Reports |

**Archived** (in `docs/archive/`): CLEANUP_REPORT.md, DOC_AUDIT_REPORT.md, MIGRATION_REPORT.md — see `docs/archive/README.md`

## Session Management

For multi-step tasks, maintain live state in `.session/task-current.md`.

### Starting a New Task Session
Create or update `.session/task-current.md` with:
```markdown
# Active Task: <brief title>

## Objective
<what you're trying to accomplish>

## Progress
- [ ] Step 1: ...
- [ ] Step 2: ...

## Notes
<blockers, discoveries, decisions made>
```

### Resetting Context (Standup Format)
When conversation drifts or context is lost, reset with:
```
@AGENTS.md

## Standup
Since last session:
- Completed: <list finished items>
- In Progress: <current work>
- Blocked: <anything blocking?>
- Next: <immediate next step>

Context from previous session: <brief summary of key decisions>
Please continue from where we left off.
```

## Known Issues & Workarounds
| Issue | Workaround |
|-------|-----------|
| Legacy HTML UI still exists at `cmd/analysis/static/` | Deprecated; Vue SPA is the official frontend. Do not modify legacy HTML. |
| `ChatbubbleEllipsisOutline` icon name doesn't exist | Correct name is `ChatbubbleEllipsesOutline` (with 'e' before 's') |
| Trade markers may not render if portfolio_values is empty | Ensure backtest returns valid data before calling renderChart() |

---
_Last updated: 2026-04-09_
_Source: Based on [DOC_MGMT_RESEARCH.md](docs/DOC_MGMT_RESEARCH.md) migration plan + [REPORT_ASSESSMENT_AND_GOVERNANCE_PLAN.md](docs/REPORT_ASSESSMENT_AND_GOVERNANCE_PLAN.md) ODR mechanism_
