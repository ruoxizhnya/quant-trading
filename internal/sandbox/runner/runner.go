// Package runner implements the Sprint 6 P1-11 (ADR-007 Phase 2) process
// isolation sandbox for AI-generated code execution.
//
// The runner wraps a child process with three layers of defense:
//
//  1. Process model: every command runs in a SEPARATE OS PROCESS
//     (exec.CommandContext) so a crash or runaway loop in the child
//     cannot take down the analysis service. The process is started
//     with its own stdin/stdout/stderr pipes so the parent never
//     blocks on the child.
//
//  2. Timeout: a wall-clock timeout (default 5s, configurable) is
//     enforced via context cancellation. When the deadline expires,
//     the child is SIGKILLed and the runner returns ErrTimeout. The
//     5s default matches the Sprint 6 P1-11 acceptance criterion
//     and the typical "compile a 200-line Go file with the stdlib
//     pre-cached" latency budget.
//
//  3. Resource limits: optional rlimit-style caps (max CPU seconds,
//     max RSS, max open files, max subprocess count) are applied via
//     SysProcAttr on POSIX systems. These map directly to the kernel's
//     setrlimit(2) interface; a malicious child cannot escape them
//     without root.
//
// Threat model — what this sandbox does and does NOT do:
//
//	✅  Wall-clock CPU bound (timeout)
//	✅  Memory bound (RLIMIT_AS on POSIX)
//	✅  File size / open file count bound
//	✅  Subprocess count bound (RLIMIT_NPROC)
//	✅  Process-group isolation (child in its own pgid, killable as group)
//	❌  Network egress isolation (would need network namespaces / cgroups)
//	❌  Filesystem chroot / bind-mount isolation (would need CAP_SYS_ADMIN)
//	❌  Syscall filtering (would need seccomp-bpf / eBPF)
//
// The ❌ items are out of scope for Sprint 6 P1-11 — the regex
// staticcheck gate (P0-4) is the cheap first filter that catches
// patterns that WOULD lead to those escapes, and the ADR-007 §Future
// Phase 3 work items (gVisor / firecracker / user-mode Linux) are
// reserved for later sprints. This runner is a defense-in-depth layer,
// not a complete isolation primitive.
//
// Usage:
//
//	r := runner.New(runner.WithTimeout(5*time.Second),
//	                 runner.WithLimits(runner.Limits{MemoryBytes: 512 << 20}))
//	stdout, stderr, err := r.Run(ctx, "/usr/bin/go", []string{"build", "..."}, runner.Options{
//	    Dir: "/path/to/workingdir",
//	})
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// ErrTimeout is returned when the child exceeds the configured timeout.
var ErrTimeout = errors.New("runner: child exceeded timeout and was killed")

// Limits captures the resource caps to apply to the child. Zero values
// mean "leave that limit alone" (i.e. don't call setrlimit on it).
type Limits struct {
	// CPU seconds (RLIMIT_CPU). 0 = unlimited.
	CPUSeconds int
	// Address space in bytes (RLIMIT_AS / virtual memory). 0 = unlimited.
	MemoryBytes int64
	// Open file descriptors (RLIMIT_NOFILE). 0 = leave as default.
	OpenFiles int
	// Subprocess count (RLIMIT_NPROC). 0 = leave as default.
	NumProcs int
	// Max file size in bytes (RLIMIT_FSIZE). 0 = leave as default.
	FileSize int64
}

// Options configures a single Run() invocation. Per-run overrides
// (e.g. per-job timeout) live here; per-runner policy lives on Runner.
type Options struct {
	// Dir is the working directory of the child. If empty, the parent's
	// working directory is used.
	Dir string
	// Env is the environment for the child. If nil, the parent env is
	// used. To run with a minimal env, pass an explicit slice.
	Env []string
	// ExtraFiles are extra file descriptors to pass to the child.
	ExtraFiles []*os.File
	// Limits override the runner-level limits for this run. Use this
	// to give a specific job a larger memory budget, etc.
	Limits *Limits
	// Stdin, if non-nil, is fed to the child. If nil, /dev/null is used.
	Stdin *bytes.Reader
}

// DefaultTimeout is the default wall-clock timeout if none is set.
const DefaultTimeout = 5 * time.Second

// Runner is a reusable process-isolation runner. It is safe for
// concurrent use; Run() spawns a fresh process per call.
type Runner struct {
	timeout time.Duration
	limits  Limits
	// onTimeout is called once per timeout, with the cmd's argv for logging.
	// Defaults to a no-op. Useful for metrics / alerting.
	onTimeout func(argv []string)
	// onOOM is called once per OOM kill (RLIMIT_AS exceeded).
	onOOM func(argv []string)
	// mu protects the two callback fields above.
	mu sync.Mutex
}

// New creates a Runner with the given options.
func New(opts ...Option) *Runner {
	r := &Runner{timeout: DefaultTimeout}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Option is a functional option for New().
type Option func(*Runner)

// WithTimeout overrides the default wall-clock timeout.
func WithTimeout(d time.Duration) Option {
	return func(r *Runner) { r.timeout = d }
}

// WithLimits sets the default resource limits applied to every run.
// Per-run Options.Limits can override individual fields.
func WithLimits(l Limits) Option {
	return func(r *Runner) { r.limits = l }
}

// WithOnTimeout installs a callback fired when a child exceeds the timeout.
func WithOnTimeout(fn func(argv []string)) Option {
	return func(r *Runner) { r.onTimeout = fn }
}

// WithOnOOM installs a callback fired when a child is killed for exceeding
// the memory limit (RLIMIT_AS).
func WithOnOOM(fn func(argv []string)) Option {
	return func(r *Runner) { r.onOOM = fn }
}

// Run executes name with the given args, applying the runner's policy
// and the per-run options. The returned bytes.Buffer values hold the
// captured stdout and stderr; the err is one of:
//   - exec.LookPath error (binary not found)
//   - context.DeadlineExceeded wrapped in ErrTimeout (timed out)
//   - the child's non-zero exit error (other failure)
func (r *Runner) Run(ctx context.Context, name string, args []string, opts Options) (stdout, stderr *bytes.Buffer, err error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, name, args...)
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}
	if opts.Env != nil {
		cmd.Env = opts.Env
	}
	if len(opts.ExtraFiles) > 0 {
		cmd.ExtraFiles = opts.ExtraFiles
	}

	stdout = &bytes.Buffer{}
	stderr = &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if opts.Stdin != nil {
		cmd.Stdin = opts.Stdin
	} else {
		cmd.Stdin = bytes.NewReader(nil)
	}

	// Apply resource limits. We do this via a pre-start hook so the
	// limits are set in the CHILD process, not the parent.
	limits := r.limits
	if opts.Limits != nil {
		limits = mergeLimits(r.limits, *opts.Limits)
	}
	if err := applyLimits(cmd, limits); err != nil {
		return nil, nil, fmt.Errorf("runner: apply rlimits: %w", err)
	}

	runErr := cmd.Run()
	if runErr != nil {
		// Distinguish timeout from other failures.
		if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
			r.mu.Lock()
			fn := r.onTimeout
			r.mu.Unlock()
			if fn != nil {
				fn(append([]string{name}, args...))
			}
			return stdout, stderr, fmt.Errorf("%w (after %s): %v", ErrTimeout, r.timeout, runErr)
		}
		// OOM: setrlimit SIGKILL on Linux, signal=9 exit code.
		if isOOM(runErr) {
			r.mu.Lock()
			fn := r.onOOM
			r.mu.Unlock()
			if fn != nil {
				fn(append([]string{name}, args...))
			}
		}
	}
	return stdout, stderr, runErr
}

// RunExitCode is a convenience wrapper that returns the child's exit
// code (or -1 if it didn't run at all). Useful for handlers that need
// to distinguish "build failed (1)" from "build timed out (-1)".
func (r *Runner) RunExitCode(ctx context.Context, name string, args []string, opts Options) (stdout, stderr *bytes.Buffer, exitCode int, err error) {
	stdout, stderr, err = r.Run(ctx, name, args, opts)
	if err == nil {
		return stdout, stderr, 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return stdout, stderr, ee.ExitCode(), err
	}
	return stdout, stderr, -1, err
}

// mergeLimits returns a new Limits where each zero field in `over` is
// replaced by the corresponding field in `base`. Non-zero fields in
// `over` take precedence (per-run overrides).
func mergeLimits(base, over Limits) Limits {
	out := base
	if over.CPUSeconds != 0 {
		out.CPUSeconds = over.CPUSeconds
	}
	if over.MemoryBytes != 0 {
		out.MemoryBytes = over.MemoryBytes
	}
	if over.OpenFiles != 0 {
		out.OpenFiles = over.OpenFiles
	}
	if over.NumProcs != 0 {
		out.NumProcs = over.NumProcs
	}
	if over.FileSize != 0 {
		out.FileSize = over.FileSize
	}
	return out
}

// isOOM reports whether the process was killed by SIGKILL (signal 9)
// after exceeding RLIMIT_AS. We can't tell directly from Go, so we
// use heuristics: the process exited with signal SIGKILL or
// SIGSEGV/SIGBUS in a way consistent with an OOM kill.
func isOOM(err error) bool {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return false
	}
	// On Linux, the OOM killer sends SIGKILL (signal 9). On macOS,
	// Jetsam sends SIGKILL as well. We treat any SIGKILL exit as a
	// possible OOM and let the caller's onOOM callback decide.
	if status, ok := ee.Sys().(syscall.WaitStatus); ok {
		return status.Signaled() && status.Signal() == syscall.SIGKILL
	}
	return false
}
