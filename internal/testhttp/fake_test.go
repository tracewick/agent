package testhttp_test

import (
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/tracewick/agent/internal/testhttp"
)

func TestFakeHTTPStatusOK(t *testing.T) {
	t.Parallel()
	srv, err := testhttp.Start([]testhttp.Action{{Status: 204}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)

	res, err := http.Post(srv.URL()+"/ingest", "text/plain", nil) //nolint:noctx // short test
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close() //nolint:errcheck // test
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode != 204 {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestFakeHTTPHangUp(t *testing.T) {
	t.Parallel()
	srv, err := testhttp.Start([]testhttp.Action{{
		Stall:            50 * time.Millisecond,
		HangUpAfterStall: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)

	client := &http.Client{Timeout: 2 * time.Second}
	_, err = client.Post(srv.URL()+"/", "text/plain", nil) //nolint:noctx // short test
	if err == nil {
		t.Fatal("expected error after hang-up")
	}
}
