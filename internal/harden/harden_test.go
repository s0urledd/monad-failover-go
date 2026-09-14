package harden

import (
	"os"
	"syscall"
	"testing"
)

func TestApply(t *testing.T) {
	rep := Apply(os.Geteuid())
	if rep.CoreLimit != nil || rep.Dumpable != nil {
		t.Errorf("report: %+v", rep)
	}
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_CORE, &lim); err != nil {
		t.Fatal(err)
	}
	if lim.Cur != 0 || lim.Max != 0 {
		t.Errorf("core limit %+v", lim)
	}
	if Dumpable() != 0 {
		t.Errorf("dumpable = %d", Dumpable())
	}
}
