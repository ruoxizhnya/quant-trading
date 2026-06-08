-- Migration 014: Add source column to all data tables (data lineage)
-- Date: 2026-05-17
-- ADR-016: Multi-Source Data Architecture
-- ODR-011: Multi-Source Integration

-- Add source column to identify which data source provided the row
-- Add ingest_time to track when data was loaded
-- Add data_version for ETL re-processing safety
--
-- All columns have safe defaults so existing data is preserved.
-- After this migration, every row can be traced back to its source.

-- stocks
ALTER TABLE stocks ADD COLUMN IF NOT EXISTS source VARCHAR(32) DEFAULT 'tushare';
ALTER TABLE stocks ADD COLUMN IF NOT EXISTS ingest_time TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE stocks ADD COLUMN IF NOT EXISTS data_version INT DEFAULT 1;
CREATE INDEX IF NOT EXISTS idx_stocks_source ON stocks(source);

-- ohlcv_daily_qfq
ALTER TABLE ohlcv_daily_qfq ADD COLUMN IF NOT EXISTS source VARCHAR(32) DEFAULT 'tushare';
ALTER TABLE ohlcv_daily_qfq ADD COLUMN IF NOT EXISTS ingest_time TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE ohlcv_daily_qfq ADD COLUMN IF NOT EXISTS data_version INT DEFAULT 1;
CREATE INDEX IF NOT EXISTS idx_ohlcv_source ON ohlcv_daily_qfq(source);
-- Composite index for common query: "latest source X data for symbol Y"
CREATE INDEX IF NOT EXISTS idx_ohlcv_symbol_source_date ON ohlcv_daily_qfq(symbol, source, trade_date DESC);

-- stock_fundamentals
ALTER TABLE stock_fundamentals ADD COLUMN IF NOT EXISTS source VARCHAR(32) DEFAULT 'tushare';
ALTER TABLE stock_fundamentals ADD COLUMN IF NOT EXISTS ingest_time TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE stock_fundamentals ADD COLUMN IF NOT EXISTS data_version INT DEFAULT 1;
CREATE INDEX IF NOT EXISTS idx_sf_source ON stock_fundamentals(source);

-- trading_calendar
ALTER TABLE trading_calendar ADD COLUMN IF NOT EXISTS source VARCHAR(32) DEFAULT 'tushare';
ALTER TABLE trading_calendar ADD COLUMN IF NOT EXISTS ingest_time TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE trading_calendar ADD COLUMN IF NOT EXISTS data_version INT DEFAULT 1;

-- dividends
ALTER TABLE dividends ADD COLUMN IF NOT EXISTS source VARCHAR(32) DEFAULT 'tushare';
ALTER TABLE dividends ADD COLUMN IF NOT EXISTS ingest_time TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE dividends ADD COLUMN IF NOT EXISTS data_version INT DEFAULT 1;
CREATE INDEX IF NOT EXISTS idx_dividends_source ON dividends(source);

-- splits
ALTER TABLE splits ADD COLUMN IF NOT EXISTS source VARCHAR(32) DEFAULT 'tushare';
ALTER TABLE splits ADD COLUMN IF NOT EXISTS ingest_time TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE splits ADD COLUMN IF NOT EXISTS data_version INT DEFAULT 1;
CREATE INDEX IF NOT EXISTS idx_splits_source ON splits(source);

-- index_constituents
ALTER TABLE index_constituents ADD COLUMN IF NOT EXISTS source VARCHAR(32) DEFAULT 'tushare';
ALTER TABLE index_constituents ADD COLUMN IF NOT EXISTS ingest_time TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE index_constituents ADD COLUMN IF NOT EXISTS data_version INT DEFAULT 1;

-- factor_cache (calculated data, source tracks the upstream data source)
ALTER TABLE factor_cache ADD COLUMN IF NOT EXISTS source VARCHAR(32) DEFAULT 'computed';
ALTER TABLE factor_cache ADD COLUMN IF NOT EXISTS ingest_time TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE factor_cache ADD COLUMN IF NOT EXISTS data_version INT DEFAULT 1;

-- fundamentals (alternative financial data table)
ALTER TABLE fundamentals ADD COLUMN IF NOT EXISTS source VARCHAR(32) DEFAULT 'tushare';
ALTER TABLE fundamentals ADD COLUMN IF NOT EXISTS ingest_time TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE fundamentals ADD COLUMN IF NOT EXISTS data_version INT DEFAULT 1;

-- strategies (configuration, not data, but track source for traceability)
ALTER TABLE strategies ADD COLUMN IF NOT EXISTS source VARCHAR(32) DEFAULT 'user';
ALTER TABLE strategies ADD COLUMN IF NOT EXISTS ingest_time TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE strategies ADD COLUMN IF NOT EXISTS data_version INT DEFAULT 1;

-- backtest_jobs
ALTER TABLE backtest_jobs ADD COLUMN IF NOT EXISTS source VARCHAR(32) DEFAULT 'user';
ALTER TABLE backtest_jobs ADD COLUMN IF NOT EXISTS ingest_time TIMESTAMPTZ DEFAULT NOW();

-- factor_returns
ALTER TABLE factor_returns ADD COLUMN IF NOT EXISTS source VARCHAR(32) DEFAULT 'computed';
ALTER TABLE factor_returns ADD COLUMN IF NOT EXISTS ingest_time TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE factor_returns ADD COLUMN IF NOT EXISTS data_version INT DEFAULT 1;

-- ic_analysis
ALTER TABLE ic_analysis ADD COLUMN IF NOT EXISTS source VARCHAR(32) DEFAULT 'computed';
ALTER TABLE ic_analysis ADD COLUMN IF NOT EXISTS ingest_time TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE ic_analysis ADD COLUMN IF NOT EXISTS data_version INT DEFAULT 1;

-- walk_forward_reports
ALTER TABLE walk_forward_reports ADD COLUMN IF NOT EXISTS source VARCHAR(32) DEFAULT 'computed';
ALTER TABLE walk_forward_reports ADD COLUMN IF NOT EXISTS ingest_time TIMESTAMPTZ DEFAULT NOW();
ALTER TABLE walk_forward_reports ADD COLUMN IF NOT EXISTS data_version INT DEFAULT 1;
