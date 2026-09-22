package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helper re-entry (AUD-26)
// ---------------------------------------------------------------------------
//
// These tests used to shell out to `echo` / `false` / `sleep` / `sh`. Those
// exist on every POSIX box, and on a Windows machine that happens to have Git
// for Windows on PATH — and nowhere else. On a bare Windows box eight of the
// tests failed, so "green locally" was a property of the developer's machine,
// not of the code under test.
//
// They now re-enter THIS test binary instead. A native executable is the one
// child every platform is guaranteed to have, it needs no PATH lookup, and it
// makes the tests hermetic: what they exercise is the runner, not the host's
// userland.
//
// The mode travels in the ENVIRONMENT rather than in argv, because argv is
// exactly what several of these tests are asserting on.

const (
	helperModeEnv  = "RUNNER_TEST_HELPER_MODE"
	helperTextEnv  = "RUNNER_TEST_HELPER_TEXT"
	helperVarEnv   = "RUNNER_TEST_HELPER_VAR"
	helperExitEnv  = "RUNNER_TEST_HELPER_EXIT"
	helperSleepEnv = "RUNNER_TEST_HELPER_SLEEP_MS"
	helperAllocEnv = "RUNNER_TEST_HELPER_ALLOC_MIB"
	helperSpinEnv  = "RUNNER_TEST_HELPER_SPIN_MS"

	// suiteGuardEnv is set by the normal path of TestMain and inherited by
	// any child that was started with Options.Env == nil.
	suiteGuardEnv = "RUNNER_TEST_SUITE_ACTIVE"
)

// TestMain intercepts helper mode BEFORE the testing framework starts.
//
// The interception lives here rather than in a `TestHelperProcess` function
// because M.Run() is what calls flag.Parse() and what prints the trailing
// "PASS" line (testing/testing.go:2247 and the summary printer). Either would
// land in the stdout the runner is capturing, and `assert.Equal("hello\n",
// stdout)` would fail for a reason that has nothing to do with the runner.
// Returning from TestMain only after the helper has exited keeps the child's
// output byte-for-byte ours.
func TestMain(m *testing.M) {
	if mode := os.Getenv(helperModeEnv); mode != "" {
		os.Exit(runHelper(mode))
	}

	// A child started with Options.Env == nil inherits this process's
	// environment, so it would arrive here with no mode and run the whole
	// suite — spawning another such child, and another. Refuse loudly
	// instead: an ExitError in the test that forgot helperOptions is a much
	// better outcome than a fork bomb.
	if os.Getenv(suiteGuardEnv) != "" {
		fmt.Fprintln(os.Stderr, "runner tests: child invocation without "+helperModeEnv+
			"; refusing to recurse. Pass Options.Env via helperOptions().")
		os.Exit(97)
	}
	os.Setenv(suiteGuardEnv, "1")

	os.Exit(m.Run())
}

// runHelper is the child side. It never returns to the test framework.
func runHelper(mode string) int {
	switch mode {
	case "print":
		fmt.Println(os.Getenv(helperTextEnv))
		return 0
	case "printenv":
		fmt.Println(os.Getenv(os.Getenv(helperVarEnv)))
		return 0
	case "exit":
		code, err := strconv.Atoi(os.Getenv(helperExitEnv))
		if err != nil {
			fmt.Fprintln(os.Stderr, "helper: bad exit code:", err)
			return 2
		}
		return code
	case "sleep":
		ms, err := strconv.Atoi(os.Getenv(helperSleepEnv))
		if err != nil {
			fmt.Fprintln(os.Stderr, "helper: bad sleep:", err)
			return 2
		}
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return 0
	case "cwd":
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "helper:", err)
			return 2
		}
		fmt.Println(wd)
		return 0
	case "stdin":
		if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
			fmt.Fprintln(os.Stderr, "helper:", err)
			return 2
		}
		return 0
	case "alloc":
		// Commit and touch n MiB. Used by the Windows Job Object tests,
		// where a memory cap must make this fail rather than merely be
		// recorded. Touch every page: Go's allocator gets zeroed pages
		// from the OS, so without the writes nothing would be committed
		// and the cap would never be reached.
		mib, err := strconv.Atoi(os.Getenv(helperAllocEnv))
		if err != nil {
			fmt.Fprintln(os.Stderr, "helper: bad alloc size:", err)
			return 2
		}
		buf := make([]byte, mib<<20)
		for i := 0; i < len(buf); i += 4096 {
			buf[i] = 1
		}
		fmt.Println("allocated")
		return 0
	case "spawn":
		// Try to start a child and report whether the OS allowed it.
		// Used to observe JOB_OBJECT_LIMIT_ACTIVE_PROCESS, which blocks
		// creation rather than killing anything.
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, "helper:", err)
			return 2
		}
		child := exec.Command(exe)
		child.Env = helperEnv(helperModeEnv+"=print", helperTextEnv+"=child")
		if err := child.Run(); err != nil {
			fmt.Println("blocked")
			return 0
		}
		fmt.Println("spawned")
		return 0
	case "spin":
		// Burn CPU for n milliseconds, so a CPU-seconds cap has
		// something to bite on.
		ms, err := strconv.Atoi(os.Getenv(helperSpinEnv))
		if err != nil {
			fmt.Fprintln(os.Stderr, "helper: bad spin duration:", err)
			return 2
		}
		deadline := time.Now().Add(time.Duration(ms) * time.Millisecond)
		var x uint64
		for time.Now().Before(deadline) {
			for i := 0; i < 4096; i++ {
				x = x*6364136223846793005 + 1
			}
		}
		_ = x
		fmt.Println("spun")
		return 0
	default:
		fmt.Fprintln(os.Stderr, "helper: unknown mode", mode)
		return 2
	}
}

// helperBinary is the absolute path to this test binary.
//
// os.Executable rather than os.Args[0]: the latter is not guaranteed to be
// absolute, and TestRun_Dir points the child at a different working directory,
// so a relative argv[0] would resolve against the wrong root.
func helperBinary(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	return exe
}

// helperOptions builds Options that re-enter this binary in the given mode.
//
// The environment is deliberately minimal — the child is a native binary, so
// it needs no PATH — but it always carries the mode plus whatever the test
// under scrutiny adds.
func helperOptions(mode string, extraEnv ...string) Options {
	return Options{Env: helperEnv(append([]string{helperModeEnv + "=" + mode}, extraEnv...)...)}
}

// helperEnv returns the child environment. On Windows SystemRoot is added
// because the OS consults it for the temp directory and the timezone database;
// a process started without it is not guaranteed to work.
func helperEnv(kv ...string) []string {
	env := append([]string(nil), kv...)
	if runtime.GOOS == "windows" {
		env = append(env, "SystemRoot="+os.Getenv("SystemRoot"))
	}
	return env
}

// samePath reports whether two absolute paths name the same directory,
// tolerating case differences (Windows) and symlinked ancestors (macOS
// /var → /private/var).
func samePath(a, b string) bool {
	if strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(ra), filepath.Clean(rb))
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRun_ExitZero(t *testing.T) {
	t.Parallel()

	r := New(WithTimeout(5 * time.Second))
	stdout, stderr, err := r.Run(context.Background(), helperBinary(t), nil,
		helperOptions("print", helperTextEnv+"=hello"))

	require.NoError(t, err)
	assert.Equal(t, "hello\n", stdout.String())
	assert.Empty(t, stderr.String(), "a clean run must not write to stderr")
}

func TestRun_NonZeroExit(t *testing.T) {
	t.Parallel()

	r := New()
	_, _, err := r.Run(context.Background(), helperBinary(t), nil,
		helperOptions("exit", helperExitEnv+"=3"))

	assert.Error(t, err)
	// It's an ExitError carrying the child's own status, not a timeout and
	// not a sandbox failure. 3 rather than 1 so the value cannot be confused
	// with a generic "something went wrong" exit.
	var ee *exec.ExitError
	require.True(t, errors.As(err, &ee), "expected ExitError, got %T", err)
	assert.Equal(t, 3, ee.ExitCode())
	assert.NotErrorIs(t, err, ErrTimeout)
	assert.NotErrorIs(t, err, ErrLimitSetupFailed)
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
	_, _, err := r.Run(context.Background(), helperBinary(t), nil,
		helperOptions("sleep", helperSleepEnv+"=5000"))
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrTimeout)
	assert.Less(t, elapsed, 2*time.Second, "timeout should fire well before the 5s sleep")
	assert.Equal(t, int32(1), atomic.LoadInt32(&timeoutCount))
	assert.Equal(t, int32(0), atomic.LoadInt32(&oomCount))
}

func TestRun_BinaryNotFound(t *testing.T) {
	t.Parallel()

	// A path inside a real (temporary) directory that cannot exist, rather
	// than "/nonexistent/binary": the latter assumes a POSIX root and reads
	// as a Windows UNC-ish path, so it tests something different there.
	missing := filepath.Join(t.TempDir(), "definitely-not-here")

	r := New()
	_, _, err := r.Run(context.Background(), missing, nil, Options{})
	assert.Error(t, err)
}

// TestRun_Dir asserts Options.Dir is the child's working directory.
//
// This used to `t.Skip` on Windows because the original assertion leaned on
// `pwd`'s output format. A skip meant the option went untested on the one
// platform whose path handling differs most, so the child now reports its own
// cwd and the comparison tolerates both canonicalisation forms.
func TestRun_Dir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	opts := helperOptions("cwd")
	opts.Dir = dir

	r := New()
	stdout, stderr, err := r.Run(context.Background(), helperBinary(t), nil, opts)
	require.NoError(t, err, "stderr: %s", stderr.String())

	got := strings.TrimSpace(stdout.String())
	assert.True(t, samePath(got, dir), "cwd = %q, want %q", got, dir)
}

func TestRunExitCode(t *testing.T) {
	t.Parallel()

	r := New()
	_, _, code, err := r.RunExitCode(context.Background(), helperBinary(t), nil,
		helperOptions("exit", helperExitEnv+"=7"))

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

func TestLimits_Describe(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "none", Limits{}.Describe())

	got := Limits{CPUSeconds: 25, MemoryBytes: 1 << 30, OpenFiles: 256}.Describe()
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

	opts := helperOptions("print", helperTextEnv+"=ok")
	opts.Limits = &Limits{}

	r := New()
	stdout, _, err := r.Run(context.Background(), helperBinary(t), nil, opts)
	require.NoError(t, err)
	assert.Equal(t, "ok\n", stdout.String())
}

// TestRun_LimitsAreEnforcedOrRefused is the cross-platform contract:
// asking for a limit must never result in a child that runs without it.
// Either the platform enforces it, or the run is refused outright.
//
// OpenFiles is the field this pins because it is the one no platform but
// POSIX can enforce — Windows Job Objects have no handle-count limit (see
// rlimit_windows.go), so the refusal path stays live there even after
// AUD-24.
func TestRun_LimitsAreEnforcedOrRefused(t *testing.T) {
	t.Parallel()

	opts := helperOptions("print", helperTextEnv+"=ok")
	opts.Limits = &Limits{OpenFiles: 256}

	r := New()
	_, _, err := r.Run(context.Background(), helperBinary(t), nil, opts)

	if runtime.GOOS == "windows" {
		require.Error(t, err, "Windows cannot enforce a handle-count cap; it must refuse")
		assert.ErrorIs(t, err, ErrLimitsUnsupported)
		return
	}

	require.NoError(t, err, "POSIX enforces rlimits in the child; the run must succeed")
}

// TestRun_EnvIsPassedToTheChild covers the env half of what used to be one
// test named TestRun_StdinAndEnv that only ever checked the env.
func TestRun_EnvIsPassedToTheChild(t *testing.T) {
	t.Parallel()

	r := New()
	stdout, stderr, err := r.Run(context.Background(), helperBinary(t), nil,
		helperOptions("printenv", helperVarEnv+"=MY_VAR", "MY_VAR=copilot-test"))

	require.NoError(t, err, "stderr: %s", stderr.String())
	assert.Equal(t, "copilot-test\n", stdout.String(),
		"the child must see a variable that exists only in Options.Env")
}

// TestRun_StdinIsFedToTheChild covers the other half. Options.Stdin had no
// test at all: the old test's name promised it and its body never set it.
func TestRun_StdinIsFedToTheChild(t *testing.T) {
	t.Parallel()

	const payload = "line one\nline two\n"

	opts := helperOptions("stdin")
	opts.Stdin = bytes.NewReader([]byte(payload))

	r := New()
	stdout, stderr, err := r.Run(context.Background(), helperBinary(t), nil, opts)
	require.NoError(t, err, "stderr: %s", stderr.String())
	assert.Equal(t, payload, stdout.String(), "the child must receive Options.Stdin verbatim")
}

// TestRun_StdinDefaultsToEmpty pins the other side of that: with Stdin nil
// the child gets an immediate EOF rather than inheriting the test runner's
// stdin (which would make the tests hang under `go test` without a TTY).
func TestRun_StdinDefaultsToEmpty(t *testing.T) {
	t.Parallel()

	r := New()
	stdout, stderr, err := r.Run(context.Background(), helperBinary(t), nil,
		helperOptions("stdin"))
	require.NoError(t, err, "stderr: %s", stderr.String())
	assert.Empty(t, stdout.String())
}

func TestDefaultTimeout(t *testing.T) {
	t.Parallel()

	r := New()
	// A child that finishes quickly must return quickly even though the
	// default timeout is 5s — the default is a ceiling, not a delay.
	//
	// The bound is 2s rather than 1s because the child is now this test
	// binary re-entering itself (AUD-26), which costs a process start that
	// `sleep 0.1` did not. 2s is still far below the 5s default, so the
	// property under test is unchanged.
	start := time.Now()
	_, stderr, err := r.Run(context.Background(), helperBinary(t), nil,
		helperOptions("sleep", helperSleepEnv+"=50"))
	elapsed := time.Since(start)

	require.NoError(t, err, "stderr: %s", stderr.String())
	assert.Less(t, elapsed, 2*time.Second, "a fast child must not wait out the default timeout")
}

// TestHelperRefusesToRecurseWithoutAMode covers the safety net in TestMain.
//
// A child started with Options.Env == nil inherits this process's environment,
// so it would arrive with suiteGuardEnv set and no helper mode — and would
// re-run the whole suite, spawning another such child, and another. The guard
// turns that fork bomb into a loud exit 97, and this test is what keeps the
// guard itself from being an untested claim.
func TestHelperRefusesToRecurseWithoutAMode(t *testing.T) {
	t.Parallel()

	// Exactly what an inherited environment looks like: the suite marker,
	// no helper mode.
	opts := Options{Env: helperEnv(suiteGuardEnv + "=1")}

	r := New()
	_, stderr, err := r.Run(context.Background(), helperBinary(t), nil, opts)

	require.Error(t, err, "the child must not have run the suite")
	assert.Contains(t, stderr.String(), "refusing to recurse",
		"the refusal must say why; got stderr: %q", stderr.String())

	var ee *exec.ExitError
	require.ErrorAs(t, err, &ee)
	assert.Equal(t, 97, ee.ExitCode())
}

// TestCrossPlatformTestsNameNoHostCommand is the structural guard for AUD-26.
//
// The tests that used to shell out to echo/false/sleep/sh passed on any POSIX
// box and on a Windows machine with Git for Windows on PATH — and failed on a
// bare Windows one. The suite was testing the host's userland as much as the
// runner. They now re-enter this test binary, and this guard keeps them that
// way: no Run/RunExitCode/exec.Command call in the cross-platform test files
// may name a program as a STRING LITERAL.
//
// The rule is "no literal", not "must be helperBinary(t)", because
// TestRun_BinaryNotFound legitimately passes a variable holding a path that
// deliberately does not exist. The invariant is about where command names come
// from, not about which single expression is allowed.
//
// Parsed as an AST rather than matched as text, so the doc comments above —
// which mention `echo`, `sleep` and `sh` by name — cannot fool it. A text
// guard would match those mentions and report a violation in the correct
// state; that is the mistake the AUD-29 gin guard made, see
// .workbuddy-ai/memory/PITFALLS.md §23.
//
// limits_windows_test.go is covered by the same rule. limits_posix_test.go is
// deliberately NOT: it is `//go:build linux || darwin`, where a POSIX userland
// is guaranteed by definition, and TestRun_WrapperPreservesArguments needs a
// real shell program to prove the wrapper does not re-parse its arguments.
func TestCrossPlatformTestsNameNoHostCommand(t *testing.T) {
	t.Parallel()

	for _, fileName := range []string{"runner_test.go", "limits_windows_test.go"} {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, fileName, nil, parser.ParseComments)
		require.NoError(t, err, "%s must parse", fileName)

		checked := 0
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			var nameArg ast.Expr
			switch sel.Sel.Name {
			case "Run", "RunExitCode":
				// Signature: Run(ctx, name, args, opts).
				if len(call.Args) < 2 {
					return true
				}
				checked++
				nameArg = call.Args[1]
			case "Command":
				// exec.Command("echo") is the other way back to the host
				// userland.
				if len(call.Args) == 0 {
					return true
				}
				nameArg = call.Args[0]
			default:
				return true
			}

			if lit, ok := nameArg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				t.Errorf("%s: %s is given the literal program %s; a host program is not "+
					"guaranteed to exist (AUD-26) — re-enter this binary with helperBinary(t) "+
					"instead", fset.Position(call.Pos()), sel.Sel.Name, lit.Value)
			}
			return true
		})

		assert.NotZero(t, checked,
			"%s: the guard found no Run/RunExitCode calls; did the file move?", fileName)
	}
}
