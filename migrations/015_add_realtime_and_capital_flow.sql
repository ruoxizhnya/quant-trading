-- Migration 015: Add multi-source tables (Sprint 1 - realtime + capital flow)
-- Date: 2026-05-17
-- ADR-016: Multi-Source Data Architecture
-- ODR-011: Multi-Source Integration

-- ===========================================================================
-- 1. Realtime quote (mootdx 5-tick snapshot)
-- ===========================================================================
-- Stores latest 5-level bid/ask quote snapshots from mootdx.
-- Designed as TimescaleDB hypertable for high-frequency writes.
CREATE TABLE IF NOT EXISTS realtime_quote (
    symbol      VARCHAR(20) NOT NULL,
    ts          TIMESTAMPTZ NOT NULL,
    price       DOUBLE PRECISION,           -- 最新价
    open        DOUBLE PRECISION,
    high        DOUBLE PRECISION,
    low         DOUBLE PRECISION,
    last_close  DOUBLE PRECISION,           -- 昨收
    volume      BIGINT,
    amount      DOUBLE PRECISION,           -- 成交额（元）
    bid1        DOUBLE PRECISION, ask1     DOUBLE PRECISION,
    bid1_vol    INT, ask1_vol              INT,
    bid2        DOUBLE PRECISION, ask2     DOUBLE PRECISION,
    bid2_vol    INT, ask2_vol              INT,
    bid3        DOUBLE PRECISION, ask3     DOUBLE PRECISION,
    bid3_vol    INT, ask3_vol              INT,
    bid4        DOUBLE PRECISION, ask4     DOUBLE PRECISION,
    bid4_vol    INT, ask4_vol              INT,
    bid5        DOUBLE PRECISION, ask5     DOUBLE PRECISION,
    bid5_vol    INT, ask5_vol              INT,
    source      VARCHAR(32) DEFAULT 'mootdx',
    ingest_time TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (symbol, ts)
);

-- Convert to TimescaleDB hypertable (1-day chunks for high-frequency data)
SELECT create_hypertable('realtime_quote', 'ts',
    chunk_time_interval => INTERVAL '1 day',
    if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS idx_realtime_quote_symbol_ts
    ON realtime_quote (symbol, ts DESC);

-- Keep realtime data for 30 days only (high volume)
SELECT add_retention_policy('realtime_quote', INTERVAL '30 days',
    if_not_exists => TRUE);

-- ===========================================================================
-- 2. Minute K-line (mootdx 1-minute bars)
-- ===========================================================================
-- Stores 1-minute OHLCV bars from mootdx for intraday strategies.
CREATE TABLE IF NOT EXISTS ohlcv_minute (
    symbol      VARCHAR(20) NOT NULL,
    ts          TIMESTAMPTZ NOT NULL,
    open        DOUBLE PRECISION NOT NULL,
    high        DOUBLE PRECISION NOT NULL,
    low         DOUBLE PRECISION NOT NULL,
    close       DOUBLE PRECISION NOT NULL,
    volume      BIGINT,
    amount      DOUBLE PRECISION,
    source      VARCHAR(32) DEFAULT 'mootdx',
    ingest_time TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (symbol, ts)
);

-- Convert to TimescaleDB hypertable (1-day chunks)
SELECT create_hypertable('ohlcv_minute', 'ts',
    chunk_time_interval => INTERVAL '1 day',
    if_not_exists => TRUE);

CREATE INDEX IF NOT EXISTS idx_ohlcv_minute_symbol_ts
    ON ohlcv_minute (symbol, ts DESC);

-- Keep minute data for 1 year (useful for backtesting)
SELECT add_retention_policy('ohlcv_minute', INTERVAL '365 days',
    if_not_exists => TRUE);

-- ===========================================================================
-- 3. Capital flow (eastmoney push2 - main fund / retail flow)
-- ===========================================================================
-- Stores intraday capital flow data from eastmoney push2.
-- Tracks main fund (主力), super-large (超大单), large (大单), medium (中单), small (小单).
CREATE TABLE IF NOT EXISTS capital_flow (
    id              BIGSERIAL PRIMARY KEY,
    symbol          VARCHAR(20) NOT NULL,
    trade_date      DATE NOT NULL,
    period          VARCHAR(16) NOT NULL,    -- 'daily' | '5d' | '10d' | '60d'
    main_net        DOUBLE PRECISION,         -- 主力净流入
    main_buy_amount DOUBLE PRECISION,         -- 主力买入额
    main_sell_amount DOUBLE PRECISION,        -- 主力卖出额
    super_net       DOUBLE PRECISION,         -- 超大单净流入
    large_net       DOUBLE PRECISION,         -- 大单净流入
    medium_net      DOUBLE PRECISION,         -- 中单净流入
    small_net       DOUBLE PRECISION,         -- 小单净流入
    main_net_ratio  DOUBLE PRECISION,         -- 主力净流入占比
    retail_net      DOUBLE PRECISION,         -- 散户净流入
    retail_net_ratio DOUBLE PRECISION,        -- 散户净流入占比
    close_price     DOUBLE PRECISION,
    change_pct      DOUBLE PRECISION,
    source          VARCHAR(32) DEFAULT 'eastmoney',
    ingest_time     TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (symbol, trade_date, period)
);

CREATE INDEX IF NOT EXISTS idx_cf_symbol_date
    ON capital_flow (symbol, trade_date DESC);
CREATE INDEX IF NOT EXISTS idx_cf_date_main_net
    ON capital_flow (trade_date DESC, main_net DESC);
CREATE INDEX IF NOT EXISTS idx_cf_symbol_period
    ON capital_flow (symbol, period, trade_date DESC);

-- ===========================================================================
-- 4. Data source registry (metadata)
-- ===========================================================================
CREATE TABLE IF NOT EXISTS data_source_registry (
    name              VARCHAR(32) PRIMARY KEY,    -- 'tushare' | 'mootdx' | ...
    type              VARCHAR(16) NOT NULL,        -- 'http' | 'sdk' | 'websocket'
    enabled           BOOLEAN DEFAULT true,
    rate_limit_per_min INT DEFAULT 60,
    priority          INT NOT NULL,                -- 降级链顺序
    config            JSONB DEFAULT '{}',
    last_health_check TIMESTAMPTZ,
    last_error        TEXT,
    last_success_at   TIMESTAMPTZ,
    created_at        TIMESTAMPTZ DEFAULT NOW(),
    updated_at        TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_dsr_enabled_priority
    ON data_source_registry (enabled, priority);

-- ===========================================================================
-- 5. Data fallback chain (per data_type, multiple sources in priority order)
-- ===========================================================================
CREATE TABLE IF NOT EXISTS data_fallback_chain (
    data_type   VARCHAR(64) NOT NULL,         -- 'ohlcv_daily' | 'realtime_quote' | ...
    source_name VARCHAR(32) NOT NULL,
    priority    INT NOT NULL,                  -- 1 = primary, 2 = secondary, ...
    enabled     BOOLEAN DEFAULT true,
    PRIMARY KEY (data_type, source_name),
    FOREIGN KEY (source_name) REFERENCES data_source_registry(name) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_dfc_data_type_priority
    ON data_fallback_chain (data_type, priority);

-- ===========================================================================
-- 6. Seed default fallback chains
-- ===========================================================================
INSERT INTO data_source_registry (name, type, enabled, rate_limit_per_min, priority) VALUES
    ('tushare', 'http', true, 200, 100),
    ('mootdx', 'sdk', true, 600, 90),
    ('eastmoney', 'http', true, 120, 80),
    ('sina', 'http', true, 120, 70),
    ('xueqiu', 'http', true, 60, 60),
    ('juchao', 'http', true, 60, 50),
    ('alpha_vantage', 'http', true, 5, 40),
    ('yahoo_finance', 'http', true, 60, 30)
ON CONFLICT (name) DO NOTHING;

-- A股日线 K线 降级链
INSERT INTO data_fallback_chain (data_type, source_name, priority) VALUES
    ('ohlcv_daily', 'tushare', 1),
    ('ohlcv_daily', 'eastmoney', 2),
    ('ohlcv_daily', 'sina', 3)
ON CONFLICT DO NOTHING;

-- A股实时行情 降级链
INSERT INTO data_fallback_chain (data_type, source_name, priority) VALUES
    ('realtime_quote', 'mootdx', 1),
    ('realtime_quote', 'eastmoney', 2),
    ('realtime_quote', 'sina', 3)
ON CONFLICT DO NOTHING;

-- A股分钟 K线 (mootdx 独有)
INSERT INTO data_fallback_chain (data_type, source_name, priority) VALUES
    ('ohlcv_minute', 'mootdx', 1),
    ('ohlcv_minute', 'eastmoney', 2)
ON CONFLICT DO NOTHING;

-- 资金流 (东财 push2 独有)
INSERT INTO data_fallback_chain (data_type, source_name, priority) VALUES
    ('capital_flow', 'eastmoney', 1)
ON CONFLICT DO NOTHING;

-- 财务数据 降级链
INSERT INTO data_fallback_chain (data_type, source_name, priority) VALUES
    ('fundamentals', 'tushare', 1),
    ('fundamentals', 'eastmoney', 2),
    ('fundamentals', 'xueqiu', 3)
ON CONFLICT DO NOTHING;

-- 全球股票 降级链
INSERT INTO data_fallback_chain (data_type, source_name, priority) VALUES
    ('global_ohlcv', 'yahoo_finance', 1),
    ('global_ohlcv', 'alpha_vantage', 2)
ON CONFLICT DO NOTHING;
