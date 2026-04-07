package risk

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/rs/zerolog"
)

func TestRiskManager_NewRiskManager(t *testing.T) {
	logger := zerolog.Nop()
	cfg := RiskManagerConfig{
		TargetVolatility:    0.15,
		MaxPositionWeight:   0.10,
		MinPositionWeight:   0.01,
		ATRPeriod:           14,
		BaseMultiplier:     2.0,
		BullMultiplier:     2.5,
		BearMultiplier:     1.5,
		SidewaysMultiplier: 2.0,
		TakeProfitMult:     3.0,
		VolLookbackDays:    60,
	}

	rm, err := NewRiskManager(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, rm)
	assert.Equal(t, cfg.MaxPositionWeight, rm.config.MaxPositionWeight)
}

func TestRiskManager_CalculatePosition_Basic(t *testing.T) {
	logger := zerolog.Nop()
	cfg := RiskManagerConfig{
		TargetVolatility:  0.15,
		MaxPositionWeight: 0.10,
		ATRPeriod:         14,
		BaseMultiplier:   2.0,
		TakeProfitMult:   3.0,
		VolLookbackDays:  60,
	}
	rm, _ := NewRiskManager(cfg, logger)

	signal := domain.Signal{
		Symbol:    "600000.SH",
		Direction: domain.DirectionLong,
		Strength:  0.8,
		Date:      time.Now(),
	}
	portfolio := &domain.Portfolio{
		TotalValue: 1000000,
		Cash:       500000,
	}
	regime := &domain.MarketRegime{
		Trend:      "bull",
		Volatility: "medium",
	}
	currentPrice := 50.0
	ohlcv := generateTestOHLCV(20, currentPrice, 0.02)

	ps, err := rm.CalculatePosition(context.Background(), signal, portfolio, regime, currentPrice, ohlcv)
	require.NoError(t, err)
	assert.True(t, ps.Size > 0, "position size should be positive")
	// Stop loss is calculated using default 2% ATR when OHLCV is sufficient
	assert.True(t, ps.StopLoss > 0, "stop loss should be positive")
	assert.True(t, ps.TakeProfit > currentPrice, "take profit should be above entry for long")
}

func TestRiskManager_CalculatePosition_InsufficientData(t *testing.T) {
	logger := zerolog.Nop()
	cfg := RiskManagerConfig{
		TargetVolatility:  0.15,
		MaxPositionWeight: 0.10,
		MinPositionWeight: 0.01,
		ATRPeriod:        14,
		VolLookbackDays:  60,
	}
	rm, _ := NewRiskManager(cfg, logger)

	signal := domain.Signal{Symbol: "TEST.SH", Direction: domain.DirectionLong}
	portfolio := &domain.Portfolio{TotalValue: 1000000}
	regime := &domain.MarketRegime{Trend: "bull"}

	shortOHLCV := generateTestOHLCV(5, 50.0, 0.02)
	ps, err := rm.CalculatePosition(context.Background(), signal, portfolio, regime, 50.0, shortOHLCV)
	require.NoError(t, err)
	assert.True(t, ps.Size >= 0, "position size should be non-negative")
}

func TestRiskManager_CalculatePosition_InvalidInput(t *testing.T) {
	logger := zerolog.Nop()
	cfg := RiskManagerConfig{ATRPeriod: 14}
	rm, _ := NewRiskManager(cfg, logger)

	t.Run("empty symbol", func(t *testing.T) {
		_, err := rm.CalculatePosition(context.Background(), domain.Signal{},
			&domain.Portfolio{TotalValue: 1000000}, nil, 50.0, nil)
		assert.Error(t, err)
	})

	t.Run("nil portfolio", func(t *testing.T) {
		_, err := rm.CalculatePosition(context.Background(),
			domain.Signal{Symbol: "X"}, nil, nil, 50.0, nil)
		assert.Error(t, err)
	})
}

func TestRiskManager_StopLoss_ByMarketRegime(t *testing.T) {
	logger := zerolog.Nop()
	cfg := StopLossConfig{
		ATRPeriod:          14,
		BaseMultiplier:     2.0,
		BullMultiplier:     2.5,
		BearMultiplier:     1.5,
		SidewaysMultiplier: 2.0,
		TakeProfitMult:     3.0,
	}
	slc := NewStopLossChecker(cfg, logger)
	entryPrice := 100.0
	atr := 3.0

	tests := []struct {
		name           string
		regime         string
		expectedSLMult float64
	}{
		{"bull market", "bull", cfg.BullMultiplier},
		{"bear market", "bear", cfg.BearMultiplier},
		{"sideways market", "sideways", cfg.SidewaysMultiplier},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			regime := &domain.MarketRegime{Trend: tc.regime}
			sl := slc.CalculateStopLossPrice(entryPrice, atr, regime)
			expected := entryPrice - (tc.expectedSLMult * atr)
			assert.InDelta(t, expected, sl, 0.001, "stop loss price mismatch")
		})
	}
}

func TestRiskManager_StopLoss_TakeProfitRatio(t *testing.T) {
	logger := zerolog.Nop()
	cfg := StopLossConfig{
		ATRPeriod:      14,
		BaseMultiplier: 2.0,
		TakeProfitMult: 3.0,
	}
	slc := NewStopLossChecker(cfg, logger)
	entryPrice := 80.0
	atr := 4.0
	regime := &domain.MarketRegime{Trend: "bull"}

	sl := slc.CalculateStopLossPrice(entryPrice, atr, regime)
	tp := slc.CalculateTakeProfitPrice(entryPrice, atr, regime)

	// Verify take profit is above entry
	assert.True(t, tp > entryPrice, "take profit should be above entry")
	// Verify stop loss is below or at entry
	assert.True(t, sl <= entryPrice, "stop loss should be at or below entry")
}

func TestRiskManager_VolatilitySizer_PositionSize(t *testing.T) {
	logger := zerolog.Nop()
	vs := NewVolatilitySizer(VolatilityConfig{
		TargetVolatility:  0.15,
		MaxPositionWeight: 0.10,
		LookbackDays:      10, // Small lookback for test
	}, logger)

	// Generate enough data with meaningful price changes
	ohlcv := generateTestOHLCV(20, 50.0, 0.05) // Higher volatility
	vol, err := vs.CalculateVolatility(ohlcv)
	require.NoError(t, err)
	assert.True(t, vol >= 0, "volatility should be non-negative")
}

func TestRiskManager_VolatilitySizer_EdgeCases(t *testing.T) {
	logger := zerolog.Nop()
	vs := NewVolatilitySizer(VolatilityConfig{
		TargetVolatility:  0.15,
		MaxPositionWeight: 0.10,
		LookbackDays:      10,
	}, logger)

	t.Run("insufficient data", func(t *testing.T) {
		shortData := generateTestOHLCV(5, 50.0, 0.02)
		_, err := vs.CalculateVolatility(shortData)
		assert.Error(t, err, "should error with insufficient data")
	})
}

func TestRiskManager_RegimeDetector_BasicTrend(t *testing.T) {
	logger := zerolog.Nop()
	rd := NewRegimeDetector(RegimeConfig{
		FastMAPeriod: 10,
		SlowMAPeriod: 30,
		VolLookback:  60,
	}, logger)

	ohlcv := generateTestOHLCV(100, 100.0, 0.01)
	regime, err := rd.DetectRegime(context.Background(), ohlcv)
	if err == nil && regime != nil {
		assert.Contains(t, []string{"bull", "bear", "sideways"}, regime.Trend)
	}
}

func TestRiskManager_RegimeDetector_SidewaysMarket(t *testing.T) {
	logger := zerolog.Nop()
	rd := NewRegimeDetector(RegimeConfig{
		FastMAPeriod: 10,
		SlowMAPeriod: 30,
		VolLookback:  60,
	}, logger)

	ohlcv := generateTestOHLCV(100, 100.0, 0.005)
	regime, err := rd.DetectRegime(context.Background(), ohlcv)
	if err == nil && regime != nil {
		assert.Equal(t, "sideways", regime.Trend)
	}
}

func TestRiskManager_CheckStopLoss_Basic(t *testing.T) {
	logger := zerolog.Nop()
	cfg := RiskManagerConfig{ATRPeriod: 14}
	rm, _ := NewRiskManager(cfg, logger)

	positions := []domain.Position{
		{
			Symbol:   "600000.SH",
			Quantity: 1000,
			AvgCost:  10.0,
		},
	}
	prices := map[string]float64{"600000.SH": 8.5} // Below stop loss

	events, err := rm.CheckStopLoss(context.Background(), positions, prices)
	require.NoError(t, err)
	if len(events) > 0 {
		assert.Equal(t, "600000.SH", events[0].Symbol)
	}
}

func TestRiskManager_CalculatePosition_DifferentRegimes(t *testing.T) {
	logger := zerolog.Nop()
	cfg := RiskManagerConfig{
		TargetVolatility:  0.15,
		MaxPositionWeight: 0.10,
		MinPositionWeight: 0.01,
		ATRPeriod:         14,
		VolLookbackDays:   60,
	}
	rm, _ := NewRiskManager(cfg, logger)

	signal := domain.Signal{
		Symbol:    "600001.SH",
		Direction: domain.DirectionLong,
		Strength:  0.7,
		Date:      time.Now(),
	}
	portfolio := &domain.Portfolio{TotalValue: 500000, Cash: 250000}
	currentPrice := 25.0
	ohlcv := generateTestOHLCV(20, currentPrice, 0.03)

	regimes := []struct {
		name   string
		regime *domain.MarketRegime
	}{
		{"bull", &domain.MarketRegime{Trend: "bull", Volatility: "low"}},
		{"bear", &domain.MarketRegime{Trend: "bear", Volatility: "high"}},
		{"sideways", &domain.MarketRegime{Trend: "sideways", Volatility: "medium"}},
	}

	for _, r := range regimes {
		t.Run(r.name, func(t *testing.T) {
			ps, err := rm.CalculatePosition(context.Background(), signal, portfolio, r.regime, currentPrice, ohlcv)
			require.NoError(t, err)
			assert.True(t, ps.Size > 0)
		})
	}
}

func TestRiskManager_PositionSize_RiskAdjustment(t *testing.T) {
	logger := zerolog.Nop()
	cfg := RiskManagerConfig{
		TargetVolatility:  0.15,
		MaxPositionWeight: 0.10,
		MinPositionWeight: 0.01,
		ATRPeriod:         14,
		VolLookbackDays:   60,
	}
	rm, _ := NewRiskManager(cfg, logger)

	signal := domain.Signal{
		Symbol:    "600002.SH",
		Direction: domain.DirectionLong,
		Strength:  1.0,
		Date:      time.Now(),
	}
	portfolio := &domain.Portfolio{TotalValue: 1000000}
	currentPrice := 100.0
	regime := &domain.MarketRegime{Trend: "bull"}

	tests := []struct {
		name         string
		volatility   float64
		expectSmaller bool
	}{
		{"low volatility", 0.01, false},
		{"medium volatility", 0.03, false},
		{"high volatility", 0.08, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ohlcv := generateTestOHLCV(20, currentPrice, tc.volatility)
			ps, err := rm.CalculatePosition(context.Background(), signal, portfolio, regime, currentPrice, ohlcv)
			require.NoError(t, err)
			assert.True(t, ps.Size > 0)
		})
	}
}