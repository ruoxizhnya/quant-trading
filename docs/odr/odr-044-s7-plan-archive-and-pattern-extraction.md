# ODR-044: S7 执行计划归档 + 工程模式提取

> **Status**: Completed
> **Date**: 2026-07-01
> **Category**: Migration
> **Related ADRs**: [ADR-015](../adr/adr-015-ai-agent-architecture.md), [ADR-020](../adr/adr-020-strategy-framework-unification.md)
> **Supersedes**: N/A

## Context

Sprint 7 (ODR-043 综合审计改进) 执行期间，通过 Trae IDE 的 Plan Mode 生成了 14 个详细实施计划文件，存放在 `.trae/documents/` 目录。这些文件是任务的**执行工件**（"如何做"的过程细节），而非设计文档（"为什么"的决策记录，已存于 ADR/ODR 中）。

随着 S7 全部 27 个任务完成（✅），这些计划文件面临两个问题：

1. **位置不持久** — `.trae/` 是 IDE 会话级目录，不在 git 跟踪范围内，可能随 IDE 状态清理而丢失
2. **知识未沉淀** — 计划中包含 5 个可复用的工程模式（leaf+aliases 提取、narrow interface 解耦等），但这些模式仅存在于执行计划中，未进入长效文档

## Decision

### 1. 归档原始计划文件

将 14 个计划文件复制到 `docs/archive/plans-2026-Q3/`，并附带 README 说明背景和文件清单。

**选择 archive 而非删除的理由**：
- 计划包含详细的风险分析和替代方案比较，对未来类似重构有参考价值
- AGENTS.md 文档生命周期规定："Archived: 移至 docs/archive/ 并带季度标签"
- 原始文件保留可追溯性，便于理解代码历史

### 2. 提取 5 个工程模式到本 ODR

将计划中最有价值的工程模式提取到本 ODR 的 §Lessons Learned，作为团队可复用的方法论。

### 3. 不扩展 SPEC.md / ARCHITECTURE.md

**未将模式写入 SPEC/ARCHITECTURE 的理由**：
- 这些是**重构手法**而非**系统架构** — SPEC/ARCHITECTURE 描述"系统是什么"，不描述"如何重构"
- 模式的受众是"执行类似重构的工程师"，ODR 是更合适的载体（记录过程决策）
- 避免 SPEC/ARCHITECTURE 膨胀 — 它们应保持"当前状态文档"的定位

## Consequences

### 正面
- ✅ 14 个计划文件从易失的 `.trae/` 迁移到 git 跟踪的 `docs/archive/`
- ✅ 5 个工程模式被显式记录，可被未来任务引用
- ✅ `.trae/` 保持为纯会话目录，不再承担文档持久化职责
- ✅ AGENTS.md Document Index 更新，归档目录可发现

### 负面
- ⚠️ `docs/archive/plans-2026-Q3/` 增加 14 个文件（~250KB），但这些是历史工件，不影响日常文档导航
- ⚠️ 模式描述在 ODR 中而非专门指南 — 若未来需要更详细的重构手册，应创建 `docs/guides/` 文档

## Artifacts

### 新建
- `docs/archive/plans-2026-Q3/README.md` — 归档说明
- `docs/archive/plans-2026-Q3/s7-*.md` × 14 — 从 `.trae/documents/` 复制的计划文件
- `docs/odr/odr-044-s7-plan-archive-and-pattern-extraction.md` — 本 ODR

### 修改
- `docs/ADR.md` — ODR Index 新增 ODR-044 条目
- `AGENTS.md` — Document Index 新增归档引用

## Metrics

| 指标 | 变化 |
|------|------|
| `.trae/documents/` 文件数 | 14 → 0（会话级，不持久） |
| `docs/archive/plans-2026-Q3/` 文件数 | 0 → 15（14 计划 + 1 README） |
| ODR 总数 | 043 → 044 |
| 提取的工程模式数 | 5（详见 §Lessons Learned） |

## Lessons Learned — 5 个可复用工程模式

### 模式 1: Leaf + Aliases 提取（God Package 拆分）

**适用场景**: 一个包过大（>5000 行），但外部调用方众多，无法一次性重命名所有 import。

**手法**:
```
pkg/backtest/          # 父包（facade）
├── contracts/         # LEAF: 共享 DTO + 接口（只 import domain + fees）
├── tracker/           # LEAF: import contracts
├── state/             # LEAF: import contracts + tracker
├── walkforward/       # LEAF: import contracts
├── batch/             # LEAF: import contracts + walkforward
├── job/               # LEAF: import contracts
└── aliases.go          # 父包 re-export: type X = subpkg.X
```

**关键点**:
- `type X = subpkg.X` 是**零成本别名**（编译期内联），调用方零改动
- Go 无函数别名 → constructor 用薄 wrapper: `func NewX(...) *X { return sub.NewX(...) }`
- LEAF 包只 import `contracts/`（不 import 父包），避免循环
- 父包的 `aliases.go` 可访问子包未导出字段（如 `engine.logger`），用于 wrapper 内部提取

**S7-P2-1 验证**: 8 个原子提交拆分 `pkg/backtest`，外部调用方（cmd/analysis, reporting/）零改动。

### 模式 2: Narrow Interface 解耦

**适用场景**: 子包需要调用父包的某个方法，但 import 父包会导致循环。

**手法**: 在 LEAF `contracts/` 包定义单方法接口：
```go
type EngineRunner interface {
    RunBacktest(ctx context.Context, req BacktestRequest) (*BacktestResponse, error)
}
```
子包接受 `contracts.EngineRunner` 而非 `*Engine`；父包 `*Engine` 自动满足接口（签名一致）。

**关键点**:
- 接口越窄越好 — 单方法优于多方法（接口隔离原则）
- 编译时断言守卫: `var _ contracts.EngineRunner = (*Engine)(nil)` — 签名漂移时编译失败
- Wrapper 可保持旧签名: `func NewJobService(store, engine *Engine) *JobService { return job.NewJobService(store, engine, engine.logger) }`

### 模式 3: Fake Stub 测试（断开测试中的 Import Cycle）

**适用场景**: 测试 helper 构建了真实对象（如 `*Engine`），但对象所在包是被测包的父包 → 循环。

**手法**: 用 fake stub 替换真实对象：
```go
type fakeRunner struct {
    mu    sync.Mutex
    calls []contracts.BacktestRequest
}
func (f *fakeRunner) RunBacktest(ctx context.Context, req contracts.BacktestRequest) (*contracts.BacktestResponse, error) {
    f.mu.Lock()
    f.calls = append(f.calls, req)
    f.mu.Unlock()
    return &contracts.BacktestResponse{}, nil  // 非 nil 防止 NPE
}
```

**关键点**:
- 逐一审查所有测试：确认无测试断言被 fake 对象的返回值 → 可安全替换
- fake 返回**非 nil** 的零值响应 — 防止生产代码访问 `result.Field` 时 NPE
- 保持 helper 的返回 arity 不变 → 零调用点改动
- 若未来测试需检查调用记录，可扩展返回值（不阻塞当前提取）

### 模式 4: Soft Layering（Type Alias View 增量迁移）

**适用场景**: 需要迁移类型到新子包，但消费者众多（如 199 个文件），无法一次性改完。

**手法**: 新建子包定义类型，旧位置保留 alias：
```go
// pkg/domain/market/ohlcv.go (新)
package market
type OHLCV struct { ... }

// pkg/domain/types.go (旧, 保留 alias)
package domain
type OHLCV = market.OHLCV  // 同一类型，零破坏
```

**关键点**:
- Go type alias (`type X = Y`) 让两者是**同一类型**（不是新类型），所有消费者零修改
- 旧代码继续用 `domain.OHLCV`，新代码可逐步迁移到 `market.OHLCV`
- 当所有消费者迁移完毕，删除 alias 即可（无运行时影响）

### 模式 5: Test-First + Atomic Commit 纪律

**适用场景**: 大型重构需要保证每一步可回滚、可验证。

**手法**: AGENTS.md §8.3 已确立的执行规范：
- **测试先行**: bug 修复先写复现测试；新功能用 TDD；重构确保现有测试不退化
- **代码审查**: 自审 + `go vet` + `gofmt -l` + `go test -race`
- **原子提交**: 一个任务 = 一个 commit；每个 commit 可独立构建、独立测试通过
- **Flakiness gate**: `go test ./... -race -count=2` — `-count>1` 暴露全局状态泄漏和 property-test 边界

**S7 验证**: 27 个任务 × 3 规范 = 81 个质量检查点，全部通过。
