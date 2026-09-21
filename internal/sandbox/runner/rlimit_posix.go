//go:build linux || darwin

// Package runner (rlimit_posix.go) turns the requested resource limits
// into a command line that applies them INSIDE THE CHILD.
//
// Why a shell wrapper instead of setrlimit(2): Go's os/exec exposes no
// pre-exec hook, so the only way to call setrlimit from Go is to call
// it on the CURRENT process — and the current process here is the
// long-running analysis service. Doing that permanently caps the
// daemon (a 25s RLIMIT_CPU eventually SIGXCPU-kills it, a 1 GiB
// RLIMIT_AS makes it OOM, RLIMIT_NOFILE=256 throttles an HTTP server),
// while the child merely inherits the damage. That was the behaviour
// this file used to have; see AUD-11.
//
// The wrapper runs `sh -c '<ulimit …>; exec "$0" "$@"'` instead. The
// shell sets the limits for itself, then execs the target, so the
// limits are in force for exactly the process we want and the parent
// is untouched. Every ulimit is checked: a limit that cannot be set
// aborts the run rather than leaving an unbounded child behind.
package runner

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// configureProcessGroup puts the child in its own session, and therefore
// its own process group, disjoint from the daemon's.
//
// This is not cosmetic. The child runs AI-generated code; if it ever
// needs to be killed as a group (`kill -- -<pgid>`), sharing the
// daemon's pgid would make that kill reach the analysis service itself.
// A separate session also keeps the child off the parent's controlling
// terminal.
//
// Applied unconditionally, including when no limits were requested.
func configureProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}

// prepareArgv rewrites argv to run under a POSIX sh wrapper that
// applies limits, then execs the original command.
//
// When no limits are requested argv is returned unchanged, so the
// common case pays no shell and needs no `sh` on PATH.
//
// unenforced is always false: POSIX can always enforce, and a limit
// that the kernel refuses to grant aborts the child instead.
func (r *Runner) prepareArgv(argv []string, l Limits) ([]string, bool, error) {
	if l.IsZero() {
		return argv, false, nil
	}

	// argv[0] becomes $0 and the rest become $@, which is exactly the
	// shape `exec "$0" "$@"` needs to re-run the original command
	// without any quoting round-trip through the shell.
	wrapped := append([]string{"sh", "-c", limitScript(l), argv[0]}, argv[1:]...)
	return wrapped, false, nil
}

// limitScript renders the sh snippet that applies l and then execs the
// target. Exported behaviour (for tests) is the exact text below.
func limitScript(l Limits) string {
	var b strings.Builder

	// Every ulimit is followed by a failure branch. Silently continuing
	// after a rejected ulimit is the exact failure mode AUD-11 is
	// about: the caller believes the child is capped when it is not.
	emit := func(flag, value, name string) {
		fmt.Fprintf(&b, "ulimit -%s %s || { echo '%s%s' >&2; exit %d; }\n",
			flag, value, limitSetupMarker, name, limitSetupFailedExit)
	}

	if l.CPUSeconds > 0 {
		emit("t", strconv.Itoa(l.CPUSeconds), "RLIMIT_CPU")
	}
	if l.MemoryBytes > 0 {
		// ulimit -v takes KiB. Round UP: 0 means "unlimited" to the
		// shell, so a tiny non-zero request must never truncate to 0.
		emit("v", strconv.FormatInt(toKiB(l.MemoryBytes), 10), "RLIMIT_AS")
	}
	if l.OpenFiles > 0 {
		emit("n", strconv.Itoa(l.OpenFiles), "RLIMIT_NOFILE")
	}
	if l.NumProcs > 0 {
		// `ulimit -u` is NOT portable, verified against the shells this
		// actually runs under:
		//
		//	bash  ✅   busybox ash  ✅   dash (Debian/Ubuntu /bin/sh)  ❌
		//
		// On a shell without it the ulimit fails and the run aborts
		// with ErrLimitSetupFailed. That is noisy, but the alternative —
		// dropping the cap silently — is the exact bug this file exists
		// to prevent. Note the production composition root does not set
		// NumProcs, so this is a landmine rather than a live problem.
		emit("u", strconv.Itoa(l.NumProcs), "RLIMIT_NPROC")
	}
	if l.FileSize > 0 {
		// ulimit -f takes 512-byte blocks, rounded up for the same
		// reason as -v.
		emit("f", strconv.FormatInt(toBlocks512(l.FileSize), 10), "RLIMIT_FSIZE")
	}

	b.WriteString("exec \"$0\" \"$@\"\n")
	return b.String()
}

// toKiB converts a byte count to KiB, rounding up so that a non-zero
// request never becomes 0 (which the shell reads as "unlimited").
func toKiB(bytes int64) int64 {
	return (bytes + 1023) / 1024
}

// toBlocks512 converts a byte count to 512-byte blocks, rounding up.
func toBlocks512(bytes int64) int64 {
	return (bytes + 511) / 512
}
