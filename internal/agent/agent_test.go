package agent_test

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

	"github.com/tracewick/agent/internal/agent"
	"github.com/tracewick/agent/internal/config"
	"github.com/tracewick/agent/internal/metrics"
)

func TestAgentRunOnceDelivers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "spool", "v2", "web-request.spool"))
	if err != nil {
		t.Fatal(err)
	}
	if wErr := os.WriteFile(filepath.Join(dir, "00000000000000000001-aaaaaaaaaaaaaaaa.spool"), body, 0o600); wErr != nil { //nolint:gosec // temp dir fixture
		t.Fatal(wErr)
	}

	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{
		SpoolDir:             dir,
		PyroscopeURL:         srv.URL,
		ReaderID:             "once-reader",
		AggregationWindowSec: 15,
		Sleep:                time.Hour,
		StaleClaim:           10 * time.Minute,
		OwnershipMargin:      time.Minute,
		MaxFilesPerPass:      32,
		MaxBatchInputBytes:   8 << 20,
		HTTPTimeout:          5 * time.Second,
		ShutdownGrace:        time.Second,
		MaxCardinality:       256,
		Once:                 true,
	}
	reg := metrics.New()
	a, err := agent.New(&cfg, nil, nil, reg)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Run(context.Background(), context.Background()); err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 1 {
		t.Fatalf("posts = %d", posts.Load())
	}
	snap := reg.Snapshot()
	if snap.Claimed != 1 || snap.Delivered != 1 || snap.UploadDurationCount != 1 {
		t.Fatalf("metrics %#v", snap)
	}
	if snap.AggregationRatio != 1 || snap.BatchFiles != 1 {
		t.Fatalf("batch/ratio %#v", snap)
	}
	if a.Metrics() != reg {
		t.Fatal("Metrics()")
	}
}

func TestAgentRunCancels(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{
		SpoolDir:             dir,
		PyroscopeURL:         srv.URL,
		ReaderID:             "cancel-reader",
		AggregationWindowSec: 15,
		Sleep:                50 * time.Millisecond,
		StaleClaim:           10 * time.Minute,
		OwnershipMargin:      time.Minute,
		MaxFilesPerPass:      8,
		MaxBatchInputBytes:   1 << 20,
		HTTPTimeout:          time.Second,
		MaxCardinality:       256,
	}
	a, err := agent.New(&cfg, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	err = a.Run(ctx, ctx)
	if err == nil {
		t.Fatal("expected cancel")
	}
}

func TestAgentRunOnceFailsOn5xx(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "spool", "v2", "edge-cases.spool"))
	if err != nil {
		t.Fatal(err)
	}
	if wErr := os.WriteFile(filepath.Join(dir, "00000000000000000001-aaaaaaaaaaaaaaaa.spool"), body, 0o600); wErr != nil { //nolint:gosec // temp dir fixture
		t.Fatal(wErr)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{
		SpoolDir: dir, PyroscopeURL: srv.URL, ReaderID: "fail-reader",
		AggregationWindowSec: 15, StaleClaim: 10 * time.Minute, OwnershipMargin: time.Minute,
		MaxFilesPerPass: 8, MaxBatchInputBytes: 8 << 20, HTTPTimeout: time.Second, MaxCardinality: 256, Once: true,
	}
	a, err := agent.New(&cfg, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Run(context.Background(), context.Background()); err == nil {
		t.Fatal("expected failure")
	}
}

func TestNewNilConfig(t *testing.T) {
	t.Parallel()
	if _, err := agent.New(nil, nil, nil, nil); err == nil {
		t.Fatal("nil config")
	}
}
