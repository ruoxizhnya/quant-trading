# ADR-020: Engine God Object 拆分 + 函数式依赖注入

**Date:** 2026-06-11
**Status:** Proposed

## Context

[ODR-013 2026-06-11 综合审查](../odr/odr-013-comprehensive-audit-2026-06-11.md) 识别 `pkg/backtest/engine.go` (1408 行) 存在严重 SRP (Single Responsibility) 违反：

### 当前 Engine 承担 5+ 个独立职责

1. **回测编排器**：`RunBacktest`, `runBacktestInternal`
2. **缓存管理**：`warmCache`, `warmFactorCache`, `LoadOHLCVInMemory`, `LoadFactorCache`
3. **信号获取**：`getSignals` (HTTP 调 strategy-service 125 行)
4. **风控/仓位**：`calculatePosition`, `calculatePositionsBatch`, `checkStopLosses`, `checkStopLossesWithATR`
5. **Live 桥接**：`SetLiveTrader`, `ExecuteSignalViaLiveTrader`, `ExecuteSignalsViaLiveTrader`, `HealthCheckLiveTrader`
6. **Execution 桥接**：`SetExecutionService`, `GetExecutionService`
7. **DataAdapter 桥接**：`SetDataAdapter`, `SwitchDataSource`, `effectiveProvider`
8. **因子缓存直接访问**：`GetFactorZScore`

### 附带的并发 / 资源问题

- **AR-012**：in-memory backtest state 进程重启即丢失
- **AR-014**：`backtests map` 释放锁后 state 对象仍被多 goroutine 访问
- **CQ-008**：`backtests map[string]*BacktestState` 完成后永不 delete，**资源泄漏**
- **CQ-019 (TG-002)**：`pkg/strategy/copilot_test.go` 模拟 panic 是 Engine 测试崩溃的根因

### 10+ Setter 违反 Go 惯用法

精确枚举（截至 2026-06-11，对齐审计后）：
- **Engine 5 个**（`pkg/backtest/engine.go`）：`SetDataAdapter`, `SetStore`, `SetRiskManager`, `SetLiveTrader`, `SetExecutionService`
- **Strategy 1 个**（`pkg/strategy/strategy.go`）：`SetFactorCache` (FactorAware interface)
- **辅助 setter ~4 个**（需 P1-19 实施时再次精确 grep 确认）：`SetLogger`, `SetNotifier`, `SetMetrics`, `SetClock` 等

合计 ~10 个 setter。P1-19 backward-compat shim 阶段保留全部 6 个月，函数式 `EngineOption` 优先用于新代码。

```go
e.SetRiskManager(rm)
e.SetDataAdapter(adapter)
e.SetStore(store)
e.SetLiveTrader(trader)
e.SetExecutionService(svc)
// ... 10+ 个
```

**Go 惯用法**：`EngineOption` 函数式注入（参考 `hashicorp/go-multierror`、`kubernetes/client-go`）。

## Decision

### §1. 职责拆分：5 个子组件

| 子组件 | 路径 | 职责 |
|---|---|---|
| **BacktestOrchestrator** | `pkg/backtest/orchestrator.go` | RunBacktest、RunBacktestAsync、日循环编排 |
| **CacheManager** | `pkg/backtest/cache.go` (新建) | warmCache、LoadOHLCVInMemory、inMemoryOHLCVAtomic |
| **FactorCacheAccessor** | `pkg/backtest/factor_cache.go` (新建) | GetFactorZScore、LoadFactorCache、warmFactorCache |
| **LiveBridge** | `pkg/backtest/live_bridge.go` (新建) | SetLiveTrader、ExecuteSignalViaLiveTrader、HealthCheckLiveTrader |
| **ExecutionBridge** | `pkg/backtest/execution_bridge.go` (新建) | SetExecutionService、GetExecutionService |

### §2. Engine 改为 Orchestrator 持有

```go
// New shape
type Engine struct {
    *BacktestOrchestrator
    cache      *CacheManager
    factor     *FactorCacheAccessor
    liveBridge *LiveBridge
    execBridge *ExecutionBridge
}

// Backward-compat shim
func (e *Engine) SetRiskManager(rm *risk.RiskManager) {
    e.BacktestOrchestrator.SetRiskManager(rm)
}
```

### §3. EngineOption 函数式注入

```go
type EngineOption func(*Engine)

func WithRiskManager(rm *risk.RiskManager) EngineOption {
    return func(e *Engine) { e.BacktestOrchestrator.SetRiskManager(rm) }
}

func WithDataAdapter(adapter *data.Adapter) EngineOption {
    return func(e *Engine) { e.adapter = adapter }
}

func WithLiveTrader(trader live.LiveTrader) EngineOption {
    return func(e *Engine) { e.liveBridge = NewLiveBridge(trader) }
}

// New constructor
func NewEngine(config *Config, provider marketdata.Provider, opts ...EngineOption) (*Engine, error) {
    e := &Engine{...}
    for _, opt := range opts {
        opt(e)
    }
    return e, nil
}
```

**Benefits**:
- 10+ Setter → 1 variadic options pattern
- 必需参数（config, provider）vs 可选参数（riskManager, liveTrader）分离
- 测试构造更简洁：`NewEngine(cfg, prov, WithRiskManager(mockRM))`
- 未来扩展无需修改 Engine struct

### §4. BacktestState 资源管理

**`backtests map` 添加 LRU 淘汰 + 持久化**：

```go
// New
type StateStore interface {
    Get(id string) (*BacktestState, bool)
    Set(id string, state *BacktestState)
    Delete(id string)
    Range(fn func(id string, state *BacktestState) bool)
}

// Two implementations
type InMemoryStateStore struct {
    mu     sync.RWMutex
    states map[string]*BacktestState
    lru    *list.List  // eviction
    cap    int         // default 1000
}

type PersistentStateStore struct {
    db   *sql.DB
    cache *lru.Cache
}
```

**Eviction 策略**：
- 默认 1000 条 in-memory 缓存
- 超过容量 LRU 淘汰 → 落 PG `backtest_jobs` 表
- Get 操作：先查 in-memory miss → 查 DB → 回填 in-memory

### §5. BacktestState 内部锁

```go
// New (per AR-014)
type BacktestState struct {
    mu      sync.RWMutex
    ID      string
    Status  string
    Result  *BacktestResult
    Tracker *Tracker
    // ...
}

func (s *BacktestState) GetResult() *BacktestResult {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.Result
}

func (s *BacktestState) SetStatus(status string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.Status = status
}
```

**Optional: 冻结模式**：
- 回测完成后 `Freeze()` 标记 immutable
- Getter 跳过锁，零开销

## Options Considered

**Option A — 维持 God Engine**：
- ❌ 拒绝：1408 行违反 SRP；新功能添加成本高；测试边界模糊

**Option B — 拆为微服务（orchestrator + cache + factor + live + execution 各自独立服务）**：
- ❌ 拒绝：违背 ADR-019 service 合并方向；微服务化反而增加复杂度

**Option C — 完全重写（如自研 actor model）**：
- ❌ 拒绝：项目级风险高；现有代码优化已见成效（5s backtest 目标达成）

**Option D — 选项 B+C 不变，仅做内部包拆分（采纳）**：
- ✅ 包内拆分保留 in-process 性能
- 5 个子文件替代 1 个 1408 行文件
- 单一 API 入口（Engine 仍存在，持有子组件）

## Consequences

### Positive
- Engine 行数从 1408 → ~300 (orchestrator only)
- 每个子组件独立单测，无需启动 Engine
- 函数式 Option 注入测试代码减少 50%
- 资源泄漏 (`backtests` 永不 delete) 修复
- 并发安全 (BacktestState 内部锁)

### Negative
- 5 个新文件 + 5 个子测试
- 旧 Setter 调用代码需重构 (with grep + sed)
- `cmd/analysis/main.go` 注入逻辑需更新

### Backward Compat
- 保留 `Set*` 方法作为 shim 6 个月
- `pkg/backtest.NewEngine()` 签名变更为 variadic options，调用方需更新
- 内部代码：`pkg/strategy`, `pkg/live` 等调用方需适配

## Implementation Roadmap

| Sprint | Tasks (见 [TASKS.md §Sprint 6](../../TASKS.md)) | Effort |
|---|---|---|
| **Sprint 6 P1-16** (3d) | 拆 CacheManager + FactorCacheAccessor 子包 | Major |
| **Sprint 6 P1-17** (3d) | 拆 LiveBridge + ExecutionBridge 子包 | Major |
| **Sprint 6 P1-18** (2d) | 引入 StateStore interface + LRU/持久化实现 | Major |
| **Sprint 6 P1-19** (3d) | EngineOption 函数式注入 + Backward-compat shim | Major |
| **Sprint 6 P1-20** (2d) | BacktestState 内部锁 + Freeze 模式 | Major |
| **Sprint 7 P2-X** (1w) | 调用方适配（`pkg/strategy`, `pkg/live`, `cmd/analysis`） | Operational |

## Related

- [ADR-014 Strategy Framework Refactor](adr-014-strategy-framework-refactor.md) — Strategy 接口拆分的 precedent
- [ADR-019 Service Merge](adr-019-service-merge-ai-copilot.md) — service 合并后 in-process 调用链简化
- [ADR-018 Test + Async Safety](adr-018-test-and-async-safety.md) — 测试边界清晰化
- [ODR-013 CQ-001/CQ-008/CQ-019/AR-012/AR-014 findings](../odr/odr-013-comprehensive-audit-2026-06-11.md)
