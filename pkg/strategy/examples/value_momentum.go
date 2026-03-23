// Package examples contains example strategy implementations.
package examples

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ValueMomentumConfig holds configuration for the value_momentum strategy.
type ValueMomentumConfig struct {
	Factors struct {
		PE struct {
			Weight               float64 `mapstructure:"weight"`
			PercentileThreshold  float64 `mapstructure:"percentile_threshold"`
		} `mapstructure:"pe"`
		PB struct {
			Weight               float64 `mapstructure:"weight"`
			PercentileThreshold  float64 `mapstructure:"percentile_threshold"`
		} `mapstructure:"pb"`
		Momentum struct {
			Weight        float64 `mapstructure:"weight"`
			LookbackDays  int     `mapstructure:"lookback_days"`
		} `mapstructure:"momentum"`
		Quality struct {
			Weight       float64 `mapstructure:"weight"`
			ROEThreshold float64 `mapstructure:"roe_threshold"`
		} `mapstructure:"quality"`
	} `mapstructure:"factors"`
	Filter struct {
		TopMcapPercentile  float64 `mapstructure:"top_mcap_percentile"`
		RequirePositivePE  bool    `mapstructure:"require_positive_pe"`
		RequirePositiveROE bool    `mapstructure:"require_positive_roe"`
	} `mapstructure:"filter"`
	Signal struct {
		LongThreshold  float64 `mapstructure:"long_threshold"`
		ShortThreshold float64 `mapstructure:"short_threshold"`
		MaxPositions   int     `mapstructure:"max_positions"`
	} `mapstructure:"signal"`
}

// valueMomentumStrategy implements the value_momentum multi-factor strategy.
type valueMomentumStrategy struct {
	config ValueMomentumConfig
	logger zerolog.Logger
}

// NewValueMomentumStrategy creates a new value_momentum strategy instance.
func NewValueMomentumStrategy() domain.Strategy {
	return &valueMomentumStrategy{}
}

// Name returns the strategy name.
func (s *valueMomentumStrategy) Name() string {
	return "value_momentum"
}

// Description returns the strategy description.
func (s *valueMomentumStrategy) Description() string {
	return "Multi-factor value and momentum strategy combining PE, PB, momentum, and quality factors"
}

// Configure configures the strategy with the given config map.
func (s *valueMomentumStrategy) Configure(config map[string]any) error {
	// Set defaults
	s.setDefaults()

	// Override with provided config
	if factors, ok := config["factors"].(map[string]any); ok {
		if pe, ok := factors["pe"].(map[string]any); ok {
			if w, ok := pe["weight"].(float64); ok {
				s.config.Factors.PE.Weight = w
			}
			if pt, ok := pe["percentile_threshold"].(float64); ok {
				s.config.Factors.PE.PercentileThreshold = pt
			}
		}
		if pb, ok := factors["pb"].(map[string]any); ok {
			if w, ok := pb["weight"].(float64); ok {
				s.config.Factors.PB.Weight = w
			}
			if pt, ok := pb["percentile_threshold"].(float64); ok {
				s.config.Factors.PB.PercentileThreshold = pt
			}
		}
		if momentum, ok := factors["momentum"].(map[string]any); ok {
			if w, ok := momentum["weight"].(float64); ok {
				s.config.Factors.Momentum.Weight = w
			}
			if lb, ok := momentum["lookback_days"].(float64); ok {
				s.config.Factors.Momentum.LookbackDays = int(lb)
			}
		}
		if quality, ok := factors["quality"].(map[string]any); ok {
			if w, ok := quality["weight"].(float64); ok {
				s.config.Factors.Quality.Weight = w
			}
			if rt, ok := quality["roe_threshold"].(float64); ok {
				s.config.Factors.Quality.ROEThreshold = rt
			}
		}
	}

	if filter, ok := config["filter"].(map[string]any); ok {
		if tmp, ok := filter["top_mcap_percentile"].(float64); ok {
			s.config.Filter.TopMcapPercentile = tmp
		}
		if rpe, ok := filter["require_positive_pe"].(bool); ok {
			s.config.Filter.RequirePositivePE = rpe
		}
		if rroe, ok := filter["require_positive_roe"].(bool); ok {
			s.config.Filter.RequirePositiveROE = rroe
		}
	}

	if signal, ok := config["signal"].(map[string]any); ok {
		if lt, ok := signal["long_threshold"].(float64); ok {
			s.config.Signal.LongThreshold = lt
		}
		if st, ok := signal["short_threshold"].(float64); ok {
			s.config.Signal.ShortThreshold = st
		}
		if mp, ok := signal["max_positions"].(float64); ok {
			s.config.Signal.MaxPositions = int(mp)
		}
	}

	return nil
}

// setDefaults sets default configuration values.
func (s *valueMomentumStrategy) setDefaults() {
	s.config = ValueMomentumConfig{}
	
	s.config.Factors.PE.Weight = 0.25
	s.config.Factors.PE.PercentileThreshold = 0.3
	s.config.Factors.PB.Weight = 0.25
	s.config.Factors.PB.PercentileThreshold = 0.3
	s.config.Factors.Momentum.Weight = 0.25
	s.config.Factors.Momentum.LookbackDays = 20
	s.config.Factors.Quality.Weight = 0.25
	s.config.Factors.Quality.ROEThreshold = 0.15
	
	s.config.Filter.TopMcapPercentile = 0.8
	s.config.Filter.RequirePositivePE = true
	s.config.Filter.RequirePositiveROE = true
	
	s.config.Signal.LongThreshold = 0.3
	s.config.Signal.ShortThreshold = -0.3
	s.config.Signal.MaxPositions = 20
}

// Signals generates trading signals based on the value_momentum strategy.
func (s *valueMomentumStrategy) Signals(
	ctx context.Context,
	stocks []domain.Stock,
	ohlcv map[string][]domain.OHLCV,
	fundamental map[string][]domain.Fundamental,
	date time.Time,
) ([]domain.Signal, error) {
	if s.logger.GetLevel() <= zerolog.DebugLevel {
		s.logger.Debug().
			Str("strategy", s.Name()).
			Time("date", date).
			Int("stock_count", len(stocks)).
			Msg("generating signals")
	}

	// Step 1: Filter stocks by market cap (top 80%)
	filteredStocks := s.filterByMarketCap(stocks)
	s.logger.Debug().Int("filtered_count", len(filteredStocks)).Msg("after market cap filter")

	// Step 2: Calculate factor values for each stock
	factorData := s.calculateFactors(filteredStocks, ohlcv, fundamental, date)

	// Step 3: Calculate percentile thresholds for PE and PB
	percentiles := s.calculatePercentiles(factorData)

	// Step 4: Calculate z-scores for each factor
	zScores := s.calculateZScores(factorData, percentiles)

	// Step 5: Calculate composite scores
	scores := s.calculateCompositeScores(zScores)

	// Step 6: Generate signals based on composite scores
	signals := s.generateSignals(scores, date)

	// Step 7: Sort by composite score and take top N
	sort.Slice(signals, func(i, j int) bool {
		return math.Abs(signals[i].CompositeScore) > math.Abs(signals[j].CompositeScore)
	})

	maxPositions := s.config.Signal.MaxPositions
	if maxPositions > 0 && len(signals) > maxPositions {
		signals = signals[:maxPositions]
	}

	s.logger.Debug().
		Str("strategy", s.Name()).
		Int("signal_count", len(signals)).
		Msg("signals generated")

	return signals, nil
}

// stockFactorData holds calculated factor data for a stock.
type stockFactorData struct {
	Symbol     string
	MarketCap  float64
	PE         float64
	PB         float64
	Momentum   float64
	ROE        float64
}

// percentileData holds percentile thresholds.
type percentileData struct {
	PE30th float64
	PB30th float64
}

// zScoreData holds z-scores for all factors.
type zScoreData struct {
	Symbol   string
	ZScorePE float64
	ZScorePB float64
	ZScoreMo float64
	ZScoreQu float64
}

// compositeScoreData holds composite score data.
type compositeScoreData struct {
	Symbol         string
	CompositeScore float64
	PE             float64
	PB             float64
	Momentum       float64
	ROE            float64
}

// filterByMarketCap filters stocks to top N% by market cap.
func (s *valueMomentumStrategy) filterByMarketCap(stocks []domain.Stock) []domain.Stock {
	if len(stocks) == 0 {
		return stocks
	}

	// Sort by market cap descending
	sorted := make([]domain.Stock, len(stocks))
	copy(sorted, stocks)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].MarketCap > sorted[j].MarketCap
	})

	// Take top percentile
	count := int(float64(len(sorted)) * s.config.Filter.TopMcapPercentile)
	if count < 1 {
		count = 1
	}

	return sorted[:count]
}

// calculateFactors calculates raw factor values for each stock.
func (s *valueMomentumStrategy) calculateFactors(
	stocks []domain.Stock,
	ohlcv map[string][]domain.OHLCV,
	fundamental map[string][]domain.Fundamental,
	date time.Time,
) map[string]*stockFactorData {
	data := make(map[string]*stockFactorData)

	for _, stock := range stocks {
		fd := &stockFactorData{
			Symbol:    stock.Symbol,
			MarketCap: stock.MarketCap,
		}

		// Get fundamental data
		if fundamentals, ok := fundamental[stock.Symbol]; ok && len(fundamentals) > 0 {
			// Use the most recent fundamental data before or on the date
			for i := len(fundamentals) - 1; i >= 0; i-- {
				if fundamentals[i].Date.Before(date) || fundamentals[i].Date.Equal(date) {
					fd.PE = fundamentals[i].PE
					fd.PB = fundamentals[i].PB
					fd.ROE = fundamentals[i].ROE
					break
				}
			}
		}

		// Calculate momentum
		if ohlcvData, ok := ohlcv[stock.Symbol]; ok && len(ohlcvData) > 0 {
			lookback := s.config.Factors.Momentum.LookbackDays
			if lookback <= 0 {
				lookback = 20
			}

			if len(ohlcvData) >= lookback+1 {
				// Find the data point at lookback days ago
				ohlcvSorted := make([]domain.OHLCV, len(ohlcvData))
				copy(ohlcvSorted, ohlcvData)
				sort.Slice(ohlcvSorted, func(i, j int) bool {
					return ohlcvSorted[i].Date.Before(ohlcvData[j].Date)
				})

				// Get current close and lookback close
				currentClose := ohlcvSorted[len(ohlcvSorted)-1].Close
				lookbackClose := ohlcvSorted[len(ohlcvSorted)-1-lookback].Close

				if lookbackClose > 0 {
					fd.Momentum = (currentClose - lookbackClose) / lookbackClose
				}
			}
		}

		data[stock.Symbol] = fd
	}

	return data
}

// calculatePercentiles calculates the 30th percentile for PE and PB.
func (s *valueMomentumStrategy) calculatePercentiles(data map[string]*stockFactorData) percentileData {
	peValues := make([]float64, 0)
	pbValues := make([]float64, 0)

	for _, d := range data {
		if d.PE > 0 {
			peValues = append(peValues, d.PE)
		}
		if d.PB > 0 {
			pbValues = append(pbValues, d.PB)
		}
	}

	return percentileData{
		PE30th: calculatePercentile(peValues, s.config.Factors.PE.PercentileThreshold),
		PB30th: calculatePercentile(pbValues, s.config.Factors.PB.PercentileThreshold),
	}
}

// calculateZScores calculates z-scores for each factor.
func (s *valueMomentumStrategy) calculateZScores(
	data map[string]*stockFactorData,
	percentiles percentileData,
) map[string]*zScoreData {
	zScores := make(map[string]*zScoreData)

	// Calculate z-scores for PE (inverse: lower is better)
	peMean, peStd := calculateMeanStd(data, func(d *stockFactorData) float64 { return d.PE })
	// Calculate z-scores for PB (inverse: lower is better)
	pbMean, pbStd := calculateMeanStd(data, func(d *stockFactorData) float64 { return d.PB })
	// Calculate z-scores for momentum (higher is better)
	moMean, moStd := calculateMeanStd(data, func(d *stockFactorData) float64 { return d.Momentum })
	// Calculate z-scores for quality/ROE (higher is better)
	quMean, quStd := calculateMeanStd(data, func(d *stockFactorData) float64 { return d.ROE })

	for symbol, d := range data {
		zs := &zScoreData{Symbol: symbol}

		// PE: inverse (cheaper = better, so negative z-score for high PE)
		if peStd > 0 && d.PE > 0 {
			zs.ZScorePE = -(d.PE - peMean) / peStd
		}

		// PB: inverse (cheaper = better)
		if pbStd > 0 && d.PB > 0 {
			zs.ZScorePB = -(d.PB - pbMean) / pbStd
		}

		// Momentum: higher is better
		if moStd > 0 {
			zs.ZScoreMo = (d.Momentum - moMean) / moStd
		}

		// Quality/ROE: higher is better
		if quStd > 0 {
			zs.ZScoreQu = (d.ROE - quMean) / quStd
		}

		zScores[symbol] = zs
	}

	return zScores
}

// calculateCompositeScores calculates weighted composite scores.
func (s *valueMomentumStrategy) calculateCompositeScores(
	zScores map[string]*zScoreData,
) map[string]*compositeScoreData {
	scores := make(map[string]*compositeScoreData)

	for symbol, zs := range zScores {
		score := zs.ZScorePE*s.config.Factors.PE.Weight +
			zs.ZScorePB*s.config.Factors.PB.Weight +
			zs.ZScoreMo*s.config.Factors.Momentum.Weight +
			zs.ZScoreQu*s.config.Factors.Quality.Weight

		scores[symbol] = &compositeScoreData{
			Symbol:         symbol,
			CompositeScore: score,
		}
	}

	return scores
}

// generateSignals generates trading signals from composite scores.
func (s *valueMomentumStrategy) generateSignals(
	scores map[string]*compositeScoreData,
	date time.Time,
) []domain.Signal {
	signals := make([]domain.Signal, 0)

	for symbol, sc := range scores {
		var direction domain.Direction
		var strength float64

		if sc.CompositeScore > s.config.Signal.LongThreshold {
			direction = domain.DirectionLong
			strength = math.Min(1.0, sc.CompositeScore)
		} else if sc.CompositeScore < s.config.Signal.ShortThreshold {
			direction = domain.DirectionShort
			strength = math.Min(1.0, -sc.CompositeScore)
		} else {
			direction = domain.DirectionHold
			strength = 0.0
		}

		signals = append(signals, domain.Signal{
			Symbol:         symbol,
			Date:           date, // Use the date passed to Signals()
			Direction:      direction,
			Strength:       strength,
			CompositeScore: sc.CompositeScore,
			Factors:        make(map[string]float64),
			Metadata:       make(map[string]any),
		})
	}

	return signals
}

// Weight calculates the portfolio weight for a signal.
func (s *valueMomentumStrategy) Weight(signal domain.Signal, portfolioValue float64) float64 {
	// Equal weight for now, can be enhanced with risk-based weighting
	if signal.Direction == domain.DirectionHold {
		return 0
	}

	// Base weight calculation
	baseWeight := signal.Strength * 0.05 // Max 5% per position

	return math.Max(0, math.Min(baseWeight, 0.2)) // Clamp between 0 and 20%
}

// Cleanup performs cleanup operations.
func (s *valueMomentumStrategy) Cleanup() {
	s.logger.Debug().Str("strategy", s.Name()).Msg("cleanup called")
}

// calculatePercentile calculates the nth percentile of a slice.
func calculatePercentile(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	index := percentile * float64(len(sorted)-1)
	lower := int(math.Floor(index))
	upper := int(math.Ceil(index))

	if lower == upper {
		return sorted[lower]
	}

	frac := index - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}

// calculateMeanStd calculates mean and standard deviation for a field.
func calculateMeanStd(data map[string]*stockFactorData, extractor func(*stockFactorData) float64) (mean, std float64) {
	values := make([]float64, 0, len(data))
	for _, d := range data {
		v := extractor(d)
		if !math.IsNaN(v) && !math.IsInf(v, 0) && v != 0 {
			values = append(values, v)
		}
	}

	if len(values) == 0 {
		return 0, 0
	}

	// Calculate mean
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	mean = sum / float64(len(values))

	// Calculate standard deviation
	varianceSum := 0.0
	for _, v := range values {
		diff := v - mean
		varianceSum += diff * diff
	}
	std = math.Sqrt(varianceSum / float64(len(values)))

	if std == 0 {
		std = 1 // Avoid division by zero
	}

	return mean, std
}

// Ensure interface compliance at compile time.
var _ domain.Strategy = (*valueMomentumStrategy)(nil)
