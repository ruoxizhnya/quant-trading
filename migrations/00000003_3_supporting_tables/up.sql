-- Migration: 0003_create_supporting_tables
-- Description: Create trading calendar, dividends, index constituents, factor cache tables

CREATE TABLE IF NOT EXISTS public.trading_calendar (
    trade_date DATE PRIMARY KEY,
    exchange VARCHAR(10) DEFAULT 'SSE',
    is_trading_day BOOLEAN DEFAULT TRUE
);

CREATE INDEX IF NOT EXISTS idx_calendar_exchange ON public.trading_calendar(exchange);

CREATE TABLE IF NOT EXISTS public.dividends (
    id SERIAL PRIMARY KEY,
    symbol VARCHAR(20) NOT NULL,
    ann_date DATE NOT NULL,
    rec_date DATE,
    pay_date DATE,
    div_amt DOUBLE PRECISION,
    stk_div DOUBLE PRECISION,
    stk_ratio DOUBLE PRECISION,
    cash_ratio DOUBLE PRECISION,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_dividends_symbol ON public.dividends(symbol);
CREATE INDEX IF NOT EXISTS idx_dividends_ann_date ON public.dividends(ann_date);

CREATE TABLE IF NOT EXISTS public.index_constituents (
    id SERIAL PRIMARY KEY,
    index_code VARCHAR(20) NOT NULL,
    symbol VARCHAR(20) NOT NULL,
    in_date DATE,
    out_date DATE,
    weight DOUBLE PRECISION,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_index_constituents_index ON public.index_constituents(index_code);
CREATE INDEX IF NOT EXISTS idx_index_constituents_symbol ON public.index_constituents(symbol);

CREATE TABLE IF NOT EXISTS public.factor_cache (
    id SERIAL PRIMARY KEY,
    symbol VARCHAR(20) NOT NULL,
    trade_date DATE NOT NULL,
    factor_name VARCHAR(50) NOT NULL,
    raw_value DOUBLE PRECISION,
    z_score DOUBLE PRECISION,
    percentile DOUBLE PRECISION,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(symbol, trade_date, factor_name)
);

CREATE INDEX IF NOT EXISTS idx_factor_cache_symbol_date ON public.factor_cache(symbol, trade_date);
CREATE INDEX IF NOT EXISTS idx_factor_cache_factor_name ON public.factor_cache(factor_name);

CREATE TABLE IF NOT EXISTS public.splits (
    id SERIAL PRIMARY KEY,
    symbol VARCHAR(20) NOT NULL,
    trade_date DATE NOT NULL,
    stk_div_ratio DOUBLE PRECISION,
    cash_div_ratio DOUBLE PRECISION,
    currency VARCHAR(10),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_splits_symbol ON public.splits(symbol);

CREATE TABLE IF NOT EXISTS public.orders (
    id SERIAL PRIMARY KEY,
    order_id VARCHAR(64) UNIQUE NOT NULL,
    symbol VARCHAR(20) NOT NULL,
    direction VARCHAR(10) NOT NULL CHECK (direction IN ('long', 'short')),
    order_type VARCHAR(10) NOT NULL DEFAULT 'market' CHECK (order_type IN ('market', 'limit')),
    quantity DECIMAL(18,4) NOT NULL CHECK (quantity > 0),
    filled_qty DECIMAL(18,4) NOT NULL DEFAULT 0 CHECK (filled_qty >= 0),
    price DECIMAL(18,4) NOT NULL DEFAULT 0 CHECK (price >= 0),
    avg_fill_price DECIMAL(18,4) NOT NULL DEFAULT 0 CHECK (avg_fill_price >= 0),
    status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'filled', 'cancelled', 'rejected', 'partial')),
    submitted_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    message TEXT
);

CREATE INDEX IF NOT EXISTS idx_orders_symbol ON public.orders(symbol);
CREATE INDEX IF NOT EXISTS idx_orders_status ON public.orders(status);
CREATE INDEX IF NOT EXISTS idx_orders_symbol_status ON public.orders(symbol, status);
CREATE INDEX IF NOT EXISTS idx_orders_submitted_at ON public.orders(submitted_at DESC);
