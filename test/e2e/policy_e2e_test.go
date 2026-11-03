//go:build e2e

package e2e_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAuthPauseKeepsSpool: HTTP 401 must not ack or reject — file returns to the
// spool (agent policy §2.8; unlike PHP drain reject-on-401).
func TestAuthPauseKeepsSpool(t *testing.T) {
	bin := agentBinary(t)
	spoolDir := filepath.Join(t.TempDir(), "spool")
	seedSpool(t, spoolDir, fixtureBytes(t, "web-request.spool"), 1)
	logf(t, "auth-pause: seeded 1 spool file → expect 401 keeps it")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	logf(t, "auth-pause: httptest %s returns 401", srv.URL)

	out, err := runAgentLogged(t, bin, []string{
		"run",
		"--spool-dir=" + spoolDir,
		"--pyroscope-url=" + srv.URL,
		"--reader-id=e2e-auth",
		"--once",
		"--sleep=50ms",
	}, time.Minute)
	if err == nil {
		t.Fatalf("expected --once failure on 401; out:\n%s", out)
	}
	logSpool(t, spoolDir, "after 401")
	left := listSpoolLeftovers(t, spoolDir)
	if len(left) == 0 {
		t.Fatalf("expected spool leftover after 401 pause")
	}
	if _, err := os.Stat(filepath.Join(spoolDir, "rejected")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(spoolDir, "rejected"))
		if len(entries) != 0 {
			t.Fatalf("rejected/ must stay empty on 401, got %v", namesOf(entries))
		}
	}
	logf(t, "auth-pause OK: leftovers=%v (not rejected)", left)
}

// TestFixtureRejectAndDeliver mirrors SpoolDrainerFixturesTest at process level:
// valid fixtures are delivered; corrupt ones land in rejected/; spool clears.
func TestFixtureRejectAndDeliver(t *testing.T) {
	bin := agentBinary(t)
	spoolDir := filepath.Join(t.TempDir(), "spool")
	if err := os.MkdirAll(spoolDir, 0o700); err != nil { //nolint:gosec // test
		t.Fatal(err)
	}

	valid := []string{"web-request.spool", "edge-cases.spool", "unknown-header-key.spool"}
	reject := []string{
		"bad-magic.spool",
		"end-before-start.spool",
		"header-not-json.spool",
		"header-too-large.spool",
		"missing-key.spool",
		"truncated-payload.spool",
		"unknown-format.spool",
		"unsupported-version.spool",
		"version-1.spool",
	}
	seq := 1
	for _, name := range append(append([]string{}, valid...), reject...) {
		body := fixtureBytes(t, name)
		dst := filepath.Join(spoolDir, formatSpoolName(seq))
		seq++
		if err := os.WriteFile(dst, body, 0o600); err != nil { //nolint:gosec // test
			t.Fatal(err)
		}
	}
	logf(t, "fixtures: seeded %d valid + %d corrupt into %s", len(valid), len(reject), spoolDir)

	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts++
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	if _, err := runAgentLogged(t, bin, []string{
		"run",
		"--spool-dir=" + spoolDir,
		"--pyroscope-url=" + srv.URL,
		"--reader-id=e2e-fixtures",
		"--once",
	}, 2*time.Minute); err != nil {
		t.Fatalf("agent fixtures: %v", err)
	}
	logSpool(t, spoolDir, "after fixtures drain")
	if left := listSpoolLeftovers(t, spoolDir); len(left) != 0 {
		t.Fatalf("spool leftovers: %v", left)
	}
	if posts < 1 || posts > 3 {
		t.Fatalf("posts=%d want 1..3", posts)
	}
	rejDir := filepath.Join(spoolDir, "rejected")
	entries, err := os.ReadDir(rejDir)
	if err != nil {
		t.Fatalf("rejected/: %v", err)
	}
	if len(entries) != len(reject) {
		t.Fatalf("rejected count=%d want %d (%v)", len(entries), len(reject), namesOf(entries))
	}
	logf(t, "fixtures OK: posts=%d rejected=%d (%s)", posts, len(entries), strings.Join(namesOf(entries), ", "))
}

func namesOf(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
