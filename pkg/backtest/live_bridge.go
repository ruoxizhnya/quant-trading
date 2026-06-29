package backtest

// P1-17 (Sprint 6, ODR-013 CQ-001, ADR-020):
// LiveBridge — 引擎与 live.LiveTrader 之间的桥接组件。
//
// 该文件从 pkg/backtest/engine.go (1408 行 God Object) 中抽离出
// 以下职责：
//
//   - 持有一个可选的 live.LiveTrader (paper / 真实券商)
//   - 通过 ExecuteSignal / ExecuteSignals 转发策略信号到 broker
//   - 暴露 HealthCheck 供运维 / `/health` 端点调用
//
// 引擎层通过 Engine.SetLiveTrader / Engine.ExecuteSignalViaLiveTrader
// 等方法保持向后兼容 (per ADR-020 §2 — 6 个月 backward-compat shim
// 阶段)；所有 shim 仅做"取 bridge → 调 method"的薄封装。
//
// 旧问题（已修复）：
//   - 业务逻辑、并发控制、logging 全部耦合在 Engine 上
//   - 测试需要构造完整 Engine 才能覆盖 LiveTrader 行为
//   - 添加新 broker 适配或新执行模式 (e.g. 拆单、TWAP) 时需修改 Engine
//
// 新设计（LiveBridge）：
//   - LiveBridge 拥有自己的 sync.RWMutex，不再受 Engine 主锁影响
//   - 可独立单测：构造一个 fake LiveTrader 即可
//   - 未来若引入多账户 / 多 broker 路由，只需扩展 LiveBridge 内部
//     数据结构，Engine 接口不变
//
// 排序/数值/类型决策：详见文件顶部 ADR-020 引用 + 函数 doc。

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
)

// LiveBridge 桥接 Engine 与 live.LiveTrader。
//
// 它本身**不**持策略或信号生成逻辑——只负责"信号 → broker 委托"这一步。
// 这种单一职责使 LiveBridge 易于 mock、易于在新场景中替换（例如
// paper 模式 → 实盘模式），也方便未来加入高级 broker 行为（拆单、
// TWAP、VWAP、Iceberg 等）。
//
// 线程安全：所有访问通过 mu (sync.RWMutex) 保护。Get / Set 是
// O(1) RLock；Execute* 在调用方 trader 上做并发控制（LiveTrader
// 实现需自行保证并发安全）。
type LiveBridge struct {
	mu     sync.RWMutex
	trader live.LiveTrader
	logger zerolog.Logger
}

// NewLiveBridge 构造一个未附加 LiveTrader 的桥接器。
// 调用方应随后通过 Set() 注入 trader，或保持 nil 以禁用 live 路径。
func NewLiveBridge(logger zerolog.Logger) *LiveBridge {
	return &LiveBridge{
		logger: logger.With().Str("component", "live_bridge").Logger(),
	}
}

// Set 注入或替换 LiveTrader。传 nil 禁用 live 路径（回到纯模拟）。
// 注意：注入非 nil 时记录一条 Info 日志，便于运维确认"已挂载真实 broker"。
func (b *LiveBridge) Set(trader live.LiveTrader) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.trader = trader
	if trader != nil {
		b.logger.Info().Str("trader", trader.Name()).Msg("LiveTrader attached to bridge")
	} else {
		b.logger.Info().Msg("LiveTrader detached from bridge (live path disabled)")
	}
}

// Get 返回当前挂载的 LiveTrader；若未设置则返回 nil。
// 调用方负责 nil 检查（通常意味着"走纯模拟"分支）。
func (b *LiveBridge) Get() live.LiveTrader {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.trader
}

// ExecuteSignal 通过已挂载的 LiveTrader 执行单个信号。
//
// 行为契约：
//   - trader 为 nil 时返回 (nil, nil)，与旧 Engine.ExecuteSignalViaLiveTrader
//     保持完全一致——这是 engine → bridge 迁移的兼容点
//   - currentPrice <= 0 时返回 error，避免向 broker 提交无意义订单
//   - 根据 signal.OrderType 自动选择市价/限价逻辑：
//   - market:  price = 0
//   - limit:   price = signal.LimitPrice (fallback: currentPrice)
//   - quantity 由 signal.Strength 推导，旧实现使用 `100.0 * max(1, strength*10)`
//     公式（封顶 10000）——保留以保持行为完全一致
//
// 返回 (result, nil) 表示下单成功（broker 接受订单）；
// 返回 (nil, err) 表示下单失败（broker 拒绝、网络错误等）。
func (b *LiveBridge) ExecuteSignal(ctx context.Context, signal domain.Signal, currentPrice float64) (*live.OrderResult, error) {
	trader := b.Get()
	if trader == nil {
		return nil, nil
	}

	if currentPrice <= 0 {
		return nil, fmt.Errorf("cannot execute signal: invalid price %.4f for %s", currentPrice, signal.Symbol)
	}

	// Determine order type from signal
	orderType := signal.OrderType
	if orderType == "" {
		orderType = domain.OrderTypeMarket
	}

	// Determine price: limit price for limit orders, 0 for market orders
	price := 0.0
	if orderType == domain.OrderTypeLimit {
		price = signal.LimitPrice
		if price <= 0 {
			price = currentPrice
		}
	}

	// Calculate quantity from position sizing or default to a fixed lot.
	// 旧实现 (engine.go:1369) 沿用 max builtin (Go 1.21+)；此处直接写出。
	quantity := 100.0 // default lot size; in production this comes from risk/position sizing
	if signal.Strength > 0 {
		scaled := 1.0
		if signal.Strength*10 > 1.0 {
			scaled = signal.Strength * 10
		}
		quantity = 100.0 * scaled
		if quantity > 10000 {
			quantity = 10000
		}
	}

	result, err := trader.SubmitOrder(ctx, signal.Symbol, signal.Direction, orderType, quantity, price)
	if err != nil {
		b.logger.Warn().
			Str("symbol", signal.Symbol).
			Str("direction", string(signal.Direction)).
			Float64("price", currentPrice).
			Err(err).
			Msg("LiveTrader order submission failed")
		return nil, err
	}

	b.logger.Info().
		Str("order_id", result.OrderID).
		Str("symbol", signal.Symbol).
		Str("direction", string(signal.Direction)).
		Str("status", result.Status).
		Float64("qty", result.FilledQty).
		Float64("price", result.Price).
		Msg("Signal executed via LiveTrader")

	return result, nil
}

// ExecuteSignals 批量执行多个信号（典型用途：每日 rebalance）。
//
// 行为契约：
//   - trader 为 nil 时返回 nil（与 ExecuteSignal 一致）
//   - 单个信号失败不中断后续信号；失败的信号不出现在结果 map 中
//   - 价格缺失或 <= 0 的信号会被跳过并打 Warn 日志
//
// 返回 symbol → OrderResult 映射，仅包含成功执行的信号。
func (b *LiveBridge) ExecuteSignals(ctx context.Context, signals []domain.Signal, prices map[string]float64) map[string]*live.OrderResult {
	trader := b.Get()
	if trader == nil {
		return nil
	}

	results := make(map[string]*live.OrderResult, len(signals))
	for _, signal := range signals {
		price, ok := prices[signal.Symbol]
		if !ok || price <= 0 {
			b.logger.Warn().Str("symbol", signal.Symbol).Msg("No price available for live execution, skipping")
			continue
		}

		result, err := b.ExecuteSignal(ctx, signal, price)
		if err != nil {
			continue
		}
		if result != nil {
			results[signal.Symbol] = result
		}
	}

	b.logger.Info().
		Int("signals", len(signals)).
		Int("executed", len(results)).
		Msg("Batch signal execution via LiveTrader completed")

	return results
}

// HealthCheck 检查挂载的 LiveTrader 健康状态。
// 返回 nil 表示"无 trader 挂载"或"trader 健康"，与旧 Engine 实现一致。
func (b *LiveBridge) HealthCheck(ctx context.Context) error {
	trader := b.Get()
	if trader == nil {
		return nil
	}
	return trader.HealthCheck(ctx)
}
