# S7-P2-1 剩余 5 个原子提交（Commits 4–8）实施计划

> **任务**: S7-P2-1 — 拆分 `pkg/backtest` God Package 为叶子子包
> **分支**: `feat/s7-p2-1-backtest-leaf-extraction`
> **已完成**: Commit 1 (contracts/) `e858a60`、Commit 2 (cache/) `5e11251`、Commit 3 (execution/) `b1a7e31`
> **待执行**: Commit 4 (tracker/) → Commit 5 (state/) → Commit 6 (walkforward/) → Commit 7 (batch/) → Commit 8 (job/)
> **基线**: `go build ./pkg/backtest/...` 通过，working tree clean

---

## 1. 当前状态分析

### 1.1 已建立的提取模式（Commits 1–3 验证有效）

| 手法 | 说明 | 示例 |
|------|------|------|
| **Type alias** | `type X = subpkg.X` — 零成本，调用方零改动 | `type CacheManager = cache.CacheManager` |
| **Const alias** | `const X = subpkg.X` — 编译期常量 | `const DefaultStampTaxRate = contracts.DefaultStampTaxRate` |
| **Var alias** | `var X = subpkg.X` — re-export exported vars | `var ErrQuintileNotFound = cache.ErrQuintileNotFound` |
| **Constructor wrapper** | Go 无函数别名 → 薄包装委托 | `func NewX(...) *X { return sub.NewX(...) }` |
| **Compile-time assertion** | 接口契约守卫 | `var _ contracts.EngineRunner = (*Engine)(nil)` |
| **Peek/Snapshot 公开方法** | 父包访问子包未导出字段的替代 | `cache.Peek(symbol)` / `cache.Snapshot()` |
| **Test stub duplication** | 测试文件随源码移动后，父包需补 stub | `fakeprovider_test.go`、`fakes_test.go` |

### 1.2 关键跨叶子依赖图（决定提交顺序）

```
contracts (LEAF) ←── tracker (LEAF)
                   ↑
                   ├── state (LEAF)          # state.go:56 引用 *tracker.Tracker
                   │
contracts ←── walkforward (LEAF)             # 引用 BacktestRequest/Response + EngineRunner + logger
                ↑
                ├── batch (LEAF)             # batch.go 引用 *walkforward.WalkForwardEngine
                │
contracts ←── job (LEAF)                    # 引用 BacktestRequest/Response + EngineRunner + logger
```

**强制定序**:
- Commit 4 (tracker/) 必须在 Commit 5 (state/) 之前 — state.go:56 `Tracker *Tracker` 将变为 `*tracker.Tracker`
- Commit 6 (walkforward/) 必须在 Commit 7 (batch/) 之前 — batch.go 引用 `WalkForwardEngine`

### 1.3 外部调用方（cmd/analysis/*、reporting/compare.go）

经 grep 确认，外部调用方使用以下 `backtest.*` 符号：
- 构造器: `NewJobService`、`NewWalkForwardEngine`、`NewBatchEngine`、`DefaultBatchConfig`
- 类型: `JobService`、`WalkForwardEngine`、`BatchEngine`、`BacktestResponse`、`CreateJobRequest`、`BatchTask`、`WalkForwardRequest`、`Engine`
- 调用点: `cmd/analysis/setup.go:272,275,278`、`cmd/analysis/deps.go:30,33,36`、`cmd/analysis/handlers_{backtest,batch,walkforward}.go`、`pkg/backtest/reporting/compare.go:117`

**策略**: 沿用 Commits 1–3 模式 — 所有移动的类型/构造器在 `aliases.go` 中 re-export，**外部调用方零改动**。

---

## 2. 5 个原子提交详细方案

### Commit 4: 提取 `tracker/` 子包

**目标**: 将 `Tracker`、`OrderLog`、`OrderExecutionOpts`、`NewTracker` 从 `pkg/backtest/tracker.go` + `order_log.go` 移至 `pkg/backtest/tracker/`。

**步骤**:

1. **移动文件** (`git mv -f` 因 `*_test.go` 被 .gitignore L28 忽略):
   - `pkg/backtest/tracker.go` → `pkg/backtest/tracker/tracker.go`
   - `pkg/backtest/order_log.go` → `pkg/backtest/tracker/order_log.go`
   - `pkg/backtest/tracker_test.go` → `pkg/backtest/tracker/tracker_test.go`
   - `pkg/backtest/tracker_t1_test.go` → `pkg/backtest/tracker/tracker_t1_test.go`
   - `pkg/backtest/tracker_limit_test.go` → `pkg/backtest/tracker/tracker_limit_test.go`
   - `pkg/backtest/tracker_split_test.go` → `pkg/backtest/tracker/tracker_split_test.go`
   - `pkg/backtest/tracker_ghost_test.go` → `pkg/backtest/tracker/tracker_ghost_test.go`
   - `pkg/backtest/tracker_settlement_test.go` → `pkg/backtest/tracker/tracker_settlement_test.go`
   - `pkg/backtest/property_test.go` → `pkg/backtest/tracker/property_test.go` (TestProperty_T1Enforced 等 property 测试主要测 Tracker；详见下方"注意")

2. **修改包名**: 所有移动文件 `package backtest` → `package tracker`

3. **修复 `tracker/tracker.go` 中 5 处父包引用** (现已移至 contracts/):
   - L38: `trading TradingConfig` → `trading contracts.TradingConfig`
   - L47: `NewTracker` 参数 `TradingConfig` → `contracts.TradingConfig`
   - L50: `defaultTradingConfig()` → `contracts.DefaultTradingConfig()`
   - L60: `DefaultShortSellingRate` → `contracts.DefaultShortSellingRate`
   - L744: `TradingDaysPerYear` → `contracts.TradingDaysPerYear`
   - 新增 import: `"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"`

4. **修复移动测试中的 `defaultTradingConfig()` 引用**:
   - `tracker_t1_test.go:11` `newTestTracker` 调用 `defaultTradingConfig()` → 改为 `contracts.DefaultTradingConfig()`
   - 其他 tracker 测试文件中若有同样引用，一并修复

5. **修改 `pkg/backtest/state.go:56`**:
   - `Tracker *Tracker` → `Tracker *tracker.Tracker`
   - 新增 import: `"github.com/ruoxizhnya/quant-trading/pkg/backtest/tracker"`

6. **扩展 `pkg/backtest/aliases.go`** 追加:
   ```go
   import "github.com/ruoxizhnya/quant-trading/pkg/backtest/tracker"

   type (
       Tracker           = tracker.Tracker
       OrderLog          = tracker.OrderLog
       OrderExecutionOpts = tracker.OrderExecutionOpts
   )

   func NewTracker(initialCapital, commissionRate, stampTaxRate float64,
       trading contracts.TradingConfig, logger zerolog.Logger) *Tracker {
       return tracker.NewTracker(initialCapital, commissionRate, stampTaxRate, trading, logger)
   }
   ```

7. **验证 property_test.go 归属** (CRITICAL — 这是 S7-P0-17 的金丝雀测试):
   - 先用 grep 确认 property_test.go 主要测试 Tracker 行为（T+1、ghost position 等），不依赖父包 Engine
   - 若确认 → 移动到 `tracker/property_test.go`
   - 若有父包依赖 → 保留在父包，但需修复其中对 Tracker 的引用（通过 alias 可保持不变）

**注意 — property_test.go 决策**: memory 记录 "property_test.go was a local-only untracked file until S7-P0-17 force-added it"。它使用 `testing/quick`，是 Tracker 不变式的金丝雀。需先 `Read` 该文件确认其 import 和 helper 依赖，再决定移动与否。

**预期编译错误** (按 Commits 2–3 经验):
- 父包测试中 `NewTracker(...)` 调用 — 通过 alias 包装器解决（参数顺序需匹配）
- 若 property_test.go 保留在父包 — 它对 `Tracker` 的引用通过 alias 解决，无需改动

**验证**:
```bash
go build ./pkg/backtest/...
go vet ./pkg/backtest/...
gofmt -l pkg/backtest/
go test ./pkg/backtest/tracker/... -race -count=1
go test ./pkg/backtest/ -race -count=1
```

**Commit message**:
```
refactor(backtest/tracker): extract Tracker + OrderLog into tracker/ leaf

Tracker, OrderLog, OrderExecutionOpts, and NewTracker move from the
parent package into pkg/backtest/tracker/. Parent-package callers
(engine.go, state.go, *_test.go) keep compiling unchanged via type
aliases and a NewTracker wrapper in aliases.go.

5 references to trading-rule constants/types in tracker.go now resolve
through pkg/backtest/contracts (TradingConfig, DefaultTradingConfig,
DefaultShortSellingRate, TradingDaysPerYear). state.go's Tracker field
is retyped to *tracker.Tracker.

Refs: S7-P2-1 (Commit 4 of 8)
Reviewed: self-review + go vet + gofmt + go test -race
```

---

### Commit 5: 提取 `state/` 子包

**目标**: 将 `BacktestState`、`BacktestStateSnapshot`、`StateStore`、`LRUStateStore`、`NoopStateStore`、`DiskStateStore`、`DefaultStateStoreCapacity` 从 `pkg/backtest/state.go` + `state_store.go` + `persistence.go` + `constants.go` 移至 `pkg/backtest/state/`。

**步骤**:

1. **移动文件**:
   - `pkg/backtest/state.go` → `pkg/backtest/state/state.go`
   - `pkg/backtest/state_store.go` → `pkg/backtest/state/state_store.go`
   - `pkg/backtest/persistence.go` → `pkg/backtest/state/persistence.go`
   - `pkg/backtest/persistence_test.go` → `pkg/backtest/state/persistence_test.go` (整体移动，自包含)

2. **修改包名**: `package backtest` → `package state`

3. **修改 `state/state.go:56`** (Commit 4 后已是 `*tracker.Tracker`):
   - 新增 import: `"github.com/ruoxizhnya/quant-trading/pkg/backtest/tracker"`
   - 字段类型不变（Commit 4 已改），但 import 需补上

4. **移动 `DefaultStateStoreCapacity` 常量**:
   - 从 `pkg/backtest/constants.go` 移到 `pkg/backtest/state/state_store.go`
   - 在 `aliases.go` 追加: `const DefaultStateStoreCapacity = state.DefaultStateStoreCapacity`

5. **拆分 `state_test.go`** (CRITICAL — 该文件混合):
   - **移动部分** (L19–271，6 个 TestBacktestState_* 测试) → `pkg/backtest/state/state_test.go`，改包名为 `state`
   - **保留部分** (L278+，`TestBacktestState_EngineGetBacktestStatusRace`，依赖 `newTestEngine`/`NewTracker`/`defaultTradingConfig()`) → 留在父包 `pkg/backtest/state_test.go`
   - 保留部分通过 aliases 访问 `NewTracker`、`defaultTradingConfig()`、`BacktestState` — 无需改动

6. **保留 `state_store_integration_test.go` 在父包** (测 Engine↔StateStore 集成边界，依赖 `newTestEngine`/`WithStateStore` 等)

7. **扩展 `pkg/backtest/aliases.go`** 追加:
   ```go
   import "github.com/ruoxizhnya/quant-trading/pkg/backtest/state"

   type (
       BacktestState         = state.BacktestState
       BacktestStateSnapshot = state.BacktestStateSnapshot
       StateStore            = state.StateStore
       LRUStateStore         = state.LRUStateStore
       NoopStateStore        = state.NoopStateStore
       DiskStateStore        = state.DiskStateStore
   )

   var (
       ErrStateNotFound  = state.ErrStateNotFound
       ErrInvalidStateID = state.ErrInvalidStateID
   )

   func NewLRUStateStore(capacity int) *LRUStateStore { return state.NewLRUStateStore(capacity) }
   func NewNoopStateStore() *NoopStateStore           { return state.NewNoopStateStore() }
   func NewDiskStateStore(dir string) (*DiskStateStore, error) { return state.NewDiskStateStore(dir) }
   ```

**注意**: `state/` 是最干净的叶子 — **零 `*Engine` 引用、零 `logger` 引用、零 `RunBacktest` 调用**。Commit 4 完成后，state.go 的 `*tracker.Tracker` 字段类型已就位，无需额外接口替换。

**验证**:
```bash
go build ./pkg/backtest/...
go vet ./pkg/backtest/...
gofmt -l pkg/backtest/
go test ./pkg/backtest/state/... -race -count=1
go test ./pkg/backtest/ -race -count=1
```

**Commit message**:
```
refactor(backtest/state): extract BacktestState + StateStore into state/ leaf

BacktestState, BacktestStateSnapshot, StateStore interface, LRUStateStore,
NoopStateStore, DiskStateStore, and DefaultStateStoreCapacity move from
the parent package into pkg/backtest/state/. The state/ leaf is the
cleanest extraction: zero *Engine references, zero logger references,
zero RunBacktest calls — it only depends on domain + tracker siblings.

state.go's Tracker field is already *tracker.Tracker (from Commit 4);
state/ adds the tracker import. persistence_test.go moves cleanly
(self-contained). state_test.go is split: 6 BacktestState unit tests
move to state/; the Engine-integration race test stays in the parent.

DefaultStateStoreCapacity is re-exported via aliases.go so constants.go
and all parent callers keep compiling unchanged.

Refs: S7-P2-1 (Commit 5 of 8)
Reviewed: self-review + go vet + gofmt + go test -race
```

---

### Commit 6: 提取 `walkforward/` 子包

**目标**: 将 `WalkForwardEngine`、`WalkForwardRequest`、`NewWalkForwardEngine` 从 `pkg/backtest/walkforward.go` 移至 `pkg/backtest/walkforward/`，并完成 `*Engine` → `contracts.EngineRunner` 接口替换。

**步骤**:

1. **移动文件**:
   - `pkg/backtest/walkforward.go` → `pkg/backtest/walkforward/walkforward.go`
   - `pkg/backtest/walkforward_test.go` → `pkg/backtest/walkforward/walkforward_test.go`
   - `pkg/backtest/walkforward_metrics_test.go` → `pkg/backtest/walkforward/walkforward_metrics_test.go`
   - `pkg/backtest/walkforward_overfit_test.go` → `pkg/backtest/walkforward/walkforward_overfit_test.go`

2. **修改包名**: `package backtest` → `package walkforward`

3. **核心重构 — `*Engine` → `contracts.EngineRunner` + 新增 `logger` 字段** (5 处 logger 偷渡自 engine.logger):
   - `walkforward.go:17`: `engine *Engine` → `runner contracts.EngineRunner`
   - `walkforward.go:18`: 新增 `logger zerolog.Logger` 字段
   - `walkforward.go:22`: 构造器 `NewWalkForwardEngine(engine *Engine, store *storage.PostgresStore)` → `NewWalkForwardEngine(runner contracts.EngineRunner, store *storage.PostgresStore, logger zerolog.Logger)`
   - 构造器体内: `logger: logger.With().Str("component", "walkforward").Logger()`
   - L100, L128, L179, L212, L218: `wf.engine.logger.X` → `wf.logger.X`
   - L210, L216: `wf.engine.RunBacktest(ctx, req)` → `wf.runner.RunBacktest(ctx, req)`
   - 新增 import: `"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"`, `"github.com/rs/zerolog"`

4. **修复 `walkforward.go` 中 DTO 引用** (现已在 contracts/):
   - L192, L201: `BacktestRequest{...}` → `contracts.BacktestRequest{...}`
   - L440: `func (wf *WalkForwardEngine) toBacktestResult(r *BacktestResponse)` → `r *contracts.BacktestResponse`

5. **修复移动测试中的引用**:
   - `walkforward_test.go:217`, `walkforward_metrics_test.go:181`: `BacktestResponse` → `contracts.BacktestResponse`
   - 测试中 `&WalkForwardEngine{}` 构造 — 需用 `NewWalkForwardEngine(nil, nil, zerolog.Nop())` 或直接构造体（runner/logger 为 nil 时仅测纯函数方法如 buildWindows/computeAggregateMetrics/detectOverfitting，不触发 RunBacktest）

6. **扩展 `pkg/backtest/aliases.go`** 追加:
   ```go
   import "github.com/ruoxizhnya/quant-trading/pkg/backtest/walkforward"

   type (
       WalkForwardEngine  = walkforward.WalkForwardEngine
       WalkForwardRequest = walkforward.WalkForwardRequest
   )

   func NewWalkForwardEngine(runner contracts.EngineRunner, store *storage.PostgresStore, logger zerolog.Logger) *WalkForwardEngine {
       return walkforward.NewWalkForwardEngine(runner, store, logger)
   }
   ```
   - **注意**: `aliases.go` 已 import `storage`？需检查。若未 import，需新增。

7. **更新外部调用方 `cmd/analysis/setup.go:275`**:
   - 当前: `wfEngine := backtest.NewWalkForwardEngine(engine, store)`
   - 新: `wfEngine := backtest.NewWalkForwardEngine(engine, store, logger)`
   - **虽然 aliases.go 包装器签名变了，但 cmd/analysis 调用点必须同步加 logger 参数** — 这是少数需改外部调用方的情况（因为构造器签名扩展了参数）。

   **决策**: 为保持外部零改动，可让 `aliases.go` 的 `NewWalkForwardEngine` 包装器保留旧签名 `NewWalkForwardEngine(engine *Engine, store)`，内部从 `engine.logger` 取 logger 传给子包：
   ```go
   func NewWalkForwardEngine(engine *Engine, store *storage.PostgresStore) *WalkForwardEngine {
       return walkforward.NewWalkForwardEngine(engine, store, engine.logger)
   }
   ```
   **但** `engine.logger` 是未导出字段 — 父包 aliases.go 在 backtest 包内，**可以访问**未导出字段。这样 cmd/analysis 零改动。
   - **采用此方案**（与 job/ 的处理不同 — job 的 logger 在构造时被 "偷走"，walkforward 是运行时直接读 engine.logger，所以包装器可代为提取）。

**验证**:
```bash
go build ./pkg/backtest/... ./cmd/analysis/...
go vet ./pkg/backtest/... ./cmd/analysis/...
gofmt -l pkg/backtest/ pkg/...
go test ./pkg/backtest/walkforward/... -race -count=1
go test ./pkg/backtest/ -race -count=1
```

**Commit message**:
```
refactor(backtest/walkforward): extract WalkForwardEngine into walkforward/ leaf

WalkForwardEngine and WalkForwardRequest move into pkg/backtest/walkforward/.
The *Engine field is replaced by contracts.EngineRunner (narrow interface)
plus a dedicated logger zerolog.Logger field — previously the engine
reached into engine.logger at 5 sites, which coupled it to the concrete
*Engine type. Now the leaf depends only on contracts + storage + statistics.

The aliases.go wrapper NewWalkForwardEngine(engine *Engine, store) preserves
the original signature by extracting engine.logger internally, so all
external callers (cmd/analysis/setup.go) compile unchanged.

Refs: S7-P2-1 (Commit 6 of 8)
Reviewed: self-review + go vet + gofmt + go test -race
```

---

### Commit 7: 提取 `batch/` 子包

**目标**: 将 `BatchEngine`、`BatchConfig`、`BatchTask`、`BatchResult`、`BatchScore`、`BatchReport`、`BatchSummary`、`Scorer`、`NewBatchEngine`、`DefaultBatchConfig`、及 7 个包级函数从 `pkg/backtest/batch.go` + `batch_scorer.go` 移至 `pkg/backtest/batch/`，并完成 `*Engine` → `contracts.EngineRunner` 替换。

**步骤**:

1. **移动文件**:
   - `pkg/backtest/batch.go` → `pkg/backtest/batch/batch.go`
   - `pkg/backtest/batch_scorer.go` → `pkg/backtest/batch/batch_scorer.go`
   - `pkg/backtest/batch_test.go` → `pkg/backtest/batch/batch_test.go`
   - `pkg/backtest/batch_score_test.go` → `pkg/backtest/batch/batch_score_test.go`
   - `pkg/backtest/batch_csv_test.go` → `pkg/backtest/batch/batch_csv_test.go`

2. **修改包名**: `package backtest` → `package batch`

3. **核心重构 — `*Engine` → `contracts.EngineRunner`**:
   - `batch.go:100`: `engine *Engine` → `runner contracts.EngineRunner`
   - **`BatchEngine` 已有自己的 `logger zerolog.Logger` 字段** (L103) — 无需新增 logger
   - `batch.go:111`: 构造器 `NewBatchEngine(engine *Engine, wfEng *WalkForwardEngine, config BatchConfig, logger zerolog.Logger)` → `NewBatchEngine(runner contracts.EngineRunner, wfEng *walkforward.WalkForwardEngine, config BatchConfig, logger zerolog.Logger)`
   - `batch.go:231`: `b.engine.RunBacktest(ctx, req)` → `b.runner.RunBacktest(ctx, req)`
   - 新增 import: `contracts`, `walkforward`

4. **修复 `batch.go` 中跨叶子引用**:
   - L101: `wfEng *WalkForwardEngine` → `wfEng *walkforward.WalkForwardEngine`
   - L245: `WalkForwardRequest{...}` → `walkforward.WalkForwardRequest{...}`
   - L258: `b.wfEng.RunWalkForward(...)` 不变（方法在 *WalkForwardEngine 上）
   - L222: `BacktestRequest{...}` → `contracts.BacktestRequest{...}`
   - L404: `func toBacktestResult(r *BacktestResponse)` → `r *contracts.BacktestResponse`

5. **修复移动测试中的引用** (低风险 — 测试传 nil engine/wfEng):
   - `batch_test.go`、`batch_score_test.go`、`batch_csv_test.go` 中对 `WalkForwardEngine`/`WalkForwardRequest`/`BacktestRequest`/`BacktestResponse` 的引用 → 加 `contracts.` 或 `walkforward.` 前缀
   - `&BatchEngine{}` 直接构造 — runner 字段为 nil 时只测纯函数方法

6. **扩展 `pkg/backtest/aliases.go`** 追加:
   ```go
   import "github.com/ruoxizhnya/quant-trading/pkg/backtest/batch"

   type (
       BatchEngine  = batch.BatchEngine
       BatchConfig  = batch.BatchConfig
       BatchTask    = batch.BatchTask
       BatchResult  = batch.BatchResult
       BatchScore   = batch.BatchScore
       BatchReport  = batch.BatchReport
       BatchSummary = batch.BatchSummary
       Scorer       = batch.Scorer
   )

   func NewBatchEngine(runner contracts.EngineRunner, wfEng *WalkForwardEngine, config BatchConfig, logger zerolog.Logger) *BatchEngine {
       return batch.NewBatchEngine(runner, wfEng, config, logger)
   }
   func NewScorer(...) *Scorer { return batch.NewScorer(...) }  // 按 batch_scorer.go:23 实际签名
   func DefaultBatchConfig() BatchConfig { return batch.DefaultBatchConfig() }
   ```
   - `NewBatchEngine` 包装器签名与子包一致 — cmd/analysis 调用点 `backtest.NewBatchEngine(engine, wfEngine, backtest.DefaultBatchConfig(), logger)` 通过 alias 类型 `*WalkForwardEngine = *walkforward.WalkForwardEngine` 隐式转换，**零改动**。

7. **外部调用方验证**: `cmd/analysis/setup.go:278`、`handlers_batch.go` 全部使用 `backtest.BatchEngine`/`backtest.NewBatchEngine`/`backtest.DefaultBatchConfig`/`backtest.BatchTask` — 全部通过 alias 解析，**零改动**。

**验证**:
```bash
go build ./pkg/backtest/... ./cmd/analysis/...
go vet ./pkg/backtest/... ./cmd/analysis/...
gofmt -l pkg/backtest/ pkg/...
go test ./pkg/backtest/batch/... -race -count=1
go test ./pkg/backtest/ -race -count=1
```

**Commit message**:
```
refactor(backtest/batch): extract BatchEngine + Scorer into batch/ leaf

BatchEngine, BatchConfig, BatchTask, BatchResult, BatchScore, BatchReport,
BatchSummary, Scorer, and 7 package-level helpers move into
pkg/backtest/batch/. The *Engine field is replaced by contracts.EngineRunner;
BatchEngine already owns its logger field (no new param needed).

batch/ imports the walkforward/ sibling (BatchEngine.wfEng field type
becomes *walkforward.WalkForwardEngine). The cross-leaf dependency
batch → walkforward is one-way and acyclic.

All external callers (cmd/analysis/setup.go, handlers_batch.go) keep
compiling via aliases.go re-exports.

Refs: S7-P2-1 (Commit 7 of 8)
Reviewed: self-review + go vet + gofmt + go test -race
```

---

### Commit 8: 提取 `job/` 子包（含 HIGH 风险 stub 修复）

**目标**: 将 `JobService`、`JobStore`、`JobRecord`、`CreateJobRequest`、`Job`、`NewJobService` 从 `pkg/backtest/job.go` 移至 `pkg/backtest/job/`，并完成 `*Engine` → `contracts.EngineRunner` 替换 + 解决 `newTestJobService` 真实 Engine 依赖。

**步骤**:

1. **移动文件**:
   - `pkg/backtest/job.go` → `pkg/backtest/job/job.go`
   - `pkg/backtest/job_test.go` → `pkg/backtest/job/job_test.go`
   - `pkg/backtest/job_shutdown_test.go` → `pkg/backtest/job/job_shutdown_test.go`

2. **修改包名**: `package backtest` → `package job`

3. **核心重构 — `*Engine` → `contracts.EngineRunner` + 新增 logger 参数**:
   - `job.go:18`: `engine *Engine` → `runner contracts.EngineRunner`
   - `job.go:19`: `logger zerolog.Logger` 字段保留
   - `job.go:124`: 构造器 `NewJobService(store JobStore, engine *Engine) *JobService` → `NewJobService(store JobStore, runner contracts.EngineRunner, logger zerolog.Logger) *JobService`
   - `job.go:128`: `logger: engine.logger.With().Str("component", "job_service").Logger()` → `logger: logger.With().Str("component", "job_service").Logger()`
   - `job.go:325`: `s.engine.RunBacktest(jobCtx, backtestReq)` → `s.runner.RunBacktest(jobCtx, backtestReq)`
   - `job.go:175`: `assert.NotNil(t, svc.engine)` → `assert.NotNil(t, svc.runner)` (测试中字段名同步)
   - 新增 import: `contracts`, `zerolog`

4. **修复 `job.go` 中 DTO 引用**:
   - L310: `BacktestRequest{...}` → `contracts.BacktestRequest{...}`
   - L542: `SaveSyncResult` 参数 `*BacktestResponse` → `*contracts.BacktestResponse`

5. **HIGH 风险 — 重写 `newTestJobService` (job_test.go:150)**:
   - **问题**: 当前实现 `eng, _ := NewEngine(v, marketdata.NewInMemoryProvider(), zerolog.Nop()); svc := NewJobService(store, eng)` — `NewEngine` 在父包，job/ 不能 import 父包（循环）。
   - **方案**: 用 fake `contracts.EngineRunner` stub 替换真实 Engine:
     ```go
     // fakeRunner — 测试用 contracts.EngineRunner stub
     type fakeRunner struct {
         mu        sync.Mutex
         calls     []BacktestRequest
         resp      *BacktestResponse
         err       error
     }
     func (f *fakeRunner) RunBacktest(ctx context.Context, req contracts.BacktestRequest) (*contracts.BacktestResponse, error) {
         f.mu.Lock()
         defer f.mu.Unlock()
         f.calls = append(f.calls, req)
         return f.resp, f.err
     }
     ```
   - 重写 `newTestJobService`:
     ```go
     func newTestJobService(t *testing.T) (*JobService, *mockJobStore, *fakeRunner) {
         t.Helper()
         store := newMockJobStore()
         runner := &fakeRunner{}
         svc := NewJobService(store, runner, zerolog.Nop())
         return svc, store, runner
     }
     ```
   - 审查所有测试用例 — 需要真实 Engine 行为的测试（如 `StartJob` 触发 `RunBacktest`）改为先配置 `fakeRunner.resp` 返回预设响应。
   - `TestJobService_NewJobService` (L171): `assert.NotNil(t, svc.engine)` → `assert.NotNil(t, svc.runner)`

6. **扩展 `pkg/backtest/aliases.go`** 追加:
   ```go
   import "github.com/ruoxizhnya/quant-trading/pkg/backtest/job"

   type (
       JobService       = job.JobService
       JobStore         = job.JobStore
       JobRecord        = job.JobRecord
       CreateJobRequest = job.CreateJobRequest
       Job              = job.Job
   )

   func NewJobService(store JobStore, engine *Engine) *JobService {
       return job.NewJobService(store, engine, engine.logger)
   }
   ```
   - **关键**: 包装器保留旧签名 `NewJobService(store, engine *Engine)`，内部从 `engine.logger` 取 logger — cmd/analysis/setup.go:272 `backtest.NewJobService(store, engine)` **零改动**。

7. **外部调用方验证**: `cmd/analysis/setup.go:272,483`、`handlers_backtest.go:19,291`、`deps.go:30`、`reporting/compare.go:117` 全部通过 alias 解析，**零改动**。

**验证**:
```bash
go build ./...                        # 全量编译（含 cmd/analysis）
go vet ./...
gofmt -l .
go test ./pkg/backtest/... -race -count=1
go test ./pkg/backtest/ -race -count=1   # 父包剩余测试
go test ./cmd/analysis/... -race -count=1   # 若有
```

**Commit message**:
```
refactor(backtest/job): extract JobService into job/ leaf

JobService, JobStore, JobRecord, CreateJobRequest, and Job move into
pkg/backtest/job/. The *Engine field is replaced by contracts.EngineRunner;
the logger is now an explicit constructor param (previously stolen from
engine.logger at construction).

The HIGH-risk blocker was newTestJobService, which built a real *Engine
via NewEngine + marketdata.NewInMemoryProvider. Since job/ cannot import
the parent package (cycle), the helper is rewritten to inject a fake
contracts.EngineRunner stub. Tests that exercise RunBacktest configure
the stub's canned response.

The aliases.go wrapper NewJobService(store, engine *Engine) preserves
the original signature by extracting engine.logger internally, so
cmd/analysis/setup.go and all external callers compile unchanged.

Refs: S7-P2-1 (Commit 8 of 8, final)
Reviewed: self-review + go vet + gofmt + go test -race
```

---

## 3. 全局收尾验证（Commit 8 后）

```bash
# 1. 全量构建
go build ./...

# 2. 全量静态检查
go vet ./...

# 3. 格式化检查
gofmt -l . | grep -v '^vendor/' | grep -v '_test.go$'  # 仅查非测试源

# 4. 全量测试（含 race + count=2 暴露全局状态泄漏）
go test ./... -race -count=2 -skip '^e2e'

# 5. 确认外部调用方零改动（git diff 应无 cmd/analysis/ 改动）
git diff main..HEAD --stat -- cmd/analysis/ pkg/backtest/reporting/
# 预期: 0 files changed in cmd/analysis/ (除 setup.go 因 walkforward/job 包装器签名兼容而无需改)
```

**注意**: 若 Commit 6/8 采用"包装器内部提取 engine.logger"方案，则 cmd/analysis/setup.go **完全零改动**。若因签名问题需改 setup.go，应在对应 commit 内一并完成并验证 `go build ./cmd/analysis/...`。

---

## 4. 假设与决策

### 4.1 关键决策

| # | 决策 | 理由 |
|---|------|------|
| D1 | **采用 `*Engine → contracts.EngineRunner` 窄接口** | ADR-020 §6 已确立；解耦 job/walkforward/batch 对具体 Engine 的依赖 |
| D2 | **walkforward/job 包装器内部提取 `engine.logger`** | 保持外部调用方零改动；父包 aliases.go 可访问未导出字段 |
| D3 | **batch 构造器签名直接改为 EngineRunner** | BatchEngine 已有 logger 参数，外部调用方 `backtest.NewBatchEngine(engine, wfEngine, cfg, logger)` 中 `engine` 通过 `*Engine` 满足 `EngineRunner` 接口（*Engine 已实现 RunBacktest），**零改动** |
| D4 | **property_test.go 归属待 Phase 1 重读确认** | memory 标记其为 S7-P0-17 金丝雀；若主要测 Tracker 则移动，否则保留 |
| D5 | **state_test.go 拆分** | 6 个纯 BacktestState 测试移动；1 个 Engine 集成测试保留 |
| D6 | **newTestJobService 重写为 fakeRunner** | 唯一避免循环 import 的方案；测试覆盖不退化 |

### 4.2 假设

- Commits 1–3 已建立的 alias/包装器模式在本批 5 个 commit 中继续有效
- `engine.logger` 是未导出字段，父包 aliases.go 可访问（同包）
- `*Engine` 已实现 `contracts.EngineRunner` 接口（Commit 1 的 `var _ contracts.EngineRunner = (*Engine)(nil)` 守卫）
- `storage.PostgresStore` 是外部类型，子包可直接 import（无循环）

### 4.3 风险与缓解

| 风险 | 等级 | 缓解 |
|------|------|------|
| property_test.go 移动后破坏 T1 金丝雀 | HIGH | 移动前 Read 确认依赖；若有父包依赖则保留并依赖 alias |
| newTestJobService 重写后测试覆盖退化 | HIGH | fakeRunner 配置预设响应；逐个测试审查 RunBacktest 调用点 |
| engine.logger 未导出字段在 aliases.go 不可访问 | LOW | 同包可访问（Go 规则） |
| 外部调用方意外需要改动 | LOW | 包装器签名兼容方案；每 commit 跑 `go build ./cmd/analysis/...` |
| 测试 stub 重复（Commits 2–3 模式） | MEDIUM | 按 fakeprovider_test.go/fakes_test.go 先例补 stub |

---

## 5. 执行顺序总览

```
Commit 4: tracker/    (移动 + 5 contracts 引用 + state.go 字段重类型 + aliases 扩展)
    ↓
Commit 5: state/      (移动 + DefaultStateStoreCapacity 移动 + state_test.go 拆分 + aliases 扩展)
    ↓
Commit 6: walkforward/ (移动 + *Engine→EngineRunner + 新增 logger 字段/参数 + aliases 包装器内部提取 logger)
    ↓
Commit 7: batch/       (移动 + *Engine→EngineRunner + 引用 walkforward 兄弟 + aliases 扩展)
    ↓
Commit 8: job/        (移动 + *Engine→EngineRunner + logger 显式参数 + newTestJobService 重写 + aliases 包装器内部提取 logger)
    ↓
全局收尾验证 + go test -race -count=2
```

每个 commit 后立即验证 `go build + go vet + gofmt + go test -race`，确保 atomic commit 可独立构建/测试通过。

---

## 6. 完成标准

- [ ] 5 个原子 commit 全部完成，每个可独立构建+测试通过
- [ ] `pkg/backtest/` 下仅保留: engine.go、engine_daily.go、options.go、constants.go、aliases.go、auction/、marketimpact/、metrics/、reporting/、cache/、contracts/、execution/、tracker/、state/、walkforward/、batch/、job/、各 engine_*_test.go、options_test.go、fakeprovider_test.go、fakes_test.go、constants_drift_test.go、state_test.go (拆分后保留部分)、state_store_integration_test.go
- [ ] `go build ./...` 通过
- [ ] `go vet ./...` 无警告
- [ ] `gofmt -l .` 无输出（已格式化）
- [ ] `go test ./... -race -count=2` 通过（excl. /e2e）
- [ ] cmd/analysis/ 外部调用方零改动（或仅 setup.go 因包装器签名兼容而无实质改动）
- [ ] constants_drift_test.go 仍通过（alias 保持常量等价）
- [ ] property_test.go (T1 金丝雀) 仍通过
- [ ] TASKS.md 中 S7-P2-1 标记为完成
