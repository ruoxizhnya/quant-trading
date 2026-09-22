package domain

import (
	"encoding/json"
	"time"
)

type FactorType string

const (
	FactorMomentum   FactorType = "momentum"
	FactorValue      FactorType = "value"
	FactorQuality    FactorType = "quality"
	FactorSize       FactorType = "size"
	FactorVolatility FactorType = "volatility"
	FactorGrowth     FactorType = "growth"

	// 桥 B1 纵向基本面因子（ADR-022 计算面，EQD-P1-2）。
	// 名字逐字对齐 docs/RESEARCH.md §3.4，最长 24 字符（见迁移 026 的列宽放宽）。
	FactorGrossMarginTrend       FactorType = "gross_margin_trend"
	FactorContractLiabilityRatio FactorType = "contract_liability_ratio"
	FactorOCFToNetProfit         FactorType = "ocf_to_net_profit"
	FactorROEDuPontLeverage      FactorType = "roe_dupont_leverage"
	FactorInventoryTurnoverDelta FactorType = "inventory_turnover_delta"
)

func ParseFactorType(s string) (FactorType, bool) {
	switch s {
	case string(FactorMomentum):
		return FactorMomentum, true
	case string(FactorValue):
		return FactorValue, true
	case string(FactorQuality):
		return FactorQuality, true
	case string(FactorSize):
		return FactorSize, true
	case string(FactorVolatility):
		return FactorVolatility, true
	case string(FactorGrowth):
		return FactorGrowth, true
	case string(FactorGrossMarginTrend):
		return FactorGrossMarginTrend, true
	case string(FactorContractLiabilityRatio):
		return FactorContractLiabilityRatio, true
	case string(FactorOCFToNetProfit):
		return FactorOCFToNetProfit, true
	case string(FactorROEDuPontLeverage):
		return FactorROEDuPontLeverage, true
	case string(FactorInventoryTurnoverDelta):
		return FactorInventoryTurnoverDelta, true
	default:
		return "", false
	}
}

// 因子假设的来源种类（P2-3）。
//
// 区分它们的意义：同样是"一个能算出正 IC 的因子"，来自文献的和在数据上
// 挖出来的，可信度不是一个量级。前者有先验的因果机制，后者很可能是噪声
// 的形状 —— 换个时间段就散了。
const (
	// FactorSourceLiterature：有公开文献支撑（学术因子）。
	FactorSourceLiterature = "literature"
	// FactorSourceSupplyChain：来自产业链逻辑推导（桥 B1 纵向因子，
	// 见 ADR-022 / docs/RESEARCH.md §3.4）。
	FactorSourceSupplyChain = "supply_chain"
	// FactorSourceAI：AI 提出的假设（Hermes 挖掘链路）。
	// 这一类必须通过可证伪预测的检验才可用 —— 见 P2-9f 因果维。
	FactorSourceAI = "ai_hypothesis"
	// FactorSourceAdHoc：临时/未记录来源。能用，但别太当真。
	FactorSourceAdHoc = "ad_hoc"
)

// FactorHypothesis 记录一个因子"为什么该有效"。
//
// P2-3：因子的数值在 factor_cache 里，但它的**假设**此前没有地方记。
// 没有这条记录，就无法区分「有经济学依据」和「数据挖掘挖出来的」。
type FactorHypothesis struct {
	FactorName FactorType `json:"factor_name"`
	SourceKind string     `json:"source_kind"`
	// Hypothesis 是一句话的机制陈述：为什么这个因子应该带来超额收益。
	Hypothesis string `json:"hypothesis"`
	// Reference 是出处（文献 / 设计文档章节）。可为空 —— 没写就是没写，
	// 不填一个看起来像出处的占位。
	Reference string `json:"reference,omitempty"`
}

// BuiltinFactorHypotheses 是内置因子的假设来源。
//
// 这些不是编出来的：前六个是公开学术因子，后五个（桥 B1）出自
// ADR-022 的产业链推导，逐字对齐 docs/RESEARCH.md §3.4。
var BuiltinFactorHypotheses = []FactorHypothesis{
	{
		FactorName: FactorMomentum,
		SourceKind: FactorSourceLiterature,
		Hypothesis: "过去一段时间涨得好的股票在短期内继续跑赢：投资者的反应不足与" +
			"羊群效应使价格趋势具有惯性。",
		Reference: "Jegadeesh & Titman (1993)",
	},
	{
		FactorName: FactorValue,
		SourceKind: FactorSourceLiterature,
		Hypothesis: "估值便宜（低 PE/PB）的股票长期回报更高：市场为" +
			"基本面风险与情绪定价过度，便宜本身是一种风险补偿。",
		Reference: "Fama & French (1992, 1993)",
	},
	{
		FactorName: FactorQuality,
		SourceKind: FactorSourceLiterature,
		Hypothesis: "高 ROE / 高盈利质量公司的超额利润更具持续性，市场" +
			"对盈利质量的定价存在滞后。",
		Reference: "Piotroski (2000) F-Score；Novy-Marx (2013) 质量因子",
	},
	{
		FactorName: FactorSize,
		SourceKind: FactorSourceLiterature,
		Hypothesis: "小市值公司承担更高的流动性与破产风险，需要更高的预期回报补偿。",
		Reference:  "Banz (1981)；Fama & French (1993) SMB",
	},
	{
		FactorName: FactorVolatility,
		SourceKind: FactorSourceLiterature,
		Hypothesis: "低波动股票的回报不低（低波异象）：受约束的投资者追逐高波动" +
			"博弈性资产，压低了低波股票的相对价格。",
		Reference: "Ang et al. (2006, 2009) 低波异象",
	},
	{
		FactorName: FactorGrowth,
		SourceKind: FactorSourceLiterature,
		Hypothesis: "营收/利润持续高增长反映未被定价的竞争优势，市场外推不足。",
		Reference:  "Lakonishok, Shleifer & Vishny (1994)",
	},
	{
		FactorName: FactorGrossMarginTrend,
		SourceKind: FactorSourceSupplyChain,
		Hypothesis: "毛利率抬升说明公司在产业链上的议价能力变强（能提价或压低" +
			"上游成本），这种优势变化领先于财报总量。",
		Reference: "ADR-022 桥 B1；docs/RESEARCH.md §3.4",
	},
	{
		FactorName: FactorContractLiabilityRatio,
		SourceKind: FactorSourceSupplyChain,
		Hypothesis: "合同负债（预收）占营收上升意味着下游需求真实且提前锁单，" +
			"是订单可见度的领先指标。",
		Reference: "ADR-022 桥 B1；docs/RESEARCH.md §3.4",
	},
	{
		FactorName: FactorOCFToNetProfit,
		SourceKind: FactorSourceSupplyChain,
		Hypothesis: "经营现金流与净利润的比值低说明利润没有变成现金，" +
			"利润质量差；持续背离往往先于基本面恶化。",
		Reference: "ADR-022 桥 B1；docs/RESEARCH.md §3.4",
	},
	{
		FactorName: FactorROEDuPontLeverage,
		SourceKind: FactorSourceSupplyChain,
		Hypothesis: "把 ROE 拆成周转率、净利率、杠杆三项，能分辨高 ROE 是来自" +
			"经营效率还是单纯加杠杆 —— 后者不可持续。",
		Reference: "ADR-022 桥 B1；docs/RESEARCH.md §3.4",
	},
	{
		FactorName: FactorInventoryTurnoverDelta,
		SourceKind: FactorSourceSupplyChain,
		Hypothesis: "存货周转率变化反映供需格局：周转加快说明需求走强或" +
			"库存去化顺利，是产业链景气度的早期信号。",
		Reference: "ADR-022 桥 B1；docs/RESEARCH.md §3.4",
	},
}

// BuiltinHypothesisFor 查内置因子的假设来源。
//
// 找不到时返回 ok=false —— 调用方应当据此如实上报"来源未记录"，
// 而不是编一个。
func BuiltinHypothesisFor(ft FactorType) (FactorHypothesis, bool) {
	for _, h := range BuiltinFactorHypotheses {
		if h.FactorName == ft {
			return h, true
		}
	}
	return FactorHypothesis{}, false
}

type FactorCacheEntry struct {
	ID         int64      `json:"id"`
	Symbol     string     `json:"symbol"`
	TradeDate  time.Time  `json:"trade_date"`
	FactorName FactorType `json:"factor_name"`
	RawValue   float64    `json:"raw_value"`
	ZScore     float64    `json:"z_score"`
	Percentile float64    `json:"percentile"`
	// Citation is the stored provenance of this row's source batches, of the
	// form [{"content_hash":"<64hex>"}]. It is deliberately json.RawMessage:
	// the stored (hash-only) shape and the output shape (ADR-022 §5 five-tuple,
	// expanded by the handler) differ, and the stored form must stay losslessly
	// upgradable. Empty citation is '[]' — "no A→B chain established yet",
	// never null (ODR-061).
	Citation json.RawMessage `json:"citation,omitempty"`
}

type FactorReturn struct {
	ID               int64      `json:"id"`
	FactorName       FactorType `json:"factor_name"`
	TradeDate        time.Time  `json:"trade_date"`
	Quintile         int        `json:"quintile"`
	AvgReturn        float64    `json:"avg_return"`
	CumulativeReturn float64    `json:"cumulative_return"`
	TopMinusBot      float64    `json:"top_minus_bot"`
}

type ICEntry struct {
	ID         int64      `json:"id"`
	FactorName FactorType `json:"factor_name"`
	TradeDate  time.Time  `json:"trade_date"`
	IC         float64    `json:"ic"`
	PValue     float64    `json:"p_value"`
	TopIC      float64    `json:"top_ic"`
}
