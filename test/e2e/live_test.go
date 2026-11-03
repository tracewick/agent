//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tracewick/agent/internal/spool"
)

// TestLivePyroscopeRoundTrip pushes via a real agent into stand Pyroscope, then
// asserts Querier LabelValues (parity with PHP PyroscopeSinkTest).
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
	body := buildSpoolBytes(t, map[string]any{
		"application": app,
		"event_type":  string(base.EventType),
		"period_ns":   base.PeriodNs,
		"start_ns":    startNs,
		"end_ns":      endNs,
		"labels":      base.Labels,
		"format":      spool.FormatCompactStacks,
	}, base.Payload)

	bin := agentBinary(t)
	spoolDir := filepath.Join(t.TempDir(), "spool")
	seedSpool(t, spoolDir, body, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "run", //nolint:gosec // test binary
		"--spool-dir="+spoolDir,
		"--pyroscope-url="+pyroscope,
		"--reader-id=e2e-live",
		"--once",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("agent: %v\n%s", err, out)
	}
	if left := listSpoolLeftovers(t, spoolDir); len(left) != 0 {
		t.Fatalf("spool leftovers after live push: %v", left)
	}

	assertLabelValues(t, pyroscope, "service_name", app, startNs, []string{app})
	assertLabelValues(t, pyroscope, "pyroscope_spy", app, startNs, []string{"tracewick"})
	assertLabelValues(t, pyroscope, "service", app, startNs, []string{"blog"})
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

func assertLabelValues(t *testing.T, baseURL, labelName, application string, aroundNs int64, want []string) {
	t.Helper()
	aroundMs := aroundNs / 1_000_000
	reqBody := map[string]any{
		"name":     labelName,
		"matchers": []string{fmt.Sprintf(`{service_name="%s"}`, application)},
		"start":    aroundMs - 60_000,
		"end":      aroundMs + 60_000,
	}
	deadline := time.Now().Add(90 * time.Second)
	var got []string
	for {
		got = querierLabelValues(t, baseURL, reqBody)
		if len(got) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for LabelValues %s (service_name=%s)", labelName, application)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !stringSlicesEqual(got, want) {
		t.Fatalf("LabelValues %s = %#v; want %#v", labelName, got, want)
	}
}

func querierLabelValues(t *testing.T, baseURL string, body map[string]any) []string {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := strings.TrimRight(baseURL, "/") + "/querier.v1.QuerierService/LabelValues"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		t.Fatalf("querier %s: %s %s", url, res.Status, payload)
	}
	var decoded struct {
		Names []string `json:"names"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("querier decode: %v body=%s", err, payload)
	}
	return decoded.Names
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
