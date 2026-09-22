package risk

import (
	"fmt"
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

// ValidateOrderQuantity reports whether `shares` is a quantity the
// exchange will accept for `symbol`, returning a descriptive error if
// not. isSell selects the sell-side rule set — pass true when the order
// closes a long, mirroring portfolio.ComputeFees' isSell argument.
//
// This is the VALIDATION counterpart of NormalizeOrderQuantity, and the
// two share the board table above so they cannot drift. The split is
// deliberate: NormalizeOrderQuantity answers "what should I send
// instead?", ValidateOrderQuantity answers "may I send this?". A broker
// boundary needs the latter — silently resizing an order at the last hop
// would hide a sizing bug instead of surfacing it.
//
// AUD-21 (ODR-065): pkg/live/broker/xtp used to enforce a blanket
// `int(quantity)%100 != 0`. That is the MAIN-BOARD / ChiNext rule only,
// so it rejected legal STAR (>=200, 1-share increments) and BSE (>=100,
// 1-share increments) orders — including orders this package's own
// normalizer had just produced. Two mechanisms, each correct in
// isolation, wrong when joined (PITFALLS §1). It also truncated through
// int(), so 100.9 shares passed a check that 250.0 shares failed.
//
// Sell-side asymmetry, and why it is not an oversight: 沪 3.3.8 /
// 深 3.3.8 allow an ODD-LOT sell when it is the whole remaining balance
// ("卖出证券时，余额不足100股（份）部分，应当一次性申报卖出"). Whether a
// given sell is that final odd lot depends on position state this
// function does not have, so it does not guess: for sells it checks only
// that the quantity is a positive whole number and leaves the
// remainder rule to the counter. Guessing "not a multiple of 100 =>
// reject" here would re-introduce exactly the false rejection AUD-21 is
// about, just on the other side.
//
// NOT enforced here: the per-order maximum (100万股 on 沪深/北交所;
// 创业板 限价 30万 / 市价 15万; 科创板 限价 10万 / 市价 5万). That needs
// the order type as well as the board, and is tracked separately — see
// docs/TASKS.md AUD-22 notes.
func ValidateOrderQuantity(shares float64, symbol string, isSell bool) error {
	if shares <= 0 {
		return fmt.Errorf("order quantity must be positive, got %v", shares)
	}
	if shares != math.Trunc(shares) {
		return fmt.Errorf("order quantity must be a whole number of shares, got %v", shares)
	}
	qty := math.Trunc(shares)

	if isSell {
		// See the doc comment: the odd-lot condition needs position
		// state, so it is left to the counter.
		return nil
	}

	switch marketdata.ClassifySymbol(symbol) {
	case marketdata.BoardSTAR:
		if qty < STARMinShares {
			return fmt.Errorf("STAR (科创板) buy orders must be at least %d shares, got %v",
				STARMinShares, shares)
		}
		return nil

	case marketdata.BoardBSE:
		if qty < BSEMinShares {
			return fmt.Errorf("BSE (北交所) buy orders must be at least %d shares, got %v",
				BSEMinShares, shares)
		}
		return nil

	default:
		if math.Mod(qty, LotSize) != 0 {
			return fmt.Errorf("main-board/ChiNext buy orders must be a multiple of %d shares (1 lot), got %v",
				LotSize, shares)
		}
		return nil
	}
}
