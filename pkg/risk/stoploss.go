package risk

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/errors"
	"github.com/rs/zerolog"
)

// StopLossChecker handles dynamic stop loss calculations using ATR.
type StopLossChecker struct {
	atrPeriod          int     // Period for ATR calculation (default 14)
	baseMultiplier     float64 // Base ATR multiplier for stop loss
	bullMultiplier     float64 // ATR multiplier for bull market (tighter)
	bearMultiplier     float64 // ATR multiplier for bear market (wider)
	sidewaysMultiplier float64 // ATR multiplier for sideways market
	takeProfitMult     float64 // ATR multiplier for take profit
	logger             zerolog.Logger
}

// StopLossConfig holds configuration for the stop loss checker.
type StopLossConfig struct {
	ATRPeriod          int
	BaseMultiplier     float64
	BullMultiplier     float64
	BearMultiplier     float64
	SidewaysMultiplier float64
	TakeProfitMult     float64
}

// NewStopLossChecker creates a new StopLossChecker with the given configuration.
func NewStopLossChecker(cfg StopLossConfig, logger zerolog.Logger) *StopLossChecker {
	return &StopLossChecker{
		atrPeriod:          cfg.ATRPeriod,
		baseMultiplier:     cfg.BaseMultiplier,
		bullMultiplier:     cfg.BullMultiplier,
		bearMultiplier:     cfg.BearMultiplier,
		sidewaysMultiplier: cfg.SidewaysMultiplier,
		takeProfitMult:     cfg.TakeProfitMult,
		logger:             logger.With().Str("component", "stop_loss_checker").Logger(),
	}
}

// CalculateATR calculates the Average True Range from OHLCV data.
func (slc *StopLossChecker) CalculateATR(ohlcv []domain.OHLCV) (float64, error) {
	if len(ohlcv) < slc.atrPeriod {
		return 0, errors.DataQuality(
			fmt.Sprintf("insufficient data for ATR calculation: need %d, got %d", slc.atrPeriod, len(ohlcv)),
			"CalculateATR",
		)
	}

	trueRanges := make([]float64, len(ohlcv)-1)
	
	for i := 1; i < len(ohlcv); i++ {
		high := ohlcv[i].High
		low := ohlcv[i].Low
		prevClose := ohlcv[i-1].Close

		tr := math.Max(high-low, math.Max(
			math.Abs(high-prevClose),
			math.Abs(low-prevClose),
		))
		trueRanges[i-1] = tr
	}

	// Use last `atrPeriod` values for calculation
	startIdx := len(trueRanges) - slc.atrPeriod
	if startIdx < 0 {
		startIdx = 0
	}
	recentTR := trueRanges[startIdx:]

	// Calculate smoothed ATR (similar to Wilder's smoothing)
	atr := 0.0
	if len(recentTR) > 0 {
		// Initial ATR is simple average of first atrPeriod true ranges
		sum := 0.0
		for _, tr := range recentTR {
			sum += tr
		}
		atr = sum / float64(len(recentTR))
	}

	slc.logger.Debug().
		Float64("atr", atr).
		Int("period", slc.atrPeriod).
		Int("data_points", len(recentTR)).
		Msg("calculated ATR")

	return atr, nil
}

// CalculateStopLoss calculates the stop loss price based on entry price and ATR.
// Note: This returns the entry price as a placeholder. Use CalculateStopLossPrice with actual ATR.
func (slc *StopLossChecker) CalculateStopLoss(entryPrice float64, regime *domain.MarketRegime) float64 {
	// This method is kept for interface compatibility
	// Actual stop loss calculation requires ATR which is passed in CheckStopLoss
	_ = slc.getStopLossMultiplier(regime)
	return entryPrice
}

// getStopLossMultiplier returns the appropriate ATR multiplier based on market regime.
func (slc *StopLossChecker) getStopLossMultiplier(regime *domain.MarketRegime) float64 {
	if regime == nil {
		return slc.baseMultiplier
	}

	switch regime.Trend {
	case "bull":
		return slc.bullMultiplier
	case "bear":
		return slc.bearMultiplier
	default:
		return slc.sidewaysMultiplier
	}
}

// CalculateStopLossPrice calculates the actual stop loss price given entry price and ATR.
func (slc *StopLossChecker) CalculateStopLossPrice(entryPrice, atr float64, regime *domain.MarketRegime) float64 {
	multiplier := slc.getStopLossMultiplier(regime)
	return entryPrice - (multiplier * atr)
}

// CalculateTakeProfitPrice calculates the take profit price given entry price and ATR.
func (slc *StopLossChecker) CalculateTakeProfitPrice(entryPrice, atr float64, regime *domain.MarketRegime) float64 {
	return entryPrice + (slc.takeProfitMult * atr)
}

// GetStopLossLevels returns both stop loss and take profit levels.
func (slc *StopLossChecker) GetStopLossLevels(entryPrice, atr float64, regime *domain.MarketRegime) (stopLoss, takeProfit float64) {
	stopLoss = slc.CalculateStopLossPrice(entryPrice, atr, regime)
	takeProfit = slc.CalculateTakeProfitPrice(entryPrice, atr, regime)
	return
}

// CheckStopLoss checks all positions against current prices and returns triggered stop loss events.
func (slc *StopLossChecker) CheckStopLoss(ctx context.Context, positions []domain.Position, prices map[string]float64, atrData map[string]float64) ([]domain.StopLossEvent, error) {
	var events []domain.StopLossEvent

	for _, pos := range positions {
		if pos.Quantity <= 0 {
			continue
		}

		currentPrice, exists := prices[pos.Symbol]
		if !exists {
			slc.logger.Warn().Str("symbol", pos.Symbol).Msg("no current price available")
			continue
		}

		atr := atrData[pos.Symbol]
		if atr <= 0 {
			// If ATR is not available, use a default percentage-based approach
			slc.logger.Warn().Str("symbol", pos.Symbol).Msg("no ATR data available, using default")
			atr = pos.AvgCost * 0.02 // 2% of entry price as default ATR
		}

		// Determine regime (would be passed in or fetched - using a simplified approach)
		// For now, we use a default regime
		// In production, this would come from the regime detector
		regime := &domain.MarketRegime{
			Trend:      slc.inferTrend(pos, currentPrice),
			Volatility: slc.inferVolatility(pos, atr),
		}

		stopLossPrice := slc.CalculateStopLossPrice(pos.AvgCost, atr, regime)
		takeProfitPrice := slc.CalculateTakeProfitPrice(pos.AvgCost, atr, regime)

		slc.logger.Debug().
			Str("symbol", pos.Symbol).
			Float64("entry_price", pos.AvgCost).
			Float64("current_price", currentPrice).
			Float64("stop_loss", stopLossPrice).
			Float64("take_profit", takeProfitPrice).
			Float64("atr", atr).
			Msg("checking stop loss levels")

		// Check for stop loss trigger (price below stop loss for long positions)
		if currentPrice <= stopLossPrice && stopLossPrice > 0 {
			event := domain.StopLossEvent{
				Symbol:   pos.Symbol,
				Type:     "stop_loss",
				Price:    stopLossPrice,
				Quantity: pos.Quantity,
				Reason:   "price dropped below stop loss level",
			}
			events = append(events, event)
			
			slc.logger.Info().
				Str("symbol", pos.Symbol).
				Float64("trigger_price", stopLossPrice).
				Float64("current_price", currentPrice).
				Msg("stop loss triggered")
		}

		// Check for take profit trigger (price above take profit for long positions)
		if currentPrice >= takeProfitPrice && takeProfitPrice > 0 {
			event := domain.StopLossEvent{
				Symbol:   pos.Symbol,
				Type:     "take_profit",
				Price:    takeProfitPrice,
				Quantity: pos.Quantity,
				Reason:   "price reached take profit level",
			}
			events = append(events, event)
			
			slc.logger.Info().
				Str("symbol", pos.Symbol).
				Float64("trigger_price", takeProfitPrice).
				Float64("current_price", currentPrice).
				Msg("take profit triggered")
		}
	}

	return events, nil
}

// CheckStopLossWithRegime checks positions with explicit regime information.
func (slc *StopLossChecker) CheckStopLossWithRegime(ctx context.Context, positions []domain.Position, prices map[string]float64, atrData map[string]float64, regime *domain.MarketRegime) ([]domain.StopLossEvent, error) {
	var events []domain.StopLossEvent

	for _, pos := range positions {
		if pos.Quantity <= 0 {
			continue
		}

		currentPrice, exists := prices[pos.Symbol]
		if !exists {
			slc.logger.Warn().Str("symbol", pos.Symbol).Msg("no current price available")
			continue
		}

		atr := atrData[pos.Symbol]
		if atr <= 0 {
			atr = pos.AvgCost * 0.02 // Default 2% if ATR unavailable
		}

		stopLossPrice := slc.CalculateStopLossPrice(pos.AvgCost, atr, regime)
		takeProfitPrice := slc.CalculateTakeProfitPrice(pos.AvgCost, atr, regime)

		// Check for stop loss trigger
		if currentPrice <= stopLossPrice && stopLossPrice > 0 {
			event := domain.StopLossEvent{
				Symbol:   pos.Symbol,
				Type:     "stop_loss",
				Price:    stopLossPrice,
				Quantity: pos.Quantity,
				Reason:   "price dropped below stop loss level",
			}
			events = append(events, event)
		}

		// Check for take profit trigger
		if currentPrice >= takeProfitPrice && takeProfitPrice > 0 {
			event := domain.StopLossEvent{
				Symbol:   pos.Symbol,
				Type:     "take_profit",
				Price:    takeProfitPrice,
				Quantity: pos.Quantity,
				Reason:   "price reached take profit level",
			}
			events = append(events, event)
		}
	}

	return events, nil
}

// inferTrend infers the current trend based on position state.
func (slc *StopLossChecker) inferTrend(pos domain.Position, currentPrice float64) string {
	// If current price is significantly above entry, likely bull
	// If below, likely bear
	pnlPercent := (currentPrice - pos.AvgCost) / pos.AvgCost
	
	if pnlPercent > 0.05 {
		return "bull"
	} else if pnlPercent < -0.05 {
		return "bear"
	}
	return "sideways"
}

// inferVolatility infers volatility regime from ATR relative to price.
func (slc *StopLossChecker) inferVolatility(pos domain.Position, atr float64) string {
	if pos.AvgCost <= 0 || atr <= 0 {
		return "medium"
	}
	
	atrPercent := atr / pos.AvgCost
	
	if atrPercent > 0.03 {
		return "high"
	} else if atrPercent < 0.015 {
		return "low"
	}
	return "medium"
}

// ATRFromOHLCV calculates ATR values for multiple symbols from OHLCV data.
func (slc *StopLossChecker) ATRFromOHLCV(ohlcvData map[string][]domain.OHLCV) (map[string]float64, error) {
	atrData := make(map[string]float64)
	
	for symbol, ohlcv := range ohlcvData {
		atr, err := slc.CalculateATR(ohlcv)
		if err != nil {
			slc.logger.Warn().Err(err).Str("symbol", symbol).Msg("failed to calculate ATR")
			continue
		}
		atrData[symbol] = atr
	}
	
	return atrData, nil
}

// GetLastUpdateTime returns the timestamp of the most recent data.
func (slc *StopLossChecker) GetLastUpdateTime(ohlcv []domain.OHLCV) time.Time {
	if len(ohlcv) == 0 {
		return time.Time{}
	}
	return ohlcv[len(ohlcv)-1].Date
}
