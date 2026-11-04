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

// frameMapKey identifies a frame without concatenating string bodies into the map key
// (hostile long function/file names must not explode merge memory).
type frameMapKey struct {
	fn, class, file *string
	line, closure   uint32
	hasClosure      bool
}

// BoundedBuilder builds frames/stacks with transactional admission (BoundedProfileBuilder).
type BoundedBuilder struct {
	maxStacks  int
	maxFrames  int
	maxStrings int

	frameIDs   map[frameMapKey]int
	frames     []Frame
	interned   map[string]*string // unique non-empty strings → stable pointer
	stacks     map[string]stackEntry
	stackOrder []string

	inWeight       uint64
	keptWeight     uint64
	overflowWeight uint64
	capped         bool
}

type stackEntry struct {
	ids       []int
	count     uint64 // wider than wire uint32 so Add cannot wrap silently
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
		frameIDs:   map[frameMapKey]int{},
		interned:   map[string]*string{},
		stacks:     map[string]stackEntry{},
	}, nil
}

// Add admits a stack (outermost first). Weight < 1 is ignored.
func (b *BoundedBuilder) Add(frames []Frame, weight uint32, truncated bool) {
	if weight < 1 {
		return
	}
	b.inWeight += uint64(weight)

	// Pending interns are discarded if this stack overflows — do not grow b.interned early.
	pendingIntern := map[string]*string{}
	intern := func(s *string) *string {
		if s == nil || *s == "" {
			return nil
		}
		if p, ok := b.interned[*s]; ok {
			return p
		}
		if p, ok := pendingIntern[*s]; ok {
			return p
		}
		cp := *s
		pendingIntern[cp] = &cp
		return &cp
	}
	makeKey := func(f Frame) (frameMapKey, Frame) {
		k := frameMapKey{
			fn:    intern(f.Function),
			class: intern(f.Class),
			file:  intern(f.File),
			line:  f.Line,
		}
		canon := Frame{Function: k.fn, Class: k.class, File: k.file, Line: f.Line}
		if f.ClosureLine != nil {
			k.hasClosure = true
			k.closure = *f.ClosureLine
			canon.ClosureLine = f.ClosureLine
		}
		return k, canon
	}

	type pendingFrame struct {
		key   frameMapKey
		id    int
		frame Frame
	}
	var pending []pendingFrame
	pendingByKey := map[frameMapKey]int{}
	ids := make([]int, 0, len(frames))
	nextID := len(b.frames)

	for _, frame := range frames {
		fk, canon := makeKey(frame)
		if id, ok := b.frameIDs[fk]; ok {
			ids = append(ids, id)
			continue
		}
		if idx, ok := pendingByKey[fk]; ok {
			ids = append(ids, pending[idx].id)
			continue
		}
		pendingByKey[fk] = len(pending)
		pending = append(pending, pendingFrame{key: fk, id: nextID, frame: canon})
		ids = append(ids, nextID)
		nextID++
	}

	stackKey := joinIDs(ids)
	if truncated {
		stackKey += ",truncated"
	}
	if e, ok := b.stacks[stackKey]; ok {
		e.count += uint64(weight)
		b.stacks[stackKey] = e
		b.keptWeight += uint64(weight)
		return
	}
	if len(b.stacks)+1 > b.maxStacks ||
		len(b.frames)+len(pending) > b.maxFrames ||
		len(b.interned)+len(pendingIntern) > b.maxStrings {
		b.capped = true
		b.overflowWeight += uint64(weight)
		return
	}
	for s, p := range pendingIntern {
		b.interned[s] = p
	}
	for _, p := range pending {
		b.frameIDs[p.key] = len(b.frames)
		b.frames = append(b.frames, p.frame)
	}
	b.stacks[stackKey] = stackEntry{ids: ids, count: uint64(weight), truncated: truncated}
	b.stackOrder = append(b.stackOrder, stackKey)
	b.keptWeight += uint64(weight)
}

// Parts returns frames and stacks including an overflow frame when needed.
func (b *BoundedBuilder) Parts() (frames []Frame, stacks []Stack) {
	frames = append([]Frame(nil), b.frames...)
	for _, key := range b.stackOrder {
		e := b.stacks[key]
		stacks = append(stacks, Stack{FrameIDs: append([]int(nil), e.ids...), Count: u32sat(e.count), Truncated: e.truncated})
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
func (b *BoundedBuilder) StringCount() int { return len(b.interned) }

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
