package pprof

import "github.com/tracewick/agent/internal/spool"

// Stack is a distinct call stack and how often it was observed (outermost caller first).
type Stack struct {
	FrameIDs  []int
	Count     uint32
	Truncated bool
}

// Profile is the format-independent profile used for merge and pprof encode.
type Profile struct {
	EventType spool.EventType
	PeriodNs  int64
	StartNs   int64
	EndNs     int64
	Frames    []Frame
	Stacks    []Stack
	Labels    map[string]string
}

// DurationNs returns end - start.
func (p *Profile) DurationNs() int64 {
	if p == nil || p.EndNs < p.StartNs {
		return 0
	}
	return p.EndNs - p.StartNs
}

// Empty reports whether there are no stacks.
func (p *Profile) Empty() bool {
	return p == nil || len(p.Stacks) == 0
}
