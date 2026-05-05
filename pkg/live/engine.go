package live

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// LiveEngine manages live trading execution
type LiveEngine struct {
	broker          Broker
	orderManager    *OrderManager
	positionManager *PositionManager
	dataFeed        DataFeed

	portfolio *domain.Portfolio
	config    domain.ExecutionConfig

	mu      sync.RWMutex
	running bool
	stopCh  chan struct{}

	onOrderUpdate func(order domain.Order)
	onTrade       func(trade domain.Trade)
	onError       func(err error)
}

// Broker interface for order execution
type Broker interface {
	Connect() error
	Disconnect() error
	SubmitOrder(order domain.Order) (string, error)
	CancelOrder(orderID string) error
	GetOrderStatus(orderID string) (string, error)
	GetPositions() ([]domain.Position, error)
	GetAccountBalance() (float64, error)
}

// DataFeed interface for real-time market data
type DataFeed interface {
	Subscribe(symbols []string) error
	Unsubscribe(symbols []string) error
	GetQuote(symbol string) (Quote, error)
	SetCallback(callback func(Quote))
}

// Quote represents a real-time market quote
type Quote struct {
	Symbol    string    `json:"symbol"`
	Timestamp time.Time `json:"timestamp"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	Volume    int64     `json:"volume"`
	Bid       float64   `json:"bid"`
	Ask       float64   `json:"ask"`
}

// NewLiveEngine creates a new live trading engine
func NewLiveEngine(
	broker Broker,
	dataFeed DataFeed,
	config domain.ExecutionConfig,
) *LiveEngine {
	return &LiveEngine{
		broker:          broker,
		dataFeed:        dataFeed,
		orderManager:    NewOrderManager(broker, config),
		positionManager: NewPositionManager(),
		config:          config,
		portfolio: &domain.Portfolio{
			Cash:       config.InitialCapital,
			Positions:  make(map[string]domain.Position),
			TotalValue: config.InitialCapital,
		},
		stopCh: make(chan struct{}),
	}
}

// SetCallbacks sets the event callbacks
func (e *LiveEngine) SetCallbacks(
	onOrderUpdate func(order domain.Order),
	onTrade func(trade domain.Trade),
	onError func(err error),
) {
	e.onOrderUpdate = onOrderUpdate
	e.onTrade = onTrade
	e.onError = onError
}

// Start starts the live trading engine
func (e *LiveEngine) Start(ctx context.Context, symbols []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return fmt.Errorf("engine already running")
	}

	if err := e.broker.Connect(); err != nil {
		return fmt.Errorf("failed to connect to broker: %w", err)
	}

	positions, err := e.broker.GetPositions()
	if err != nil {
		return fmt.Errorf("failed to get positions: %w", err)
	}
	for _, pos := range positions {
		e.positionManager.UpdatePosition(pos)
	}

	e.dataFeed.SetCallback(e.handleQuote)

	if err := e.dataFeed.Subscribe(symbols); err != nil {
		return fmt.Errorf("failed to subscribe to symbols: %w", err)
	}

	e.running = true
	go e.orderManager.Run(ctx)
	go e.run(ctx)

	return nil
}

// Stop stops the live trading engine
func (e *LiveEngine) Stop(symbols []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return nil
	}

	close(e.stopCh)
	e.running = false

	if err := e.dataFeed.Unsubscribe(symbols); err != nil {
		return fmt.Errorf("failed to unsubscribe: %w", err)
	}

	if err := e.broker.Disconnect(); err != nil {
		return fmt.Errorf("failed to disconnect from broker: %w", err)
	}

	return nil
}

// IsRunning returns whether the engine is running
func (e *LiveEngine) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

// GetPortfolio returns the current portfolio
func (e *LiveEngine) GetPortfolio() *domain.Portfolio {
	return e.portfolio
}

// GetPositions returns current positions
func (e *LiveEngine) GetPositions() []domain.Position {
	return e.positionManager.GetPositions()
}

// GetOrders returns all orders
func (e *LiveEngine) GetOrders() []domain.Order {
	return e.orderManager.GetOrders()
}

// SubmitOrder submits a new order through the order manager
func (e *LiveEngine) SubmitOrder(order domain.Order) (string, error) {
	return e.orderManager.SubmitOrder(order)
}

// GetOrder returns a specific order by ID
func (e *LiveEngine) GetOrder(orderID string) (domain.Order, bool) {
	return e.orderManager.GetOrder(orderID)
}

// CancelOrder cancels a pending order
func (e *LiveEngine) CancelOrder(orderID string) error {
	return e.orderManager.CancelOrder(orderID)
}

// GetTrades returns all executed trades
func (e *LiveEngine) GetTrades() []domain.Trade {
	return e.orderManager.GetTrades()
}

func (e *LiveEngine) run(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.updatePortfolio()
		}
	}
}

func (e *LiveEngine) handleQuote(quote Quote) {
	orders := e.orderManager.GetPendingOrders()
	for _, order := range orders {
		if order.Symbol == quote.Symbol {
			e.tryFillOrder(order, quote)
		}
	}
}

func (e *LiveEngine) tryFillOrder(order domain.Order, quote Quote) {
	if order.OrderType == domain.OrderTypeMarket {
		fillPrice := quote.Close
		if order.Direction == domain.DirectionLong {
			fillPrice = quote.Ask
		} else {
			fillPrice = quote.Bid
		}

		trade := domain.Trade{
			ID:         generateID(),
			Symbol:     order.Symbol,
			Direction:  order.Direction,
			Quantity:   order.Quantity,
			Price:      fillPrice,
			Commission: calculateCommission(fillPrice*order.Quantity, e.config),
			Timestamp:  time.Now(),
		}

		e.orderManager.UpdateOrderStatus(order.ID, "filled")
		e.positionManager.UpdateFromTrade(trade)

		if e.onTrade != nil {
			e.onTrade(trade)
		}
	}
}

func (e *LiveEngine) updatePortfolio() {
	positions := e.positionManager.GetPositions()
	for i := range positions {
		quote, err := e.dataFeed.GetQuote(positions[i].Symbol)
		if err != nil {
			continue
		}
		positions[i].CurrentPrice = quote.Close
		positions[i].MarketValue = quote.Close * positions[i].Quantity
		positions[i].UnrealizedPnL = positions[i].MarketValue - (positions[i].AvgCost * positions[i].Quantity)
	}
}

func calculateCommission(amount float64, config domain.ExecutionConfig) float64 {
	commission := amount * config.CommissionRate
	if commission < config.MinCommission {
		commission = config.MinCommission
	}
	return commission
}

func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
