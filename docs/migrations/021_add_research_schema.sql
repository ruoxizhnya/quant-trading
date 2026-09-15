-- Migration 021: research schema (ADR-022 §2 类 E — 研究结构化状态投影)
-- Date: 2026-09-15
--
-- 定位: vault markdown（类 D，事实源）的确定性投影，等价于索引 / 物化视图。
--       写入者唯一：确定脚本（不得由 LLM 直接产出）。字段映射见 RESEARCH.md §3.3 契约 C2。
-- 可重建性: 可重建 —— `DROP SCHEMA research CASCADE` 后由 vault markdown 重放。
--          本 schema 不持有任何外部 FK（citations 内的 content_hash 在写入时校验存在性），
--          因此删除本 schema 不影响 ingest / market / quant 任何一侧。
-- 同源副本: 实际执行路径为 pkg/storage/postgres.go 内联 migrate()，本文件为文档副本。

CREATE SCHEMA IF NOT EXISTS research;

-- 1 标的 : 1 行（_profile.json 头部）
CREATE TABLE IF NOT EXISTS research.profile (
    ticker          VARCHAR(20) PRIMARY KEY,
    name            VARCHAR(100) NOT NULL,
    schema_version  INT NOT NULL DEFAULT 1,
    source_file     TEXT,
    source_mtime    TIMESTAMPTZ,
    last_researched DATE,
    needs_review    BOOLEAN NOT NULL DEFAULT FALSE,
    review_reason   TEXT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 结论（_profile.json → conclusions[]）
CREATE TABLE IF NOT EXISTS research.conclusion (
    ticker           VARCHAR(20) NOT NULL REFERENCES research.profile(ticker) ON DELETE CASCADE,
    conclusion_id    VARCHAR(16) NOT NULL,
    title            TEXT NOT NULL,
    body             TEXT,
    as_of            VARCHAR(16),                 -- 如 "2025Q3"
    confidence       VARCHAR(8),                  -- 高 | 中 | 低
    status           VARCHAR(16) NOT NULL DEFAULT 'active', -- active | refuted | superseded
    citations        JSONB NOT NULL DEFAULT '[]', -- [{source,dataset,key,as_of,content_hash}]
    evidence_pointer TEXT,                        -- JSON Pointer，如 snapshots/x.json#/data/合同负债
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (ticker, conclusion_id)
);

-- 疑点（_profile.json → questions[]）
CREATE TABLE IF NOT EXISTS research.question (
    ticker      VARCHAR(20) NOT NULL REFERENCES research.profile(ticker) ON DELETE CASCADE,
    question_id VARCHAR(16) NOT NULL,
    text        TEXT NOT NULL,
    status      VARCHAR(16) NOT NULL DEFAULT 'open', -- open | resolved | dropped
    raised_at   VARCHAR(16),
    PRIMARY KEY (ticker, question_id)
);

CREATE INDEX IF NOT EXISTS idx_research_conclusion_ticker ON research.conclusion(ticker);
CREATE INDEX IF NOT EXISTS idx_research_conclusion_status ON research.conclusion(status);
CREATE INDEX IF NOT EXISTS idx_research_question_ticker ON research.question(ticker);
CREATE INDEX IF NOT EXISTS idx_research_question_status ON research.question(status);
CREATE INDEX IF NOT EXISTS idx_research_profile_needs_review ON research.profile(needs_review) WHERE needs_review = TRUE;