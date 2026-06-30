// S7-P1-2 regression tests: verify the local DriftDetector interface
// breaks the monitor → pkg/ai/drift reverse dependency.
//
// This test file is INTENTIONALLY self-contained: it does NOT import
// pkg/ai/drift. All stubs are defined locally so the test binary's
// import graph proves the reverse dep is gone from production code.
package monitor

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubDriftDetector is a test-only DriftDetector implementation that
// does NOT import pkg/ai/drift — proving the local interface breaks
// the reverse dependency.
type stubDriftDetector struct {
	results []*DriftResult
	err     error
	calls   int
}

func (s *stubDriftDetector) DetectAll(values []float64) ([]*DriftResult, error) {
	s.calls++
	return s.results, s.err
}

// TestS7P1_2_LocalDriftInterfacesExist verifies the local DriftDetector
// interface and DriftResult type can be referenced without importing
// pkg/ai/drift. The compile-time var-assertion is the real check.
func TestS7P1_2_LocalDriftInterfacesExist(t *testing.T) {
	var _ DriftDetector = (*stubDriftDetector)(nil)
	var _ = DriftResult{
		DriftDetected: false,
		DriftType:     "mean_shift",
		Severity:      "high",
		Statistic:     0.0,
		Message:       "",
	}
	t.Log("local interfaces exist: DriftDetector, DriftResult")
}

// TestS7P1_2_SetDriftDetector_AcceptsLocalInterface verifies the
// setter accepts any type satisfying the local DriftDetector interface
// (not just *drift.Detector from pkg/ai/drift).
func TestS7P1_2_SetDriftDetector_AcceptsLocalInterface(t *testing.T) {
	m := NewStrategyMonitor(DefaultAlertThresholds(), zerolog.Nop())
	stub := &stubDriftDetector{
		results: []*DriftResult{
			{
				DriftDetected: true,
				DriftType:     "mean_shift",
				Severity:      "high",
				Statistic:     2.5,
				Message:       "stub: mean shifted",
			},
		},
	}
	m.SetDriftDetector(stub)
	// Setting should not panic; the detector is wired for CheckStatus.

	m.Register("test-strat")
	require.NoError(t, m.Update("test-strat", -0.05, 95.0))

	alerts := m.CheckStatus()
	// The stub reports adverse drift (DriftType != "improvement"), so
	// we expect at least one AlertDriftDetected.
	found := false
	for _, a := range alerts {
		if a.Type == AlertDriftDetected {
			found = true
			assert.Equal(t, "stub: mean shifted", a.Message,
				"alert Message must come from the stub's DriftResult.Message")
			break
		}
	}
	assert.True(t, found, "stub DriftDetector reporting adverse drift must produce an AlertDriftDetected")
	assert.GreaterOrEqual(t, stub.calls, 1,
		"stub DriftDetector.DetectAll must be invoked by CheckStatus")
}

// TestS7P1_2_SetDriftDetector_NilDisables verifies that passing nil to
// SetDriftDetector disables drift detection (no panic, no alerts of
// type AlertDriftDetected).
func TestS7P1_2_SetDriftDetector_NilDisables(t *testing.T) {
	m := NewStrategyMonitor(DefaultAlertThresholds(), zerolog.Nop())
	m.SetDriftDetector(nil)
	m.Register("test-strat")
	require.NoError(t, m.Update("test-strat", -0.05, 95.0))

	alerts := m.CheckStatus()
	for _, a := range alerts {
		assert.NotEqual(t, AlertDriftDetected, a.Type,
			"nil DriftDetector must not produce drift alerts")
	}
}

// TestS7P1_2_DriftResult_ImprovementSkipped verifies that a drift
// result with DriftType=="improvement" is NOT surfaced as an alert
// (the monitor only surfaces adverse drift).
func TestS7P1_2_DriftResult_ImprovementSkipped(t *testing.T) {
	m := NewStrategyMonitor(DefaultAlertThresholds(), zerolog.Nop())
	m.SetDriftDetector(&stubDriftDetector{
		results: []*DriftResult{
			{
				DriftDetected: true,
				DriftType:     "improvement", // must be skipped
				Severity:      "low",
				Statistic:     0.0,
				Message:       "stub: improved",
			},
		},
	})
	m.Register("test-strat")
	require.NoError(t, m.Update("test-strat", 0.01, 105.0))

	// CheckStatus is synchronous, so no wait needed — just call it.
	alerts := m.CheckStatus()
	for _, a := range alerts {
		assert.NotEqual(t, AlertDriftDetected, a.Type,
			"improvement drift must not produce an alert")
	}
}
