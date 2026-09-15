-- Migration 022: fundamentals_detail (契约 C1 — EquityDeep 深财务快照落库, TASKS.md EQD-P1-1)
-- Date: 2026-09-15
--
-- 定位: ADR-022 计算面（原 ADR-021 桥 B1）的落库表。EquityDeep 侧派生的逐字段读数
--       经 ETL（`equitydeep export --format=jsonl` → Quant Lab 摄取命令）写入本表，
--       再由 FactorComputer 计算 5 个纵向基本面因子写入 factor_cache。
-- 可重建性: 可由 vault 快照 + 字段字典确定性重放（确定性 ETL），非唯一事实源；
--           数字的唯一证据坐标是 ingest.raw 的 content_hash（本表 snapshot_uri 指向之）。
-- 同源副本: 实际执行路径为 pkg/storage/postgres.go 内联 migrate()，本文件为文档副本，
--           契约副本为 contracts/fundamentals_detail.schema.sql（三处分歧以契约副本为准）。
-- 冻结语义（改动前必读，详见契约副本 header）:
--   1. 主键含 fetched_at → 支持 restatement 保留历史（同报告期多版本并存，不覆盖）。
--   2. ann_date 是 PIT 对齐硬要求 → 因子计算只用 ann_date <= D 的行，否则 look-ahead bias；
--      缺 ann_date 的读数视为不合规，不得写入本表。
--   3. raw_field_name 必须 ∈ contracts/field_dictionary.yaml 的 raw_names 白名单。
--   4. source 前缀（约定 'equitydeep:'）与既有 tushare 来源物理隔离；不复用 market.*。
--   5. 与 fundamentals / stock_fundamentals 的表重叠收口见 025（EQD-P3-1）。

CREATE TABLE IF NOT EXISTS fundamentals_detail (
    ts_code        VARCHAR(12)  NOT NULL,   -- 600519.SH
    end_date       DATE         NOT NULL,   -- 报告期
    ann_date       DATE         NOT NULL,   -- 公告日 ← PIT 对齐必需
    field_code     VARCHAR(64)  NOT NULL,   -- 规范字段码 total_revenue
    raw_field_name VARCHAR(128) NOT NULL,   -- 原始字段名 营业总收入（保溯源）
    value          NUMERIC(24,4),
    unit           VARCHAR(16)  NOT NULL,   -- CNY / percent / ratio
    source         VARCHAR(32)  NOT NULL,   -- 'equitydeep:akshare.ths'
    fetched_at     TIMESTAMPTZ  NOT NULL,   -- 快照时间
    snapshot_uri   TEXT         NOT NULL,   -- 反查原始记录（content_hash / vault 相对路径）
    PRIMARY KEY (ts_code, end_date, ann_date, field_code, fetched_at)
);

CREATE INDEX IF NOT EXISTS idx_fund_detail_lookup
    ON fundamentals_detail (ts_code, field_code, end_date DESC);