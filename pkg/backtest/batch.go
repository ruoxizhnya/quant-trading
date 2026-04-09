package backtest

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

type BatchTask struct {
	ID          string            `json:"id"`
	Strategy    string            `json:"strategy"`
	StockPool   []string          `json:"stock_pool"`
	StartDate   string            `json:"start_date"`
	EndDate     string            `json:"end_date"`
	Capital     float64           `json:"capital"`
	RiskFreeRate float64          `json:"risk_free_rate"`
	Params      map[string]any    `json:"params,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type BatchResult struct {
	TaskID       string                `json:"task_id"`
	Strategy     string                `json:"strategy"`
	Status       string                `json:"status"`
	Error        string                `json:"error,omitempty"`
	DurationMs   int64                 `json:"duration_ms"`
	Result       *domain.BacktestResult `json:"result,omitempty"`
	WalkForward  *domain.WalkForwardReport `json:"walk_forward,omitempty"`
	Score        *BatchScore           `json:"score,omitempty"`
}

type BatchScore struct {
	Grade           string  `json:"grade"`
	OverfitScore    float64 `json:"overfit_score"`
	StabilityScore  float64 `json:"stability_score"`
	CompositeScore  float64 `json:"composite_score"`
	Rank            int     `json:"rank"`
}

type BatchReport struct {
	BatchID      string                  `json:"batch_id"`
	CreatedAt    time.Time               `json:"created_at"`
	CompletedAt time.Time               `json:"completed_at"`
	Status       string                  `json:"status"`
	TotalTasks   int                     `json:"total_tasks"`
	Completed    int                     `json:"completed"`
	Failed       int                     `json:"failed"`
	DurationMs   int64                   `json:"duration_ms"`
	Results      []*BatchResult         `json:"results"`
	Summary      *BatchSummary          `json:"summary"`
}

type BatchSummary struct {
	TotalTasks       int             `json:"total_tasks"`
	SuccessCount     int             `json:"success_count"`
	FailCount        int             `json:"fail_count"`
	AvgSharpe        float64         `json:"avg_sharpe"`
	AvgReturn        float64         `json:"avg_annual_return"`
	AvgMaxDD         float64         `json:"avg_max_drawdown"`
	AvgWinRate       float64         `json:"avg_win_rate"`
	AvgOverfitScore  float64         `json:"avg_overfit_score"`
	AvgStabilityScore float64        `json:"avg_stability_score"`
	GradeDistribution map[string]int `json:"grade_distribution"`
	BestTaskID       string          `json:"best_task_id"`
	WorstTaskID      string          `json:"worst_task_id"`
}

type BatchConfig struct {
	Concurrency int `json:"concurrency"`
	RunWF       bool `json:"run_walk_forward"`
	WFTrainDays int `json:"wf_train_days"`
	WFTestDays  int `json:"wf_test_days"`
	WFStepDays  int `json:"wf_step_days"`
}

func DefaultBatchConfig() BatchConfig {
	return BatchConfig{
		Concurrency: 4,
		RunWF:       false,
		WFTrainDays: 252,
		WFTestDays:  63,
		WFStepDays:  63,
	}
}

type BatchEngine struct {
	engine *Engine
	wfEng  *WalkForwardEngine
	config BatchConfig
	logger zerolog.Logger
}

func NewBatchEngine(engine *Engine, wfEng *WalkForwardEngine, config BatchConfig, logger zerolog.Logger) *BatchEngine {
	if config.Concurrency <= 0 {
		config.Concurrency = 4
	}
	return &BatchEngine{
		engine: engine,
		wfEng:  wfEng,
		config: config,
		logger: logger.With().Str("component", "batch_engine").Logger(),
	}
}

func (b *BatchEngine) Run(ctx context.Context, tasks []BatchTask) (*BatchReport, error) {
	if len(tasks) == 0 {
		return nil, fmt.Errorf("no tasks to run")
	}

	batchID := fmt.Sprintf("batch_%d", time.Now().UnixNano())
	startTime := time.Now()

	b.logger.Info().
		Str("batch_id", batchID).
		Int("tasks", len(tasks)).
		Int("concurrency", b.config.Concurrency).
		Bool("walk_forward", b.config.RunWF).
		Msg("Starting batch backtest")

	report := &BatchReport{
		BatchID:   batchID,
		CreatedAt: startTime,
		Status:    "running",
		TotalTasks: len(tasks),
		Results:   make([]*BatchResult, len(tasks)),
	}

	sem := make(chan struct{}, b.config.Concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, t BatchTask) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result := b.runSingleTask(ctx, t)

			mu.Lock()
			report.Results[idx] = result
			if result.Status == "completed" {
				report.Completed++
			} else {
				report.Failed++
			}
			mu.Unlock()
		}(i, task)
	}
	wg.Wait()

	report.CompletedAt = time.Now()
	report.DurationMs = report.CompletedAt.Sub(startTime).Milliseconds()
	report.Status = "completed"

	b.computeScores(report)
	buildSummary(report)

	b.logger.Info().
		Str("batch_id", batchID).
		Int("completed", report.Completed).
		Int("failed", report.Failed).
		Int64("duration_ms", report.DurationMs).
		Msg("Batch backtest complete")

	return report, nil
}

func (b *BatchEngine) runSingleTask(ctx context.Context, task BatchTask) *BatchResult {
	taskStart := time.Now()
	result := &BatchResult{
		TaskID:   task.ID,
		Strategy: task.Strategy,
		Status:   "running",
	}

	req := BacktestRequest{
		Strategy:       task.Strategy,
		StockPool:      task.StockPool,
		StartDate:      task.StartDate,
		EndDate:        task.EndDate,
		InitialCapital: task.Capital,
		RiskFreeRate:   task.RiskFreeRate,
	}

	resp, err := b.engine.RunBacktest(ctx, req)
	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		result.DurationMs = time.Since(taskStart).Milliseconds()
		return result
	}

	br := toBacktestResult(resp)
	result.Result = br
	result.Status = "completed"
	result.DurationMs = time.Since(taskStart).Milliseconds()

	if b.config.RunWF && b.wfEng != nil && len(task.StockPool) > 0 {
		wfReq := WalkForwardRequest{
			Strategy:       task.Strategy,
			StockPool:      task.StockPool,
			StartDate:      task.StartDate,
			EndDate:        task.EndDate,
			InitialCapital: task.Capital,
			RiskFreeRate:   task.RiskFreeRate,
			WalkForwardParams: domain.WalkForwardParams{
				TrainDays: b.config.WFTrainDays,
				TestDays:  b.config.WFTestDays,
				StepDays:  b.config.WFStepDays,
			},
		}
		wfReport, wfErr := b.wfEng.RunWalkForward(ctx, wfReq)
		if wfErr == nil {
			result.WalkForward = wfReport
		} else {
			b.logger.Debug().Err(wfErr).Str("task", task.ID).Msg("Walk-forward skipped for this task")
		}
	}

	return result
}

func (b *BatchEngine) computeScores(report *BatchReport) {
	scores := make([]*BatchScore, 0, len(report.Results))
	for _, r := range report.Results {
		if r.Status != "completed" || r.Result == nil {
			continue
		}
		score := b.scoreResult(r)
		r.Score = score
		scores = append(scores, score)
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].CompositeScore > scores[j].CompositeScore
	})
	for i, s := range scores {
		s.Rank = i + 1
	}
}

func (b *BatchEngine) scoreResult(r *BatchResult) *BatchScore {
	score := &BatchScore{}

	br := r.Result
	overfit := 0.5
	stability := 0.5

	if r.WalkForward != nil {
		overfit = r.WalkForward.OverfitScore
		stability = r.WalkForward.StabilityScore
	}

	sharpeNorm := normalizeMetric(br.SharpeRatio, -1, 3)
	returnNorm := normalizeMetric(br.AnnualReturn, -0.3, 0.5)
	ddNorm := normalizeMetric(-br.MaxDrawdown, -0.5, -0.05)
	winRateNorm := normalizeMetric(br.WinRate, 0.3, 0.7)
	tradesNorm := normalizeMetric(math.Log10(float64(br.TotalTrades+1)), 0.5, 2.5)
	calmarNorm := normalizeMetric(br.CalmarRatio, 0, 5)

	rawPerformance := sharpeNorm*0.25 +
		returnNorm*0.15 +
		ddNorm*0.15 +
		winRateNorm*0.10 +
		tradesNorm*0.05 +
		calmarNorm*0.10

	robustnessPenalty := overfit * 0.15
	stabilityBonus := stability * 0.05

	composite := rawPerformance - robustnessPenalty + stabilityBonus
	composite = math.Max(0, math.Min(composite, 1))

	score.OverfitScore = overfit
	score.StabilityScore = stability
	score.CompositeScore = composite
	score.Grade = gradeFromScore(composite)

	return score
}

func buildSummary(report *BatchReport) {
	s := &BatchSummary{
		TotalTasks:        report.TotalTasks,
		SuccessCount:      report.Completed,
		FailCount:         report.Failed,
		GradeDistribution: make(map[string]int),
	}

	var sumSharpe, sumReturn, sumMaxDD, sumWinRate, sumOF, sumSS float64
	count := 0
	bestScore := -1.0
	worstScore := 2.0
	var bestID, worstID string

	for _, r := range report.Results {
		if r.Status != "completed" || r.Result == nil {
			continue
		}
		count++
		sumSharpe += r.Result.SharpeRatio
		sumReturn += r.Result.AnnualReturn
		sumMaxDD += r.Result.MaxDrawdown
		sumWinRate += r.Result.WinRate

		if r.Score != nil {
			sumOF += r.Score.OverfitScore
			sumSS += r.Score.StabilityScore
			s.GradeDistribution[r.Score.Grade]++

			if r.Score.CompositeScore > bestScore {
				bestScore = r.Score.CompositeScore
				bestID = r.TaskID
			}
			if r.Score.CompositeScore < worstScore {
				worstScore = r.Score.CompositeScore
				worstID = r.TaskID
			}
		}
	}

	if count > 0 {
		fn := float64(count)
		s.AvgSharpe = sumSharpe / fn
		s.AvgReturn = sumReturn / fn
		s.AvgMaxDD = sumMaxDD / fn
		s.AvgWinRate = sumWinRate / fn
		s.AvgOverfitScore = sumOF / fn
		s.AvgStabilityScore = sumSS / fn
	}

	s.BestTaskID = bestID
	s.WorstTaskID = worstID
	report.Summary = s
}

func normalizeMetric(value, low, high float64) float64 {
	if high <= low {
		return 0.5
	}
	norm := (value - low) / (high - low)
	return math.Max(0, math.Min(norm, 1))
}

func gradeFromScore(score float64) string {
	switch {
	case score >= 0.80:
		return "A"
	case score >= 0.65:
		return "B"
	case score >= 0.45:
		return "C"
	default:
		return "D"
	}
}

func toBacktestResult(r *BacktestResponse) *domain.BacktestResult {
	if r == nil {
		return nil
	}
	var maxDDDate time.Time
	if r.MaxDrawdownDate != "" {
		maxDDDate, _ = time.Parse("2006-01-02", r.MaxDrawdownDate)
	}
	var completedAt time.Time
	if r.CompletedAt != "" {
		completedAt, _ = time.Parse(time.RFC3339, r.CompletedAt)
	}
	var startedAt time.Time
	if r.StartedAt != "" {
		startedAt, _ = time.Parse(time.RFC3339, r.StartedAt)
	}
	return &domain.BacktestResult{
		StartDate:        startedAt,
		EndDate:         completedAt,
		TotalReturn:     r.TotalReturn,
		AnnualReturn:    r.AnnualReturn,
		SharpeRatio:     r.SharpeRatio,
		SortinoRatio:    r.SortinoRatio,
		MaxDrawdown:     r.MaxDrawdown,
		MaxDrawdownDate: maxDDDate,
		WinRate:         r.WinRate,
		TotalTrades:     r.TotalTrades,
		WinTrades:       r.WinTrades,
		LoseTrades:      r.LoseTrades,
		AvgHoldingDays:  r.AvgHoldingDays,
		CalmarRatio:     r.CalmarRatio,
		PortfolioValues: r.PortfolioValues,
		Trades:          r.Trades,
	}
}

func ParseBatchCSV(path string) ([]BatchTask, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open csv: %w", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("csv must have header + at least 1 data row")
	}

	header := records[0]
	colMap := make(map[string]int)
	for i, h := range header {
		colMap[strings.TrimSpace(strings.ToLower(h))] = i
	}

	requiredCols := []string{"strategy", "stock_pool", "start_date", "end_date"}
	for _, col := range requiredCols {
		if _, ok := colMap[col]; !ok {
			return nil, fmt.Errorf("missing required column: %q", col)
		}
	}

	tasks := make([]BatchTask, 0, len(records)-1)
	for rowIdx, row := range records[1:] {
		get := func(key string) string {
			if idx, ok := colMap[key]; ok && idx < len(row) {
				return strings.TrimSpace(row[idx])
			}
			return ""
		}

		strategy := get("strategy")
		stockPoolStr := get("stock_pool")
		startDate := get("start_date")
		endDate := get("end_date")

		if strategy == "" || stockPoolStr == "" || startDate == "" || endDate == "" {
			continue
		}

		stockPool := splitStocks(stockPoolStr)
		capital := parseFloat(get("capital"), 1000000.0)
		riskFree := parseFloat(get("risk_free_rate"), 0.03)

		task := BatchTask{
			ID:          fmt.Sprintf("task_%d_%s", rowIdx+1, strategy),
			Strategy:    strategy,
			StockPool:   stockPool,
			StartDate:   startDate,
			EndDate:     endDate,
			Capital:     capital,
			RiskFreeRate: riskFree,
		}

		if tags := get("tags"); tags != "" {
			task.Tags = strings.Split(tags, "|")
		}

		tasks = append(tasks, task)
	}

	return tasks, nil
}

func ParseJinCeCSV(path string) ([]BatchTask, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open csv: %w", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("csv must have header + at least 1 data row")
	}

	header := records[0]
	colMap := make(map[string]int)
	for i, h := range header {
		colMap[strings.TrimSpace(strings.ToLower(h))] = i
	}

	jinCeMapping := map[string]string{
		"策略名称":  "strategy",
		"股票池":   "stock_pool",
		"开始日期":  "start_date",
		"结束日期":  "end_date",
		"初始资金":  "capital",
		"无风险利率": "risk_free_rate",
		"标签":    "tags",
	}

	for zh, en := range jinCeMapping {
		if idx, ok := colMap[zh]; ok {
			colMap[en] = idx
		}
	}

	requiredCols := []string{"strategy", "stock_pool", "start_date", "end_date"}
	for _, col := range requiredCols {
		if _, ok := colMap[col]; !ok {
			return nil, fmt.Errorf("missing required column: %q (金策格式支持中英文列名)", col)
		}
	}

	tasks := make([]BatchTask, 0, len(records)-1)
	for rowIdx, row := range records[1:] {
		get := func(key string) string {
			if idx, ok := colMap[key]; ok && idx < len(row) {
				return strings.TrimSpace(row[idx])
			}
			return ""
		}

		strategy := get("strategy")
		stockPoolStr := get("stock_pool")
		startDate := get("start_date")
		endDate := get("end_date")

		if strategy == "" || stockPoolStr == "" || startDate == "" || endDate == "" {
			continue
		}

		stockPool := splitStocks(stockPoolStr)
		capital := parseFloat(get("capital"), 1000000.0)
		riskFree := parseFloat(get("risk_free_rate"), 0.03)

		task := BatchTask{
			ID:           fmt.Sprintf("jince_%d_%s", rowIdx+1, strategy),
			Strategy:     strategy,
			StockPool:    stockPool,
			StartDate:    startDate,
			EndDate:      endDate,
			Capital:      capital,
			RiskFreeRate: riskFree,
		}

		if tags := get("tags"); tags != "" {
			task.Tags = strings.Split(tags, "|")
		}

		tasks = append(tasks, task)
	}

	return tasks, nil
}

func GenerateTaskMatrix(strategies []string, stockPools [][]string, dateRanges [][2]string) []BatchTask {
	var tasks []BatchTask
	taskIdx := 0
	for _, strat := range strategies {
		for _, pool := range stockPools {
			for _, dr := range dateRanges {
				taskIdx++
				tasks = append(tasks, BatchTask{
					ID:         fmt.Sprintf("matrix_%d_%s", taskIdx, strat),
					Strategy:   strat,
					StockPool:  pool,
					StartDate:  dr[0],
					EndDate:    dr[1],
					Capital:    1000000.0,
					RiskFreeRate: 0.03,
				})
			}
		}
	}
	return tasks
}

func ExportBatchReportJSON(report *BatchReport, path string) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func ExportBatchReportCSV(report *BatchReport, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	defer f.Close()

	writer := csv.NewWriter(f)
	defer writer.Flush()

	headers := []string{
		"task_id", "strategy", "status", "error",
		"duration_ms", "sharpe_ratio", "annual_return", "max_drawdown",
		"win_rate", "total_trades", "calmar_ratio", "sortino_ratio",
		"grade", "overfit_score", "stability_score", "composite_score", "rank",
	}
	if err := writer.Write(headers); err != nil {
		return err
	}

	for _, r := range report.Results {
		sharpe, ret, dd, wr, trades, calmar, sortino := 0.0, 0.0, 0.0, 0.0, 0, 0.0, 0.0
		grade, of, ss, cs, rank := "", 0.0, 0.0, 0.0, 0
		if r.Result != nil {
			sharpe = r.Result.SharpeRatio
			ret = r.Result.AnnualReturn
			dd = r.Result.MaxDrawdown
			wr = r.Result.WinRate
			trades = r.Result.TotalTrades
			calmar = r.Result.CalmarRatio
			sortino = r.Result.SortinoRatio
		}
		if r.Score != nil {
			grade = r.Score.Grade
			of = r.Score.OverfitScore
			ss = r.Score.StabilityScore
			cs = r.Score.CompositeScore
			rank = r.Score.Rank
		}
		row := []string{
			r.TaskID, r.Strategy, r.Status, r.Error,
			fmt.Sprintf("%d", r.DurationMs),
			fmt.Sprintf("%.4f", sharpe), fmt.Sprintf("%.4f", ret), fmt.Sprintf("%.4f", dd),
			fmt.Sprintf("%.4f", wr), fmt.Sprintf("%d", trades), fmt.Sprintf("%.4f", calmar), fmt.Sprintf("%.4f", sortino),
			grade, fmt.Sprintf("%.4f", of), fmt.Sprintf("%.4f", ss), fmt.Sprintf("%.4f", cs), fmt.Sprintf("%d", rank),
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	return nil
}

func splitStocks(s string) []string {
	for _, sep := range []string{",", ";", "|"} {
		if strings.Contains(s, sep) {
			parts := strings.Split(s, sep)
			result := make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					result = append(result, p)
				}
			}
			return result
		}
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return []string{s}
}

func parseFloat(s string, defaultVal float64) float64 {
	if s == "" {
		return defaultVal
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return defaultVal
	}
	return v
}
