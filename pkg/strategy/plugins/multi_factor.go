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

// multiFactorStrategy implements a weighted multi-factor ranking strategy.
// Factors: value (1/PE), quality (ROE), momentum (price return).
type multiFactorStrategy struct {
	name   string
	params MultiFactorConfig

	httpClient *http.Client

	// Cache: date string -> screening results
	cache      sync.Map
	cacheLimit int
}

// Configure sets the strategy parameters.
func (s *multiFactorStrategy) Configure(params map[string]any) {
	// Ensure cacheLimit has a sane default even if init() was skipped
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

	url := "http://localhost:8081/screen"
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
		sellSignals, _ := s.generateSellSignals(bars, portfolio, lookback)
		return sellSignals, nil
	}

	// Get all stocks with fundamentals from the /screen API
	screened, err := s.callScreenAPI(screenDateStr)
	if err != nil {
		return nil, fmt.Errorf("multi-factor screening failed: %w", err)
	}

	// Calculate momentum for each screened stock
	type stockData struct {
		symbol     string
		pe         *float64
		pb         *float64
		roe        *float64
		momentum   float64
		valid      bool
	}

	stockList := make([]stockData, 0, len(screened))
	for _, sr := range screened {
		sd := stockData{symbol: sr.TsCode}

		// PE and ROE from screening results
		if sr.PE != nil && *sr.PE > 0 { // Only positive PE for value scoring
			sd.pe = sr.PE
		}
		if sr.ROE != nil {
			sd.roe = sr.ROE
		}

		// Momentum from OHLCV bars
		data, ok := bars[sr.TsCode]
		if !ok || len(data) < lookback+1 {
			stockList = append(stockList, sd)
			continue
		}

		sorted := make([]domain.OHLCV, len(data))
		copy(sorted, data)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Date.Before(sorted[j].Date)
		})

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

	// Separate into groups for normalization
	var peValues []float64
	var roeValues []float64
	var momValues []float64

	peIndices := make(map[int]bool) // which stocks have valid PE
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

	// Compute percentile ranks for each factor
	peRanks := rankPercentile(peValues)   // rank among PE-valid stocks
	roeRanks := rankPercentile(roeValues) // rank among ROE-valid stocks
	momRanks := rankPercentile(momValues) // rank among momentum-valid stocks

	// Build reverse maps: stockIndex -> rankIndex
	peRankIdx := 0
	roeRankIdx := 0
	momRankIdx := 0

	type rankedStock struct {
		symbol         string
		compositeScore float64
		valueScore     float64
		qualityScore   float64
		momentumScore  float64
		momentum       float64
	}

	var ranked []rankedStock
	for i, sd := range stockList {
		var valueScore, qualityScore, momentumScore float64

		// Value score: 1/PE normalized via percentile rank among PE-valid stocks
		if peIndices[i] {
			valueScore = peRanks[peRankIdx]
			peRankIdx++
		} else {
			valueScore = 0.0 // no valid PE = no value score
		}

		// Quality score: ROE percentile rank
		if roeIndices[i] {
			qualityScore = roeRanks[roeRankIdx]
			roeRankIdx++
		} else {
			qualityScore = 0.0
		}

		// Momentum score: percentile rank
		if momIndices[i] {
			momentumScore = momRanks[momRankIdx]
			momRankIdx++
		} else {
			momentumScore = 0.0
		}

		// Composite score
		composite := vw*valueScore + qw*qualityScore + mw*momentumScore

		// For stocks with missing fundamentals, boost momentum weight
		// This ensures stocks with partial data can still be ranked
		if !peIndices[i] && !roeIndices[i] {
			// Only momentum available
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
			momentum:       sd.momentum,
		})
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
		if ranked[i].momentum <= 0 {
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
