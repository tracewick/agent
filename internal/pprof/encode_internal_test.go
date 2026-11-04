package pprof

import (
	"errors"
	"strings"
	"testing"

	"github.com/tracewick/agent/internal/spool"
)

func TestStringTableRemainingEdges(t *testing.T) {
	t.Parallel()
	unlimited := &stringTable{maxBytes: 0}
	if unlimited.remaining() != 1<<30 {
		t.Fatalf("unlimited remaining=%d", unlimited.remaining())
	}
	over := &stringTable{maxBytes: 5, byteLen: 9}
	if over.remaining() != 0 {
		t.Fatalf("over budget remaining=%d", over.remaining())
	}
}

func TestEncodeBudgetAddRejects(t *testing.T) {
	t.Parallel()
	b := &encodeBudget{max: 4}
	if err := b.add(-1); err == nil {
		t.Fatal("expected negative reject")
	}
	if err := b.add(3); err != nil {
		t.Fatal(err)
	}
	if err := b.add(2); !errors.Is(err, ErrEncodeBudget) {
		t.Fatalf("got %v", err)
	}
	if (&encodeBudget{max: 0}).add(100) != nil {
		t.Fatal("max<=0 is unlimited")
	}
}

func TestProtoWriterBytesFailsOnHeader(t *testing.T) {
	t.Parallel()
	w := &protoWriter{budget: &encodeBudget{max: 1}}
	if err := w.bytes(1, []byte("payload")); !errors.Is(err, ErrEncodeBudget) {
		t.Fatalf("got %v", err)
	}
}

func TestEncodeLimitedFileIndexBudget(t *testing.T) {
	t.Parallel()
	fn := "fn"
	file := strings.Repeat("p", 40)
	p := &Profile{
		EventType: spool.EventCPU, PeriodNs: 1, StartNs: 1, EndNs: 2,
		Frames: []Frame{{Function: &fn, File: &file}},
		Stacks: []Stack{{FrameIDs: []int{0}, Count: 1}},
	}
	// cpu(3)+nanoseconds(11)+fn(2)=16; file of 40 needs more.
	_, err := EncodeLimited(p, EncodeLimits{MaxStringBytes: 20, MaxEncodedBytes: 1 << 20, MaxNameBytes: 100})
	if !errors.Is(err, ErrEncodeBudget) {
		t.Fatalf("got %v", err)
	}
}

func TestEncodeLimitedSpanIDBudget(t *testing.T) {
	t.Parallel()
	fn := "a"
	span := strings.Repeat("s", 50)
	p := &Profile{
		EventType: spool.EventCPU, PeriodNs: 1, StartNs: 1, EndNs: 2,
		Frames: []Frame{{Function: &fn}},
		Stacks: []Stack{{FrameIDs: []int{0}, Count: 1}},
		Labels: map[string]string{sampleLabelSpanID: span},
	}
	_, err := EncodeLimited(p, EncodeLimits{MaxStringBytes: 30, MaxEncodedBytes: 1 << 20, MaxNameBytes: 100})
	if !errors.Is(err, ErrEncodeBudget) {
		t.Fatalf("got %v", err)
	}
}

func TestEncodeLimitedLocationBudget(t *testing.T) {
	t.Parallel()
	fn := "work"
	p := &Profile{
		EventType: spool.EventCPU, PeriodNs: 10, StartNs: 1, EndNs: 2,
		Frames: []Frame{{Function: &fn}},
		Stacks: []Stack{{FrameIDs: []int{0}, Count: 1}},
	}
	// Large enough for head sample-type field, too small for full sample+location+function+tail.
	_, err := EncodeLimited(p, EncodeLimits{MaxStringBytes: 1 << 20, MaxEncodedBytes: 40, MaxNameBytes: 100})
	if !errors.Is(err, ErrEncodeBudget) {
		t.Fatalf("got %v", err)
	}
}

func TestEncodeLimitedUnknownFrameBudget(t *testing.T) {
	t.Parallel()
	p := &Profile{
		EventType: spool.EventCPU, PeriodNs: 10, StartNs: 1, EndNs: 2,
		Frames: nil,
		Stacks: []Stack{{FrameIDs: []int{0}, Count: 1}}, // out of range → UnknownFrame
	}
	_, err := EncodeLimited(p, EncodeLimits{MaxStringBytes: 1 << 20, MaxEncodedBytes: 35, MaxNameBytes: 100})
	if !errors.Is(err, ErrEncodeBudget) {
		t.Fatalf("got %v", err)
	}
}

func TestEncodeLimitedTruncatedBudget(t *testing.T) {
	t.Parallel()
	fn := "x"
	p := &Profile{
		EventType: spool.EventCPU, PeriodNs: 10, StartNs: 1, EndNs: 2,
		Frames: []Frame{{Function: &fn}},
		Stacks: []Stack{{FrameIDs: []int{0}, Count: 1, Truncated: true}},
	}
	_, err := EncodeLimited(p, EncodeLimits{MaxStringBytes: 1 << 20, MaxEncodedBytes: 55, MaxNameBytes: 100})
	if !errors.Is(err, ErrEncodeBudget) {
		t.Fatalf("got %v", err)
	}
}

func TestEncodeLimitedUnknownNameBudget(t *testing.T) {
	t.Parallel()
	// Out-of-range frame → function("[unknown]") without EstimatedNameBytes gate.
	p := &Profile{
		EventType: spool.EventCPU, PeriodNs: 1, StartNs: 1, EndNs: 2,
		Stacks: []Stack{{FrameIDs: []int{0}, Count: 1}},
	}
	_, err := EncodeLimited(p, EncodeLimits{
		MaxStringBytes: 1 << 20, MaxEncodedBytes: 1 << 20, MaxNameBytes: 5, // "[unknown]" is 9
	})
	if !errors.Is(err, ErrEncodeBudget) {
		t.Fatalf("got %v", err)
	}
}

func TestEncodeLimitedUnknownStringBudget(t *testing.T) {
	t.Parallel()
	p := &Profile{
		EventType: spool.EventCPU, PeriodNs: 1, StartNs: 1, EndNs: 2,
		Stacks: []Stack{{FrameIDs: []int{0}, Count: 1}},
	}
	// cpu+nanoseconds leave too little for "[unknown]".
	_, err := EncodeLimited(p, EncodeLimits{MaxStringBytes: 16, MaxEncodedBytes: 1 << 20, MaxNameBytes: 100})
	if !errors.Is(err, ErrEncodeBudget) {
		t.Fatalf("got %v", err)
	}
}

func TestEncodeLimitedEmptyStackUnknownBudget(t *testing.T) {
	t.Parallel()
	p := &Profile{
		EventType: spool.EventCPU, PeriodNs: 1, StartNs: 1, EndNs: 2,
		Stacks: []Stack{{FrameIDs: nil, Count: 1}}, // empty → UnknownFrame placeholder
	}
	_, err := EncodeLimited(p, EncodeLimits{MaxStringBytes: 1 << 20, MaxEncodedBytes: 30, MaxNameBytes: 100})
	if !errors.Is(err, ErrEncodeBudget) {
		t.Fatalf("got %v", err)
	}
}

func TestEncodeLimitedSpanIDKeyBudget(t *testing.T) {
	t.Parallel()
	fn := strings.Repeat("x", 10)
	p := &Profile{
		EventType: spool.EventCPU, PeriodNs: 1, StartNs: 1, EndNs: 2,
		Frames: []Frame{{Function: &fn}},
		Stacks: []Stack{{FrameIDs: []int{0}, Count: 1}},
		Labels: map[string]string{sampleLabelSpanID: "ab"},
	}
	// ""+cpu+nanoseconds+fn = 0+3+11+10 = 24; span_id needs 7 more.
	_, err := EncodeLimited(p, EncodeLimits{MaxStringBytes: 24, MaxEncodedBytes: 1 << 20, MaxNameBytes: 100})
	if !errors.Is(err, ErrEncodeBudget) {
		t.Fatalf("got %v", err)
	}
}

func TestEncodeLimitedValueTypeUnitBudget(t *testing.T) {
	t.Parallel()
	fn := "a"
	p := &Profile{
		EventType: spool.EventCPU, PeriodNs: 1, StartNs: 1, EndNs: 2,
		Frames: []Frame{{Function: &fn}},
		Stacks: []Stack{{FrameIDs: []int{0}, Count: 1}},
	}
	// Enough for "cpu" (3), not "nanoseconds" (11).
	_, err := EncodeLimited(p, EncodeLimits{MaxStringBytes: 3, MaxEncodedBytes: 1 << 20, MaxNameBytes: 100})
	if !errors.Is(err, ErrEncodeBudget) {
		t.Fatalf("got %v", err)
	}
}

func TestEncodeLimitedSweepEncodedBudgets(t *testing.T) {
	t.Parallel()
	fn := "work"
	file := "a.php"
	span := "deadbeef"
	p := &Profile{
		EventType: spool.EventCPU, PeriodNs: 10, StartNs: 1000, EndNs: 2000,
		Frames: []Frame{{Function: &fn, File: &file}},
		Stacks: []Stack{
			{FrameIDs: []int{0}, Count: 1, Truncated: true},
			{FrameIDs: nil, Count: 1},
			{FrameIDs: []int{9}, Count: 1},
		},
		Labels: map[string]string{sampleLabelSpanID: span},
	}
	sawBudget := false
	sawOK := false
	for maxEnc := 1; maxEnc < 400; maxEnc++ {
		_, err := EncodeLimited(p, EncodeLimits{
			MaxStringBytes: 1 << 20, MaxEncodedBytes: maxEnc, MaxNameBytes: 256,
		})
		if err == nil {
			sawOK = true
			continue
		}
		if errors.Is(err, ErrEncodeBudget) {
			sawBudget = true
			continue
		}
		t.Fatalf("maxEnc=%d: %v", maxEnc, err)
	}
	if !sawBudget || !sawOK {
		t.Fatalf("expected both budget failures and success, budget=%v ok=%v", sawBudget, sawOK)
	}
}
