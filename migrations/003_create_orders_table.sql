-- Migration: Create orders table for execution-service
-- This migration persists order data from memory to PostgreSQL

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

-- Indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_orders_symbol ON public.orders(symbol);
CREATE INDEX IF NOT EXISTS idx_orders_status ON public.orders(status);
CREATE INDEX IF NOT EXISTS idx_orders_symbol_status ON public.orders(symbol, status);
CREATE INDEX IF NOT EXISTS idx_orders_submitted_at ON public.orders(submitted_at DESC);

-- Comments
COMMENT ON TABLE public.orders IS 'Order records from execution-service';
COMMENT ON COLUMN public.orders.order_id IS 'Unique order identifier (UUID)';
COMMENT ON COLUMN public.orders.symbol IS 'Stock symbol (e.g., 600000.SH)';
COMMENT ON COLUMN public.orders.direction IS 'Trade direction: long or short';
COMMENT ON COLUMN public.orders.status IS 'Order status: pending, filled, cancelled, rejected, partial';
