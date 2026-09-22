package tracker

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/fees"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AUD-22 — 风险警示（ST 系）股票当日累计买入上限。
//
// 规则（2026-09-22 回原文核，见 pkg/marketdata/riskwarning.go 的引文）：
//
//	沪深主板/创业板  50 万股   沪 4.4.10 / 深 4.5.4
//	北交所           20 万股   北 4.5.4（2026-08-31 起施行）
//	科创板           不适用    沪 6.14 科创板 ST 不进风险警示板
//
// 口径：委托买入 + 当日已买入 + 已申报未成交未撤销 ≤ 上限。

var (
	rwDay1 = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	rwDay2 = time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
)

const (
	rwSymBSE   = "830799.BJ" // 北交所，上限 20 万股
	rwSymMain  = "600053.SH" // 沪主板，上限 50 万股
	rwSymSTAR  = "688981.SH" // 科创板，无此上限
	rwNameST   = "*ST测试"
	rwNameNorm = "普通股票"
)

// newRiskWarningTracker builds a tracker with plenty of cash and the
// given symbol -> name table already installed, the way the engine
// installs it each day.
func newRiskWarningTracker(names map[string]string) *Tracker {
	tr := NewTracker(
		100_000_000,
		fees.DefaultCommissionRate,
		0, // no slippage: keep the share counts in the assertions exact
		contracts.DefaultTradingConfig(),
		zerolog.Nop(),
	)
	tr.SetStockNames(names)
	return tr
}

// buy is a small helper: a market buy with no limit-order options.
func buy(t *testing.T, tr *Tracker, symbol string, qty float64, at time.Time) error {
	t.Helper()
	_, err := tr.ExecuteTrade(symbol, domain.DirectionLong, qty, 1.0, at, nil)
	return err
}

// TestTracker_RiskWarningDailyBuyCap_BSE pins the 北交所 20 万股 cap:
// the buy that would take the day's cumulative total over the cap is
// rejected, and the rejection must happen BEFORE any state changes.
func TestTracker_RiskWarningDailyBuyCap_BSE(t *testing.T) {
	tr := newRiskWarningTracker(map[string]string{rwSymBSE: rwNameST})

	require.NoError(t, buy(t, tr, rwSymBSE, marketdata.RiskWarningDailyBuyCapBSE, rwDay1),
		"a buy of exactly the cap is legal — the rule is 不得超过, so == is allowed")

	err := buy(t, tr, rwSymBSE, 100, rwDay1)
	require.Error(t, err, "one more share must breach the 北交所 cap")
	assert.Contains(t, err.Error(), "daily buy cap exceeded")

	// Rejected means rejected: no position, no cash movement.
	_, exists := tr.GetPosition(rwSymBSE)
	assert.True(t, exists, "the legal first buy must still be there")
	assert.InDelta(t, marketdata.RiskWarningDailyBuyCapBSE, tr.mustPositionQty(t, rwSymBSE), 1e-9,
		"the rejected order must not have added shares")
}

// TestTracker_RiskWarningDailyBuyCap_MainBoard is the same shape on the
// 沪深 50 万股 cap, and asserts the two caps are genuinely different —
// a single shared constant would pass the BSE test above and fail here.
func TestTracker_RiskWarningDailyBuyCap_MainBoard(t *testing.T) {
	tr := newRiskWarningTracker(map[string]string{rwSymMain: rwNameST})

	// A 北交所-sized buy (200k) is legal here; the 沪深 cap is 500k.
	require.NoError(t, buy(t, tr, rwSymMain, marketdata.RiskWarningDailyBuyCapBSE, rwDay1))

	// Top it up to just under the cap, then breach it.
	require.NoError(t, buy(t, tr, rwSymMain,
		marketdata.RiskWarningDailyBuyCapMainBoard-marketdata.RiskWarningDailyBuyCapBSE, rwDay1),
		"reaching exactly the cap is allowed")

	err := buy(t, tr, rwSymMain, 100, rwDay1)
	require.Error(t, err, "one more share must breach the 沪深 cap")
	assert.Contains(t, err.Error(), "daily buy cap exceeded")
}

// TestTracker_RiskWarningDailyBuyCap_ResetsNextDay — the cap is per
// trading day (当日累计), not a lifetime total. Without the reset, a
// long backtest would slowly stop being able to buy any risk-warning
// stock at all.
func TestTracker_RiskWarningDailyBuyCap_ResetsNextDay(t *testing.T) {
	tr := newRiskWarningTracker(map[string]string{rwSymBSE: rwNameST})

	require.NoError(t, buy(t, tr, rwSymBSE, marketdata.RiskWarningDailyBuyCapBSE, rwDay1))
	require.Error(t, buy(t, tr, rwSymBSE, 100, rwDay1), "same day: capped")

	require.NoError(t, buy(t, tr, rwSymBSE, 100, rwDay2),
		"a new trading day starts a fresh counter")
}

// TestTracker_RiskWarningDailyBuyCap_STARExempt is the case a naive
// implementation gets wrong in the harmful direction: 沪 6.14 keeps
// 科创板 risk-warning stocks out of 风险警示板, so 4.4.10's 50 万股 does
// not apply to them. Applying it there would refuse legal STAR trades.
func TestTracker_RiskWarningDailyBuyCap_STARExempt(t *testing.T) {
	tr := newRiskWarningTracker(map[string]string{rwSymSTAR: rwNameST})

	require.NoError(t, buy(t, tr, rwSymSTAR, marketdata.RiskWarningDailyBuyCapMainBoard*4, rwDay1),
		"科创板 ST is not subject to the 风险警示板 daily buy cap")
}

// TestTracker_RiskWarningDailyBuyCap_NonRiskWarningUnaffected — the cap
// is a property of risk-warning status, not of the board. An ordinary
// stock on the same board must be unconstrained.
func TestTracker_RiskWarningDailyBuyCap_NonRiskWarningUnaffected(t *testing.T) {
	tr := newRiskWarningTracker(map[string]string{rwSymBSE: rwNameNorm})

	require.NoError(t, buy(t, tr, rwSymBSE, marketdata.RiskWarningDailyBuyCapBSE*5, rwDay1),
		"a non-ST stock has no daily buy cap")
}

// TestTracker_RiskWarningDailyBuyCap_SellSideUnaffected — the rule caps
// 买入. A sell must not be refused by it (the sell is partial-filled down
// to the held quantity for other reasons, which is why this asserts
// "no cap error" rather than "fully filled").
func TestTracker_RiskWarningDailyBuyCap_SellSideUnaffected(t *testing.T) {
	tr := newRiskWarningTracker(map[string]string{rwSymMain: rwNameST})

	require.NoError(t, buy(t, tr, rwSymMain, 1000, rwDay1))

	// AdvanceDay so the shares bought on day 1 become sellable (T+1):
	// without it the sell fails on settlement, not on the cap, and the
	// test would pass for the wrong reason.
	tr.AdvanceDay(rwDay2)

	// Day 2 so T+1 permits the sale. Ask for far more than the cap.
	_, err := tr.ExecuteTrade(rwSymMain, domain.DirectionClose, 2_000_000, 1.0, rwDay2, nil)
	require.NoError(t, err, "the daily buy cap must not apply to a sell")
	assert.NotContains(t, errString(err), "daily buy cap exceeded")
	assert.NotContains(t, errString(err), "T+1",
		"the sale must fail on nothing at all — a T+1 error here would make the assertion vacuous")
}

// TestTracker_RiskWarningDailyBuyCap_UnknownNameFailsOpen pins the
// deliberate fail-open: an unknown name must not block legitimate
// trading. The trade-off is that a missing name table silently disables
// the cap, which is why the tracker logs a warning and why engine.go's
// population point has its own guard.
func TestTracker_RiskWarningDailyBuyCap_UnknownNameFailsOpen(t *testing.T) {
	tr := newRiskWarningTracker(nil) // table never populated

	require.NoError(t, buy(t, tr, rwSymBSE, marketdata.RiskWarningDailyBuyCapBSE*3, rwDay1),
		"an unpopulated name table must not block trading")

	// An entry that exists but carries an empty name (getStock failed)
	// must behave the same as a missing entry, not as "not ST".
	tr2 := newRiskWarningTracker(map[string]string{rwSymBSE: ""})
	require.NoError(t, buy(t, tr2, rwSymBSE, marketdata.RiskWarningDailyBuyCapBSE*3, rwDay1))
}

// TestTracker_RiskWarningDailyBuyCap_AccumulatesAcrossOrders is the core
// 当日累计 semantics: several individually-legal buys must still be
// stopped once their SUM crosses the cap. A per-order check would pass
// every one of them.
func TestTracker_RiskWarningDailyBuyCap_AccumulatesAcrossOrders(t *testing.T) {
	tr := newRiskWarningTracker(map[string]string{rwSymBSE: rwNameST})

	// Ten buys of 30k = 300k > 200k cap. The 7th crosses it.
	for i := 0; i < 6; i++ {
		require.NoErrorf(t, buy(t, tr, rwSymBSE, 30_000, rwDay1), "buy #%d must be legal", i+1)
	}
	err := buy(t, tr, rwSymBSE, 30_000, rwDay1)
	require.Error(t, err, "the cumulative total (210k) must breach the 200k cap")
	assert.Contains(t, err.Error(), "daily buy cap exceeded")
}

// TestTracker_RiskWarningDailyBuyCap_ApplyTradePath covers the SECOND
// entry point into the portfolio — and it is not a duplicate.
//
// Every test above drives ExecuteTrade. Production does not: NewEngine
// unconditionally installs an execution service, so a buy reaches the
// portfolio through executeViaExecutionService -> ApplyTrade. This test
// was added because deleting the cap check from ApplyTrade alone left
// every other test in this file GREEN (verified by destructive test,
// AUD-22) — the suite was blind to the path production actually uses.
func TestTracker_RiskWarningDailyBuyCap_ApplyTradePath(t *testing.T) {
	tr := newRiskWarningTracker(map[string]string{rwSymBSE: rwNameST})

	fill := func(qty float64) error {
		t.Helper()
		_, err := tr.ApplyTrade(domain.Trade{
			Symbol:    rwSymBSE,
			Direction: domain.DirectionLong,
			Quantity:  qty,
			Price:     1.0,
			Timestamp: rwDay1,
		})
		return err
	}

	require.NoError(t, fill(marketdata.RiskWarningDailyBuyCapBSE),
		"reaching exactly the cap is legal on the ApplyTrade path too")

	err := fill(100)
	require.Error(t, err, "ApplyTrade must enforce the same cap as ExecuteTrade")
	assert.Contains(t, err.Error(), "daily buy cap exceeded")
	assert.InDelta(t, marketdata.RiskWarningDailyBuyCapBSE, tr.mustPositionQty(t, rwSymBSE), 1e-9,
		"the rejected fill must not have added shares")
}

// errString is a nil-safe err.Error() for assertions that must also hold
// when err is nil (assert.NotContains on a nil error would panic).
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// mustPositionQty returns the position size, failing the test if absent.
func (t *Tracker) mustPositionQty(tb testing.TB, symbol string) float64 {
	tb.Helper()
	pos, exists := t.GetPosition(symbol)
	require.True(tb, exists, "position for %s must exist", symbol)
	return pos.Quantity
}
