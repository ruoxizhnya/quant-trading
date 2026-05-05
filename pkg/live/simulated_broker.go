package live

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// SimulatedBroker implements a paper trading broker for testing
type SimulatedBroker struct {
	mu         sync.RWMutex
	connected  bool
	orders     map[string]domain.Order
	positions  map[string]domain.Position
	balance    float64
	orderCount int
}

// NewSimulatedBroker creates a new simulated broker
func NewSimulatedBroker(initialBalance float64) *SimulatedBroker {
	return &SimulatedBroker{
		orders:    make(map[string]domain.Order),
		positions: make(map[string]domain.Position),
		balance:   initialBalance,
	}
}

// Connect connects to the simulated broker
func (b *SimulatedBroker) Connect() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.connected = true
	return nil
}

// Disconnect disconnects from the simulated broker
func (b *SimulatedBroker) Disconnect() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.connected = false
	return nil
}

// SubmitOrder submits an order
func (b *SimulatedBroker) SubmitOrder(order domain.Order) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.connected {
		return "", fmt.Errorf("broker not connected")
	}

	b.orderCount++
	orderID := fmt.Sprintf("SIM-ORD-%d", b.orderCount)
	order.ID = orderID
	order.Status = "submitted"
	order.Timestamp = time.Now()

	b.orders[orderID] = order

	// Simulate immediate fill for market orders
	if order.OrderType == domain.OrderTypeMarket {
		b.fillOrder(orderID)
	}

	return orderID, nil
}

// CancelOrder cancels an order
func (b *SimulatedBroker) CancelOrder(orderID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	order, exists := b.orders[orderID]
	if !exists {
		return fmt.Errorf("order not found: %s", orderID)
	}

	if order.Status == "filled" {
		return fmt.Errorf("cannot cancel filled order")
	}

	order.Status = "cancelled"
	b.orders[orderID] = order

	return nil
}

// GetOrderStatus returns order status
func (b *SimulatedBroker) GetOrderStatus(orderID string) (string, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	order, exists := b.orders[orderID]
	if !exists {
		return "", fmt.Errorf("order not found: %s", orderID)
	}

	return order.Status, nil
}

// GetPositions returns current positions
func (b *SimulatedBroker) GetPositions() ([]domain.Position, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	result := make([]domain.Position, 0, len(b.positions))
	for _, position := range b.positions {
		result = append(result, position)
	}
	return result, nil
}

// GetAccountBalance returns account balance
func (b *SimulatedBroker) GetAccountBalance() (float64, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.balance, nil
}

// fillOrder simulates order filling
func (b *SimulatedBroker) fillOrder(orderID string) {
	order := b.orders[orderID]

	// Use order limit price or simulate a price
	fillPrice := order.LimitPrice
	if fillPrice == 0 {
		fillPrice = 100.0 // Default simulated price
	}

	// Add small random slippage
	slippage := (float64(order.ID[0]%10) - 5.0) / 1000.0
	fillPrice = fillPrice * (1 + slippage)
	fillPrice = math.Round(fillPrice*100) / 100

	order.Status = "filled"
	order.FilledQty = order.Quantity
	order.FillPrice = fillPrice
	b.orders[orderID] = order

	// Update positions
	position, exists := b.positions[order.Symbol]
	if !exists {
		position = domain.Position{Symbol: order.Symbol}
	}

	amount := fillPrice * order.Quantity
	commission := math.Max(amount*0.00025, 5.0)

	if order.Direction == domain.DirectionLong {
		position.Quantity += order.Quantity
		position.AvgCost = (position.AvgCost*(position.Quantity-order.Quantity) + amount) / position.Quantity
		b.balance -= amount + commission
	} else {
		position.Quantity -= order.Quantity
		if position.Quantity == 0 {
			position.AvgCost = 0
		}
		b.balance += amount - commission
	}

	b.positions[order.Symbol] = position
}

// SetPrice sets the simulated price for a symbol (for testing)
func (b *SimulatedBroker) SetPrice(symbol string, price float64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	position, exists := b.positions[symbol]
	if exists {
		position.CurrentPrice = price
		position.MarketValue = price * position.Quantity
		position.UnrealizedPnL = position.MarketValue - (position.AvgCost * position.Quantity)
		b.positions[symbol] = position
	}
}
