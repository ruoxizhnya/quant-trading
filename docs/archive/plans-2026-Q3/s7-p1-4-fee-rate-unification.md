# S7-P1-4: 修复费率配置三重定义（费率单一来源统一）

> **任务来源**: ODR-043 P1 — "修复费率配置三重定义（让 backtest 引用 fees.AShareFees）"
> **执行规范**: AGENTS.md §8.3 — 测试先行 + 代码审查 + 原子提交

---

## 1. Summary / 摘要

A 股交易费率（佣金/印花税/过户费/最小佣金/滑点）目前在 4 处独立硬编码，导致：
- `pkg/backtest/constants.go` 5 个常量是**独立字面量**（与 `pkg/fees` 重复定义）
- `pkg/domain/execution.go:17` 用了**错误佣金率 `0.00025`**（应为 `0.0003`）— D1 bug
- `pkg/ai/yaml/generator.go:149-150` 用了**错误佣金率 `0.00025`** + **错误滑点率 `0.001`** — D2 bug
- `pkg/ai/yaml/generator.go:222` 格式串 `%.3f` 会把 `0.0001` 截断为 `"0.000"` — D5 bug

本任务将 `pkg/fees` 确立为**唯一事实来源**：`backtest` 5 常量改为 `fees.Default*` 别名（零成本 const aliasing，无行为变化），并修复 D1/D2/D5 三处真实数值/格式 bug。同时清理 7 处测试 fixture 中的 `0.00025` 字面量。

**预期行为变化**:
- `backtest` 5 常量 aliasing：**无行为变化**（值完全相同）
- D1 修复 `domain.DefaultExecutionConfig().CommissionRate`：`0.00025` → `0.0003`（Dead code，无调用方，但需修正+测试）
- D2 修复 `ai/yaml/generator` 生成 YAML 的 `commission_rate`：`0.00025` → `0.0003`；`slippage_rate`：`0.001` → `0.0001`
- D5 修复格式串：`%.3f` → `%.5f`（避免 `0.0001` 被截断为 `"0.000"`）

---

## 2. Current State Analysis / 现状分析

### 2.1 单一来源已存在：`pkg/fees/ashare.go`

```
DefaultCommissionRate  = 0.0003   // 佣金 0.03%
DefaultStampTaxRate    = 0.001    // 印花税 0.1% (卖出)
DefaultTransferFeeRate = 0.00001  // 过户费 0.001%
DefaultMinCommission   = 5.0      // 最小佣金 ¥5
DefaultSlippageRate    = 0.0001   // 滑点假设 0.01%
FixedSlippageRate      = 0.001    // 固定滑点模型 0.1%
```
- 只 import `fmt` → **无循环依赖风险**（`backtest`/`domain`/`ai/yaml` 均可安全 import）
- 已有 `DefaultAShareFees()` / `ApplyDefaults()` / `Validate()` 完整 API

### 2.2 三重定义问题

| 位置 | 常量 | 当前值 | 是否与 fees 一致 |
|------|------|--------|-----------------|
| `pkg/backtest/constants.go:8` | `DefaultStampTaxRate` | `0.001` | ✅ 一致（但独立字面量，drift 风险）|
| `pkg/backtest/constants.go:11` | `DefaultMinCommission` | `5.0` | ✅ 一致 |
| `pkg/backtest/constants.go:14` | `DefaultTransferFeeRate` | `0.00001` | ✅ 一致 |
| `pkg/backtest/constants.go:35` | `DefaultCommissionRate` | `0.0003` | ✅ 一致 |
| `pkg/backtest/constants.go:38` | `DefaultSlippageRate` | `0.0001` | ✅ 一致 |
| `pkg/domain/execution.go:17` | `CommissionRate` 字面量 | `0.00025` | ❌ **D1 bug** |
| `pkg/ai/yaml/generator.go:149` | `CommissionRate` 字面量 | `0.00025` | ❌ **D2 bug** |
| `pkg/ai/yaml/generator.go:150` | `SlippageRate` 字面量 | `0.001` | ❌ **D2 bug**（用了 FixedSlippageRate 而非 DefaultSlippageRate）|
| `pkg/ai/yaml/generator.go:222` | 格式串 `%.3f` | — | ❌ **D5 bug**（`0.0001` → `"0.000"` 截断）|

### 2.3 测试 Fixture 中的 0.00025（7 处）

| 文件 | 行号 | 用途 |
|------|------|------|
| `pkg/ai/yaml/generator_test.go` | 71 | 断言 `commission_rate: 0.00025`（需改为 `0.00030`）|
| `pkg/backtest/execution_integration_test.go` | 20, 30, 261, 286, 357 | NewTracker/ExecutionConfig fixture |
| `pkg/live/engine_test.go` | 32 | newTestEngine fixture |

> **断言安全性已验证**: `execution_integration_test.go` 的断言用 `trade.Commission > 0`（非具体值），滑点用 `FixedSlippageRate`（不变）→ 改佣金率不会破坏断言。

### 2.4 必须排除的假阳性

| 文件 | 行号 | 内容 | 为何不能改 |
|------|------|------|-----------|
| `pkg/ai/cost.go` | 47 | `InputPer1K: 0.00025` | Claude API USD 定价，**非交易佣金** |
| `pkg/fees/ashare.go` | 5 | 注释文本 | 包文档说明 |
| `pkg/live/simulated_broker_test.go` | 多行 | 注释/断言 | S7-P0-5 已修复源码，测试断言源码不再含 `0.00025` |

### 2.5 import 现状

- `pkg/backtest/execution.go` **已 import** `pkg/fees` → `constants.go` 加 import 无新依赖
- `pkg/domain` **未 import** `pkg/fees` → 需新增（安全，`fees` 只依赖 `fmt`）
- `pkg/ai/yaml/generator.go` **未 import** `pkg/fees` → 需新增（安全）

---

## 3. Proposed Changes / 实施步骤

### Step 0: 创建 feature branch

```bash
git checkout -b fix/s7-p1-4-fee-rate-unification
```

### Step 1 (Red — 测试先行): 编写/修改失败测试

**1a. 新建 `pkg/domain/execution_test.go`**（若不存在）:
```go
package domain

import (
    "testing"
    "github.com/ruoxizhnya/quant-trading/pkg/fees"
    "github.com/stretchr/testify/assert"
)

// TestDefaultExecutionConfig_UsesCanonicalFees (S7-P1-4, D1)
// DefaultExecutionConfig 之前硬编码 0.00025 佣金率，与 fees 包
// 的 0.0003 不一致。修复后必须引用 fees.DefaultCommissionRate。
func TestDefaultExecutionConfig_UsesCanonicalFees(t *testing.T) {
    cfg := DefaultExecutionConfig()
    assert.Equal(t, fees.DefaultCommissionRate, cfg.CommissionRate,
        "DefaultExecutionConfig.CommissionRate must equal fees.DefaultCommissionRate")
    assert.Equal(t, fees.DefaultMinCommission, cfg.MinCommission,
        "DefaultExecutionConfig.MinCommission must equal fees.DefaultMinCommission")
}
```

**1b. 修改 `pkg/ai/yaml/generator_test.go:71`**:
```go
// 旧: assert.Contains(t, yaml, "commission_rate: 0.00025")
// 新:
assert.Contains(t, yaml, "commission_rate: 0.00030")  // %.5f of 0.0003
// 新增 D5 断言（捕获 %.3f 截断 bug）:
assert.Contains(t, yaml, "slippage_rate: 0.00010",    // %.5f of 0.0001; %.3f would render "0.000"
    "slippage_rate must not be truncated by %% .3f format")
```

**1c. 新建 `pkg/backtest/constants_drift_test.go`**（drift guard）:
```go
package backtest

import (
    "testing"
    "github.com/ruoxizhnya/quant-trading/pkg/fees"
    "github.com/stretchr/testify/assert"
)

// TestBacktestFeeConstants_AliasedToFees (S7-P1-4)
// 确保 backtest 包的 5 个费率常量始终与 pkg/fees 单一来源一致。
// 如果有人将来在 constants.go 改回独立字面量，此测试会立即失败。
func TestBacktestFeeConstants_AliasedToFees(t *testing.T) {
    assert.Equal(t, fees.DefaultCommissionRate, DefaultCommissionRate)
    assert.Equal(t, fees.DefaultStampTaxRate, DefaultStampTaxRate)
    assert.Equal(t, fees.DefaultTransferFeeRate, DefaultTransferFeeRate)
    assert.Equal(t, fees.DefaultMinCommission, DefaultMinCommission)
    assert.Equal(t, fees.DefaultSlippageRate, DefaultSlippageRate)
}
```

> `*_test.go` 默认被 .gitignore 忽略（memory 记录），新建测试文件需 `git add -f`。

### Step 2 (Green — 核心 aliasing): 修改 `pkg/backtest/constants.go`

**加 import**:
```go
import (
    "time"
    "github.com/ruoxizhnya/quant-trading/pkg/fees"
)
```

**5 个常量改为别名**（值不变，零成本 const aliasing）:
```go
const (
    DefaultStampTaxRate     = fees.DefaultStampTaxRate
    DefaultMinCommission    = fees.DefaultMinCommission
    DefaultTransferFeeRate  = fees.DefaultTransferFeeRate
    // ...（PriceLimit/NewStockDays 等非费率常量保持不变）
)

const (
    // ...（InitialCapital/RiskFreeRate 等保持不变）
    DefaultCommissionRate = fees.DefaultCommissionRate
    DefaultSlippageRate   = fees.DefaultSlippageRate
    // ...（ShortSellingRate/TradingDaysPerYear 保持不变）
)
```

### Step 3 (Green — D1 修复): 修改 `pkg/domain/execution.go`

**加 import** `fees`，**改 line 17**:
```go
import "github.com/ruoxizhnya/quant-trading/pkg/fees"

func DefaultExecutionConfig() ExecutionConfig {
    return ExecutionConfig{
        OrderType:      OrderTypeMarket,
        SlippageModel:  "fixed",
        CommissionRate: fees.DefaultCommissionRate,  // was 0.00025
        MinCommission:  fees.DefaultMinCommission,
        InitialCapital: 1000000,
    }
}
```

### Step 4 (Green — D2+D5 修复): 修改 `pkg/ai/yaml/generator.go`

**加 import** `fees`。

**改 lines 149-150**（`intentToConfig` 内）:
```go
Backtest: BacktestConfig{
    StartDate:      "2020-01-01",
    EndDate:        "2024-01-01",
    InitialCapital: 1000000,
    CommissionRate: fees.DefaultCommissionRate,  // was 0.00025
    SlippageRate:   fees.DefaultSlippageRate,    // was 0.001 (FixedSlippageRate)
    RebalanceFreq:  "daily",
},
```

**改 line 222**（格式串 D5）:
```go
// 旧: b.WriteString(fmt.Sprintf("%sslippage_rate: %.3f\n", indent, config.Backtest.SlippageRate))
// 新:
b.WriteString(fmt.Sprintf("%sslippage_rate: %.5f\n", indent, config.Backtest.SlippageRate))
```

### Step 5 (Test fixture cleanup): 清理 7 处 `0.00025` 字面量

**5a. `pkg/backtest/execution_integration_test.go`**（5 处: lines 20, 30, 261, 286, 357）:
- 加 `import "github.com/ruoxizhnya/quant-trading/pkg/fees"`
- 每处 `0.00025` → `fees.DefaultCommissionRate`

**5b. `pkg/live/engine_test.go`**（1 处: line 32）:
- 加 `import "github.com/ruoxizhnya/quant-trading/pkg/fees"`
- `CommissionRate: 0.00025` → `CommissionRate: fees.DefaultCommissionRate`

**5c. `pkg/ai/yaml/generator_test.go`**（Step 1b 已处理断言改值）

### Step 6 (Doc sync): 文档同步

- **`docs/TASKS.md`**: 找到 S7-P1-4 行，标记 ✅ 完成
- **`AGENTS.md` §13/§14**: 检查是否需更新（已知问题表中若有 `0.00025` 条目则移除）— 实测 §14 无此条目，无需改
- **`docs/odr/odr-043-*.md`**: 无需改（任务追踪在 TASKS.md）
- **`docs/live-trading.md`** 等设计文档: grep 检查是否引用旧费率值，按需更新

### Step 7 (Verify): 验证

```bash
go build ./...
go vet ./...
gofmt -l pkg/backtest/constants.go pkg/domain/execution.go pkg/ai/yaml/generator.go \
       pkg/backtest/execution_integration_test.go pkg/live/engine_test.go \
       pkg/ai/yaml/generator_test.go pkg/domain/execution_test.go \
       pkg/backtest/constants_drift_test.go
# gofmt -l 应无输出

go test ./pkg/fees/... ./pkg/backtest/... ./pkg/domain/... ./pkg/ai/yaml/... ./pkg/live/... -count=1 -race

# Grep 审计: 0.00025 应只剩假阳性
grep -rn "0\.00025" --include="*.go" .
# 预期仅: pkg/ai/cost.go:47 (Claude API 定价)
#          pkg/fees/ashare.go:5 (注释)
#          pkg/live/simulated_broker_test.go (S7-P0-5 断言注释)
```

### Step 8 (Atomic commit + merge): 原子提交

```bash
git add pkg/backtest/constants.go \
        pkg/domain/execution.go pkg/domain/execution_test.go \
        pkg/ai/yaml/generator.go pkg/ai/yaml/generator_test.go \
        pkg/backtest/execution_integration_test.go \
        pkg/backtest/constants_drift_test.go \
        pkg/live/engine_test.go \
        docs/TASKS.md
git add -f pkg/backtest/constants_drift_test.go pkg/domain/execution_test.go  # *_test.go 被 gitignore

git commit -F- <<'EOF'
fix(fees): unify fee rate definitions to single source (pkg/fees)

Aliased 5 backtest fee constants to fees.Default* (zero-cost const
aliasing, no behavior change). Fixed 2 numeric divergence bugs:
- domain/execution.go: CommissionRate 0.00025 -> fees.DefaultCommissionRate (0.0003)
- ai/yaml/generator.go: CommissionRate 0.00025 -> fees.DefaultCommissionRate;
  SlippageRate 0.001 -> fees.DefaultSlippageRate (0.0001)
Fixed format string truncation: %.3f -> %.5f (0.0001 was rendered as "0.000").

Cleaned 7 test fixtures using 0.00025 literal. Added drift-guard test
ensuring backtest fee constants stay aliased to pkg/fees.

Refs: S7-P1-4 (ODR-043)
Reviewed: self-review + go vet + gofmt + tests pass
EOF

git checkout main && git merge --no-ff fix/s7-p1-4-fee-rate-unification
```

---

## 4. Assumptions & Decisions / 假设与决策

### D1: 滑点率从 `0.001` 改为 `0.0001`（`DefaultSlippageRate`）— 行为变化
- **理由**: `0.001` = `fees.FixedSlippageRate`（固定模型，悲观 10x），语义错误；YAML `slippage_rate` 是回测假设值，应对应 `fees.DefaultSlippageRate` (0.0001)，与 `backtest.DefaultSlippageRate` 一致
- **影响**: AI 生成的策略 YAML 滑点假设从 0.1% 降为 0.01%，更贴合回测引擎默认值
- **格式串必须同步改**: `%.3f` → `%.5f`，否则 `0.0001` 被截断为 `"0.000"`

### D2: 不重构 `TradingConfig` 结构体（推迟到 S7-P1-1）
- **理由**: `fees.AShareFees` 无 mapstructure tag，结构体嵌入会导致 YAML 配置 schema 变化，影响面大
- **本任务范围**: 仅做常量级别 aliasing + 字面量 bug 修复，不触碰结构体
- **S7-P1-1**: 届时统一抽取 `pkg/settlement` + `pkg/portfolio` 共享原语包，再处理结构体融合

### D3: `domain.DefaultExecutionConfig()` 是 dead code 但仍修复
- **理由**: 审计 D1 标记为 bug；虽无调用方，但未来可能被引用，且 drift-guard 测试需要它正确
- **不删除**: 删除属于 API 变更，超出本任务范围

### D4: 测试文件 `git add -f`
- **理由**: `.gitignore` line 28 默认忽略 `*_test.go`；新建的 `execution_test.go` 和 `constants_drift_test.go` 必须 force-add（memory 记录的约定）

### D5: `pkg/backtest/constants.go` 中非费率常量不动
- `DefaultPriceLimitNormal/ST/New`、`DefaultNewStockDays`、`DefaultInitialCapital`、`DefaultRiskFreeRate`、`DefaultShortSellingRate`、`TradingDaysPerYear`、`JobPoll*`、`DefaultStateStoreCapacity` — 均非费率，`pkg/fees` 无对应常量，保持独立

---

## 5. Verification Checklist / 验证清单

- [ ] `go build ./...` 通过
- [ ] `go vet ./...` 无警告
- [ ] `gofmt -l` 对所有修改文件无输出
- [ ] `go test ./pkg/fees/... ./pkg/backtest/... ./pkg/domain/... ./pkg/ai/yaml/... ./pkg/live/... -count=1 -race` 全绿
- [ ] Grep 审计: `0.00025` 仅剩 `pkg/ai/cost.go:47` + 注释行
- [ ] `docs/TASKS.md` S7-P1-4 标记 ✅
- [ ] Commit message 符合 AGENTS.md §8.3 格式
- [ ] 单一 atomic commit（不混合其他任务）
