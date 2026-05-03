package main

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

type rankedStock struct {
	symbol         string
	compositeScore float64
	valueScore     float64
	qualityScore   float64
	momentumScore  float64
}

func main() {
	now := time.Now()
	basePrice := 100.0
	lookback := 60

	makeBars := func(symbol string, endPrice float64) []domain.OHLCV {
		startPrice := basePrice
		bars := make([]domain.OHLCV, lookback+2)
		for i := range bars {
			ratio := float64(i) / float64(len(bars)-1)
			bars[i] = domain.OHLCV{
				Symbol: symbol,
				Date:   now.AddDate(0, 0, -len(bars)+i),
				Close:  startPrice + (endPrice-startPrice)*ratio,
			}
		}
		return bars
	}

	bars := map[string][]domain.OHLCV{
		"A": makeBars("A", basePrice*1.10),
		"B": makeBars("B", basePrice*1.05),
		"C": makeBars("C", basePrice*1.01),
	}

	vw, qw, mw := 0.4, 0.3, 0.3

	var ranked []rankedStock
	for symbol, data := range bars {
		if len(data) < lookback+1 {
			continue
		}
		sorted := make([]domain.OHLCV, len(data))
		copy(sorted, data)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date.Before(sorted[j].Date) })

		endIdx := len(sorted) - 1
		startPrice := sorted[endIdx-lookback].Close
		endPrice := sorted[endIdx].Close
		if startPrice <= 0 {
			continue
		}

		momentumScore := (endPrice - startPrice) / startPrice

		var minPrice, maxPrice float64
		for i := endIdx - lookback; i <= endIdx; i++ {
			if i == endIdx-lookback || sorted[i].Low < minPrice {
				minPrice = sorted[i].Low
			}
			if i == endIdx-lookback || sorted[i].High > maxPrice {
				maxPrice = sorted[i].High
			}
		}
		valueScore := 0.5
		if maxPrice > minPrice {
			valueScore = 1.0 - (endPrice-minPrice)/(maxPrice-minPrice)
		}

		var sumPrice, sumSq float64
		for i := endIdx - lookback; i <= endIdx; i++ {
			sumPrice += sorted[i].Close
			sumSq += sorted[i].Close * sorted[i].Close
		}
		avgPrice := sumPrice / float64(lookback+1)
		variance := sumSq/float64(lookback+1) - avgPrice*avgPrice
		if variance < 0 {
			variance = 0
		}
		stdDev := math.Sqrt(variance)
		cv := stdDev / avgPrice
		qualityScore := 1.0 / (1.0 + cv*10.0)
		if momentumScore > 0 {
			qualityScore *= (1.0 + momentumScore)
		}

		// Original: composite := vw*valueScore + qw*qualityScore + mw*math.Abs(momentumScore)
		composite := vw*valueScore + qw*qualityScore + mw*math.Max(momentumScore, 0)
		if composite < 0.1 {
			composite = 0.1
		}

		fmt.Printf("Stock %s: momentum=%.4f value=%.4f quality=%.4f composite=%.4f\n",
			symbol, momentumScore, valueScore, qualityScore, composite)

		ranked = append(ranked, rankedStock{
			symbol:         symbol,
			compositeScore: composite,
			valueScore:     valueScore,
			qualityScore:   qualityScore,
			momentumScore:  momentumScore,
		})
	}

	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].compositeScore > ranked[j].compositeScore
	})

	fmt.Println("\nRanking:")
	for i, r := range ranked {
		fmt.Printf("%d. %s: composite=%.4f\n", i+1, r.symbol, r.compositeScore)
	}
}
