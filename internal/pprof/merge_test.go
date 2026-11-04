package pprof_test

import (
	"testing"

	"github.com/tracewick/agent/internal/pprof"
	"github.com/tracewick/agent/internal/spool"
)

func TestMergeAddsWeightsAndWidensWindow(t *testing.T) {
	t.Parallel()
	frame := fr(str("work"), nil, nil, 0, nil)
	a := &pprof.Profile{
		EventType: spool.EventWall, PeriodNs: 10_000_000, StartNs: 100, EndNs: 200,
		Frames: []pprof.Frame{frame}, Stacks: []pprof.Stack{{FrameIDs: []int{0}, Count: 2}},
		Labels: map[string]string{"env": "prod"},
	}
	b := &pprof.Profile{
		EventType: spool.EventWall, PeriodNs: 10_000_000, StartNs: 150, EndNs: 300,
		Frames: []pprof.Frame{frame}, Stacks: []pprof.Stack{{FrameIDs: []int{0}, Count: 5}},
		Labels: map[string]string{"env": "prod"},
	}
	merged, err := pprof.Merge([]*pprof.Profile{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if merged.StartNs != 100 || merged.EndNs != 300 || len(merged.Stacks) != 1 || merged.Stacks[0].Count != 7 {
		t.Fatalf("%#v", merged)
	}
	if merged.Labels["env"] != "prod" {
		t.Fatal(merged.Labels)
	}
}

func TestMergeKeepsDistinctAndTruncatedSeparate(t *testing.T) {
	t.Parallel()
	a := &pprof.Profile{
		EventType: spool.EventCPU, PeriodNs: 1, StartNs: 1, EndNs: 2,
		Frames: []pprof.Frame{fr(str("a"), nil, nil, 0, nil)}, Stacks: []pprof.Stack{{FrameIDs: []int{0}, Count: 1}},
	}
	b := &pprof.Profile{
		EventType: spool.EventCPU, PeriodNs: 1, StartNs: 1, EndNs: 2,
		Frames: []pprof.Frame{fr(str("b"), nil, nil, 0, nil)}, Stacks: []pprof.Stack{{FrameIDs: []int{0}, Count: 3}},
	}
	merged, err := pprof.Merge([]*pprof.Profile{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Frames) != 2 || len(merged.Stacks) != 2 {
		t.Fatalf("%#v", merged)
	}
	frame := fr(str("work"), nil, nil, 0, nil)
	full := &pprof.Profile{
		EventType: spool.EventWall, PeriodNs: 1, StartNs: 1, EndNs: 2,
		Frames: []pprof.Frame{frame}, Stacks: []pprof.Stack{{FrameIDs: []int{0}, Count: 2}},
	}
	trunc := &pprof.Profile{
		EventType: spool.EventWall, PeriodNs: 1, StartNs: 1, EndNs: 2,
		Frames: []pprof.Frame{frame}, Stacks: []pprof.Stack{{FrameIDs: []int{0}, Count: 3, Truncated: true}},
	}
	merged, err = pprof.Merge([]*pprof.Profile{full, trunc})
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Stacks) != 2 {
		t.Fatal(merged.Stacks)
	}
}

func TestMergeRejectsAndCaps(t *testing.T) {
	t.Parallel()
	if _, err := pprof.Merge(nil); err == nil {
		t.Fatal("empty")
	}
	if _, err := pprof.Merge([]*pprof.Profile{nil}); err == nil {
		t.Fatal("nil")
	}
	a := &pprof.Profile{
		EventType: spool.EventWall, PeriodNs: 10, StartNs: 1, EndNs: 2,
		Frames: []pprof.Frame{fr(str("a"), nil, nil, 0, nil)}, Stacks: []pprof.Stack{{FrameIDs: []int{0}, Count: 1}},
	}
	b := &pprof.Profile{
		EventType: spool.EventWall, PeriodNs: 20, StartNs: 1, EndNs: 2,
		Frames: []pprof.Frame{fr(str("a"), nil, nil, 0, nil)}, Stacks: []pprof.Stack{{FrameIDs: []int{0}, Count: 1}},
	}
	if _, err := pprof.Merge([]*pprof.Profile{a, b}); err == nil {
		t.Fatal("period")
	}
	bad := &pprof.Profile{
		EventType: spool.EventWall, PeriodNs: 1, StartNs: 1, EndNs: 2,
		Frames: []pprof.Frame{fr(str("a"), nil, nil, 0, nil)}, Stacks: []pprof.Stack{{FrameIDs: []int{99}, Count: 1}},
	}
	if _, err := pprof.Merge([]*pprof.Profile{bad}); err == nil {
		t.Fatal("missing frame")
	}
	pa := &pprof.Profile{
		EventType: spool.EventWall, PeriodNs: 1, StartNs: 1, EndNs: 2,
		Frames: []pprof.Frame{fr(str("a"), nil, nil, 0, nil)}, Stacks: []pprof.Stack{{FrameIDs: []int{0}, Count: 2}},
	}
	pb := &pprof.Profile{
		EventType: spool.EventWall, PeriodNs: 1, StartNs: 1, EndNs: 2,
		Frames: []pprof.Frame{fr(str("b"), nil, nil, 0, nil)}, Stacks: []pprof.Stack{{FrameIDs: []int{0}, Count: 3}},
	}
	pc := &pprof.Profile{
		EventType: spool.EventWall, PeriodNs: 1, StartNs: 1, EndNs: 2,
		Frames: []pprof.Frame{fr(str("c"), nil, nil, 0, nil)}, Stacks: []pprof.Stack{{FrameIDs: []int{0}, Count: 5}},
	}
	merged, err := (pprof.Merger{MaxStacks: 2}).Merge([]*pprof.Profile{pa, pb, pc})
	if err != nil {
		t.Fatal(err)
	}
	var total uint64
	var sawOverflow bool
	for _, f := range merged.Frames {
		if f.Function != nil && *f.Function == pprof.OverflowFunction {
			sawOverflow = true
		}
	}
	for _, s := range merged.Stacks {
		total += s.Count
	}
	if !sawOverflow || total != 10 {
		t.Fatalf("overflow=%v total=%d %#v", sawOverflow, total, merged)
	}
}

func TestFromSpoolFileAndMergeSpool(t *testing.T) {
	t.Parallel()
	web := readSpool(t, "web-request.spool")
	p, err := pprof.FromSpoolFile(web)
	if err != nil {
		t.Fatal(err)
	}
	if p.Empty() || p.DurationNs() != web.EndNs-web.StartNs || p.EventType != spool.EventWall {
		t.Fatalf("%#v", p)
	}
	if _, errNil := pprof.FromSpoolFile(nil); errNil == nil {
		t.Fatal("nil")
	}
	bad := *web
	bad.Format = "other"
	if _, errFmt := pprof.FromSpoolFile(&bad); errFmt == nil {
		t.Fatal("format")
	}
	merged, errMerge := pprof.MergeSpoolFiles([]*spool.File{web, web})
	if errMerge != nil {
		t.Fatal(errMerge)
	}
	if len(merged.Stacks) == 0 {
		t.Fatal("empty merge")
	}
	// doubled weights vs single decode
	single, errSingle := pprof.FromSpoolFile(web)
	if errSingle != nil {
		t.Fatal(errSingle)
	}
	if merged.Stacks[0].Count != single.Stacks[0].Count*2 {
		t.Fatalf("count %d vs %d", merged.Stacks[0].Count, single.Stacks[0].Count)
	}
}

func TestProfileEmptyDuration(t *testing.T) {
	t.Parallel()
	if !(*pprof.Profile)(nil).Empty() {
		t.Fatal("nil")
	}
	p := &pprof.Profile{StartNs: 10, EndNs: 5}
	if p.DurationNs() != 0 {
		t.Fatal(p.DurationNs())
	}
}
