//go:build e2e

package e2e_test

import (
	"context"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestStandPyroscopeReady(t *testing.T) {
	pyroscope, toxic, toxiAPI, ok := standEnv()
	if !ok {
		t.Skipf("stand env unset (%s, %s, %s); run make stand-up", envPyroscopeURL, envPyroscopeToxicURL, envToxiproxyURL)
	}
	for _, u := range []string{pyroscope + "/ready", toxic + "/ready"} {
		assertReady(t, u)
	}
	tx := newToxiproxy(toxiAPI)
	if err := tx.reset(); err != nil {
		t.Fatal(err)
	}
}

// TestStandToxicLatencyForcesAgentFailure mirrors main-repo chaos intent: inject
// downstream latency via Toxiproxy and require the agent to fail within its HTTP budget
// (not hang), leaving spool reclaimable.
func TestStandToxicLatencyForcesAgentFailure(t *testing.T) {
	_, toxic, toxiAPI, ok := standEnv()
	if !ok {
		t.Skipf("stand env unset; run make stand-up")
	}

	bin := agentBinary(t)
	spoolDir := filepath.Join(t.TempDir(), "spool")
	seedSpool(t, spoolDir, fixtureBytes(t, "web-request.spool"), 1)

	tx := newToxiproxy(toxiAPI)
	if err := tx.reset(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.reset() })
	// timeout=0 holds forever without closing (same pattern as PHP PyroscopeChaosTest).
	// Agent default HTTP timeout is 10s — must fail inside that budget.
	if err := tx.addToxic(proxyPyroscope, "timeout", "upstream", map[string]int{"timeout": 0}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	start := time.Now()
	cmd := exec.CommandContext(ctx, bin, "run", //nolint:gosec // test binary
		"--spool-dir="+spoolDir,
		"--pyroscope-url="+toxic,
		"--reader-id=e2e-toxic",
		"--once",
		"--sleep=50ms",
	)
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected agent failure under toxic timeout; stderr/out:\n%s", out)
	}
	if elapsed > 15*time.Second {
		t.Fatalf("agent took too long under toxic timeout: %v\n%s", elapsed, out)
	}
	// File must still be present (released or still claimed→reclaimable), not silently acked.
	left := listSpoolLeftovers(t, spoolDir)
	if len(left) == 0 {
		t.Fatalf("expected spool leftover after failed delivery, got none; out:\n%s", out)
	}
}

func assertReady(t *testing.T, url string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode < 200 || res.StatusCode > 299 {
		t.Fatalf("%s: %s", url, res.Status)
	}
}
