package pprof_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/tracewick/agent/internal/pprof"
	"github.com/tracewick/agent/internal/spool"
)

func TestEncodeMatchesPHPGoldens(t *testing.T) {
	t.Parallel()
	cases := []struct {
		spool  string
		golden string
	}{
		{"web-request.spool", "pprof-web-request.pb"},
		{"edge-cases.spool", "pprof-edge-cases.pb"},
	}
	for _, tc := range cases {
		t.Run(tc.golden, func(t *testing.T) {
			t.Parallel()
			f := readSpool(t, tc.spool)
			p, err := pprof.FromSpoolFile(f)
			if err != nil {
				t.Fatal(err)
			}
			got, err := pprof.Encode(p)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("..", "..", "testdata", "pprof", tc.golden))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("encode mismatch: got %d bytes, want %d", len(got), len(want))
			}
		})
	}
}

func TestEncodeEmptyProfile(t *testing.T) {
	t.Parallel()
	got, err := pprof.Encode(&pprof.Profile{EventType: spool.EventCPU, PeriodNs: 10, StartNs: 1000, EndNs: 1500})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("empty bytes")
	}
	// Must contain string table entries for "", "cpu", "nanoseconds" — smoke via re-encode size stability.
	got2, err := pprof.Encode(&pprof.Profile{EventType: spool.EventCPU, PeriodNs: 10, StartNs: 1000, EndNs: 1500})
	if err != nil || !bytes.Equal(got, got2) {
		t.Fatal("not deterministic")
	}
}

func TestEncodeSampleValuesAndSpanID(t *testing.T) {
	t.Parallel()
	work := fr(str("work"), nil, nil, 0, nil)
	p := &pprof.Profile{
		EventType: spool.EventWall, PeriodNs: 10, StartNs: 0, EndNs: 100,
		Frames: []pprof.Frame{work},
		Stacks: []pprof.Stack{{FrameIDs: []int{0}, Count: 3}, {FrameIDs: nil, Count: 2}},
		Labels: map[string]string{"env": "prod", "service": "blog"},
	}
	got, err := pprof.Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(got, []byte("env")) || bytes.Contains(got, []byte("blog")) {
		t.Fatal("series labels must not appear on samples")
	}
	withSpan := &pprof.Profile{
		EventType: spool.EventWall, PeriodNs: 10, StartNs: 0, EndNs: 100,
		Frames: []pprof.Frame{work},
		Stacks: []pprof.Stack{{FrameIDs: []int{0}, Count: 1}},
		Labels: map[string]string{"span_id": "00f067aa0ba902b7", "env": "prod"},
	}
	got, err = pprof.Encode(withSpan)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("span_id")) || !bytes.Contains(got, []byte("00f067aa0ba902b7")) {
		t.Fatal("expected span_id sample label")
	}
	if bytes.Contains(got, []byte("env")) {
		t.Fatal("env must not be a sample label")
	}
}

func TestEncodeUnknownTruncatedAndSharedFunctions(t *testing.T) {
	t.Parallel()
	p := &pprof.Profile{
		EventType: spool.EventCPU, PeriodNs: 10, StartNs: 0, EndNs: 100,
		Frames: []pprof.Frame{fr(str("work"), nil, nil, 0, nil)},
		Stacks: []pprof.Stack{
			{FrameIDs: []int{0, 7}, Count: 1, Truncated: true},
			{FrameIDs: nil, Count: 1, Truncated: true},
			{FrameIDs: nil, Count: 1},
			{FrameIDs: []int{9}, Count: 1},
		},
	}
	got, err := pprof.Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte(pprof.UnknownFrame)) || !bytes.Contains(got, []byte(pprof.TruncatedFrame)) {
		t.Fatal("placeholders")
	}
	if !bytes.Contains(got, []byte("work")) {
		t.Fatal("work")
	}

	shared := &pprof.Profile{
		EventType: spool.EventWall, PeriodNs: 10, StartNs: 0, EndNs: 100,
		Frames: []pprof.Frame{
			fr(str("work"), nil, str("/app/w.php"), 1, nil),
			fr(str("work"), nil, str("/app/w.php"), 2, nil),
			fr(str("work"), nil, str("/app/other.php"), 2, nil),
			fr(str("{closure}"), nil, str("/app/w.php"), 9, u32(7)),
			fr(str("a"), nil, str("b.php"), 1, nil),
			fr(str("ab"), nil, str(".php"), 1, nil),
		},
		Stacks: []pprof.Stack{{FrameIDs: []int{3, 0, 1, 2, 4, 5}, Count: 1}},
	}
	got, err = pprof.Encode(shared)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte("{closure:/app/w.php:7}")) {
		t.Fatal("closure name")
	}
}

func TestEncodeNilAndMediaType(t *testing.T) {
	t.Parallel()
	if _, err := pprof.Encode(nil); err == nil {
		t.Fatal("nil")
	}
	if pprof.MediaType != "application/x-protobuf" {
		t.Fatal(pprof.MediaType)
	}
}
