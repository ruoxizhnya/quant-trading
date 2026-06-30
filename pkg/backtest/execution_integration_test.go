package backtest

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/fees"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEngine_ExecutionServiceIntegration tests that Engine correctly uses ExecutionService
func TestEngine_ExecutionServiceIntegration(t *testing.T) {
	logger := zerolog.New(nil)

	// Create tracker
	tracker := NewTracker(
		100000,
		fees.DefaultCommissionRate,
		0.001,
		defaultTradingConfig(),
		logger,
	)

	// Create execution service with fixed slippage
	config := domain.ExecutionConfig{
		OrderType:      domain.OrderTypeMarket,
		SlippageModel:  "fixed",
		CommissionRate: fees.DefaultCommissionRate,
		MinCommission:  5.0,
		InitialCapital: 100000,
	}
	execSvc := NewBacktestExecutionService(config)

	// Test 1: Execute long trade via ExecutionService
	t.Run("long_trade_with_fixed_slippage", func(t *testing.T) {
		order := domain.Order{
			Symbol:    "000001.SZ",
			Direction: domain.DirectionLong,
			OrderType: domain.OrderTypeMarket,
			Quantity:  100,
			Timestamp: time.Now(),
		}

		quote := Quote{
			Symbol: "000001.SZ",
			Open:   10.0,
			High:   11.0,
			Low:    9.0,
			Close:  10.0,
			Volume: 10000,
			Date:   time.Now(),
		}

		trade, err := execSvc.ExecuteOrder(order, quote)
		require.NoError(t, err)
		assert.Equal(t, "000001.SZ", trade.Symbol)
		assert.Equal(t, domain.DirectionLong, trade.Direction)
		assert.Equal(t, 100.0, trade.Quantity)
		// Fixed slippage: 10.0 * 1.001 = 10.01
		assert.InDelta(t, 10.01, trade.Price, 0.001)
		assert.True(t, trade.Commission > 0)

		// Apply to tracker
		applied, err := tracker.ApplyTrade(trade)
		require.NoError(t, err)
		assert.Equal(t, trade.Price, applied.Price)
		assert.Equal(t, trade.Quantity, applied.Quantity)

		// Verify position
		pos, exists := tracker.GetPosition("000001.SZ")
		require.True(t, exists)
		assert.Equal(t, 100.0, pos.Quantity)
	})

	// Test 2: Execute short trade via ExecutionService
	t.Run("short_trade_with_fixed_slippage", func(t *testing.T) {
		tracker.Reset(100000)

		order := domain.Order{
			Symbol:    "000001.SZ",
			Direction: domain.DirectionShort,
			OrderType: domain.OrderTypeMarket,
			Quantity:  100,
			Timestamp: time.Now(),
		}

		quote := Quote{
			Symbol: "000001.SZ",
			Open:   10.0,
			High:   11.0,
			Low:    9.0,
			Close:  10.0,
			Volume: 10000,
			Date:   time.Now(),
		}

		trade, err := execSvc.ExecuteOrder(order, quote)
		require.NoError(t, err)
		// Fixed slippage for sell: 10.0 * 0.999 = 9.99
		assert.InDelta(t, 9.99, trade.Price, 0.001)

		applied, err := tracker.ApplyTrade(trade)
		require.NoError(t, err)
		assert.Equal(t, 100.0, applied.Quantity)

		// Verify short position
		pos, exists := tracker.GetPosition("000001.SZ")
		require.True(t, exists)
		assert.Equal(t, -100.0, pos.Quantity)
	})

	// Test 3: Variable slippage model
	t.Run("variable_slippage_model", func(t *testing.T) {
		tracker.Reset(100000)
		execSvc.SetSlippageModel("variable")

		order := domain.Order{
			Symbol:    "000001.SZ",
			Direction: domain.DirectionLong,
			OrderType: domain.OrderTypeMarket,
			Quantity:  100,
			Timestamp: time.Now(),
		}

		quote := Quote{
			Symbol: "000001.SZ",
			Open:   10.0,
			High:   12.0,
			Low:    8.0,
			Close:  10.0,
			Volume: 10000,
			Date:   time.Now(),
		}

		trade, err := execSvc.ExecuteOrder(order, quote)
		require.NoError(t, err)
		// Volatility = (12-8)/10 = 0.4, slippage = 0.4 * 0.1 = 0.04
		// Price = 10.0 * (1 + 0.04) = 10.4
		assert.InDelta(t, 10.4, trade.Price, 0.1)

		_, err = tracker.ApplyTrade(trade)
		require.NoError(t, err)
	})

	// Test 4: Limit order execution
	t.Run("limit_order_execution", func(t *testing.T) {
		tracker.Reset(100000)
		execSvc.SetSlippageModel("none")

		order := domain.Order{
			Symbol:     "000001.SZ",
			Direction:  domain.DirectionLong,
			OrderType:  domain.OrderTypeLimit,
			Quantity:   100,
			LimitPrice: 9.5,
			Timestamp:  time.Now(),
		}

		quote := Quote{
			Symbol: "000001.SZ",
			Open:   10.0,
			High:   11.0,
			Low:    9.0,
			Close:  10.0,
			Volume: 10000,
			Date:   time.Now(),
		}

		// Limit buy at 9.5, low is 9.0 → fills at min(9.5, 11.0) = 9.5
		trade, err := execSvc.ExecuteOrder(order, quote)
		require.NoError(t, err)
		assert.Equal(t, 9.5, trade.Price)
		assert.Equal(t, 100.0, trade.Quantity)

		_, err = tracker.ApplyTrade(trade)
		require.NoError(t, err)
	})

	// Test 5: Limit order not reached
	t.Run("limit_order_not_reached", func(t *testing.T) {
		order := domain.Order{
			Symbol:     "000001.SZ",
			Direction:  domain.DirectionLong,
			OrderType:  domain.OrderTypeLimit,
			Quantity:   100,
			LimitPrice: 8.0,
			Timestamp:  time.Now(),
		}

		quote := Quote{
			Symbol: "000001.SZ",
			Open:   10.0,
			High:   11.0,
			Low:    9.0,
			Close:  10.0,
			Volume: 10000,
			Date:   time.Now(),
		}

		// Limit buy at 8.0, low is 9.0 → does not fill
		_, err := execSvc.ExecuteOrder(order, quote)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "limit price not reached")
	})

	// Test 6: Close position via ExecutionService
	t.Run("close_position_via_execution_service", func(t *testing.T) {
		tracker.Reset(100000)
		execSvc.SetSlippageModel("none")

		// First buy
		buyOrder := domain.Order{
			Symbol:    "000001.SZ",
			Direction: domain.DirectionLong,
			OrderType: domain.OrderTypeMarket,
			Quantity:  100,
			Timestamp: time.Now(),
		}
		quote := Quote{
			Symbol: "000001.SZ",
			Close:  10.0,
			Date:   time.Now(),
		}

		buyTrade, err := execSvc.ExecuteOrder(buyOrder, quote)
		require.NoError(t, err)
		_, err = tracker.ApplyTrade(buyTrade)
		require.NoError(t, err)

		// Advance day to avoid T+1 violation
		tracker.AdvanceDay(time.Now())

		// Then sell
		sellOrder := domain.Order{
			Symbol:    "000001.SZ",
			Direction: domain.DirectionClose,
			OrderType: domain.OrderTypeMarket,
			Quantity:  100,
			Timestamp: time.Now().Add(24 * time.Hour),
		}

		sellTrade, err := execSvc.ExecuteOrder(sellOrder, quote)
		require.NoError(t, err)

		_, err = tracker.ApplyTrade(sellTrade)
		require.NoError(t, err)

		// Position should be closed
		_, exists := tracker.GetPosition("000001.SZ")
		assert.False(t, exists)
	})
}

// TestTracker_ApplyTrade_InsufficientCash tests cash validation in ApplyTrade
func TestTracker_ApplyTrade_InsufficientCash(t *testing.T) {
	logger := zerolog.New(nil)
	tracker := NewTracker(
		1000, // Small capital
		fees.DefaultCommissionRate,
		0.001,
		defaultTradingConfig(),
		logger,
	)

	trade := domain.Trade{
		Symbol:     "000001.SZ",
		Direction:  domain.DirectionLong,
		Quantity:   1000,
		Price:      100.0, // Total = 1000 * 100 = 100000, way over cash
		Commission: 25.0,
		Timestamp:  time.Now(),
	}

	_, err := tracker.ApplyTrade(trade)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient cash")
}

// TestTracker_ApplyTrade_T1Violation tests T+1 rule in ApplyTrade
func TestTracker_ApplyTrade_T1Violation(t *testing.T) {
	logger := zerolog.New(nil)
	tracker := NewTracker(
		100000,
		fees.DefaultCommissionRate,
		0.001,
		defaultTradingConfig(),
		logger,
	)

	now := time.Now()

	// Buy today
	buyTrade := domain.Trade{
		Symbol:     "000001.SZ",
		Direction:  domain.DirectionLong,
		Quantity:   100,
		Price:      10.0,
		Commission: 2.5,
		Timestamp:  now,
	}

	_, err := tracker.ApplyTrade(buyTrade)
	require.NoError(t, err)

	// Try to sell today (T+1 violation)
	sellTrade := domain.Trade{
		Symbol:     "000001.SZ",
		Direction:  domain.DirectionClose,
		Quantity:   100,
		Price:      11.0,
		Commission: 2.75,
		Timestamp:  now,
	}

	_, err = tracker.ApplyTrade(sellTrade)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "T+1 settlement violation")
}

// TestEngine_SetExecutionService tests the setter method
func TestEngine_SetExecutionService(t *testing.T) {
	// Create a minimal engine with the bridges pre-initialized.
	// P1-17 (ADR-020): SetExecutionService now delegates to the
	// ExecutionBridge sub-component; constructing &Engine{...} directly
	// leaves e.executionBridge nil and panics. Initialize bridges here.
	engine := &Engine{
		logger:          zerolog.New(nil),
		liveBridge:      NewLiveBridge(zerolog.New(nil)),
		executionBridge: NewExecutionBridge(zerolog.New(nil)),
	}

	config := domain.ExecutionConfig{
		SlippageModel:  "variable",
		CommissionRate: 0.0003,
		MinCommission:  5.0,
	}
	svc := NewBacktestExecutionService(config)

	engine.SetExecutionService(svc)
	retrieved := engine.GetExecutionService()

	require.NotNil(t, retrieved)
	assert.Equal(t, "variable", retrieved.GetSlippageModel())

	// Test nil setter
	engine.SetExecutionService(nil)
	assert.Nil(t, engine.GetExecutionService())
}

// TestBacktestExecutionService_NoSlippage ensures "none" model passes through
func TestBacktestExecutionService_NoSlippage(t *testing.T) {
	config := domain.ExecutionConfig{
		OrderType:      domain.OrderTypeMarket,
		SlippageModel:  "none",
		CommissionRate: fees.DefaultCommissionRate,
		MinCommission:  5.0,
	}
	svc := NewBacktestExecutionService(config)

	order := domain.Order{
		Symbol:    "000001.SZ",
		Direction: domain.DirectionLong,
		OrderType: domain.OrderTypeMarket,
		Quantity:  100,
		Timestamp: time.Now(),
	}

	quote := Quote{
		Symbol: "000001.SZ",
		Close:  10.0,
		Date:   time.Now(),
	}

	trade, err := svc.ExecuteOrder(order, quote)
	require.NoError(t, err)
	assert.Equal(t, 10.0, trade.Price) // No slippage
}
