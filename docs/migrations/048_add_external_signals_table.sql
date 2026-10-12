-- Migration 048: quant.external_signals 外部模型信号表（TASKS.md K8 / K7 遗留①）
-- Date: 2026-10-12
--
-- 定位: K7 切片 1 冻结的外部信号契约（ExternalSignal / SignalContentHash）落库表。
--       外部模型（进程外，任意 Python/ML 框架）不直接下单，产出信号注入内核、
--       由 SignalStrategy 按 as_of 拉取消费（蓝图 §6.4）。
-- 列语义:
--       model_id  —— 外部模型标识（source）；content_hash 坐标的 source 位。
--       symbol    —— 标的（key 位）。
--       direction —— long/short/close（TEXT，与 quant.orders.direction 同型）。
--       strength  —— 强度（不约束范围，Save 层拒 NaN/±Inf）。
--       as_of     —— 信号生效交易日；「按 as_of 拉取」防前视的锚点。
--       content_hash —— sha256 可执行字段（model_id/symbol/direction/strength/
--                    as_of），UNIQUE：幂等去重 + 可追溯坐标。
--       factors   —— 诊断因子快照（JSONB，非身份，不入哈希）。
-- 影响面: 新建 quant schema（幂等）+ 一张表 + 一个消费索引，无存量数据迁移。
-- 安全性: 全部 IF NOT EXISTS 幂等，重复执行等价于 no-op。
-- 同源副本: 实际执行路径为 pkg/storage/postgres.go 内联 migrate()（Migration 048，
--       DDL 文本在 ExternalSignalsSchemaDDL），本文件为文档副本。与
--       pkg/strategy.SignalSchemaDDL（EnsureSchema 幂等兜底）归一化空白后逐条
--       相等，一致性由 pkg/strategy 的测试钉住（K8 破坏腿③）。

CREATE SCHEMA IF NOT EXISTS quant;

CREATE TABLE IF NOT EXISTS quant.external_signals (
	id           BIGSERIAL PRIMARY KEY,
	model_id     TEXT NOT NULL,
	symbol       TEXT NOT NULL,
	direction    TEXT NOT NULL,
	strength     DOUBLE PRECISION NOT NULL,
	as_of        TIMESTAMPTZ NOT NULL,
	content_hash TEXT NOT NULL,
	factors      JSONB,
	created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
	CONSTRAINT uq_external_signals_hash UNIQUE (content_hash)
);

-- 消费侧（SignalStrategy.ListFor）的查询形态：
-- WHERE model_id = $1 AND symbol = $2 AND as_of <= $3 ORDER BY as_of。
CREATE INDEX IF NOT EXISTS idx_external_signals_lookup
	ON quant.external_signals (model_id, symbol, as_of);
