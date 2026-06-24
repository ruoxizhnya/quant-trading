package decimal

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Construction & conversion ────────────────────────────────────

func TestNew(t *testing.T) {
	t.Parallel()
	d := New(314, 2)
	assert.Equal(t, int64(314), d.Value())
	assert.Equal(t, 2, d.Scale())
}

func TestNew_NegativeScaleClamped(t *testing.T) {
	t.Parallel()
	d := New(100, -3)
	assert.Equal(t, 0, d.Scale())
	assert.Equal(t, int64(100), d.Value())
}

func TestFromInt(t *testing.T) {
	t.Parallel()
	d := FromInt(42)
	assert.Equal(t, int64(42), d.Value())
	assert.Equal(t, 0, d.Scale())
}

func TestFromFloat_BasicPrecision(t *testing.T) {
	t.Parallel()
	d := FromFloat(3.14, 2)
	assert.Equal(t, int64(314), d.Value())
	assert.Equal(t, 2, d.Scale())
}

// KEY TEST: 0.1 + 0.2 must equal 0.3 (float64 fails this).
func TestFromFloat_PointOne(t *testing.T) {
	t.Parallel()
	d := FromFloat(0.1, 1)
	assert.Equal(t, int64(1), d.Value())
	assert.Equal(t, 1, d.Scale())
}

func TestFromFloat_Negative(t *testing.T) {
	t.Parallel()
	d := FromFloat(-2.5, 1)
	assert.Equal(t, int64(-25), d.Value())
	assert.Equal(t, 1, d.Scale())
}

func TestFromString_Integer(t *testing.T) {
	t.Parallel()
	d, err := FromString("123")
	require.NoError(t, err)
	assert.Equal(t, int64(123), d.Value())
	assert.Equal(t, 0, d.Scale())
}

func TestFromString_Decimal(t *testing.T) {
	t.Parallel()
	d, err := FromString("3.14")
	require.NoError(t, err)
	assert.Equal(t, int64(314), d.Value())
	assert.Equal(t, 2, d.Scale())
}

func TestFromString_Negative(t *testing.T) {
	t.Parallel()
	d, err := FromString("-0.5")
	require.NoError(t, err)
	assert.Equal(t, int64(-5), d.Value())
	assert.Equal(t, 1, d.Scale())
}

func TestFromString_LeadingDot(t *testing.T) {
	t.Parallel()
	d, err := FromString(".5")
	require.NoError(t, err)
	assert.Equal(t, int64(5), d.Value())
	assert.Equal(t, 1, d.Scale())
}

func TestFromString_TrailingDot(t *testing.T) {
	t.Parallel()
	d, err := FromString("1.")
	require.NoError(t, err)
	assert.Equal(t, int64(1), d.Value())
	assert.Equal(t, 0, d.Scale())
}

func TestFromString_Invalid(t *testing.T) {
	t.Parallel()
	cases := []string{"", "abc", "1.2.3", "--1", "1a", "+", "-"}
	for _, s := range cases {
		_, err := FromString(s)
		assert.ErrorIs(t, err, ErrInvalidFormat, "input %q", s)
	}
}

func TestMustFromString_PanicOnError(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { MustFromString("abc") })
	assert.NotPanics(t, func() { MustFromString("1.5") })
}

func TestToString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		d   Decimal
		out string
	}{
		{New(0, 0), "0"},
		{New(314, 2), "3.14"},
		{New(-314, 2), "-3.14"},
		{New(100, 0), "100"},
		{New(5, 1), "0.5"},
		{New(50, 2), "0.50"},
		{New(0, 2), "0.00"},
		{New(-5, 1), "-0.5"},
	}
	for _, c := range cases {
		assert.Equal(t, c.out, c.d.ToString(), "value=%d scale=%d", c.d.Value(), c.d.Scale())
	}
}

func TestToFloat(t *testing.T) {
	t.Parallel()
	d := New(314, 2)
	assert.InDelta(t, 3.14, d.ToFloat(), 1e-9)
}

// ─── Arithmetic ──────────────────────────────────────────────────

// KEY TEST: 0.1 + 0.2 = 0.3 (float64 fails this, decimal must pass).
func TestAdd_PointOnePlusPointTwo(t *testing.T) {
	t.Parallel()
	a := FromFloat(0.1, 1)
	b := FromFloat(0.2, 1)
	c := FromFloat(0.3, 1)
	sum := Add(a, b)
	assert.True(t, Equal(sum, c), "%s + %s = %s, want %s", a, b, sum, c)
}

func TestAdd_DifferentScales(t *testing.T) {
	t.Parallel()
	a := New(1, 1)   // 0.1
	b := New(25, 2)  // 0.25
	sum := Add(a, b) // 0.35
	assert.Equal(t, int64(35), sum.Value())
	assert.Equal(t, 2, sum.Scale())
}

func TestAdd_Negative(t *testing.T) {
	t.Parallel()
	a := New(5, 0)   // 5
	b := New(-3, 0)  // -3
	sum := Add(a, b) // 2
	assert.Equal(t, int64(2), sum.Value())
}

func TestSub(t *testing.T) {
	t.Parallel()
	a := New(5, 0)
	b := New(3, 0)
	diff := Sub(a, b)
	assert.Equal(t, int64(2), diff.Value())
}

func TestSub_NegativeResult(t *testing.T) {
	t.Parallel()
	a := New(3, 0)
	b := New(5, 0)
	diff := Sub(a, b)
	assert.Equal(t, int64(-2), diff.Value())
}

// KEY TEST: 1000000 * 0.0001 = 100 (large × small precision).
func TestMul_LargeTimesSmall(t *testing.T) {
	t.Parallel()
	a := FromInt(1000000)       // 1,000,000
	b := New(1, 4)              // 0.0001
	product := Mul(a, b)        // scale 0+4=4 → 1000000/10^4 = 100
	assert.True(t, Equal(product, FromInt(100)),
		"1000000 * 0.0001 = %s, want 100", product)
}

func TestMul_Simple(t *testing.T) {
	t.Parallel()
	a := New(3, 0)  // 3
	b := New(4, 0)  // 4
	p := Mul(a, b) // 12
	assert.Equal(t, int64(12), p.Value())
	assert.Equal(t, 0, p.Scale())
}

func TestMul_DecimalPlaces(t *testing.T) {
	t.Parallel()
	a := New(314, 2) // 3.14
	b := New(2, 0)   // 2
	p := Mul(a, b)   // 6.28
	assert.Equal(t, int64(628), p.Value())
	assert.Equal(t, 2, p.Scale())
	assert.True(t, Equal(p, MustFromString("6.28")))
}

func TestMulRound(t *testing.T) {
	t.Parallel()
	a := New(314, 2) // 3.14
	b := New(314, 2) // 3.14
	// 3.14 * 3.14 = 9.8596; round to 2 places → 9.86
	p := MulRound(a, b, 2)
	assert.True(t, Equal(p, MustFromString("9.86")), "got %s", p)
}

func TestDiv_Basic(t *testing.T) {
	t.Parallel()
	a := FromInt(10)
	b := FromInt(4)
	q, err := Div(a, b, 2)
	require.NoError(t, err)
	assert.True(t, Equal(q, MustFromString("2.50")), "got %s", q)
}

func TestDiv_Repeating(t *testing.T) {
	t.Parallel()
	a := FromInt(1)
	b := FromInt(3)
	q, err := Div(a, b, 6)
	require.NoError(t, err)
	// 1/3 = 0.333333
	assert.True(t, Equal(q, MustFromString("0.333333")), "got %s", q)
}

func TestDiv_ByZero(t *testing.T) {
	t.Parallel()
	_, err := Div(FromInt(1), Zero, 2)
	assert.ErrorIs(t, err, ErrDivisionByZero)
}

func TestDiv_Negative(t *testing.T) {
	t.Parallel()
	a := FromInt(-10)
	b := FromInt(4)
	q, err := Div(a, b, 2)
	require.NoError(t, err)
	assert.True(t, Equal(q, MustFromString("-2.50")), "got %s", q)
}

func TestNeg(t *testing.T) {
	t.Parallel()
	d := New(5, 0)
	n := Neg(d)
	assert.Equal(t, int64(-5), n.Value())
	assert.Equal(t, int64(5), Neg(n).Value())
}

func TestAbs(t *testing.T) {
	t.Parallel()
	assert.Equal(t, int64(5), Abs(New(-5, 0)).Value())
	assert.Equal(t, int64(5), Abs(New(5, 0)).Value())
	assert.Equal(t, int64(0), Abs(Zero).Value())
}

// ─── Comparison ──────────────────────────────────────────────────

func TestEqual(t *testing.T) {
	t.Parallel()
	assert.True(t, Equal(New(1, 0), New(100, 2)))  // 1 == 1.00
	assert.True(t, Equal(New(0, 0), New(0, 5)))     // 0 == 0.00000
	assert.False(t, Equal(New(1, 0), New(2, 0)))
}

func TestLessThan(t *testing.T) {
	t.Parallel()
	assert.True(t, LessThan(New(1, 0), New(2, 0)))
	assert.False(t, LessThan(New(2, 0), New(1, 0)))
	assert.False(t, LessThan(New(1, 0), New(1, 0)))
}

func TestGreaterThan(t *testing.T) {
	t.Parallel()
	assert.True(t, GreaterThan(New(2, 0), New(1, 0)))
	assert.False(t, GreaterThan(New(1, 0), New(2, 0)))
}

func TestCompare(t *testing.T) {
	t.Parallel()
	assert.Equal(t, -1, Compare(New(1, 0), New(2, 0)))
	assert.Equal(t, 0, Compare(New(1, 0), New(100, 2)))
	assert.Equal(t, 1, Compare(New(3, 0), New(2, 0)))
}

func TestLessThanOrEqual(t *testing.T) {
	t.Parallel()
	assert.True(t, LessThanOrEqual(New(1, 0), New(2, 0)))
	assert.True(t, LessThanOrEqual(New(1, 0), New(1, 0)))
	assert.False(t, LessThanOrEqual(New(2, 0), New(1, 0)))
}

func TestGreaterThanOrEqual(t *testing.T) {
	t.Parallel()
	assert.True(t, GreaterThanOrEqual(New(2, 0), New(1, 0)))
	assert.True(t, GreaterThanOrEqual(New(1, 0), New(1, 0)))
	assert.False(t, GreaterThanOrEqual(New(1, 0), New(2, 0)))
}

// ─── Predicates ──────────────────────────────────────────────────

func TestIsZero(t *testing.T) {
	t.Parallel()
	assert.True(t, Zero.IsZero())
	assert.True(t, New(0, 5).IsZero())
	assert.False(t, New(1, 0).IsZero())
}

func TestIsNegative(t *testing.T) {
	t.Parallel()
	assert.True(t, New(-1, 0).IsNegative())
	assert.False(t, New(0, 0).IsNegative())
	assert.False(t, New(1, 0).IsNegative())
}

func TestIsPositive(t *testing.T) {
	t.Parallel()
	assert.True(t, New(1, 0).IsPositive())
	assert.False(t, New(0, 0).IsPositive())
	assert.False(t, New(-1, 0).IsPositive())
}

// ─── Rounding ────────────────────────────────────────────────────

func TestRound(t *testing.T) {
	t.Parallel()
	cases := []struct {
		d      Decimal
		places int
		want   string
	}{
		{New(3141, 3), 2, "3.14"},   // 3.141 → 3.14
		{New(3145, 3), 2, "3.15"},   // 3.145 → 3.15 (half away from zero)
		{New(3149, 3), 2, "3.15"},   // 3.149 → 3.15
		{New(3140, 3), 2, "3.14"},   // 3.140 → 3.14
		{New(-3145, 3), 2, "-3.15"}, // -3.145 → -3.15
		{New(1, 0), 2, "1.00"},      // increase precision
	}
	for _, c := range cases {
		got := c.d.Round(c.places)
		assert.Equal(t, c.want, got.ToString(), "Round(%s, %d)", c.d.ToString(), c.places)
	}
}

func TestCeiling(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "4", New(314, 2).Ceiling().ToString())  // 3.14 → 4
	assert.Equal(t, "3", New(300, 2).Ceiling().ToString())   // 3.00 → 3
	assert.Equal(t, "-3", New(-314, 2).Ceiling().ToString()) // -3.14 → -3
}

func TestFloor(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "3", New(314, 2).Floor().ToString())  // 3.14 → 3
	assert.Equal(t, "3", New(300, 2).Floor().ToString())   // 3.00 → 3
	assert.Equal(t, "-4", New(-314, 2).Floor().ToString())  // -3.14 → -4
}

func TestTruncate(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "3.14", New(3141, 3).Truncate(2).ToString()) // 3.141 → 3.14
	assert.Equal(t, "3.14", New(3149, 3).Truncate(2).ToString())  // 3.149 → 3.14 (no rounding)
}

// ─── JSON marshalling ───────────────────────────────────────────

func TestMarshalJSON(t *testing.T) {
	t.Parallel()
	d := New(314, 2)
	data, err := json.Marshal(d)
	require.NoError(t, err)
	assert.Equal(t, `"3.14"`, string(data))
}

func TestUnmarshalJSON(t *testing.T) {
	t.Parallel()
	var d Decimal
	err := json.Unmarshal([]byte(`"3.14"`), &d)
	require.NoError(t, err)
	assert.True(t, Equal(d, New(314, 2)))
}

func TestUnmarshalJSON_Null(t *testing.T) {
	t.Parallel()
	var d Decimal
	err := json.Unmarshal([]byte(`null`), &d)
	require.NoError(t, err)
	assert.True(t, d.IsZero())
}

// ─── Edge cases ──────────────────────────────────────────────────

func TestFloat64FailsButDecimalPasses(t *testing.T) {
	t.Parallel()
	// Demonstrate the float64 bug that motivates this package.
	// NOTE: Go evaluates constant expressions at arbitrary precision,
	// so `0.1 + 0.2` as a constant equals 0.3 exactly. We must use
	// float64 VARIABLES to reproduce the IEEE-754 rounding error.
	var a, b float64 = 0.1, 0.2
	floatSum := a + b
	assert.NotEqual(t, 0.3, floatSum, "float64 0.1+0.2 should NOT equal 0.3")
	assert.InDelta(t, 0.3, floatSum, 1e-15, "but it's very close")

	// Decimal must pass.
	da := FromFloat(0.1, 1)
	db := FromFloat(0.2, 1)
	dc := FromFloat(0.3, 1)
	assert.True(t, Equal(Add(da, db), dc), "decimal 0.1+0.2 must equal 0.3")
}

func TestLargeValueNoOverflow(t *testing.T) {
	t.Parallel()
	// 1 billion with scale 0 — well within int64 range.
	big := FromInt(1_000_000_000)
	assert.Equal(t, int64(1_000_000_000), big.Value())
	// Multiply by 1 million → 10^15, still fits int64.
	p := Mul(big, FromInt(1_000_000))
	assert.True(t, Equal(p, FromInt(1_000_000_000_000_000)))
}

func TestVerySmallValue(t *testing.T) {
	t.Parallel()
	// 0.00001 with scale 5.
	d := New(1, 5)
	assert.Equal(t, "0.00001", d.ToString())
	assert.InDelta(t, 0.00001, d.ToFloat(), 1e-15)
}

func TestFromString_ExcessPrecision(t *testing.T) {
	t.Parallel()
	// 20 decimal places — exceeds MaxScale (18), should truncate.
	d, err := FromString("0.12345678901234567890")
	require.NoError(t, err)
	assert.LessOrEqual(t, d.Scale(), MaxScale)
}

func TestConstants(t *testing.T) {
	t.Parallel()
	assert.True(t, Zero.IsZero())
	assert.True(t, One.IsPositive())
	assert.Equal(t, "0", Zero.ToString())
	assert.Equal(t, "1", One.ToString())
}

func TestFormat(t *testing.T) {
	t.Parallel()
	d := New(314, 2)
	assert.Equal(t, "3.14", d.String())
}

func TestMaxScale(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 18, MaxScale)
	// Ensure MaxScale doesn't exceed int64 capacity.
	assert.Less(t, int64(math.Pow10(MaxScale)), int64(1)<<62)
}
