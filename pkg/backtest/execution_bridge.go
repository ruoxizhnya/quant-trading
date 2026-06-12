package backtest

// P1-17 (Sprint 6, ODR-013 CQ-001, ADR-020):
// ExecutionBridge — 引擎与 ExecutionService 之间的桥接组件。
//
// 该文件从 pkg/backtest/engine.go (1408 行 God Object) 中抽离出
// 以下职责：
//
//   - 持有一个可选的 ExecutionService (slippage / commission / 限价单)
//   - 暴露 Set / Get 接口供 Engine 与外部注入
//   - 把 slippage model 信息作为子组件自有状态管理
//
// Engine.SetExecutionService / Engine.GetExecutionService 保持向后
// 兼容 (per ADR-020 §2 — 6 个月 backward-compat shim 阶段)。
//
// 设计动机（与 LiveBridge 一致）：
//   - ExecutionService 是横切关注点（滑点模型/费率/限价撮合），
//     不应与回测编排逻辑混在一起
//   - 测试可以单独构造 ExecutionBridge + 假 ExecutionService 验证
//     "engine 调 bridge 调 service"的链路，无需启动完整 Engine
//   - 未来支持多 ExecutionService (e.g. 主备切换 / 路由) 时，
//     只需扩展 ExecutionBridge 内部

import (
	"sync"

	"github.com/rs/zerolog"
)

// ExecutionBridge 桥接 Engine 与 ExecutionService。
//
// ExecutionService 是 backtest 包内定义的接口 (见 execution.go)，
// 涵盖 order → trade 的执行流程（slippage、commission、限价撮合）。
// 该接口与 live.LiveTrader 不同：ExecutionService 作用于"回测上下文"
// （给定一根 bar 一笔委托，返回一笔成交），而 LiveTrader 作用于
// "实盘上下文"（异步、broker API）。
//
// 线程安全：mu (sync.RWMutex) 保护 service 字段的并发读写。
type ExecutionBridge struct {
	mu      sync.RWMutex
	service ExecutionService
	logger  zerolog.Logger
}

// NewExecutionBridge 构造一个未挂载 ExecutionService 的桥接器。
// Engine 启动时若未注入 service，会在 RunBacktest 阶段走 Tracker
// 内置执行逻辑（见 engine.go runBacktestInternal）。
func NewExecutionBridge(logger zerolog.Logger) *ExecutionBridge {
	return &ExecutionBridge{
		logger: logger.With().Str("component", "execution_bridge").Logger(),
	}
}

// Set 注入或替换 ExecutionService。传 nil 切回 Tracker 内置执行。
// 非 nil 注入会打 Info 日志（含 slippage_model）便于排障。
func (b *ExecutionBridge) Set(svc ExecutionService) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.service = svc
	if svc != nil {
		b.logger.Info().Str("slippage_model", svc.GetSlippageModel()).Msg("ExecutionService attached to bridge")
	} else {
		b.logger.Info().Msg("ExecutionService detached from bridge (fallback to Tracker execution)")
	}
}

// Get 返回当前挂载的 ExecutionService；若未设置则返回 nil。
// Engine 的 runBacktestInternal 在每次执行委托前调用此方法决定
// "走 service" 或 "走 Tracker" 路径。
func (b *ExecutionBridge) Get() ExecutionService {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.service
}

// GetSlippageModel 是常用便捷方法；Engine.GetExecutionService
// 旧 API 的等价物（保留以兼容）——避免外部代码在拿不到 ExecutionService
// 时多做一次 nil check。
//
// 返回值：
//   - service 未挂载 → ""
//   - service 挂载 → svc.GetSlippageModel()
func (b *ExecutionBridge) GetSlippageModel() string {
	svc := b.Get()
	if svc == nil {
		return ""
	}
	return svc.GetSlippageModel()
}
