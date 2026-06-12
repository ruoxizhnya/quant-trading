package backtest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// P1-6 (ODR-018) — A-share call auction tests
//
// 覆盖:
//   - 7 个交易时段判定 (周末 / 9:15-15:00 全部切片)
//   - 撮合算法: 最大成交量 + 中间价 tie-break + pro-rata 分配
//   - 边界: 单边空, 价格无重叠, 市场单, 涨停/跌停
// ---------------------------------------------------------------------------

// shanghai returns a time in Asia/Shanghai (UTC+8) for the given
// y/m/d/h/m components, so the tests don't depend on the runner's TZ.
func shanghai(y int, m time.Month, d, h, min int) time.Time {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	return time.Date(y, m, d, h, min, 0, 0, loc)
}

func TestSessionAt_AllSessions(t *testing.T) {
	// 2026-06-10 is a Wednesday.
	cases := []struct {
		hh, mm int
		want   TradingSession
	}{
		{0, 0, SessionClosed},         // midnight
		{9, 0, SessionClosed},         // before 9:15
		{9, 14, SessionClosed},        // last minute before pre-open
		{9, 15, SessionPreOpen},       // 9:15 sharp
		{9, 19, SessionPreOpen},       // last minute of pre-open
		{9, 20, SessionPreOpenFreeze}, // 9:20 — freeze begins
		{9, 24, SessionPreOpenFreeze}, // 9:24 — last minute of freeze
		{9, 25, SessionOpeningMatch},  // 9:25 — match begins
		{9, 29, SessionOpeningMatch},  // 9:29 — last minute before continuous
		{9, 30, SessionMorningContinuous},
		{11, 29, SessionMorningContinuous},
		{11, 30, SessionLunch},
		{12, 0, SessionLunch},
		{12, 59, SessionLunch},
		{13, 0, SessionAfternoonContinuous},
		{14, 56, SessionAfternoonContinuous},
		{14, 57, SessionClosingCall},
		{14, 59, SessionClosingCall},
		{15, 0, SessionClosed},
		{18, 0, SessionClosed},
		{23, 59, SessionClosed},
	}
	for _, tc := range cases {
		ts := shanghai(2026, 6, 10, tc.hh, tc.mm)
		got := SessionAt(ts)
		assert.Equal(t, tc.want, got, "%02d:%02d → %s, got %s", tc.hh, tc.mm, tc.want, got)
	}
}

func TestSessionAt_Weekend(t *testing.T) {
	// 2026-06-13 is a Saturday
	sat := shanghai(2026, 6, 13, 10, 0)
	assert.Equal(t, SessionClosed, SessionAt(sat))
	// 2026-06-14 is a Sunday
	sun := shanghai(2026, 6, 14, 10, 0)
	assert.Equal(t, SessionClosed, SessionAt(sun))
}

func TestSessionWindow(t *testing.T) {
	cases := []struct {
		s        TradingSession
		wantStr  string
		wantMin  int
		wantEnd  int
	}{
		{SessionPreOpen, "09:15-09:20", 555, 560},
		{SessionPreOpenFreeze, "09:20-09:25", 560, 565},
		{SessionOpeningMatch, "09:25-09:30", 565, 570},
		{SessionMorningContinuous, "09:30-11:30", 570, 690},
		{SessionLunch, "11:30-13:00", 690, 780},
		{SessionAfternoonContinuous, "13:00-14:57", 780, 897},
		{SessionClosingCall, "14:57-15:00", 897, 900},
		{SessionClosed, "00:00-00:00", 0, 0},
	}
	for _, tc := range cases {
		t.Run(string(tc.s), func(t *testing.T) {
			gotStart, gotEnd := SessionWindow(tc.s)
			assert.Equal(t, tc.wantMin, gotStart, "start")
			assert.Equal(t, tc.wantEnd, gotEnd, "end")
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Call auction matching algorithm
// ─────────────────────────────────────────────────────────────────────────────

func TestCallAuction_EmptySide(t *testing.T) {
	m := NewCallAuctionMatcher()
	r := m.Match(nil, []CallAuctionOrder{{ID: "s1", Direction: "sell", Quantity: 100, LimitPrice: 10.0}})
	assert.Equal(t, "one_side_empty", r.NoMatchReason)
	assert.Zero(t, r.ClearingPrice)
}

func TestCallAuction_NoOverlap(t *testing.T) {
	m := NewCallAuctionMatcher()
	buys := []CallAuctionOrder{{ID: "b1", Direction: "buy", Quantity: 100, LimitPrice: 9.0}}
	sells := []CallAuctionOrder{{ID: "s1", Direction: "sell", Quantity: 100, LimitPrice: 11.0}}
	r := m.Match(buys, sells)
	assert.Equal(t, "no_overlap", r.NoMatchReason)
}

func TestCallAuction_BasicMatch(t *testing.T) {
	// 买: 100@10.00, 100@10.05  (cumBuy=200@10.00, cumBuy=100@10.05)
	// 卖: 150@10.00  (cumSell=150@10.00)
	// Matchable: 150@10.00 (min of 200/150)
	m := NewCallAuctionMatcher()
	buys := []CallAuctionOrder{
		{ID: "b1", Direction: "buy", Quantity: 100, LimitPrice: 10.00},
		{ID: "b2", Direction: "buy", Quantity: 100, LimitPrice: 10.05},
	}
	sells := []CallAuctionOrder{
		{ID: "s1", Direction: "sell", Quantity: 150, LimitPrice: 10.00},
	}
	r := m.Match(buys, sells)
	assert.Equal(t, 10.00, r.ClearingPrice)
	assert.Equal(t, 150.0, r.MatchedVolume)
	// Both buys: pro-rata within matched demand
	// total demand = 200, matched = 150 → ratio = 0.75
	// b1=100 → 75, b2=100 → 75
	assert.InDelta(t, 75.0, r.Fills["b1"], 0.5)
	assert.InDelta(t, 75.0, r.Fills["b2"], 0.5)
	// sell 全部成交 150
	assert.Equal(t, 150.0, r.Fills["s1"])
}

func TestCallAuction_ProRataAllocation(t *testing.T) {
	// 买: 100@10.10, 100@10.00, 100@9.90
	// 卖: 150@9.95
	// 候选价: 9.89, 9.90, 9.91, 9.94, 9.95, 9.96, 10.00, 10.10, 10.11
	// 9.95: cumBuy=200 (b1 10.10, b2 10.00; b3 9.90 不达), cumSell=150 → 150 ← max
	// 其它价格都不超 150
	// → clearing @ 9.95, matched=150
	// 买侧 eligible (>=9.95): b1 (10.10), b2 (10.00). b3 (9.90) 不达 → 0.
	// total demand = 200, matched = 150, ratio = 0.75
	// b1 = 100*0.75 = 75, b2 = 75, b3 = 0
	m := NewCallAuctionMatcher()
	buys := []CallAuctionOrder{
		{ID: "b1", Direction: "buy", Quantity: 100, LimitPrice: 10.10},
		{ID: "b2", Direction: "buy", Quantity: 100, LimitPrice: 10.00},
		{ID: "b3", Direction: "buy", Quantity: 100, LimitPrice: 9.90},
	}
	sells := []CallAuctionOrder{
		{ID: "s1", Direction: "sell", Quantity: 150, LimitPrice: 9.95},
	}
	r := m.Match(buys, sells)
	assert.Equal(t, 9.95, r.ClearingPrice)
	assert.Equal(t, 150.0, r.MatchedVolume)
	assert.InDelta(t, 75.0, r.Fills["b1"], 0.5)
	assert.InDelta(t, 75.0, r.Fills["b2"], 0.5)
	assert.InDelta(t, 0.0, r.Fills["b3"], 0.5)
	assert.Equal(t, 150.0, r.Fills["s1"])
}

func TestCallAuction_TieBreakByAnchor(t *testing.T) {
	// 设计: 让多个候选价格匹配量相同, 但 anchor 唯一.
	// 候选 9.89, 9.90, 9.91, 10.00, 10.09, 10.10, 10.11 (anchor 10.00 在集合中)
	// 9.90: cumBuy=200, cumSell=100 → 100
	// 10.00: cumBuy=200, cumSell=100 → 100
	// 10.09: cumBuy=100, cumSell=200 → 100
	// 10.10: cumBuy=100, cumSell=200 → 100
	// Distance from anchor 10.00: 0.10 / 0.00 / 0.09 / 0.10
	// → 10.00 wins (closest to anchor, distance 0).
	buys := []CallAuctionOrder{
		{ID: "b1", Direction: "buy", Quantity: 100, LimitPrice: 9.90},
		{ID: "b2", Direction: "buy", Quantity: 100, LimitPrice: 10.10},
	}
	sells := []CallAuctionOrder{
		{ID: "s1", Direction: "sell", Quantity: 100, LimitPrice: 9.90},
		{ID: "s2", Direction: "sell", Quantity: 100, LimitPrice: 10.10},
	}
	m := NewCallAuctionMatcher().WithAnchor(10.00)
	r := m.Match(buys, sells)
	assert.Equal(t, 100.0, r.MatchedVolume)
	assert.Equal(t, 10.00, r.ClearingPrice, "tie broken: 10.00 is closest to anchor (distance 0)")
}

func TestCallAuction_TieBreakByLowerWhenEquidistant(t *testing.T) {
	// anchor 0 → 所有 tie 都用 lower fallback.
	// 候选 9.89, 9.90, 9.91, 10.09, 10.10, 10.11
	// 9.90: cumBuy=200, cumSell=100 → 100
	// 10.10: cumBuy=100, cumSell=200 → 100
	// distance from anchor 0: 9.90 vs 10.10 → 9.90 closer
	buys := []CallAuctionOrder{
		{ID: "b1", Direction: "buy", Quantity: 100, LimitPrice: 9.90},
		{ID: "b2", Direction: "buy", Quantity: 100, LimitPrice: 10.10},
	}
	sells := []CallAuctionOrder{
		{ID: "s1", Direction: "sell", Quantity: 100, LimitPrice: 9.90},
		{ID: "s2", Direction: "sell", Quantity: 100, LimitPrice: 10.10},
	}
	m := NewCallAuctionMatcher() // no anchor
	r := m.Match(buys, sells)
	assert.Equal(t, 100.0, r.MatchedVolume)
	// No anchor, 9.90 与 10.10 都入选, 距离 0 → 平手 → 选较低价 9.90
	assert.Equal(t, 9.90, r.ClearingPrice)
}

func TestCallAuction_MarketOrdersFilledFirst(t *testing.T) {
	// 买市价单 (LimitPrice=0) 应在 limit 单之前全额撮合.
	buys := []CallAuctionOrder{
		{ID: "b1", Direction: "buy", Quantity: 50, LimitPrice: 0},   // market
		{ID: "b2", Direction: "buy", Quantity: 100, LimitPrice: 10.10},
	}
	sells := []CallAuctionOrder{
		{ID: "s1", Direction: "sell", Quantity: 100, LimitPrice: 10.00},
	}
	// 候选 9.99, 10.00, 10.10, 10.11
	// 10.00: cumBuy=150 (both), cumSell=100 → 100 ← max
	// 10.10: cumBuy=100 (b2 only), cumSell=100 → 100 (tie, 10.10 closer to no anchor → 0 dist; 10.00 also 0)
	// 距离相等取较低 → 10.00
	m := NewCallAuctionMatcher()
	r := m.Match(buys, sells)
	assert.Equal(t, 10.00, r.ClearingPrice)
	assert.Equal(t, 100.0, r.MatchedVolume)
	// Market buy b1: 优先全部成交 50
	assert.Equal(t, 50.0, r.Fills["b1"])
	// Limit buy b2: 剩余 50 全给 b2 (b2 wants 100, gets 50 from remaining pool)
	assert.Equal(t, 50.0, r.Fills["b2"])
	// Sell s1: 100 全成交
	assert.Equal(t, 100.0, r.Fills["s1"])
}

func TestCallAuction_LimitUpScenario(t *testing.T) {
	// 涨停一字板: 所有买单都高价, 卖单很少.
	// 买 1000@11.00 (涨停)
	// 卖 100@10.50
	// 候选 10.49, 10.50, 11.00, 11.01
	// 10.50: cumBuy=1000, cumSell=100 → 100 ← max (anywhere from 10.50 to 11.00 same)
	// tie (4 candidates) → lower price wins → 10.50
	m := NewCallAuctionMatcher()
	buys := []CallAuctionOrder{{ID: "b1", Direction: "buy", Quantity: 1000, LimitPrice: 11.00}}
	sells := []CallAuctionOrder{{ID: "s1", Direction: "sell", Quantity: 100, LimitPrice: 10.50}}
	r := m.Match(buys, sells)
	assert.Equal(t, 100.0, r.MatchedVolume)
	assert.Equal(t, 10.50, r.ClearingPrice)
	// b1 想要 1000, 但只成交 100
	assert.Equal(t, 100.0, r.Fills["b1"])
	assert.Equal(t, 100.0, r.Fills["s1"])
}

func TestCallAuction_LimitDownScenario(t *testing.T) {
	// 跌停一字板
	buys := []CallAuctionOrder{{ID: "b1", Direction: "buy", Quantity: 100, LimitPrice: 9.50}}
	sells := []CallAuctionOrder{{ID: "s1", Direction: "sell", Quantity: 1000, LimitPrice: 9.00}}
	m := NewCallAuctionMatcher()
	r := m.Match(buys, sells)
	assert.Equal(t, 100.0, r.MatchedVolume)
	// tie (multiple price levels match 100) → lower wins
	// 候选 8.99, 9.00, 9.50, 9.51
	// 9.00: cumBuy=100, cumSell=1000 → 100
	// 9.50: cumBuy=100, cumSell=1000 → 100
	// tie, lower wins → 9.00
	assert.Equal(t, 9.00, r.ClearingPrice)
	assert.Equal(t, 100.0, r.Fills["b1"])
	assert.Equal(t, 100.0, r.Fills["s1"])
}

func TestCallAuction_FillRatio(t *testing.T) {
	// 测试 FillRatio / IsFullyFilled 工具方法.
	m := NewCallAuctionMatcher()
	buys := []CallAuctionOrder{{ID: "b1", Direction: "buy", Quantity: 200, LimitPrice: 10.0}}
	sells := []CallAuctionOrder{{ID: "s1", Direction: "sell", Quantity: 100, LimitPrice: 10.0}}
	r := m.Match(buys, sells)
	assert.Equal(t, 100.0, r.MatchedVolume)
	// 买 200, 成交 100, ratio = 0.5
	assert.InDelta(t, 0.5, r.FillRatio("b1", 200), 0.01)
	// 卖 100, 成交 100, ratio = 1.0
	assert.InDelta(t, 1.0, r.FillRatio("s1", 100), 0.01)
	assert.True(t, r.IsFullyFilled("s1", 100))
	assert.False(t, r.IsFullyFilled("b1", 200))
}

func TestCallAuction_IntegrationScenario(t *testing.T) {
	// 综合场景: 多档位, 大量 pro-rata.
	// 买: 100@10.20, 200@10.10, 300@10.00, 500@9.90
	// 卖: 200@9.95, 400@10.05, 300@10.15
	buys := []CallAuctionOrder{
		{ID: "b1", Direction: "buy", Quantity: 100, LimitPrice: 10.20},
		{ID: "b2", Direction: "buy", Quantity: 200, LimitPrice: 10.10},
		{ID: "b3", Direction: "buy", Quantity: 300, LimitPrice: 10.00},
		{ID: "b4", Direction: "buy", Quantity: 500, LimitPrice: 9.90},
	}
	sells := []CallAuctionOrder{
		{ID: "s1", Direction: "sell", Quantity: 200, LimitPrice: 9.95},
		{ID: "s2", Direction: "sell", Quantity: 400, LimitPrice: 10.05},
		{ID: "s3", Direction: "sell", Quantity: 300, LimitPrice: 10.15},
	}
	m := NewCallAuctionMatcher()
	r := m.Match(buys, sells)

	require.NotZero(t, r.ClearingPrice, "should clear")
	require.NotZero(t, r.MatchedVolume, "should have volume")
	t.Logf("clearing=%.4f volume=%.0f", r.ClearingPrice, r.MatchedVolume)
	// Sanity: 成交的买单总和应 = 成交的卖单总和
	var buyFilled, sellFilled float64
	for _, o := range buys {
		buyFilled += r.Fills[o.ID]
	}
	for _, o := range sells {
		sellFilled += r.Fills[o.ID]
	}
	assert.InDelta(t, sellFilled, buyFilled, 0.01, "buy/sell fill totals must balance")
	assert.InDelta(t, r.MatchedVolume, buyFilled, 0.01, "filled total must match matched")
}

func TestAuctionRound4(t *testing.T) {
	assert.Equal(t, 10.0, auctionRound4(10.0))
	assert.Equal(t, 10.1235, auctionRound4(10.12345)) // Go math.Round: half away from zero
	assert.Equal(t, 10.1235, auctionRound4(10.12346))
	assert.Equal(t, 10.1234, auctionRound4(10.12344))
}
