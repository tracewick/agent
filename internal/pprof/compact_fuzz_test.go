package pprof_test

import (
	"testing"

	"github.com/tracewick/agent/internal/pprof"
)

func FuzzDecodeCompactStacks(f *testing.F) {
	// Hostile capacity-before-bytes cases from the review.
	f.Add([]byte{1, 0xff, 0xff, 0xff, 0xff})
	f.Add([]byte{1, 0, 0, 0, 1, 0, 0, 0, 1, 'x', 0, 0, 0, 1, 0, 0, 0, 1, 0, 0xff, 0xff, 0xff, 0xff})
	f.Add([]byte{1})
	f.Add([]byte(nil))

	fn := "main"
	ok, err := pprof.EncodeCompactStacks(
		[]pprof.Frame{{Function: &fn}},
		[]pprof.Stack{{FrameIDs: []int{0}, Count: 1}},
	)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(ok)

	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _, _ = pprof.DecodeCompactStacks(data) // must not panic or OOM on counts
	})
}
