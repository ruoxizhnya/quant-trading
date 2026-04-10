package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// SaveStock saves or updates a stock record.
func (s *PostgresStore) SaveStock(ctx context.Context, stock *domain.Stock) error {
	query := `
		INSERT INTO stocks (symbol, name, exchange, industry, market_cap, list_date, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (symbol) DO UPDATE SET
			name = EXCLUDED.name,
			exchange = EXCLUDED.exchange,
			industry = EXCLUDED.industry,
			market_cap = EXCLUDED.market_cap,
			list_date = EXCLUDED.list_date,
			status = EXCLUDED.status,
			updated_at = NOW()
	`
	_, err := s.pool.Exec(ctx, query,
		stock.Symbol, stock.Name, stock.Exchange, stock.Industry,
		stock.MarketCap, stock.ListDate, stock.Status,
	)
	if err != nil {
		return fmt.Errorf("failed to save stock: %w", err)
	}
	s.logger.Debug().Str("symbol", stock.Symbol).Msg("Stock saved")
	return nil
}

// SaveStockBatch saves multiple stocks in a batch.
func (s *PostgresStore) SaveStockBatch(ctx context.Context, stocks []domain.Stock) error {
	if len(stocks) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, st := range stocks {
		batch.Queue(`
			INSERT INTO stocks (symbol, name, exchange, industry, market_cap, list_date, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (symbol) DO UPDATE SET
				name = EXCLUDED.name, exchange = EXCLUDED.exchange,
				industry = EXCLUDED.industry, market_cap = EXCLUDED.market_cap,
				list_date = EXCLUDED.list_date, status = EXCLUDED.status,
				updated_at = NOW()
		`, st.Symbol, st.Name, st.Exchange, st.Industry, st.MarketCap, st.ListDate, st.Status)
	}

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(stocks); i++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch stock insert failed at index %d: %w", i, err)
		}
	}

	s.logger.Info().Int("count", len(stocks)).Msg("Batch stocks saved")
	return nil
}

// GetStocks retrieves stocks, optionally filtered by exchange.
func (s *PostgresStore) GetStocks(ctx context.Context, exchange string) ([]domain.Stock, error) {
	var query string
	var args []interface{}

	if exchange != "" {
		query = `
			SELECT symbol, name, exchange, industry, market_cap, list_date, status
			FROM stocks WHERE exchange = $1 ORDER BY symbol
		`
		args = []interface{}{exchange}
	} else {
		query = `
			SELECT symbol, name, exchange, industry, market_cap, list_date, status
			FROM stocks ORDER BY symbol
		`
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query stocks: %w", err)
	}
	defer rows.Close()

	var results []domain.Stock
	for rows.Next() {
		var st domain.Stock
		if err := rows.Scan(&st.Symbol, &st.Name, &st.Exchange, &st.Industry, &st.MarketCap, &st.ListDate, &st.Status); err != nil {
			return nil, fmt.Errorf("failed to scan stock row: %w", err)
		}
		results = append(results, st)
	}

	return results, rows.Err()
}

// GetStock retrieves a single stock by symbol.
func (s *PostgresStore) GetStock(ctx context.Context, symbol string) (*domain.Stock, error) {
	query := `
		SELECT symbol, name, exchange, industry, market_cap, list_date, status
		FROM stocks WHERE symbol = $1
	`
	var st domain.Stock
	err := s.pool.QueryRow(ctx, query, symbol).Scan(
		&st.Symbol, &st.Name, &st.Exchange, &st.Industry,
		&st.MarketCap, &st.ListDate, &st.Status,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get stock: %w", err)
	}
	return &st, nil
}

// GetAllStocks returns all stocks from the database.
func (s *PostgresStore) GetAllStocks(ctx context.Context) ([]domain.Stock, error) {
	query := `
		SELECT symbol, name, exchange, industry, market_cap, list_date, status
		FROM stocks ORDER BY symbol
	`
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query all stocks: %w", err)
	}
	defer rows.Close()

	var results []domain.Stock
	for rows.Next() {
		var st domain.Stock
		if err := rows.Scan(&st.Symbol, &st.Name, &st.Exchange, &st.Industry, &st.MarketCap, &st.ListDate, &st.Status); err != nil {
			return nil, fmt.Errorf("failed to scan stock row: %w", err)
		}
		results = append(results, st)
	}
	return results, rows.Err()
}
