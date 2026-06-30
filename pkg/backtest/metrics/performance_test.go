package metrics

import (
	"math"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/statistics"
)

func makePortfolioValues(values []float64) []domain.PortfolioValue {
	pvs := make([]domain.PortfolioValue, len(values))
	baseDate := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	for i, v := range values {
		pvs[i] = domain.PortfolioValue{
			Date:       baseDate.AddDate(0, 0, i),
			TotalValue: v,
		}
	}
	return pvs
}

func TestCalculateReturns(t *testing.T) {
	pvs := makePortfolioValues([]float64{100, 101, 102, 100, 103})
	returns := CalculateReturns(pvs)

	if len(returns) != 4 {
		t.Fatalf("expected 4 returns, got %d", len(returns))
	}

	if abs(returns[0]-0.01) > 1e-6 {
		t.Errorf("expected first return 0.01, got %.6f", returns[0])
	}
	if abs(returns[1]-0.00990099) > 1e-4 {
		t.Errorf("expected second return ~0.0099, got %.6f", returns[1])
	}
}

func TestCalculateReturns_Empty(t *testing.T) {
	returns := CalculateReturns(nil)
	if len(returns) != 0 {
		t.Errorf("expected empty returns, got %d", len(returns))
	}
}

func TestCalculateReturns_SingleValue(t *testing.T) {
	pvs := makePortfolioValues([]float64{100})
	returns := CalculateReturns(pvs)
	if len(returns) != 0 {
		t.Errorf("expected 0 returns for single value, got %d", len(returns))
	}
}

func TestCalculateSharpeRatio(t *testing.T) {
	returns := []float64{0.01, 0.02, -0.01, 0.015, 0.005, -0.005, 0.02, 0.01, -0.015, 0.008}
	sharpe := CalculateSharpeRatio(returns, 0.03)

	if sharpe == 0 {
		t.Error("expected non-zero Sharpe ratio")
	}

	if math.IsInf(sharpe, 0) || math.IsNaN(sharpe) {
		t.Errorf("expected finite Sharpe ratio, got %.4f", sharpe)
	}
}

func TestCalculateSharpeRatio_ZeroStdDev(t *testing.T) {
	returns := []float64{0.01, 0.01, 0.01}
	sharpe := CalculateSharpeRatio(returns, 0.0)
	if sharpe != 0 {
		t.Errorf("expected 0 Sharpe for zero std dev, got %.4f", sharpe)
	}
}

func TestCalculateSharpeRatio_InsufficientData(t *testing.T) {
	sharpe := CalculateSharpeRatio([]float64{0.01}, 0.03)
	if sharpe != 0 {
		t.Errorf("expected 0 Sharpe for insufficient data, got %.4f", sharpe)
	}
}

func TestCalculateMaxDrawdown(t *testing.T) {
	pvs := makePortfolioValues([]float64{100, 110, 105, 95, 100, 90, 95})
	mdd, mddDate := CalculateMaxDrawdown(pvs)

	if mdd >= 0 {
		t.Errorf("expected negative max drawdown, got %.4f", mdd)
	}

	if abs(mdd-(-0.1818)) > 0.01 {
		t.Errorf("expected max drawdown ~-0.1818, got %.4f", mdd)
	}

	if mddDate.IsZero() {
		t.Error("expected non-zero max drawdown date")
	}
}

func TestCalculateMaxDrawdown_NoDrawdown(t *testing.T) {
	pvs := makePortfolioValues([]float64{100, 110, 120, 130})
	mdd, _ := CalculateMaxDrawdown(pvs)

	if mdd != 0 {
		t.Errorf("expected 0 max drawdown for monotonically increasing, got %.4f", mdd)
	}
}

func TestCalculateMaxDrawdown_SingleValue(t *testing.T) {
	pvs := makePortfolioValues([]float64{100})
	mdd, _ := CalculateMaxDrawdown(pvs)
	if mdd != 0 {
		t.Errorf("expected 0 max drawdown for single value, got %.4f", mdd)
	}
}

func TestCalculateSortinoRatio(t *testing.T) {
	returns := []float64{0.01, 0.02, -0.01, 0.015, 0.005, -0.005, 0.02, 0.01, -0.015, 0.008}
	sortino := CalculateSortinoRatio(returns, 0.03)

	if sortino == 0 {
		t.Error("expected non-zero Sortino ratio")
	}
}

func TestCalculateSortinoRatio_NoDownside(t *testing.T) {
	returns := []float64{0.01, 0.02, 0.015, 0.005}
	sortino := CalculateSortinoRatio(returns, 0.0)

	if sortino != math.MaxFloat64 {
		t.Errorf("expected MaxFloat64 for no downside, got %.4f", sortino)
	}
}

func TestCalculateCalmarRatio(t *testing.T) {
	calmar := CalculateCalmarRatio(0.20, -0.10)
	if abs(calmar+2.0) > 1e-6 {
		t.Errorf("expected Calmar ratio -2.0 (negative drawdown), got %.4f", calmar)
	}
}

func TestCalculateCalmarRatio_ZeroDrawdown(t *testing.T) {
	calmar := CalculateCalmarRatio(0.20, 0.0)
	if calmar != 0 {
		t.Errorf("expected 0 Calmar for zero drawdown, got %.4f", calmar)
	}
}

func TestCalculateMetrics(t *testing.T) {
	pvs := makePortfolioValues([]float64{1_000_000, 1_010_000, 1_005_000, 1_020_000, 1_015_000, 1_030_000})
	trades := []domain.Trade{
		{Symbol: "600519", Direction: domain.DirectionLong, Quantity: 100, Price: 50.0, Timestamp: time.Now()},
		{Symbol: "600519", Direction: domain.DirectionClose, Quantity: 100, Price: 55.0, Timestamp: time.Now()},
	}

	metrics := CalculateMetrics(pvs, trades, 0.03)

	if metrics.Volatility <= 0 {
		t.Errorf("expected positive volatility, got %.4f", metrics.Volatility)
	}
	if metrics.MaxDrawdown > 0 {
		t.Errorf("expected negative or zero max drawdown, got %.4f", metrics.MaxDrawdown)
	}
}

func TestCalculateTradeMetrics(t *testing.T) {
	date1 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	date2 := time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC)

	trades := []domain.Trade{
		{Symbol: "600519", Direction: domain.DirectionLong, Quantity: 100, Price: 50.0, Commission: 1.5, Timestamp: date1},
		{Symbol: "600519", Direction: domain.DirectionClose, Quantity: 100, Price: 55.0, Commission: 1.65, Timestamp: date2},
		{Symbol: "000858", Direction: domain.DirectionLong, Quantity: 200, Price: 30.0, Commission: 1.8, Timestamp: date1},
		{Symbol: "000858", Direction: domain.DirectionClose, Quantity: 200, Price: 28.0, Commission: 1.68, Timestamp: date2},
	}

	pvs := makePortfolioValues([]float64{1_000_000, 1_010_000, 1_005_000})

	winRate, totalTrades, winTrades, loseTrades, avgHoldingDays := calculateTradeMetrics(trades, pvs)

	if totalTrades != 2 {
		t.Errorf("expected 2 total trades, got %d", totalTrades)
	}
	if winTrades != 1 {
		t.Errorf("expected 1 win trade, got %d", winTrades)
	}
	if loseTrades != 1 {
		t.Errorf("expected 1 lose trade, got %d", loseTrades)
	}
	if abs(winRate-0.5) > 1e-6 {
		t.Errorf("expected win rate 0.5, got %.4f", winRate)
	}
	if avgHoldingDays <= 0 {
		t.Errorf("expected positive avg holding days, got %.2f", avgHoldingDays)
	}
}

func TestCalculateVaR(t *testing.T) {
	returns := make([]float64, 100)
	for i := range returns {
		returns[i] = float64(i-50) * 0.001
	}

	var95 := calculateVaR(returns, 0.95)
	if var95 >= 0 {
		t.Errorf("expected negative VaR, got %.4f", var95)
	}
}

func TestCalculateCVaR(t *testing.T) {
	returns := make([]float64, 100)
	for i := range returns {
		returns[i] = float64(i-50) * 0.001
	}

	cvar95 := calculateCVaR(returns, 0.95)
	if cvar95 >= 0 {
		t.Errorf("expected negative CVaR, got %.4f", cvar95)
	}

	if cvar95 > var95ForReturns(returns) {
		t.Errorf("CVaR should be worse (more negative) than VaR, got CVaR=%.4f", cvar95)
	}
}

func var95ForReturns(returns []float64) float64 {
	sorted := make([]float64, len(returns))
	copy(sorted, returns)
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] < sorted[i] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	idx := int(float64(len(sorted)) * 0.05)
	return sorted[idx]
}

func TestCalculateVolatility(t *testing.T) {
	returns := []float64{0.01, -0.01, 0.02, -0.005, 0.015}
	vol := calculateVolatility(returns)
	if vol <= 0 {
		t.Errorf("expected positive volatility, got %.4f", vol)
	}
}

func TestGenerateBacktestResult(t *testing.T) {
	pvs := makePortfolioValues([]float64{1_000_000, 1_010_000, 1_005_000, 1_020_000, 1_030_000})
	trades := []domain.Trade{
		{Symbol: "600519", Direction: domain.DirectionLong, Quantity: 100, Price: 50.0, Commission: 1.5, Timestamp: time.Now()},
		{Symbol: "600519", Direction: domain.DirectionClose, Quantity: 100, Price: 55.0, Commission: 1.65, Timestamp: time.Now()},
	}

	result := GenerateBacktestResult(pvs, trades, 0.03, pvs[0].Date, pvs[len(pvs)-1].Date, 1_000_000)

	if abs(result.TotalReturn-0.03) > 0.01 {
		t.Errorf("expected total return ~0.03, got %.4f", result.TotalReturn)
	}
	if result.SharpeRatio == 0 && len(pvs) > 2 {
		t.Error("expected non-zero Sharpe ratio for varied portfolio values")
	}
	if result.MaxDrawdown > 0 {
		t.Errorf("expected negative or zero max drawdown, got %.4f", result.MaxDrawdown)
	}
	if result.TotalTrades < 1 {
		t.Errorf("expected at least 1 total trade, got %d", result.TotalTrades)
	}
}

func TestMean(t *testing.T) {
	vals := []float64{1.0, 2.0, 3.0, 4.0, 5.0}
	m := statistics.Mean(vals)
	if abs(m-3.0) > 1e-6 {
		t.Errorf("expected mean 3.0, got %.4f", m)
	}
}

func TestStandardDeviation(t *testing.T) {
	vals := []float64{2.0, 4.0, 4.0, 4.0, 5.0, 5.0, 7.0, 9.0}
	sd := statistics.SampleStdDev(vals)
	if sd <= 0 {
		t.Errorf("expected positive std dev, got %.4f", sd)
	}
	if abs(sd-2.0) > 0.5 {
		t.Errorf("expected std dev ~2.0, got %.4f", sd)
	}
}

func TestFilterNegative(t *testing.T) {
	vals := []float64{1.0, -2.0, 3.0, -4.0, 5.0}
	neg := filterNegative(vals)
	if len(neg) != 2 {
		t.Errorf("expected 2 negative values, got %d", len(neg))
	}
}
