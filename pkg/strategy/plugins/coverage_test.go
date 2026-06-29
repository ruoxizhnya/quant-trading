package plugins

import (
	"context"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBollingerMR_Description(t *testing.T) {
	s := &bollingerMRStrategy{
		BaseStrategy: strategy.NewBaseStrategy("bollinger_mr", "Bollinger Band Mean Reversion"),
	}
	assert.Contains(t, s.Description(), "Bollinger")
}

func TestVPT_Description(t *testing.T) {
	s := &vptStrategy{
		BaseStrategy: strategy.NewBaseStrategy("volume_price_trend", "Volume Price Trend"),
	}
	assert.Contains(t, s.Description(), "Volume")
}

func TestVolBreakout_Description(t *testing.T) {
	s := &volBreakoutStrategy{
		BaseStrategy: strategy.NewBaseStrategy("volatility_breakout", "Volatility Breakout"),
	}
	assert.Contains(t, s.Description(), "Volatility")
}

func TestMultiFactor_SetFactorCache(t *testing.T) {
	s := &multiFactorStrategy{
		BaseStrategy: strategy.NewBaseStrategy("multi_factor", "Multi-factor strategy"),
		params:       MultiFactorConfig{ValueWeight: 0.4, QualityWeight: 0.3, MomentumWeight: 0.3},
	}

	reader := func(ft domain.FactorType, date time.Time, symbol string) (float64, bool) {
		return 1.5, true
	}
	s.SetFactorCache(reader)
	assert.NotNil(t, s.factorReader)

	s.SetFactorCache(nil)
	assert.Nil(t, s.factorReader)
}

func TestMultiFactor_GenerateSignalsFromFactorCache(t *testing.T) {
	s := &multiFactorStrategy{
		BaseStrategy: strategy.NewBaseStrategy("multi_factor", "Multi-factor strategy"),
		params:       MultiFactorConfig{ValueWeight: 0.4, QualityWeight: 0.3, MomentumWeight: 0.3, TopN: 3},
	}

	screenDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	s.SetFactorCache(func(ft domain.FactorType, date time.Time, symbol string) (float64, bool) {
		if symbol == "600000.SH" {
			switch ft {
			case domain.FactorValue:
				return 1.2, true
			case domain.FactorQuality:
				return 0.8, true
			case domain.FactorMomentum:
				return 1.5, true
			}
		}
		if symbol == "600001.SH" {
			switch ft {
			case domain.FactorValue:
				return -0.5, true
			case domain.FactorQuality:
				return 0.3, true
			case domain.FactorMomentum:
				return -1.0, true
			}
		}
		return 0, false
	})

	bars := map[string][]domain.OHLCV{
		"600000.SH": {{Symbol: "600000.SH", Date: screenDate, Close: 10.0}},
		"600001.SH": {{Symbol: "600001.SH", Date: screenDate, Close: 20.0}},
	}

	ranked := s.generateSignalsFromFactorCache(bars, screenDate, 0.4, 0.3, 0.3)
	assert.Len(t, ranked, 2)
	assert.Equal(t, "600000.SH", ranked[0].symbol)
	assert.Greater(t, ranked[0].compositeScore, ranked[1].compositeScore)
}

func TestMultiFactor_GenerateSignalsFromFactorCache_NoFactors(t *testing.T) {
	s := &multiFactorStrategy{
		BaseStrategy: strategy.NewBaseStrategy("multi_factor", "Multi-factor strategy"),
		params:       MultiFactorConfig{ValueWeight: 0.4, QualityWeight: 0.3, MomentumWeight: 0.3},
	}

	s.SetFactorCache(func(ft domain.FactorType, date time.Time, symbol string) (float64, bool) {
		return 0, false
	})

	bars := map[string][]domain.OHLCV{
		"600000.SH": {{Symbol: "600000.SH", Date: time.Now(), Close: 10.0}},
	}

	ranked := s.generateSignalsFromFactorCache(bars, time.Now(), 0.4, 0.3, 0.3)
	assert.Empty(t, ranked)
}

func TestMultiFactor_GenerateSignalsFromFactorCache_PartialFactors(t *testing.T) {
	s := &multiFactorStrategy{
		BaseStrategy: strategy.NewBaseStrategy("multi_factor", "Multi-factor strategy"),
		params:       MultiFactorConfig{ValueWeight: 0.4, QualityWeight: 0.3, MomentumWeight: 0.3},
	}

	screenDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	s.SetFactorCache(func(ft domain.FactorType, date time.Time, symbol string) (float64, bool) {
		if symbol == "600000.SH" && ft == domain.FactorValue {
			return 1.0, true
		}
		return 0, false
	})

	bars := map[string][]domain.OHLCV{
		"600000.SH": {{Symbol: "600000.SH", Date: screenDate, Close: 10.0}},
	}

	ranked := s.generateSignalsFromFactorCache(bars, screenDate, 0.4, 0.3, 0.3)
	assert.Len(t, ranked, 1)
	assert.InDelta(t, 1.0, ranked[0].compositeScore, 0.01)
}

func TestMultiFactor_GenerateSellSignals(t *testing.T) {
	s := &multiFactorStrategy{
		BaseStrategy: strategy.NewBaseStrategy("multi_factor", "Multi-factor strategy"),
		params:       MultiFactorConfig{ValueWeight: 0.4, QualityWeight: 0.3, MomentumWeight: 0.3},
	}

	bars := map[string][]domain.OHLCV{}
	days := make([]domain.OHLCV, 25)
	base := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	for i := range days {
		days[i] = domain.OHLCV{
			Symbol: "600000.SH",
			Date:   base.AddDate(0, 0, i),
			Close:  10.0 + float64(i)*0.1,
		}
	}
	bars["600000.SH"] = days

	portfolio := &domain.Portfolio{
		Positions: map[string]domain.Position{
			"600000.SH": {Symbol: "600000.SH", Quantity: 100, AvgCost: 10.0},
		},
	}

	signals, err := s.generateSellSignals(bars, portfolio, 20)
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestMultiFactor_GenerateSellSignals_NegativeMomentum(t *testing.T) {
	s := &multiFactorStrategy{
		BaseStrategy: strategy.NewBaseStrategy("multi_factor", "Multi-factor strategy"),
		params:       MultiFactorConfig{ValueWeight: 0.4, QualityWeight: 0.3, MomentumWeight: 0.3},
	}

	bars := map[string][]domain.OHLCV{}
	days := make([]domain.OHLCV, 10)
	base := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	for i := range days {
		price := 15.0 - float64(i)*0.5
		days[i] = domain.OHLCV{
			Symbol: "600000.SH",
			Date:   base.AddDate(0, 0, i),
			Close:  price,
		}
	}
	bars["600000.SH"] = days

	portfolio := &domain.Portfolio{
		Positions: map[string]domain.Position{
			"600000.SH": {Symbol: "600000.SH", Quantity: 100, AvgCost: 15.0},
		},
	}

	signals, err := s.generateSellSignals(bars, portfolio, 5)
	require.NoError(t, err)
	assert.NotEmpty(t, signals)
	assert.Equal(t, "600000.SH", signals[0].Symbol)
}

func TestMultiFactor_GenerateSellSignals_NilPortfolio(t *testing.T) {
	s := &multiFactorStrategy{
		BaseStrategy: strategy.NewBaseStrategy("multi_factor", "Multi-factor strategy"),
		params:       MultiFactorConfig{},
	}

	signals, err := s.generateSellSignals(nil, nil, 5)
	require.NoError(t, err)
	assert.Nil(t, signals)
}

func TestValueScreen_GenerateSellSignals(t *testing.T) {
	s := &valueScreeningStrategy{
		BaseStrategy: strategy.NewBaseStrategy("value_screen", "Value screening strategy"),
		params:       ValueScreeningConfig{MomentumDays: 5},
	}

	bars := map[string][]domain.OHLCV{}
	days := make([]domain.OHLCV, 10)
	base := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	for i := range days {
		price := 20.0 - float64(i)*0.8
		days[i] = domain.OHLCV{
			Symbol: "600001.SH",
			Date:   base.AddDate(0, 0, i),
			Close:  price,
		}
	}
	bars["600001.SH"] = days

	portfolio := &domain.Portfolio{
		Positions: map[string]domain.Position{
			"600001.SH": {Symbol: "600001.SH", Quantity: 200, AvgCost: 20.0},
		},
	}

	signals, err := s.generateSellSignals(bars, portfolio, 5)
	require.NoError(t, err)
	assert.NotEmpty(t, signals)
}

func TestValueScreen_GenerateSellSignals_NilPortfolio(t *testing.T) {
	s := &valueScreeningStrategy{
		BaseStrategy: strategy.NewBaseStrategy("value_screen", "Value screening strategy"),
		params:       ValueScreeningConfig{},
	}

	signals, err := s.generateSellSignals(nil, nil, 5)
	require.NoError(t, err)
	assert.Nil(t, signals)
}

func TestMultiFactor_GenerateSignals_WithFactorCache(t *testing.T) {
	s := &multiFactorStrategy{
		BaseStrategy: strategy.NewBaseStrategy("multi_factor", "Multi-factor strategy"),
		params:       MultiFactorConfig{ValueWeight: 0.4, QualityWeight: 0.3, MomentumWeight: 0.3, TopN: 1, RebalanceFrequency: "daily"},
	}

	screenDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	s.SetFactorCache(func(ft domain.FactorType, date time.Time, symbol string) (float64, bool) {
		if symbol == "600000.SH" {
			return 1.0, true
		}
		return 0, false
	})

	bars := map[string][]domain.OHLCV{
		"600000.SH": {{Symbol: "600000.SH", Date: screenDate, Close: 10.0}},
	}

	portfolio := &domain.Portfolio{
		Cash:       1000000,
		TotalValue: 1000000,
		Positions:  map[string]domain.Position{},
	}

	signals, err := s.GenerateSignals(context.Background(), bars, portfolio)
	require.NoError(t, err)
	_ = signals
}

func TestIsRebalanceDay_MultiFactor(t *testing.T) {
	day1 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	assert.True(t, strategy.IsRebalanceDay(day1, "20d"))
}

func TestIsRebalanceDay_ValueScreen(t *testing.T) {
	day1 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	assert.True(t, strategy.IsRebalanceDay(day1, "20d"))
}
