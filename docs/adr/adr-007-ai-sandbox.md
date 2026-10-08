# ADR-007: AI Evolution Layer — Sandbox & Safety

**Date:** 2026-03-24
**Status:** **Accepted** (2026-06-11 — see Decision) —— **Phase 3 activated by [ADR-029](adr-029-ai-layer-2026-agent-practice-alignment.md)** (2026-10-06)
**Supersedes:** —

> ### ⚠️ 2026-10-06 更新：Phase 3 从 Optional 转为主路径（ADR-029 §2）
>
> 本 ADR 的 Phase 3 写的是「**Optional**: WASM sandbox via `wazero` (Go-native
> WebAssembly runtime) for stronger isolation」。
>
> [ADR-024](adr-024-expression-as-execution-target.md)（2026-09-17）随后裁定
> 「LLM 生成的代码只是 artifact，**不加载、不执行**」，这让 Phase 3 失去了用途 ——
> 但**本 ADR 状态仍是 Accepted，Phase 3 仍是 Optional，两者矛盾悬置半年**，
> ADR-024 从未 supersede 它。
>
> [ADR-029](adr-029-ai-layer-2026-agent-practice-alignment.md)（2026-10-06）收口这条矛盾：
> 用户裁决策略执行载体改为**双轨**，轨道 B 就是 WASM 策略插件，**Phase 3 转为轨道 B
> 的主路径，不再是 Optional**。
>
> **实现现状（ADR-029 §Context② 取证）**：`internal/sandbox/wasm/sandbox.go`
> （505 LOC）**骨架已实现且测试齐全**（内存上限 / 超时 / 越界读保护 / 编译缓存复用），
> 插件协议已定义（`initialize` + `generate_signals`），但 **`wazero` 未入 go.mod** ——
> 现用的是包注释自陈「**no real isolation**」的 `InProcessRuntime` fallback。
>
> **两处必须按 ADR-029 修正后才可上生产**：
> 1. 换 wazero，`InProcessRuntime` 退役（否则测试通过但生产无隔离）
> 2. **协议有前视漏洞** —— `generate_signals(bars_ptr, bars_len)` 是 host 一次性写入
>    全部 bars，沙箱内代码读 `bars[len-1]` 就拿到未来数据。必须改为逐 bar 的
>    `on_bar(t)` + `get_bar(t_offset ≤ 0)` host import，`t_offset > 0` 直接 trap。
>    见 ADR-029 §3.1–3.2
>
> **下列正文保持原样，作为 2026-03-24 / 2026-06-11 时点的判断记录。**
> Phase 1（staticcheck）与 Phase 2（进程隔离 runner）的结论**不受影响，仍然有效** ——
> 它们在轨道 B 下继续作为编译前的静态门禁与编译执行器（ADR-029 §Context⑬）。

> ### 🔗 2026-10-08 对齐：轨道 B 在四层表达力模型中的位置（D2 裁决）
>
> 按 2026-10-08 拍板的四层表达力模型（[design/kernel/target-architecture-modular-kernel.md](../design/kernel/target-architecture-modular-kernel.md) §6），本 ADR 的轨道 B（WASM）= **L3a**，是表达力的**最后一层**，仅在 L2 确定性有状态算子（EWMA / RMA / IIR / Kalman）也无法表达时启用（D2：能用 L2 表达的不允许上 L3）。上文的前视修正（`on_bar(t)` + `get_bar(t_offset ≤ 0)`，越界 trap）与四层模型完全一致，**不变**。

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

**Status updated to Accepted** based on 2026-06-11 comprehensive audit ([ODR-013](../archive/odr/odr-013-comprehensive-audit-2026-06-11.md) AR-003 finding).

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

Phase 2 implementation tied to Sprint 6 (P0-4, P1-11 in [TASKS.md §Sprint 6](../TASKS.md)).
Final review checkpoint at Sprint 6 retrospective.

## Related

- [ODR-013 AR-003 finding](../archive/odr/odr-013-comprehensive-audit-2026-06-11.md#ar-003)
- [ADR-019 §AI Copilot Sandbox Refactor](adr-019-service-merge-ai-copilot.md)
- Original discussion: see git history commit 0c8bfb3 (pre-Status update)
