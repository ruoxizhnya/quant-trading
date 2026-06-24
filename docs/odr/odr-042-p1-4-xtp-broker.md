# ODR-042: P1-4 中泰 XTP 券商对接 (接口层 + OfflineMode stub)

> **Status**: Completed
> **Date**: 2026-06-14
> **Category**: Implementation
> **Related ADRs**: None
> **Supersedes**: None

## Context
P1-4 要求实现 A 股券商真实对接，推荐使用中泰证券 XTP SDK。XTP 是 C++ SDK，需要 CGo 绑定。由于 SDK 是私有库且需要券商账户，按 AGENTS.md "Ask First" 原则，先实现接口层和 stub。

## Decision
在 `pkg/live/broker/xtp/` 中创建 XTP 券商对接的 Go 接口层：

1. **Config**: 完整的 XTP 连接配置 (IP/Port/AccountID/Password/Protocol/Environment/Heartbeat/Reconnect)
2. **ConnectionState**: 7 状态机 (Disconnected→Connecting→Connected→LoggingIn→Ready→Reconnecting→Error)
3. **XTPTrader**: 实现 `live.LiveTrader` 接口的完整 stub
4. **OfflineMode**: 默认 true，所有方法返回 ErrOffline；SDK 接入后设为 false
5. **A 股规则校验**: 数量必须 100 的倍数 (1 lot = 100 股)
6. **5 种 BusinessType**: Cash/Margin/Future/Option/HKStock
7. **EmergencyFlatten**: 复用 P2-3 的 EmergencyFlattenResult，bypassed_t1=true

## Consequences
- **Positive**: 接口层完整就绪，SDK 接入只需实现 cgoCall 方法
- **Positive**: 30 个测试覆盖所有 offline/not-connected/validation 路径
- **Positive**: 编译时接口检查 (`var _ live.LiveTrader = (*XTPTrader)(nil)`)
- **Negative**: 实际交易功能需要 SDK 链接后才能使用
- **Negative**: CGo 绑定增加构建复杂度

## Artifacts
- `pkg/live/broker/xtp/xtp.go` (新建, ~520 行)
- `pkg/live/broker/xtp/xtp_test.go` (新建, 30 TestXxx race-clean)

## Metrics
- 代码行数: ~520 行
- 测试数: 30 TestXxx (race-clean)
- 接口: live.LiveTrader (7 方法全部实现)
- 状态机: 7 状态 + atomic.Int32 线程安全

## Lessons Learned
- 接口层和实现层分离是处理私有 SDK 的最佳实践
- OfflineMode 模式允许系统在没有 SDK 的情况下编译和测试
- A 股 100 股最小交易单位应在接口层校验，减少无效请求
- ConnectionState 使用 atomic.Int32 避免锁竞争
