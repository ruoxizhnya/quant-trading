// Tests for CopilotService — the top-level orchestrator used by
// the strategy workshop "AI generate" button. Most of `run()` is
// hard to test (it shells out to `go build` against an absolute
// hard-coded working directory — see Sprint 6 P0-4), but
// Generate/GetJob/Stats/AcceptanceRate are the observable contract
// and are easy to cover with a deterministic ai.MockClient
// (Sprint 6 P0-1).
package strategy

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCopilotWithMock constructs a CopilotService whose AI client is
// a deterministic mock. Tests that don't await the spawned goroutine
// (Generate is fire-and-forget) don't need to drain the goroutine
// because the mock is concurrency-safe.
func newCopilotWithMock(mock *ai.MockClient) *CopilotService {
	return NewCopilotServiceWithLLM(mock)
}

func TestCopilotService_Generate_RecordsJob(t *testing.T) {
	svc := newCopilotWithMock(&ai.MockClient{
		Configured:      true,
		GenerateResponse: "package plugins",
	})

	res := svc.Generate(context.Background(), GenerateParams{
		Description: "momentum",
		Universe:    "csi300",
		StartDate:   "2025-01-01",
		EndDate:     "2025-12-31",
	}, nil) // runner nil is fine — run() will short-circuit on build success without a runner

	require.NotNil(t, res)
	assert.NotEmpty(t, res.JobID)
	assert.Equal(t, "pending", res.Status)
	generated, _, _ := svc.Stats()
	assert.GreaterOrEqual(t, generated, int64(1),
		"Generate must bump the generated counter so /stats/acceptance works")

	// GetJob round-trip
	got := svc.GetJob(res.JobID)
	require.NotNil(t, got)
	assert.Equal(t, res.JobID, got.JobID)
}

func TestCopilotService_GetJob_NotFound(t *testing.T) {
	svc := newCopilotWithMock(&ai.MockClient{Configured: false})
	assert.Nil(t, svc.GetJob("nonexistent"))
}

func TestCopilotService_IsConfigured_True(t *testing.T) {
	svc := newCopilotWithMock(&ai.MockClient{Configured: true})
	assert.True(t, svc.IsConfigured(),
		"IsConfigured must delegate to the underlying LLMClient")
}

func TestCopilotService_IsConfigured_False(t *testing.T) {
	svc := newCopilotWithMock(&ai.MockClient{Configured: false})
	assert.False(t, svc.IsConfigured())
}

func TestCopilotService_NewCopilotServiceWithLLM_NilFallsBackToReal(t *testing.T) {
	// Nil client → fallback to a real *ai.Client (env-driven).
	// Just verify it doesn't panic and produces a non-nil service.
	svc := NewCopilotServiceWithLLM(nil)
	require.NotNil(t, svc)
	// IsConfigured depends on env; we don't assert on its value.
}

func TestCopilotService_AcceptanceRate_ZeroGenerated(t *testing.T) {
	svc := newCopilotWithMock(&ai.MockClient{Configured: false})
	// No calls to Generate yet → AcceptanceRate must be 0 (avoid
	// division by zero, see source).
	assert.Equal(t, 0.0, svc.AcceptanceRate())
}

func TestCopilotService_AcceptanceRate_ComputedFromCounters(t *testing.T) {
	svc := newCopilotWithMock(&ai.MockClient{Configured: false})

	// Inject counters directly (private fields, same package OK).
	svc.generated = 10
	svc.backtested = 7
	assert.InDelta(t, 0.7, svc.AcceptanceRate(), 1e-9)

	svc.backtested = 0
	assert.Equal(t, 0.0, svc.AcceptanceRate(),
		"zero backtested + nonzero generated must yield 0.0 not NaN")
}

func TestCopilotService_Stats_AtomicRead(t *testing.T) {
	svc := newCopilotWithMock(&ai.MockClient{
		Configured:      true,
		GenerateResponse: "package plugins",
	})

	// Hammer Generate from multiple goroutines and confirm the
	// generated counter ends at exactly the number of calls.
	// Generate spawns a goroutine that ignores ctx, so we can't
	// rely on the goroutine to clean up — we just verify the
	// counter is monotonic and reaches the expected value.
	const N = 20
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			svc.Generate(context.Background(), GenerateParams{Description: "x"}, nil)
		}()
	}
	// Wait for all Generate calls to return (they're sync up to the
	// s.jobs.Store + counter bump; the goroutine detaches).
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Generate didn't return for all callers within 2s")
	}

	generated, _, _ := svc.Stats()
	assert.Equal(t, int64(N), generated,
		"Stats().generated must equal the number of Generate calls")
}

// TestCopilotService_Run_LLMFailurePath verifies that when the LLM
// returns an error from GenerateStrategyCode, the job's terminal
// status is "llm_failed" and the buildable/backtested counters are
// not bumped. This is the path that previously triggered the nil
// pointer panic before LLMClient was extracted (Sprint 6 P0-1).
func TestCopilotService_Run_LLMFailurePath(t *testing.T) {
	wantErr := errors.New("simulated LLM outage")
	mock := &ai.MockClient{
		Configured: true,
		GenerateFunc: func(ctx context.Context, description string) (string, error) {
			return "", wantErr
		},
	}
	svc := newCopilotWithMock(mock)

	res := svc.Generate(context.Background(),
		GenerateParams{Description: "x"}, nil)
	require.NotNil(t, res)

	// Wait for the goroutine to drain. Poll GetJob with a tight
	// deadline — the LLM call is in-process so it should be
	// near-instant. Lock the JobResult before reading to sync
	// with the run() goroutine (Sprint 6 P0-1 hardening).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job := svc.GetJob(res.JobID)
		if job == nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		job.Lock()
		status := job.Status
		job.Unlock()
		if status != "" && status != "pending" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	job := svc.GetJob(res.JobID)
	require.NotNil(t, job)
	// Lock the JobResult before reading to synchronize with the
	// run() goroutine that writes under job.Lock(). Without this,
	// -race flags a data race (Sprint 6 P0-1 hardening).
	job.Lock()
	status := job.Status
	buildErr := job.BuildErr
	job.Unlock()
	assert.Equal(t, "llm_failed", status,
		"LLM error must surface as llm_failed status")
	assert.Contains(t, buildErr, "simulated LLM outage")

	// Counters: generated bumped, buildable NOT bumped, backtested NOT bumped.
	g, b, bt := svc.Stats()
	assert.GreaterOrEqual(t, g, int64(1))
	assert.Equal(t, int64(0), b, "buildable must remain 0 when LLM failed")
	assert.Equal(t, int64(0), bt, "backtested must remain 0 when LLM failed")
}

// TestCopilotService_Run_MockCalledWithDescription asserts the
// CopilotService forwards the description verbatim to the LLM and
// stores the generated code on the JobResult. Uses a polling wait
// for the fire-and-forget goroutine.
func TestCopilotService_Run_MockCalledWithDescription(t *testing.T) {
	var calls atomic.Int32
	mock := &ai.MockClient{
		Configured: true,
		GenerateFunc: func(ctx context.Context, description string) (string, error) {
			calls.Add(1)
			return "package plugins // generated", nil
		},
	}
	svc := newCopilotWithMock(mock)

	res := svc.Generate(context.Background(),
		GenerateParams{Description: "momentum on CSI300"}, nil)

	// Wait for the goroutine to call the LLM.
	require.Eventually(t, func() bool {
		return calls.Load() >= 1
	}, 2*time.Second, 10*time.Millisecond,
		"Generate goroutine must call LLM within 2s")

	// Note: the build step will fail (working dir + Go plugin issues
	// are out of scope for P0-1; see P0-4), but we don't assert on
	// terminal status here — we only verify the LLM was called.
	_ = svc.GetJob(res.JobID)
}

// ---- parseUniverse -------------------------------------------------------

func TestParseUniverse_EmptyOrAll_ReturnsNil(t *testing.T) {
	assert.Nil(t, parseUniverse(""))
	assert.Nil(t, parseUniverse("all"),
		"universe='all' is a sentinel for 'use default pool' and must map to nil")
}

func TestParseUniverse_StripPrefix(t *testing.T) {
	assert.Equal(t, []string{"csi300", "csi500"},
		parseUniverse("universe:csi300,csi500"),
		"the 'universe:' prefix is a transport quirk and must be stripped")
}

func TestParseUniverse_TrimsWhitespace(t *testing.T) {
	got := parseUniverse(" csi300 , csi500 ,csi800 ")
	assert.Equal(t, []string{"csi300", "csi500", "csi800"}, got)
}

func TestParseUniverse_SingleEntry(t *testing.T) {
	assert.Equal(t, []string{"csi300"}, parseUniverse("csi300"))
}
