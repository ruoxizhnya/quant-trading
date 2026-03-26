-- Migration 006: Add factor_cache table for pre-computed factor z-scores
-- This table stores cross-sectional z-scores and percentile ranks for each stock per date per factor.
-- Used by multi-factor backtests to avoid re-computing z-scores on every run.

CREATE TABLE IF NOT EXISTS factor_cache (
    id SERIAL PRIMARY KEY,
    symbol VARCHAR(20) NOT NULL,
    trade_date DATE NOT NULL,
    factor_name VARCHAR(20) NOT NULL,
    raw_value DOUBLE PRECISION,
    z_score DOUBLE PRECISION,
    percentile DOUBLE PRECISION,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Primary key covers all query patterns
CREATE UNIQUE INDEX IF NOT EXISTS idx_fc_pk ON factor_cache(symbol, trade_date, factor_name);
CREATE INDEX IF NOT EXISTS idx_fc_trade_date ON factor_cache(trade_date);
CREATE INDEX IF NOT EXISTS idx_fc_factor_name ON factor_cache(factor_name);
