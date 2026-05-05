-- Migration: 012_add_sync_jobs_table
-- Description: Create sync_jobs table for data synchronization task queue
-- Author: Quant Lab Architecture Team
-- Date: 2026-05-04

CREATE TABLE IF NOT EXISTS public.sync_jobs (
    id VARCHAR(64) PRIMARY KEY,
    job_type VARCHAR(50) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    params JSONB NOT NULL DEFAULT '{}',
    progress_percent INT NOT NULL DEFAULT 0,
    total_items INT NOT NULL DEFAULT 0,
    processed_items INT NOT NULL DEFAULT 0,
    failed_items INT NOT NULL DEFAULT 0,
    error_message TEXT,
    result JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    retry_count INT NOT NULL DEFAULT 0,
    max_retries INT NOT NULL DEFAULT 3,
    scheduled_at TIMESTAMPTZ,
    worker_id VARCHAR(50)
);

-- Indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_sync_jobs_status ON public.sync_jobs(status);
CREATE INDEX IF NOT EXISTS idx_sync_jobs_type ON public.sync_jobs(job_type);
CREATE INDEX IF NOT EXISTS idx_sync_jobs_created_at ON public.sync_jobs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_sync_jobs_scheduled_at ON public.sync_jobs(scheduled_at);
CREATE INDEX IF NOT EXISTS idx_sync_jobs_status_created ON public.sync_jobs(status, created_at);

-- Comments
COMMENT ON TABLE public.sync_jobs IS 'Data synchronization job queue for async task processing';
COMMENT ON COLUMN public.sync_jobs.job_type IS 'Type of sync job: stocks, ohlcv, fundamentals, dividends, splits, calendar, factors, etc.';
COMMENT ON COLUMN public.sync_jobs.status IS 'Job status: pending, running, completed, failed, cancelled';
COMMENT ON COLUMN public.sync_jobs.params IS 'JSON parameters specific to the job type';
COMMENT ON COLUMN public.sync_jobs.progress_percent IS 'Progress percentage 0-100';
COMMENT ON COLUMN public.sync_jobs.retry_count IS 'Number of retry attempts made';
COMMENT ON COLUMN public.sync_jobs.max_retries IS 'Maximum allowed retry attempts';
