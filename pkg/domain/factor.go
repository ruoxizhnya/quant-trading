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
