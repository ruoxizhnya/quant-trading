package live

import (
	"time"
)

// ============================================================
// Enhanced Trading Types (New additions)
// ============================================================

// TradeRecord represents a completed trade execution
type TradeRecord struct {
	TradeID   string    `json:"trade_id"`
	OrderID   string    `json:"order_id"`
	Symbol    string    `json:"symbol"`
	Direction string    `json:"direction"` // "buy" or "sell"
	Quantity  float64   `json:"quantity"`
	Price     float64   `json:"price"`
	Fee       float64   `json:"fee"`
	TradeTime time.Time `json:"trade_time"`
}

// OrderFilter for listing orders with criteria
type OrderFilter struct {
	Symbol    string      `json:"symbol,omitempty"`
	Status    OrderStatus `json:"status,omitempty"`
	Direction string      `json:"direction,omitempty"`
	StartTime *time.Time  `json:"start_time,omitempty"`
	EndTime   *time.Time  `json:"end_time,omitempty"`
	Limit     int         `json:"limit,omitempty"`
	Offset    int         `json:"offset,omitempty"`
}

// BatchOrderResult contains results of batch order submission
type BatchOrderResult struct {
	Successes []*OrderResult `json:"successes"`
	Failures  []BatchError   `json:"failures"`
}

type BatchError struct {
	Symbol  string `json:"symbol"`
	Error   string `json:"error"`
	OrderID string `json:"order_id,omitempty"`
}

// MarketData represents real-time market quote
type MarketData struct {
	Symbol     string    `json:"symbol"`
	LastPrice  float64   `json:"last_price"`
	Open       float64   `json:"open"`
	High       float64   `json:"high"`
	Low        float64   `json:"low"`
	PrevClose  float64   `json:"prev_close"`
	Volume     int64     `json:"volume"`
	Turnover   float64   `json:"turnover"`
	BidPrice   []float64 `json:"bid_price"` // bid prices (up to 5 levels)
	AskPrice   []float64 `json:"ask_price"` // ask prices (up to 5 levels)
	BidVolume  []int64   `json:"bid_volume"` // bid volumes
	AskVolume  []int64   `json:"ask_volume"` // ask volumes
	UpdateTime time.Time `json:"update_time"`
}

// ConnectionStatus represents trader connection state
type ConnectionStatus struct {
	Connected    bool      `json:"connected"`
	ServerTime   time.Time `json:"server_time"`
	LoginTime    time.Time `json:"login_time"`
	LastPing     time.Time `json:"last_ping"`
	LatencyMs    int64     `json:"latency_ms"`
	ErrorMessage string    `json:"error_message,omitempty"`
}

// PositionDetail extends PositionInfo with additional trading info
type PositionDetail struct {
	PositionInfo
	CanBuyQty   float64 `json:"can_buy_qty"`   // max buyable quantity with available cash
	CanSellQty  float64 `json:"can_sell_qty"`  // sellable quantity after T+1 check
	ProfitRatio float64 `json:"profit_ratio"`  // profit/cost ratio in percentage
	DaysHeld    int     `json:"days_held"`     // number of days position held
}

// CashFlow represents a cash movement record
type CashFlow struct {
	ID          int64     `json:"id"`
	Type        string    `json:"type"`        // "deposit", "withdraw", "trade_fee", "dividend", etc.
	Amount      float64   `json:"amount"`
	Balance     float64   `json:"balance"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}
