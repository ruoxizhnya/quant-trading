-- 契约 C1-b — fundamentals_detail 表（Quant Lab 侧，逐字段行存）
--
-- Status:  frozen
-- Version: 1
-- Frozen:  2026-09-15
-- Source:  docs/RESEARCH.md §3.2（契约 C1）
--
-- 本文件是**契约副本**，不是运行时可执行迁移。落地时必须三处同源：
--   1. pkg/storage/postgres.go 内联 migrate()（**实际执行路径**，Docker/本地启动时生效）
--   2. docs/migrations/022_equitydeep_fundamentals.sql（文档副本）
--   3. 本文件（契约副本）
-- 三者内容如有分歧，以本文件为准，并同时修正另外两处。
-- 触发条件：阶段 P3 的 EQD-P1-1（docs/TASKS.md）。在此之前本表不存在，
-- 任何依赖它的读取端点/摄取命令（C-3 / C-4）都不得先行落地。
--
-- 冻结语义（改动前必读）:
--   1. 主键含 fetched_at → 支持 restatement 保留历史（同一报告期可有多个版本并存，不覆盖）。
--   2. ann_date 是 PIT 对齐硬要求 → 因子计算必须用公告日而非报告期，
--      否则用未公布的财报回测，产生 look-ahead bias（RESEARCH §3.2 ⚠️ / §3.8 加固项 #6）。
--      缺 ann_date 的快照/读数视为**不合规**，不得写入本表。
--   3. raw_field_name 必须 ∈ contracts/field_dictionary.yaml 的 raw_names 白名单；
--      保留它是为了形成完整溯源链：报告脚注 → 原始响应(ingest.raw) → 本表行。
--   4. source 前缀（约定 'equitydeep:' / 'tushare'）用于与既有 tushare 来源数据物理隔离；
--      本表不复用 market.*（口径不同：本表逐字段行存 + PIT，market.* 为宽表）。
--   5. 表重叠问题的收口见 docs/migrations/025_equitydeep_field_consolidation.sql（EQD-P3-1）。

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