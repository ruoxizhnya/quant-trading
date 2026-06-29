package backtest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

func sampleResponse(id string, strategy string, totalReturn float64) BacktestResponse {
	return BacktestResponse{
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
	resolver := func(_ context.Context, id string) (BacktestResponse, error) {
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
	_, err := CompareReports(context.Background(), []string{"only-one"}, func(_ context.Context, id string) (BacktestResponse, error) {
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
	_, err := CompareReports(context.Background(), ids, func(_ context.Context, id string) (BacktestResponse, error) {
		return sampleResponse(id, "x", 0.1), nil
	})
	if err == nil || !strings.Contains(err.Error(), "at most 8") {
		t.Fatalf("expected max-count error, got %v", err)
	}
}

func TestCompareReports_RejectsAfterDedup(t *testing.T) {
	// 3 input ids, but all blank → dedupe drops them all → < 2 distinct
	_, err := CompareReports(context.Background(), []string{"", " ", "\t"}, func(_ context.Context, id string) (BacktestResponse, error) {
		return sampleResponse(id, "x", 0.1), nil
	})
	if err == nil || !strings.Contains(err.Error(), "distinct") {
		t.Fatalf("expected distinct-id error, got %v", err)
	}
}

func TestCompareReports_PartialResolution(t *testing.T) {
	// One ID succeeds, one fails. We expect the success to land in
	// Reports/Entries and the failure to be captured in Missing.
	resolver := func(_ context.Context, id string) (BacktestResponse, error) {
		if id == "bt-missing" {
			return BacktestResponse{}, errors.New("not found in store")
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
	resolver := func(_ context.Context, id string) (BacktestResponse, error) {
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
	resolver := func(_ context.Context, id string) (BacktestResponse, error) {
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
	resolver := func(_ context.Context, id string) (BacktestResponse, error) {
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
	resolver := func(_ context.Context, id string) (BacktestResponse, error) {
		return BacktestResponse{}, errors.New("nope")
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
