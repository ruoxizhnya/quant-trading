-- Migration 004: Add dividends table
-- Stores dividend events (cash + stock) for all A-share stocks.
-- Reference: Tushare dividend API (dividend)

CREATE TABLE IF NOT EXISTS dividends (
    id SERIAL PRIMARY KEY,
    symbol VARCHAR(20) NOT NULL,
    ann_date DATE NOT NULL,
    rec_date DATE,              -- record date: shareholders as of this date receive dividend
    pay_date DATE,              -- payment / ex-dividend date
    div_amt DOUBLE PRECISION,   -- cash dividend per share (元/股)
    stk_div DOUBLE PRECISION,   -- stock dividend per share (股/股)
    stk_ratio DOUBLE PRECISION, -- stock split ratio
    cash_ratio DOUBLE PRECISION, -- cash dividend ratio
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_dividends_symbol_ann
    ON dividends(symbol, ann_date);
CREATE INDEX IF NOT EXISTS idx_dividends_pay_date
    ON dividends(pay_date);
