-- Migration 056: market_activity（D5 新数据源样板——市场活跃度）
--
-- 个股日频活跃度观测表：Tushare daily_basic（换手率族）经
-- tushare_daily_basic adapter（pkg/data/source/tushare_daily_basic_adapter.go）
-- 导入，是「挂新数据 → 可挖因子」全链路（ADR-031 蓝图 §4.4，任务 D5）的
-- 规范层落点。market_activity 因子（D5 名册协议变更后的第一个新因子名）
-- 以 turnover_rate 为输入：turnover_rate / ts_mean(turnover_rate, 20) - 1。
--
-- 设计要点：
--   * 时间语义 after_close（D2 契约）：换手率由当日全天成交量除以股本统计
--     得出，收盘前不存在——蓝图 §4.2 点名的高危 PIT 场景（活跃度/拥挤度）
--     的第一个真实考卷。取数侧由 GetMarketActivityForDateRange 的
--     `trade_date <= end` 执法。
--   * 缺失≠0：turnover_rate / turnover_rate_f / volume_ratio 三列可空，
--     NULL = 上游未发布（停牌股当日无换手），绝不落 0 冒充有效观测；写入面
--     对非空值校验有限性（NaN/Inf 拒绝）。
--   * 幂等键 (symbol, trade_date, source)：同键重导刷新取值与 as_of
--     （ON CONFLICT DO UPDATE）——上游会重述修正。
--   * 与 stock_fundamentals（fina_indicator 财报期截面）并列不重复：那是
--     财报口径，这是日频市场关注度截面。
--
-- 唯一执行真相在 pkg/storage/postgres.go 的 Migration 056 追加段
-- （migrate() 按序执行）；本文件是记录镜像，不被执行。

CREATE TABLE IF NOT EXISTS market_activity (
    id              BIGSERIAL      PRIMARY KEY,
    symbol          VARCHAR(20)    NOT NULL,
    trade_date      DATE           NOT NULL,
    turnover_rate   DOUBLE PRECISION,
    turnover_rate_f DOUBLE PRECISION,
    volume_ratio    DOUBLE PRECISION,
    source          VARCHAR(64)    NOT NULL,
    as_of           TIMESTAMPTZ    NOT NULL,
    CONSTRAINT market_activity_identity_present CHECK (
        btrim(symbol) <> '' AND btrim(source) <> ''
    ),
    UNIQUE (symbol, trade_date, source)
);

CREATE INDEX IF NOT EXISTS idx_market_activity_trade_date ON market_activity(trade_date);
