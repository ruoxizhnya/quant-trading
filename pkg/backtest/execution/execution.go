package execution

import (
	"fmt"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	costexec "github.com/ruoxizhnya/quant-trading/pkg/execution"
)

// ExecutionService handles order execution in backtests.
// This abstraction allows the backtest engine to work with both
// simulated and (eventually) live execution.
type ExecutionService interface {
	// ExecuteOrder processes an order and returns the resulting trade
	ExecuteOrder(order domain.Order, quote Quote) (domain.Trade, error)

	// GetSlippageModel returns the current slippage model name
	GetSlippageModel() string

	// SetSlippageModel sets the slippage model
	SetSlippageModel(model string)
}

// Quote represents a price quote for execution
type Quote struct {
	Symbol string
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
	Date   time.Time
}

// BacktestExecutionService implements ExecutionService for backtesting
type BacktestExecutionService struct {
	config        domain.ExecutionConfig
	slippageModel string
}

// NewBacktestExecutionService creates a new backtest execution service
func NewBacktestExecutionService(config domain.ExecutionConfig) *BacktestExecutionService {
	return &BacktestExecutionService{
		config:        config,
		slippageModel: config.SlippageModel,
	}
}

// ExecuteOrder executes an order against a quote
func (s *BacktestExecutionService) ExecuteOrder(order domain.Order, quote Quote) (domain.Trade, error) {
	if order.Quantity <= 0 {
		return domain.Trade{}, fmt.Errorf("invalid order quantity: %f", order.Quantity)
	}

	// Determine execution price based on order type and direction
	var executionPrice float64

	switch order.OrderType {
	case domain.OrderTypeMarket:
		executionPrice = s.applySlippage(quote.Close, order.Direction, quote, order.Quantity)
	case domain.OrderTypeLimit:
		if order.LimitPrice <= 0 {
			return domain.Trade{}, fmt.Errorf("invalid limit price: %f", order.LimitPrice)
		}
		// For buy orders, execute if limit price >= low
		// For sell orders, execute if limit price <= high
		if order.Direction == domain.DirectionLong && order.LimitPrice >= quote.Low {
			executionPrice = min(order.LimitPrice, quote.High)
		} else if order.Direction == domain.DirectionShort && order.LimitPrice <= quote.High {
			executionPrice = max(order.LimitPrice, quote.Low)
		} else {
			return domain.Trade{}, fmt.Errorf("limit price not reached")
		}
	default:
		return domain.Trade{}, fmt.Errorf("unsupported order type: %s", order.OrderType)
	}

	if executionPrice <= 0 {
		return domain.Trade{}, fmt.Errorf("invalid execution price: %f", executionPrice)
	}

	// Calculate commission
	amount := executionPrice * order.Quantity
	commission := s.calculateCommission(amount)

	trade := domain.Trade{
		ID:         generateTradeID(),
		Symbol:     order.Symbol,
		Direction:  order.Direction,
		Quantity:   order.Quantity,
		Price:      executionPrice,
		Commission: commission,
		Timestamp:  quote.Date,
	}

	return trade, nil
}

// GetSlippageModel returns the current slippage model
func (s *BacktestExecutionService) GetSlippageModel() string {
	return s.slippageModel
}

// SetSlippageModel sets the slippage model
func (s *BacktestExecutionService) SetSlippageModel(model string) {
	s.slippageModel = model
}

// applySlippage applies slippage to the execution price.
//
// orderQty was added by K4 so the "impact" branch can size impact against
// the order's participation in volume. The existing "fixed" / "variable" /
// "none" branches ignore it and are byte-for-byte unchanged.
//
// K5 切片 1：函数体改为委托 pkg/execution 的**共享成本核**——paper 侧
// （pkg/live）走的是同一段代码，回测与 paper 的滑点公式从此不可能漂移。
// 这里把可变的 s.slippageModel 覆盖进 config 再构造 core，以保留
// SetSlippageModel 的既有语义（s.slippageModel 可能已不同于
// config.SlippageModel）。数值行为逐位不变。
func (s *BacktestExecutionService) applySlippage(price float64, direction domain.Direction, quote Quote, orderQty float64) float64 {
	cfg := s.config
	cfg.SlippageModel = s.slippageModel
	core := costexec.NewCostModel(cfg)
	// quote.Volume 作 ADV 代理（K4 裁决，见 pkg/execution 的 SlippagePrice）。
	return core.SlippagePrice(price, direction, orderQty, quote.Volume, quote.High, quote.Low)
}

// calculateCommission calculates trading commission.
//
// K5 切片 1：委托 pkg/execution 的共享成本核——回测与 paper 用同一条
// max(notional*CommissionRate, MinCommission) 公式。
func (s *BacktestExecutionService) calculateCommission(amount float64) float64 {
	return costexec.NewCostModel(s.config).Commission(amount)
}

func generateTradeID() string {
	return fmt.Sprintf("TRD-%d", time.Now().UnixNano())
}

// min and max are provided by Go 1.21+ builtins (P0-10).
