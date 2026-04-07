package risk

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/rs/zerolog"
)

func TestATRStopLoss_Calculation(t *testing.T) {
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

	t.Run("CalculateATR_Basic", func(t *testing.T) {
		ohlcv := generateTestOHLCV(20, 100.0, 0.02)
		atr, err := slc.CalculateATR(ohlcv)
		if err != nil {
			t.Fatalf("ATR calculation failed: %v", err)
		}
		if atr <= 0 {
			t.Errorf("ATR should be positive, got %.4f", atr)
		}
		if atr > 10 {
			t.Errorf("ATR seems too high: %.4f", atr)
		}
	})

	t.Run("CalculateATR_InsufficientData", func(t *testing.T) {
		ohlcv := generateTestOHLCV(10, 100.0, 0.02)
		_, err := slc.CalculateATR(ohlcv)
		if err == nil {
			t.Error("should return error for insufficient data")
		}
	})

	t.Run("CalculateStopLossPrice_SidewaysMarket", func(t *testing.T) {
		entryPrice := 100.0
		atr := 2.0
		regime := &domain.MarketRegime{
			Trend:      "sideways",
			Volatility: "medium",
		}

		stopLoss := slc.CalculateStopLossPrice(entryPrice, atr, regime)
		expected := entryPrice - (cfg.SidewaysMultiplier * atr)
		if math.Abs(stopLoss-expected) > 0.001 {
			t.Errorf("stop loss = %.2f, expected %.2f", stopLoss, expected)
		}
	})

	t.Run("CalculateStopLossPrice_BullMarket", func(t *testing.T) {
		entryPrice := 100.0
		atr := 2.0
		regime := &domain.MarketRegime{
			Trend:      "bull",
			Volatility: "low",
		}

		stopLoss := slc.CalculateStopLossPrice(entryPrice, atr, regime)
		expected := entryPrice - (cfg.BullMultiplier * atr)
		if math.Abs(stopLoss-expected) > 0.001 {
			t.Errorf("stop loss (bull) = %.2f, expected %.2f", stopLoss, expected)
		}
	})

	t.Run("CalculateStopLossPrice_BearMarket", func(t *testing.T) {
		entryPrice := 100.0
		atr := 2.0
		regime := &domain.MarketRegime{
			Trend:      "bear",
			Volatility: "high",
		}

		stopLoss := slc.CalculateStopLossPrice(entryPrice, atr, regime)
		expected := entryPrice - (cfg.BearMultiplier * atr)
		if math.Abs(stopLoss-expected) > 0.001 {
			t.Errorf("stop loss (bear) = %.2f, expected %.2f", stopLoss, expected)
		}
	})

	t.Run("CalculateTakeProfitPrice", func(t *testing.T) {
		entryPrice := 100.0
		atr := 2.0
		regime := &domain.MarketRegime{Trend: "bull"}

		takeProfit := slc.CalculateTakeProfitPrice(entryPrice, atr, regime)
		expected := entryPrice + (cfg.TakeProfitMult * atr)
		if math.Abs(takeProfit-expected) > 0.001 {
			t.Errorf("take profit = %.2f, expected %.2f", takeProfit, expected)
		}
	})
}

func TestRiskManager_CalculatePosition_WithATR(t *testing.T) {
	logger := zerolog.Nop()
	cfg := RiskManagerConfig{
		TargetVolatility:    0.15,
		MaxPositionWeight:   0.05,
		MinPositionWeight:   0.01,
		ATRPeriod:           14,
		BaseMultiplier:      2.0,
		BullMultiplier:      2.5,
		BearMultiplier:      1.5,
		SidewaysMultiplier: 2.0,
		TakeProfitMult:      3.0,
		VolLookbackDays:     20,
		AnnualizationFactor: 252,
		FastMAPeriod:        10,
		SlowMAPeriod:        30,
		RegimeVolLookback:   20,
	}

	rm, err := NewRiskManager(cfg, logger)
	if err != nil {
		t.Fatalf("failed to create RiskManager: %v", err)
	}

	ctx := context.Background()
	signal := domain.Signal{
		Symbol:        "600000.SH",
		Direction:     domain.DirectionLong,
		Strength:      0.8,
		CompositeScore: 0.7,
	}
	portfolio := &domain.Portfolio{
		Cash:       500000.0,
		Positions:  make(map[string]domain.Position),
		TotalValue: 1000000.0,
	}
	regime := &domain.MarketRegime{
		Trend:      "bull",
		Volatility: "medium",
		Sentiment:  0.3,
	}
	currentPrice := 10.0
	ohlcv := generateTestOHLCV(20, currentPrice, 0.03)

	t.Run("StopLoss_IsNonZero", func(t *testing.T) {
		posSize, err := rm.CalculatePosition(ctx, signal, portfolio, regime, currentPrice, ohlcv)
		if err != nil {
			t.Fatalf("CalculatePosition failed: %v", err)
		}

		if posSize.StopLoss == 0 {
			t.Error("StopLoss should not be zero when OHLCV provided")
		}
		if posSize.TakeProfit == 0 {
			t.Error("TakeProfit should not be zero when OHLCV provided")
		}
	})

	t.Run("StopLoss_LessThanEntry", func(t *testing.T) {
		posSize, err := rm.CalculatePosition(ctx, signal, portfolio, regime, currentPrice, ohlcv)
		if err != nil {
			t.Fatalf("CalculatePosition failed: %v", err)
		}

		if posSize.StopLoss >= currentPrice {
			t.Errorf("StopLoss (%.2f) should be less than entry price (%.2f)", posSize.StopLoss, currentPrice)
		}
	})

	t.Run("TakeProfit_GreaterThanEntry", func(t *testing.T) {
		posSize, err := rm.CalculatePosition(ctx, signal, portfolio, regime, currentPrice, ohlcv)
		if err != nil {
			t.Fatalf("CalculatePosition failed: %v", err)
		}

		if posSize.TakeProfit <= currentPrice {
			t.Errorf("TakeProfit (%.2f) should be greater than entry price (%.2f)", posSize.TakeProfit, currentPrice)
		}
	})

	t.Run("DefaultATR_WhenInsufficientData", func(t *testing.T) {
		shortOHLCV := generateTestOHLCV(5, currentPrice, 0.03)
		posSize, err := rm.CalculatePosition(ctx, signal, portfolio, regime, currentPrice, shortOHLCV)
		if err != nil {
			t.Fatalf("CalculatePosition failed: %v", err)
		}

		if posSize.StopLoss == 0 {
			t.Error("should use default ATR (2%) when insufficient data")
		}
		if posSize.StopLoss >= currentPrice {
			t.Errorf("stop loss (%.2f) should be less than entry price", posSize.StopLoss)
		}
	})
}

func generateTestOHLCV(days int, basePrice float64, volatility float64) []domain.OHLCV {
	ohlcv := make([]domain.OHLCV, days)
	currentPrice := basePrice

	for i := 0; i < days; i++ {
		change := (0.5 - randFloat()) * 2 * volatility * basePrice
		currentPrice += change

		high := currentPrice + (randFloat()*volatility*basePrice*0.5)
		low := currentPrice - (randFloat()*volatility*basePrice*0.5)
		open_ := low + randFloat()*(high-low)
		volume := 1000000 + randFloat()*5000000

		ohlcv[i] = domain.OHLCV{
			Symbol:    "600000.SH",
			Date:      time.Date(2024, 1, 1+i, 0, 0, 0, 0, time.UTC),
			Open:      open_,
			High:      high,
			Low:       low,
			Close:     currentPrice,
			Volume:    volume,
			Turnover:  volume * currentPrice,
			TradeDays: i + 1,
		}
	}

	return ohlcv
}

func randFloat() float64 {
	return 0.5
}
