# ODR-034: P2-9 融资融券 + 做空

> **Status**: Completed
> **Date**: 2026-06-14
> **Category**: Implementation
> **Related ADRs**: None
> **Supersedes**: None

## Context
P2-9 要求实现融资融券和做空机制，支持 A 股市场的信用交易。需要管理保证金账户、融资买入、融券卖出、强制平仓等核心功能。

## Decision
在 `pkg/live/margin.go` 中实现完整的融资融券系统：

1. **MarginAccount**: 管理现金、持仓、融资余额、融券持仓
2. **ShortableList**: 线程安全的融券标的注册表 (RWMutex)
3. **MarginCalculator**: 纯函数计算器，计算维持担保比例、可用保证金
4. **4 类操作**: MarginBuy / ShortSell / BuyToCover / MarginSell
5. **利息计提**: 按日计息，支持融资利率和融券利率
6. **强制平仓**: 维持担保比例 < 130% 时触发强制平仓

## Consequences
- **Positive**: 支持信用交易策略，扩大策略范围
- **Positive**: 强制平仓机制确保合规
- **Negative**: 增加了风险管理的复杂度
- **Negative**: 利息计提需要定期执行

## Artifacts
- `pkg/live/margin.go` (新建)
- `pkg/live/margin_test.go` (新建, 55 TestXxx)

## Metrics
- 代码行数: ~600 行
- 测试数: 55 TestXxx (race-clean)
- 覆盖功能: 4 类操作 + 利息计提 + 强制平仓 + ShortableList

## Lessons Learned
- 使用纯函数 MarginCalculator 便于测试和复用
- ShortableList 用 RWMutex 而非 Mutex，读多写少场景性能更好
