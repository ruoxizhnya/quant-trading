package expression

import (
	"fmt"
	"math"
	"sort"

	aiexpr "github.com/ruoxizhnya/quant-trading/pkg/ai/expression"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ohlcvFields are the data fields extractable from domain.OHLCV bars.
var ohlcvFields = map[string]bool{
	"open":     true,
	"high":     true,
	"low":      true,
	"close":    true,
	"volume":   true,
	"turnover": true,
}

// fundamentalFields 是表达式里能用的基本面字段（P2-12）。
//
// 没有它们之前，value / quality 两类意图只能明确失败 —— 引擎不是"不支持"，
// 是"不给假数字"（ADR-024）。现在按 PIT 接进来：每个交易日取**该日已可用**
// 的最新一期财报，取不到就是 NaN（不是 0 —— PE 缺失被读成 0 会变成"极便宜"，
// 那是 P2-10 修掉的那个坑）。
var fundamentalFields = map[string]bool{
	"pe":  true,
	"pb":  true,
	"ps":  true,
	"roe": true,
	"roa": true,
}

// OHLCVDataProvider adapts map[string][]domain.OHLCV (the strategy
// GenerateSignals input shape) to the AI expression engine's
// aiexpr.DataProvider interface.
//
// This lets the SignalGenerator evaluate DSL expressions against the
// same bar data the strategy engine already passes to plugins, without
// a separate data fetch round-trip.
type OHLCVDataProvider struct {
	bars    map[string][]domain.OHLCV
	symbols []string // pre-sorted for deterministic cross-sectional ordering
	// fundamentals 是每股的财务记录，**按可用日升序**（见 SetFundamentals）。
	// nil = 这一路没有基本面数据，用了 pe/pb 之类会明确报错。
	fundamentals map[string][]domain.Fundamental
}

// NewOHLCVDataProvider constructs a provider from a bars map.
//
// Symbols are sorted alphabetically so cross-sectional operations
// (cs_rank, cs_neutralize, etc.) process symbols in a deterministic
// order — critical for reproducible backtests.
func NewOHLCVDataProvider(bars map[string][]domain.OHLCV) *OHLCVDataProvider {
	return NewOHLCVDataProviderWithFundamentals(bars, nil)
}

// NewOHLCVDataProviderWithFundamentals 额外带上基本面数据（P2-12）。
//
// records 里每条的 Date 必须是**可用日**（= COALESCE(ann_date, trade_date)），
// 不是报告期截止日 —— 对齐错一格就是前视偏差（见 storage.GetFundamentalsPIT）。
func NewOHLCVDataProviderWithFundamentals(
	bars map[string][]domain.OHLCV, records map[string][]domain.Fundamental,
) *OHLCVDataProvider {
	symbols := make([]string, 0, len(bars))
	for s := range bars {
		symbols = append(symbols, s)
	}
	sort.Strings(symbols)
	return &OHLCVDataProvider{
		bars:         bars,
		symbols:      symbols,
		fundamentals: records,
	}
}

// GetSymbols returns the pre-sorted symbol list.
func (p *OHLCVDataProvider) GetSymbols() []string {
	return p.symbols
}

// GetField returns the per-bar values for the requested field of the
// given symbol.
//
// Supported fields:
//   - 行情：open, high, low, close, volume, turnover
//   - 基本面（P2-12）：pe, pb, ps, roe, roa —— 需要构造时传入财报数据
//
// 基本面没传进来时，pe/pb 之类**报错而不是返回 0**：给一串 0 会让估值
// 表达式「看起来能跑」，产出的却是最危险的假信号（PE=0 = 白送的股票）。
//
// If lookback > 0 and the series is longer than lookback, only the
// most recent `lookback` bars are returned. If lookback <= 0, all bars
// are returned. An unknown symbol yields an empty slice (the evaluator
// handles NaN propagation for missing symbols).
func (p *OHLCVDataProvider) GetField(symbol, field string, lookback int) ([]float64, error) {
	bars, ok := p.bars[symbol]
	if !ok {
		return []float64{}, nil
	}

	var vals []float64
	switch {
	case ohlcvFields[field]:
		vals = make([]float64, len(bars))
		for i, bar := range bars {
			vals[i] = extractField(bar, field)
		}
	case fundamentalFields[field]:
		if p.fundamentals == nil {
			return nil, fmt.Errorf(
				"data_provider: 表达式用了基本面字段 %q，但这一路没有财报数据 —— "+
					"没有就是没有，给一串 0 只会产出看似能跑的假信号", field)
		}
		vals = p.fundamentalSeries(symbol, field, bars)
	default:
		return nil, fmt.Errorf("data_provider: unknown field %q", field)
	}

	if lookback > 0 && len(vals) > lookback {
		vals = vals[len(vals)-lookback:]
	}
	return vals, nil
}

// fundamentalSeries 把财报对齐到每根 K 线：第 i 根 K 线用「截至该日已可用」
// 的那一期。
//
// 取不到（还没披露 / 该期没披露这一项）一律 NaN —— 不是 0。
// 财报是低频数据，一期管很多天，所以是阶梯状的（forward fill 的自然结果）。
func (p *OHLCVDataProvider) fundamentalSeries(
	symbol, field string, bars []domain.OHLCV,
) []float64 {
	records := p.fundamentals[symbol]
	vals := make([]float64, len(bars))
	if len(records) == 0 {
		for i := range vals {
			vals[i] = math.NaN()
		}
		return vals
	}

	next := 0 // 下一条还没用上的记录；随 K 线日期单调前进
	for i, bar := range bars {
		vals[i] = math.NaN()
		for next < len(records) && !records[next].Date.After(bar.Date) {
			next++
		}
		if next == 0 {
			continue // 这一天的财报还没披露
		}
		v, ok := fundamentalValue(records[next-1], field)
		if !ok {
			continue // 这一期没披露这一项 —— 不知道就是不知道
		}
		vals[i] = v
	}
	return vals
}

// fundamentalValue 取一条财报里的某个字段。nil = 未披露。
//
// 估值倍数（pe / pb / ps）非正一律当作「不知道」（NaN），不返回原值。
// 理由：PE 为负不是「便宜」，是公司在亏钱；PB 为负是净资产为负。把它们
// 原样交出去，`neg(pe)` 这种排序会把亏得最狠的排成最便宜 —— 对谨慎型
// 投资者来说，这是个会让人真金白银买错东西的坑。NaN 会在 cs_rank 里被
// 排除（见 operators.csRank），既不出信号也不占排名位。
//
// 盈利率（roe / roa）可以为负且有意义 —— 「差」本身就是低排名的理由，
// 所以原样返回。
func fundamentalValue(f domain.Fundamental, field string) (float64, bool) {
	var p *float64
	// 估值倍数用 NaN 兜住非正值
	nonPositiveIsNaN := false
	switch field {
	case "pe":
		p, nonPositiveIsNaN = f.PE, true
	case "pb":
		p, nonPositiveIsNaN = f.PB, true
	case "ps":
		p, nonPositiveIsNaN = f.PS, true
	case "roe":
		p = f.ROE
	case "roa":
		p = f.ROA
	default:
		return 0, false
	}
	if p == nil {
		return 0, false
	}
	if nonPositiveIsNaN && *p <= 0 {
		return 0, false
	}
	return *p, true
}

// extractField returns the requested field from a single OHLCV bar.
// Caller must ensure field is one of ohlcvFields.
func extractField(bar domain.OHLCV, field string) float64 {
	switch field {
	case "open":
		return bar.Open
	case "high":
		return bar.High
	case "low":
		return bar.Low
	case "close":
		return bar.Close
	case "volume":
		return bar.Volume
	case "turnover":
		return bar.Turnover
	default:
		return 0
	}
}

// Compile-time interface satisfaction check.
var _ aiexpr.DataProvider = (*OHLCVDataProvider)(nil)
