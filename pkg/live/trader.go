package live

import (
	"context"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

type OrderResult struct {
	OrderID    string    `json:"order_id"`
	Symbol     string    `json:"symbol"`
	Direction  domain.Direction `json:"direction"`
	OrderType  domain.OrderType `json:"order_type"`
	Quantity   float64   `json:"quantity"`
	FilledQty  float64   `json:"filled_qty"`
	Price      float64   `json:"price"`
	Status     string    `json:"status"`
	SubmittedAt time.Time `json:"submitted_at"`
	Message    string    `json:"message,omitempty"`
}

type AccountInfo struct {
	TotalAssets  float64   `json:"total_assets"`
	Cash         float64   `json:"cash"`
	MarketValue  float64   `json:"market_value"`
	UnrealizedPnL float64  `json:"unrealized_pnl"`
	RealizedPnL  float64   `json:"realized_pnl"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type PositionInfo struct {
	Symbol           string    `json:"symbol"`
	Quantity         float64   `json:"quantity"`
	AvailableQty     float64   `json:"available_qty"`
	AvgCost          float64   `json:"avg_cost"`
	CurrentPrice     float64   `json:"current_price"`
	MarketValue      float64   `json:"market_value"`
	UnrealizedPnL    float64   `json:"unrealized_pnl"`
	QuantityToday    float64   `json:"quantity_today"`
	QuantityYesterday float64  `json:"quantity_yesterday"`
}

type LiveTrader interface {
	SubmitOrder(ctx context.Context, symbol string, direction domain.Direction, orderType domain.OrderType, quantity float64, price float64) (*OrderResult, error)
	CancelOrder(ctx context.Context, orderID string) error
	GetOrder(ctx context.Context, orderID string) (*OrderResult, error)
	GetPositions(ctx context.Context) ([]PositionInfo, error)
	GetAccount(ctx context.Context) (*AccountInfo, error)
	Name() string
	HealthCheck(ctx context.Context) error
}
