package live

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// OrderManager manages order lifecycle
type OrderManager struct {
	broker  Broker
	config  domain.ExecutionConfig
	orders  map[string]domain.Order
	trades  []domain.Trade
	pending []string
	mu      sync.RWMutex
}

// NewOrderManager creates a new order manager
func NewOrderManager(broker Broker, config domain.ExecutionConfig) *OrderManager {
	return &OrderManager{
		broker:  broker,
		config:  config,
		orders:  make(map[string]domain.Order),
		trades:  make([]domain.Trade, 0),
		pending: make([]string, 0),
	}
}

// Run starts the order manager's background tasks
func (om *OrderManager) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			om.syncOrderStatus()
		}
	}
}

// SubmitOrder submits a new order
func (om *OrderManager) SubmitOrder(order domain.Order) (string, error) {
	if err := validateOrderShape(&order); err != nil {
		return "", err
	}
	order.ID = generateOrderID()
	order.Status = "pending"
	order.Timestamp = time.Now()

	brokerID, err := om.broker.SubmitOrder(order)
	if err != nil {
		order.Status = "rejected"
		om.saveOrder(order)
		return "", fmt.Errorf("failed to submit order: %w", err)
	}

	if brokerID != "" {
		order.ID = brokerID
	}

	order.Status = "submitted"
	om.saveOrder(order)
	om.addPending(order.ID)

	return order.ID, nil
}

// CancelOrder cancels an order
func (om *OrderManager) CancelOrder(orderID string) error {
	order, exists := om.GetOrder(orderID)
	if !exists {
		return fmt.Errorf("order not found: %s", orderID)
	}

	if order.Status != "pending" && order.Status != "submitted" {
		return fmt.Errorf("cannot cancel order with status: %s", order.Status)
	}

	if err := om.broker.CancelOrder(orderID); err != nil {
		return fmt.Errorf("failed to cancel order: %w", err)
	}

	order.Status = "cancelled"
	om.saveOrder(order)
	om.removePending(orderID)

	return nil
}

// GetOrder returns an order by ID
func (om *OrderManager) GetOrder(orderID string) (domain.Order, bool) {
	om.mu.RLock()
	defer om.mu.RUnlock()
	order, exists := om.orders[orderID]
	return order, exists
}

// GetOrders returns all orders
func (om *OrderManager) GetOrders() []domain.Order {
	om.mu.RLock()
	defer om.mu.RUnlock()

	result := make([]domain.Order, 0, len(om.orders))
	for _, order := range om.orders {
		result = append(result, order)
	}
	return result
}

// GetPendingOrders returns pending orders
func (om *OrderManager) GetPendingOrders() []domain.Order {
	om.mu.RLock()
	defer om.mu.RUnlock()

	result := make([]domain.Order, 0)
	for _, id := range om.pending {
		if order, exists := om.orders[id]; exists {
			result = append(result, order)
		}
	}
	return result
}

// UpdateOrderStatus updates order status
func (om *OrderManager) UpdateOrderStatus(orderID string, status string) {
	om.mu.Lock()
	defer om.mu.Unlock()

	if order, exists := om.orders[orderID]; exists {
		order.Status = status
		om.orders[orderID] = order

		if status == "filled" || status == "cancelled" || status == "rejected" {
			om.removePendingLocked(orderID)
		}
	}
}

// UpdateOrder replaces an order's record in place. Used by the engine to
// persist running state on the order (e.g. trailing-stop HWM) so that
// subsequent reads see the updated snapshot.
//
// P1-3 (ODR-016) — added for LiveEngine trailing-stop bookkeeping.
func (om *OrderManager) UpdateOrder(orderID string, order *domain.Order) {
	if order == nil {
		return
	}
	om.mu.Lock()
	defer om.mu.Unlock()
	// Preserve identity by aligning the map key with the supplied ID.
	aligned := *order
	if aligned.ID == "" {
		aligned.ID = orderID
	}
	om.orders[orderID] = aligned
}

func (om *OrderManager) saveOrder(order domain.Order) {
	om.mu.Lock()
	defer om.mu.Unlock()
	om.orders[order.ID] = order
}

func (om *OrderManager) addPending(orderID string) {
	om.mu.Lock()
	defer om.mu.Unlock()
	om.pending = append(om.pending, orderID)
}

func (om *OrderManager) removePending(orderID string) {
	om.mu.Lock()
	defer om.mu.Unlock()
	om.removePendingLocked(orderID)
}

func (om *OrderManager) removePendingLocked(orderID string) {
	for i, id := range om.pending {
		if id == orderID {
			om.pending = append(om.pending[:i], om.pending[i+1:]...)
			break
		}
	}
}

func (om *OrderManager) syncOrderStatus() {
	om.mu.RLock()
	pending := make([]string, len(om.pending))
	copy(pending, om.pending)
	om.mu.RUnlock()

	for _, orderID := range pending {
		status, err := om.broker.GetOrderStatus(orderID)
		if err != nil {
			continue
		}
		om.UpdateOrderStatus(orderID, status)
	}
}

// GetTrades returns all executed trades
func (om *OrderManager) GetTrades() []domain.Trade {
	om.mu.RLock()
	defer om.mu.RUnlock()

	result := make([]domain.Trade, len(om.trades))
	copy(result, om.trades)
	return result
}

// AddTrade adds a trade to the trade history
func (om *OrderManager) AddTrade(trade domain.Trade) {
	om.mu.Lock()
	defer om.mu.Unlock()
	om.trades = append(om.trades, trade)
}

func generateOrderID() string {
	return fmt.Sprintf("ORD-%d", time.Now().UnixNano())
}

// validateOrderShape enforces the type-specific invariants of an Order
// before it is forwarded to the broker. Returns an error if the order
// is missing a required trigger field (e.g. LimitPrice for a limit order).
//
// P1-3 (ODR-016) — added so misconfigured limit / stop / trailing orders
// are rejected at submission time rather than silently sitting in
// pending without ever filling.
func validateOrderShape(order *domain.Order) error {
	if order == nil {
		return fmt.Errorf("nil order")
	}
	if order.Symbol == "" {
		return fmt.Errorf("order symbol is required")
	}
	if order.Quantity <= 0 {
		return fmt.Errorf("order quantity must be positive, got %v", order.Quantity)
	}
	switch order.OrderType {
	case domain.OrderTypeLimit:
		if order.LimitPrice <= 0 {
			return fmt.Errorf("limit order requires LimitPrice > 0")
		}
	case domain.OrderTypeStop:
		if order.StopPrice <= 0 {
			return fmt.Errorf("stop order requires StopPrice > 0")
		}
	case domain.OrderTypeTrailing:
		if order.TrailAmount <= 0 && order.TrailPercent <= 0 {
			return fmt.Errorf("trailing order requires TrailAmount or TrailPercent > 0")
		}
	}
	return nil
}
