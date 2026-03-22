package risk

import (
	"context"
	"math"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/rs/zerolog"
)

// RegimeDetector detects market regime from price data.
type RegimeDetector struct {
	fastMAPeriod  int // Fast MA period (e.g., 50-day)
	slowMAPeriod  int // Slow MA period (e.g., 200-day)
	volLookback   int // Lookback for volatility comparison
	logger        zerolog.Logger
}

// RegimeConfig holds configuration for the regime detector.
type RegimeConfig struct {
	FastMAPeriod int
	SlowMAPeriod int
	VolLookback  int
}

// NewRegimeDetector creates a new RegimeDetector with the given configuration.
func NewRegimeDetector(cfg RegimeConfig, logger zerolog.Logger) *RegimeDetector {
	return &RegimeDetector{
		fastMAPeriod:  cfg.FastMAPeriod,
		slowMAPeriod:  cfg.SlowMAPeriod,
		volLookback:   cfg.VolLookback,
		logger:        logger.With().Str("component", "regime_detector").Logger(),
	}
}

// DetectRegime detects the market regime from OHLCV data.
func (rd *RegimeDetector) DetectRegime(ctx context.Context, ohlcv []domain.OHLCV) (*domain.MarketRegime, error) {
	if len(ohlcv) < rd.slowMAPeriod {
		return nil, ErrInsufficientData
	}

	trend := rd.detectTrend(ohlcv)
	volatility := rd.detectVolatility(ohlcv)
	sentiment := rd.calculateSentiment(ohlcv, trend, volatility)

	// Determine final volatility regime based on comparison
	finalVolRegime := volatility
	if len(ohlcv) >= rd.volLookback {
		longTermVol := rd.calculateHistoricalVolatility(ohlcv, rd.volLookback)
		currentVol := rd.calculateHistoricalVolatility(ohlcv, 20)
		
		if currentVol > longTermVol*1.5 {
			finalVolRegime = "high"
		} else if currentVol < longTermVol*0.7 {
			finalVolRegime = "low"
		} else {
			finalVolRegime = "medium"
		}
	}

	regime := &domain.MarketRegime{
		Trend:      trend,
		Volatility: finalVolRegime,
		Sentiment:  sentiment,
		Timestamp:  time.Now(),
	}

	rd.logger.Info().
		Str("trend", regime.Trend).
		Str("volatility", regime.Volatility).
		Float64("sentiment", regime.Sentiment).
		Int("data_points", len(ohlcv)).
		Msg("detected market regime")

	return regime, nil
}

// detectTrend determines the trend direction by comparing fast and slow MAs.
func (rd *RegimeDetector) detectTrend(ohlcv []domain.OHLCV) string {
	if len(ohlcv) < rd.slowMAPeriod {
		return "sideways"
	}

	fastMA := rd.calculateMA(ohlcv, rd.fastMAPeriod)
	slowMA := rd.calculateMA(ohlcv, rd.slowMAPeriod)

	// Calculate MA slope over recent period
	recentPrices := make([]float64, 20)
	for i := 0; i < 20 && i < len(ohlcv); i++ {
		recentPrices[i] = ohlcv[len(ohlcv)-1-i].Close
	}
	slopeMA := rd.calculateSlope(recentPrices)

	ratio := fastMA / slowMA
	tolerance := 0.02 // 2% tolerance for sideways market

	rd.logger.Debug().
		Float64("fast_ma", fastMA).
		Float64("slow_ma", slowMA).
		Float64("ratio", ratio).
		Float64("slope", slopeMA).
		Msg("trend analysis")

	if ratio > 1+tolerance && slopeMA > 0 {
		return "bull"
	} else if ratio < 1-tolerance && slopeMA < 0 {
		return "bear"
	}
	return "sideways"
}

// detectVolatility determines volatility regime by comparing recent vol to historical.
func (rd *RegimeDetector) detectVolatility(ohlcv []domain.OHLCV) string {
	if len(ohlcv) < rd.volLookback {
		return "medium"
	}

	// Calculate 20-day volatility
	recentVol := rd.calculateHistoricalVolatility(ohlcv, 20)
	
	// Calculate long-term (252-day or available) volatility
	longTermVol := rd.calculateHistoricalVolatility(ohlcv, rd.volLookback)

	if longTermVol == 0 {
		return "medium"
	}

	ratio := recentVol / longTermVol

	rd.logger.Debug().
		Float64("recent_vol", recentVol).
		Float64("long_term_vol", longTermVol).
		Float64("ratio", ratio).
		Msg("volatility analysis")

	if ratio > 1.5 {
		return "high"
	} else if ratio < 0.7 {
		return "low"
	}
	return "medium"
}

// calculateSentiment calculates sentiment score from -1.0 to 1.0.
func (rd *RegimeDetector) calculateSentiment(ohlcv []domain.OHLCV, trend, volatility string) float64 {
	// Base sentiment from trend
	var sentiment float64
	switch trend {
	case "bull":
		sentiment = 0.5
	case "bear":
		sentiment = -0.5
	default:
		sentiment = 0.0
	}

	// Adjust based on momentum (rate of price change)
	momentum := rd.calculateMomentum(ohlcv, 20)
	if momentum > 0.05 { // 5% positive momentum
		sentiment += 0.2
	} else if momentum < -0.05 { // 5% negative momentum
		sentiment -= 0.2
	}

	// Adjust based on volatility
	switch volatility {
	case "high":
		sentiment -= 0.15 // High volatility is slightly bearish
	case "low":
		sentiment += 0.1 // Low volatility is slightly bullish
	}

	// Adjust based on trend strength
	trendStrength := rd.calculateTrendStrength(ohlcv)
	sentiment += trendStrength * 0.3

	// Clamp to [-1.0, 1.0]
	if sentiment > 1.0 {
		sentiment = 1.0
	}
	if sentiment < -1.0 {
		sentiment = -1.0
	}

	return sentiment
}

// calculateMA calculates moving average over the specified period.
func (rd *RegimeDetector) calculateMA(ohlcv []domain.OHLCV, period int) float64 {
	if len(ohlcv) < period {
		period = len(ohlcv)
	}
	
	sum := 0.0
	startIdx := len(ohlcv) - period
	for i := startIdx; i < len(ohlcv); i++ {
		sum += ohlcv[i].Close
	}
	return sum / float64(period)
}

// calculateSlope calculates the slope of price series using linear regression.
func (rd *RegimeDetector) calculateSlope(prices []float64) float64 {
	if len(prices) < 2 {
		return 0.0
	}

	n := float64(len(prices))
	sumX := 0.0
	sumY := 0.0
	sumXY := 0.0
	sumX2 := 0.0

	for i, price := range prices {
		x := float64(i)
		sumX += x
		sumY += price
		sumXY += x * price
		sumX2 += x * x
	}

	denominator := n*sumX2 - sumX*sumX
	if denominator == 0 {
		return 0.0
	}

	slope := (n*sumXY - sumX*sumY) / denominator
	
	// Normalize by average price
	avgPrice := sumY / n
	if avgPrice > 0 {
		slope = slope / avgPrice
	}

	return slope
}

// calculateHistoricalVolatility calculates annualized volatility over specified lookback.
func (rd *RegimeDetector) calculateHistoricalVolatility(ohlcv []domain.OHLCV, lookback int) float64 {
	if len(ohlcv) < lookback+1 {
		lookback = len(ohlcv) - 1
	}
	if lookback < 2 {
		return 0.0
	}

	// Calculate returns
	returns := make([]float64, lookback)
	endIdx := len(ohlcv)
	startIdx := endIdx - lookback - 1
	
	for i := startIdx + 1; i < endIdx; i++ {
		idx := i - startIdx - 1
		if ohlcv[i-1].Close > 0 {
			returns[idx] = math.Log(ohlcv[i].Close / ohlcv[i-1].Close)
		}
	}

	// Calculate standard deviation
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))

	variance := 0.0
	for _, r := range returns {
		diff := r - mean
		variance += diff * diff
	}
	variance /= float64(len(returns) - 1)

	dailyVol := math.Sqrt(variance)
	annualizedVol := dailyVol * 16.0 // sqrt(252)

	return annualizedVol
}

// calculateMomentum calculates the rate of price change over the period.
func (rd *RegimeDetector) calculateMomentum(ohlcv []domain.OHLCV, period int) float64 {
	if len(ohlcv) < period+1 {
		period = len(ohlcv) - 1
	}
	if period < 1 {
		return 0.0
	}

	currentPrice := ohlcv[len(ohlcv)-1].Close
	oldPrice := ohlcv[len(ohlcv)-1-period].Close

	if oldPrice <= 0 {
		return 0.0
	}

	return (currentPrice - oldPrice) / oldPrice
}

// calculateTrendStrength returns a value from 0 to 1 indicating trend strength.
func (rd *RegimeDetector) calculateTrendStrength(ohlcv []domain.OHLCV) float64 {
	if len(ohlcv) < rd.slowMAPeriod {
		return 0.0
	}

	fastMA := rd.calculateMA(ohlcv, rd.fastMAPeriod)
	slowMA := rd.calculateMA(ohlcv, rd.slowMAPeriod)

	if slowMA <= 0 {
		return 0.0
	}

	// Distance between MAs as percentage of price
	strength := math.Abs(fastMA-slowMA) / slowMA

	// Normalize to 0-1 range (capping at 20% difference)
	if strength > 0.2 {
		strength = 0.2
	}
	strength = strength / 0.2

	return strength
}
