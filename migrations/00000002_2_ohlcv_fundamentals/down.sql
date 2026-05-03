-- Rollback: 0002_create_ohlcv_and_fundamentals_tables

DROP TABLE IF EXISTS public.stock_fundamentals;
DROP TABLE IF EXISTS public.fundamentals;
DROP TABLE IF EXISTS public.ohlcv_daily_qfq;
