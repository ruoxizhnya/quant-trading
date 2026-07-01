package state

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBacktestState_GetSetRoundTrip verifies the basic setter/getter
// behavior of BacktestState (P1-20): the values written by Set* must
// be visible to subsequent Get* calls, and the prev-returned-value
// must match the value before the write.
func TestBacktestState_GetSetRoundTrip(t *testing.T) {
	s := &BacktestState{ID: "bt-roundtrip"}

	// Status
	prev, ok := s.SetStatus("running")
	require.True(t, ok)
	assert.Equal(t, "", prev)
	assert.Equal(t, "running", s.GetStatus())

	prev, ok = s.SetStatus("completed")
	require.True(t, ok)
	assert.Equal(t, "running", prev)
	assert.Equal(t, "completed", s.GetStatus())

	// Result
	res := &domain.BacktestResult{TotalReturn: 0.42}
	require.True(t, s.SetResult(res))
	assert.Same(t, res, s.GetResult())

	// Error
	err := errors.New("boom")
	require.True(t, s.SetError(err))
	assert.Equal(t, err, s.GetError())

	// CompletedAt
	now := time.Now()
	require.True(t, s.SetCompletedAt(now))
	assert.Equal(t, now, s.GetCompletedAt())
}

// TestBacktestState_FreezeRejectsWrites verifies that once Freeze is
// called, all Set* methods are no-ops (return prev, false) and the
// frozen state is visible via IsFrozen/IsCompleted.
func TestBacktestState_FreezeRejectsWrites(t *testing.T) {
	s := &BacktestState{ID: "bt-freeze"}
	s.SetStatus("running")
	s.SetResult(&domain.BacktestResult{TotalReturn: 0.1})

	s.Freeze()
	assert.True(t, s.IsFrozen())
	assert.True(t, s.IsCompleted())

	// All writes rejected
	prev, ok := s.SetStatus("failed")
	assert.False(t, ok)
	assert.Equal(t, "running", prev, "status must not change after freeze")
	assert.Equal(t, "running", s.GetStatus())

	ok = s.SetResult(&domain.BacktestResult{TotalReturn: 0.99})
	assert.False(t, ok)
	assert.InDelta(t, 0.1, s.GetResult().TotalReturn, 1e-9, "result must not change after freeze")

	ok = s.SetError(errors.New("late"))
	assert.False(t, ok)
	assert.Nil(t, s.GetError(), "error must remain nil after freeze")

	ok = s.SetCompletedAt(time.Now().Add(time.Hour))
	assert.False(t, ok)
	assert.True(t, s.GetCompletedAt().IsZero(), "completedAt must not change after freeze")

	// Freeze is idempotent
	s.Freeze()
	assert.True(t, s.IsFrozen())
}

// TestBacktestState_SnapshotAtomicity verifies that Snapshot returns
// all fields coherently — no read can be partial-update.
func TestBacktestState_SnapshotAtomicity(t *testing.T) {
	started := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)
	completed := started.Add(2 * time.Hour)
	res := &domain.BacktestResult{TotalReturn: 0.18, SharpeRatio: 1.42}
	params := domain.BacktestParams{StrategyName: "momentum", InitialCapital: 1_000_000}

	s := &BacktestState{
		ID:        "bt-snap",
		Params:    params,
		StartedAt: started,
	}
	s.SetStatus("running")
	s.SetResult(res)
	s.SetCompletedAt(completed)
	s.Freeze()

	snap := s.Snapshot()
	assert.Equal(t, "bt-snap", snap.ID)
	assert.Equal(t, "running", snap.Status)
	assert.Equal(t, params, snap.Params)
	assert.Same(t, res, snap.Result)
	assert.Equal(t, started, snap.StartedAt)
	assert.Equal(t, completed, snap.CompletedAt)
	assert.True(t, snap.Frozen)
}

// TestBacktestState_SnapshotIsValueCopy verifies that mutating the
// snapshot does not affect the live state, and vice versa.
func TestBacktestState_SnapshotIsValueCopy(t *testing.T) {
	s := &BacktestState{ID: "bt-snap-copy"}
	s.SetStatus("running")

	// Direction 1: snapshot mutation must not leak to live state.
	snap := s.Snapshot()
	snap.Status = "garbage"
	assert.Equal(t, "running", s.GetStatus(), "snapshot mutation must not leak to live state")

	// Direction 2: live mutation must not retroactively change earlier snapshots.
	frozen := s.Snapshot()
	s.SetStatus("completed")
	assert.Equal(t, "running", frozen.Status, "live mutation must not retroactively change earlier snapshots")
	assert.Equal(t, "completed", s.GetStatus(), "live state must reflect new value")
}

// TestBacktestState_ConcurrentReadWriteStatus is the core race-detector
// test for P1-20: many goroutines write Status (running -> completed)
// while many other goroutines read Status / IsFrozen / Snapshot. The
// race detector must report zero races, and every read must observe a
// valid Status (one of "", "running", "completed", "failed").
func TestBacktestState_ConcurrentReadWriteStatus(t *testing.T) {
	s := &BacktestState{ID: "bt-race-status"}

	const writers = 8
	const readers = 16
	const iterations = 200

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Writers: alternate between "running" and "completed"
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				select {
				case <-stop:
					return
				default:
				}
				if (id+j)%2 == 0 {
					s.SetStatus("running")
				} else {
					s.SetStatus("completed")
				}
			}
		}(i)
	}

	// Readers: GetStatus, IsFrozen, Snapshot — must always observe a
	// well-formed status (not torn read).
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				select {
				case <-stop:
					return
				default:
				}
				status := s.GetStatus()
				switch status {
				case "", "running", "completed", "failed":
					// OK
				default:
					t.Errorf("torn read of Status: %q", status)
					return
				}
				_ = s.IsFrozen()
				_ = s.Snapshot()
			}
		}()
	}

	// Concurrent Freeze — must become immutable at some point, and
	// IsFrozen must eventually return true.
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		s.Freeze()
	}()

	wg.Wait()
	assert.True(t, s.IsFrozen(), "Freeze must have taken effect")
}

// TestBacktestState_ConcurrentResultErrorCompletedAt stresses the
// remaining mutable fields in parallel: every read must observe a
// valid value (pointer / interface / time) and never a torn write.
func TestBacktestState_ConcurrentResultErrorCompletedAt(t *testing.T) {
	s := &BacktestState{ID: "bt-race-fields"}

	res1 := &domain.BacktestResult{TotalReturn: 0.1}
	res2 := &domain.BacktestResult{TotalReturn: 0.2}
	err1 := errors.New("err1")
	err2 := errors.New("err2")
	t1 := time.Now()
	t2 := t1.Add(time.Hour)

	var wg sync.WaitGroup
	const writers = 4
	const readers = 8
	const iterations = 200

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if (id+j)%2 == 0 {
					s.SetResult(res1)
				} else {
					s.SetResult(res2)
				}
				if (id+j)%2 == 0 {
					s.SetError(err1)
				} else {
					s.SetError(err2)
				}
				if (id+j)%2 == 0 {
					s.SetCompletedAt(t1)
				} else {
					s.SetCompletedAt(t2)
				}
			}
		}(i)
	}

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				r := s.GetResult()
				if r != nil && r != res1 && r != res2 {
					t.Errorf("torn read of Result pointer: %p", r)
					return
				}
				e := s.GetError()
				if e != nil && e != err1 && e != err2 {
					t.Errorf("torn read of Error: %v", e)
					return
				}
				ts := s.GetCompletedAt()
				if !ts.IsZero() && !ts.Equal(t1) && !ts.Equal(t2) {
					t.Errorf("torn read of CompletedAt: %v", ts)
					return
				}
			}
		}()
	}

	wg.Wait()
}
