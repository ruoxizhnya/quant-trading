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
	strat, err := strategy.DefaultRegistry.Get("multi_factor")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	// Simulate backtest data for multiple days
	basePrice := 10.0
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Simulate 100 trading days
	for day := 20; day < 100; day++ {
		bars := make(map[string][]domain.OHLCV)
		var data []domain.OHLCV
		for i := 0; i <= day; i++ {
			price := basePrice + float64(i)*0.05
			data = append(data, domain.OHLCV{
				Date:   startDate.AddDate(0, 0, i),
				Open:   price - 0.02,
				High:   price + 0.03,
				Low:    price - 0.03,
				Close:  price,
				Volume: 1000000,
			})
		}
		bars["600000.SH"] = data

		currentDate := startDate.AddDate(0, 0, day)
		signals, err := strat.GenerateSignals(context.Background(), bars, nil)
		if err != nil {
			fmt.Println("Error:", err)
			return
		}

		if len(signals) > 0 {
			fmt.Printf("Day %d (%s): %d signals\n", day, currentDate.Format("2006-01-02"), len(signals))
			for _, s := range signals {
				fmt.Printf("  %s %s strength=%.4f price=%.2f date=%s\n",
					s.Symbol, s.Action, s.Strength, s.Price, s.Date)
			}
		}
	}
}
