package live

import (
	"sync"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// PositionManager manages trading positions
type PositionManager struct {
	positions map[string]domain.Position
	mu        sync.RWMutex
}

// NewPositionManager creates a new position manager
func NewPositionManager() *PositionManager {
	return &PositionManager{
		positions: make(map[string]domain.Position),
	}
}

// UpdatePosition updates a position directly
func (pm *PositionManager) UpdatePosition(position domain.Position) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.positions[position.Symbol] = position
}

// UpdateFromTrade updates positions based on a trade
func (pm *PositionManager) UpdateFromTrade(trade domain.Trade) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	position, exists := pm.positions[trade.Symbol]
	if !exists {
		position = domain.Position{
			Symbol: trade.Symbol,
		}
	}

	if trade.Direction == domain.DirectionLong {
		totalCost := position.AvgCost*position.Quantity + trade.Price*trade.Quantity
		position.Quantity += trade.Quantity
		if position.Quantity > 0 {
			position.AvgCost = totalCost / position.Quantity
		}
	} else {
		if position.Quantity > 0 {
			realizedPnL := (trade.Price - position.AvgCost) * trade.Quantity
			position.RealizedPnL += realizedPnL
		}
		position.Quantity -= trade.Quantity
		if position.Quantity == 0 {
			position.AvgCost = 0
		}
	}

	pm.positions[trade.Symbol] = position
}

// GetPosition returns a position by symbol
func (pm *PositionManager) GetPosition(symbol string) (domain.Position, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	position, exists := pm.positions[symbol]
	return position, exists
}

// GetPositions returns all positions
func (pm *PositionManager) GetPositions() []domain.Position {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make([]domain.Position, 0, len(pm.positions))
	for _, position := range pm.positions {
		result = append(result, position)
	}
	return result
}

// GetTotalMarketValue returns total market value of all positions
func (pm *PositionManager) GetTotalMarketValue() float64 {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	total := 0.0
	for _, position := range pm.positions {
		total += position.MarketValue
	}
	return total
}

// GetTotalUnrealizedPnL returns total unrealized P&L
func (pm *PositionManager) GetTotalUnrealizedPnL() float64 {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	total := 0.0
	for _, position := range pm.positions {
		total += position.UnrealizedPnL
	}
	return total
}

// GetTotalRealizedPnL returns total realized P&L
func (pm *PositionManager) GetTotalRealizedPnL() float64 {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	total := 0.0
	for _, position := range pm.positions {
		total += position.RealizedPnL
	}
	return total
}

// HasPosition checks if there's a position for a symbol
func (pm *PositionManager) HasPosition(symbol string) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	position, exists := pm.positions[symbol]
	return exists && position.Quantity != 0
}

// RemovePosition removes a position (when fully closed)
func (pm *PositionManager) RemovePosition(symbol string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.positions, symbol)
}
