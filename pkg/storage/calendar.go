package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// TradingCalendarEntry represents a trading calendar entry.
type TradingCalendarEntry struct {
	TradeDate    time.Time `json:"trade_date"`
	Exchange     string    `json:"exchange"`
	IsTradingDay bool      `json:"is_trading_day"`
}

// SaveTradingCalendarEntry saves or updates a trading calendar entry.
func (s *PostgresStore) SaveTradingCalendarEntry(ctx context.Context, entry *TradingCalendarEntry) error {
	query := `
		INSERT INTO trading_calendar (trade_date, exchange, is_trading_day)
		VALUES ($1, $2, $3)
		ON CONFLICT (trade_date) DO UPDATE SET
			exchange = EXCLUDED.exchange,
			is_trading_day = EXCLUDED.is_trading_day
	`
	_, err := s.pool.Exec(ctx, query, entry.TradeDate, entry.Exchange, entry.IsTradingDay)
	if err != nil {
		return fmt.Errorf("failed to save trading calendar entry: %w", err)
	}
	return nil
}

// SaveTradingCalendarBatch saves multiple trading calendar entries in a batch.
func (s *PostgresStore) SaveTradingCalendarBatch(ctx context.Context, entries []*TradingCalendarEntry) error {
	if len(entries) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, e := range entries {
		batch.Queue(`
			INSERT INTO trading_calendar (trade_date, exchange, is_trading_day)
			VALUES ($1, $2, $3)
			ON CONFLICT (trade_date) DO UPDATE SET
				exchange = EXCLUDED.exchange,
				is_trading_day = EXCLUDED.is_trading_day
		`, e.TradeDate, e.Exchange, e.IsTradingDay)
	}

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(entries); i++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch trading calendar insert failed at index %d: %w", i, err)
		}
	}

	s.logger.Info().Int("count", len(entries)).Msg("Batch trading calendar saved")
	return nil
}

// GetTradingCalendar returns trading calendar entries within a date range.
func (s *PostgresStore) GetTradingCalendar(ctx context.Context, startDate, endDate time.Time) ([]TradingCalendarEntry, error) {
	query := `
		SELECT trade_date, exchange, is_trading_day
		FROM trading_calendar
		WHERE trade_date >= $1 AND trade_date <= $2
		ORDER BY trade_date ASC
	`
	rows, err := s.pool.Query(ctx, query, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query trading calendar: %w", err)
	}
	defer rows.Close()

	var results []TradingCalendarEntry
	for rows.Next() {
		var e TradingCalendarEntry
		if err := rows.Scan(&e.TradeDate, &e.Exchange, &e.IsTradingDay); err != nil {
			return nil, fmt.Errorf("failed to scan trading calendar row: %w", err)
		}
		results = append(results, e)
	}

	return results, rows.Err()
}

// GetTradingDates returns only trading dates (is_trading_day=true) within a date range.
func (s *PostgresStore) GetTradingDates(ctx context.Context, startDate, endDate time.Time) ([]time.Time, error) {
	query := `
		SELECT trade_date FROM trading_calendar
		WHERE trade_date >= $1 AND trade_date <= $2 AND is_trading_day = TRUE
		ORDER BY trade_date ASC
	`
	rows, err := s.pool.Query(ctx, query, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query trading dates: %w", err)
	}
	defer rows.Close()

	var days []time.Time
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return nil, fmt.Errorf("failed to scan trading date: %w", err)
		}
		days = append(days, d)
	}
	return days, rows.Err()
}

// IsTradingDay checks if a given date is a trading day.
func (s *PostgresStore) IsTradingDay(ctx context.Context, date time.Time) (bool, error) {
	query := `SELECT is_trading_day FROM trading_calendar WHERE trade_date = $1`
	var isTrading bool
	err := s.pool.QueryRow(ctx, query, date).Scan(&isTrading)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return false, nil
		}
		return false, fmt.Errorf("failed to check trading day: %w", err)
	}
	return isTrading, nil
}
