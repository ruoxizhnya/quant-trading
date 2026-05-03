package domain

import "time"

type FactorType string

const (
	FactorMomentum   FactorType = "momentum"
	FactorValue      FactorType = "value"
	FactorQuality    FactorType = "quality"
	FactorSize       FactorType = "size"
	FactorVolatility FactorType = "volatility"
	FactorGrowth     FactorType = "growth"
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
