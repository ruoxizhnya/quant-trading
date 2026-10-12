-- Migration 049: 产业链静态数据层四表（quant schema）—— TASKS.md §五 P2-2
-- Date: 2026-10-12
--
-- 定位: ADR-023 数据层 L1「产业链图谱」的静态数据底座（申万行业树 /
--       组件→整机映射 / 行业景气指标 / ETF 成分），取代手抄快照，服务未来
--       K60 跨板块联动与 ODR-052 C4「无来源单源」改造。
-- 统一约定（四张表同款）:
--       source —— 来源标识（tushare / seed-csv / 人工维护标识），NOT NULL。
--       as_of  —— 观察时点（该行数据的获取/生效时刻），NOT NULL。
--       幂等唯一约束 —— 同唯一键重导刷新行（ON CONFLICT DO UPDATE），不插新行。
-- 列语义:
--   sw_industry_tree  —— code 为申万行业代码（唯一键）；parent_code 一级行业取
--                       空串哨兵；is_leaf 由导入方按父子关系推导。
--   supply_chain_edge —— upstream/downstream 可以是 symbol 或行业 code；
--                       relation 说明语义（industry_member=申万行业成分，
--                       组件→整机等用各自取值）；weight 语义随 relation。
--   industry_vitality —— obs_date 为指标观察日（DATE，PIT）；唯一键含 source
--                       （不同来源的同指标各行其行）。
--   etf_constituent   —— obs_date 为报告期（DATE，PIT，调仓后新旧各自成行）；
--                       weight 为占净值比例（百分数，同 tushare stk_mkv_ratio）。
-- 影响面: 新建 quant schema（幂等，048 已建则顺延）+ 四张表 + 四个查询索引，
--       无存量数据迁移。
-- 安全性: 全部 IF NOT EXISTS 幂等，重复执行等价于 no-op。
-- 同源副本: 实际执行路径为 pkg/storage/postgres.go 内联 migrate()（Migration 049，
--       DDL 文本在 SupplyChainSchemaDDL），本文件为文档副本。两侧「归一化空白后
--       逐条相等」由 pkg/storage 的一致性测试钉住（破坏腿：改一处不改另一处 →
--       测试红）。

CREATE SCHEMA IF NOT EXISTS quant;

CREATE TABLE IF NOT EXISTS quant.sw_industry_tree (
	id          BIGSERIAL PRIMARY KEY,
	code        TEXT NOT NULL,
	parent_code TEXT NOT NULL DEFAULT '',
	level       INTEGER NOT NULL DEFAULT 1,
	name        TEXT NOT NULL,
	is_leaf     BOOLEAN NOT NULL DEFAULT FALSE,
	source      TEXT NOT NULL,
	as_of       TIMESTAMPTZ NOT NULL,
	created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
	CONSTRAINT uq_sw_industry_tree_code UNIQUE (code)
);

CREATE TABLE IF NOT EXISTS quant.supply_chain_edge (
	id         BIGSERIAL PRIMARY KEY,
	upstream   TEXT NOT NULL,
	downstream TEXT NOT NULL,
	relation   TEXT NOT NULL,
	weight     DOUBLE PRECISION NOT NULL DEFAULT 0,
	source     TEXT NOT NULL,
	as_of      TIMESTAMPTZ NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	CONSTRAINT uq_supply_chain_edge_triple UNIQUE (upstream, downstream, relation)
);

CREATE TABLE IF NOT EXISTS quant.industry_vitality (
	id            BIGSERIAL PRIMARY KEY,
	industry_code TEXT NOT NULL,
	metric        TEXT NOT NULL,
	obs_date      DATE NOT NULL,
	value         DOUBLE PRECISION NOT NULL,
	unit          TEXT NOT NULL DEFAULT '',
	source        TEXT NOT NULL,
	as_of         TIMESTAMPTZ NOT NULL,
	created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
	CONSTRAINT uq_industry_vitality_key UNIQUE (industry_code, metric, obs_date, source)
);

CREATE TABLE IF NOT EXISTS quant.etf_constituent (
	id        BIGSERIAL PRIMARY KEY,
	etf_code  TEXT NOT NULL,
	symbol    TEXT NOT NULL,
	weight    DOUBLE PRECISION NOT NULL DEFAULT 0,
	obs_date  DATE NOT NULL,
	source    TEXT NOT NULL,
	as_of     TIMESTAMPTZ NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	CONSTRAINT uq_etf_constituent_key UNIQUE (etf_code, symbol, obs_date)
);

-- 查询索引（对齐只读 API 的查询形态：按父+层级、按下游+关系、按行业+
-- 指标+日期、按 ETF+报告期）。
CREATE INDEX IF NOT EXISTS idx_sw_industry_tree_parent
	ON quant.sw_industry_tree (parent_code, level);

CREATE INDEX IF NOT EXISTS idx_supply_chain_edge_downstream
	ON quant.supply_chain_edge (downstream, relation);

CREATE INDEX IF NOT EXISTS idx_industry_vitality_lookup
	ON quant.industry_vitality (industry_code, metric, obs_date);

CREATE INDEX IF NOT EXISTS idx_etf_constituent_lookup
	ON quant.etf_constituent (etf_code, obs_date);
