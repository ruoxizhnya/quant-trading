-- Migration 007: Factor Returns table for quintile portfolio attribution
CREATE TABLE IF NOT EXISTS factor_returns (
    id SERIAL PRIMARY KEY,
    factor_name VARCHAR(20) NOT NULL,
    trade_date DATE NOT NULL,
    quintile INTEGER NOT NULL CHECK (quintile BETWEEN 1 AND 5),
    avg_return DOUBLE PRECISION,
    cumulative_return DOUBLE PRECISION,
    top_minus_bot DOUBLE PRECISION,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_fr_pk ON factor_returns(factor_name, trade_date, quintile);
CREATE INDEX IF NOT EXISTS idx_fr_trade_date ON factor_returns(trade_date);
CREATE INDEX IF NOT EXISTS idx_fr_factor ON factor_returns(factor_name);
