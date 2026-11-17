//go:build e2e

package e2e_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestExecutableEnvironmentConfiguration(t *testing.T) {
	bin := agentBinary(t)
	env := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "TRACEWICK_") {
			env = append(env, entry)
		}
	}
	env = append(env,
		"TRACEWICK_SPOOL_DIR="+t.TempDir(), "TRACEWICK_PYROSCOPE_URL=http://127.0.0.1:1",
		"TRACEWICK_READER_ID=env-process", "TRACEWICK_SLEEP=bad", "TRACEWICK_AGGREGATION_WINDOW=10",
		"TRACEWICK_MAX_STACKS=4096", "TRACEWICK_MAX_FRAMES=16384", "TRACEWICK_MAX_STRINGS=32768",
		"TRACEWICK_MAX_HELD_DECODED_BYTES=67108864", "TRACEWICK_MAX_IN_FLIGHT_BYTES=67108864",
		"TRACEWICK_SHUTDOWN_GRACE=0s", "TRACEWICK_MAX_REJECTED_FILES=0",
	)
	for _, tc := range []struct {
		name      string
		args      []string
		wantError bool
		message   string
	}{
		{"invalid env", []string{"run", "--once"}, true, "--sleep"},
		{"CLI overrides invalid env", []string{"run", "--once", "--sleep=0s"}, false, ""},
		{"explicit empty required", []string{"run", "--once", "--sleep=0s", "--spool-dir="}, true, "spool-dir"},
		{"help ignores invalid env", []string{"run", "--help"}, false, "TRACEWICK_SLEEP"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bin, tc.args...)
			cmd.Env = env
			output, err := cmd.CombinedOutput()
			if (err != nil) != tc.wantError || ctx.Err() != nil {
				t.Fatalf("exit: %v timeout: %v output: %s", err, ctx.Err(), output)
			}
			if !strings.Contains(string(output), tc.message) {
				t.Fatalf("expected %q: %s", tc.message, output)
			}
		})
	}
}
