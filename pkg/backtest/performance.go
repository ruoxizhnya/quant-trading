package backtest

import (
	"math"
	"sort"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// CalculateMetrics computes all risk and performance metrics from portfolio values and trades.
func CalculateMetrics(portfolioValues []domain.PortfolioValue, trades []domain.Trade, riskFreeRate float64) domain.RiskMetrics {
	if len(portfolioValues) < 2 {
		return domain.RiskMetrics{}
	}

	// Calculate daily returns
	returns := CalculateReturns(portfolioValues)
	if len(returns) == 0 {
		return domain.RiskMetrics{}
	}

	// Calculate metrics
	sharpe := CalculateSharpeRatio(returns, riskFreeRate)
	sortino := CalculateSortinoRatio(returns, riskFreeRate)
	maxDD, maxDDDate := CalculateMaxDrawdown(portfolioValues)

	// Calculate volatility
	volatility := calculateVolatility(returns)

	return domain.RiskMetrics{
		Volatility:      volatility,
		SharpeRatio:     sharpe,
		SortinoRatio:    sortino,
		MaxDrawdown:     maxDD,
		MaxDrawdownDate: maxDDDate,
		VaR95:           calculateVaR(returns, 0.95),
		CVaR95:          calculateCVaR(returns, 0.95),
	}
}

// TradeMetrics contains trade-specific performance metrics.
type TradeMetrics struct {
	WinRate        float64
	TotalTrades    int
	WinTrades      int
	LoseTrades     int
	AvgHoldingDays float64
	CalmarRatio    float64
}

// CalculateTradeMetrics calculates trade-specific metrics.
func CalculateTradeMetrics(trades []domain.Trade, portfolioValues []domain.PortfolioValue) TradeMetrics {
	if len(trades) == 0 {
		return TradeMetrics{}
	}

	winRate, totalTrades, winTrades, loseTrades, avgHoldingDays := calculateTradeMetrics(trades, portfolioValues)

	// Calculate Calmar ratio
	annualReturn := calculateAnnualizedReturn(portfolioValues)
	maxDD, _ := CalculateMaxDrawdown(portfolioValues)
	calmar := CalculateCalmarRatio(annualReturn, math.Abs(maxDD))

	return TradeMetrics{
		WinRate:        winRate,
		TotalTrades:    totalTrades,
		WinTrades:      winTrades,
		LoseTrades:     loseTrades,
		AvgHoldingDays: avgHoldingDays,
		CalmarRatio:    calmar,
	}
}

// CalculateReturns calculates daily returns from portfolio values.
// Daily return = (today_value - yesterday_value - new_inflow) / yesterday_value
func CalculateReturns(portfolioValues []domain.PortfolioValue) []float64 {
	if len(portfolioValues) < 2 {
		return nil
	}

	returns := make([]float64, 0, len(portfolioValues)-1)
	for i := 1; i < len(portfolioValues); i++ {
		prevValue := portfolioValues[i-1].TotalValue
		currValue := portfolioValues[i].TotalValue

		// Calculate cash flow (new inflow/outflow)
		// This is simplified; in a real system we'd track actual cash flows
		cashFlow := portfolioValues[i].Cash - portfolioValues[i-1].Cash

		if prevValue > 0 {
			netValue := currValue - cashFlow
			dailyReturn := (netValue - prevValue) / prevValue
			returns = append(returns, dailyReturn)
		}
	}

	return returns
}

// CalculateMaxDrawdown calculates the maximum drawdown and its date.
func CalculateMaxDrawdown(portfolioValues []domain.PortfolioValue) (float64, time.Time) {
	if len(portfolioValues) == 0 {
		return 0, time.Time{}
	}

	maxDrawdown := 0.0
	maxDrawdownDate := portfolioValues[0].Date
	peak := portfolioValues[0].TotalValue

	for _, pv := range portfolioValues {
		if pv.TotalValue > peak {
			peak = pv.TotalValue
		}

		drawdown := (peak - pv.TotalValue) / peak
		if drawdown > maxDrawdown {
			maxDrawdown = drawdown
			maxDrawdownDate = pv.Date
		}
	}

	return -maxDrawdown, maxDrawdownDate // Return as negative percentage
}

// CalculateSharpeRatio calculates the Sharpe ratio.
// Sharpe = (mean_daily_return - risk_free_rate) / std(daily_returns) * sqrt(252)
func CalculateSharpeRatio(returns []float64, riskFreeRate float64) float64 {
	if len(returns) < 2 {
		return 0
	}

	// Daily risk-free rate
	dailyRF := riskFreeRate / 252

	// Mean return
	meanReturn := mean(returns)

	// Standard deviation
	stdDev := standardDeviation(returns)

	if stdDev == 0 {
		return 0
	}

	// Annualized Sharpe ratio
	sharpe := (meanReturn - dailyRF) / stdDev * math.Sqrt(252)

	return sharpe
}

// CalculateSortinoRatio calculates the Sortino ratio.
// Sortino = (mean_daily_return - risk_free_rate) / std(downside_returns) * sqrt(252)
func CalculateSortinoRatio(returns []float64, riskFreeRate float64) float64 {
	if len(returns) < 2 {
		return 0
	}

	// Daily risk-free rate
	dailyRF := riskFreeRate / 252

	// Mean return
	meanReturn := mean(returns)

	// Downside deviation (only negative returns)
	downsideReturns := filterNegative(returns)
	if len(downsideReturns) == 0 {
		// No downside returns means perfect positive skew
		return math.MaxFloat64
	}

	downsideDev := standardDeviation(downsideReturns)
	if downsideDev == 0 {
		return 0
	}

	// Annualized Sortino ratio
	sortino := (meanReturn - dailyRF) / downsideDev * math.Sqrt(252)

	return sortino
}

// CalculateCalmarRatio calculates the Calmar ratio.
// Calmar = annual_return / max_drawdown
func CalculateCalmarRatio(annualReturn float64, maxDrawdown float64) float64 {
	if maxDrawdown == 0 {
		return 0
	}
	return annualReturn / maxDrawdown
}

// calculateAnnualizedReturn calculates the annualized return from portfolio values.
func calculateAnnualizedReturn(portfolioValues []domain.PortfolioValue) float64 {
	if len(portfolioValues) < 2 {
		return 0
	}

	startValue := portfolioValues[0].TotalValue
	endValue := portfolioValues[len(portfolioValues)-1].TotalValue

	if startValue <= 0 {
		return 0
	}

	// Calculate number of years
	startDate := portfolioValues[0].Date
	endDate := portfolioValues[len(portfolioValues)-1].Date
	years := endDate.Sub(startDate).Hours() / (24 * 365.25)

	if years < 0.01 {
		return 0
	}

	// Annualized return
	totalReturn := endValue / startValue
	annualizedReturn := math.Pow(totalReturn, 1/years) - 1

	return annualizedReturn
}

// calculateVolatility calculates the annualized volatility.
func calculateVolatility(returns []float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	return standardDeviation(returns) * math.Sqrt(252)
}

// calculateVaR calculates Value at Risk at the given confidence level.
func calculateVaR(returns []float64, confidence float64) float64 {
	if len(returns) == 0 {
		return 0
	}

	sorted := make([]float64, len(returns))
	copy(sorted, returns)
	sort.Float64s(sorted)

	// VaR is the loss at the (1-confidence) percentile
	idx := int(float64(len(sorted)) * (1 - confidence))
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}

	return sorted[idx]
}

// calculateCVaR calculates Conditional Value at Risk (Expected Shortfall).
func calculateCVaR(returns []float64, confidence float64) float64 {
	if len(returns) == 0 {
		return 0
	}

	sorted := make([]float64, len(returns))
	copy(sorted, returns)
	sort.Float64s(sorted)

	// CVaR is the mean of losses beyond VaR
	idx := int(float64(len(sorted)) * (1 - confidence))
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}

	if idx == 0 {
		return sorted[0]
	}

	// Mean of the worst (1-confidence)% returns
	sum := 0.0
	for i := 0; i <= idx; i++ {
		sum += sorted[i]
	}

	return sum / float64(idx+1)
}

// calculateTradeMetrics calculates trade statistics.
func calculateTradeMetrics(trades []domain.Trade, portfolioValues []domain.PortfolioValue) (winRate float64, totalTrades, winTrades, loseTrades int, avgHoldingDays float64) {
	if len(trades) == 0 {
		return 0, 0, 0, 0, 0
	}

	// Calculate trade PnLs
	// Group trades by symbol and calculate realized PnL
	symbolTrades := make(map[string][]domain.Trade)
	for _, trade := range trades {
		symbolTrades[trade.Symbol] = append(symbolTrades[trade.Symbol], trade)
	}

	var winningTrades, losingTrades int
	var totalHoldingDays float64
	var holdingDaysCount int

	for _, symTrades := range symbolTrades {
		// Calculate position changes to determine entry/exit
		var entryPrice, exitPrice float64
		var entryTime time.Time
		var positionQty float64
		var realizedPnL float64

		for i, trade := range symTrades {
			switch trade.Direction {
			case domain.DirectionLong:
				if positionQty == 0 {
					entryPrice = trade.Price
					entryTime = trade.Timestamp
				}
				positionQty += trade.Quantity

			case domain.DirectionShort:
				if positionQty == 0 {
					entryPrice = trade.Price
					entryTime = trade.Timestamp
				}
				positionQty -= trade.Quantity

			case domain.DirectionClose:
				exitPrice = trade.Price
				if positionQty > 0 {
					// Closing long
					pnl := (exitPrice - entryPrice) * min(trade.Quantity, positionQty)
					realizedPnL = pnl - trade.Commission
				} else if positionQty < 0 {
					// Closing short
					pnl := (entryPrice - exitPrice) * min(trade.Quantity, -positionQty)
					realizedPnL = pnl - trade.Commission
				}

				if positionQty != 0 {
					holdingDays := trade.Timestamp.Sub(entryTime).Hours() / 24
					totalHoldingDays += holdingDays
					holdingDaysCount++

					if realizedPnL > 0 {
						winningTrades++
					} else if realizedPnL < 0 {
						losingTrades++
					}

					// Reset for next round trip
					positionQty = 0

					// If there's remaining quantity to close, start new tracking
					remainingQty := abs(trade.Quantity) - abs(positionQty)
					if remainingQty > 0 && i+1 < len(symTrades) {
						// Look ahead for next entry
					}
				}
			}
		}
	}

	totalTrades = winningTrades + losingTrades
	if totalTrades > 0 {
		winRate = float64(winningTrades*100) / float64(totalTrades)
	}
	if holdingDaysCount > 0 {
		avgHoldingDays = totalHoldingDays / float64(holdingDaysCount)
	}

	return float64(winRate) / 100.0, totalTrades, winningTrades, losingTrades, avgHoldingDays
}

// Helper functions
func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func standardDeviation(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}

	m := mean(values)
	sumSquares := 0.0
	for _, v := range values {
		diff := v - m
		sumSquares += diff * diff
	}

	return math.Sqrt(sumSquares / float64(len(values)-1))
}

func filterNegative(values []float64) []float64 {
	var negative []float64
	for _, v := range values {
		if v < 0 {
			negative = append(negative, v)
		}
	}
	return negative
}

// GenerateBacktestResult creates a BacktestResult from tracker data and metrics.
func GenerateBacktestResult(
	portfolioValues []domain.PortfolioValue,
	trades []domain.Trade,
	riskFreeRate float64,
	startDate, endDate time.Time,
	initialCapital float64,
) domain.BacktestResult {
	if len(portfolioValues) == 0 {
		return domain.BacktestResult{
			StartDate: startDate,
			EndDate:   endDate,
		}
	}

	endValue := portfolioValues[len(portfolioValues)-1].TotalValue
	startValue := portfolioValues[0].TotalValue

	// Total return
	totalReturn := 0.0
	if startValue > 0 {
		totalReturn = (endValue - startValue) / startValue
	}

	// Annualized return
	years := endDate.Sub(startDate).Hours() / (24 * 365.25)
	annualReturn := totalReturn
	if years > 0 && startValue > 0 {
		annualReturn = math.Pow(1+totalReturn, 1/years) - 1
	}

	// Calculate returns and metrics
	returns := CalculateReturns(portfolioValues)
	sharpe := CalculateSharpeRatio(returns, riskFreeRate)
	sortino := CalculateSortinoRatio(returns, riskFreeRate)
	maxDD, maxDDDate := CalculateMaxDrawdown(portfolioValues)
	calmar := CalculateCalmarRatio(annualReturn, math.Abs(maxDD))

	// Trade metrics
	winRate, totalTrades, winTrades, loseTrades, avgHoldingDays := calculateTradeMetrics(trades, portfolioValues)

	return domain.BacktestResult{
		StartDate:        startDate,
		EndDate:          endDate,
		TotalReturn:      totalReturn,
		AnnualReturn:     annualReturn,
		SharpeRatio:      sharpe,
		SortinoRatio:     sortino,
		MaxDrawdown:      maxDD,
		MaxDrawdownDate:  maxDDDate,
		WinRate:          winRate,
		TotalTrades:      totalTrades,
		WinTrades:        winTrades,
		LoseTrades:       loseTrades,
		AvgHoldingDays:   avgHoldingDays,
		CalmarRatio:      calmar,
		PortfolioValues:  portfolioValues,
		Trades:           trades,
	}
}
