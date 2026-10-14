// Package marketdata 是规范层数据取用的**唯一入口**（透明路由）。
//
// 定位（ADR-031 蓝图 §4）：全系统数据取用只认一套规范层 schema
// （pkg/domain/market + pkg/domain），connector 之外无人感知底层表。
// 调用方向 Router 声明「要什么频率的 bar」，物理表在哪、15m 从哪来，
// 是这里的事。
//
// 路由口径（docs/SPEC.md 规范层 Schema 节是文档真相，本包是代码真相）：
//
//	1d  → ohlcv_daily_qfq（qfq 权威源，唯一进回测的口径）
//	1m  → ohlcv_minute
//	15m → ohlcv_minute 聚合产出（不物化；口径见 aggregate.go）
//	其他 → fail-loud 报错，不静默回退
//
// 1m→1d 聚合口径同样成文（备 D6/双核对拍），但 1d 路由**不**走分钟聚合：
// 日线表是前复权权威源，分钟表复权口径不同，混用会破坏「现有日线回测
// 数值不变」的回归红线。
//
// 物理表名只允许出现在本包 + internal/repoguard 基线登记的遗留文件：
// 执法者是 canonical_layer_test.go（新文件直连这两张表 = 测试红）。
// 「不许绕过规范层」因此有执法者，不是文档里的一句话。
package marketdata
