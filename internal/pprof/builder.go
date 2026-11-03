package pprof

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	OverflowFunction  = "[overflow]"
	DefaultMaxStacks  = 4096
	DefaultMaxFrames  = 16384
	DefaultMaxStrings = 32768
	minCap            = 1
	maxStacksCap      = 100_000
	maxFramesCap      = 500_000
	maxStringsCap     = 1_000_000
)

// BoundedBuilder builds frames/stacks with transactional admission (BoundedProfileBuilder).
type BoundedBuilder struct {
	maxStacks  int
	maxFrames  int
	maxStrings int

	frameIDs   map[string]int
	frames     []Frame
	strings    map[string]struct{}
	stacks     map[string]stackEntry
	stackOrder []string

	inWeight       uint64
	keptWeight     uint64
	overflowWeight uint64
	capped         bool
}

type stackEntry struct {
	ids       []int
	count     uint32
	truncated bool
}

// NewBoundedBuilder validates caps. Passing 0,0,0 selects the PHP defaults;
// any other zero is rejected (caps must be between min and max inclusive).
func NewBoundedBuilder(maxStacks, maxFrames, maxStrings int) (*BoundedBuilder, error) {
	if maxStacks == 0 && maxFrames == 0 && maxStrings == 0 {
		maxStacks = DefaultMaxStacks
		maxFrames = DefaultMaxFrames
		maxStrings = DefaultMaxStrings
	}
	if maxStacks < minCap || maxStacks > maxStacksCap {
		return nil, fmt.Errorf("pprof: max stacks must be between %d and %d, got %d", minCap, maxStacksCap, maxStacks)
	}
	if maxFrames < minCap || maxFrames > maxFramesCap {
		return nil, fmt.Errorf("pprof: max frames must be between %d and %d, got %d", minCap, maxFramesCap, maxFrames)
	}
	if maxStrings < minCap || maxStrings > maxStringsCap {
		return nil, fmt.Errorf("pprof: max strings must be between %d and %d, got %d", minCap, maxStringsCap, maxStrings)
	}
	return &BoundedBuilder{
		maxStacks:  maxStacks,
		maxFrames:  maxFrames,
		maxStrings: maxStrings,
		frameIDs:   map[string]int{},
		strings:    map[string]struct{}{},
		stacks:     map[string]stackEntry{},
	}, nil
}

// Add admits a stack (outermost first). Weight < 1 is ignored.
func (b *BoundedBuilder) Add(frames []Frame, weight uint32, truncated bool) {
	if weight < 1 {
		return
	}
	b.inWeight += uint64(weight)

	type pendingFrame struct {
		key   string
		id    int
		frame Frame
	}
	var pending []pendingFrame
	pendingByKey := map[string]int{}
	pendingStrings := map[string]struct{}{}
	ids := make([]int, 0, len(frames))
	nextID := len(b.frames)

	for _, frame := range frames {
		fk := frame.key()
		if id, ok := b.frameIDs[fk]; ok {
			ids = append(ids, id)
			continue
		}
		if idx, ok := pendingByKey[fk]; ok {
			ids = append(ids, pending[idx].id)
			continue
		}
		pendingByKey[fk] = len(pending)
		pending = append(pending, pendingFrame{key: fk, id: nextID, frame: frame})
		ids = append(ids, nextID)
		nextID++
		for _, s := range []*string{frame.Function, frame.Class, frame.File} {
			if s == nil || *s == "" {
				continue
			}
			if _, ok := b.strings[*s]; ok {
				continue
			}
			if _, ok := pendingStrings[*s]; ok {
				continue
			}
			pendingStrings[*s] = struct{}{}
		}
	}

	stackKey := joinIDs(ids)
	if truncated {
		stackKey += ",truncated"
	}
	if e, ok := b.stacks[stackKey]; ok {
		e.count += weight
		b.stacks[stackKey] = e
		b.keptWeight += uint64(weight)
		return
	}
	if len(b.stacks)+1 > b.maxStacks ||
		len(b.frames)+len(pending) > b.maxFrames ||
		len(b.strings)+len(pendingStrings) > b.maxStrings {
		b.capped = true
		b.overflowWeight += uint64(weight)
		return
	}
	for _, p := range pending {
		b.frameIDs[p.key] = len(b.frames)
		b.frames = append(b.frames, p.frame)
	}
	for s := range pendingStrings {
		b.strings[s] = struct{}{}
	}
	b.stacks[stackKey] = stackEntry{ids: ids, count: weight, truncated: truncated}
	b.stackOrder = append(b.stackOrder, stackKey)
	b.keptWeight += uint64(weight)
}

// Parts returns frames and stacks including an overflow frame when needed.
func (b *BoundedBuilder) Parts() (frames []Frame, stacks []Stack) {
	frames = append([]Frame(nil), b.frames...)
	for _, key := range b.stackOrder {
		e := b.stacks[key]
		stacks = append(stacks, Stack{FrameIDs: append([]int(nil), e.ids...), Count: e.count, Truncated: e.truncated})
	}
	if b.overflowWeight > 0 {
		overflowID := len(frames)
		fn := OverflowFunction
		frames = append(frames, Frame{Function: &fn})
		stacks = append(stacks, Stack{FrameIDs: []int{overflowID}, Count: u32sat(b.overflowWeight), Truncated: true})
	}
	return frames, stacks
}

// WasCapped reports whether any weight was diverted to overflow.
func (b *BoundedBuilder) WasCapped() bool { return b.capped }

// InWeight is the sum of admitted sample weights (including overflow).
func (b *BoundedBuilder) InWeight() uint64 { return b.inWeight }

// KeptWeight is weight retained in distinct stacks.
func (b *BoundedBuilder) KeptWeight() uint64 { return b.keptWeight }

// OverflowWeight is weight diverted to the overflow stack.
func (b *BoundedBuilder) OverflowWeight() uint64 { return b.overflowWeight }

// IsEmpty reports whether no stacks (including overflow) were kept.
func (b *BoundedBuilder) IsEmpty() bool {
	return len(b.stacks) == 0 && b.overflowWeight == 0
}

// UniqueStackCount includes the overflow stack when present.
func (b *BoundedBuilder) UniqueStackCount() int {
	n := len(b.stacks)
	if b.overflowWeight > 0 {
		n++
	}
	return n
}

// FrameCount is committed unique frames (overflow frame is not counted until Parts).
func (b *BoundedBuilder) FrameCount() int { return len(b.frames) }

// StringCount is committed unique non-empty frame strings.
func (b *BoundedBuilder) StringCount() int { return len(b.strings) }

func u32sat(v uint64) uint32 {
	if v > uint64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(v) //nolint:gosec // range-checked above
}

func joinIDs(ids []int) string {
	if len(ids) == 0 {
		return ""
	}
	var b strings.Builder
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(id))
	}
	return b.String()
}
