# ODR-036: P2-11 期权定价 + Greeks

> **Status**: Completed
> **Date**: 2026-06-14
> **Category**: Implementation
> **Related ADRs**: None
> **Supersedes**: None

## Context
P2-11 要求实现期权定价模型和 Greeks 计算，支持欧式 (Black-Scholes) 和美式 (Binomial CRR) 期权。

## Decision
在 `pkg/strategy/options/` 中实现完整的期权定价系统：

1. **Black-Scholes 模型**: 欧式期权定价 (Call/Put)
2. **Binomial CRR 模型**: 美式期权定价 (支持提前行权)
3. **5 个 Greeks**: Delta, Gamma, Vega, Theta, Rho
4. **Implied Volatility**: Newton-Raphson 迭代求解
5. **Normal CDF**: Abramowitz & Stegun 近似 (避免外部依赖)
6. **收敛验证**: BS 与 Binomial 在大步数时收敛

## Consequences
- **Positive**: 支持期权策略开发和风险管理
- **Positive**: 纯 Go 实现，无外部数学库依赖
- **Negative**: Binomial 模型在大步数时性能较慢

## Artifacts
- `pkg/strategy/options/types.go` (新建)
- `pkg/strategy/options/normal.go` (新建)
- `pkg/strategy/options/black_scholes.go` (新建)
- `pkg/strategy/options/binomial.go` (新建)
- `pkg/strategy/options/normal_test.go` (新建)
- `pkg/strategy/options/black_scholes_test.go` (新建)
- `pkg/strategy/options/binomial_test.go` (新建)

## Metrics
- 代码行数: ~800 行
- 测试数: 75 TestXxx (race-clean)
- 覆盖功能: BS + Binomial + 5 Greeks + ImpliedVol + NormCDF

## Lessons Learned
- Normal CDF 使用 A&S 26.2.17 近似公式，精度 1e-7 足够金融应用
- Binomial CRR 在 steps=1000 时与 BS 价格差异 < 0.01，验证了正确性
- ImpliedVol 使用 Newton-Raphson 迭代，收敛速度快 (通常 < 5 次迭代)
