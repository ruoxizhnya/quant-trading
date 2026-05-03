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
	// Test bollinger_mr
	strat, _ := strategy.DefaultRegistry.Get("bollinger_mr")
	bars := createTestData()
	signals, _ := strat.GenerateSignals(context.Background(), bars, nil)
	fmt.Printf("bollinger_mr: %d signals\n", len(signals))
	for _, s := range signals {
		fmt.Printf("  %s %s strength=%.4f price=%.2f\n", s.Symbol, s.Action, s.Strength, s.Price)
	}

	// Test mean_reversion
	strat2, _ := strategy.DefaultRegistry.Get("mean_reversion")
	signals2, _ := strat2.GenerateSignals(context.Background(), bars, nil)
	fmt.Printf("mean_reversion: %d signals\n", len(signals2))
	for _, s := range signals2 {
		fmt.Printf("  %s %s strength=%.4f price=%.2f\n", s.Symbol, s.Action, s.Strength, s.Price)
	}
}

func createTestData() map[string][]domain.OHLCV {
	basePrice := 10.0
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

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
	return bars
}
