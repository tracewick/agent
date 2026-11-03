//go:build e2e

package e2e_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestLongRunSIGTERMAfterDrain starts the agent without --once, waits until the
// spool is emptied, then SIGTERM; process must exit 0 and leave no leftovers
// (real Notify/grace path).
func TestLongRunSIGTERMAfterDrain(t *testing.T) {
	bin := agentBinary(t)
	spoolDir := filepath.Join(t.TempDir(), "spool")
	seedSpool(t, spoolDir, fixtureBytes(t, "web-request.spool"), 3)
	logf(t, "loop: seeded 3 files; long-run then SIGTERM after idle spool")

	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts++
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	cmd, stderr, err := startAgentLogged(t, bin, []string{
		"run",
		"--spool-dir=" + spoolDir,
		"--pyroscope-url=" + srv.URL,
		"--reader-id=e2e-loop",
		"--sleep=100ms",
		"--shutdown-grace=200ms",
	})
	if err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	var lastProg time.Time
	deadline := time.Now().Add(30 * time.Second)
	for {
		left := listSpoolLeftovers(t, spoolDir)
		if len(left) == 0 && posts >= 1 {
			logf(t, "loop: spool empty after %s; posts=%d", time.Since(started).Round(time.Millisecond), posts)
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
			t.Fatalf("timeout waiting for drain; posts=%d leftovers=%v\n%s", posts, left, stderr.String())
		}
		waitProgress(t, started, 2*time.Second, &lastProg, "posts=%d leftovers=%d", posts, len(left))
		time.Sleep(20 * time.Millisecond)
	}

	logf(t, "loop: sending SIGTERM to pid %d (expect exit within grace+ε)", cmd.Process.Pid)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SIGTERM exit: %v\n%s", err, stderr.String())
		}
		logf(t, "loop: exited 0 after SIGTERM; agent log: %s", trimLog(stderr.String(), 400))
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("agent did not exit after SIGTERM\n%s", stderr.String())
	}
	logSpool(t, spoolDir, "after SIGTERM")
	if left := listSpoolLeftovers(t, spoolDir); len(left) != 0 {
		t.Fatalf("leftovers after SIGTERM: %v", left)
	}
	if _, err := os.Stat(filepath.Join(spoolDir, "rejected")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(spoolDir, "rejected"))
		if len(entries) != 0 {
			t.Fatalf("unexpected rejects: %v", namesOf(entries))
		}
	}
}
