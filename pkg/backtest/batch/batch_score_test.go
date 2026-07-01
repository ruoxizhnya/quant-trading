package batch

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScoreResult(t *testing.T) {
	be := &BatchEngine{}
	br := &domain.BacktestResult{
		SharpeRatio:  1.5,
		AnnualReturn: 0.2,
		MaxDrawdown:  -0.15,
		WinRate:      0.6,
		TotalTrades:  50,
		CalmarRatio:  1.3,
	}

	r := &BatchResult{
		TaskID: "task-1",
		Result: br,
	}

	score := be.scoreResult(r)
	assert.Equal(t, 0.5, score.OverfitScore)
	assert.Equal(t, 0.5, score.StabilityScore)
	assert.Greater(t, score.CompositeScore, 0.0)
	assert.LessOrEqual(t, score.CompositeScore, 1.0)
	assert.NotEmpty(t, score.Grade)
}

func TestScoreResult_WithWalkForward(t *testing.T) {
	be := &BatchEngine{}
	br := &domain.BacktestResult{
		SharpeRatio:  2.0,
		AnnualReturn: 0.3,
		MaxDrawdown:  -0.1,
		WinRate:      0.65,
		TotalTrades:  100,
		CalmarRatio:  3.0,
	}

	r := &BatchResult{
		TaskID: "task-2",
		Result: br,
		WalkForward: &domain.WalkForwardReport{
			OverfitScore:   0.2,
			StabilityScore: 0.8,
		},
	}

	score := be.scoreResult(r)
	assert.InDelta(t, 0.2, score.OverfitScore, 0.001)
	assert.InDelta(t, 0.8, score.StabilityScore, 0.001)
	assert.Greater(t, score.CompositeScore, 0.3)
}

func TestScoreResult_ExtremeValues(t *testing.T) {
	be := &BatchEngine{}
	br := &domain.BacktestResult{
		SharpeRatio:  -2.0,
		AnnualReturn: -0.5,
		MaxDrawdown:  -0.8,
		WinRate:      0.1,
		TotalTrades:  0,
		CalmarRatio:  -1.0,
	}

	r := &BatchResult{
		TaskID: "task-3",
		Result: br,
	}

	score := be.scoreResult(r)
	assert.GreaterOrEqual(t, score.CompositeScore, 0.0)
	assert.LessOrEqual(t, score.CompositeScore, 1.0)
}

func TestBuildSummary(t *testing.T) {
	report := &BatchReport{
		TotalTasks: 3,
		Completed:  2,
		Failed:     1,
		Results: []*BatchResult{
			{
				TaskID: "task-1",
				Status: "completed",
				Result: &domain.BacktestResult{SharpeRatio: 1.5, AnnualReturn: 0.2, MaxDrawdown: -0.1, WinRate: 0.6},
				Score:  &BatchScore{Grade: "A", CompositeScore: 0.8, OverfitScore: 0.2, StabilityScore: 0.8},
			},
			{
				TaskID: "task-2",
				Status: "completed",
				Result: &domain.BacktestResult{SharpeRatio: 0.5, AnnualReturn: 0.1, MaxDrawdown: -0.2, WinRate: 0.5},
				Score:  &BatchScore{Grade: "C", CompositeScore: 0.4, OverfitScore: 0.6, StabilityScore: 0.3},
			},
			{
				TaskID: "task-3",
				Status: "failed",
				Error:  "timeout",
			},
		},
	}

	buildSummary(report)
	require.NotNil(t, report.Summary)
	assert.Equal(t, 3, report.Summary.TotalTasks)
	assert.Equal(t, 2, report.Summary.SuccessCount)
	assert.Equal(t, 1, report.Summary.FailCount)
	assert.InDelta(t, 1.0, report.Summary.AvgSharpe, 0.01)
	assert.InDelta(t, 0.15, report.Summary.AvgReturn, 0.01)
	assert.Equal(t, "task-1", report.Summary.BestTaskID)
	assert.Equal(t, "task-2", report.Summary.WorstTaskID)
	assert.Equal(t, 1, report.Summary.GradeDistribution["A"])
	assert.Equal(t, 1, report.Summary.GradeDistribution["C"])
}

func TestBuildSummary_EmptyResults(t *testing.T) {
	report := &BatchReport{
		TotalTasks: 0,
		Results:    []*BatchResult{},
	}

	buildSummary(report)
	require.NotNil(t, report.Summary)
	assert.Equal(t, 0, report.Summary.TotalTasks)
	assert.InDelta(t, 0.0, report.Summary.AvgSharpe, 0.01)
}

func TestBuildSummary_NoScores(t *testing.T) {
	report := &BatchReport{
		TotalTasks: 1,
		Completed:  1,
		Results: []*BatchResult{
			{
				TaskID: "task-ns",
				Status: "completed",
				Result: &domain.BacktestResult{SharpeRatio: 1.0, AnnualReturn: 0.1, MaxDrawdown: -0.1, WinRate: 0.5},
			},
		},
	}

	buildSummary(report)
	require.NotNil(t, report.Summary)
	assert.Equal(t, 1, report.Summary.SuccessCount)
	assert.Empty(t, report.Summary.BestTaskID)
}

func TestToBacktestResult_Nil(t *testing.T) {
	result := toBacktestResult(nil)
	assert.Nil(t, result)
}

func TestToBacktestResult_Basic(t *testing.T) {
	resp := &contracts.BacktestResponse{
		TotalReturn:     0.15,
		AnnualReturn:    0.08,
		SharpeRatio:     1.2,
		SortinoRatio:    1.5,
		MaxDrawdown:     -0.1,
		MaxDrawdownDate: "2023-06-15",
		WinRate:         0.6,
		TotalTrades:     30,
		WinTrades:       18,
		LoseTrades:      12,
		AvgHoldingDays:  5.0,
		CalmarRatio:     0.8,
		StartedAt:       time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		CompletedAt:     time.Date(2023, 12, 31, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}

	result := toBacktestResult(resp)
	require.NotNil(t, result)
	assert.InDelta(t, 0.15, result.TotalReturn, 0.001)
	assert.InDelta(t, 0.08, result.AnnualReturn, 0.001)
	assert.InDelta(t, 1.2, result.SharpeRatio, 0.001)
	assert.Equal(t, 30, result.TotalTrades)
	assert.False(t, result.StartDate.IsZero())
	assert.False(t, result.EndDate.IsZero())
}

func TestToBacktestResult_EmptyDates(t *testing.T) {
	resp := &contracts.BacktestResponse{
		TotalReturn: 0.1,
	}

	result := toBacktestResult(resp)
	require.NotNil(t, result)
	assert.True(t, result.StartDate.IsZero())
	assert.True(t, result.EndDate.IsZero())
	assert.True(t, result.MaxDrawdownDate.IsZero())
}

func TestExportBatchReportCSV(t *testing.T) {
	report := &BatchReport{
		TotalTasks: 2,
		Completed:  1,
		Failed:     1,
		Results: []*BatchResult{
			{
				TaskID:     "task-1",
				Strategy:   "momentum",
				Status:     "completed",
				DurationMs: 1500,
				Result: &domain.BacktestResult{
					SharpeRatio:  1.5,
					AnnualReturn: 0.2,
					MaxDrawdown:  -0.1,
					WinRate:      0.6,
					TotalTrades:  50,
					CalmarRatio:  2.0,
					SortinoRatio: 1.8,
				},
				Score: &BatchScore{
					Grade:          "A",
					OverfitScore:   0.2,
					StabilityScore: 0.8,
					CompositeScore: 0.75,
					Rank:           1,
				},
			},
			{
				TaskID:     "task-2",
				Strategy:   "value",
				Status:     "failed",
				Error:      "timeout",
				DurationMs: 5000,
			},
		},
	}

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "report.csv")

	err := ExportBatchReportCSV(report, path)
	require.NoError(t, err)

	_, err = os.Stat(path)
	require.NoError(t, err)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(content), "task_id")
	assert.Contains(t, string(content), "momentum")
	assert.Contains(t, string(content), "value")
	assert.Contains(t, string(content), "failed")
}

func TestComputeScores(t *testing.T) {
	be := &BatchEngine{}
	report := &BatchReport{
		Results: []*BatchResult{
			{
				TaskID: "task-1",
				Status: "completed",
				Result: &domain.BacktestResult{
					SharpeRatio: 1.5, AnnualReturn: 0.2, MaxDrawdown: -0.1,
					WinRate: 0.6, TotalTrades: 50, CalmarRatio: 2.0,
				},
			},
			{
				TaskID: "task-2",
				Status: "completed",
				Result: &domain.BacktestResult{
					SharpeRatio: 0.5, AnnualReturn: 0.05, MaxDrawdown: -0.3,
					WinRate: 0.4, TotalTrades: 20, CalmarRatio: 0.2,
				},
			},
		},
	}

	be.computeScores(report)

	assert.NotNil(t, report.Results[0].Score)
	assert.NotNil(t, report.Results[1].Score)
	assert.Greater(t, report.Results[0].Score.CompositeScore, report.Results[1].Score.CompositeScore)
	assert.Equal(t, 1, report.Results[0].Score.Rank)
	assert.Equal(t, 2, report.Results[1].Score.Rank)
}

func TestComputeScores_SkipsFailed(t *testing.T) {
	be := &BatchEngine{}
	report := &BatchReport{
		Results: []*BatchResult{
			{
				TaskID: "task-fail",
				Status: "failed",
				Error:  "timeout",
			},
		},
	}

	be.computeScores(report)
	assert.Nil(t, report.Results[0].Score)
}

func TestComputeScores_SingleResult(t *testing.T) {
	be := &BatchEngine{}
	report := &BatchReport{
		Results: []*BatchResult{
			{
				TaskID: "task-only",
				Status: "completed",
				Result: &domain.BacktestResult{
					SharpeRatio: 1.0, AnnualReturn: 0.1, MaxDrawdown: -0.1,
					WinRate: 0.5, TotalTrades: 30, CalmarRatio: 1.0,
				},
			},
		},
	}

	be.computeScores(report)
	assert.NotNil(t, report.Results[0].Score)
	assert.Equal(t, 1, report.Results[0].Score.Rank)
}

func TestScoreResult_PerfectStrategy(t *testing.T) {
	be := &BatchEngine{}
	br := &domain.BacktestResult{
		SharpeRatio:  math.MaxFloat64,
		AnnualReturn: 1.0,
		MaxDrawdown:  -0.001,
		WinRate:      1.0,
		TotalTrades:  1000,
		CalmarRatio:  math.MaxFloat64,
	}

	r := &BatchResult{
		TaskID: "perfect",
		Result: br,
		WalkForward: &domain.WalkForwardReport{
			OverfitScore:   0.0,
			StabilityScore: 1.0,
		},
	}

	score := be.scoreResult(r)
	assert.LessOrEqual(t, score.CompositeScore, 1.0)
	assert.GreaterOrEqual(t, score.CompositeScore, 0.0)
}
