package live

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/fees"
	"github.com/ruoxizhnya/quant-trading/pkg/id"
)

// MockTraderConfig configures the mock trader simulation.
//
// Rate fields default to `pkg/fees.DefaultAShareFees()` when
// left zero; that is the regulatory default from the 2024-Q1
// 上交所/深交所公告. See pkg/fees for the exact values and
// the change log.
type MockTraderConfig struct {
	InitialCash     float64
	CommissionRate  float64
	StampTaxRate    float64
	SlippageRate    float64
	TransferFeeRate float64
	MinCommission   float64
	PriceProvider   func(symbol string) float64
}

// MockTrader implements LiveTrader with in-memory simulation.
// It enforces A-share trading rules including T+1 settlement,
// stamp tax on sells, minimum commission, and transfer fees.
type MockTrader struct {
	mu        sync.RWMutex
	config    MockTraderConfig
	positions map[string]*PositionInfo
	orders    map[string]*OrderResult
	cash      float64
	logger    zerolog.Logger
}

// NewMockTrader creates a new mock trader for paper trading simulation.
// It uses A-share trading rules by default.
func NewMockTrader(config MockTraderConfig, logger zerolog.Logger) *MockTrader {
	if config.InitialCash <= 0 {
		config.InitialCash = 1000000
	}
	// Sprint 6 P1-22 (ODR-013): fee defaults now sourced from
	// the unified `pkg/fees` package. Pre-P1-22 these 5 lines
	// were hardcoded literals (0.0003, 0.001, 0.0001, 0.00001,
	// 5.0) and diverged from `pkg/backtest/constants.go` after
	// the 2023-08 stamp tax cut.
	defaults := fees.DefaultAShareFees()
	if config.CommissionRate <= 0 {
		config.CommissionRate = defaults.CommissionRate
	}
	if config.StampTaxRate <= 0 {
		config.StampTaxRate = defaults.StampTaxRate
	}
	if config.SlippageRate <= 0 {
		config.SlippageRate = defaults.SlippageRate
	}
	if config.TransferFeeRate <= 0 {
		config.TransferFeeRate = defaults.TransferFeeRate
	}
	if config.MinCommission <= 0 {
		config.MinCommission = defaults.MinCommission
	}
	return &MockTrader{
		config:    config,
		positions: make(map[string]*PositionInfo),
		orders:    make(map[string]*OrderResult),
		cash:      config.InitialCash,
		logger:    logger.With().Str("component", "mock_trader").Logger(),
	}
}

func (m *MockTrader) Name() string { return "mock_trader" }

func (m *MockTrader) HealthCheck(_ context.Context) error { return nil }

// SubmitOrder submits an order with A-share trading rules simulation.
// For buy orders: deducts cash including commission and transfer fee.
// For sell orders: credits cash after deducting commission, transfer fee, and stamp tax.
// T+1 settlement: bought shares are tracked as QuantityToday (not sellable until next day).
func (m *MockTrader) SubmitOrder(_ context.Context, symbol string, direction domain.Direction, orderType domain.OrderType, quantity float64, price float64) (*OrderResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if quantity <= 0 {
		return nil, fmt.Errorf("quantity must be positive, got %.2f", quantity)
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
		return m.executeBuy(symbol, orderType, quantity, execPrice)
	case domain.DirectionClose:
		return m.executeSell(symbol, orderType, quantity, execPrice)
	default:
		return nil, fmt.Errorf("unsupported direction: %s (only 'long' and 'close' supported)", direction)
	}
}

func (m *MockTrader) executeBuy(symbol string, orderType domain.OrderType, quantity float64, execPrice float64) (*OrderResult, error) {
	slippage := execPrice * m.config.SlippageRate
	fillPrice := execPrice + slippage
	tradeValue := quantity * fillPrice
	commission := max(tradeValue*m.config.CommissionRate, m.config.MinCommission)
	transferFee := tradeValue * m.config.TransferFeeRate
	totalCost := tradeValue + commission + transferFee

	if totalCost > m.cash {
		return nil, fmt.Errorf("insufficient cash: need %.2f (value=%.2f commission=%.2f transfer=%.2f), have %.2f",
			totalCost, tradeValue, commission, transferFee, m.cash)
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
			Symbol:            symbol,
			Quantity:          quantity,
			AvailableQty:      0, // T+1: not available until next day
			AvgCost:           fillPrice,
			CurrentPrice:      fillPrice,
			MarketValue:       quantity * fillPrice,
			QuantityToday:     quantity,
			QuantityYesterday: 0,
		}
	}

	result := &OrderResult{
		OrderID:     id.OrderID(),
		Symbol:      symbol,
		Direction:   domain.DirectionLong,
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
		Str("direction", "buy").
		Float64("qty", quantity).
		Float64("price", execPrice).
		Float64("fill_price", fillPrice).
		Float64("commission", commission).
		Float64("transfer_fee", transferFee).
		Float64("cash_remaining", m.cash).
		Msg("Mock buy order filled")

	return result, nil
}

func (m *MockTrader) executeSell(symbol string, orderType domain.OrderType, quantity float64, execPrice float64) (*OrderResult, error) {
	pos, ok := m.positions[symbol]
	if !ok || pos.Quantity <= 0 {
		return nil, fmt.Errorf("no position to close for %s", symbol)
	}

	// T+1 check: can only sell QuantityYesterday, not QuantityToday
	if pos.QuantityYesterday <= 0 {
		return nil, fmt.Errorf("T+1 settlement violation: no sellable shares for %s (all %f shares bought today)", symbol, pos.QuantityToday)
	}

	if quantity > pos.QuantityYesterday {
		m.logger.Warn().
			Str("symbol", symbol).
			Float64("requested", quantity).
			Float64("sellable", pos.QuantityYesterday).
			Float64("total", pos.Quantity).
			Msg("Reducing sell quantity to sellable shares (T+1)")
		quantity = pos.QuantityYesterday
	}

	slippage := execPrice * m.config.SlippageRate
	fillPrice := execPrice - slippage
	tradeValue := quantity * fillPrice
	commission := max(tradeValue*m.config.CommissionRate, m.config.MinCommission)
	transferFee := tradeValue * m.config.TransferFeeRate
	stampTax := tradeValue * m.config.StampTaxRate
	netProceeds := tradeValue - commission - transferFee - stampTax

	m.cash += netProceeds

	pos.QuantityYesterday -= quantity
	pos.Quantity -= quantity
	if pos.Quantity <= 0 {
		delete(m.positions, symbol)
	} else {
		pos.MarketValue = pos.Quantity * fillPrice
	}

	result := &OrderResult{
		OrderID:     id.OrderID(),
		Symbol:      symbol,
		Direction:   domain.DirectionClose,
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
		Str("direction", "sell").
		Float64("qty", quantity).
		Float64("price", execPrice).
		Float64("fill_price", fillPrice).
		Float64("commission", commission).
		Float64("transfer_fee", transferFee).
		Float64("stamp_tax", stampTax).
		Float64("net_proceeds", netProceeds).
		Float64("cash_remaining", m.cash).
		Msg("Mock sell order filled")

	return result, nil
}

func (m *MockTrader) CancelOrder(_ context.Context, orderID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	order, ok := m.orders[orderID]
	if !ok {
		return fmt.Errorf("order not found: %s", orderID)
	}
	if !order.IsActive() {
		return fmt.Errorf("cannot cancel order in status: %s", order.Status)
	}
	order.Status = "cancelled"
	return nil
}

func (m *MockTrader) GetOrder(_ context.Context, orderID string) (*OrderResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	order, ok := m.orders[orderID]
	if !ok {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}
	// Return a copy
	copy := *order
	return &copy, nil
}

func (m *MockTrader) GetPositions(_ context.Context) ([]PositionInfo, error) {
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

func (m *MockTrader) GetAccount(_ context.Context) (*AccountInfo, error) {
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

// AdvanceDay rolls over T+1 settlement: QuantityToday → QuantityYesterday.
// Call this at the end of each trading day to make today's buys sellable tomorrow.
func (m *MockTrader) AdvanceDay() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for sym, pos := range m.positions {
		if pos.QuantityToday > 0 {
			pos.QuantityYesterday += pos.QuantityToday
			pos.AvailableQty = pos.QuantityYesterday
			pos.QuantityToday = 0
			m.logger.Debug().
				Str("symbol", sym).
				Float64("quantity_yesterday", pos.QuantityYesterday).
				Msg("T+1 rollover: yesterday quantity updated")
		}
	}
}

// GetCash returns the current cash balance.
func (m *MockTrader) GetCash() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cash
}

// Reset resets the mock trader to its initial state.
func (m *MockTrader) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cash = m.config.InitialCash
	m.positions = make(map[string]*PositionInfo)
	m.orders = make(map[string]*OrderResult)
}

// max is provided by Go 1.21+ builtin (P0-10).
