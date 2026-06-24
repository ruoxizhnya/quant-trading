package options

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNormPDF_KnownValues checks the normal density at well-known points.
func TestNormPDF_KnownValues(t *testing.T) {
	cases := []struct {
		name string
		x    float64
		want float64
		tol  float64
	}{
		{"origin", 0, 1 / math.Sqrt(2*math.Pi), 1e-12},
		{"one", 1, 0.24197072451914337, 1e-10},
		{"minus_one", -1, 0.24197072451914337, 1e-10},
		{"two", 2, 0.05399096651318806, 1e-10},
		{"three", 3, 0.0044318484119380075, 1e-10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NormPDF(c.x)
			assert.InDelta(t, c.want, got, c.tol)
		})
	}
}

// TestNormPDF_Symmetry verifies NormPDF(x) == NormPDF(-x).
func TestNormPDF_Symmetry(t *testing.T) {
	for _, x := range []float64{0.5, 1.0, 1.5, 2.5, 3.7} {
		assert.InDelta(t, NormPDF(x), NormPDF(-x), 1e-15)
	}
}

// TestNormPDF_IntegratesToOne checks the density integrates to ~1 via
// a coarse Riemann sum (sanity, not precision).
func TestNormPDF_IntegratesToOne(t *testing.T) {
	const (
		lo, hi = -10.0, 10.0
		n      = 100000
		dx     = (hi - lo) / n
	)
	sum := 0.0
	for i := 0; i < n; i++ {
		x := lo + (float64(i)+0.5)*dx
		sum += NormPDF(x) * dx
	}
	assert.InDelta(t, 1.0, sum, 1e-4)
}

// TestNormCDF_KnownValues checks the CDF at standard reference points.
func TestNormCDF_KnownValues(t *testing.T) {
	cases := []struct {
		name string
		x    float64
		want float64
		tol  float64
	}{
		{"zero", 0, 0.5, 1e-7},
		{"one", 1, 0.8413447460685429, 1e-7},
		{"minus_one", -1, 0.15865525393145707, 1e-7},
		{"two", 2, 0.9772498680518208, 1e-7},
		{"minus_two", -2, 0.022750131948179195, 1e-7},
		{"three", 3, 0.9986501019683699, 1e-7},
		{"minus_three", -3, 0.0013498980316301035, 1e-7},
		{"1.96", 1.96, 0.9750021048517795, 1e-6},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NormCDF(c.x)
			assert.InDelta(t, c.want, got, c.tol)
		})
	}
}

// TestNormCDF_Monotonic ensures the CDF is non-decreasing.
func TestNormCDF_Monotonic(t *testing.T) {
	prev := NormCDF(-5.0)
	for x := -4.9; x <= 5.0; x += 0.1 {
		cur := NormCDF(x)
		assert.GreaterOrEqual(t, cur, prev, "CDF decreased at x=%v", x)
		prev = cur
	}
}

// TestNormCDF_Symmetry verifies N(x) + N(-x) == 1.
func TestNormCDF_Symmetry(t *testing.T) {
	for _, x := range []float64{0.1, 0.5, 1.0, 1.5, 2.0, 3.0, 5.0} {
		assert.InDelta(t, 1.0, NormCDF(x)+NormCDF(-x), 1e-7)
	}
}

// TestNormCDF_TailLimits checks the asymptotic clamping.
func TestNormCDF_TailLimits(t *testing.T) {
	assert.Equal(t, 0.0, NormCDF(-100))
	assert.Equal(t, 1.0, NormCDF(100))
	// A&S approximation has documented error up to 7.5e-8; at x=0 the
	// actual error is ~5e-10, well within the approximation's tolerance.
	assert.InDelta(t, 0.5, NormCDF(0), 1e-7)
}

// TestNormCDF_Bounds ensures the CDF stays in [0,1].
func TestNormCDF_Bounds(t *testing.T) {
	for x := -10.0; x <= 10.0; x += 0.05 {
		v := NormCDF(x)
		assert.GreaterOrEqual(t, v, 0.0)
		assert.LessOrEqual(t, v, 1.0)
	}
}

// TestNormCDF_AccuracyVsErf compares against math.Erf for a fine grid.
// math.Erf is the stdlib reference; we require agreement within 7.5e-8
// (the A&S approximation's documented accuracy).
func TestNormCDF_AccuracyVsErf(t *testing.T) {
	const tol = 7.5e-8
	for x := -5.0; x <= 5.0; x += 0.01 {
		want := 0.5 * (1 + math.Erf(x/math.Sqrt(2)))
		got := NormCDF(x)
		assert.InDelta(t, want, got, tol, "mismatch at x=%v", x)
	}
}
