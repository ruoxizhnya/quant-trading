// Package backtest provides the backtesting engine for quantitative trading strategies.
package tracker

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/fees"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/portfolio"
	"github.com/ruoxizhnya/quant-trading/pkg/settlement"
)

// Tracker maintains portfolio state during a backtest run.
type Tracker struct {
	mu sync.RWMutex

	// Portfolio state
	cash        float64
	positions   map[string]*domain.Position
	initialCash float64

	// History
	portfolioValues []domain.PortfolioValue
	trades          []domain.Trade
	equityCurve     []domain.PortfolioValue

	// asOf 是回测的「今天」——引擎推进到哪个交易日，这里就是哪天。
	//
	// P1-12：从前 GetPortfolio 里写的是 time.Now()，于是策略判断调仓日时
	// 拿到的是墙钟时间。后果是 weekly / monthly 回测的成交取决于你周几跑
	// ——同一份代码周一跑有信号、周四跑零成交，回测不可复现。
	// 回测里根本不该有墙钟：一切时间都来自被回放的日期序列。
	asOf time.Time

	// Configuration
	commissionRate   float64
	slippageRate     float64
	liquidityFactor  float64 // fraction of prev day volume used for partial fill threshold
	shortSellingRate float64 // annual securities lending rate accrued daily on short positions

	// Trading rules (loaded from config)
	trading contracts.TradingConfig

	// Order log for tracking all orders
	orderLog *OrderLog

	// stockNames maps symbol -> display name, used to detect risk-warning
	// (ST-family) stocks for the daily buy cap (AUD-22). The engine
	// refreshes it once per trading day, because risk-warning status is
	// not static — a stock can be placed under or released from a warning
	// at any time, so a table set once at construction would apply
	// today's ST list to a 2015 backtest.
	//
	// It is a table rather than a per-call argument because there are TWO
	// order entry points — ExecuteTrade and ApplyTrade — and the
	// PRODUCTION path is ApplyTrade (NewEngine always installs an
	// execution service, so executeViaExecutionService wins whenever the
	// direction is not Hold). Threading a name through only ExecuteTrade
	// would have produced a guard that is green in unit tests and inert in
	// production; see docs/TASKS.md AUD-22.
	stockNames map[string]string

	// dailyRWBuy is the number of shares of each risk-warning stock bought
	// on dailyRWBuyDay. Reset when the trade date moves to another day.
	// Guarded by mu.
	dailyRWBuy    map[string]float64
	dailyRWBuyDay time.Time

	logger zerolog.Logger
}

// NewTracker creates a new portfolio tracker.
func NewTracker(initialCapital, commissionRate, slippageRate float64, trading contracts.TradingConfig, logger zerolog.Logger) *Tracker {
	// Use defaults if trading config is empty
	if trading.StampTaxRate == 0 {
		trading = contracts.DefaultTradingConfig()
	}

	return &Tracker{
		cash:             initialCapital,
		initialCash:      initialCapital,
		positions:        make(map[string]*domain.Position),
		commissionRate:   commissionRate,
		slippageRate:     slippageRate,
		liquidityFactor:  0.1, // default: 10% of prev day volume
		shortSellingRate: contracts.DefaultShortSellingRate,
		trading:          trading,
		orderLog:         &OrderLog{},
		logger:           logger.With().Str("component", "tracker").Logger(),
	}
}

// GetCash returns the current cash balance.
func (t *Tracker) GetCash() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.cash
}

// SetShortSellingRate overrides the annual securities lending rate
// used to accrue daily interest on short positions in AdvanceDay.
// Pass 0 to disable short-selling cost accrual. The rate is expressed
// as a fraction (e.g. 0.106 for 10.6%/year) and converted to a daily
// rate using TradingDaysPerYear (252).
func (t *Tracker) SetShortSellingRate(rate float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.shortSellingRate = rate
}

// GetShortSellingRate returns the configured annual securities lending rate.
func (t *Tracker) GetShortSellingRate() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.shortSellingRate
}

// feeSchedule returns the tracker's fee configuration as a fees.AShareFees
// struct, for use with portfolio.ComputeFees (S7-P1-1). This bridges the
// flat Tracker fields (commissionRate + trading.*) to the shared fee-
// calculation primitive, ensuring tracker and mock_trader can never drift
// on the commission/transfer/stamp formula.
//
// AUD-20 (ODR-065): asOf is the TRADE DATE, and it selects the stamp-tax
// rate in force on that day. Reading t.trading.StampTaxRate directly (as
// this method used to) charges the post-2023-08-28 rate to every sell in
// the window, which UNDERSTATES cost — and therefore OVERSTATES return —
// for every sell before the cut. Pass the trade's own timestamp; do NOT
// pass time.Now() (see the asOf field comment above: a backtest has no
// wall clock, and mixing one in is exactly the P1-12 defect).
//
// Callers must already hold t.mu (Lock or RLock) — this method is lock-free
// to avoid reentrant RLock deadlock (Go's sync.RWMutex is NOT reentrant).
func (t *Tracker) feeSchedule(asOf time.Time) fees.AShareFees {
	return fees.AShareFees{
		CommissionRate: t.commissionRate,
		// Date-segmented: 0.1% before 2023-08-28, 0.05% on/after it.
		StampTaxRate:    fees.StampTaxRateFor(asOf, t.trading.StampTaxRate, t.trading.StampTaxRateBefore),
		TransferFeeRate: t.trading.TransferFeeRate,
		MinCommission:   t.trading.MinCommission,
		SlippageRate:    t.slippageRate,
	}
}

// SetStockNames replaces the symbol -> display-name table used for
// risk-warning (ST-family) detection (AUD-22).
//
// engine_daily.go calls this once per trading day with that day's stock
// metadata. It is the ONLY place that populates the table, and the
// end-to-end test TestEngine_RiskWarningDailyBuyCap_ProductionPath pins
// that wiring — a table nobody fills in would make the cap a silent
// no-op, which is exactly the failure mode AUD-14 and AUD-35 were about.
//
// Passing nil or an empty map is legal and means "no names known"; in
// that state no risk-warning cap is applied. That is a deliberate
// fail-OPEN choice (an unknown name must not block legitimate trading),
// and it is why an unknown name at buy time is logged — see
// enforceRiskWarningDailyBuy.
func (t *Tracker) SetStockNames(names map[string]string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stockNames = names
}

// enforceRiskWarningDailyBuy applies the per-investor daily cumulative
// buy cap for risk-warning stocks and records the fill when it passes.
//
// Regulatory basis (rule text verified 2026-09-22, see
// pkg/marketdata/riskwarning.go for the citations):
//
//	沪深主板/创业板  50 万股   沪 4.4.10 / 深 4.5.4
//	北交所           20 万股   北 4.5.4（2026-08-31 起施行）
//	科创板           不适用    沪 6.14 科创板 ST 不进风险警示板
//
// 口径：委托买入 + 当日已买入 + 已申报未成交未撤销 ≤ 上限；
// 普通账户与信用账户合并计算。例外：回购、5% 以上股东按已披露计划增持 —
// 本函数不区分委托来源，故对例外情形会**多拦**，这是刻意的保守方向。
//
// Buy side only: the cap is on 买入, and a short is an opening sale.
// Non-positive quantities and non-risk-warning names are no-ops.
//
// Callers must hold t.mu (Lock).
func (t *Tracker) enforceRiskWarningDailyBuy(symbol string, qty float64, asOf time.Time) error {
	if qty <= 0 {
		return nil
	}
	name, known := t.stockNames[symbol]
	if !known || name == "" {
		// Fail open, but loudly: an unpopulated table means the engine
		// forgot to call SetStockNames, and an empty name means getStock
		// failed for this symbol (the key is present but carries a zero
		// Stock). Silence here would look identical to "no risk warnings
		// in this backtest", which is the one thing we must not confuse
		// it with.
		t.logger.Warn().
			Str("symbol", symbol).
			Msg("risk-warning daily buy cap not evaluated: stock name unknown " +
				"(SetStockNames not populated, or getStock failed for this symbol)")
		return nil
	}
	if !marketdata.IsRiskWarningName(name) {
		return nil
	}
	capShares := marketdata.RiskWarningDailyBuyCap(symbol)
	if capShares <= 0 {
		return nil
	}

	// Reset the accumulator when the trading day changes. Trades are
	// expected in non-decreasing date order (the engine advances one day
	// at a time); an out-of-order earlier date would restart the counter,
	// which is conservative in neither direction but cannot happen on the
	// engine's path.
	day := asOf.Truncate(24 * time.Hour)
	if !day.Equal(t.dailyRWBuyDay) {
		t.dailyRWBuy = nil
		t.dailyRWBuyDay = day
	}
	if t.dailyRWBuy == nil {
		t.dailyRWBuy = make(map[string]float64)
	}

	already := t.dailyRWBuy[symbol]
	if already+qty > capShares {
		return fmt.Errorf(
			"risk-warning daily buy cap exceeded for %s (%s): already bought %.0f today, "+
				"order for %.0f would total %.0f, cap is %.0f shares/day",
			symbol, name, already, qty, already+qty, capShares)
	}
	t.dailyRWBuy[symbol] = already + qty
	return nil
}

// GetPosition returns a copy of the position for a symbol.
func (t *Tracker) GetPosition(symbol string) (*domain.Position, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	pos, exists := t.positions[symbol]
	if !exists {
		return nil, false
	}
	// Return a copy
	copy := *pos
	return &copy, true
}

// GetAllPositions returns all current positions.
func (t *Tracker) GetAllPositions() map[string]domain.Position {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make(map[string]domain.Position)
	for sym, pos := range t.positions {
		result[sym] = *pos
	}
	return result
}

// GetPortfolioValue calculates the total portfolio value at current prices.
func (t *Tracker) GetPortfolioValue(prices map[string]float64) float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	totalValue := t.cash
	for _, sym := range sortedKeys(t.positions) {
		pos := t.positions[sym]
		if price, ok := prices[sym]; ok {
			pos.MarketValue = pos.Quantity * price
			pos.CurrentPrice = price
			pos.UnrealizedPnL = (price - pos.AvgCost) * pos.Quantity
			totalValue += pos.MarketValue
		}
	}
	return totalValue
}

// OrderExecutionOpts holds options for trade execution.
type OrderExecutionOpts struct {
	OrderType  domain.OrderType
	LimitPrice float64
	DayBar     *domain.OHLCV // today's OHLCV bar (needed for limit order fill check)
}

// ExecuteTrade executes a trade and returns the trade record.
// opts may be nil (defaults to market order at given price).
func (t *Tracker) ExecuteTrade(symbol string, direction domain.Direction, quantity float64, price float64, timestamp time.Time, opts *OrderExecutionOpts) (*domain.Trade, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	orderType := domain.OrderTypeMarket
	limitPrice := price
	var dayBar *domain.OHLCV
	if opts != nil {
		orderType = opts.OrderType
		limitPrice = opts.LimitPrice
		dayBar = opts.DayBar
	}

	// --- Limit order fill check ---
	filledQty := quantity
	executionPrice := price
	orderStatus := "filled"

	if orderType == domain.OrderTypeLimit && dayBar != nil {
		limitFilled := false
		if direction == domain.DirectionLong {
			// Buy limit: fills if price dropped to or below limit
			if dayBar.Low <= limitPrice {
				limitFilled = true
				executionPrice = min(limitPrice, dayBar.Close)
			}
		} else if direction == domain.DirectionShort {
			// Sell limit: fills if price rose to or above limit
			if dayBar.High >= limitPrice {
				limitFilled = true
				executionPrice = max(limitPrice, dayBar.Close)
			}
		}
		if !limitFilled {
			// Expired — record order and return
			order := domain.Order{
				ID:         uuid.New().String(),
				Symbol:     symbol,
				Direction:  direction,
				OrderType:  orderType,
				Quantity:   quantity,
				LimitPrice: limitPrice,
				Timestamp:  timestamp,
				FilledQty:  0,
				FillPrice:  0,
				Status:     "expired",
			}
			t.orderLog.Record(order)
			return nil, fmt.Errorf("limit order expired: %s %s @ %.4f (day low=%.4f high=%.4f)",
				direction, symbol, limitPrice, dayBar.Low, dayBar.High)
		}
	}

	// --- Partial fill: liquidity check ---
	if dayBar != nil && dayBar.Volume > 0 {
		maxLiquidity := dayBar.Volume * t.liquidityFactor
		if filledQty > maxLiquidity {
			filledQty = maxLiquidity
			orderStatus = "partial"
			t.logger.Info().
				Str("symbol", symbol).
				Float64("requested", quantity).
				Float64("filled", filledQty).
				Float64("max_liquidity", maxLiquidity).
				Msg("Partial fill: liquidity constraint")
		}
	}

	// --- Apply slippage (for market orders only; limit orders use LimitPrice) ---
	if orderType != domain.OrderTypeLimit {
		switch direction {
		case domain.DirectionLong:
			executionPrice = price * (1 + t.slippageRate)
		case domain.DirectionShort:
			executionPrice = price * (1 - t.slippageRate)
		case domain.DirectionClose:
			if pos, exists := t.positions[symbol]; exists {
				if pos.Quantity > 0 {
					executionPrice = price * (1 - t.slippageRate)
				} else {
					executionPrice = price * (1 + t.slippageRate)
				}
			}
		}
	}

	tradeValue := filledQty * executionPrice
	// S7-P1-1: delegate fee math to the shared primitive. Stamp tax
	// applies on the sell side: closing long (DirectionClose) and opening
	// short (DirectionShort). A-share stamp tax is sell-side-only and
	// date-segmented (0.1% before 2023-08-28, 0.05% since) — the rate is
	// resolved per trade in feeSchedule, not hard-coded here.
	isSell := direction == domain.DirectionClose || direction == domain.DirectionShort
	fb := portfolio.ComputeFees(tradeValue, isSell, t.feeSchedule(timestamp))

	trade := &domain.Trade{
		ID:          uuid.New().String(),
		Symbol:      symbol,
		Direction:   direction,
		Quantity:    filledQty,
		FilledQty:   filledQty,
		Price:       executionPrice,
		Commission:  fb.Commission,
		TransferFee: fb.TransferFee,
		StampTax:    fb.StampTax,
		Timestamp:   timestamp,
		PendingQty:  quantity - filledQty, // track unfilled portion
	}

	switch direction {
	case domain.DirectionLong:
		// Cost includes commission + transfer fee (stamp tax does not apply to buy)
		cost := tradeValue + fb.Total()
		if cost > t.cash {
			return nil, fmt.Errorf("insufficient cash: required %.2f, available %.2f", cost, t.cash)
		}
		// AUD-22: per-investor daily cumulative buy cap for risk-warning
		// stocks. Checked before any state mutation so a rejected order
		// leaves the portfolio and the cash balance untouched.
		if err := t.enforceRiskWarningDailyBuy(symbol, filledQty, timestamp); err != nil {
			return nil, err
		}
		t.cash -= cost

		tradeDate := timestamp.Truncate(24 * time.Hour)

		if existing, exists := t.positions[symbol]; exists {
			totalQty := existing.Quantity + filledQty
			if settlement.IsFlat(totalQty) {
				// S7-P0-17 (ODR-043): the buy exactly offsets an existing
				// short (e.g. short 100 + buy 100 = flat). Delete the
				// position so a later close returns "position not found"
				// instead of the confusing "cannot close position: quantity
				// is zero", and so AvgCost isn't computed as NaN from the
				// divide-by-zero below. Only the DirectionClose branch
				// previously had this cleanup.
				delete(t.positions, symbol)
			} else {
				// Update average cost
				existing.AvgCost = (existing.AvgCost*existing.Quantity + executionPrice*filledQty) / totalQty
				existing.Quantity = totalQty
				existing.EntryDate = timestamp

				// T+1 tracking: if new trading day, reset today's qty (yesterday's already set by AdvanceDay)
				lastBuyDate := existing.BuyDate.Truncate(24 * time.Hour)
				if !lastBuyDate.Equal(tradeDate) {
					// New trading day: today's qty starts fresh (yesterday's carry already in QuantityYesterday)
					existing.QuantityToday = 0
				}
				existing.QuantityToday += filledQty
				existing.BuyDate = timestamp
			}
		} else {
			t.positions[symbol] = &domain.Position{
				Symbol:            symbol,
				Quantity:          filledQty,
				AvgCost:           executionPrice,
				EntryDate:         timestamp,
				BuyDate:           timestamp,
				QuantityToday:     filledQty, // newly bought, not sellable until T+1
				QuantityYesterday: 0,
			}
		}

	case domain.DirectionShort:
		// Short selling: receive cash, owe shares (commission + transfer fee deducted)
		proceeds := tradeValue - fb.Total()
		t.cash += proceeds

		if existing, exists := t.positions[symbol]; exists {
			existing.Quantity -= filledQty
			if settlement.IsFlat(existing.Quantity) {
				// S7-P0-17 (ODR-043): the short exactly offsets an existing
				// long (e.g. long 100 + short 100 = flat). Delete the ghost
				// position; see the matching guard in DirectionLong above.
				delete(t.positions, symbol)
			}
		} else {
			t.positions[symbol] = &domain.Position{
				Symbol:    symbol,
				Quantity:  -filledQty, // negative for short
				AvgCost:   executionPrice,
				EntryDate: timestamp,
			}
		}

	case domain.DirectionClose:
		if pos, exists := t.positions[symbol]; exists {
			closeQty := abs(pos.Quantity)
			if closeQty <= 0 {
				return nil, fmt.Errorf("cannot close position: quantity is zero")
			}

			if pos.Quantity > 0 {
				// Closing long position — enforce T+1 settlement (A-share rule)
				tradeDate := timestamp.Truncate(24 * time.Hour)
				buyDate := pos.BuyDate.Truncate(24 * time.Hour)

				// If BuyDate is today AND no carryover from previous days, all shares bought today — T+1 violation
				// Note: if BuyDate == today but QuantityYesterday > 0, we have sellable shares from a prior position
				if buyDate.Equal(tradeDate) && pos.QuantityYesterday == 0 {
					t.logger.Warn().
						Str("symbol", symbol).
						Float64("attempted_sell", quantity).
						Float64("can_sell", 0).
						Time("trade_date", timestamp).
						Msg("T+1 violation: attempted to sell shares bought today")
					return nil, fmt.Errorf("T+1 settlement violation: cannot sell shares bought on %s (today: %s)", buyDate.Format("2006-01-02"), tradeDate.Format("2006-01-02"))
				}

				// canSell = QuantityYesterday (shares from previous days that can be sold today)
				// QuantityToday shares cannot be sold today (T+1 rule)
				canSell := pos.QuantityYesterday

				// actualQty = min(canSell, requested, position_size)
				actualQty := min(canSell, min(quantity, closeQty))
				if actualQty <= 0 {
					return nil, fmt.Errorf("T+1 settlement violation: no shares available to sell (all bought today)")
				}

				if actualQty < quantity {
					t.logger.Warn().
						Str("symbol", symbol).
						Float64("attempted_sell", quantity).
						Float64("actual_sell", actualQty).
						Float64("can_sell", canSell).
						Time("timestamp", timestamp).
						Msg("T+1 partial fill: reducing sell quantity to sellable shares")
				}

				// Recalculate commission, transfer fee, and stamp tax based on actualQty.
				// S7-P1-1: closing long is a sell-side transaction (stamp tax applies).
				actualTradeValue := actualQty * executionPrice
				actualFb := portfolio.ComputeFees(actualTradeValue, true, t.feeSchedule(timestamp))

				// Update trade record
				trade.Quantity = actualQty
				trade.Commission = actualFb.Commission
				trade.TransferFee = actualFb.TransferFee
				trade.StampTax = actualFb.StampTax

				// Update yesterday qty
				pos.QuantityYesterday -= actualQty

				// Closing long: apply stamp tax (date-segmented, see
				// feeSchedule) + commission, calculate PnL
				pnl := (executionPrice - pos.AvgCost) * actualQty
				pos.RealizedPnL += pnl - actualFb.Commission - actualFb.StampTax
				t.cash += actualQty*executionPrice - actualFb.Total()
				pos.Quantity -= actualQty
			} else {
				// Closing short position — no T+1 restriction
				actualQty := min(quantity, closeQty)
				if actualQty <= 0 {
					return nil, fmt.Errorf("cannot close position: quantity is zero")
				}
				// S7-P1-1: closing short is a buy-back (no stamp tax).
				actualTradeValue := actualQty * executionPrice
				actualFb := portfolio.ComputeFees(actualTradeValue, false, t.feeSchedule(timestamp))

				// Update trade record with actual values
				trade.Quantity = actualQty
				trade.Commission = actualFb.Commission
				trade.TransferFee = actualFb.TransferFee
				// No stamp tax for short close (stamp tax only on sell of long positions)

				pnl := (pos.AvgCost - executionPrice) * actualQty
				pos.RealizedPnL += pnl - actualFb.Commission - actualFb.TransferFee
				t.cash += actualQty*executionPrice - actualFb.Commission - actualFb.TransferFee
				pos.Quantity += actualQty
			}

			// Remove position if fully closed
			if settlement.IsFlat(pos.Quantity) {
				delete(t.positions, symbol)
			}
		} else {
			return nil, fmt.Errorf("position not found for symbol %s", symbol)
		}
	}

	// Update position market value and unrealized PnL
	if pos, exists := t.positions[symbol]; exists {
		pos.MarketValue = abs(pos.Quantity) * price
		pos.CurrentPrice = price
		pos.UnrealizedPnL = (price - pos.AvgCost) * pos.Quantity
	}

	t.trades = append(t.trades, *trade)

	// Record order in order log
	order := domain.Order{
		ID:         uuid.New().String(),
		Symbol:     symbol,
		Direction:  direction,
		OrderType:  orderType,
		Quantity:   quantity,
		LimitPrice: limitPrice,
		Timestamp:  timestamp,
		FilledQty:  filledQty,
		FillPrice:  executionPrice,
		Status:     orderStatus,
	}
	t.orderLog.Record(order)

	t.logger.Debug().
		Str("symbol", symbol).
		Str("direction", string(direction)).
		Str("order_type", string(orderType)).
		Float64("filled_qty", filledQty).
		Float64("price", executionPrice).
		Float64("commission", fb.Commission).
		Str("status", orderStatus).
		Time("timestamp", timestamp).
		Msg("Trade executed")

	return trade, nil
}

// ApplyTrade applies a pre-computed trade (from ExecutionService) to the portfolio.
// Unlike ExecuteTrade which calculates slippage/commission internally, ApplyTrade
// uses the trade's already-computed Price and Commission. This enables pluggable
// execution models via ExecutionService.
func (t *Tracker) ApplyTrade(trade domain.Trade) (*domain.Trade, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	symbol := trade.Symbol
	direction := trade.Direction
	quantity := trade.Quantity
	executionPrice := trade.Price
	commission := trade.Commission
	transferFee := trade.TransferFee
	_ = trade.StampTax // may be used in future; referenced to avoid unused var
	timestamp := trade.Timestamp

	if quantity <= 0 {
		return nil, fmt.Errorf("invalid trade quantity: %f", quantity)
	}

	tradeValue := quantity * executionPrice

	switch direction {
	case domain.DirectionLong:
		// Cost includes commission + transfer fee (stamp tax does not apply to buy)
		cost := tradeValue + commission + transferFee
		if cost > t.cash {
			return nil, fmt.Errorf("insufficient cash: required %.2f, available %.2f", cost, t.cash)
		}
		// AUD-22: same cap as ExecuteTrade. This is the PRODUCTION path —
		// NewEngine always installs an execution service, so buys reach the
		// portfolio through ApplyTrade, not ExecuteTrade. Enforcing in only
		// one of the two would leave the other silently uncapped.
		if err := t.enforceRiskWarningDailyBuy(symbol, quantity, timestamp); err != nil {
			return nil, err
		}
		t.cash -= cost

		tradeDate := timestamp.Truncate(24 * time.Hour)

		if existing, exists := t.positions[symbol]; exists {
			totalQty := existing.Quantity + quantity
			if settlement.IsFlat(totalQty) {
				// S7-P0-17 (ODR-043): buy exactly offsets existing short
				// → flat. Delete to avoid ghost zero-quantity position and
				// AvgCost NaN. See ExecuteTrade for the full rationale.
				delete(t.positions, symbol)
			} else {
				// Update average cost
				existing.AvgCost = (existing.AvgCost*existing.Quantity + executionPrice*quantity) / totalQty
				existing.Quantity = totalQty
				existing.EntryDate = timestamp

				// T+1 tracking
				lastBuyDate := existing.BuyDate.Truncate(24 * time.Hour)
				if !lastBuyDate.Equal(tradeDate) {
					existing.QuantityToday = 0
				}
				existing.QuantityToday += quantity
				existing.BuyDate = timestamp
			}
		} else {
			t.positions[symbol] = &domain.Position{
				Symbol:            symbol,
				Quantity:          quantity,
				AvgCost:           executionPrice,
				EntryDate:         timestamp,
				BuyDate:           timestamp,
				QuantityToday:     quantity,
				QuantityYesterday: 0,
			}
		}

	case domain.DirectionShort:
		// Short selling: receive cash, owe shares
		proceeds := tradeValue - commission - transferFee
		t.cash += proceeds

		if existing, exists := t.positions[symbol]; exists {
			existing.Quantity -= quantity
			if settlement.IsFlat(existing.Quantity) {
				// S7-P0-17 (ODR-043): short exactly offsets existing long
				// → flat. Delete the ghost position.
				delete(t.positions, symbol)
			}
		} else {
			t.positions[symbol] = &domain.Position{
				Symbol:    symbol,
				Quantity:  -quantity,
				AvgCost:   executionPrice,
				EntryDate: timestamp,
			}
		}

	case domain.DirectionClose:
		if pos, exists := t.positions[symbol]; exists {
			closeQty := abs(pos.Quantity)
			if closeQty <= 0 {
				return nil, fmt.Errorf("cannot close position: quantity is zero")
			}

			if pos.Quantity > 0 {
				// Closing long position
				tradeDate := timestamp.Truncate(24 * time.Hour)
				buyDate := pos.BuyDate.Truncate(24 * time.Hour)

				if buyDate.Equal(tradeDate) && pos.QuantityYesterday == 0 {
					return nil, fmt.Errorf("T+1 settlement violation: cannot sell shares bought on %s (today: %s)", buyDate.Format("2006-01-02"), tradeDate.Format("2006-01-02"))
				}

				canSell := pos.QuantityYesterday
				actualQty := min(canSell, min(quantity, closeQty))
				if actualQty <= 0 {
					return nil, fmt.Errorf("T+1 settlement violation: no shares available to sell")
				}

				actualTradeValue := actualQty * executionPrice
				// S7-P1-1: closing long is a sell-side transaction (stamp tax applies).
				actualFb := portfolio.ComputeFees(actualTradeValue, true, t.feeSchedule(timestamp))

				// Override trade values with actual
				trade.Quantity = actualQty
				trade.Commission = actualFb.Commission
				trade.TransferFee = actualFb.TransferFee
				trade.StampTax = actualFb.StampTax

				pos.QuantityYesterday -= actualQty
				pnl := (executionPrice - pos.AvgCost) * actualQty
				pos.RealizedPnL += pnl - actualFb.Commission - actualFb.StampTax
				t.cash += actualQty*executionPrice - actualFb.Total()
				pos.Quantity -= actualQty
			} else {
				// Closing short position
				actualQty := min(quantity, closeQty)
				if actualQty <= 0 {
					return nil, fmt.Errorf("cannot close position: quantity is zero")
				}
				// S7-P1-1: closing short is a buy-back (no stamp tax).
				actualTradeValue := actualQty * executionPrice
				actualFb := portfolio.ComputeFees(actualTradeValue, false, t.feeSchedule(timestamp))

				trade.Quantity = actualQty
				trade.Commission = actualFb.Commission
				trade.TransferFee = actualFb.TransferFee

				pnl := (pos.AvgCost - executionPrice) * actualQty
				pos.RealizedPnL += pnl - actualFb.Commission - actualFb.TransferFee
				t.cash += actualQty*executionPrice - actualFb.Commission - actualFb.TransferFee
				pos.Quantity += actualQty
			}

			if settlement.IsFlat(pos.Quantity) {
				delete(t.positions, symbol)
			}
		} else {
			return nil, fmt.Errorf("position not found for symbol %s", symbol)
		}
	}

	// Update position market value
	if pos, exists := t.positions[symbol]; exists {
		pos.MarketValue = abs(pos.Quantity) * executionPrice
		pos.CurrentPrice = executionPrice
		pos.UnrealizedPnL = (executionPrice - pos.AvgCost) * pos.Quantity
	}

	t.trades = append(t.trades, trade)

	t.logger.Debug().
		Str("symbol", symbol).
		Str("direction", string(direction)).
		Float64("qty", quantity).
		Float64("price", executionPrice).
		Float64("commission", commission).
		Time("timestamp", timestamp).
		Msg("Trade applied")

	return &trade, nil
}

// ProcessDividend credits cash when a dividend is paid for a held position.
// divAmt is the cash dividend per share (e.g. 0.10 means 0.10 CNY per share).
// The credit is: position.Quantity * dividend.DivAmt
// For long positions only; short positions are liabilities (no dividend paid to holder).
func (t *Tracker) ProcessDividend(symbol string, dividend domain.Dividend) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	pos, exists := t.positions[symbol]
	if !exists || pos.Quantity <= 0 {
		return nil
	}

	dividendCredit := pos.Quantity * dividend.DivAmt
	t.cash += dividendCredit

	t.logger.Info().
		Str("symbol", symbol).
		Float64("quantity", pos.Quantity).
		Float64("div_amt", dividend.DivAmt).
		Float64("credit", dividendCredit).
		Time("pay_date", dividend.PayDate).
		Msg("Dividend credited to cash")

	return nil
}

func (t *Tracker) ProcessSplit(symbol string, split domain.Split) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	pos, exists := t.positions[symbol]
	if !exists || pos.Quantity <= 0 {
		return nil
	}

	oldQty := pos.Quantity
	oldCost := pos.AvgCost

	if split.StkDivRatio > 0 {
		additionalShares := oldQty * split.StkDivRatio
		pos.Quantity += additionalShares
		pos.AvgCost = (oldCost * oldQty) / pos.Quantity
		pos.QuantityToday += additionalShares
	}

	if split.CashDivRatio > 0 {
		cashCredit := oldQty * split.CashDivRatio
		t.cash += cashCredit
	}

	t.logger.Info().
		Str("symbol", symbol).
		Float64("old_qty", oldQty).
		Float64("new_qty", pos.Quantity).
		Float64("old_cost", oldCost).
		Float64("new_cost", pos.AvgCost).
		Float64("stk_div_ratio", split.StkDivRatio).
		Float64("cash_div_ratio", split.CashDivRatio).
		Msg("Stock split/dividend processed")

	return nil
}

// AdvanceDay shifts QuantityToday → QuantityYesterday at the end of each trading day.
// This implements T+1 settlement: shares bought today become sellable tomorrow.
//
// In the same pass it accrues one day of securities lending interest on every
// open short position (Quantity < 0). The daily cost is:
//
//	cost = position_value * (shortSellingRate / TradingDaysPerYear)
//
// where position_value is abs(Quantity) * CurrentPrice (falling back to AvgCost
// when CurrentPrice has not been populated). The cost is deducted from cash.
// The rate defaults to DefaultShortSellingRate (10.6%/year) and can be tuned
// via SetShortSellingRate.
func (t *Tracker) AdvanceDay(date time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.logger.Debug().
		Time("date", date).
		Msg("Advancing day for T+1 settlement")

	for sym, pos := range t.positions {
		if pos.QuantityToday > 0 {
			pos.QuantityYesterday += pos.QuantityToday
			pos.QuantityToday = 0
			t.logger.Debug().
				Str("symbol", sym).
				Float64("quantity_yesterday", pos.QuantityYesterday).
				Msg("T+1 rollover: yesterday quantity updated")
		}
	}

	// Accrue daily short-selling (securities lending) interest on open
	// short positions. The cost is deducted from cash.
	if t.shortSellingRate <= 0 {
		return
	}
	dailyRate := t.shortSellingRate / float64(contracts.TradingDaysPerYear)
	totalLendingCost := 0.0
	for sym, pos := range t.positions {
		if pos.Quantity >= 0 {
			continue
		}
		price := pos.CurrentPrice
		if price <= 0 {
			price = pos.AvgCost
		}
		positionValue := abs(pos.Quantity) * price
		cost := positionValue * dailyRate
		t.cash -= cost
		totalLendingCost += cost
		t.logger.Debug().
			Str("symbol", sym).
			Float64("short_qty", abs(pos.Quantity)).
			Float64("position_value", positionValue).
			Float64("lending_cost", cost).
			Msg("Short-selling interest accrued")
	}
	if totalLendingCost > 0 {
		t.logger.Info().
			Time("date", date).
			Float64("total_lending_cost", totalLendingCost).
			Float64("cash_after", t.cash).
			Float64("annual_rate", t.shortSellingRate).
			Msg("Short-selling interest accrued for trading day")
	}
}

// RecordDailyValue records the portfolio value for a given day.
func (t *Tracker) RecordDailyValue(date time.Time, prices map[string]float64) domain.PortfolioValue {
	t.mu.Lock()
	defer t.mu.Unlock()

	cash := t.cash
	positionsValue := 0.0

	// Update positions with current prices
	for _, sym := range sortedKeys(t.positions) {
		pos := t.positions[sym]
		if price, ok := prices[sym]; ok {
			pos.CurrentPrice = price
			pos.MarketValue = abs(pos.Quantity) * price
			pos.UnrealizedPnL = (price - pos.AvgCost) * pos.Quantity
			// For long positions: add market value to equity
			// For short positions: subtract market value (liability to buy back)
			if pos.Quantity > 0 {
				positionsValue += pos.MarketValue
			} else {
				positionsValue -= pos.MarketValue
			}
		}
	}

	totalValue := cash + positionsValue

	pv := domain.PortfolioValue{
		Date:       date,
		TotalValue: totalValue,
		Cash:       cash,
		Positions:  positionsValue,
	}

	t.portfolioValues = append(t.portfolioValues, pv)
	t.equityCurve = append(t.equityCurve, pv)

	return pv
}

// GetPortfolioValues returns all recorded portfolio values.
func (t *Tracker) GetPortfolioValues() []domain.PortfolioValue {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]domain.PortfolioValue, len(t.portfolioValues))
	copy(result, t.portfolioValues)
	return result
}

// GetTrades returns all executed trades.
func (t *Tracker) GetTrades() []domain.Trade {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]domain.Trade, len(t.trades))
	copy(result, t.trades)
	return result
}

// GetEquityCurve returns the equity curve.
func (t *Tracker) GetEquityCurve() []domain.PortfolioValue {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]domain.PortfolioValue, len(t.equityCurve))
	copy(result, t.equityCurve)
	return result
}

// GetTotalValue returns the current total portfolio value.
func (t *Tracker) GetTotalValue(prices map[string]float64) float64 {
	return t.GetPortfolioValue(prices)
}

// ClosePosition closes a position for a symbol.
// No lock needed here — ExecuteTrade acquires its own write lock.
func (t *Tracker) ClosePosition(symbol string, price float64, timestamp time.Time) (*domain.Trade, error) {
	// Read position quantity without lock (ExecuteTrade locks internally)
	t.mu.RLock()
	pos, exists := t.positions[symbol]
	t.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("position not found for symbol %s", symbol)
	}

	// ExecuteTrade acquires its own write lock — no deadlock since we release RLock first
	return t.ExecuteTrade(symbol, domain.DirectionClose, abs(pos.Quantity), price, timestamp, nil)
}

// HasPosition checks if there is an open position for a symbol.
func (t *Tracker) HasPosition(symbol string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	pos, exists := t.positions[symbol]
	return exists && !settlement.IsFlat(pos.Quantity)
}

// SetAsOf 告诉 tracker 回测当前处于哪一天（P1-12）。
//
// 引擎必须在每个交易日**开始**时调用，先于任何信号生成与仓位计算 ——
// 否则策略看到的还是昨天的日期，调仓日判断会整体错一位。
func (t *Tracker) SetAsOf(date time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.asOf = date
}

// AsOf 返回回测的当前日期，未设置则返回零值。
func (t *Tracker) AsOf() time.Time {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.asOf
}

// GetPortfolio returns a snapshot of the current portfolio state.
func (t *Tracker) GetPortfolio(prices map[string]float64) *domain.Portfolio {
	t.mu.RLock()
	defer t.mu.RUnlock()

	positions := make(map[string]domain.Position)
	for _, sym := range sortedKeys(t.positions) {
		pos := t.positions[sym]
		if price, ok := prices[sym]; ok {
			posCopy := *pos
			posCopy.CurrentPrice = price
			posCopy.MarketValue = abs(pos.Quantity) * price
			posCopy.UnrealizedPnL = (price - pos.AvgCost) * pos.Quantity
			positions[sym] = posCopy
		}
	}

	totalValue := t.cash
	for _, sym := range sortedKeys(positions) {
		pos := positions[sym]
		// Long positions add value, short positions are liabilities
		if pos.Quantity > 0 {
			totalValue += pos.MarketValue
		} else {
			totalValue -= pos.MarketValue
		}
	}

	// 回测里的时间只能来自被回放的日期序列（P1-12）：asOf 由引擎每交易日
	// 设置。零值意味着调用方不是回测引擎（实时撮合、手写用例等），
	// 这时才退回墙钟 —— 实时场景下「现在」确实是现在。
	asOf := t.asOf
	if asOf.IsZero() {
		asOf = time.Now()
	}

	return &domain.Portfolio{
		Cash:       t.cash,
		Positions:  positions,
		TotalValue: totalValue,
		UpdatedAt:  asOf,
	}
}

// Reset resets the tracker to initial state.
func (t *Tracker) Reset(initialCapital float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cash = initialCapital
	t.initialCash = initialCapital
	t.positions = make(map[string]*domain.Position)
	t.portfolioValues = nil
	t.trades = nil
	t.equityCurve = nil
	t.asOf = time.Time{}
}

// sortedKeys 返回 map 的键，按字典序排好。
//
// 为什么必须有它：Go 的 map 遍历顺序是随机的，而持仓求和是浮点加法 ——
// 加法不满足结合律，换一种顺序结果就差最后几位（实测 1e-10）。这点差异
// 在回测里会随复利放大，最后变成不同的成交数量。要复现一份回测，
// 所有影响数值和顺序的遍历都得先定序（P1-14）。
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Helper functions
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
