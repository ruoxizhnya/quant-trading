package live

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ---------------------------------------------------------------------------
// P1-5 (ODR-018) — OrderManager 集成测试: 验证 SubmitOrder 走完 cage 校验
// 流程; 失败的限价单应被 OrderManager 直接拒绝, broker 不会收到。
// ---------------------------------------------------------------------------

// stubBroker 是一个 minimal broker, 用于测试 SubmitOrder 流程中的 cage 校验.
// 仅实现必要方法, 全部通过 (无网络).
type stubBroker struct {
	submitCalls int
}

func (s *stubBroker) Connect() error                                       { return nil }
func (s *stubBroker) Disconnect() error                                    { return nil }
func (s *stubBroker) SubmitOrder(_ domain.Order) (string, error)           { s.submitCalls++; return "BROKER-ID", nil }
func (s *stubBroker) CancelOrder(_ string) error                           { return nil }
func (s *stubBroker) GetOrderStatus(_ string) (string, error)              { return "submitted", nil }
func (s *stubBroker) GetPositions() ([]domain.Position, error)             { return nil, nil }
func (s *stubBroker) GetAccountBalance() (float64, error)                  { return 1e6, nil }

func newTestOrderManagerWithCage() (*OrderManager, *stubBroker) {
	broker := &stubBroker{}
	om := NewOrderManager(broker, domain.DefaultExecutionConfig())
	v := NewCageValidator()
	om.SetPriceCageValidator(v, func(sym string) ReferencePrice {
		// 600000.SH: prev close 10.00, ask 10.00, bid 9.99
		// cage 顶 10.20, 底 9.7902
		return ref(9.99, 10.00, 10.00, 10.00)
	})
	return om, broker
}

func TestOrderManager_SubmitOrder_AcceptsValidLimit(t *testing.T) {
	om, broker := newTestOrderManagerWithCage()
	order := domain.Order{
		Symbol:     "600000.SH",
		Direction:  domain.DirectionLong,
		OrderType:  domain.OrderTypeLimit,
		LimitPrice: 10.05, // within 9.7902-10.20 cage
		Quantity:   100,
	}
	id, err := om.SubmitOrder(order)
	require.NoError(t, err)
	assert.NotEmpty(t, id)
	assert.Equal(t, 1, broker.submitCalls, "broker should have been called once")
}

func TestOrderManager_SubmitOrder_RejectsCageViolation(t *testing.T) {
	om, broker := newTestOrderManagerWithCage()
	order := domain.Order{
		Symbol:     "600000.SH",
		Direction:  domain.DirectionLong,
		OrderType:  domain.OrderTypeLimit,
		LimitPrice: 10.30, // exceeds 10.20 cage ceiling
		Quantity:   100,
	}
	_, err := om.SubmitOrder(order)
	require.Error(t, err)
	var cage *PriceCageError
	require.True(t, errors.As(err, &cage))
	assert.Equal(t, "cage_violation", cage.Reason)
	assert.Equal(t, 0, broker.submitCalls, "broker should NOT have been called")
}

func TestOrderManager_SubmitOrder_RejectsLimitUpViolation(t *testing.T) {
	om, broker := newTestOrderManagerWithCage()
	order := domain.Order{
		Symbol:     "600000.SH",
		Direction:  domain.DirectionLong,
		OrderType:  domain.OrderTypeLimit,
		LimitPrice: 11.05, // exceeds 11.00 (10% limit up)
		Quantity:   100,
	}
	_, err := om.SubmitOrder(order)
	require.Error(t, err)
	var cage *PriceCageError
	require.True(t, errors.As(err, &cage))
	assert.Equal(t, "limit_up_violation", cage.Reason)
	assert.Equal(t, 0, broker.submitCalls, "broker should NOT have been called")
}

func TestOrderManager_SubmitOrder_AllowsAtLimitUp(t *testing.T) {
	om, broker := newTestOrderManagerWithCage()
	order := domain.Order{
		Symbol:     "600000.SH",
		Direction:  domain.DirectionLong,
		OrderType:  domain.OrderTypeLimit,
		LimitPrice: 11.00, // 涨停 — cage 规则豁免
		Quantity:   100,
	}
	_, err := om.SubmitOrder(order)
	require.NoError(t, err)
	assert.Equal(t, 1, broker.submitCalls)
}

func TestOrderManager_SubmitOrder_MarketOrderSkipsCage(t *testing.T) {
	om, broker := newTestOrderManagerWithCage()
	order := domain.Order{
		Symbol:    "600000.SH",
		Direction: domain.DirectionLong,
		OrderType: domain.OrderTypeMarket,
		Quantity:  100,
		// Market 单不走 cage (无价格可校验).
	}
	_, err := om.SubmitOrder(order)
	require.NoError(t, err)
	assert.Equal(t, 1, broker.submitCalls)
}

func TestOrderManager_SubmitOrder_NoCageWired_AllowsAnyLimit(t *testing.T) {
	// 没有 wire validator → 不做 cage 校验 (回测/老 broker 兼容).
	broker := &stubBroker{}
	om := NewOrderManager(broker, domain.DefaultExecutionConfig())
	order := domain.Order{
		Symbol:     "600000.SH",
		Direction:  domain.DirectionLong,
		OrderType:  domain.OrderTypeLimit,
		LimitPrice: 999.99, // 离谱高价, 但没有 cage 校验
		Quantity:   100,
	}
	_, err := om.SubmitOrder(order)
	require.NoError(t, err)
	assert.Equal(t, 1, broker.submitCalls)
}
