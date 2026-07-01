package batch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

func TestDefaultBatchConfig(t *testing.T) {
	cfg := DefaultBatchConfig()
	if cfg.Concurrency != 4 {
		t.Errorf("expected concurrency 4, got %d", cfg.Concurrency)
	}
	if cfg.RunWF {
		t.Error("expected RunWF false by default")
	}
	if cfg.WFTrainDays != 252 {
		t.Errorf("expected WFTrainDays 252, got %d", cfg.WFTrainDays)
	}
	if cfg.WFTestDays != 63 {
		t.Errorf("expected WFTestDays 63, got %d", cfg.WFTestDays)
	}
	if cfg.WFStepDays != 63 {
		t.Errorf("expected WFStepDays 63, got %d", cfg.WFStepDays)
	}
}

func TestNewBatchEngine(t *testing.T) {
	logger := zerolog.New(nil)
	be := NewBatchEngine(nil, nil, DefaultBatchConfig(), logger)
	if be == nil {
		t.Fatal("expected non-nil BatchEngine")
	}
	if be.config.Concurrency != 4 {
		t.Errorf("expected concurrency 4, got %d", be.config.Concurrency)
	}
	if be.scorer == nil {
		t.Error("expected scorer to be initialized")
	}
	if be.reports == nil {
		t.Error("expected reports map to be initialized")
	}
}

func TestBatchEngineSetConfig(t *testing.T) {
	logger := zerolog.New(nil)
	be := NewBatchEngine(nil, nil, DefaultBatchConfig(), logger)

	newCfg := BatchConfig{Concurrency: 8, RunWF: true, WFTrainDays: 100}
	be.SetConfig(newCfg)
	if be.config.Concurrency != 8 {
		t.Errorf("expected concurrency 8, got %d", be.config.Concurrency)
	}
	if !be.config.RunWF {
		t.Error("expected RunWF true")
	}
	if be.config.WFTrainDays != 100 {
		t.Errorf("expected WFTrainDays 100, got %d", be.config.WFTrainDays)
	}

	// Zero concurrency should default to 4
	be.SetConfig(BatchConfig{Concurrency: 0})
	if be.config.Concurrency != 4 {
		t.Errorf("expected concurrency 4 when set to 0, got %d", be.config.Concurrency)
	}
}

func TestBatchEngineGetReport(t *testing.T) {
	logger := zerolog.New(nil)
	be := NewBatchEngine(nil, nil, DefaultBatchConfig(), logger)

	// Not found
	_, err := be.GetReport("nonexistent")
	if err == nil {
		t.Error("expected error for missing report")
	}

	// Store and retrieve
	report := &BatchReport{BatchID: "batch_123", Status: "completed"}
	be.reportsMu.Lock()
	be.reports["batch_123"] = report
	be.reportsMu.Unlock()

	got, err := be.GetReport("batch_123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.BatchID != "batch_123" {
		t.Errorf("expected batch_123, got %s", got.BatchID)
	}
}

func TestNormalizeMetric(t *testing.T) {
	tests := []struct {
		value, low, high, want float64
	}{
		{0.5, 0, 1, 0.5},
		{-0.5, -1, 1, 0.25},
		{2, 0, 1, 1},
		{-1, 0, 1, 0},
		{5, 5, 5, 0.5}, // high <= low fallback
	}
	for _, tt := range tests {
		got := normalizeMetric(tt.value, tt.low, tt.high)
		if got != tt.want {
			t.Errorf("normalizeMetric(%v,%v,%v) = %v, want %v", tt.value, tt.low, tt.high, got, tt.want)
		}
	}
}

func TestGradeFromScore(t *testing.T) {
	tests := []struct {
		score float64
		want  string
	}{
		{0.85, "A"},
		{0.80, "A"},
		{0.79, "B"},
		{0.65, "B"},
		{0.64, "C"},
		{0.45, "C"},
		{0.44, "D"},
		{0.0, "D"},
	}
	for _, tt := range tests {
		got := gradeFromScore(tt.score)
		if got != tt.want {
			t.Errorf("gradeFromScore(%v) = %s, want %s", tt.score, got, tt.want)
		}
	}
}

func TestBuildSummary_BatchEngine(t *testing.T) {
	report := &BatchReport{
		TotalTasks: 3,
		Completed:  2,
		Failed:     1,
		Results: []*BatchResult{
			{
				TaskID: "t1", Status: "completed",
				Result: &domain.BacktestResult{SharpeRatio: 1.5, AnnualReturn: 0.2, MaxDrawdown: -0.1, WinRate: 0.6, TotalTrades: 10},
				Score:  &BatchScore{CompositeScore: 0.8, Grade: "A"},
			},
			{
				TaskID: "t2", Status: "completed",
				Result: &domain.BacktestResult{SharpeRatio: 0.5, AnnualReturn: 0.1, MaxDrawdown: -0.2, WinRate: 0.4, TotalTrades: 5},
				Score:  &BatchScore{CompositeScore: 0.4, Grade: "C"},
			},
			{TaskID: "t3", Status: "failed"},
		},
	}

	buildSummary(report)
	s := report.Summary
	if s == nil {
		t.Fatal("expected summary")
	}
	if s.TotalTasks != 3 {
		t.Errorf("expected total 3, got %d", s.TotalTasks)
	}
	if s.SuccessCount != 2 {
		t.Errorf("expected success 2, got %d", s.SuccessCount)
	}
	if s.FailCount != 1 {
		t.Errorf("expected fail 1, got %d", s.FailCount)
	}
	if s.AvgSharpe != 1.0 {
		t.Errorf("expected avg sharpe 1.0, got %f", s.AvgSharpe)
	}
	if s.BestTaskID != "t1" {
		t.Errorf("expected best t1, got %s", s.BestTaskID)
	}
	if s.WorstTaskID != "t2" {
		t.Errorf("expected worst t2, got %s", s.WorstTaskID)
	}
	if s.GradeDistribution["A"] != 1 || s.GradeDistribution["C"] != 1 {
		t.Errorf("unexpected grade distribution: %v", s.GradeDistribution)
	}
}

func TestParseBatchCSV(t *testing.T) {
	content := `strategy,stock_pool,start_date,end_date,capital,risk_free_rate,tags
momentum,000001.SZ;000002.SZ,2023-01-01,2023-12-31,1000000,0.03,test|fast
value,000001.SZ,2023-01-01,2023-06-30,,,`

	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.csv")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tasks, err := ParseBatchCSV(path)
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}

	t1 := tasks[0]
	if t1.Strategy != "momentum" {
		t.Errorf("expected strategy momentum, got %s", t1.Strategy)
	}
	if len(t1.StockPool) != 2 {
		t.Errorf("expected 2 stocks, got %d", len(t1.StockPool))
	}
	if t1.Capital != 1000000 {
		t.Errorf("expected capital 1000000, got %f", t1.Capital)
	}
	if len(t1.Tags) != 2 || t1.Tags[0] != "test" {
		t.Errorf("unexpected tags: %v", t1.Tags)
	}

	t2 := tasks[1]
	if t2.Capital != 1000000.0 {
		t.Errorf("expected default capital 1000000, got %f", t2.Capital)
	}
	if t2.RiskFreeRate != 0.03 {
		t.Errorf("expected default risk free 0.03, got %f", t2.RiskFreeRate)
	}
}

func TestParseJinCeCSV(t *testing.T) {
	content := `策略名称,股票池,开始日期,结束日期,初始资金,无风险利率,标签
momentum,000001.SZ,2023-01-01,2023-12-31,500000,0.02,alpha`

	dir := t.TempDir()
	path := filepath.Join(dir, "jince.csv")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tasks, err := ParseJinCeCSV(path)
	if err != nil {
		t.Fatalf("parse jince csv: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Capital != 500000 {
		t.Errorf("expected capital 500000, got %f", tasks[0].Capital)
	}
	if tasks[0].RiskFreeRate != 0.02 {
		t.Errorf("expected risk free 0.02, got %f", tasks[0].RiskFreeRate)
	}
}

func TestGenerateTaskMatrix(t *testing.T) {
	strategies := []string{"s1", "s2"}
	stockPools := [][]string{{"A"}, {"B", "C"}}
	dateRanges := [][2]string{{"2023-01-01", "2023-12-31"}}

	tasks := GenerateTaskMatrix(strategies, stockPools, dateRanges)
	if len(tasks) != 4 {
		t.Fatalf("expected 4 tasks, got %d", len(tasks))
	}
	if tasks[0].Strategy != "s1" || len(tasks[0].StockPool) != 1 {
		t.Errorf("unexpected first task: %+v", tasks[0])
	}
	if tasks[3].Strategy != "s2" || len(tasks[3].StockPool) != 2 {
		t.Errorf("unexpected last task: %+v", tasks[3])
	}
}

func TestExportBatchReportJSON(t *testing.T) {
	report := &BatchReport{
		BatchID: "b1",
		Status:  "completed",
		Results: []*BatchResult{
			{TaskID: "t1", Status: "completed"},
		},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	if err := ExportBatchReportJSON(report, path); err != nil {
		t.Fatalf("export json: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.Contains(string(data), "b1") {
		t.Error("expected JSON to contain batch id")
	}
}

func TestExportBatchReportCSV_BatchEngine(t *testing.T) {
	report := &BatchReport{
		BatchID: "b1",
		Results: []*BatchResult{
			{
				TaskID: "t1", Status: "completed",
				Result: &domain.BacktestResult{SharpeRatio: 1.2, TotalTrades: 5},
				Score:  &BatchScore{Grade: "B", OverfitScore: 0.3, CompositeScore: 0.6, Rank: 1},
			},
		},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "report.csv")
	if err := ExportBatchReportCSV(report, path); err != nil {
		t.Fatalf("export csv: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.Contains(string(data), "t1") {
		t.Error("expected CSV to contain task id")
	}
}

func TestSplitStocks(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"A,B,C", 3},
		{"A;B", 2},
		{"A|B|C", 3},
		{"  A  ", 1},
		{"", 0},
	}
	for _, tt := range tests {
		got := splitStocks(tt.input)
		if len(got) != tt.want {
			t.Errorf("splitStocks(%q) = %v, want len %d", tt.input, got, tt.want)
		}
	}
}

func TestParseFloat(t *testing.T) {
	if parseFloat("3.14", 0) != 3.14 {
		t.Error("expected 3.14")
	}
	if parseFloat("", 1.5) != 1.5 {
		t.Error("expected default 1.5")
	}
	if parseFloat("bad", 2.0) != 2.0 {
		t.Error("expected default 2.0 for bad input")
	}
}

func TestToBacktestResult(t *testing.T) {
	resp := &contracts.BacktestResponse{
		ID:              "id1",
		TotalReturn:     0.1,
		SharpeRatio:     1.2,
		MaxDrawdownDate: "2023-06-01",
		StartedAt:       time.Now().Format(time.RFC3339),
		CompletedAt:     time.Now().Format(time.RFC3339),
	}
	br := toBacktestResult(resp)
	if br == nil {
		t.Fatal("expected non-nil result")
	}
	if br.TotalReturn != 0.1 {
		t.Errorf("expected total return 0.1, got %f", br.TotalReturn)
	}
	if br.SharpeRatio != 1.2 {
		t.Errorf("expected sharpe 1.2, got %f", br.SharpeRatio)
	}
	if br.MaxDrawdownDate.IsZero() {
		t.Error("expected max drawdown date parsed")
	}

	// nil input
	if toBacktestResult(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func TestBatchEngineRunEmptyTasks(t *testing.T) {
	logger := zerolog.New(nil)
	be := NewBatchEngine(nil, nil, DefaultBatchConfig(), logger)
	_, err := be.Run(context.Background(), nil)
	if err == nil {
		t.Error("expected error for empty tasks")
	}
}

func TestComputeScoresAndRanks(t *testing.T) {
	logger := zerolog.New(nil)
	be := NewBatchEngine(nil, nil, DefaultBatchConfig(), logger)

	report := &BatchReport{
		Results: []*BatchResult{
			{
				TaskID: "t1", Status: "completed",
				Result: &domain.BacktestResult{SharpeRatio: 2.0, AnnualReturn: 0.3, MaxDrawdown: -0.05, WinRate: 0.7, TotalTrades: 20, CalmarRatio: 3.0},
			},
			{
				TaskID: "t2", Status: "completed",
				Result: &domain.BacktestResult{SharpeRatio: 0.5, AnnualReturn: 0.05, MaxDrawdown: -0.2, WinRate: 0.4, TotalTrades: 10, CalmarRatio: 0.5},
			},
			{TaskID: "t3", Status: "failed"},
		},
	}

	be.computeScores(report)

	if report.Results[0].Score == nil || report.Results[1].Score == nil {
		t.Fatal("expected scores for completed results")
	}
	if report.Results[2].Score != nil {
		t.Error("expected no score for failed result")
	}

	// t1 should rank higher than t2
	if report.Results[0].Score.Rank >= report.Results[1].Score.Rank {
		t.Errorf("expected t1 rank < t2 rank, got %d vs %d", report.Results[0].Score.Rank, report.Results[1].Score.Rank)
	}

	// Grades should be reasonable
	if report.Results[0].Score.Grade == "" {
		t.Error("expected grade for t1")
	}
}

func TestScorerScoreResult(t *testing.T) {
	scorer := NewScorer()

	result := &BatchResult{
		Status: "completed",
		Result: &domain.BacktestResult{
			SharpeRatio:  1.5,
			AnnualReturn: 0.2,
			MaxDrawdown:  -0.1,
			WinRate:      0.6,
			TotalTrades:  20,
			CalmarRatio:  2.0,
		},
	}

	score := scorer.ScoreResult(result)
	if score == nil {
		t.Fatal("expected non-nil score")
	}
	if score.CompositeScore < 0 || score.CompositeScore > 1 {
		t.Errorf("composite score out of range: %f", score.CompositeScore)
	}
	if score.Grade == "" {
		t.Error("expected grade")
	}
	if score.OverfitScore < 0 || score.OverfitScore > 1 {
		t.Errorf("overfit score out of range: %f", score.OverfitScore)
	}
	if score.StabilityScore < 0 || score.StabilityScore > 1 {
		t.Errorf("stability score out of range: %f", score.StabilityScore)
	}
}

func TestScorerScoreResultWithWalkForward(t *testing.T) {
	scorer := NewScorer()

	result := &BatchResult{
		Status: "completed",
		Result: &domain.BacktestResult{
			SharpeRatio:  1.5,
			AnnualReturn: 0.2,
			MaxDrawdown:  -0.1,
			WinRate:      0.6,
			TotalTrades:  20,
			CalmarRatio:  2.0,
		},
		WalkForward: &domain.WalkForwardReport{
			OverfitScore:   0.2,
			StabilityScore: 0.8,
		},
	}

	score := scorer.ScoreResult(result)
	if score == nil {
		t.Fatal("expected non-nil score")
	}
	if score.OverfitScore != 0.2 {
		t.Errorf("expected overfit 0.2, got %f", score.OverfitScore)
	}
	if score.StabilityScore != 0.8 {
		t.Errorf("expected stability 0.8, got %f", score.StabilityScore)
	}
}

func TestScorerScoreBatch(t *testing.T) {
	scorer := NewScorer()
	report := &BatchReport{
		Results: []*BatchResult{
			{
				TaskID: "t1", Status: "completed",
				Result: &domain.BacktestResult{SharpeRatio: 2.0, AnnualReturn: 0.3, MaxDrawdown: -0.05, WinRate: 0.7, TotalTrades: 20, CalmarRatio: 3.0},
			},
			{
				TaskID: "t2", Status: "completed",
				Result: &domain.BacktestResult{SharpeRatio: 0.5, AnnualReturn: 0.05, MaxDrawdown: -0.2, WinRate: 0.4, TotalTrades: 10, CalmarRatio: 0.5},
			},
		},
	}

	scorer.ScoreBatch(report)

	if report.Results[0].Score == nil || report.Results[1].Score == nil {
		t.Fatal("expected scores")
	}
	if report.Results[0].Score.Rank == 0 || report.Results[1].Score.Rank == 0 {
		t.Error("expected ranks assigned")
	}
	// Higher composite should have lower rank number
	if report.Results[0].Score.CompositeScore <= report.Results[1].Score.CompositeScore {
		t.Error("expected t1 to have higher composite score than t2")
	}
	if report.Results[0].Score.Rank >= report.Results[1].Score.Rank {
		t.Errorf("expected t1 rank < t2 rank, got %d vs %d", report.Results[0].Score.Rank, report.Results[1].Score.Rank)
	}
}

func TestEstimateOverfitHeuristic(t *testing.T) {
	// High sharpe + few trades -> higher overfit
	br := &domain.BacktestResult{SharpeRatio: 3.5, TotalTrades: 5, AnnualReturn: 0.5, MaxDrawdown: -0.02}
	of := estimateOverfitHeuristic(br)
	if of < 0.5 {
		t.Errorf("expected high overfit for extreme sharpe+few trades, got %f", of)
	}

	// Normal result -> lower overfit
	br2 := &domain.BacktestResult{SharpeRatio: 1.0, TotalTrades: 50, AnnualReturn: 0.1, MaxDrawdown: -0.1}
	of2 := estimateOverfitHeuristic(br2)
	if of2 > 0.5 {
		t.Errorf("expected lower overfit for normal result, got %f", of2)
	}

	// nil input
	if estimateOverfitHeuristic(nil) != 0.5 {
		t.Error("expected 0.5 for nil input")
	}
}

func TestEstimateStabilityHeuristic(t *testing.T) {
	// Many trades -> higher stability
	br := &domain.BacktestResult{TotalTrades: 60, CalmarRatio: 1.5, MaxDrawdown: -0.1}
	s := estimateStabilityHeuristic(br)
	if s < 0.5 {
		t.Errorf("expected higher stability for many trades, got %f", s)
	}

	// Few trades -> lower stability
	br2 := &domain.BacktestResult{TotalTrades: 2, CalmarRatio: 0.5, MaxDrawdown: -0.05}
	s2 := estimateStabilityHeuristic(br2)
	if s2 > 0.5 {
		t.Errorf("expected lower stability for few trades, got %f", s2)
	}

	// nil input
	if estimateStabilityHeuristic(nil) != 0.5 {
		t.Error("expected 0.5 for nil input")
	}
}

func TestBatchEngineStoresReport(t *testing.T) {
	logger := zerolog.New(nil)
	be := NewBatchEngine(nil, nil, DefaultBatchConfig(), logger)

	// Manually inject a report
	report := &BatchReport{BatchID: "batch_test", Status: "completed"}
	be.reportsMu.Lock()
	be.reports["batch_test"] = report
	be.reportsMu.Unlock()

	got, err := be.GetReport("batch_test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.BatchID != "batch_test" {
		t.Errorf("expected batch_test, got %s", got.BatchID)
	}
}
