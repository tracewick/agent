package metrics_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tracewick/agent/internal/metrics"
)

func TestListenEmptyAndServe(t *testing.T) {
	t.Parallel()
	s, err := metrics.Listen("", metrics.New())
	if err != nil || s != nil {
		t.Fatalf("%v %#v", err, s)
	}
	if _, listenErr := metrics.Listen("127.0.0.1:0", nil); listenErr == nil {
		t.Fatal("nil registry")
	}

	reg := metrics.New()
	reg.AddDelivered(9)
	srv, err := metrics.Listen("127.0.0.1:0", reg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	res, err := http.Get("http://" + srv.Addr() + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close() //nolint:errcheck // test cleanup
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), "profiles_delivered_total 9") {
		t.Fatalf("%s", body)
	}
}
