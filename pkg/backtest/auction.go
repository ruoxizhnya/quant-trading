// Package backtest — A-share call auction (集合竞价) matching.
//
// P1-6 (ODR-018) — 集合竞价撮合 (9:15-9:25 + 14:57-15:00).
//
// 背景:
//   - 开盘集合竞价: 9:15-9:20 接受订单+撤单, 9:20-9:25 接受订单但禁撤单,
//     9:25 一次性按"最大成交量原则"撮合, 确定开盘价。
//   - 收盘集合竞价: 14:57-15:00 同样按最大成交量原则撮合, 确定收盘价
//     (主板 2018-08 启用, 创业板 2020-08 跟进, 科创板 2020-07 同步,
//     北交所 2021-11 启用)。
//
// Backtest 视角:
//   - 回测引擎按日粒度回放 OHLCV, 撮合价直接取自 dailyBar.Open / Close
//     (集合竞价撮合在交易所内部发生, 对外只发布 open/close 一根 K 线)。
//   - 但对于"我的订单在集合竞价能否成交 / 按什么价成交"的判定, 回测
//     需要一个 call auction 模拟器, 输入"日内产生的信号", 输出"集合
//     竞价阶段的成交"。
//   - 本模块提供两套 API:
//     ① Session + SessionAt: 给定一个 time.Time, 判定它属于哪个交易
//        时段, 决定信号的撮合窗口 (open / continuous / close).
//     ② CallAuctionMatcher.Match: 给定一组买卖单, 按"最大成交量 +
//        最小成交价"原则计算理论成交价和成交量, 用于回放假设场景
//        (e.g. T-1 收盘后模型预测的 open price vs 实际 open).
//
// 算法 (上交所/深交所通用):
//   1. 按价格档位 (price tick) 排序, 维护:
//      - cumBuy(p)  = Σ buy.qty for buy with limit_price >= p
//      - cumSell(p) = Σ sell.qty for sell with limit_price <= p
//      - matched(p) = min(cumBuy(p), cumSell(p))
//   2. clearing_price = argmax_p matched(p)
//   3. 若多个价格匹配量相同, 取最接近 prev_close 的 (主板规则: 中间价).
//   4. 在 clearing_price 上的剩余单边按"时间优先 + 等量按比例"分配。
//
// 注意: 这是"理论撮合器"而非实盘撮合 — 实盘没有逐档 LOB 数据可用。
// Backtest 假设的盘口快照是: prev_close (anchor) + 当日 open/close 区间。
package backtest

import (
	"math"
	"sort"
	"time"
)

// TradingSession enumerates A-share intraday sessions.
//
// Times are in Asia/Shanghai (UTC+8) — backtest callers should convert
// any UTC timestamps before passing to SessionAt.
type TradingSession string

const (
	// SessionPreOpen — 9:15-9:20 (开市前, 接受订单 + 可撤单).
	SessionPreOpen TradingSession = "pre_open"
	// SessionPreOpenFreeze — 9:20-9:25 (集合竞价冻结, 接受订单但不可撤单).
	SessionPreOpenFreeze TradingSession = "pre_open_freeze"
	// SessionOpeningMatch — 9:25 一次性撮合, 9:25-9:30 之间系统准备
	// 推送连续撮合. 回测视角下这一段没有新订单产生, 仅承接开盘撮合结果.
	SessionOpeningMatch TradingSession = "opening_match"
	// SessionMorningContinuous — 9:30-11:30 (上午连续撮合).
	SessionMorningContinuous TradingSession = "morning_continuous"
	// SessionLunch — 11:30-13:00 (午休, 不接受新订单).
	SessionLunch TradingSession = "lunch"
	// SessionAfternoonContinuous — 13:00-14:57 (下午连续撮合).
	SessionAfternoonContinuous TradingSession = "afternoon_continuous"
	// SessionClosingCall — 14:57-15:00 (收盘集合竞价, 上交所/深交所主板 +
	// 创业板/科创板/北交所 全部启用).
	SessionClosingCall TradingSession = "closing_call"
	// SessionClosed — 15:00 后 / 9:15 前 / 周末 / 节假日.
	SessionClosed TradingSession = "closed"
)

// String returns the canonical session name.
func (s TradingSession) String() string { return string(s) }

// SessionAt returns the trading session for a given timestamp.
//
// Inputs are expected in Asia/Shanghai local time. We do not perform
// the conversion ourselves — callers should normalize via time.FixedZone
// or by passing time.Time whose Location is Asia/Shanghai.
//
// Weekend (Sat/Sun) always returns SessionClosed.
func SessionAt(t time.Time) TradingSession {
	// Go's time.Weekday: Sunday=0, Monday=1, ..., Saturday=6.
	wd := t.Weekday()
	if wd == time.Saturday || wd == time.Sunday {
		return SessionClosed
	}
	// Convert to minutes since midnight for simple comparison.
	h, m, _ := t.Clock()
	minutes := h*60 + m
	switch {
	case minutes < 9*60+15:
		return SessionClosed
	case minutes < 9*60+20:
		return SessionPreOpen
	case minutes < 9*60+25:
		return SessionPreOpenFreeze
	case minutes < 9*60+30:
		return SessionOpeningMatch
	case minutes < 11*60+30:
		return SessionMorningContinuous
	case minutes < 13*60:
		return SessionLunch
	case minutes < 14*60+57:
		return SessionAfternoonContinuous
	case minutes < 15*60:
		return SessionClosingCall
	default:
		return SessionClosed
	}
}

// SessionWindow returns the start and end (in minutes-since-midnight) of
// the given session, for tests and reporting. End is exclusive.
func SessionWindow(s TradingSession) (startMin, endMin int) {
	switch s {
	case SessionPreOpen:
		return 9*60 + 15, 9*60 + 20
	case SessionPreOpenFreeze:
		return 9*60 + 20, 9*60 + 25
	case SessionOpeningMatch:
		return 9*60 + 25, 9*60 + 30
	case SessionMorningContinuous:
		return 9*60 + 30, 11*60 + 30
	case SessionLunch:
		return 11*60 + 30, 13*60
	case SessionAfternoonContinuous:
		return 13*60, 14*60 + 57
	case SessionClosingCall:
		return 14*60 + 57, 15*60
	case SessionClosed:
		return 0, 0
	}
	return 0, 0
}

// CallAuctionOrder is a single order submitted to a call auction.
// Direction: 'buy' or 'sell'. LimitPrice is the price the trader is
// willing to pay (buy) or accept (sell). Orders without a limit
// (market-on-close / market-on-open) are represented as LimitPrice=0
// and trade at whatever the clearing price turns out to be.
type CallAuctionOrder struct {
	ID         string
	Symbol     string
	Direction  string // "buy" or "sell"
	Quantity   float64
	LimitPrice float64 // 0 = market (trades at clearing)
	Timestamp  time.Time
}

// ClearingResult is the outcome of CallAuctionMatcher.Match.
type ClearingResult struct {
	// ClearingPrice is the theoretical price at which the call auction
	// matches the maximum number of shares. 0 if no match.
	ClearingPrice float64
	// MatchedVolume is the total volume cleared (single-sided count, e.g.
	// 1000 shares traded total).
	MatchedVolume float64
	// BuyFillRatio / SellFillRatio is the fill ratio for unmatched orders
	// (0.0 to 1.0). 1.0 means fully filled, 0.0 means no fill.
	// Pro-rata allocation when matched volume < total demanded.
	BuyFillRatio  float64
	SellFillRatio float64
	// Fills maps order ID → filled quantity (≤ order.Quantity).
	Fills map[string]float64
	// NoMatchReason explains why no clearing happened. Empty if matched.
	NoMatchReason string
}

// CallAuctionMatcher implements the A-share "max-volume" call-auction
// matching algorithm. It is stateless and safe for concurrent use.
type CallAuctionMatcher struct {
	// PriceTick is the minimum price increment (default 0.01 for stocks).
	PriceTick float64
	// AnchorPrice is the reference price (typically prev_close) used to
	// disambiguate ties. If zero, ties are broken by lower price.
	AnchorPrice float64
}

// NewCallAuctionMatcher creates a matcher with the default A-share
// price tick (0.01 CNY) and no anchor price.
func NewCallAuctionMatcher() *CallAuctionMatcher {
	return &CallAuctionMatcher{PriceTick: 0.01}
}

// WithAnchor sets the reference price (prev close) and returns the
// matcher, for fluent construction. Used to break ties in favour of
// the price closest to anchor (per 撮合细则 §3.4).
func (m *CallAuctionMatcher) WithAnchor(prevClose float64) *CallAuctionMatcher {
	m.AnchorPrice = prevClose
	return m
}

// Match runs the call auction and returns the clearing result.
//
// Algorithm (simplified, follows 上交所/深交所 撮合细则):
//  1. Bucket buy orders by LimitPrice (or infinity for market).
//  2. Bucket sell orders by LimitPrice (or 0 for market).
//  3. For each candidate clearing price p on the price grid
//     (union of all limit prices ± 1 tick):
//     - cumBuy(p) = Σ buy.qty for buy with LimitPrice >= p
//     - cumSell(p) = Σ sell.qty for sell with LimitPrice <= p
//     - matchable(p) = min(cumBuy, cumSell)
//  4. Pick p* = argmax_p matchable(p).
//  5. Tie-break: prefer p closer to AnchorPrice; if still tied, prefer lower.
//  6. Fills: for buys with LimitPrice >= p*, pro-rata; for sells with
//     LimitPrice <= p*, pro-rata. Market orders always fully filled
//     (up to the matched volume).
func (m *CallAuctionMatcher) Match(buys, sells []CallAuctionOrder) ClearingResult {
	result := ClearingResult{Fills: make(map[string]float64)}

	if len(buys) == 0 || len(sells) == 0 {
		result.NoMatchReason = "one_side_empty"
		return result
	}

	// Build candidate price set: union of all limit prices, plus
	// ±1 tick of each to handle "limit just outside" cases.
	tick := m.PriceTick
	if tick <= 0 {
		tick = 0.01
	}
	candidateSet := map[float64]struct{}{}
	for _, o := range buys {
		if o.LimitPrice > 0 {
			candidateSet[o.LimitPrice] = struct{}{}
			candidateSet[o.LimitPrice+tick] = struct{}{} // one tick above (more lenient)
		}
	}
	for _, o := range sells {
		if o.LimitPrice > 0 {
			candidateSet[o.LimitPrice] = struct{}{}
			candidateSet[o.LimitPrice-tick] = struct{}{}
		}
	}
	// 总是考虑 anchor 价格 (± tick)
	if m.AnchorPrice > 0 {
		candidateSet[m.AnchorPrice] = struct{}{}
	}
	if len(candidateSet) == 0 {
		result.NoMatchReason = "no_price_levels"
		return result
	}
	candidates := make([]float64, 0, len(candidateSet))
	for p := range candidateSet {
		candidates = append(candidates, auctionRound4(p))
	}
	sort.Float64s(candidates)

	// For each candidate, compute matchable volume.
	type pv struct {
		price    float64
		matchable float64
	}
	scored := make([]pv, 0, len(candidates))
	for _, p := range candidates {
		var cumBuy, cumSell float64
		for _, o := range buys {
			if o.LimitPrice == 0 || o.LimitPrice >= p {
				cumBuy += o.Quantity
			}
		}
		for _, o := range sells {
			if o.LimitPrice == 0 || o.LimitPrice <= p {
				cumSell += o.Quantity
			}
		}
		scored = append(scored, pv{price: p, matchable: math.Min(cumBuy, cumSell)})
	}

	// Find max matchable.
	best := scored[0]
	for _, s := range scored[1:] {
		if s.matchable > best.matchable {
			best = s
			continue
		}
		if s.matchable == best.matchable && s.matchable > 0 {
			// Tie-break: prefer closer to anchor.
			distNew := math.Abs(s.price - m.AnchorPrice)
			distBest := math.Abs(best.price - m.AnchorPrice)
			if distNew < distBest {
				best = s
			} else if distNew == distBest && s.price < best.price {
				best = s
			}
		}
	}

	if best.matchable <= 0 {
		result.NoMatchReason = "no_overlap"
		return result
	}
	result.ClearingPrice = best.price
	result.MatchedVolume = best.matchable

	// Allocate fills.
	result.Fills = allocateFills(buys, sells, best.price, best.matchable, tick)
	return result
}

// allocateFills distributes matched volume to orders, using pro-rata
// allocation within each side. Market orders (LimitPrice==0) are filled
// first to the extent matched volume allows.
func allocateFills(buys, sells []CallAuctionOrder, price float64, matched float64, tick float64) map[string]float64 {
	fills := make(map[string]float64)

	// Buy allocation: market orders first, then limit orders at or above
	// the clearing price. Pro-rata when total demand > matched.
	var marketBuyTotal, limitBuyTotal float64
	var marketBuys, limitBuys []CallAuctionOrder
	for _, o := range buys {
		if o.LimitPrice == 0 || o.LimitPrice >= price {
			if o.LimitPrice == 0 {
				marketBuys = append(marketBuys, o)
				marketBuyTotal += o.Quantity
			} else {
				limitBuys = append(limitBuys, o)
				limitBuyTotal += o.Quantity
			}
		}
	}

	var sellTotal float64
	var eligibleSells []CallAuctionOrder
	for _, o := range sells {
		if o.LimitPrice == 0 || o.LimitPrice <= price {
			eligibleSells = append(eligibleSells, o)
			sellTotal += o.Quantity
		}
	}

	totalBuyDemand := marketBuyTotal + limitBuyTotal
	buyRatio := 1.0
	if totalBuyDemand > 0 && totalBuyDemand > matched {
		buyRatio = matched / totalBuyDemand
	}
	sellRatio := 1.0
	if sellTotal > 0 && sellTotal > matched {
		sellRatio = matched / sellTotal
	}

	// ① market buys: 优先全额
	matchedAfterMarket := matched
	for _, o := range marketBuys {
		fill := math.Min(o.Quantity, matchedAfterMarket)
		fills[o.ID] = fill
		matchedAfterMarket -= fill
	}
	// ② limit buys: 剩余按比例
	limitRemaining := matchedAfterMarket
	for _, o := range limitBuys {
		raw := o.Quantity * buyRatio
		fill := math.Min(o.Quantity, math.Min(raw, limitRemaining))
		fills[o.ID] = fill
		limitRemaining -= fill
	}

	// ③ sells (market first, then limit at or below)
	matchedAfterSell := matched
	var marketSells, limitSells []CallAuctionOrder
	for _, o := range eligibleSells {
		if o.LimitPrice == 0 {
			marketSells = append(marketSells, o)
		} else {
			limitSells = append(limitSells, o)
		}
	}
	for _, o := range marketSells {
		fill := math.Min(o.Quantity, matchedAfterSell)
		fills[o.ID] = fill
		matchedAfterSell -= fill
	}
	sellLimitRemaining := matchedAfterSell
	for _, o := range limitSells {
		raw := o.Quantity * sellRatio
		fill := math.Min(o.Quantity, math.Min(raw, sellLimitRemaining))
		fills[o.ID] = fill
		sellLimitRemaining -= fill
	}

	return fills
}

// round4 rounds to 4 decimal places (same helper as pkg/live).
func auctionRound4(x float64) float64 {
	return math.Round(x*1e4) / 1e4
}

// FillRatio returns the fill ratio (filled / total demand) for a
// given order ID in a ClearingResult. Convenience helper for callers
// who want to know "did this order get filled?".
func (r ClearingResult) FillRatio(orderID string, totalQty float64) float64 {
	if totalQty <= 0 {
		return 0
	}
	filled, ok := r.Fills[orderID]
	if !ok {
		return 0
	}
	return filled / totalQty
}

// IsFullyFilled reports whether the order got a 100% fill. The orderQty
// argument must be the original order quantity (Fills only stores filled
// amount, not the order reference).
func (r ClearingResult) IsFullyFilled(orderID string, orderQty float64) bool {
	if orderQty <= 0 {
		return false
	}
	filled, ok := r.Fills[orderID]
	return ok && filled >= orderQty-1e-6
}
