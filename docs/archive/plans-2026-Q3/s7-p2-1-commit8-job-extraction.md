# S7-P2-1 Commit 8: 提取 `job/` 子包（最终提交）

> **任务**: S7-P2-1 — 拆分 `pkg/backtest` God Package 为叶子子包
> **分支**: `feat/s7-p2-1-backtest-leaf-extraction`
> **已完成**: Commits 1–7 (contracts/ cache/ execution/ tracker/ state/ walkforward/ batch/)
> **本计划**: 仅 Commit 8 — 提取 `job/` 子包 + 全局收尾验证
> **基线**: working tree clean, 7 commits ahead of merge-base

---

## 1. 当前状态分析

### 1.1 已验证的提取模式（Commits 1–7 通用）

| 手法 | 本提交用途 |
|------|-----------|
| **Type alias** `type X = job.X` | JobService/JobStore/JobRecord/CreateJobRequest/Job |
| **Constructor wrapper** | `NewJobService(store, engine *Engine)` → 内部提取 `engine.logger` |
| **`*Engine` → `contracts.EngineRunner`** | 解耦 job/ 对父包 Engine 的依赖，断开循环 |
| **logger 显式参数** | 构造器新增 `logger zerolog.Logger`，包装器从 `engine.logger` 提取 |
| **Compile-time assertion** | `var _ contracts.EngineRunner = (*Engine)(nil)` 已在 aliases.go L179 |

### 1.2 目标文件现状（已通读确认）

**`pkg/backtest/job.go`** (613 行):
- L16-38: `JobService` struct — `engine *Engine` 字段 (L18) + `logger zerolog.Logger` (L19)
- L42-50: `JobStore` interface (7 方法, 纯 `map[string]any`，无父包依赖)
- L54-67: `JobRecord` struct (纯标量 + `time.Time`/`*time.Time`)
- L70-121: `jobRecordToMap` / `mapToJobRecord` (私有 helper, 纯 stdlib)
- L124-131: `NewJobService(store JobStore, engine *Engine)` — L128 `engine.logger.With()...`
- L134-159: `CreateJobRequest` / `Job` (纯 JSON DTO)
- L228-368: `StartJob` — L310 `BacktestRequest{...}` + L325 `s.engine.RunBacktest(jobCtx, backtestReq)`
- L446-484: `Shutdown` (纯 stdlib + zerolog)
- L500-539: `CleanupStaleRunning` (纯 stdlib)
- L542-576: `SaveSyncResult(ctx, resp *BacktestResponse)` — L542 参数类型
- L579-613: `parseUniverse` / `recordToJob` (私有 helper)

**`pkg/backtest/job_test.go`** (647 行):
- L1-16: imports — `marketdata` + `viper` (仅用于 `newTestJobService` 构建真实 Engine)
- L18-148: `mockJobStore` + `cloneJobMap` (自包含, 纯 stdlib)
- L150-169: **`newTestJobService`** — 用 `NewEngine(v, marketdata.NewInMemoryProvider(), zerolog.Nop())` 构建真实 Engine → **循环依赖根源**
- L171-176: `TestJobService_NewJobService` — L175 `svc.engine` 字段访问
- L260-283: `TestJobService_StartJob_CancelViaParentContext` — 创建 job + StartJob + cancel + sleep, 不断言 RunBacktest 结果
- L358-402: `TestJobService_SaveSyncResult` — L361 `&BacktestResponse{...}`, L378 `var result BacktestResponse`, L388 `&BacktestResponse{...}` (直接调用 SaveSyncResult, 不经 RunBacktest)
- 其余测试: 纯 mockJobStore / helper 测试, 无 Engine 依赖

**`pkg/backtest/job_shutdown_test.go`** (294 行):
- 9 个 shutdown/cleanup 测试, 全部用 `newTestJobService`
- 不直接引用 `BacktestRequest`/`BacktestResponse`/`svc.engine`
- 手动操作 `svc.inflightWg` / `svc.cancelFuncs` (同包字段, 移动后仍可访问)

### 1.3 外部调用方（经 grep 确认, 全部通过 alias 解析, 零改动）

| 文件 | 行 | 用法 |
|------|----|------|
| `cmd/analysis/setup.go` | 272 | `backtest.NewJobService(store, engine)` |
| `cmd/analysis/setup.go` | 483 | `func gracefulShutdown(..., jobService *backtest.JobService, ...)` |
| `cmd/analysis/deps.go` | 30 | `JobService *backtest.JobService` 字段 |
| `cmd/analysis/handlers_backtest.go` | 19, 42, 291 | `*backtest.JobService` 参数 + `backtest.CreateJobRequest` |
| `cmd/analysis/main.go` | 148, 175 | `ds.JobService` 字段访问 |
| `pkg/backtest/reporting/compare.go` | 117 | `*backtest.JobService` 参数 + `jobService.GetJob()` |

### 1.4 关键风险分析 — `fakeRunner` stub 安全性

已逐一审查 22 个测试函数, **无任何测试断言 `RunBacktest` 返回值**:

- `StartJob` 类测试 (CreateJob/CancelViaParentContext/StoreStartFails/ConcurrentCreates): 仅检查 job 在 store 中可见, 不检查 status/result
- `CancelJob` 类测试: 直接在 store 中 seed job, 不经 StartJob → 不调 RunBacktest
- `SaveSyncResult` 测试: 直接调 `SaveSyncResult(resp)`, 不经 RunBacktest
- `Shutdown` 测试: 手动操作 `inflightWg`/`cancelFuncs`, 不经 StartJob
- `CleanupStaleRunning` 测试: 直接 seed + 调 CleanupStaleRunning

**fakeRunner 默认返回 `&contracts.BacktestResponse{}` (非 nil 零值)** — 防止 StartJob L345 `result.TotalReturn` NPE。所有测试通过。✓

### 1.5 决策: `newTestJobService` 返回值 arity

旧计划 (commits-4-8.md L458) 用 3-value return `(*JobService, *mockJobStore, *fakeRunner)`, 需更新 ~24 处调用点 (15 job_test + 9 shutdown_test)。

**本计划改用 2-value return** `(*JobService, *mockJobStore)` — fakeRunner 内部创建并丢弃:
- 无任何测试需要检查 runner 调用记录 → 3rd 返回值无用
- 零调用点改动, 降低出错风险
- 若未来测试需检查 runner, 可单独构建 (不阻塞当前提取)

---

## 2. 实施步骤（单原子提交）

### Step 1: 移动文件 + 改包名

```bash
mkdir -p pkg/backtest/job
git mv pkg/backtest/job.go pkg/backtest/job/job.go
git mv -f pkg/backtest/job_test.go pkg/backtest/job/job_test.go       # -f: *_test.go gitignored
git mv -f pkg/backtest/job_shutdown_test.go pkg/backtest/job/job_shutdown_test.go
```

三个文件: `package backtest` → `package job`

### Step 2: 重构 `job/job.go` — `*Engine` → `contracts.EngineRunner`

| 行 | 原始 | 修改后 |
|----|------|--------|
| 1 | `package backtest` | `package job` |
| imports | — | 新增 `"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"` |
| 18 | `engine *Engine` | `runner contracts.EngineRunner` |
| 124 | `func NewJobService(store JobStore, engine *Engine) *JobService {` | `func NewJobService(store JobStore, runner contracts.EngineRunner, logger zerolog.Logger) *JobService {` |
| 125-130 | `return &JobService{store: store, engine: engine, logger: engine.logger.With()...}` | `return &JobService{store: store, runner: runner, logger: logger.With().Str("component", "job_service").Logger(), shutdown: make(chan struct{})}` |
| 310 | `backtestReq := BacktestRequest{` | `backtestReq := contracts.BacktestRequest{` |
| 325 | `result, err := s.engine.RunBacktest(jobCtx, backtestReq)` | `result, err := s.runner.RunBacktest(jobCtx, backtestReq)` |
| 542 | `func (s *JobService) SaveSyncResult(ctx context.Context, resp *BacktestResponse) error {` | `func (s *JobService) SaveSyncResult(ctx context.Context, resp *contracts.BacktestResponse) error {` |

### Step 3: 重构 `job/job_test.go` — fakeRunner stub

**imports 变更**:
- 移除: `"github.com/ruoxizhnya/quant-trading/pkg/marketdata"`, `"github.com/spf13/viper"`
- 新增: `"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"`
- 保留: `zerolog` (用于 `zerolog.Nop()`)

**新增 fakeRunner 类型** (放在 `mockJobStore` 定义之后):
```go
// fakeRunner — 测试用 contracts.EngineRunner stub。
// 替代真实 *Engine, 断开 job/ → 父包循环依赖。
// 默认返回非 nil 的空 BacktestResponse, 防止 StartJob 中
// result.TotalReturn 等 logger 字段触发 NPE。
type fakeRunner struct {
    mu    sync.Mutex
    calls []contracts.BacktestRequest
}

func (f *fakeRunner) RunBacktest(ctx context.Context, req contracts.BacktestRequest) (*contracts.BacktestResponse, error) {
    f.mu.Lock()
    f.calls = append(f.calls, req)
    f.mu.Unlock()
    return &contracts.BacktestResponse{}, nil
}
```

**重写 `newTestJobService`** (L150-169):
```go
func newTestJobService(t *testing.T) (*JobService, *mockJobStore) {
    t.Helper()
    store := newMockJobStore()
    runner := &fakeRunner{}
    svc := NewJobService(store, runner, zerolog.Nop())
    return svc, store
}
```
（返回 arity 不变 → **零调用点改动**）

**字段名同步** (L175):
- `assert.NotNil(t, svc.engine)` → `assert.NotNil(t, svc.runner)`

**DTO 引用修复**:
- L361: `&BacktestResponse{` → `&contracts.BacktestResponse{`
- L378: `var result BacktestResponse` → `var result contracts.BacktestResponse`
- L388: `&BacktestResponse{` → `&contracts.BacktestResponse{`

### Step 4: `job/job_shutdown_test.go` — 仅改包名

包名 `backtest` → `job`。无其他修改 (不引用 BacktestResponse/BacktestRequest/svc.engine)。

### Step 5: 扩展 `pkg/backtest/aliases.go`

在 `// --- batch/ subpackage re-exports` 块之后、`var _ contracts.EngineRunner` 之前追加:

```go
// --- job/ subpackage re-exports (S7-P2-1 Commit 8) ---

type (
    JobService       = job.JobService
    JobStore         = job.JobStore
    JobRecord        = job.JobRecord
    CreateJobRequest = job.CreateJobRequest
    Job              = job.Job
)

// NewJobService wrapper preserves the original signature (store, engine *Engine)
// by extracting engine.logger internally, so cmd/analysis/setup.go and all
// external callers compile unchanged.
func NewJobService(store JobStore, engine *Engine) *JobService {
    return job.NewJobService(store, engine, engine.logger)
}
```

在 import 块中新增: `"github.com/ruoxizhnya/quant-trading/pkg/backtest/job"`

---

## 3. 验证清单

```bash
# 1. 全量编译 (含 cmd/analysis, pkg/backtest/reporting)
go build ./...

# 2. 静态分析
go vet ./...

# 3. 格式化检查
gofmt -l .

# 4. 父包 + job/ 测试
go test ./pkg/backtest/ -race -count=1
go test ./pkg/backtest/job/ -race -count=1

# 5. 全套竞态测试 (flakiness gate, 排除 e2e)
go test ./... -race -count=2 -skip '^TestE2E'
```

**预期结果**:
- `go build ./...` 通过 — 所有外部调用方通过 alias 解析
- `go vet` 无警告
- `gofmt -l .` 无输出
- `job/` 包 22 个测试全部通过 (fakeRunner 默认响应安全)
- `pkg/backtest/` 父包测试通过 (state_test.go 等不受影响)
- `cmd/analysis/...` 编译通过 (零改动验证)

---

## 4. Commit Message

```
refactor(backtest/job): extract JobService into job/ leaf

JobService, JobStore, JobRecord, CreateJobRequest, and Job move into
pkg/backtest/job/. The *Engine field is replaced by contracts.EngineRunner;
the logger is now an explicit constructor param (previously stolen from
engine.logger at construction).

The HIGH-risk blocker was newTestJobService, which built a real *Engine
via NewEngine + marketdata.NewInMemoryProvider. Since job/ cannot import
the parent package (cycle), the helper is rewritten to inject a fake
contracts.EngineRunner stub that returns a non-nil empty response. The
2-value return signature (*JobService, *mockJobStore) is preserved so
all ~24 call sites compile unchanged.

The aliases.go wrapper NewJobService(store, engine *Engine) preserves
the original signature by extracting engine.logger internally, so
cmd/analysis/setup.go and all external callers compile unchanged.

This completes S7-P2-1: pkg/backtest is now split into 7 leaf
subpackages (contracts/ cache/ execution/ tracker/ state/ walkforward/
batch/ job/) + the parent facade (engine + aliases).

Refs: S7-P2-1 (Commit 8 of 8, final)
Reviewed: self-review + go vet + gofmt + tests pass
```

---

## 5. S7-P2-1 完成后收尾

Commit 8 通过验证后:
1. 在 `docs/TASKS.md` 中将 S7-P2-1 标记为 ✅ 完成
2. 合并 `feat/s7-p2-1-backtest-leaf-extraction` → `main` (PR 或 fast-forward)
3. 更新 project_memory.md 记录 S7-P2-1 完成状态

---

## 6. 假设与决策

| ID | 决策 | 理由 |
|----|------|------|
| D1 | `newTestJobService` 保持 2-value return | 无测试需检查 runner; 避免 ~24 处调用点改动 |
| D2 | fakeRunner 默认返回 `&contracts.BacktestResponse{}` | 非 nil 防止 StartJob L345 `result.TotalReturn` NPE |
| D3 | 包装器 `NewJobService(store, engine *Engine)` 内部提取 `engine.logger` | 保持 cmd/analysis/setup.go:272 零改动, 沿用 walkforward/ 模式 |
| D4 | `job_shutdown_test.go` 仅改包名 | 9 个测试不引用 DTO/svc.engine, 纯同包字段访问 |
| D5 | 移除 `marketdata`/`viper` import | 仅 `newTestJobService` 用到, 改用 fakeRunner 后不再需要 |
