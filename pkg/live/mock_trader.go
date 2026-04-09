package live

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

type MockTraderConfig struct {
	InitialCash     float64
	CommissionRate  float64
	StampTaxRate    float64
	SlippageRate    float64
	PriceProvider   func(symbol string) float64
}

type mockTrader struct {
	mu       sync.RWMutex
	config   MockTraderConfig
	positions map[string]*PositionInfo
	orders   map[string]*OrderResult
	cash     float64
	logger   zerolog.Logger
}

func NewMockTrader(config MockTraderConfig, logger zerolog.Logger) LiveTrader {
	if config.InitialCash <= 0 {
		config.InitialCash = 1000000
	}
	if config.CommissionRate <= 0 {
		config.CommissionRate = 0.0003
	}
	if config.StampTaxRate <= 0 {
		config.StampTaxRate = 0.001
	}
	if config.SlippageRate <= 0 {
		config.SlippageRate = 0.0001
	}
	return &mockTrader{
		config:    config,
		positions: make(map[string]*PositionInfo),
		orders:    make(map[string]*OrderResult),
		cash:      config.InitialCash,
		logger:    logger.With().Str("component", "mock_trader").Logger(),
	}
}

func (m *mockTrader) Name() string { return "mock_trader" }

func (m *mockTrader) HealthCheck(_ context.Context) error { return nil }

func (m *mockTrader) SubmitOrder(_ context.Context, symbol string, direction domain.Direction, orderType domain.OrderType, quantity float64, price float64) (*OrderResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive")
	}
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}

	execPrice := price
	if orderType == domain.OrderTypeMarket && m.config.PriceProvider != nil {
		execPrice = m.config.PriceProvider(symbol)
	}
	if execPrice <= 0 {
		return nil, fmt.Errorf("invalid execution price: %.4f", execPrice)
	}

	switch direction {
	case domain.DirectionLong:
		slippage := execPrice * m.config.SlippageRate
		fillPrice := execPrice + slippage
		tradeValue := quantity * fillPrice
		commission := max(tradeValue*m.config.CommissionRate, 5.0)
		transferFee := tradeValue * 0.00001
		totalCost := tradeValue + commission + transferFee

		if totalCost > m.cash {
			return nil, fmt.Errorf("insufficient cash: need %.2f, have %.2f", totalCost, m.cash)
		}

		m.cash -= totalCost

		if pos, ok := m.positions[symbol]; ok {
			totalQty := pos.Quantity + quantity
			pos.AvgCost = (pos.AvgCost*pos.Quantity + fillPrice*quantity) / totalQty
			pos.Quantity = totalQty
			pos.QuantityToday += quantity
			pos.MarketValue = pos.Quantity * fillPrice
		} else {
			m.positions[symbol] = &PositionInfo{
				Symbol:           symbol,
				Quantity:         quantity,
				AvailableQty:     0,
				AvgCost:          fillPrice,
				CurrentPrice:     fillPrice,
				MarketValue:      quantity * fillPrice,
				QuantityToday:    quantity,
				QuantityYesterday: 0,
			}
		}

	case domain.DirectionClose:
		pos, ok := m.positions[symbol]
		if !ok || pos.Quantity <= 0 {
			return nil, fmt.Errorf("no position to close for %s", symbol)
		}
		if quantity > pos.Quantity {
			quantity = pos.Quantity
		}

		slippage := execPrice * m.config.SlippageRate
		fillPrice := execPrice - slippage
		tradeValue := quantity * fillPrice
		commission := max(tradeValue*m.config.CommissionRate, 5.0)
		transferFee := tradeValue * 0.00001
		stampTax := tradeValue * m.config.StampTaxRate
		netProceeds := tradeValue - commission - transferFee - stampTax

		m.cash += netProceeds

		pos.Quantity -= quantity
		if pos.Quantity <= 0 {
			delete(m.positions, symbol)
		} else {
			pos.MarketValue = pos.Quantity * fillPrice
		}

	default:
		return nil, fmt.Errorf("unsupported direction: %s", direction)
	}

	result := &OrderResult{
		OrderID:     uuid.New().String(),
		Symbol:      symbol,
		Direction:   direction,
		OrderType:   orderType,
		Quantity:    quantity,
		FilledQty:   quantity,
		Price:       execPrice,
		Status:      "filled",
		SubmittedAt: time.Now(),
	}
	m.orders[result.OrderID] = result

	m.logger.Info().
		Str("order_id", result.OrderID).
		Str("symbol", symbol).
		Str("direction", string(direction)).
		Float64("qty", quantity).
		Float64("price", execPrice).
		Msg("Mock order filled")

	return result, nil
}

func (m *mockTrader) CancelOrder(_ context.Context, orderID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	order, ok := m.orders[orderID]
	if !ok {
		return fmt.Errorf("order not found: %s", orderID)
	}
	if order.Status != "pending" {
		return fmt.Errorf("cannot cancel order in status: %s", order.Status)
	}
	order.Status = "cancelled"
	return nil
}

func (m *mockTrader) GetOrder(_ context.Context, orderID string) (*OrderResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	order, ok := m.orders[orderID]
	if !ok {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}
	return order, nil
}

func (m *mockTrader) GetPositions(_ context.Context) ([]PositionInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]PositionInfo, 0, len(m.positions))
	for _, pos := range m.positions {
		if m.config.PriceProvider != nil {
			pos.CurrentPrice = m.config.PriceProvider(pos.Symbol)
			pos.MarketValue = pos.Quantity * pos.CurrentPrice
			pos.UnrealizedPnL = (pos.CurrentPrice - pos.AvgCost) * pos.Quantity
		}
		result = append(result, *pos)
	}
	return result, nil
}

func (m *mockTrader) GetAccount(_ context.Context) (*AccountInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var marketValue float64
	var unrealizedPnL float64
	for _, pos := range m.positions {
		if m.config.PriceProvider != nil {
			pos.CurrentPrice = m.config.PriceProvider(pos.Symbol)
		}
		marketValue += pos.Quantity * pos.CurrentPrice
		unrealizedPnL += (pos.CurrentPrice - pos.AvgCost) * pos.Quantity
	}

	return &AccountInfo{
		TotalAssets:   m.cash + marketValue,
		Cash:          m.cash,
		MarketValue:   marketValue,
		UnrealizedPnL: unrealizedPnL,
		UpdatedAt:     time.Now(),
	}, nil
}
