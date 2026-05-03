package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	_ "github.com/ruoxizhnya/quant-trading/pkg/strategy/plugins"
)

func main() {
	strat, err := strategy.DefaultRegistry.Get("td_sequential")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	// Test case 1: Perfect bearish setup (9 consecutive closes below close 4 bars ago)
	fmt.Println("=== Test 1: Perfect bearish setup ===")
	bars1 := createPerfectBearishSetup()
	signals1, _ := strat.GenerateSignals(context.Background(), bars1, nil)
	fmt.Printf("Signals: %d\n", len(signals1))
	for _, s := range signals1 {
		fmt.Printf("  %s %s strength=%.4f\n", s.Symbol, s.Action, s.Strength)
	}

	// Test case 2: Mixed data - should still find setup
	fmt.Println("\n=== Test 2: Mixed data with setup ===")
	bars2 := createMixedDataWithSetup()
	signals2, _ := strat.GenerateSignals(context.Background(), bars2, nil)
	fmt.Printf("Signals: %d\n", len(signals2))
	for _, s := range signals2 {
		fmt.Printf("  %s %s strength=%.4f\n", s.Symbol, s.Action, s.Strength)
	}

	// Test case 3: Realistic data - prices going down
	fmt.Println("\n=== Test 3: Realistic downtrend ===")
	bars3 := createRealisticDowntrend()
	signals3, _ := strat.GenerateSignals(context.Background(), bars3, nil)
	fmt.Printf("Signals: %d\n", len(signals3))
	for _, s := range signals3 {
		fmt.Printf("  %s %s strength=%.4f\n", s.Symbol, s.Action, s.Strength)
	}
}

func createPerfectBearishSetup() map[string][]domain.OHLCV {
	bars := make(map[string][]domain.OHLCV)
	var data []domain.OHLCV
	basePrice := 10.0
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Create 20 bars with perfect bearish setup
	// Each close is below the close 4 bars ago
	for i := 0; i < 20; i++ {
		price := basePrice - float64(i)*0.1
		data = append(data, domain.OHLCV{
			Date:   startDate.AddDate(0, 0, i),
			Open:   price + 0.02,
			High:   price + 0.05,
			Low:    price - 0.02,
			Close:  price,
			Volume: 1000000,
		})
	}
	bars["600000.SH"] = data
	return bars
}

func createMixedDataWithSetup() map[string][]domain.OHLCV {
	bars := make(map[string][]domain.OHLCV)
	var data []domain.OHLCV
	basePrice := 10.0
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Create 20 bars with mixed data
	// First 10 bars: uptrend, then 9 bars: downtrend (bearish setup)
	for i := 0; i < 20; i++ {
		var price float64
		if i < 10 {
			price = basePrice + float64(i)*0.1 // uptrend
		} else {
			price = basePrice + 1.0 - float64(i-10)*0.15 // downtrend
		}
		data = append(data, domain.OHLCV{
			Date:   startDate.AddDate(0, 0, i),
			Open:   price + 0.02,
			High:   price + 0.05,
			Low:    price - 0.02,
			Close:  price,
			Volume: 1000000,
		})
	}
	bars["600000.SH"] = data
	return bars
}

func createRealisticDowntrend() map[string][]domain.OHLCV {
	bars := make(map[string][]domain.OHLCV)
	var data []domain.OHLCV
	basePrice := 10.0
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Create 20 bars with realistic downtrend
	for i := 0; i < 20; i++ {
		price := basePrice - float64(i)*0.05
		data = append(data, domain.OHLCV{
			Date:   startDate.AddDate(0, 0, i),
			Open:   price + 0.02,
			High:   price + 0.05,
			Low:    price - 0.02,
			Close:  price,
			Volume: 1000000,
		})
	}
	bars["600000.SH"] = data
	return bars
}
