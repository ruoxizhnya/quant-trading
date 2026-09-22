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

// ApplyQuote marks one position to market at price.
//
// AUD-16: this exists because the engine's mark-to-market used to be a no-op —
// updatePortfolio fetched a snapshot of positions (GetPositions returns copies),
// wrote CurrentPrice / MarketValue / UnrealizedPnL into the copies and dropped
// them, so every mark-to-market field stayed 0 forever and the two totals below
// were permanently 0 as well.
//
// The read-modify-write is done under one write lock on purpose: doing it as
// "GetPositions → mutate → UpdatePosition" would clobber a fill (or a broker
// position sync) that lands in between, which is the same class of bug one
// layer up.
//
// Returns false when the symbol is not held, so a quote for something we do not
// own cannot create a position.
func (pm *PositionManager) ApplyQuote(symbol string, price float64) (domain.Position, bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	position, exists := pm.positions[symbol]
	if !exists {
		return domain.Position{}, false
	}
	position.CurrentPrice = price
	position.MarketValue = price * position.Quantity
	position.UnrealizedPnL = position.MarketValue - (position.AvgCost * position.Quantity)
	pm.positions[symbol] = position
	return position, true
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
