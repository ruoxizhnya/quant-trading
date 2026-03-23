// Package backtest provides the backtesting engine for quantitative trading strategies.
package backtest

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// Tracker maintains portfolio state during a backtest run.
type Tracker struct {
	mu sync.RWMutex

	// Portfolio state
	cash         float64
	positions    map[string]*domain.Position
	initialCash  float64

	// History
	portfolioValues []domain.PortfolioValue
	trades          []domain.Trade
	equityCurve     []domain.PortfolioValue

	// Configuration
	commissionRate float64
	slippageRate   float64

	logger zerolog.Logger
}

// NewTracker creates a new portfolio tracker.
func NewTracker(initialCapital, commissionRate, slippageRate float64, logger zerolog.Logger) *Tracker {
	return &Tracker{
		cash:          initialCapital,
		initialCash:   initialCapital,
		positions:     make(map[string]*domain.Position),
		commissionRate: commissionRate,
		slippageRate:   slippageRate,
		logger:        logger.With().Str("component", "tracker").Logger(),
	}
}

// GetCash returns the current cash balance.
func (t *Tracker) GetCash() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.cash
}

// GetPosition returns a copy of the position for a symbol.
func (t *Tracker) GetPosition(symbol string) (*domain.Position, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	pos, exists := t.positions[symbol]
	if !exists {
		return nil, false
	}
	// Return a copy
	copy := *pos
	return &copy, true
}

// GetAllPositions returns all current positions.
func (t *Tracker) GetAllPositions() map[string]domain.Position {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make(map[string]domain.Position)
	for sym, pos := range t.positions {
		result[sym] = *pos
	}
	return result
}

// GetPortfolioValue calculates the total portfolio value at current prices.
func (t *Tracker) GetPortfolioValue(prices map[string]float64) float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	totalValue := t.cash
	for sym, pos := range t.positions {
		if price, ok := prices[sym]; ok {
			pos.MarketValue = pos.Quantity * price
			pos.CurrentPrice = price
			pos.UnrealizedPnL = (price - pos.AvgCost) * pos.Quantity
			totalValue += pos.MarketValue
		}
	}
	return totalValue
}

// ExecuteTrade executes a trade and returns the trade record.
func (t *Tracker) ExecuteTrade(symbol string, direction domain.Direction, quantity float64, price float64, timestamp time.Time) (*domain.Trade, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Apply slippage: buy at higher price, sell at lower price
	executionPrice := price
	switch direction {
	case domain.DirectionLong:
		executionPrice = price * (1 + t.slippageRate)
	case domain.DirectionShort:
		executionPrice = price * (1 - t.slippageRate)
	case domain.DirectionClose:
		// For closing, use the direction of the existing position
		if pos, exists := t.positions[symbol]; exists {
			if pos.Quantity > 0 {
				executionPrice = price * (1 - t.slippageRate)
			} else {
				executionPrice = price * (1 + t.slippageRate)
			}
		}
	}

	commission := quantity * executionPrice * t.commissionRate

	trade := &domain.Trade{
		ID:         uuid.New().String(),
		Symbol:     symbol,
		Direction:  direction,
		Quantity:   quantity,
		Price:      executionPrice,
		Commission: commission,
		Timestamp:  timestamp,
	}

	switch direction {
	case domain.DirectionLong:
		// Cost includes commission
		cost := quantity*executionPrice + commission
		if cost > t.cash {
			return nil, fmt.Errorf("insufficient cash: required %.2f, available %.2f", cost, t.cash)
		}
		t.cash -= cost

		if existing, exists := t.positions[symbol]; exists {
			// Update average cost
			totalQty := existing.Quantity + quantity
			existing.AvgCost = (existing.AvgCost*existing.Quantity + executionPrice*quantity) / totalQty
			existing.Quantity = totalQty
			existing.EntryDate = timestamp
		} else {
			t.positions[symbol] = &domain.Position{
				Symbol:    symbol,
				Quantity:  quantity,
				AvgCost:   executionPrice,
				EntryDate: timestamp,
			}
		}

	case domain.DirectionShort:
		// Short selling: receive cash, owe shares
		proceeds := quantity*executionPrice - commission
		t.cash += proceeds

		if existing, exists := t.positions[symbol]; exists {
			existing.Quantity -= quantity
		} else {
			t.positions[symbol] = &domain.Position{
				Symbol:    symbol,
				Quantity:  -quantity, // negative for short
				AvgCost:   executionPrice,
				EntryDate: timestamp,
			}
		}

	case domain.DirectionClose:
		if pos, exists := t.positions[symbol]; exists {
			closeQty := abs(pos.Quantity)
			actualQty := min(quantity, closeQty)
			if actualQty <= 0 {
				return nil, fmt.Errorf("cannot close position: quantity is zero")
			}

			if pos.Quantity > 0 {
				// Closing long position
				pnl := (executionPrice - pos.AvgCost) * actualQty
				pos.RealizedPnL += pnl - commission
				t.cash += actualQty*executionPrice - commission
				pos.Quantity -= actualQty
			} else {
				// Closing short position
				pnl := (pos.AvgCost - executionPrice) * actualQty
				pos.RealizedPnL += pnl - commission
				t.cash += actualQty*executionPrice - commission
				pos.Quantity += actualQty
			}

			// Remove position if fully closed
			if abs(pos.Quantity) < 1e-8 {
				delete(t.positions, symbol)
			}
		} else {
			return nil, fmt.Errorf("position not found for symbol %s", symbol)
		}
	}

	// Update position market value and unrealized PnL
	if pos, exists := t.positions[symbol]; exists {
		pos.MarketValue = abs(pos.Quantity) * price
		pos.CurrentPrice = price
		pos.UnrealizedPnL = (price - pos.AvgCost) * pos.Quantity
	}

	t.trades = append(t.trades, *trade)
	t.logger.Debug().
		Str("symbol", symbol).
		Str("direction", string(direction)).
		Float64("quantity", quantity).
		Float64("price", executionPrice).
		Float64("commission", commission).
		Time("timestamp", timestamp).
		Msg("Trade executed")

	return trade, nil
}

// RecordDailyValue records the portfolio value for a given day.
func (t *Tracker) RecordDailyValue(date time.Time, prices map[string]float64) domain.PortfolioValue {
	t.mu.Lock()
	defer t.mu.Unlock()

	cash := t.cash
	positionsValue := 0.0

	// Update positions with current prices
	for sym, pos := range t.positions {
		if price, ok := prices[sym]; ok {
			pos.CurrentPrice = price
			pos.MarketValue = abs(pos.Quantity) * price
			pos.UnrealizedPnL = (price - pos.AvgCost) * pos.Quantity
			// For long positions: add market value to equity
			// For short positions: subtract market value (liability to buy back)
			if pos.Quantity > 0 {
				positionsValue += pos.MarketValue
			} else {
				positionsValue -= pos.MarketValue
			}
		}
	}

	totalValue := cash + positionsValue

	pv := domain.PortfolioValue{
		Date:       date,
		TotalValue: totalValue,
		Cash:       cash,
		Positions:  positionsValue,
	}

	t.portfolioValues = append(t.portfolioValues, pv)
	t.equityCurve = append(t.equityCurve, pv)

	return pv
}

// GetPortfolioValues returns all recorded portfolio values.
func (t *Tracker) GetPortfolioValues() []domain.PortfolioValue {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]domain.PortfolioValue, len(t.portfolioValues))
	copy(result, t.portfolioValues)
	return result
}

// GetTrades returns all executed trades.
func (t *Tracker) GetTrades() []domain.Trade {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]domain.Trade, len(t.trades))
	copy(result, t.trades)
	return result
}

// GetEquityCurve returns the equity curve.
func (t *Tracker) GetEquityCurve() []domain.PortfolioValue {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]domain.PortfolioValue, len(t.equityCurve))
	copy(result, t.equityCurve)
	return result
}

// GetTotalValue returns the current total portfolio value.
func (t *Tracker) GetTotalValue(prices map[string]float64) float64 {
	return t.GetPortfolioValue(prices)
}

// ClosePosition closes a position for a symbol.
func (t *Tracker) ClosePosition(symbol string, price float64, timestamp time.Time) (*domain.Trade, error) {
	t.mu.RLock()
	pos, exists := t.positions[symbol]
	t.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("position not found for symbol %s", symbol)
	}

	return t.ExecuteTrade(symbol, domain.DirectionClose, abs(pos.Quantity), price, timestamp)
}

// HasPosition checks if there is an open position for a symbol.
func (t *Tracker) HasPosition(symbol string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	pos, exists := t.positions[symbol]
	return exists && abs(pos.Quantity) > 1e-8
}

// GetPortfolio returns a snapshot of the current portfolio state.
func (t *Tracker) GetPortfolio(prices map[string]float64) *domain.Portfolio {
	t.mu.RLock()
	defer t.mu.RUnlock()

	positions := make(map[string]domain.Position)
	for sym, pos := range t.positions {
		if price, ok := prices[sym]; ok {
			posCopy := *pos
			posCopy.CurrentPrice = price
			posCopy.MarketValue = abs(pos.Quantity) * price
			posCopy.UnrealizedPnL = (price - pos.AvgCost) * pos.Quantity
			positions[sym] = posCopy
		}
	}

	totalValue := t.cash
	for _, pos := range positions {
		// Long positions add value, short positions are liabilities
		if pos.Quantity > 0 {
			totalValue += pos.MarketValue
		} else {
			totalValue -= pos.MarketValue
		}
	}

	return &domain.Portfolio{
		Cash:      t.cash,
		Positions: positions,
		TotalValue: totalValue,
		UpdatedAt: time.Now(),
	}
}

// Reset resets the tracker to initial state.
func (t *Tracker) Reset(initialCapital float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cash = initialCapital
	t.initialCash = initialCapital
	t.positions = make(map[string]*domain.Position)
	t.portfolioValues = nil
	t.trades = nil
	t.equityCurve = nil
}

// Helper functions
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
