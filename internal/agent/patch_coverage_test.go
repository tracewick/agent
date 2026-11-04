package agent

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tracewick/agent/internal/aggregate"
	"github.com/tracewick/agent/internal/config"
	"github.com/tracewick/agent/internal/delivery"
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

func TestDeliverGroupBisectAbortsOnFirstHalfFailure(t *testing.T) {
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
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		n := posts.Add(1)
		switch n {
		case 1:
			w.WriteHeader(http.StatusRequestEntityTooLarge) // shrink → bisect
		case 2:
			w.WriteHeader(http.StatusBadGateway) // first half fails → early return
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{
		SpoolDir: dir, PyroscopeURL: srv.URL, ReaderID: "bisect-fail",
		AggregationWindowSec: 15, StaleClaim: 10 * time.Minute, OwnershipMargin: time.Minute,
		MaxFilesPerPass: 8, MaxBatchInputBytes: 8 << 20, HTTPTimeout: time.Second, MaxCardinality: 256,
	}
	a, err := New(&cfg, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	res := a.drainer.Drain(context.Background(), context.Background())
	if res.Outcome != OutcomeFailed {
		t.Fatalf("want fail after bisect first-half error, got %#v", res)
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
	before := a.drainer.MaxBatchInputBytes
	res := a.drainer.Drain(context.Background(), context.Background())
	if a.drainer.MaxBatchInputBytes != before {
		t.Fatalf("encode budget must not permanently shrink MaxBatchInputBytes")
	}
	// Oversized multi-file group is bisected to singles and rejected.
	if res.Rejected < 1 {
		t.Fatalf("want rejects after bisect on oversize multi aggregate, got %#v", res)
	}
}

func TestDrainHeldBudgetAndEmptyPushGroup(t *testing.T) {
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
		SpoolDir: dir, PyroscopeURL: srv.URL, ReaderID: "held-budget",
		AggregationWindowSec: 15, StaleClaim: 10 * time.Minute, OwnershipMargin: time.Minute,
		MaxFilesPerPass: 8, MaxBatchInputBytes: 8 << 20, HTTPTimeout: time.Second, MaxCardinality: 256,
	}
	a, err := New(&cfg, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.drainer.Limits.MaxHeldProfiles = 0 // exercises defaulting to DefaultMaxHeldProfiles
	_ = a.drainer.Drain(context.Background(), context.Background())
	// refill for the capped held-budget pass
	dir2 := t.TempDir()
	for _, name := range []string{
		"00000000000000000001-aaaaaaaaaaaaaaaa.spool",
		"00000000000000000002-aaaaaaaaaaaaaaaa.spool",
	} {
		if wErr := os.WriteFile(filepath.Join(dir2, name), body, 0o600); wErr != nil { //nolint:gosec // temp
			t.Fatal(wErr)
		}
	}
	src2, err := spool.NewSource(spool.Config{Directory: dir2, ReaderID: "held-budget-2", MaxRejectedFiles: 100})
	if err != nil {
		t.Fatal(err)
	}
	a.drainer.Source = src2
	a.drainer.Limits.MaxHeldProfiles = 1
	res := a.drainer.Drain(context.Background(), context.Background())
	if res.Delivered != 1 {
		t.Fatalf("held budget: %#v", res)
	}
	dec := a.drainer.pushGroup(context.Background(), &aggregate.Group{}, nil, nil)
	if dec.Action != delivery.ActionReject {
		t.Fatalf("empty pushGroup: %#v", dec)
	}

	// MaxHeldDecodedBytes: second file released when held byte budget is 1 byte under two files.
	dir3 := t.TempDir()
	for _, name := range []string{
		"00000000000000000001-aaaaaaaaaaaaaaaa.spool",
		"00000000000000000002-aaaaaaaaaaaaaaaa.spool",
	} {
		if wErr := os.WriteFile(filepath.Join(dir3, name), body, 0o600); wErr != nil { //nolint:gosec // temp
			t.Fatal(wErr)
		}
	}
	src3, err := spool.NewSource(spool.Config{Directory: dir3, ReaderID: "held-bytes", MaxRejectedFiles: 100})
	if err != nil {
		t.Fatal(err)
	}
	a.drainer.Source = src3
	a.drainer.Limits.MaxHeldProfiles = 256
	a.drainer.Limits.MaxHeldDecodedBytes = int64(len(body)) // exactly one file
	res = a.drainer.Drain(context.Background(), context.Background())
	if res.Delivered != 1 {
		t.Fatalf("held decoded bytes budget: %#v", res)
	}
}

func TestPushGroupInFlightAndOwnershipBeforePost(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "spool", "v2", "web-request.spool"))
	if err != nil {
		t.Fatal(err)
	}
	if wErr := os.WriteFile(filepath.Join(dir, "00000000000000000001-aaaaaaaaaaaaaaaa.spool"), body, 0o600); wErr != nil { //nolint:gosec // temp
		t.Fatal(wErr)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{
		SpoolDir: dir, PyroscopeURL: srv.URL, ReaderID: "pre-post",
		AggregationWindowSec: 15, StaleClaim: 10 * time.Minute, OwnershipMargin: time.Minute,
		MaxFilesPerPass: 8, MaxBatchInputBytes: 8 << 20, HTTPTimeout: time.Second, MaxCardinality: 256,
	}
	a, err := New(&cfg, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	file, parseErr := spool.Parse(body)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	prof, decErr := pprof.FromSpoolFile(file)
	if decErr != nil {
		t.Fatal(decErr)
	}
	key, keyErr := aggregate.KeyFromFile(file)
	if keyErr != nil {
		t.Fatal(keyErr)
	}
	g := &aggregate.Group{Key: key}

	// Encoded payload over MaxInFlightBytes → reject (single) / shrink (multi).
	a.drainer.Limits.MaxInFlightBytes = 1
	single := a.drainer.pushGroup(context.Background(), g, []*spool.Claimed{{Name: "a", ClaimedAt: time.Now()}}, []*pprof.Profile{prof})
	if single.Action != delivery.ActionReject {
		t.Fatalf("single over inflight: %#v", single)
	}
	multi := a.drainer.pushGroup(context.Background(), g,
		[]*spool.Claimed{{Name: "a"}, {Name: "b"}},
		[]*pprof.Profile{prof, prof},
	)
	if multi.Action != delivery.ActionShrinkBatch {
		t.Fatalf("multi over inflight: %#v", multi)
	}

	// Ownership exhausted at the pre-POST re-check.
	a.drainer.Limits = delivery.DefaultLimits()
	claimedAt := time.Now().Add(-9 * time.Minute)
	a.drainer.HTTPTimeout = 2 * time.Minute // remaining budget < HTTP timeout
	own := a.drainer.pushGroup(context.Background(), g,
		[]*spool.Claimed{{Name: "a", ClaimedAt: claimedAt}},
		[]*pprof.Profile{prof},
	)
	if own.Action != delivery.ActionRetry || own.Reason == "" {
		t.Fatalf("ownership before post: %#v", own)
	}

	// claimBatch defaults MaxInFlightBytes when unset; empty deliverGroup early-return.
	a.drainer.Limits.MaxInFlightBytes = 0
	batch, _, _, claimErr := a.drainer.claimBatch(context.Background(), 1, 8<<20)
	if claimErr != nil {
		t.Fatal(claimErr)
	}
	a.drainer.releaseAll(batch)
	held := []*spool.Claimed{}
	posts := 0
	d, r, p, f := a.drainer.deliverGroup(context.Background(), g, nil, nil, &held, &posts)
	if d != 0 || r != 0 || p != nil || f != nil {
		t.Fatalf("empty deliverGroup: %d %d %#v %#v", d, r, p, f)
	}
	a.drainer.setAggRatio(3, 0)
}

func TestRefreshBacklogDefaultIntervalAndIncomplete(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "00000000000000000001-aaaaaaaaaaaaaaaa.spool"), []byte("x"), 0o600); err != nil { //nolint:gosec // temp
		t.Fatal(err)
	}
	shard := filepath.Join(dir, "ab")
	if err := os.Mkdir(shard, 0o700); err != nil { //nolint:gosec // temp
		t.Fatal(err)
	}
	if err := os.Chmod(shard, 0o000); err != nil { //nolint:gosec // intentional
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(shard, 0o700) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{
		SpoolDir: dir, PyroscopeURL: srv.URL, ReaderID: "backlog-incomplete",
		AggregationWindowSec: 15, StaleClaim: 10 * time.Minute, OwnershipMargin: time.Minute,
		MaxFilesPerPass: 4, MaxBatchInputBytes: 1 << 20, HTTPTimeout: time.Second, MaxCardinality: 256,
	}
	a, err := New(&cfg, nil, nil, metrics.New())
	if err != nil {
		t.Fatal(err)
	}
	a.backlogMinInterval = 0 // force defaultBacklogMinInterval branch
	a.refreshSpoolBacklog(true)
}

func TestClaimBatchRejectsOverInFlightCap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "spool", "v2", "web-request.spool"))
	if err != nil {
		t.Fatal(err)
	}
	name := "00000000000000000001-aaaaaaaaaaaaaaaa.spool"
	if wErr := os.WriteFile(filepath.Join(dir, name), body, 0o600); wErr != nil { //nolint:gosec // temp
		t.Fatal(wErr)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{
		SpoolDir: dir, PyroscopeURL: srv.URL, ReaderID: "inflight-cap",
		AggregationWindowSec: 15, StaleClaim: 10 * time.Minute, OwnershipMargin: time.Minute,
		MaxFilesPerPass: 8, MaxBatchInputBytes: 8 << 20, HTTPTimeout: time.Second, MaxCardinality: 256,
	}
	a, err := New(&cfg, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.drainer.Limits.MaxInFlightBytes = 1 // smaller than fixture → reject at claim
	res := a.drainer.Drain(context.Background(), context.Background())
	if res.Rejected < 1 || res.Delivered != 0 {
		t.Fatalf("want reject over MaxInFlightBytes, got %#v", res)
	}
}

func TestRefreshSpoolBacklogLogsScanIssues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	shard := filepath.Join(dir, "ab")
	if err := os.Mkdir(shard, 0o700); err != nil { //nolint:gosec // temp
		t.Fatal(err)
	}
	if err := os.Chmod(shard, 0o000); err != nil { //nolint:gosec // intentional
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(shard, 0o700) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{
		SpoolDir: dir, PyroscopeURL: srv.URL, ReaderID: "scan-issues",
		AggregationWindowSec: 15, StaleClaim: 10 * time.Minute, OwnershipMargin: time.Minute,
		MaxFilesPerPass: 4, MaxBatchInputBytes: 1 << 20, HTTPTimeout: time.Second, MaxCardinality: 256, Once: true,
	}
	reg := metrics.New()
	a, err := New(&cfg, nil, nil, reg)
	if err != nil {
		t.Fatal(err)
	}
	// Force a scan that notes the locked shard, then log issues (independent of metrics).
	if _, err := a.drainer.Source.Claim(); err != nil {
		t.Fatal(err)
	}
	a.logScanIssues()
	a.refreshSpoolBacklog(true)
	a.refreshSpoolBacklog(false) // throttled path uses QueueLen

	nilAgent := &Agent{log: slog.Default()}
	nilAgent.logScanIssues() // early return when drainer/source is nil
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
