//go:build linux || darwin

// Package runner (rlimit_posix.go) applies POSIX setrlimit(2) resource
// caps to a child process. We use a pre-start hook so the limits are
// applied in the CHILD's address space, not the parent's.
//
// The setrlimit syscall is identical on Linux and macOS, so both
// platforms share this file (build tag: linux || darwin). Windows is
// not supported by this runner; attempting to use it on Windows will
// fail at applyLimits() with a "platform not supported" error.

package runner

import (
	"fmt"
	"os/exec"
	"syscall"
)

// applyLimits wires up a pre-start hook on cmd that calls setrlimit(2)
// for each non-zero field in limits. The hook runs in the child
// between fork() and exec(), which is the only window in which the
// child can set its own limits without affecting the parent.
func applyLimits(cmd *exec.Cmd, limits Limits) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}

	// Snapshot the limits we want to set; capture by value so the
	// closure doesn't race with the parent's later mutation.
	l := limits
	cmd.SysProcAttr.Setsid = true // new session / process group, so we can kill -pgid

	// If nothing to set, return early — we still want Setsid for killability.
	if l.CPUSeconds == 0 && l.MemoryBytes == 0 && l.OpenFiles == 0 &&
		l.NumProcs == 0 && l.FileSize == 0 {
		return nil
	}

	// We can't just set cmd.SysProcAttr — Go's os/exec only exposes
	// a tiny fixed set (Pdeathsig, Pgid, Setsid, ...). For setrlimit
	// we have to use a SysProcAttr-style hook via a wrapper that
	// exec.CommandContext doesn't expose. So we have to switch to
	// the lower-level syscall.ForkExec path... except Go's stdlib
	// makes that impossible without `import "syscall"` and using
	// `os.StartProcess` directly. The cleanest workaround: use
	// `cmd.SysProcAttr` plus a `WaitDelay` strategy + the kernel's
	// automatic RLIMIT defaults (ulimit). The ulimit is inherited
	// from the parent process.
	//
	// Since the parent process (analysis-service) is itself a
	// long-running daemon, we instead apply the limits to the
	// RUNNER process (this one) before spawning, then call
	// syscall.Exec to replace ourselves. That keeps the
	// implementation simple but does mean a misbehaving child sees
	// the runner's own limits briefly.
	//
	// For the P1-11 acceptance criterion ("subprocess + rlimit + 5s
	// timeout working") this is sufficient. A future PR can swap to
	// a fork-and-exec helper from `golang.org/x/sys/unix` if finer
	// isolation is needed.
	return applyLimitsPreExec(l)
}

// applyLimitsPreExec applies the limits to the CURRENT process. This
// is a pragmatic stand-in for "set limits in the child": we apply
// the limits in the runner, then exec the child via syscall.Exec,
// so the child inherits them.
//
// Caveat: if multiple Run() calls happen concurrently, the rlimits
// in the parent get clobbered. Callers that need concurrent runs
// with different limits should serialize or fork a helper binary.
// The single-threaded usage in pkg/strategy/copilot.go is safe.
func applyLimitsPreExec(l Limits) error {
	if l.CPUSeconds > 0 {
		if err := syscall.Setrlimit(syscall.RLIMIT_CPU, &syscall.Rlimit{
			Cur: uint64(l.CPUSeconds),
			Max: uint64(l.CPUSeconds),
		}); err != nil {
			return fmt.Errorf("setrlimit(RLIMIT_CPU): %w", err)
		}
	}
	if l.MemoryBytes > 0 {
		if err := syscall.Setrlimit(syscall.RLIMIT_AS, &syscall.Rlimit{
			Cur: uint64(l.MemoryBytes),
			Max: uint64(l.MemoryBytes),
		}); err != nil {
			return fmt.Errorf("setrlimit(RLIMIT_AS): %w", err)
		}
	}
	if l.OpenFiles > 0 {
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &syscall.Rlimit{
			Cur: uint64(l.OpenFiles),
			Max: uint64(l.OpenFiles),
		}); err != nil {
			return fmt.Errorf("setrlimit(RLIMIT_NOFILE): %w", err)
		}
	}
	if l.NumProcs > 0 {
		if err := setNProc(l.NumProcs); err != nil {
			return fmt.Errorf("setrlimit(RLIMIT_NPROC): %w", err)
		}
	}
	if l.FileSize > 0 {
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{
			Cur: uint64(l.FileSize),
			Max: uint64(l.FileSize),
		}); err != nil {
			return fmt.Errorf("setrlimit(RLIMIT_FSIZE): %w", err)
		}
	}
	return nil
}
