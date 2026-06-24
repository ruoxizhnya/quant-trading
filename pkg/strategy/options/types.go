// Package options implements option pricing models and Greeks calculation
// for the quant-trading platform (P2-11).
//
// Supported models:
//   - Black-Scholes (European options, analytical Greeks)
//   - Binomial CRR tree (American options with early exercise, numerical Greeks)
//
// All exported struct fields carry JSON tags (snake_case) for API serialization.
package options

import "errors"

// Sentinel errors returned by the options package for invalid inputs.
var (
	// ErrNegativeSpot is returned when the spot price is negative.
	ErrNegativeSpot = errors.New("options: spot price must not be negative")
	// ErrInvalidStrike is returned when the strike price is not positive.
	ErrInvalidStrike = errors.New("options: strike price must be positive")
	// ErrNegativeTime is returned when time to expiry is negative.
	ErrNegativeTime = errors.New("options: time to expiry must not be negative")
	// ErrNegativeVolatility is returned when volatility is negative.
	ErrNegativeVolatility = errors.New("options: volatility must not be negative")
	// ErrInvalidOptionType is returned when Type is neither Call nor Put.
	ErrInvalidOptionType = errors.New("options: invalid option type, must be 'call' or 'put'")
	// ErrInvalidExerciseStyle is returned when Style is neither European nor American.
	ErrInvalidExerciseStyle = errors.New("options: invalid exercise style, must be 'european' or 'american'")
	// ErrNoConvergence is returned when implied volatility iteration fails to converge.
	ErrNoConvergence = errors.New("options: implied volatility iteration did not converge")
	// ErrInvalidSteps is returned when the binomial step count is not positive.
	ErrInvalidSteps = errors.New("options: number of binomial steps must be positive")
)

// OptionType specifies whether an option is a call or a put.
type OptionType string

const (
	// Call gives the holder the right to buy the underlying at the strike.
	Call OptionType = "call"
	// Put gives the holder the right to sell the underlying at the strike.
	Put OptionType = "put"
)

// ExerciseStyle specifies when the option can be exercised.
type ExerciseStyle string

const (
	// European options can only be exercised at expiry.
	European ExerciseStyle = "european"
	// American options can be exercised at any time up to expiry.
	American ExerciseStyle = "american"
)

// OptionSpec describes the inputs required to price an option.
// All monetary values are in the same currency unit; rates are in
// decimal form (e.g. 0.025 means 2.5%).
type OptionSpec struct {
	Spot           float64        `json:"spot"`            // 标的现价 S
	Strike         float64        `json:"strike"`          // 行权价 K
	TimeToExpiry   float64        `json:"time_to_expiry"`  // 剩余期限 T (years)
	RiskFreeRate   float64        `json:"risk_free_rate"`  // 无风险利率 r (decimal, e.g. 0.025)
	Volatility     float64        `json:"volatility"`      // 隐含波动率 σ (decimal, e.g. 0.20)
	DividendYield  float64        `json:"dividend_yield"`  // 股息率 q (decimal, e.g. 0.01)
	Type           OptionType     `json:"type"`            // Call or Put
	Style          ExerciseStyle  `json:"style"`           // European or American
}

// Greeks holds the five standard option sensitivity measures.
// Vega, Theta and Rho are reported in trading-friendly units:
//   - Vega: per 1% absolute change in volatility (already divided by 100)
//   - Theta: per calendar day (already divided by 365)
//   - Rho:   per 1% absolute change in rate (already divided by 100)
type Greeks struct {
	Delta float64 `json:"delta"` // ∂V/∂S
	Gamma float64 `json:"gamma"` // ∂²V/∂S²
	Vega  float64 `json:"vega"`  // ∂V/∂σ per 1% vol
	Theta float64 `json:"theta"` // ∂V/∂t per day
	Rho   float64 `json:"rho"`    // ∂V/∂r per 1% rate
}

// Validate checks the OptionSpec for obvious input errors.
// It returns an error if any required field is out of range.
// TimeToExpiry and Volatility may be zero (handled as edge cases by the
// pricing models), but they must not be negative.
func (s OptionSpec) Validate() error {
	if s.Spot < 0 {
		return ErrNegativeSpot
	}
	if s.Strike <= 0 {
		return ErrInvalidStrike
	}
	if s.TimeToExpiry < 0 {
		return ErrNegativeTime
	}
	if s.Volatility < 0 {
		return ErrNegativeVolatility
	}
	if s.Type != Call && s.Type != Put {
		return ErrInvalidOptionType
	}
	if s.Style != European && s.Style != American {
		return ErrInvalidExerciseStyle
	}
	return nil
}
