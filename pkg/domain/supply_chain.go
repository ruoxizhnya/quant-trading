package domain

// 产业链静态数据层（P2-2，TASKS.md §五 遗留待裁决）——四类静态数据的领域类型。
//
// 定位：ADR-023 数据层 L1 的「产业链图谱」底座（公司-环节归属 / 上下游映射 /
// 行业景气 / ETF 成分），取代手抄快照。所有类型统一带 Source + AsOf 两个
// 溯源列（source=来源标识，as_of=观察时点），与 quant schema 四张表的列一一
// 对应；唯一键即幂等键（同键重导不重复插行）。
//
// 边界：本层只做静态数据的落库与只读查询。跨板块联动因子（K60）、传导指标
// 计算不在此处——那些是消费方的事。

import "time"

// RelationIndustryMember 是 supply_chain_edge 的 relation 取值之一：
// 申万行业成分映射（index_member 导入：upstream=成分股 symbol，
// downstream=行业 code）。组件→整机等其它关系用各自 relation 值，
// 同一对端点的不同关系各成一行（唯一键 upstream+downstream+relation）。
const RelationIndustryMember = "industry_member"

// SWIndustryNode 是申万行业树的一个节点（quant.sw_industry_tree）。
//
// code 为申万行业代码（唯一键，如 801080.SI）；parent_code 为父行业 code，
// 一级行业取空串（NOT NULL DEFAULT ” 哨兵，避免 NULL 比较陷阱）；
// level 为层级（1/2/3）；is_leaf 表示无子节点（由父子关系推导，不依赖
// 上游 API 字段）。
type SWIndustryNode struct {
	Code       string    `json:"code"`
	ParentCode string    `json:"parent_code"`
	Level      int       `json:"level"`
	Name       string    `json:"name"`
	IsLeaf     bool      `json:"is_leaf"`
	Source     string    `json:"source"`
	AsOf       time.Time `json:"as_of"`
}

// SupplyChainEdge 是一条上下游/环节映射（quant.supply_chain_edge）。
//
// upstream/downstream 可以是 symbol（如 600519.SH）或行业 code（如 801080.SI），
// 由 relation 说明语义：industry_member=行业成分（symbol→行业 code），
// 组件→整机等关系用各自取值。weight 语义随 relation 而定（成分关系恒 0 —
// index_member 不含权重）；唯一键 upstream+downstream+relation。
type SupplyChainEdge struct {
	Upstream   string    `json:"upstream"`
	Downstream string    `json:"downstream"`
	Relation   string    `json:"relation"`
	Weight     float64   `json:"weight"`
	Source     string    `json:"source"`
	AsOf       time.Time `json:"as_of"`
}

// IndustryVitality 是一个行业景气指标观测（quant.industry_vitality）。
//
// obs_date 为指标观察日（DATE，PIT——不同观察日各自成行）；unit 为计量单位
// （如 %/倍/亿元）；唯一键 industry_code+metric+obs_date+source（不同来源的
// 同指标各行其行，不互相覆盖）。
type IndustryVitality struct {
	IndustryCode string    `json:"industry_code"`
	Metric       string    `json:"metric"`
	ObsDate      time.Time `json:"obs_date"`
	Value        float64   `json:"value"`
	Unit         string    `json:"unit"`
	Source       string    `json:"source"`
	AsOf         time.Time `json:"as_of"`
}

// ETFConstituent 是一个 ETF 的成分持仓（quant.etf_constituent）。
//
// obs_date 为报告期（DATE，PIT——ETF 调仓后新旧报告期各自成行）；
// weight 为占净值比例（百分数，如 5.23 表示 5.23%，与 tushare fund_portfolio
// 的 stk_mkv_ratio 同口径）；唯一键 etf_code+symbol+obs_date。
type ETFConstituent struct {
	ETFCode string    `json:"etf_code"`
	Symbol  string    `json:"symbol"`
	Weight  float64   `json:"weight"`
	ObsDate time.Time `json:"obs_date"`
	Source  string    `json:"source"`
	AsOf    time.Time `json:"as_of"`
}
