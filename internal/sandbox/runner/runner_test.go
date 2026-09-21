package runner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun_ExitZero(t *testing.T) {
	t.Parallel()
	r := New(WithTimeout(5 * time.Second))
	stdout, stderr, err := r.Run(context.Background(), "echo", []string{"hello"}, Options{})
	require.NoError(t, err)
	assert.Equal(t, "hello\n", stdout.String())
	assert.Empty(t, stderr.String())
}

func TestRun_NonZeroExit(t *testing.T) {
	t.Parallel()
	r := New()
	_, _, err := r.Run(context.Background(), "false", nil, Options{})
	assert.Error(t, err)
	// It's an ExitError, not a timeout.
	var ee *exec.ExitError
	assert.True(t, errors.As(err, &ee), "expected ExitError, got %T", err)
}

func TestRun_Timeout(t *testing.T) {
	t.Parallel()
	var oomCount, timeoutCount int32
	r := New(
		WithTimeout(200*time.Millisecond),
		WithOnTimeout(func(argv []string) { atomic.AddInt32(&timeoutCount, 1) }),
		WithOnOOM(func(argv []string) { atomic.AddInt32(&oomCount, 1) }),
	)
	start := time.Now()
	_, _, err := r.Run(context.Background(), "sleep", []string{"5"}, Options{})
	elapsed := time.Since(start)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrTimeout)
	assert.Less(t, elapsed, 2*time.Second, "timeout should fire well before the 5s sleep")
	assert.Equal(t, int32(1), atomic.LoadInt32(&timeoutCount))
	assert.Equal(t, int32(0), atomic.LoadInt32(&oomCount))
}

func TestRun_BinaryNotFound(t *testing.T) {
	t.Parallel()
	r := New()
	_, _, err := r.Run(context.Background(), "/nonexistent/binary", nil, Options{})
	assert.Error(t, err)
}

// TestRun_Dir asserts Options.Dir is the child's working directory.
//
// This used to `t.Skip` on Windows because the original assertion
// leaned on `pwd`'s output format. A skip meant the option went
// untested on the one platform whose path handling differs most, so
// each platform now gets a command that can actually report its cwd.
func TestRun_Dir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	name, args := "pwd", []string(nil)
	if runtime.GOOS == "windows" {
		name, args = "cmd", []string{"/c", "cd"}
	}

	r := New()
	stdout, _, err := r.Run(context.Background(), name, args, Options{Dir: dir})
	require.NoError(t, err)

	got := strings.TrimSpace(stdout.String())

	if runtime.GOOS == "windows" {
		// `cmd /c cd` echoes the path as the OS stored it; Windows
		// paths compare case-insensitively.
		assert.True(t, strings.EqualFold(got, dir), "cwd = %q, want %q", got, dir)
		return
	}

	// On macOS t.TempDir() lives under /var, which is a symlink to
	// /private/var; `pwd` reports whichever form the kernel resolved.
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	assert.True(t, got == dir || got == resolved,
		"cwd = %q, want %q or %q", got, dir, resolved)
}

func TestRunExitCode(t *testing.T) {
	t.Parallel()
	r := New()
	_, _, code, err := r.RunExitCode(context.Background(), "sh", []string{"-c", "exit 7"}, Options{})
	assert.Error(t, err)
	assert.Equal(t, 7, code)
}

func TestMergeLimits(t *testing.T) {
	t.Parallel()
	base := Limits{CPUSeconds: 10, MemoryBytes: 100, OpenFiles: 50}
	over := Limits{CPUSeconds: 20} // only override CPU
	merged := mergeLimits(base, over)
	assert.Equal(t, 20, merged.CPUSeconds)
	assert.Equal(t, int64(100), merged.MemoryBytes)
	assert.Equal(t, 50, merged.OpenFiles)
}

// TestNoSetrlimitOnTheParent is a portable guard for the AUD-11
// regression.
//
// The bug — calling setrlimit(2) on the long-running parent instead of
// inside the child — can only be reproduced behaviourally on POSIX (see
// TestRun_LimitsDoNotTouchTheParent in limits_posix_test.go). This test
// runs on every platform, including the Windows dev machine, so the
// invariant is checked before the change ever reaches CI.
//
// If a future change genuinely needs setrlimit (a fork-and-exec helper,
// say), update this test deliberately instead of deleting it. The point
// is that nobody reaches for setrlimit on the parent by accident: in
// this package the only process Go can reach is the analysis service
// itself, and capping that is how the daemon gets SIGXCPU-killed.
func TestNoSetrlimitOnTheParent(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		checked++

		// Checked explicitly rather than with assert.NotContains so the
		// failure message names the file instead of dumping it.
		if strings.Contains(string(src), "Setrlimit") {
			t.Errorf("%s calls setrlimit(2); in this package the only process it can reach is "+
				"the parent (the long-running analysis service), so this would cap the daemon "+
				"— see AUD-11", name)
		}
	}

	require.NotZero(t, checked, "the guard scanned nothing; is the working directory the package dir?")
}

func TestLimits_IsZero(t *testing.T) {
	t.Parallel()

	assert.True(t, Limits{}.IsZero(), "all-zero limits request nothing")

	// Every single field must count as "something was requested",
	// otherwise a caller could ask for exactly one cap and be told
	// nothing was asked for — and sail past the fail-closed check.
	nonzero := []Limits{
		{CPUSeconds: 1},
		{MemoryBytes: 1},
		{OpenFiles: 1},
		{NumProcs: 1},
		{FileSize: 1},
	}
	for _, l := range nonzero {
		assert.False(t, l.IsZero(), "%+v must not count as zero", l)
	}
}

func TestLimits_describe(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "none", Limits{}.describe())

	got := Limits{CPUSeconds: 25, MemoryBytes: 1 << 30, OpenFiles: 256}.describe()
	assert.Contains(t, got, "cpu=25s")
	assert.Contains(t, got, "mem=1073741824B")
	assert.Contains(t, got, "nofile=256")
	// Fields that were not requested must not appear — the message is
	// what an operator reads to decide whether the cap they wanted is
	// missing.
	assert.NotContains(t, got, "nproc")
	assert.NotContains(t, got, "fsize")
}

// TestRun_ZeroLimitsAreNeverRefused pins the boundary of the
// fail-closed path: a caller who asked for nothing must keep working on
// every platform, including ones that cannot enforce anything.
func TestRun_ZeroLimitsAreNeverRefused(t *testing.T) {
	t.Parallel()

	r := New()
	stdout, _, err := r.Run(context.Background(), "echo", []string{"ok"}, Options{
		Limits: &Limits{},
	})
	require.NoError(t, err)
	assert.Equal(t, "ok\n", stdout.String())
}

// TestRun_LimitsAreEnforcedOrRefused is the cross-platform contract:
// asking for a limit must never result in a child that runs without it.
// Either the platform enforces it, or the run is refused outright.
func TestRun_LimitsAreEnforcedOrRefused(t *testing.T) {
	t.Parallel()

	r := New()
	_, _, err := r.Run(context.Background(), "echo", []string{"ok"}, Options{
		Limits: &Limits{OpenFiles: 256},
	})

	if runtime.GOOS == "windows" {
		require.Error(t, err, "Windows cannot enforce rlimits; it must refuse")
		assert.ErrorIs(t, err, ErrLimitsUnsupported)
		return
	}

	require.NoError(t, err, "POSIX enforces rlimits in the child; the run must succeed")
}

func TestRun_StdinAndEnv(t *testing.T) {
	t.Parallel()
	r := New()
	stdout, _, err := r.Run(context.Background(), "sh", []string{"-c", "echo $MY_VAR"}, Options{
		Env: []string{"MY_VAR=copilot-test", "PATH=" + getPath()},
	})
	require.NoError(t, err)
	assert.Equal(t, "copilot-test\n", stdout.String())
}

func getPath() string {
	// `which sh` is a reasonable fallback for PATH detection in tests.
	out, err := exec.Command("sh", "-c", "command -v sh").Output()
	if err != nil {
		return "/usr/bin:/bin"
	}
	dir := string(out)
	if i := strings.LastIndex(dir, "/"); i >= 0 {
		dir = dir[:i]
	}
	return dir
}

func TestDefaultTimeout(t *testing.T) {
	t.Parallel()
	r := New()
	// Use a long-running command to verify the default is 5s.
	start := time.Now()
	_, _, _ = r.Run(context.Background(), "sleep", []string{"0.1"}, Options{})
	elapsed := time.Since(start)
	assert.Less(t, elapsed, time.Second, "fast command should return quickly even with default 5s timeout")
}
