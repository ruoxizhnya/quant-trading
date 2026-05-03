package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	_ "github.com/ruoxizhnya/quant-trading/pkg/strategy/plugins"
)

func main() {
	bars := createOscillatingData()
	data := bars["600000.SH"]

	// Calculate z-score for last day
	period := 20
	sorted := make([]domain.OHLCV, len(data))
	copy(sorted, data)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date.Before(sorted[j].Date) })

	window := sorted[len(sorted)-period:]
	var sum, sumSq float64
	for _, b := range window {
		sum += b.Close
		sumSq += b.Close * b.Close
	}
	mean := sum / float64(period)
	variance := sumSq/float64(period) - mean*mean
	if variance < 0 {
		variance = 0
	}
	sd := math.Sqrt(variance)
	latestPrice := sorted[len(sorted)-1].Close
	zScore := (latestPrice - mean) / sd

	fmt.Printf("Latest price: %.2f\n", latestPrice)
	fmt.Printf("Mean: %.2f\n", mean)
	fmt.Printf("StdDev: %.2f\n", sd)
	fmt.Printf("Z-Score: %.2f\n", zScore)
	fmt.Printf("Buy threshold: -1.5\n")
	fmt.Printf("Sell threshold: 1.5\n")

	// Test bollinger_mr
	strat, _ := strategy.DefaultRegistry.Get("bollinger_mr")
	signals, _ := strat.GenerateSignals(context.Background(), bars, nil)
	fmt.Printf("bollinger_mr signals: %d\n", len(signals))
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
