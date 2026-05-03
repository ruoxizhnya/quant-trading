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
	strat, err := strategy.DefaultRegistry.Get("mean_reversion")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	// Simulate backtest data - same as engine provides
	basePrice := 10.0
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Create bars with 100 days of data (same as backtest)
	bars := make(map[string][]domain.OHLCV)
	var data []domain.OHLCV
	for i := 0; i < 100; i++ {
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

	// Create portfolio
	portfolio := &domain.Portfolio{
		TotalValue: 1000000,
		Cash:       1000000,
		Positions:  make(map[string]domain.Position),
		UpdatedAt:  time.Now(),
	}

	// Test signal generation
	signals, err := strat.GenerateSignals(context.Background(), bars, portfolio)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("Generated %d signals\n", len(signals))
	for _, s := range signals {
		fmt.Printf("  %s %s strength=%.4f price=%.2f\n", s.Symbol, s.Action, s.Strength, s.Price)
	}
}
