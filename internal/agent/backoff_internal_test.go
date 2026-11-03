package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tracewick/agent/internal/config"
)

func TestRunHonorsRetryAfterHint(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "spool", "v2", "edge-cases.spool"))
	if err != nil {
		t.Fatal(err)
	}
	if wErr := os.WriteFile(filepath.Join(dir, "00000000000000000001-aaaaaaaaaaaaaaaa.spool"), body, 0o600); wErr != nil { //nolint:gosec // temp
		t.Fatal(wErr)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{
		SpoolDir:             dir,
		PyroscopeURL:         srv.URL,
		ReaderID:             "backoff-reader",
		AggregationWindowSec: 15,
		Sleep:                5 * time.Second,
		StaleClaim:           10 * time.Minute,
		OwnershipMargin:      time.Minute,
		MaxFilesPerPass:      8,
		MaxBatchInputBytes:   8 << 20,
		HTTPTimeout:          time.Second,
		MaxCardinality:       256,
	}
	a, err := New(&cfg, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	var slept atomic.Int64
	a.sleep = func(_ context.Context, d time.Duration) error {
		slept.Store(int64(d))
		return context.Canceled
	}

	runErr := a.Run(context.Background(), context.Background())
	if runErr == nil {
		t.Fatal("expected cancel from sleep hook")
	}
	got := time.Duration(slept.Load())
	if got != 120*time.Second {
		t.Fatalf("backoff = %s want 120s (Retry-After)", got)
	}
}
