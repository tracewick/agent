package pyroscope_test

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tracewick/agent/internal/pyroscope"
	"github.com/tracewick/agent/internal/spool"
	"github.com/tracewick/agent/internal/testhttp"
)

func TestClientPushFakeHTTPStallTimesOut(t *testing.T) {
	t.Parallel()

	srv, err := testhttp.Start([]testhttp.Action{{
		Stall:            3 * time.Second,
		HangUpAfterStall: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)

	client, err := pyroscope.NewClient(&pyroscope.Config{
		ServerURL:  srv.URL(),
		HTTPClient: &http.Client{Timeout: 300 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	pushErr := client.Push(context.Background(), sampleProfile())
	elapsed := time.Since(start)
	if pushErr == nil {
		t.Fatal("expected timeout against stalled peer")
	}
	if elapsed < 250*time.Millisecond || elapsed > 1500*time.Millisecond {
		t.Fatalf("elapsed %v outside timeout budget; err=%v", elapsed, pushErr)
	}
}

func TestClientPushFakeHTTPCloseBeforeRead(t *testing.T) {
	t.Parallel()

	srv, err := testhttp.Start([]testhttp.Action{{Close: true}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)

	client, err := pyroscope.NewClient(&pyroscope.Config{
		ServerURL:  srv.URL(),
		HTTPClient: &http.Client{Timeout: 2 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Push(context.Background(), sampleProfile()); err == nil {
		t.Fatal("expected error when peer closes before reading")
	}
}

func TestClientPushFakeHTTPTruncatedResponse(t *testing.T) {
	t.Parallel()

	srv, err := testhttp.Start([]testhttp.Action{{
		Raw: []byte("HTTP/1.1 200"), // incomplete status line / headers
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)

	client, err := pyroscope.NewClient(&pyroscope.Config{
		ServerURL:  srv.URL(),
		HTTPClient: &http.Client{Timeout: 2 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Push(context.Background(), sampleProfile()); err == nil {
		t.Fatal("expected error on truncated response")
	}
}

func TestClientPushFakeHTTPTLSHandshakeFails(t *testing.T) {
	t.Parallel()

	// Plain TCP peer that closes on accept — HTTPS client cannot complete handshake.
	srv, err := testhttp.Start([]testhttp.Action{{Close: true}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)

	httpsURL := "https://" + strings.TrimPrefix(srv.URL(), "http://")
	client, err := pyroscope.NewClient(&pyroscope.Config{
		ServerURL: httpsURL,
		HTTPClient: &http.Client{
			Timeout: 2 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, //nolint:gosec // test: system roots; handshake must fail
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	pushErr := client.Push(context.Background(), sampleProfile())
	if pushErr == nil {
		t.Fatal("expected TLS handshake failure")
	}
	// net/http wraps the TLS error; require a non-StatusError transport failure.
	var st *pyroscope.StatusError
	if errors.As(pushErr, &st) {
		t.Fatalf("want transport/TLS error, got StatusError %#v", st)
	}
}

func TestClientPushFakeHTTPNothingListens(t *testing.T) {
	t.Parallel()

	srv, err := testhttp.Start(nil)
	if err != nil {
		t.Fatal(err)
	}
	url := srv.URL()
	srv.Close()

	client, err := pyroscope.NewClient(&pyroscope.Config{
		ServerURL:  url,
		HTTPClient: &http.Client{Timeout: time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Push(context.Background(), sampleProfile()); err == nil {
		t.Fatal("expected connect error against closed peer")
	}
}

func sampleProfile() *pyroscope.Profile {
	return &pyroscope.Profile{
		Application: "app",
		EventType:   spool.EventWall,
		RawProfile:  []byte("x"),
		SampleID:    "id",
	}
}
