//go:build windows

package runner

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Windows has no setrlimit(2), so the runner cannot enforce the caps a
// caller asks for. It used to return silently from applyLimits() and
// run the child unbounded — the production composition root asks for
// 1 GiB / 25 CPU-seconds / 256 fds and got none of them, with nothing
// in the logs. These tests pin the replacement behaviour: refuse by
// default, and make the opt-out explicit and observable.

func TestRun_RefusesLimitsOnWindows(t *testing.T) {
	t.Parallel()

	r := New()
	_, _, err := r.Run(context.Background(), "echo", []string{"should-not-run"}, Options{
		Limits: &Limits{MemoryBytes: 1 << 30},
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLimitsUnsupported)
	// The message has to say what was asked for, otherwise an operator
	// staring at a failed build cannot tell which cap is missing.
	assert.Contains(t, err.Error(), "mem=1073741824B")
	assert.Contains(t, err.Error(), "WithAllowUnenforcedLimits")
}

// TestRun_RunnerLevelLimitsAlsoRefuse covers the limits that come from
// WithLimits rather than per-run Options — the production path.
func TestRun_RunnerLevelLimitsAlsoRefuse(t *testing.T) {
	t.Parallel()

	r := New(WithLimits(Limits{MemoryBytes: 1 << 30, CPUSeconds: 25, OpenFiles: 256}))
	_, _, err := r.Run(context.Background(), "echo", []string{"should-not-run"}, Options{})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLimitsUnsupported)
}

func TestRun_AllowUnenforcedLimitsOptIn(t *testing.T) {
	t.Parallel()

	var unenforced int32
	r := New(
		WithAllowUnenforcedLimits(),
		WithOnUnenforcedLimits(func([]string) { atomic.AddInt32(&unenforced, 1) }),
	)

	stdout, stderr, err := r.Run(context.Background(), "echo", []string{"ok"}, Options{
		Limits: &Limits{MemoryBytes: 1 << 30},
	})
	require.NoError(t, err, "stderr: %s", stderr.String())
	assert.Equal(t, "ok\n", stdout.String())
	assert.Equal(t, int32(1), atomic.LoadInt32(&unenforced),
		"the degradation must be observable, not silent")
}

// TestRun_OptInDoesNotFireForZeroLimits keeps the callback honest: a
// run that never asked for limits is not "degraded".
func TestRun_OptInDoesNotFireForZeroLimits(t *testing.T) {
	t.Parallel()

	var unenforced int32
	r := New(
		WithAllowUnenforcedLimits(),
		WithOnUnenforcedLimits(func([]string) { atomic.AddInt32(&unenforced, 1) }),
	)

	_, _, err := r.Run(context.Background(), "echo", []string{"ok"}, Options{})
	require.NoError(t, err)
	assert.Equal(t, int32(0), atomic.LoadInt32(&unenforced))
}
