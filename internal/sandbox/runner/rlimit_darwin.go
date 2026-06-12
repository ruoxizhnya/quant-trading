//go:build darwin

package runner

import "errors"

// setNProc is a no-op on macOS — RLIMIT_NPROC is not available in
// Darwin's userland; the equivalent is the kernel's per-uid maxproc
// tunable (kern.maxprocperuid), which is not user-modifiable.
func setNProc(n int) error {
	return errors.New("runner: RLIMIT_NPROC not supported on darwin")
}
