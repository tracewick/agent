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
	"github.com/tracewick/agent/internal/aggregate"
	"github.com/tracewick/agent/internal/delivery"
	"github.com/tracewick/agent/internal/pyroscope"
	"github.com/tracewick/agent/internal/spool"
)

func TestDrainerDeliversAndAcks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	copyFixture(t, dir, "web-request.spool")

	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := newTestDrainer(t, dir, srv.URL, nil)
	res := d.Drain(context.Background())
	if res.Outcome != agent.OutcomeEmpty || res.Delivered != 1 || res.Rejected != 0 {
		t.Fatalf("%#v", res)
	}
	if posts.Load() != 1 {
		t.Fatalf("posts = %d", posts.Load())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("spool not empty: %v", entries)
	}
}

func TestDrainerRejectsCorruptAndPausesOn401(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "00000000000000000001-aaaaaaaaaaaaaaaa.spool"), []byte("not-twsp"), 0o600); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	d := newTestDrainer(t, dir, srv.URL, nil)
	res := d.Drain(context.Background())
	if res.Outcome != agent.OutcomeEmpty || res.Rejected != 1 {
		t.Fatalf("corrupt: %#v", res)
	}

	copyFixture(t, dir, "web-request.spool")
	res = d.Drain(context.Background())
	if res.Outcome != agent.OutcomePaused || res.PauseFor <= 0 {
		t.Fatalf("401: %#v", res)
	}
	// File released back to spool (not rejected).
	entries, _ := os.ReadDir(dir)
	found := false
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".spool" && e.Name()[0] != '.' {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected released spool file, got %v", names(entries))
	}
}

func TestDrainerRetriesReleaseOn5xx(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	copyFixture(t, dir, "edge-cases.spool")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	d := newTestDrainer(t, dir, srv.URL, nil)
	res := d.Drain(context.Background())
	if res.Outcome != agent.OutcomeFailed || res.Delivered != 0 {
		t.Fatalf("%#v", res)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("want released file, got %v", names(entries))
	}
}

func TestOutcomeString(t *testing.T) {
	t.Parallel()
	for _, o := range []agent.Outcome{agent.OutcomeEmpty, agent.OutcomeMore, agent.OutcomeFailed, agent.OutcomePaused, agent.Outcome(9)} {
		if o.String() == "" {
			t.Fatal(o)
		}
	}
}

func TestDrainer413SingleRejects(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	copyFixture(t, dir, "web-request.spool")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
	}))
	t.Cleanup(srv.Close)
	d := newTestDrainer(t, dir, srv.URL, nil)
	res := d.Drain(context.Background())
	if res.Rejected != 1 {
		t.Fatalf("%#v", res)
	}
}

func TestDrainerEmptyAndCancelled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	d := newTestDrainer(t, dir, "http://127.0.0.1:1", nil)
	res := d.Drain(context.Background())
	if res.Outcome != agent.OutcomeEmpty {
		t.Fatalf("%#v", res)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	copyFixture(t, dir, "web-request.spool")
	res = d.Drain(ctx)
	if res.Outcome != agent.OutcomeFailed {
		t.Fatalf("%#v", res)
	}
}

func newTestDrainer(t *testing.T, dir, url string, now func() time.Time) *agent.Drainer {
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
	return &agent.Drainer{
		Source:             src,
		Client:             client,
		Window:             window,
		Gate:               gate,
		Ownership:          own,
		Limits:             delivery.DefaultLimits(),
		MaxFiles:           32,
		MaxBatchInputBytes: 8 << 20,
		HTTPTimeout:        5 * time.Second,
		Now:                now,
	}
}

func copyFixture(t *testing.T, dir, name string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "spool", "v2", name))
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "00000000000000000001-aaaaaaaaaaaaaaaa.spool")
	if err := os.WriteFile(dst, body, 0o600); err != nil { //nolint:gosec // temp dir fixture
		t.Fatal(err)
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
