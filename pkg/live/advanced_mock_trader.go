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

type advancedMockTrader struct {
	*persistentMockTrader
	mu           sync.RWMutex
	trades       []TradeRecord
	cashFlows    []CashFlow
	subscribers  map[string]chan MarketData
	logger       zerolog.Logger
}

func NewAdvancedMockTrader(config PersistentMockTraderConfig, logger zerolog.Logger) AdvancedTrader {
	base := NewPersistentMockTrader(config, logger)
	return &advancedMockTrader{
		persistentMockTrader: base.(*persistentMockTrader),
		trades:               make([]TradeRecord, 0),
		cashFlows:            make([]CashFlow, 0),
		subscribers:          make(map[string]chan MarketData),
		logger:               logger.With().Str("component", "advanced_mock_trader").Logger(),
	}
}

func (t *advancedMockTrader) SubmitOrders(ctx context.Context, orders []OrderRequest) (*BatchOrderResult, error) {
	result := &BatchOrderResult{
		Successes: make([]*OrderResult, 0),
		Failures:  make([]BatchError, 0),
	}

	for _, req := range orders {
		order, err := t.SubmitOrder(ctx, req.Symbol, req.Direction, req.OrderType, req.Quantity, req.Price)
		if err != nil {
			result.Failures = append(result.Failures, BatchError{
				Symbol: req.Symbol,
				Error:  err.Error(),
			})
			continue
		}
		result.Successes = append(result.Successes, order)
	}

	t.logger.Info().
		Int("successes", len(result.Successes)).
		Int("failures", len(result.Failures)).
		Msg("Batch orders submitted")

	return result, nil
}

func (t *advancedMockTrader) CancelAllOrders(_ context.Context, symbol string) (int, error) {
	if t.config.OrderStore == nil {
		return 0, fmt.Errorf("order store not available")
	}

	orders, err := t.config.OrderStore.List(context.Background(), symbol, OrderStatusPending)
	if err != nil {
		return 0, err
	}

	cancelled := 0
	for _, order := range orders {
		if err := t.CancelOrder(context.Background(), order.OrderID); err != nil {
			t.logger.Warn().Err(err).Str("order_id", order.OrderID).Msg("Failed to cancel order")
			continue
		}
		cancelled++
	}

	return cancelled, nil
}

func (t *advancedMockTrader) ListOrders(_ context.Context, filter OrderFilter) ([]*OrderResult, int, error) {
	if t.config.OrderStore == nil {
		return []*OrderResult{}, 0, nil
	}

	orders, err := t.config.OrderStore.List(context.Background(), filter.Symbol, filter.Status)
	if err != nil {
		return nil, 0, err
	}

	results := make([]*OrderResult, len(orders))
	for i, order := range orders {
		results[i] = convertToOrderResult(order)
	}

	return results, len(results), nil
}

func (t *advancedMockTrader) GetTrades(_ context.Context, orderID string) ([]TradeRecord, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result []TradeRecord
	for _, trade := range t.trades {
		if trade.OrderID == orderID {
			result = append(result, trade)
		}
	}
	return result, nil
}

func (t *advancedMockTrader) ListTodayTrades(_ context.Context) ([]TradeRecord, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	now := time.Now()
	var result []TradeRecord
	for _, trade := range t.trades {
		if trade.TradeTime.Year() == now.Year() &&
			trade.TradeTime.Month() == now.Month() &&
			trade.TradeTime.Day() == now.Day() {
			result = append(result, trade)
		}
	}
	return result, nil
}

func (t *advancedMockTrader) GetPosition(_ context.Context, symbol string) (*PositionDetail, error) {
	positions, err := t.GetPositions(context.Background())
	if err != nil {
		return nil, err
	}

	for _, pos := range positions {
		if pos.Symbol == symbol {
			detail := &PositionDetail{PositionInfo: pos}
			if pos.Quantity > 0 {
				detail.ProfitRatio = (pos.UnrealizedPnL / pos.MarketValue) * 100
				detail.DaysHeld = int(time.Since(time.Now()).Hours() / 24) // mock value
			}
			return detail, nil
		}
	}

	return nil, fmt.Errorf("position not found for %s", symbol)
}

func (t *advancedMockTrader) GetAvailableQuantity(_ context.Context, symbol string) (float64, error) {
	pos, err := t.GetPosition(context.Background(), symbol)
	if err != nil {
		return 0, err
	}

	return pos.AvailableQty, nil
}

func (t *advancedMockTrader) GetCashFlow(_ context.Context, startTime, endTime *time.Time) ([]CashFlow, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result []CashFlow
	for _, cf := range t.cashFlows {
		if startTime != nil && cf.CreatedAt.Before(*startTime) {
			continue
		}
		if endTime != nil && cf.CreatedAt.After(*endTime) {
			continue
		}
		result = append(result, cf)
	}
	return result, nil
}

func (t *advancedMockTrader) GetFrozenCash(_ context.Context) (float64, error) {
	account, err := t.GetAccount(context.Background())
	if err != nil {
		return 0, err
	}

	frozen := account.TotalAssets - account.Cash - account.MarketValue
	if frozen < 0 {
		frozen = 0
	}
	return frozen, nil
}

func (t *advancedMockTrader) SubscribeQuotes(_ context.Context, symbols []string) (<-chan MarketData, error) {
	ch := make(chan MarketData, 10)

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			for _, symbol := range symbols {
				data := MarketData{
					Symbol:     symbol,
					LastPrice:  10.0 + float64(len(symbol)), // mock price
					Volume:     int64(1000000),
					Turnover:   1e7,
					UpdateTime: time.Now(),
				}
				select {
				case ch <- data:
				default:
					t.logger.Warn().Str("symbol", symbol).Msg("Quote channel full, dropping message")
				}
			}
		}
	}()

	t.mu.Lock()
	key := uuid.New().String()
	t.subscribers[key] = ch
	t.mu.Unlock()

	t.logger.Info().Int("symbols", len(symbols)).Msg("Quotes subscribed")
	return ch, nil
}

func (t *advancedMockTrader) UnsubscribeQuotes(symbols []string) error {
	t.logger.Info().Int("symbols", len(symbols)).Msg("Quotes unsubscribed")
	return nil
}

func (t *advancedMockTrader) GetQuote(_ context.Context, symbol string) (*MarketData, error) {
	return &MarketData{
		Symbol:     symbol,
		LastPrice:  10.5,
		Open:       10.2,
		High:       10.8,
		Low:        10.1,
		PrevClose:  10.3,
		Volume:     1000000,
		Turnover:   1.05e7,
		UpdateTime: time.Now(),
	}, nil
}

func (t *advancedMockTrader) Connect(_ context.Context) error {
	t.logger.Info().Msg("Advanced mock trader connected")
	return nil
}

func (t *advancedMockTrader) Disconnect() error {
	t.logger.Info().Msg("Advanced mock trader disconnected")
	return nil
}

func (t *advancedMockTrader) GetConnectionStatus() ConnectionStatus {
	return ConnectionStatus{
		Connected:  true,
		ServerTime: time.Now(),
		LoginTime:  time.Now().Add(-1 * time.Hour),
		LastPing:   time.Now(),
		LatencyMs:  1,
	}
}

func (t *advancedMockTrader) CheckMargin(_ context.Context, symbol string, direction domain.Direction, quantity float64) (*MarginCheckResult, error) {
	account, err := t.GetAccount(context.Background())
	if err != nil {
		return nil, err
	}

	requiredCash := quantity * 10.0 // mock price
	sufficient := account.Cash >= requiredCash

	return &MarginCheckResult{
		Sufficient:    sufficient,
		RequiredCash:  requiredCash,
		AvailableCash: account.Cash,
		Shortfall:     requiredCash - account.Cash,
		Message:       fmt.Sprintf("margin check for %s: %v", symbol, sufficient),
	}, nil
}
