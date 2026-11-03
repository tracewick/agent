package testhttp_test

import (
	"io"
	"net"
	"net/http"
	"strings"
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

func TestFakeHTTPDefaultStatusAndBody(t *testing.T) {
	t.Parallel()
	srv, err := testhttp.Start([]testhttp.Action{{}}) // Status 0 → 200
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)

	res, err := http.Post(srv.URL()+"/", "text/plain", strings.NewReader("hello-body")) //nolint:noctx // short test
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close() //nolint:errcheck // test
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestFakeHTTPRawResponse(t *testing.T) {
	t.Parallel()
	srv, err := testhttp.Start([]testhttp.Action{{
		Raw: []byte("HTTP/1.1 201 Created\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL() + "/") //nolint:noctx // short test
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close() //nolint:errcheck // test
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode != 201 {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestFakeHTTPCloseBeforeRead(t *testing.T) {
	t.Parallel()
	srv, err := testhttp.Start([]testhttp.Action{{Close: true}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)

	client := &http.Client{Timeout: 2 * time.Second}
	_, err = client.Get(srv.URL() + "/") //nolint:noctx // short test
	if err == nil {
		t.Fatal("expected error when peer closes immediately")
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

func TestFakeHTTPStallThenStatus(t *testing.T) {
	t.Parallel()
	srv, err := testhttp.Start([]testhttp.Action{{
		Stall:  20 * time.Millisecond,
		Status: 204,
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL() + "/") //nolint:noctx // short test
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close() //nolint:errcheck // test
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode != 204 {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestFakeHTTPDrainExtraDial(t *testing.T) {
	t.Parallel()
	srv, err := testhttp.Start([]testhttp.Action{{Status: 200}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL() + "/") //nolint:noctx // short test
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close() //nolint:errcheck // test

	// Second dial hits the drain loop (action list exhausted).
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(srv.URL(), "http://"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
	buf := make([]byte, 64)
	_, _ = conn.Read(buf)
	_ = conn.Close()
}

func TestFakeHTTPClientAbortMidHeaders(t *testing.T) {
	t.Parallel()
	srv, err := testhttp.Start([]testhttp.Action{{Status: 200}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)

	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(srv.URL(), "http://"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n"))
	_ = conn.Close() // abort before blank line → readHTTPRequest error path
	time.Sleep(50 * time.Millisecond)
}

func TestFakeHTTPInvalidContentLengthIgnored(t *testing.T) {
	t.Parallel()
	srv, err := testhttp.Start([]testhttp.Action{{Status: 200}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)

	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(srv.URL(), "http://"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck // test
	req := "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: nope\r\n\r\n"
	_, _ = conn.Write([]byte(req))
	buf := make([]byte, 128)
	n, err := conn.Read(buf)
	if err != nil && n == 0 {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(buf[:n]), "200") {
		t.Fatalf("response %q", buf[:n])
	}
}
