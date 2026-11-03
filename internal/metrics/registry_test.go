package metrics_test

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tracewick/agent/internal/metrics"
)

func TestRegistryNilSafe(t *testing.T) {
	t.Parallel()
	var r *metrics.Registry
	r.AddClaimed(1)
	r.AddDelivered(1)
	r.AddRetried(1)
	r.AddRejected(1)
	r.IncInflight()
	r.DecInflight()
	r.ObserveUpload(time.Millisecond)
	r.ObserveBatch(2, 10)
	r.SetAggregationRatio(1.5)
	r.SetSpoolBacklog(1, 2, time.Second)
	if r.Snapshot() != (metrics.Snapshot{}) {
		t.Fatal("nil snapshot")
	}
}

func TestRegistryCountersAndGauges(t *testing.T) {
	t.Parallel()
	r := metrics.New()
	r.AddClaimed(3)
	r.AddDelivered(2)
	r.AddRetried(1)
	r.AddRejected(4)
	r.IncInflight()
	r.IncInflight()
	r.DecInflight()
	r.ObserveBatch(5, 100)
	r.SetAggregationRatio(2.5)
	r.SetSpoolBacklog(7, 70, 3*time.Second)
	r.ObserveUpload(100 * time.Millisecond)
	r.ObserveUpload(200 * time.Millisecond)
	r.SetAggregationRatio(math.NaN()) // clamped to 0

	s := r.Snapshot()
	if s.Claimed != 3 || s.Delivered != 2 || s.Retried != 1 || s.Rejected != 4 {
		t.Fatalf("counters %#v", s)
	}
	if s.Inflight != 1 || s.BatchFiles != 5 || s.BatchBytes != 100 {
		t.Fatalf("gauges %#v", s)
	}
	if s.SpoolFiles != 7 || s.SpoolBytes != 70 || s.SpoolOldestAgeSec != 3 {
		t.Fatalf("backlog %#v", s)
	}
	if s.AggregationRatio != 0 {
		t.Fatalf("nan clamp %#v", s)
	}
	if s.UploadDurationCount != 2 || s.UploadDurationSum < 0.29 || s.UploadDurationSum > 0.31 {
		t.Fatalf("upload %#v", s)
	}
	r.DecInflight()
	r.DecInflight() // floor at 0
	if r.Snapshot().Inflight != 0 {
		t.Fatal("inflight floor")
	}
}

func TestPrometheusExposition(t *testing.T) {
	t.Parallel()
	r := metrics.New()
	r.AddClaimed(1)
	r.AddDelivered(1)
	r.ObserveUpload(time.Second)
	r.SetSpoolBacklog(2, 20, time.Second)
	r.ObserveBatch(2, 20)
	r.SetAggregationRatio(2)
	r.IncInflight()

	srv := httptest.NewServer(r.Handler())
	t.Cleanup(srv.Close)
	res, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close() //nolint:errcheck // test cleanup
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"profiles_claimed_total 1",
		"profiles_delivered_total 1",
		"upload_duration_seconds_sum 1",
		"upload_duration_seconds_count 1",
		"spool_files 2",
		"batch_files 2",
		"aggregation_ratio 2",
		"inflight_uploads 1",
		"# TYPE profiles_claimed_total counter",
		"# TYPE spool_files gauge",
		"# TYPE upload_duration_seconds summary",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/metrics", http.NoBody)
	bad, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = bad.Body.Close()
	if bad.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("post %d", bad.StatusCode)
	}

	head, err := http.NewRequest(http.MethodHead, srv.URL+"/metrics", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	headRes, err := http.DefaultClient.Do(head)
	if err != nil {
		t.Fatal(err)
	}
	_ = headRes.Body.Close()
	if headRes.StatusCode != http.StatusOK {
		t.Fatalf("head %d", headRes.StatusCode)
	}

	r.SetSpoolBacklog(0, 0, -time.Second)
	if r.Snapshot().SpoolOldestAgeSec != 0 {
		t.Fatal("neg age")
	}
	var nilSrv *metrics.Server
	if nilSrv.Addr() != "" || nilSrv.Shutdown(context.Background()) != nil {
		t.Fatal("nil server")
	}
}
