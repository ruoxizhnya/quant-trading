# ODR-037: P2-12 港股通/北向因子

> **Status**: Completed
> **Date**: 2026-06-14
> **Category**: Implementation
> **Related ADRs**: None
> **Supersedes**: None

## Context
P2-12 要求实现港股通和北向资金因子，支持北向资金流向分析和因子构建。

## Decision
在 `pkg/data/source/hkex/` 中实现北向资金数据管道：

1. **EastmoneyNorthboundFetcher**: 从东方财富抓取北向资金数据
2. **NorthboundFactor**: 5 类因子计算
   - MA 因子: 北向资金净流入移动平均
   - 动量因子: 北向资金净流入变化率
   - 持股变化因子: 个股北向持股比例变化
   - 排名因子: 北向资金持股排名
   - 信号因子: 综合信号生成
3. **ExchangeRateConverter**: CNY/HKD 汇率换算
4. **线程安全**: 所有 fetcher 和 factor 线程安全

## Consequences
- **Positive**: 支持北向资金因子策略开发
- **Positive**: 汇率换算确保跨市场数据一致性
- **Negative**: 依赖东方财富数据源，可能受反爬影响

## Artifacts
- `pkg/data/source/hkex/types.go` (新建)
- `pkg/data/source/hkex/fetcher.go` (新建)
- `pkg/data/source/hkex/factor.go` (新建)
- `pkg/data/source/hkex/fetcher_test.go` (新建)
- `pkg/data/source/hkex/factor_test.go` (新建)

## Metrics
- 代码行数: ~900 行
- 测试数: 75 TestXxx (race-clean)
- 覆盖功能: Fetcher + 5 类因子 + 汇率换算

## Lessons Learned
- 北向资金数据需要处理 A 股和港股的交易日差异
- 汇率换算使用日中间价，避免实时波动影响因子稳定性
- 因子计算使用纯函数，便于并行计算和测试
