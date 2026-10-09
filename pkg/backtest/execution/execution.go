package execution

import (
	"fmt"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest/marketimpact"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/fees"
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
func (s *BacktestExecutionService) applySlippage(price float64, direction domain.Direction, quote Quote, orderQty float64) float64 {
	switch s.slippageModel {
	case "fixed":
		// Sprint 6 P1-22 (ODR-013): pulled the literal 0.001
		// out of this branch into fees.FixedSlippageRate so a
		// "what-if" sensitivity sweep can change the fixed
		// model rate in one place.
		slippage := fees.FixedSlippageRate
		if direction == domain.DirectionLong {
			return price * (1 + slippage)
		}
		return price * (1 - slippage)
	case "variable":
		// Variable slippage based on volatility (high-low range)
		volatility := (quote.High - quote.Low) / quote.Close
		slippage := volatility * 0.1
		if direction == domain.DirectionLong {
			return price * (1 + slippage)
		}
		return price * (1 - slippage)
	case "impact":
		// K4（D5）：平方根市场冲击模型——滑点随 orderQty/ADV 增大，
		// 使「大单滑点 > 小单滑点」成为可测事实。
		//
		// ADV 代理口径（裁决）：用当根 quote.Volume 近似 ADV。真实 ADV 是
		// 过去 N 日平均成交量，本切片不引入历史窗口状态（撮合服务无该
		// 状态），Volume 是 Q5 现状能拿到的最接近的量纲——局限见报告：
		// 单日量尖峰/地量都会让冲击被高估/低估。
		//
		// quote.Volume <= 0 → 无法估 ADV，**退化为 fixed 滑点**而非
		// 零冲击：静默返回零会让「无成交量数据」被误读成「无冲击、成本
		// 为零」，是危险的乐观偏差。退化到 fixed 至少保持与旧模型一致
		// 的保守 haircut。
		if quote.Volume <= 0 {
			slippage := fees.FixedSlippageRate
			if direction == domain.DirectionLong {
				return price * (1 + slippage)
			}
			return price * (1 - slippage)
		}
		model := marketimpact.MarketImpactModel{
			Sigma:           s.config.ImpactSigma,
			LiquidityFactor: s.config.ImpactLiquidityFactor,
		}
		impact := model.CalculateImpact(orderQty, quote.Volume)
		if direction == domain.DirectionLong {
			return price * (1 + impact)
		}
		return price * (1 - impact)
	case "none":
		return price
	default:
		return price
	}
}

// calculateCommission calculates trading commission
func (s *BacktestExecutionService) calculateCommission(amount float64) float64 {
	commission := amount * s.config.CommissionRate
	if commission < s.config.MinCommission {
		commission = s.config.MinCommission
	}
	return commission
}

func generateTradeID() string {
	return fmt.Sprintf("TRD-%d", time.Now().UnixNano())
}

// min and max are provided by Go 1.21+ builtins (P0-10).
