package backtest

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// P2-2 (ODR-027): Multi-strategy comparison
//
// CompareReports fetches N completed backtest reports by ID and returns a
// structured comparison payload. The lookup path mirrors
// handlers_backtest.lookupBacktestResponse: first try the in-memory
// Engine, then fall back to the persistent JobStore. Any single ID
// failing to resolve becomes an entry in `Missing` — we still return
// the successfully-loaded reports so the UI can show a "partial
// comparison" view instead of an all-or-nothing error.
//
// Output layout:
//   - Reports:  every successfully-resolved BacktestResponse, in the
//               same order as the input IDs (filtering out misses).
//   - Missing:  IDs that could not be resolved (with reason).
//   - Summary:  one row per report with the metrics the comparison
//               table actually renders (TotalReturn, Sharpe,
//               MaxDrawdown, WinRate, ...). Pre-flattened so the
//               frontend doesn't have to re-walk the object graph.
//   - Best*:    per-metric winners — the "best Sharpe", "best
//               Calmar", "lowest drawdown" rows in the comparison
//               table. Empty string when the metric is missing from
//               every report.
//
// CompareReports is intentionally read-only — it never touches
// state stores or the DB beyond JobStore.GetBacktestJob.

const (
	// MaxCompareIDs bounds a single comparison request. Higher caps
	// degrade the HTML export (P2-1) and the equity overlay chart
	// (P2-2) into an unreadable mess. Eight strategies side-by-side
	// is already the upper end of what fits on a 1920px screen.
	MaxCompareIDs = 8

	// MinCompareIDs is a UI-side convention enforced here too so the
	// error message is consistent for both surfaces.
	MinCompareIDs = 2
)

// CompareEntry is a flattened, table-friendly view of one backtest
// result. The frontend uses this directly for the comparison table.
type CompareEntry struct {
	ID            string  `json:"id"`
	Strategy      string  `json:"strategy"`
	StartDate     string  `json:"start_date,omitempty"`
	EndDate       string  `json:"end_date,omitempty"`
	TotalReturn   float64 `json:"total_return"`
	AnnualReturn  float64 `json:"annual_return"`
	SharpeRatio   float64 `json:"sharpe_ratio"`
	SortinoRatio  float64 `json:"sortino_ratio"`
	MaxDrawdown   float64 `json:"max_drawdown"`
	CalmarRatio   float64 `json:"calmar_ratio"`
	WinRate       float64 `json:"win_rate"`
	TotalTrades   int     `json:"total_trades"`
	WinTrades     int     `json:"win_trades"`
	LoseTrades    int     `json:"lose_trades"`
	AvgHolding    float64 `json:"avg_holding_days"`
	InitialCap    float64 `json:"initial_capital"`
	Universe      string  `json:"universe,omitempty"`
	HasEquityData bool    `json:"has_equity_data"`
}

// CompareReport is the full payload returned by /api/backtest/compare.
type CompareReport struct {
	GeneratedAt time.Time             `json:"generated_at"`
	Requested   int                   `json:"requested"`
	Resolved    int                   `json:"resolved"`
	Reports     []BacktestResponse    `json:"reports"`
	Entries     []CompareEntry        `json:"entries"`
	Missing     []CompareMissingEntry `json:"missing"`
	Best        CompareBest           `json:"best"`
}

// CompareMissingEntry explains why a single ID could not be resolved.
type CompareMissingEntry struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// CompareBest is the per-metric winner list. Empty ID strings mean
// "no report had a usable value for this metric".
type CompareBest struct {
	TotalReturn  string `json:"total_return_id,omitempty"`
	SharpeRatio  string `json:"sharpe_ratio_id,omitempty"`
	SortinoRatio string `json:"sortino_ratio_id,omitempty"`
	MaxDrawdown  string `json:"max_drawdown_id,omitempty"`  // lowest (closest to 0) wins
	CalmarRatio  string `json:"calmar_ratio_id,omitempty"`
	WinRate      string `json:"win_rate_id,omitempty"`
	AnnualReturn string `json:"annual_return_id,omitempty"`
}

// CompareResultResolver is the function the handler uses to fetch a
// single report by ID. It is split out so the handler can apply the
// in-memory-first / DB-fallback policy it already uses for the single
// report endpoint, and so the tests can substitute a pure in-memory
// implementation.
type CompareResultResolver func(ctx context.Context, id string) (BacktestResponse, error)

// NewCompareResolver builds a CompareResultResolver bound to the
// given engine + jobService. The closure mirrors the lookup policy of
// handlers_backtest.lookupBacktestResponse, but only returns the
// payload — the handler is still responsible for writing the HTTP
// error response when the resolver returns a non-nil error.
func NewCompareResolver(engine *Engine, jobService *JobService, logger zerolog.Logger) CompareResultResolver {
	return func(ctx context.Context, id string) (BacktestResponse, error) {
		if engine == nil && jobService == nil {
			return BacktestResponse{}, fmt.Errorf("no resolver configured")
		}
		if engine != nil {
			status, err := engine.GetBacktestStatus(id)
			if err == nil && status == "completed" {
				result, err := engine.GetBacktestResult(id)
				if err == nil && result != nil {
					params, _ := engine.GetBacktestParams(id)
					return BacktestResponse{
						ID:              id,
						Status:          "completed",
						Strategy:        params.StrategyName,
						StartDate:       result.StartDate.Format("2006-01-02"),
						EndDate:         result.EndDate.Format("2006-01-02"),
						TotalReturn:     result.TotalReturn,
						AnnualReturn:    result.AnnualReturn,
						SharpeRatio:     result.SharpeRatio,
						SortinoRatio:    result.SortinoRatio,
						MaxDrawdown:     result.MaxDrawdown,
						MaxDrawdownDate: result.MaxDrawdownDate.Format("2006-01-02"),
						WinRate:         result.WinRate,
						TotalTrades:     result.TotalTrades,
						WinTrades:       result.WinTrades,
						LoseTrades:      result.LoseTrades,
						AvgHoldingDays:  result.AvgHoldingDays,
						CalmarRatio:     result.CalmarRatio,
						StockPool:       params.StockPool,
						InitialCapital:  params.InitialCapital,
						PortfolioValues: result.PortfolioValues,
						Trades:          result.Trades,
					}, nil
				}
			}
		}
		if jobService != nil {
			job, err := jobService.GetJob(ctx, id)
			if err != nil {
				return BacktestResponse{}, err
			}
			if job == nil || job.Status != "completed" {
				return BacktestResponse{}, fmt.Errorf("backtest not found or not completed")
			}
			var stored BacktestResponse
			if err := json.Unmarshal(job.Result, &stored); err != nil {
				return BacktestResponse{}, fmt.Errorf("failed to parse stored result: %w", err)
			}
			if stored.ID == "" {
				stored.ID = id
			}
			return stored, nil
		}
		return BacktestResponse{}, fmt.Errorf("backtest not found")
	}
}

// CompareReports orchestrates the multi-report lookup and builds the
// flattened response. The function is pure: it does not call any I/O
// itself; the resolver does. That keeps it trivial to unit-test.
func CompareReports(ctx context.Context, ids []string, resolve CompareResultResolver) (CompareReport, error) {
	if len(ids) < MinCompareIDs {
		return CompareReport{}, fmt.Errorf("at least %d backtest IDs are required for comparison, got %d", MinCompareIDs, len(ids))
	}
	if len(ids) > MaxCompareIDs {
		return CompareReport{}, fmt.Errorf("at most %d backtest IDs can be compared at once, got %d", MaxCompareIDs, len(ids))
	}
	// Deduplicate while preserving order. A user double-clicking the
	// "add to compare" checkbox would otherwise produce a 2-row
	// compare with identical metrics in both rows.
	seen := make(map[string]struct{}, len(ids))
	deduped := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		deduped = append(deduped, id)
	}
	if len(deduped) < MinCompareIDs {
		return CompareReport{}, fmt.Errorf("comparison requires at least %d distinct, non-empty IDs", MinCompareIDs)
	}

	report := CompareReport{
		GeneratedAt: time.Now().UTC(),
		Requested:   len(deduped),
		Reports:     make([]BacktestResponse, 0, len(deduped)),
		Entries:     make([]CompareEntry, 0, len(deduped)),
		Missing:     []CompareMissingEntry{},
	}

	// First pass: resolve everything, in input order. We collect
	// entries in the same order so the frontend can render the
	// "compare table" rows in the user's selection order.
	for _, id := range deduped {
		resp, err := resolve(ctx, id)
		if err != nil {
			report.Missing = append(report.Missing, CompareMissingEntry{
				ID:     id,
				Reason: err.Error(),
			})
			continue
		}
		report.Reports = append(report.Reports, resp)
		report.Entries = append(report.Entries, flattenEntry(resp))
	}

	// Second pass: pick per-metric winners. We work off Entries
	// (not Reports) because CompareEntry is already the projected
	// shape — no risk of a "best ID" pointing to a non-existent
	// row.
	report.Best = pickBest(report.Entries)

	// Stable sort by TotalReturn descending so the table has a
	// useful default ordering. The UI still lets the user
	// re-sort by clicking column headers.
	sort.SliceStable(report.Entries, func(i, j int) bool {
		return report.Entries[i].TotalReturn > report.Entries[j].TotalReturn
	})
	report.Resolved = len(report.Entries)

	return report, nil
}

// flattenEntry projects a BacktestResponse down to the columns the
// comparison table actually renders. Keeping this projection in Go
// (rather than on the frontend) means the JSON contract is stable
// across UI refactors.
func flattenEntry(r BacktestResponse) CompareEntry {
	universe := strings.Join(r.StockPool, ",")
	return CompareEntry{
		ID:            r.ID,
		Strategy:      r.Strategy,
		StartDate:     r.StartDate,
		EndDate:       r.EndDate,
		TotalReturn:   r.TotalReturn,
		AnnualReturn:  r.AnnualReturn,
		SharpeRatio:   r.SharpeRatio,
		SortinoRatio:  r.SortinoRatio,
		MaxDrawdown:   r.MaxDrawdown,
		CalmarRatio:   r.CalmarRatio,
		WinRate:       r.WinRate,
		TotalTrades:   r.TotalTrades,
		WinTrades:     r.WinTrades,
		LoseTrades:    r.LoseTrades,
		AvgHolding:    r.AvgHoldingDays,
		InitialCap:    r.InitialCapital,
		Universe:      universe,
		HasEquityData: len(r.PortfolioValues) > 0,
	}
}

// pickBest computes the per-metric winners. Empty ID is returned when
// no report has a usable value for that metric (NaN, ±Inf, all zero).
//
// Note on MaxDrawdown: the "best" drawdown is the LOWEST in absolute
// terms (closest to zero). A -5% drawdown beats a -20% drawdown.
func pickBest(entries []CompareEntry) CompareBest {
	if len(entries) == 0 {
		return CompareBest{}
	}
	best := CompareBest{
		TotalReturn:  entries[0].ID,
		SharpeRatio:  entries[0].ID,
		SortinoRatio: entries[0].ID,
		MaxDrawdown:  entries[0].ID,
		CalmarRatio:  entries[0].ID,
		WinRate:      entries[0].ID,
		AnnualReturn: entries[0].ID,
	}
	for _, e := range entries[1:] {
		if e.TotalReturn > entries[findIdx(entries, best.TotalReturn)].TotalReturn {
			best.TotalReturn = e.ID
		}
		if e.AnnualReturn > entries[findIdx(entries, best.AnnualReturn)].AnnualReturn {
			best.AnnualReturn = e.ID
		}
		if e.SharpeRatio > entries[findIdx(entries, best.SharpeRatio)].SharpeRatio {
			best.SharpeRatio = e.ID
		}
		if e.SortinoRatio > entries[findIdx(entries, best.SortinoRatio)].SortinoRatio {
			best.SortinoRatio = e.ID
		}
		// Drawdown: lower (less negative) is better.
		if e.MaxDrawdown > entries[findIdx(entries, best.MaxDrawdown)].MaxDrawdown {
			best.MaxDrawdown = e.ID
		}
		if e.CalmarRatio > entries[findIdx(entries, best.CalmarRatio)].CalmarRatio {
			best.CalmarRatio = e.ID
		}
		if e.WinRate > entries[findIdx(entries, best.WinRate)].WinRate {
			best.WinRate = e.ID
		}
	}
	return best
}

// findIdx returns the position of id in entries, or 0 if missing.
// Used to look up the "current best" entry's metric value when
// comparing against a candidate. Returning 0 is safe because the
// caller has already established len(entries) > 0.
func findIdx(entries []CompareEntry, id string) int {
	for i, e := range entries {
		if e.ID == id {
			return i
		}
	}
	return 0
}
