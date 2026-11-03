package spool_test

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tracewick/agent/internal/spool"
)

const startNs = 1_798_000_000_000_000_000

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "spool", "v2", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func buildSpool(t *testing.T, header map[string]any, payload []byte) []byte {
	t.Helper()
	header["payload_size"] = len(payload)
	raw, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > spool.MaxHeaderBytes {
		t.Fatalf("test header too large: %d", len(raw))
	}
	out := make([]byte, 0, 9+len(raw)+len(payload))
	out = append(out, "TWSP"...)
	out = append(out, spool.Version2)
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(raw))) //nolint:gosec // bounded by MaxHeaderBytes above
	out = append(out, lenBuf[:]...)
	out = append(out, raw...)
	out = append(out, payload...)
	return out
}

func validHeader() map[string]any {
	return map[string]any{
		"application":  "blog",
		"event_type":   "wall",
		"period_ns":    10_000_000,
		"start_ns":     startNs,
		"end_ns":       startNs + 1,
		"labels":       map[string]string{"service": "blog"},
		"format":       spool.FormatCompactStacks,
		"payload_size": 0,
	}
}

func TestParseWebRequest(t *testing.T) {
	t.Parallel()

	f, err := spool.Parse(fixture(t, "web-request.spool"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Application != "blog" {
		t.Fatalf("application = %q", f.Application)
	}
	if f.EventType != spool.EventWall {
		t.Fatalf("event = %q", f.EventType)
	}
	if f.PeriodNs != 10_000_000 {
		t.Fatalf("period = %d", f.PeriodNs)
	}
	if f.StartNs != startNs || f.EndNs != startNs+200_000_000 {
		t.Fatalf("window = %d..%d", f.StartNs, f.EndNs)
	}
	if f.Labels["service"] != "blog" || len(f.Labels) != 1 {
		t.Fatalf("labels = %#v", f.Labels)
	}
	if f.Format != spool.FormatCompactStacks || f.Version != spool.Version2 {
		t.Fatalf("format/version = %q/%d", f.Format, f.Version)
	}
	if len(f.Payload) == 0 {
		t.Fatal("empty payload")
	}
}

func TestParseEdgeCases(t *testing.T) {
	t.Parallel()

	f, err := spool.Parse(fixture(t, "edge-cases.spool"))
	if err != nil {
		t.Fatal(err)
	}
	if f.EventType != spool.EventCPU {
		t.Fatalf("event = %q", f.EventType)
	}
	if len(f.Labels) != 0 {
		t.Fatalf("labels = %#v", f.Labels)
	}
}

func TestParseUnknownHeaderKey(t *testing.T) {
	t.Parallel()

	f, err := spool.Parse(fixture(t, "unknown-header-key.spool"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Labels["service"] != "blog" {
		t.Fatalf("labels = %#v", f.Labels)
	}
}

func TestParseRejectsInvalidFixtures(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"bad-magic.spool",
		"unsupported-version.spool",
		"version-1.spool",
		"header-too-large.spool",
		"truncated-payload.spool",
		"header-not-json.spool",
		"missing-key.spool",
		"end-before-start.spool",
		"unknown-format.spool",
	}
	for _, name := range invalid {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := spool.Parse(fixture(t, name)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestParseRejectsCraftedHeaders(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"period zero", func(h map[string]any) { h["period_ns"] = 0 }},
		{"period negative", func(h map[string]any) { h["period_ns"] = -1 }},
		{"unknown event", func(h map[string]any) { h["event_type"] = "gpu" }},
		{"labels array", func(h map[string]any) { h["labels"] = []string{"a"} }},
		{"label non-string", func(h map[string]any) { h["labels"] = map[string]any{"k": 1} }},
		{"application int", func(h map[string]any) { h["application"] = 1 }},
		{"truncated header", nil},
		{"empty", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			switch tc.name {
			case "empty":
				if _, err := spool.Parse(nil); err == nil {
					t.Fatal("expected error")
				}
				return
			case "truncated header":
				buf := []byte("TWSP\x02\x00\x00\x00\xff")
				if _, err := spool.Parse(buf); err == nil {
					t.Fatal("expected error")
				}
				return
			}
			h := validHeader()
			tc.mutate(h)
			if _, err := spool.Parse(buildSpool(t, h, []byte{1, 2, 3})); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestParseAcceptsCPUAndEmptyLabelsObject(t *testing.T) {
	t.Parallel()

	h := validHeader()
	h["event_type"] = "cpu"
	h["labels"] = map[string]string{}
	f, err := spool.Parse(buildSpool(t, h, []byte{9}))
	if err != nil {
		t.Fatal(err)
	}
	if f.EventType != spool.EventCPU || len(f.Labels) != 0 || len(f.Payload) != 1 {
		t.Fatalf("%#v", f)
	}
}

func TestParseRejectsNullLabelsAndHeaderArray(t *testing.T) {
	t.Parallel()

	h := validHeader()
	h["labels"] = nil
	if _, err := spool.Parse(buildSpool(t, h, []byte{1})); err == nil {
		t.Fatal("expected null labels error")
	}

	header := []byte("[]")
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(header))) //nolint:gosec // tiny test header
	buf := append(append([]byte{'T', 'W', 'S', 'P', spool.Version2}, lenBuf[:]...), header...)
	if _, err := spool.Parse(buf); err == nil {
		t.Fatal("expected array header error")
	}
}

func TestFreshTemporaryNotDeleted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	src, err := spool.NewSource(spool.Config{
		Directory:      dir,
		ReaderID:       "reader-a",
		StaleTemporary: time.Hour,
		Now:            func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	writeSpool(t, dir, ".fresh.tmp", []byte("t"))
	if _, claimErr := src.Claim(); claimErr != nil {
		t.Fatal(claimErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".fresh.tmp")); statErr != nil {
		t.Fatalf("fresh temp should remain: %v", statErr)
	}
}
