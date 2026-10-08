-- ============================================================================
-- kernel_modules.schema.sql —— 模块化内核 DDL（K0 契约冻结 · 切片 1）
-- ============================================================================
--
-- 冻结范围（K0 切片 1）：
--   * 本文件只冻结 audit.message_log 一张表。
--   * 模块化内核的其余 7 张 quant.* 表（quant.portfolio_snapshot /
--     quant.positions / quant.risk_events / quant.orders / quant.fills /
--     quant.recon_report / quant.strategy_state，见 docs/SPEC.md
--     「模块化内核新表」一节）属 K0 切片 2，不在本文件。
--
-- 归属模块（冻结）：eventstore —— 该表的唯一写者是 pkg/eventstore
--（经 msgbus 派发前钩子写入，BusTap「先记录后分发」语义，D4 拍板
-- 全量落库）；禁止任何他处双写（AGENTS.md 数据归属铁律）。
--
-- 合并时机（冻结）：本文件是 K0 契约证据，不直接执行。K1 实施时
-- 将其并入 pkg/storage/postgres.go 的内联 migrate() DDL（项目加表的
-- 唯一执行路径，见 AGENTS.md §3 / migrations/README.md）；届时两者
-- 以本文件为准保持一致，本文件保留作为契约证据。
--
-- 依据：docs/design/kernel/target-architecture-modular-kernel.md
-- §5 模块矩阵（eventstore 行）+ §11 D4 拍板。
-- ============================================================================

-- 前置：audit schema 尚不存在（pkg/storage/postgres.go 现只建
-- ingest/research 两个 schema）。K1 合并时须一并加入此行；
-- 与 postgres.go 现有 CREATE SCHEMA IF NOT EXISTS 模式一致。
CREATE SCHEMA IF NOT EXISTS audit;

CREATE TABLE IF NOT EXISTS audit.message_log (
    id         BIGSERIAL PRIMARY KEY,
    ts         TIMESTAMPTZ NOT NULL,
    topic      TEXT NOT NULL,
    payload    JSONB NOT NULL,
    publisher  TEXT NOT NULL,
    run_id     TEXT
);

CREATE INDEX IF NOT EXISTS idx_message_log_ts ON audit.message_log (ts);
CREATE INDEX IF NOT EXISTS idx_message_log_topic_ts ON audit.message_log (topic, ts);
