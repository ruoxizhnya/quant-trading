# ADR-007: AI Evolution Layer — Sandbox & Safety

**Date:** 2026-03-24
**Status:** OPEN — needs decision before Phase 3

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

## Recommendation

**Option A (Process Isolation) + Option B (Static Analysis Gate)** layered approach:
1. LLM generates code
2. Static analysis rejects dangerous patterns
3. Code compiles to isolated subprocess
4. Subprocess has resource limits
5. Signals returned via IPC

## Consequences

- Higher implementation complexity for Phase 3
- Better safety guarantees
- Enables truly untrusted strategy generation

## Review

Must be decided before Phase 3 begins. See Phase 2 exit criteria.
