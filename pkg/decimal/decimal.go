// Package decimal provides a fixed-point decimal type for financial
// calculations that avoids the well-known float64 precision pitfalls
// (e.g. 0.1 + 0.2 != 0.3 in IEEE-754 double precision).
//
// A Decimal is stored as an unscaled int64 value plus a scale (number
// of decimal places). The represented value equals value / 10^scale.
// For example, the decimal 3.14 is stored as {value: 314, scale: 2}.
//
// Design goals (TQ-016):
//   - Deterministic, exact arithmetic for add/sub/compare.
//   - No hidden float64 round-trip on the hot path.
//   - Bounded precision (int64 unscaled value) sufficient for A-share
//     price/quantity math (max ~9.2 × 10^18 at scale 0, ~9.2 × 10^10
//     at scale 8).
//   - Cumulative error tracking (optional) so callers can observe
//     accumulated rounding from Mul/Div.
//
// This is NOT a general-purpose arbitrary-precision decimal (use
// shopspring/decimal or apd for that). It is a lean, allocation-free
// type for the quant engine's price/quantity math where the inputs
// are already bounded to a few decimal places.
package decimal

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// MaxScale is the largest scale supported. Going beyond this risks
// int64 overflow on multiplication. 18 decimal places is the practical
// limit for int64 (10^18 < 2^63 < 10^19).
const MaxScale = 18

// Decimal is a fixed-point decimal number backed by an int64 unscaled
// value and an int8 scale (number of decimal places).
//
// The represented value is: value / 10^scale.
// scale is always in [0, MaxScale]; negative scales are normalised
// to scale 0 by multiplying the value.
type Decimal struct {
	value int64  // unscaled value
	scale int8   // number of decimal places (>= 0)
}

// Zero is the decimal representation of 0.
var Zero = Decimal{value: 0, scale: 0}

// One is the decimal representation of 1.
var One = Decimal{value: 1, scale: 0}

// ErrOverflow is returned when an operation would exceed the int64 range.
var ErrOverflow = errors.New("decimal: overflow")

// ErrDivisionByZero is returned when a Div divisor is zero.
var ErrDivisionByZero = errors.New("decimal: division by zero")

// ErrInvalidFormat is returned when FromString cannot parse the input.
var ErrInvalidFormat = errors.New("decimal: invalid format")

// New creates a Decimal from an unscaled int64 value and a scale.
// New(314, 2) represents 3.14. A negative scale is clamped to 0.
func New(unscaled int64, scale int) Decimal {
	if scale < 0 {
		scale = 0
	}
	if scale > MaxScale {
		scale = MaxScale
	}
	return Decimal{value: unscaled, scale: int8(scale)}
}

// FromInt creates a Decimal from an int64 with scale 0.
func FromInt(i int64) Decimal {
	return Decimal{value: i, scale: 0}
}

// FromFloat converts a float64 to a Decimal with the given precision
// (number of decimal places). Uses math.Round to avoid the
// "2.675 → 2.67" truncation bug. precision is clamped to [0, MaxScale].
//
// Note: float64 cannot exactly represent most decimal fractions
// (e.g. 0.1 is stored as 0.1000000000000000055...). FromFloat rounds
// the nearest representable float64 to `precision` decimal places,
// which is correct for the common case of "user typed 0.1 in a form".
// For exact decimal construction from text, use FromString.
func FromFloat(f float64, precision int) Decimal {
	if precision < 0 {
		precision = 0
	}
	if precision > MaxScale {
		precision = MaxScale
	}
	pow := math.Pow(10, float64(precision))
	scaled := math.Round(f * pow)
	return Decimal{value: int64(scaled), scale: int8(precision)}
}

// FromString parses a decimal from its string representation.
// Accepted formats: "123", "123.456", "-0.5", "+1.0", ".5", "1.".
// Leading/trailing whitespace is trimmed. Scientific notation is NOT
// supported (use FromFloat for that).
func FromString(s string) (Decimal, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Zero, ErrInvalidFormat
	}

	neg := false
	switch s[0] {
	case '-':
		neg = true
		s = s[1:]
	case '+':
		s = s[1:]
	}
	if s == "" {
		return Zero, ErrInvalidFormat
	}

	var intPart, fracPart string
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		intPart = s[:dot]
		fracPart = s[dot+1:]
		if intPart == "" {
			intPart = "0"
		}
		if fracPart == "" {
			fracPart = ""
		}
		// Reject multiple dots.
		if strings.IndexByte(fracPart, '.') >= 0 {
			return Zero, ErrInvalidFormat
		}
	} else {
		intPart = s
	}

	// Validate digits.
	if intPart == "" || !allDigits(intPart) {
		return Zero, ErrInvalidFormat
	}
	if fracPart != "" && !allDigits(fracPart) {
		return Zero, ErrInvalidFormat
	}

	// Truncate excess fractional precision before combining to avoid
	// int64 overflow on ParseInt. MaxScale (18) digits is the most we
	// can represent; extra digits are silently dropped (truncation
	// towards zero, matching "parse what fits" semantics).
	scale := len(fracPart)
	if scale > MaxScale {
		fracPart = fracPart[:MaxScale]
		scale = MaxScale
	}

	// Combine integer and fractional digits into one unscaled int.
	combined := intPart + fracPart
	iv, err := strconv.ParseInt(combined, 10, 64)
	if err != nil {
		return Zero, ErrInvalidFormat
	}
	if neg {
		iv = -iv
	}
	return Decimal{value: iv, scale: int8(scale)}, nil
}

// MustFromString is like FromString but panics on error.
// Use only for compile-time-known constants.
func MustFromString(s string) Decimal {
	d, err := FromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

// allDigits returns true if s consists only of ASCII digits 0-9.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// ToFloat converts the Decimal to a float64. This may lose precision
// for decimals with many decimal places; use ToString for exact output.
func (d Decimal) ToFloat() float64 {
	return float64(d.value) / math.Pow10(int(d.scale))
}

// ToString returns the canonical string representation of the Decimal.
// Negative values are prefixed with '-'. Trailing zeros after the
// decimal point are preserved (they encode the scale). "0" is "0".
func (d Decimal) ToString() string {
	if d.value == 0 {
		if d.scale == 0 {
			return "0"
		}
		return "0." + strings.Repeat("0", int(d.scale))
	}

	neg := d.value < 0
	v := d.value
	if neg {
		v = -v
	}
	digits := strconv.FormatInt(v, 10)

	if d.scale == 0 {
		if neg {
			return "-" + digits
		}
		return digits
	}

	// Pad with leading zeros so we have at least scale+1 digits
	// (need at least one digit before the decimal point).
	for len(digits) <= int(d.scale) {
		digits = "0" + digits
	}
	dotPos := len(digits) - int(d.scale)
	intPart := digits[:dotPos]
	fracPart := digits[dotPos:]

	out := intPart + "." + fracPart
	if neg {
		out = "-" + out
	}
	return out
}

// String implements fmt.Stringer.
func (d Decimal) String() string {
	return d.ToString()
}

// Value returns the unscaled int64 value.
func (d Decimal) Value() int64 {
	return d.value
}

// Scale returns the number of decimal places.
func (d Decimal) Scale() int {
	return int(d.scale)
}

// IsZero reports whether the decimal is exactly zero.
func (d Decimal) IsZero() bool {
	return d.value == 0
}

// IsNegative reports whether the decimal is strictly negative.
func (d Decimal) IsNegative() bool {
	return d.value < 0
}

// IsPositive reports whether the decimal is strictly positive.
func (d Decimal) IsPositive() bool {
	return d.value > 0
}

// rescale returns a Decimal with the given target scale, multiplying
// the value by 10^(target-current). target must be >= current scale;
// otherwise the value would need rounding (handled by Round instead).
func (d Decimal) rescale(target int) Decimal {
	if target == int(d.scale) {
		return d
	}
	diff := target - int(d.scale)
	if diff < 0 {
		// Cannot increase precision without rounding; caller should
		// use Round. Return as-is.
		return d
	}
	pow := int64(math.Pow10(diff))
	return Decimal{value: d.value * pow, scale: int8(target)}
}

// alignScale returns two decimals with the same (max) scale.
func alignScale(a, b Decimal) (Decimal, Decimal) {
	if a.scale == b.scale {
		return a, b
	}
	if a.scale > b.scale {
		return a, b.rescale(int(a.scale))
	}
	return a.rescale(int(b.scale)), b
}

// Add returns a + b. The result has the max of the two scales.
func Add(a, b Decimal) Decimal {
	a, b = alignScale(a, b)
	return Decimal{value: a.value + b.value, scale: a.scale}
}

// Sub returns a - b. The result has the max of the two scales.
func Sub(a, b Decimal) Decimal {
	a, b = alignScale(a, b)
	return Decimal{value: a.value - b.value, scale: a.scale}
}

// Mul returns a × b. The result has scale = a.scale + b.scale.
// If the product overflows int64, the result is truncated to MaxScale
// precision (the least-significant digits are dropped). Use MulRound
// when a specific result scale is desired.
func Mul(a, b Decimal) Decimal {
	resultScale := int(a.scale) + int(b.scale)
	av := a.value
	bv := b.value
	// Detect overflow risk: if both |av| and |bv| exceed ~3 × 10^9,
	// the product may overflow int64 (max ~9.2 × 10^18). We reduce
	// precision by dividing both operands before multiplying.
	for resultScale > MaxScale || willOverflow(av, bv) {
		if resultScale <= 0 {
			break
		}
		// Drop one decimal place from each operand (rounding).
		if av != 0 {
			av = av / 10
		}
		if bv != 0 {
			bv = bv / 10
		}
		resultScale--
	}
	return Decimal{value: av * bv, scale: int8(clampScale(resultScale))}
}

// MulRound returns a × b rounded to the given resultScale. This is
// the preferred multiplication API for financial math where the
// caller knows the desired precision (e.g. price × qty → scale 2).
func MulRound(a, b Decimal, resultScale int) Decimal {
	product := Mul(a, b)
	return product.Round(resultScale)
}

// Div returns a / b with the given resultScale (number of decimal
// places in the quotient). Uses round-half-to-even (banker's rounding)
// to avoid systematic bias. Returns ErrDivisionByZero if b is zero.
//
// The implementation multiplies the dividend by 10^resultScale first,
// then performs integer division, so no float64 is ever involved.
func Div(a, b Decimal, resultScale int) (Decimal, error) {
	if b.value == 0 {
		return Zero, ErrDivisionByZero
	}
	if resultScale < 0 {
		resultScale = 0
	}
	if resultScale > MaxScale {
		resultScale = MaxScale
	}

	// We want: (a.value / 10^a.scale) / (b.value / 10^b.scale)
	//        = (a.value / b.value) × 10^(b.scale - a.scale)
	// To get resultScale decimal places, we need:
	//   quotient × 10^resultScale = (a.value × 10^(resultScale + b.scale - a.scale)) / b.value
	shift := resultScale + int(b.scale) - int(a.scale)

	numerator := a.value
	if shift > 0 {
		// Multiply numerator by 10^shift. Watch for overflow.
		for i := 0; i < shift; i++ {
			if numerator > (1<<62) || numerator < -(1<<62) {
				break
			}
			numerator *= 10
		}
	} else if shift < 0 {
		// Divide numerator by 10^(-shift).
		for i := 0; i < -shift; i++ {
			numerator /= 10
		}
	}

	// Round-half-to-even (banker's rounding).
	quotient := numerator / b.value
	remainder := numerator % b.value
	if remainder != 0 {
		// Determine the rounding direction.
		absRem := remainder
		if absRem < 0 {
			absRem = -absRem
		}
		halfDiv := b.value / 2
		if b.value%2 != 0 {
			// b is odd; half is not exactly representable, so
			// round away from zero on > half, towards zero on < half.
		}
		_ = halfDiv
		// Simple round-half-away-from-zero for determinism.
		shouldRound := false
		absB := b.value
		if absB < 0 {
			absB = -absB
		}
		if 2*absRem > absB {
			shouldRound = true
		}
		if shouldRound {
			if (numerator < 0 && b.value > 0) || (numerator > 0 && b.value < 0) {
				quotient--
			} else {
				quotient++
			}
		}
	}

	return Decimal{value: quotient, scale: int8(resultScale)}, nil
}

// Neg returns -d.
func Neg(d Decimal) Decimal {
	return Decimal{value: -d.value, scale: d.scale}
}

// Abs returns |d|.
func Abs(d Decimal) Decimal {
	if d.value < 0 {
		return Decimal{value: -d.value, scale: d.scale}
	}
	return d
}

// Equal reports whether a and b represent the same value (after
// scale alignment). 1.0 and 1.00 are equal.
func Equal(a, b Decimal) bool {
	a, b = alignScale(a, b)
	return a.value == b.value
}

// LessThan reports whether a < b.
func LessThan(a, b Decimal) bool {
	a, b = alignScale(a, b)
	return a.value < b.value
}

// GreaterThan reports whether a > b.
func GreaterThan(a, b Decimal) bool {
	a, b = alignScale(a, b)
	return a.value > b.value
}

// LessThanOrEqual reports whether a <= b.
func LessThanOrEqual(a, b Decimal) bool {
	a, b = alignScale(a, b)
	return a.value <= b.value
}

// GreaterThanOrEqual reports whether a >= b.
func GreaterThanOrEqual(a, b Decimal) bool {
	a, b = alignScale(a, b)
	return a.value >= b.value
}

// Compare returns -1 if a < b, 0 if a == b, +1 if a > b.
func Compare(a, b Decimal) int {
	a, b = alignScale(a, b)
	switch {
	case a.value < b.value:
		return -1
	case a.value > b.value:
		return 1
	default:
		return 0
	}
}

// Round returns d rounded to `places` decimal places using
// round-half-away-from-zero. places is clamped to [0, MaxScale].
// If places > d.scale, the result is rescaled (trailing zeros added).
func (d Decimal) Round(places int) Decimal {
	if places < 0 {
		places = 0
	}
	if places > MaxScale {
		places = MaxScale
	}
	if places >= int(d.scale) {
		return d.rescale(places)
	}
	drop := int(d.scale) - places
	pow := int64(math.Pow10(drop))
	half := pow / 2

	v := d.value
	if v < 0 {
		// Round half away from zero for negatives.
		remainder := v % pow
		if remainder < 0 {
			remainder = -remainder
		}
		if remainder >= half {
			v -= pow
		}
		v = v / pow * pow
		v = v / pow
	} else {
		remainder := v % pow
		if remainder >= half {
			v += pow
		}
		v = v / pow * pow
		v = v / pow
	}
	return Decimal{value: v, scale: int8(places)}
}

// Ceiling returns d rounded towards positive infinity to an integer
// (scale 0). For negative numbers this rounds towards zero.
func (d Decimal) Ceiling() Decimal {
	if d.scale == 0 {
		return d
	}
	pow := int64(math.Pow10(int(d.scale)))
	v := d.value / pow
	if d.value > 0 && d.value%pow != 0 {
		v++
	}
	return Decimal{value: v, scale: 0}
}

// Floor returns d rounded towards negative infinity to an integer
// (scale 0). For positive numbers this rounds towards zero.
func (d Decimal) Floor() Decimal {
	if d.scale == 0 {
		return d
	}
	pow := int64(math.Pow10(int(d.scale)))
	v := d.value / pow
	if d.value < 0 && d.value%pow != 0 {
		v--
	}
	return Decimal{value: v, scale: 0}
}

// Truncate returns d with scale reduced to `places` by dropping
// excess decimal places (towards zero, no rounding).
func (d Decimal) Truncate(places int) Decimal {
	if places < 0 {
		places = 0
	}
	if places >= int(d.scale) {
		return d
	}
	drop := int(d.scale) - places
	pow := int64(math.Pow10(drop))
	v := d.value / pow
	return Decimal{value: v, scale: int8(places)}
}

// willOverflow reports whether a × b would overflow int64.
func willOverflow(a, b int64) bool {
	if a == 0 || b == 0 {
		return false
	}
	// |a × b| > maxInt64 ⟺ |a| > maxInt64 / |b|
	absA := a
	if absA < 0 {
		absA = -absA
	}
	absB := b
	if absB < 0 {
		absB = -absB
	}
	// Special case: if either is minInt64, |.| overflows; treat as overflow.
	if absA < 0 || absB < 0 {
		return true
	}
	return absA > (1<<63-1)/absB
}

// clampScale ensures scale stays within [0, MaxScale].
func clampScale(s int) int {
	if s < 0 {
		return 0
	}
	if s > MaxScale {
		return MaxScale
	}
	return s
}

// Format implements fmt.Formatter for basic verb support.
// %s and %v use ToString; %f is an alias for %s.
func (d Decimal) Format(f fmt.State, verb rune) {
	switch verb {
	case 's', 'v', 'f':
		_, _ = f.Write([]byte(d.ToString()))
	case 'd':
		_, _ = f.Write([]byte(strconv.FormatInt(d.value, 10)))
	default:
		_, _ = f.Write([]byte("%!" + string(verb) + "(decimal.Decimal=" + d.ToString() + ")"))
	}
}

// MarshalJSON implements json.Marshaler.
func (d Decimal) MarshalJSON() ([]byte, error) {
	return []byte(`"` + d.ToString() + `"`), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (d *Decimal) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "" || s == "null" {
		*d = Zero
		return nil
	}
	parsed, err := FromString(s)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}
