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
	// PHP BoundedProfileBuilder: MaxStacks regular stacks + 1 [overflow] sentinel.
	const maxStacks = 8
	b, err := pprof.NewBoundedBuilder(maxStacks, 64, 64)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxStacks+5; i++ {
		fn := "fn-" + strconv.Itoa(i)
		b.Add([]pprof.Frame{{Function: &fn}}, 3, false)
	}
	if !b.WasCapped() || b.OverflowWeight() == 0 {
		t.Fatalf("expected overflow weight, capped=%v overflow=%d", b.WasCapped(), b.OverflowWeight())
	}
	frames, stacks := b.Parts()
	if len(stacks) != maxStacks+1 {
		t.Fatalf("parts stacks = %d want %d", len(stacks), maxStacks+1)
	}
	payload, err := pprof.EncodeCompactStacks(frames, stacks)
	if err != nil {
		t.Fatal(err)
	}
	gotFrames, gotStacks, err := pprof.DecodeCompactStacksLimited(payload, pprof.DecodeLimits{
		MaxStacks: maxStacks, MaxFrames: 64, MaxStrings: 64,
	})
	if err != nil {
		t.Fatalf("PHP overflow profile must decode: %v", err)
	}
	var in, out uint64
	for _, s := range stacks {
		in += uint64(s.Count)
	}
	for _, s := range gotStacks {
		out += uint64(s.Count)
	}
	if in != out || len(gotFrames) == 0 {
		t.Fatalf("weight in=%d out=%d frames=%d", in, out, len(gotFrames))
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
	payload := []byte{1}
	payload = appendU32Be(payload, 3) // stringCount
	payload = appendU32Be(payload, 1)
	payload = append(payload, 'a')
	payload = appendU32Be(payload, 1)
	payload = append(payload, 'b')
	payload = appendU32Be(payload, 1)
	payload = append(payload, 'c')
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
