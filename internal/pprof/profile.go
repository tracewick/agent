package pprof

import "github.com/tracewick/agent/internal/spool"

// Stack is a distinct call stack and how often it was observed (outermost caller first).
// Count is the aggregated sample weight (may exceed uint32 after merge); compact-stacks
// wire encoding rejects weights above math.MaxUint32 instead of saturating silently.
type Stack struct {
	FrameIDs  []int
	Count     uint64
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

// EstimatedBytes is a conservative heap estimate for holding this profile decoded
// (frames, stacks, labels, string payloads) — not the spool wire size.
func (p *Profile) EstimatedBytes() int64 {
	if p == nil {
		return 0
	}
	const (
		profileOverhead = 96
		frameOverhead   = 64
		stackOverhead   = 48
		ptrSize         = 8
		mapEntryOver    = 40
	)
	n := int64(profileOverhead)
	for _, f := range p.Frames {
		n += frameOverhead
		n += int64(f.EstimatedNameBytes())
		if f.Function != nil {
			n += int64(len(*f.Function))
		}
		if f.Class != nil {
			n += int64(len(*f.Class))
		}
		if f.File != nil {
			n += int64(len(*f.File))
		}
	}
	for _, s := range p.Stacks {
		n += stackOverhead + int64(len(s.FrameIDs))*ptrSize
	}
	for k, v := range p.Labels {
		n += mapEntryOver + int64(len(k)+len(v))
	}
	return n
}
