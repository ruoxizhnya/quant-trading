# ADR-007: AI Evolution Layer — Sandbox & Safety

**Date:** 2026-03-24
**Status:** **Accepted** (2026-06-11 — see Decision)
**Supersedes:** —

## Context

AI Evolution Layer generates Go strategy code via LLM and compiles+runs it in the same process as the backtest engine. This creates two risks:
1. **Security:** Generated code could contain malicious operations (`os.RemoveAll`, infinite loops, memory exhaustion)
2. **Execution safety:** Compiled strategy could crash the backtest engine

## Options

**Option A — Process Isolation (recommended)**

Run generated strategy code in a separate goroutine/process with:
- Resource limits: CPU time, memory, max iterations
- No filesystem access
- Timeout on `GenerateSignals()` call (e.g., 5 seconds max)
- Compile to separate binary, execute as subprocess, communicate via stdin/stdout

**Option B — Static Analysis Gate**

Before running any LLM-generated code:
- `go vet` + custom linter for dangerous patterns
- AI code review via separate LLM call
- Syntax check via `go build -o /dev/null`

**Option C — Managed Strategy Library**

Only allow strategies from a curated library. AI Copilot helps modify existing strategies, not generate from scratch.

## Decision (2026-06-11)

**Status updated to Accepted** based on 2026-06-11 comprehensive audit ([ODR-013](../odr/odr-013-comprehensive-audit-2026-06-11.md) AR-003 finding).

**Adopt Option A (Process Isolation) + Option B (Static Analysis Gate) layered approach:**

1. LLM generates code
2. Static analysis rejects dangerous patterns
3. Code compiles to isolated subprocess
4. Subprocess has resource limits
5. Signals returned via IPC

### Implementation Path (Sprint 6 — see ADR-019 §Service Merge & AI Copilot Sandbox)

**Phase 1 (Sprint 6 P0-4, 2 days):**
- Replace `pkg/strategy/copilot.go:158-162` hard-coded `buildCmd.Dir = "/Users/ruoxi/..."` with config-injected `WorkingDir` (immediate fix)
- Introduce `LLMClient interface` in `pkg/ai/client.go`; struct → interface to allow mock injection
- Add `internal/sandbox/staticcheck` package: regex/AST-based rejection of `os.RemoveAll`, `exec.Command`, `net.Dial`, `panic` patterns

**Phase 2 (Sprint 6 P1-11, 1 week):**
- Implement Option A: separate subprocess via `os/exec` + stdin/stdout JSON-RPC
- Resource limits: `ulimit -v` (memory), `rlimit.CPU` (time), context timeout 5s per GenerateSignals call
- Disable filesystem write in subprocess (chroot or empty bind mount)

**Phase 3 (Sprint 7+, 1 month):**
- Optional: WASM sandbox via `wazero` (Go-native WebAssembly runtime) for stronger isolation

## Consequences

- Higher implementation complexity
- Better safety guarantees
- Enables truly untrusted strategy generation
- **Phased rollout** allows immediate fix (P0) without blocking on full WASM migration

## Review

Phase 2 implementation tied to Sprint 6 (P0-4, P1-11 in [TASKS.md §Sprint 6](../../TASKS.md)).
Final review checkpoint at Sprint 6 retrospective.

## Related

- [ODR-013 AR-003 finding](../odr/odr-013-comprehensive-audit-2026-06-11.md#ar-003)
- [ADR-019 §AI Copilot Sandbox Refactor](adr-019-service-merge-ai-copilot.md)
- Original discussion: see git history commit 0c8bfb3 (pre-Status update)
