-- Migration 050: 宏观指标序列表（quant schema）—— TASKS.md §五 P2-1
-- Date: 2026-10-12
--
-- 定位: 国家层宏观指标观测（CPI / PPI / PMI / M2 / 社融 / LPR / 汇率）的静态
--       数据底座，Tushare 宏观接口导入（复用 TUSHARE_TOKEN，0 新 key）。与
--       049 的 industry_vitality（行业景气）并列不重复——后者是行业维度，
--       本表是国家维度。
-- 统一约定（同 049 四表）:
--       source —— 来源标识（tushare），NOT NULL。
--       as_of  —— 观察时点（该行数据的获取/生效时刻），NOT NULL。
--       幂等唯一约束 —— 同唯一键重导刷新行（ON CONFLICT DO UPDATE），不插新行。
-- 列语义:
--   indicator —— 指标代码（CPI_YOY / PPI_YOY / PMI / M2 / M2_YOY / TSF_INC /
--                 LPR_1Y / LPR_5Y / USDCNY_CLOSE，目录见 pkg/data/macro.go
--                 的 macroIndicatorSpecs）。
--   freq      —— 频率（M=月 / D=日）。
--   obs_date  —— 观察日（DATE，观察时点；月频取当月首日，日频取自然日。fx_daily
--                 的 trade_date 为 GMT 口径，原样入库不做时区转换）。
--                 注意：**可见/PIT 过滤锚点是 as_of（导入时刻），不是 obs_date**
--                 —— 观测日早于真实发布日，见 pkg/data/source/declarations.go
--                 宏观声明（2026-10-16 用户裁决 A）。
--   value     —— 观测值；unit 为计量单位（% / 亿元 / 点 / 汇率）。
-- 影响面: 新建 quant schema（幂等，048/049 已建则顺延）+ 一张表 + 一个查询索引，
--       无存量数据迁移。
-- 安全性: 全部 IF NOT EXISTS 幂等，重复执行等价于 no-op。
-- 同源副本: 实际执行路径为 pkg/storage/postgres.go 内联 migrate()（Migration 050，
--       DDL 文本在 MacroSeriesSchemaDDL），本文件为文档副本。两侧「归一化空白后
--       逐条相等」由 pkg/storage 的一致性测试钉住（破坏腿：改一处不改另一处 →
--       测试红）。

CREATE SCHEMA IF NOT EXISTS quant;

CREATE TABLE IF NOT EXISTS quant.macro_series (
	id         BIGSERIAL PRIMARY KEY,
	indicator  TEXT NOT NULL,
	freq       TEXT NOT NULL,
	obs_date   DATE NOT NULL,
	value      DOUBLE PRECISION NOT NULL,
	unit       TEXT NOT NULL DEFAULT '',
	source     TEXT NOT NULL,
	as_of      TIMESTAMPTZ NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	CONSTRAINT uq_macro_series_key UNIQUE (indicator, freq, obs_date, source)
);

-- 查询索引（对齐只读 API 的查询形态：按指标+频率+日期区间）。
CREATE INDEX IF NOT EXISTS idx_macro_series_lookup
	ON quant.macro_series (indicator, freq, obs_date);
