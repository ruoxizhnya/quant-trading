// Package plugins contains built-in strategy implementations.
package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// ValueScreeningConfig holds configuration for the value screening strategy.
type ValueScreeningConfig struct {
	PEMax            float64
	PBMax            float64
	ROEMin           float64
	MomentumDays     int
	TopN             int
	RebalanceFrequency string
}

// valueScreeningStrategy screens stocks by value metrics (PE, PB, ROE)
// and ranks filtered stocks by price momentum.
type valueScreeningStrategy struct {
	name   string
	params ValueScreeningConfig

	// HTTP client for data service calls
	httpClient *http.Client

	// Cache: date string -> screening results
	// Avoids repeated API calls within the same backtest day
	cache      sync.Map
	cacheLimit int // max cached dates (default 30, set in init)
}

// Configure sets the strategy parameters.
func (s *valueScreeningStrategy) Configure(params map[string]any) {
	// Ensure cacheLimit has a sane default even if init() was skipped
	if s.cacheLimit == 0 {
		s.cacheLimit = 30
	}
	if v, ok := params["pe_max"]; ok {
		switch val := v.(type) {
		case float64:
			s.params.PEMax = val
		case int:
			s.params.PEMax = float64(val)
		}
	}
	if v, ok := params["pb_max"]; ok {
		switch val := v.(type) {
		case float64:
			s.params.PBMax = val
		case int:
			s.params.PBMax = float64(val)
		}
	}
	if v, ok := params["roe_min"]; ok {
		switch val := v.(type) {
		case float64:
			s.params.ROEMin = val
		case int:
			s.params.ROEMin = float64(val)
		}
	}
	if v, ok := params["momentum_days"]; ok {
		switch val := v.(type) {
		case float64:
			s.params.MomentumDays = int(val)
		case int:
			s.params.MomentumDays = val
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
func (s *valueScreeningStrategy) Name() string {
	return "value_screening"
}

// Description returns the strategy description.
func (s *valueScreeningStrategy) Description() string {
	return "Value screening: filter stocks by PE, PB, ROE criteria then rank by momentum"
}

// Parameters returns the configurable parameters.
func (s *valueScreeningStrategy) Parameters() []strategy.Parameter {
	return []strategy.Parameter{
		{
			Name:        "pe_max",
			Type:       "float",
			Default:     30.0,
			Description: "Maximum PE ratio to include",
			Min:        1.0,
			Max:        100.0,
		},
		{
			Name:        "pb_max",
			Type:       "float",
			Default:     3.0,
			Description: "Maximum PB ratio to include",
			Min:        0.1,
			Max:        20.0,
		},
		{
			Name:        "roe_min",
			Type:       "float",
			Default:     0.1,
			Description: "Minimum ROE to include (e.g., 0.1 = 10%)",
			Min:        -1.0,
			Max:        1.0,
		},
		{
			Name:        "momentum_days",
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
func (s *valueScreeningStrategy) isRebalanceDay(date time.Time) bool {
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

// callScreenAPI calls the /screen endpoint and returns filtered stock results.
func (s *valueScreeningStrategy) callScreenAPI(dateStr string) ([]domain.ScreenResult, error) {
	// Check cache first
	if cached, ok := s.cache.Load(dateStr); ok {
		return cached.([]domain.ScreenResult), nil
	}

	peMax := s.params.PEMax
	roeMin := s.params.ROEMin
	pbMax := s.params.PBMax

	reqBody := domain.ScreenRequest{
		Filters: domain.ScreenFilters{
			PE_max: &peMax,
			PB_max: &pbMax,
			ROE_min: &roeMin,
		},
		Date:  dateStr,
		Limit: 500, // Get up to 500 candidates for momentum ranking
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

	// Cache the result
	s.cache.Store(dateStr, result.Results)

	// Simple cache size limit: evict oldest if over limit
	count := 0
	s.cache.Range(func(key, value any) bool {
		count++
		return true
	})
	if count > s.cacheLimit {
		// Evict the oldest entry (first key found)
		s.cache.Range(func(key, value any) bool {
			s.cache.Delete(key)
			return false
		})
	}

	return result.Results, nil
}

// GenerateSignals generates buy/sell signals based on value screening + momentum ranking.
func (s *valueScreeningStrategy) GenerateSignals(
	ctx context.Context,
	bars map[string][]domain.OHLCV,
	portfolio *domain.Portfolio,
) ([]strategy.Signal, error) {
	if len(bars) == 0 {
		return nil, nil
	}

	// Apply defaults
	peMax := s.params.PEMax
	if peMax <= 0 {
		peMax = 30
	}
	pbMax := s.params.PBMax
	if pbMax <= 0 {
		pbMax = 3
	}
	roeMin := s.params.ROEMin
	if roeMin == 0 {
		roeMin = 0.1 // default 10%
	}
	momentumDays := s.params.MomentumDays
	if momentumDays <= 0 {
		momentumDays = 60
	}
	topN := s.params.TopN
	if topN <= 0 {
		topN = 10
	}

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
		// Not a rebalance day: return sell signals only for positions that
		// should be exited (momentum turned negative), no new buys
		sellSignals, _ := s.generateSellSignals(bars, portfolio, momentumDays)
		return sellSignals, nil
	}

	// Call the /screen API
	screened, err := s.callScreenAPI(screenDateStr)
	if err != nil {
		// Fallback: log and return empty signals
		// (in production you'd want proper error handling)
		return nil, fmt.Errorf("value screening failed: %w", err)
	}

	// Build a set of screened symbols for quick lookup
	screenedSet := make(map[string]struct{})
	for _, sr := range screened {
		screenedSet[sr.TsCode] = struct{}{}
	}

	// Calculate momentum for screened stocks
	type stockMomentum struct {
		symbol   string
		momentum float64
	}

	var momResults []stockMomentum
	for _, sr := range screened {
		symbol := sr.TsCode
		data, ok := bars[symbol]
		if !ok || len(data) < momentumDays+1 {
			continue
		}

		sorted := make([]domain.OHLCV, len(data))
		copy(sorted, data)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Date.Before(sorted[j].Date)
		})

		endIdx := len(sorted) - 1
		if endIdx < momentumDays {
			continue
		}

		startPrice := sorted[endIdx-momentumDays].Close
		endPrice := sorted[endIdx].Close
		if startPrice <= 0 {
			continue
		}

		momentum := (endPrice - startPrice) / startPrice

		momResults = append(momResults, stockMomentum{
			symbol:   symbol,
			momentum: momentum,
		})
	}

	// Sort by momentum descending (highest return = rank 1)
	sort.Slice(momResults, func(i, j int) bool {
		return momResults[i].momentum > momResults[j].momentum
	})

	// Generate buy signals for top N with positive momentum
	n := len(momResults)
	if n > topN {
		n = topN
	}

	var signals []strategy.Signal
	for i := 0; i < n; i++ {
		if momResults[i].momentum <= 0 {
			continue // Skip stocks with no or negative momentum
		}

		var price float64
		if data, ok := bars[momResults[i].symbol]; ok && len(data) > 0 {
			sorted := make([]domain.OHLCV, len(data))
			copy(sorted, data)
			sort.Slice(sorted, func(i, j int) bool {
				return sorted[i].Date.Before(sorted[j].Date)
			})
			price = sorted[len(sorted)-1].Close
		}

		signals = append(signals, strategy.Signal{
			Symbol:   momResults[i].symbol,
			Action:   "buy",
			Strength: momResults[i].momentum,
			Price:    price,
		})
	}

	// Generate sell signals for held positions not in top N
	if portfolio != nil {
		for symbol := range portfolio.Positions {
			// Check if this position is still in our top N
			inTopN := false
			for i := 0; i < n; i++ {
				if momResults[i].symbol == symbol {
					inTopN = true
					break
				}
			}
			// Also sell if it didn't pass screening
			if !inTopN {
				// Not in top momentum stocks, sell
				if _, ok := bars[symbol]; ok {
					var price float64
					data := bars[symbol]
					if len(data) > 0 {
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
	}

	return signals, nil
}

// generateSellSignals generates sell signals for positions with negative momentum.
// Called on non-rebalance days.
func (s *valueScreeningStrategy) generateSellSignals(
	bars map[string][]domain.OHLCV,
	portfolio *domain.Portfolio,
	momentumDays int,
) ([]strategy.Signal, error) {
	if portfolio == nil {
		return nil, nil
	}

	var signals []strategy.Signal
	for symbol := range portfolio.Positions {
		data, ok := bars[symbol]
		if !ok || len(data) < momentumDays+1 {
			continue
		}

		sorted := make([]domain.OHLCV, len(data))
		copy(sorted, data)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Date.Before(sorted[j].Date)
		})

		endIdx := len(sorted) - 1
		if endIdx < momentumDays {
			continue
		}

		startPrice := sorted[endIdx-momentumDays].Close
		endPrice := sorted[endIdx].Close
		if startPrice <= 0 {
			continue
		}

		momentum := (endPrice - startPrice) / startPrice
		if momentum < 0 {
			// Negative momentum: exit the position
			signals = append(signals, strategy.Signal{
				Symbol:   symbol,
				Action:   "sell",
				Strength: -momentum,
				Price:    endPrice,
			})
		}
	}

	return signals, nil
}

func init() {
	s := &valueScreeningStrategy{
		name:   "value_screening",
		params: ValueScreeningConfig{
			PEMax:             30.0,
			PBMax:             3.0,
			ROEMin:            0.1,
			MomentumDays:      60,
			TopN:              10,
			RebalanceFrequency: "monthly",
		},
		httpClient: &http.Client{Timeout: 10 * time.Second},
		cacheLimit: 30, // Cache up to 30 dates
	}
	strategy.GlobalRegister(s)
}
