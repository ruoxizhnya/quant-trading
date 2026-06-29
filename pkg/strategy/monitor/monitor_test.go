package monitor

import (
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nopLogger returns a no-op zerolog logger for tests, mirroring the
// pattern used in pkg/alert/manager_test.go.
func nopLogger() zerolog.Logger {
	return zerolog.New(nil)
}

// TestStrategyMonitor_Register verifies that Register adds a strategy
// to the monitor and that re-registration is idempotent.
func TestStrategyMonitor_Register(t *testing.T) {
	m := NewStrategyMonitor(DefaultAlertThresholds(), nopLogger())

	m.Register("alpha")
	m.Register("alpha") // idempotent — must not duplicate
	m.Register("beta")

	assert.Equal(t, []string{"alpha", "beta"}, m.ListStrategies())

	state, err := m.GetState("alpha")
	require.NoError(t, err)
	assert.Equal(t, "alpha", state.Name)
	assert.Equal(t, StatusActive, state.Status)
	assert.Empty(t, state.Returns)
}

// TestStrategyMonitor_Update_RollingSharpe verifies the annualized
// rolling Sharpe computation against a hand-computed expected value.
//
// Returns = [0.01, 0.02, -0.01, 0.005, 0.015]
//
//	mean      = 0.008
//	sample std = sqrt(0.0001325) ≈ 0.0115111
//	Sharpe    = 0.008 * sqrt(252) / 0.0115111 ≈ 11.03
func TestStrategyMonitor_Update_RollingSharpe(t *testing.T) {
	m := NewStrategyMonitor(DefaultAlertThresholds(), nopLogger())
	m.Register("alpha")

	returns := []float64{0.01, 0.02, -0.01, 0.005, 0.015}
	equity := 100.0
	for _, r := range returns {
		equity *= (1 + r)
		require.NoError(t, m.Update("alpha", r, equity))
	}

	state, err := m.GetState("alpha")
	require.NoError(t, err)

	assert.Len(t, state.Returns, 5)
	assert.Equal(t, 5, state.TotalTrades)
	assert.Equal(t, 4, state.Wins) // 0.01, 0.02, 0.005, 0.015 are positive; -0.01 is negative
	assert.InDelta(t, 11.03, state.RollingSharpe, 0.05)
	assert.False(t, state.LastUpdate.IsZero())
}

// TestStrategyMonitor_CheckStatus_SharpeDegradation verifies that a
// strategy whose rolling Sharpe falls below MinSharpe emits exactly
// one Warning alert of type SharpeDegradation. Drawdown and consecutive
// loss checks are disabled to isolate the Sharpe rule.
func TestStrategyMonitor_CheckStatus_SharpeDegradation(t *testing.T) {
	thresholds := AlertThresholds{
		MinSharpe:            0.5,
		MaxDrawdown:          0, // disabled
		ConsecutiveLossLimit: 0, // disabled
	}
	m := NewStrategyMonitor(thresholds, nopLogger())
	m.Register("alpha")

	// Returns with negative mean → strongly negative Sharpe.
	returns := []float64{0.001, -0.001, 0.001, -0.001, -0.001}
	equity := 100.0
	for _, r := range returns {
		equity *= (1 + r)
		require.NoError(t, m.Update("alpha", r, equity))
	}

	alerts := m.CheckStatus()
	require.Len(t, alerts, 1)
	assert.Equal(t, "alpha", alerts[0].StrategyName)
	assert.Equal(t, AlertSharpeDegradation, alerts[0].Type)
	assert.Equal(t, SeverityWarning, alerts[0].Severity)
	assert.Less(t, alerts[0].Value, alerts[0].Threshold)
	assert.False(t, alerts[0].Timestamp.IsZero())

	// Status should have escalated Active → Warning.
	state, err := m.GetState("alpha")
	require.NoError(t, err)
	assert.Equal(t, StatusWarning, state.Status)
}

// TestStrategyMonitor_CheckStatus_DrawdownBreach verifies that a
// declining equity curve exceeding MaxDrawdown emits exactly one
// Critical alert of type DrawdownBreach. Sharpe and consecutive-loss
// checks are disabled to isolate the drawdown rule.
func TestStrategyMonitor_CheckStatus_DrawdownBreach(t *testing.T) {
	thresholds := AlertThresholds{
		MinSharpe:            0,    // disabled
		MaxDrawdown:          0.15, // 15%
		ConsecutiveLossLimit: 0,    // disabled
	}
	m := NewStrategyMonitor(thresholds, nopLogger())
	m.Register("alpha")

	// Equity 100 → 95 → 90 → 85 → 80: peak-to-trough drawdown = 20%.
	equities := []float64{100, 95, 90, 85, 80}
	for i, e := range equities {
		dailyReturn := 0.0
		if i > 0 {
			dailyReturn = (e - equities[i-1]) / equities[i-1]
		}
		require.NoError(t, m.Update("alpha", dailyReturn, e))
	}

	alerts := m.CheckStatus()
	require.Len(t, alerts, 1)
	assert.Equal(t, "alpha", alerts[0].StrategyName)
	assert.Equal(t, AlertDrawdownBreach, alerts[0].Type)
	assert.Equal(t, SeverityCritical, alerts[0].Severity)
	assert.Greater(t, alerts[0].Value, alerts[0].Threshold)
	assert.InDelta(t, 0.20, alerts[0].Value, 0.001)
}

// TestStrategyMonitor_CheckStatus_ConsecutiveLoss verifies that a run
// of losing days at or above ConsecutiveLossLimit emits exactly one
// Critical alert of type ConsecutiveLoss. Sharpe and drawdown checks
// are disabled to isolate the consecutive-loss rule.
func TestStrategyMonitor_CheckStatus_ConsecutiveLoss(t *testing.T) {
	thresholds := AlertThresholds{
		MinSharpe:            0, // disabled
		MaxDrawdown:          0, // disabled
		ConsecutiveLossLimit: 3,
	}
	m := NewStrategyMonitor(thresholds, nopLogger())
	m.Register("alpha")

	// 4 consecutive losing days; limit is 3.
	returns := []float64{-0.01, -0.01, -0.01, -0.01}
	equity := 100.0
	for _, r := range returns {
		equity *= (1 + r)
		require.NoError(t, m.Update("alpha", r, equity))
	}

	alerts := m.CheckStatus()
	require.Len(t, alerts, 1)
	assert.Equal(t, "alpha", alerts[0].StrategyName)
	assert.Equal(t, AlertConsecutiveLoss, alerts[0].Type)
	assert.Equal(t, SeverityCritical, alerts[0].Severity)
	assert.GreaterOrEqual(t, alerts[0].Value, alerts[0].Threshold)
	assert.Equal(t, float64(4), alerts[0].Value)
	assert.Equal(t, float64(3), alerts[0].Threshold)
}

// TestStrategyMonitor_CheckStatus_NoAlert verifies that a healthy
// strategy (positive Sharpe, modest drawdown, no consecutive loss
// run) produces zero alerts under the default thresholds.
func TestStrategyMonitor_CheckStatus_NoAlert(t *testing.T) {
	m := NewStrategyMonitor(DefaultAlertThresholds(), nopLogger())
	m.Register("alpha")

	// Alternating positive/negative returns with positive mean →
	// high Sharpe. Equity grows overall → minimal drawdown. Max
	// consecutive losses = 1 (well below 5).
	returns := []float64{0.002, -0.001, 0.002, -0.001, 0.002}
	equity := 100.0
	for _, r := range returns {
		equity *= (1 + r)
		require.NoError(t, m.Update("alpha", r, equity))
	}

	alerts := m.CheckStatus()
	assert.Empty(t, alerts)

	// Status remains Active (no alert → no escalation).
	state, err := m.GetState("alpha")
	require.NoError(t, err)
	assert.Equal(t, StatusActive, state.Status)
}

// TestStrategyMonitor_GetState verifies that GetState returns an
// accurate deep copy of the internal state and that mutating the
// returned copy does not leak back into the monitor.
func TestStrategyMonitor_GetState(t *testing.T) {
	m := NewStrategyMonitor(DefaultAlertThresholds(), nopLogger())
	m.Register("alpha")

	require.NoError(t, m.Update("alpha", 0.01, 101.0))
	require.NoError(t, m.Update("alpha", -0.005, 100.495))

	state, err := m.GetState("alpha")
	require.NoError(t, err)
	assert.Equal(t, "alpha", state.Name)
	assert.Len(t, state.Returns, 2)
	assert.Len(t, state.Equities, 2)
	assert.Equal(t, 2, state.TotalTrades)
	assert.Equal(t, 1, state.Wins)
	assert.Equal(t, 1, state.ConsecutiveLosses) // last return was negative
	assert.False(t, state.LastUpdate.IsZero())

	// Mutating the returned copy must not affect the monitor.
	state.Returns[0] = 999.0
	state.Status = StatusStopped

	state2, err := m.GetState("alpha")
	require.NoError(t, err)
	assert.NotEqual(t, 999.0, state2.Returns[0])
	assert.NotEqual(t, StatusStopped, state2.Status)
}

// TestStrategyMonitor_ListStrategies verifies that ListStrategies
// returns all registered names in lexicographic order and returns an
// empty slice when no strategies are registered.
func TestStrategyMonitor_ListStrategies(t *testing.T) {
	m := NewStrategyMonitor(DefaultAlertThresholds(), nopLogger())
	assert.Empty(t, m.ListStrategies())

	m.Register("gamma")
	m.Register("alpha")
	m.Register("beta")

	assert.Equal(t, []string{"alpha", "beta", "gamma"}, m.ListStrategies())
}

// TestStrategyMonitor_Concurrent exercises concurrent Update /
// CheckStatus / GetState / ListStrategies access to validate
// thread-safety under `go test -race`. Required by the P1-E
// constraint that the monitor be safe for concurrent use.
func TestStrategyMonitor_Concurrent(t *testing.T) {
	m := NewStrategyMonitor(DefaultAlertThresholds(), nopLogger())
	m.Register("alpha")
	m.Register("beta")

	var wg sync.WaitGroup
	const iterations = 20
	for i := 0; i < iterations; i++ {
		wg.Add(4)
		go func(i int) {
			defer wg.Done()
			_ = m.Update("alpha", 0.001*float64(i%5-2), 100+float64(i))
		}(i)
		go func(i int) {
			defer wg.Done()
			_ = m.Update("beta", 0.001*float64(i%5-2), 100+float64(i))
		}(i)
		go func() {
			defer wg.Done()
			_ = m.CheckStatus()
		}()
		go func() {
			defer wg.Done()
			_, _ = m.GetState("alpha")
			_ = m.ListStrategies()
		}()
	}
	wg.Wait()
}
