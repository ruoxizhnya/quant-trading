// Package risk implements risk management components for the quant trading system.
package risk

import (
	"context"
	"math"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/rs/zerolog"
)

// VolatilitySizer calculates position sizes based on volatility targeting.
type VolatilitySizer struct {
	targetVolatility   float64 // Target annual volatility (e.g., 0.15 for 15%)
	maxPositionWeight  float64 // Maximum position weight (e.g., 0.10 for 10%)
	minPositionWeight  float64 // Minimum position weight (e.g., 0.01 for 1%)
	lookbackDays       int     // Days for rolling volatility calculation
	annualizationFactor float64 // Factor to annualize volatility (sqrt(252))
	logger             zerolog.Logger
}

// VolatilityConfig holds configuration for the volatility sizer.
type VolatilityConfig struct {
	TargetVolatility    float64
	MaxPositionWeight   float64
	MinPositionWeight   float64
	LookbackDays        int
	AnnualizationFactor float64
}

// NewVolatilitySizer creates a new VolatilitySizer with the given configuration.
func NewVolatilitySizer(cfg VolatilityConfig, logger zerolog.Logger) *VolatilitySizer {
	return &VolatilitySizer{
		targetVolatility:    cfg.TargetVolatility,
		maxPositionWeight:   cfg.MaxPositionWeight,
		minPositionWeight:   cfg.MinPositionWeight,
		lookbackDays:        cfg.LookbackDays,
		annualizationFactor: cfg.AnnualizationFactor,
		logger:              logger.With().Str("component", "volatility_sizer").Logger(),
	}
}

// CalculateVolatility computes the annualized volatility from OHLCV data.
// Uses close-to-close returns for volatility calculation.
func (vs *VolatilitySizer) CalculateVolatility(ohlcv []domain.OHLCV) (float64, error) {
	if len(ohlcv) < vs.lookbackDays {
		return 0, ErrInsufficientData
	}

	// Calculate daily returns
	returns := make([]float64, len(ohlcv)-1)
	for i := 1; i < len(ohlcv); i++ {
		returns[i-1] = math.Log(ohlcv[i].Close / ohlcv[i-1].Close)
	}

	// Use last `lookbackDays` returns for rolling volatility
	startIdx := len(returns) - vs.lookbackDays
	if startIdx < 0 {
		startIdx = 0
	}
	recentReturns := returns[startIdx:]

	// Calculate standard deviation of returns
	mean := 0.0
	for _, r := range recentReturns {
		mean += r
	}
	mean /= float64(len(recentReturns))

	variance := 0.0
	for _, r := range recentReturns {
		diff := r - mean
		variance += diff * diff
	}
	variance /= float64(len(recentReturns) - 1) // Sample standard deviation

	dailyVol := math.Sqrt(variance)
	annualizedVol := dailyVol * vs.annualizationFactor

	vs.logger.Debug().
		Float64("daily_vol", dailyVol).
		Float64("annualized_vol", annualizedVol).
		Int("returns_count", len(recentReturns)).
		Msg("calculated volatility")

	return annualizedVol, nil
}

// CalculatePositionWeight computes the target position weight based on volatility targeting.
// Formula: position_size = target_vol / current_vol * base_weight
// The result is capped between min and max position weights.
func (vs *VolatilitySizer) CalculatePositionWeight(currentVol float64, regime *domain.MarketRegime) float64 {
	if currentVol <= 0 {
		vs.logger.Warn().Msg("invalid current volatility, using base weight")
		return vs.minPositionWeight
	}

	// Base weight is derived from target volatility allocation
	baseWeight := vs.targetVolatility / currentVol

	// Apply regime-based adjustments
	multiplier := 1.0
	if regime != nil {
		switch regime.Volatility {
		case "high":
			multiplier = 0.5 // Reduce exposure in high volatility
		case "low":
			multiplier = 1.5 // Increase exposure in low volatility
		default:
			multiplier = 1.0
		}

		// Additional adjustment based on trend
		if regime.Trend == "bear" {
			multiplier *= 0.7 // Further reduce in bear market
		} else if regime.Trend == "bull" && regime.Volatility == "low" {
			multiplier *= 1.2 // Slight increase in bull market with low vol
		}
	}

	weight := baseWeight * multiplier

	// Cap at max position weight
	if weight > vs.maxPositionWeight*2 {
		weight = vs.maxPositionWeight * 2
	}

	// Ensure minimum weight
	if weight < vs.minPositionWeight {
		weight = vs.minPositionWeight
	}

	// Cap at max position weight
	if weight > vs.maxPositionWeight {
		weight = vs.maxPositionWeight
	}

	vs.logger.Debug().
		Float64("current_vol", currentVol).
		Float64("target_vol", vs.targetVolatility).
		Float64("raw_weight", baseWeight*multiplier).
		Float64("capped_weight", weight).
		Msg("calculated position weight")

	return weight
}

// CalculateBaseWeight returns the base weight before capping.
func (vs *VolatilitySizer) CalculateBaseWeight(currentVol float64) float64 {
	if currentVol <= 0 {
		return vs.minPositionWeight
	}
	return vs.targetVolatility / currentVol
}

// GetVolatilityStats returns volatility statistics for risk reporting.
func (vs *VolatilitySizer) GetVolatilityStats(ohlcv []domain.OHLCV) (daily, annualized float64, err error) {
	if len(ohlcv) < 2 {
		return 0, 0, ErrInsufficientData
	}

	returns := make([]float64, len(ohlcv)-1)
	for i := 1; i < len(ohlcv); i++ {
		returns[i-1] = math.Log(ohlcv[i].Close / ohlcv[i-1].Close)
	}

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

	daily = math.Sqrt(variance)
	annualized = daily * vs.annualizationFactor

	return daily, annualized, nil
}

// CalculatePosition calls CalculatePositionWeight but returns a PositionSize.
func (vs *VolatilitySizer) CalculatePosition(ctx context.Context, signal domain.Signal, portfolio *domain.Portfolio, regime *domain.MarketRegime, ohlcv []domain.OHLCV) (domain.PositionSize, error) {
	currentVol, err := vs.CalculateVolatility(ohlcv)
	if err != nil {
		vs.logger.Error().Err(err).Msg("failed to calculate volatility")
		return domain.PositionSize{}, err
	}

	weight := vs.CalculatePositionWeight(currentVol, regime)

	// Adjust weight based on signal strength
	if signal.Strength > 0 && signal.Strength <= 1.0 {
		weight *= signal.Strength
	}

	// Ensure weight is within bounds
	if weight < vs.minPositionWeight {
		weight = vs.minPositionWeight
	}
	if weight > vs.maxPositionWeight {
		weight = vs.maxPositionWeight
	}

	// Calculate position size in shares
	positionValue := portfolio.TotalValue * weight
	latestPrice := ohlcv[len(ohlcv)-1].Close
	size := math.Floor(positionValue / latestPrice)

	vs.logger.Info().
		Str("symbol", signal.Symbol).
		Float64("weight", weight).
		Float64("position_value", positionValue).
		Float64("size", size).
		Msg("calculated position size")

	return domain.PositionSize{
		Size:    size,
		Weight:  weight,
		RiskScore: 1.0 - signal.Strength, // Higher strength = lower risk
	}, nil
}
