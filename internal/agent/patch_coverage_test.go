package agent

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tracewick/agent/internal/config"
	"github.com/tracewick/agent/internal/metrics"
	"github.com/tracewick/agent/internal/pprof"
	"github.com/tracewick/agent/internal/spool"
)

func TestDrainNilPushCtxDefaults(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "spool", "v2", "web-request.spool"))
	if err != nil {
		t.Fatal(err)
	}
	if wErr := os.WriteFile(filepath.Join(dir, "00000000000000000001-aaaaaaaaaaaaaaaa.spool"), body, 0o600); wErr != nil { //nolint:gosec // temp
		t.Fatal(wErr)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{
		SpoolDir: dir, PyroscopeURL: srv.URL, ReaderID: "nil-push",
		AggregationWindowSec: 15, StaleClaim: 10 * time.Minute, OwnershipMargin: time.Minute,
		MaxFilesPerPass: 8, MaxBatchInputBytes: 8 << 20, HTTPTimeout: time.Second, MaxCardinality: 256, Once: true,
	}
	a, err := New(&cfg, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	res := a.drainer.Drain(context.Background(), nil)
	if res.Delivered != 1 {
		t.Fatalf("%#v", res)
	}
}

func TestRunNilPushCtxAndUnreadableBacklog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	cfg := config.Config{
		SpoolDir: dir, PyroscopeURL: srv.URL, ReaderID: "unreadable-backlog",
		AggregationWindowSec: 15, Sleep: time.Hour, StaleClaim: 10 * time.Minute, OwnershipMargin: time.Minute,
		MaxFilesPerPass: 4, MaxBatchInputBytes: 1 << 20, HTTPTimeout: time.Second, MaxCardinality: 256, Once: true,
	}
	reg := metrics.New()
	a, err := New(&cfg, nil, nil, reg)
	if err != nil {
		t.Fatal(err)
	}
	if chmodErr := os.Chmod(dir, 0o000); chmodErr != nil { //nolint:gosec // intentional
		t.Fatal(chmodErr)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	runErr := a.Run(context.Background(), nil)
	if runErr == nil {
		t.Fatal("expected failure on unreadable spool")
	}
}

func TestDrainRejectsEmptyApplicationKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fn := "f"
	payload, err := pprof.EncodeCompactStacks(
		[]pprof.Frame{{Function: &fn}},
		[]pprof.Stack{{FrameIDs: []int{0}, Count: 1}},
	)
	if err != nil {
		t.Fatal(err)
	}
	raw := buildSpoolBytes(t, map[string]any{
		"application":  "",
		"event_type":   "wall",
		"period_ns":    10_000_000,
		"start_ns":     1_798_000_000_000_000_000,
		"end_ns":       1_798_000_000_000_000_001,
		"labels":       map[string]string{},
		"format":       spool.FormatCompactStacks,
		"payload_size": len(payload),
	}, payload)
	if wErr := os.WriteFile(filepath.Join(dir, "00000000000000000001-aaaaaaaaaaaaaaaa.spool"), raw, 0o600); wErr != nil { //nolint:gosec // temp
		t.Fatal(wErr)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{
		SpoolDir: dir, PyroscopeURL: srv.URL, ReaderID: "empty-app",
		AggregationWindowSec: 15, StaleClaim: 10 * time.Minute, OwnershipMargin: time.Minute,
		MaxFilesPerPass: 8, MaxBatchInputBytes: 8 << 20, HTTPTimeout: time.Second, MaxCardinality: 256,
	}
	a, err := New(&cfg, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	res := a.drainer.Drain(context.Background(), context.Background())
	if res.Rejected != 1 {
		t.Fatalf("%#v", res)
	}
}

func TestDrainShrinksOversizedMultiFileAggregate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "spool", "v2", "web-request.spool"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"00000000000000000001-aaaaaaaaaaaaaaaa.spool",
		"00000000000000000002-aaaaaaaaaaaaaaaa.spool",
	} {
		if wErr := os.WriteFile(filepath.Join(dir, name), body, 0o600); wErr != nil { //nolint:gosec // temp
			t.Fatal(wErr)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{
		SpoolDir: dir, PyroscopeURL: srv.URL, ReaderID: "oversize-agg",
		AggregationWindowSec: 15, StaleClaim: 10 * time.Minute, OwnershipMargin: time.Minute,
		MaxFilesPerPass: 8, MaxBatchInputBytes: 8 << 20, HTTPTimeout: time.Second, MaxCardinality: 256,
	}
	a, err := New(&cfg, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.drainer.Limits.MaxAggregatedPprofBytes = 1
	res := a.drainer.Drain(context.Background(), context.Background())
	if res.Outcome != OutcomeFailed || res.Failure == "" {
		t.Fatalf("want shrink/fail on oversize multi aggregate, got %#v", res)
	}
}

func buildSpoolBytes(t *testing.T, header map[string]any, payload []byte) []byte {
	t.Helper()
	header["payload_size"] = len(payload)
	raw, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, 0, 9+len(raw)+len(payload))
	out = append(out, "TWSP"...)
	out = append(out, spool.Version2)
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(raw))) //nolint:gosec // test
	out = append(out, lenBuf[:]...)
	out = append(out, raw...)
	out = append(out, payload...)
	return out
}
