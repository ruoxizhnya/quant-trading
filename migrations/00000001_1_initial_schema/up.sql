-- Migration: 0001_create_stocks_table
-- Description: Create stocks table for storing stock basic information

CREATE TABLE IF NOT EXISTS public.stocks (
    symbol VARCHAR(20) PRIMARY KEY,
    name VARCHAR(200) NOT NULL,
    exchange VARCHAR(20) NOT NULL,
    industry VARCHAR(100),
    market_cap DOUBLE PRECISION,
    list_date DATE,
    status VARCHAR(20) DEFAULT 'active',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_stocks_exchange ON public.stocks(exchange);
CREATE INDEX IF NOT EXISTS idx_stocks_industry ON public.stocks(industry);
CREATE INDEX IF NOT EXISTS idx_stocks_status ON public.stocks(status);
