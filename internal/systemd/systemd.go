// Package systemd drives the three Monad units through systemctl.
package systemd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// Units are the services a migration manages, in the order the tool names them.
var Units = []string{"monad-bft", "monad-execution", "monad-rpc"}

func run(stdout, stderr io.Writer, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", args...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	return cmd.Run()
}

// IsEnabled returns the first line systemctl prints ("masked", "enabled",
// ...) or "" when the query fails.
func IsEnabled(unit string) string {
	var out bytes.Buffer
	_ = run(&out, io.Discard, "is-enabled", unit)
	line, _, _ := strings.Cut(out.String(), "\n")
	return strings.TrimSpace(line)
}

// IsMasked reports whether the unit is masked.
func IsMasked(unit string) bool { return IsEnabled(unit) == "masked" }

// IsActive reports whether the unit is active.
func IsActive(unit string) bool {
	return run(io.Discard, io.Discard, "is-active", "--quiet", unit) == nil
}

// Mask masks all units; the result is verified by the caller per unit.
func Mask(units ...string) {
	_ = run(io.Discard, io.Discard, append([]string{"mask"}, units...)...)
}

// Unmask unmasks one unit; verified by the caller.
func Unmask(unit string) { _ = run(io.Discard, io.Discard, "unmask", unit) }

// Stop stops all units; the caller checks is-active afterwards.
func Stop(stdout io.Writer, units ...string) error {
	return run(stdout, io.Discard, append([]string{"stop"}, units...)...)
}

// Enable enables the units. Failure is ignored: enabling is best effort,
// starting is what gets checked.
func Enable(units ...string) {
	_ = run(io.Discard, io.Discard, append([]string{"enable"}, units...)...)
}

// Start starts the units, passing systemctl's own output through.
func Start(stdout, stderr io.Writer, units ...string) error {
	return run(stdout, stderr, append([]string{"start"}, units...)...)
}

// ActiveState distinguishes a failed query from a confirmed stopped unit.
func ActiveState(unit string) (string, error) {
	var out bytes.Buffer
	if err := run(&out, io.Discard, "show", "--property=ActiveState", "--value", unit); err != nil {
		return "", fmt.Errorf("cannot query %s: %w", unit, err)
	}
	value := strings.TrimSpace(out.String())
	switch value {
	case "active", "inactive", "failed", "activating", "deactivating", "reloading", "refreshing", "maintenance":
		return value, nil
	}
	return "", fmt.Errorf("unknown active state for %s: %q", unit, value)
}

func Masked(unit string) (bool, error) {
	var out bytes.Buffer
	err := run(&out, io.Discard, "is-enabled", unit)
	value := strings.TrimSpace(out.String())
	// Disabled units legitimately return a nonzero status. Missing output or
	// unknown states cannot establish whether unmasking is safe.
	switch value {
	case "masked":
		return true, nil
	case "enabled", "enabled-runtime", "linked", "linked-runtime", "alias", "static", "indirect", "generated", "transient":
		if err == nil {
			return false, nil
		}
	case "disabled":
		return false, nil
	}
	return false, fmt.Errorf("cannot establish persistent mask state for %s (%q): %v", unit, value, err)
}
