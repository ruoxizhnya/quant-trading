-- Migration 008: IC (Information Coefficient) analysis table
CREATE TABLE IF NOT EXISTS ic_analysis (
    id SERIAL PRIMARY KEY,
    factor_name VARCHAR(20) NOT NULL,
    trade_date DATE NOT NULL,
    ic DOUBLE PRECISION,
    p_value DOUBLE PRECISION,
    top_ic DOUBLE PRECISION,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ic_pk ON ic_analysis(factor_name, trade_date);
CREATE INDEX IF NOT EXISTS idx_ic_trade_date ON ic_analysis(trade_date);
CREATE INDEX IF NOT EXISTS idx_ic_factor ON ic_analysis(factor_name);
