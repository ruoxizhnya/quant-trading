-- Migration 027: factor_cache 加 citation JSONB 列（TASKS.md P5-1 / ODR-061 切片 C2）
-- Date: 2026-09-16
--
-- 定位: ADR-022 §5 证据链（PRODUCT §6.3/§6.4）要求乘积侧数字可用 content_hash 回溯。
--       factor_cache 的 5 个纵向基本面因子行携带来源批次坐标 [{"content_hash":"<64hex>"}]，
--       输出面（cmd/data getFactorHandler）按 ingest.raw 展开为 §5 五元组。
--       列存 hash-only 形态（可无损升级为完整五元组），输出展开由 handler 负责。
-- 影响面: factor_cache 单表增量一列。momentum / value / quality 与历史行 citation 恒为
--         '[]' —— 显式语义「该数字的 A→B 链尚未建立」，与 NULL（无法与未采集区分）相对。
-- 安全性: PG 11+ 带默认值加列为 metadata-only 变更，不重写表，无锁表风险；
--         ADD COLUMN IF NOT EXISTS 幂等，重复执行等价于 no-op。
--         列内形态不设 CHECK 约束（可升级性优先于入库期强校验），与
--         research.conclusion.citations（JSONB 默认 '[]'）的既有口径一致。
-- 同源副本: 实际执行路径为 pkg/storage/postgres.go 内联 migrate()，本文件为文档副本。
-- 历史副本不动: 006 保持原样，记录各自时点的 DDL（迁移不可改写历史）。

ALTER TABLE factor_cache ADD COLUMN IF NOT EXISTS citation JSONB NOT NULL DEFAULT '[]'::jsonb;
