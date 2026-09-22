package backtest

// A-share daily price-limit resolution.
//
// AUD-07 (ODR-065 H2) + AUD-08 (ODR-065 H3/H4): before this file, the
// engine picked a limit rate from a 3-way if/else in engine_daily.go
// using only Normal/ST/New config values. That had two defects:
//
//  1. No board dimension. ChiNext (300/301), STAR (688/689) and BSE
//     (8x/4x) all fell through to the 10% main-board rate, so a 20%
//     move on ChiNext was judged non-limit and a genuine limit-up was
//     treated as tradable.
//  2. ST detection used name[:2], which can never match the 3- and
//     4-character prefixes ("*ST", "SST", "S*ST") that the regulation
//     actually uses.
//
// Both are now handled here. The board lookup reuses
// pkg/marketdata.ClassifySymbol / Board.DailyPriceLimit rather than
// re-deriving prefixes, because that package already had a correct
// implementation that the engine simply was not calling.

import (
	"math"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
)

// stLimitChangeDate is the effective date of the rule change that
// raised the main-board risk-warning (ST / *ST) daily limit from
// ±5% to ±10%, aligning it with other main-board stocks.
//
// 沪深北三大交易所 2026-04 修订交易规则，2026-07-06 起施行。
// ChiNext / STAR risk-warning stocks were NOT changed — they stay at
// their board limit (±20%).
//
// Stored as a date-only value in the exchange's local calendar; the
// comparison is done on the trading day, not on wall-clock time.
var stLimitChangeDate = time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)

// stLimitBeforeChange and stLimitAfterChange are the main-board ST
// limits either side of stLimitChangeDate.
const (
	stLimitBeforeChange = 0.05
	stLimitAfterChange  = 0.10
)

// PriceLimitInput carries everything the limit decision depends on.
//
// Kept as a struct rather than a long parameter list so that adding a
// dimension later (e.g. the AUD-20 fee-history treatment, or a proper
// first-N-days new-stock rule) does not churn every call site.
type PriceLimitInput struct {
	// Symbol is the canonical ts_code ("000001.SZ", "300750.SZ").
	// Used for board classification. Empty => BoardUnknown => 10%.
	Symbol string

	// Name is the stock's display name, used for ST detection
	// ("*ST某某", "ST某某", "SST某某"). Not the symbol — passing a
	// ts_code here is a silent no-op (see TestPriceLimit_STDetection).
	Name string

	// TradeDays is the number of trading days since listing, as the
	// caller estimates it. Only compared against newStockDays.
	//
	// KNOWN INACCURACY (accepted, AUD-07 scope): the caller derives
	// this as (calendar days / 7 * 5), which ignores holidays. It is
	// also compared against newStockDays (config default 60), whereas
	// the actual rule is "no limit for the first 5 trading days, then
	// the board limit". So the new-stock branch is wrong in both the
	// threshold and the rate. Left as-is deliberately — see TASKS
	// AUD-07 notes — but named here so the next reader does not
	// mistake it for a modelled rule.
	TradeDays int

	// AsOf is the trading day the limit is being evaluated for.
	// Drives the date-segmented ST rate. Zero value is treated as
	// "after all known rule changes" so that a caller which cannot
	// supply a date gets today's rules rather than a silent 5%.
	AsOf time.Time
}

// PriceLimitConfigValues is the subset of the engine's TradingConfig
// that resolvePriceLimit needs. Passed explicitly instead of reaching
// into the engine so the function stays pure and table-testable.
type PriceLimitConfigValues struct {
	Normal       float64
	ST           float64 // main-board ST rate AFTER stLimitChangeDate
	STBefore     float64 // main-board ST rate BEFORE stLimitChangeDate
	New          float64
	NewStockDays int
}

// resolvePriceLimit returns the daily price-limit fraction (0.10 for
// ±10%) for the given stock on the given day.
//
// Precedence, and why:
//
//  1. New stock — takes priority over everything, including ST. A
//     newly listed stock cannot simultaneously be under a risk warning.
//  2. ST — the *board* decides which rate applies. Main-board ST is
//     date-segmented (5% before 2026-07-06, 10% after); ChiNext and
//     STAR ST stay at their board limit (20%). This is the part a
//     naive `if isST { return stRate }` gets wrong: a *ST stock on
//     ChiNext is still ±20%.
//  3. Board — ChiNext/STAR 20%, BSE 30%, everything else 10%, via
//     pkg/marketdata.
func resolvePriceLimit(in PriceLimitInput, cfg PriceLimitConfigValues) float64 {
	// 1. New stock. Deliberately retains the historical (incorrect)
	//    behaviour — see PriceLimitInput.TradeDays.
	if cfg.NewStockDays > 0 && in.TradeDays < cfg.NewStockDays && cfg.New > 0 {
		return cfg.New
	}

	board := marketdata.ClassifySymbol(in.Symbol)
	boardLimit := board.DailyPriceLimit()

	// 2. ST.
	if IsRiskWarningName(in.Name) {
		switch board {
		case marketdata.BoardChiNext, marketdata.BoardSTAR:
			// Risk-warning stocks on registration-based boards keep
			// the board limit; there is no 5%/10% narrowing.
			return boardLimit
		case marketdata.BoardBSE:
			// BSE has no ST regime in the main-board sense; keep 30%.
			return boardLimit
		default:
			// Main board (and unknown, which is treated as main board):
			// date-segmented.
			before := cfg.STBefore
			if before <= 0 {
				before = stLimitBeforeChange
			}
			after := cfg.ST
			if after <= 0 {
				after = stLimitAfterChange
			}
			if !in.AsOf.IsZero() && in.AsOf.Before(stLimitChangeDate) {
				return before
			}
			return after
		}
	}

	// 3. Plain board limit.
	if board == marketdata.BoardUnknown && cfg.Normal > 0 {
		// BoardUnknown only happens for malformed / unclassifiable
		// ts_codes. Honour the configured Normal rate there rather
		// than marketdata's own 10% default, so operators who tune
		// Normal still control the fallback case.
		return cfg.Normal
	}
	return boardLimit
}

// IsRiskWarningName reports whether a stock's display name carries a
// risk-warning (ST-family) prefix.
//
// AUD-08 (ODR-065 H3): the previous implementation was
//
//	prefix := name[:2]
//	return prefix == "ST" || prefix == "*ST" || prefix == "SST" || prefix == "S*ST"
//
// which could only ever match "ST": the other three alternatives are
// 3–4 characters long and can never equal a 2-character slice. The
// condition looked like it handled all four forms and in fact handled
// one. Detection is now prefix-based.
//
// AUD-22 (ODR-065): the implementation MOVED to pkg/marketdata (leaf)
// because the order path needs it and pkg/backtest is a parent package —
// pkg/live and pkg/risk cannot import it without a reverse dependency.
// This wrapper is kept so the existing call sites in this package
// (resolvePriceLimit, hasSTPrefix) do not churn, and so the price-limit
// code keeps reading as "ask the board question here".
//
// There is exactly ONE implementation; do not re-inline it here.
func IsRiskWarningName(name string) bool {
	return marketdata.IsRiskWarningName(name)
}

// roundToCent rounds a price to the nearest 0.01 CNY, the tick size
// for A-share equities.
//
// AUD-07 (ODR-065 H2): the engine compared raw float products
// (prevClose * (1 + rate)) against the bar close. Exchange limit
// prices are rounded to the cent, so an unrounded bound is up to
// half a cent off — enough to change the verdict on a bar that closes
// exactly at the limit, which is precisely the case this logic
// exists to detect.
//
// math.Round rounds half away from zero, which matches the exchange
// convention for positive prices. Prices are always positive here, so
// negative-half behaviour is not a concern.
func roundToCent(price float64) float64 {
	return math.Round(price*100) / 100
}

// LimitPrices computes the rounded upper and lower limit prices for a
// previous close and a rate. Returns (upper, lower).
func LimitPrices(prevClose, rate float64) (upper, lower float64) {
	return roundToCent(prevClose * (1 + rate)), roundToCent(prevClose * (1 - rate))
}
