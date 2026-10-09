package live

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/execution"
	"github.com/ruoxizhnya/quant-trading/pkg/fees"
)

// SimulatedBroker implements a paper trading broker for testing
type SimulatedBroker struct {
	mu         sync.RWMutex
	connected  bool
	orders     map[string]domain.Order
	positions  map[string]domain.Position
	balance    float64
	orderCount int
	// fees is the A-share fee schedule used for commission calculation.
	// S7-P0-5 (ODR-043): previously the commission rate (0.025%) and
	// minimum (¥5) were hardcoded literals that diverged from pkg/fees
	// (regulatory 0.03% / ¥5) and from MockTrader. Now sourced from
	// the shared fees package so backtest / paper-trading / live stay
	// in sync when rates change.
	fees fees.AShareFees

	// costModel 是共享执行成本核（K5 切片 1）。
	//
	// 改动前 fillOrder 用伪随机滑点 `(order.ID[0]%10 - 5.0)/1000.0`：
	// 它既不可复现（取决于订单 ID 首字符），又与回测/paper 的成本模型
	// 完全不同构。现改为走 pkg/execution —— 与回测撮合、MockTrader
	// 同一段代码，滑点由 config.SlippageModel 决定。
	costModel *execution.CostModel
}

// NewSimulatedBroker creates a new simulated broker
func NewSimulatedBroker(initialBalance float64) *SimulatedBroker {
	defaultFees := fees.DefaultAShareFees()
	return &SimulatedBroker{
		orders:    make(map[string]domain.Order),
		positions: make(map[string]domain.Position),
		balance:   initialBalance,
		fees:      defaultFees,
		costModel: execution.NewCostModel(domain.DefaultExecutionConfig()),
	}
}

// SetExecutionConfig 替换本 broker 的执行成本配置（K5 切片 1）。
//
// 默认是 domain.DefaultExecutionConfig()（SlippageModel = "fixed"）。
// 需要冲击模型时传入 SlippageModel="impact" + ImpactSigma 即可，与回测
// 用同一份 ExecutionConfig 类型。本方法替换的是**成本模型**，不影响费用
// 费率（fees 字段）。
func (b *SimulatedBroker) SetExecutionConfig(cfg domain.ExecutionConfig) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.costModel = execution.NewCostModel(cfg)
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

	// K5 切片 1：用共享成本核取代旧的伪随机滑点。SimulatedBroker 没有
	// bar（无 high/low/volume），故 high=low=fillPrice、adv=0：
	//   - "fixed" 模型不受影响；
	//   - "impact" 因 adv=0 按文档退化为 fixed（不静默零冲击）。
	fillPrice = b.costModel.SlippagePrice(fillPrice, order.Direction, order.Quantity, 0, fillPrice, fillPrice)
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
	// S7-P0-5 (ODR-043): source commission rate and minimum from the
	// shared fees package instead of hardcoded literals, keeping
	// SimulatedBroker consistent with MockTrader and the backtest engine.
	commission := math.Max(amount*b.fees.CommissionRate, b.fees.MinCommission)

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
