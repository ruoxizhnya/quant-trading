package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
	"github.com/rs/zerolog"
)

// PostgresStore handles PostgreSQL/TimescaleDB operations.
type PostgresStore struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger
}

// NewPostgresStore creates a new PostgresStore with the given connection string.
func NewPostgresStore(ctx context.Context, connString string) (*PostgresStore, error) {
	logger := logging.WithContext(map[string]any{"component": "postgres_store"})

	poolConfig, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	poolConfig.MaxConns = 20
	poolConfig.MinConns = 5
	poolConfig.MaxConnLifetime = time.Hour
	poolConfig.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	store := &PostgresStore{pool: pool, logger: logger}
	if err := store.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	logger.Info().Msg("PostgreSQL/TimescaleDB connection established")
	return store, nil
}

// Close closes the database connection pool.
func (s *PostgresStore) Close() {
	s.pool.Close()
	s.logger.Info().Msg("PostgreSQL connection pool closed")
}

// migrate creates tables and hypertables.
func (s *PostgresStore) migrate(ctx context.Context) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS stocks (
			symbol VARCHAR(20) PRIMARY KEY,
			name VARCHAR(200) NOT NULL,
			exchange VARCHAR(20) NOT NULL,
			industry VARCHAR(100),
			market_cap DOUBLE PRECISION,
			list_date DATE,
			status VARCHAR(20) DEFAULT 'active',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS ohlcv_daily (
			symbol VARCHAR(20) NOT NULL,
			date DATE NOT NULL,
			open DOUBLE PRECISION NOT NULL,
			high DOUBLE PRECISION NOT NULL,
			low DOUBLE PRECISION NOT NULL,
			close DOUBLE PRECISION NOT NULL,
			volume DOUBLE PRECISION NOT NULL,
			turnover DOUBLE PRECISION DEFAULT 0,
			trade_days INT DEFAULT 0,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			PRIMARY KEY (symbol, date)
		)`,
		`CREATE TABLE IF NOT EXISTS fundamentals (
			symbol VARCHAR(20) NOT NULL,
			date DATE NOT NULL,
			pe DOUBLE PRECISION,
			pb DOUBLE PRECISION,
			ps DOUBLE PRECISION,
			roe DOUBLE PRECISION,
			roa DOUBLE PRECISION,
			debt_to_equity DOUBLE PRECISION,
			gross_margin DOUBLE PRECISION,
			net_margin DOUBLE PRECISION,
			revenue DOUBLE PRECISION,
			net_profit DOUBLE PRECISION,
			total_assets DOUBLE PRECISION,
			total_liab DOUBLE PRECISION,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			PRIMARY KEY (symbol, date)
		)`,
	}

	for _, m := range migrations {
		if _, err := s.pool.Exec(ctx, m); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	// Create TimescaleDB hypertable for ohlcv_daily
	hypertableSQL := `SELECT create_hypertable('ohlcv_daily', 'date', if_not_exists => TRUE)`
	if _, err := s.pool.Exec(ctx, hypertableSQL); err != nil {
		s.logger.Warn().Err(err).Msg("Could not create hypertable (TimescaleDB may not be available)")
	} else {
		s.logger.Info().Msg("TimescaleDB hypertable created/verified for ohlcv_daily")
	}

	// Create indexes
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_ohlcv_symbol ON ohlcv_daily(symbol)`,
		`CREATE INDEX IF NOT EXISTS idx_ohlcv_date ON ohlcv_daily(date)`,
		`CREATE INDEX IF NOT EXISTS idx_fundamentals_symbol ON fundamentals(symbol)`,
		`CREATE INDEX IF NOT EXISTS idx_fundamentals_date ON fundamentals(date)`,
		`CREATE INDEX IF NOT EXISTS idx_stocks_exchange ON stocks(exchange)`,
	}

	for _, idx := range indexes {
		if _, err := s.pool.Exec(ctx, idx); err != nil {
			return fmt.Errorf("index creation failed: %w", err)
		}
	}

	s.logger.Info().Msg("Database migrations completed")
	return nil
}

// SaveOHLCV saves or updates OHLCV data.
func (s *PostgresStore) SaveOHLCV(ctx context.Context, ohlcv *domain.OHLCV) error {
	query := `
		INSERT INTO ohlcv_daily (symbol, date, open, high, low, close, volume, turnover, trade_days)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (symbol, date) DO UPDATE SET
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

	batch := &pgx.Batch{}
	for _, o := range records {
		batch.Queue(`
			INSERT INTO ohlcv_daily (symbol, date, open, high, low, close, volume, turnover, trade_days)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (symbol, date) DO UPDATE SET
				open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low,
				close = EXCLUDED.close, volume = EXCLUDED.volume,
				turnover = EXCLUDED.turnover, trade_days = EXCLUDED.trade_days
		`, o.Symbol, o.Date, o.Open, o.High, o.Low, o.Close, o.Volume, o.Turnover, o.TradeDays)
	}

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(records); i++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch insert failed at index %d: %w", i, err)
		}
	}

	s.logger.Info().Int("count", len(records)).Msg("Batch OHLCV saved")
	return nil
}

// GetOHLCV retrieves OHLCV data for a symbol within a date range.
func (s *PostgresStore) GetOHLCV(ctx context.Context, symbol string, startDate, endDate time.Time) ([]domain.OHLCV, error) {
	query := `
		SELECT symbol, date, open, high, low, close, volume, turnover, trade_days
		FROM ohlcv_daily
		WHERE symbol = $1 AND date >= $2 AND date <= $3
		ORDER BY date ASC
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

// SaveFundamental saves or updates fundamental data.
func (s *PostgresStore) SaveFundamental(ctx context.Context, f *domain.Fundamental) error {
	query := `
		INSERT INTO fundamentals (symbol, date, pe, pb, ps, roe, roa, debt_to_equity,
			gross_margin, net_margin, revenue, net_profit, total_assets, total_liab)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (symbol, date) DO UPDATE SET
			pe = EXCLUDED.pe, pb = EXCLUDED.pb, ps = EXCLUDED.ps,
			roe = EXCLUDED.roe, roa = EXCLUDED.roa, debt_to_equity = EXCLUDED.debt_to_equity,
			gross_margin = EXCLUDED.gross_margin, net_margin = EXCLUDED.net_margin,
			revenue = EXCLUDED.revenue, net_profit = EXCLUDED.net_profit,
			total_assets = EXCLUDED.total_assets, total_liab = EXCLUDED.total_liab
	`
	_, err := s.pool.Exec(ctx, query,
		f.Symbol, f.Date, f.PE, f.PB, f.PS, f.ROE, f.ROA, f.DebtToEquity,
		f.GrossMargin, f.NetMargin, f.Revenue, f.NetProfit, f.TotalAssets, f.TotalLiab,
	)
	if err != nil {
		return fmt.Errorf("failed to save fundamental: %w", err)
	}
	s.logger.Debug().Str("symbol", f.Symbol).Time("date", f.Date).Msg("Fundamental saved")
	return nil
}

// SaveFundamentalBatch saves multiple fundamental records in a batch.
func (s *PostgresStore) SaveFundamentalBatch(ctx context.Context, records []*domain.Fundamental) error {
	if len(records) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, f := range records {
		batch.Queue(`
			INSERT INTO fundamentals (symbol, date, pe, pb, ps, roe, roa, debt_to_equity,
				gross_margin, net_margin, revenue, net_profit, total_assets, total_liab)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			ON CONFLICT (symbol, date) DO UPDATE SET
				pe = EXCLUDED.pe, pb = EXCLUDED.pb, ps = EXCLUDED.ps,
				roe = EXCLUDED.roe, roa = EXCLUDED.roa, debt_to_equity = EXCLUDED.debt_to_equity
		`, f.Symbol, f.Date, f.PE, f.PB, f.PS, f.ROE, f.ROA, f.DebtToEquity,
			f.GrossMargin, f.NetMargin, f.Revenue, f.NetProfit, f.TotalAssets, f.TotalLiab)
	}

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(records); i++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch fundamental insert failed at index %d: %w", i, err)
		}
	}

	s.logger.Info().Int("count", len(records)).Msg("Batch fundamentals saved")
	return nil
}

// GetFundamental retrieves fundamental data for a symbol on a specific date.
func (s *PostgresStore) GetFundamental(ctx context.Context, symbol string, date time.Time) (*domain.Fundamental, error) {
	query := `
		SELECT symbol, date, pe, pb, ps, roe, roa, debt_to_equity,
			gross_margin, net_margin, revenue, net_profit, total_assets, total_liab
		FROM fundamentals WHERE symbol = $1 AND date = $2
	`
	var f domain.Fundamental
	err := s.pool.QueryRow(ctx, query, symbol, date).Scan(
		&f.Symbol, &f.Date, &f.PE, &f.PB, &f.PS, &f.ROE, &f.ROA, &f.DebtToEquity,
		&f.GrossMargin, &f.NetMargin, &f.Revenue, &f.NetProfit, &f.TotalAssets, &f.TotalLiab,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get fundamental: %w", err)
	}
	return &f, nil
}

// Ping checks the database connection.
func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}
