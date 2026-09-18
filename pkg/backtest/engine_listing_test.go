package backtest

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// P2-4：回测的池子必须按「当天在市」逐日过滤。
//
// 不做这件事，回测 2020 年时看到的是 2026 年的上市名单 —— 期间退市的票
// 连同它们最惨的那段行情一起消失，收益被系统性高估，而且**高估多少从
// 结果里看不出来**。验证器能诊断出这个偏差（P2-9d），但此前引擎治不了。

func date(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func testWindows() map[string]storage.ListingWindow {
	delist := date("2021-06-01")
	return map[string]storage.ListingWindow{
		"ALIVE":  {List: date("2015-01-01")},
		"GONE":   {List: date("2010-01-01"), Delist: &delist},
		"FUTURE": {List: date("2022-01-01")},
	}
}

func TestEligibleUniverse_FiltersByListingDate(t *testing.T) {
	eng := newTestEngine(t)
	eng.SetListingWindows(testWindows())
	pool := []string{"ALIVE", "GONE", "FUTURE"}

	// 2020 年：FUTURE 还没上市（未来股，另一种前视偏差）
	assert.Equal(t, []string{"ALIVE", "GONE"},
		eng.eligibleUniverse(pool, date("2020-06-01"), nil),
		"未上市的票不能出现在历史池子里")

	// 2023 年：GONE 已摘牌
	assert.Equal(t, []string{"ALIVE", "FUTURE"},
		eng.eligibleUniverse(pool, date("2023-01-01"), nil),
		"已摘牌的票不能在摘牌后继续交易")

	// 摘牌当天仍算在市（退市整理期通常还有行情，也该能卖出）
	assert.Equal(t, []string{"ALIVE", "GONE"},
		eng.eligibleUniverse(pool, date("2021-06-01"), nil))
}

// 修复幸存者偏差的关键不在「剔除已退市」，而在「退市前让它一直在池子里」。
func TestEligibleUniverse_DelistedStockStaysInPoolBeforeDelisting(t *testing.T) {
	eng := newTestEngine(t)
	eng.SetListingWindows(testWindows())

	got := eng.eligibleUniverse([]string{"GONE"}, date("2019-03-01"), nil)
	assert.Equal(t, []string{"GONE"}, got,
		"退市票在退市**之前**必须在池子里 —— 否则修的就不是幸存者偏差，是把它坐实了")
}

// 已退市但还持仓：必须留在 universe 里，否则永远平不掉仓。
func TestEligibleUniverse_DelistedButHeldIsKeptForLiquidation(t *testing.T) {
	eng := newTestEngine(t)
	eng.SetListingWindows(testWindows())

	got := eng.eligibleUniverse([]string{"ALIVE", "GONE"}, date("2023-01-01"),
		map[string]bool{"GONE": true})
	assert.Equal(t, []string{"ALIVE", "GONE"}, got,
		"还持仓的退市票要留着，好把它平掉")
}

// 日历里查不到 = 不知道，不能假设"一直在市"（那正是要修的偏差本身）。
func TestEligibleUniverse_UnknownSymbolIsExcluded(t *testing.T) {
	eng := newTestEngine(t)
	eng.SetListingWindows(testWindows())

	got := eng.eligibleUniverse([]string{"ALIVE", "WHO"}, date("2020-06-01"), nil)
	assert.Equal(t, []string{"ALIVE"}, got,
		"查不到上市日期的票不能默认它在市")
}

// 没有日历时不过滤 —— 但调用方必须照实上报这条偏差，不能当成"已修好"。
func TestEligibleUniverse_NoCalendarPassesThrough(t *testing.T) {
	eng := newTestEngine(t)
	pool := []string{"ALIVE", "GONE", "FUTURE"}

	assert.Nil(t, eng.ListingWindows(), "没注入日历就该是空的")
	assert.Equal(t, pool, eng.eligibleUniverse(pool, date("2020-06-01"), nil),
		"没有日历就不能乱剔除，但偏差维要报 PoolSourceCurrent")
}

// 顺序必须稳定：谁先谁后决定成交序列，随机化会让回测不可复现（P1-14）。
func TestEligibleUniverse_PreservesOrder(t *testing.T) {
	eng := newTestEngine(t)
	eng.SetListingWindows(testWindows())
	pool := []string{"ALIVE", "GONE", "FUTURE"}

	first := eng.eligibleUniverse(pool, date("2020-06-01"), nil)
	for i := 0; i < 20; i++ {
		assert.Equal(t, first, eng.eligibleUniverse(pool, date("2020-06-01"), nil))
	}
}

// ─── 退市持仓的强平 ────────────────────────────────────────────────────

func newTestState(t *testing.T) *BacktestState {
	t.Helper()
	tracker := NewTracker(1_000_000, 0.0003, 0.0001, defaultTradingConfig(), zerolog.Nop())
	return &BacktestState{ID: "t", Status: "running", Tracker: tracker}
}

// buy 建一笔多头持仓。
//
// 必须紧跟一次 AdvanceDay：A 股 T+1，不推进交易日的话这笔持仓永远算
// "今天买入"，任何卖出都会被 T+1 检查拒掉（生产循环每天都会调 AdvanceDay，
// 所以这是测试脚手架要补的，不是引擎的问题）。
func buy(t *testing.T, state *BacktestState, symbol string, qty, price float64, at time.Time) {
	t.Helper()
	_, err := state.Tracker.ExecuteTrade(symbol, domain.DirectionLong, qty, price, at, nil)
	require.NoError(t, err)
	state.Tracker.AdvanceDay(at)
}

func TestForceCloseDelisted_ClosesPositionAfterDelisting(t *testing.T) {
	eng := newTestEngine(t)
	eng.SetListingWindows(testWindows())

	state := newTestState(t)
	buy(t, state, "GONE", 100, 10, date("2021-05-20"))
	require.True(t, state.Tracker.HasPosition("GONE"))

	// 摘牌后的第一个交易日：当天已无行情，走持仓里的最后已知价。
	eng.forceCloseDelisted(state, map[string]float64{}, date("2021-06-02"), zerolog.Nop())

	pos, ok := state.Tracker.GetPosition("GONE")
	if ok && pos.Quantity != 0 {
		t.Fatalf("GONE 摘牌后仍持仓 %.0f 股 —— 这笔钱会一路挂到回测结束，中间所有损益被抹平", pos.Quantity)
	}
	require.Len(t, state.Tracker.GetTrades(), 2, "买入 + 摘牌强平，各一笔")
}

// 摘牌当天不算退市，不该被平（当天通常还有价格，按强平反而失真）。
func TestForceCloseDelisted_NotTriggeredOnDelistDay(t *testing.T) {
	eng := newTestEngine(t)
	eng.SetListingWindows(testWindows())

	state := newTestState(t)
	buy(t, state, "GONE", 100, 10, date("2021-05-20"))

	eng.forceCloseDelisted(state, map[string]float64{"GONE": 9}, date("2021-06-01"), zerolog.Nop())

	pos, ok := state.Tracker.GetPosition("GONE")
	require.True(t, ok)
	assert.NotZero(t, pos.Quantity, "摘牌当天仍算在市，不该被强平")
}

// 还在市的票不能被动。这是最容易写错的地方：把过滤条件写反，
// 每只持仓票每天都被平一次，回测会变成一串假成交。
func TestForceCloseDelisted_LeavesListedPositionsAlone(t *testing.T) {
	eng := newTestEngine(t)
	eng.SetListingWindows(testWindows())

	state := newTestState(t)
	buy(t, state, "ALIVE", 100, 10, date("2020-05-20"))

	eng.forceCloseDelisted(state, map[string]float64{"ALIVE": 11}, date("2020-06-01"), zerolog.Nop())

	pos, ok := state.Tracker.GetPosition("ALIVE")
	require.True(t, ok)
	assert.Equal(t, float64(100), pos.Quantity, "在市的持仓不能被摘牌强平碰掉")
}

func TestForceCloseDelisted_NoCalendarIsNoop(t *testing.T) {
	eng := newTestEngine(t)

	state := newTestState(t)
	buy(t, state, "GONE", 100, 10, date("2021-05-20"))

	eng.forceCloseDelisted(state, map[string]float64{}, date("2021-06-02"), zerolog.Nop())

	pos, ok := state.Tracker.GetPosition("GONE")
	require.True(t, ok)
	assert.Equal(t, float64(100), pos.Quantity, "没有上市日历就无从判断谁退市了")
}
