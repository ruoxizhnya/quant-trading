package execution

// P1-17 (Sprint 6, ODR-013 CQ-001, ADR-020):
// LiveBridge 独立单元测试 — 不依赖 Engine 全栈构造。
//
// 验证点：
//   1. Set / Get round-trip
//   2. ExecuteSignal trader=nil 时返回 (nil, nil)（兼容旧 API）
//   3. ExecuteSignal currentPrice<=0 报 invalid price 错
//   4. ExecuteSignal 市价单 / 限价单分别走 price=0 / price=LimitPrice
//   5. quantity scaling 与强度公式与旧实现 byte-equal
//   6. ExecuteSignals 批量跳过缺价信号
//   7. HealthCheck nil / healthy / unhealthy 三态

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
)

// fakeLiveTrader — 测试用 LiveTrader stub，只记录调用、不写 broker。
type fakeLiveTrader struct {
	name        string
	submitCalls []fakeSubmitCall
	submitErr   error
	healthErr   error

	// Optional hooks to override the returned OrderResult
	resultHook func(symbol string, dir domain.Direction, orderType domain.OrderType, qty, price float64) *live.OrderResult
}

type fakeSubmitCall struct {
	Symbol    string
	Direction domain.Direction
	OrderType domain.OrderType
	Quantity  float64
	Price     float64
}

func (f *fakeLiveTrader) SubmitOrder(ctx context.Context, symbol string, dir domain.Direction, orderType domain.OrderType, qty, price float64) (*live.OrderResult, error) {
	f.submitCalls = append(f.submitCalls, fakeSubmitCall{
		Symbol:    symbol,
		Direction: dir,
		OrderType: orderType,
		Quantity:  qty,
		Price:     price,
	})
	if f.submitErr != nil {
		return nil, f.submitErr
	}
	if f.resultHook != nil {
		return f.resultHook(symbol, dir, orderType, qty, price), nil
	}
	return &live.OrderResult{
		OrderID:   "TEST-" + symbol,
		Symbol:    symbol,
		Direction: dir,
		OrderType: orderType,
		Quantity:  qty,
		FilledQty: qty,
		Price:     price,
		Status:    "filled",
	}, nil
}

func (f *fakeLiveTrader) CancelOrder(ctx context.Context, orderID string) error { return nil }
func (f *fakeLiveTrader) GetOrder(ctx context.Context, orderID string) (*live.OrderResult, error) {
	return nil, nil
}
func (f *fakeLiveTrader) GetPositions(ctx context.Context) ([]live.PositionInfo, error) {
	return nil, nil
}
func (f *fakeLiveTrader) GetAccount(ctx context.Context) (*live.AccountInfo, error) {
	return nil, nil
}
func (f *fakeLiveTrader) Name() string                          { return f.name }
func (f *fakeLiveTrader) HealthCheck(ctx context.Context) error { return f.healthErr }
func (f *fakeLiveTrader) EmergencyFlatten(ctx context.Context, reason string) (*live.EmergencyFlattenResult, error) {
	// Tests don't exercise emergency flatten; return an empty
	// result so the interface stays satisfied.
	return &live.EmergencyFlattenResult{Reason: reason}, nil
}

// ----- Tests -----

func newTestLiveBridge() *LiveBridge {
	return NewLiveBridge(zerolog.New(nil))
}

func TestLiveBridge_SetGetRoundTrip(t *testing.T) {
	b := newTestLiveBridge()
	assert.Nil(t, b.Get(), "fresh bridge should have nil trader")

	trader := &fakeLiveTrader{name: "fake"}
	b.Set(trader)
	assert.Same(t, trader, b.Get(), "Get should return the trader we just Set")

	// Setting nil detaches
	b.Set(nil)
	assert.Nil(t, b.Get(), "Set(nil) should detach the trader")
}

func TestLiveBridge_ExecuteSignal_NilTraderReturnsNil(t *testing.T) {
	b := newTestLiveBridge()
	signal := domain.Signal{Symbol: "600000.SH", Direction: domain.DirectionLong}
	result, err := b.ExecuteSignal(context.Background(), signal, 10.0)
	require.NoError(t, err)
	assert.Nil(t, result, "nil trader must return (nil, nil) per backward compat contract")
}

func TestLiveBridge_ExecuteSignal_InvalidPrice(t *testing.T) {
	b := newTestLiveBridge()
	b.Set(&fakeLiveTrader{name: "fake"})

	signal := domain.Signal{Symbol: "600000.SH", Direction: domain.DirectionLong}
	for _, badPrice := range []float64{0, -0.01, -100} {
		result, err := b.ExecuteSignal(context.Background(), signal, badPrice)
		require.Error(t, err, "price=%.2f must be rejected", badPrice)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "invalid price")
	}
}

func TestLiveBridge_ExecuteSignal_MarketOrder(t *testing.T) {
	trader := &fakeLiveTrader{name: "fake"}
	b := newTestLiveBridge()
	b.Set(trader)

	signal := domain.Signal{
		Symbol:    "600000.SH",
		Direction: domain.DirectionLong,
		OrderType: domain.OrderTypeMarket,
		Strength:  0.0, // → quantity = 100 default
	}
	result, err := b.ExecuteSignal(context.Background(), signal, 12.5)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, trader.submitCalls, 1)
	call := trader.submitCalls[0]
	assert.Equal(t, "600000.SH", call.Symbol)
	assert.Equal(t, domain.DirectionLong, call.Direction)
	assert.Equal(t, domain.OrderTypeMarket, call.OrderType)
	assert.Equal(t, 0.0, call.Price, "market order must send price=0 to broker")
	assert.Equal(t, 100.0, call.Quantity, "strength=0 must use default lot of 100")
}

func TestLiveBridge_ExecuteSignal_LimitOrder(t *testing.T) {
	trader := &fakeLiveTrader{name: "fake"}
	b := newTestLiveBridge()
	b.Set(trader)

	signal := domain.Signal{
		Symbol:     "600000.SH",
		Direction:  domain.DirectionLong,
		OrderType:  domain.OrderTypeLimit,
		LimitPrice: 11.5,
		Strength:   0.5, // → quantity = 100 * max(1.0, 0.5*10) = 100 * 5.0 = 500
	}
	result, err := b.ExecuteSignal(context.Background(), signal, 12.0)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, trader.submitCalls, 1)
	call := trader.submitCalls[0]
	assert.Equal(t, domain.OrderTypeLimit, call.OrderType)
	assert.Equal(t, 11.5, call.Price, "limit order must use LimitPrice, not currentPrice")
	assert.Equal(t, 500.0, call.Quantity, "strength=0.5 must scale to 100*5.0=500 lots")
}

func TestLiveBridge_ExecuteSignal_LimitPriceFallback(t *testing.T) {
	trader := &fakeLiveTrader{name: "fake"}
	b := newTestLiveBridge()
	b.Set(trader)

	// Limit order with no explicit LimitPrice → fallback to currentPrice
	signal := domain.Signal{
		Symbol:    "600000.SH",
		Direction: domain.DirectionLong,
		OrderType: domain.OrderTypeLimit,
		// LimitPrice == 0
	}
	_, err := b.ExecuteSignal(context.Background(), signal, 9.5)
	require.NoError(t, err)
	require.Len(t, trader.submitCalls, 1)
	assert.Equal(t, 9.5, trader.submitCalls[0].Price, "missing LimitPrice should fallback to currentPrice")
}

func TestLiveBridge_ExecuteSignal_QuantityScalingCap(t *testing.T) {
	trader := &fakeLiveTrader{name: "fake"}
	b := newTestLiveBridge()
	b.Set(trader)

	// strength=200 → scaled = 200*10 = 2000 → quantity = 200000, but cap to 10000
	signal := domain.Signal{
		Symbol:    "600000.SH",
		Direction: domain.DirectionLong,
		Strength:  200.0,
	}
	_, err := b.ExecuteSignal(context.Background(), signal, 10.0)
	require.NoError(t, err)
	require.Len(t, trader.submitCalls, 1)
	assert.Equal(t, 10000.0, trader.submitCalls[0].Quantity, "quantity must be capped at 10000")
}

func TestLiveBridge_ExecuteSignal_SubmitError(t *testing.T) {
	trader := &fakeLiveTrader{
		name:      "fake",
		submitErr: errors.New("broker rejected"),
	}
	b := newTestLiveBridge()
	b.Set(trader)

	signal := domain.Signal{Symbol: "600000.SH", Direction: domain.DirectionLong}
	result, err := b.ExecuteSignal(context.Background(), signal, 10.0)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "broker rejected")
}

func TestLiveBridge_ExecuteSignals_BatchWithMissingPrices(t *testing.T) {
	trader := &fakeLiveTrader{name: "fake"}
	b := newTestLiveBridge()
	b.Set(trader)

	signals := []domain.Signal{
		{Symbol: "600000.SH", Direction: domain.DirectionLong},
		{Symbol: "600001.SH", Direction: domain.DirectionLong},
		{Symbol: "600002.SH", Direction: domain.DirectionLong}, // missing price → skip
		{Symbol: "600003.SH", Direction: domain.DirectionLong}, // price=0 → skip
	}
	prices := map[string]float64{
		"600000.SH": 10.0,
		"600001.SH": 20.0,
		"600003.SH": 0.0,
		// 600002.SH absent
	}

	results := b.ExecuteSignals(context.Background(), signals, prices)
	require.NotNil(t, results)
	assert.Len(t, results, 2, "only 2 signals have valid prices; should skip the rest")
	assert.Contains(t, results, "600000.SH")
	assert.Contains(t, results, "600001.SH")
	assert.NotContains(t, results, "600002.SH")
	assert.NotContains(t, results, "600003.SH")
}

func TestLiveBridge_ExecuteSignals_NilTraderReturnsNil(t *testing.T) {
	b := newTestLiveBridge()
	signals := []domain.Signal{{Symbol: "600000.SH", Direction: domain.DirectionLong}}
	results := b.ExecuteSignals(context.Background(), signals, map[string]float64{"600000.SH": 10.0})
	assert.Nil(t, results, "nil trader must return nil map per backward compat")
}

func TestLiveBridge_HealthCheck_NilTrader(t *testing.T) {
	b := newTestLiveBridge()
	err := b.HealthCheck(context.Background())
	assert.NoError(t, err, "nil trader should be treated as 'no health check needed'")
}

func TestLiveBridge_HealthCheck_DelegatesToTrader(t *testing.T) {
	trader := &fakeLiveTrader{name: "fake", healthErr: errors.New("broker disconnected")}
	b := newTestLiveBridge()
	b.Set(trader)

	err := b.HealthCheck(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broker disconnected")
}
