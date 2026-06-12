package live

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ---------------------------------------------------------------------------
// P1-3 (ODR-016) — LiveEngine 限价单单元测试
//
// 覆盖范围:
//   - Market  市价单 立即成交
//   - Limit   限价单 价格穿越撮合
//   - Stop    止损单 触发后市价成交
//   - Trailing 跟踪止损 HWM 维护 + 回撤触发
//
// 测试策略: 直接调用 tryFillOrder (包内可见) 与 handleQuote, 不启动
// 真实 goroutine. 内部状态通过 OrderManager.orders / pending 直接断言.
// ---------------------------------------------------------------------------

// newTestEngine 构造一个无 broker / 无 data feed 的 LiveEngine 用于纯撮合逻辑测试.
// 注入 nil 是因为 tryFillOrder / shouldFill / computeFillPrice 完全不依赖 broker.
func newTestEngine() *LiveEngine {
	return NewLiveEngine(nil, nil, domain.ExecutionConfig{
		OrderType:      domain.OrderTypeMarket,
		CommissionRate: 0.00025,
		MinCommission:  5.0,
		InitialCapital: 1000000,
	})
}

// seedPending 直接将 order 写入 OrderManager 的内部 map, 模拟 SubmitOrder 后
// 尚未成交的中间状态. 绕过 Broker 调用, 使测试聚焦于撮合语义.
func seedPending(t *testing.T, e *LiveEngine, order domain.Order) domain.Order {
	t.Helper()
	om := e.orderManager
	om.mu.Lock()
	defer om.mu.Unlock()
	if order.ID == "" {
		order.ID = "TEST-" + order.Symbol + "-" + string(order.OrderType)
	}
	if order.Status == "" {
		order.Status = "submitted"
	}
	om.orders[order.ID] = order
	om.pending = append(om.pending, order.ID)
	return order
}

func mkQuote(symbol string, bid, ask float64) Quote {
	return Quote{
		Symbol:    symbol,
		Timestamp: time.Now(),
		Open:      ask,
		High:      ask,
		Low:      bid,
		Close:     (bid + ask) / 2,
		Bid:       bid,
		Ask:       ask,
		Volume:    1000,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Market
// ─────────────────────────────────────────────────────────────────────────────

func TestTryFillOrder_Market_BuyFillsAtAsk(t *testing.T) {
	e := newTestEngine()
	var tradeMu sync.Mutex
	var trades []domain.Trade
	e.onTrade = func(tr domain.Trade) {
		tradeMu.Lock()
		defer tradeMu.Unlock()
		trades = append(trades, tr)
	}

	order := seedPending(t, e, domain.Order{
		Symbol:    "600000.SH",
		Direction: domain.DirectionLong,
		OrderType: domain.OrderTypeMarket,
		Quantity:  100,
	})

	e.tryFillOrder(&order, mkQuote("600000.SH", 10.0, 10.1))

	tradeMu.Lock()
	defer tradeMu.Unlock()
	require.Len(t, trades, 1)
	assert.Equal(t, 10.1, trades[0].Price, "market buy must fill at ask")

	// 状态写入 manager 侧副本, 从 manager 读取验证.
	stored, ok := e.orderManager.GetOrder(order.ID)
	require.True(t, ok)
	assert.Equal(t, "filled", stored.Status)
}

// ─────────────────────────────────────────────────────────────────────────────
// Limit
// ─────────────────────────────────────────────────────────────────────────────

func TestTryFillOrder_Limit_BuyFillsWhenAskAtOrBelowLimit(t *testing.T) {
	e := newTestEngine()
	var trades int
	e.onTrade = func(domain.Trade) { trades++ }

	order := seedPending(t, e, domain.Order{
		Symbol:     "600000.SH",
		Direction:  domain.DirectionLong,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   100,
		LimitPrice: 10.0,
	})

	// Ask > limit → 不成交
	e.tryFillOrder(&order, mkQuote("600000.SH", 9.9, 10.05))
	assert.Equal(t, 0, trades, "ask above limit must not fill")

	// Ask == limit → 成交 (按 limit price)
	e.tryFillOrder(&order, mkQuote("600000.SH", 9.9, 10.0))
	assert.Equal(t, 1, trades, "ask at limit must fill")
}

func TestTryFillOrder_Limit_SellFillsWhenBidAtOrAboveLimit(t *testing.T) {
	e := newTestEngine()
	var trades int
	e.onTrade = func(domain.Trade) { trades++ }

	order := seedPending(t, e, domain.Order{
		Symbol:     "600000.SH",
		Direction:  domain.DirectionClose,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   100,
		LimitPrice: 11.0,
	})

	// Bid < limit → 不成交
	e.tryFillOrder(&order, mkQuote("600000.SH", 10.9, 11.1))
	assert.Equal(t, 0, trades)

	// Bid == limit → 成交
	e.tryFillOrder(&order, mkQuote("600000.SH", 11.0, 11.1))
	assert.Equal(t, 1, trades, "bid at limit must fill")
}

func TestTryFillOrder_Limit_FillPriceIsLimitNotAsk(t *testing.T) {
	e := newTestEngine()
	var captured domain.Trade
	var once sync.Once
	e.onTrade = func(tr domain.Trade) { once.Do(func() { captured = tr }) }

	order := seedPending(t, e, domain.Order{
		Symbol:     "600000.SH",
		Direction:  domain.DirectionLong,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   100,
		LimitPrice: 10.0,
	})

	// Ask 9.8, Limit 10.0 — 限价买单应按 Ask 成交 (价格更优)
	e.tryFillOrder(&order, mkQuote("600000.SH", 9.7, 9.8))
	assert.Equal(t, 9.8, captured.Price, "buy limit fills at ask when ask is better")
}

// ─────────────────────────────────────────────────────────────────────────────
// Stop
// ─────────────────────────────────────────────────────────────────────────────

func TestTryFillOrder_Stop_BuyTriggersOnAskBreakout(t *testing.T) {
	e := newTestEngine()
	var trades int
	e.onTrade = func(domain.Trade) { trades++ }

	order := seedPending(t, e, domain.Order{
		Symbol:     "600000.SH",
		Direction:  domain.DirectionLong,
		OrderType:  domain.OrderTypeStop,
		Quantity:   100,
		StopPrice:  10.0,
	})

	e.tryFillOrder(&order, mkQuote("600000.SH", 9.7, 9.8)) // 不触发
	assert.Equal(t, 0, trades)

	e.tryFillOrder(&order, mkQuote("600000.SH", 10.0, 10.1)) // Ask == Stop 触发
	assert.Equal(t, 1, trades)
}

func TestTryFillOrder_Stop_SellTriggersOnBidBreakdown(t *testing.T) {
	e := newTestEngine()
	var trades int
	e.onTrade = func(domain.Trade) { trades++ }

	order := seedPending(t, e, domain.Order{
		Symbol:     "600000.SH",
		Direction:  domain.DirectionClose,
		OrderType:  domain.OrderTypeStop,
		Quantity:   100,
		StopPrice:  10.0,
	})

	e.tryFillOrder(&order, mkQuote("600000.SH", 10.1, 10.3))
	assert.Equal(t, 0, trades)

	e.tryFillOrder(&order, mkQuote("600000.SH", 9.9, 10.0)) // Bid <= Stop 触发
	assert.Equal(t, 1, trades)
}

func TestTryFillOrder_Stop_FillPriceIsMarket(t *testing.T) {
	// 触发后, Stop 单转为市价单, 买入按 Ask, 卖出按 Bid.
	e := newTestEngine()
	var captured domain.Trade
	var once sync.Once
	e.onTrade = func(tr domain.Trade) { once.Do(func() { captured = tr }) }

	order := seedPending(t, e, domain.Order{
		Symbol:     "600000.SH",
		Direction:  domain.DirectionClose,
		OrderType:  domain.OrderTypeStop,
		Quantity:   100,
		StopPrice:  10.0,
	})
	e.tryFillOrder(&order, mkQuote("600000.SH", 9.8, 9.9))
	assert.Equal(t, 9.8, captured.Price, "stop sell converts to market sell at bid")
}

// ─────────────────────────────────────────────────────────────────────────────
// Trailing
// ─────────────────────────────────────────────────────────────────────────────

func TestTryFillOrder_Trailing_HWMRisesMonotonically(t *testing.T) {
	e := newTestEngine()

	order := seedPending(t, e, domain.Order{
		Symbol:         "600000.SH",
		Direction:      domain.DirectionClose,
		OrderType:      domain.OrderTypeTrailing,
		Quantity:       100,
		TrailAmount:    0.5,
		HighWaterMark:  0,
	})

	// 第一笔 quote: 推动 HWM 上升
	e.tryFillOrder(&order, mkQuote("600000.SH", 10.0, 10.1))
	assert.InDelta(t, 10.1, order.HighWaterMark, 1e-9, "HWM must follow the ask")

	// 更高的 quote: HWM 继续上升
	e.tryFillOrder(&order, mkQuote("600000.SH", 10.5, 10.6))
	assert.InDelta(t, 10.6, order.HighWaterMark, 1e-9)

	// 更低的 quote: HWM 不下降
	e.tryFillOrder(&order, mkQuote("600000.SH", 9.5, 9.6))
	assert.InDelta(t, 10.6, order.HighWaterMark, 1e-9, "HWM must be monotone-up")
}

func TestTryFillOrder_Trailing_TriggersOnPullback(t *testing.T) {
	e := newTestEngine()
	var trades int
	e.onTrade = func(domain.Trade) { trades++ }

	order := seedPending(t, e, domain.Order{
		Symbol:        "600000.SH",
		Direction:     domain.DirectionClose,
		OrderType:     domain.OrderTypeTrailing,
		Quantity:      100,
		TrailAmount:   0.5,
		HighWaterMark: 0,
	})

	// 价格上行, HWM 升至 10.6
	e.tryFillOrder(&order, mkQuote("600000.SH", 10.0, 10.1))
	e.tryFillOrder(&order, mkQuote("600000.SH", 10.5, 10.6))

	// 触发价 = 10.6 - 0.5 = 10.1; Bid 10.2 > 10.1 → 不触发
	e.tryFillOrder(&order, mkQuote("600000.SH", 10.2, 10.4))
	assert.Equal(t, 0, trades)

	// Bid 10.0 <= 10.1 → 触发
	e.tryFillOrder(&order, mkQuote("600000.SH", 10.0, 10.05))
	assert.Equal(t, 1, trades, "trailing stop must trigger on pullback to trigger price")
}

func TestTryFillOrder_Trailing_TriggerPriceUsesHWMNotQuote(t *testing.T) {
	// 关键不变量: 触发价由 HWM 决定, 与当前 quote 无关.
	// HWM 10.6, trail 0.5 → 触发价恒为 10.1.
	e := newTestEngine()

	order := seedPending(t, e, domain.Order{
		Symbol:        "600000.SH",
		Direction:     domain.DirectionClose,
		OrderType:     domain.OrderTypeTrailing,
		Quantity:      100,
		TrailAmount:   0.5,
		HighWaterMark: 10.6,
	})

	assert.InDelta(t, 10.1, e.trailingTriggerPrice(&order), 1e-9)
}

func TestTryFillOrder_Trailing_TrailPercentFallback(t *testing.T) {
	e := newTestEngine()
	var trades int
	e.onTrade = func(domain.Trade) { trades++ }

	// TrailAmount=0, TrailPercent=0.05 (5%), HWM 预置 100 → 触发价 95
	order := seedPending(t, e, domain.Order{
		Symbol:        "600000.SH",
		Direction:     domain.DirectionClose,
		OrderType:     domain.OrderTypeTrailing,
		Quantity:      100,
		TrailPercent:  0.05,
		HighWaterMark: 100,
	})

	e.tryFillOrder(&order, mkQuote("600000.SH", 96.0, 97.0)) // Bid 96 > 95 → 不触发
	assert.Equal(t, 0, trades)
	e.tryFillOrder(&order, mkQuote("600000.SH", 94.5, 95.0)) // Bid 94.5 <= 95 → 触发
	assert.Equal(t, 1, trades)
}

func TestTryFillOrder_Trailing_AmountWinsOverPercent(t *testing.T) {
	e := newTestEngine()

	order := seedPending(t, e, domain.Order{
		Symbol:        "600000.SH",
		Direction:     domain.DirectionClose,
		OrderType:     domain.OrderTypeTrailing,
		Quantity:      100,
		TrailAmount:   2.0,                // 期望触发价 100 - 2 = 98
		TrailPercent:  0.5,                // 50% (应被忽略)
		HighWaterMark: 100,
	})
	assert.InDelta(t, 98.0, e.trailingTriggerPrice(&order), 1e-9,
		"TrailAmount must take precedence over TrailPercent")
}

func TestTryFillOrder_Trailing_NoHWMYetNeverFills(t *testing.T) {
	// HWM 仍为 0 时, 跟踪止损不应触发 (无参考价).
	e := newTestEngine()
	var trades int
	e.onTrade = func(domain.Trade) { trades++ }

	order := seedPending(t, e, domain.Order{
		Symbol:        "600000.SH",
		Direction:     domain.DirectionClose,
		OrderType:     domain.OrderTypeTrailing,
		Quantity:      100,
		TrailAmount:   0.5,
		HighWaterMark: 0,
	})

	e.tryFillOrder(&order, mkQuote("600000.SH", 1.0, 1.1))
	assert.Equal(t, 0, trades, "trailing without HWM must never fill")
}

// ─────────────────────────────────────────────────────────────────────────────
// 边界 / 防御
// ─────────────────────────────────────────────────────────────────────────────

func TestTryFillOrder_LimitZero_DoesNotSilentlyFill(t *testing.T) {
	// LimitPrice == 0 视为配置错误, 不应被静默当作市价单成交.
	e := newTestEngine()
	var trades int
	e.onTrade = func(domain.Trade) { trades++ }

	order := seedPending(t, e, domain.Order{
		Symbol:     "600000.SH",
		Direction:  domain.DirectionLong,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   100,
		LimitPrice: 0,
	})
	e.tryFillOrder(&order, mkQuote("600000.SH", 9.9, 10.0))
	assert.Equal(t, 0, trades, "limit order with zero price must not fill")
}

func TestTryFillOrder_StopZero_DoesNotTrigger(t *testing.T) {
	e := newTestEngine()
	var trades int
	e.onTrade = func(domain.Trade) { trades++ }

	order := seedPending(t, e, domain.Order{
		Symbol:    "600000.SH",
		Direction: domain.DirectionLong,
		OrderType: domain.OrderTypeStop,
		Quantity:  100,
	})
	e.tryFillOrder(&order, mkQuote("600000.SH", 1.0, 999.0))
	assert.Equal(t, 0, trades, "stop with zero stop price must never trigger")
}

func TestTryFillOrder_NilOrder_NoPanic(t *testing.T) {
	e := newTestEngine()
	assert.NotPanics(t, func() { e.tryFillOrder(nil, mkQuote("X", 1, 1)) })
}

func TestUpdateTrailingHWM_PersistsAcrossManager(t *testing.T) {
	// 验证: trailing 单 HWM 变化后, OrderManager 中的快照能看到新 HWM.
	e := newTestEngine()

	order := seedPending(t, e, domain.Order{
		Symbol:        "600000.SH",
		Direction:     domain.DirectionClose,
		OrderType:     domain.OrderTypeTrailing,
		Quantity:      100,
		TrailAmount:   0.5,
		HighWaterMark: 0,
	})

	e.tryFillOrder(&order, mkQuote("600000.SH", 10.0, 10.7))

	stored, ok := e.orderManager.GetOrder(order.ID)
	require.True(t, ok)
	assert.InDelta(t, 10.7, stored.HighWaterMark, 1e-9,
		"manager-side snapshot must reflect HWM update")
}

// ─────────────────────────────────────────────────────────────────────────────
// validateOrderShape
// ─────────────────────────────────────────────────────────────────────────────

func TestValidateOrderShape(t *testing.T) {
	cases := []struct {
		name    string
		order   domain.Order
		wantErr string // substring; empty = expect no error
	}{
		{
			name:    "market order needs no extras",
			order:   domain.Order{Symbol: "X", Quantity: 1, OrderType: domain.OrderTypeMarket},
			wantErr: "",
		},
		{
			name:    "limit needs price",
			order:   domain.Order{Symbol: "X", Quantity: 1, OrderType: domain.OrderTypeLimit},
			wantErr: "LimitPrice",
		},
		{
			name:    "limit with price ok",
			order:   domain.Order{Symbol: "X", Quantity: 1, OrderType: domain.OrderTypeLimit, LimitPrice: 10},
			wantErr: "",
		},
		{
			name:    "stop needs stop price",
			order:   domain.Order{Symbol: "X", Quantity: 1, OrderType: domain.OrderTypeStop},
			wantErr: "StopPrice",
		},
		{
			name:    "trailing needs offset",
			order:   domain.Order{Symbol: "X", Quantity: 1, OrderType: domain.OrderTypeTrailing},
			wantErr: "TrailAmount or TrailPercent",
		},
		{
			name:    "trailing with percent ok",
			order:   domain.Order{Symbol: "X", Quantity: 1, OrderType: domain.OrderTypeTrailing, TrailPercent: 0.05},
			wantErr: "",
		},
		{
			name:    "missing symbol",
			order:   domain.Order{Quantity: 1, OrderType: domain.OrderTypeMarket},
			wantErr: "symbol is required",
		},
		{
			name:    "zero quantity",
			order:   domain.Order{Symbol: "X", Quantity: 0, OrderType: domain.OrderTypeMarket},
			wantErr: "quantity must be positive",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateOrderShape(&tc.order)
			if tc.wantErr == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			}
		})
	}
}

func TestValidateOrderShape_NilOrder(t *testing.T) {
	assert.Error(t, validateOrderShape(nil))
}
