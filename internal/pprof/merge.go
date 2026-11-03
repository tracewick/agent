package pprof

import (
	"fmt"
	"maps"

	"github.com/tracewick/agent/internal/spool"
)

// Merger merges profiles that share event type and period (ProfileMerger).
type Merger struct {
	MaxStacks  int
	MaxFrames  int
	MaxStrings int
}

// Merge combines profiles. Labels come from the first profile; the window is min/max.
func (m Merger) Merge(profiles []*Profile) (*Profile, error) {
	if len(profiles) == 0 {
		return nil, fmt.Errorf("pprof: nothing to merge")
	}
	first := profiles[0]
	if first == nil {
		return nil, fmt.Errorf("pprof: nil profile")
	}
	maxStacks, maxFrames, maxStrings := m.MaxStacks, m.MaxFrames, m.MaxStrings
	if maxStacks == 0 {
		maxStacks = DefaultMaxStacks
	}
	if maxFrames == 0 {
		maxFrames = DefaultMaxFrames
	}
	if maxStrings == 0 {
		maxStrings = DefaultMaxStrings
	}
	builder, err := NewBoundedBuilder(maxStacks, maxFrames, maxStrings)
	if err != nil {
		return nil, err
	}
	startNs := first.StartNs
	endNs := first.EndNs
	for i, p := range profiles {
		if p == nil {
			return nil, fmt.Errorf("pprof: nil profile at %d", i)
		}
		if p.EventType != first.EventType || p.PeriodNs != first.PeriodNs {
			return nil, fmt.Errorf("pprof: profiles to merge must share event type and period")
		}
		if p.StartNs < startNs {
			startNs = p.StartNs
		}
		if p.EndNs > endNs {
			endNs = p.EndNs
		}
		for _, stack := range p.Stacks {
			frames := make([]Frame, 0, len(stack.FrameIDs))
			for _, id := range stack.FrameIDs {
				if id < 0 || id >= len(p.Frames) {
					return nil, fmt.Errorf("pprof: stack references missing frame %d", id)
				}
				frames = append(frames, p.Frames[id])
			}
			builder.Add(frames, stack.Count, stack.Truncated)
		}
	}
	frames, stacks := builder.Parts()
	labels := maps.Clone(first.Labels)
	if labels == nil {
		labels = map[string]string{}
	}
	return &Profile{
		EventType: first.EventType,
		PeriodNs:  first.PeriodNs,
		StartNs:   startNs,
		EndNs:     endNs,
		Frames:    frames,
		Stacks:    stacks,
		Labels:    labels,
	}, nil
}

// Merge is Merger{} with default caps.
func Merge(profiles []*Profile) (*Profile, error) {
	return (Merger{}).Merge(profiles)
}

// FromSpoolFile decodes a SpoolFile compact-stacks payload into a Profile
// using DefaultDecodeLimits.
func FromSpoolFile(f *spool.File) (*Profile, error) {
	return FromSpoolFileLimited(f, DefaultDecodeLimits())
}

// FromSpoolFileLimited decodes with explicit compact-stacks allocation caps.
func FromSpoolFileLimited(f *spool.File, lim DecodeLimits) (*Profile, error) {
	if f == nil {
		return nil, fmt.Errorf("pprof: nil spool file")
	}
	if f.Format != spool.FormatCompactStacks {
		return nil, fmt.Errorf("pprof: unsupported spool format %q", f.Format)
	}
	frames, stacks, err := DecodeCompactStacksLimited(f.Payload, lim)
	if err != nil {
		return nil, err
	}
	labels := maps.Clone(f.Labels)
	if labels == nil {
		labels = map[string]string{}
	}
	return &Profile{
		EventType: f.EventType,
		PeriodNs:  f.PeriodNs,
		StartNs:   f.StartNs,
		EndNs:     f.EndNs,
		Frames:    frames,
		Stacks:    stacks,
		Labels:    labels,
	}, nil
}

// MergeSpoolFiles decodes and merges SpoolFile members that share event/period.
func MergeSpoolFiles(files []*spool.File) (*Profile, error) {
	profiles := make([]*Profile, 0, len(files))
	for i, f := range files {
		p, err := FromSpoolFile(f)
		if err != nil {
			return nil, fmt.Errorf("pprof: file %d: %w", i, err)
		}
		profiles = append(profiles, p)
	}
	return Merge(profiles)
}
