package pyroscope_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tracewick/agent/internal/pyroscope"
	"github.com/tracewick/agent/internal/spool"
)

func TestNewClientDefaultRefusesRedirectFollow(t *testing.T) {
	t.Parallel()
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("login"))
	}))
	t.Cleanup(final.Close)
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	}))
	t.Cleanup(redir.Close)

	client, err := pyroscope.NewClient(&pyroscope.Config{ServerURL: redir.URL})
	if err != nil {
		t.Fatal(err)
	}
	// Push hits redir+Path; server returns 302 — Classify path uses StatusError, not a followed 200.
	err = client.Push(context.Background(), &pyroscope.Profile{
		Application: "blog",
		EventType:   spool.EventWall,
		RawProfile:  []byte("x"),
	})
	var st *pyroscope.StatusError
	if !errors.As(err, &st) || st.StatusCode != http.StatusFound {
		t.Fatalf("want 302 StatusError, got %v", err)
	}
}

func TestClientPushHappyPath(t *testing.T) {
	t.Parallel()

	var gotMethod, gotCT, gotAuth, gotTenant string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		gotAuth = r.Header.Get("Authorization")
		gotTenant = r.Header.Get("X-Scope-OrgID")
		if r.URL.Path != pyroscope.Path {
			t.Errorf("path = %q", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		gotBody = body
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client, err := pyroscope.NewClient(&pyroscope.Config{
		ServerURL:     srv.URL,
		TenantID:      "team-a",
		BasicUser:     "user",
		BasicPassword: "secret",
		NewSampleID:   func() (string, error) { return "fixed-sample-id", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.Endpoint() != srv.URL+pyroscope.Path {
		t.Fatalf("endpoint = %q", client.Endpoint())
	}

	err = client.Push(context.Background(), &pyroscope.Profile{
		Application: "blog",
		EventType:   spool.EventWall,
		Labels:      map[string]string{"http.route": "/x", "span_id": "abc"},
		RawProfile:  []byte("pprof-bytes"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q", gotMethod)
	}
	if gotCT != pyroscope.MediaType {
		t.Fatalf("content-type = %q", gotCT)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:secret"))
	if gotAuth != wantAuth {
		t.Fatalf("auth = %q want %q", gotAuth, wantAuth)
	}
	if gotTenant != "team-a" {
		t.Fatalf("tenant = %q", gotTenant)
	}

	labels, raw, id := decodePush(t, gotBody)
	if labels["__name__"] != "wall" || labels["service_name"] != "blog" || labels["pyroscope_spy"] != "tracewick" {
		t.Fatalf("labels = %#v", labels)
	}
	if labels["http_route"] != "/x" {
		t.Fatalf("http_route = %q", labels["http_route"])
	}
	if _, ok := labels["span_id"]; ok {
		t.Fatal("span_id leaked into series labels")
	}
	if string(raw) != "pprof-bytes" || id != "fixed-sample-id" {
		t.Fatalf("raw/id = %q %q", raw, id)
	}
}

func TestClientPushNon2xx(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("slow down"))
	}))
	t.Cleanup(srv.Close)

	client, err := pyroscope.NewClient(&pyroscope.Config{ServerURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	err = client.Push(context.Background(), &pyroscope.Profile{
		Application: "app",
		EventType:   spool.EventCPU,
		RawProfile:  []byte("x"),
		SampleID:    "id",
	})
	var st *pyroscope.StatusError
	if !errors.As(err, &st) {
		t.Fatalf("want StatusError, got %v", err)
	}
	if st.StatusCode != http.StatusTooManyRequests || st.Body != "slow down" || st.RetryAfter != 7*time.Second {
		t.Fatalf("%#v", st)
	}
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	if d := pyroscope.ParseRetryAfter("12", now); d != 12*time.Second {
		t.Fatal(d)
	}
	if d := pyroscope.ParseRetryAfter("-1", now); d != 0 {
		t.Fatal(d)
	}
	if d := pyroscope.ParseRetryAfter("", now); d != 0 {
		t.Fatal(d)
	}
	if d := pyroscope.ParseRetryAfter("not-a-date", now); d != 0 {
		t.Fatal(d)
	}
	future := now.Add(30 * time.Second).Format(http.TimeFormat)
	if d := pyroscope.ParseRetryAfter(future, now); d < 29*time.Second || d > 31*time.Second {
		t.Fatal(d)
	}
	past := now.Add(-time.Minute).Format(http.TimeFormat)
	if d := pyroscope.ParseRetryAfter(past, now); d != 0 {
		t.Fatal(d)
	}
}

func TestNewClientValidation(t *testing.T) {
	t.Parallel()

	cases := []*pyroscope.Config{
		{},
		{ServerURL: "not-a-url"},
		{ServerURL: "http://pyroscope", BasicUser: "u"},
		{ServerURL: "http://pyroscope", BasicPassword: "p"},
		{ServerURL: "http://pyroscope", BasicUser: "u:bad", BasicPassword: "p"},
		nil,
	}
	for _, cfg := range cases {
		if _, err := pyroscope.NewClient(cfg); err == nil {
			t.Fatalf("expected error for %#v", cfg)
		}
	}
}

func TestClientOmitsOptionalHeaders(t *testing.T) {
	t.Parallel()

	var gotAuth, gotTenant string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotTenant = r.Header.Get("X-Scope-OrgID")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	client, err := pyroscope.NewClient(&pyroscope.Config{ServerURL: srv.URL + "/"})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Push(context.Background(), &pyroscope.Profile{
		Application: "app",
		EventType:   spool.EventWall,
		RawProfile:  []byte("x"),
		SampleID:    "id",
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.Push(context.Background(), nil); err == nil {
		t.Fatal("expected nil profile error")
	}
	if gotAuth != "" || gotTenant != "" {
		t.Fatalf("auth/tenant = %q %q", gotAuth, gotTenant)
	}
}

func TestClientPushTransportAndEncodeErrors(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	url := srv.URL
	srv.Close()

	client, err := pyroscope.NewClient(&pyroscope.Config{ServerURL: url})
	if err != nil {
		t.Fatal(err)
	}
	pushErr := client.Push(context.Background(), &pyroscope.Profile{
		Application: "app",
		EventType:   spool.EventWall,
		RawProfile:  []byte("x"),
		SampleID:    "id",
	})
	if pushErr == nil {
		t.Fatal("expected transport error against closed server")
	}

	badID, err := pyroscope.NewClient(&pyroscope.Config{
		ServerURL:   "http://127.0.0.1:1",
		NewSampleID: func() (string, error) { return "", errors.New("id boom") },
	})
	if err != nil {
		t.Fatal(err)
	}
	idErr := badID.Push(context.Background(), &pyroscope.Profile{
		Application: "app",
		EventType:   spool.EventWall,
		RawProfile:  []byte("x"),
	})
	if idErr == nil || idErr.Error() != "id boom" {
		t.Fatalf("sample id error = %v", idErr)
	}

	encodeFail, err := pyroscope.NewClient(&pyroscope.Config{ServerURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	encErr := encodeFail.Push(context.Background(), &pyroscope.Profile{
		EventType:  spool.EventWall,
		RawProfile: []byte("x"),
		SampleID:   "id",
	})
	if encErr == nil {
		t.Fatal("expected encode error for empty application")
	}
}

func TestClientPushEmptyErrorBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	client, err := pyroscope.NewClient(&pyroscope.Config{ServerURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	err = client.Push(context.Background(), &pyroscope.Profile{
		Application: "app",
		EventType:   spool.EventWall,
		RawProfile:  []byte("x"),
		SampleID:    "id",
	})
	var st *pyroscope.StatusError
	if !errors.As(err, &st) || st.StatusCode != 500 || st.Error() != "pyroscope: push HTTP 500" {
		t.Fatalf("%v %#v", err, st)
	}
}
