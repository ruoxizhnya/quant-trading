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
//     max address space, max open files, max subprocess count, max
//     file size). On POSIX they are applied INSIDE THE CHILD, by
//     wrapping the command in `sh -c 'ulimit …; exec "$0" "$@"'`.
//     The caps therefore belong to the child process and never touch
//     the long-running parent.
//
//     POSIX is not one capability, though. `ulimit -u` (RLIMIT_NPROC)
//     is absent from dash — the /bin/sh on Debian and Ubuntu — so the
//     wrapper probes for each option it is about to use and aborts
//     with ErrLimitsUnsupported rather than reporting a value problem.
//     The production runtime image is Alpine, whose busybox ash has it.
//
//     On Windows there is no in-process equivalent, so the caps are
//     applied by the parent just after Start, through a Job Object
//     (AUD-24). A Job Object cannot express all five — OpenFiles and
//     FileSize have no equivalent anywhere in the API — so a request
//     containing them is REFUSED rather than silently dropped; see
//     ErrLimitsUnsupported and WithAllowUnenforcedLimits.
//
// Threat model — what this sandbox does and does NOT do:
//
//	✅  Wall-clock CPU bound (timeout) — all platforms
//	✅  Memory bound (RLIMIT_AS on POSIX; Job Object PROCESS_MEMORY on
//	    Windows) — all platforms
//	⚠️  CPU-seconds bound (RLIMIT_CPU on POSIX; Job Object PROCESS_TIME
//	    on Windows) — POSIX only as a TIGHT bound. On Windows the limit
//	    is real, but its firing point barely tracks the value: measured
//	    at 5.3-7.2s of user time for limits of 0.1s, 1s and 3s alike.
//	    Treat it as a backstop against a runaway build, not a budget.
//	✅  Subprocess count bound — POSIX only, and only under a shell
//	    that has `ulimit -u` (bash, busybox ash; not dash). On Windows a
//	    Job Object ACTIVE_PROCESS limit does the same job, but counts
//	    THIS JOB's processes rather than the user's, so the same number
//	    means something different.
//	❌  File size / open file count bound on Windows — a Job Object has
//	    no handle-count and no file-size limit; such requests are
//	    refused rather than dropped.
//	✅  Process-group isolation (child in its own session/pgid, so a
//	    group kill can never reach the daemon) — POSIX only. Windows
//	    gets the equivalent property from KILL_ON_JOB_CLOSE: a runaway
//	    child cannot outlive the daemon that started it.
//	❌  Network egress isolation (would need network namespaces / cgroups)
//	❌  Filesystem chroot / bind-mount isolation (would need CAP_SYS_ADMIN)
//	❌  Syscall filtering (would need seccomp-bpf / eBPF)
//
// The ❌ items are out of scope for Sprint 6 P1-11 — the regex
// staticcheck gate (P0-4) is the cheap first filter that catches the
// plain-literal forms of those patterns. It is a regex over source
// text, not a boundary: aliased / dot imports, indirect calls and
// split selectors all walk straight through it, so "clean scan" means
// "no obvious bad literal", never "safe to run" — see
// internal/sandbox/staticcheck's package doc. The ADR-007 §Future
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
	"strings"
	"sync"
	"syscall"
	"time"
)

// ErrTimeout is returned when the child exceeds the configured timeout.
var ErrTimeout = errors.New("runner: child exceeded timeout and was killed")

// ErrLimitsUnsupported is returned when the caller asked for resource
// limits that cannot be enforced here and the runner was not told to
// proceed anyway.
//
// Refusing is deliberate. A sandbox that silently drops the caps it was
// asked for is worse than no sandbox at all, because every caller that
// budgeted on those caps being real (memory headroom, fd budget,
// runaway-build protection) is now wrong without knowing it.
//
// Two different situations reach it, and they are the same thing to a
// caller — the cap is not in force:
//
//   - The platform has no enforcement mechanism at all. Windows, where
//     even MemoryBytes is unenforceable; see rlimit_windows.go.
//   - The POSIX wrapper's shell cannot express the cap. dash — the
//     /bin/sh on Debian and Ubuntu — has no `ulimit -u`, so a NumProcs
//     request dies before the child starts; see rlimit_posix.go.
//
// WithAllowUnenforcedLimits covers only the first. It cannot cover the
// second: that is decided inside the child, after it has already been
// started, and by then there is no parent-side switch left to consult.
// The escape there is to stop asking for the one cap the shell cannot
// express — which is a real option, because a POSIX caller usually wants
// the other four.
var ErrLimitsUnsupported = errors.New("runner: requested resource limits cannot be enforced here")

// ErrLimitSetupFailed is returned when the child could not be started
// under the requested limits — the wrapper's ulimit call was rejected
// by the shell. The child never ran, so this is not a failure of the
// child itself and its exit status is meaningless.
var ErrLimitSetupFailed = errors.New("runner: failed to apply resource limits in the child")

// limitSetupFailedExit is the exit status the POSIX wrapper uses when
// it cannot apply a limit. 125 is the conventional "the command could
// not be invoked" status (used by GNU timeout, xargs, …).
const limitSetupFailedExit = 125

// limitSetupMarker is written to stderr by the POSIX wrapper before it
// exits with limitSetupFailedExit. Run() requires BOTH the exit status
// and this marker before reporting ErrLimitSetupFailed, so a child that
// legitimately exits 125 on its own is not misreported.
const limitSetupMarker = "runner: cannot set "

// limitUnsupportedExit is the exit status the POSIX wrapper uses when
// its shell cannot even express the limit — as opposed to being handed
// a value it rejects.
//
// Kept distinct from limitSetupFailedExit because the two need opposite
// answers. "cannot set RLIMIT_NPROC" reads like a bad number and sends
// the operator hunting for a value that was never the problem; the real
// answer is that their /bin/sh is dash.
const limitUnsupportedExit = 126

// limitUnsupportedMarker is written to stderr before exiting with
// limitUnsupportedExit. As with limitSetupMarker, Run() requires BOTH
// the status and the marker — 126 is a legal exit status for any
// program.
const limitUnsupportedMarker = "runner: shell does not support "

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

// IsZero reports whether the caller asked for no limits at all.
//
// Zero means "leave that limit alone" (see Limits), so an all-zero
// Limits is satisfiable on every platform — including ones with no
// enforcement mechanism. That is what keeps the fail-closed path in
// Run from blocking callers who never asked for limits in the first
// place.
func (l Limits) IsZero() bool {
	return l.CPUSeconds == 0 && l.MemoryBytes == 0 && l.OpenFiles == 0 &&
		l.NumProcs == 0 && l.FileSize == 0
}

// Describe renders the requested limits for error messages, logs and
// the degradation callback.
//
// Exported because a caller that learns "some limit was not enforced"
// needs to name it in its own logs, and re-deriving the same string in
// cmd/analysis would let the two drift.
func (l Limits) Describe() string {
	var parts []string
	if l.CPUSeconds != 0 {
		parts = append(parts, fmt.Sprintf("cpu=%ds", l.CPUSeconds))
	}
	if l.MemoryBytes != 0 {
		parts = append(parts, fmt.Sprintf("mem=%dB", l.MemoryBytes))
	}
	if l.OpenFiles != 0 {
		parts = append(parts, fmt.Sprintf("nofile=%d", l.OpenFiles))
	}
	if l.NumProcs != 0 {
		parts = append(parts, fmt.Sprintf("nproc=%d", l.NumProcs))
	}
	if l.FileSize != 0 {
		parts = append(parts, fmt.Sprintf("fsize=%dB", l.FileSize))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, " ")
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
	// allowUnenforcedLimits lets Run proceed on platforms that cannot
	// enforce the requested limits. Off by default (fail-closed).
	allowUnenforcedLimits bool
	// onTimeout is called once per timeout, with the cmd's argv for logging.
	// Defaults to a no-op. Useful for metrics / alerting.
	onTimeout func(argv []string)
	// onOOM is called once per OOM kill (RLIMIT_AS exceeded).
	onOOM func(argv []string)
	// onUnenforcedLimits is called once per run that proceeds with part
	// or all of the requested limits NOT enforced, and is handed the
	// subset that was dropped. Only reachable when allowUnenforcedLimits
	// is set. Defaults to a no-op.
	onUnenforcedLimits func(argv []string, unenforced Limits)
	// mu protects the three callback fields above.
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

// WithAllowUnenforcedLimits makes Run proceed on a platform that cannot
// enforce the requested limits (currently Windows) instead of returning
// ErrLimitsUnsupported.
//
// This is an explicit opt-out and should stay off outside local
// development. The whole point of the default is that a caller who
// asked for a memory cap must not be handed a process without one
// while believing otherwise.
//
// It does NOT cover a POSIX shell that cannot express one of the caps
// (dash has no `ulimit -u`). That is discovered inside the child, after
// this decision was already made, so a NumProcs request on such a
// system fails closed regardless. See ErrLimitsUnsupported.
func WithAllowUnenforcedLimits() Option {
	return func(r *Runner) { r.allowUnenforcedLimits = true }
}

// WithOnUnenforcedLimits installs a callback fired once per run that
// proceeds with some of its limits NOT enforced. Wire it to a WARN log
// so the degradation is visible; it is only reachable when
// WithAllowUnenforcedLimits is set.
//
// The callback receives the subset that was dropped, because "some
// limit was not applied" is not actionable on its own — an operator
// needs to know WHICH cap is missing. On Windows that is normally
// OpenFiles and FileSize, which a Job Object cannot express; the other
// three are enforced.
func WithOnUnenforcedLimits(fn func(argv []string, unenforced Limits)) Option {
	return func(r *Runner) { r.onUnenforcedLimits = fn }
}

// notifyUnenforced fires the degradation callback, if any.
func (r *Runner) notifyUnenforced(argv []string, unenforced Limits) {
	r.mu.Lock()
	fn := r.onUnenforcedLimits
	r.mu.Unlock()
	if fn != nil {
		fn(argv, unenforced)
	}
}

// Run executes name with the given args, applying the runner's policy
// and the per-run options. The returned bytes.Buffer values hold the
// captured stdout and stderr; the err is one of:
//   - exec.LookPath error (binary not found)
//   - ErrLimitsUnsupported (limits requested, this platform or the
//     wrapper's shell cannot enforce them)
//   - ErrLimitSetupFailed (limits requested, the child's ulimit was rejected,
//     or the Windows Job Object could not be applied to it)
//   - context.DeadlineExceeded wrapped in ErrTimeout (timed out)
//   - the child's non-zero exit error (other failure)
func (r *Runner) Run(ctx context.Context, name string, args []string, opts Options) (stdout, stderr *bytes.Buffer, err error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	limits := r.limits
	if opts.Limits != nil {
		limits = mergeLimits(r.limits, *opts.Limits)
	}

	// Resolve the limits into the command line BEFORE building the
	// process. On POSIX this rewrites argv so the command runs under a
	// `sh -c` wrapper that sets the limits in the child and then execs
	// the target; on Windows it fails closed for the caps a Job Object
	// cannot express.
	//
	// This has to happen before exec.CommandContext, because the wrapper
	// replaces argv[0] and LookPath must resolve the wrapper instead of
	// the original binary.
	argv := append([]string{name}, args...)
	argv, unenforced, err := r.prepareArgv(argv, limits)
	if err != nil {
		return nil, nil, err
	}

	cmd := exec.CommandContext(timeoutCtx, argv[0], argv[1:]...)
	// Put the child in its own session/process group. Applied
	// unconditionally, not only when limits were requested.
	configureProcessGroup(cmd)
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

	// Start and Wait are split rather than using cmd.Run() so that the
	// platform hook below can run while the child exists. On Windows
	// that is the only window there is: a Job Object cannot be handed
	// to a process before it starts (see attachProcessLimits), so the
	// caps are applied immediately after Start.
	if startErr := cmd.Start(); startErr != nil {
		return stdout, stderr, startErr
	}

	// Platform hook. A no-op on POSIX, where the limits are already in
	// force inside the child before it execs the target.
	hookUnenforced, release, attachErr := attachProcessLimits(cmd, limits, r.allowUnenforcedLimits)
	// Must run AFTER Wait: on Windows this closes the Job Object handle,
	// and KILL_ON_JOB_CLOSE would take the child down with it.
	defer release()
	if attachErr != nil {
		// Fail closed. The child is running uncapped, so it must not be
		// left behind.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return stdout, stderr, attachErr
	}
	// mergeLimits is a field-wise union here: it answers "which caps are
	// not in force", from both the pre-start decision (caps this
	// platform cannot express at all) and the post-start one (caps that
	// could not be applied to this child).
	if unenforced = mergeLimits(unenforced, hookUnenforced); !unenforced.IsZero() {
		r.notifyUnenforced(argv, unenforced)
	}

	runErr := cmd.Wait()
	if runErr != nil {
		// The wrapper's shell does not have this ulimit at all. That is
		// a capability gap, not a rejected value, and the caller can act
		// on the difference (drop the cap) whereas "cannot set" invites
		// them to go looking for a bad number.
		if isLimitUnsupportedFailure(runErr, stderr) {
			return stdout, stderr, fmt.Errorf("%w: %s",
				ErrLimitsUnsupported, strings.TrimSpace(stderr.String()))
		}
		// The POSIX wrapper aborts with a marker on stderr when a limit
		// could not be applied. The target never ran, so report the
		// sandbox failure rather than the wrapper's exit status.
		if isLimitSetupFailure(runErr, stderr) {
			return stdout, stderr, fmt.Errorf("%w: %s",
				ErrLimitSetupFailed, strings.TrimSpace(stderr.String()))
		}
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

// isLimitUnsupportedFailure reports whether runErr is the POSIX
// wrapper giving up because its shell does not have the ulimit the
// caller's limits need.
//
// Same two-part test as isLimitSetupFailure, for the same reason: the
// exit status alone is not evidence.
func isLimitUnsupportedFailure(runErr error, stderr *bytes.Buffer) bool {
	var ee *exec.ExitError
	if !errors.As(runErr, &ee) || ee.ExitCode() != limitUnsupportedExit {
		return false
	}
	return stderr != nil && strings.Contains(stderr.String(), limitUnsupportedMarker)
}

// isLimitSetupFailure reports whether runErr is the POSIX wrapper
// giving up because it could not apply a limit.
//
// Both the exit status and the stderr marker must match: a child that
// happens to exit 125 on its own business must not be misreported as a
// sandbox setup failure.
func isLimitSetupFailure(runErr error, stderr *bytes.Buffer) bool {
	var ee *exec.ExitError
	if !errors.As(runErr, &ee) || ee.ExitCode() != limitSetupFailedExit {
		return false
	}
	return stderr != nil && strings.Contains(stderr.String(), limitSetupMarker)
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
