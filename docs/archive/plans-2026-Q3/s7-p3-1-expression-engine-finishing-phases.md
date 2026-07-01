# S7-P3-1 收尾计划：Phase 5/6/7（表达式引擎扩展）

> **Task**: S7-P3-1 扩展 pkg/ai/expression/ 到信号/仓位/风控层 + ExpressionStrategy 适配器
> **Branch**: `fix/s7-p3-1-cs-neutralize-2arg`
> **Status**: Phase 1-4 已提交，Phase 5 实现已写但未测试，Phase 6-7 未开始

---

## 1. Summary / 摘要

S7-P3-1 的 7 阶段计划中，前 4 阶段（cs_neutralize 修复 + signal/sizing/risk 三层）已提交到
`fix/s7-p3-1-cs-neutralize-2arg` 分支。本计划覆盖剩余 3 个阶段：

| Phase | 内容 | 状态 | Commit |
|-------|------|------|--------|
| 1 | cs_neutralize 2-arg 修复（AST/parser/evaluator/operators） | ✅ | `9f79543` |
| 2 | Signal 层（DSL → []Signal） | ✅ | `9d14f3b` |
| 3 | Position sizing 层（equal/strength/fixed） | ✅ | `de1175d` |
| 4 | Risk control 层（clamp/truncate/scale） | ✅ | `0ba1b8b` |
| **5** | **OHLCVDataProvider 适配器测试 + 提交** | **🔄 实现已写** | — |
| **6** | **ExpressionStrategy 适配器（组合 signal+sizing+risk）** | **⬜** | — |
| **7** | **文档同步（SPEC/ARCHITECTURE/TASKS/AGENTS）** | **⬜** | — |

---

## 2. Current State Analysis / 当前状态（已验证）

### Git 状态
```
Branch: fix/s7-p3-1-cs-neutralize-2arg
Latest: 0ba1b8b feat(strategy/expression): add risk control layer
Untracked: pkg/strategy/expression/data_provider.go (Phase 5 实现，已写好)
```

### 已有文件（`pkg/strategy/expression/`）
- `signal.go` + `signal_test.go` — SignalGenerator（12 tests pass）
- `sizing.go` + `sizing_test.go` — PositionSizer（9 tests pass，定义了 `approxEqual` helper）
- `risk.go` + `risk_test.go` — RiskController（8 tests pass）
- `data_provider.go` — OHLCVDataProvider（**实现已写，测试未写**）

### 关键接口确认
- `aiexpr.DataProvider` 接口（`pkg/ai/expression/evaluator.go:9-12`）：
  ```go
  type DataProvider interface {
      GetField(symbol string, field string, lookback int) ([]float64, error)
      GetSymbols() []string
  }
  ```
- `aiexpr.NewEvaluator(provider DataProvider) *Evaluator`
- `(*Evaluator).Evaluate(node Node, lookback int) (map[string][]float64, error)`
- `strategy.Strategy` 复合接口 = StrategyCore + Configurable + SignalGenerator + ResourceManaged（7 方法）
- `strategy.BaseStrategy` 提供 Name/Description/Configure/Parameters/Cleanup 默认实现 + 线程安全 GetParamInt/Float/String/Bool
- 测试共享 helper：`approxEqual(a, b float64) bool` 定义在 `sizing_test.go:22`

### 关键约束
- `*_test.go` 被 `.gitignore` 排除（line 28），新测试文件必须 `git add -f`
- 合规测试 `pkg/strategy/interfaces_compliance_test.go` blank-imports `examples` + `plugins`，Phase 6 需新增 `expression` 的 blank import
- `pkg/strategy/plugins/utils.go` 的 parse helpers 是**未导出**的，无法直接 import

---

## 3. Phase 5: OHLCVDataProvider 测试 + 提交

### 3.1 现状
`data_provider.go` 已写好（111 行），实现正确：
- `NewOHLCVDataProvider(bars)` — symbols 预排序，保证 cross-sectional 确定性
- `GetField(symbol, field, lookback)` — 支持 open/high/low/close/volume/turnover；fundamental 字段（pe/pb 等）返回 error；unknown symbol 返回空 slice；lookback>0 截取最近 N 条
- `GetSymbols()` — 返回排序后的 symbol 列表
- 编译时检查：`var _ aiexpr.DataProvider = (*OHLCVDataProvider)(nil)`

### 3.2 待办

**创建** `pkg/strategy/expression/data_provider_test.go`，覆盖 7 个测试：

| # | 测试名 | 验证点 |
|---|--------|--------|
| 1 | `TestGetField_Close_10Bars` | 10 根 bar，GetField("A", "close", 0) 返回 10 个 close 值 |
| 2 | `TestGetField_Lookback5_ReturnsLast5` | lookback=5，返回最后 5 个值 |
| 3 | `TestGetField_Lookback0_ReturnsAll` | lookback=0，返回全部（同 lookback<0） |
| 4 | `TestGetSymbols_Sorted` | 3 个 symbol 输入乱序，GetSymbols 返回字母序 |
| 5 | `TestGetField_UnknownSymbol_Empty` | unknown symbol → 空 slice, nil error |
| 6 | `TestGetField_FundamentalField_Error` | field="pe" → error 包含 "not available" |
| 7 | `TestGetField_EmptyBars_EmptySymbols` | 空 bars map → GetSymbols 返回空 slice |

测试用 `domain.OHLCV{Close: ..., Date: time.Now()}` 构造 bars，使用 `approxEqual` helper（已在 sizing_test.go 定义）。

### 3.3 验证
```bash
go test ./pkg/strategy/expression/... -count=1 -race -v
go vet ./pkg/strategy/expression/...
gofmt -l pkg/strategy/expression/
```

### 3.4 提交
```bash
git add pkg/strategy/expression/data_provider.go
git add -f pkg/strategy/expression/data_provider_test.go
git commit -m "feat(strategy/expression): add OHLCVDataProvider adapter

Bridges map[string][]domain.OHLCV (strategy GenerateSignals input) to
aiexpr.DataProvider so the expression evaluator can read bar data
without a separate fetch round-trip. Supports open/high/low/close/
volume/turnover; fundamental fields error out (v1). Symbols pre-sorted
for deterministic cross-sectional ops.

Refs: S7-P3-1
Reviewed: self-review + go vet + gofmt + tests pass"
```

---

## 4. Phase 6: ExpressionStrategy 适配器

### 4.1 目标
创建 `ExpressionStrategy`，组合 SignalGenerator + PositionSizer + RiskController +
OHLCVDataProvider + Evaluator 为一个 `strategy.Strategy` 实现。这是 S7-P3-1 的核心交付物——
让 DSL 表达式可以直接作为策略运行。

### 4.2 设计决策

**决策 1：不创建 helpers.go，改用 BaseStrategy 的类型化 getter**

原计划要内联 ~30 LOC parse helpers（parseIntParam 等）from plugins/utils.go。但
`BaseStrategy` 已提供 `GetParamInt/Float/String/Bool`，处理 JSON 的 float64/int 透明转换。
Configure 的模式是：
```go
// 先存入 base params map
s.BaseStrategy.Configure(params)
// 再用 GetParam* 读取（missing key 返回当前值作为 default，实现"部分更新"）
expr := s.GetParamString("signal_expr", s.signalCfg.Expression)
```
这样无需任何 parse helper，减少 ~30 LOC。

**决策 2：GenerateSignals 内完成 sizing+risk，Weight() 返回预计算权重**

Strategy 接口的 `Weight(signal, portfolioValue)` 是 per-signal 的，但 risk 控制是 set-level
（MaxOpenPositions、MinCashBuffer 需要看全部权重）。因此：
- `GenerateSignals`：生成 signals → sizer.Size → riskCtl.Check → 将最终权重写回 Signal.Strength
- `Weight(signal, _)`：直接返回 `signal.Strength`（已是最终权重）

原始 DSL 值保留在 `Signal.Metadata["raw_strength"]` 供调试。

**决策 3：默认策略名 `expression_template`，通过 init() 自注册**

合规测试需要所有 blank-imported 包的 init() 注册策略。`expression_template` 使用合理默认值：
- Expression: `cs_rank(close) > 0.8`（买入 close 排名前 20% 的股票）
- Action: buy, Direction: long
- Sizing: equal, MaxTotal=1.0, MaxPerStock=0.10
- Risk: MaxPositionPct=0.10, MaxOpenPositions=20, MinCashBuffer=0.05

### 4.3 待办

#### 4.3.1 创建 `pkg/strategy/expression/strategy.go`

```go
package expression

import (
    "context"
    "fmt"
    "sort"

    aiexpr "github.com/ruoxizhnya/quant-trading/pkg/ai/expression"
    "github.com/ruoxizhnya/quant-trading/pkg/domain"
    "github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// ExpressionStrategyConfig 持有所有配置（用于 Configure 重建组件）
type ExpressionStrategyConfig struct {
    SignalCfg  SignalConfig
    SizingCfg  SizingConfig
    RiskCfg    RiskConfig
    Lookback   int
}

// ExpressionStrategy 组合 signal/sizing/risk 为一个 strategy.Strategy
type ExpressionStrategy struct {
    *strategy.BaseStrategy
    
    signalGen *SignalGenerator
    sizer     *PositionSizer
    riskCtl   *RiskController
    cfg       ExpressionStrategyConfig
}

// NewExpressionStrategy 构造策略（应用默认值）
func NewExpressionStrategy(name string, cfg ExpressionStrategyConfig) (*ExpressionStrategy, error)

// Configure 从 map[string]any 重建所有组件
func (s *ExpressionStrategy) Configure(params map[string]interface{}) error

// GenerateSignals: bars → provider → evaluator → signals → size → risk → 返回
func (s *ExpressionStrategy) GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]strategy.Signal, error)

// Weight 返回 signal.Strength（已在 GenerateSignals 中预计算为最终权重）
func (s *ExpressionStrategy) Weight(signal strategy.Signal, portfolioValue float64) float64

// Parameters 返回参数 schema
func (s *ExpressionStrategy) Parameters() []strategy.Parameter

// 编译时检查
var _ strategy.Strategy = (*ExpressionStrategy)(nil)

// init 自注册默认模板策略
func init() {
    s, _ := NewExpressionStrategy("expression_template", defaultConfig())
    strategy.GlobalRegister(s)
}
```

**GenerateSignals 流程**：
1. `provider := NewOHLCVDataProvider(bars)`
2. `evaluator := aiexpr.NewEvaluator(provider)`
3. `signals, err := s.signalGen.Generate(bars, evaluator)`
4. `weights, err := s.sizer.Size(signals, portfolio.TotalValue)`
5. `filtered, err := s.riskCtl.Check(weights, portfolio)`
6. 遍历 signals：若 symbol 在 filtered 中且 weight>0，设 Strength=weight, Metadata["raw_strength"]=原值；否则丢弃
7. 返回过滤后的 signals（按 symbol 排序）

**Configure 参数 schema**：
| key | type | default | 说明 |
|-----|------|---------|------|
| signal_expr | string | "cs_rank(close) > 0.8" | DSL 表达式 |
| action | string | "buy" | buy/sell |
| direction | string | "long" | long/short/close |
| min_strength | float | 0.0 | 最小信号强度 |
| sizing_method | string | "equal" | equal/strength_prop/fixed |
| fixed_weight | float | 0.05 | SizingFixed 时每信号权重 |
| max_per_stock | float | 0.10 | 单仓上限 |
| max_total | float | 1.0 | 总仓位上限 |
| max_position_pct | float | 0.10 | 风控单仓上限 |
| max_open_positions | int | 20 | 风控最大持仓数 |
| min_cash_buffer | float | 0.05 | 风控现金缓冲 |
| lookback | int | 60 | evaluator lookback |

#### 4.3.2 创建 `pkg/strategy/expression/strategy_test.go`

| # | 测试名 | 验证点 |
|---|--------|--------|
| 1 | `TestExpressionStrategy_ImplementsStrategy` | 编译时 `var _ strategy.Strategy = (*ExpressionStrategy)(nil)` |
| 2 | `TestNewExpressionStrategy_Defaults` | 空配置构造，验证默认值应用 |
| 3 | `TestNewExpressionStrategy_InvalidExpression` | 空表达式 → error |
| 4 | `TestExpressionStrategy_Configure_RebuildsComponents` | Configure 后 signal 表达式更新 |
| 5 | `TestExpressionStrategy_GenerateSignals_HappyPath` | 3 个 symbol (close=40/60/70)，expr=`close > 50` → 2 signals (60,70)，权重=0.10 each |
| 6 | `TestExpressionStrategy_GenerateSignals_EmptyBars` | 空 bars → 0 signals, no error |
| 7 | `TestExpressionStrategy_Weight_ReturnsStrength` | Weight(sig, 0) == sig.Strength |

测试用例 5 的表达式 `close > 50` 是简单比较，evaluator 会返回 1.0/0.0，truthy 的 symbol 生成信号。

#### 4.3.3 更新 `pkg/strategy/interfaces_compliance_test.go`

在 blank import 区块（line 44-45）新增：
```go
_ "github.com/ruoxizhnya/quant-trading/pkg/strategy/expression"
```

这确保 `TestRegisteredStrategies_SatisfyComposite` 包含 `expression_template`。

### 4.4 验证
```bash
go test ./pkg/strategy/expression/... -count=1 -race -v
go test ./pkg/strategy/... -count=1 -race -v   # 包含 compliance test
go vet ./pkg/strategy/...
gofmt -l pkg/strategy/expression/ pkg/strategy/interfaces_compliance_test.go
```

### 4.5 提交
```bash
git add pkg/strategy/expression/strategy.go
git add -f pkg/strategy/expression/strategy_test.go
git add pkg/strategy/interfaces_compliance_test.go
git commit -m "feat(strategy/expression): add ExpressionStrategy adapter

Composes SignalGenerator + PositionSizer + RiskController + OHLCVDataProvider
into a strategy.Strategy. GenerateSignals builds an evaluator from bars,
produces signals, sizes them, applies risk checks, and returns filtered
signals with final weights as Strength. Weight() returns the precomputed
weight. Default 'expression_template' strategy self-registers via init().

Refs: S7-P3-1
Reviewed: self-review + go vet + gofmt + tests pass"
```

---

## 5. Phase 7: 文档同步

### 5.1 待办

#### 5.1.1 `docs/TASKS.md`
将 S7-P3-1 行的状态从 ⬜ 改为 ✅（line 1627 附近）。

#### 5.1.2 `docs/ARCHITECTURE.md`
在目录树中 `pkg/strategy/` 下新增 `expression/` 子包说明（如果目录树有细粒度列出）。

#### 5.1.3 `docs/SPEC.md`
在策略章节新增 ExpressionStrategy 说明：
- DSL 表达式 → 策略的端到端流程
- 配置参数表（signal_expr, action, sizing_method 等）
- 与 AI pipeline 的集成点（AI 输出 YAML → Configure → 回测）

#### 5.1.4 `AGENTS.md`
检查目录树（§3）是否需要新增 `pkg/strategy/expression/` 条目。如已有 `strategy/` 概括则无需改动。

### 5.2 验证
- 文档交叉引用一致性
- 无 broken link

### 5.3 提交
```bash
git add docs/TASKS.md docs/ARCHITECTURE.md docs/SPEC.md AGENTS.md  # 按实际改动
git commit -m "docs(strategy/expression): document ExpressionStrategy and mark S7-P3-1 done

Refs: S7-P3-1
Reviewed: consistency check"
```

---

## 6. Verification / 全局验证

Phase 7 完成后运行完整验证：
```bash
# 后端构建
go build ./...

# 静态检查
go vet ./...

# 格式检查
gofmt -l .

# 受影响包测试
go test ./pkg/strategy/... -count=1 -race
go test ./pkg/ai/expression/... -count=1 -race

# 全量回归（排除 e2e）
go test ./... -count=1 -race -short
```

---

## 7. Assumptions & Decisions / 假设与决策

| # | 决策 | 理由 |
|---|------|------|
| 1 | 不创建 helpers.go，用 BaseStrategy.GetParam* | 避免重复 parse 逻辑，BaseStrategy 已处理 JSON 类型转换 |
| 2 | GenerateSignals 内完成 sizing+risk，Weight 返回预计算值 | risk 是 set-level，无法拆到 per-signal Weight |
| 3 | 默认策略名 `expression_template` | 合规测试需要自注册实例 |
| 4 | 测试用 `close > 50` 简单比较表达式 | 避免依赖复杂 evaluator 行为，聚焦策略编排逻辑 |
| 5 | 原始 DSL 值存入 Signal.Metadata["raw_strength"] | 调试可见性，不污染 Strength（已是权重） |
| 6 | data_provider.go 实现已写好且正确，Phase 5 只需补测试 | 代码审查确认实现与接口匹配 |

---

## 8. 执行顺序

1. **Phase 5**: 写 `data_provider_test.go` → 验证 → 提交
2. **Phase 6**: 写 `strategy.go` → 写 `strategy_test.go` → 更新 compliance test → 验证 → 提交
3. **Phase 7**: 更新 4 个文档 → 验证 → 提交
4. **全局验证**: build + vet + fmt + test
5. **合并**: 准备 PR 或直接合并到 main（按用户指示）

每个 phase 遵循 atomic commit 原则，commit message 格式遵循 AGENTS.md §8.3。
