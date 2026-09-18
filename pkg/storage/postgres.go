package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
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
		// Migration 024: docs/migrations/024_add_factor_returns_table.sql
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
		// Migration 019: auth tables (ADR-017 §2 / P1-2)
		`CREATE TABLE IF NOT EXISTS users (
			id BIGSERIAL PRIMARY KEY,
			username VARCHAR(64) NOT NULL UNIQUE,
			password_hash VARCHAR(120) NOT NULL,
			role VARCHAR(16) NOT NULL DEFAULT 'viewer',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_login_at TIMESTAMPTZ,
			disabled BOOLEAN NOT NULL DEFAULT FALSE
		)`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
			role VARCHAR(16),
			ip INET,
			endpoint TEXT NOT NULL,
			method VARCHAR(8) NOT NULL,
			payload_hash VARCHAR(64),
			trace_id VARCHAR(64),
			status_code INT NOT NULL,
			timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		// Migration 020: docs/migrations/020_add_ingest_raw.sql (ADR-022 §2 类 A)
		`CREATE SCHEMA IF NOT EXISTS ingest`,
		`CREATE TABLE IF NOT EXISTS ingest.raw (
			content_hash VARCHAR(64) PRIMARY KEY,
			source VARCHAR(64) NOT NULL,
			dataset VARCHAR(128) NOT NULL,
			key TEXT NOT NULL,
			as_of DATE,
			payload JSONB NOT NULL,
			fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ingest_raw_source_dataset ON ingest.raw(source, dataset)`,
		`CREATE INDEX IF NOT EXISTS idx_ingest_raw_as_of ON ingest.raw(as_of DESC)`,
		// Migration 021: docs/migrations/021_add_research_schema.sql (ADR-022 §2 类 E)
		`CREATE SCHEMA IF NOT EXISTS research`,
		`CREATE TABLE IF NOT EXISTS research.profile (
			ticker VARCHAR(20) PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			schema_version INT NOT NULL DEFAULT 1,
			source_file TEXT,
			source_mtime TIMESTAMPTZ,
			last_researched DATE,
			needs_review BOOLEAN NOT NULL DEFAULT FALSE,
			review_reason TEXT,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS research.conclusion (
			ticker VARCHAR(20) NOT NULL REFERENCES research.profile(ticker) ON DELETE CASCADE,
			conclusion_id VARCHAR(16) NOT NULL,
			title TEXT NOT NULL,
			body TEXT,
			as_of VARCHAR(16),
			confidence VARCHAR(8),
			status VARCHAR(16) NOT NULL DEFAULT 'active',
			citations JSONB NOT NULL DEFAULT '[]',
			evidence_pointer TEXT,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (ticker, conclusion_id)
		)`,
		`CREATE TABLE IF NOT EXISTS research.question (
			ticker VARCHAR(20) NOT NULL REFERENCES research.profile(ticker) ON DELETE CASCADE,
			question_id VARCHAR(16) NOT NULL,
			text TEXT NOT NULL,
			status VARCHAR(16) NOT NULL DEFAULT 'open',
			raised_at VARCHAR(16),
			PRIMARY KEY (ticker, question_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_research_conclusion_ticker ON research.conclusion(ticker)`,
		`CREATE INDEX IF NOT EXISTS idx_research_conclusion_status ON research.conclusion(status)`,
		`CREATE INDEX IF NOT EXISTS idx_research_question_ticker ON research.question(ticker)`,
		`CREATE INDEX IF NOT EXISTS idx_research_question_status ON research.question(status)`,
		`CREATE INDEX IF NOT EXISTS idx_research_profile_needs_review ON research.profile(needs_review) WHERE needs_review = TRUE`,
		// Migration 022: docs/migrations/022_equitydeep_fundamentals.sql (契约 C1, EQD-P1-1)
		// 逐字段行存 + ann_date PIT 对齐 + snapshot_uri 溯源。仅新增表，不改存量表。
		`CREATE TABLE IF NOT EXISTS fundamentals_detail (
			ts_code        VARCHAR(12)  NOT NULL,
			end_date       DATE         NOT NULL,
			ann_date       DATE         NOT NULL,
			field_code     VARCHAR(64)  NOT NULL,
			raw_field_name VARCHAR(128) NOT NULL,
			value          NUMERIC(24,4),
			unit           VARCHAR(16)  NOT NULL,
			source         VARCHAR(32)  NOT NULL,
			fetched_at     TIMESTAMPTZ  NOT NULL,
			snapshot_uri   TEXT         NOT NULL,
			PRIMARY KEY (ts_code, end_date, ann_date, field_code, fetched_at)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_fund_detail_lookup
			ON fundamentals_detail (ts_code, field_code, end_date DESC)`,
		// Migration 028: experiments (P1-1 / S1 通回路)
		// AI 每次尝试落一行，回答五个问题：从哪开始(hypothesis) · 试了什么(params) ·
		// 结果怎样(metrics) · 怎么走到这(seq + parent_id) · 用的哪份数据(dataset_split)。
		// 失败也必须留行 —— 「试几次才撞出来」本身就是过拟合检测的证据
		// （PRODUCT §验证器：试 5 次和试 500 次撞出来的，可信度差一个量级）。
		// 因此 status 停在 running 的行不是垃圾数据，那是「跑到第几步被叫停」，
		// 清理它就等于抹掉回路中断的位置。
		// UNIQUE(run_id, seq)：一次 run 内第 n 次尝试只有一条，路径回放才不会歧义。
		`CREATE TABLE IF NOT EXISTS experiments (
			id            BIGSERIAL PRIMARY KEY,
			run_id        VARCHAR(64)  NOT NULL,
			seq           INT          NOT NULL,
			parent_id     BIGINT       REFERENCES experiments(id) ON DELETE SET NULL,
			hypothesis    TEXT,
			params        JSONB        NOT NULL DEFAULT '{}',
			dataset_split VARCHAR(16)  NOT NULL DEFAULT 'train',
			strategy_name VARCHAR(100),
			expression    TEXT,
			status        VARCHAR(16)  NOT NULL DEFAULT 'running',
			metrics       JSONB,
			verdict       JSONB,
			error_message TEXT,
			created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
			finished_at   TIMESTAMPTZ,
			UNIQUE (run_id, seq)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_experiments_run_seq ON experiments(run_id, seq)`,
		// Migration 029: experiments.verdict (P2-9 接线 / A)
		// 验证器链每次尝试后跑一遍，裁决（概率 + 质疑清单）写回那一行。
		// 为什么要落库而不是只在内存里传给前端：runs 是进程内 map，一重启裁决
		// 就没了，而「这次尝试当时被质疑了什么」是复盘时唯一能回答
		// 「为什么当初没采纳它」的东西 —— 它和 metrics 一样是实验的证据。
		// 加列带默认值即 metadata-only，幂等。
		`ALTER TABLE experiments ADD COLUMN IF NOT EXISTS verdict JSONB`,
		// Migration 030: stocks.delist_date (P2-4 / 幸存者偏差)
		// 没有摘牌日就不可能构造「某日仍在市」的股票池：回测 2020 年时，
		// 2021 年退市的票必须在池子里（否则收益被系统性高估），但又不能在
		// 2021 年之后继续参与交易。两条都只能靠这个日期判定。
		// 带默认值加列为 metadata-only，幂等。
		`ALTER TABLE stocks ADD COLUMN IF NOT EXISTS delist_date DATE`,
		// Migration 026: docs/migrations/026_widen_factor_name.sql (EQD-P1-2 / 桥 B1)
		// 桥 B1 的 5 个纵向基本面因子名最长 24 字符，超出既有 VARCHAR(20)。
		// 因子链路的三个表同源同一列，须同时放宽；加宽 varchar 为 metadata-only，不改写表。
		`ALTER TABLE factor_cache ALTER COLUMN factor_name TYPE VARCHAR(32)`,
		`ALTER TABLE factor_returns ALTER COLUMN factor_name TYPE VARCHAR(32)`,
		`ALTER TABLE ic_analysis ALTER COLUMN factor_name TYPE VARCHAR(32)`,
		// Migration 027: docs/migrations/027_factor_cache_citation.sql (ODR-061 / P5-1 切片 C2)
		// factor_cache 因子行携带来源批次坐标 [{"content_hash":"<64hex>"}]，输出面展开为
		// ADR-022 §5 五元组。'[]' = 该数字的 A→B 链尚未建立（momentum/value/quality 及
		// 历史行），与 NULL 相对。带默认值加列为 metadata-only；幂等，重复启动等价 no-op。
		`ALTER TABLE factor_cache ADD COLUMN IF NOT EXISTS citation JSONB NOT NULL DEFAULT '[]'::jsonb`,
		// Migration 025: docs/migrations/025_equitydeep_field_consolidation.sql (EQD-P3-1 / C-8)
		// fundamentals 与 stock_fundamentals 的 12 个指标列同名同义，两条写入路径同源于
		// tushare fina_indicator 且都把 trade_date 取成 end_date，故存量按 symbol→ts_code
		// 直通并入幸存表 stock_fundamentals 后 DROP 旧表。DO 块守卫存在性以保证重复启动为 no-op;
		// 冲突时保留幸存表已有非空值（COALESCE 方向为「幸存表优先」）。
		`DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = 'public' AND table_name = 'fundamentals'
			) THEN
				INSERT INTO stock_fundamentals (
					ts_code, trade_date, end_date,
					pe, pb, ps, roe, roa, debt_to_equity, gross_margin, net_margin,
					revenue, net_profit, total_assets, total_liab
				)
				SELECT f.symbol, f.trade_date, f.trade_date,
					f.pe, f.pb, f.ps, f.roe, f.roa, f.debt_to_equity, f.gross_margin,
					f.net_margin, f.revenue, f.net_profit, f.total_assets, f.total_liab
				FROM fundamentals f
				ON CONFLICT (ts_code, trade_date) DO UPDATE SET
					pe             = COALESCE(stock_fundamentals.pe, EXCLUDED.pe),
					pb             = COALESCE(stock_fundamentals.pb, EXCLUDED.pb),
					ps             = COALESCE(stock_fundamentals.ps, EXCLUDED.ps),
					roe            = COALESCE(stock_fundamentals.roe, EXCLUDED.roe),
					roa            = COALESCE(stock_fundamentals.roa, EXCLUDED.roa),
					debt_to_equity = COALESCE(stock_fundamentals.debt_to_equity, EXCLUDED.debt_to_equity),
					gross_margin   = COALESCE(stock_fundamentals.gross_margin, EXCLUDED.gross_margin),
					net_margin     = COALESCE(stock_fundamentals.net_margin, EXCLUDED.net_margin),
					revenue        = COALESCE(stock_fundamentals.revenue, EXCLUDED.revenue),
					net_profit     = COALESCE(stock_fundamentals.net_profit, EXCLUDED.net_profit),
					total_assets   = COALESCE(stock_fundamentals.total_assets, EXCLUDED.total_assets),
					total_liab     = COALESCE(stock_fundamentals.total_liab, EXCLUDED.total_liab);
				DROP TABLE fundamentals;
			END IF;
		END $$`,
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
		// P1-2: auth indexes
		`CREATE INDEX IF NOT EXISTS idx_audit_user_time ON audit_logs(user_id, timestamp DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_endpoint ON audit_logs(endpoint, timestamp DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_logs(timestamp DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_users_role ON users(role) WHERE disabled = FALSE`,
		// Migration 020: ingest.raw indexes (类 A 原始源响应归档)
		`CREATE INDEX IF NOT EXISTS idx_ingest_raw_source_dataset ON ingest.raw(source, dataset)`,
		`CREATE INDEX IF NOT EXISTS idx_ingest_raw_as_of ON ingest.raw(as_of DESC)`,
		// Migration 021: research schema indexes (类 E 研究结构化投影)
		`CREATE INDEX IF NOT EXISTS idx_research_conclusion_ticker ON research.conclusion(ticker)`,
		`CREATE INDEX IF NOT EXISTS idx_research_conclusion_status ON research.conclusion(status)`,
		`CREATE INDEX IF NOT EXISTS idx_research_question_ticker ON research.question(ticker)`,
		`CREATE INDEX IF NOT EXISTS idx_research_question_status ON research.question(status)`,
		`CREATE INDEX IF NOT EXISTS idx_research_profile_needs_review ON research.profile(needs_review) WHERE needs_review = TRUE`,
	}

	for _, idx := range indexes {
		if _, err := s.pool.Exec(ctx, idx); err != nil {
			return fmt.Errorf("index creation failed: %w", err)
		}
	}

	s.logger.Info().Msg("Database migrations completed")
	return nil
}
