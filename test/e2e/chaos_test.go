//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestStandToxicChaosSuite mirrors main-repo PyroscopeChaosTest toxics against a
// real agent process: each fault must fail inside the HTTP budget and leave the
// spool reclaimable (not silently acked). Healthy toxic proxy delivers.
func TestStandToxicChaosSuite(t *testing.T) {
	_, toxic, toxiAPI, ok := standEnv()
	if !ok {
		t.Skipf("stand env unset; run make stand-up")
	}

	bin := agentBinary(t)
	tx := newToxiproxy(toxiAPI)
	if err := tx.reset(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.reset() })

	t.Run("healthy", func(t *testing.T) {
		if err := tx.reset(); err != nil {
			t.Fatal(err)
		}
		spoolDir := filepath.Join(t.TempDir(), "spool")
		seedSpool(t, spoolDir, fixtureBytes(t, "web-request.spool"), 1)
		out, err := runAgentOnce(t, bin, spoolDir, toxic, "e2e-chaos-ok", 45*time.Second)
		if err != nil {
			t.Fatalf("healthy toxic proxy: %v\n%s", err, out)
		}
		if left := listSpoolLeftovers(t, spoolDir); len(left) != 0 {
			t.Fatalf("leftovers after healthy push: %v", left)
		}
	})

	cases := []struct {
		name   string
		typ    string
		stream string
		attrs  map[string]int
		// Agent default HTTP timeout is 10s; keep wall under that + slack.
		maxElapsed time.Duration
	}{
		{
			name:       "latency",
			typ:        "latency",
			stream:     "downstream",
			attrs:      map[string]int{"latency": 15_000},
			maxElapsed: 15 * time.Second,
		},
		{
			name:       "timeout",
			typ:        "timeout",
			stream:     "upstream",
			attrs:      map[string]int{"timeout": 0},
			maxElapsed: 15 * time.Second,
		},
		{
			name:       "reset_peer",
			typ:        "reset_peer",
			stream:     "upstream",
			attrs:      map[string]int{"timeout": 0},
			maxElapsed: 15 * time.Second,
		},
		{
			name:       "limit_data",
			typ:        "limit_data",
			stream:     "downstream",
			attrs:      map[string]int{"bytes": 5},
			maxElapsed: 15 * time.Second,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tx.reset(); err != nil {
				t.Fatal(err)
			}
			if err := tx.addToxic(proxyPyroscope, tc.typ, tc.stream, tc.attrs); err != nil {
				t.Fatal(err)
			}
			spoolDir := filepath.Join(t.TempDir(), "spool")
			seedSpool(t, spoolDir, fixtureBytes(t, "web-request.spool"), 1)

			start := time.Now()
			out, err := runAgentOnce(t, bin, spoolDir, toxic, "e2e-chaos-"+tc.name, 45*time.Second)
			elapsed := time.Since(start)
			if err == nil {
				t.Fatalf("expected failure under %s; out:\n%s", tc.name, out)
			}
			if elapsed > tc.maxElapsed {
				t.Fatalf("%s: agent wall %v > %v\n%s", tc.name, elapsed, tc.maxElapsed, out)
			}
			if left := listSpoolLeftovers(t, spoolDir); len(left) == 0 {
				t.Fatalf("%s: expected spool leftover after failed delivery; out:\n%s", tc.name, out)
			}
		})
	}
}

// TestProcessKillLeavesClaimThenStaleReclaim: kill -9 while the agent holds a
// claimed file mid-push; forge a stale claim epoch; a second agent reclaims and
// delivers (parity with main-repo stale reclaim / crash hand-off).
func TestProcessKillLeavesClaimThenStaleReclaim(t *testing.T) {
	bin := agentBinary(t)
	spoolDir := filepath.Join(t.TempDir(), "spool")
	seedSpool(t, spoolDir, fixtureBytes(t, "web-request.spool"), 1)

	// Block POSTs until closed so kill -9 can land while a claim is held mid-push.
	releasePush := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-releasePush
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "run", //nolint:gosec // test binary
		"--spool-dir="+spoolDir,
		"--pyroscope-url="+srv.URL,
		"--reader-id=e2e-kill",
		"--once",
		"--sleep=50ms",
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	// Wait until a claim appears (agent entered delivery).
	deadline := time.Now().Add(10 * time.Second)
	var claimed string
	for time.Now().Before(deadline) {
		left := listSpoolLeftovers(t, spoolDir)
		for _, name := range left {
			if strings.Contains(name, ".spool.claimed-") {
				claimed = name
				break
			}
		}
		if claimed != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if claimed == "" {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		close(releasePush)
		t.Fatal("timed out waiting for claim before kill")
	}

	if err := cmd.Process.Signal(os.Kill); err != nil { // kill -9
		t.Fatal(err)
	}
	_, _ = cmd.Process.Wait()
	close(releasePush)

	if left := listSpoolLeftovers(t, spoolDir); len(left) == 0 {
		t.Fatal("expected claimed leftover after kill -9")
	}

	if err := forgeStaleClaimEpoch(spoolDir, 15*time.Minute); err != nil {
		t.Fatal(err)
	}

	out, err := runAgentOnce(t, bin, spoolDir, srv.URL, "e2e-reclaim", 45*time.Second)
	if err != nil {
		t.Fatalf("reclaim agent: %v\n%s", err, out)
	}
	if left := listSpoolLeftovers(t, spoolDir); len(left) != 0 {
		t.Fatalf("leftovers after stale reclaim: %v", left)
	}
}

var claimEpochRE = regexp.MustCompile(`^(.*\.claimed-[A-Za-z0-9._-]+-)(\d+)$`)

func forgeStaleClaimEpoch(spoolDir string, age time.Duration) error {
	entries, err := os.ReadDir(spoolDir)
	if err != nil {
		return err
	}
	oldEpoch := strconv.FormatInt(time.Now().Add(-age).Unix(), 10)
	for _, e := range entries {
		name := e.Name()
		m := claimEpochRE.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		newName := m[1] + oldEpoch
		if newName == name {
			continue
		}
		if err := os.Rename(filepath.Join(spoolDir, name), filepath.Join(spoolDir, newName)); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("no claimed file to forge in %s", spoolDir)
}

func runAgentOnce(t *testing.T, bin, spoolDir, pyroscopeURL, readerID string, timeout time.Duration) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "run", //nolint:gosec // test binary
		"--spool-dir="+spoolDir,
		"--pyroscope-url="+pyroscopeURL,
		"--reader-id="+readerID,
		"--once",
		"--sleep=50ms",
	)
	return cmd.CombinedOutput()
}
