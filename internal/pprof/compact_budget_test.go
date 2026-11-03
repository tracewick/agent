package pprof_test

import (
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
