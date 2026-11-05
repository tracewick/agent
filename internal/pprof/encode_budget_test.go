package pprof_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/tracewick/agent/internal/pprof"
	"github.com/tracewick/agent/internal/spool"
)

func TestEncodeLimitedRejectsHugeDerivedClosureName(t *testing.T) {
	t.Parallel()
	file := strings.Repeat("p", 8<<10) // 8 KiB path
	closure := "{closure}"
	line := uint32(1)
	p := &pprof.Profile{
		EventType: spool.EventCPU,
		PeriodNs:  10,
		StartNs:   1,
		EndNs:     2,
		Frames: []pprof.Frame{{
			Function:    &closure,
			File:        &file,
			ClosureLine: &line,
		}},
		Stacks: []pprof.Stack{{FrameIDs: []int{0}, Count: 1}},
	}
	_, err := pprof.EncodeLimited(p, pprof.EncodeLimits{
		MaxStringBytes:  1 << 20,
		MaxEncodedBytes: 1 << 20,
		MaxNameBytes:    1024, // derived name is ~8KiB
	})
	if !errors.Is(err, pprof.ErrEncodeBudget) {
		t.Fatalf("expected ErrEncodeBudget, got %v", err)
	}
}

func TestEncodeLimitedRejectsStringTableGrowth(t *testing.T) {
	t.Parallel()
	// Many short unique names that fit MaxNameBytes but exhaust MaxStringBytes.
	const n = 50
	frames := make([]pprof.Frame, n)
	stacks := make([]pprof.Stack, n)
	for i := 0; i < n; i++ {
		fn := strings.Repeat("x", 40) + string(rune('a'+i%26)) + strings.Repeat("y", i%5)
		frames[i] = pprof.Frame{Function: &fn}
		stacks[i] = pprof.Stack{FrameIDs: []int{i}, Count: 1}
	}
	p := &pprof.Profile{
		EventType: spool.EventWall,
		PeriodNs:  1,
		StartNs:   1,
		EndNs:     2,
		Frames:    frames,
		Stacks:    stacks,
	}
	_, err := pprof.EncodeLimited(p, pprof.EncodeLimits{
		MaxStringBytes:  200, // event type + unit already use some; unique names blow the rest
		MaxEncodedBytes: 1 << 20,
		MaxNameBytes:    256,
	})
	if !errors.Is(err, pprof.ErrEncodeBudget) {
		t.Fatalf("expected ErrEncodeBudget, got %v", err)
	}
}

func TestEncodeLimitedRejectsEncodedSize(t *testing.T) {
	t.Parallel()
	fn := "main"
	p := &pprof.Profile{
		EventType: spool.EventCPU,
		PeriodNs:  10,
		StartNs:   1000,
		EndNs:     2000,
		Frames:    []pprof.Frame{{Function: &fn}},
		Stacks:    []pprof.Stack{{FrameIDs: []int{0}, Count: 1}},
	}
	_, err := pprof.EncodeLimited(p, pprof.EncodeLimits{
		MaxStringBytes:  1 << 20,
		MaxEncodedBytes: 8, // impossibly small
		MaxNameBytes:    256,
	})
	if !errors.Is(err, pprof.ErrEncodeBudget) {
		t.Fatalf("expected ErrEncodeBudget, got %v", err)
	}
}

func TestEstimatedNameBytesMatchesName(t *testing.T) {
	t.Parallel()
	file := "/var/www/" + strings.Repeat("a", 100) + ".php"
	closure := "{closure}"
	line := uint32(42)
	class := "App\\Handler"
	empty := ""
	cases := []pprof.Frame{
		{},
		{Function: &empty},
		{Function: strPtr("idx")},
		{Function: strPtr("run"), Class: &class},
		{Function: &closure}, // no closure line
		{Function: &closure, Class: &class},
		{Function: &closure, File: &file, ClosureLine: &line},
		{Function: &closure, Class: &class, ClosureLine: &line},
		{Function: strPtr("{closure:foo.php:9}")},
		{Function: strPtr("{closure:App::run():9}")},
		{Function: strPtr("{closure:run():9}")},
		{Function: strPtr("{closure:no-line}")},
		{Function: strPtr("{closure:x:abc}")}, // non-digit line → raw function
		{Function: strPtr("{closure:scope:9}"), Class: &class},
	}
	for _, f := range cases {
		if got, want := f.EstimatedNameBytes(), len(f.Name()); got != want {
			t.Fatalf("estimate %d != len(Name)=%d for %#v → %q", got, want, f, f.Name())
		}
	}
}

func TestEncodeLimitedDefaultLimitsWhenZero(t *testing.T) {
	t.Parallel()
	fn := "main"
	p := &pprof.Profile{
		EventType: spool.EventCPU, PeriodNs: 10, StartNs: 1, EndNs: 2,
		Frames: []pprof.Frame{{Function: &fn}},
		Stacks: []pprof.Stack{{FrameIDs: []int{0}, Count: 1}},
	}
	got, err := pprof.EncodeLimited(p, pprof.EncodeLimits{})
	if err != nil || len(got) == 0 {
		t.Fatalf("defaults: %v len=%d", err, len(got))
	}
}

func TestEncodeLimitedRejectsTinyStringBudgetOnValueType(t *testing.T) {
	t.Parallel()
	fn := "main"
	p := &pprof.Profile{
		EventType: spool.EventCPU, PeriodNs: 10, StartNs: 1, EndNs: 2,
		Frames: []pprof.Frame{{Function: &fn}},
		Stacks: []pprof.Stack{{FrameIDs: []int{0}, Count: 1}},
	}
	_, err := pprof.EncodeLimited(p, pprof.EncodeLimits{
		MaxStringBytes: 2, MaxEncodedBytes: 1 << 20, MaxNameBytes: 100,
	})
	if !errors.Is(err, pprof.ErrEncodeBudget) {
		t.Fatalf("expected ErrEncodeBudget, got %v", err)
	}
}

func TestEncodeLimitedRejectsLongFunctionNameDirectly(t *testing.T) {
	t.Parallel()
	// Truncated placeholder path calls function() with a fixed short name; exercise
	// MaxNameBytes via a regular frame whose Name() is long without being a closure.
	fn := strings.Repeat("f", 500)
	p := &pprof.Profile{
		EventType: spool.EventCPU, PeriodNs: 10, StartNs: 1, EndNs: 2,
		Frames: []pprof.Frame{{Function: &fn}},
		Stacks: []pprof.Stack{{FrameIDs: []int{0}, Count: 1}},
	}
	_, err := pprof.EncodeLimited(p, pprof.EncodeLimits{
		MaxStringBytes: 1 << 20, MaxEncodedBytes: 1 << 20, MaxNameBytes: 64,
	})
	if !errors.Is(err, pprof.ErrEncodeBudget) {
		t.Fatalf("expected ErrEncodeBudget, got %v", err)
	}
}

func TestEncodeClosureLineZeroDigits(t *testing.T) {
	t.Parallel()
	closure := "{closure}"
	line := uint32(0)
	file := "x.php"
	f := pprof.Frame{Function: &closure, File: &file, ClosureLine: &line}
	if got, want := f.EstimatedNameBytes(), len(f.Name()); got != want {
		t.Fatalf("%d != %d (%q)", got, want, f.Name())
	}
}

func TestEncodeLimitedRejectsWhenStringBudgetExhaustedBeforeName(t *testing.T) {
	t.Parallel()
	// Fill most of the string budget with a long unique function, then a long closure name.
	pad := strings.Repeat("n", 180)
	file := strings.Repeat("f", 80)
	closure := "{closure}"
	line := uint32(3)
	p := &pprof.Profile{
		EventType: spool.EventCPU, PeriodNs: 1, StartNs: 1, EndNs: 2,
		Frames: []pprof.Frame{
			{Function: &pad},
			{Function: &closure, File: &file, ClosureLine: &line},
		},
		Stacks: []pprof.Stack{{FrameIDs: []int{0, 1}, Count: 1}},
	}
	_, err := pprof.EncodeLimited(p, pprof.EncodeLimits{
		MaxStringBytes:  220, // cpu/nanoseconds + pad leave too little for closure name
		MaxEncodedBytes: 1 << 20,
		MaxNameBytes:    1 << 20,
	})
	if !errors.Is(err, pprof.ErrEncodeBudget) {
		t.Fatalf("expected ErrEncodeBudget, got %v", err)
	}
}

func TestEncodeLimitedReusedFrameIDAtFullStringBudget(t *testing.T) {
	t.Parallel()
	// wall(4)+nanoseconds(11)+foo(3)=18. Second reference to the same frame adds 0 bytes.
	fn := "foo"
	p := &pprof.Profile{
		EventType: spool.EventWall, PeriodNs: 1, StartNs: 1, EndNs: 2,
		Frames: []pprof.Frame{{Function: &fn}},
		Stacks: []pprof.Stack{{FrameIDs: []int{0, 0}, Count: 1}},
	}
	got, err := pprof.EncodeLimited(p, pprof.EncodeLimits{
		MaxStringBytes: 18, MaxEncodedBytes: 1 << 20, MaxNameBytes: 64,
	})
	if err != nil || len(got) == 0 {
		t.Fatalf("reused frameID must encode at full string budget: %v", err)
	}
}

func TestEncodeLimitedSharedNameAcrossFramesAtFullStringBudget(t *testing.T) {
	t.Parallel()
	// Two frames, same function name, different lines → one string-table entry for the name.
	fn := "foo"
	p := &pprof.Profile{
		EventType: spool.EventWall, PeriodNs: 1, StartNs: 1, EndNs: 2,
		Frames: []pprof.Frame{
			{Function: &fn, Line: 1},
			{Function: &fn, Line: 2},
		},
		Stacks: []pprof.Stack{{FrameIDs: []int{0, 1}, Count: 1}},
	}
	got, err := pprof.EncodeLimited(p, pprof.EncodeLimits{
		MaxStringBytes: 18, MaxEncodedBytes: 1 << 20, MaxNameBytes: 64,
	})
	if err != nil || len(got) == 0 {
		t.Fatalf("shared name must not re-charge string budget: %v", err)
	}
}

func strPtr(s string) *string { return &s }
