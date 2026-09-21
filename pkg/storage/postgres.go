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

	// P2-3：把内置因子的假设来源写进库（只补没有的，不覆盖人写的）。
	// 失败只记日志 —— 假设来源查不到会降级到代码里的内置表，不至于让服务起不来。
	if _, err := store.SeedBuiltinFactorHypotheses(ctx); err != nil {
		logger.Warn().Err(err).Msg("Failed to seed factor hypotheses — 查询会回退到代码里的内置表")
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
//
// ─── 这里是 DDL 的唯一真相（P1-4）─────────────────────────────────────
//
// 每次启动按顺序跑一遍下面这个数组，每条都必须是幂等的（CREATE ...
// IF NOT EXISTS / ALTER ... ADD COLUMN IF NOT EXISTS / DO 块守卫）。
// 没有版本表、没有 down、没有执行记录 —— 这是有意的取舍，理由见下。
//
// 目录里那些 .sql 的历史：
//   - `migrations/`：3 个 golang-migrate 格式的子目录 + 10 个裸编号 .sql
//   - `docs/migrations/`：另一批，含 025-027 的实际变更记录
//
// 它们**不被执行**。golang-migrate 的封装（MigrationManager）从来没有被
// 调用过，2026-09-18 删除。为什么没有改用标准迁移工具：
//   - 现有库都是这套 Go DDL 建的，没有 schema_migrations 表，接入要先
//     baseline，而 baseline 一旦和真实结构对不上，后面每条迁移都在
//     错误的假设上跑；
//   - 回测结果依赖表结构。为了工程上的整洁去动一个能用的库，收益是
//     洁癖，风险是数据。不划算。
//
// 因此约定：**加表 / 加列就在这个数组末尾追加一条，并写上
// `// Migration 0NN:` 注释**（编号续最大的那个）。散落的 .sql 留作历史
// 记录，只读、不维护、不进导航。
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
		// Migration 031: stock_fundamentals.available_date (P1-4 / 语义债)
		// 「这条财报从哪天起可见」此前没有自己的列，只在 5 个查询里以
		// COALESCE(ann_date, trade_date) 的形式散着写 —— 另外 5 个查询压根
		// 没写，直接拿 trade_date 当可见日，而 trade_date 装的是报告期截止日。
		// 语义散在调用方脑子里，就一定会有人忘。
		//
		// 现在钉成一个列：available_date = COALESCE(ann_date, trade_date)。
		// ann_date 为 NULL 时它仍等于报告期截止日（那笔债还在，见路径 A），
		// 但**列的存在让债可见** —— 可以数得出来有多少行 ann_date 是空的，
		// 也可以在回填之后把缺口补上。
		// 用**生成列**而不是普通列：普通列要靠每个写入函数记得填，而
		// 任何绕过写入函数的路径（手工 INSERT、修数、别的 ETL）都会留下
		// 一行 available_date 为 NULL 的数据 —— 它随后会从所有按可用日
		// 过滤的查询里**凭空消失**，而且看不出为什么少了一只票。
		// 生成列从物理上保证：它不可能为空，也不可能和 COALESCE 的结果
		// 不一致，加列时存量自动算好（不需要回填 UPDATE）。
		//
		// 代价：不能 INSERT/UPDATE 这一列，写入函数里必须去掉它。
		`ALTER TABLE stock_fundamentals
			ADD COLUMN IF NOT EXISTS available_date DATE
			GENERATED ALWAYS AS (COALESCE(ann_date, trade_date)) STORED`,
		// 按可用日过滤是这个表最主要的读法，给个索引。
		`CREATE INDEX IF NOT EXISTS idx_stock_fundamentals_available
			ON stock_fundamentals (ts_code, available_date DESC)`,
		// 清理历史脏数据：domain.FundamentalData.AnnDate 是 time.Time 而非
		// 指针，缺字段时写进去的是零值 0001-01-01 —— 那不是披露日，是"没填"。
		// 它让 COALESCE(ann_date, trade_date) 拿到一个公元前的值，整行数据
		// 从此在所有按可用日过滤的查询里消失。存量的这类行必须清成 NULL，
		// 否则加完列反而会少数据（写入侧已用 nullableDate 堵住新增）。
		`UPDATE stock_fundamentals SET ann_date = NULL WHERE ann_date < DATE '1900-01-01'`,
		// Migration 032: factor_hypothesis (P2-3 / 因子的因果来源)
		//
		// 因子的数值存在 factor_cache 里，但**它为什么该有效**此前没有地方记。
		// 缺了这一条就没法区分「有经济学依据」和「数据挖掘挖出来的」—— 而
		// 后者正是过拟合的主要来源：在数据上试出来的相关性，换个时间段就散。
		//
		// 为什么是独立的表而不是 factor_cache 的一列：假设是**因子级**元数据，
		// 而 factor_cache 是 symbol × date × factor 的行级缓存 —— 存成列会让
		// 同一个字符串重复几十万次，改一次要全表 UPDATE。
		`CREATE TABLE IF NOT EXISTS factor_hypothesis (
			factor_name  VARCHAR(32) PRIMARY KEY,
			source_kind  VARCHAR(24) NOT NULL,
			hypothesis   TEXT        NOT NULL,
			reference    TEXT,
			created_at   TIMESTAMPTZ DEFAULT NOW(),
			updated_at   TIMESTAMPTZ DEFAULT NOW()
		)`,
		// 把「已经是普通列」的存量安装升级成生成列。
		//
		// ADD COLUMN IF NOT EXISTS 只管"列在不在"，不管"列是怎么算的"：
		// 早期那版把它建成普通 DATE 列，之后再跑到的 GENERATED 版就会因为
		// IF NOT EXISTS 被整个跳过 —— 结果是一个永远为 NULL 的普通列，
		// 所有按可用日过滤的查询静默返回空。这个 DO 块按
		// generation_expression 是否为空来判断，只在实际需要时重建一次，
		// 之后重复启动等价 no-op。
		`DO $$
		BEGIN
			IF EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'stock_fundamentals'
				  AND column_name = 'available_date'
				  AND coalesce(generation_expression, '') = ''
			) THEN
				ALTER TABLE stock_fundamentals DROP COLUMN available_date;
				ALTER TABLE stock_fundamentals
					ADD COLUMN available_date DATE
					GENERATED ALWAYS AS (COALESCE(ann_date, trade_date)) STORED;
			END IF;
		END $$`,
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

		// === Migration 033-043: 多源数据面的 11 张目标表（C5 / ODR-065）===
		//
		// 这些表的 DDL 此前只存在于 migrations/015~018，而那个目录**不被任何代码
		// 执行**（golang-migrate 封装已于 2026-09-18 删除，注释里写明了）。后果：
		// ETL 的 TableMapper 声明 13 类数据，其中 11 类的目标表在全新库里根本不
		// 存在，任一多源同步任务一跑就是 `relation does not exist` 硬失败 ——
		// 多源数据面在干净环境开箱不可用，存量库全靠手工跑过 .sql 才活着。
		//
		// 逐字移植自 migrations/*.sql，但**不建 hypertable**：这一批的目标是
		// 「表存在、能写入」，不该为此引入 TimescaleDB 硬依赖。源 SQL 里的
		// create_hypertable / add_retention_policy 刻意省略，需要时单独加。
		//
		// data_source_registry / data_fallback_chain 不在这一批里 —— 数据源注册表
		// 是内存态（pkg/data/source/registry.go 明确不写这两张表）。

		// Migration 033: realtime_quote (mootdx 五档快照)
		`CREATE TABLE IF NOT EXISTS realtime_quote (
			symbol      VARCHAR(20) NOT NULL,
			ts          TIMESTAMPTZ NOT NULL,
			price       DOUBLE PRECISION,
			open        DOUBLE PRECISION,
			high        DOUBLE PRECISION,
			low         DOUBLE PRECISION,
			last_close  DOUBLE PRECISION,
			volume      BIGINT,
			amount      DOUBLE PRECISION,
			bid1        DOUBLE PRECISION, ask1     DOUBLE PRECISION,
			bid1_vol    INT, ask1_vol              INT,
			bid2        DOUBLE PRECISION, ask2     DOUBLE PRECISION,
			bid2_vol    INT, ask2_vol              INT,
			bid3        DOUBLE PRECISION, ask3     DOUBLE PRECISION,
			bid3_vol    INT, ask3_vol              INT,
			bid4        DOUBLE PRECISION, ask4     DOUBLE PRECISION,
			bid4_vol    INT, ask4_vol              INT,
			bid5        DOUBLE PRECISION, ask5     DOUBLE PRECISION,
			bid5_vol    INT, ask5_vol              INT,
			source      VARCHAR(32) DEFAULT 'mootdx',
			ingest_time TIMESTAMPTZ DEFAULT NOW(),
			PRIMARY KEY (symbol, ts)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_realtime_quote_symbol_ts ON realtime_quote (symbol, ts DESC)`,

		// Migration 034: ohlcv_minute (mootdx 1 分钟 K 线)
		`CREATE TABLE IF NOT EXISTS ohlcv_minute (
			symbol      VARCHAR(20) NOT NULL,
			ts          TIMESTAMPTZ NOT NULL,
			open        DOUBLE PRECISION NOT NULL,
			high        DOUBLE PRECISION NOT NULL,
			low         DOUBLE PRECISION NOT NULL,
			close       DOUBLE PRECISION NOT NULL,
			volume      BIGINT,
			amount      DOUBLE PRECISION,
			source      VARCHAR(32) DEFAULT 'mootdx',
			ingest_time TIMESTAMPTZ DEFAULT NOW(),
			PRIMARY KEY (symbol, ts)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ohlcv_minute_symbol_ts ON ohlcv_minute (symbol, ts DESC)`,

		// Migration 035: capital_flow (东财 push2 资金流)
		// UNIQUE (symbol, trade_date, period) 是 TableMapper 的冲突键，
		// 缺了它 BulkInsert 的 ON CONFLICT 就没有目标约束。
		`CREATE TABLE IF NOT EXISTS capital_flow (
			id               BIGSERIAL PRIMARY KEY,
			symbol           VARCHAR(20) NOT NULL,
			trade_date       DATE NOT NULL,
			period           VARCHAR(16) NOT NULL,
			main_net         DOUBLE PRECISION,
			main_buy_amount  DOUBLE PRECISION,
			main_sell_amount DOUBLE PRECISION,
			super_net        DOUBLE PRECISION,
			large_net        DOUBLE PRECISION,
			medium_net       DOUBLE PRECISION,
			small_net        DOUBLE PRECISION,
			main_net_ratio   DOUBLE PRECISION,
			retail_net       DOUBLE PRECISION,
			retail_net_ratio DOUBLE PRECISION,
			close_price      DOUBLE PRECISION,
			change_pct       DOUBLE PRECISION,
			source           VARCHAR(32) DEFAULT 'eastmoney',
			ingest_time      TIMESTAMPTZ DEFAULT NOW(),
			UNIQUE (symbol, trade_date, period)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cf_symbol_date ON capital_flow (symbol, trade_date DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_cf_date_main_net ON capital_flow (trade_date DESC, main_net DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_cf_symbol_period ON capital_flow (symbol, period, trade_date DESC)`,

		// Migration 036: sectors (东财板块快照)
		`CREATE TABLE IF NOT EXISTS sectors (
			sector_code     VARCHAR(32) PRIMARY KEY,
			sector_name     VARCHAR(64) NOT NULL,
			category        VARCHAR(32) NOT NULL DEFAULT 'industry',
			trade_date      DATE NOT NULL,
			change_pct      DOUBLE PRECISION,
			leading_symbol  VARCHAR(20),
			leading_change  DOUBLE PRECISION,
			source          VARCHAR(32) DEFAULT 'eastmoney',
			ingest_time     TIMESTAMPTZ DEFAULT NOW(),
			data_version    INT DEFAULT 1
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sectors_date ON sectors (trade_date DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_sectors_category ON sectors (category, trade_date DESC)`,

		// Migration 037: stock_sector_map (个股 ↔ 板块，多对多)
		`CREATE TABLE IF NOT EXISTS stock_sector_map (
			symbol      VARCHAR(20) NOT NULL,
			sector_code VARCHAR(32) NOT NULL,
			sector_name VARCHAR(64) NOT NULL,
			source      VARCHAR(32) DEFAULT 'eastmoney',
			ingest_time TIMESTAMPTZ DEFAULT NOW(),
			PRIMARY KEY (symbol, sector_code)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ssm_sector ON stock_sector_map (sector_code)`,

		// Migration 038: top_list (龙虎榜)
		`CREATE TABLE IF NOT EXISTS top_list (
			id          BIGSERIAL PRIMARY KEY,
			trade_date  DATE NOT NULL,
			symbol      VARCHAR(20) NOT NULL,
			name        VARCHAR(100) NOT NULL,
			net_buy     DOUBLE PRECISION,
			buy_amount  DOUBLE PRECISION,
			sell_amount DOUBLE PRECISION,
			turnover    DOUBLE PRECISION,
			reason      TEXT,
			explain     TEXT,
			close_price DOUBLE PRECISION,
			change_pct  DOUBLE PRECISION,
			source      VARCHAR(32) DEFAULT 'eastmoney',
			ingest_time TIMESTAMPTZ DEFAULT NOW(),
			data_version INT DEFAULT 1,
			UNIQUE (trade_date, symbol)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_top_list_date ON top_list (trade_date DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_top_list_symbol ON top_list (symbol, trade_date DESC)`,

		// Migration 039: limit_up_pool (涨停池)
		`CREATE TABLE IF NOT EXISTS limit_up_pool (
			id          BIGSERIAL PRIMARY KEY,
			trade_date  DATE NOT NULL,
			symbol      VARCHAR(20) NOT NULL,
			name        VARCHAR(100) NOT NULL,
			limit_price DOUBLE PRECISION,
			first_time  TIMESTAMPTZ,
			last_time   TIMESTAMPTZ,
			limit_times INT DEFAULT 1,
			continuous  INT DEFAULT 1,
			industry    VARCHAR(64),
			concept     TEXT,
			amount      DOUBLE PRECISION,
			source      VARCHAR(32) DEFAULT 'eastmoney',
			ingest_time TIMESTAMPTZ DEFAULT NOW(),
			data_version INT DEFAULT 1,
			UNIQUE (trade_date, symbol)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_limit_up_date ON limit_up_pool (trade_date DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_limit_up_symbol ON limit_up_pool (symbol, trade_date DESC)`,

		// Migration 040: announcements (巨潮公告)
		`CREATE TABLE IF NOT EXISTS announcements (
			ann_id       VARCHAR(64) PRIMARY KEY,
			symbol       VARCHAR(20) NOT NULL,
			ann_title    TEXT NOT NULL,
			ann_time     TIMESTAMPTZ NOT NULL,
			ann_type     VARCHAR(64),
			pdf_url      TEXT,
			source       VARCHAR(32) DEFAULT 'juchao',
			ingest_time  TIMESTAMPTZ DEFAULT NOW(),
			data_version INT DEFAULT 1
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ann_symbol_time ON announcements (symbol, ann_time DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_ann_time ON announcements (ann_time DESC)`,

		// Migration 041: news (个股新闻)
		`CREATE TABLE IF NOT EXISTS news (
			news_id      VARCHAR(64) PRIMARY KEY,
			symbol       VARCHAR(20) NOT NULL,
			title        TEXT NOT NULL,
			content      TEXT,
			publish_time TIMESTAMPTZ NOT NULL,
			url          TEXT,
			source_name  VARCHAR(64),
			source       VARCHAR(32) DEFAULT 'eastmoney',
			ingest_time  TIMESTAMPTZ DEFAULT NOW(),
			data_version INT DEFAULT 1
		)`,
		`CREATE INDEX IF NOT EXISTS idx_news_symbol_time ON news (symbol, publish_time DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_news_time ON news (publish_time DESC)`,

		// Migration 042: hot_search (雪球热门搜索)
		`CREATE TABLE IF NOT EXISTS hot_search (
			id            BIGSERIAL PRIMARY KEY,
			rank          INT NOT NULL,
			keyword       VARCHAR(128) NOT NULL,
			snapshot_time TIMESTAMPTZ NOT NULL,
			heat          DOUBLE PRECISION,
			source        VARCHAR(32) DEFAULT 'xueqiu',
			ingest_time   TIMESTAMPTZ DEFAULT NOW(),
			data_version  INT DEFAULT 1,
			UNIQUE (rank, snapshot_time)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_hot_search_time ON hot_search (snapshot_time DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_hot_search_keyword ON hot_search (keyword, snapshot_time DESC)`,

		// Migration 043: global_ohlcv (美股 / 港股等非 A 股日线)
		`CREATE TABLE IF NOT EXISTS global_ohlcv (
			symbol       VARCHAR(20) NOT NULL,
			trade_date   DATE NOT NULL,
			open         DOUBLE PRECISION NOT NULL,
			high         DOUBLE PRECISION NOT NULL,
			low          DOUBLE PRECISION NOT NULL,
			close        DOUBLE PRECISION NOT NULL,
			volume       DOUBLE PRECISION,
			adj_close    DOUBLE PRECISION,
			source       VARCHAR(32) DEFAULT 'yahoo_finance',
			ingest_time  TIMESTAMPTZ DEFAULT NOW(),
			data_version INT DEFAULT 1,
			PRIMARY KEY (symbol, trade_date)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_global_ohlcv_source ON global_ohlcv (source, trade_date DESC)`,

		// === Migration 044-047: 同步作业队列与基因池（C5 的漏网之鱼）===
		//
		// 上一批只补了 TableMapper 声明的 11 张写入目标表，但**代码直接 SQL 引用**
		// 的表不止那些。这是真跑起来才暴露的：起服务后打 /sync/stocks，
		// 直接 `relation "sync_jobs" does not exist` —— 同步链路自己的队列表压根
		// 没建。静态清单式审查补不完，得拿真实库对一遍。
		//
		// 这四张同样来自不被执行的 migrations/012、013、023。

		// Migration 044: sync_jobs (同步作业队列)
		`CREATE TABLE IF NOT EXISTS sync_jobs (
			id              VARCHAR(64) PRIMARY KEY,
			job_type        VARCHAR(50) NOT NULL,
			status          VARCHAR(20) NOT NULL DEFAULT 'pending',
			params          JSONB NOT NULL DEFAULT '{}',
			progress_percent INT NOT NULL DEFAULT 0,
			total_items     INT NOT NULL DEFAULT 0,
			processed_items INT NOT NULL DEFAULT 0,
			failed_items    INT NOT NULL DEFAULT 0,
			error_message   TEXT,
			result          JSONB,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			started_at      TIMESTAMPTZ,
			completed_at    TIMESTAMPTZ,
			retry_count     INT NOT NULL DEFAULT 0,
			max_retries     INT NOT NULL DEFAULT 3,
			scheduled_at    TIMESTAMPTZ,
			worker_id       VARCHAR(50)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_jobs_status ON sync_jobs(status)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_jobs_type ON sync_jobs(job_type)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_jobs_created_at ON sync_jobs(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_jobs_scheduled_at ON sync_jobs(scheduled_at)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_jobs_status_created ON sync_jobs(status, created_at)`,

		// Migration 045: sync_schedules (cron 定时同步)
		`CREATE TABLE IF NOT EXISTS sync_schedules (
			id              SERIAL PRIMARY KEY,
			name            VARCHAR(100) NOT NULL UNIQUE,
			description     TEXT,
			job_type        VARCHAR(50) NOT NULL,
			cron_expression VARCHAR(100) NOT NULL,
			params          JSONB NOT NULL DEFAULT '{}',
			is_active       BOOLEAN NOT NULL DEFAULT TRUE,
			last_run_at     TIMESTAMPTZ,
			last_run_status VARCHAR(20),
			last_run_job_id VARCHAR(64),
			next_run_at     TIMESTAMPTZ,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			created_by      VARCHAR(50) DEFAULT 'system'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_schedules_active ON sync_schedules(is_active)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_schedules_type ON sync_schedules(job_type)`,
		`CREATE INDEX IF NOT EXISTS idx_sync_schedules_next_run ON sync_schedules(next_run_at)`,

		// Migration 046: factor_genes (AI 因子基因池)
		`CREATE TABLE IF NOT EXISTS factor_genes (
			id          VARCHAR(50) PRIMARY KEY,
			name        VARCHAR(100) NOT NULL,
			category    VARCHAR(30) NOT NULL,
			formula     TEXT NOT NULL,
			description TEXT,
			rationale   TEXT,
			ic          DOUBLE PRECISION DEFAULT 0,
			ir          DOUBLE PRECISION DEFAULT 0,
			turnover    DOUBLE PRECISION DEFAULT 0,
			sharpe      DOUBLE PRECISION DEFAULT 0,
			fitness     DOUBLE PRECISION DEFAULT 0,
			generation  INTEGER DEFAULT 0,
			parent_ids  JSONB DEFAULT '[]',
			status      VARCHAR(20) DEFAULT 'pending',
			created_at  TIMESTAMPTZ DEFAULT NOW(),
			updated_at  TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_factor_genes_category ON factor_genes(category)`,
		`CREATE INDEX IF NOT EXISTS idx_factor_genes_status ON factor_genes(status)`,
		`CREATE INDEX IF NOT EXISTS idx_factor_genes_fitness ON factor_genes(fitness DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_factor_genes_cat_status ON factor_genes(category, status)`,

		// Migration 047: strategy_genes (AI 策略基因池)
		`CREATE TABLE IF NOT EXISTS strategy_genes (
			id            VARCHAR(50) PRIMARY KEY,
			name          VARCHAR(100) NOT NULL,
			description   TEXT,
			strategy_type VARCHAR(30) NOT NULL,
			code          TEXT,
			params        JSONB DEFAULT '{}',
			factor_ids    JSONB DEFAULT '[]',
			parent_ids    JSONB DEFAULT '[]',
			total_return  DOUBLE PRECISION DEFAULT 0,
			sharpe        DOUBLE PRECISION DEFAULT 0,
			max_drawdown  DOUBLE PRECISION DEFAULT 0,
			win_rate      DOUBLE PRECISION DEFAULT 0,
			fitness       DOUBLE PRECISION DEFAULT 0,
			generation    INTEGER DEFAULT 0,
			status        VARCHAR(20) DEFAULT 'pending',
			created_at    TIMESTAMPTZ DEFAULT NOW(),
			updated_at    TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_strategy_genes_type ON strategy_genes(strategy_type)`,
		`CREATE INDEX IF NOT EXISTS idx_strategy_genes_status ON strategy_genes(status)`,
		`CREATE INDEX IF NOT EXISTS idx_strategy_genes_fitness ON strategy_genes(fitness DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_strategy_genes_type_status ON strategy_genes(strategy_type, status)`,
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
