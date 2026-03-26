-- Migration 007: Add splits table
-- Stores stock split and rights issue events for all A-share stocks.
-- Reference: Tushare split API
-- Used for forward-price adjustment verification.

CREATE TABLE IF NOT EXISTS splits (
    id SERIAL PRIMARY KEY,
    symbol VARCHAR(20) NOT NULL,
    trade_date DATE NOT NULL,    -- ex-date of the split/rights issue
    ann_date DATE,               -- announcement date
    stk_div_ratio DOUBLE PRECISION, -- stock dividend / split ratio
    cash_div_ratio DOUBLE PRECISION, -- cash dividend ratio
    currency VARCHAR(10) DEFAULT 'CNY',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- A stock can have multiple splits on different dates
CREATE UNIQUE INDEX IF NOT EXISTS idx_splits_symbol_trade
    ON splits(symbol, trade_date);
CREATE INDEX IF NOT EXISTS idx_splits_trade_date
    ON splits(trade_date);
CREATE INDEX IF NOT EXISTS idx_splits_symbol
    ON splits(symbol);
