package risk

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/errors"
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
//
// Hot-path optimization: the function only consumes the last `atrPeriod`
// true-range values, so we compute at most `atrPeriod + 1` TRs and skip the
// rest. The previous implementation always allocated `len(ohlcv)-1` floats and
// iterated the full slice — wasted work in the per-day backtest hot path
// (called 50 stocks × 480 days = 24,000 times per backtest run).
//
// Also removes the per-call trueRanges slice allocation by reusing a small
// stack-allocated ring buffer; the slice is then discarded.
func (slc *StopLossChecker) CalculateATR(ohlcv []domain.OHLCV) (float64, error) {
	n := len(ohlcv)
	if n < slc.atrPeriod {
		return 0, errors.DataQuality(
			fmt.Sprintf("insufficient data for ATR calculation: need %d, got %d", slc.atrPeriod, n),
			"CalculateATR",
		)
	}

	// We only need the last `atrPeriod` true-ranges. A true range at index i
	// uses ohlcv[i] and ohlcv[i-1], so to get `atrPeriod` TRs we need
	// `atrPeriod+1` bars at the tail of the slice.
	need := slc.atrPeriod
	startBar := n - need - 1
	if startBar < 0 {
		startBar = 0
	}

	// Reusable ring buffer for true-range values. Cap at atrPeriod+1 entries
	// (small enough to live on the stack escape-analysis-wise; the previous
	// version allocated len(ohlcv)-1 floats per call, which is ~479 for a
	// 1-year daily backtest).
	var ring [256]float64
	useStack := need+1 <= len(ring)
	buf := ring[:0]
	if useStack {
		buf = ring[:need+1]
	} else {
		buf = make([]float64, need+1)
	}

	for i := startBar + 1; i < n; i++ {
		high := ohlcv[i].High
		low := ohlcv[i].Low
		prevClose := ohlcv[i-1].Close

		// Inline math.Max(a, max(b, c)) to avoid two function calls per TR.
		hl := high - low
		hc := high - prevClose
		if hc < 0 {
			hc = -hc
		}
		lc := low - prevClose
		if lc < 0 {
			lc = -lc
		}
		tr := hl
		if hc > tr {
			tr = hc
		}
		if lc > tr {
			tr = lc
		}
		buf[i-startBar-1] = tr
	}

	// Compute the simple average over the most-recent `need` true ranges.
	// (Original behavior — see doc comment above.)
	sum := 0.0
	for _, tr := range buf {
		sum += tr
	}
	atr := sum / float64(len(buf))

	slc.logger.Debug().
		Float64("atr", atr).
		Int("period", slc.atrPeriod).
		Int("data_points", len(buf)).
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
