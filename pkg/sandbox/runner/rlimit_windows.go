//go:build windows

package runner

import (
	"fmt"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// configureProcessGroup is a no-op on Windows.
//
// CREATE_NEW_PROCESS_GROUP would be the analogue, but it also changes
// how Ctrl+C and console signals are delivered to the child, and
// nothing here needs group-kill semantics yet. Left alone deliberately
// rather than guessed at.
//
// Note that KILL_ON_JOB_CLOSE (set in createJobObject) already covers
// the property configureProcessGroup exists for on POSIX: a runaway
// child cannot outlive the daemon, because the last handle to its Job
// Object is closed when this process dies.
func configureProcessGroup(cmd *exec.Cmd) {}

// unenforceableOnWindows returns the subset of l that a Job Object
// simply cannot express.
//
// Checked field by field against Microsoft's
// JOBOBJECT_BASIC_LIMIT_INFORMATION and
// JOBOBJECT_EXTENDED_LIMIT_INFORMATION documentation. There is no
// handle-count limit and no per-process file-size limit anywhere in
// the Job Object API, so OpenFiles and FileSize have nowhere to go.
// That is a platform gap, not a mapping nobody has written yet — no
// amount of further work here will produce one.
//
// The other three DO map:
//
//	CPUSeconds  -> JOB_OBJECT_LIMIT_PROCESS_TIME   (100ns units)
//	MemoryBytes -> JOB_OBJECT_LIMIT_PROCESS_MEMORY (committed memory)
//	NumProcs    -> JOB_OBJECT_LIMIT_ACTIVE_PROCESS (per job, not per user)
func unenforceableOnWindows(l Limits) Limits {
	var out Limits
	if l.OpenFiles != 0 {
		out.OpenFiles = l.OpenFiles
	}
	if l.FileSize != 0 {
		out.FileSize = l.FileSize
	}
	return out
}

// prepareArgv decides whether the requested limits can be honoured at
// all on this platform, and refuses if they cannot.
//
// argv is returned unchanged: unlike POSIX there is no wrapper to
// insert, because the limits are applied by the parent after Start
// (see attachProcessLimits).
//
// Windows used to return argv unchanged and run the child unbounded —
// the production composition root asks for 1 GiB / 25 CPU-seconds / 256
// fds and got none of them, with nothing in the logs. Callers budget on
// those caps being real, so a cap that cannot be applied is refused.
func (r *Runner) prepareArgv(argv []string, l Limits) ([]string, Limits, error) {
	if l.IsZero() {
		return argv, Limits{}, nil
	}

	unenforced := unenforceableOnWindows(l)
	if unenforced.IsZero() {
		// Everything asked for maps onto a Job Object. No opt-out
		// needed, because nothing is being dropped.
		return argv, Limits{}, nil
	}
	if r.allowUnenforcedLimits {
		return argv, unenforced, nil
	}
	return nil, Limits{}, fmt.Errorf(
		"%w: %s cannot be expressed by a Windows Job Object (it has no handle-count "+
			"or file-size limit); drop it, or call WithAllowUnenforcedLimits() to run "+
			"with it unenforced",
		ErrLimitsUnsupported, unenforced.Describe())
}

// attachProcessLimits creates a Job Object carrying the mappable subset
// of l, assigns the child to it, and returns a release func that closes
// the job handle.
//
// TIMING IS THE HONEST WEAK POINT, and it is structural rather than a
// shortcut. os/exec gives no way to hand a Job Object to a child before
// it starts:
//
//   - syscall.SysProcAttr has no PROC_THREAD_ATTRIBUTE_JOB_LIST field,
//     so STARTUPINFOEX cannot be built.
//   - CREATE_SUSPENDED is not a way around it: syscall.StartProcess
//     closes the main thread handle before it returns
//     (sdk/go1.25.0/src/syscall/exec_windows.go, the deferred
//     CloseHandle(Handle(pi.Thread))), so there is no thread left to
//     resume.
//
// So the child runs for a moment before the caps apply. Two
// consequences, both recorded rather than hidden:
//
//   - A child that finishes inside that window was never capped. There
//     is nothing to report — it is already gone — so it is treated as a
//     success. The window is one OpenProcess plus one
//     AssignProcessToJobObject: microseconds.
//   - A grandchild spawned inside the window is not in the job and is
//     therefore uncapped. The window is far too short to spawn anything
//     in practice, but it is not zero.
//
// If the assignment fails while the child is still running, the child
// is running uncapped; that fails closed unless allowUnenforced is set.
func attachProcessLimits(cmd *exec.Cmd, l Limits, allowUnenforced bool) (Limits, func(), error) {
	unenforced := unenforceableOnWindows(l)
	if l.IsZero() || cmd.Process == nil {
		return unenforced, func() {}, nil
	}

	job, err := createJobObject(l)
	if err != nil {
		return degradeOrFail(unenforced, l, allowUnenforced, err)
	}
	release := func() { _ = windows.CloseHandle(job) }

	ph, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE,
		false, uint32(cmd.Process.Pid))
	if err != nil {
		release()
		return degradeOrFail(unenforced, l, allowUnenforced, fmt.Errorf("OpenProcess: %w", err))
	}
	defer windows.CloseHandle(ph)

	// The child may already be gone. Then there is nothing left to cap,
	// and nothing to report: it ran for microseconds.
	if event, werr := windows.WaitForSingleObject(ph, 0); werr == nil && event == windows.WAIT_OBJECT_0 {
		return unenforced, release, nil
	}

	if err := windows.AssignProcessToJobObject(job, ph); err != nil {
		release()
		return degradeOrFail(unenforced, l, allowUnenforced, fmt.Errorf("AssignProcessToJobObject: %w", err))
	}
	return unenforced, release, nil
}

// degradeOrFail decides what to do when the Job Object could not be
// applied: run uncapped and say so, or refuse.
//
// Note the failure mode this covers. AssignProcessToJobObject fails
// with ERROR_ACCESS_DENIED when the caller is already inside a Job
// Object that does not allow nesting — a hosted CI agent, some terminal
// emulators. The platform can express the caps; this environment will
// not let us apply them. Both answers are defensible and the caller
// chooses: WithAllowUnenforcedLimits means "run anyway, and tell me".
func degradeOrFail(unenforced, l Limits, allowUnenforced bool, cause error) (Limits, func(), error) {
	if allowUnenforced {
		return mergeLimits(unenforced, l), func() {}, nil
	}
	return Limits{}, func() {}, fmt.Errorf(
		"%w: could not apply %s to the child (%v); call WithAllowUnenforcedLimits() to run anyway",
		ErrLimitSetupFailed, l.Describe(), cause)
}

// createJobObject builds a Job Object carrying every limit in l that
// Windows can express.
func createJobObject(l Limits) (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("CreateJobObject: %w", err)
	}

	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	// KILL_ON_JOB_CLOSE is the safety net: if this process dies without
	// closing the handle, the child goes with it, so a runaway build
	// cannot outlive the daemon that started it. It also means release()
	// must not run before the child has been waited on.
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE

	if l.MemoryBytes > 0 {
		info.BasicLimitInformation.LimitFlags |= windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY
		// ProcessMemoryLimit is committed memory, and it is uintptr
		// (pointer-sized) rather than uint64.
		info.ProcessMemoryLimit = uintptr(l.MemoryBytes)
	}
	if l.CPUSeconds > 0 {
		info.BasicLimitInformation.LimitFlags |= windows.JOB_OBJECT_LIMIT_PROCESS_TIME
		// 100-nanosecond units. Unlike POSIX RLIMIT_CPU — which sends
		// SIGXCPU — exceeding this TERMINATES the process, with
		// STATUS_QUOTA_EXCEEDED (0xC0000044).
		//
		// Measured firing point on Windows (AUD-24; the numbers are in
		// docs/TASKS.md): it barely moves with the limit. A 0.1s, 1s and
		// 3s limit all killed an 8s CPU burn at 5.3-7.2s of accumulated
		// user time, while the same burn with no limit ran to completion.
		// So this is a backstop against a runaway build, NOT a hard
		// "at most N CPU-seconds" — do not size a job budget on it.
		info.BasicLimitInformation.PerProcessUserTimeLimit = int64(l.CPUSeconds) * 10_000_000
	}
	if l.NumProcs > 0 {
		info.BasicLimitInformation.LimitFlags |= windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS
		// Semantics differ from POSIX: RLIMIT_NPROC caps the whole
		// user's process count, ActiveProcessLimit caps this job's. A
		// value of N here means "N processes in this job", so a value
		// that would be generous under RLIMIT_NPROC can be tight here.
		info.BasicLimitInformation.ActiveProcessLimit = uint32(l.NumProcs)
	}

	if _, err := windows.SetInformationJobObject(job,
		uint32(windows.JobObjectExtendedLimitInformation),
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return 0, fmt.Errorf("SetInformationJobObject: %w", err)
	}
	return job, nil
}
