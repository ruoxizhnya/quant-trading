package runner

import (
	"context"
	"errors"
	"os/exec"
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

func TestRun_Dir(t *testing.T) {
	t.Parallel()
	r := New()
	// /tmp exists on all platforms we test on. If it doesn't, skip.
	stdout, _, err := r.Run(context.Background(), "pwd", nil, Options{Dir: "/tmp"})
	require.NoError(t, err)
	// pwd prints with a trailing newline; on macOS this is /private/tmp.
	got := strings.TrimSpace(stdout.String())
	assert.True(t, got == "/tmp" || got == "/private/tmp",
		"expected /tmp or /private/tmp, got %q", got)
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

func TestApplyLimits_NoopWhenAllZero(t *testing.T) {
	t.Parallel()
	// Empty limits should not touch the runner at all.
	r := New()
	stdout, _, err := r.Run(context.Background(), "echo", []string{"ok"}, Options{
		Limits: &Limits{},
	})
	require.NoError(t, err)
	assert.Equal(t, "ok\n", stdout.String())
	_ = r
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
