package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// capture runs f with stdout and stderr redirected and returns what was written.
func capture(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	done := make(chan string)
	go func() {
		var b bytes.Buffer
		io.Copy(&b, r)
		done <- b.String()
	}()
	f()
	w.Close()
	return <-done
}

func TestFlags(t *testing.T) {
	cases := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"--version"}, 0, "monad-failover v" + Version},
		{[]string{"--help"}, 0, "Usage:"},
		{[]string{"-h"}, 0, "--dry-run"},
		{[]string{"help"}, 0, "--public-ip"},
		{[]string{"--bogus"}, 1, "Unknown argument: --bogus. Run with --help for usage."},
		{[]string{"--backup-dir"}, 1, "--backup-dir requires a value"},
		{[]string{"--public-ip", "999.1.1.1"}, 1, "--public-ip must be a valid IPv4 address"},
		{[]string{"--public-ip"}, 1, "--public-ip must be a valid IPv4 address"},
		{[]string{"--beneficiary", "0xabc"}, 1, "--beneficiary must be a 0x-prefixed 40-hex-character address"},
		{[]string{"--beneficiary"}, 1, "--beneficiary must be a 0x-prefixed 40-hex-character address"},
		{[]string{"--node-name", "bad name"}, 1, "--node-name may contain only letters, digits, dot, dash, underscore (max 64)"},
		{[]string{"--node-name", ""}, 1, "--node-name may contain only letters, digits, dot, dash, underscore (max 64)"},
		{[]string{"--seq", "0"}, 1, "--seq must be a positive number without a leading zero"},
		{[]string{"--seq", "007"}, 1, "--seq must be a positive number without a leading zero"},
		{[]string{"--seq", "99999999999999999"}, 1, "--seq must be a positive number without a leading zero"},
		{[]string{"--seq"}, 1, "--seq must be a positive number without a leading zero"},
		{[]string{"help"}, 0, "--beneficiary"},
	}
	for _, tc := range cases {
		var code int
		out := capture(t, func() { code = run(append([]string{"monad-failover"}, tc.args...)) })
		if code != tc.code || !strings.Contains(out, tc.want) {
			t.Errorf("%v: exit %d, output %q; want exit %d containing %q", tc.args, code, out, tc.code, tc.want)
		}
	}
}

// A live run refuses test-only overrides outside the sandbox before it
// touches anything; the dry run parses the environment the same way.
func TestTestOnlyOverrideIsRefused(t *testing.T) {
	t.Setenv("MF_ALLOW_NONROOT", "")
	os.Unsetenv("MF_ALLOW_NONROOT")
	t.Setenv("MF_STATE_DIR", "/tmp/x")
	var code int
	out := capture(t, func() { code = run([]string{"monad-failover", "--dry-run"}) })
	if code != 1 || !strings.Contains(out, "MF_STATE_DIR is only honoured by the unprivileged test suite.") {
		t.Errorf("exit %d output %q", code, out)
	}
}

// An operator override in the environment is said before anything runs.
func TestEnvironmentOverrideIsAnnounced(t *testing.T) {
	t.Setenv("MF_ALLOW_NONROOT", "")
	os.Unsetenv("MF_ALLOW_NONROOT")
	t.Setenv("MF_STATE_DIR", "")
	os.Unsetenv("MF_STATE_DIR")
	t.Setenv("MONAD_HOME", t.TempDir())
	out := capture(t, func() { run([]string{"monad-failover", "--dry-run"}) })
	if !strings.Contains(out, "Environment override in effect: MONAD_HOME=") {
		t.Errorf("override not announced:\n%s", out)
	}
}
