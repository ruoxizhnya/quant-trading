package expression

import (
	"fmt"
	"sort"

	aiexpr "github.com/ruoxizhnya/quant-trading/pkg/ai/expression"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ohlcvFields are the data fields extractable from domain.OHLCV bars.
// Fundamental fields (pe, pb, market_cap, etc.) are NOT available from
// OHLCV and require a separate fundamentals provider (out of scope for v1).
var ohlcvFields = map[string]bool{
	"open":     true,
	"high":     true,
	"low":      true,
	"close":    true,
	"volume":   true,
	"turnover": true,
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
}

// NewOHLCVDataProvider constructs a provider from a bars map.
//
// Symbols are sorted alphabetically so cross-sectional operations
// (cs_rank, cs_neutralize, etc.) process symbols in a deterministic
// order — critical for reproducible backtests.
func NewOHLCVDataProvider(bars map[string][]domain.OHLCV) *OHLCVDataProvider {
	symbols := make([]string, 0, len(bars))
	for s := range bars {
		symbols = append(symbols, s)
	}
	sort.Strings(symbols)
	return &OHLCVDataProvider{
		bars:    bars,
		symbols: symbols,
	}
}

// GetSymbols returns the pre-sorted symbol list.
func (p *OHLCVDataProvider) GetSymbols() []string {
	return p.symbols
}

// GetField returns the per-bar values for the requested field of the
// given symbol.
//
// Supported fields: open, high, low, close, volume, turnover.
// Fundamental fields (pe, pb, market_cap, etc.) return an error —
// they require a fundamentals provider not available in v1.
//
// If lookback > 0 and the series is longer than lookback, only the
// most recent `lookback` bars are returned. If lookback <= 0, all bars
// are returned. An unknown symbol yields an empty slice (the evaluator
// handles NaN propagation for missing symbols).
func (p *OHLCVDataProvider) GetField(symbol, field string, lookback int) ([]float64, error) {
	if !ohlcvFields[field] {
		return nil, fmt.Errorf("data_provider: field %q not available from OHLCV bars (fundamental fields require a separate provider)", field)
	}

	bars, ok := p.bars[symbol]
	if !ok {
		return []float64{}, nil
	}

	vals := make([]float64, len(bars))
	for i, bar := range bars {
		vals[i] = extractField(bar, field)
	}

	if lookback > 0 && len(vals) > lookback {
		vals = vals[len(vals)-lookback:]
	}
	return vals, nil
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
