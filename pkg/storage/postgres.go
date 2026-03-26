package storage

import (
	"context"
	"fmt"
	"strings"
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

// DB returns the underlying pgxpool.Pool for direct queries.
func (s *PostgresStore) DB() *pgxpool.Pool {
	return s.pool
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
		`CREATE TABLE IF NOT EXISTS ohlcv_daily_qfq (
			symbol VARCHAR(20) NOT NULL,
			trade_date DATE NOT NULL,
			open DOUBLE PRECISION NOT NULL,
			high DOUBLE PRECISION NOT NULL,
			low DOUBLE PRECISION NOT NULL,
			close DOUBLE PRECISION NOT NULL,
			volume DOUBLE PRECISION NOT NULL,
			turnover DOUBLE PRECISION DEFAULT 0,
			trade_days INT DEFAULT 0,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			PRIMARY KEY (symbol, trade_date)
		)`,
		`CREATE TABLE IF NOT EXISTS fundamentals (
			symbol VARCHAR(20) NOT NULL,
			trade_date DATE NOT NULL,
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
			PRIMARY KEY (symbol, trade_date)
		)`,
		`CREATE TABLE IF NOT EXISTS stock_fundamentals (
			id SERIAL PRIMARY KEY,
			ts_code VARCHAR(20) NOT NULL,
			trade_date DATE NOT NULL,
			ann_date DATE,
			end_date DATE,
			pe FLOAT,
			pb FLOAT,
			ps FLOAT,
			roe FLOAT,
			roa FLOAT,
			debt_to_equity FLOAT,
			gross_margin FLOAT,
			net_margin FLOAT,
			revenue FLOAT,
			net_profit FLOAT,
			total_assets FLOAT,
			total_liab FLOAT,
			created_at TIMESTAMP DEFAULT NOW(),
			UNIQUE(ts_code, trade_date)
		)`,
		`CREATE TABLE IF NOT EXISTS trading_calendar (
			trade_date DATE PRIMARY KEY,
			exchange VARCHAR(10) DEFAULT 'SSE',
			is_trading_day BOOLEAN DEFAULT TRUE
		)`,
		// Migration 004: docs/migrations/004_add_dividends_table.sql
		`CREATE TABLE IF NOT EXISTS dividends (
			id SERIAL PRIMARY KEY,
			symbol VARCHAR(20) NOT NULL,
			ann_date DATE NOT NULL,
			rec_date DATE,
			pay_date DATE,
			div_amt DOUBLE PRECISION,
			stk_div DOUBLE PRECISION,
			stk_ratio DOUBLE PRECISION,
			cash_ratio DOUBLE PRECISION,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		// Migration 005: docs/migrations/005_add_index_constituents_table.sql
		`CREATE TABLE IF NOT EXISTS index_constituents (
			id SERIAL PRIMARY KEY,
			index_code VARCHAR(20) NOT NULL,
			symbol VARCHAR(20) NOT NULL,
			in_date DATE,
			out_date DATE,
			weight DOUBLE PRECISION,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		// Migration 006: docs/migrations/006_add_factor_cache_table.sql
		`CREATE TABLE IF NOT EXISTS factor_cache (
			id SERIAL PRIMARY KEY,
			symbol VARCHAR(20) NOT NULL,
			trade_date DATE NOT NULL,
			factor_name VARCHAR(20) NOT NULL,
			raw_value DOUBLE PRECISION,
			z_score DOUBLE PRECISION,
			percentile DOUBLE PRECISION,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		// Migration 007: docs/migrations/007_add_splits_table.sql
		`CREATE TABLE IF NOT EXISTS splits (
			id SERIAL PRIMARY KEY,
			symbol VARCHAR(20) NOT NULL,
			trade_date DATE NOT NULL,
			ann_date DATE,
			stk_div_ratio DOUBLE PRECISION,
			cash_div_ratio DOUBLE PRECISION,
			currency VARCHAR(10) DEFAULT 'CNY',
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
	}

	for _, m := range migrations {
		if _, err := s.pool.Exec(ctx, m); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	// Create TimescaleDB hypertable for ohlcv_daily_qfq
	hypertableSQL := `SELECT create_hypertable('ohlcv_daily_qfq', 'trade_date', if_not_exists => TRUE)`
	if _, err := s.pool.Exec(ctx, hypertableSQL); err != nil {
		s.logger.Warn().Err(err).Msg("Could not create hypertable (TimescaleDB may not be available)")
	} else {
		s.logger.Info().Msg("TimescaleDB hypertable created/verified for ohlcv_daily_qfq")
	}

	// Create indexes
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_ohlcv_symbol ON ohlcv_daily_qfq(symbol)`,
		`CREATE INDEX IF NOT EXISTS idx_ohlcv_trade_date ON ohlcv_daily_qfq(trade_date)`,
		`CREATE INDEX IF NOT EXISTS idx_fundamentals_symbol ON fundamentals(symbol)`,
		`CREATE INDEX IF NOT EXISTS idx_fundamentals_trade_date ON fundamentals(trade_date)`,
		`CREATE INDEX IF NOT EXISTS idx_stock_fundamentals_code ON stock_fundamentals(ts_code)`,
		`CREATE INDEX IF NOT EXISTS idx_stock_fundamentals_date ON stock_fundamentals(trade_date)`,
		`CREATE INDEX IF NOT EXISTS idx_stocks_exchange ON stocks(exchange)`,
		`CREATE INDEX IF NOT EXISTS idx_trading_calendar_exchange ON trading_calendar(exchange)`,
		`CREATE INDEX IF NOT EXISTS idx_trading_calendar_is_trading ON trading_calendar(is_trading_day)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_dividends_symbol_ann ON dividends(symbol, ann_date)`,
		`CREATE INDEX IF NOT EXISTS idx_dividends_pay_date ON dividends(pay_date)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_ic_symbol_index ON index_constituents(symbol, index_code)`,
		`CREATE INDEX IF NOT EXISTS idx_ic_index_code ON index_constituents(index_code)`,
		`CREATE INDEX IF NOT EXISTS idx_ic_in_date ON index_constituents(in_date)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_fc_pk ON factor_cache(symbol, trade_date, factor_name)`,
		`CREATE INDEX IF NOT EXISTS idx_fc_trade_date ON factor_cache(trade_date)`,
		`CREATE INDEX IF NOT EXISTS idx_fc_factor_name ON factor_cache(factor_name)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_splits_symbol_trade ON splits(symbol, trade_date)`,
		`CREATE INDEX IF NOT EXISTS idx_splits_trade_date ON splits(trade_date)`,
		`CREATE INDEX IF NOT EXISTS idx_splits_symbol ON splits(symbol)`,
	}

	for _, idx := range indexes {
		if _, err := s.pool.Exec(ctx, idx); err != nil {
			return fmt.Errorf("index creation failed: %w", err)
		}
	}

	s.logger.Info().Msg("Database migrations completed")
	return nil
}

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
		INSERT INTO fundamentals (symbol, trade_date, pe, pb, ps, roe, roa, debt_to_equity,
			gross_margin, net_margin, revenue, net_profit, total_assets, total_liab)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (symbol, trade_date) DO UPDATE SET
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
			INSERT INTO fundamentals (symbol, trade_date, pe, pb, ps, roe, roa, debt_to_equity,
				gross_margin, net_margin, revenue, net_profit, total_assets, total_liab)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			ON CONFLICT (symbol, trade_date) DO UPDATE SET
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
		SELECT symbol, trade_date, pe, pb, ps, roe, roa, debt_to_equity,
			gross_margin, net_margin, revenue, net_profit, total_assets, total_liab
		FROM fundamentals WHERE symbol = $1 AND trade_date = $2
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

// GetFundamentals retrieves all fundamental records for a symbol on or before the given date.
// Returns an empty slice if no records found.
func (s *PostgresStore) GetFundamentals(ctx context.Context, symbol string, date time.Time) ([]domain.Fundamental, error) {
	query := `
		SELECT symbol, trade_date, pe, pb, ps, roe, roa, debt_to_equity,
			gross_margin, net_margin, revenue, net_profit, total_assets, total_liab
		FROM fundamentals
		WHERE symbol = $1 AND trade_date <= $2
		ORDER BY trade_date DESC
	`
	rows, err := s.pool.Query(ctx, query, symbol, date)
	if err != nil {
		return nil, fmt.Errorf("failed to query fundamentals: %w", err)
	}
	defer rows.Close()

	var records []domain.Fundamental
	for rows.Next() {
		var f domain.Fundamental
		if err := rows.Scan(
			&f.Symbol, &f.Date, &f.PE, &f.PB, &f.PS, &f.ROE, &f.ROA, &f.DebtToEquity,
			&f.GrossMargin, &f.NetMargin, &f.Revenue, &f.NetProfit, &f.TotalAssets, &f.TotalLiab,
		); err != nil {
			return nil, fmt.Errorf("failed to scan fundamental row: %w", err)
		}
		records = append(records, f)
	}
	return records, rows.Err()
}

// SaveFundamentalData saves or updates fundamental data from Tushare financial_data API.
func (s *PostgresStore) SaveFundamentalData(ctx context.Context, f *domain.FundamentalData) error {
	query := `
		INSERT INTO stock_fundamentals (ts_code, trade_date, ann_date, end_date,
			pe, pb, ps, roe, roa, debt_to_equity, gross_margin, net_margin,
			revenue, net_profit, total_assets, total_liab)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		ON CONFLICT (ts_code, trade_date) DO UPDATE SET
			ann_date = EXCLUDED.ann_date,
			end_date = EXCLUDED.end_date,
			pe = EXCLUDED.pe,
			pb = EXCLUDED.pb,
			ps = EXCLUDED.ps,
			roe = EXCLUDED.roe,
			roa = EXCLUDED.roa,
			debt_to_equity = EXCLUDED.debt_to_equity,
			gross_margin = EXCLUDED.gross_margin,
			net_margin = EXCLUDED.net_margin,
			revenue = EXCLUDED.revenue,
			net_profit = EXCLUDED.net_profit,
			total_assets = EXCLUDED.total_assets,
			total_liab = EXCLUDED.total_liab
	`
	_, err := s.pool.Exec(ctx, query,
		f.TsCode, f.TradeDate, f.AnnDate, f.EndDate,
		f.PE, f.PB, f.PS, f.ROE, f.ROA, f.DebtToEquity,
		f.GrossMargin, f.NetMargin, f.Revenue, f.NetProfit,
		f.TotalAssets, f.TotalLiab,
	)
	if err != nil {
		return fmt.Errorf("failed to save fundamental data: %w", err)
	}
	s.logger.Debug().Str("ts_code", f.TsCode).Time("date", f.TradeDate).Msg("FundamentalData saved")
	return nil
}

// SaveFundamentalDataBatch saves multiple fundamental data records in a batch.
func (s *PostgresStore) SaveFundamentalDataBatch(ctx context.Context, records []*domain.FundamentalData) error {
	if len(records) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, f := range records {
		batch.Queue(`
			INSERT INTO stock_fundamentals (ts_code, trade_date, ann_date, end_date,
				pe, pb, ps, roe, roa, debt_to_equity, gross_margin, net_margin,
				revenue, net_profit, total_assets, total_liab)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
			ON CONFLICT (ts_code, trade_date) DO UPDATE SET
				ann_date = EXCLUDED.ann_date,
				end_date = EXCLUDED.end_date,
				pe = EXCLUDED.pe,
				pb = EXCLUDED.pb,
				ps = EXCLUDED.ps,
				roe = EXCLUDED.roe,
				roa = EXCLUDED.roa,
				debt_to_equity = EXCLUDED.debt_to_equity,
				gross_margin = EXCLUDED.gross_margin,
				net_margin = EXCLUDED.net_margin,
				revenue = EXCLUDED.revenue,
				net_profit = EXCLUDED.net_profit,
				total_assets = EXCLUDED.total_assets,
				total_liab = EXCLUDED.total_liab
		`, f.TsCode, f.TradeDate, f.AnnDate, f.EndDate,
			f.PE, f.PB, f.PS, f.ROE, f.ROA, f.DebtToEquity,
			f.GrossMargin, f.NetMargin, f.Revenue, f.NetProfit,
			f.TotalAssets, f.TotalLiab)
	}

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(records); i++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch fundamental data insert failed at index %d: %w", i, err)
		}
	}

	s.logger.Info().Int("count", len(records)).Msg("Batch FundamentalData saved")
	return nil
}

// GetFundamentalDataLatest retrieves the latest fundamental data for a symbol.
func (s *PostgresStore) GetFundamentalDataLatest(ctx context.Context, tsCode string) (*domain.FundamentalData, error) {
	query := `
		SELECT id, ts_code, trade_date, ann_date, end_date,
			pe, pb, ps, roe, roa, debt_to_equity, gross_margin, net_margin,
			revenue, net_profit, total_assets, total_liab, created_at
		FROM stock_fundamentals
		WHERE ts_code = $1
		ORDER BY trade_date DESC
		LIMIT 1
	`
	var f domain.FundamentalData
	err := s.pool.QueryRow(ctx, query, tsCode).Scan(
		&f.ID, &f.TsCode, &f.TradeDate, &f.AnnDate, &f.EndDate,
		&f.PE, &f.PB, &f.PS, &f.ROE, &f.ROA, &f.DebtToEquity,
		&f.GrossMargin, &f.NetMargin, &f.Revenue, &f.NetProfit,
		&f.TotalAssets, &f.TotalLiab, &f.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get latest fundamental data: %w", err)
	}
	return &f, nil
}

// GetFundamentalDataHistory retrieves historical fundamental data for a symbol.
func (s *PostgresStore) GetFundamentalDataHistory(ctx context.Context, tsCode string, startDate, endDate *time.Time) ([]domain.FundamentalData, error) {
	var query string
	var args []interface{}

	if startDate != nil && endDate != nil {
		query = `
			SELECT id, ts_code, trade_date, ann_date, end_date,
				pe, pb, ps, roe, roa, debt_to_equity, gross_margin, net_margin,
				revenue, net_profit, total_assets, total_liab, created_at
			FROM stock_fundamentals
			WHERE ts_code = $1 AND trade_date >= $2 AND trade_date <= $3
			ORDER BY trade_date DESC
		`
		args = []interface{}{tsCode, *startDate, *endDate}
	} else {
		query = `
			SELECT id, ts_code, trade_date, ann_date, end_date,
				pe, pb, ps, roe, roa, debt_to_equity, gross_margin, net_margin,
				revenue, net_profit, total_assets, total_liab, created_at
			FROM stock_fundamentals
			WHERE ts_code = $1
			ORDER BY trade_date DESC
		`
		args = []interface{}{tsCode}
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query fundamental data history: %w", err)
	}
	defer rows.Close()

	var results []domain.FundamentalData
	for rows.Next() {
		var f domain.FundamentalData
		if err := rows.Scan(
			&f.ID, &f.TsCode, &f.TradeDate, &f.AnnDate, &f.EndDate,
			&f.PE, &f.PB, &f.PS, &f.ROE, &f.ROA, &f.DebtToEquity,
			&f.GrossMargin, &f.NetMargin, &f.Revenue, &f.NetProfit,
			&f.TotalAssets, &f.TotalLiab, &f.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan fundamental data row: %w", err)
		}
		results = append(results, f)
	}

	return results, rows.Err()
}

// ScreenFundamentals filters stocks by fundamental criteria.
func (s *PostgresStore) ScreenFundamentals(ctx context.Context, filters domain.ScreenFilters, date *time.Time, limit int) ([]domain.ScreenResult, error) {
	// Build dynamic query
	query := `
		SELECT sf.ts_code, sf.pe, sf.pb, sf.ps, sf.roe, sf.roa, sf.debt_to_equity,
			sf.gross_margin, sf.net_margin, st.market_cap
		FROM stock_fundamentals sf
		LEFT JOIN stocks st ON sf.ts_code = st.symbol
	`
	var conditions []string
	var args []interface{}
	argIdx := 1

	if date != nil {
		conditions = append(conditions, fmt.Sprintf("sf.trade_date = $%d", argIdx))
		args = append(args, *date)
		argIdx++
	} else {
		// Use latest data per ts_code
		query = fmt.Sprintf(`
			SELECT sf.ts_code, sf.pe, sf.pb, sf.ps, sf.roe, sf.roa, sf.debt_to_equity,
				sf.gross_margin, sf.net_margin, st.market_cap
			FROM (
				SELECT ts_code, pe, pb, ps, roe, roa, debt_to_equity,
					gross_margin, net_margin,
					ROW_NUMBER() OVER (PARTITION BY ts_code ORDER BY trade_date DESC) as rn
				FROM stock_fundamentals
			) sf
			LEFT JOIN stocks st ON sf.ts_code = st.symbol
			WHERE sf.rn = 1
		`)
	}

	if filters.PE_min != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.pe IS NULL OR sf.pe >= $%d)", argIdx))
		args = append(args, *filters.PE_min)
		argIdx++
	}
	if filters.PE_max != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.pe IS NULL OR sf.pe <= $%d)", argIdx))
		args = append(args, *filters.PE_max)
		argIdx++
	}
	if filters.PB_min != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.pb IS NULL OR sf.pb >= $%d)", argIdx))
		args = append(args, *filters.PB_min)
		argIdx++
	}
	if filters.PB_max != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.pb IS NULL OR sf.pb <= $%d)", argIdx))
		args = append(args, *filters.PB_max)
		argIdx++
	}
	if filters.PS_min != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.ps IS NULL OR sf.ps >= $%d)", argIdx))
		args = append(args, *filters.PS_min)
		argIdx++
	}
	if filters.PS_max != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.ps IS NULL OR sf.ps <= $%d)", argIdx))
		args = append(args, *filters.PS_max)
		argIdx++
	}
	if filters.ROE_min != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.roe IS NULL OR sf.roe >= $%d)", argIdx))
		args = append(args, *filters.ROE_min)
		argIdx++
	}
	if filters.ROA_min != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.roa IS NULL OR sf.roa >= $%d)", argIdx))
		args = append(args, *filters.ROA_min)
		argIdx++
	}
	if filters.DebtToEquity_max != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.debt_to_equity IS NULL OR sf.debt_to_equity <= $%d)", argIdx))
		args = append(args, *filters.DebtToEquity_max)
		argIdx++
	}
	if filters.GrossMargin_min != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.gross_margin IS NULL OR sf.gross_margin >= $%d)", argIdx))
		args = append(args, *filters.GrossMargin_min)
		argIdx++
	}
	if filters.NetMargin_min != nil {
		conditions = append(conditions, fmt.Sprintf("(sf.net_margin IS NULL OR sf.net_margin >= $%d)", argIdx))
		args = append(args, *filters.NetMargin_min)
		argIdx++
	}
	if filters.MarketCap_min != nil {
		conditions = append(conditions, fmt.Sprintf("(st.market_cap IS NULL OR st.market_cap >= $%d)", argIdx))
		args = append(args, *filters.MarketCap_min)
		argIdx++
	}

	if len(conditions) > 0 {
		if strings.Contains(query, "WHERE sf.rn = 1") {
			query += " AND " + strings.Join(conditions, " AND ")
		} else {
			query += " WHERE " + strings.Join(conditions, " AND ")
		}
	}

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to screen fundamentals: %w", err)
	}
	defer rows.Close()

	var results []domain.ScreenResult
	for rows.Next() {
		var r domain.ScreenResult
		if err := rows.Scan(
			&r.TsCode, &r.PE, &r.PB, &r.PS, &r.ROE, &r.ROA,
			&r.DebtToEquity, &r.GrossMargin, &r.NetMargin, &r.MarketCap,
		); err != nil {
			return nil, fmt.Errorf("failed to scan screen result row: %w", err)
		}
		results = append(results, r)
	}

	return results, rows.Err()
}

// Ping checks the database connection.
func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
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

// TradingCalendarEntry represents a trading calendar entry.
type TradingCalendarEntry struct {
	TradeDate      time.Time `json:"trade_date"`
	Exchange       string    `json:"exchange"`
	IsTradingDay   bool      `json:"is_trading_day"`
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

// SaveDividendBatch saves multiple dividend records in a batch.
func (s *PostgresStore) SaveDividendBatch(ctx context.Context, records []*domain.Dividend) error {
	if len(records) == 0 {
		return nil
	}

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

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(records); i++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch dividend insert failed at index %d: %w", i, err)
		}
	}

	s.logger.Info().Int("count", len(records)).Msg("Batch dividends saved")
	return nil
}

// SaveIndexConstituentBatch saves multiple index constituent records in a batch.
// Uses ON CONFLICT to update existing entries (symbol, index_code).
func (s *PostgresStore) SaveIndexConstituentBatch(ctx context.Context, records []*domain.IndexConstituent) error {
	if len(records) == 0 {
		return nil
	}

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

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(records); i++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch index constituent insert failed at index %d: %w", i, err)
		}
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

// SaveFactorCacheBatch saves multiple factor cache entries in a batch.
func (s *PostgresStore) SaveFactorCacheBatch(ctx context.Context, entries []*domain.FactorCacheEntry) error {
	if len(entries) == 0 {
		return nil
	}

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

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(entries); i++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch factor_cache insert failed at index %d: %w", i, err)
		}
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

// SaveSplitBatch saves multiple split/rights-issue records in a batch.
func (s *PostgresStore) SaveSplitBatch(ctx context.Context, records []*domain.Split) error {
	if len(records) == 0 {
		return nil
	}

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

	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(records); i++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch split insert failed at index %d: %w", i, err)
		}
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
