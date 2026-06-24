// Package plugins contains built-in strategy implementations.
package plugins

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// ConvertibleBondData holds the static and runtime data for a convertible bond.
// All amounts are in CNY. A-share convertible bonds have a par value of 100 CNY
// by convention and trade on the Shanghai/Shenzhen exchanges.
type ConvertibleBondData struct {
	// Symbol is the convertible bond code, e.g. "113001.SH".
	Symbol string `json:"symbol"`
	// UnderlyingSymbol is the underlying stock code, e.g. "600000.SH".
	UnderlyingSymbol string `json:"underlying_symbol"`
	// ParValue is the face value of the bond (default 100 CNY).
	ParValue float64 `json:"par_value"`
	// ConversionPrice is the price at which the bond can be converted into shares.
	ConversionPrice float64 `json:"conversion_price"`
	// CouponRate is the annual coupon rate (e.g. 0.02 = 2%).
	CouponRate float64 `json:"coupon_rate"`
	// MaturityDate is the bond's maturity date.
	MaturityDate time.Time `json:"maturity_date"`
	// CallPrice is the redemption price the issuer pays when exercising the call option.
	CallPrice float64 `json:"call_price"`
	// PutPrice is the price the investor receives when exercising the put option.
	PutPrice float64 `json:"put_price"`
	// CallTriggerPrice is the stock price above which the issuer can force redemption.
	// Defaults to ConversionPrice × 1.3 if not set.
	CallTriggerPrice float64 `json:"call_trigger_price"`
	// PutTriggerPrice is the stock price below which the investor can put the bond back.
	// Defaults to ConversionPrice × 0.7 if not set.
	PutTriggerPrice float64 `json:"put_trigger_price"`
}

// ConversionValue calculates the conversion value of the bond.
// ConversionValue = (ParValue / ConversionPrice) × stockPrice.
// Returns 0 if ConversionPrice or stockPrice is non-positive.
func (cbd *ConvertibleBondData) ConversionValue(stockPrice float64) float64 {
	if cbd.ConversionPrice <= 0 || stockPrice <= 0 {
		return 0
	}
	return (cbd.ParValue / cbd.ConversionPrice) * stockPrice
}

// PureBondValue calculates the pure bond value (debt floor) as the present
// value of remaining coupons plus the present value of par at maturity.
// Uses standard annual-coupon bond pricing with the given discount rate.
// If the bond has matured, returns ParValue. If discountRate is 0, returns
// the undiscounted sum of remaining coupons plus par.
func (cbd *ConvertibleBondData) PureBondValue(discountRate float64, now time.Time) float64 {
	if cbd.ParValue <= 0 {
		return 0
	}
	years := cbd.MaturityDate.Sub(now).Hours() / 24.0 / 365.25
	if years <= 0 {
		return cbd.ParValue
	}
	// Snap to the nearest integer if within ~3 days, to absorb leap-year
	// imprecision (e.g. 6 calendar years = 2192 days = 6.0014 years).
	roundedYears := math.Round(years)
	if math.Abs(years-roundedYears) < 0.01 {
		years = roundedYears
	}
	n := int(math.Ceil(years))
	if n <= 0 {
		return cbd.ParValue
	}
	coupon := cbd.CouponRate * cbd.ParValue
	if discountRate <= 0 {
		return cbd.ParValue + coupon*float64(n)
	}
	r := discountRate
	pvCoupons := coupon * (1.0 - math.Pow(1.0+r, float64(-n))) / r
	pvPar := cbd.ParValue / math.Pow(1.0+r, float64(n))
	return pvCoupons + pvPar
}

// PremiumRate calculates the premium rate of the bond over its conversion value.
// PremiumRate = (bondPrice - conversionValue) / conversionValue.
// Returns 0 if conversionValue is non-positive.
func (cbd *ConvertibleBondData) PremiumRate(bondPrice, stockPrice float64) float64 {
	cv := cbd.ConversionValue(stockPrice)
	if cv <= 0 {
		return 0
	}
	return (bondPrice - cv) / cv
}

// Delta calculates the approximate delta of the convertible bond.
// Delta = conversionValue / bondPrice.
// Returns 0 if bondPrice is non-positive.
func (cbd *ConvertibleBondData) Delta(bondPrice, stockPrice float64) float64 {
	if bondPrice <= 0 {
		return 0
	}
	cv := cbd.ConversionValue(stockPrice)
	return cv / bondPrice
}

// ArbitrageSignal checks whether a conversion arbitrage opportunity exists.
// Returns true if conversionValue > bondPrice × (1 + threshold), meaning the
// investor can buy the bond and immediately convert for a risk-free profit.
func (cbd *ConvertibleBondData) ArbitrageSignal(bondPrice, stockPrice, threshold float64) bool {
	if bondPrice <= 0 || threshold < 0 {
		return false
	}
	cv := cbd.ConversionValue(stockPrice)
	return cv > bondPrice*(1.0+threshold)
}

// EffectiveCallTriggerPrice returns the call trigger price, using the stored
// value if set, otherwise deriving it as ConversionPrice × 1.3.
func (cbd *ConvertibleBondData) EffectiveCallTriggerPrice() float64 {
	if cbd.CallTriggerPrice > 0 {
		return cbd.CallTriggerPrice
	}
	if cbd.ConversionPrice > 0 {
		return cbd.ConversionPrice * 1.3
	}
	return 0
}

// EffectivePutTriggerPrice returns the put trigger price, using the stored
// value if set, otherwise deriving it as ConversionPrice × 0.7.
func (cbd *ConvertibleBondData) EffectivePutTriggerPrice() float64 {
	if cbd.PutTriggerPrice > 0 {
		return cbd.PutTriggerPrice
	}
	if cbd.ConversionPrice > 0 {
		return cbd.ConversionPrice * 0.7
	}
	return 0
}

// CheckCallTrigger checks whether the forced redemption (强制赎回) trigger
// condition is met: the stock closing price must be at or above the call
// trigger price for at least triggerDays out of the last window trading days.
// Returns false if there is insufficient data or the trigger price is invalid.
func (cbd *ConvertibleBondData) CheckCallTrigger(stockBars []domain.OHLCV, window, triggerDays int) bool {
	if window <= 0 || triggerDays <= 0 || len(stockBars) == 0 {
		return false
	}
	triggerPrice := cbd.EffectiveCallTriggerPrice()
	if triggerPrice <= 0 {
		return false
	}
	sorted := sortOHLCV(stockBars)
	if len(sorted) < triggerDays {
		return false
	}
	start := len(sorted) - window
	if start < 0 {
		start = 0
	}
	count := 0
	for i := start; i < len(sorted); i++ {
		if sorted[i].Close >= triggerPrice {
			count++
		}
	}
	return count >= triggerDays
}

// CheckPutTrigger checks whether the put-back (回售) trigger condition is met:
// the stock closing price must be at or below the put trigger price for
// consecutiveDays consecutive trading days. Returns false if there is
// insufficient data or the trigger price is invalid.
func (cbd *ConvertibleBondData) CheckPutTrigger(stockBars []domain.OHLCV, consecutiveDays int) bool {
	if consecutiveDays <= 0 || len(stockBars) == 0 {
		return false
	}
	triggerPrice := cbd.EffectivePutTriggerPrice()
	if triggerPrice <= 0 {
		return false
	}
	sorted := sortOHLCV(stockBars)
	if len(sorted) < consecutiveDays {
		return false
	}
	start := len(sorted) - consecutiveDays
	for i := start; i < len(sorted); i++ {
		if sorted[i].Close > triggerPrice {
			return false
		}
	}
	return true
}

// ConvertibleBondConfig holds configuration for the convertible bond strategy.
type ConvertibleBondConfig struct {
	// ConversionThreshold is the premium rate below which to buy (e.g. -0.05 = -5%).
	ConversionThreshold float64 `json:"conversion_threshold"`
	// PremiumExitThreshold is the premium rate above which to sell (e.g. 0.10 = 10%).
	PremiumExitThreshold float64 `json:"premium_exit_threshold"`
	// MinPureBondRatio is the minimum pure bond value / par value ratio required
	// to consider the bond (debt floor protection).
	MinPureBondRatio float64 `json:"min_pure_bond_ratio"`
	// DiscountRate is the annual discount rate used for pure bond value calculation.
	DiscountRate float64 `json:"discount_rate"`
	// CallTriggerWindow is the number of trading days to look back for call trigger.
	CallTriggerWindow int `json:"call_trigger_window"`
	// CallTriggerDays is the number of days the stock must be above the call trigger
	// price within the window to trigger forced redemption.
	CallTriggerDays int `json:"call_trigger_days"`
	// PutTriggerDays is the number of consecutive days the stock must be below the
	// put trigger price to trigger put-back.
	PutTriggerDays int `json:"put_trigger_days"`
}

// convertibleBondStrategy implements a convertible bond arbitrage strategy.
// It buys convertible bonds when the conversion premium rate falls below a
// threshold and sells when it rises above an exit threshold, while respecting
// debt floor protection and call/put trigger conditions.
type convertibleBondStrategy struct {
	*strategy.BaseStrategy
	params ConvertibleBondConfig
	bonds  map[string]ConvertibleBondData
	bondMu sync.RWMutex
}

// Name returns the unique strategy name.
func (s *convertibleBondStrategy) Name() string {
	return "convertible_bond"
}

// Description returns a human-readable description of the strategy.
func (s *convertibleBondStrategy) Description() string {
	return "可转债套利策略: 转股溢价率 < 阈值时买入转债, 溢价率 > 阈值时卖出"
}

// Parameters returns the configurable parameter schema.
func (s *convertibleBondStrategy) Parameters() []strategy.Parameter {
	return []strategy.Parameter{
		{
			Name:        "conversion_threshold",
			Type:        "float",
			Default:     -0.05,
			Description: "Buy when premium rate is below this threshold (e.g. -0.05 = -5%)",
			Min:         -1.0,
			Max:         0.0,
		},
		{
			Name:        "premium_exit_threshold",
			Type:        "float",
			Default:     0.10,
			Description: "Sell when premium rate exceeds this threshold (e.g. 0.10 = 10%)",
			Min:         0.0,
			Max:         1.0,
		},
		{
			Name:        "min_pure_bond_ratio",
			Type:        "float",
			Default:     0.70,
			Description: "Skip if pure bond value / par value is below this ratio (debt floor protection)",
			Min:         0.0,
			Max:         1.0,
		},
		{
			Name:        "discount_rate",
			Type:        "float",
			Default:     0.05,
			Description: "Annual discount rate for pure bond value calculation",
			Min:         0.0,
			Max:         0.5,
		},
	}
}

// RegisterBond registers a convertible bond with the strategy.
// This must be called before GenerateSignals for the strategy to
// consider a bond symbol.
func (s *convertibleBondStrategy) RegisterBond(data ConvertibleBondData) {
	s.bondMu.Lock()
	defer s.bondMu.Unlock()
	if s.bonds == nil {
		s.bonds = make(map[string]ConvertibleBondData)
	}
	s.bonds[data.Symbol] = data
}

// GetBond retrieves a registered bond by symbol.
func (s *convertibleBondStrategy) GetBond(symbol string) (ConvertibleBondData, bool) {
	s.bondMu.RLock()
	defer s.bondMu.RUnlock()
	data, ok := s.bonds[symbol]
	return data, ok
}

// GenerateSignals inspects the bars map and the current portfolio, returning
// buy/sell signals for registered convertible bonds.
func (s *convertibleBondStrategy) GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]strategy.Signal, error) {
	if len(bars) == 0 {
		return nil, nil
	}

	s.bondMu.RLock()
	defer s.bondMu.RUnlock()

	if len(s.bonds) == 0 {
		return nil, nil
	}

	params := s.params
	if params.DiscountRate < 0 {
		params.DiscountRate = 0.05
	}
	if params.CallTriggerWindow <= 0 {
		params.CallTriggerWindow = 30
	}
	if params.CallTriggerDays <= 0 {
		params.CallTriggerDays = 15
	}
	if params.PutTriggerDays <= 0 {
		params.PutTriggerDays = 30
	}

	now := time.Now()
	var signals []strategy.Signal

	for symbol, bondData := range s.bonds {
		bondBars, ok := bars[symbol]
		if !ok || len(bondBars) == 0 {
			continue
		}

		stockBars, ok := bars[bondData.UnderlyingSymbol]
		if !ok || len(stockBars) == 0 {
			continue
		}

		sortedBondBars := sortOHLCV(bondBars)
		sortedStockBars := sortOHLCV(stockBars)

		bondPrice := sortedBondBars[len(sortedBondBars)-1].Close
		stockPrice := sortedStockBars[len(sortedStockBars)-1].Close

		if bondPrice <= 0 || stockPrice <= 0 {
			continue
		}

		if bondData.ParValue <= 0 || bondData.ConversionPrice <= 0 {
			continue
		}

		conversionValue := bondData.ConversionValue(stockPrice)
		if conversionValue <= 0 {
			continue
		}

		pureBondValue := bondData.PureBondValue(params.DiscountRate, now)
		premiumRate := bondData.PremiumRate(bondPrice, stockPrice)
		delta := bondData.Delta(bondPrice, stockPrice)

		// Debt floor protection: skip if pure bond value / par value is too low.
		if pureBondValue/bondData.ParValue < params.MinPureBondRatio {
			continue
		}

		latestDate := sortedBondBars[len(sortedBondBars)-1].Date

		factors := map[string]float64{
			"premium_rate":     premiumRate,
			"conversion_value": conversionValue,
			"pure_bond_value":  pureBondValue,
			"delta":            delta,
		}

		// Check call/put triggers — these force a sell regardless of premium.
		callTriggered := bondData.CheckCallTrigger(sortedStockBars, params.CallTriggerWindow, params.CallTriggerDays)
		putTriggered := bondData.CheckPutTrigger(sortedStockBars, params.PutTriggerDays)

		if callTriggered || putTriggered {
			reason := "put_trigger"
			if callTriggered {
				reason = "call_trigger"
			}
			signals = append(signals, strategy.Signal{
				Symbol:   symbol,
				Action:   "sell",
				Strength: 1.0,
				Price:    bondPrice,
				Date:     latestDate,
				Factors:  factors,
				Metadata: map[string]interface{}{"trigger": reason},
			})
			continue
		}

		// Buy signal: premium rate below threshold.
		if premiumRate < params.ConversionThreshold {
			strength := buySignalStrength(premiumRate, params.ConversionThreshold)
			signals = append(signals, strategy.Signal{
				Symbol:   symbol,
				Action:   "buy",
				Strength: strength,
				Price:    bondPrice,
				Date:     latestDate,
				Factors:  factors,
				Metadata: map[string]interface{}{
					"arbitrage": bondData.ArbitrageSignal(bondPrice, stockPrice, params.ConversionThreshold),
				},
			})
		}

		// Sell signal: premium rate above exit threshold (only if we hold the bond).
		if premiumRate > params.PremiumExitThreshold && portfolio != nil {
			if pos, exists := portfolio.Positions[symbol]; exists && pos.Quantity > 0 {
				strength := sellSignalStrength(premiumRate, params.PremiumExitThreshold)
				signals = append(signals, strategy.Signal{
					Symbol:   symbol,
					Action:   "sell",
					Strength: strength,
					Price:    bondPrice,
					Date:     latestDate,
					Factors:  factors,
				})
			}
		}
	}

	return signals, nil
}

// buySignalStrength normalises the buy signal strength to [0.01, 1.0].
// The strength is the ratio of the distance from the threshold to the
// threshold's absolute value. If the threshold is zero, the absolute
// premium rate is used.
func buySignalStrength(premiumRate, threshold float64) float64 {
	var strength float64
	absThreshold := math.Abs(threshold)
	if absThreshold > 1e-10 {
		strength = (threshold - premiumRate) / absThreshold
	} else {
		strength = math.Abs(premiumRate)
	}
	strength = math.Min(strength, 1.0)
	strength = math.Max(strength, 0.01)
	return strength
}

// sellSignalStrength normalises the sell signal strength to [0.01, 1.0].
func sellSignalStrength(premiumRate, exitThreshold float64) float64 {
	var strength float64
	if exitThreshold > 1e-10 {
		strength = (premiumRate - exitThreshold) / exitThreshold
	} else {
		strength = premiumRate
	}
	strength = math.Min(strength, 1.0)
	strength = math.Max(strength, 0.01)
	return strength
}

// Configure applies a partial parameter update with validation.
func (s *convertibleBondStrategy) Configure(params map[string]any) error {
	s.Lock()
	defer s.Unlock()
	if v, ok := params["conversion_threshold"]; ok {
		if val, ok := parseFloatParam(v); ok {
			result := validateFloatRange("conversion_threshold", val, -1.0, 0.0)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.ConversionThreshold = val
		}
	}
	if v, ok := params["premium_exit_threshold"]; ok {
		if val, ok := parseFloatParam(v); ok {
			result := validateFloatRange("premium_exit_threshold", val, 0.0, 1.0)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.PremiumExitThreshold = val
		}
	}
	if v, ok := params["min_pure_bond_ratio"]; ok {
		if val, ok := parseFloatParam(v); ok {
			result := validateFloatRange("min_pure_bond_ratio", val, 0.0, 1.0)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.MinPureBondRatio = val
		}
	}
	if v, ok := params["discount_rate"]; ok {
		if val, ok := parseFloatParam(v); ok {
			result := validateFloatRange("discount_rate", val, 0.0, 0.5)
			if !result.Valid {
				return fmt.Errorf("invalid parameter: %s", result.Message)
			}
			s.params.DiscountRate = val
		}
	}
	return nil
}

// Weight returns the position weight based on signal strength.
// Weight is proportional to strength × 0.1, clamped to [0.01, 0.05].
func (s *convertibleBondStrategy) Weight(signal strategy.Signal, portfolioValue float64) float64 {
	weight := signal.Strength * 0.1
	if weight > 0.05 {
		weight = 0.05
	}
	if weight < 0.01 {
		weight = 0.01
	}
	return weight
}

// Cleanup releases all resources held by the strategy.
func (s *convertibleBondStrategy) Cleanup() {
	s.Lock()
	s.params = ConvertibleBondConfig{}
	s.Unlock()

	s.bondMu.Lock()
	s.bonds = nil
	s.bondMu.Unlock()
}

func init() {
	s := &convertibleBondStrategy{
		BaseStrategy: strategy.NewBaseStrategy(
			"convertible_bond",
			"可转债套利策略: 转股溢价率 < 阈值时买入转债, 溢价率 > 阈值时卖出",
		),
		params: ConvertibleBondConfig{
			ConversionThreshold:  -0.05,
			PremiumExitThreshold: 0.10,
			MinPureBondRatio:     0.70,
			DiscountRate:         0.05,
			CallTriggerWindow:    30,
			CallTriggerDays:      15,
			PutTriggerDays:       30,
		},
		bonds: make(map[string]ConvertibleBondData),
	}
	strategy.GlobalRegister(s)
}
