package backtest

// S7-P2-1: fakeLiveTrader and fakeExecutionService test stubs
// extracted from execution/live_bridge_test.go and
// execution/execution_bridge_test.go when ExecutionBridge + LiveBridge
// moved to pkg/backtest/execution/. The execution/ subpackage keeps
// its own copies (Go test files are package-scoped). This copy serves
// the parent-package tests — notably options_test.go's
// NewEngineWithOptions tests that inject WithLiveTrader /
// WithExecutionService.

import (
	"context"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
)

// ----- fakeLiveTrader -----

type fakeSubmitCall struct {
	Symbol    string
	Direction domain.Direction
	OrderType domain.OrderType
	Quantity  float64
	Price     float64
}

// fakeLiveTrader — test LiveTrader stub that records submit calls but
// does not write to a real broker.
type fakeLiveTrader struct {
	name        string
	submitCalls []fakeSubmitCall
	submitErr   error
	healthErr   error

	// Optional hook to override the returned OrderResult
	resultHook func(symbol string, dir domain.Direction, orderType domain.OrderType, qty, price float64) *live.OrderResult
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
	return &live.EmergencyFlattenResult{Reason: reason}, nil
}

// Compile-time check
var _ live.LiveTrader = (*fakeLiveTrader)(nil)

// ----- fakeExecutionService -----

// fakeExecutionService — test ExecutionService stub that only exposes
// the slippage model (no real order execution).
type fakeExecutionService struct {
	slippage string
}

func (s *fakeExecutionService) ExecuteOrder(order domain.Order, quote Quote) (domain.Trade, error) {
	return domain.Trade{}, nil
}
func (s *fakeExecutionService) GetSlippageModel() string      { return s.slippage }
func (s *fakeExecutionService) SetSlippageModel(model string) { s.slippage = model }

// Compile-time check
var _ ExecutionService = (*fakeExecutionService)(nil)
