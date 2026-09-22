//go:build windows

package runner

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// Windows has no setrlimit(2), so the caps a caller asks for have to be
// applied through a Job Object — and a Job Object cannot express all of
// them.
//
// Before AUD-24 this file returned silently from applyLimits() and ran
// the child unbounded: the production composition root asks for 1 GiB /
// 25 CPU-seconds / 256 fds and got none of them, with nothing in the
// logs. AUD-24 replaced that with two things, and these tests cover both
// halves:
//
//   - the mappable subset (CPUSeconds, MemoryBytes, NumProcs) is really
//     applied, and there is a behavioural test per field with a control
//     run, because "the struct was filled in" is not evidence;
//   - the unmappable subset (OpenFiles, FileSize) is refused, or dropped
//     visibly via WithAllowUnenforcedLimits.
//
// AUD-26: the child is this test binary re-entering itself, not `echo`.
// `echo` is not a Windows program — it only exists here because Git for
// Windows happens to be on PATH, which made these tests pass on the dev
// machine and fail on a bare Windows box.

// TestUnenforceableOnWindows pins the split between what a Job Object
// can express and what it cannot. Everything else here depends on it.
func TestUnenforceableOnWindows(t *testing.T) {
	t.Parallel()

	all := Limits{CPUSeconds: 25, MemoryBytes: 1 << 30, OpenFiles: 256, NumProcs: 64, FileSize: 1 << 20}
	assert.Equal(t, Limits{OpenFiles: 256, FileSize: 1 << 20}, unenforceableOnWindows(all),
		"only the handle-count and file-size caps have no Job Object equivalent")

	for _, mappable := range []Limits{{CPUSeconds: 1}, {MemoryBytes: 1}, {NumProcs: 1}} {
		assert.True(t, unenforceableOnWindows(mappable).IsZero(),
			"%+v maps onto a Job Object and must not be reported as dropped", mappable)
	}
}

// TestCreateJobObject_MapsEverySupportedLimit reads the Job Object back
// instead of trusting the struct we just filled in. The 100ns unit
// conversion and the flag bits are exactly the kind of thing that is
// wrong while looking right.
func TestCreateJobObject_MapsEverySupportedLimit(t *testing.T) {
	t.Parallel()

	job, err := createJobObject(Limits{CPUSeconds: 25, MemoryBytes: 1 << 30, NumProcs: 64})
	require.NoError(t, err)
	defer windows.CloseHandle(job)

	info := queryJobObject(t, job)
	flags := info.BasicLimitInformation.LimitFlags

	assert.NotZero(t, flags&windows.JOB_OBJECT_LIMIT_PROCESS_TIME, "CPU seconds")
	assert.NotZero(t, flags&windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY, "memory")
	assert.NotZero(t, flags&windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS, "process count")
	assert.NotZero(t, flags&windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		"without this a runaway child outlives the daemon that started it")

	assert.Equal(t, int64(25)*10_000_000, info.BasicLimitInformation.PerProcessUserTimeLimit,
		"JOB_OBJECT_LIMIT_PROCESS_TIME is in 100-nanosecond units, not seconds")
	assert.Equal(t, uintptr(1<<30), info.ProcessMemoryLimit)
	assert.Equal(t, uint32(64), info.BasicLimitInformation.ActiveProcessLimit)
}

// TestCreateJobObject_SetsNoFlagForUnmappableLimits is the other half of
// the mapping: a cap that cannot be expressed must not be quietly
// translated into a different one.
func TestCreateJobObject_SetsNoFlagForUnmappableLimits(t *testing.T) {
	t.Parallel()

	job, err := createJobObject(Limits{OpenFiles: 256, FileSize: 1 << 20})
	require.NoError(t, err)
	defer windows.CloseHandle(job)

	flags := queryJobObject(t, job).BasicLimitInformation.LimitFlags
	for name, f := range map[string]uint32{
		"PROCESS_TIME":   windows.JOB_OBJECT_LIMIT_PROCESS_TIME,
		"PROCESS_MEMORY": windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY,
		"ACTIVE_PROCESS": windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS,
	} {
		assert.Zero(t, flags&f, "%s must not be set: nothing mappable was requested", name)
	}
	assert.NotZero(t, flags&windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE)
}

// TestRun_JobObjectMemoryLimitIsEnforced proves the cap is in force
// rather than merely recorded.
//
// The control run comes first and allocates the same amount under a
// generous cap. Without it, a child that failed for an unrelated reason
// — no memory on the machine, a broken helper — would look like the cap
// working.
func TestRun_JobObjectMemoryLimitIsEnforced(t *testing.T) {
	t.Parallel()

	const allocMiB = 512

	opts := helperOptions("alloc", helperAllocEnv+"="+strconv.Itoa(allocMiB))
	r := New(WithTimeout(30 * time.Second))

	opts.Limits = &Limits{MemoryBytes: 2 << 30}
	_, stderr, err := r.Run(context.Background(), helperBinary(t), nil, opts)
	require.NoError(t, err, "control: 512 MiB must fit in a 2 GiB job; stderr: %s", stderr.String())

	opts.Limits = &Limits{MemoryBytes: 128 << 20}
	_, _, err = r.Run(context.Background(), helperBinary(t), nil, opts)
	assert.Error(t, err, "512 MiB must not fit in a 128 MiB job")
}

// TestRun_JobObjectActiveProcessLimitIsEnforced observes
// JOB_OBJECT_LIMIT_ACTIVE_PROCESS, which blocks process creation instead
// of killing anything — so the child survives to report what happened.
func TestRun_JobObjectActiveProcessLimitIsEnforced(t *testing.T) {
	t.Parallel()

	opts := helperOptions("spawn")
	r := New(WithTimeout(30 * time.Second))

	stdout, stderr, err := r.Run(context.Background(), helperBinary(t), nil, opts)
	require.NoError(t, err, "control: stderr: %s", stderr.String())
	require.Equal(t, "spawned\n", stdout.String(),
		"control: with no cap the helper must be able to start a child")

	opts.Limits = &Limits{NumProcs: 1}
	stdout, stderr, err = r.Run(context.Background(), helperBinary(t), nil, opts)
	require.NoError(t, err, "the capped child itself survives; stderr: %s", stderr.String())
	assert.Equal(t, "blocked\n", stdout.String(),
		"a job capped at one active process must refuse to create a second")
}

// TestRun_JobObjectCPUTimeLimitIsEnforced observes
// JOB_OBJECT_LIMIT_PROCESS_TIME. Unlike POSIX RLIMIT_CPU, exceeding it
// terminates the process outright, so the only thing to assert is that
// it died before its burn would have ended on its own.
//
// The assertion is deliberately LOOSE, because the mechanism is. Measured
// on this machine (see the AUD-24 landing note in docs/TASKS.md):
//
//	limit 0.1s, spin 8s -> killed at 5.3s of user time
//	limit   1s, spin 8s -> killed at 6.1-7.2s
//	limit   3s, spin 8s -> killed at 6.4s
//	no limit,   spin 8s -> ran the full 8.1s and exited 0
//
// So the limit is real — the control proves the process dies only when
// one is set — but the firing point barely moves with the value: it is a
// backstop against a runaway build, not a hard "at most N CPU-seconds".
// A spin short enough to make this test fast would be flaky.
//
// Skipped under -short. It cannot run in CI either way: CI is Linux, and
// this file is //go:build windows (see AUD-38, which adds a compile-only
// gate so at least the build is checked).
func TestRun_JobObjectCPUTimeLimitIsEnforced(t *testing.T) {
	if testing.Short() {
		t.Skip("burns a real CPU core for several seconds; run without -short")
	}
	t.Parallel()

	const spinMS = 12000

	opts := helperOptions("spin", helperSpinEnv+"="+strconv.Itoa(spinMS))
	r := New(WithTimeout(30 * time.Second))

	opts.Limits = &Limits{CPUSeconds: 1}
	start := time.Now()
	_, _, err := r.Run(context.Background(), helperBinary(t), nil, opts)
	elapsed := time.Since(start)

	assert.Error(t, err, "a 12s CPU burn must not survive a 1 CPU-second cap")
	assert.Less(t, elapsed, time.Duration(spinMS)*time.Millisecond,
		"the cap must fire before the burn would have ended on its own")
}

// TestRun_RefusesUnenforceableLimitsOnWindows pins the fail-closed path.
//
// OpenFiles is the field this uses because it is the one a Job Object
// genuinely cannot express, so the refusal stays live no matter how much
// more of the Job Object API gets wired up.
func TestRun_RefusesUnenforceableLimitsOnWindows(t *testing.T) {
	t.Parallel()

	opts := helperOptions("print", helperTextEnv+"=should-not-run")
	opts.Limits = &Limits{OpenFiles: 256}

	r := New()
	_, _, err := r.Run(context.Background(), helperBinary(t), nil, opts)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLimitsUnsupported)
	// The message has to say what was asked for, otherwise an operator
	// staring at a failed build cannot tell which cap is missing.
	assert.Contains(t, err.Error(), "nofile=256")
	assert.Contains(t, err.Error(), "WithAllowUnenforcedLimits")
}

// TestRun_RunnerLevelLimitsAlsoRefuse covers the limits that come from
// WithLimits rather than per-run Options — the production path.
func TestRun_RunnerLevelLimitsAlsoRefuse(t *testing.T) {
	t.Parallel()

	r := New(WithLimits(Limits{MemoryBytes: 1 << 30, CPUSeconds: 25, OpenFiles: 256}))
	_, _, err := r.Run(context.Background(), helperBinary(t), nil,
		helperOptions("print", helperTextEnv+"=should-not-run"))

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLimitsUnsupported)
	assert.Contains(t, err.Error(), "nofile=256",
		"the message must name the cap that cannot be expressed, not the whole request")
}

// TestRun_MappableLimitsNeedNoOptOut is the AUD-24 improvement stated
// positively: a request a Job Object can satisfy is enforced, not
// refused, and nothing is reported as degraded.
func TestRun_MappableLimitsNeedNoOptOut(t *testing.T) {
	t.Parallel()

	var unenforced int32
	r := New(WithOnUnenforcedLimits(func([]string, Limits) { atomic.AddInt32(&unenforced, 1) }))

	opts := helperOptions("print", helperTextEnv+"=ok")
	opts.Limits = &Limits{MemoryBytes: 1 << 30, CPUSeconds: 25, NumProcs: 8}

	stdout, stderr, err := r.Run(context.Background(), helperBinary(t), nil, opts)
	require.NoError(t, err, "every cap here maps onto a Job Object; stderr: %s", stderr.String())
	assert.Equal(t, "ok\n", stdout.String())
	assert.Equal(t, int32(0), atomic.LoadInt32(&unenforced),
		"nothing was dropped, so nothing may be reported as degraded")
}

// TestRun_AllowUnenforcedLimitsOptIn covers the documented opt-out, and
// the reason the callback takes a Limits argument: "some cap was not
// applied" is not actionable, the operator needs to know which one.
func TestRun_AllowUnenforcedLimitsOptIn(t *testing.T) {
	t.Parallel()

	var got Limits
	var calls int32
	r := New(
		WithAllowUnenforcedLimits(),
		WithOnUnenforcedLimits(func(_ []string, unenforced Limits) {
			atomic.AddInt32(&calls, 1)
			got = unenforced
		}),
	)

	opts := helperOptions("print", helperTextEnv+"=ok")
	// MemoryBytes IS enforceable, so it must not appear in the report:
	// the point is that the mappable subset still gets applied while the
	// rest is named.
	opts.Limits = &Limits{MemoryBytes: 1 << 30, OpenFiles: 256}

	stdout, stderr, err := r.Run(context.Background(), helperBinary(t), nil, opts)
	require.NoError(t, err, "stderr: %s", stderr.String())
	assert.Equal(t, "ok\n", stdout.String())
	require.Equal(t, int32(1), atomic.LoadInt32(&calls),
		"the degradation must be observable, not silent")
	assert.Equal(t, Limits{OpenFiles: 256}, got,
		"the report must name only what was actually dropped")
}

// TestRun_OptInDoesNotFireForZeroLimits keeps the callback honest: a
// run that never asked for limits is not "degraded".
func TestRun_OptInDoesNotFireForZeroLimits(t *testing.T) {
	t.Parallel()

	var unenforced int32
	r := New(
		WithAllowUnenforcedLimits(),
		WithOnUnenforcedLimits(func([]string, Limits) { atomic.AddInt32(&unenforced, 1) }),
	)

	// Limits stays nil so the runner-level (zero) limits apply — the point
	// is that nothing was requested, so nothing was degraded.
	_, _, err := r.Run(context.Background(), helperBinary(t), nil,
		helperOptions("print", helperTextEnv+"=ok"))
	require.NoError(t, err)
	assert.Equal(t, int32(0), atomic.LoadInt32(&unenforced))
}

func queryJobObject(t *testing.T, job windows.Handle) windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION {
	t.Helper()

	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	require.NoError(t, windows.QueryInformationJobObject(job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil))
	return info
}
