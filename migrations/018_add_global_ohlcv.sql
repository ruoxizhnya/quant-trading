-- Migration 018: Global OHLCV table (Sprint 4)
-- Date: 2026-05-17
-- ADR-016: Multi-Source Data Architecture
-- ODR-011: Multi-Source Integration

-- ===========================================================================
-- 1. Global OHLCV (Yahoo Finance / Alpha Vantage)
-- ===========================================================================
-- Stores daily OHLCV for non-A-share markets (US, HK, etc.).
-- Symbol is a free-form ticker (e.g. "AAPL", "0700.HK").
CREATE TABLE IF NOT EXISTS global_ohlcv (
    symbol      VARCHAR(20) NOT NULL,
    trade_date  DATE NOT NULL,
    open        DOUBLE PRECISION NOT NULL,
    high        DOUBLE PRECISION NOT NULL,
    low         DOUBLE PRECISION NOT NULL,
    close       DOUBLE PRECISION NOT NULL,
    volume      DOUBLE PRECISION,
    adj_close   DOUBLE PRECISION,                                -- adjusted close (splits/dividends)
    source      VARCHAR(32) DEFAULT 'yahoo_finance',
    ingest_time TIMESTAMPTZ DEFAULT NOW(),
    data_version INT DEFAULT 1,
    PRIMARY KEY (symbol, trade_date)
);

-- Try to convert to a hypertable; this is best-effort and silently
-- no-ops on plain Postgres.
SELECT create_hypertable('global_ohlcv', 'trade_date',
    chunk_time_interval => INTERVAL '90 days',
    if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS idx_global_ohlcv_source
    ON global_ohlcv (source, trade_date DESC);

-- ===========================================================================
-- 2. Seed fallback chains for Sprint 4 data types
-- ===========================================================================
INSERT INTO data_fallback_chain (data_type, source_name, priority) VALUES
    ('global_ohlcv', 'yahoo_finance', 1),
    ('global_ohlcv', 'alpha_vantage', 2)
ON CONFLICT DO NOTHING;
