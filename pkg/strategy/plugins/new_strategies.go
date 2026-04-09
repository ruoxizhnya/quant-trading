package plugins

import (
	"context"
	"math"
	"sort"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

type TDSequentialConfig struct {
	SetupCount    int
	CancelDays    int
	CountdownFrom int
}

type tdSequentialStrategy struct {
	name        string
	description string
	params      TDSequentialConfig
}

func (s *tdSequentialStrategy) Name() string { return "td_sequential" }

func (s *tdSequentialStrategy) Description() string {
	return "TD Sequential: Tom DeMark's sequential indicator, buy on bearish setup completion (9 consecutive closes below close 4 bars ago), sell on bullish setup"
}

func (s *tdSequentialStrategy) Parameters() []strategy.Parameter {
	return []strategy.Parameter{
		{Name: "setup_count", Type: "int", Default: 9, Description: "Number of consecutive closes to complete a setup", Min: 5, Max: 13},
		{Name: "cancel_days", Type: "int", Default: 4, Description: "Setup cancellation lookback days", Min: 2, Max: 8},
		{Name: "countdown_from", Type: "int", Default: 13, Description: "Countdown start threshold (TDST)", Min: 8, Max: 20},
	}
}

func (s *tdSequentialStrategy) GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]strategy.Signal, error) {
	if len(bars) == 0 {
		return nil, nil
	}
	setupN := s.params.SetupCount
	if setupN <= 0 {
		setupN = 9
	}
	cancelN := s.params.CancelDays
	if cancelN <= 0 {
		cancelN = 4
	}
	var signals []strategy.Signal
	for symbol, data := range bars {
		if len(data) < setupN+cancelN+2 {
			continue
		}
		sorted := make([]domain.OHLCV, len(data))
		copy(sorted, data)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date.Before(sorted[j].Date) })

		bearishSetup := 0
		bullishSetup := 0
		for i := cancelN; i < len(sorted); i++ {
			refIdx := i - cancelN
			if refIdx < 0 {
				continue
			}
			closeCurr := sorted[i].Close
			closeRef := sorted[refIdx].Close

			if closeCurr < closeRef {
				bearishSetup++
				bullishSetup = 0
			} else if closeCurr > closeRef {
				bullishSetup++
				bearishSetup = 0
			} else {
				bearishSetup = 0
				bullishSetup = 0
			}
		}

		latestPrice := sorted[len(sorted)-1].Close
		if latestPrice <= 0 {
			continue
		}

		if bearishSetup >= setupN {
			strength := math.Min(float64(bearishSetup)/float64(setupN), 1.0)
			signals = append(signals, strategy.Signal{
				Symbol:   symbol,
				Action:   "buy",
				Strength: strength,
				Price:    latestPrice,
				Factors:  map[string]float64{"bearish_setup": float64(bearishSetup)},
			})
		} else if bullishSetup >= setupN {
			strength := math.Min(float64(bullishSetup)/float64(setupN), 1.0)
			if portfolio != nil {
				if pos, ok := portfolio.Positions[symbol]; ok && pos.Quantity > 0 {
					signals = append(signals, strategy.Signal{
						Symbol:   symbol,
						Action:   "sell",
						Strength: strength,
						Price:    latestPrice,
						Factors:  map[string]float64{"bullish_setup": float64(bullishSetup)},
					})
				}
			}
		}
	}
	return signals, nil
}

func (s *tdSequentialStrategy) Configure(params map[string]any) error {
	if v, ok := params["setup_count"]; ok { switch val := v.(type) { case float64: s.params.SetupCount = int(val); case int: s.params.SetupCount = val } }
	if v, ok := params["cancel_days"]; ok { switch val := v.(type) { case float64: s.params.CancelDays = int(val); case int: s.params.CancelDays = val } }
	if v, ok := params["countdown_from"]; ok { switch val := v.(type) { case float64: s.params.CountdownFrom = int(val); case int: s.params.CountdownFrom = val } }
	return nil
}

func (s *tdSequentialStrategy) Weight(signal strategy.Signal, _ float64) float64 {
	w := signal.Strength * 0.12
	if w > 0.08 { w = 0.08 }
	if w < 0.01 { w = 0.01 }
	return w
}

func (s *tdSequentialStrategy) Cleanup() {}

func init() {
	strategy.GlobalRegister(&tdSequentialStrategy{
		name: "td_sequential",
		description: "TD Sequential: Tom DeMark's sequential indicator for trend exhaustion detection",
		params: TDSequentialConfig{SetupCount: 9, CancelDays: 4, CountdownFrom: 13},
	})
}

type BollingerMRConfig struct {
	Period      int
	StdDev      float64
	BuyZScore   float64
	SellZScore  float64
}

type bollingerMRStrategy struct {
	name        string
	description string
	params      BollingerMRConfig
}

func (s *bollingerMRStrategy) Name() string { return "bollinger_mr" }

func (s *bollingerMRStrategy) Description() string {
	return "Bollinger Mean Reversion: buy when price touches lower band (oversold), sell when price touches upper band (overbought)"
}

func (s *bollingerMRStrategy) Parameters() []strategy.Parameter {
	return []strategy.Parameter{
		{Name: "period", Type: "int", Default: 20, Description: "Bollinger Band period", Min: 5, Max: 50},
		{Name: "std_dev", Type: "float", Default: 2.0, Description: "Standard deviation multiplier", Min: 1.0, Max: 3.0},
		{Name: "buy_zscore", Type: "float", Default: -2.0, Description: "Buy threshold (z-score below mean)", Min: -3.0, Max: -0.5},
		{Name: "sell_zscore", Type: "float", Default: 2.0, Description: "Sell threshold (z-score above mean)", Min: 0.5, Max: 3.0},
	}
}

func (s *bollingerMRStrategy) GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]strategy.Signal, error) {
	if len(bars) == 0 {
		return nil, nil
	}
	period := s.params.Period
	if period <= 0 { period = 20 }
	stdDev := s.params.StdDev
	if stdDev <= 0 { stdDev = 2.0 }
	var signals []strategy.Signal
	for symbol, data := range bars {
		if len(data) < period { continue }
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
		if variance < 0 { variance = 0 }
		sd := math.Sqrt(variance)

		latestPrice := sorted[len(sorted)-1].Close
		if sd <= 0 || latestPrice <= 0 { continue }

		zScore := (latestPrice - mean) / sd

		if zScore <= s.params.BuyZScore {
			strength := math.Min(math.Abs(zScore-s.params.BuyZScore)/(math.Abs(s.params.BuyZScore)+1), 1.0)
			lowerBand := mean - stdDev*sd
			signals = append(signals, strategy.Signal{
				Symbol:    symbol,
				Action:    "buy",
				Strength:  strength,
				Price:     latestPrice,
				OrderType: domain.OrderTypeLimit,
				LimitPrice: lowerBand,
				Factors:   map[string]float64{"z_score": zScore, "lower_band": lowerBand, "upper_band": mean + stdDev*sd},
			})
		} else if zScore >= s.params.SellZScore {
			strength := math.Min((zScore-s.params.SellZScore)/(s.params.SellZScore+1), 1.0)
			if portfolio != nil {
				if pos, ok := portfolio.Positions[symbol]; ok && pos.Quantity > 0 {
					upperBand := mean + stdDev*sd
					signals = append(signals, strategy.Signal{
						Symbol:    symbol,
						Action:    "sell",
						Strength:  strength,
						Price:     latestPrice,
						OrderType: domain.OrderTypeLimit,
						LimitPrice: upperBand,
						Factors:   map[string]float64{"z_score": zScore, "lower_band": mean - stdDev*sd, "upper_band": upperBand},
					})
				}
			}
		}
	}
	return signals, nil
}

func (s *bollingerMRStrategy) Configure(params map[string]any) error {
	if v, ok := params["period"]; ok { switch val := v.(type) { case float64: s.params.Period = int(val); case int: s.params.Period = val } }
	if v, ok := params["std_dev"]; ok { switch val := v.(type) { case float64: s.params.StdDev = val; case int: s.params.StdDev = float64(val) } }
	if v, ok := params["buy_zscore"]; ok { switch val := v.(type) { case float64: s.params.BuyZScore = val; case int: s.params.BuyZScore = float64(val) } }
	if v, ok := params["sell_zscore"]; ok { switch val := v.(type) { case float64: s.params.SellZScore = val; case int: s.params.SellZScore = float64(val) } }
	return nil
}

func (s *bollingerMRStrategy) Weight(signal strategy.Signal, _ float64) float64 {
	w := signal.Strength * 0.10
	if w > 0.06 { w = 0.06 }
	if w < 0.01 { w = 0.01 }
	return w
}

func (s *bollingerMRStrategy) Cleanup() {}

func init() {
	strategy.GlobalRegister(&bollingerMRStrategy{
		name: "bollinger_mr",
		description: "Bollinger Mean Reversion: trade band bounces with z-score thresholds",
		params: BollingerMRConfig{Period: 20, StdDev: 2.0, BuyZScore: -2.0, SellZScore: 2.0},
	})
}

type VPTConfig struct {
	SMALookback int
	TopN        int
}

type vptStrategy struct {
	name        string
	description string
	params      VPTConfig
}

func (s *vptStrategy) Name() string { return "volume_price_trend" }

func (s *vptStrategy) Description() string {
	return "Volume-Price Trend: buy stocks with rising VPT (price×volume momentum), sell falling VPT"
}

func (s *vptStrategy) Parameters() []strategy.Parameter {
	return []strategy.Parameter{
		{Name: "sma_lookback", Type: "int", Default: 20, Description: "SMA period for VPT smoothing", Min: 5, Max: 60},
		{Name: "top_n", Type: "int", Default: 5, Description: "Number of top VPT stocks to buy", Min: 1, Max: 20},
	}
}

func (s *vptStrategy) GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]strategy.Signal, error) {
	if len(bars) == 0 {
		return nil, nil
	}
	lookback := s.params.SMALookback
	if lookback <= 0 { lookback = 20 }
	topN := s.params.TopN
	if topN <= 0 { topN = 5 }

	type vptResult struct {
		symbol    string
		vpt       float64
		vptSlope  float64
		price     float64
	}
	var results []vptResult

	for symbol, data := range bars {
		if len(data) < lookback+1 { continue }
		sorted := make([]domain.OHLCV, len(data))
		copy(sorted, data)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date.Before(sorted[j].Date) })

		vpt := 0.0
		prevClose := sorted[0].Close
		for i := 1; i < len(sorted); i++ {
			changePct := 0.0
			if prevClose != 0 {
				changePct = (sorted[i].Close - prevClose) / prevClose
			}
			vpt += changePct * sorted[i].Volume
			prevClose = sorted[i].Close
		}

		if len(sorted) >= lookback+1 {
			recentWindow := sorted[len(sorted)-lookback:]
			vptRecent := 0.0
			pC := recentWindow[0].Close
			for i := 1; i < len(recentWindow); i++ {
				cp := 0.0
				if pC != 0 { cp = (recentWindow[i].Close - pC) / pC }
				vptRecent += cp * recentWindow[i].Volume
				pC = recentWindow[i].Close
			}
			oldVPT := 0.0
			oldWindow := sorted[len(sorted)-lookback-1 : len(sorted)-lookback]
			oC := oldWindow[0].Close
			for i := 1; i < len(oldWindow); i++ {
				cp := 0.0
				if oC != 0 { cp = (oldWindow[i].Close - oC) / oC }
				oldVPT += cp * oldWindow[i].Volume
				oC = oldWindow[i].Close
			}
			results = append(results, vptResult{symbol: symbol, vpt: vpt, vptSlope: vptRecent - oldVPT, price: sorted[len(sorted)-1].Close})
		} else {
			results = append(results, vptResult{symbol: symbol, vpt: vpt, vptSlope: vpt, price: sorted[len(sorted)-1].Close})
		}
	}

	sort.Slice(results, func(i, j int) bool { return results[i].vptSlope > results[j].vptSlope })

	var signals []strategy.Signal
	for i, r := range results {
		if i >= topN { break }
		if r.price <= 0 { continue }
		maxSlope := results[0].vptSlope
		minSlope := results[len(results)-1].vptSlope
		strength := 0.5
		if maxSlope != minSlope {
			strength = (r.vptSlope - minSlope) / (maxSlope - minSlope)
		}
		signals = append(signals, strategy.Signal{
			Symbol:   r.symbol,
			Action:   "buy",
			Strength: strength,
			Price:    r.price,
			Factors:  map[string]float64{"vpt": r.vpt, "vpt_slope": r.vptSlope},
		})
	}

	if portfolio != nil && portfolio.Positions != nil {
		for _, r := range results[len(results)-topN:] {
			if r.vptSlope < 0 {
				if pos, ok := portfolio.Positions[r.symbol]; ok && pos.Quantity > 0 {
					strength := math.Abs(r.vptSlope)
					maxAbs := 0.0
					for _, x := range results { if math.Abs(x.vptSlope) > maxAbs { maxAbs = math.Abs(x.vptSlope) } }
					if maxAbs > 0 { strength /= maxAbs }
					signals = append(signals, strategy.Signal{
						Symbol:   r.symbol,
						Action:   "sell",
						Strength: strength,
						Price:    r.price,
						Factors:  map[string]float64{"vpt": r.vpt, "vpt_slope": r.vptSlope},
					})
				}
			}
		}
	}
	return signals, nil
}

func (s *vptStrategy) Configure(params map[string]any) error {
	if v, ok := params["sma_lookback"]; ok { switch val := v.(type) { case float64: s.params.SMALookback = int(val); case int: s.params.SMALookback = val } }
	if v, ok := params["top_n"]; ok { switch val := v.(type) { case float64: s.params.TopN = int(val); case int: s.params.TopN = val } }
	return nil
}

func (s *vptStrategy) Weight(signal strategy.Signal, _ float64) float64 {
	w := signal.Strength * 0.10
	if w > 0.06 { w = 0.06 }
	if w < 0.01 { w = 0.01 }
	return w
}

func (s *vptStrategy) Cleanup() {}

func init() {
	strategy.GlobalRegister(&vptStrategy{
		name: "volume_price_trend",
		description: "Volume-Price Trend: volume-weighted price momentum indicator",
		params: VPTConfig{SMALookback: 20, TopN: 5},
	})
}

type VolBreakoutConfig struct {
	ATRPeriod    int
	ATRMultiplier float64
	Lookback     int
	TopN         int
}

type volBreakoutStrategy struct {
	name        string
	description string
	params      VolBreakoutConfig
}

func (s *volBreakoutStrategy) Name() string { return "volatility_breakout" }

func (s *volBreakoutStrategy) Description() string {
	return "Volatility Breakout: buy when price breaks above ATR upper channel, sell when breaks below ATR lower channel"
}

func (s *volBreakoutStrategy) Parameters() []strategy.Parameter {
	return []strategy.Parameter{
		{Name: "atr_period", Type: "int", Default: 14, Description: "ATR calculation period", Min: 5, Max: 30},
		{Name: "atr_multiplier", Type: "float", Default: 2.0, Description: "ATR multiplier for channel width", Min: 1.0, Max: 4.0},
		{Name: "lookback", Type: "int", Default: 20, Description: "Donchian channel lookback period", Min: 5, Max: 60},
		{Name: "top_n", Type: "int", Default: 5, Description: "Max number of positions", Min: 1, Max: 20},
	}
}

func (s *volBreakoutStrategy) GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]strategy.Signal, error) {
	if len(bars) == 0 {
		return nil, nil
	}
	atrPeriod := s.params.ATRPeriod
	if atrPeriod <= 0 { atrPeriod = 14 }
	atrMult := s.params.ATRMultiplier
	if atrMult <= 0 { atrMult = 2.0 }
	lookback := s.params.Lookback
	if lookback <= 0 { lookback = 20 }
	topN := s.params.TopN
	if topN <= 0 { topN = 5 }

	type breakoutResult struct {
		symbol    string
		breakType string
		strength  float64
		price     float64
		atr       float64
	}
	var results []breakoutResult

	for symbol, data := range bars {
		n := atrPeriod + lookback + 1
		if len(data) < n { continue }
		sorted := make([]domain.OHLCV, len(data))
		copy(sorted, data)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date.Before(sorted[j].Date) })

		atrSum := 0.0
		for i := len(sorted) - atrPeriod; i < len(sorted); i++ {
			hl := sorted[i].High - sorted[i].Low
			hc := math.Abs(sorted[i].High - sorted[i-1].Close)
			lc := math.Abs(sorted[i].Low - sorted[i-1].Close)
			tr := math.Max(hl, math.Max(hc, lc))
			atrSum += tr
		}
		atr := atrSum / float64(atrPeriod)

		highMax := sorted[len(sorted)-1].High
		lowMin := sorted[len(sorted)-1].Low
		for i := len(sorted) - lookback; i < len(sorted); i++ {
			if sorted[i].High > highMax { highMax = sorted[i].High }
			if sorted[i].Low < lowMin { lowMin = sorted[i].Low }
		}

		latestClose := sorted[len(sorted)-1].Close
		prevClose := sorted[len(sorted)-2].Close
		upperBand := highMax + atr*atrMult
		lowerBand := lowMin - atr*atrMult

		if latestClose <= 0 || atr <= 0 { continue }

		if prevClose <= upperBand && latestClose > upperBand {
			rangeVal := upperBand - lowerBand
			strength := 0.7
			if rangeVal > 0 { strength = (latestClose - upperBand) / rangeVal + 0.7 }
			if strength > 1.0 { strength = 1.0 }
			results = append(results, breakoutResult{symbol: symbol, breakType: "buy", strength: strength, price: latestClose, atr: atr})
		} else if prevClose >= lowerBand && latestClose < lowerBand {
			rangeVal := upperBand - lowerBand
			strength := 0.7
			if rangeVal > 0 { strength = (lowerBand - latestClose) / rangeVal + 0.7 }
			if strength > 1.0 { strength = 1.0 }
			results = append(results, breakoutResult{symbol: symbol, breakType: "sell", strength: strength, price: latestClose, atr: atr})
		}
	}

	sort.Slice(results, func(i, j int) bool { return results[i].strength > results[j].strength })

	var signals []strategy.Signal
	buyCount := 0
	for _, r := range results {
		if r.breakType == "buy" && buyCount < topN {
			signals = append(signals, strategy.Signal{
				Symbol:   r.symbol,
				Action:   "buy",
				Strength: r.strength,
				Price:    r.price,
				Factors:  map[string]float64{"atr": r.atr},
			})
			buyCount++
		} else if r.breakType == "sell" {
			if portfolio != nil {
				if pos, ok := portfolio.Positions[r.symbol]; ok && pos.Quantity > 0 {
					signals = append(signals, strategy.Signal{
						Symbol:   r.symbol,
						Action:   "sell",
						Strength: r.strength,
						Price:    r.price,
						Factors:  map[string]float64{"atr": r.atr},
					})
				}
			}
		}
	}
	return signals, nil
}

func (s *volBreakoutStrategy) Configure(params map[string]any) error {
	if v, ok := params["atr_period"]; ok { switch val := v.(type) { case float64: s.params.ATRPeriod = int(val); case int: s.params.ATRPeriod = val } }
	if v, ok := params["atr_multiplier"]; ok { switch val := v.(type) { case float64: s.params.ATRMultiplier = val; case int: s.params.ATRMultiplier = float64(val) } }
	if v, ok := params["lookback"]; ok { switch val := v.(type) { case float64: s.params.Lookback = int(val); case int: s.params.Lookback = val } }
	if v, ok := params["top_n"]; ok { switch val := v.(type) { case float64: s.params.TopN = int(val); case int: s.params.TopN = val } }
	return nil
}

func (s *volBreakoutStrategy) Weight(signal strategy.Signal, _ float64) float64 {
	w := signal.Strength * 0.10
	if w > 0.08 { w = 0.08 }
	if w < 0.01 { w = 0.01 }
	return w
}

func (s *volBreakoutStrategy) Cleanup() {}

func init() {
	strategy.GlobalRegister(&volBreakoutStrategy{
		name: "volatility_breakout",
		description: "Volatility Breakout: ATR Donchian channel breakout system",
		params: VolBreakoutConfig{ATRPeriod: 14, ATRMultiplier: 2.0, Lookback: 20, TopN: 5},
	})
}
