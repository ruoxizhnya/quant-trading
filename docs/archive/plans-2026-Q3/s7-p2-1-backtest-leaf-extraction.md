# S7-P2-1: 拆分 pkg/backtest 上帝包为子包 (Phase 1: 叶子+接口)

> **Task ID**: S7-P2-1 (来自 `docs/TASKS.md`)
> **Branch**: `feat/s7-p2-1-backtest-leaf-extraction` (已创建)
> **Approach**: 增量式叶子+接口拆分 (用户已批准)
> **Scope**: 7 个叶子子包 + `contracts/` 接口解耦层
> **Defers**: `engine.go` / `engine_daily.go` 内部拆分 → S7-P2-1b

---

## Summary

`pkg/backtest` 当前是上帝包：57 个 `.go` 文件（含 42 个测试文件）、~9000 行生产代码，
单包内混杂了引擎、Tracker、Job 调度、Walk-Forward、Batch、Cache、Execution 等多个
职责域。本任务采用**增量式叶子+接口**策略，提取 7 个叶子子包 + 1 个 `contracts/`
接口解耦层，通过 Go type/const alias 实现消费者零改动。

**核心约束**:
- 子包**不能** import 父包 `pkg/backtest`（避免环）→ 共享 DTO 先入 `contracts/`
- 父包通过 `type X = subpackage.X` / `const X = subpackage.X` 重导出（零成本 alias）
- 消费者（`cmd/analysis/*`, `pkg/backtest/reporting/*`）使用 `backtest.X` 形式 → 经 alias 解析 → **零改动**
- 每个子包独立编译 + 独立测试，符合 S7 执行规范 (ODR-043) 三原则（测试先行 / 代码审查 / 原子提交）

---

## Current State Analysis (Phase 1 探索结果)

### 现有目录结构
```
pkg/backtest/
├── engine.go (1434 行)         ← God Object (28 个导出方法), 留在父包
├── engine_daily.go (722 行)    ← 12 个 *Engine 方法, 留在父包
├── constants.go (89 行)        ← 交易规则常量 + 运营常量 + StateStore 常量
├── tracker.go (922 行)         ← Tracker struct (23 方法) → tracker/
├── order_log.go (20 行)        ← OrderLog struct → tracker/ (仅 tracker 使用)
├── state.go (208 行)          ← BacktestState → state/ (依赖 Tracker)
├── state_store.go (234 行)    ← StateStore iface + LRUStateStore → state/
├── persistence.go (265 行)    ← DiskStateStore → state/
├── cache.go (195 行)          ← CacheManager (纯叶子) → cache/
├── factor_cache.go (250 行)   ← FactorCacheAccessor (纯叶子) → cache/
├── execution.go (151 行)     ← ExecutionService iface → execution/
├── execution_bridge.go (89 行)← ExecutionBridge → execution/
├── walkforward.go (478 行)    ← WalkForwardEngine → walkforward/
├── batch.go (711 行)          ← BatchEngine (持有 *Engine + *WalkForwardEngine) → batch/
├── batch_scorer.go (176 行)   ← Scorer → batch/
├── job.go (613 行)            ← JobService → job/
├── live_bridge.go (208 行)   ← LiveBridge → execution/ (紧密耦合 ExecutionService)
├── options.go (160 行)        ← EngineOption → 留父包 (Engine 构造选项)
└── *_test.go (42 个测试文件)
```

### 关键依赖图 (已验证无环)
```
contracts (LEAF: domain + stdlib only)
   ▲
   ├── cache/        (domain + marketdata)
   ├── execution/    (domain + live)
   ├── tracker/      (domain + fees + portfolio + settlement + contracts)
   ├── state/        (domain + tracker)   ← state.go:56 Tracker *Tracker
   ├── walkforward/  (contracts.EngineRunner + storage + statistics)
   ├── batch/        (walkforward + storage + domain)
   └── job/          (contracts.EngineRunner + storage + domain)

parent pkg/backtest (留 Engine + Options + Config + Daily loop)
   imports: cache/, execution/, tracker/, state/, walkforward/, batch/, job/
   re-exports via aliases.go
```

### 父包需保留的 (Engine 生命周期 + 配置)
- `engine.go` 的 `Engine` struct + `Config` struct + `NewEngine` / `NewEngineWithOptions`
- `engine_daily.go` 的 12 个 *Engine 日报循环方法
- `constants.go` 的运营常量 (`DefaultInitialCapital`, `DefaultCommissionRate` 等) + Polling + StateStore 常量
- `options.go` 的 `EngineOption` 函数式选项

### 关键解耦点 (已 grep 确认)

**DTO 消费者** (grep `backtest\.(BacktestRequest|BacktestResponse|TradingConfig|PriceLimitConfig)` → 6 files):
- `cmd/analysis/main.go` — `backtest.BacktestRequest` / `BacktestResponse` (零改动 via alias)
- `cmd/analysis/handlers_backtest.go` — 同上
- `pkg/backtest/reporting/export.go` + `compare.go` — import 父包 `pkg/backtest` (零改动)
- `web/src/api/backtest.ts` — TypeScript, 与 Go alias 无关

**`defaultTradingConfig()`** (3 调用点):
- `engine.go:247, 341` — 父包内调用
- `tracker.go:50` — 随 tracker 移动到 tracker/, 改用 `contracts.DefaultTradingConfig()`

**`DefaultShortSellingRate` + `TradingDaysPerYear`** (仅 tracker 使用):
- `tracker.go:60, 744` — 随 tracker 移动, 改用 `contracts.DefaultShortSellingRate` / `contracts.TradingDaysPerYear`

**Logger 耦合** (grep `wf\.engine\.logger|engine\.logger\.With` → 6 sites):
- `walkforward.go:100, 128, 179, 212, 218` — 5 处 `wf.engine.logger.*`
- `job.go:128` — 1 处 `engine.logger.With()`

**`*Engine` 强耦合** (job/batch/walkforward 持有具体 `*Engine`):
- `walkforward.go:17` — `engine *Engine` → 改为 `runner contracts.EngineRunner`
- `job.go:124` — `engine *Engine` → 改为 `runner contracts.EngineRunner` + 添加 `logger zerolog.Logger`
- `batch.go` — 持有 `engine *Engine` + `wfEng *WalkForwardEngine` → 改为 `*walkforward.WalkForwardEngine`

### 测试文件迁移策略

| 源文件 | 测试文件 | 目标子包 |
|--------|---------|---------|
| cache.go, factor_cache.go | cache_test.go, factor_cache_test.go | `cache/` |
| execution.go, execution_bridge.go, live_bridge.go | execution_test.go, execution_bridge_test.go, live_bridge_test.go | `execution/` |
| tracker.go, order_log.go | tracker_*.go (6 files), order_log_test.go | `tracker/` |
| state.go, state_store.go, persistence.go | state_test.go, persistence_test.go, state_store_integration_test.go, invariants_test.go | `state/` |
| walkforward.go | walkforward_test.go, walkforward_metrics_test.go, walkforward_overfit_test.go | `walkforward/` |
| batch.go, batch_scorer.go | batch_test.go, batch_score_test.go, batch_csv_test.go | `batch/` |
| job.go | job_test.go, job_shutdown_test.go | `job/` |

**留在父包的测试**: engine_*.go (10 个), constants_drift_test.go, options_test.go, signal_conversion_test.go, fixture_gen_test.go, fixtures_p1_7_test.go, execution_integration_test.go (跨包集成, 父包保留)

**注意**: `*_test.go` 默认被 `.gitignore` (line 28) 忽略；移动测试文件需用 `git mv -f`。

---

## Proposed Changes (8 个原子 Commit)

### Commit 1: 创建 `contracts/` 叶子包 + 父包 alias

**目的**: 建立无依赖的叶子包，承载共享 DTO + 接口，为后续子包解耦铺路。

**新建** `pkg/backtest/contracts/contracts.go`:
```go
// Package contracts is the LEAF package for pkg/backtest subpackages.
// Imports ONLY pkg/domain + stdlib. Subpackages import contracts (not parent)
// to avoid import cycles.
package contracts

import (
    "context"
    "github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// --- DTOs (moved from engine.go) ---

// TradingConfig holds A-share trading rules.
type TradingConfig struct {
    StampTaxRate    float64          `mapstructure:"stamp_tax_rate"`
    MinCommission   float64          `mapstructure:"min_commission"`
    TransferFeeRate float64          `mapstructure:"transfer_fee_rate"`
    PriceLimit      PriceLimitConfig `mapstructure:"price_limit"`
    NewStockDays    int              `mapstructure:"new_stock_days"`
}

// PriceLimitConfig holds price limit rules.
type PriceLimitConfig struct {
    Normal float64 `mapstructure:"normal"`
    ST     float64 `mapstructure:"st"`
    New    float64 `mapstructure:"new"`
}

// BacktestRequest represents the API request to start a backtest.
type BacktestRequest struct {
    Strategy       string   `json:"strategy" binding:"required"`
    StockPool      []string `json:"stock_pool"`
    IndexCode      string   `json:"index_code"`
    StartDate      string   `json:"start_date" binding:"required"`
    EndDate        string   `json:"end_date" binding:"required"`
    InitialCapital float64  `json:"initial_capital"`
    RiskFreeRate   float64  `json:"risk_free_rate"`
}

// BacktestResponse represents the API response for a backtest run.
type BacktestResponse struct {
    ID              string                  `json:"id"`
    Status          string                  `json:"status"`
    Strategy        string                  `json:"strategy,omitempty"`
    // ... (全部字段从 engine.go:187-213 复制)
    PortfolioValues []domain.PortfolioValue `json:"portfolio_values,omitempty"`
    Trades          []domain.Trade          `json:"trades,omitempty"`
}

// --- Constants (moved from constants.go) ---

const (
    DefaultStampTaxRate      = 0.001   // alias of fees.DefaultStampTaxRate
    DefaultMinCommission    = 5.0
    DefaultTransferFeeRate   = 0.00001
    DefaultPriceLimitNormal  = 0.10
    DefaultPriceLimitST      = 0.05
    DefaultPriceLimitNew     = 0.20
    DefaultNewStockDays      = 60
    DefaultShortSellingRate  = 0.106
    TradingDaysPerYear       = 252
)

// DefaultTradingConfig returns the default A-share trading rules.
// (Renamed from unexported defaultTradingConfig to allow cross-package use.)
func DefaultTradingConfig() TradingConfig {
    return TradingConfig{
        StampTaxRate:    DefaultStampTaxRate,
        MinCommission:   DefaultMinCommission,
        TransferFeeRate: DefaultTransferFeeRate,
        PriceLimit: PriceLimitConfig{
            Normal: DefaultPriceLimitNormal,
            ST:     DefaultPriceLimitST,
            New:    DefaultPriceLimitNew,
        },
        NewStockDays: DefaultNewStockDays,
    }
}

// --- Narrow interface (decouples job/walkforward/batch from *Engine) ---

// EngineRunner is the narrow contract for running a backtest.
// *backtest.Engine implements this; subpackages accept this interface
// (not the concrete *Engine) to break the import cycle.
type EngineRunner interface {
    RunBacktest(ctx context.Context, req BacktestRequest) (*BacktestResponse, error)
}
```

**注意**: 父包 `constants.go` 中 `DefaultStampTaxRate = fees.DefaultStampTaxRate` 等 5 个常量已与 `pkg/fees` 同步。移到 contracts/ 时保持 `= fees.DefaultXxx` 形式（仍 const-alias 到 fees，单一事实来源不变）。需在 contracts.go 中 `import "github.com/ruoxizhnya/quant-trading/pkg/fees"`。

**新建** `pkg/backtest/aliases.go` (父包内):
```go
package backtest

import "github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"

// Type aliases (zero-cost, backward compatible)
type (
    TradingConfig     = contracts.TradingConfig
    PriceLimitConfig  = contracts.PriceLimitConfig
    BacktestRequest   = contracts.BacktestRequest
    BacktestResponse  = contracts.BacktestResponse
)

// Const aliases
const (
    DefaultStampTaxRate     = contracts.DefaultStampTaxRate
    DefaultMinCommission    = contracts.DefaultMinCommission
    DefaultTransferFeeRate  = contracts.DefaultTransferFeeRate
    DefaultPriceLimitNormal = contracts.DefaultPriceLimitNormal
    DefaultPriceLimitST     = contracts.DefaultPriceLimitST
    DefaultPriceLimitNew    = contracts.DefaultPriceLimitNew
    DefaultNewStockDays     = contracts.DefaultNewStockDays
    DefaultShortSellingRate = contracts.DefaultShortSellingRate
    TradingDaysPerYear      = contracts.TradingDaysPerYear
)

// EngineRunner re-exported for any parent-internal consumers.
type EngineRunner = contracts.EngineRunner

// defaultTradingConfig wraps the exported contracts.DefaultTradingConfig
// to keep engine.go's existing call sites (L247, L341) unchanged.
func defaultTradingConfig() TradingConfig {
    return contracts.DefaultTradingConfig()
}
```

**编辑** `pkg/backtest/engine.go`:
- 删除 L40-46 (`TradingConfig` struct) → 已由 alias 提供
- 删除 L49-53 (`PriceLimitConfig` struct) → 已由 alias 提供
- 删除 L56-68 (`defaultTradingConfig` func) → 已由 aliases.go wrapper 提供
- 删除 L175-184 (`BacktestRequest` struct) → 已由 alias 提供
- 删除 L186-213 (`BacktestResponse` struct) → 已由 alias 提供
- `Config` struct L36 字段 `Trading TradingConfig` → 通过 alias 仍然有效

**编辑** `pkg/backtest/constants.go`:
- 删除 L18-39 (交易规则常量块) → 已由 alias 提供
- 删除 L59 (`DefaultShortSellingRate`) + L66 (`TradingDaysPerYear`) → 已由 alias 提供
- 保留 L42-57 运营常量 (`DefaultInitialCapital`, `DefaultCommissionRate`, `DefaultSlippageRate`, `DefaultRiskFreeRate`)
- 保留 L70-89 (Polling + StateStore 常量)

**新建测试** `pkg/backtest/contracts/contracts_test.go`:
- `TestDefaultTradingConfig_Values`: 断言所有字段匹配预期默认值
- `TestDefaultTradingConfig_StampTaxFromFees`: 断言 `DefaultStampTaxRate == fees.DefaultStampTaxRate` (守卫测试, 防 drift)
- `TestEngineRunner_InterfaceShape`: 编译期断言 `*Engine` 满足 `contracts.EngineRunner` (var _ EngineRunner = (*Engine)(nil)) — 注意此断言放父包测试, 因 *Engine 在父包

**验证**:
```bash
go build ./pkg/backtest/... ./pkg/backtest/contracts/...
go vet ./pkg/backtest/... ./pkg/backtest/contracts/...
go test ./pkg/backtest/... -count=1
```

---

### Commit 2: 提取 `cache/` 叶子子包

**目的**: CacheManager + FactorCacheAccessor 是纯叶子 (仅依赖 domain + marketdata)，最易提取。

**操作**:
```bash
git mv pkg/backtest/cache.go        pkg/backtest/cache/cache.go
git mv pkg/backtest/factor_cache.go pkg/backtest/cache/factor_cache.go
git mv -f pkg/backtest/cache_test.go        pkg/backtest/cache/cache_test.go
git mv -f pkg/backtest/factor_cache_test.go pkg/backtest/cache/factor_cache_test.go
```

**编辑** `pkg/backtest/cache/cache.go`: `package backtest` → `package cache`
**编辑** `pkg/backtest/cache/factor_cache.go`: 同上

**编辑** `pkg/backtest/engine.go`:
- `cache *CacheManager` → `cache *cache.CacheManager` (L115)
- `NewCacheManager(componentLogger)` → `cache.NewCacheManager(componentLogger)` (L297)
- `factor *FactorCacheAccessor` → `factor *cache.FactorCacheAccessor` (L126)
- `NewFactorCacheAccessor(componentLogger)` → `cache.NewFactorCacheAccessor(...)` (L298)
- import `"github.com/ruoxizhnya/quant-trading/pkg/backtest/cache"`

**新建** `pkg/backtest/aliases.go` 中追加 (Commit 1 已创建, 此处 Edit 追加):
```go
type (
    CacheManager         = cache.CacheManager
    FactorCacheAccessor  = cache.FactorCacheAccessor
)
```
注意: aliases.go 顶部 import 增加 `"github.com/ruoxizhnya/quant-trading/pkg/backtest/cache"`

**验证**:
```bash
go build ./pkg/backtest/... && go vet ./pkg/backtest/... && go test ./pkg/backtest/... -count=1
```

---

### Commit 3: 提取 `execution/` 子包 (含 ExecutionBridge + LiveBridge)

**操作**:
```bash
git mv pkg/backtest/execution.go         pkg/backtest/execution/execution.go
git mv pkg/backtest/execution_bridge.go   pkg/backtest/execution/execution_bridge.go
git mv pkg/backtest/live_bridge.go        pkg/backtest/execution/live_bridge.go
git mv -f pkg/backtest/execution_test.go        pkg/backtest/execution/execution_test.go
git mv -f pkg/backtest/execution_bridge_test.go pkg/backtest/execution/execution_bridge_test.go
git mv -f pkg/backtest/live_bridge_test.go       pkg/backtest/execution/live_bridge_test.go
```

**编辑** 3 个源文件: `package backtest` → `package execution`

**编辑** `pkg/backtest/engine.go`:
- `executionBridge *ExecutionBridge` → `executionBridge *execution.ExecutionBridge` (L158)
- `liveBridge *LiveBridge` → `liveBridge *execution.LiveBridge` (L152)
- `NewExecutionBridge` / `NewBacktestExecutionService` / `NewLiveBridge` 调用加 `execution.` 前缀
- import `execution` 子包

**新建** `pkg/backtest/aliases.go` 追加:
```go
type (
    ExecutionService            = execution.ExecutionService
    BacktestExecutionService    = execution.BacktestExecutionService
    ExecutionBridge             = execution.ExecutionBridge
    LiveBridge                  = execution.LiveBridge
)
// 构造函数包装 (因 Go 无函数 alias)
func NewBacktestExecutionService(cfg domain.ExecutionConfig) *BacktestExecutionService {
    return execution.NewBacktestExecutionService(cfg)
}
func NewExecutionBridge(logger zerolog.Logger) *ExecutionBridge {
    return execution.NewExecutionBridge(logger)
}
func NewLiveBridge(logger zerolog.Logger) *LiveBridge {
    return execution.NewLiveBridge(logger)
}
```

**注意**: 父包中所有 `NewXxx(...)` 调用点保持不变 (经 wrapper 解析)。

**验证**:
```bash
go build ./pkg/backtest/... && go vet ./pkg/backtest/... && go test ./pkg/backtest/... -count=1
```

---

### Commit 4: 提取 `tracker/` 子包 (含 OrderLog)

**操作**:
```bash
git mv pkg/backtest/tracker.go     pkg/backtest/tracker/tracker.go
git mv pkg/backtest/order_log.go   pkg/backtest/tracker/order_log.go
git mv -f pkg/backtest/tracker_settlement_test.go pkg/backtest/tracker/tracker_settlement_test.go
git mv -f pkg/backtest/tracker_ghost_test.go      pkg/backtest/tracker/tracker_ghost_test.go
git mv -f pkg/backtest/tracker_t1_test.go         pkg/backtest/tracker/tracker_t1_test.go
git mv -f pkg/backtest/tracker_limit_test.go      pkg/backtest/tracker/tracker_limit_test.go
git mv -f pkg/backtest/tracker_split_test.go      pkg/backtest/tracker/tracker_split_test.go
git mv -f pkg/backtest/tracker_test.go            pkg/backtest/tracker/tracker_test.go
git mv -f pkg/backtest/order_log_test.go          pkg/backtest/tracker/order_log_test.go
git mv -f pkg/backtest/property_test.go           pkg/backtest/tracker/property_test.go
```

**注意**: `property_test.go` (testing/quick 属性测试, 测试 Tracker T+1 不变量) 必须随 tracker 移动。

**编辑** `pkg/backtest/tracker/tracker.go`:
- `package backtest` → `package tracker`
- import 增加 `"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"`
- L38: `trading TradingConfig` → `trading contracts.TradingConfig`
- L47: `func NewTracker(..., trading TradingConfig, ...)` → `func NewTracker(..., trading contracts.TradingConfig, ...)`
- L50: `trading = defaultTradingConfig()` → `trading = contracts.DefaultTradingConfig()`
- L60: `shortSellingRate: DefaultShortSellingRate` → `shortSellingRate: contracts.DefaultShortSellingRate`
- L744: `float64(TradingDaysPerYear)` → `float64(contracts.TradingDaysPerYear)`

**编辑** `pkg/backtest/tracker/order_log.go`: `package backtest` → `package tracker`

**编辑** `pkg/backtest/state.go` (state 仍在父包, 但引用 Tracker):
- L56: `Tracker *Tracker` → `Tracker *tracker.Tracker`
- import `"github.com/ruoxizhnya/quant-trading/pkg/backtest/tracker"`

**编辑** `pkg/backtest/engine.go` (多处 NewTracker 调用):
- 加 `tracker.` 前缀 (grep `NewTracker(` 找全部调用点)

**新建** `pkg/backtest/aliases.go` 追加:
```go
type (
    Tracker  = tracker.Tracker
    OrderLog = tracker.OrderLog
)
func NewTracker(initialCapital, commissionRate, slippageRate float64, trading TradingConfig, logger zerolog.Logger) *Tracker {
    return tracker.NewTracker(initialCapital, commissionRate, slippageRate, trading, logger)
}
```

**验证**:
```bash
go build ./pkg/backtest/... && go vet ./pkg/backtest/... && go test ./pkg/backtest/... -count=1 -race
```
**重点**: `property_test.go` 的 `TestProperty_T1Enforced` 必须通过 (S7-P0-17 canary)。

---

### Commit 5: 提取 `state/` 子包 (state + state_store + persistence)

**操作**:
```bash
git mv pkg/backtest/state.go           pkg/backtest/state/state.go
git mv pkg/backtest/state_store.go     pkg/backtest/state/state_store.go
git mv pkg/backtest/persistence.go     pkg/backtest/state/persistence.go
git mv -f pkg/backtest/state_test.go                pkg/backtest/state/state_test.go
git mv -f pkg/backtest/persistence_test.go          pkg/backtest/state/persistence_test.go
git mv -f pkg/backtest/state_store_integration_test.go pkg/backtest/state/state_store_integration_test.go
git mv -f pkg/backtest/invariants_test.go           pkg/backtest/state/invariants_test.go
```

**编辑** 3 个源文件: `package backtest` → `package state`

**编辑** `pkg/backtest/state/state.go`:
- import `"github.com/ruoxizhnya/quant-trading/pkg/backtest/tracker"`
- L56: `Tracker *Tracker` → `Tracker *tracker.Tracker`

**编辑** `pkg/backtest/engine.go`:
- `stateStore StateStore` → `stateStore state.StateStore` (L99)
- `NewLRUStateStore(...)` → `state.NewLRUStateStore(...)` (L288)
- `BacktestState` 引用 → `state.BacktestState`
- import `state` 子包

**新建** `pkg/backtest/aliases.go` 追加:
```go
type (
    BacktestState    = state.BacktestState
    StateStore       = state.StateStore
    LRUStateStore    = state.LRUStateStore
    NoopStateStore   = state.NoopStateStore
    DiskStateStore   = state.DiskStateStore
)
func NewLRUStateStore(capacity int) StateStore { return state.NewLRUStateStore(capacity) }
func NewNoopStateStore() StateStore           { return state.NewNoopStateStore() }
// 等其他构造函数
```

**验证**:
```bash
go build ./pkg/backtest/... && go vet ./pkg/backtest/... && go test ./pkg/backtest/... -count=1 -race
```

---

### Commit 6: 提取 `walkforward/` 子包 (logger 解耦)

**操作**:
```bash
git mv pkg/backtest/walkforward.go pkg/backtest/walkforward/walkforward.go
git mv -f pkg/backtest/walkforward_test.go          pkg/backtest/walkforward/walkforward_test.go
git mv -f pkg/backtest/walkforward_metrics_test.go  pkg/backtest/walkforward/walkforward_metrics_test.go
git mv -f pkg/backtest/walkforward_overfit_test.go   pkg/backtest/walkforward/walkforward_overfit_test.go
```

**编辑** `pkg/backtest/walkforward/walkforward.go`:
- `package backtest` → `package walkforward`
- import `"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"`
- import `"github.com/rs/zerolog"`
- L16-19: `WalkForwardEngine` struct 添加 `logger zerolog.Logger` 字段, `engine *Engine` → `runner contracts.EngineRunner`
- L22-27: `NewWalkForwardEngine(engine *Engine, store)` → `NewWalkForwardEngine(runner contracts.EngineRunner, store *storage.PostgresStore, logger zerolog.Logger)`
- L100, 128, 179, 212, 218: `wf.engine.logger.*` → `wf.logger.*`
- 所有 `wf.engine.RunBacktest(ctx, req)` → `wf.runner.RunBacktest(ctx, req)` (注意 req 类型已 alias, 兼容)

**编辑** `pkg/backtest/engine.go`: 找 `NewWalkForwardEngine` 调用点, 加 `walkforward.` 前缀 + 传 `engine.logger`

**编辑** `pkg/backtest/batch.go` (Commit 7 前的临时修改, 或合到 Commit 7):
- 暂时保留 `wfEng *WalkForwardEngine` 引用 (经 alias 解析) → Commit 7 一起改

**新建** `pkg/backtest/aliases.go` 追加:
```go
type (
    WalkForwardEngine  = walkforward.WalkForwardEngine
    WalkForwardRequest = walkforward.WalkForwardRequest
)
func NewWalkForwardEngine(runner contracts.EngineRunner, store *storage.PostgresStore, logger zerolog.Logger) *WalkForwardEngine {
    return walkforward.NewWalkForwardEngine(runner, store, logger)
}
```

**验证**:
```bash
go build ./pkg/backtest/... && go vet ./pkg/backtest/... && go test ./pkg/backtest/... -count=1 -race
```

---

### Commit 7: 提取 `batch/` 子包 (依赖 walkforward 兄弟)

**操作**:
```bash
git mv pkg/backtest/batch.go        pkg/backtest/batch/batch.go
git mv pkg/backtest/batch_scorer.go pkg/backtest/batch/batch_scorer.go
git mv -f pkg/backtest/batch_test.go      pkg/backtest/batch/batch_test.go
git mv -f pkg/backtest/batch_score_test.go pkg/backtest/batch/batch_score_test.go
git mv -f pkg/backtest/batch_csv_test.go   pkg/backtest/batch/batch_csv_test.go
```

**编辑** `pkg/backtest/batch/batch.go`:
- `package backtest` → `package batch`
- import `"github.com/ruoxizhnya/quant-trading/pkg/backtest/walkforward"`
- import `"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"`
- `wfEng *WalkForwardEngine` → `wfEng *walkforward.WalkForwardEngine`
- `engine *Engine` → `runner contracts.EngineRunner` + 添加 `logger zerolog.Logger`
- 构造函数签名调整: `NewBatchEngine(runner contracts.EngineRunner, wfEng *walkforward.WalkForwardEngine, ...)`
- `b.engine.RunBacktest(...)` → `b.runner.RunBacktest(...)`
- `b.engine.logger.*` → `b.logger.*`

**编辑** `pkg/backtest/engine.go` / `cmd/analysis/*.go`: 调用点加 `batch.` 前缀 + 传 runner + logger

**新建** `pkg/backtest/aliases.go` 追加:
```go
type (
    BatchEngine  = batch.BatchEngine
    Scorer       = batch.Scorer
)
func NewBatchEngine(runner contracts.EngineRunner, wfEng *WalkForwardEngine, ...) *BatchEngine { ... }
```

**验证**:
```bash
go build ./pkg/backtest/... && go vet ./pkg/backtest/... && go test ./pkg/backtest/... -count=1 -race
```

---

### Commit 8: 提取 `job/` 子包 (logger 解耦 + 全量验证)

**操作**:
```bash
git mv pkg/backtest/job.go pkg/backtest/job/job.go
git mv -f pkg/backtest/job_test.go          pkg/backtest/job/job_test.go
git mv -f pkg/backtest/job_shutdown_test.go pkg/backtest/job/job_shutdown_test.go
```

**编辑** `pkg/backtest/job/job.go`:
- `package backtest` → `package job`
- import `contracts`
- `engine *Engine` → `runner contracts.EngineRunner` + 添加 `logger zerolog.Logger`
- L128: `engine.logger.With()` → `logger.With()`
- L325: `s.engine.RunBacktest(ctx, req)` → `s.runner.RunBacktest(ctx, req)`
- `NewJobService(store JobStore, engine *Engine)` → `NewJobService(store JobStore, runner contracts.EngineRunner, logger zerolog.Logger)`

**编辑** `pkg/backtest/engine.go` / `cmd/analysis/*.go`: 调用点加 `job.` 前缀

**新建** `pkg/backtest/aliases.go` 追加:
```go
type (
    JobService      = job.JobService
    JobStore        = job.JobStore
    Job             = job.Job
    CreateJobRequest = job.CreateJobRequest
)
func NewJobService(store JobStore, runner contracts.EngineRunner, logger zerolog.Logger) *JobService {
    return job.NewJobService(store, runner, logger)
}
```

**最终全量验证**:
```bash
go build ./...
go vet ./...
gofmt -l . (无输出)
go test ./... -count=1 -race (excl. /e2e)
cd web && npm run typecheck (若前端无改动可跳过, 但保险)
```

---

## Assumptions & Decisions

### 已决策 (用户已批准)
1. **Scope**: 增量式叶子+接口拆分 — 7 个叶子子包 + contracts/，不拆 engine.go 内部
2. **Interface approach**: 窄 Runner 接口 (`contracts.EngineRunner` 单方法)，非 Strategy ISP 风格拆分
3. **Defer**: engine.go / engine_daily.go 内部重构 → S7-P2-1b (未来任务)
4. **Branch**: `feat/s7-p2-1-backtest-leaf-extraction` (已创建)

### 关键设计决策
1. **type/const alias 优于 re-declaration**: 零成本 (编译期 inlining)，类型安全，199 个消费者零改动
2. **函数无 alias** → 用 wrapper 函数 (`func NewXxx(...) { return sub.NewXxx(...) }`)
3. **`defaultTradingConfig()` 保留 unexported wrapper**: 父包内 2 处调用 (engine.go L247, L341) 不改动
4. **contracts 是 LEAF**: 仅 import `pkg/domain` + `pkg/fees` + stdlib，不 import 任何行为包
5. **测试文件用 `git mv -f`**: 因 `.gitignore` L28 忽略 `*_test.go`
6. **logger 解耦**: 子包持有自己的 `zerolog.Logger` 字段，构造时由父包传入 `engine.logger`
7. **constants_drift_test.go 留父包**: 它守卫 `DefaultStampTaxRate == fees.DefaultStampTaxRate`，移到 contracts 后由 contracts_test.go 接管

### 假设
1. `*Engine` 已实现 `RunBacktest(ctx, BacktestRequest) (*BacktestResponse, error)` (grep 确认存在) — Commit 1 验证编译期断言
2. `cmd/analysis/main.go` 等消费者不直接 import 子包 (经 `backtest.X` alias 解析) — Commit 1 后 grep 验证无新 import
3. 父包内 `Config.Trading TradingConfig` 字段经 alias 仍 viper.Unmarshal 正常 (type alias 完全等价)

### 风险与缓解
| 风险 | 缓解 |
|------|------|
| Import 环 | 依赖图已验证无环；每 commit `go build` 验证 |
| alias 漏写导致编译错误 | 每 commit 后 `go build ./...` 全量编译 |
| 测试迁移后 package 名不一致 | `git mv` 后立即改 `package` 声明 + `go test` 验证 |
| Edit 工具缓存 bug | 关键编辑用 Python 脚本 `open/read/replace/write`，验证用 `sed -n`/`grep` 直接读磁盘 |
| `property_test.go` canary 失败 | Commit 4 后强制 `go test -race -count=2` 验证 T+1 不变量 |

---

## Verification Steps (每 Commit 通用)

```bash
# 1. 编译
go build ./pkg/backtest/... && go build ./...

# 2. 静态检查
go vet ./pkg/backtest/... && go vet ./...

# 3. 格式
gofmt -l pkg/backtest/

# 4. 测试 (单包 + 全量)
go test ./pkg/backtest/... -count=1 -race
go test ./... -count=1 -race (excl. /e2e)  # 最终 Commit 8 全量

# 5. 消费者零改动验证
grep -rn "backtest\.\(BacktestRequest\|BacktestResponse\|TradingConfig\|Tracker\|CacheManager\)" cmd/  # 应无变化
```

---

## Commit Message 模板

```
refactor(backtest/<sub>): extract <Sub> into leaf subpackage

<body — what/why>

Refs: S7-P2-1 (Commit N/8)
Reviewed: self-review + go vet + gofmt + go test -race
```

---

## Out of Scope (显式排除)

- ❌ engine.go / engine_daily.go 内部方法拆分 → S7-P2-1b
- ❌ options.go 提取 (EngineOption 紧耦合 Engine 构造, 留父包)
- ❌ constants.go 运营常量提取 (DefaultInitialCapital 等仅 Engine 使用, 留父包)
- ❌ 子包内进一步重构 (如 tracker.go 922 行可再拆 trade_executor / position_manager)
- ❌ 文档同步 (TASKS.md 标记完成 + AGENTS.md 更新) — Commit 8 后单独 docs commit

---

## Success Criteria

- [ ] 7 个子包 + contracts/ 全部创建, 各自独立编译 + 测试通过
- [ ] `go build ./...` 全量通过
- [ ] `go test ./... -count=1 -race` (excl. /e2e) 全量通过
- [ ] `property_test.go` T+1 canary 通过
- [ ] 消费者 (cmd/analysis/*, reporting/*) 零改动
- [ ] TASKS.md S7-P2-1 标记 ✅
- [ ] 8 个原子 commit, 每个可独立构建测试
