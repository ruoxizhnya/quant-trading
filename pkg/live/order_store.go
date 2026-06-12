package live

import (
	"context"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

type OrderStatus string

const (
	OrderStatusPending  OrderStatus = "pending"
	OrderStatusFilled   OrderStatus = "filled"
	OrderStatusCancelled OrderStatus = "cancelled"
	OrderStatusRejected OrderStatus = "rejected"
	OrderStatusPartial  OrderStatus = "partial"
)

type OrderRecord struct {
	OrderID     string      `json:"order_id" db:"order_id"`
	Symbol      string      `json:"symbol" db:"symbol"`
	Direction   string      `json:"direction" db:"direction"`
	OrderType   string      `json:"order_type" db:"order_type"`
	Quantity    float64     `json:"quantity" db:"quantity"`
	FilledQty   float64     `json:"filled_qty" db:"filled_qty"`
	Price       float64     `json:"price" db:"price"`
	AvgFillPrice float64    `json:"avg_fill_price" db:"avg_fill_price"`
	Status      OrderStatus `json:"status" db:"status"`
	SubmittedAt int64       `json:"submitted_at" db:"submitted_at"`
	UpdatedAt   int64       `json:"updated_at" db:"updated_at"`
	Message     string      `json:"message,omitempty" db:"message"`
}

type OrderStore interface {
	Save(ctx context.Context, order *OrderRecord) error
	Get(ctx context.Context, orderID string) (*OrderRecord, error)
	List(ctx context.Context, symbol string, status OrderStatus) ([]*OrderRecord, error)
	Update(ctx context.Context, orderID string, updates map[string]interface{}) error
	Delete(ctx context.Context, orderID string) error
}

// convertToOrderResult converts a persistence OrderRecord to the in-memory
// OrderResult type used by MockTrader and the HTTP API.
//
// Moved here from the now-deleted `persistent_mock_trader.go` (Sprint 6
// P1-26, ODR-022) so both `order_store_test.go` and any future adapter
// can reuse the conversion without depending on the mock trader.
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
