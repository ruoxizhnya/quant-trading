//go:build windows

package runner

import (
	"fmt"
	"os/exec"
)

// configureProcessGroup is a no-op on Windows.
//
// CREATE_NEW_PROCESS_GROUP would be the analogue, but it also changes
// how Ctrl+C and console signals are delivered to the child, and
// nothing here needs group-kill semantics yet. Left alone deliberately
// rather than guessed at.
func configureProcessGroup(cmd *exec.Cmd) {}

// prepareArgv fails closed on Windows.
//
// Windows has no setrlimit(2). The equivalent caps live in Job Objects
// (JOB_OBJECT_LIMIT_*), which are not wired up yet — the wall-clock
// timeout is the only bound this runner can actually enforce here.
//
// Returning argv unchanged, as this file used to, silently dropped
// every cap the caller asked for: the production composition root
// requests 1 GiB / 25 CPU-seconds / 256 fds and got none of them while
// the logs said nothing. Callers budget on those caps being real, so
// the honest answer is to refuse.
//
// WithAllowUnenforcedLimits() is the documented opt-out for local
// development; it is opt-in precisely so that the default cannot be
// mistaken for enforcement.
func (r *Runner) prepareArgv(argv []string, l Limits) ([]string, bool, error) {
	if l.IsZero() {
		return argv, false, nil
	}
	if r.allowUnenforcedLimits {
		return argv, true, nil
	}
	return nil, false, fmt.Errorf(
		"%w: requested %s; call WithAllowUnenforcedLimits() to run anyway",
		ErrLimitsUnsupported, l.describe())
}
