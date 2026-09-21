package risk

import (
	"math"

	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
)

// A-share order-quantity rules (申报数量规则).
//
// These are exchange rules, not tunables: an order that violates them
// is rejected by the exchange, so they are hard-coded constants
// rather than config keys. Verified against the exchanges' published
// rules on 2026-09-21:
//
//   - 上交所《交易规则（2023 年修订）》3.3.8 — 通过竞价交易买入证券的，
//     申报数量应当为 100 股（份）或其整数倍。
//   - 深交所《交易规则（2023 年修订）》3.3.8 — 同上（创业板适用）。
//   - 上交所《交易规则（2023 年修订）》6.1.7 — 通过限价 / 市价申报
//     买卖科创板股票的，单笔申报数量应当不小于 200 股。
//   - 北交所《交易规则》3.3.8（2026-07-06 起施行）— 通过竞价交易
//     买卖股票的，单笔申报数量应当不低于 100 股。
//
// Note that the "100+1" reform aired in 2023-08 — which would have
// allowed main-board and ChiNext orders of "100 shares plus any
// 1-share increment" — was only ever 拟调整 / 研究. It never took
// effect: the 2023 revisions still require an exact multiple of 100,
// and that is still what the exchanges publish today. Code that
// assumed "100+1" landed would be wrong.
const (
	// LotSize is the main-board / ChiNext board lot (一手). Buy
	// orders must be an exact multiple of this.
	LotSize = 100

	// STARMinShares is the STAR market (科创板) minimum buy quantity.
	// At or above it, increments of 1 share are allowed, so the only
	// constraint below the floor is the floor itself.
	STARMinShares = 200

	// BSEMinShares is the BSE (北交所) minimum buy quantity. Same
	// shape as STAR: a floor, then 1-share increments.
	BSEMinShares = 100
)

// NormalizeOrderQuantity rounds a desired share count to a quantity
// the exchange will actually accept, for the board that symbol trades
// on.
//
// The rules differ per board, so a blanket "round down to 100" is
// wrong in both directions:
//
//   - Main board / ChiNext (60/00/001/002/300/301…): floor to a
//     multiple of 100.
//   - STAR (688/689): floor to a whole share, but never below 200.
//     Rounding these to 100 would systematically under-buy.
//   - BSE (8x/4x): floor to a whole share, but never below 100.
//
// Quantities that fall short of the board minimum are lifted to the
// minimum rather than dropped, so a main-board signal wanting 50
// shares becomes a 100-share order. Callers must therefore tolerate
// the normalized quantity exceeding the requested one.
//
// Two cases deliberately yield 0 rather than a lifted minimum:
// a non-positive input, and an input below one whole share. The
// latter matters because positionValue / price can be a fraction
// when the target position is tiny; lifting 0.4 shares to 100 would
// place an order ~250x the intended size. "Below one lot" is a sizing
// decision, "below one share" is a mistake.
func NormalizeOrderQuantity(shares float64, symbol string) float64 {
	if shares < 1 {
		return 0
	}

	switch marketdata.ClassifySymbol(symbol) {
	case marketdata.BoardSTAR:
		if shares < STARMinShares {
			return STARMinShares
		}
		return math.Floor(shares)

	case marketdata.BoardBSE:
		if shares < BSEMinShares {
			return BSEMinShares
		}
		return math.Floor(shares)

	default:
		// Main board, ChiNext, and anything unclassifiable. Falling
		// back to the main-board rule for BoardUnknown is the
		// conservative choice: it is the strictest of the three, so
		// a mis-classified symbol produces an order that is still
		// legal everywhere.
		lots := math.Floor(shares / LotSize)
		if lots < 1 {
			return LotSize
		}
		return lots * LotSize
	}
}
