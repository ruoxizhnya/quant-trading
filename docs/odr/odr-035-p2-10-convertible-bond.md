# ODR-035: P2-10 可转债策略

> **Status**: Completed
> **Date**: 2026-06-14
> **Category**: Implementation
> **Related ADRs**: None
> **Supersedes**: None

## Context
P2-10 要求实现可转债策略，包括转股价值、纯债价值、溢价率计算，以及强制赎回和回售条款的触发逻辑。

## Decision
在 `pkg/strategy/plugins/convertible_bond.go` 中实现完整的可转债策略：

1. **核心指标**: 转股价值、纯债价值、转股溢价率、纯债溢价率
2. **Delta 计算**: 用于衡量可转债对正股价格变动的敏感度
3. **强制赎回**: 正股价格连续 15/30 个交易日高于转股价 130% 时触发
4. **回售**: 正股价格连续 30 个交易日低于转股价 70% 时触发
5. **Strategy 接口**: 完整实现 Strategy interface，支持动态加载

## Consequences
- **Positive**: 支持可转债这一重要 A 股衍生品策略
- **Positive**: 强制赎回和回售逻辑确保合规
- **Negative**: 需要持续监控正股价格以触发条款

## Artifacts
- `pkg/strategy/plugins/convertible_bond.go` (新建)
- `pkg/strategy/plugins/convertible_bond_test.go` (新建, 59 TestXxx)

## Metrics
- 代码行数: ~550 行
- 测试数: 59 TestXxx (race-clean)
- 覆盖功能: 转股价值/纯债价值/溢价率/Delta + 强制赎回 + 回售

## Lessons Learned
- 可转债的强制赎回和回售需要维护连续交易日计数器
- Delta 计算使用数值微分近似，精度足够且避免复杂数学库依赖
