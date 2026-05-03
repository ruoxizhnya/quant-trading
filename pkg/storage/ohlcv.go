package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// SaveOHLCV saves or updates OHLCV data to ohlcv_daily_qfq.
func (s *PostgresStore) SaveOHLCV(ctx context.Context, ohlcv *domain.OHLCV) error {
	query := `
		INSERT INTO ohlcv_daily_qfq (symbol, trade_date, open, high, low, close, volume, turnover, trade_days)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (symbol, trade_date) DO UPDATE SET
			open = EXCLUDED.open,
			high = EXCLUDED.high,
			low = EXCLUDED.low,
			close = EXCLUDED.close,
			volume = EXCLUDED.volume,
			turnover = EXCLUDED.turnover,
			trade_days = EXCLUDED.trade_days,
			created_at = NOW()
	`
	_, err := s.pool.Exec(ctx, query,
		ohlcv.Symbol, ohlcv.Date, ohlcv.Open, ohlcv.High, ohlcv.Low,
		ohlcv.Close, ohlcv.Volume, ohlcv.Turnover, ohlcv.TradeDays,
	)
	if err != nil {
		return fmt.Errorf("failed to save OHLCV: %w", err)
	}
	s.logger.Debug().Str("symbol", ohlcv.Symbol).Time("date", ohlcv.Date).Msg("OHLCV saved")
	return nil
}

// SaveOHLCVBatch saves multiple OHLCV records in a batch.
func (s *PostgresStore) SaveOHLCVBatch(ctx context.Context, records []*domain.OHLCV) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}
	for _, o := range records {
		batch.Queue(`
			INSERT INTO ohlcv_daily_qfq (symbol, trade_date, open, high, low, close, volume, turnover, trade_days)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (symbol, trade_date) DO UPDATE SET
				open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low,
				close = EXCLUDED.close, volume = EXCLUDED.volume,
				turnover = EXCLUDED.turnover, trade_days = EXCLUDED.trade_days
		`, o.Symbol, o.Date, o.Open, o.High, o.Low, o.Close, o.Volume, o.Turnover, o.TradeDays)
	}

	results := tx.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(records); i++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch insert failed at index %d: %w", i, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.logger.Info().Int("count", len(records)).Msg("Batch OHLCV saved")
	return nil
}

// GetOHLCV retrieves OHLCV data for a symbol within a date range.
func (s *PostgresStore) GetOHLCV(ctx context.Context, symbol string, startDate, endDate time.Time) ([]domain.OHLCV, error) {
	query := `
		SELECT symbol, trade_date, open, high, low, close, volume, turnover, trade_days
		FROM ohlcv_daily_qfq
		WHERE symbol = $1 AND trade_date >= $2 AND trade_date <= $3
		ORDER BY trade_date ASC
	`
	rows, err := s.pool.Query(ctx, query, symbol, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query OHLCV: %w", err)
	}
	defer rows.Close()

	var results []domain.OHLCV
	for rows.Next() {
		var o domain.OHLCV
		if err := rows.Scan(&o.Symbol, &o.Date, &o.Open, &o.High, &o.Low, &o.Close, &o.Volume, &o.Turnover, &o.TradeDays); err != nil {
			return nil, fmt.Errorf("failed to scan OHLCV row: %w", err)
		}
		results = append(results, o)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return results, nil
}

// GetTradingDays returns distinct trading days within a date range.
func (s *PostgresStore) GetTradingDays(ctx context.Context, startDate, endDate time.Time) ([]time.Time, error) {
	query := `
		SELECT DISTINCT trade_date FROM ohlcv_daily_qfq
		WHERE trade_date >= $1 AND trade_date <= $2
		ORDER BY trade_date ASC
	`
	rows, err := s.pool.Query(ctx, query, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query trading days: %w", err)
	}
	defer rows.Close()

	var days []time.Time
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return nil, fmt.Errorf("failed to scan trading day: %w", err)
		}
		days = append(days, d)
	}
	return days, rows.Err()
}

// GetOHLCVForDateRange returns OHLCV data for ALL stocks within a date range.
// Used by FactorComputer to compute cross-sectional factors (momentum).
func (s *PostgresStore) GetOHLCVForDateRange(ctx context.Context, startDate, endDate time.Time) ([]domain.OHLCV, error) {
	query := `
		SELECT symbol, trade_date, open, high, low, close, volume, turnover, trade_days
		FROM ohlcv_daily_qfq
		WHERE trade_date >= $1 AND trade_date <= $2
		ORDER BY symbol ASC, trade_date ASC
	`
	rows, err := s.pool.Query(ctx, query, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query bulk OHLCV: %w", err)
	}
	defer rows.Close()

	var results []domain.OHLCV
	for rows.Next() {
		var o domain.OHLCV
		if err := rows.Scan(&o.Symbol, &o.Date, &o.Open, &o.High, &o.Low, &o.Close, &o.Volume, &o.Turnover, &o.TradeDays); err != nil {
			return nil, fmt.Errorf("failed to scan OHLCV row: %w", err)
		}
		results = append(results, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.logger.Debug().Time("start", startDate).Time("end", endDate).Int("count", len(results)).Msg("Bulk OHLCV loaded")
	return results, nil
}

// HasOHLCVData checks whether we have OHLCV data for a given symbol.
func (s *PostgresStore) HasOHLCVData(ctx context.Context, symbol string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM ohlcv_daily_qfq WHERE symbol = $1 LIMIT 1)`
	var exists bool
	if err := s.pool.QueryRow(ctx, query, symbol).Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to check OHLCV data: %w", err)
	}
	return exists, nil
}

// GetLatestOHLCVDate returns the most recent OHLCV trade date for a symbol.
func (s *PostgresStore) GetLatestOHLCVDate(ctx context.Context, symbol string) (time.Time, error) {
	query := `SELECT MAX(trade_date) FROM ohlcv_daily_qfq WHERE symbol = $1`
	var t *time.Time
	if err := s.pool.QueryRow(ctx, query, symbol).Scan(&t); err != nil {
		return time.Time{}, fmt.Errorf("failed to get latest OHLCV date: %w", err)
	}
	if t == nil {
		return time.Time{}, nil
	}
	return *t, nil
}
