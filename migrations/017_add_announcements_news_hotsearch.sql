-- Migration 017: Announcements / News / HotSearch tables (Sprint 3)
-- Date: 2026-05-17
-- ADR-016: Multi-Source Data Architecture
-- ODR-011: Multi-Source Integration

-- ===========================================================================
-- 1. Announcements (巨潮)
-- ===========================================================================
-- Stores corporate announcements from 巨潮资讯网 (cninfo.com.cn).
-- orgId is resolved dynamically by the JuchaoAdapter.
CREATE TABLE IF NOT EXISTS announcements (
    ann_id         VARCHAR(64) PRIMARY KEY,
    symbol         VARCHAR(20) NOT NULL,
    ann_title      TEXT NOT NULL,
    ann_time       TIMESTAMPTZ NOT NULL,
    ann_type       VARCHAR(64),                                 -- 'annual_report' | 'dividend' | etc.
    pdf_url        TEXT,
    source         VARCHAR(32) DEFAULT 'juchao',
    ingest_time    TIMESTAMPTZ DEFAULT NOW(),
    data_version   INT DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_ann_symbol_time
    ON announcements (symbol, ann_time DESC);
CREATE INDEX IF NOT EXISTS idx_ann_time
    ON announcements (ann_time DESC);
CREATE INDEX IF NOT EXISTS idx_ann_type
    ON announcements (ann_type, ann_time DESC);
CREATE INDEX IF NOT EXISTS idx_ann_source
    ON announcements (source);

-- ===========================================================================
-- 2. News (东财 / 雪球 个股新闻)
-- ===========================================================================
-- Stores news articles per stock.
CREATE TABLE IF NOT EXISTS news (
    news_id        VARCHAR(64) PRIMARY KEY,
    symbol         VARCHAR(20) NOT NULL,
    title          TEXT NOT NULL,
    content        TEXT,
    publish_time   TIMESTAMPTZ NOT NULL,
    url            TEXT,
    source_name    VARCHAR(64),                                 -- original media name
    source         VARCHAR(32) DEFAULT 'eastmoney',
    ingest_time    TIMESTAMPTZ DEFAULT NOW(),
    data_version   INT DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_news_symbol_time
    ON news (symbol, publish_time DESC);
CREATE INDEX IF NOT EXISTS idx_news_time
    ON news (publish_time DESC);
CREATE INDEX IF NOT EXISTS idx_news_source
    ON news (source);

-- ===========================================================================
-- 3. Hot search (雪球 热门搜索)
-- ===========================================================================
-- Stores hot-keyword snapshots from xueqiu.
CREATE TABLE IF NOT EXISTS hot_search (
    id             BIGSERIAL PRIMARY KEY,
    rank           INT NOT NULL,
    keyword        VARCHAR(128) NOT NULL,
    snapshot_time  TIMESTAMPTZ NOT NULL,
    heat           DOUBLE PRECISION,
    source         VARCHAR(32) DEFAULT 'xueqiu',
    ingest_time    TIMESTAMPTZ DEFAULT NOW(),
    data_version   INT DEFAULT 1,
    UNIQUE (rank, snapshot_time)
);

CREATE INDEX IF NOT EXISTS idx_hot_search_time
    ON hot_search (snapshot_time DESC);
CREATE INDEX IF NOT EXISTS idx_hot_search_keyword
    ON hot_search (keyword, snapshot_time DESC);

-- ===========================================================================
-- 4. Seed fallback chains for Sprint 3 data types
-- ===========================================================================
INSERT INTO data_fallback_chain (data_type, source_name, priority) VALUES
    ('announcements', 'juchao', 1),
    ('announcements', 'eastmoney', 2),
    ('news', 'eastmoney', 1),
    ('news', 'xueqiu', 2),
    ('hot_search', 'xueqiu', 1)
ON CONFLICT DO NOTHING;
