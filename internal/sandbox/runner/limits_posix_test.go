//go:build linux || darwin

package runner

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
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
	//
	// Since AUD-25 each requested limit costs TWO ulimit calls — the
	// capability probe and the set — and each has its own status, so
	// "the shell has no such option" stays distinguishable from "the
	// shell rejected this value".
	script := limitScript(Limits{CPUSeconds: 25, MemoryBytes: 1 << 30, OpenFiles: 256})

	assert.Equal(t, 6, strings.Count(script, "ulimit -"),
		"two ulimit calls per requested limit: probe, then set")
	assert.Equal(t, 3, strings.Count(script, "exit 125"), "every set must fail closed")
	assert.Equal(t, 3, strings.Count(script, "exit 126"), "every probe must fail closed")
	assert.Equal(t, 3, strings.Count(script, limitSetupMarker))
	assert.Equal(t, 3, strings.Count(script, limitUnsupportedMarker))
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

// TestLimitScript_ProbesTheShellBeforeSettingEveryLimit pins the AUD-25
// shape: two ulimit calls per requested limit, probe first.
//
// The order matters. Probing after the set would make "the shell has no
// `ulimit -u`" and "the shell rejected this value" indistinguishable
// again, because the set's failure is what gets reported.
func TestLimitScript_ProbesTheShellBeforeSettingEveryLimit(t *testing.T) {
	t.Parallel()

	script := limitScript(Limits{
		CPUSeconds:  25,
		MemoryBytes: 1 << 30,
		OpenFiles:   256,
		NumProcs:    64,
		FileSize:    1 << 20,
	})
	lines := strings.Split(strings.TrimRight(script, "\n"), "\n")

	require.Len(t, lines, 11, "5 limits -> 5 probes + 5 sets + 1 exec, got:\n%s", script)

	for i, tc := range []struct{ flag, name string }{
		{"t", "RLIMIT_CPU"},
		{"v", "RLIMIT_AS"},
		{"n", "RLIMIT_NOFILE"},
		{"u", "RLIMIT_NPROC"},
		{"f", "RLIMIT_FSIZE"},
	} {
		want := fmt.Sprintf("ulimit -%s >/dev/null 2>&1 || { echo '%s%s' >&2; exit %d; }",
			tc.flag, limitUnsupportedMarker, tc.name, limitUnsupportedExit)
		assert.Equal(t, want, lines[2*i], "%s: the capability probe must come first", tc.name)

		set := lines[2*i+1]
		assert.True(t, strings.HasPrefix(set, "ulimit -"+tc.flag+" "),
			"%s: the set must follow its probe, got %q", tc.name, set)
		assert.Contains(t, set, limitSetupMarker)
		assert.NotContains(t, set, ">/dev/null",
			"%s: the set must not swallow the shell's complaint", tc.name)
	}

	assert.Equal(t, `exec "$0" "$@"`, lines[10])
}

// TestLimitScript_UlimitUCapabilityIsDetectedUnderRealShells runs the
// generated script under two shells whose `ulimit -u` support differs,
// so the probe is exercised in both directions rather than only
// asserted as text.
//
// The bash half is the control and it is load-bearing: without it, a
// script that simply always exited 126 would pass the dash half.
//
// Both halves run the target as a SHELL BUILTIN (`bash -c 'ulimit -u'`)
// rather than as a Go binary. RLIMIT_NPROC is counted against the real
// user's total process count, so a child that needs to clone a thread —
// which any Go program does — can fail to start under a low cap. A
// builtin forks nothing.
func TestLimitScript_UlimitUCapabilityIsDetectedUnderRealShells(t *testing.T) {
	t.Parallel()

	dash, err := exec.LookPath("dash")
	if err != nil {
		t.Skip("dash is not installed in this image, so the unsupported branch cannot be exercised here")
	}
	bash, err := exec.LookPath("bash")
	require.NoError(t, err, "bash is the control group; without it this test proves nothing")

	const want = 64
	script := limitScript(Limits{NumProcs: want})

	run := func(t *testing.T, shell string) (stdout, stderr string, code int) {
		t.Helper()
		// $0 and $@ become the target, which must be a shell that HAS
		// `ulimit -u` — otherwise the success case would fail on the
		// inner shell and look like a wrapper bug.
		cmd := exec.Command(shell, "-c", script, bash, "-c", "ulimit -u")
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		runErr := cmd.Run()
		if runErr == nil {
			return out.String(), errb.String(), 0
		}
		var ee *exec.ExitError
		require.ErrorAs(t, runErr, &ee, "shell %s: %v", shell, runErr)
		return out.String(), errb.String(), ee.ExitCode()
	}

	t.Run("dash lacks ulimit -u and the run aborts", func(t *testing.T) {
		stdout, stderr, code := run(t, dash)
		assert.Equal(t, limitUnsupportedExit, code, "stderr: %s", stderr)
		assert.Contains(t, stderr, limitUnsupportedMarker+"RLIMIT_NPROC",
			"the marker must name the missing limit")
		assert.Empty(t, stdout, "the target must not run when the cap cannot be applied")
	})

	t.Run("bash has it and the cap reaches the target", func(t *testing.T) {
		stdout, stderr, code := run(t, bash)
		require.Equal(t, 0, code, "stderr: %s", stderr)
		assert.Equal(t, strconv.Itoa(want), strings.TrimSpace(stdout),
			"the target must observe the cap, not the shell default")
	})
}

// TestRun_NumProcsIsEitherEnforcedOrReportedUnsupported is the contract
// AUD-25 establishes for `ulimit -u` through the public API.
//
// Which branch is taken depends on which `sh` this system has — dash on
// Debian/Ubuntu, busybox ash on Alpine — so both are asserted rather
// than one being skipped. What must never happen is a bare "cannot set
// RLIMIT_NPROC": that reads like a bad value and sends the operator
// hunting for a number that was never the problem.
func TestRun_NumProcsIsEitherEnforcedOrReportedUnsupported(t *testing.T) {
	t.Parallel()

	const want = 64

	// The target is a shell builtin, not a Go program: see the note in
	// TestLimitScript_UlimitUCapabilityIsDetectedUnderRealShells about
	// RLIMIT_NPROC and thread creation.
	r := New()
	stdout, stderr, err := r.Run(context.Background(), "sh",
		[]string{"-c", "ulimit -u"},
		Options{Limits: &Limits{NumProcs: want}})

	if err != nil {
		assert.ErrorIs(t, err, ErrLimitsUnsupported,
			"a shell without `ulimit -u` is a capability gap, not a rejected value; stderr: %s",
			stderr.String())
		assert.NotErrorIs(t, err, ErrLimitSetupFailed,
			"reporting a value problem for a missing shell option is the AUD-25 bug")
		assert.Empty(t, strings.TrimSpace(stdout.String()),
			"the target must not run when the cap cannot be applied")
		return
	}

	assert.Equal(t, strconv.Itoa(want), strings.TrimSpace(stdout.String()),
		"a successful run must show the cap in force, not a shell default")
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

// TestRun_Exit126WithoutMarkerIsNotASandboxFailure is the symmetric
// guard for the AUD-25 status: 126 is a legal exit status for any
// program, so only the wrapper's marker may be read as "the shell has
// no such ulimit".
func TestRun_Exit126WithoutMarkerIsNotASandboxFailure(t *testing.T) {
	t.Parallel()

	r := New()
	_, _, err := r.Run(context.Background(), "sh", []string{"-c", "exit 126"}, Options{})

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrLimitsUnsupported,
		"a child exiting 126 on its own is not a missing shell capability")
	assert.NotErrorIs(t, err, ErrLimitSetupFailed)

	var ee *exec.ExitError
	require.ErrorAs(t, err, &ee)
	assert.Equal(t, 126, ee.ExitCode())
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
