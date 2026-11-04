package pprof_test

import (
	"strconv"
	"testing"

	"github.com/tracewick/agent/internal/pprof"
)

func TestDecodeCompactRejectsHugeStringCountBeforeAlloc(t *testing.T) {
	t.Parallel()
	// schema=1, stringCount=0xffffffff — must fail before allocating ~4e9 slots.
	payload := []byte{1, 0xff, 0xff, 0xff, 0xff}
	if _, _, err := pprof.DecodeCompactStacks(payload); err == nil {
		t.Fatal("expected reject of huge stringCount")
	}
}

func TestDecodeCompactRejectsHugeFrameCountBeforeAlloc(t *testing.T) {
	t.Parallel()
	// 1 string "x", 1 stack, weight 1, flags 0, frameCount=0xffffffff
	payload := []byte{1}
	payload = appendU32Be(payload, 1, 1) // 1 string len 1
	payload = append(payload, 'x')
	payload = appendU32Be(payload, 1, 1) // 1 stack, weight 1
	payload = append(payload, 0)
	payload = appendU32Be(payload, 0xffffffff) // frameCount
	if _, _, err := pprof.DecodeCompactStacks(payload); err == nil {
		t.Fatal("expected reject of huge frameCount")
	}
}

func TestDecodeCompactAppliesDefaultLimitsWhenZero(t *testing.T) {
	t.Parallel()
	fn := "main"
	ok, err := pprof.EncodeCompactStacks(
		[]pprof.Frame{{Function: &fn}},
		[]pprof.Stack{{FrameIDs: []int{0}, Count: 1}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := pprof.DecodeCompactStacksLimited(ok, pprof.DecodeLimits{}); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeCompactRejectsTooManyStacks(t *testing.T) {
	t.Parallel()
	// MaxStacks=2 allows wire budget 3 (regular + overflow). Four stacks must fail.
	payload := []byte{1}
	payload = appendU32Be(payload, 0) // strings
	payload = appendU32Be(payload, 4) // stacks
	for i := 0; i < 4; i++ {
		payload = appendU32Be(payload, 1) // weight
		payload = append(payload, 0)      // flags
		payload = appendU32Be(payload, 0) // frameCount
	}
	_, _, err := pprof.DecodeCompactStacksLimited(payload, pprof.DecodeLimits{
		MaxStacks: 2, MaxFrames: 10, MaxStrings: 10,
	})
	if err == nil {
		t.Fatal("expected max stacks")
	}
}

func TestDecodeCompactAcceptsPHPOverflowStack(t *testing.T) {
	t.Parallel()
	// BoundedBuilder matches PHP: MaxStacks regular stacks + 1 [overflow] sentinel.
	const maxStacks = 8
	b, err := pprof.NewBoundedBuilder(maxStacks, 64, 64)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxStacks+5; i++ {
		fn := "fn-" + strconv.Itoa(i)
		b.Add([]pprof.Frame{{Function: &fn}}, 3, false)
	}
	assertPHPOverflowDecodes(t, b, pprof.DecodeLimits{MaxStacks: maxStacks, MaxFrames: 64, MaxStrings: 64})
}

func TestDecodeCompactAcceptsPHPOverflowFrame(t *testing.T) {
	t.Parallel()
	// Builder keeps MaxFrames regular frames; Parts appends [overflow] → MaxFrames+1.
	const maxFrames = 8
	b, err := pprof.NewBoundedBuilder(64, maxFrames, 64)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxFrames+5; i++ {
		fn := "frame-" + strconv.Itoa(i)
		b.Add([]pprof.Frame{{Function: &fn}}, 2, false)
	}
	if !b.WasCapped() || b.OverflowWeight() == 0 {
		t.Fatalf("expected frame overflow, capped=%v overflow=%d", b.WasCapped(), b.OverflowWeight())
	}
	frames, _ := b.Parts()
	if len(frames) != maxFrames+1 {
		t.Fatalf("parts frames = %d want %d", len(frames), maxFrames+1)
	}
	assertPHPOverflowDecodes(t, b, pprof.DecodeLimits{MaxStacks: 64, MaxFrames: maxFrames, MaxStrings: 64})
}

func TestDecodeCompactAcceptsPHPOverflowString(t *testing.T) {
	t.Parallel()
	// MaxStrings regular names; Parts adds "[overflow]" → MaxStrings+1 on wire.
	const maxStrings = 8
	b, err := pprof.NewBoundedBuilder(64, 64, maxStrings)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxStrings+5; i++ {
		fn := "str-" + strconv.Itoa(i)
		b.Add([]pprof.Frame{{Function: &fn}}, 4, false)
	}
	if !b.WasCapped() || b.OverflowWeight() == 0 {
		t.Fatalf("expected string overflow, capped=%v overflow=%d", b.WasCapped(), b.OverflowWeight())
	}
	assertPHPOverflowDecodes(t, b, pprof.DecodeLimits{MaxStacks: 64, MaxFrames: 64, MaxStrings: maxStrings})
}

func assertPHPOverflowDecodes(t *testing.T, b *pprof.BoundedBuilder, lim pprof.DecodeLimits) {
	t.Helper()
	frames, stacks := b.Parts()
	wantWeight := b.InWeight()
	payload, err := pprof.EncodeCompactStacks(frames, stacks)
	if err != nil {
		t.Fatal(err)
	}
	gotFrames, gotStacks, err := pprof.DecodeCompactStacksLimited(payload, lim)
	if err != nil {
		t.Fatalf("PHP overflow profile must decode: %v", err)
	}
	var out uint64
	for _, s := range gotStacks {
		out += s.Count
	}
	if out != wantWeight || len(gotFrames) == 0 {
		t.Fatalf("weight want=%d got=%d frames=%d", wantWeight, out, len(gotFrames))
	}
}

func TestWireBudgetsDefault(t *testing.T) {
	t.Parallel()
	if got := pprof.WireStackBudget(pprof.DecodeLimits{}); got != pprof.DefaultMaxStacks+pprof.MaxOverflowStacks {
		t.Fatalf("stacks got %d", got)
	}
	if got := pprof.WireFrameBudget(pprof.DecodeLimits{}); got != pprof.DefaultMaxFrames+pprof.MaxOverflowFrames {
		t.Fatalf("frames got %d", got)
	}
	if got := pprof.WireStringBudget(pprof.DecodeLimits{}); got != pprof.DefaultMaxStrings+pprof.MaxOverflowStrings {
		t.Fatalf("strings got %d", got)
	}
}

func TestDecodeCompactRejectsFrameRefsAndUniqueFrames(t *testing.T) {
	t.Parallel()
	// MaxFrames=2 → wire budget 3; four unique frames must fail.
	payload := []byte{1}
	payload = appendU32Be(payload, 0) // no strings
	payload = appendU32Be(payload, 4)
	for _, line := range []uint32{1, 2, 3, 4} {
		payload = appendU32Be(payload, 1)
		payload = append(payload, 0)
		payload = appendU32Be(payload, 1)
		payload = appendU32Be(payload, 0xffffffff, 0xffffffff, 0xffffffff, line, 0xffffffff)
	}
	_, _, err := pprof.DecodeCompactStacksLimited(payload, pprof.DecodeLimits{
		MaxStacks: 10, MaxFrames: 2, MaxStrings: 10, MaxFrameRefs: 100,
	})
	if err == nil {
		t.Fatal("expected unique frames limit")
	}

	// Frame refs: MaxFrameRefs=1, one stack with 2 frame slots
	payload2 := []byte{1}
	payload2 = appendU32Be(payload2, 0)
	payload2 = appendU32Be(payload2, 1)
	payload2 = appendU32Be(payload2, 1)
	payload2 = append(payload2, 0)
	payload2 = appendU32Be(payload2, 2)
	for line := uint32(1); line <= 2; line++ {
		payload2 = appendU32Be(payload2, 0xffffffff, 0xffffffff, 0xffffffff, line, 0xffffffff)
	}
	_, _, err = pprof.DecodeCompactStacksLimited(payload2, pprof.DecodeLimits{
		MaxStacks: 10, MaxFrames: 10, MaxStrings: 10, MaxFrameRefs: 1,
	})
	if err == nil {
		t.Fatal("expected frame refs limit")
	}
}

func TestDecodeCompactRejectsHugeStringBytes(t *testing.T) {
	t.Parallel()
	big := make([]byte, 100)
	for i := range big {
		big[i] = 'x'
	}
	payload := []byte{1}
	payload = appendU32Be(payload, 1)
	payload = appendU32Be(payload, uint32(len(big))) //nolint:gosec // test
	payload = append(payload, big...)
	payload = appendU32Be(payload, 0) // stacks
	_, _, err := pprof.DecodeCompactStacksLimited(payload, pprof.DecodeLimits{
		MaxStacks: 10, MaxFrames: 10, MaxStrings: 10, MaxStringBytes: 50,
	})
	if err == nil {
		t.Fatal("expected string byte budget")
	}
}

func TestDecodeCompactRespectsMaxStrings(t *testing.T) {
	t.Parallel()
	// MaxStrings=2 → wire budget 3; four strings must fail.
	payload := []byte{1}
	payload = appendU32Be(payload, 4) // stringCount
	for _, c := range []byte{'a', 'b', 'c', 'd'} {
		payload = appendU32Be(payload, 1)
		payload = append(payload, c)
	}
	_, _, err := pprof.DecodeCompactStacksLimited(payload, pprof.DecodeLimits{
		MaxStacks: 10, MaxFrames: 10, MaxStrings: 2,
	})
	if err == nil {
		t.Fatal("expected max strings")
	}
}

func appendU32Be(out []byte, values ...uint32) []byte {
	for _, v := range values {
		out = append(out,
			byte(v>>24), //nolint:gosec // test helper
			byte(v>>16), //nolint:gosec // test helper
			byte(v>>8),  //nolint:gosec // test helper
			byte(v),     //nolint:gosec // test helper
		)
	}
	return out
}
