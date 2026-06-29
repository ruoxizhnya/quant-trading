// Package live — A-share price cage validator.
//
// P1-5 (ODR-018) — A 股价格笼子校验.
//
// 背景: 2023-08 上交所/深交所主板启用"价格笼子"申报机制, 限价单的
// 申报价格不得偏离当前最优对价 ±2% (除了涨跌幅位置外)。 此规则
// 与日涨跌幅 ±10% 并存, 是"申报"环节的前置校验, 而非撮合规则。
//
// 本模块实现"4 套" 规则:
//  1. 沪深主板 (MainBoardSH/SZ): 日涨跌幅 ±10% + 申报价格笼子 ±2%
//  2. 创业板 (ChiNext): 日涨跌幅 ±20%, 无 2% 笼子
//  3. 科创板 (STAR): 日涨跌幅 ±20%, 无 2% 笼子
//  4. 北交所 (BSE): 日涨跌幅 ±30%, 无 2% 笼子
//
// CageRule 算法 (以沪/深主板 买入为例):
//  1. limit_up = prev_close × (1 + daily_limit)        // 涨停价
//  2. limit_down = prev_close × (1 - daily_limit)      // 跌停价
//  3. 若 order_price == limit_up 或 order_price == limit_down,
//     允许 (笼子规则不适用)
//  4. 否则:
//     a. ref = best_ask (有最优卖价时) 或 best_bid (有最优买价时) 或
//     last_price (无对价时)
//     b. order_price 必须满足: ref × (1 - 2%) ≤ order_price ≤ ref × (1 + 2%)
//  5. 同时任何方向都必须满足: limit_down ≤ order_price ≤ limit_up
//
// 卖出则对称: 卖单价必须 ≥ best_bid × (1 - 2%) 且 ≤ best_ask × (1 + 2%)。
//
// 注意: 本实现假设 prev_close > 0 且 best_bid ≤ best_ask; 数据源
// (OHLCV / Quote) 的清洗不在职责内。 若 prev_close <= 0, validator
// 返回 ErrMissingReferencePrice 由调用方决定是否降级处理。
package live

import (
	"fmt"
	"math"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// PriceCageError is returned by the validator when a limit-order price
// falls outside the allowed cage or exceeds the daily price limit. It
// carries the structured reason + computed bounds so callers can map
// the rejection to a user-friendly explanation.
type PriceCageError struct {
	Symbol     string  `json:"symbol"`
	Board      Board   `json:"board"`
	Direction  string  `json:"direction"`
	OrderPrice float64 `json:"order_price"`
	PrevClose  float64 `json:"prev_close"`
	LimitUp    float64 `json:"limit_up"`
	LimitDown  float64 `json:"limit_down"`
	Reference  float64 `json:"reference"`
	CageFloor  float64 `json:"cage_floor,omitempty"`
	CageCeil   float64 `json:"cage_ceil,omitempty"`
	Reason     string  `json:"reason"`
}

// Error implements the error interface.
func (e *PriceCageError) Error() string {
	switch e.Reason {
	case "limit_up_violation":
		return fmt.Sprintf("price cage: %s buy price %.4f exceeds limit-up %.4f (prev_close=%.4f, limit=+%.0f%%)",
			e.Symbol, e.OrderPrice, e.LimitUp, e.PrevClose, e.Board.DailyPriceLimit()*100)
	case "limit_down_violation":
		return fmt.Sprintf("price cage: %s sell price %.4f below limit-down %.4f (prev_close=%.4f, limit=-%.0f%%)",
			e.Symbol, e.OrderPrice, e.LimitDown, e.PrevClose, e.Board.DailyPriceLimit()*100)
	case "cage_violation":
		return fmt.Sprintf("price cage: %s price %.4f outside ±%.0f%% cage [%.4f, %.4f] around reference %.4f",
			e.Symbol, e.OrderPrice, e.Board.CagePercent()*100, e.CageFloor, e.CageCeil, e.Reference)
	case "non_positive_price":
		return fmt.Sprintf("price cage: %s order price must be positive, got %.4f", e.Symbol, e.OrderPrice)
	case "missing_reference":
		return fmt.Sprintf("price cage: %s missing reference price (prev_close/best bid-ask)", e.Symbol)
	default:
		return fmt.Sprintf("price cage: %s invalid order price %.4f (reason=%s)", e.Symbol, e.OrderPrice, e.Reason)
	}
}

// Is implements the optional errors.Is contract: callers can check
// errors.Is(err, &PriceCageError{}) to detect cage violations.
func (e *PriceCageError) Is(target error) bool {
	_, ok := target.(*PriceCageError)
	return ok
}

// ReferencePrice carries the live reference prices used to compute the
// cage bounds. At least one of BestBid / BestAsk / Last must be positive;
// the validator picks the appropriate one based on the order direction.
type ReferencePrice struct {
	BestBid   float64
	BestAsk   float64
	Last      float64
	PrevClose float64
}

// HasMinimum returns true if the reference has enough data to validate.
// We need prev_close for daily limit, and at least one of bid/ask/last
// for the cage (if the board requires it).
func (r ReferencePrice) HasMinimum() bool {
	return r.PrevClose > 0
}

// CageValidator validates A-share limit-order prices against the
// appropriate board rules.
//
// Construction:
//
//	validator := NewCageValidator()  // uses canonical A-share rules
//	err := validator.Validate(order, reference)
//
// The validator is stateless and safe for concurrent use.
type CageValidator struct {
	// OverrideBoard lets tests / special configurations force a board
	// (e.g. an ETF or a ST-flagged stock). If nil, board is derived
	// from the order's symbol via ClassifySymbol.
	OverrideBoard func(symbol string) Board
}

// NewCageValidator creates a validator with the canonical A-share rules.
func NewCageValidator() *CageValidator {
	return &CageValidator{}
}

// NewCageValidatorWithBoard creates a validator that uses a custom board
// resolver. This is useful for backtest / paper-trading setups that
// maintain their own symbol → board metadata.
func NewCageValidatorWithBoard(resolve func(symbol string) Board) *CageValidator {
	return &CageValidator{OverrideBoard: resolve}
}

// board returns the board for the given symbol, applying override if set.
func (v *CageValidator) board(symbol string) Board {
	if v.OverrideBoard != nil {
		if b := v.OverrideBoard(symbol); b != BoardUnknown {
			return b
		}
	}
	return ClassifySymbol(symbol)
}

// Validate checks whether the order's price is allowed for the given
// symbol, direction, and reference price. Returns nil if valid, or a
// *PriceCageError describing the violation.
//
// Market orders (OrderType=market) always pass — the cage is a
// limit-order submission rule. (The exchange will then fill the market
// order at the optimal price within the daily limit, which is the
// broker's responsibility.)
func (v *CageValidator) Validate(order *domain.Order, ref ReferencePrice) error {
	if order == nil {
		return &PriceCageError{Reason: "non_positive_price"}
	}
	if order.OrderType == domain.OrderTypeMarket {
		return nil
	}
	if order.LimitPrice <= 0 {
		return &PriceCageError{
			Symbol:     order.Symbol,
			OrderPrice: order.LimitPrice,
			Reason:     "non_positive_price",
		}
	}
	if !ref.HasMinimum() {
		return &PriceCageError{
			Symbol:     order.Symbol,
			OrderPrice: order.LimitPrice,
			Reason:     "missing_reference",
		}
	}

	board := v.board(order.Symbol)
	limit := board.DailyPriceLimit()
	limitUp := round4(ref.PrevClose * (1.0 + limit))
	limitDown := round4(ref.PrevClose * (1.0 - limit))

	// ① 涨跌幅绝对边界: 任何限价单必须落在 [limit_down, limit_up].
	if order.LimitPrice > limitUp {
		return &PriceCageError{
			Symbol: order.Symbol, Board: board,
			Direction:  string(order.Direction),
			OrderPrice: order.LimitPrice, PrevClose: ref.PrevClose,
			LimitUp: limitUp, LimitDown: limitDown,
			Reason: "limit_up_violation",
		}
	}
	if order.LimitPrice < limitDown {
		return &PriceCageError{
			Symbol: order.Symbol, Board: board,
			Direction:  string(order.Direction),
			OrderPrice: order.LimitPrice, PrevClose: ref.PrevClose,
			LimitUp: limitUp, LimitDown: limitDown,
			Reason: "limit_down_violation",
		}
	}

	// ② 价格笼子 (仅沪/深主板): 若订单价恰为涨停/跌停, 笼子规则不适用.
	if !board.HasCageRule() {
		return nil
	}
	if approxEqual(order.LimitPrice, limitUp) || approxEqual(order.LimitPrice, limitDown) {
		return nil
	}

	// 选取参考价: 买用 ask (or last as fallback), 卖用 bid (or last as fallback).
	var reference float64
	switch order.Direction {
	case domain.DirectionLong:
		reference = ref.BestAsk
		if reference <= 0 {
			reference = ref.Last
		}
	case domain.DirectionShort, domain.DirectionClose:
		reference = ref.BestBid
		if reference <= 0 {
			reference = ref.Last
		}
	default:
		// DirectionHold / unknown — fall back to last.
		reference = ref.Last
	}
	if reference <= 0 {
		// No reference for cage → conservatively allow; the daily
		// limit has already been enforced above. Brokers usually fall
		// back to last price in this case.
		return nil
	}

	cagePct := board.CagePercent()
	floor := round4(reference * (1.0 - cagePct))
	ceil := round4(reference * (1.0 + cagePct))

	if order.LimitPrice < floor || order.LimitPrice > ceil {
		return &PriceCageError{
			Symbol: order.Symbol, Board: board,
			Direction:  string(order.Direction),
			OrderPrice: order.LimitPrice, PrevClose: ref.PrevClose,
			LimitUp: limitUp, LimitDown: limitDown,
			Reference: reference, CageFloor: floor, CageCeil: ceil,
			Reason: "cage_violation",
		}
	}
	return nil
}

// LimitUpDown computes the daily limit up / limit down for the given
// board and prev close. Returns 0 for boards with no prev close.
func LimitUpDown(board Board, prevClose float64) (limitUp, limitDown float64) {
	if prevClose <= 0 {
		return 0, 0
	}
	limit := board.DailyPriceLimit()
	return round4(prevClose * (1.0 + limit)), round4(prevClose * (1.0 - limit))
}

// approxEqual compares two float64 with a tiny tolerance to absorb
// rounding error when the order price is essentially at the limit.
func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-4
}

// round4 rounds to 4 decimal places — A-share price tick is 0.01,
// so 4 decimals is plenty (e.g. 10.1234).
func round4(x float64) float64 {
	return math.Round(x*1e4) / 1e4
}
