# ODR-040: P2-27~P2-30 基础设施增强

> **Status**: Completed
> **Date**: 2026-06-14
> **Category**: Implementation
> **Related ADRs**: ADR-007, ADR-019
> **Supersedes**: None

## Context
P2-27 到 P2-30 要求增强基础设施，包括 WASM sandbox、EventBus backpressure、跨日状态持久化和数值精度。

## Decision
1. **P2-27 WASM sandbox**: `internal/sandbox/wasm/` — WASMSandbox 接口 + InProcessRuntime 回退 (wazero 未加入依赖，按 AGENTS.md "Ask First" 原则)
2. **P2-28 EventBus backpressure**: `pkg/marketdata/backpressure_bus.go` — drop-oldest 策略 + 每订阅者独立 goroutine + 原子指标
3. **P2-29 跨日状态持久化**: `pkg/backtest/persistence.go` — DiskStateStore + 原子写入 (temp+rename) + 并发安全
4. **P2-30 数值精度**: `pkg/decimal/` — Decimal (int64 定点) + Add/Sub/Mul/Div + Round + 0.1+0.2=0.3 验证

## Consequences
- **Positive**: WASM sandbox 接口已就绪，后续接入 wazero 只需实现 Runtime 接口
- **Positive**: EventBus backpressure 防止慢消费者阻塞系统
- **Positive**: Decimal 解决浮点精度问题 (0.1+0.2=0.3)
- **Negative**: WASM sandbox 当前使用 InProcessRuntime 回退，未实现真正隔离

## Artifacts
- `internal/sandbox/wasm/sandbox.go` + `sandbox_test.go` (新建, 28 TestXxx)
- `pkg/marketdata/backpressure_bus.go` + `backpressure_bus_test.go` (新建, 24 TestXxx)
- `pkg/backtest/persistence.go` + `persistence_test.go` (新建, 22 TestXxx)
- `pkg/decimal/decimal.go` + `decimal_test.go` (新建, 53 TestXxx)

## Metrics
- 代码行数: ~2000 行
- 测试数: 127 TestXxx (race-clean)
- 新增包: 4 个

## Lessons Learned
- WASM sandbox 接口设计应预留多种 Runtime 实现 (wazero/wasmtime/native)
- backpressure 的 drop-oldest 策略适合实时行情场景，丢弃旧数据保留最新
- Decimal 使用 int64 定点数，避免 big.Float 的性能开销
- 原子写入 (temp+rename) 是跨平台安全的文件持久化方式
