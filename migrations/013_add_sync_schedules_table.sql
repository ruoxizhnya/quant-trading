-- Migration: 013_add_sync_schedules_table
-- Description: Create sync_schedules table for cron-based sync scheduling
-- Author: Quant Lab Architecture Team
-- Date: 2026-05-04

CREATE TABLE IF NOT EXISTS public.sync_schedules (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL UNIQUE,
    description TEXT,
    job_type VARCHAR(50) NOT NULL,
    cron_expression VARCHAR(100) NOT NULL,
    params JSONB NOT NULL DEFAULT '{}',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    last_run_at TIMESTAMPTZ,
    last_run_status VARCHAR(20),
    last_run_job_id VARCHAR(64),
    next_run_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(50) DEFAULT 'system'
);

-- Indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_sync_schedules_active ON public.sync_schedules(is_active);
CREATE INDEX IF NOT EXISTS idx_sync_schedules_type ON public.sync_schedules(job_type);
CREATE INDEX IF NOT EXISTS idx_sync_schedules_next_run ON public.sync_schedules(next_run_at);

-- Comments
COMMENT ON TABLE public.sync_schedules IS 'Cron-based sync schedule configurations';
COMMENT ON COLUMN public.sync_schedules.cron_expression IS 'Cron expression in standard format (with optional seconds field)';
COMMENT ON COLUMN public.sync_schedules.job_type IS 'Type of sync job to execute: stocks, ohlcv, fundamentals, dividends, splits, calendar, factors, etc.';
COMMENT ON COLUMN public.sync_schedules.params IS 'JSON parameters passed to the job when triggered';
COMMENT ON COLUMN public.sync_schedules.last_run_status IS 'Status of last execution: completed, failed, running';
