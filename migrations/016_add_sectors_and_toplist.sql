-- Migration 016: Sector / TopList / LimitUp tables (Sprint 2)
-- Date: 2026-05-17
-- ADR-016: Multi-Source Data Architecture
-- ODR-011: Multi-Source Integration

-- ===========================================================================
-- 1. Sectors (东财 slist 概念/行业板块)
-- ===========================================================================
-- Stores sector board snapshots from Eastmoney. One row per
-- (sector_code, trade_date).
CREATE TABLE IF NOT EXISTS sectors (
    sector_code     VARCHAR(32) PRIMARY KEY,
    sector_name     VARCHAR(64) NOT NULL,
    category        VARCHAR(32) NOT NULL DEFAULT 'industry',  -- 'industry' | 'concept' | 'style'
    trade_date      DATE NOT NULL,
    change_pct      DOUBLE PRECISION,                          -- 板块涨跌幅
    leading_symbol  VARCHAR(20),                               -- 领涨股
    leading_change  DOUBLE PRECISION,                           -- 领涨股涨跌幅
    source          VARCHAR(32) DEFAULT 'eastmoney',
    ingest_time     TIMESTAMPTZ DEFAULT NOW(),
    data_version    INT DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_sectors_date ON sectors(trade_date DESC);
CREATE INDEX IF NOT EXISTS idx_sectors_category ON sectors(category, trade_date DESC);
CREATE INDEX IF NOT EXISTS idx_sectors_source ON sectors(source);

-- ===========================================================================
-- 2. Stock → Sector mapping
-- ===========================================================================
-- Many-to-many relation: a stock can belong to multiple sectors.
CREATE TABLE IF NOT EXISTS stock_sector_map (
    symbol          VARCHAR(20) NOT NULL,
    sector_code     VARCHAR(32) NOT NULL,
    sector_name     VARCHAR(64) NOT NULL,
    source          VARCHAR(32) DEFAULT 'eastmoney',
    ingest_time     TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (symbol, sector_code)
);

CREATE INDEX IF NOT EXISTS idx_ssm_sector ON stock_sector_map(sector_code);
CREATE INDEX IF NOT EXISTS idx_ssm_source ON stock_sector_map(source);

-- ===========================================================================
-- 3. TopList (龙虎榜)
-- ===========================================================================
-- Stores dragon-tiger list (institutional net buy/sell disclosures).
CREATE TABLE IF NOT EXISTS top_list (
    id              BIGSERIAL PRIMARY KEY,
    trade_date      DATE NOT NULL,
    symbol          VARCHAR(20) NOT NULL,
    name            VARCHAR(100) NOT NULL,
    net_buy         DOUBLE PRECISION,                          -- 净买入额
    buy_amount      DOUBLE PRECISION,                          -- 买入总额
    sell_amount     DOUBLE PRECISION,                          -- 卖出总额
    turnover        DOUBLE PRECISION,                          -- 当日成交额
    reason          TEXT,                                       -- 上榜原因
    explain         TEXT,                                       -- 详细说明
    close_price     DOUBLE PRECISION,
    change_pct      DOUBLE PRECISION,
    source          VARCHAR(32) DEFAULT 'eastmoney',
    ingest_time     TIMESTAMPTZ DEFAULT NOW(),
    data_version    INT DEFAULT 1,
    UNIQUE (trade_date, symbol)
);

CREATE INDEX IF NOT EXISTS idx_top_list_date ON top_list(trade_date DESC);
CREATE INDEX IF NOT EXISTS idx_top_list_symbol ON top_list(symbol, trade_date DESC);
CREATE INDEX IF NOT EXISTS idx_top_list_net_buy ON top_list(trade_date DESC, net_buy DESC);

-- ===========================================================================
-- 4. LimitUpPool (涨停池)
-- ===========================================================================
-- Stores stocks that hit the upper price limit on a given trading day.
CREATE TABLE IF NOT EXISTS limit_up_pool (
    id              BIGSERIAL PRIMARY KEY,
    trade_date      DATE NOT NULL,
    symbol          VARCHAR(20) NOT NULL,
    name            VARCHAR(100) NOT NULL,
    limit_price     DOUBLE PRECISION,                          -- 涨停价
    first_time      TIMESTAMPTZ,                               -- 首次涨停时间
    last_time       TIMESTAMPTZ,                               -- 最后涨停时间
    limit_times     INT DEFAULT 1,                             -- 涨停次数
    continuous      INT DEFAULT 1,                             -- 连板数
    industry        VARCHAR(64),                                -- 所属行业
    concept         TEXT,                                       -- 概念标签
    amount          DOUBLE PRECISION,                          -- 成交额
    source          VARCHAR(32) DEFAULT 'eastmoney',
    ingest_time     TIMESTAMPTZ DEFAULT NOW(),
    data_version    INT DEFAULT 1,
    UNIQUE (trade_date, symbol)
);

CREATE INDEX IF NOT EXISTS idx_limit_up_date ON limit_up_pool(trade_date DESC);
CREATE INDEX IF NOT EXISTS idx_limit_up_symbol ON limit_up_pool(symbol, trade_date DESC);
CREATE INDEX IF NOT EXISTS idx_limit_up_continuous ON limit_up_pool(trade_date DESC, continuous DESC);
CREATE INDEX IF NOT EXISTS idx_limit_up_industry ON limit_up_pool(industry, trade_date DESC);

-- ===========================================================================
-- 5. Seed fallback chains for Sprint 2 data types
-- ===========================================================================
INSERT INTO data_fallback_chain (data_type, source_name, priority) VALUES
    ('sectors', 'eastmoney', 1),
    ('stock_sector', 'eastmoney', 1),
    ('top_list', 'eastmoney', 1),
    ('limit_up_pool', 'eastmoney', 1)
ON CONFLICT DO NOTHING;
