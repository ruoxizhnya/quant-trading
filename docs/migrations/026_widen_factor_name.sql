-- Migration 026: factor_name 列宽 VARCHAR(20) → VARCHAR(32)（TASKS.md EQD-P1-2 / 桥 B1）
-- Date: 2026-09-15
--
-- 定位: 桥 B1 新增 5 个纵向基本面因子（ADR-022 计算面），其中
--       contract_liability_ratio / roe_dupont_leverage / inventory_turnover_delta
--       三个名字长 23~24 字符，超出既有 VARCHAR(20)，写入会直接报错。
--       故在因子链路的三个表上统一放宽到 VARCHAR(32)。
-- 影响面: factor_cache（写入）/ factor_returns（IC 分层归因）/ ic_analysis（IC 统计）
--         三表同源同一列，必须同时放宽，否则 IC 与归因链路会在新因子名上失败。
-- 安全性: Postgres 加宽 varchar 为 metadata-only 变更，不重写表，无锁表风险；
--         ALTER 语句幂等，重复执行等价于 no-op。
-- 同源副本: 实际执行路径为 pkg/storage/postgres.go 内联 migrate()，本文件为文档副本。
-- 历史副本不动: 006 / 008 / 024 保持原样，记录各自时点的 DDL（迁移不可改写历史）。

ALTER TABLE factor_cache  ALTER COLUMN factor_name TYPE VARCHAR(32);
ALTER TABLE factor_returns ALTER COLUMN factor_name TYPE VARCHAR(32);
ALTER TABLE ic_analysis   ALTER COLUMN factor_name TYPE VARCHAR(32);