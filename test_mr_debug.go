package main

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	_ "github.com/ruoxizhnya/quant-trading/pkg/strategy/plugins"
)

func main() {
	bars := createOscillatingData()
	data := bars["600000.SH"]

	// Calculate MA for last day
	maPeriod := 20
	var sum float64
	for i := len(data) - maPeriod; i < len(data); i++ {
		sum += data[i].Close
	}
	ma := sum / float64(maPeriod)
	latestPrice := data[len(data)-1].Close
	priceRatio := latestPrice / ma

	fmt.Printf("Latest price: %.2f\n", latestPrice)
	fmt.Printf("MA: %.2f\n", ma)
	fmt.Printf("Price/MA ratio: %.4f\n", priceRatio)
	fmt.Printf("Buy threshold: 0.98\n")
	fmt.Printf("Sell threshold: 1.02\n")

	// Test mean_reversion
	strat, _ := strategy.DefaultRegistry.Get("mean_reversion")
	signals, _ := strat.GenerateSignals(context.Background(), bars, nil)
	fmt.Printf("mean_reversion signals: %d\n", len(signals))
	for _, s := range signals {
		fmt.Printf("  %s %s strength=%.4f price=%.2f\n", s.Symbol, s.Action, s.Strength, s.Price)
	}
}

func createOscillatingData() map[string][]domain.OHLCV {
	basePrice := 10.0
	startDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	bars := make(map[string][]domain.OHLCV)
	var data []domain.OHLCV
	for i := 0; i < 100; i++ {
		price := basePrice + math.Sin(float64(i)*0.3)*2.0 + float64(i)*0.02
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
