// K15 dualrun 测试：wrReplay 决策语义（与 weight_replay.py 逐字对拍）+
// diff 报告的反证腿。破坏验证（改坏 → 红 → 还原）见各测试的 DESTRUCTION 注。
package main

import (
	"math"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

func testBar(sym string, y, m, d int, close, volume float64) domain.OHLCV {
	return domain.OHLCV{
		Symbol: sym,
		Date:   time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC),
		Open:   close, High: close, Low: close, Close: close,
		Volume: volume,
	}
}

func cfgFor(capital float64) wrConfig { return defaultWRConfig(capital) }

func TestWRReplay_OpenAndClose(t *testing.T) {
	// 恒定名义额口径：day2 价格 10→11 触发「隐含减仓」（目标 4500 < 5000）：
	// 3 笔成交 = day1 建 5000 / day2 减 500 / day3 清仓 4500。
	capital := 100_000.0
	bars := map[string][]domain.OHLCV{
		"000001.SZ": {
			testBar("000001.SZ", 2024, 1, 2, 10, 1_000_000),
			testBar("000001.SZ", 2024, 1, 3, 11, 1_000_000),
			testBar("000001.SZ", 2024, 1, 4, 12, 1_000_000),
		},
	}
	signals := []contracts.SignalWeight{
		{Symbol: "000001.SZ", Weight: 0.5, AsOf: "2024-01-02"},
		{Symbol: "000001.SZ", Weight: 0.0, AsOf: "2024-01-04"},
	}
	res, err := wrReplay(cfgFor(capital), signals, 0.0003, contracts.DefaultTradingConfig(), bars, zerolog.Nop())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(res.Trades) != 3 {
		t.Fatalf("expect 3 fills (open / notional rebalance / close), got %d: %+v", len(res.Trades), res.Trades)
	}
	for _, tc := range []struct {
		i   int
		dir domain.Direction
		qty float64
	}{{0, domain.DirectionLong, 5000}, {1, domain.DirectionShort, 500}, {2, domain.DirectionShort, 4500}} {
		got := res.Trades[tc.i]
		if got.Direction != tc.dir || got.Quantity != tc.qty {
			t.Fatalf("trade[%d]: %s %v, want %s %v", tc.i, got.Direction, got.Quantity, tc.dir, tc.qty)
		}
	}
	// 清仓后末日净值 = 纯现金（无持仓 mark）。
	last := res.PortfolioValues[len(res.PortfolioValues)-1]
	if last.Positions != 0 {
		t.Fatalf("final day must be flat, positions=%v", last.Positions)
	}
}

func TestWRReplay_ConstantNotionalRebalance(t *testing.T) {
	// 恒定名义额再平衡：10 → 11 → 目标 4500，delta=-500 减仓；
	// 11 → 9：目标 floor(50000/9/100)*100 = 5500，delta=1000 加仓。
	// （跌则加、涨则减 —— K14-R1 G 已显式声明，与侧车同。）
	capital := 100_000.0
	bars := map[string][]domain.OHLCV{
		"000001.SZ": {
			testBar("000001.SZ", 2024, 1, 2, 10, 1_000_000),
			testBar("000001.SZ", 2024, 1, 3, 11, 1_000_000),
			testBar("000001.SZ", 2024, 1, 4, 9, 1_000_000),
		},
	}
	signals := []contracts.SignalWeight{
		{Symbol: "000001.SZ", Weight: 0.5, AsOf: "2024-01-02"},
	}
	res, err := wrReplay(cfgFor(capital), signals, 0.0003, contracts.DefaultTradingConfig(), bars, zerolog.Nop())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	// Day1: buy 5000. Day2: target 4500, delta -500 → sell 500. Day3: target
	// 5500, delta 1000 → buy 1000.
	if len(res.Trades) != 3 {
		t.Fatalf("expect 3 fills, got %d: %+v", len(res.Trades), res.Trades)
	}
	for _, tc := range []struct {
		i   int
		dir domain.Direction
		qty float64
	}{{0, domain.DirectionLong, 5000}, {1, domain.DirectionShort, 500}, {2, domain.DirectionLong, 1000}} {
		if res.Trades[tc.i].Direction != tc.dir || res.Trades[tc.i].Quantity != tc.qty {
			t.Fatalf("trade[%d]: %s %v want %s %v", tc.i,
				res.Trades[tc.i].Direction, res.Trades[tc.i].Quantity, tc.dir, tc.qty)
		}
	}
}

func TestWRReplay_NoChaseBuyAtLimitUp(t *testing.T) {
	// Day2 涨停（prev_close 10 → upper 11.00，close=11.00 ≥ upper）：不追买。
	capital := 100_000.0
	bars := map[string][]domain.OHLCV{
		"000001.SZ": {
			testBar("000001.SZ", 2024, 1, 2, 10, 1_000_000),
			testBar("000001.SZ", 2024, 1, 3, 11.00, 1_000_000),
		},
	}
	signals := []contracts.SignalWeight{
		{Symbol: "000001.SZ", Weight: 0.5, AsOf: "2024-01-02"},
		{Symbol: "000001.SZ", Weight: 0.9, AsOf: "2024-01-03"}, // 触发加仓到 0.9
	}
	res, err := wrReplay(cfgFor(capital), signals, 0.0003, contracts.DefaultTradingConfig(), bars, zerolog.Nop())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	// 只有 day1 的建仓 500；day2 涨停加仓被跳过。
	if len(res.Trades) != 1 {
		t.Fatalf("expect only the day-1 fill (limit-up no chase), got %d: %+v", len(res.Trades), res.Trades)
	}
}

func TestWRReplay_NoChaseSellAtLimitDown(t *testing.T) {
	// Day2 跌停（prev_close 10 → lower 9.00，close=9.00 ≤ lower）：不追卖。
	capital := 100_000.0
	bars := map[string][]domain.OHLCV{
		"000001.SZ": {
			testBar("000001.SZ", 2024, 1, 2, 10, 1_000_000),
			testBar("000001.SZ", 2024, 1, 3, 9.00, 1_000_000),
		},
	}
	signals := []contracts.SignalWeight{
		{Symbol: "000001.SZ", Weight: 0.5, AsOf: "2024-01-02"},
		{Symbol: "000001.SZ", Weight: 0.0, AsOf: "2024-01-03"}, // 清仓撞跌停
	}
	res, err := wrReplay(cfgFor(capital), signals, 0.0003, contracts.DefaultTradingConfig(), bars, zerolog.Nop())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(res.Trades) != 1 {
		t.Fatalf("expect only the day-1 fill (limit-down no chase), got %d: %+v", len(res.Trades), res.Trades)
	}
}

func TestWRReplay_ParticipationCapTruncatesBuy(t *testing.T) {
	// volume=1000、D_max=0.10 → cap=100 股/日：建仓请求 500 股被截到 100。
	capital := 100_000.0
	bars := map[string][]domain.OHLCV{
		"000001.SZ": {
			testBar("000001.SZ", 2024, 1, 2, 10, 1000),
			testBar("000001.SZ", 2024, 1, 3, 10, 1000),
			testBar("000001.SZ", 2024, 1, 4, 10, 1000),
			testBar("000001.SZ", 2024, 1, 5, 10, 1000),
			testBar("000001.SZ", 2024, 1, 8, 10, 1000),
		},
	}
	signals := []contracts.SignalWeight{
		{Symbol: "000001.SZ", Weight: 0.5, AsOf: "2024-01-02"},
	}
	res, err := wrReplay(cfgFor(capital), signals, 0.0003, contracts.DefaultTradingConfig(), bars, zerolog.Nop())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	// 每日 100 股 × 5 个交易日 = 500 = 目标。
	total := 0.0
	for _, tr := range res.Trades {
		total += tr.Quantity
	}
	if total != 500 {
		t.Fatalf("expect 500 shares accumulated over 5 capped days, got %v (%d fills)", total, len(res.Trades))
	}
}

func TestWRReplay_Deterministic(t *testing.T) {
	capital := 100_000.0
	bars := map[string][]domain.OHLCV{
		"000001.SZ": {testBar("000001.SZ", 2024, 1, 2, 10, 1_000_000)},
	}
	signals := []contracts.SignalWeight{{Symbol: "000001.SZ", Weight: 0.5, AsOf: "2024-01-02"}}
	r1, err := wrReplay(cfgFor(capital), signals, 0.0003, contracts.DefaultTradingConfig(), bars, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	r2, err := wrReplay(cfgFor(capital), signals, 0.0003, contracts.DefaultTradingConfig(), bars, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	if len(r1.Trades) != len(r2.Trades) || len(r1.PortfolioValues) != len(r2.PortfolioValues) {
		t.Fatal("two identical replays diverged")
	}
	for i := range r1.Trades {
		if r1.Trades[i].Quantity != r2.Trades[i].Quantity || r1.Trades[i].Price != r2.Trades[i].Price {
			t.Fatalf("trade[%d] diverged: %+v vs %+v", i, r1.Trades[i], r2.Trades[i])
		}
	}
}

func TestTargetWeight_LatestAsOfWinsAndFutureInvisible(t *testing.T) {
	signals := []contracts.SignalWeight{
		{Symbol: "S", Weight: 0.3, AsOf: "2024-01-02"},
		{Symbol: "S", Weight: 0.7, AsOf: "2024-01-04"},
		{Symbol: "OTHER", Weight: 0.9, AsOf: "2024-01-02"},
	}
	d2 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	d3 := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)
	d4 := time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC)
	if w, ok := targetWeight(signals, "S", d2); !ok || w != 0.3 {
		t.Fatalf("d2: w=%v ok=%v, want 0.3", w, ok)
	}
	if w, ok := targetWeight(signals, "S", d3); !ok || w != 0.3 {
		t.Fatalf("d3 (future signal must be invisible): w=%v ok=%v, want 0.3", w, ok)
	}
	if w, ok := targetWeight(signals, "S", d4); !ok || w != 0.7 {
		t.Fatalf("d4: w=%v ok=%v, want 0.7", w, ok)
	}
	if _, ok := targetWeight(signals, "OTHER", d2); !ok {
		t.Fatal("OTHER signal should be visible")
	}
	if _, ok := targetWeight(signals, "MISSING", d2); ok {
		t.Fatal("missing symbol should report none")
	}
}

func TestBuildReport_TradeReconciliationNamesOrphans(t *testing.T) {
	// DESTRUCTION 反证腿：NT 侧少一条成交 → 报告必须点名 go_only，而不是
	// 静默 matched。把 ntTrades 的第二条删掉 → 本测试红（护栏反向自检见
	// 台账收口记录）。
	day := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	goTrades := []domain.Trade{
		{Symbol: "A", Direction: domain.DirectionLong, Quantity: 100, Price: 10, Timestamp: day, Commission: 5},
		{Symbol: "B", Direction: domain.DirectionLong, Quantity: 200, Price: 20, Timestamp: day, Commission: 5},
	}
	ntTrades := []domain.Trade{
		{Symbol: "A", Direction: domain.DirectionLong, Quantity: 100, Price: 10, Timestamp: day, Commission: 5},
	}
	report := buildReport("w", "s", "k", domain.BacktestResult{Trades: goTrades}, domain.BacktestResult{Trades: ntTrades})
	if report.MatchedTrades != 1 || report.GoOnlyTrades != 1 {
		t.Fatalf("expect 1 matched + 1 go_only, got matched=%d go_only=%d",
			report.MatchedTrades, report.GoOnlyTrades)
	}
	if !containsKind(report, "go_only") {
		t.Fatal("report must name the go-only trade")
	}
}

func containsKind(r *Report, kind string) bool {
	for _, row := range r.TradeRows {
		if row.Kind == kind {
			return true
		}
	}
	return false
}

func TestBuildReport_MetricDiffComputed(t *testing.T) {
	goRes := domain.BacktestResult{TotalReturn: 0.10, SharpeRatio: 1.5}
	ntRes := domain.BacktestResult{TotalReturn: 0.12, SharpeRatio: 1.4}
	report := buildReport("w", "s", "k", goRes, ntRes)
	var found bool
	for _, m := range report.MetricRows {
		if m.Name == "total_return" {
			found = true
			if math.Abs(m.Diff-0.02) > 1e-12 {
				t.Fatalf("total_return diff = %v, want 0.02", m.Diff)
			}
		}
	}
	if !found {
		t.Fatal("total_return row missing")
	}
}
