package backtest

// S7-P2-1: state_test.go split — 6 BacktestState unit tests moved to
// pkg/backtest/state/state_test.go. This file retains only the
// Engine-integration race test, which depends on newTestEngine /
// NewTracker / defaultTradingConfig (parent-package symbols).

import (
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
)

// TestBacktestState_EngineGetBacktestStatusRace uses the real Engine
// API to exercise the public read paths concurrently with a goroutine
// that mutates the same state. This is the integration-level race test
// — it mirrors the production scenario where the HTTP handler reads
// status while the backtest goroutine writes it.
func TestBacktestState_EngineGetBacktestStatusRace(t *testing.T) {
	eng := newTestEngine(t)
	tracker := NewTracker(1_000_000, 0.0003, 0.001, defaultTradingConfig(), zerolog.Nop())
	state := &BacktestState{
		ID:      "bt-engine-race",
		Params:  domain.BacktestParams{StrategyName: "momentum"},
		Tracker: tracker,
	}
	state.SetStatus("running")

	eng.stateStore.Put(state.ID, state)

	var wg sync.WaitGroup
	const readers = 16
	const iterations = 200

	// Concurrent reads via public API
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_, _ = eng.GetBacktestStatus("bt-engine-race")
				_, _ = eng.GetBacktestParams("bt-engine-race")
			}
		}()
	}

	// Concurrent writer — mutates Status and Result
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			state.SetStatus("running")
			state.SetResult(&domain.BacktestResult{TotalReturn: float64(j) / 100.0})
			state.SetCompletedAt(time.Now())
		}
		state.SetStatus("completed")
		state.Freeze()
	}()

	wg.Wait()
	assert.Equal(t, "completed", state.GetStatus())
	assert.True(t, state.IsFrozen())
}
