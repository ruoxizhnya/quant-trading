-- Sprint 5.5: Background backtest worker — async job queue
-- docs/migrations/009_add_backtest_jobs_table.sql

CREATE TABLE IF NOT EXISTS backtest_jobs (
    id VARCHAR(64) PRIMARY KEY,
    strategy_id VARCHAR(50) NOT NULL,
    params JSONB NOT NULL DEFAULT '{}',
    universe VARCHAR(100) NOT NULL,
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    result JSONB,
    error_message TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_bj_status ON backtest_jobs(status);
CREATE INDEX IF NOT EXISTS idx_bj_created_at ON backtest_jobs(created_at);
