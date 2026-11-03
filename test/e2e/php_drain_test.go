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
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestGoAndPHPDrainOneSpool runs a real tracewick-agent and PHP tracewick-drain
// against one spool directory (claim coexistence; parity with main-repo app-drain
// hand-off). httptest is enough for claim/race smoke — live Querier is covered
// elsewhere. Skips when TRACEWICK_DRAIN_BIN is unset and no local drain is found
// (make php-drain, or sibling ../tracewick checkout).
func TestGoAndPHPDrainOneSpool(t *testing.T) {
	drainBin, ok := phpDrainBinary(t)
	if !ok {
		t.Skipf("PHP drain unset; set TRACEWICK_DRAIN_BIN or run make php-drain")
	}

	agentBin := agentBinary(t)
	spoolDir := filepath.Join(t.TempDir(), "spool")
	const files = 24
	seedSpool(t, spoolDir, fixtureBytes(t, "web-request.spool"), files)

	var posts atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(25 * time.Millisecond)
		posts.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	type result struct {
		name   string
		err    error
		stderr string
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		var stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, agentBin, "run", //nolint:gosec // test binary
			"--spool-dir="+spoolDir,
			"--pyroscope-url="+srv.URL,
			"--reader-id=e2e-go",
			"--once",
			"--sleep=50ms",
		)
		cmd.Stderr = &stderr
		cmd.Stdout = io.Discard
		err := cmd.Run()
		results <- result{name: "go-agent", err: err, stderr: stderr.String()}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		var stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, drainBin, //nolint:gosec // Packagist / sibling binary
			"--spool-dir="+spoolDir,
			"--pyroscope-url="+srv.URL,
			"--reader-id=e2e-php",
			"--once",
		)
		cmd.Stderr = &stderr
		cmd.Stdout = io.Discard
		err := cmd.Run()
		results <- result{name: "php-drain", err: err, stderr: stderr.String()}
	}()

	wg.Wait()
	close(results)

	for res := range results {
		if res.err != nil {
			t.Fatalf("%s: %v\nstderr:\n%s", res.name, res.err, res.stderr)
		}
		t.Logf("%s ok; stderr snippet: %q", res.name, trimLog(res.stderr, 200))
	}

	if left := listSpoolLeftovers(t, spoolDir); len(left) != 0 {
		t.Fatalf("spool leftovers after Go+PHP drain: %v", left)
	}
	got := posts.Load()
	if got < 1 || got > 2 {
		t.Fatalf("pyroscope posts = %d; want 1 or 2", got)
	}
}

func phpDrainBinary(t *testing.T) (string, bool) {
	t.Helper()
	candidates := []string{}
	if v := os.Getenv("TRACEWICK_DRAIN_BIN"); v != "" {
		candidates = append(candidates, v)
	}
	root := repoRoot(t)
	candidates = append(candidates,
		filepath.Join(root, ".cache", "php-drain", "vendor", "bin", "tracewick-drain"),
		filepath.Join(root, "..", "tracewick", "bin", "tracewick-drain"),
	)
	for _, path := range candidates {
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			continue
		}
		// Resolve absolute path so Command works regardless of cwd.
		abs, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		return abs, true
	}
	return "", false
}
