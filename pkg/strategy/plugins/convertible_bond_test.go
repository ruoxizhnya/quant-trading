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

// makeBondData returns a canonical ConvertibleBondData for tests.
// ParValue=100, ConversionPrice=10, CouponRate=2%, Maturity=2030-01-01.
func makeBondData() ConvertibleBondData {
	return ConvertibleBondData{
		Symbol:           "113001.SH",
		UnderlyingSymbol: "600000.SH",
		ParValue:         100,
		ConversionPrice:  10.0,
		CouponRate:       0.02,
		MaturityDate:     time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
		CallPrice:        100,
		PutPrice:         70,
	}
}

// makeCloseBars builds a slice of OHLCV bars with the given closing prices.
// Renamed to avoid collision with makeOHLCV in new_strategies_test.go.
func makeCloseBars(symbol string, closes []float64) []domain.OHLCV {
	bars := make([]domain.OHLCV, len(closes))
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, c := range closes {
		bars[i] = domain.OHLCV{
			Symbol: symbol,
			Date:   base.AddDate(0, 0, i),
			Open:   c,
			High:   c + 0.5,
			Low:    c - 0.5,
			Close:  c,
			Volume: 1000000,
		}
	}
	return bars
}

// ─── ConvertibleBondData valuation methods ──────────────────────────────

func TestConvertibleBond_ConversionValue(t *testing.T) {
	cbd := makeBondData()
	// ConversionValue = (100/10) × 15 = 150
	assert.InDelta(t, 150.0, cbd.ConversionValue(15.0), 0.001)
	// stock price = 10 → conversion value = 100
	assert.InDelta(t, 100.0, cbd.ConversionValue(10.0), 0.001)
	// stock price = 5 → conversion value = 50
	assert.InDelta(t, 50.0, cbd.ConversionValue(5.0), 0.001)
}

func TestConvertibleBond_ConversionValue_ZeroConversionPrice(t *testing.T) {
	cbd := makeBondData()
	cbd.ConversionPrice = 0
	assert.Equal(t, 0.0, cbd.ConversionValue(15.0))
}

func TestConvertibleBond_ConversionValue_NegativeConversionPrice(t *testing.T) {
	cbd := makeBondData()
	cbd.ConversionPrice = -1.0
	assert.Equal(t, 0.0, cbd.ConversionValue(15.0))
}

func TestConvertibleBond_ConversionValue_ZeroStockPrice(t *testing.T) {
	cbd := makeBondData()
	assert.Equal(t, 0.0, cbd.ConversionValue(0.0))
}

func TestConvertibleBond_PureBondValue(t *testing.T) {
	cbd := makeBondData()
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	// years ≈ 6, n = 6, coupon = 2, r = 0.05
	// PV_coupons = 2 × (1 - 1.05^-6) / 0.05 ≈ 10.1527
	// PV_par = 100 / 1.05^6 ≈ 74.6215
	// PureBondValue ≈ 84.7742
	val := cbd.PureBondValue(0.05, now)
	assert.InDelta(t, 84.7742, val, 0.01)
}

func TestConvertibleBond_PureBondValue_ZeroCouponRate(t *testing.T) {
	cbd := makeBondData()
	cbd.CouponRate = 0
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	// With zero coupons, pure bond value = PV of par = 100 / 1.05^6
	val := cbd.PureBondValue(0.05, now)
	assert.InDelta(t, 74.6215, val, 0.01)
}

func TestConvertibleBond_PureBondValue_PastMaturity(t *testing.T) {
	cbd := makeBondData()
	now := cbd.MaturityDate.Add(24 * time.Hour)
	// Past maturity → return par value
	assert.Equal(t, 100.0, cbd.PureBondValue(0.05, now))
}

func TestConvertibleBond_PureBondValue_AtMaturity(t *testing.T) {
	cbd := makeBondData()
	// Exactly at maturity → return par value
	assert.Equal(t, 100.0, cbd.PureBondValue(0.05, cbd.MaturityDate))
}

func TestConvertibleBond_PureBondValue_ZeroDiscountRate(t *testing.T) {
	cbd := makeBondData()
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	// r=0 → par + coupon × n = 100 + 2×6 = 112
	val := cbd.PureBondValue(0.0, now)
	assert.InDelta(t, 112.0, val, 0.01)
}

func TestConvertibleBond_PureBondValue_ZeroParValue(t *testing.T) {
	cbd := makeBondData()
	cbd.ParValue = 0
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, 0.0, cbd.PureBondValue(0.05, now))
}

func TestConvertibleBond_PremiumRate(t *testing.T) {
	cbd := makeBondData()
	// stock price = 10 → conversion value = 100
	// bond price = 90 → premium = (90-100)/100 = -0.10
	assert.InDelta(t, -0.10, cbd.PremiumRate(90.0, 10.0), 0.001)
	// bond price = 110 → premium = (110-100)/100 = 0.10
	assert.InDelta(t, 0.10, cbd.PremiumRate(110.0, 10.0), 0.001)
}

func TestConvertibleBond_PremiumRate_ZeroConversionValue(t *testing.T) {
	cbd := makeBondData()
	// stock price = 0 → conversion value = 0 → premium rate = 0
	assert.Equal(t, 0.0, cbd.PremiumRate(100.0, 0.0))
}

func TestConvertibleBond_Delta(t *testing.T) {
	cbd := makeBondData()
	// stock price = 10 → conversion value = 100
	// bond price = 100 → delta = 100/100 = 1.0
	assert.InDelta(t, 1.0, cbd.Delta(100.0, 10.0), 0.001)
	// bond price = 125 → delta = 100/125 = 0.8
	assert.InDelta(t, 0.8, cbd.Delta(125.0, 10.0), 0.001)
}

func TestConvertibleBond_Delta_ZeroBondPrice(t *testing.T) {
	cbd := makeBondData()
	assert.Equal(t, 0.0, cbd.Delta(0.0, 10.0))
}

func TestConvertibleBond_ArbitrageSignal_Triggered(t *testing.T) {
	cbd := makeBondData()
	// stock price = 15 → conversion value = 150
	// bond price = 100, threshold = 0.40
	// 150 > 100 × 1.40 = 140 → true
	assert.True(t, cbd.ArbitrageSignal(100.0, 15.0, 0.40))
}

func TestConvertibleBond_ArbitrageSignal_NotTriggered(t *testing.T) {
	cbd := makeBondData()
	// stock price = 10 → conversion value = 100
	// bond price = 100, threshold = 0.05
	// 100 > 100 × 1.05 = 105 → false
	assert.False(t, cbd.ArbitrageSignal(100.0, 10.0, 0.05))
}

func TestConvertibleBond_ArbitrageSignal_ZeroBondPrice(t *testing.T) {
	cbd := makeBondData()
	assert.False(t, cbd.ArbitrageSignal(0.0, 15.0, 0.40))
}

func TestConvertibleBond_ArbitrageSignal_NegativeThreshold(t *testing.T) {
	cbd := makeBondData()
	assert.False(t, cbd.ArbitrageSignal(100.0, 15.0, -0.10))
}

// ─── Effective trigger prices ───────────────────────────────────────────

func TestConvertibleBond_EffectiveCallTriggerPrice_Stored(t *testing.T) {
	cbd := makeBondData()
	cbd.CallTriggerPrice = 14.0
	assert.Equal(t, 14.0, cbd.EffectiveCallTriggerPrice())
}

func TestConvertibleBond_EffectiveCallTriggerPrice_Derived(t *testing.T) {
	cbd := makeBondData()
	// ConversionPrice = 10 → derived = 10 × 1.3 = 13
	assert.InDelta(t, 13.0, cbd.EffectiveCallTriggerPrice(), 0.001)
}

func TestConvertibleBond_EffectivePutTriggerPrice_Stored(t *testing.T) {
	cbd := makeBondData()
	cbd.PutTriggerPrice = 8.0
	assert.Equal(t, 8.0, cbd.EffectivePutTriggerPrice())
}

func TestConvertibleBond_EffectivePutTriggerPrice_Derived(t *testing.T) {
	cbd := makeBondData()
	// ConversionPrice = 10 → derived = 10 × 0.7 = 7
	assert.InDelta(t, 7.0, cbd.EffectivePutTriggerPrice(), 0.001)
}

// ─── Call/Put trigger detection ─────────────────────────────────────────

func TestConvertibleBond_CheckCallTrigger_Triggered(t *testing.T) {
	cbd := makeBondData()
	// CallTriggerPrice = 10 × 1.3 = 13
	// 20 bars all at 14.0 → 20 out of 30 days above → triggered
	bars := makeCloseBars("600000.SH", repeatPrice(14.0, 20))
	assert.True(t, cbd.CheckCallTrigger(bars, 30, 15))
}

func TestConvertibleBond_CheckCallTrigger_NotTriggered(t *testing.T) {
	cbd := makeBondData()
	// CallTriggerPrice = 13, all prices = 10 → 0 days above → not triggered
	bars := makeCloseBars("600000.SH", repeatPrice(10.0, 30))
	assert.False(t, cbd.CheckCallTrigger(bars, 30, 15))
}

func TestConvertibleBond_CheckCallTrigger_PartialWindow(t *testing.T) {
	cbd := makeBondData()
	// 30 bars: 15 at 14.0 (above trigger) + 15 at 10.0 (below)
	// 15 out of 30 → exactly meets threshold → triggered
	prices := append(repeatPrice(14.0, 15), repeatPrice(10.0, 15)...)
	bars := makeCloseBars("600000.SH", prices)
	assert.True(t, cbd.CheckCallTrigger(bars, 30, 15))
}

func TestConvertibleBond_CheckCallTrigger_InsufficientData(t *testing.T) {
	cbd := makeBondData()
	bars := makeCloseBars("600000.SH", repeatPrice(14.0, 10))
	// Only 10 bars, need 15 trigger days → false
	assert.False(t, cbd.CheckCallTrigger(bars, 30, 15))
}

func TestConvertibleBond_CheckCallTrigger_ZeroWindow(t *testing.T) {
	cbd := makeBondData()
	bars := makeCloseBars("600000.SH", repeatPrice(14.0, 30))
	assert.False(t, cbd.CheckCallTrigger(bars, 0, 15))
}

func TestConvertibleBond_CheckPutTrigger_Triggered(t *testing.T) {
	cbd := makeBondData()
	// PutTriggerPrice = 10 × 0.7 = 7
	// 30 bars all at 6.0 → 30 consecutive days below → triggered
	bars := makeCloseBars("600000.SH", repeatPrice(6.0, 30))
	assert.True(t, cbd.CheckPutTrigger(bars, 30))
}

func TestConvertibleBond_CheckPutTrigger_NotTriggered(t *testing.T) {
	cbd := makeBondData()
	// PutTriggerPrice = 7, all prices = 10 → not below → false
	bars := makeCloseBars("600000.SH", repeatPrice(10.0, 30))
	assert.False(t, cbd.CheckPutTrigger(bars, 30))
}

func TestConvertibleBond_CheckPutTrigger_BreaksStreak(t *testing.T) {
	cbd := makeBondData()
	// 29 bars at 6.0 + 1 bar at 10.0 → streak broken → false
	prices := append(repeatPrice(6.0, 29), 10.0)
	bars := makeCloseBars("600000.SH", prices)
	assert.False(t, cbd.CheckPutTrigger(bars, 30))
}

func TestConvertibleBond_CheckPutTrigger_InsufficientData(t *testing.T) {
	cbd := makeBondData()
	bars := makeCloseBars("600000.SH", repeatPrice(6.0, 20))
	// Only 20 bars, need 30 consecutive → false
	assert.False(t, cbd.CheckPutTrigger(bars, 30))
}

func TestConvertibleBond_CheckPutTrigger_ZeroDays(t *testing.T) {
	cbd := makeBondData()
	bars := makeCloseBars("600000.SH", repeatPrice(6.0, 30))
	assert.False(t, cbd.CheckPutTrigger(bars, 0))
}

// ─── Strategy interface tests ───────────────────────────────────────────

func TestConvertibleBondStrategy_Name(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
	}
	assert.Equal(t, "convertible_bond", s.Name())
}

func TestConvertibleBondStrategy_Description(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
	}
	desc := s.Description()
	assert.NotEmpty(t, desc)
	assert.Contains(t, desc, "可转债")
}

func TestConvertibleBondStrategy_Parameters(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
	}
	params := s.Parameters()
	assert.Len(t, params, 4)
	names := []string{params[0].Name, params[1].Name, params[2].Name, params[3].Name}
	assert.Contains(t, names, "conversion_threshold")
	assert.Contains(t, names, "premium_exit_threshold")
	assert.Contains(t, names, "min_pure_bond_ratio")
	assert.Contains(t, names, "discount_rate")
}

func TestConvertibleBondStrategy_Configure_Valid(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
		params: ConvertibleBondConfig{
			ConversionThreshold:  -0.05,
			PremiumExitThreshold: 0.10,
			MinPureBondRatio:     0.70,
			DiscountRate:         0.05,
		},
	}
	err := s.Configure(map[string]any{
		"conversion_threshold":   -0.08,
		"premium_exit_threshold": 0.15,
		"min_pure_bond_ratio":    0.65,
		"discount_rate":          0.06,
	})
	require.NoError(t, err)
	assert.InDelta(t, -0.08, s.params.ConversionThreshold, 0.001)
	assert.InDelta(t, 0.15, s.params.PremiumExitThreshold, 0.001)
	assert.InDelta(t, 0.65, s.params.MinPureBondRatio, 0.001)
	assert.InDelta(t, 0.06, s.params.DiscountRate, 0.001)
}

func TestConvertibleBondStrategy_Configure_InvalidConversionThreshold(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
	}
	// conversion_threshold must be in [-1.0, 0.0]
	err := s.Configure(map[string]any{"conversion_threshold": 0.5})
	assert.Error(t, err)
}

func TestConvertibleBondStrategy_Configure_InvalidPremiumExit(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
	}
	// premium_exit_threshold must be in [0.0, 1.0]
	err := s.Configure(map[string]any{"premium_exit_threshold": 1.5})
	assert.Error(t, err)
}

func TestConvertibleBondStrategy_Configure_InvalidMinPureBondRatio(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
	}
	err := s.Configure(map[string]any{"min_pure_bond_ratio": 1.5})
	assert.Error(t, err)
}

func TestConvertibleBondStrategy_Configure_InvalidDiscountRate(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
	}
	err := s.Configure(map[string]any{"discount_rate": 0.6})
	assert.Error(t, err)
}

// ─── RegisterBond / GetBond ─────────────────────────────────────────────

func TestConvertibleBondStrategy_RegisterBond(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
		bonds:        make(map[string]ConvertibleBondData),
	}
	cbd := makeBondData()
	s.RegisterBond(cbd)

	got, ok := s.GetBond("113001.SH")
	require.True(t, ok)
	assert.Equal(t, "113001.SH", got.Symbol)
	assert.Equal(t, "600000.SH", got.UnderlyingSymbol)
}

func TestConvertibleBondStrategy_GetBond_NotFound(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
		bonds:        make(map[string]ConvertibleBondData),
	}
	_, ok := s.GetBond("nonexistent.SH")
	assert.False(t, ok)
}

// ─── GenerateSignals tests ──────────────────────────────────────────────

func TestConvertibleBondStrategy_GenerateSignals_EmptyBars(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
		params: ConvertibleBondConfig{
			ConversionThreshold:  -0.05,
			PremiumExitThreshold: 0.10,
			MinPureBondRatio:     0.70,
			DiscountRate:         0.05,
		},
		bonds: make(map[string]ConvertibleBondData),
	}
	signals, err := s.GenerateSignals(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestConvertibleBondStrategy_GenerateSignals_NoBondData(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
		params: ConvertibleBondConfig{
			ConversionThreshold:  -0.05,
			PremiumExitThreshold: 0.10,
			MinPureBondRatio:     0.70,
			DiscountRate:         0.05,
		},
		bonds: make(map[string]ConvertibleBondData),
	}
	bars := map[string][]domain.OHLCV{
		"113001.SH": makeCloseBars("113001.SH", []float64{100}),
	}
	signals, err := s.GenerateSignals(context.Background(), bars, nil)
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestConvertibleBondStrategy_GenerateSignals_BuySignal(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
		params: ConvertibleBondConfig{
			ConversionThreshold:  -0.05,
			PremiumExitThreshold: 0.10,
			MinPureBondRatio:     0.0, // disable floor protection for this test
			DiscountRate:         0.05,
		},
		bonds: make(map[string]ConvertibleBondData),
	}
	s.RegisterBond(makeBondData())

	// stock price = 10 → conversion value = 100
	// bond price = 90 → premium = -0.10 < -0.05 → buy
	bars := map[string][]domain.OHLCV{
		"113001.SH": makeCloseBars("113001.SH", []float64{90}),
		"600000.SH": makeCloseBars("600000.SH", []float64{10}),
	}
	signals, err := s.GenerateSignals(context.Background(), bars, nil)
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "113001.SH", signals[0].Symbol)
	assert.Equal(t, "buy", signals[0].Action)
	assert.Greater(t, signals[0].Strength, 0.0)
	assert.LessOrEqual(t, signals[0].Strength, 1.0)
}

func TestConvertibleBondStrategy_GenerateSignals_SellSignal(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
		params: ConvertibleBondConfig{
			ConversionThreshold:  -0.05,
			PremiumExitThreshold: 0.10,
			MinPureBondRatio:     0.0,
			DiscountRate:         0.05,
		},
		bonds: make(map[string]ConvertibleBondData),
	}
	s.RegisterBond(makeBondData())

	// stock price = 10 → conversion value = 100
	// bond price = 112 → premium = 0.12 > 0.10 → sell
	bars := map[string][]domain.OHLCV{
		"113001.SH": makeCloseBars("113001.SH", []float64{112}),
		"600000.SH": makeCloseBars("600000.SH", []float64{10}),
	}
	portfolio := &domain.Portfolio{
		Positions: map[string]domain.Position{
			"113001.SH": {Symbol: "113001.SH", Quantity: 100, CurrentPrice: 112},
		},
	}
	signals, err := s.GenerateSignals(context.Background(), bars, portfolio)
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "113001.SH", signals[0].Symbol)
	assert.Equal(t, "sell", signals[0].Action)
}

func TestConvertibleBondStrategy_GenerateSignals_SellNoPosition(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
		params: ConvertibleBondConfig{
			ConversionThreshold:  -0.05,
			PremiumExitThreshold: 0.10,
			MinPureBondRatio:     0.0,
			DiscountRate:         0.05,
		},
		bonds: make(map[string]ConvertibleBondData),
	}
	s.RegisterBond(makeBondData())

	// premium > exit threshold but no position held → no sell signal
	bars := map[string][]domain.OHLCV{
		"113001.SH": makeCloseBars("113001.SH", []float64{112}),
		"600000.SH": makeCloseBars("600000.SH", []float64{10}),
	}
	signals, err := s.GenerateSignals(context.Background(), bars, nil)
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestConvertibleBondStrategy_GenerateSignals_BondFloorProtection(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
		params: ConvertibleBondConfig{
			ConversionThreshold:  -0.05,
			PremiumExitThreshold: 0.10,
			MinPureBondRatio:     0.95, // high floor requirement
			DiscountRate:         0.05,
		},
		bonds: make(map[string]ConvertibleBondData),
	}
	s.RegisterBond(makeBondData())

	// Pure bond value ≈ 84.77, par = 100 → ratio ≈ 0.848 < 0.95 → skip
	bars := map[string][]domain.OHLCV{
		"113001.SH": makeCloseBars("113001.SH", []float64{90}),
		"600000.SH": makeCloseBars("600000.SH", []float64{10}),
	}
	signals, err := s.GenerateSignals(context.Background(), bars, nil)
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestConvertibleBondStrategy_GenerateSignals_MissingStockBars(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
		params: ConvertibleBondConfig{
			ConversionThreshold:  -0.05,
			PremiumExitThreshold: 0.10,
			MinPureBondRatio:     0.0,
			DiscountRate:         0.05,
		},
		bonds: make(map[string]ConvertibleBondData),
	}
	s.RegisterBond(makeBondData())

	// Only bond bars, no stock bars → skip
	bars := map[string][]domain.OHLCV{
		"113001.SH": makeCloseBars("113001.SH", []float64{90}),
	}
	signals, err := s.GenerateSignals(context.Background(), bars, nil)
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestConvertibleBondStrategy_GenerateSignals_MissingBondBars(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
		params: ConvertibleBondConfig{
			ConversionThreshold:  -0.05,
			PremiumExitThreshold: 0.10,
			MinPureBondRatio:     0.0,
			DiscountRate:         0.05,
		},
		bonds: make(map[string]ConvertibleBondData),
	}
	s.RegisterBond(makeBondData())

	// Only stock bars, no bond bars → skip
	bars := map[string][]domain.OHLCV{
		"600000.SH": makeCloseBars("600000.SH", []float64{10}),
	}
	signals, err := s.GenerateSignals(context.Background(), bars, nil)
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestConvertibleBondStrategy_GenerateSignals_ZeroBondPrice(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
		params: ConvertibleBondConfig{
			ConversionThreshold:  -0.05,
			PremiumExitThreshold: 0.10,
			MinPureBondRatio:     0.0,
			DiscountRate:         0.05,
		},
		bonds: make(map[string]ConvertibleBondData),
	}
	s.RegisterBond(makeBondData())

	bars := map[string][]domain.OHLCV{
		"113001.SH": makeCloseBars("113001.SH", []float64{0}),
		"600000.SH": makeCloseBars("600000.SH", []float64{10}),
	}
	signals, err := s.GenerateSignals(context.Background(), bars, nil)
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestConvertibleBondStrategy_GenerateSignals_CallTrigger(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
		params: ConvertibleBondConfig{
			ConversionThreshold:  -0.05,
			PremiumExitThreshold: 0.10,
			MinPureBondRatio:     0.0,
			DiscountRate:         0.05,
			CallTriggerWindow:    30,
			CallTriggerDays:      15,
			PutTriggerDays:       30,
		},
		bonds: make(map[string]ConvertibleBondData),
	}
	s.RegisterBond(makeBondData())

	// Stock price = 14 (above call trigger 13) for 20 days → call triggered
	stockBars := makeCloseBars("600000.SH", repeatPrice(14.0, 20))
	bondBars := makeCloseBars("113001.SH", []float64{100})
	bars := map[string][]domain.OHLCV{
		"113001.SH": bondBars,
		"600000.SH": stockBars,
	}
	signals, err := s.GenerateSignals(context.Background(), bars, nil)
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "sell", signals[0].Action)
	assert.Equal(t, "call_trigger", signals[0].Metadata["trigger"])
}

func TestConvertibleBondStrategy_GenerateSignals_PutTrigger(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
		params: ConvertibleBondConfig{
			ConversionThreshold:  -0.05,
			PremiumExitThreshold: 0.10,
			MinPureBondRatio:     0.0,
			DiscountRate:         0.05,
			CallTriggerWindow:    30,
			CallTriggerDays:      15,
			PutTriggerDays:       30,
		},
		bonds: make(map[string]ConvertibleBondData),
	}
	s.RegisterBond(makeBondData())

	// Stock price = 6 (below put trigger 7) for 30 consecutive days → put triggered
	stockBars := makeCloseBars("600000.SH", repeatPrice(6.0, 30))
	bondBars := makeCloseBars("113001.SH", []float64{100})
	bars := map[string][]domain.OHLCV{
		"113001.SH": bondBars,
		"600000.SH": stockBars,
	}
	signals, err := s.GenerateSignals(context.Background(), bars, nil)
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "sell", signals[0].Action)
	assert.Equal(t, "put_trigger", signals[0].Metadata["trigger"])
}

func TestConvertibleBondStrategy_GenerateSignals_FactorsPopulated(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
		params: ConvertibleBondConfig{
			ConversionThreshold:  -0.05,
			PremiumExitThreshold: 0.10,
			MinPureBondRatio:     0.0,
			DiscountRate:         0.05,
		},
		bonds: make(map[string]ConvertibleBondData),
	}
	s.RegisterBond(makeBondData())

	bars := map[string][]domain.OHLCV{
		"113001.SH": makeCloseBars("113001.SH", []float64{90}),
		"600000.SH": makeCloseBars("600000.SH", []float64{10}),
	}
	signals, err := s.GenerateSignals(context.Background(), bars, nil)
	require.NoError(t, err)
	require.Len(t, signals, 1)
	factors := signals[0].Factors
	assert.Contains(t, factors, "premium_rate")
	assert.Contains(t, factors, "conversion_value")
	assert.Contains(t, factors, "pure_bond_value")
	assert.Contains(t, factors, "delta")
	assert.InDelta(t, -0.10, factors["premium_rate"], 0.001)
	assert.InDelta(t, 100.0, factors["conversion_value"], 0.001)
}

// ─── Weight / Cleanup ───────────────────────────────────────────────────

func TestConvertibleBondStrategy_Weight(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
	}
	signal := strategy.Signal{Strength: 0.5}
	weight := s.Weight(signal, 1000000)
	assert.Greater(t, weight, 0.0)
	assert.LessOrEqual(t, weight, 0.05)
}

func TestConvertibleBondStrategy_Weight_CappedAtMax(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
	}
	signal := strategy.Signal{Strength: 2.0}
	weight := s.Weight(signal, 1000000)
	assert.LessOrEqual(t, weight, 0.05)
}

func TestConvertibleBondStrategy_Weight_MinimumFloor(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
	}
	signal := strategy.Signal{Strength: 0.0}
	weight := s.Weight(signal, 1000000)
	assert.GreaterOrEqual(t, weight, 0.01)
}

func TestConvertibleBondStrategy_Cleanup(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
		params: ConvertibleBondConfig{
			ConversionThreshold:  -0.05,
			PremiumExitThreshold: 0.10,
		},
		bonds: make(map[string]ConvertibleBondData),
	}
	s.RegisterBond(makeBondData())
	s.Cleanup()

	assert.Equal(t, ConvertibleBondConfig{}, s.params)
	_, ok := s.GetBond("113001.SH")
	assert.False(t, ok)
}

// ─── Interface compliance ───────────────────────────────────────────────

func TestConvertibleBondStrategy_InterfaceCompliance(t *testing.T) {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy("convertible_bond", "test"),
		params: ConvertibleBondConfig{
			ConversionThreshold:  -0.05,
			PremiumExitThreshold: 0.10,
			MinPureBondRatio:     0.70,
			DiscountRate:         0.05,
		},
		bonds: make(map[string]ConvertibleBondData),
	}
	var _ strategy.Strategy = s
	var _ strategy.StrategyCore = s
	var _ strategy.Configurable = s
	var _ strategy.SignalGenerator = s
	var _ strategy.ResourceManaged = s
}

// ─── Helpers ────────────────────────────────────────────────────────────

// repeatPrice returns a slice of n copies of the given price.
func repeatPrice(price float64, n int) []float64 {
	prices := make([]float64, n)
	for i := range prices {
		prices[i] = price
	}
	return prices
}
