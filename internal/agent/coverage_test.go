package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tracewick/agent/internal/aggregate"
	"github.com/tracewick/agent/internal/config"
	"github.com/tracewick/agent/internal/delivery"
	"github.com/tracewick/agent/internal/pyroscope"
	"github.com/tracewick/agent/internal/spool"
)

func TestMaxInt64AndDefaults(t *testing.T) {
	t.Parallel()
	if maxInt64(3, 1) != 3 || maxInt64(1, 4) != 4 {
		t.Fatal("maxInt64")
	}
	d := &Drainer{Log: nil, Now: nil, MaxFiles: 0, MaxBatchInputBytes: 0}
	if d.Log != nil {
		t.Fatal("pre")
	}
	// Drain with empty source still sets defaults via early return on empty claim.
	dir := t.TempDir()
	src, err := spool.NewSource(spool.Config{Directory: dir, ReaderID: "cov"})
	if err != nil {
		t.Fatal(err)
	}
	d.Source = src
	d.Window, _ = aggregate.NewWindow(15)
	d.Ownership, _ = delivery.NewOwnership(0, 0)
	d.Limits = delivery.DefaultLimits()
	client, _ := pyroscope.NewClient(&pyroscope.Config{ServerURL: "http://127.0.0.1:1"})
	d.Client = client
	res := d.Drain(context.Background(), context.Background())
	if res.Outcome != OutcomeEmpty {
		t.Fatalf("%#v", res)
	}
}

func TestDrainer413ShrinkMulti(t *testing.T) {
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
		if wErr := os.WriteFile(filepath.Join(dir, name), body, 0o600); wErr != nil { //nolint:gosec // temp fixture
			t.Fatal(wErr)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusRequestEntityTooLarge)
	}))
	t.Cleanup(srv.Close)
	d := testDrainer(t, dir, srv.URL, nil)
	before := d.MaxBatchInputBytes
	res := d.Drain(context.Background(), context.Background())
	if res.Outcome != OutcomeFailed || d.MaxBatchInputBytes >= before {
		t.Fatalf("%#v bytes=%d", res, d.MaxBatchInputBytes)
	}
}

func TestDrainerOwnershipExhausted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "spool", "v2", "edge-cases.spool"))
	if err != nil {
		t.Fatal(err)
	}
	if wErr := os.WriteFile(filepath.Join(dir, "00000000000000000001-aaaaaaaaaaaaaaaa.spool"), body, 0o600); wErr != nil { //nolint:gosec // temp fixture
		t.Fatal(wErr)
	}
	claimAt := time.Unix(1_000, 0)
	var calls int
	nowFn := func() time.Time {
		calls++
		// First calls cover claim rename epoch + ClaimedAt; later checks see budget exhausted.
		if calls <= 3 {
			return claimAt
		}
		return claimAt.Add(20 * time.Minute)
	}
	src, err := spool.NewSource(spool.Config{Directory: dir, ReaderID: "own", Now: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	client, _ := pyroscope.NewClient(&pyroscope.Config{ServerURL: srv.URL})
	window, _ := aggregate.NewWindow(15)
	own, _ := delivery.NewOwnership(10*time.Minute, time.Minute)
	d := &Drainer{
		Source: src, Client: client, Window: window, Ownership: own, Limits: delivery.DefaultLimits(),
		MaxFiles: 8, MaxBatchInputBytes: 8 << 20, HTTPTimeout: time.Second, Now: nowFn,
	}
	res := d.Drain(context.Background(), context.Background())
	if res.Delivered != 0 || res.Outcome != OutcomeFailed {
		t.Fatalf("%#v", res)
	}
}

func TestAgentPauseOnce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "spool", "v2", "web-request.spool"))
	if err != nil {
		t.Fatal(err)
	}
	if wErr := os.WriteFile(filepath.Join(dir, "00000000000000000001-aaaaaaaaaaaaaaaa.spool"), body, 0o600); wErr != nil { //nolint:gosec // temp fixture
		t.Fatal(wErr)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	cfg := &config.Config{
		SpoolDir: dir, PyroscopeURL: srv.URL, ReaderID: "pause",
		AggregationWindowSec: 15, StaleClaim: 10 * time.Minute, OwnershipMargin: time.Minute,
		MaxFilesPerPass: 8, MaxBatchInputBytes: 8 << 20, HTTPTimeout: time.Second, MaxCardinality: 256, Once: true,
	}
	a, err := New(cfg, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Run(context.Background(), context.Background()); err == nil {
		t.Fatal("expected pause failure on --once")
	}
}

func TestClaimBudgetHitsMore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "spool", "v2", "web-request.spool"))
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{
		"00000000000000000001-aaaaaaaaaaaaaaaa.spool",
		"00000000000000000002-aaaaaaaaaaaaaaaa.spool",
	} {
		_ = i
		if wErr := os.WriteFile(filepath.Join(dir, name), body, 0o600); wErr != nil { //nolint:gosec // temp fixture
			t.Fatal(wErr)
		}
	}
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	d := testDrainer(t, dir, srv.URL, nil)
	d.MaxFiles = 1
	res := d.Drain(context.Background(), context.Background())
	if res.Outcome != OutcomeMore || res.Delivered != 1 {
		t.Fatalf("%#v", res)
	}
}

func testDrainer(t *testing.T, dir, url string, now func() time.Time) *Drainer {
	t.Helper()
	if now == nil {
		now = time.Now
	}
	src, err := spool.NewSource(spool.Config{Directory: dir, ReaderID: "test-reader", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	client, err := pyroscope.NewClient(&pyroscope.Config{ServerURL: url})
	if err != nil {
		t.Fatal(err)
	}
	window, err := aggregate.NewWindow(15)
	if err != nil {
		t.Fatal(err)
	}
	own, err := delivery.NewOwnership(10*time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := aggregate.NewCardinalityGate(aggregate.NewMemoryStore(), 256)
	if err != nil {
		t.Fatal(err)
	}
	return &Drainer{
		Source: src, Client: client, Window: window, Gate: gate, Ownership: own,
		Limits: delivery.DefaultLimits(), MaxFiles: 32, MaxBatchInputBytes: 8 << 20,
		HTTPTimeout: 5 * time.Second, Now: now,
	}
}

func writeSpool(t *testing.T, dir, name, fixture string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "spool", "v2", fixture))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil { //nolint:gosec // temp fixture
		t.Fatal(err)
	}
}

func TestDrainerOwnershipBeforeParse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeSpool(t, dir, "00000000000000000001-aaaaaaaaaaaaaaaa.spool", "edge-cases.spool")
	claimAt := time.Unix(2_000, 0)
	var calls int
	nowFn := func() time.Time {
		calls++
		// Claim() takes one Now(); the next call must already be past ownership budget.
		if calls <= 1 {
			return claimAt
		}
		return claimAt.Add(20 * time.Minute)
	}
	src, err := spool.NewSource(spool.Config{Directory: dir, ReaderID: "pre", Now: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	client, _ := pyroscope.NewClient(&pyroscope.Config{ServerURL: "http://127.0.0.1:1"})
	window, _ := aggregate.NewWindow(15)
	own, _ := delivery.NewOwnership(10*time.Minute, time.Minute)
	d := &Drainer{
		Source: src, Client: client, Window: window, Ownership: own, Limits: delivery.DefaultLimits(),
		MaxFiles: 1, MaxBatchInputBytes: 8 << 20, HTTPTimeout: time.Second, Now: nowFn,
	}
	res := d.Drain(context.Background(), context.Background())
	// MaxFiles hit with nothing parseable → OutcomeMore (backlog may remain).
	if res.Delivered != 0 || res.Outcome != OutcomeMore {
		t.Fatalf("%#v calls=%d", res, calls)
	}
}

func TestDrainerRejectOversizedAndByteBudget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeSpool(t, dir, "00000000000000000001-aaaaaaaaaaaaaaaa.spool", "web-request.spool")
	writeSpool(t, dir, "00000000000000000002-aaaaaaaaaaaaaaaa.spool", "web-request.spool")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := testDrainer(t, dir, srv.URL, nil)
	d.Limits.MaxDecodedBytes = 100 // below fixture size → reject before parse
	res := d.Drain(context.Background(), context.Background())
	if res.Rejected < 1 {
		t.Fatalf("oversized: %#v", res)
	}

	dir2 := t.TempDir()
	writeSpool(t, dir2, "00000000000000000001-aaaaaaaaaaaaaaaa.spool", "web-request.spool")
	writeSpool(t, dir2, "00000000000000000002-aaaaaaaaaaaaaaaa.spool", "web-request.spool")
	d2 := testDrainer(t, dir2, srv.URL, nil)
	d2.MaxBatchInputBytes = 1200 // first file ~1114 fits; second trips byte budget
	res = d2.Drain(context.Background(), context.Background())
	if res.Outcome != OutcomeMore || res.Delivered != 1 {
		t.Fatalf("byte budget: %#v", res)
	}
}

func TestDrainerRejectHTTP400AndPprofCaps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeSpool(t, dir, "00000000000000000001-aaaaaaaaaaaaaaaa.spool", "web-request.spool")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	d := testDrainer(t, dir, srv.URL, nil)
	res := d.Drain(context.Background(), context.Background())
	if res.Rejected != 1 || res.Outcome != OutcomeEmpty {
		t.Fatalf("400: %#v", res)
	}

	dir2 := t.TempDir()
	writeSpool(t, dir2, "00000000000000000001-aaaaaaaaaaaaaaaa.spool", "edge-cases.spool")
	d2 := testDrainer(t, dir2, srv.URL, nil)
	d2.Limits.MaxStacks = 100_001 // Merger / NewBoundedBuilder rejects
	res = d2.Drain(context.Background(), context.Background())
	if res.Rejected != 1 {
		t.Fatalf("merge cap: %#v", res)
	}

	dir3 := t.TempDir()
	writeSpool(t, dir3, "00000000000000000001-aaaaaaaaaaaaaaaa.spool", "edge-cases.spool")
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(okSrv.Close)
	d3 := testDrainer(t, dir3, okSrv.URL, nil)
	d3.Limits.MaxAggregatedPprofBytes = 1
	res = d3.Drain(context.Background(), context.Background())
	if res.Rejected != 1 {
		t.Fatalf("agg bytes: %#v", res)
	}
}

func TestDrainerNilWindowAndHelpers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeSpool(t, dir, "00000000000000000001-aaaaaaaaaaaaaaaa.spool", "web-request.spool")
	d := testDrainer(t, dir, "http://127.0.0.1:1", nil)
	d.Window = nil
	res := d.Drain(context.Background(), context.Background())
	if res.Outcome != OutcomeFailed {
		t.Fatalf("%#v", res)
	}
	d.releaseAll([]*spool.Claimed{nil})
	held := []*spool.Claimed{{Name: "a"}, {Name: "b"}}
	if len(withoutClaim(held, held[0])) != 1 {
		t.Fatal("withoutClaim")
	}
	if err := sleepCtx(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Hour); err == nil {
		t.Fatal("expected cancel")
	}
}

func TestDrainerAckReleaseFailures(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeSpool(t, dir, "00000000000000000001-aaaaaaaaaaaaaaaa.spool", "web-request.spool")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		// Freeze spool dir so Acknowledge (unlink) fails after a successful push.
		_ = os.Chmod(dir, 0o500) //nolint:gosec // intentional for failure path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o700) //nolint:gosec // restore for cleanup
		srv.Close()
	})
	d := testDrainer(t, dir, srv.URL, nil)
	res := d.Drain(context.Background(), context.Background())
	_ = os.Chmod(dir, 0o700) //nolint:gosec // restore
	if res.Delivered != 1 {
		t.Fatalf("%#v", res)
	}

	dir2 := t.TempDir()
	writeSpool(t, dir2, "00000000000000000001-aaaaaaaaaaaaaaaa.spool", "edge-cases.spool")
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = os.Chmod(dir2, 0o500) //nolint:gosec // Release in defer must fail
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(func() {
		_ = os.Chmod(dir2, 0o700) //nolint:gosec // restore
		failSrv.Close()
	})
	d2 := testDrainer(t, dir2, failSrv.URL, nil)
	res = d2.Drain(context.Background(), context.Background())
	_ = os.Chmod(dir2, 0o700) //nolint:gosec // restore
	if res.Outcome != OutcomeFailed {
		t.Fatalf("%#v", res)
	}

	dir3 := t.TempDir()
	writeSpool(t, dir3, "00000000000000000001-aaaaaaaaaaaaaaaa.spool", "web-request.spool")
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ok.Close)
	d3 := testDrainer(t, dir3, ok.URL, nil)
	d3.MaxBatchInputBytes = 1 // first file still claimed; then nbytes >= maxBytes
	res = d3.Drain(context.Background(), context.Background())
	if res.Outcome != OutcomeMore || res.Delivered != 1 {
		t.Fatalf("single-file byte cap: %#v", res)
	}
}

func TestNewValidationErrors(t *testing.T) {
	t.Parallel()
	base := func() config.Config {
		return config.Config{
			SpoolDir: t.TempDir(), PyroscopeURL: "http://127.0.0.1:9", ReaderID: "ok",
			AggregationWindowSec: 15, StaleClaim: 10 * time.Minute, OwnershipMargin: time.Minute,
			MaxFilesPerPass: 8, MaxBatchInputBytes: 1 << 20, HTTPTimeout: time.Second, MaxCardinality: 16,
		}
	}
	cases := []config.Config{}
	{
		c := base()
		c.AggregationWindowSec = 1
		cases = append(cases, c)
	}
	{
		c := base()
		c.StaleClaim = -time.Second
		cases = append(cases, c)
	}
	{
		c := base()
		c.ReaderID = "bad id"
		cases = append(cases, c)
	}
	{
		c := base()
		c.PyroscopeURL = "://bad"
		cases = append(cases, c)
	}
	{
		c := base()
		c.MaxCardinality = 0
		cases = append(cases, c)
	}
	{
		c := base()
		c.TLSCAFile = filepath.Join(t.TempDir(), "missing-ca.pem")
		cases = append(cases, c)
	}
	for i, cfg := range cases {
		cfg := cfg
		var client *http.Client
		if cfg.TLSCAFile == "" {
			client = http.DefaultClient
		}
		if _, err := New(&cfg, nil, client, nil); err == nil {
			t.Fatalf("case %d: want error", i)
		}
	}
}

func TestAgentRunPauseBackoffAndMore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeSpool(t, dir, "00000000000000000001-aaaaaaaaaaaaaaaa.spool", "web-request.spool")
	writeSpool(t, dir, "00000000000000000002-aaaaaaaaaaaaaaaa.spool", "web-request.spool")
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts++
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	cfg := &config.Config{
		SpoolDir: dir, PyroscopeURL: srv.URL, ReaderID: "more",
		AggregationWindowSec: 15, StaleClaim: 10 * time.Minute, OwnershipMargin: time.Minute,
		MaxFilesPerPass: 1, MaxBatchInputBytes: 8 << 20, HTTPTimeout: time.Second, MaxCardinality: 256, Once: true,
	}
	a, err := New(cfg, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if runErr := a.Run(context.Background(), context.Background()); runErr != nil {
		t.Fatal(runErr)
	}
	if posts != 2 {
		t.Fatalf("posts=%d", posts)
	}

	// Non-once: OutcomeMore then idle sleep cancelled.
	dir2 := t.TempDir()
	writeSpool(t, dir2, "00000000000000000001-aaaaaaaaaaaaaaaa.spool", "web-request.spool")
	writeSpool(t, dir2, "00000000000000000002-aaaaaaaaaaaaaaaa.spool", "web-request.spool")
	cfg2 := *cfg
	cfg2.SpoolDir = dir2
	cfg2.Once = false
	cfg2.Sleep = time.Hour
	cfg2.ReaderID = "loop"
	a2, err := New(&cfg2, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	a2.drainer.MaxFiles = 1
	var slept bool
	a2.sleep = func(_ context.Context, _ time.Duration) error {
		slept = true
		return context.Canceled
	}
	if runErr := a2.Run(context.Background(), context.Background()); runErr == nil {
		t.Fatal("expected cancel after idle sleep")
	}
	if !slept {
		t.Fatal("expected idle sleep after draining backlog")
	}

	// Pause then sleep cancel; failed pass with short Sleep raises backoff.
	dir3 := t.TempDir()
	writeSpool(t, dir3, "00000000000000000001-aaaaaaaaaaaaaaaa.spool", "web-request.spool")
	pauseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(pauseSrv.Close)
	cfg3 := *cfg
	cfg3.SpoolDir = dir3
	cfg3.PyroscopeURL = pauseSrv.URL
	cfg3.Once = false
	cfg3.Sleep = 10 * time.Millisecond
	cfg3.ReaderID = "pause2"
	a3, err := New(&cfg3, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Unix(5_000, 0)
	a3.now = func() time.Time { return t0 }
	var pauseSleeps int
	a3.sleep = func(_ context.Context, d time.Duration) error {
		if d < time.Minute {
			t.Fatalf("pause wait too short: %s", d)
		}
		pauseSleeps++
		if pauseSleeps == 1 {
			return nil // cover successful pause sleep → continue
		}
		return context.Canceled
	}
	if runErr := a3.Run(context.Background(), context.Background()); runErr == nil {
		t.Fatal("expected cancel")
	}
	if pauseSleeps < 2 {
		t.Fatalf("pauseSleeps=%d", pauseSleeps)
	}

	ctxDone, cancelDone := context.WithCancel(context.Background())
	cancelDone()
	if runErr := a3.Run(ctxDone, ctxDone); runErr == nil {
		t.Fatal("expected ctx err")
	}

	dir4 := t.TempDir()
	writeSpool(t, dir4, "00000000000000000001-aaaaaaaaaaaaaaaa.spool", "edge-cases.spool")
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(failSrv.Close)
	cfg4 := *cfg
	cfg4.SpoolDir = dir4
	cfg4.PyroscopeURL = failSrv.URL
	cfg4.Once = false
	cfg4.Sleep = 10 * time.Millisecond
	cfg4.ReaderID = "fail2"
	a4, err := New(&cfg4, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	a4.rng = nil // deterministic backoff floor for assertion
	var backoff time.Duration
	a4.sleep = func(_ context.Context, d time.Duration) error {
		backoff = d
		return context.Canceled
	}
	if runErr := a4.Run(context.Background(), context.Background()); runErr == nil {
		t.Fatal("expected cancel")
	}
	if backoff < time.Second {
		t.Fatalf("backoff=%s", backoff)
	}
}
