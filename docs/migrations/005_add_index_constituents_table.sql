-- Migration 005: Add index_constituents table
-- Stores constituent stocks of major indices (CSI 300, CSI 500, CSI 800)

CREATE TABLE IF NOT EXISTS index_constituents (
    id SERIAL PRIMARY KEY,
    index_code VARCHAR(20) NOT NULL,      -- e.g. 000300.SH
    symbol VARCHAR(20) NOT NULL,          -- stock ts_code
    in_date DATE,
    out_date DATE,
    weight DOUBLE PRECISION,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Unique constraint: each (symbol, index_code) pair appears at most once
-- (represents the current/latest entry; historical changes tracked via in_date/out_date)
CREATE UNIQUE INDEX IF NOT EXISTS idx_ic_symbol_index
    ON index_constituents(symbol, index_code);

CREATE INDEX IF NOT EXISTS idx_ic_index_code
    ON index_constituents(index_code);

CREATE INDEX IF NOT EXISTS idx_ic_in_date
    ON index_constituents(in_date);
