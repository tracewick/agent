package cli_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tracewick/agent/internal/cli"
	"github.com/tracewick/agent/internal/version"
)

func TestRunVersion(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	if err := cli.Run([]string{"version"}, &out, io.Discard); err != nil {
		t.Fatalf("Run(version): %v", err)
	}
	got := strings.TrimSpace(out.String())
	if got != version.String() {
		t.Fatalf("stdout = %q, want %q", got, version.String())
	}
}

func TestRunDefaultAndHelp(t *testing.T) {
	t.Parallel()

	var errBuf bytes.Buffer
	if err := cli.Run(nil, io.Discard, &errBuf); err != nil {
		t.Fatalf("Run(nil): %v", err)
	}
	if !strings.Contains(errBuf.String(), "early development") {
		t.Fatalf("stderr missing early-dev warning: %q", errBuf.String())
	}

	var out bytes.Buffer
	if err := cli.Run([]string{"help"}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "tracewick-agent run") && !strings.Contains(out.String(), "--spool-dir") {
		t.Fatalf("help = %q", out.String())
	}
}

func TestRunAgentOnce(t *testing.T) {
	dir := t.TempDir()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "spool", "v2", "web-request.spool"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "00000000000000000001-aaaaaaaaaaaaaaaa.spool"), body, 0o600); err != nil { //nolint:gosec // temp dir fixture
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var errOut bytes.Buffer
	args := []string{
		"run", "--once", "--reader-id=cli-test",
		"--spool-dir=" + dir, "--pyroscope-url=" + srv.URL,
		"--metrics-listen=127.0.0.1:0",
	}
	if err := cli.RunContext(ctx, args, io.Discard, &errOut); err != nil {
		t.Fatalf("%v\n%s", err, errOut.String())
	}
	if !strings.Contains(errOut.String(), "metrics listening") {
		t.Fatalf("stderr missing metrics bind: %s", errOut.String())
	}
}

func TestRunAgentMetricsListenBad(t *testing.T) {
	t.Parallel()
	err := cli.Run([]string{
		"run", "--once", "--reader-id=x",
		"--spool-dir=" + t.TempDir(),
		"--pyroscope-url=http://127.0.0.1:1",
		"--metrics-listen=not-a-bind",
	}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected listen error")
	}
}

func TestRunAgentBadConfig(t *testing.T) {
	t.Parallel()
	err := cli.Run([]string{"run"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected config error")
	}
}

func TestRunAgentShutdownOnCancel(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	var errOut bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- cli.RunContext(ctx, []string{
			"run", "--reader-id=cli-shutdown",
			"--spool-dir=" + dir,
			"--pyroscope-url=" + srv.URL,
			"--sleep=50ms",
			"--shutdown-grace=20ms",
		}, io.Discard, &errOut)
	}()
	// Let the idle loop start, then cancel like SIGTERM via Notify(parent).
	time.Sleep(80 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("want clean shutdown, got %v\n%s", err, errOut.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not exit after cancel")
	}
	if !strings.Contains(errOut.String(), "shutdown complete") {
		t.Fatalf("stderr missing shutdown complete: %s", errOut.String())
	}
}
