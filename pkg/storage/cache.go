package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ── Factor Cache ──────────────────────────────────────────────────────────────

// SaveFactorCacheBatch saves multiple factor cache entries in a batch.
func (s *PostgresStore) SaveFactorCacheBatch(ctx context.Context, entries []*domain.FactorCacheEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}
	for _, e := range entries {
		batch.Queue(`
			INSERT INTO factor_cache (symbol, trade_date, factor_name, raw_value, z_score, percentile)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (symbol, trade_date, factor_name) DO UPDATE SET
				raw_value = EXCLUDED.raw_value,
				z_score = EXCLUDED.z_score,
				percentile = EXCLUDED.percentile
		`, e.Symbol, e.TradeDate, e.FactorName, e.RawValue, e.ZScore, e.Percentile)
	}

	results := tx.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(entries); i++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch factor_cache insert failed at index %d: %w", i, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.logger.Info().Int("count", len(entries)).Msg("Batch factor_cache saved")
	return nil
}

// GetFactorCache retrieves a single factor cache entry.
func (s *PostgresStore) GetFactorCache(ctx context.Context, symbol string, date time.Time, factor domain.FactorType) (*domain.FactorCacheEntry, error) {
	query := `
		SELECT id, symbol, trade_date, factor_name, raw_value, z_score, percentile
		FROM factor_cache
		WHERE symbol = $1 AND trade_date = $2 AND factor_name = $3
	`
	var e domain.FactorCacheEntry
	err := s.pool.QueryRow(ctx, query, symbol, date, factor).Scan(
		&e.ID, &e.Symbol, &e.TradeDate, &e.FactorName, &e.RawValue, &e.ZScore, &e.Percentile,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get factor cache: %w", err)
	}
	return &e, nil
}

// GetFactorCacheRange retrieves factor cache entries for a factor within a date range.
func (s *PostgresStore) GetFactorCacheRange(ctx context.Context, factor domain.FactorType, startDate, endDate time.Time) ([]*domain.FactorCacheEntry, error) {
	query := `
		SELECT id, symbol, trade_date, factor_name, raw_value, z_score, percentile
		FROM factor_cache
		WHERE factor_name = $1 AND trade_date >= $2 AND trade_date <= $3
		ORDER BY trade_date ASC, symbol ASC
	`
	rows, err := s.pool.Query(ctx, query, factor, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query factor cache range: %w", err)
	}
	defer rows.Close()

	var results []*domain.FactorCacheEntry
	for rows.Next() {
		var e domain.FactorCacheEntry
		if err := rows.Scan(&e.ID, &e.Symbol, &e.TradeDate, &e.FactorName, &e.RawValue, &e.ZScore, &e.Percentile); err != nil {
			return nil, fmt.Errorf("failed to scan factor cache row: %w", err)
		}
		results = append(results, &e)
	}

	return results, rows.Err()
}

// ── Factor Returns ────────────────────────────────────────────────────────────

// SaveFactorReturnBatch saves multiple factor return records in a batch.
func (s *PostgresStore) SaveFactorReturnBatch(ctx context.Context, records []*domain.FactorReturn) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}
	for _, r := range records {
		batch.Queue(`
			INSERT INTO factor_returns (factor_name, trade_date, quintile, avg_return, cumulative_return, top_minus_bot)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (factor_name, trade_date, quintile) DO UPDATE SET
				avg_return = EXCLUDED.avg_return,
				cumulative_return = EXCLUDED.cumulative_return,
				top_minus_bot = EXCLUDED.top_minus_bot
		`, r.FactorName, r.TradeDate, r.Quintile, r.AvgReturn, r.CumulativeReturn, r.TopMinusBot)
	}

	results := tx.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(records); i++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch factor_returns insert failed at index %d: %w", i, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.logger.Info().Int("count", len(records)).Msg("Batch factor_returns saved")
	return nil
}

// GetFactorReturns retrieves factor returns for a factor within a date range.
func (s *PostgresStore) GetFactorReturns(ctx context.Context, factor domain.FactorType, startDate, endDate time.Time) ([]*domain.FactorReturn, error) {
	query := `
		SELECT id, factor_name, trade_date, quintile, avg_return, cumulative_return, top_minus_bot
		FROM factor_returns
		WHERE factor_name = $1 AND trade_date >= $2 AND trade_date <= $3
		ORDER BY trade_date ASC, quintile ASC
	`
	rows, err := s.pool.Query(ctx, query, factor, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query factor_returns: %w", err)
	}
	defer rows.Close()

	var results []*domain.FactorReturn
	for rows.Next() {
		var r domain.FactorReturn
		if err := rows.Scan(&r.ID, &r.FactorName, &r.TradeDate, &r.Quintile, &r.AvgReturn, &r.CumulativeReturn, &r.TopMinusBot); err != nil {
			return nil, fmt.Errorf("failed to scan factor_return row: %w", err)
		}
		results = append(results, &r)
	}

	return results, rows.Err()
}

// ── IC Analysis ───────────────────────────────────────────────────────────────

// SaveICEntryBatch saves multiple IC analysis records in a batch.
func (s *PostgresStore) SaveICEntryBatch(ctx context.Context, records []*domain.ICEntry) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}
	for _, r := range records {
		batch.Queue(`
			INSERT INTO ic_analysis (factor_name, trade_date, ic, p_value, top_ic)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (factor_name, trade_date) DO UPDATE SET
				ic = EXCLUDED.ic,
				p_value = EXCLUDED.p_value,
				top_ic = EXCLUDED.top_ic
		`, r.FactorName, r.TradeDate, r.IC, r.PValue, r.TopIC)
	}

	results := tx.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(records); i++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch ic_analysis insert failed at index %d: %w", i, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.logger.Info().Int("count", len(records)).Msg("Batch ic_analysis saved")
	return nil
}

// GetICEntries retrieves IC entries for a factor within a date range.
func (s *PostgresStore) GetICEntries(ctx context.Context, factor domain.FactorType, startDate, endDate time.Time) ([]*domain.ICEntry, error) {
	query := `
		SELECT id, factor_name, trade_date, ic, p_value, top_ic
		FROM ic_analysis
		WHERE factor_name = $1 AND trade_date >= $2 AND trade_date <= $3
		ORDER BY trade_date ASC
	`
	rows, err := s.pool.Query(ctx, query, factor, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query ic_analysis: %w", err)
	}
	defer rows.Close()

	var results []*domain.ICEntry
	for rows.Next() {
		var r domain.ICEntry
		if err := rows.Scan(&r.ID, &r.FactorName, &r.TradeDate, &r.IC, &r.PValue, &r.TopIC); err != nil {
			return nil, fmt.Errorf("failed to scan ic_entry row: %w", err)
		}
		results = append(results, &r)
	}

	return results, rows.Err()
}

// ── Dividends ─────────────────────────────────────────────────────────────────

// SaveDividendBatch saves multiple dividend records in a batch.
func (s *PostgresStore) SaveDividendBatch(ctx context.Context, records []*domain.Dividend) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}
	for _, d := range records {
		batch.Queue(`
			INSERT INTO dividends (symbol, ann_date, rec_date, pay_date, div_amt, stk_div, stk_ratio, cash_ratio)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (symbol, ann_date) DO UPDATE SET
				rec_date = EXCLUDED.rec_date,
				pay_date = EXCLUDED.pay_date,
				div_amt = EXCLUDED.div_amt,
				stk_div = EXCLUDED.stk_div,
				stk_ratio = EXCLUDED.stk_ratio,
				cash_ratio = EXCLUDED.cash_ratio
		`, d.Symbol, d.AnnDate, d.RecDate, d.PayDate, d.DivAmt, d.StkDiv, d.StkRatio, d.CashRatio)
	}

	results := tx.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(records); i++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch dividend insert failed at index %d: %w", i, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.logger.Info().Int("count", len(records)).Msg("Batch dividends saved")
	return nil
}

func (s *PostgresStore) GetDividendsInRange(ctx context.Context, startDate, endDate time.Time) ([]*domain.Dividend, error) {
	query := `
		SELECT id, symbol, ann_date, rec_date, pay_date, div_amt, stk_div, stk_ratio, cash_ratio
		FROM dividends
		WHERE pay_date >= $1 AND pay_date <= $2
		ORDER BY pay_date ASC
	`
	rows, err := s.pool.Query(ctx, query, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query dividends in range: %w", err)
	}
	defer rows.Close()

	var results []*domain.Dividend
	for rows.Next() {
		var r domain.Dividend
		if err := rows.Scan(&r.ID, &r.Symbol, &r.AnnDate, &r.RecDate, &r.PayDate, &r.DivAmt, &r.StkDiv, &r.StkRatio, &r.CashRatio); err != nil {
			return nil, fmt.Errorf("failed to scan dividend row: %w", err)
		}
		results = append(results, &r)
	}
	return results, rows.Err()
}

func (s *PostgresStore) GetDividendsBySymbol(ctx context.Context, symbol string) ([]*domain.Dividend, error) {
	query := `
		SELECT id, symbol, ann_date, rec_date, pay_date, div_amt, stk_div, stk_ratio, cash_ratio
		FROM dividends
		WHERE symbol = $1
		ORDER BY pay_date ASC
	`
	rows, err := s.pool.Query(ctx, query, symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to query dividends for symbol %s: %w", symbol, err)
	}
	defer rows.Close()

	var results []*domain.Dividend
	for rows.Next() {
		var r domain.Dividend
		if err := rows.Scan(&r.ID, &r.Symbol, &r.AnnDate, &r.RecDate, &r.PayDate, &r.DivAmt, &r.StkDiv, &r.StkRatio, &r.CashRatio); err != nil {
			return nil, fmt.Errorf("failed to scan dividend row: %w", err)
		}
		results = append(results, &r)
	}
	return results, rows.Err()
}

// ── Index Constituents ────────────────────────────────────────────────────────

// SaveIndexConstituentBatch saves multiple index constituent records in a batch.
// Uses ON CONFLICT to update existing entries (symbol, index_code).
func (s *PostgresStore) SaveIndexConstituentBatch(ctx context.Context, records []*domain.IndexConstituent) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}
	for _, c := range records {
		batch.Queue(`
			INSERT INTO index_constituents (index_code, symbol, in_date, out_date, weight)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (symbol, index_code) DO UPDATE SET
				in_date = EXCLUDED.in_date,
				out_date = EXCLUDED.out_date,
				weight = EXCLUDED.weight
		`, c.IndexCode, c.Symbol, c.InDate, c.OutDate, c.Weight)
	}

	results := tx.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(records); i++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch index constituent insert failed at index %d: %w", i, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.logger.Info().Int("count", len(records)).Msg("Batch index constituents saved")
	return nil
}

// GetIndexConstituents returns all current constituents for a given index.
// A constituent is "current" if out_date is NULL or in the future.
func (s *PostgresStore) GetIndexConstituents(ctx context.Context, indexCode string) ([]domain.IndexConstituent, error) {
	query := `
		SELECT id, index_code, symbol, in_date, out_date, weight
		FROM index_constituents
		WHERE index_code = $1
		ORDER BY symbol
	`
	rows, err := s.pool.Query(ctx, query, indexCode)
	if err != nil {
		return nil, fmt.Errorf("failed to query index constituents: %w", err)
	}
	defer rows.Close()

	var results []domain.IndexConstituent
	for rows.Next() {
		var c domain.IndexConstituent
		if err := rows.Scan(&c.ID, &c.IndexCode, &c.Symbol, &c.InDate, &c.OutDate, &c.Weight); err != nil {
			return nil, fmt.Errorf("failed to scan index constituent row: %w", err)
		}
		results = append(results, c)
	}

	return results, rows.Err()
}

func (s *PostgresStore) GetIndexConstituentsByDate(ctx context.Context, indexCode string, date time.Time) ([]string, error) {
	query := `
		SELECT symbol
		FROM index_constituents
		WHERE index_code = $1
		  AND in_date <= $2
		  AND (out_date IS NULL OR out_date > $2)
		ORDER BY symbol
	`
	rows, err := s.pool.Query(ctx, query, indexCode, date)
	if err != nil {
		return nil, fmt.Errorf("failed to query index constituents by date: %w", err)
	}
	defer rows.Close()

	var symbols []string
	for rows.Next() {
		var sym string
		if err := rows.Scan(&sym); err != nil {
			return nil, fmt.Errorf("failed to scan index constituent symbol: %w", err)
		}
		symbols = append(symbols, sym)
	}
	return symbols, rows.Err()
}

// ── Splits ────────────────────────────────────────────────────────────────────

// SaveSplitBatch saves multiple split/rights-issue records in a batch.
func (s *PostgresStore) SaveSplitBatch(ctx context.Context, records []*domain.Split) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}
	for _, r := range records {
		batch.Queue(`
			INSERT INTO splits (symbol, trade_date, ann_date, stk_div_ratio, cash_div_ratio, currency)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (symbol, trade_date) DO UPDATE SET
				ann_date = EXCLUDED.ann_date,
				stk_div_ratio = EXCLUDED.stk_div_ratio,
				cash_div_ratio = EXCLUDED.cash_div_ratio,
				currency = EXCLUDED.currency
		`, r.Symbol, r.TradeDate, r.AnnDate, r.StkDivRatio, r.CashDivRatio, r.Currency)
	}

	results := tx.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(records); i++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch split insert failed at index %d: %w", i, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.logger.Info().Int("count", len(records)).Msg("Batch splits saved")
	return nil
}

// GetSplitsBySymbol retrieves all split records for a given symbol.
func (s *PostgresStore) GetSplitsBySymbol(ctx context.Context, symbol string) ([]*domain.Split, error) {
	query := `
		SELECT id, symbol, trade_date, ann_date, stk_div_ratio, cash_div_ratio, currency
		FROM splits
		WHERE symbol = $1
		ORDER BY trade_date ASC
	`
	rows, err := s.pool.Query(ctx, query, symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to query splits for symbol %s: %w", symbol, err)
	}
	defer rows.Close()

	var results []*domain.Split
	for rows.Next() {
		var r domain.Split
		if err := rows.Scan(&r.ID, &r.Symbol, &r.TradeDate, &r.AnnDate, &r.StkDivRatio, &r.CashDivRatio, &r.Currency); err != nil {
			return nil, fmt.Errorf("failed to scan split row: %w", err)
		}
		results = append(results, &r)
	}

	return results, rows.Err()
}

// GetSplitsInRange retrieves split records within a date range.
func (s *PostgresStore) GetSplitsInRange(ctx context.Context, startDate, endDate time.Time) ([]*domain.Split, error) {
	query := `
		SELECT id, symbol, trade_date, ann_date, stk_div_ratio, cash_div_ratio, currency
		FROM splits
		WHERE trade_date >= $1 AND trade_date <= $2
		ORDER BY trade_date ASC, symbol ASC
	`
	rows, err := s.pool.Query(ctx, query, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query splits in range: %w", err)
	}
	defer rows.Close()

	var results []*domain.Split
	for rows.Next() {
		var r domain.Split
		if err := rows.Scan(&r.ID, &r.Symbol, &r.TradeDate, &r.AnnDate, &r.StkDivRatio, &r.CashDivRatio, &r.Currency); err != nil {
			return nil, fmt.Errorf("failed to scan split row: %w", err)
		}
		results = append(results, &r)
	}

	return results, rows.Err()
}

// ── Walk-Forward Reports ─────────────────────────────────────────────────────

// SaveWalkForwardReport saves a walk-forward validation report to the database.
func (s *PostgresStore) SaveWalkForwardReport(ctx context.Context, report *domain.WalkForwardReport) error {
	windowsJSON, err := json.Marshal(report.Windows)
	if err != nil {
		return fmt.Errorf("failed to marshal windows: %w", err)
	}

	reportDate := time.Now()
	query := `
		INSERT INTO walk_forward_reports (
			strategy_id, universe, report_date,
			avg_test_sharpe, avg_test_return, avg_test_max_dd,
			avg_degradation, pass_rate, overall_pass, windows_json
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err = s.pool.Exec(ctx, query,
		report.StrategyID, report.Universe, reportDate,
		report.AvgTestSharpe, report.AvgTestReturn, report.AvgTestMaxDD,
		report.AvgDegradation, report.PassRate, report.OverallPass, windowsJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to save walk-forward report: %w", err)
	}
	s.logger.Info().
		Str("strategy_id", report.StrategyID).
		Bool("overall_pass", report.OverallPass).
		Float64("avg_test_sharpe", report.AvgTestSharpe).
		Int("windows", len(report.Windows)).
		Msg("Walk-forward report saved")
	return nil
}

// GetWalkForwardReports retrieves walk-forward reports for a strategy.
func (s *PostgresStore) GetWalkForwardReports(ctx context.Context, strategyID string, limit int) ([]*domain.WalkForwardReport, error) {
	if limit <= 0 {
		limit = 10
	}
	query := `
		SELECT strategy_id, universe, report_date,
			avg_test_sharpe, avg_test_return, avg_test_max_dd,
			avg_degradation, pass_rate, overall_pass, windows_json
		FROM walk_forward_reports
		WHERE strategy_id = $1
		ORDER BY report_date DESC
		LIMIT $2
	`
	rows, err := s.pool.Query(ctx, query, strategyID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query walk-forward reports: %w", err)
	}
	defer rows.Close()

	var reports []*domain.WalkForwardReport
	for rows.Next() {
		var r domain.WalkForwardReport
		var windowsJSON []byte
		var reportDate time.Time
		if err := rows.Scan(
			&r.StrategyID, &r.Universe, &reportDate,
			&r.AvgTestSharpe, &r.AvgTestReturn, &r.AvgTestMaxDD,
			&r.AvgDegradation, &r.PassRate, &r.OverallPass, &windowsJSON,
		); err != nil {
			return nil, fmt.Errorf("failed to scan walk-forward report row: %w", err)
		}
		if err := json.Unmarshal(windowsJSON, &r.Windows); err != nil {
			return nil, fmt.Errorf("failed to unmarshal windows JSON: %w", err)
		}
		reports = append(reports, &r)
	}
	return reports, rows.Err()
}

// GetLatestWalkForwardReport retrieves the most recent walk-forward report for a strategy.
func (s *PostgresStore) GetLatestWalkForwardReport(ctx context.Context, strategyID string) (*domain.WalkForwardReport, error) {
	query := `
		SELECT strategy_id, universe, report_date,
			avg_test_sharpe, avg_test_return, avg_test_max_dd,
			avg_degradation, pass_rate, overall_pass, windows_json
		FROM walk_forward_reports
		WHERE strategy_id = $1
		ORDER BY report_date DESC
		LIMIT 1
	`
	var r domain.WalkForwardReport
	var windowsJSON []byte
	var reportDate time.Time
	err := s.pool.QueryRow(ctx, query, strategyID).Scan(
		&r.StrategyID, &r.Universe, &reportDate,
		&r.AvgTestSharpe, &r.AvgTestReturn, &r.AvgTestMaxDD,
		&r.AvgDegradation, &r.PassRate, &r.OverallPass, &windowsJSON,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get latest walk-forward report: %w", err)
	}
	if err := json.Unmarshal(windowsJSON, &r.Windows); err != nil {
		return nil, fmt.Errorf("failed to unmarshal windows JSON: %w", err)
	}
	return &r, nil
}

// ListAllWalkForwardReports returns all walk-forward reports, newest first.
func (s *PostgresStore) ListAllWalkForwardReports(ctx context.Context, limit int) ([]*domain.WalkForwardReport, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT strategy_id, universe, report_date,
			avg_test_sharpe, avg_test_return, avg_test_max_dd,
			avg_degradation, pass_rate, overall_pass, windows_json
		FROM walk_forward_reports
		ORDER BY report_date DESC
		LIMIT $1
	`
	rows, err := s.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list walk-forward reports: %w", err)
	}
	defer rows.Close()

	var reports []*domain.WalkForwardReport
	for rows.Next() {
		var r domain.WalkForwardReport
		var windowsJSON []byte
		var reportDate time.Time
		if err := rows.Scan(
			&r.StrategyID, &r.Universe, &reportDate,
			&r.AvgTestSharpe, &r.AvgTestReturn, &r.AvgTestMaxDD,
			&r.AvgDegradation, &r.PassRate, &r.OverallPass, &windowsJSON,
		); err != nil {
			return nil, fmt.Errorf("failed to scan walk-forward report row: %w", err)
		}
		if err := json.Unmarshal(windowsJSON, &r.Windows); err != nil {
			return nil, fmt.Errorf("failed to unmarshal windows JSON: %w", err)
		}
		reports = append(reports, &r)
	}
	return reports, rows.Err()
}
