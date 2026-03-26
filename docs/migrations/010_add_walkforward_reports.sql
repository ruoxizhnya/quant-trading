-- Migration 010: Walk-forward validation reports
CREATE TABLE IF NOT EXISTS walk_forward_reports (
    id SERIAL PRIMARY KEY,
    strategy_id VARCHAR(50) NOT NULL,
    universe VARCHAR(100),
    report_date DATE NOT NULL,
    avg_test_sharpe DOUBLE PRECISION,
    avg_test_return DOUBLE PRECISION,
    avg_test_max_dd DOUBLE PRECISION,
    avg_degradation DOUBLE PRECISION,
    pass_rate DOUBLE PRECISION,
    overall_pass BOOLEAN,
    windows_json JSONB NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_wfr_strategy ON walk_forward_reports(strategy_id);
CREATE INDEX IF NOT EXISTS idx_wfr_report_date ON walk_forward_reports(report_date);
