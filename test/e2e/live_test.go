//go:build e2e

package e2e_test

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/tracewick/agent/internal/spool"
)

// TestLivePyroscopeRoundTrip pushes via a real agent into stand Pyroscope, then
// asserts Querier readback at check-profiles.sh depth: LabelValues, ProfileTypes
// (wall:nanoseconds), SelectMergeStacktraces with fixture frames, and no raw URLs
// in LabelNames (parity with main-repo PyroscopeSinkTest + reference-app check).
func TestLivePyroscopeRoundTrip(t *testing.T) {
	pyroscope, _, _, ok := standEnv()
	if !ok {
		t.Skipf("stand env unset; run make stand-up")
	}

	app := "tracewick-agent-e2e-" + randomHex(t, 4)
	now := time.Now()
	startNs := now.Add(-2 * time.Second).UnixNano()
	endNs := now.UnixNano()

	base, err := spool.Parse(fixtureBytes(t, "web-request.spool"))
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{}
	for k, v := range base.Labels {
		labels[k] = v
	}
	// Coarse route label (SinkTest / check-profiles style); never a raw URL.
	labels["http.route"] = "/posts/{id}"

	body := buildSpoolBytes(t, map[string]any{
		"application": app,
		"event_type":  string(base.EventType),
		"period_ns":   base.PeriodNs,
		"start_ns":    startNs,
		"end_ns":      endNs,
		"labels":      labels,
		"format":      spool.FormatCompactStacks,
	}, base.Payload)

	bin := agentBinary(t)
	spoolDir := filepath.Join(t.TempDir(), "spool")
	seedSpool(t, spoolDir, body, 1)
	logf(t, "live push app=%s spool=%s pyroscope=%s", app, spoolDir, pyroscope)

	if _, err := runAgentLogged(t, bin, []string{
		"run",
		"--spool-dir=" + spoolDir,
		"--pyroscope-url=" + pyroscope,
		"--reader-id=e2e-live",
		"--once",
	}, 2*time.Minute); err != nil {
		t.Fatalf("agent live push: %v", err)
	}
	logSpool(t, spoolDir, "after live push")
	if left := listSpoolLeftovers(t, spoolDir); len(left) != 0 {
		t.Fatalf("spool leftovers after live push: %v", left)
	}

	waitLabelValues(t, pyroscope, "service_name", app, startNs, []string{app})
	waitLabelValues(t, pyroscope, "pyroscope_spy", app, startNs, []string{"tracewick"})
	waitLabelValues(t, pyroscope, "service", app, startNs, []string{"blog"})
	waitLabelValues(t, pyroscope, "http_route", app, startNs, []string{"/posts/{id}"})

	aroundMs := startNs / 1_000_000
	names := querierPOST(t, pyroscope, "LabelNames", map[string]any{
		"matchers": []string{fmt.Sprintf(`{service_name="%s"}`, app)},
		"start":    aroundMs - 60_000,
		"end":      aroundMs + 60_000,
	})
	assertNoRawURLsInJSON(t, "LabelNames", names)
	nameList := stringList(names["names"])
	logf(t, "LabelNames=%v", nameList)

	typeID := waitWallProfileTypeID(t, pyroscope, startNs)
	waitSelectMergeHasFrame(t, pyroscope, typeID, app, `PostController::show`, startNs)
	logf(t, "live Querier round-trip OK for %s", app)
}

func buildSpoolBytes(t *testing.T, header map[string]any, payload []byte) []byte {
	t.Helper()
	header["payload_size"] = len(payload)
	raw, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > spool.MaxHeaderBytes {
		t.Fatalf("header too large: %d", len(raw))
	}
	out := make([]byte, 0, 9+len(raw)+len(payload))
	out = append(out, spool.Magic...)
	out = append(out, spool.Version2)
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(raw))) //nolint:gosec // bounded
	out = append(out, lenBuf[:]...)
	out = append(out, raw...)
	out = append(out, payload...)
	return out
}

func randomHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
