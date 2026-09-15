-- Migration 020: ingest.raw (ADR-022 §2 类 A — 原始源响应归档)
-- Date: 2026-09-15
--
-- 定位: 所有外部数据源响应的唯一归档。content_hash 是全部数字的最终证据坐标，
--       也是 GET /api/evidence/{content_hash} 的查询键（ADR-022 §5）。
-- 可重建性: 不可重建（外部源响应），但可由 L0 单一摄取入口按 key + as_of 重抓。
-- 同源副本: 实际执行路径为 pkg/storage/postgres.go 内联 migrate()，本文件为文档副本。

CREATE SCHEMA IF NOT EXISTS ingest;

CREATE TABLE IF NOT EXISTS ingest.raw (
    content_hash VARCHAR(64) PRIMARY KEY,   -- sha256(payload 规范化序列化)
    source       VARCHAR(64)  NOT NULL,     -- akshare | tushare | eastmoney | mootdx ...
    dataset      VARCHAR(128) NOT NULL,     -- balance_sheet | income | cashflow | daily ...
    key          TEXT         NOT NULL,     -- 源侧主键（如 600519.SH:2025Q3）
    as_of        DATE,                      -- 数据截止日（PIT 对齐用）
    payload      JSONB        NOT NULL,     -- 原始响应体（不可改写）
    fetched_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ingest_raw_source_dataset ON ingest.raw(source, dataset);
CREATE INDEX IF NOT EXISTS idx_ingest_raw_as_of ON ingest.raw(as_of DESC);