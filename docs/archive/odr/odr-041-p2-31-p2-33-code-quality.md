# ODR-041: P2-31~P2-33 代码质量

> **Status**: Completed
> **Date**: 2026-06-14
> **Category**: Implementation
> **Related ADRs**: None
> **Supersedes**: None

## Context
P2-31 到 P2-33 要求提升代码质量，包括拆分长函数、删除空测试和替换 placeholder 断言。

## Decision
1. **P2-31 拆分长函数**:
   - `getSignals` (pkg/backtest/engine.go) 拆为 5 个子函数，每个 < 50 行
   - `RunBacktest` (pkg/live/mock_trader.go) 拆为 4 个子函数
   - `EmergencyFlatten` (pkg/live/mock_trader.go) 拆为 2 个子函数

2. **P2-32 删除空测试**: 删除 4 个 `Test*_Cleanup` 空测试 (TDSequential/BollingerMR/VPT/VolBreakout)

3. **P2-33 替换 placeholder**: 提取 `buildScreenFundamentalsQuery` 纯函数 + 7 个子测试验证 SQL 构建 (替代 `assert.True(t, true)`)

## Consequences
- **Positive**: 函数可读性提升，每个函数职责单一
- **Positive**: 删除空测试减少噪音
- **Positive**: SQL 构建逻辑可独立测试
- **Negative**: 拆分后函数调用链变长

## Artifacts
- `pkg/backtest/engine.go` (修改)
- `pkg/live/mock_trader.go` (修改)
- `pkg/strategy/plugins/coverage_test.go` (修改)
- `pkg/storage/fundamentals.go` (修改)
- `pkg/storage/postgres_screen_test.go` (修改)

## Metrics
- 拆分函数: 3 个 → 11 个子函数
- 删除空测试: 4 个
- 替换 placeholder: 1 处 → 7 个子测试

## Lessons Learned
- 长函数拆分应按职责边界，而非行数
- 空测试比没有测试更糟糕，因为它们制造虚假的安全感
- SQL 构建逻辑提取为纯函数是最有效的测试方式
