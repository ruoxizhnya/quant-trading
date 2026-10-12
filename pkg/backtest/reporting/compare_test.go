package reporting

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
)

func sampleResponse(id string, strategy string, totalReturn float64) backtest.BacktestResponse {
	return backtest.BacktestResponse{
		ID:              id,
		Status:          "completed",
		Strategy:        strategy,
		StartDate:       "2024-01-01",
		EndDate:         "2024-06-30",
		TotalReturn:     totalReturn,
		AnnualReturn:    totalReturn * 2,
		SharpeRatio:     1.2,
		SortinoRatio:    1.4,
		MaxDrawdown:     -0.10,
		MaxDrawdownDate: "2024-04-15",
		WinRate:         0.55,
		TotalTrades:     20,
		WinTrades:       11,
		LoseTrades:      9,
		AvgHoldingDays:  5.0,
		CalmarRatio:     1.5,
		StockPool:       []string{"600000.SH"},
		InitialCapital:  1_000_000,
		PortfolioValues: []domain.PortfolioValue{
			{Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), TotalValue: 1_000_000, Cash: 1_000_000, Positions: 0},
			{Date: time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC), TotalValue: 1_100_000, Cash: 500_000, Positions: 600_000},
		},
		Trades: []domain.Trade{
			{Symbol: "600000.SH", Direction: domain.DirectionLong, Price: 10.0, Quantity: 1000, Timestamp: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)},
		},
	}
}

func TestCompareReports_HappyPath(t *testing.T) {
	resolver := func(_ context.Context, id string) (backtest.BacktestResponse, error) {
		return sampleResponse(id, "momentum-"+id, 0.10+float64(len(id))*0.01), nil
	}
	ids := []string{"bt-1", "bt-2", "bt-3"}
	report, err := CompareReports(context.Background(), ids, resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Requested != 3 {
		t.Errorf("Requested = %d, want 3", report.Requested)
	}
	if report.Resolved != 3 {
		t.Errorf("Resolved = %d, want 3", report.Resolved)
	}
	if len(report.Reports) != 3 || len(report.Entries) != 3 {
		t.Errorf("Reports/Entries length mismatch: %d/%d", len(report.Reports), len(report.Entries))
	}
	if len(report.Missing) != 0 {
		t.Errorf("Missing should be empty on happy path, got %d entries", len(report.Missing))
	}
	// Default sort: TotalReturn desc. First ID "bt-1" is shortest
	// (len=4) so its sample response has 0.10 + 4*0.01 = 0.14, which
	// is the largest of the three — and should land at index 0.
	if report.Entries[0].ID != "bt-1" {
		t.Errorf("Entries[0] = %q, want %q (highest TotalReturn)", report.Entries[0].ID, "bt-1")
	}
	// Best.TotalReturn should match the highest-return entry.
	if report.Best.TotalReturn != "bt-1" {
		t.Errorf("Best.TotalReturn = %q, want %q", report.Best.TotalReturn, "bt-1")
	}
	// All sample responses have MaxDrawdown = -0.10 → tied → first
	// wins (bt-1 by input order). Test that the field is non-empty.
	if report.Best.MaxDrawdown == "" {
		t.Errorf("Best.MaxDrawdown should be set when all entries have a drawdown value")
	}
}

func TestCompareReports_RejectsBelowMin(t *testing.T) {
	_, err := CompareReports(context.Background(), []string{"only-one"}, func(_ context.Context, id string) (backtest.BacktestResponse, error) {
		return sampleResponse(id, "x", 0.1), nil
	})
	if err == nil || !strings.Contains(err.Error(), "at least 2") {
		t.Fatalf("expected min-count error, got %v", err)
	}
}

func TestCompareReports_RejectsAboveMax(t *testing.T) {
	ids := make([]string, MaxCompareIDs+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("bt-%d", i)
	}
	_, err := CompareReports(context.Background(), ids, func(_ context.Context, id string) (backtest.BacktestResponse, error) {
		return sampleResponse(id, "x", 0.1), nil
	})
	if err == nil || !strings.Contains(err.Error(), "at most 8") {
		t.Fatalf("expected max-count error, got %v", err)
	}
}

func TestCompareReports_RejectsAfterDedup(t *testing.T) {
	// 3 input ids, but all blank → dedupe drops them all → < 2 distinct
	_, err := CompareReports(context.Background(), []string{"", " ", "\t"}, func(_ context.Context, id string) (backtest.BacktestResponse, error) {
		return sampleResponse(id, "x", 0.1), nil
	})
	if err == nil || !strings.Contains(err.Error(), "distinct") {
		t.Fatalf("expected distinct-id error, got %v", err)
	}
}

func TestCompareReports_PartialResolution(t *testing.T) {
	// One ID succeeds, one fails. We expect the success to land in
	// Reports/Entries and the failure to be captured in Missing.
	resolver := func(_ context.Context, id string) (backtest.BacktestResponse, error) {
		if id == "bt-missing" {
			return backtest.BacktestResponse{}, errors.New("not found in store")
		}
		return sampleResponse(id, "momentum", 0.10), nil
	}
	report, err := CompareReports(context.Background(), []string{"bt-ok", "bt-missing"}, resolver)
	if err != nil {
		t.Fatalf("partial resolution should not error, got %v", err)
	}
	if report.Resolved != 1 {
		t.Errorf("Resolved = %d, want 1", report.Resolved)
	}
	if len(report.Missing) != 1 || report.Missing[0].ID != "bt-missing" {
		t.Errorf("Missing entry wrong: %+v", report.Missing)
	}
	if !strings.Contains(report.Missing[0].Reason, "not found") {
		t.Errorf("Missing reason should propagate the resolver error, got %q", report.Missing[0].Reason)
	}
}

func TestCompareReports_DedupesIDs(t *testing.T) {
	calls := map[string]int{}
	resolver := func(_ context.Context, id string) (backtest.BacktestResponse, error) {
		calls[id]++
		return sampleResponse(id, "x", 0.1), nil
	}
	// 3 input ids with "bt-1" duplicated — should resolve to 2
	// distinct entries and call the resolver only once for "bt-1".
	report, err := CompareReports(context.Background(), []string{"bt-1", "bt-2", "bt-1"}, resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Resolved != 2 {
		t.Errorf("Resolved = %d, want 2 (duplicates should drop)", report.Resolved)
	}
	if calls["bt-1"] != 1 {
		t.Errorf("resolver called %d times for bt-1, want 1 (dedup)", calls["bt-1"])
	}
}

func TestCompareReports_BestPicksHighestMetric(t *testing.T) {
	// Build three reports with strictly increasing TotalReturn
	// but strictly decreasing Sharpe (so the two "bests" differ).
	resolver := func(_ context.Context, id string) (backtest.BacktestResponse, error) {
		r := sampleResponse(id, "x", 0)
		switch id {
		case "low":
			r.TotalReturn = 0.05
			r.SharpeRatio = 2.0
		case "mid":
			r.TotalReturn = 0.10
			r.SharpeRatio = 1.5
		case "high":
			r.TotalReturn = 0.20
			r.SharpeRatio = 1.0
		}
		return r, nil
	}
	report, err := CompareReports(context.Background(), []string{"low", "mid", "high"}, resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Best.TotalReturn != "high" {
		t.Errorf("Best.TotalReturn = %q, want %q", report.Best.TotalReturn, "high")
	}
	if report.Best.SharpeRatio != "low" {
		t.Errorf("Best.SharpeRatio = %q, want %q (highest Sharpe)", report.Best.SharpeRatio, "low")
	}
}

func TestCompareReports_BestDrawdownPicksLeastNegative(t *testing.T) {
	// MaxDrawdown is special: the "best" is the least negative.
	resolver := func(_ context.Context, id string) (backtest.BacktestResponse, error) {
		r := sampleResponse(id, "x", 0.1)
		switch id {
		case "deep":
			r.MaxDrawdown = -0.30
		case "shallow":
			r.MaxDrawdown = -0.05
		case "medium":
			r.MaxDrawdown = -0.15
		}
		return r, nil
	}
	report, err := CompareReports(context.Background(), []string{"deep", "shallow", "medium"}, resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Best.MaxDrawdown != "shallow" {
		t.Errorf("Best.MaxDrawdown = %q, want %q (least negative)", report.Best.MaxDrawdown, "shallow")
	}
}

func TestFlattenEntry_ProjectsExpectedFields(t *testing.T) {
	resp := sampleResponse("bt-x", "momentum", 0.42)
	entry := flattenEntry(resp)
	if entry.ID != "bt-x" || entry.Strategy != "momentum" {
		t.Errorf("ID/Strategy not projected: %+v", entry)
	}
	if entry.TotalReturn != 0.42 {
		t.Errorf("TotalReturn not projected: got %f", entry.TotalReturn)
	}
	if entry.Universe != "600000.SH" {
		t.Errorf("Universe (joined StockPool) = %q, want %q", entry.Universe, "600000.SH")
	}
	if !entry.HasEquityData {
		t.Errorf("HasEquityData should be true when PortfolioValues is non-empty")
	}
}

func TestFlattenEntry_NoEquityDataFlag(t *testing.T) {
	resp := sampleResponse("bt-x", "momentum", 0.1)
	resp.PortfolioValues = nil
	entry := flattenEntry(resp)
	if entry.HasEquityData {
		t.Errorf("HasEquityData should be false when PortfolioValues is empty")
	}
}

func TestCompareReports_EmptyEntriesYieldsEmptyBest(t *testing.T) {
	// Two IDs, both fail to resolve — we still return a valid
	// (but empty) report.
	resolver := func(_ context.Context, id string) (backtest.BacktestResponse, error) {
		return backtest.BacktestResponse{}, errors.New("nope")
	}
	report, err := CompareReports(context.Background(), []string{"a", "b"}, resolver)
	if err != nil {
		t.Fatalf("partial failure should not error, got %v", err)
	}
	if report.Resolved != 0 {
		t.Errorf("Resolved = %d, want 0", report.Resolved)
	}
	if len(report.Missing) != 2 {
		t.Errorf("Missing length = %d, want 2", len(report.Missing))
	}
	if report.Best.TotalReturn != "" {
		t.Errorf("Best.TotalReturn should be empty when no entries resolved, got %q", report.Best.TotalReturn)
	}
}

// --- K9：compare 含 invalid 的混合集（OBS-01 / ResultSpec status 三值） ---

// newMemoryCompareEngine 构造带 NoopStateStore 的内存引擎（不触库、不跑回测），
// 走 NewCompareResolver 的真实 engine 路径 —— 破坏腿必须打在闸门上：
// 把 Engine.ResponseFromState 的闸门改回只认 "completed"，下面的 reasons
// 断言立刻红（invalid 会被当「解析不到」丢进 Missing，Invalid 列表变空）。
func newMemoryCompareEngine(t *testing.T) *backtest.Engine {
	t.Helper()
	eng, err := backtest.NewEngineWithOptions(
		backtest.Config{},
		marketdata.NewInMemoryProvider(),
		backtest.WithStateStore(backtest.NewNoopStateStore()),
		backtest.WithLogger(zerolog.Nop()),
	)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	return eng
}

// putCompareState 把一个终态直接塞进引擎的 state store。
func putCompareState(eng *backtest.Engine, id, status string, totalReturn float64, reasons []string) {
	day := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	eng.StateStore().Put(id, &backtest.BacktestState{
		ID:     id,
		Status: status,
		Params: domain.BacktestParams{StrategyName: "momentum-" + id, StartDate: day, EndDate: day},
		Result: &domain.BacktestResult{
			StartDate:      day,
			EndDate:        day,
			TotalReturn:    totalReturn,
			InvalidReasons: reasons,
		},
	})
}

// 核心腿：completed ×2 + invalid ×1 的混合集 ——
//   1. invalid 不混入成功集（Entries）；
//   2. compare 能呈现 reasons（Invalid 列表带上 InvalidReasons）；
//   3. Best* 只在成功集里选（invalid 的指标再高也不当冠军）；
//   4. Resolved 计数不把 invalid 蒸发掉。
func TestCompareReports_MixedInvalidSet(t *testing.T) {
	eng := newMemoryCompareEngine(t)
	reasons := []string{"empty_universe", "zero_trades"}
	putCompareState(eng, "bt-ok-1", "completed", 0.10, nil)
	putCompareState(eng, "bt-ok-2", "completed", 0.20, nil)
	// invalid 的 TotalReturn 故意全场最大 —— 只要它混进成功集，Best 断言就红。
	putCompareState(eng, "bt-bad", "invalid", 9.99, reasons)

	resolver := NewCompareResolver(eng, nil, zerolog.Nop())
	report, err := CompareReports(context.Background(), []string{"bt-ok-1", "bt-bad", "bt-ok-2"}, resolver)
	if err != nil {
		t.Fatalf("mixed set should not error: %v", err)
	}

	if report.Resolved != 3 || len(report.Reports) != 3 {
		t.Errorf("invalid 不该在解析计数里蒸发: Resolved=%d Reports=%d, want 3/3", report.Resolved, len(report.Reports))
	}
	if len(report.Missing) != 0 {
		t.Errorf("invalid 不是 missing: %+v", report.Missing)
	}

	// 1. 不混入成功集。
	if len(report.Entries) != 2 {
		t.Fatalf("Entries = %d, want 2（invalid 不混入成功集）", len(report.Entries))
	}
	for _, e := range report.Entries {
		if e.ID == "bt-bad" {
			t.Errorf("invalid 运行混入了成功集: %+v", e)
		}
	}

	// 2. compare 能呈现 reasons —— 破坏腿判据：闸门改回只认 completed ⇒ 这里红。
	if len(report.Invalid) != 1 {
		t.Fatalf("Invalid = %+v, want 1 条（compare 必须呈现 invalid 运行）", report.Invalid)
	}
	inv := report.Invalid[0]
	if inv.ID != "bt-bad" || inv.Strategy != "momentum-bt-bad" {
		t.Errorf("Invalid 条目身份错: %+v", inv)
	}
	if len(inv.InvalidReasons) != 2 || inv.InvalidReasons[0] != "empty_universe" || inv.InvalidReasons[1] != "zero_trades" {
		t.Errorf("InvalidReasons = %v, want %v（reasons 必须透传到 compare）", inv.InvalidReasons, reasons)
	}

	// 3. Best 只在成功集里选。
	if report.Best.TotalReturn != "bt-ok-2" {
		t.Errorf("Best.TotalReturn = %q, want %q（invalid 的 9.99 不得夺冠）", report.Best.TotalReturn, "bt-ok-2")
	}
}

// failed（没跑完）fail-closed 归 Missing 并带上真实错误 —— 不进成功集也不装 invalid。
func TestCompareReports_FailedRunGoesToMissing(t *testing.T) {
	resolver := func(_ context.Context, id string) (backtest.BacktestResponse, error) {
		if id == "bt-fail" {
			return backtest.BacktestResponse{ID: id, Status: backtest.StatusFailed, Error: "provider down"}, nil
		}
		return sampleResponse(id, "x", 0.1), nil
	}
	report, err := CompareReports(context.Background(), []string{"bt-ok", "bt-fail"}, resolver)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Entries) != 1 || report.Entries[0].ID != "bt-ok" {
		t.Errorf("Entries = %+v, want 只含 bt-ok", report.Entries)
	}
	if len(report.Invalid) != 0 {
		t.Errorf("failed 不是 invalid: %+v", report.Invalid)
	}
	if len(report.Missing) != 1 || report.Missing[0].ID != "bt-fail" {
		t.Fatalf("Missing = %+v, want bt-fail", report.Missing)
	}
	if !strings.Contains(report.Missing[0].Reason, "provider down") {
		t.Errorf("Missing reason 应带上真实错误, got %q", report.Missing[0].Reason)
	}
}
