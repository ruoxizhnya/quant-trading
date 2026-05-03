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

	// Test case: Recent 3 bars are bullish, but bars 4-12 have bearish setup
	// Current implementation will break at first bullish bar (bar 19)
	// and miss the setup in bars 10-16
	fmt.Println("=== Test: Recent bars bullish, earlier bars have setup ===")
	bars := createRecentBreak3()
	signals, _ := strat.GenerateSignals(context.Background(), bars, nil)
	fmt.Printf("Signals: %d\n", len(signals))
	for _, s := range signals {
		fmt.Printf("  %s %s strength=%.4f\n", s.Symbol, s.Action, s.Strength)
	}

	// Manual analysis
	data := bars["600000.SH"]
	fmt.Println("\nManual analysis:")
	fmt.Println("Bar index | Close | Ref (i-4) | Close < Ref?")
	fmt.Println("----------|-------|-----------|------------")
	for i := len(data) - 1; i >= 4; i-- {
		refIdx := i - 4
		closeCurr := data[i].Close
		closeRef := data[refIdx].Close
		isBelow := closeCurr < closeRef
		fmt.Printf("%9d | %5.2f | %9.2f | %v\n", i, closeCurr, closeRef, isBelow)
	}
}

func createRecentBreak3() map[string][]domain.OHLCV {
	bars := make(map[string][]domain.OHLCV)
	var data []domain.OHLCV
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Create 20 bars:
	// - Bars 0-9: uptrend
	// - Bars 10-16: downtrend (bearish setup of 7 bars)
	// - Bars 17-19: uptrend (breaks sequence)
	// The setup should be found in bars 10-16, but current implementation
	// will break at bar 17 and miss it
	prices := []float64{
		10.0, 10.1, 10.2, 10.3, 10.4, // 0-4: uptrend
		10.5, 10.6, 10.7, 10.8, 10.9, // 5-9: uptrend
		10.8, 10.7, 10.6, 10.5, 10.4, // 10-14: downtrend
		10.3, 10.2, 10.1, 10.2, 10.3, // 15-19: downtrend, break at 17-19
	}

	for i, price := range prices {
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
