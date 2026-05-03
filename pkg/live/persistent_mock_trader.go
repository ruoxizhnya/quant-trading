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

type PersistentMockTraderConfig struct {
	MockTraderConfig
	OrderStore OrderStore
}

type persistentMockTrader struct {
	mu        sync.RWMutex
	config    PersistentMockTraderConfig
	positions map[string]*PositionInfo
	cash      float64
	logger    zerolog.Logger
}

func NewPersistentMockTrader(config PersistentMockTraderConfig, logger zerolog.Logger) LiveTrader {
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
	return &persistentMockTrader{
		config:    config,
		positions: make(map[string]*PositionInfo),
		cash:      config.InitialCash,
		logger:    logger.With().Str("component", "persistent_mock_trader").Logger(),
	}
}

func (m *persistentMockTrader) Name() string { return "persistent_mock_trader" }

func (m *persistentMockTrader) HealthCheck(_ context.Context) error { return nil }

func (m *persistentMockTrader) SubmitOrder(_ context.Context, symbol string, direction domain.Direction, orderType domain.OrderType, quantity float64, price float64) (*OrderResult, error) {
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
			orderRecord := &OrderRecord{
				OrderID:     uuid.New().String(),
				Symbol:      symbol,
				Direction:   string(direction),
				OrderType:   string(orderType),
				Quantity:    quantity,
				FilledQty:   0,
				Price:       execPrice,
				Status:      OrderStatusRejected,
				SubmittedAt: time.Now().Unix(),
				Message:     fmt.Sprintf("insufficient cash: need %.2f, have %.2f", totalCost, m.cash),
			}
			if m.config.OrderStore != nil {
				m.config.OrderStore.Save(context.Background(), orderRecord)
			}
			return &OrderResult{
				OrderID:     orderRecord.OrderID,
				Symbol:      symbol,
				Direction:   direction,
				OrderType:   orderType,
				Quantity:    quantity,
				FilledQty:   0,
				Price:       execPrice,
				Status:      "rejected",
				SubmittedAt: time.Now(),
				Message:     orderRecord.Message,
			}, fmt.Errorf("%s", orderRecord.Message)
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

	now := time.Now()
	result := &OrderResult{
		OrderID:     uuid.New().String(),
		Symbol:      symbol,
		Direction:   direction,
		OrderType:   orderType,
		Quantity:    quantity,
		FilledQty:   quantity,
		Price:       execPrice,
		Status:      "filled",
		SubmittedAt: now,
	}

	if m.config.OrderStore != nil {
		orderRecord := &OrderRecord{
			OrderID:      result.OrderID,
			Symbol:       result.Symbol,
			Direction:    string(result.Direction),
			OrderType:    string(result.OrderType),
			Quantity:     result.Quantity,
			FilledQty:    result.FilledQty,
			Price:        result.Price,
			AvgFillPrice: execPrice,
			Status:       OrderStatusFilled,
			SubmittedAt:  now.Unix(),
			UpdatedAt:    now.Unix(),
		}
		if err := m.config.OrderStore.Save(context.Background(), orderRecord); err != nil {
			m.logger.Warn().Err(err).Str("order_id", result.OrderID).Msg("Failed to persist order")
		}
	}

	m.logger.Info().
		Str("order_id", result.OrderID).
		Str("symbol", symbol).
		Str("direction", string(direction)).
		Float64("qty", quantity).
		Float64("price", execPrice).
		Msg("Persistent mock order filled")

	return result, nil
}

func (m *persistentMockTrader) CancelOrder(ctx context.Context, orderID string) error {
	if m.config.OrderStore != nil {
		order, err := m.config.OrderStore.Get(ctx, orderID)
		if err != nil {
			return fmt.Errorf("order not found: %s", orderID)
		}
		if order.Status != OrderStatusPending && order.Status != OrderStatusPartial {
			return fmt.Errorf("cannot cancel order in status: %s", order.Status)
		}

		err = m.config.OrderStore.Update(ctx, orderID, map[string]interface{}{
			"status": OrderStatusCancelled,
		})
		if err != nil {
			return fmt.Errorf("failed to cancel order: %w", err)
		}

		m.logger.Info().Str("order_id", orderID).Msg("Order cancelled and persisted")
		return nil
	}

	return fmt.Errorf("order store not available for cancellation")
}

func (m *persistentMockTrader) GetOrder(ctx context.Context, orderID string) (*OrderResult, error) {
	if m.config.OrderStore != nil {
		order, err := m.config.OrderStore.Get(ctx, orderID)
		if err != nil {
			return nil, err
		}
		return convertToOrderResult(order), nil
	}

	return nil, fmt.Errorf("order not found (no store configured)")
}

func (m *persistentMockTrader) GetPositions(_ context.Context) ([]PositionInfo, error) {
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

func (m *persistentMockTrader) GetAccount(_ context.Context) (*AccountInfo, error) {
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

func convertToOrderResult(record *OrderRecord) *OrderResult {
	return &OrderResult{
		OrderID:     record.OrderID,
		Symbol:      record.Symbol,
		Direction:   domain.Direction(record.Direction),
		OrderType:   domain.OrderType(record.OrderType),
		Quantity:    record.Quantity,
		FilledQty:   record.FilledQty,
		Price:       record.Price,
		Status:      string(record.Status),
		SubmittedAt: time.Unix(record.SubmittedAt, 0),
		Message:     record.Message,
	}
}
