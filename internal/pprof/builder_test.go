package pprof_test

import (
	"strconv"
	"testing"

	"github.com/tracewick/agent/internal/pprof"
)

func itoa(i int) string { return strconv.Itoa(i) }

func TestBoundedBuilderMergesIdenticalStacks(t *testing.T) {
	t.Parallel()
	b, err := pprof.NewBoundedBuilder(10, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	frames := []pprof.Frame{fr(str("handle"), nil, nil, 0, nil)}
	b.Add(frames, 2, false)
	b.Add(frames, 3, false)
	if b.InWeight() != 5 || b.KeptWeight() != 5 || b.OverflowWeight() != 0 {
		t.Fatalf("weights in=%d kept=%d overflow=%d", b.InWeight(), b.KeptWeight(), b.OverflowWeight())
	}
	outFrames, stacks := b.Parts()
	if len(outFrames) != 1 || len(stacks) != 1 || stacks[0].Count != 5 {
		t.Fatalf("%#v %#v", outFrames, stacks)
	}
}

func TestBoundedBuilderOverflowDoesNotIntern(t *testing.T) {
	t.Parallel()
	b, err := pprof.NewBoundedBuilder(1, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	b.Add([]pprof.Frame{fr(str("kept"), nil, nil, 0, nil)}, 1, false)
	after := b.FrameCount()
	for i := 0; i < 50; i++ {
		fn := "flood_" + itoa(i)
		file := "/tmp/" + fn + ".php"
		b.Add([]pprof.Frame{fr(str(fn), nil, str(file), 0, nil)}, 1, false)
	}
	if !b.WasCapped() || b.FrameCount() != after || b.StringCount() != 1 {
		t.Fatalf("capped=%v frames=%d strings=%d", b.WasCapped(), b.FrameCount(), b.StringCount())
	}
	if b.InWeight() != 51 || b.KeptWeight() != 1 || b.OverflowWeight() != 50 {
		t.Fatalf("weights %#v", b)
	}
	frames, stacks := b.Parts()
	if len(frames) != 2 || frames[1].Name() != pprof.OverflowFunction || len(stacks) != 2 || stacks[1].Count != 50 {
		t.Fatalf("%#v %#v", frames, stacks)
	}
}

func TestBoundedBuilderCaps(t *testing.T) {
	t.Parallel()
	b, err := pprof.NewBoundedBuilder(100, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	b.Add([]pprof.Frame{fr(str("a"), nil, nil, 0, nil)}, 1, false)
	before := b.FrameCount()
	b.Add([]pprof.Frame{fr(str("b"), nil, nil, 0, nil)}, 2, false)
	if !b.WasCapped() || b.FrameCount() != before || b.OverflowWeight() != 2 {
		t.Fatal("frame cap")
	}

	b2, err := pprof.NewBoundedBuilder(100, 100, 1)
	if err != nil {
		t.Fatal(err)
	}
	b2.Add([]pprof.Frame{fr(str("only"), nil, nil, 0, nil)}, 1, false)
	bf, bs := b2.FrameCount(), b2.StringCount()
	b2.Add([]pprof.Frame{fr(str("other"), nil, nil, 0, nil)}, 3, false)
	if !b2.WasCapped() || b2.FrameCount() != bf || b2.StringCount() != bs || b2.OverflowWeight() != 3 {
		t.Fatal("string cap")
	}
}

func TestBoundedBuilderValidationAndEmpty(t *testing.T) {
	t.Parallel()
	if _, err := pprof.NewBoundedBuilder(0, 10, 10); err == nil {
		t.Fatal("stacks")
	}
	if _, err := pprof.NewBoundedBuilder(10, 0, 10); err == nil {
		t.Fatal("frames")
	}
	if _, err := pprof.NewBoundedBuilder(10, 10, 0); err == nil {
		t.Fatal("strings")
	}
	b, err := pprof.NewBoundedBuilder(10, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	b.Add([]pprof.Frame{fr(str("a"), nil, nil, 0, nil)}, 0, false)
	if !b.IsEmpty() || b.InWeight() != 0 {
		t.Fatal("zero weight")
	}
}

func TestBoundedBuilderWeightPreservedBeyondUint32(t *testing.T) {
	t.Parallel()
	b, err := pprof.NewBoundedBuilder(10, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	frames := []pprof.Frame{fr(str("hot"), nil, nil, 0, nil)}
	b.Add(frames, uint64(^uint32(0)), false)
	b.Add(frames, 2, false)
	want := uint64(^uint32(0)) + 2
	if b.KeptWeight() != want {
		t.Fatalf("keptWeight = %d", b.KeptWeight())
	}
	_, stacks := b.Parts()
	if len(stacks) != 1 || stacks[0].Count != want {
		t.Fatalf("Parts must keep full weight, got %#v", stacks)
	}
	if _, err := pprof.EncodeCompactStacks(frames, stacks); err == nil {
		t.Fatal("compact wire must reject weight above uint32")
	}
	if _, err := pprof.WireWeight(want); err == nil {
		t.Fatal("WireWeight must error")
	}
	if w, err := pprof.WireWeight(1); err != nil || w != 1 {
		t.Fatalf("WireWeight(1)=%d %v", w, err)
	}
}

func TestBoundedBuilderReusesPendingAndStrings(t *testing.T) {
	t.Parallel()
	b, err := pprof.NewBoundedBuilder(10, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	same := fr(str("same"), str("same"), str("same"), 0, nil)
	b.Add([]pprof.Frame{same, same}, 1, false)
	if b.FrameCount() != 1 || b.StringCount() != 1 || b.UniqueStackCount() != 1 {
		t.Fatal("pending reuse")
	}
	b.Add([]pprof.Frame{fr(str("a"), nil, str("/shared.php"), 0, nil)}, 1, false)
	b.Add([]pprof.Frame{fr(str("b"), nil, str("/shared.php"), 0, nil)}, 1, false)
	if b.FrameCount() != 3 || b.StringCount() != 4 {
		t.Fatalf("frames=%d strings=%d", b.FrameCount(), b.StringCount())
	}
	b2, _ := pprof.NewBoundedBuilder(10, 10, 10)
	empty := ""
	b2.Add([]pprof.Frame{{Function: &empty, Class: &empty, File: &empty}}, 1, false)
	if b2.StringCount() != 0 {
		t.Fatal("empty strings skipped")
	}
}
