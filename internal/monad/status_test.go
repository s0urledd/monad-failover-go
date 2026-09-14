package monad

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStatusHasDeadline(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "monad-status")
	os.WriteFile(p, []byte("#!/bin/sh\nexec sleep 5\n"), 0700)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	start := time.Now()
	_, _, ok := StatusWithin(50 * time.Millisecond)
	if ok || time.Since(start) > 2*time.Second {
		t.Fatal("status deadline not enforced")
	}
}
