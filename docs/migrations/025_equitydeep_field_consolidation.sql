-- Migration 025: fundamentals / stock_fundamentals 表重叠收口（TASKS.md EQD-P3-1 / RESEARCH §3.7 C-8）
-- Date: 2026-09-15
--
-- 定位: migration 0002 建了两张列几乎完全重叠的基本面表（pe/pb/ps/roe/roa/debt_to_equity/
--       gross_margin/net_margin/revenue/net_profit/total_assets/total_liab 共 12 列同名同义），
--       migration 014 又各自补了 source/ingest_time/data_version，重叠进一步固化。
--       两条写入路径同源于 tushare 的同一个 fina_indicator API，字段列表逐字相同，且都把
--       trade_date 置为 end_date（pkg/data/tushare.go 的 normalizeFundamentals /
--       normalizeFundamentalsData），故合并是纯列名直通：fundamentals.symbol 存的本来就是
--       ts_code 字面量（FetchFundamentals 以 ts_code 为参数、取回 item[0]），无需格式转换。
-- 决策: 幸存表为 stock_fundamentals —— 它已承载横截面快照（GetFundamentalsSnapshot）、
--       筛选（ScreenFundamentals）、历史查询（GetFundamentalDataLatest/History）与
--       BulkInsert（data_type "fundamentals" → stock_fundamentals）四条路径；
--       fundamentals 的自然键 (symbol, trade_date) 与之一一对应，存量并入后 DROP。
-- 影响面: pkg/storage/fundamentals.go 的 SaveFundamental / SaveFundamentalBatch /
--         GetFundamental / GetFundamentals 四个函数已同步改指 stock_fundamentals；
--         pkg/storage/postgres.go 内联 migrate() 已移除本表 DDL 与两处索引。
-- 冲突语义: 同 (ts_code, trade_date) 同时存在于两表时，保留 stock_fundamentals 已有的非空值，
--           仅用旧表值补空（COALESCE 方向为「幸存表优先」），避免旧表覆盖较新的数据。
-- 遗留: ann_date 在旧表中不存在，并入行该列为 NULL；end_date 用 trade_date 回填（两表口径一致）。
--       旧表 lineage 列不参与搬运，并入行沿用 stock_fundamentals 的默认值（source='tushare'）。
--       migrations/014_add_source_columns.sql（golang-migrate 目录，当前未被应用调用）仍对本表执行
--       ADD COLUMN；该目录不含 025 且版本顺序恒早于内联 migrate()，不构成执行冲突，故不修改。
-- 安全性: 幂等（DO 块守卫存在性 + INSERT..SELECT ON CONFLICT），重复执行等价于 no-op；
--         DROP 不可逆，回滚需从备份恢复（旧表结构与数据在 migration 0002 / 014 中可重建）。
-- 同源副本: 实际执行路径为 pkg/storage/postgres.go 内联 migrate()，本文件为文档副本。
-- 收口校验（人工执行，确认并入行数符合预期后再接受 DROP 生效）:
--   SELECT (SELECT count(*) FROM stock_fundamentals) AS survivor_rows;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'public' AND table_name = 'fundamentals'
    ) THEN
        INSERT INTO stock_fundamentals (
            ts_code, trade_date, end_date,
            pe, pb, ps, roe, roa, debt_to_equity, gross_margin, net_margin,
            revenue, net_profit, total_assets, total_liab
        )
        SELECT
            f.symbol, f.trade_date, f.trade_date,
            f.pe, f.pb, f.ps, f.roe, f.roa, f.debt_to_equity, f.gross_margin,
            f.net_margin, f.revenue, f.net_profit, f.total_assets, f.total_liab
        FROM fundamentals f
        ON CONFLICT (ts_code, trade_date) DO UPDATE SET
            pe             = COALESCE(stock_fundamentals.pe, EXCLUDED.pe),
            pb             = COALESCE(stock_fundamentals.pb, EXCLUDED.pb),
            ps             = COALESCE(stock_fundamentals.ps, EXCLUDED.ps),
            roe            = COALESCE(stock_fundamentals.roe, EXCLUDED.roe),
            roa            = COALESCE(stock_fundamentals.roa, EXCLUDED.roa),
            debt_to_equity = COALESCE(stock_fundamentals.debt_to_equity, EXCLUDED.debt_to_equity),
            gross_margin   = COALESCE(stock_fundamentals.gross_margin, EXCLUDED.gross_margin),
            net_margin     = COALESCE(stock_fundamentals.net_margin, EXCLUDED.net_margin),
            revenue        = COALESCE(stock_fundamentals.revenue, EXCLUDED.revenue),
            net_profit     = COALESCE(stock_fundamentals.net_profit, EXCLUDED.net_profit),
            total_assets   = COALESCE(stock_fundamentals.total_assets, EXCLUDED.total_assets),
            total_liab     = COALESCE(stock_fundamentals.total_liab, EXCLUDED.total_liab);
        DROP TABLE fundamentals;
    END IF;
END $$;