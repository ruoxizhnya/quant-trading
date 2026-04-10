package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
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

	poolConfig.MaxConns = PostgresMaxConns
	poolConfig.MinConns = PostgresMinConns
	poolConfig.MaxConnLifetime = PostgresConnMaxLifetime
	poolConfig.MaxConnIdleTime = PostgresConnMaxIdleTime

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

// Ping checks the database connection.
func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
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
		// Migration 009: factor_returns table
		`CREATE TABLE IF NOT EXISTS factor_returns (
			id SERIAL PRIMARY KEY,
			factor_name VARCHAR(20) NOT NULL,
			trade_date DATE NOT NULL,
			quintile INTEGER NOT NULL CHECK (quintile BETWEEN 1 AND 5),
			avg_return DOUBLE PRECISION,
			cumulative_return DOUBLE PRECISION,
			top_minus_bot DOUBLE PRECISION,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		// Migration 010: ic_analysis table
		`CREATE TABLE IF NOT EXISTS ic_analysis (
			id SERIAL PRIMARY KEY,
			factor_name VARCHAR(20) NOT NULL,
			trade_date DATE NOT NULL,
			ic DOUBLE PRECISION,
			p_value DOUBLE PRECISION,
			top_ic DOUBLE PRECISION,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		// Migration 010: walk_forward_reports table
		`CREATE TABLE IF NOT EXISTS walk_forward_reports (
			id SERIAL PRIMARY KEY,
			strategy_id VARCHAR(50) NOT NULL,
			universe VARCHAR(100),
			report_date DATE NOT NULL,
			avg_test_sharpe DOUBLE PRECISION,
			avg_test_return DOUBLE PRECISION,
			avg_test_max_dd DOUBLE PRECISION,
			avg_degradation DOUBLE PRECISION,
			pass_rate DOUBLE PRECISION,
			overall_pass BOOLEAN,
			windows_json JSONB NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		// Migration 011: strategies table (Sprint 6.2)
		`CREATE TABLE IF NOT EXISTS strategies (
			id SERIAL PRIMARY KEY,
			strategy_id VARCHAR(50) UNIQUE NOT NULL,
			name VARCHAR(100) NOT NULL,
			description TEXT,
			strategy_type VARCHAR(30) NOT NULL,
			params JSONB NOT NULL DEFAULT '{}',
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		// Migration 009: backtest_jobs table (async job queue)
		`CREATE TABLE IF NOT EXISTS backtest_jobs (
			id VARCHAR(64) PRIMARY KEY,
			strategy_id VARCHAR(50) NOT NULL,
			params JSONB NOT NULL DEFAULT '{}',
			universe VARCHAR(100) NOT NULL,
			start_date DATE NOT NULL,
			end_date DATE NOT NULL,
			status VARCHAR(20) NOT NULL DEFAULT 'pending',
			result JSONB,
			error_message TEXT,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			started_at TIMESTAMPTZ,
			completed_at TIMESTAMPTZ
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
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_fr_pk ON factor_returns(factor_name, trade_date, quintile)`,
		`CREATE INDEX IF NOT EXISTS idx_fr_trade_date ON factor_returns(trade_date)`,
		`CREATE INDEX IF NOT EXISTS idx_fr_factor ON factor_returns(factor_name)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_ic_pk ON ic_analysis(factor_name, trade_date)`,
		`CREATE INDEX IF NOT EXISTS idx_ic_trade_date ON ic_analysis(trade_date)`,
		`CREATE INDEX IF NOT EXISTS idx_ic_factor ON ic_analysis(factor_name)`,
		`CREATE INDEX IF NOT EXISTS idx_wfr_strategy ON walk_forward_reports(strategy_id)`,
		`CREATE INDEX IF NOT EXISTS idx_wfr_report_date ON walk_forward_reports(report_date)`,
		`CREATE INDEX IF NOT EXISTS idx_strategies_type ON strategies(strategy_type)`,
		`CREATE INDEX IF NOT EXISTS idx_strategies_active ON strategies(is_active)`,
		`CREATE INDEX IF NOT EXISTS idx_bj_status ON backtest_jobs(status)`,
		`CREATE INDEX IF NOT EXISTS idx_bj_created_at ON backtest_jobs(created_at)`,
	}

	for _, idx := range indexes {
		if _, err := s.pool.Exec(ctx, idx); err != nil {
			return fmt.Errorf("index creation failed: %w", err)
		}
	}

	s.logger.Info().Msg("Database migrations completed")
	return nil
}
