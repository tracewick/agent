//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestTwoAgentsOneSpool starts two real agent processes against one spool dir.
// Rename claim ensures one winner per file; both processes must exit cleanly and
// leave no unacked spool / claimed leftovers.
func TestTwoAgentsOneSpool(t *testing.T) {
	bin := agentBinary(t)
	spoolDir := filepath.Join(t.TempDir(), "spool")
	const files = 24
	seedSpool(t, spoolDir, fixtureBytes(t, "web-request.spool"), files)
	logf(t, "two-agents: spool=%s files=%d (httptest claim race)", spoolDir, files)

	var posts atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow enough that both agents can observe / contend on claims.
		time.Sleep(25 * time.Millisecond)
		posts.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	type result struct {
		reader string
		err    error
		stderr string
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, reader := range []string{"e2e-a", "e2e-b"} {
		wg.Add(1)
		go func(readerID string) {
			defer wg.Done()
			var stderr bytes.Buffer
			cmd := exec.CommandContext(ctx, bin, "run", //nolint:gosec // test binary path
				"--spool-dir="+spoolDir,
				"--pyroscope-url="+srv.URL,
				"--reader-id="+readerID,
				"--once",
				"--sleep=50ms",
			)
			cmd.Stderr = &stderr
			cmd.Stdout = io.Discard
			err := cmd.Run()
			results <- result{reader: readerID, err: err, stderr: stderr.String()}
		}(reader)
	}
	wg.Wait()
	close(results)

	for res := range results {
		if res.err != nil {
			t.Fatalf("agent %s: %v\nstderr:\n%s", res.reader, res.err, res.stderr)
		}
		logf(t, "agent %s ok; %s", res.reader, trimLog(res.stderr, 200))
	}

	logSpool(t, spoolDir, "after two-agents")
	if left := listSpoolLeftovers(t, spoolDir); len(left) != 0 {
		t.Fatalf("spool leftovers after dual drain: %v", left)
	}
	got := posts.Load()
	logf(t, "two-agents: pyroscope POSTs=%d (want 1 or 2)", got)
	if got < 1 || got > 2 {
		// Same aggregation key → one POST if a single agent claimed everything,
		// or two if work split. More than two would mean unexpected double-delivery
		// batches beyond the two readers.
		t.Fatalf("pyroscope posts = %d; want 1 or 2", got)
	}

	// Second pass: empty spool, both --once should still exit 0.
	for _, reader := range []string{"e2e-a2", "e2e-b2"} {
		cmd := exec.CommandContext(ctx, bin, "run", //nolint:gosec // test binary path
			"--spool-dir="+spoolDir,
			"--pyroscope-url="+srv.URL,
			"--reader-id="+reader,
			"--once",
		)
		cmd.Stderr = io.Discard
		cmd.Stdout = io.Discard
		if err := cmd.Run(); err != nil {
			t.Fatalf("empty-spool agent %s: %v", reader, err)
		}
	}
	if _, err := os.Stat(filepath.Join(spoolDir, "rejected")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(spoolDir, "rejected"))
		if len(entries) != 0 {
			t.Fatalf("unexpected rejects: %d", len(entries))
		}
	}
}

func trimLog(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " | ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
