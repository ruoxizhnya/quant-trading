package data

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/statistics"
)

// FactorStore defines the interface for factor data operations.
// This interface allows for easy mocking in tests.
type FactorStore interface {
	GetOHLCVForDateRange(ctx context.Context, startDate, endDate time.Time) ([]domain.OHLCV, error)
	GetFundamentalsSnapshot(ctx context.Context, cutoffDate time.Time) ([]domain.FundamentalData, error)
	// GetFundamentalsDetailAsOf serves the 桥 B1 vertical factors
	// (factor_equitydeep.go). Its ann_date filter is the contract's PIT rule and
	// is not optional: a factor computed from a report the market had not yet
	// seen is a look-ahead bug, not a data-quality issue.
	GetFundamentalsDetailAsOf(ctx context.Context, fieldCodes []string, asOf time.Time) ([]domain.FundamentalsDetailRow, error)
	GetTradingDays(ctx context.Context, startDate, endDate time.Time) ([]time.Time, error)
	SaveFactorCacheBatch(ctx context.Context, entries []*domain.FactorCacheEntry) error
}

// FactorComputer computes and caches factor z-scores.
// It reads raw data (OHLCV, fundamentals) from PostgresStore,
// computes cross-sectional z-scores, and persists to factor_cache table.
type FactorComputer struct {
	store  FactorStore
	logger zerolog.Logger
	// registry 是因子定义的库管理读路径（D3）。计算清单、按名取因子、
	// 版本指纹全部以它为准——本文件不再维护任何硬编码因子清单。
	registry *Registry
}

// NewFactorComputer creates a new FactorComputer.
//
// 注册表默认装载内置种子定义（BuiltinFactorDefinitions）。种子是编译期
// 常量，加载校验失败意味着定义本身写错——开发期错误，立即 panic 好过
// 带着坏定义上线。运行期要用 DB 定义集覆盖时走 SetRegistry。
func NewFactorComputer(store FactorStore) *FactorComputer {
	return &FactorComputer{
		store:    store,
		logger:   zerolog.Nop(),
		registry: mustBuiltinRegistry(),
	}
}

// mustBuiltinRegistry 加载内置种子定义（fail-loud）。
func mustBuiltinRegistry() *Registry {
	r, err := NewRegistry(domain.BuiltinFactorDefinitions)
	if err != nil {
		panic(fmt.Sprintf("builtin factor definitions invalid: %v", err))
	}
	return r
}

// SetRegistry 替换注册表（供 DB 加载的定义集覆盖内置种子，或测试注入）。
func (f *FactorComputer) SetRegistry(r *Registry) { f.registry = r }

func (f *FactorComputer) SetLogger(l zerolog.Logger) {
	f.logger = l.With().Str("component", "factor_computer").Logger()
}

// ZScore normalizes raw values to z-scores (cross-sectional, per date).
// Returns a map from symbol to z-score. Symbols with NaN or zero variance
// are assigned a z-score of 0.
//
// The math (mean / population stddev) is delegated to pkg/statistics
// (ODR-013 P1-21). The map input/output is kept here because the
// factor pipeline passes OHLCV-derived symbol keys; converting to
// []float64 at every call site would lose that mapping.
//
// We build a (symbol, value) slice once so that the same key index is
// used both for the z-score lookup and the result assignment; relying
// on `for symbol := range values` twice would mis-pair entries because
// Go map iteration order is not stable across iterations.
func ZScore(values map[string]float64) map[string]float64 {
	n := len(values)
	if n == 0 {
		return make(map[string]float64)
	}
	type kv struct {
		symbol string
		value  float64
	}
	pairs := make([]kv, 0, n)
	for symbol, v := range values {
		pairs = append(pairs, kv{symbol, v})
	}
	raw := make([]float64, n)
	for i, p := range pairs {
		raw[i] = p.value
	}
	zs := statistics.ZScore(raw)
	result := make(map[string]float64, n)
	for i, p := range pairs {
		result[p.symbol] = zs[i]
	}
	return result
}

// PercentileRank converts raw values to percentile ranks [0, 100].
//
// The formula here is the **ordinal** percentile (no tie handling):
// for the i-th smallest value in a slice of length n, the rank is
// (i+1 - 0.5) / n * 100. This is the standard SAS / SQL PERCENT_RANK
// flavour and is what the factor pipeline stores downstream. It is
// intentionally distinct from statistics.PercentileRank, which
// uses the **average** (competition) rank. Do not migrate this to
// the shared helper without first auditing factor-cache consumers.
func PercentileRank(values map[string]float64) map[string]float64 {
	n := len(values)
	if n == 0 {
		return make(map[string]float64)
	}
	type kv struct {
		symbol string
		value  float64
	}
	sorted := make([]kv, 0, n)
	for symbol, v := range values {
		sorted = append(sorted, kv{symbol, v})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].value < sorted[j].value })
	result := make(map[string]float64, n)
	for i, kv := range sorted {
		rank := float64(i+1) - 0.5
		result[kv.symbol] = (rank / float64(n)) * 100
	}
	return result
}

// ComputeMomentumFactor computes N-day return for all stocks on a given date.
// Momentum = (close_today / close_N_days_ago) - 1.
// Default lookback is 20 trading days.
func (f *FactorComputer) ComputeMomentumFactor(ctx context.Context, date time.Time, lookbackDays int) error {
	if lookbackDays <= 0 {
		lookbackDays = 20
	}
	startDate := date.AddDate(0, 0, -(lookbackDays + 10))
	allOHLCV, err := f.store.GetOHLCVForDateRange(ctx, startDate, date)
	if err != nil {
		return fmt.Errorf("load OHLCV for momentum: %w", err)
	}
	rawValues := f.computeMomentumRaw(allOHLCV, date, lookbackDays)
	if len(rawValues) == 0 {
		f.logger.Warn().Time("date", date).Msg("No momentum values computed (insufficient OHLCV data)")
		return nil
	}
	zScores := ZScore(rawValues)
	percentiles := PercentileRank(rawValues)
	var entries []*domain.FactorCacheEntry
	for symbol, raw := range rawValues {
		entries = append(entries, &domain.FactorCacheEntry{
			Symbol:     symbol,
			TradeDate:  date,
			FactorName: domain.FactorMomentum,
			RawValue:   raw,
			ZScore:     zScores[symbol],
			Percentile: percentiles[symbol],
		})
	}
	if err := f.store.SaveFactorCacheBatch(ctx, entries); err != nil {
		return fmt.Errorf("save momentum factor_cache: %w", err)
	}
	f.logger.Info().
		Time("date", date).
		Int("stocks", len(entries)).
		Int("lookback", lookbackDays).
		Msg("Momentum factor computed and cached")
	return nil
}

func (f *FactorComputer) computeMomentumRaw(allOHLCV []domain.OHLCV, targetDate time.Time, lookback int) map[string]float64 {
	type bar struct {
		date  time.Time
		close float64
	}
	stockBars := make(map[string][]bar)
	for _, o := range allOHLCV {
		stockBars[o.Symbol] = append(stockBars[o.Symbol], bar{o.Date, o.Close})
	}
	results := make(map[string]float64)
	for symbol, bars := range stockBars {
		sort.Slice(bars, func(i, j int) bool { return bars[i].date.Before(bars[j].date) })
		endIdx := -1
		for i := len(bars) - 1; i >= 0; i-- {
			if !bars[i].date.After(targetDate) {
				endIdx = i
				break
			}
		}
		if endIdx < 0 || endIdx < lookback {
			continue
		}
		endPrice := bars[endIdx].close
		startPrice := bars[endIdx-lookback].close
		if startPrice <= 0 || endPrice <= 0 {
			continue
		}
		results[symbol] = (endPrice - startPrice) / startPrice
	}
	return results
}

// ComputeValueFactor computes value factor (EP composite) for all stocks on a given date.
// Value = 1/PE (earnings yield). Uses fundamentals snapshot as of cutoff date.
func (f *FactorComputer) ComputeValueFactor(ctx context.Context, date time.Time) error {
	fundamentals, err := f.store.GetFundamentalsSnapshot(ctx, date)
	if err != nil {
		return fmt.Errorf("load fundamentals for value factor: %w", err)
	}
	rawValues := make(map[string]float64)
	for _, fd := range fundamentals {
		if fd.PE != nil && *fd.PE > 0 {
			rawValues[fd.TsCode] = 1.0 / (*fd.PE)
		}
	}
	if len(rawValues) == 0 {
		f.logger.Warn().Time("date", date).Msg("No value factor values (no valid PE data)")
		return nil
	}
	zScores := ZScore(rawValues)
	percentiles := PercentileRank(rawValues)
	var entries []*domain.FactorCacheEntry
	for symbol, raw := range rawValues {
		entries = append(entries, &domain.FactorCacheEntry{
			Symbol:     symbol,
			TradeDate:  date,
			FactorName: domain.FactorValue,
			RawValue:   raw,
			ZScore:     zScores[symbol],
			Percentile: percentiles[symbol],
		})
	}
	if err := f.store.SaveFactorCacheBatch(ctx, entries); err != nil {
		return fmt.Errorf("save value factor_cache: %w", err)
	}
	f.logger.Info().
		Time("date", date).
		Int("stocks", len(entries)).
		Msg("Value factor computed and cached")
	return nil
}

// ComputeQualityFactor computes quality factor (ROE) for all stocks on a given date.
func (f *FactorComputer) ComputeQualityFactor(ctx context.Context, date time.Time) error {
	fundamentals, err := f.store.GetFundamentalsSnapshot(ctx, date)
	if err != nil {
		return fmt.Errorf("load fundamentals for quality factor: %w", err)
	}
	rawValues := make(map[string]float64)
	for _, fd := range fundamentals {
		if fd.ROE != nil && *fd.ROE > 0 {
			rawValues[fd.TsCode] = *fd.ROE
		}
	}
	if len(rawValues) == 0 {
		f.logger.Warn().Time("date", date).Msg("No quality factor values (no valid ROE data)")
		return nil
	}
	zScores := ZScore(rawValues)
	percentiles := PercentileRank(rawValues)
	var entries []*domain.FactorCacheEntry
	for symbol, raw := range rawValues {
		entries = append(entries, &domain.FactorCacheEntry{
			Symbol:     symbol,
			TradeDate:  date,
			FactorName: domain.FactorQuality,
			RawValue:   raw,
			ZScore:     zScores[symbol],
			Percentile: percentiles[symbol],
		})
	}
	if err := f.store.SaveFactorCacheBatch(ctx, entries); err != nil {
		return fmt.Errorf("save quality factor_cache: %w", err)
	}
	f.logger.Info().
		Time("date", date).
		Int("stocks", len(entries)).
		Msg("Quality factor computed and cached")
	return nil
}

// SkippedFactor records one factor a batch run did not compute, and why.
type SkippedFactor struct {
	Name   string
	Reason error
}

// FactorBatchReport is what one ComputeAllFactors run actually did.
//
// AUD-15: "which factors ran" cannot be inferred from the error alone. An empty
// fundamentals_detail makes all five vertical factors return
// ErrNoFundamentalsDetail; that is a property of the data source, not of the
// cross-sectional factors, so the run as a whole succeeds and the vertical
// factors are reported as skipped instead. Without the report, "no error" would
// be read as "eight factors cached" — the same lie that loadStatementBook's
// empty book used to tell, one level up.
type FactorBatchReport struct {
	Computed []string
	Skipped  []SkippedFactor
}

// LogFields renders the report for a structured log line. Skipped factors are
// named rather than counted: a count of 5 says nothing about which.
func (r FactorBatchReport) LogFields() []any {
	names := make([]string, 0, len(r.Skipped))
	for _, s := range r.Skipped {
		names = append(names, s.Name)
	}
	return []any{
		"computed", r.Computed,
		"skipped", names,
	}
}

// ComputeAllFactors runs all factor computations for a single date: the three
// cross-sectional factors (momentum / value / quality) plus the five 桥 B1
// vertical fundamentals factors (factor_equitydeep.go).
//
// When parallel=true they are computed concurrently using goroutines. The list
// is built once and walked by both branches so that adding a factor cannot make
// the two paths disagree.
//
// A factor that fails with ErrNoFundamentalsDetail is skipped and recorded in
// the report rather than failing the run: an empty fundamentals_detail is one
// source being empty, not a reason to withhold momentum / value / quality.
// Any other error still fails the run at the first offender in list order —
// including in the parallel path, where results are re-ordered by list position
// so the reported cause does not depend on which goroutine happened to finish
// first.
// factorComputation 是一条「当日算哪个因子」的清单项。
type factorComputation struct {
	name string
	run  func() error
}

// registryComputations 按注册表生成当日计算清单——「注册表改库管理」
// 的落点：因子清单不再是硬编码切片，而是库里定义的投影（D3 ③4）。
//
// 分派规则（D3 ③2 计算内核三型）：
//   - code 型 → Go 出口闭包（CodeRef 绑定，原 Compute*Factor 方法）；
//   - leaf/formula 型 → 表达式引擎现算。窗口由定义 Formula 钉死
//     （momentum 的 ts_delay(close, 20)）：改窗口 = 改定义 = 新版本
//     哈希（版本纪律），不存在运行时覆盖；
//   - pending 型 → 不进清单（由 ComputeAllFactors 记入 Skipped）。
func (f *FactorComputer) registryComputations(ctx context.Context, date time.Time) []factorComputation {
	names := f.registry.Names()
	computations := make([]factorComputation, 0, len(names))
	for _, name := range names {
		def, ok := f.registry.Definition(name)
		if !ok {
			continue // Names() 与 Definition() 同源，不可达
		}
		switch def.Kernel {
		case domain.KernelCode:
			kernel, err := f.registry.CodeKernel(f, name)
			if err != nil {
				// 注册表加载期已保证绑定存在；真出现即注册表损坏，
				// 按 fail-closed 让这一天红，而不是静默跳过。
				computations = append(computations, factorComputation{
					name: string(name),
					run:  func() error { return err },
				})
				continue
			}
			computations = append(computations, factorComputation{
				name: string(name),
				run:  func() error { return kernel(ctx, date) },
			})
		case domain.KernelLeaf, domain.KernelFormula:
			computations = append(computations, factorComputation{
				name: string(name),
				run:  func() error { return f.ComputeFormulaFactor(ctx, def, date) },
			})
		case domain.KernelPending:
			// 名册在册、内核待定：不计算。ComputeAllFactors 统一记
			// Skipped（ErrFactorKernelUndefined），与
			// ErrNoFundamentalsDetail 的「跳过但上报」同构。
		}
	}
	return computations
}

func (f *FactorComputer) ComputeAllFactors(ctx context.Context, date time.Time, parallel bool) (FactorBatchReport, error) {
	computations := f.registryComputations(ctx, date)

	type outcome struct {
		index int
		name  string
		err   error
	}

	var outcomes []outcome

	if !parallel {
		for i, c := range computations {
			outcomes = append(outcomes, outcome{index: i, name: c.name, err: c.run()})
		}
	} else {
		// Parallel execution: compute every factor concurrently. The channel is
		// buffered to len(computations) so no goroutine can block after wg.Wait
		// returns.
		var wg sync.WaitGroup
		outCh := make(chan outcome, len(computations))
		for i, c := range computations {
			wg.Add(1)
			go func(idx int, name string, run func() error) {
				defer wg.Done()
				outCh <- outcome{index: idx, name: name, err: run()}
			}(i, c.name, c.run)
		}
		wg.Wait()
		close(outCh)
		for o := range outCh {
			outcomes = append(outcomes, o)
		}
		// Restore list order: which goroutine finished first is not a property
		// of the data, and letting it decide the reported error makes failures
		// irreproducible.
		sort.Slice(outcomes, func(i, j int) bool { return outcomes[i].index < outcomes[j].index })
	}

	report := FactorBatchReport{}
	// pending 型因子（名册在册、内核待定）显式上报，不静默缺席——
	// AUD-15 同款纪律：「没算」必须说出口，count 和名字都要在。
	for _, name := range f.registry.Names() {
		def, ok := f.registry.Definition(name)
		if ok && def.Kernel == domain.KernelPending {
			report.Skipped = append(report.Skipped, SkippedFactor{
				Name:   string(name),
				Reason: fmt.Errorf("%w: %q", ErrFactorKernelUndefined, name),
			})
		}
	}
	for _, o := range outcomes {
		if o.err == nil {
			report.Computed = append(report.Computed, o.name)
			continue
		}
		if errors.Is(o.err, ErrNoFundamentalsDetail) {
			report.Skipped = append(report.Skipped, SkippedFactor{Name: o.name, Reason: o.err})
			f.logger.Warn().
				Time("date", date).
				Str("factor", o.name).
				Err(o.err).
				Msg("Vertical factor skipped: fundamentals_detail is empty")
			continue
		}
		return report, fmt.Errorf("%s: %w", o.name, o.err)
	}
	return report, nil
}

// ComputeFactorsForRange computes all factors for every trading day in [startDate, endDate].
// This is the main entry point for batch pre-computation.
// Returns the number of dates processed and any error.
func (f *FactorComputer) ComputeFactorsForRange(ctx context.Context, startDate, endDate time.Time) (int, error) {
	tradingDays, err := f.store.GetTradingDays(ctx, startDate, endDate)
	if err != nil {
		return 0, fmt.Errorf("get trading days: %w", err)
	}
	if len(tradingDays) == 0 {
		return 0, fmt.Errorf("no trading days in range [%s, %s]", startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	}
	f.logger.Info().
		Time("start", startDate).
		Time("end", endDate).
		Int("trading_days", len(tradingDays)).
		Msg("Starting batch factor computation")

	count := 0
	for _, day := range tradingDays {
		select {
		case <-ctx.Done():
			return count, ctx.Err()
		default:
		}
		report, err := f.ComputeAllFactors(ctx, day, false)
		if err != nil {
			f.logger.Warn().Time("date", day).Err(err).Msg("Skipping date due to error")
			continue
		}
		// AUD-15: dates whose vertical factors were skipped still count as
		// processed — the cross-sectional factors did run — but the skip is
		// logged per date so an empty fundamentals_detail cannot hide behind a
		// batch that "completed".
		if len(report.Skipped) > 0 {
			f.logger.Warn().Time("date", day).Fields(report.LogFields()).
				Msg("Some factors skipped for this date")
		}
		count++
	}
	f.logger.Info().
		Int("dates_processed", count).
		Int("total_dates", len(tradingDays)).
		Msg("Batch factor computation complete")
	return count, nil
}

// LoadFactorCacheIntoMap loads pre-computed factor cache into an in-memory map
// suitable for Engine integration. Structure: factorType -> tradeDate -> symbol -> zScore.
func LoadFactorCacheIntoMap(entries []*domain.FactorCacheEntry) map[domain.FactorType]map[time.Time]map[string]float64 {
	result := make(map[domain.FactorType]map[time.Time]map[string]float64)
	for _, e := range entries {
		if result[e.FactorName] == nil {
			result[e.FactorName] = make(map[time.Time]map[string]float64)
		}
		if result[e.FactorName][e.TradeDate] == nil {
			result[e.FactorName][e.TradeDate] = make(map[string]float64)
		}
		result[e.FactorName][e.TradeDate][e.Symbol] = e.ZScore
	}
	return result
}
