# ADR-018: 测试架构升级 + 异步安全 + 确定性回放

**Date:** 2026-06-11
**Status:** Proposed

## Context

[ODR-013 2026-06-11 综合审查](../odr/odr-013-comprehensive-audit-2026-06-11.md) 揭示测试维度严重问题（综合评分 47/100）：

1. **CI 套件在标准流程下直接失败**：
   - `pkg/strategy` 触发 SIGSEGV（`copilot_test.go:24-50` 故意构造 `aiClient: nil`）
   - `pkg/backtest` 触发 `concurrent map read and map write` panic
2. **覆盖率虚高**：`pkg/storage` 实际 8.2%（SkipIfNoDB 自欺欺人）；`pkg/ai/agents` L4 validate 是 placeholder 冒充实现
3. **可重放性破坏**：`pkg/backtest/engine.go:244-246` `rand.NewSource()` 返回值被丢弃，配置 `Seed` 实际是 no-op；实盘订单 ID 用 `time.Now().UnixNano()` 无法对齐回测
4. **AI client 不可 mock**：`pkg/ai/client.go` 是 struct 而非 interface，测试被迫走 nil-deref 歧途
5. **异步任务无优雅停机**：`cmd/analysis/main.go` shutdown 不通知 in-flight goroutine，DB status 永远 'running' zombie 任务

## Decision

### §1. 强制 `-race` + CI 阻断

**Makefile 新增 `test-race` target**：
```makefile
test-race:
    go test -race -count=1 -timeout=60s ./...

.PHONY: test-race
```

**CI 必跑**：`test-race` 与 `test` 并列，失败即阻断 PR merge。

**Consequence**: pkg/strategy nil-deref + pkg/backtest concurrent map write panic 都会自动暴露。

### §2. LLMClient Interface 化（pkg/ai 重构）

**核心变更**：`pkg/ai/client.go::Client` (struct) → `LLMClient` (interface)

```go
// New interface
type LLMClient interface {
    Chat(ctx context.Context, messages []Message, opts ...Option) (*Response, error)
    GenerateStrategyCode(ctx context.Context, intent Intent) (string, error)
    FixStrategyCode(ctx context.Context, code, err string) (string, error)
    IsConfigured() bool
}

// Existing Client implements it
// New MockLLMClient for tests
type MockLLMClient struct {
    ResponseFn func(ctx context.Context, ...) (*Response, error)
    // ...
}
```

**Injection point**：`pkg/strategy/copilot.go::NewCopilotService` 接收 `LLMClient` 而非 `*ai.Client`；测试传入 `MockLLMClient`。

**Benefits**:
- `pkg/strategy` 测试套件从 100% panic 变为可运行
- Phase 4 AI 模块建立可测试基线
- 未来切换 LLM provider (OpenAI/Anthropic/Local) 零侵入

### §3. 确定性回放（Determinism）

**`pkg/backtest/engine.go` 重构**：
- 删除 `rand.NewSource()` 丢失返回值的 bug
- 引入 `*rand.Rand` 字段，所有 `math/rand` 全局调用替换
- `pkg/live/engine.go::generateID` 改用 UUID v7（带时间戳，可排序）

```go
// New pattern
type Engine struct {
    rng    *rand.Rand  // injected, not global
    clock  clock.Clock // sony/gobreaker pattern, allows fake clock in tests
    // ...
}
```

**Verification test**：
```go
func TestEngine_DeterministicReplay(t *testing.T) {
    eng1 := NewEngine(config, seed=42, clock=realClock)
    res1 := eng1.RunBacktest(...)

    eng2 := NewEngine(config, seed=42, clock=realClock)
    res2 := eng2.RunBacktest(...)

    assert.Equal(t, res1.EquityCurve, res2.EquityCurve)  // byte-level
}
```

### §4. 异步任务优雅停机

**`cmd/analysis/main.go` shutdown 流程重构**：
```go
// New shutdown sequence
1. signal received
2. srv.Shutdown(ctx, 30s)  // stop accepting new requests
3. jobService.CancelAllInFlight(ctx, 10s)  // signal all in-flight jobs
4. jobService.WaitForCompletion(ctx, 30s)  // wait for cleanup
5. db.Close()
6. exit
```

**Stale 'running' cleanup at startup**:
```sql
UPDATE backtest_jobs
SET status = 'failed', error_message = 'interrupted by restart', completed_at = NOW()
WHERE status = 'running' AND started_at < NOW() - INTERVAL '1 hour';
```

### §5. pkg/storage 集成测试（dockertest）

**新增 `pkg/storage/integration_test.go`**：
- 使用 `github.com/ory/dockertest` 启动临时 pg + redis 容器
- TestMain：setup → run tests → teardown
- 拆为 2 类：
  - `*_unit_test.go`: 无 DB 依赖，参数校验（已部分存在）
  - `*_integration_test.go`: 真实 CRUD + 事务回滚

**CI 必跑**：`make test-integration` 与 `make test` 并列。

**Makefile 新增**：
```makefile
test-integration:
    go test -tags=integration -count=1 -timeout=120s ./pkg/storage/... ./pkg/data/source/...
```

### §6. property-based testing (testing/quick)

实施 [TEST.md §2.4 列出的 5 个 property](../../TEST.md)：
1. `Cash >= 0` 任何时刻
2. `Position qty >= 0`
3. `NAV == Cash + Σ(position.value)`
4. `Total fee > 0` 任何 trade
5. `T+1 settlement` 强制（不能卖当日买入）

**实现**：`testing/quick` 库，1000 次随机序列运行，property 违反即 fail。

### §7. S8-11 AI 因子 fail-gate

**`pkg/ai/agents/research_batch_test.go` 强化**：

```go
// Old: t.Logf(...) + no assert
// New:
highICFactors := countFactorsWithIC(results, threshold=0.03)
assert.GreaterOrEqual(t, highICFactors, 10, "Phase 4 S8-11 fail-gate: must have 10+ factors with IC > 0.03")
```

## Options Considered

**Option A — 仅修 panic，不引入 dockertest/OTel/quick**
- ❌ 拒绝：治标不治本；下个 Sprint 仍会出现类似问题

**Option B — 引入 commercial CI 平台（CircleCI）**
- ❌ 拒绝：当前 GitHub Actions 足够；商业平台不能解决测试质量问题

**Option C — 完全重写测试基础设施（mockall、ginkgo）**
- ❌ 拒绝：现有 testify 生态充足；过度工程

## Consequences

### Positive
- CI 套件可信度从 20% 提升到 80%
- 回测可重放性基础契约建立
- L4 验证不再 placeholder
- DB 集成测试从 8.2% 提升到 60%+

### Negative
- 新增 dockertest 依赖（CI 启动慢 5-10s）
- testing/quick 学习曲线
- UUID v7 需要 `github.com/google/uuid` v1.6+（已使用）

### Migration
- 6 个月双轨：`-race` 与非 `-race` 并存
- 现有 SkipIfNoDB 测试逐步改为 dockertest
- LLMClient interface 化时保留 `Client` struct 别名 3 个月

## Implementation Roadmap

| Sprint | Tasks (见 [TASKS.md §Sprint 6](../../TASKS.md)) | Effort | Owner |
|---|---|---|---|
| **Sprint 6 P0-1** (1d) | LLMClient interface 化 + pkg/strategy 测试 mock | Critical | TBD |
| **Sprint 6 P0-2** (1d) | pkg/live/engine.go::Stop 重复 close panic 修复 | Critical | TBD |
| **Sprint 6 P0-5** (2d) | engine.go::rand seed 修复 + determinism test | Critical | TBD |
| **Sprint 6 P0-6** (2d) | pkg/backtest 并发 map RWMutex + race test | Critical | TBD |
| **Sprint 6 P0-7** (3d) | pkg/storage dockertest 集成 + 覆盖率 8.2%→60% | Critical | TBD |
| **Sprint 6 P0-8** (2d) | cmd/analysis 优雅停机 waitGroup + stale cleanup | Critical | TBD |
| **Sprint 6 P1-9** (3d) | testing/quick property-based 5 个 invariant | Major | TBD |
| **Sprint 6 P1-10** (1d) | research_batch_test.go fail-gate 强化 | Major | TBD |
| **Sprint 6 P1-12** (3d) | L4 validate 实际 walk-forward 实现 | Major | TBD |

## Related

- [ODR-013 D 维度全问题列表](../odr/odr-013-comprehensive-audit-2026-06-11.md)
- [TEST.md §2.4 property-based](../../TEST.md)
- [ADR-017 鉴权需求](adr-017-observability-and-auth.md)（audit log 写入依赖事务安全）
- [ADR-020 Engine 拆分](adr-020-engine-decomposition.md)（god object 拆分后测试边界更清晰）
