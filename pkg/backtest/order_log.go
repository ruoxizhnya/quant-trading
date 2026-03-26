package backtest

import (
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// OrderLog records all orders during a backtest run.
type OrderLog struct {
	orders []domain.Order
}

// Record adds an order to the log.
func (log *OrderLog) Record(o domain.Order) {
	log.orders = append(log.orders, o)
}

// GetOrders returns all recorded orders.
func (log *OrderLog) GetOrders() []domain.Order {
	return log.orders
}
