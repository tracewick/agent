package pprof

import (
	"testing"

	"github.com/tracewick/agent/internal/spool"
)

func TestNewBoundedBuilderDefaultsAndBounds(t *testing.T) {
	t.Parallel()
	b, err := NewBoundedBuilder(0, 0, 0)
	if err != nil || b.maxStacks != DefaultMaxStacks {
		t.Fatalf("%v %#v", err, b)
	}
	if _, err := NewBoundedBuilder(maxStacksCap+1, 10, 10); err == nil {
		t.Fatal("stacks high")
	}
	if _, err := NewBoundedBuilder(10, maxFramesCap+1, 10); err == nil {
		t.Fatal("frames high")
	}
	if _, err := NewBoundedBuilder(10, 10, maxStringsCap+1); err == nil {
		t.Fatal("strings high")
	}
	if u32len(-1) != 0 || u32len(int(^uint32(0))+1) != ^uint32(0) {
		t.Fatal("u32len sat")
	}
	if u32sat(uint64(^uint32(0))+1) != ^uint32(0) || u32sat(1) != 1 {
		t.Fatal("u32sat")
	}
}

func TestUniqueStackCountWithOverflowAndEmptyStackKey(t *testing.T) {
	t.Parallel()
	b, err := NewBoundedBuilder(1, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	b.Add([]Frame{{Function: strP("a")}}, 1, false)
	b.Add([]Frame{{Function: strP("b")}}, 1, false)
	if b.UniqueStackCount() != 2 { // kept + overflow
		t.Fatal(b.UniqueStackCount())
	}
	b2, _ := NewBoundedBuilder(10, 10, 10)
	b2.Add(nil, 1, false) // empty frame list → empty joinIDs
	frames, stacks := b2.Parts()
	if len(stacks) != 1 || len(stacks[0].FrameIDs) != 0 || len(frames) != 0 {
		t.Fatalf("%#v %#v", frames, stacks)
	}
}

func TestProtoPackedEmpty(t *testing.T) {
	t.Parallel()
	w := &protoWriter{}
	_ = w.packed(1, nil)
	if len(w.buf) != 0 {
		t.Fatal(w.buf)
	}
}

func TestMergeNilLaterAndNilLabels(t *testing.T) {
	t.Parallel()
	a := &Profile{
		EventType: spool.EventWall, PeriodNs: 1, StartNs: 5, EndNs: 6,
		Frames: []Frame{{Function: strP("a")}}, Stacks: []Stack{{FrameIDs: []int{0}, Count: 1}},
	}
	if _, err := Merge([]*Profile{a, nil}); err == nil {
		t.Fatal("nil later")
	}
	a.Labels = nil
	b := &Profile{
		EventType: spool.EventWall, PeriodNs: 1, StartNs: 1, EndNs: 10,
		Frames: []Frame{{Function: strP("a")}}, Stacks: []Stack{{FrameIDs: []int{0}, Count: 1}},
	}
	merged, err := Merge([]*Profile{a, b})
	if err != nil || merged.Labels == nil || merged.StartNs != 1 || merged.EndNs != 10 {
		t.Fatalf("%v %#v", err, merged)
	}
	if _, err := (Merger{MaxStacks: maxStacksCap + 1}).Merge([]*Profile{a}); err == nil {
		t.Fatal("bad caps")
	}
}

func TestFromSpoolDecodeErrorAndNilLabels(t *testing.T) {
	t.Parallel()
	f := &spool.File{Format: spool.FormatCompactStacks, Payload: []byte{9}, Labels: nil}
	if _, err := FromSpoolFile(f); err == nil {
		t.Fatal("decode")
	}
	f.Payload = mustEncodeCompact(t)
	f.EventType = spool.EventCPU
	f.PeriodNs = 1
	f.StartNs = 1
	f.EndNs = 2
	p, err := FromSpoolFile(f)
	if err != nil || p.Labels == nil {
		t.Fatalf("%v %#v", err, p)
	}
	if _, err := MergeSpoolFiles([]*spool.File{f, {Format: "nope"}}); err == nil {
		t.Fatal("merge spool err")
	}
}

func TestDecodeCompactTruncations(t *testing.T) {
	t.Parallel()
	full := mustEncodeCompact(t)
	for n := 0; n < len(full); n++ {
		if _, _, err := DecodeCompactStacks(full[:n]); err == nil {
			t.Fatalf("expected error at truncate %d", n)
		}
	}
	// Truncate inside a string body after declaring length.
	partial := []byte{1, 0, 0, 0, 1, 0, 0, 0, 4, 'a', 'b'}
	if _, _, err := DecodeCompactStacks(partial); err == nil {
		t.Fatal("string body")
	}
	// One string "x"; frame with valid function index but bad class / file indexes.
	base := []byte{1}
	base = appendU32Be(base, 1, 1) // 1 string of len 1
	base = append(base, 'x')
	base = appendU32Be(base, 1, 1) // 1 stack, weight 1
	base = append(base, 0)
	base = appendU32Be(base, 1) // 1 frame
	badClass := append(append([]byte{}, base...), appendU32Be(nil, 0, 9, nullIndex, 1, nullIndex)...)
	if _, _, err := DecodeCompactStacks(badClass); err == nil {
		t.Fatal("class index")
	}
	badFile := append(append([]byte{}, base...), appendU32Be(nil, 0, nullIndex, 9, 1, nullIndex)...)
	if _, _, err := DecodeCompactStacks(badFile); err == nil {
		t.Fatal("file index")
	}
}

func appendU32Be(out []byte, values ...uint32) []byte {
	for _, v := range values {
		out = append(out,
			byte(v>>24), //nolint:gosec // big-endian byte split
			byte(v>>16), //nolint:gosec // big-endian byte split
			byte(v>>8),  //nolint:gosec // big-endian byte split
			byte(v),     //nolint:gosec // big-endian byte split
		)
	}
	return out
}

func mustEncodeCompact(t *testing.T) []byte {
	t.Helper()
	fn := "fn"
	b, err := EncodeCompactStacks([]Frame{{Function: &fn}}, []Stack{{FrameIDs: []int{0}, Count: 1}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func strP(s string) *string { return &s }
