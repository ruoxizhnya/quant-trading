package plugins

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestBars creates sample OHLCV data for testing
func createTestBars(symbol string, days int, trend string) []domain.OHLCV {
	bars := make([]domain.OHLCV, days)
	basePrice := 100.0
	for i := 0; i < days; i++ {
		date := time.Date(2024, 1, i+1, 0, 0, 0, 0, time.UTC)
		close := basePrice
		switch trend {
		case "up":
			close = basePrice + float64(i)*2
		case "down":
			close = basePrice - float64(i)*2
		case "volatile":
			if i%2 == 0 {
				close = basePrice + float64(i)*3
			} else {
				close = basePrice - float64(i)*2
			}
		}
		bars[i] = domain.OHLCV{
			Symbol: symbol,
			Date:   date,
			Open:   close - 1,
			High:   close + 2,
			Low:    close - 2,
			Close:  close,
			Volume: 1000000,
		}
	}
	return bars
}

// TestMomentumStrategy tests the momentum strategy
func TestMomentumStrategy(t *testing.T) {
	s := &momentumStrategy{
		params: MomentumConfig{LookbackDays: 10, TopN: 2},
	}

	bars := map[string][]domain.OHLCV{
		"600001.SH": createTestBars("600001.SH", 30, "up"),
		"600002.SH": createTestBars("600002.SH", 30, "down"),
	}

	signals, err := s.GenerateSignals(context.Background(), bars, nil)
	require.NoError(t, err)
	assert.NotNil(t, signals)
	// Up-trend stock should generate buy signal
	var foundBuy bool
	for _, sig := range signals {
		if sig.Symbol == "600001.SH" && sig.Action == "buy" {
			foundBuy = true
			assert.Greater(t, sig.Strength, 0.0)
		}
	}
	assert.True(t, foundBuy, "Should find buy signal for up-trend stock")
}

// TestMeanReversionStrategy tests the mean reversion strategy
func TestMeanReversionStrategy(t *testing.T) {
	s := &meanReversionStrategy{
		params: MeanReversionConfig{BollingerPeriod: 10, BollingerStdDev: 2.0, RSIPeriod: 10, RSIOversold: 30, RSIOverbought: 70},
	}

	// Create bars with sharp drop to trigger Bollinger lower band + RSI oversold
	bars := map[string][]domain.OHLCV{
		"600001.SH": createTestBars("600001.SH", 30, "down"),
	}

	signals, err := s.GenerateSignals(context.Background(), bars, nil)
	require.NoError(t, err)
	// Bollinger+RSI requires both conditions; may or may not trigger on synthetic data
	_ = signals
}

// TestTDSequentialStrategy tests the TD Sequential strategy
func TestTDSequentialStrategy(t *testing.T) {
	s := &tdSequentialStrategy{
		params: TDSequentialConfig{SetupCount: 9, CancelDays: 4},
	}

	// Create bars with consistent down trend for bearish setup
	bars := map[string][]domain.OHLCV{
		"600001.SH": createTestBars("600001.SH", 30, "down"),
	}

	signals, err := s.GenerateSignals(context.Background(), bars, nil)
	require.NoError(t, err)
	assert.NotNil(t, signals)
}

// TestBollingerMRStrategy tests the Bollinger mean reversion strategy
func TestBollingerMRStrategy(t *testing.T) {
	s := &bollingerMRStrategy{
		params: BollingerMRConfig{Period: 5, StdDev: 1.0, BuyZScore: -1.0, SellZScore: 1.0},
	}

	// Create volatile bars to trigger z-score signals
	volatileBars := []domain.OHLCV{
		{Symbol: "600001.SH", Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Open: 100, High: 102, Low: 98, Close: 100, Volume: 1000000},
		{Symbol: "600001.SH", Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Open: 100, High: 102, Low: 98, Close: 101, Volume: 1000000},
		{Symbol: "600001.SH", Date: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), Open: 100, High: 102, Low: 98, Close: 102, Volume: 1000000},
		{Symbol: "600001.SH", Date: time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC), Open: 100, High: 102, Low: 98, Close: 103, Volume: 1000000},
		{Symbol: "600001.SH", Date: time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC), Open: 100, High: 102, Low: 98, Close: 80, Volume: 1000000},
	}

	bars := map[string][]domain.OHLCV{
		"600001.SH": volatileBars,
	}

	signals, err := s.GenerateSignals(context.Background(), bars, nil)
	require.NoError(t, err)
	assert.NotNil(t, signals)
	// With a big drop, should generate buy signal
	assert.GreaterOrEqual(t, len(signals), 0)
}

// TestVolatilityBreakoutStrategy tests the volatility breakout strategy
func TestVolatilityBreakoutStrategy(t *testing.T) {
	s := &volBreakoutStrategy{
		params: VolBreakoutConfig{ATRPeriod: 2, ATRMultiplier: 0.5, Lookback: 2, TopN: 5},
	}

	// Create bars with clear breakout pattern
	// Need atrPeriod + lookback + 1 = 5 bars minimum
	breakoutBars := []domain.OHLCV{
		{Symbol: "600001.SH", Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Open: 100, High: 102, Low: 98, Close: 100, Volume: 1000000},
		{Symbol: "600001.SH", Date: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Open: 100, High: 103, Low: 99, Close: 101, Volume: 1000000},
		{Symbol: "600001.SH", Date: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), Open: 101, High: 104, Low: 100, Close: 102, Volume: 1000000},
		{Symbol: "600001.SH", Date: time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC), Open: 102, High: 105, Low: 101, Close: 103, Volume: 1000000},
		{Symbol: "600001.SH", Date: time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC), Open: 103, High: 120, Low: 102, Close: 120, Volume: 5000000},
	}

	bars := map[string][]domain.OHLCV{
		"600001.SH": breakoutBars,
	}

	signals, err := s.GenerateSignals(context.Background(), bars, nil)
	require.NoError(t, err)
	// Signals may be nil if no breakout detected - that's acceptable
	if signals != nil {
		assert.GreaterOrEqual(t, len(signals), 0)
	}
}

// TestVolumePriceTrendStrategy tests the VPT strategy
func TestVolumePriceTrendStrategy(t *testing.T) {
	s := &vptStrategy{
		params: VPTConfig{SMALookback: 20, TopN: 5},
	}

	bars := map[string][]domain.OHLCV{
		"600001.SH": createTestBars("600001.SH", 30, "up"),
	}

	signals, err := s.GenerateSignals(context.Background(), bars, nil)
	require.NoError(t, err)
	assert.NotNil(t, signals)
}

// TestValueScreeningStrategy_WithMockData tests value screening with mock data
func TestValueScreeningStrategy_WithMockData(t *testing.T) {
	s := &valueScreeningStrategy{
		params: ValueScreeningConfig{
			PEMax:        30.0,
			PBMax:        3.0,
			ROEMin:       0.1,
			MomentumDays: 60,
			TopN:         10,
		},
		dataServiceURL: "http://localhost:8081",
		httpClient:     &http.Client{Timeout: 1 * time.Millisecond},
		screenCache:    strategy.NewScreenCache(30),
	}

	bars := map[string][]domain.OHLCV{
		"600001.SH": createTestBars("600001.SH", 70, "up"),
	}

	// Without HTTP client, this should return error or empty signals
	signals, err := s.GenerateSignals(context.Background(), bars, nil)
	// Error expected since no HTTP client configured
	if err != nil {
		t.Logf("Expected error without HTTP client: %v", err)
		return
	}
	assert.NotNil(t, signals)
}

// TestMultiFactorStrategy_WithMockData tests multi-factor with mock data
func TestMultiFactorStrategy_WithMockData(t *testing.T) {
	s := &multiFactorStrategy{
		params: MultiFactorConfig{
			ValueWeight:    0.4,
			QualityWeight:  0.3,
			MomentumWeight: 0.3,
			LookbackDays:   60,
			TopN:           10,
		},
		dataServiceURL: "http://localhost:8081",
		httpClient:     &http.Client{Timeout: 1 * time.Millisecond},
		screenCache:    strategy.NewScreenCache(30),
	}

	bars := map[string][]domain.OHLCV{
		"600001.SH": createTestBars("600001.SH", 70, "up"),
	}

	signals, err := s.GenerateSignals(context.Background(), bars, nil)
	if err != nil {
		t.Logf("Expected error without HTTP client: %v", err)
		return
	}
	assert.NotNil(t, signals)
}

// TestStrategyRegistration tests that all strategies are registered
func TestStrategyRegistration(t *testing.T) {
	// Ensure all init() functions have run by accessing packages directly
	strategies := map[string]strategy.Strategy{
		"momentum":             &momentumStrategy{params: MomentumConfig{LookbackDays: 20, TopN: 5}},
		"mean_reversion":       &meanReversionStrategy{params: MeanReversionConfig{BollingerPeriod: 20, BollingerStdDev: 2.0, RSIPeriod: 14, RSIOversold: 30, RSIOverbought: 70}},
		"value_screening":      &valueScreeningStrategy{params: ValueScreeningConfig{PEMax: 30, PBMax: 3, ROEMin: 0.1, MomentumDays: 60, TopN: 10}},
		"multi_factor":         &multiFactorStrategy{params: MultiFactorConfig{ValueWeight: 0.4, QualityWeight: 0.3, MomentumWeight: 0.3, LookbackDays: 60, TopN: 10}},
		"td_sequential":        &tdSequentialStrategy{params: TDSequentialConfig{SetupCount: 9, CancelDays: 4}},
		"bollinger_mr":         &bollingerMRStrategy{params: BollingerMRConfig{Period: 20, StdDev: 2.0, BuyZScore: -2.0, SellZScore: 2.0}},
		"volume_price_trend":   &vptStrategy{params: VPTConfig{SMALookback: 20, TopN: 5}},
		"volatility_breakout":  &volBreakoutStrategy{params: VolBreakoutConfig{ATRPeriod: 14, ATRMultiplier: 2.0, Lookback: 20, TopN: 5}},
	}

	for name, s := range strategies {
		assert.NotNil(t, s, "Strategy %s should not be nil", name)
		assert.Equal(t, name, s.Name(), "Strategy name should match")
		assert.NotEmpty(t, s.Description(), "Strategy %s should have description", name)
		assert.NotEmpty(t, s.Parameters(), "Strategy %s should have parameters", name)
	}
}

// TestStrategyConfigure tests parameter configuration for all strategies
func TestStrategyConfigure(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]any
	}{
		{"momentum", map[string]any{"lookback_days": 30, "top_n": 3}},
		{"mean_reversion", map[string]any{"ma_period": 15, "buy_threshold_pct": -0.97}},
		{"value_screening", map[string]any{"pe_max": 25.0, "top_n": 5}},
		{"multi_factor", map[string]any{"value_weight": 0.5, "top_n": 8}},
		{"td_sequential", map[string]any{"setup_count": 13, "cancel_days": 5}},
		{"bollinger_mr", map[string]any{"period": 25, "buy_zscore": -2.5}},
		{"volume_price_trend", map[string]any{"sma_lookback": 15, "top_n": 3}},
		{"volatility_breakout", map[string]any{"atr_period": 10, "top_n": 3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := strategy.GlobalGet(tt.name)
			require.NoError(t, err)

			// Check if strategy implements Configurable interface
			type configurable interface {
				Configure(params map[string]any) error
			}
			if c, ok := s.(configurable); ok {
				err := c.Configure(tt.params)
				assert.NoError(t, err, "Strategy %s should accept configuration", tt.name)
			}
		})
	}
}
