-- Migration: 0002_create_ohlcv_and_fundamentals_tables
-- Description: Create OHLCV and fundamentals tables for market data storage

CREATE TABLE IF NOT EXISTS public.ohlcv_daily_qfq (
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
);

CREATE INDEX IF NOT EXISTS idx_ohlcv_symbol_date ON public.ohlcv_daily_qfq(symbol, trade_date);
CREATE INDEX IF NOT EXISTS idx_ohlcv_trade_date ON public.ohlcv_daily_qfq(trade_date);

CREATE TABLE IF NOT EXISTS public.fundamentals (
    symbol VARCHAR(20) NOT NULL,
    trade_date DATE NOT NULL,
    pe DOUBLE PRECISION,
    pb DOUBLE PRECISION,
    ps DOUBLE PRECISION,
    roe DOUBLE PRECISION,
    roa DOUBLE PRECISION,
    debt_to_equity DOUBLE PRECISION,
    gross_margin DOUBLE PRECISION,
    net_margin DOUBLE PRECISION,
    revenue DOUBLE PRECISION,
    net_profit DOUBLE PRECISION,
    total_assets DOUBLE PRECISION,
    total_liab DOUBLE PRECISION,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (symbol, trade_date)
);

CREATE TABLE IF NOT EXISTS public.stock_fundamentals (
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
);

CREATE INDEX IF NOT EXISTS idx_fundamentals_symbol ON public.fundamentals(symbol);
CREATE INDEX IF NOT EXISTS idx_stock_fundamentals_tscode ON public.stock_fundamentals(ts_code);
