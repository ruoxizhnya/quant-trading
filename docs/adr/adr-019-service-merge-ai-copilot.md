# ADR-019: Service 合并 + AI Copilot Sandbox 重构

**Date:** 2026-06-11
**Status:** Proposed

## Context

[ODR-013 2026-06-11 综合审查](../odr/odr-013-comprehensive-audit-2026-06-11.md) 识别 3 项关键架构问题：

1. **AR-002 / AR-009 — 微服务过度拆分**：7 个服务中 risk-service、execution-service 与 analysis-service 是紧密耦合的同步 HTTP 调用（`pkg/backtest/engine.go:798-819, 964-993, 1049-1080`）。`pkg/risk` 已是完整 Go 包，提供 0-HTTP in-process 能力。
2. **AR-003 — Copilot 沙箱违反 ADR-007 设计**：`pkg/strategy/copilot.go:158-162` 硬编码 `buildCmd.Dir = "/Users/ruoxi/..."` 执行 `go build`，无 sandbox 隔离、跨平台/CI 必失败。
3. **AR-008 — AI Service HTTP 客户端无超时/重试/熔断**：`pkg/ai/client.go:27` 直接 `&http.Client{}` 零配置。

## Decision

### §1. 7 服务 → 3 服务（合并 risk/execution/strategy-service）

**Phase 1 (Sprint 6, 1 week)**：

| 服务 | 端口 | 状态变化 |
|---|---|---|
| analysis-service | :8085 | 保留；新增 risk + live in-process 引用 |
| data-service | :8081 | 保留 |
| ai-research-service | :8086 | 保留 |
| ~~risk-service~~ | ~~:8083~~ | 合并到 analysis-service in-process |
| ~~execution-service~~ | ~~:8084~~ | 合并到 analysis-service in-process |
| ~~strategy-service~~ | ~~:8082~~ | 继续 standby（沿用 [ADR-012](adr-012-strategy-service-standby.md)） |
| ~~sync~~ | (docker-compose) | 改为 data-service 内部组件 |

**合并方式**：

1. `cmd/risk/main.go` → 删除（实际目录名，docker-compose service 名仍为 `risk-service`）
2. `cmd/execution/main.go` → 删除（实际目录名，docker-compose service 名仍为 `execution-service`）
3. `pkg/risk/`, `pkg/live/` 已是 in-process library，直接在 `cmd/analysis/main.go` import
4. `pkg/backtest/engine.go` 移除 HTTP 调用路径，全部走 in-process
5. Docker compose 文件简化：`analysis-service` 启动时初始化 risk + live 实例
6. 保留 `cmd/risk/`, `cmd/execution/` 目录作为 6 个月 shim，HTTP 端点返 301 重定向到 analysis-service

**Migration impact**:
- 5 年回测 latency: -1-6s (HTTP 调用 1260 次消除)
- 部署资源: 7 容器 → 3 容器 (内存 -40%)
- 监控: 4 个 metrics endpoint 减少为 1 个
- 配置漂移风险消除

### §2. AI Copilot Sandbox (合并 ADR-007 实施路径)

**核心变更**：

1. **硬编码路径修复**（P0-4, 1d）：
   ```go
   // pkg/strategy/copilot.go
   type CopilotServiceConfig struct {
       WorkingDir  string  // 替代硬编码
       SandboxMode string  // "subprocess" | "wasm" | "none"
       // ...
   }
   ```

2. **LLMClient interface 化**（与 [ADR-018 §2](adr-018-test-and-async-safety.md) 协同）：
   ```go
   type LLMClient interface { ... }
   ```

3. **Phase 1 静态分析闸**（Sprint 6 P0-4, 1d）：
   - `internal/sandbox/staticcheck` 包
   - 正则拒绝 `os.RemoveAll`, `exec.Command`, `net.Dial`, `panic`, `go func()` (无超时)
   - 拒绝后报错并记录到 `ai_sandbox_rejections` 表

4. **Phase 2 进程隔离**（Sprint 6 P1-11, 1w）：
   - `internal/sandbox/runner` 包
   - `os/exec` 启动独立进程，stdin/stdout JSON-RPC
   - 资源限制：`syscall.Setrlimit` + `cgroup`
   - 上下文超时 5s/调用

5. **Phase 3 WASM 强化**（Sprint 7+, 1mo, optional）：
   - `wazero` Go-native WebAssembly
   - 完全内存隔离
   - 仅在 3rd-party LLM 启用

### §3. AI Service HTTP 客户端加固

**`pkg/ai/client.go` 复用 `pkg/httpclient` 模式**：

```go
// New
type Client struct {
    httpClient *httpclient.Client  // 复用现有 timeout/retry/backoff
    rateLimiter *rate.Limiter      // 10 req/min/user
    costTracker *CostTracker        // 记录每次 LLM 调用 token 数
    // ...
}
```

**LLM Cost Tracking**：
- 每次 LLM 调用记录 `prompt_tokens`, `completion_tokens`, `cost_usd` 到 `ai_llm_costs` 表
- 月底生成 cost report（dashboard 内部端点）
- 配额：free 100k tokens/month, pro 1M, enterprise unlimited

## Options Considered

**Option A — 维持 7 服务**：
- ❌ 拒绝：ADR-008 自承 regime HTTP 仍是瓶颈；与现有 ADR-012 策略冲突

**Option B — 完全不做 sandbox，仅做 WorkingDir 配置**：
- ❌ 拒绝：安全风险未消除；不符合 ADR-007 决策意图

**Option C — 全面 WASM（跳到 Phase 3）**：
- ❌ 拒绝：实施周期 1 月；Sprint 6 短期收益不如静态分析 + 进程隔离

## Consequences

### Positive
- 部署拓扑从 7 容器 → 3 容器，运维成本 -50%
- 5 年回测 latency 减少 1-6s
- AI Copilot 安全等级从 "无 sandbox" → "静态分析 + 进程隔离"
- LLM 成本可观测 + 限流
- 配置文件单一来源（消除 risk-service 漂移）

### Negative
- 失去 risk-service 独立扩展能力（可接受，per ADR-008 决策）
- Service 合并需要 1 周集中重构
- WASM 增强 1 月后置

### Migration Risk
- 6 个月双轨期保留 `cmd/risk/`、`cmd/execution/` HTTP shim（service 名 `risk-service`/`execution-service`）
- 客户端可逐步迁移
- 监控告警 endpoint 重新配置

## Implementation Roadmap

| Sprint | Tasks (见 [TASKS.md §Sprint 6](../../TASKS.md)) | Effort |
|---|---|---|
| **Sprint 6 P0-4** (1d) | Copilot `WorkingDir` 配置 + 静态分析闸 | Critical |
| **Sprint 6 P0-1** (1d) | LLMClient interface 化 | Critical |
| **Sprint 6 P1-11** (1w) | 进程隔离 sandbox + rlimit + 5s timeout | Major |
| **Sprint 6 P1-14** (3d) | AI service httpclient 重构 + rate limit + cost tracking | Major |
| **Sprint 6 P1-15** (1w) | risk/execution service 合并到 analysis | Major |
| **Sprint 7+ P2-X** (1mo) | WASM 强化 (optional) | Optional |

## Related

- [ADR-007 AI Sandbox](adr-007-ai-sandbox.md) — 安全决策上下文
- [ADR-008 Inter-Service Comm](adr-008-inter-service-comm.md) — regime 决策已 Accepted
- [ADR-012 Strategy Service Standby](adr-012-strategy-service-standby.md) — service 合并的 precedent
- [ADR-017 Observability + Auth](adr-017-observability-and-auth.md) — middleware 复用
- [ADR-018 Test + Async Safety](adr-018-test-and-async-safety.md) — LLMClient interface 化联动
- [ODR-013 AR-002/AR-003/AR-008/AR-009 findings](../odr/odr-013-comprehensive-audit-2026-06-11.md)
