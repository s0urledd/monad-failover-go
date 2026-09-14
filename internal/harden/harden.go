// Package harden narrows how far a secret held by this process can travel:
// no core dump can carry it, the process is not dumpable by anyone but
// root, and (as root) its memory is never paged out to swap.
package harden

import (
	"fmt"
	"syscall"
)

// Report says which measures did not apply. Each is best effort: a failure
// leaves the run no less safe than before and is reported, not fatal.
type Report struct {
	CoreLimit  error
	Dumpable   error
	MemoryLock error // nil when not attempted (unprivileged)
}

// Problems lists the failures in operator wording.
func (r Report) Problems() []string {
	var out []string
	if r.CoreLimit != nil {
		out = append(out, "core dumps could not be disabled: "+r.CoreLimit.Error())
	}
	if r.Dumpable != nil {
		out = append(out, "the process could not be made non-dumpable: "+r.Dumpable.Error())
	}
	if r.MemoryLock != nil {
		out = append(out, "memory could not be locked against swapping: "+r.MemoryLock.Error())
	}
	return out
}

// Apply sets the process limits and reports what failed.
func Apply(euid int) Report {
	var r Report
	r.CoreLimit = syscall.Setrlimit(syscall.RLIMIT_CORE, &syscall.Rlimit{Cur: 0, Max: 0})
	if _, _, e := syscall.RawSyscall(syscall.SYS_PRCTL, syscall.PR_SET_DUMPABLE, 0, 0); e != 0 {
		r.Dumpable = fmt.Errorf("prctl: %v", e)
	}
	if euid == 0 {
		// MCL_FUTURE pins every later mapping too. Only as root: an
		// unprivileged process could hit RLIMIT_MEMLOCK on a later
		// allocation and fail there instead of here.
		r.MemoryLock = syscall.Mlockall(syscall.MCL_CURRENT | syscall.MCL_FUTURE)
	}
	return r
}

// Dumpable reports the process's dumpable flag, for tests.
func Dumpable() int {
	v, _, _ := syscall.RawSyscall(syscall.SYS_PRCTL, syscall.PR_GET_DUMPABLE, 0, 0)
	return int(v)
}
