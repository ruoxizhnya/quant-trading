//go:build linux || darwin

package runner

import (
	"context"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tests in this file cover the POSIX limit path: limits are applied
// inside the child via a `sh -c 'ulimit …; exec "$0" "$@"'` wrapper.
//
// They exist because the previous implementation called setrlimit(2) on
// the PARENT — the long-running analysis service — and no test ever
// passed non-zero limits, so the damage was invisible until production.

func TestLimitScript_AppliesEveryRequestedLimit(t *testing.T) {
	t.Parallel()

	script := limitScript(Limits{
		CPUSeconds:  25,
		MemoryBytes: 1 << 30,
		OpenFiles:   256,
		NumProcs:    64,
		FileSize:    1 << 20,
	})

	assert.Contains(t, script, "ulimit -t 25")
	assert.Contains(t, script, "ulimit -v 1048576", "1 GiB expressed in KiB")
	assert.Contains(t, script, "ulimit -n 256")
	assert.Contains(t, script, "ulimit -u 64")
	assert.Contains(t, script, "ulimit -f 2048", "1 MiB expressed in 512-byte blocks")
	assert.True(t, strings.HasSuffix(script, "exec \"$0\" \"$@\"\n"),
		"the wrapper must hand off to the real command, got:\n%s", script)
}

func TestLimitScript_RoundsUpSoZeroNeverMeansUnlimited(t *testing.T) {
	t.Parallel()

	// `ulimit -v 0` and `ulimit -f 0` mean UNLIMITED, not "zero bytes".
	// A tiny non-zero request must therefore round up to 1 instead of
	// truncating to 0 — otherwise the tightest possible cap silently
	// becomes no cap at all, which is the same class of bug as a
	// zero-valued config field being read as "unset".
	script := limitScript(Limits{MemoryBytes: 1, FileSize: 1})

	assert.Contains(t, script, "ulimit -v 1")
	assert.Contains(t, script, "ulimit -f 1")
	assert.NotContains(t, script, "ulimit -v 0")
	assert.NotContains(t, script, "ulimit -f 0")
}

func TestLimitScript_ChecksEveryLimitItSets(t *testing.T) {
	t.Parallel()

	// Every ulimit needs its own failure branch. A ulimit whose result
	// is ignored reproduces the AUD-11 failure mode exactly: the caller
	// believes the child is capped, the child is not, and nothing says
	// so.
	script := limitScript(Limits{CPUSeconds: 25, MemoryBytes: 1 << 30, OpenFiles: 256})

	assert.Equal(t, 3, strings.Count(script, "ulimit -"), "one ulimit per requested limit")
	assert.Equal(t, 3, strings.Count(script, "exit 125"), "every ulimit must fail closed")
	assert.Equal(t, 3, strings.Count(script, limitSetupMarker))
}

func TestLimitScript_OmitsLimitsNotRequested(t *testing.T) {
	t.Parallel()

	script := limitScript(Limits{OpenFiles: 256})

	assert.Contains(t, script, "ulimit -n 256")
	for _, flag := range []string{"-t ", "-v ", "-u ", "-f "} {
		assert.NotContains(t, script, "ulimit "+flag,
			"a limit the caller did not ask for must not be tightened")
	}
}

// TestRun_LimitsReachTheChild is the positive half of the contract:
// the caps the caller asked for must be visible from inside the child.
func TestRun_LimitsReachTheChild(t *testing.T) {
	t.Parallel()

	r := New()
	stdout, stderr, err := r.Run(context.Background(), "sh",
		[]string{"-c", "ulimit -n; ulimit -t"},
		Options{Limits: &Limits{OpenFiles: 777, CPUSeconds: 7}})
	require.NoError(t, err, "stderr: %s", stderr.String())

	lines := strings.Fields(strings.TrimSpace(stdout.String()))
	require.Len(t, lines, 2, "expected nofile and cpu, got %q", stdout.String())
	assert.Equal(t, "777", lines[0], "RLIMIT_NOFILE must be in force in the child")
	assert.Equal(t, "7", lines[1], "RLIMIT_CPU must be in force in the child")
}

// TestRun_LimitsDoNotTouchTheParent is the regression guard for AUD-11.
//
// Run() used to call setrlimit(2) on the calling process, which in
// production is the long-running analysis service. A 25-second
// RLIMIT_CPU eventually SIGXCPU-kills that daemon, a 1 GiB RLIMIT_AS
// makes it OOM, and RLIMIT_NOFILE=256 throttles an HTTP server. The
// child only inherited the damage.
//
// The requested values are deliberately huge-but-settable: if the
// parent is ever written to again, this shows up as a clean assertion
// failure instead of a SIGXCPU kill part-way through the test run.
//
// Not parallel: it samples process-wide rlimits.
func TestRun_LimitsDoNotTouchTheParent(t *testing.T) {
	before := readRlimits(t)

	r := New()
	_, stderr, err := r.Run(context.Background(), "echo", []string{"ok"}, Options{
		Limits: &Limits{CPUSeconds: 100000, MemoryBytes: 1 << 40, OpenFiles: 512},
	})
	require.NoError(t, err, "stderr: %s", stderr.String())

	after := readRlimits(t)

	assert.Equal(t, before.cpu, after.cpu, "RLIMIT_CPU must not be changed on the parent")
	assert.Equal(t, before.as, after.as, "RLIMIT_AS must not be changed on the parent")
	assert.Equal(t, before.nofile, after.nofile, "RLIMIT_NOFILE must not be changed on the parent")
}

type rlimitSnapshot struct {
	cpu    syscall.Rlimit
	as     syscall.Rlimit
	nofile syscall.Rlimit
}

func readRlimits(t *testing.T) rlimitSnapshot {
	t.Helper()

	var snap rlimitSnapshot
	require.NoError(t, syscall.Getrlimit(syscall.RLIMIT_CPU, &snap.cpu))
	require.NoError(t, syscall.Getrlimit(syscall.RLIMIT_AS, &snap.as))
	require.NoError(t, syscall.Getrlimit(syscall.RLIMIT_NOFILE, &snap.nofile))
	return snap
}

// TestRun_LimitSetupFailureFailsClosed covers the other half: when the
// shell refuses a limit, the target must not run at all.
func TestRun_LimitSetupFailureFailsClosed(t *testing.T) {
	t.Parallel()

	// 2^40 file descriptors exceeds any real hard limit, so `ulimit -n`
	// is rejected. Continuing regardless would hand the caller a child
	// with no fd cap while reporting success.
	r := New()
	stdout, stderr, err := r.Run(context.Background(), "echo", []string{"should-not-run"},
		Options{Limits: &Limits{OpenFiles: 1 << 40}})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLimitSetupFailed)
	assert.Contains(t, stderr.String(), limitSetupMarker)
	assert.Empty(t, strings.TrimSpace(stdout.String()), "the target must not have run")
}

// TestRun_Exit125WithoutMarkerIsNotASandboxFailure guards the detector
// against false positives: 125 is a legal exit status for any program,
// so only the wrapper's stderr marker may be read as a setup failure.
func TestRun_Exit125WithoutMarkerIsNotASandboxFailure(t *testing.T) {
	t.Parallel()

	r := New()
	_, _, err := r.Run(context.Background(), "sh", []string{"-c", "exit 125"}, Options{})

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrLimitSetupFailed,
		"a child exiting 125 on its own is not a sandbox setup failure")

	var ee *exec.ExitError
	require.ErrorAs(t, err, &ee)
	assert.Equal(t, 125, ee.ExitCode())
}

// TestConfigureProcessGroup_IsolatesTheChild pins the property that
// keeps a group kill from reaching the daemon.
//
// Without Setsid the child shares the analysis service's process group,
// so a future `kill -- -<pgid>` aimed at a runaway build would take the
// service down with it.
//
// Asserted at the flag rather than by observing the child's pgid: the
// only portable way to read a pgid is `ps -o pgid=`, and busybox ps
// (Alpine, and most slim containers) has no `-o`. Setsid IS the
// mechanism, so asserting it is asserting the property, and it is what
// breaks if someone drops the call.
func TestConfigureProcessGroup_IsolatesTheChild(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("echo")
	configureProcessGroup(cmd)

	require.NotNil(t, cmd.SysProcAttr)
	assert.True(t, cmd.SysProcAttr.Setsid,
		"the child must be in its own session, or a group kill reaches the daemon")
}

// TestRun_WrapperPreservesArguments pins the reason the wrapper uses
// `exec "$0" "$@"` instead of interpolating the command into a string:
// the shell must not get a second chance to parse the arguments.
func TestRun_WrapperPreservesArguments(t *testing.T) {
	t.Parallel()

	awkward := []string{"a b", "c*d", "$HOME", "quote'inside"}

	r := New()
	stdout, stderr, err := r.Run(context.Background(), "printf",
		append([]string{"[%s]"}, awkward...),
		Options{Limits: &Limits{OpenFiles: 512}})
	require.NoError(t, err, "stderr: %s", stderr.String())

	for _, a := range awkward {
		assert.Contains(t, stdout.String(), "["+a+"]",
			"argument %q must reach the child verbatim", a)
	}
}
