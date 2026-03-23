package risk

import (
	"context"
	"errors"
	"math"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/rs/zerolog"
)

// Common errors
var (
	ErrInsufficientData = errors.New("insufficient data for calculation")
	ErrInvalidInput      = errors.New("invalid input parameters")
	ErrNoDataForSymbol   = errors.New("no data available for symbol")
)

// RiskManagerConfig holds all configuration for the risk manager.
type RiskManagerConfig struct {
	TargetVolatility    float64
	MaxPositionWeight   float64
	MinPositionWeight   float64
	ATRPeriod           int
	BaseMultiplier     float64
	BullMultiplier     float64
	BearMultiplier     float64
	SidewaysMultiplier float64
	TakeProfitMult     float64
	VolLookbackDays    int
	AnnualizationFactor float64
	FastMAPeriod        int
	SlowMAPeriod        int
	RegimeVolLookback   int
}

// RiskManager implements the domain.RiskManager interface.
type RiskManager struct {
	volatilitySizer *VolatilitySizer
	regimeDetector  *RegimeDetector
	stopLossChecker *StopLossChecker
	logger          zerolog.Logger
	config          RiskManagerConfig
}

// NewRiskManager creates a new RiskManager with all components.
func NewRiskManager(cfg RiskManagerConfig, logger zerolog.Logger) (*RiskManager, error) {
	// Initialize volatility sizer
	volConfig := VolatilityConfig{
		TargetVolatility:    cfg.TargetVolatility,
		MaxPositionWeight:   cfg.MaxPositionWeight,
		MinPositionWeight:   cfg.MinPositionWeight,
		LookbackDays:        cfg.VolLookbackDays,
		AnnualizationFactor: cfg.AnnualizationFactor,
	}
	volSizer := NewVolatilitySizer(volConfig, logger)

	// Initialize regime detector
	regimeConfig := RegimeConfig{
		FastMAPeriod: cfg.FastMAPeriod,
		SlowMAPeriod: cfg.SlowMAPeriod,
		VolLookback:  cfg.RegimeVolLookback,
	}
	regimeDetector := NewRegimeDetector(regimeConfig, logger)

	// Initialize stop loss checker
	stopLossConfig := StopLossConfig{
		ATRPeriod:          cfg.ATRPeriod,
		BaseMultiplier:     cfg.BaseMultiplier,
		BullMultiplier:     cfg.BullMultiplier,
		BearMultiplier:     cfg.BearMultiplier,
		SidewaysMultiplier: cfg.SidewaysMultiplier,
		TakeProfitMult:     cfg.TakeProfitMult,
	}
	stopLossChecker := NewStopLossChecker(stopLossConfig, logger)

	logger.Info().
		Float64("target_volatility", cfg.TargetVolatility).
		Float64("max_position_weight", cfg.MaxPositionWeight).
		Int("atr_period", cfg.ATRPeriod).
		Msg("risk manager initialized")

	return &RiskManager{
		volatilitySizer: volSizer,
		regimeDetector:  regimeDetector,
		stopLossChecker: stopLossChecker,
		logger:          logger.With().Str("component", "risk_manager").Logger(),
		config:          cfg,
	}, nil
}

// CalculatePosition calculates the appropriate position size for a signal.
func (rm *RiskManager) CalculatePosition(ctx context.Context, signal domain.Signal, portfolio *domain.Portfolio, regime *domain.MarketRegime, currentPrice float64) (domain.PositionSize, error) {
	rm.logger.Debug().
		Str("symbol", signal.Symbol).
		Str("direction", string(signal.Direction)).
		Float64("strength", signal.Strength).
		Float64("composite_score", signal.CompositeScore).
		Float64("current_price", currentPrice).
		Msg("calculating position size")

	// Validate inputs
	if signal.Symbol == "" {
		return domain.PositionSize{}, ErrInvalidInput
	}
	if portfolio == nil || portfolio.TotalValue <= 0 {
		return domain.PositionSize{}, ErrInvalidInput
	}
	if regime == nil {
		// Use neutral regime if not provided
		regime = &domain.MarketRegime{
			Trend:      "sideways",
			Volatility: "medium",
			Sentiment:  0.0,
		}
	}

	// Calculate base weight from market regime
	baseWeight := rm.calculateBaseWeightFromRegime(regime)

	// Adjust for signal strength
	weight := baseWeight * signal.Strength

	// Ensure weight is within bounds
	if weight < rm.config.MinPositionWeight {
		weight = rm.config.MinPositionWeight
	}
	if weight > rm.config.MaxPositionWeight {
		weight = rm.config.MaxPositionWeight
	}

	// Calculate position size using the provided current price
	positionValue := portfolio.TotalValue * weight
	if currentPrice <= 0 {
		currentPrice = 100.0 // Fallback if price is invalid
	}
	size := math.Floor(positionValue / currentPrice)

	// Calculate risk score (higher strength = lower risk)
	riskScore := 1.0 - signal.CompositeScore

	rm.logger.Debug().
		Str("symbol", signal.Symbol).
		Float64("weight", weight).
		Float64("size", size).
		Float64("risk_score", riskScore).
		Msg("position size calculated")

	return domain.PositionSize{
		Size:       size,
		Weight:     weight,
		StopLoss:   0, // Would be calculated with ATR
		TakeProfit: 0, // Would be calculated with ATR
		RiskScore:  riskScore,
	}, nil
}

// calculateBaseWeightFromRegime calculates base weight based on market regime.
func (rm *RiskManager) calculateBaseWeightFromRegime(regime *domain.MarketRegime) float64 {
	base := rm.config.TargetVolatility // Start with target volatility as base

	// Adjust based on volatility regime
	switch regime.Volatility {
	case "high":
		base *= 0.5
	case "low":
		base *= 1.2
	}

	// Adjust based on trend
	switch regime.Trend {
	case "bull":
		base *= 1.1
	case "bear":
		base *= 0.7
	}

	// Cap at max position weight
	if base > rm.config.MaxPositionWeight {
		base = rm.config.MaxPositionWeight
	}

	return base
}

// DetectRegime detects the current market regime from OHLCV data.
func (rm *RiskManager) DetectRegime(ctx context.Context, ohlcv []domain.OHLCV) (*domain.MarketRegime, error) {
	rm.logger.Debug().
		Int("data_points", len(ohlcv)).
		Msg("detecting market regime")

	if len(ohlcv) < rm.config.SlowMAPeriod {
		rm.logger.Error().
			Int("required", rm.config.SlowMAPeriod).
			Int("available", len(ohlcv)).
			Msg("insufficient data for regime detection")
		return nil, ErrInsufficientData
	}

	regime, err := rm.regimeDetector.DetectRegime(ctx, ohlcv)
	if err != nil {
		rm.logger.Error().Err(err).Msg("regime detection failed")
		return nil, err
	}

	rm.logger.Debug().
		Str("trend", regime.Trend).
		Str("volatility", regime.Volatility).
		Float64("sentiment", regime.Sentiment).
		Msg("market regime detected")

	return regime, nil
}

// CheckStopLoss checks positions for stop loss and take profit triggers.
func (rm *RiskManager) CheckStopLoss(ctx context.Context, positions []domain.Position, prices map[string]float64) ([]domain.StopLossEvent, error) {
	rm.logger.Debug().
		Int("positions", len(positions)).
		Int("prices", len(prices)).
		Msg("checking stop losses")

	if len(positions) == 0 {
		return nil, nil
	}
	if len(prices) == 0 {
		return nil, ErrInvalidInput
	}

	// Calculate ATR for each symbol
	// In production, this would come from cached market data
	atrData := make(map[string]float64)
	for _, pos := range positions {
		// Using a simplified ATR estimation
		// In production, this would be calculated from actual OHLCV data
		currentPrice, exists := prices[pos.Symbol]
		if !exists {
			continue
		}
		// Estimate ATR as 2% of price (rough approximation)
		atrData[pos.Symbol] = currentPrice * 0.02
	}

	// Infer regime from positions (simplified)
	regime := rm.inferRegimeFromMarket(positions, prices)

	events, err := rm.stopLossChecker.CheckStopLossWithRegime(ctx, positions, prices, atrData, regime)
	if err != nil {
		rm.logger.Error().Err(err).Msg("stop loss check failed")
		return nil, err
	}

	rm.logger.Debug().
		Int("events", len(events)).
		Msg("stop loss check completed")

	return events, nil
}

// inferRegimeFromMarket infers market regime from position performance.
func (rm *RiskManager) inferRegimeFromMarket(positions []domain.Position, prices map[string]float64) *domain.MarketRegime {
	totalPnL := 0.0
	count := 0
	
	for _, pos := range positions {
		currentPrice, exists := prices[pos.Symbol]
		if !exists {
			continue
		}
		pnlPercent := (currentPrice - pos.AvgCost) / pos.AvgCost
		totalPnL += pnlPercent
		count++
	}

	if count == 0 {
		return &domain.MarketRegime{
			Trend:      "sideways",
			Volatility: "medium",
			Sentiment:  0.0,
		}
	}

	avgPnL := totalPnL / float64(count)
	
	var trend string
	var sentiment float64
	
	if avgPnL > 0.02 {
		trend = "bull"
		sentiment = 0.5
	} else if avgPnL < -0.02 {
		trend = "bear"
		sentiment = -0.5
	} else {
		trend = "sideways"
		sentiment = 0.0
	}

	return &domain.MarketRegime{
		Trend:      trend,
		Volatility: "medium",
		Sentiment:  sentiment,
	}
}

// GetVolatilitySizer returns the volatility sizer for direct access.
func (rm *RiskManager) GetVolatilitySizer() *VolatilitySizer {
	return rm.volatilitySizer
}

// GetRegimeDetector returns the regime detector for direct access.
func (rm *RiskManager) GetRegimeDetector() *RegimeDetector {
	return rm.regimeDetector
}

// GetStopLossChecker returns the stop loss checker for direct access.
func (rm *RiskManager) GetStopLossChecker() *StopLossChecker {
	return rm.stopLossChecker
}

// GetConfig returns the current risk manager configuration.
func (rm *RiskManager) GetConfig() RiskManagerConfig {
	return rm.config
}
