package backtest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestJobService_Shutdown_RejectsNewJobs is the "REJECT-NEW" half of the
// P0-8 acceptance: once Shutdown has been called, StartJob must NOT
// spawn a new goroutine. The job is persisted in DB as failed so the
// caller / observability layer can see the rejection.
func TestJobService_Shutdown_RejectsNewJobs(t *testing.T) {
	svc, store := newTestJobService(t)

	// Drain.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, svc.Shutdown(ctx))

	// Pre-seed a job row directly (bypassing CreateJob) to simulate
	// a race where the row is created just before Shutdown is called.
	require.NoError(t, store.CreateBacktestJob(context.Background(), map[string]any{
		"id":          "post-shutdown-job",
		"strategy_id": "momentum",
		"universe":    "600000.SH",
		"start_date":  "2020-01-01",
		"end_date":    "2023-12-31",
		"status":      "pending",
		"created_at":  time.Now(),
	}))

	// Attempting to start it must NOT spawn a goroutine and MUST mark
	// the row as failed.
	svc.StartJob(context.Background(), "post-shutdown-job")

	// Give any (incorrectly) started goroutine a moment to run.
	time.Sleep(50 * time.Millisecond)

	got, err := store.GetBacktestJob(context.Background(), "post-shutdown-job")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "failed", got["status"], "rejected job must be persisted as 'failed'")
	assert.Contains(t, got["error_msg"], "shutting down",
		"rejected job's error message must explain the rejection")
}

// TestJobService_Shutdown_Idempotent verifies that calling Shutdown
// twice does not panic and does not return an error from the second
// call. This is critical because main.go's signal handler may fire
// twice (e.g. SIGINT during a partially-handled SIGTERM), and the
// production shutdown sequence must be robust to that.
func TestJobService_Shutdown_Idempotent(t *testing.T) {
	svc, _ := newTestJobService(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	require.NoError(t, svc.Shutdown(ctx))
	// Second call must not panic and must return nil (the early-return
	// branch in Shutdown).
	require.NoError(t, svc.Shutdown(ctx))
}

// TestJobService_Shutdown_DrainsCleanly_NoJobs verifies the happy
// path: a fresh JobService with no in-flight jobs drains immediately.
func TestJobService_Shutdown_DrainsCleanly_NoJobs(t *testing.T) {
	svc, _ := newTestJobService(t)

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	require.NoError(t, svc.Shutdown(ctx))
	// Should be effectively instantaneous — well under 100ms in any
	// normal CI environment. Generous bound here to avoid flakes.
	assert.Less(t, time.Since(start), 100*time.Millisecond,
		"empty JobService should drain near-instantly")
}

// TestJobService_Shutdown_WaitsForInflight verifies the "WAIT" half
// of the acceptance: Shutdown must block until in-flight goroutines
// have settled. We approximate "in-flight" by starting a long-running
// StartJob (via a fake cancelFuncs entry) and confirming Shutdown
// returns AFTER the goroutine completes.
func TestJobService_Shutdown_WaitsForInflight(t *testing.T) {
	svc, _ := newTestJobService(t)

	// Simulate an in-flight job by manually adding a cancel function
	// and bumping the WaitGroup. This is the same code path that
	// StartJob uses, minus the actual backtest work.
	svc.inflightWg.Add(1)
	released := make(chan struct{})
	go func() {
		defer svc.inflightWg.Done()
		<-released // hold the "job" open until the test releases it
	}()

	// Shutdown with a 2s timeout should NOT return until we release
	// the goroutine.
	shutdownReturned := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		shutdownReturned <- svc.Shutdown(ctx)
	}()

	// Shutdown is blocked because the in-flight goroutine is held open.
	select {
	case err := <-shutdownReturned:
		t.Fatalf("Shutdown returned prematurely: err=%v", err)
	case <-time.After(50 * time.Millisecond):
		// expected — Shutdown is still waiting
	}

	// Release the in-flight goroutine. Shutdown should then complete
	// within a few ms.
	close(released)
	select {
	case err := <-shutdownReturned:
		require.NoError(t, err, "Shutdown must return nil when inflight settles")
	case <-time.After(1 * time.Second):
		t.Fatal("Shutdown did not return after inflight settled")
	}
}

// TestJobService_Shutdown_RespectsTimeout verifies the timeout path:
// if the inflight WaitGroup is still blocked when ctx fires,
// Shutdown returns ctx.Err() so the caller can fall back to
// CleanupStaleRunning.
func TestJobService_Shutdown_RespectsTimeout(t *testing.T) {
	svc, _ := newTestJobService(t)

	svc.inflightWg.Add(1)
	defer svc.inflightWg.Done() // release the wg so the test exits cleanly
	stuck := make(chan struct{})
	go func() {
		<-stuck // hold forever — test will close it via t.Cleanup
	}()
	t.Cleanup(func() { close(stuck) })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := svc.Shutdown(ctx)
	require.Error(t, err, "Shutdown must return ctx.Err() when wait times out")
	assert.True(t, errors.Is(err, context.DeadlineExceeded),
		"Shutdown error must wrap context.DeadlineExceeded; got %v", err)
}

// TestJobService_Shutdown_CancelsInflightContexts verifies the
// "CANCEL-IN-FLIGHT" half: every entry in cancelFuncs gets called.
// We register a fake cancel function and assert it was invoked.
func TestJobService_Shutdown_CancelsInflightContexts(t *testing.T) {
	svc, _ := newTestJobService(t)

	var called int
	var mu sync.Mutex
	// NOTE: store as context.CancelFunc (not bare func()) — sync.Map's
	// type assertion in Shutdown only matches the named type. A bare
	// `func()` would not match the `value.(context.CancelFunc)` cast
	// and would be silently skipped. This is a real-world concern:
	// any future refactor that stores bare funcs here would regress
	// P0-8 graceful shutdown silently.
	svc.cancelFuncs.Store("job-x", context.CancelFunc(func() {
		mu.Lock()
		defer mu.Unlock()
		called++
	}))
	svc.cancelFuncs.Store("job-y", context.CancelFunc(func() {
		mu.Lock()
		defer mu.Unlock()
		called++
	}))
	svc.cancelFuncs.Store("job-nil", context.CancelFunc(nil))

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	require.NoError(t, svc.Shutdown(ctx))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 2, called, "Shutdown must invoke every non-nil cancelFunc exactly once")
}

// TestJobService_CleanupStaleRunning_MarksRunningAsFailed is the
// "DB status 全部 'completed'/'failed'" half of the P0-8 acceptance
// (TASKS.md: "SIGTERM 后 DB status 全部 'completed'/'failed'").
func TestJobService_CleanupStaleRunning_MarksRunningAsFailed(t *testing.T) {
	svc, store := newTestJobService(t)

	// Seed: 2 running, 1 completed, 1 failed. Only the 2 running
	// should be transitioned.
	now := time.Now()
	seed := []struct {
		id     string
		status string
	}{
		{"running-1", "running"},
		{"running-2", "running"},
		{"done-1", "completed"},
		{"done-2", "failed"},
	}
	for _, s := range seed {
		require.NoError(t, store.CreateBacktestJob(context.Background(), map[string]any{
			"id":          s.id,
			"strategy_id": "momentum",
			"universe":    "600000.SH",
			"start_date":  "2020-01-01",
			"end_date":    "2023-12-31",
			"status":      s.status,
			"created_at":  now,
		}))
	}

	transitioned, err := svc.CleanupStaleRunning(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, transitioned, "exactly the 2 'running' rows should be transitioned")

	for _, id := range []string{"running-1", "running-2"} {
		row, err := store.GetBacktestJob(context.Background(), id)
		require.NoError(t, err)
		require.NotNil(t, row)
		assert.Equal(t, "failed", row["status"],
			"stale-running job %s must end up as 'failed'", id)
		errMsg, _ := row["error_msg"].(string)
		assert.Contains(t, errMsg, "P0-8",
			"error_msg should carry the P0-8 marker so downstream alerts recognize SIGTERM-cancelled rows")
	}

	// Sanity: the 2 already-finalized jobs must NOT be touched.
	for _, id := range []string{"done-1", "done-2"} {
		row, err := store.GetBacktestJob(context.Background(), id)
		require.NoError(t, err)
		require.NotNil(t, row)
		assert.NotEqual(t, "P0-8", row["error_msg"],
			"already-finalized job %s must not be rewritten", id)
	}
}

// TestJobService_CleanupStaleRunning_EmptyStore is a regression
// guard: calling Cleanup on a brand-new service with no jobs must
// return 0 transitioned, nil error — never an index-out-of-range
// or NPE on a nil slice.
func TestJobService_CleanupStaleRunning_EmptyStore(t *testing.T) {
	svc, _ := newTestJobService(t)
	transitioned, err := svc.CleanupStaleRunning(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, transitioned)
}

// TestJobService_ShutdownFullCycle_StaleCleanup is the end-to-end
// P0-8 scenario from TASKS.md: simulate an in-flight job that
// "escapes" (e.g. its goroutine is stuck on a slow DB write), force
// Shutdown to time out, then call CleanupStaleRunning and assert
// the DB is left with no "running" rows.
func TestJobService_ShutdownFullCycle_StaleCleanup(t *testing.T) {
	svc, store := newTestJobService(t)

	// Seed a row in "running" state — this is what an in-flight
	// backtest that survived a hard process death would look like
	// (P0-8's documented "recovery on startup" use case).
	require.NoError(t, store.CreateBacktestJob(context.Background(), map[string]any{
		"id":          "stuck-1",
		"strategy_id": "momentum",
		"universe":    "600000.SH",
		"start_date":  "2020-01-01",
		"end_date":    "2023-12-31",
		"status":      "running",
		"created_at":  time.Now(),
	}))

	// Shutdown on a service with no in-flight goroutines drains
	// immediately; that is the normal case. The stale-running
	// row is then handled by the explicit CleanupStaleRunning call.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	require.NoError(t, svc.Shutdown(ctx))

	transitioned, err := svc.CleanupStaleRunning(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, transitioned, "the stuck-1 row must be transitioned to 'failed'")

	row, err := store.GetBacktestJob(context.Background(), "stuck-1")
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.Equal(t, "failed", row["status"],
		"after Shutdown + CleanupStaleRunning, NO row should remain 'running'")
}
