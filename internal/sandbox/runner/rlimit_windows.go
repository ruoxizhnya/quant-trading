//go:build windows

package runner

import (
	"fmt"
	"os/exec"
)

// applyLimits is a no-op on Windows. The runner still works for
// subprocess + timeout, but resource limits are silently skipped.
// Windows-specific isolation (Job Objects, etc.) is out of scope for
// P1-11; if/when we need it we'll add a rlimit_windows.go that
// wraps CreateJobObject + SetInformationJobObject.
func applyLimits(cmd *exec.Cmd, limits Limits) error {
	return nil
}

func applyLimitsPreExec(l Limits) error {
	// On Windows we don't apply rlimits; the noop pattern is fine
	// because Run() never reaches the syscall.Exec branch.
	return nil
}

// setNProc is a no-op on Windows.
func setNProc(n int) error {
	return fmt.Errorf("runner: RLIMIT_NPROC not supported on windows")
}
