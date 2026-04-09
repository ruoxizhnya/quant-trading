// Package plugins contains built-in strategy implementations.
package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// MultiFactorConfig holds configuration for the multi-factor strategy.
type MultiFactorConfig struct {
	ValueWeight      float64
	QualityWeight    float64
	MomentumWeight   float64
	LookbackDays     int
	RebalanceFrequency string
	TopN             int
}

type rankedStock struct {
	symbol         string
	compositeScore float64
	valueScore     float64
	qualityScore   float64
	momentumScore  float64
}

// multiFactorStrategy implements a weighted multi-factor ranking strategy.
// Factors: value (1/PE), quality (ROE), momentum (price return).
type multiFactorStrategy struct {
	name   string
	params MultiFactorConfig

	httpClient *http.Client

	// Cache: date string -> screening results
	cache      sync.Map
	cacheLimit int

	// L1 factor cache reader — set via SetFactorCache before backtest.
	// When nil, falls back to HTTP-based real-time computation.
	factorReader strategy.FactorZScoreReader
}

// Configure sets the strategy parameters.
func (s *multiFactorStrategy) Configure(params map[string]any) error {
	if s.cacheLimit == 0 {
		s.cacheLimit = 30
	}
	if v, ok := params["value_weight"]; ok {
		switch val := v.(type) {
		case float64:
			s.params.ValueWeight = val
		case int:
			s.params.ValueWeight = float64(val)
		}
	}
	if v, ok := params["quality_weight"]; ok {
		switch val := v.(type) {
		case float64:
			s.params.QualityWeight = val
		case int:
			s.params.QualityWeight = float64(val)
		}
	}
	if v, ok := params["momentum_weight"]; ok {
		switch val := v.(type) {
		case float64:
			s.params.MomentumWeight = val
		case int:
			s.params.MomentumWeight = float64(val)
		}
	}
	if v, ok := params["lookback_days"]; ok {
		switch val := v.(type) {
		case float64:
			s.params.LookbackDays = int(val)
		case int:
			s.params.LookbackDays = val
		}
	}
	if v, ok := params["top_n"]; ok {
		switch val := v.(type) {
		case float64:
			s.params.TopN = int(val)
		case int:
			s.params.TopN = val
		}
	}
	if v, ok := params["rebalance_frequency"]; ok {
		if val, ok := v.(string); ok {
			s.params.RebalanceFrequency = val
		}
	}
	return nil
}

// Weight returns position weight based on composite score.
// For multi-factor: weight proportional to composite score (capped at 0.05).
func (s *multiFactorStrategy) Weight(signal strategy.Signal, portfolioValue float64) float64 {
	baseWeight := 1.0 / float64(s.params.TopN)
	weight := signal.Strength * baseWeight
	if weight > 0.05 {
		weight = 0.05
	}
	if weight < 0.01 {
		weight = 0.01
	}
	return weight
}

// Cleanup releases resources (cache, HTTP client).
func (s *multiFactorStrategy) Cleanup() {
	s.cache.Range(func(key, value interface{}) bool {
		s.cache.Delete(key)
		return true
	})
	s.httpClient = nil
	s.params = MultiFactorConfig{}
	s.factorReader = nil
}

// SetFactorCache injects a pre-computed factor z-score reader.
// When set, GenerateSignals reads z-scores from this cache (zero-latency)
// instead of computing them via HTTP API calls on each rebalance day.
// Pass nil to clear and fall back to HTTP-based computation.
func (s *multiFactorStrategy) SetFactorCache(reader strategy.FactorZScoreReader) {
	s.factorReader = reader
}

// Name returns the strategy name.
func (s *multiFactorStrategy) Name() string {
	return "multi_factor"
}

// Description returns the strategy description.
func (s *multiFactorStrategy) Description() string {
	return "Multi-factor: rank stocks by weighted combination of value, quality, and momentum scores"
}

// Parameters returns the configurable parameters.
func (s *multiFactorStrategy) Parameters() []strategy.Parameter {
	return []strategy.Parameter{
		{
			Name:        "value_weight",
			Type:       "float",
			Default:     0.4,
			Description: "Weight for value score (1/PE), normalized",
			Min:        0.0,
			Max:        1.0,
		},
		{
			Name:        "quality_weight",
			Type:       "float",
			Default:     0.3,
			Description: "Weight for quality score (ROE), normalized",
			Min:        0.0,
			Max:        1.0,
		},
		{
			Name:        "momentum_weight",
			Type:       "float",
			Default:     0.3,
			Description: "Weight for momentum score (return), normalized",
			Min:        0.0,
			Max:        1.0,
		},
		{
			Name:        "lookback_days",
			Type:       "int",
			Default:     60,
			Description: "Number of days for momentum lookback",
			Min:        5,
			Max:        250,
		},
		{
			Name:        "top_n",
			Type:       "int",
			Default:     10,
			Description: "Number of top stocks to buy",
			Min:        1,
			Max:        50,
		},
		{
			Name:        "rebalance_frequency",
			Type:       "string",
			Default:     "monthly",
			Description: "Rebalance frequency: daily, weekly, monthly",
		},
	}
}

// isRebalanceDay checks if the given date is a rebalance day per the configured frequency.
func (s *multiFactorStrategy) isRebalanceDay(date time.Time) bool {
	switch s.params.RebalanceFrequency {
	case "weekly":
		if date.Weekday() == time.Monday {
			return true
		}
		prevDay := date.AddDate(0, 0, -1)
		if prevDay.Weekday() == time.Sunday && date.Weekday() == time.Tuesday {
			return true
		}
		if prevDay.Weekday() == time.Saturday && date.Weekday() == time.Monday {
			return true
		}
		return false
	case "monthly":
		if date.Day() == 1 {
			return true
		}
		if date.Day() <= 3 {
			prevDay := date.AddDate(0, 0, -1)
			if prevDay.Month() != date.Month() {
				return true
			}
		}
		return false
	case "daily", "":
		return true
	default:
		return true
	}
}

// callScreenAPI calls the /screen endpoint to get all stocks with fundamental data.
// An empty filter set returns stocks that have any fundamental data recorded.
func (s *multiFactorStrategy) callScreenAPI(dateStr string) ([]domain.ScreenResult, error) {
	// Check cache first
	if cached, ok := s.cache.Load(dateStr); ok {
		return cached.([]domain.ScreenResult), nil
	}

	// Request with no filters to get all stocks with available fundamentals
	reqBody := domain.ScreenRequest{
		Filters: domain.ScreenFilters{}, // Empty = no PE/PB/ROE filters
		Date:    dateStr,
		Limit:   2000, // Get a large universe for proper factor normalization
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal screen request: %w", err)
	}

	url := "http://data-service:8081/screen"
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create screen request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(httpReq.WithContext(context.Background()))
	if err != nil {
		return nil, fmt.Errorf("failed to call screen API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("screen API returned status %d", resp.StatusCode)
	}

	var result struct {
		Count   int                   `json:"count"`
		Results []domain.ScreenResult `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode screen response: %w", err)
	}

	s.cache.Store(dateStr, result.Results)

	// Simple cache eviction
	count := 0
	s.cache.Range(func(key, value any) bool {
		count++
		return true
	})
	if count > s.cacheLimit {
		s.cache.Range(func(key, value any) bool {
			s.cache.Delete(key)
			return false
		})
	}

	return result.Results, nil
}

// rankPercentile sorts the input values and returns each element's percentile rank.
// Tied values receive the same percentile rank (average of their positions).
// Uses a stable sort so that equal values preserve original relative order.
func rankPercentile(values []float64) []float64 {
	n := len(values)
	if n == 0 {
		return nil
	}

	// Create index-value pairs and sort by value (stable sort for tie-breaking)
	type iv struct {
		idx   int
		value float64
	}
	pairs := make([]iv, n)
	for i, v := range values {
		pairs[i] = iv{idx: i, value: v}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		return pairs[i].value < pairs[j].value
	})

	// Assign percentile ranks, grouping ties and assigning the average rank
	ranks := make([]float64, n)
	i := 0
	for i < n {
		// Find the range of equal values
		j := i + 1
		for j < n && pairs[j].value == pairs[i].value {
			j++
		}
		// Assign average rank (in 0-1 range) to all tied items
		avgRank := 0.0
		for k := i; k < j; k++ {
			avgRank += float64(k+1) / float64(n)
		}
		avgRank /= float64(j - i)
		for k := i; k < j; k++ {
			ranks[pairs[k].idx] = avgRank
		}
		i = j
	}

	return ranks
}

// GenerateSignals generates buy/sell signals using multi-factor ranking.
func (s *multiFactorStrategy) GenerateSignals(
	ctx context.Context,
	bars map[string][]domain.OHLCV,
	portfolio *domain.Portfolio,
) ([]strategy.Signal, error) {
	if len(bars) == 0 {
		return nil, nil
	}

	// Apply defaults
	vw := s.params.ValueWeight
	if vw <= 0 {
		vw = 0.4
	}
	qw := s.params.QualityWeight
	if qw <= 0 {
		qw = 0.3
	}
	mw := s.params.MomentumWeight
	if mw <= 0 {
		mw = 0.3
	}
	lookback := s.params.LookbackDays
	if lookback <= 0 {
		lookback = 60
	}
	topN := s.params.TopN
	if topN <= 0 {
		topN = 10
	}

	// Normalize weights to sum to 1
	totalWeight := vw + qw + mw
	if totalWeight <= 0 {
		totalWeight = 1.0
	}
	vw /= totalWeight
	qw /= totalWeight
	mw /= totalWeight

	// Determine the date for screening and rebalance check from portfolioUpdatedAt
	var screenDate time.Time
	if portfolio != nil && !portfolio.UpdatedAt.IsZero() {
		screenDate = portfolio.UpdatedAt
	} else {
		screenDate = time.Now()
	}
	screenDateStr := screenDate.Format("20060102")

	// Check rebalance frequency
	if !s.isRebalanceDay(screenDate) {
		sellSignals, err := s.generateSellSignals(bars, portfolio, lookback)
		if err != nil {
			return nil, fmt.Errorf("multi-factor sell signal generation failed: %w", err)
		}
		return sellSignals, nil
	}

	// Get all stocks with fundamentals from the /screen API
	// OR read pre-computed z-scores from L1 factor cache (zero-latency path)
	var ranked []rankedStock

	if s.factorReader != nil {
		ranked = s.generateSignalsFromFactorCache(bars, screenDate, vw, qw, mw)
	} else {
		var err error
		ranked, err = s.generateSignalsFromHTTP(ctx, bars, screenDate, screenDateStr, lookback, vw, qw, mw)
		if err != nil {
			return nil, fmt.Errorf("multi-factor screening failed: %w", err)
		}
	}

	// Sort by composite score descending
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].compositeScore > ranked[j].compositeScore
	})

	// Generate buy signals for top N
	n := len(ranked)
	if n > topN {
		n = topN
	}

	var signals []strategy.Signal
	topNSymbols := make(map[string]bool)
	for i := 0; i < n; i++ {
		// Require positive momentum for buy signals
		if ranked[i].momentumScore <= 0 {
			continue
		}
		// Require at least some fundamental score
		if ranked[i].compositeScore <= 0 {
			continue
		}

		var price float64
		if data, ok := bars[ranked[i].symbol]; ok && len(data) > 0 {
			sorted := make([]domain.OHLCV, len(data))
			copy(sorted, data)
			sort.Slice(sorted, func(i, j int) bool {
				return sorted[i].Date.Before(sorted[j].Date)
			})
			price = sorted[len(sorted)-1].Close
		}

		signals = append(signals, strategy.Signal{
			Symbol:   ranked[i].symbol,
			Action:   "buy",
			Strength: ranked[i].compositeScore,
			Price:    price,
		})
		topNSymbols[ranked[i].symbol] = true
	}

	// Generate sell signals for held positions not in top N
	if portfolio != nil {
		for symbol := range portfolio.Positions {
			if !topNSymbols[symbol] {
				var price float64
				if data, ok := bars[symbol]; ok && len(data) > 0 {
					sorted := make([]domain.OHLCV, len(data))
					copy(sorted, data)
					sort.Slice(sorted, func(i, j int) bool {
						return sorted[i].Date.Before(sorted[j].Date)
					})
					price = sorted[len(sorted)-1].Close
				}
				signals = append(signals, strategy.Signal{
					Symbol:   symbol,
					Action:   "sell",
					Strength: 1.0,
					Price:    price,
				})
			}
		}
	}

	return signals, nil
}

// generateSignalsFromFactorCache is the fast path: reads pre-computed z-scores from L1 cache.
// No HTTP calls, no percentile computation — z-scores are already normalized.
func (s *multiFactorStrategy) generateSignalsFromFactorCache(
	bars map[string][]domain.OHLCV,
	screenDate time.Time,
	vw, qw, mw float64,
) []rankedStock {
	var ranked []rankedStock
	for symbol := range bars {
		valZ, valOk := s.factorReader(domain.FactorValue, screenDate, symbol)
		quaZ, quaOk := s.factorReader(domain.FactorQuality, screenDate, symbol)
		momZ, momOk := s.factorReader(domain.FactorMomentum, screenDate, symbol)

		factorsAvailable := 0
		if valOk {
			factorsAvailable++
		}
		if quaOk {
			factorsAvailable++
		}
		if momOk {
			factorsAvailable++
		}
		if factorsAvailable == 0 {
			continue
		}

		composite := 0.0
		totalW := 0.0
		if valOk {
			composite += vw * valZ
			totalW += vw
		}
		if quaOk {
			composite += qw * quaZ
			totalW += qw
		}
		if momOk {
			composite += mw * momZ
			totalW += mw
		}
		if totalW > 0 {
			composite /= totalW
		}

		ranked = append(ranked, rankedStock{
			symbol:         symbol,
			compositeScore: composite,
			valueScore:     valZ,
			qualityScore:   quaZ,
			momentumScore:  momZ,
		})
	}
	return ranked
}

// generateSignalsFromHTTP is the fallback path: calls screen API and computes factors in-process.
func (s *multiFactorStrategy) generateSignalsFromHTTP(
	ctx context.Context,
	bars map[string][]domain.OHLCV,
	screenDate time.Time,
	screenDateStr string,
	lookback int,
	vw, qw, mw float64,
) ([]rankedStock, error) {
	screened, err := s.callScreenAPI(screenDateStr)
	if err != nil {
		return nil, fmt.Errorf("screen API call failed: %w", err)
	}

	type stockData struct {
		symbol   string
		pe       *float64
		roe      *float64
		momentum float64
		valid    bool
	}

	stockList := make([]stockData, 0, len(screened))
	for _, sr := range screened {
		sd := stockData{symbol: sr.TsCode}
		if sr.PE != nil && *sr.PE > 0 {
			sd.pe = sr.PE
		}
		if sr.ROE != nil {
			sd.roe = sr.ROE
		}
		data, ok := bars[sr.TsCode]
		if !ok || len(data) < lookback+1 {
			stockList = append(stockList, sd)
			continue
		}
		sorted := make([]domain.OHLCV, len(data))
		copy(sorted, data)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date.Before(sorted[j].Date) })
		endIdx := len(sorted) - 1
		if endIdx >= lookback {
			startPrice := sorted[endIdx-lookback].Close
			endPrice := sorted[endIdx].Close
			if startPrice > 0 {
				sd.momentum = (endPrice - startPrice) / startPrice
				sd.valid = true
			}
		}
		stockList = append(stockList, sd)
	}

	var peValues []float64
	var roeValues []float64
	var momValues []float64
	peIndices := make(map[int]bool)
	roeIndices := make(map[int]bool)
	momIndices := make(map[int]bool)

	for i, sd := range stockList {
		if sd.pe != nil {
			peValues = append(peValues, *sd.pe)
			peIndices[i] = true
		}
		if sd.roe != nil {
			roeValues = append(roeValues, *sd.roe)
			roeIndices[i] = true
		}
		if sd.valid {
			momValues = append(momValues, sd.momentum)
			momIndices[i] = true
		}
	}

	peRanks := rankPercentile(peValues)
	roeRanks := rankPercentile(roeValues)
	momRanks := rankPercentile(momValues)

	peRankIdx := 0
	roeRankIdx := 0
	momRankIdx := 0

	var ranked []rankedStock
	for i, sd := range stockList {
		var valueScore, qualityScore, momentumScore float64
		if peIndices[i] {
			valueScore = peRanks[peRankIdx]
			peRankIdx++
		}
		if roeIndices[i] {
			qualityScore = roeRanks[roeRankIdx]
			roeRankIdx++
		}
		if momIndices[i] {
			momentumScore = momRanks[momRankIdx]
			momRankIdx++
		}

		composite := vw*valueScore + qw*qualityScore + mw*momentumScore
		if !peIndices[i] && !roeIndices[i] {
			composite = momentumScore
			valueScore = 0
			qualityScore = 0
		}

		ranked = append(ranked, rankedStock{
			symbol:         sd.symbol,
			compositeScore: composite,
			valueScore:     valueScore,
			qualityScore:   qualityScore,
			momentumScore:  momentumScore,
		})
	}
	return ranked, nil
}

// generateSellSignals generates sell signals for positions with negative momentum.
func (s *multiFactorStrategy) generateSellSignals(
	bars map[string][]domain.OHLCV,
	portfolio *domain.Portfolio,
	lookback int,
) ([]strategy.Signal, error) {
	if portfolio == nil {
		return nil, nil
	}

	var signals []strategy.Signal
	for symbol := range portfolio.Positions {
		data, ok := bars[symbol]
		if !ok || len(data) < lookback+1 {
			continue
		}

		sorted := make([]domain.OHLCV, len(data))
		copy(sorted, data)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Date.Before(sorted[j].Date)
		})

		endIdx := len(sorted) - 1
		if endIdx < lookback {
			continue
		}

		startPrice := sorted[endIdx-lookback].Close
		endPrice := sorted[endIdx].Close
		if startPrice <= 0 {
			continue
		}

		momentum := (endPrice - startPrice) / startPrice
		if momentum < 0 {
			signals = append(signals, strategy.Signal{
				Symbol:   symbol,
				Action:   "sell",
				Strength: math.Min(-momentum, 1.0),
				Price:    endPrice,
			})
		}
	}

	return signals, nil
}

func init() {
	s := &multiFactorStrategy{
		name:   "multi_factor",
		params: MultiFactorConfig{
			ValueWeight:      0.4,
			QualityWeight:    0.3,
			MomentumWeight:   0.3,
			LookbackDays:     60,
			TopN:             10,
			RebalanceFrequency: "monthly",
		},
		httpClient: &http.Client{Timeout: 10 * time.Second},
		cacheLimit: 30,
	}
	strategy.GlobalRegister(s)
}
