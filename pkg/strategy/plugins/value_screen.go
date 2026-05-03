// Package plugins contains built-in strategy implementations.
package plugins

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// ValueScreeningConfig holds configuration for the value screening strategy.
type ValueScreeningConfig struct {
	PEMax              float64
	PBMax              float64
	ROEMin             float64
	MomentumDays       int
	TopN               int
	RebalanceFrequency string
}

// valueScreeningStrategy screens stocks by value metrics (PE, PB, ROE)
// and ranks filtered stocks by price momentum.
type valueScreeningStrategy struct {
	name   string
	params ValueScreeningConfig

	httpClient     *http.Client
	screenCache    *strategy.ScreenCache
	dataServiceURL string
}

// Configure sets the strategy parameters.
func (s *valueScreeningStrategy) Configure(params map[string]any) error {
	if s.screenCache == nil {
		s.screenCache = strategy.NewScreenCache(30)
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
	return nil
}

// Weight returns position weight based on signal strength and composite score.
// For value screening: weight proportional to strength (capped at 0.05).
func (s *valueScreeningStrategy) Weight(signal strategy.Signal, portfolioValue float64) float64 {
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
func (s *valueScreeningStrategy) Cleanup() {
	s.screenCache = nil
	s.httpClient = nil
	s.params = ValueScreeningConfig{}
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
			Type:        "float",
			Default:     30.0,
			Description: "Maximum PE ratio to include",
			Min:         1.0,
			Max:         100.0,
		},
		{
			Name:        "pb_max",
			Type:        "float",
			Default:     3.0,
			Description: "Maximum PB ratio to include",
			Min:         0.1,
			Max:         20.0,
		},
		{
			Name:        "roe_min",
			Type:        "float",
			Default:     0.1,
			Description: "Minimum ROE to include (e.g., 0.1 = 10%)",
			Min:         -1.0,
			Max:         1.0,
		},
		{
			Name:        "momentum_days",
			Type:        "int",
			Default:     60,
			Description: "Number of days for momentum lookback",
			Min:         5,
			Max:         250,
		},
		{
			Name:        "top_n",
			Type:        "int",
			Default:     10,
			Description: "Number of top stocks to buy",
			Min:         1,
			Max:         50,
		},
		{
			Name:        "rebalance_frequency",
			Type:        "string",
			Default:     "monthly",
			Description: "Rebalance frequency: daily, weekly, monthly",
		},
	}
}

// callScreenAPI calls the /screen endpoint and returns filtered stock results.
func (s *valueScreeningStrategy) callScreenAPI(dateStr string) ([]domain.ScreenResult, error) {
	peMax := s.params.PEMax
	roeMin := s.params.ROEMin
	pbMax := s.params.PBMax

	req := domain.ScreenRequest{
		Filters: domain.ScreenFilters{
			PE_max:  &peMax,
			PB_max:  &pbMax,
			ROE_min: &roeMin,
		},
		Date:  dateStr,
		Limit: 500,
	}
	return strategy.CallScreenAPI(s.httpClient, s.dataServiceURL, s.screenCache, req)
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
	// For single/few stocks, use smaller lookback to ensure signals are generated
	if len(bars) <= 3 && momentumDays > 20 {
		momentumDays = 20
	}
	topN := s.params.TopN
	if topN <= 0 {
		topN = 10
	}

	// Determine the date for screening and rebalance check
	// Use the latest date from bars data (for backtesting) or portfolio.UpdatedAt or now
	var screenDate time.Time
	for _, data := range bars {
		if len(data) > 0 {
			latest := data[len(data)-1].Date
			if latest.After(screenDate) {
				screenDate = latest
			}
		}
	}
	if screenDate.IsZero() {
		if portfolio != nil && !portfolio.UpdatedAt.IsZero() {
			screenDate = portfolio.UpdatedAt
		} else {
			screenDate = time.Now()
		}
	}
	screenDateStr := screenDate.Format("20060102")

	// Check rebalance frequency
	// For single/few stocks, always rebalance to ensure signals are generated
	rebalanceFreq := s.params.RebalanceFrequency
	if len(bars) <= 3 {
		rebalanceFreq = "daily"
	}
	if !strategy.IsRebalanceDay(screenDate, rebalanceFreq) {
		// Not a rebalance day: return sell signals only for positions that
		// should be exited (momentum turned negative), no new buys
		sellSignals, err := s.generateSellSignals(bars, portfolio, momentumDays)
		if err != nil {
			return nil, fmt.Errorf("value screen sell signal generation failed: %w", err)
		}
		return sellSignals, nil
	}

	// Call the /screen API, or use bars directly for single-stock mode
	type stockMomentum struct {
		symbol   string
		momentum float64
	}
	var momResults []stockMomentum

	if len(bars) <= 3 {
		// Single/few stocks mode: compute momentum directly from bars
		for symbol, data := range bars {
			if len(data) < momentumDays+1 {
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
	} else {
		// Multi-stock mode: call screen API
		screened, err := s.callScreenAPI(screenDateStr)
		if err != nil {
			// Fallback: use bars directly
			for symbol, data := range bars {
				if len(data) < momentumDays+1 {
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
		} else {
			// Build a set of screened symbols for quick lookup
			screenedSet := make(map[string]struct{})
			for _, sr := range screened {
				screenedSet[sr.TsCode] = struct{}{}
			}

			// Calculate momentum for screened stocks
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
		}
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
		// Use absolute momentum for strength, but allow negative momentum stocks
		// to be considered (they may be value opportunities)
		strength := momResults[i].momentum
		if strength < 0 {
			strength = -strength * 0.5 // Negative momentum gets lower strength
		}
		if strength <= 0 {
			continue
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
			Strength: strength,
			Price:    price,
			Date:     screenDate,
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
					Date:     screenDate,
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
	dsURL := os.Getenv("DATA_SERVICE_URL")
	if dsURL == "" {
		dsURL = "http://localhost:8081"
	}
	s := &valueScreeningStrategy{
		name: "value_screening",
		params: ValueScreeningConfig{
			PEMax:              30.0,
			PBMax:              3.0,
			ROEMin:             0.1,
			MomentumDays:       60,
			TopN:               10,
			RebalanceFrequency: "monthly",
		},
		httpClient:     &http.Client{Timeout: 10 * time.Second},
		screenCache:    strategy.NewScreenCache(30),
		dataServiceURL: dsURL,
	}
	strategy.GlobalRegister(s)
}
