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

	// Test case: Setup exists but most recent bar breaks the sequence
	// This is the problematic case - the current implementation will break
	// and miss the setup that exists in earlier bars
	fmt.Println("=== Test: Setup exists but recent bar breaks sequence ===")
	bars := createSetupWithBreak()
	signals, _ := strat.GenerateSignals(context.Background(), bars, nil)
	fmt.Printf("Signals: %d\n", len(signals))
	for _, s := range signals {
		fmt.Printf("  %s %s strength=%.4f\n", s.Symbol, s.Action, s.Strength)
	}

	// Let's manually check what the setup count should be
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

func createSetupWithBreak() map[string][]domain.OHLCV {
	bars := make(map[string][]domain.OHLCV)
	var data []domain.OHLCV
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Create 20 bars:
	// - Bars 0-9: uptrend (close[i] > close[i-4])
	// - Bars 10-18: downtrend (close[i] < close[i-4]) - this forms a bearish setup
	// - Bar 19: uptrend (close[19] > close[15]) - this breaks the sequence
	prices := []float64{
		10.0, 10.1, 10.2, 10.3, 10.4, // 0-4: uptrend
		10.5, 10.6, 10.7, 10.8, 10.9, // 5-9: uptrend
		10.8, 10.7, 10.6, 10.5, 10.4, // 10-14: downtrend
		10.3, 10.2, 10.1, 10.0, 10.1, // 15-19: downtrend then break at 19
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
