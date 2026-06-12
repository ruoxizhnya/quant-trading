// Package live provides interfaces and implementations for live trading execution.
// It abstracts broker-specific order management, position tracking, and account queries
// behind the LiveTrader interface, enabling seamless switching between paper trading
// (MockTrader) and real broker integrations.
//
// Architecture (Sprint 6 P1-26, ODR-022 — consolidated from 5 to 2 entities):
//   - LiveTrader: Core interface for order submission, cancellation, and account queries
//   - MockTrader: In-memory simulation with A-share trading rules (T+1, stamp tax, price limits).
//                 Supports optional OrderStore for persistence (replaces PersistentMockTrader).
//   - LiveEngine: Real-time quote-driven execution orchestrator for paper/live trading
//                 (uses Broker + DataFeed interfaces, separate from LiveTrader)
//
// Previously-deleted entities (see ODR-022 for details):
//   - PersistentMockTrader: merged into MockTrader via OrderStore config field
//   - AdvancedMockTrader:   unused (no production callers) — deleted
//   - AdvancedTrader:       unused interface (no production callers) — deleted
//
// Usage:
//
//	trader := live.NewMockTrader(live.MockTraderConfig{InitialCash: 1e6}, logger)
//	result, err := trader.SubmitOrder(ctx, "000001.SZ", domain.DirectionLong, domain.OrderTypeMarket, 100, 0)
//
// A-Share Specifics:
//   - T+1 settlement: shares bought today cannot be sold today
//   - Stamp tax: 0.1% on sell trades only
//   - Commission: min 5 CNY per trade
//   - Transfer fee: 0.001% of trade value
//   - Price limits: ±10% for normal stocks, ±5% for ST stocks
package live

import (
	"context"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// LiveTrader defines the core interface for live trading execution.
// All broker implementations (real or simulated) must satisfy this interface.
//
// Implementations must be safe for concurrent use by multiple goroutines.
type LiveTrader interface {
	// SubmitOrder submits a new order to the broker.
	// For market orders, set price to 0.
	// For limit orders, price is the limit price.
	// Returns the order result with assigned OrderID and fill status.
	SubmitOrder(ctx context.Context, symbol string, direction domain.Direction, orderType domain.OrderType, quantity float64, price float64) (*OrderResult, error)

	// CancelOrder attempts to cancel a pending or partially filled order.
	// Returns error if the order is already filled, cancelled, or not found.
	CancelOrder(ctx context.Context, orderID string) error

	// GetOrder retrieves the current status of an order by ID.
	GetOrder(ctx context.Context, orderID string) (*OrderResult, error)

	// GetPositions returns all current positions in the account.
	GetPositions(ctx context.Context) ([]PositionInfo, error)

	// GetAccount returns current account summary (cash, market value, PnL).
	GetAccount(ctx context.Context) (*AccountInfo, error)

	// Name returns the trader implementation name (e.g., "mock_trader", "interactive_brokers").
	Name() string

	// HealthCheck verifies connectivity to the broker/exchange.
	// Returns nil if healthy, error otherwise.
	HealthCheck(ctx context.Context) error

	// EmergencyFlatten closes all open positions at the best
	// available market price. This is intended for the kill-switch
	// path — operator presses the EMERGENCY FLATTEN button on the
	// dashboard, the server issues market orders for every held
	// symbol, and the portfolio returns to 100% cash within seconds.
	//
	// Unlike SubmitOrder(Sell), EmergencyFlatten bypasses T+1
	// restrictions: positions bought today are force-closed and
	// tracked with `bypassed_t1: true` in the result so the
	// audit trail records the operator override. The reason is
	// recorded for compliance review.
	//
	// Implementations should be idempotent: calling EmergencyFlatten
	// on a flat portfolio returns an empty result, not an error.
	// Failures to close a single symbol do not abort the whole call;
	// each symbol's outcome is reported in the result.
	EmergencyFlatten(ctx context.Context, reason string) (*EmergencyFlattenResult, error)
}

// EmergencyFlattenResult reports the outcome of an EmergencyFlatten
// call. Sold and Skipped are mutually exclusive — a symbol appears
// in exactly one of them.
type EmergencyFlattenResult struct {
	// Sold is the list of positions that were successfully closed
	// during the emergency flatten. Each entry corresponds to one
	// order that was submitted and filled.
	Sold []EmergencyFlattenOrder `json:"sold"`

	// Skipped is the list of positions that could not be closed
	// (e.g. broker rejected the order, price feed unavailable).
	// These are reported to the operator for manual follow-up.
	Skipped []EmergencyFlattenSkip `json:"skipped"`

	// SoldTotal is the sum of Sold[i].NetProceeds, in CNY. Provided
	// for at-a-glance display in the UI without forcing callers to
	// sum the slice themselves.
	SoldTotal float64 `json:"sold_total"`

	// StartedAt and CompletedAt bracket the flatten call. Both are
	// UTC. CompletedAt - StartedAt is the wall-clock latency the
	// operator should expect; in production with N symbols this is
	// usually < 1s.
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`

	// Reason is the operator-supplied explanation that was passed
	// to EmergencyFlatten. Persisted with each order for audit.
	Reason string `json:"reason"`
}

// EmergencyFlattenOrder describes a single successful close in an
// EmergencyFlatten call.
type EmergencyFlattenOrder struct {
	Symbol       string    `json:"symbol"`
	OrderID      string    `json:"order_id"`
	Quantity     float64   `json:"quantity"`
	FillPrice    float64   `json:"fill_price"`
	NetProceeds  float64   `json:"net_proceeds"`
	BypassedT1   bool      `json:"bypassed_t1"` // true if T+1 was overridden
	SubmittedAt  time.Time `json:"submitted_at"`
}

// EmergencyFlattenSkip describes a single failed close in an
// EmergencyFlatten call. The portfolio retains the position; the
// operator must intervene manually.
type EmergencyFlattenSkip struct {
	Symbol   string `json:"symbol"`
	Quantity float64 `json:"quantity"`
	Reason   string `json:"reason"`
}

// OrderResult represents the outcome of an order submission.
type OrderResult struct {
	OrderID     string           `json:"order_id"`
	Symbol      string           `json:"symbol"`
	Direction   domain.Direction `json:"direction"`
	OrderType   domain.OrderType `json:"order_type"`
	Quantity    float64          `json:"quantity"`
	FilledQty   float64          `json:"filled_qty"`
	Price       float64          `json:"price"`
	Status      string           `json:"status"` // "pending" / "filled" / "partial" / "cancelled" / "rejected" / "expired"
	SubmittedAt time.Time        `json:"submitted_at"`
	Message     string           `json:"message,omitempty"` // error or informational message
}

// AccountInfo represents a snapshot of the trading account.
type AccountInfo struct {
	TotalAssets   float64   `json:"total_assets"`
	Cash          float64   `json:"cash"`
	MarketValue   float64   `json:"market_value"`
	UnrealizedPnL float64   `json:"unrealized_pnl"`
	RealizedPnL   float64   `json:"realized_pnl"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// PositionInfo represents a single position in the portfolio.
type PositionInfo struct {
	Symbol            string  `json:"symbol"`
	Quantity          float64 `json:"quantity"`
	AvailableQty      float64 `json:"available_qty"`
	AvgCost           float64 `json:"avg_cost"`
	CurrentPrice      float64 `json:"current_price"`
	MarketValue       float64 `json:"market_value"`
	UnrealizedPnL     float64 `json:"unrealized_pnl"`
	QuantityToday     float64 `json:"quantity_today"`
	QuantityYesterday float64 `json:"quantity_yesterday"`
}

// IsFilled returns true if the order is completely filled.
func (r *OrderResult) IsFilled() bool {
	return r.Status == "filled" && r.FilledQty >= r.Quantity
}

// IsActive returns true if the order can still be filled or cancelled.
func (r *OrderResult) IsActive() bool {
	return r.Status == "pending" || r.Status == "partial"
}

// TotalFees returns the estimated total fees for a trade (commission + transfer fee + stamp tax).
// This is a convenience method for quick fee estimation.
func (r *OrderResult) TotalFees(commissionRate, stampTaxRate, transferFeeRate float64) float64 {
	tradeValue := r.FilledQty * r.Price
	commission := tradeValue * commissionRate
	if commission < 5.0 {
		commission = 5.0
	}
	transferFee := tradeValue * transferFeeRate
	stampTax := 0.0
	if r.Direction == domain.DirectionClose || r.Direction == domain.DirectionShort {
		stampTax = tradeValue * stampTaxRate
	}
	return commission + transferFee + stampTax
}
