package live

import (
	"context"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ============================================================
// Enhanced LiveTrader Interface (v2)
// Extends basic LiveTrader with advanced trading features
// ============================================================

type AdvancedTrader interface {
	// --- Inherited from LiveTrader ---
	LiveTrader

	// --- Batch Operations ---
	SubmitOrders(ctx context.Context, orders []OrderRequest) (*BatchOrderResult, error)
	CancelAllOrders(ctx context.Context, symbol string) (int, error)

	// --- Query Operations ---
	ListOrders(ctx context.Context, filter OrderFilter) ([]*OrderResult, int, error)
	GetTrades(ctx context.Context, orderID string) ([]TradeRecord, error)
	ListTodayTrades(ctx context.Context) ([]TradeRecord, error)

	// --- Position Queries ---
	GetPosition(ctx context.Context, symbol string) (*PositionDetail, error)
	GetAvailableQuantity(ctx context.Context, symbol string) (float64, error)

	// --- Account & Cash Flow ---
	GetCashFlow(ctx context.Context, startTime, endTime *time.Time) ([]CashFlow, error)
	GetFrozenCash(ctx context.Context) (float64, error)

	// --- Market Data ---
	SubscribeQuotes(ctx context.Context, symbols []string) (<-chan MarketData, error)
	UnsubscribeQuotes(symbols []string) error
	GetQuote(ctx context.Context, symbol string) (*MarketData, error)

	// --- Connection Management ---
	Connect(ctx context.Context) error
	Disconnect() error
	GetConnectionStatus() ConnectionStatus

	// --- Risk Management ---
	CheckMargin(ctx context.Context, symbol string, direction domain.Direction, quantity float64) (*MarginCheckResult, error)
}

// OrderRequest for batch submission
type OrderRequest struct {
	Symbol    string           `json:"symbol"`
	Direction domain.Direction `json:"direction"`
	OrderType domain.OrderType `json:"order_type"`
	Quantity  float64          `json:"quantity"`
	Price     float64          `json:"price"` // 0 for market order
}

// MarginCheckResult represents margin sufficiency check result
type MarginCheckResult struct {
	Sufficient    bool    `json:"sufficient"`
	RequiredCash  float64 `json:"required_cash"`
	AvailableCash float64 `json:"available_cash"`
	Shortfall     float64 `json:"shortfall"`
	Message       string  `json:"message"`
}
