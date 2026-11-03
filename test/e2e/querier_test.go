//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

func querierPOST(t *testing.T, baseURL, method string, body map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	url := strings.TrimRight(baseURL, "/") + "/querier.v1.QuerierService/" + method
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
	payload, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		t.Fatalf("querier %s: %s %s", method, res.Status, payload)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("querier decode %s: %v body=%s", method, err, payload)
	}
	return decoded
}

func querierLabelValues(t *testing.T, baseURL, labelName, application string, aroundNs int64) []string {
	t.Helper()
	aroundMs := aroundNs / 1_000_000
	body := map[string]any{
		"name":     labelName,
		"matchers": []string{fmt.Sprintf(`{service_name="%s"}`, application)},
		"start":    aroundMs - 60_000,
		"end":      aroundMs + 60_000,
	}
	decoded := querierPOST(t, baseURL, "LabelValues", body)
	return stringList(decoded["names"])
}

func waitLabelValues(t *testing.T, baseURL, labelName, application string, aroundNs int64, want []string) {
	t.Helper()
	logf(t, "Querier LabelValues name=%s service_name=%s want=%v", labelName, application, want)
	deadline := time.Now().Add(90 * time.Second)
	started := time.Now()
	var lastProg time.Time
	var got []string
	for {
		got = querierLabelValues(t, baseURL, labelName, application, aroundNs)
		if len(got) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for LabelValues %s (service_name=%s)", labelName, application)
		}
		waitProgress(t, started, 5*time.Second, &lastProg, "LabelValues %s still empty", labelName)
		time.Sleep(200 * time.Millisecond)
	}
	if !stringSlicesEqual(got, want) {
		t.Fatalf("LabelValues %s = %#v; want %#v", labelName, got, want)
	}
	logf(t, "LabelValues %s = %v", labelName, got)
}

func waitWallProfileTypeID(t *testing.T, baseURL string, aroundNs int64) string {
	t.Helper()
	logf(t, "Querier ProfileTypes (want *:wall:nanoseconds:*)")
	aroundMs := aroundNs / 1_000_000
	body := map[string]any{
		"start": aroundMs - 600_000,
		"end":   aroundMs + 60_000,
	}
	deadline := time.Now().Add(90 * time.Second)
	started := time.Now()
	var lastProg time.Time
	for {
		decoded := querierPOST(t, baseURL, "ProfileTypes", body)
		id := findWallNanosecondsType(decoded)
		if id != "" {
			logf(t, "ProfileTypes wall id=%s", id)
			return id
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for wall:nanoseconds ProfileTypes: %#v", decoded)
		}
		waitProgress(t, started, 5*time.Second, &lastProg, "no wall:nanoseconds type yet")
		time.Sleep(500 * time.Millisecond)
	}
}

func findWallNanosecondsType(decoded map[string]any) string {
	// Response shape varies slightly by Pyroscope version; walk for *:wall:nanoseconds:*.
	raw, _ := json.Marshal(decoded)
	re := regexp.MustCompile(`[^"\\]*:wall:nanoseconds:[^"\\]*`)
	return re.FindString(string(raw))
}

func waitSelectMergeHasFrame(t *testing.T, baseURL, typeID, application, frameNeedle string, aroundNs int64) {
	t.Helper()
	logf(t, "Querier SelectMergeStacktraces type=%s service_name=%s frame=%q", typeID, application, frameNeedle)
	aroundMs := aroundNs / 1_000_000
	selector := fmt.Sprintf(`{service_name="%s"}`, application)
	body := map[string]any{
		"profileTypeID": typeID,
		"labelSelector": selector,
		"start":         aroundMs - 60_000,
		"end":           aroundMs + 60_000,
	}
	deadline := time.Now().Add(90 * time.Second)
	started := time.Now()
	var lastProg time.Time
	for {
		decoded := querierPOST(t, baseURL, "SelectMergeStacktraces", body)
		blob, _ := json.Marshal(decoded)
		s := string(blob)
		if strings.Contains(s, frameNeedle) || strings.Contains(s, strings.ReplaceAll(frameNeedle, `\`, `\\`)) {
			// Non-empty flamebearer / names payload.
			if strings.Contains(s, "flamebearer") || strings.Contains(s, "Flamebearer") || strings.Contains(s, `"names"`) {
				logf(t, "SelectMergeStacktraces contains frame %q (%d JSON bytes)", frameNeedle, len(blob))
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for frame %q in SelectMergeStacktraces; got %s", frameNeedle, truncate(s, 1500))
		}
		waitProgress(t, started, 5*time.Second, &lastProg, "frame %q not in flamegraph yet", frameNeedle)
		time.Sleep(500 * time.Millisecond)
	}
}

func assertNoRawURLsInJSON(t *testing.T, label string, decoded map[string]any) {
	t.Helper()
	blob, _ := json.Marshal(decoded)
	s := string(blob)
	if regexp.MustCompile(`(?i)"[^"]*https?://[^"]*"`).MatchString(s) {
		t.Fatalf("%s contains http(s) URL in series labels: %s", label, truncate(s, 800))
	}
	if regexp.MustCompile(`"[^"]*/[^"]*\?[^"]*"`).MatchString(s) {
		t.Fatalf("%s contains path?query in series labels: %s", label, truncate(s, 800))
	}
}

func stringList(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		s, ok := x.(string)
		if ok {
			out = append(out, s)
		}
	}
	return out
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
