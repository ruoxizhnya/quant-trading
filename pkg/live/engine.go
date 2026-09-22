package live

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
)

// LiveEngine manages live trading execution
type LiveEngine struct {
	broker          Broker
	orderManager    *OrderManager
	positionManager *PositionManager
	dataFeed        DataFeed

	// AUD-31（2026-09-22）：这里原有一个 `portfolio *domain.Portfolio`
	// 字段，从构造之后**从未被写入过** —— 于是 GetPortfolio() 恒返回
	// 「初始资金 + 空持仓」，现金也从不随成交增减。它的唯一消费方是
	// /api/paper/* 那批从未挂载的端点（AUD-19 已整条删除）。
	// 组合状态的真实来源是 positionManager（AUD-16 起 mark-to-market
	// 会真的写回），所以这个字段连同 GetPortfolio() 一并删掉，
	// 而不是留着一个永远说谎的读数。
	config domain.ExecutionConfig

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

// Quote represents a real-time market quote.
//
// S7-P1-2 (ODR-043): Quote now lives in pkg/marketdata (the lower layer).
// This alias preserves backward compatibility for all existing `live.Quote`
// references. The canonical definition is marketdata.Quote.
type Quote = marketdata.Quote

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
		stopCh:          make(chan struct{}),
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

// Stop stops the live trading engine.
//
// Sprint 6 P0-2 (CQ-009): the previous implementation held e.mu across
// Unsubscribe() and Disconnect(), which are network I/O. A misbehaving
// broker or data feed could block the caller's goroutine for an
// arbitrary duration while every other LiveEngine accessor (Start,
// IsRunning, etc.) waits on the same mutex. The fix splits the
// critical section into a state-only phase (lock held) and an I/O
// phase (lock released).
func (e *LiveEngine) Stop(symbols []string) error {
	// ── Phase 1: state transition under lock ────────────────────────
	// Only flip flags and signal run-loop. Network calls live outside
	// the critical section so a slow broker can't freeze the engine.
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return nil
	}
	close(e.stopCh)
	e.running = false
	e.mu.Unlock()

	// ── Phase 2: network I/O without the lock ───────────────────────
	// close(stopCh) above is sufficient to make the run() goroutine
	// exit, so we can drain broker / data feed independently of
	// engine state. If Unsubscribe fails we still attempt Disconnect
	// (best-effort teardown) and return the first error.
	var firstErr error
	if err := e.dataFeed.Unsubscribe(symbols); err != nil {
		firstErr = fmt.Errorf("failed to unsubscribe: %w", err)
	}
	if err := e.broker.Disconnect(); err != nil {
		if firstErr == nil {
			firstErr = fmt.Errorf("failed to disconnect from broker: %w", err)
		}
		// Don't shadow firstErr — both failures are reported
		// separately via log-friendly concatenation would be ideal,
		// but for now we just return the unsubscribe error if any.
	}
	return firstErr
}

// IsRunning returns whether the engine is running
func (e *LiveEngine) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

// AUD-31（2026-09-22）：GetPortfolio() 已删除 —— 它返回的 `e.portfolio`
// 从构造后从未被更新，是个恒假读数（「初始资金 + 空持仓」）。同时它
// 无锁返回内部指针（AUD-23），调用方既能读到半更新状态也能改写引擎组合。
// 组合的真实来源是 positionManager：
//   - 持仓：GetPositions() / GetPosition(symbol)
//   - 汇总：GetTotalMarketValue() / GetTotalUnrealizedPnL() / GetTotalRealizedPnL()
// 若将来要做实盘组合视图，应在 PositionManager 上加一个带锁的快照方法，
// 而不是恢复一个「构造时算一次」的字段。

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

// SetPriceCageValidator (P1-5, ODR-018) wires the A-share price cage
// validator and a reference-price provider into the engine. The provider
// receives the order's symbol and must return a ReferencePrice snapshot
// (best bid/ask + prev close). When set, all subsequent limit orders
// are validated before being forwarded to the broker.
func (e *LiveEngine) SetPriceCageValidator(v *CageValidator, refProvider func(symbol string) ReferencePrice) {
	e.orderManager.SetPriceCageValidator(v, refProvider)
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
	for i := range orders {
		order := orders[i]
		if order.Symbol != quote.Symbol {
			continue
		}
		e.tryFillOrder(&order, quote)
	}
}

// tryFillOrder evaluates whether a pending order should fill against the
// incoming quote and, if so, executes the fill at the appropriate price.
//
// P1-3 (ODR-016) — LiveEngine 限价单实现.
//
// Order-type semantics:
//
//   - Market:   fill at Ask (buy) / Bid (sell) immediately.
//   - Limit:    fill only when the market crosses the limit price.
//     Buy  → Ask <= LimitPrice
//     Sell → Bid >= LimitPrice
//     (crossing the spread, conservative for the taker side.)
//   - Stop:     a stop order. Once the market touches the stop price, it
//     becomes a market order and fills at the next available
//     price. StopBuy triggers on Ask >= StopPrice, StopSell on
//     Bid <= StopPrice.
//   - Trailing: a trailing stop. We track a high water mark (HWM) per
//     pending order. The trigger price is HWM - TrailOffset.
//     Buy  triggers when Ask <= trigger
//     Sell triggers when Bid <= trigger
//     Only meaningful for Sell/Close direction in practice but
//     the engine supports both for symmetry.
//
// The function updates the in-memory order (HWM for trailing orders,
// status transition) via the OrderManager and emits the onTrade callback
// when a fill occurs.
func (e *LiveEngine) tryFillOrder(order *domain.Order, quote Quote) {
	if order == nil {
		return
	}

	// Trailing orders require per-quote HWM bookkeeping regardless of fill.
	if order.OrderType == domain.OrderTypeTrailing {
		e.updateTrailingHWM(order, quote)
	}

	if !e.shouldFill(order, quote) {
		return
	}

	fillPrice := e.computeFillPrice(order, quote)

	trade := domain.Trade{
		ID:         generateID(),
		Symbol:     order.Symbol,
		Direction:  order.Direction,
		Quantity:   order.Quantity,
		Price:      fillPrice,
		Commission: calculateCommission(fillPrice*order.Quantity, e.config),
		Timestamp:  quote.Timestamp,
	}

	e.orderManager.UpdateOrderStatus(order.ID, "filled")
	e.positionManager.UpdateFromTrade(trade)

	if e.onOrderUpdate != nil {
		filled := *order
		filled.FillPrice = fillPrice
		filled.FilledQty = order.Quantity
		filled.Status = "filled"
		e.onOrderUpdate(filled)
	}
	if e.onTrade != nil {
		e.onTrade(trade)
	}
}

// shouldFill returns true when the order's trigger condition is met by the
// current quote. It is order-type and direction aware.
func (e *LiveEngine) shouldFill(order *domain.Order, quote Quote) bool {
	switch order.OrderType {
	case domain.OrderTypeMarket:
		return true

	case domain.OrderTypeLimit:
		// Crossing-the-spread semantics: a buy limit is fillable when the
		// ask is at or below the limit; a sell limit is fillable when the
		// bid is at or above the limit.
		if order.LimitPrice <= 0 {
			// Misconfigured limit order — skip rather than silently fill.
			return false
		}
		switch order.Direction {
		case domain.DirectionLong:
			return quote.Ask > 0 && quote.Ask <= order.LimitPrice
		case domain.DirectionShort, domain.DirectionClose:
			return quote.Bid > 0 && quote.Bid >= order.LimitPrice
		}

	case domain.OrderTypeStop:
		if order.StopPrice <= 0 {
			return false
		}
		switch order.Direction {
		case domain.DirectionLong:
			// Stop buy: triggered when price breaks out upward.
			return quote.Ask >= order.StopPrice
		case domain.DirectionShort, domain.DirectionClose:
			// Stop sell / protective stop: triggered when price breaks down.
			return quote.Bid > 0 && quote.Bid <= order.StopPrice
		}

	case domain.OrderTypeTrailing:
		trigger := e.trailingTriggerPrice(order)
		if trigger <= 0 {
			return false
		}
		switch order.Direction {
		case domain.DirectionLong:
			// Defensive trailing-buy: only fill when ask is sane.
			return quote.Ask > 0 && quote.Ask <= trigger
		case domain.DirectionShort, domain.DirectionClose:
			return quote.Bid > 0 && quote.Bid <= trigger
		}
	}
	return false
}

// computeFillPrice picks the execution price based on order type.
// Stop / Trailing orders convert to a market fill once triggered.
func (e *LiveEngine) computeFillPrice(order *domain.Order, quote Quote) float64 {
	// For stop / trailing that have been triggered, fill at the market side
	// the taker would naturally hit.
	if order.OrderType == domain.OrderTypeStop ||
		order.OrderType == domain.OrderTypeTrailing {
		if order.Direction == domain.DirectionLong {
			return quote.Ask
		}
		return quote.Bid
	}
	if order.OrderType == domain.OrderTypeLimit {
		// Limit fill at limit price or better — for a buy, "better" is lower,
		// so we cap at the actual ask (conservative for the buyer).
		if order.Direction == domain.DirectionLong {
			if quote.Ask > 0 && quote.Ask < order.LimitPrice {
				return quote.Ask
			}
		} else if quote.Bid > 0 && quote.Bid > order.LimitPrice {
			return quote.Bid
		}
		return order.LimitPrice
	}
	// Market
	if order.Direction == domain.DirectionLong {
		return quote.Ask
	}
	return quote.Bid
}

// trailingTriggerPrice returns the price at which a trailing order would
// trigger given the current HWM. TrailAmount and TrailPercent are mutually
// exclusive; if both are set, TrailAmount wins (deterministic precedence).
func (e *LiveEngine) trailingTriggerPrice(order *domain.Order) float64 {
	if order.HighWaterMark <= 0 {
		return 0
	}
	offset := order.TrailAmount
	if offset <= 0 && order.TrailPercent > 0 {
		offset = order.HighWaterMark * order.TrailPercent
	}
	return order.HighWaterMark - offset
}

// updateTrailingHWM moves the order's high water mark up to the new high
// observed in the quote, but never down. It also persists the updated
// order back to the manager so future reads see the new HWM.
func (e *LiveEngine) updateTrailingHWM(order *domain.Order, quote Quote) {
	// Use the most aggressive side (Bid/Ask) for HWM tracking so the
	// trailing offset reflects the worst-case price for the holder.
	candidate := quote.Bid
	if quote.Ask > candidate {
		candidate = quote.Ask
	}
	if quote.High > candidate {
		candidate = quote.High
	}
	if candidate <= 0 {
		return
	}
	if order.HighWaterMark == 0 || candidate > order.HighWaterMark {
		order.HighWaterMark = candidate
		// Persist HWM back to the order store so a re-read sees it.
		e.orderManager.UpdateOrder(order.ID, order)
	}
}

// updatePortfolio marks every held position to market.
//
// AUD-16: the three assignments used to be made on the slice returned by
// GetPositions — which is a slice of copies — and never written back, so this
// function computed a mark for every position and threw it away. Every
// mark-to-market field (CurrentPrice / MarketValue / UnrealizedPnL) and both
// totals derived from them stayed 0 for the life of the engine. The write now
// goes through PositionManager.ApplyQuote, which mutates the stored position
// under its own lock.
//
// Two non-prices are rejected rather than applied:
//
//   - a quote error → the position keeps its last mark and is retried on the
//     next tick. An absent quote is not a price of zero.
//   - a non-positive price → same reason. A feed that hands back a zero-value
//     Quote without an error (this repo's own test double does) would otherwise
//     silently wipe the mark to zero.
func (e *LiveEngine) updatePortfolio() {
	positions := e.positionManager.GetPositions()
	for i := range positions {
		quote, err := e.dataFeed.GetQuote(positions[i].Symbol)
		if err != nil {
			continue
		}
		if quote.Close <= 0 {
			continue
		}
		e.positionManager.ApplyQuote(positions[i].Symbol, quote.Close)
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
