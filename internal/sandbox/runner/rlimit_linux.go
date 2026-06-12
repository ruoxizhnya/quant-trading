//go:build linux

package runner

import "syscall"

// RLIMIT_NPROC is hardcoded because the constant is not exposed by
// Go's syscall package on Linux (it varies by kernel version but has
// been 6 since 2.6.x). From <sys/resource.h>:
//   #define RLIMIT_NPROC 6
const rlimitNProc = 6

// setNProc sets RLIMIT_NPROC on Linux. The constant isn't in the Go
// stdlib (intentionally — see https://github.com/golang/go/issues/14385)
// so we hardcode the value 6, which has been stable since Linux 2.6.
func setNProc(n int) error {
	return syscall.Setrlimit(rlimitNProc, &syscall.Rlimit{
		Cur: uint64(n),
		Max: uint64(n),
	})
}
