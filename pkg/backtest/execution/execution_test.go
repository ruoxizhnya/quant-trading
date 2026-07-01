package execution

import (
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBacktestExecutionService(t *testing.T) {
	config := domain.DefaultExecutionConfig()
	service := NewBacktestExecutionService(config)
	require.NotNil(t, service)
	assert.Equal(t, "fixed", service.GetSlippageModel())
}

func TestBacktestExecutionService_ExecuteOrder_Market(t *testing.T) {
	config := domain.DefaultExecutionConfig()
	service := NewBacktestExecutionService(config)

	quote := Quote{
		Symbol: "000001.SZ",
		Open:   10.0,
		High:   11.0,
		Low:    9.0,
		Close:  10.0,
		Volume: 1000,
		Date:   time.Now(),
	}

	t.Run("buy market order", func(t *testing.T) {
		order := domain.Order{
			Symbol:    "000001.SZ",
			Direction: domain.DirectionLong,
			OrderType: domain.OrderTypeMarket,
			Quantity:  100,
		}

		trade, err := service.ExecuteOrder(order, quote)
		require.NoError(t, err)
		assert.Equal(t, "000001.SZ", trade.Symbol)
		assert.Equal(t, domain.DirectionLong, trade.Direction)
		assert.Equal(t, 100.0, trade.Quantity)
		assert.Greater(t, trade.Price, quote.Close) // Slippage applied
		assert.Greater(t, trade.Commission, 0.0)
	})

	t.Run("sell market order", func(t *testing.T) {
		order := domain.Order{
			Symbol:    "000001.SZ",
			Direction: domain.DirectionShort,
			OrderType: domain.OrderTypeMarket,
			Quantity:  100,
		}

		trade, err := service.ExecuteOrder(order, quote)
		require.NoError(t, err)
		assert.Less(t, trade.Price, quote.Close) // Slippage applied for sell
	})

	t.Run("invalid quantity", func(t *testing.T) {
		order := domain.Order{
			Symbol:    "000001.SZ",
			Direction: domain.DirectionLong,
			OrderType: domain.OrderTypeMarket,
			Quantity:  0,
		}

		_, err := service.ExecuteOrder(order, quote)
		assert.Error(t, err)
	})
}

func TestBacktestExecutionService_ExecuteOrder_Limit(t *testing.T) {
	config := domain.DefaultExecutionConfig()
	service := NewBacktestExecutionService(config)

	quote := Quote{
		Symbol: "000001.SZ",
		Open:   10.0,
		High:   11.0,
		Low:    9.0,
		Close:  10.0,
		Volume: 1000,
		Date:   time.Now(),
	}

	t.Run("buy limit order executed", func(t *testing.T) {
		order := domain.Order{
			Symbol:     "000001.SZ",
			Direction:  domain.DirectionLong,
			OrderType:  domain.OrderTypeLimit,
			Quantity:   100,
			LimitPrice: 10.5,
		}

		trade, err := service.ExecuteOrder(order, quote)
		require.NoError(t, err)
		assert.Equal(t, 10.5, trade.Price)
	})

	t.Run("buy limit order not reached", func(t *testing.T) {
		order := domain.Order{
			Symbol:     "000001.SZ",
			Direction:  domain.DirectionLong,
			OrderType:  domain.OrderTypeLimit,
			Quantity:   100,
			LimitPrice: 8.0, // Below low
		}

		_, err := service.ExecuteOrder(order, quote)
		assert.Error(t, err)
	})

	t.Run("sell limit order executed", func(t *testing.T) {
		order := domain.Order{
			Symbol:     "000001.SZ",
			Direction:  domain.DirectionShort,
			OrderType:  domain.OrderTypeLimit,
			Quantity:   100,
			LimitPrice: 9.5,
		}

		trade, err := service.ExecuteOrder(order, quote)
		require.NoError(t, err)
		assert.Equal(t, 9.5, trade.Price)
	})

	t.Run("sell limit order not reached", func(t *testing.T) {
		order := domain.Order{
			Symbol:     "000001.SZ",
			Direction:  domain.DirectionShort,
			OrderType:  domain.OrderTypeLimit,
			Quantity:   100,
			LimitPrice: 12.0, // Above high
		}

		_, err := service.ExecuteOrder(order, quote)
		assert.Error(t, err)
	})

	t.Run("invalid limit price", func(t *testing.T) {
		order := domain.Order{
			Symbol:     "000001.SZ",
			Direction:  domain.DirectionLong,
			OrderType:  domain.OrderTypeLimit,
			Quantity:   100,
			LimitPrice: 0,
		}

		_, err := service.ExecuteOrder(order, quote)
		assert.Error(t, err)
	})
}

func TestBacktestExecutionService_SlippageModels(t *testing.T) {
	quote := Quote{
		Symbol: "000001.SZ",
		Open:   10.0,
		High:   12.0,
		Low:    8.0,
		Close:  10.0,
		Volume: 1000,
		Date:   time.Now(),
	}

	order := domain.Order{
		Symbol:    "000001.SZ",
		Direction: domain.DirectionLong,
		OrderType: domain.OrderTypeMarket,
		Quantity:  100,
	}

	t.Run("fixed slippage", func(t *testing.T) {
		config := domain.DefaultExecutionConfig()
		service := NewBacktestExecutionService(config)
		service.SetSlippageModel("fixed")

		trade, err := service.ExecuteOrder(order, quote)
		require.NoError(t, err)
		assert.InDelta(t, 10.01, trade.Price, 0.01) // 0.1% slippage
	})

	t.Run("variable slippage", func(t *testing.T) {
		config := domain.DefaultExecutionConfig()
		service := NewBacktestExecutionService(config)
		service.SetSlippageModel("variable")

		trade, err := service.ExecuteOrder(order, quote)
		require.NoError(t, err)
		// Volatility = (12-8)/10 = 0.4, slippage = 0.4 * 0.1 = 0.04
		assert.InDelta(t, 10.4, trade.Price, 0.1)
	})

	t.Run("no slippage", func(t *testing.T) {
		config := domain.DefaultExecutionConfig()
		service := NewBacktestExecutionService(config)
		service.SetSlippageModel("none")

		trade, err := service.ExecuteOrder(order, quote)
		require.NoError(t, err)
		assert.Equal(t, 10.0, trade.Price)
	})
}

func TestBacktestExecutionService_Commission(t *testing.T) {
	t.Run("percentage commission", func(t *testing.T) {
		config := domain.ExecutionConfig{
			CommissionRate: 0.001, // 0.1%
			MinCommission:  5.0,
		}
		service := NewBacktestExecutionService(config)

		quote := Quote{
			Symbol: "000001.SZ",
			Close:  100.0,
			Date:   time.Now(),
		}

		order := domain.Order{
			Symbol:    "000001.SZ",
			Direction: domain.DirectionLong,
			OrderType: domain.OrderTypeMarket,
			Quantity:  100,
		}

		trade, err := service.ExecuteOrder(order, quote)
		require.NoError(t, err)
		// Commission = 100 * 100 * 0.001 = 10
		assert.InDelta(t, 10.0, trade.Commission, 1.0)
	})

	t.Run("minimum commission", func(t *testing.T) {
		config := domain.ExecutionConfig{
			CommissionRate: 0.0001, // Very low rate
			MinCommission:  5.0,
		}
		service := NewBacktestExecutionService(config)

		quote := Quote{
			Symbol: "000001.SZ",
			Close:  10.0,
			Date:   time.Now(),
		}

		order := domain.Order{
			Symbol:    "000001.SZ",
			Direction: domain.DirectionLong,
			OrderType: domain.OrderTypeMarket,
			Quantity:  10,
		}

		trade, err := service.ExecuteOrder(order, quote)
		require.NoError(t, err)
		// Commission would be 10 * 10 * 0.0001 = 0.01, but min is 5.0
		assert.Equal(t, 5.0, trade.Commission)
	})
}

func TestMinMax(t *testing.T) {
	assert.Equal(t, 1.0, min(1.0, 2.0))
	assert.Equal(t, 1.0, min(2.0, 1.0))
	assert.Equal(t, 2.0, max(1.0, 2.0))
	assert.Equal(t, 2.0, max(2.0, 1.0))
}
