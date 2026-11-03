//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// logf is t.Logf with a stable prefix so CI -v output is easy to scan.
func logf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Logf("e2e: "+format, args...)
}

// runAgentLogged runs the agent, always logs a one-line outcome + stderr snippet
// on success, and the full combined output on failure.
func runAgentLogged(t *testing.T, bin string, args []string, timeout time.Duration) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // test binary + argv
	logf(t, "exec %s %s (timeout=%s)", bin, strings.Join(args, " "), timeout)
	start := time.Now()
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		logf(t, "FAIL after %s: %v\n--- agent output ---\n%s\n--- end ---", elapsed, err, strings.TrimSpace(string(out)))
		return out, err
	}
	logf(t, "ok after %s; agent: %s", elapsed, trimLog(string(out), 480))
	return out, nil
}

// startAgentLogged starts the agent with stderr captured for later summary.
func startAgentLogged(t *testing.T, bin string, args []string) (*exec.Cmd, *bytes.Buffer, error) {
	t.Helper()
	cmd := exec.Command(bin, args...) //nolint:gosec // test binary
	var stderr bytes.Buffer
	cmd.Stdout = &stderr
	cmd.Stderr = &stderr
	logf(t, "start %s %s", bin, strings.Join(args, " "))
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	logf(t, "pid %d", cmd.Process.Pid)
	return cmd, &stderr, nil
}

func logSpool(t *testing.T, dir, label string) {
	t.Helper()
	left := listSpoolLeftovers(t, dir)
	if len(left) == 0 {
		logf(t, "spool %s: empty (no .spool / claimed)", label)
		return
	}
	logf(t, "spool %s: %d leftover(s): %s", label, len(left), strings.Join(left, ", "))
}

func waitProgress(t *testing.T, started time.Time, every time.Duration, last *time.Time, format string, args ...any) {
	t.Helper()
	if last.IsZero() || time.Since(*last) >= every {
		msg := fmt.Sprintf(format, args...)
		logf(t, "waiting (+%s): %s", time.Since(started).Round(time.Millisecond), msg)
		*last = time.Now()
	}
}
