# ADR-014: Strategy Framework Refactor & Unified Interface

> **Status**: Proposed  
> **Date**: 2026-05-04  
> **Category**: Architecture  
> **Related ADRs**: ADR-001 (Plugin Loading), ADR-005 (Strategy Config), ADR-012 (Strategy Service Standby)  
> **Supersedes**: None (enhances existing decisions)  
> **Author**: AI Assistant (Code Review 2026-05-04)

---

## Context

2026-05-04 代码审查发现策略框架存在以下结构性问题：

1. **双接口并存**：`pkg/strategy/strategy.go` 定义新版 `Strategy` 接口（`GenerateSignals`），而 `pkg/domain/` 保留旧版 `domain.Strategy` 接口（`Signals` 方法）。`examples/` 目录使用旧接口，`plugins/` 目录使用新接口，造成混淆和维护负担。

2. **重复代码泛滥**：7 个策略文件中，OHLCV 数据排序逻辑重复 20+ 次，`map[string]any` 参数解析的类型 switch 重复 30+ 次。

3. **并发安全隐患**：策略实例在 `init()` 中全局注册，但 `Configure` 方法直接修改结构体字段，无锁保护。回测引擎支持 `parallelWorkers` 并发，存在数据竞态风险。

4. **双注册表债务**：`Registry`（新）与 `oldRegistry`（兼容 `domain.Strategy`）同时维护，旧注册表仅用于 `cmd/strategy` 服务的有限兼容。

5. **因子系统不完整**：已定义 6 种因子类型（Momentum/Value/Quality/Size/Volatility/Growth），但仅实现 3 种计算逻辑（Momentum/Value/Quality）。

## Decision

### 1. 统一 Strategy 接口（单一事实来源）

以 `pkg/strategy/strategy.go` 中的 `Strategy` 接口为**唯一标准**，废弃 `domain.Strategy`。

```go
// Strategy 是唯一的策略接口（v2.1，统一版）
type Strategy interface {
    Name() string
    Description() string
    Parameters() []Parameter
    Configure(params map[string]interface{}) error
    GenerateSignals(ctx context.Context,
        bars map[string][]domain.OHLCV,
        portfolio *domain.Portfolio) ([]Signal, error)
    Weight(signal Signal, portfolioValue float64) float64
    Cleanup()
}
```

迁移路径：
- `examples/` 目录中的策略迁移到 `plugins/`，适配新接口
- 回测引擎 `getSignals` 中的信号转换逻辑简化（无需再处理旧接口的 `domain.Signal` 字段映射）
- `domain.Strategy` 标记为 deprecated，保留一个 Sprint 后移除

### 2. 提取策略基类与工具函数

创建 `pkg/strategy/base.go` 提供线程安全的配置基类：

```go
type ConfigurableBase struct {
    mu     sync.RWMutex
    params map[string]any
}

func (c *ConfigurableBase) Configure(params map[string]any) error
func (c *ConfigurableBase) GetInt(key string, def int) int
func (c *ConfigurableBase) GetFloat(key string, def float64) float64
func (c *ConfigurableBase) GetString(key string, def string) string
```

创建 `pkg/strategy/utils.go` 提供通用工具：

```go
func SortOHLCV(data []domain.OHLCV)
func SortedCopy(data []domain.OHLCV) []domain.OHLCV
func LatestPrice(data []domain.OHLCV) float64
func ParseIntParam(params map[string]any, key string, def int) int
func ParseFloatParam(params map[string]any, key string, def float64) float64
```

### 3. 清理旧注册表

移除 `oldRegistry` 及 `domain.Strategy` 兼容层：
- 删除 `pkg/strategy/registry.go` 中 `oldRegistry` 相关代码（L182-374）
- 删除 `pkg/strategy/examples/` 目录或标记为 deprecated
- 更新 `cmd/strategy` 服务（如仍使用旧接口）适配新注册表

### 4. 因子系统扩展

在 `pkg/data/factor.go` 中补充缺失因子计算：
- `ComputeSizeFactor` — 对数市值逆序 Z-Score
- `ComputeVolatilityFactor` — N 日收益标准差（低波动 = 高分）
- `ComputeGrowthFactor` — 营收/利润同比增长率

新增 `pkg/data/factor_neutral.go` 实现行业/市值中性化。

## Consequences

### Positive

- **单一接口**：消除双接口混淆，降低新策略开发门槛
- **DRY**：消除 20+ 处重复排序、30+ 处重复参数解析
- **线程安全**：`Configure` 方法受锁保护，支持并发回测
- **因子完备**：6 大经典因子全部可计算，支持多因子策略扩展
- **可测试性**：基类提供 mock 友好接口，便于单元测试

### Negative

- **破坏性变更**：`cmd/strategy` 服务（如使用旧接口）需要同步修改
- **迁移成本**：`examples/` 目录策略需要适配新接口
- **回归风险**：注册表清理可能影响外部插件（需验证 `.so` 插件兼容性）

## Alternatives Considered

| 方案 | 评估 | 结果 |
|------|------|------|
| 保留双接口，添加适配器 | 增加复杂度，不解决根本问题 | ❌ 拒绝 |
| 逐步迁移，不删除旧代码 | 技术债务累积 | ❌ 拒绝 |
| 统一接口 + 保留旧注册表只读 | 兼容性好，但维护负担仍在 | ⚠️ 备选（如 cmd/strategy 无法立即迁移）|

## Migration Plan

1. **Week 1**: 创建 `base.go` + `utils.go`，迁移 `plugins/` 策略使用新工具
2. **Week 1-2**: 迁移 `examples/` → `plugins/`，统一接口
3. **Week 2**: 清理 `oldRegistry`，更新 `cmd/strategy` 适配
4. **Week 3**: 实现缺失因子 + 中性化 + IC 计算
5. **Week 3-4**: 全面单元测试，验证覆盖率 ≥ 70%

## Artifacts

- 新建：`pkg/strategy/base.go`, `pkg/strategy/utils.go`, `pkg/strategy/errors.go`
- 新建：`pkg/data/factor_neutral.go`, `pkg/data/factor_ic.go`
- 修改：`pkg/strategy/strategy.go`, `pkg/strategy/registry.go`, `pkg/strategy/plugins/*.go`
- 修改：`pkg/backtest/engine.go`（简化信号转换）
- 删除：`pkg/strategy/examples/`（或标记 deprecated）

## Metrics

| 指标 | 当前 | 目标 |
|------|------|------|
| 策略重复代码块 | 50+ | ≤ 5 |
| `pkg/strategy` 测试覆盖率 | 12.3% | ≥ 70% |
| 因子类型实现率 | 3/6 | 6/6 |
| 接口数量 | 2 | 1 |

---

_Last updated: 2026-05-04_
