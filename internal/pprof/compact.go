package pprof

import (
	"encoding/binary"
	"fmt"
)

const (
	compactSchemaVersion = 1
	nullIndex            = 0xFFFFFFFF
	// Minimum on-wire sizes used to reject malicious counts before allocation.
	compactMinStringRecord = 4  // u32 length prefix
	compactMinFrameRecord  = 20 // 5 × u32
	// MaxOverflowStacks / MaxOverflowFrames / MaxOverflowStrings are the extra
	// wire entries PHP BoundedProfileBuilder::toProfileParts may append beyond
	// the corresponding builder caps (one [overflow] stack + frame + string).
	MaxOverflowStacks  = 1
	MaxOverflowFrames  = 1
	MaxOverflowStrings = 1
)

// DecodeLimits caps compact-stacks decode resource use (before BoundedBuilder).
//
// MaxStacks / MaxFrames / MaxStrings are builder/merge regular budgets (PHP
// defaults). The wire may declare one extra stack, unique frame, and string for
// the [overflow] sentinel. MaxFrameRefs caps total frame references;
// MaxStringBytes caps the string table payload size.
type DecodeLimits struct {
	MaxStacks      int
	MaxFrames      int
	MaxStrings     int
	MaxStringBytes int
	MaxFrameRefs   int
}

// DefaultDecodeLimits matches BoundedBuilder defaults plus wire overflow headroom
// and string-byte / frame-ref budgets.
func DefaultDecodeLimits() DecodeLimits {
	return DecodeLimits{
		MaxStacks:      DefaultMaxStacks,
		MaxFrames:      DefaultMaxFrames,
		MaxStrings:     DefaultMaxStrings,
		MaxStringBytes: DefaultMaxStringBytes,
		MaxFrameRefs:   DefaultMaxFrameRefs,
	}
}

const (
	DefaultMaxStringBytes = 1 << 20 // 1 MiB string table
	DefaultMaxFrameRefs   = 1 << 20 // total frame slots across stacks
)

// WireStackBudget is how many stacks a compact payload may declare for lim.
func WireStackBudget(lim DecodeLimits) int {
	maxStacks := lim.MaxStacks
	if maxStacks <= 0 {
		maxStacks = DefaultMaxStacks
	}
	return maxStacks + MaxOverflowStacks
}

// WireFrameBudget is how many unique frames a compact payload may carry for lim.
func WireFrameBudget(lim DecodeLimits) int {
	maxFrames := lim.MaxFrames
	if maxFrames <= 0 {
		maxFrames = DefaultMaxFrames
	}
	return maxFrames + MaxOverflowFrames
}

// WireStringBudget is how many string-table entries a compact payload may declare.
func WireStringBudget(lim DecodeLimits) int {
	maxStrings := lim.MaxStrings
	if maxStrings <= 0 {
		maxStrings = DefaultMaxStrings
	}
	return maxStrings + MaxOverflowStrings
}

// DecodeCompactStacks parses an ADR-13 compact-stacks payload with default caps.
func DecodeCompactStacks(data []byte) (frames []Frame, stacks []Stack, err error) {
	return DecodeCompactStacksLimited(data, DefaultDecodeLimits())
}

// DecodeCompactStacksLimited parses compact-stacks and refuses counts that would
// allocate beyond lim or beyond the remaining payload (hostile spool files).
func DecodeCompactStacksLimited(data []byte, lim DecodeLimits) (frames []Frame, stacks []Stack, err error) {
	lim = normalizeDecodeLimits(lim)
	r := &byteReader{data: data}
	schema, err := r.byte()
	if err != nil {
		return nil, nil, err
	}
	if schema != compactSchemaVersion {
		return nil, nil, fmt.Errorf("pprof: compact stacks schema version %d is not supported", schema)
	}
	stringCount, err := r.u32()
	if err != nil {
		return nil, nil, err
	}
	if rcErr := rejectCount("strings", stringCount, WireStringBudget(lim), r.remaining(), compactMinStringRecord); rcErr != nil {
		return nil, nil, rcErr
	}
	strings := make([]string, 0, int(stringCount)) //nolint:gosec // bounded by rejectCount
	var stringBytes int
	for i := uint32(0); i < stringCount; i++ {
		var length uint32
		length, err = r.u32()
		if err != nil {
			return nil, nil, err
		}
		if int(length) > lim.MaxStringBytes-stringBytes { //nolint:gosec // length vs remaining budget
			return nil, nil, fmt.Errorf("pprof: compact stacks string table exceeds %d byte limit", lim.MaxStringBytes)
		}
		var s []byte
		s, err = r.bytes(int(length)) //nolint:gosec // length bounded by remaining payload
		if err != nil {
			return nil, nil, err
		}
		stringBytes += len(s)
		strings = append(strings, string(s))
	}
	var stackCount uint32
	stackCount, err = r.u32()
	if err != nil {
		return nil, nil, err
	}
	wireBudget := WireStackBudget(lim)
	if int(stackCount) > wireBudget { //nolint:gosec // stackCount fits int after bound check intent
		return nil, nil, fmt.Errorf("pprof: compact stacks declares %d stacks; wire limit is %d (max stacks %d + overflow)", stackCount, wireBudget, lim.MaxStacks)
	}
	// frameWireKey dedupes by string-table indices — never concatenates string bodies into map keys.
	type frameWireKey struct {
		fn, class, file, line, closure uint32
	}
	frameIDs := map[frameWireKey]int{}
	wireFrames := WireFrameBudget(lim)
	var frameRefs int
	for s := uint32(0); s < stackCount; s++ {
		var weight uint32
		weight, err = r.u32()
		if err != nil {
			return nil, nil, err
		}
		var flags byte
		flags, err = r.byte()
		if err != nil {
			return nil, nil, err
		}
		var frameCount uint32
		frameCount, err = r.u32()
		if err != nil {
			return nil, nil, err
		}
		// Per-stack frame count still bounded by unique-frame wire budget and remaining bytes.
		if rcErr := rejectCount("frames", frameCount, wireFrames, r.remaining(), compactMinFrameRecord); rcErr != nil {
			return nil, nil, rcErr
		}
		if frameRefs+int(frameCount) > lim.MaxFrameRefs { //nolint:gosec // frameCount bounded above
			return nil, nil, fmt.Errorf("pprof: compact stacks exceeds %d total frame references", lim.MaxFrameRefs)
		}
		ids := make([]int, 0, int(frameCount)) //nolint:gosec // bounded by rejectCount
		for f := uint32(0); f < frameCount; f++ {
			var fnIdx, classIdx, fileIdx, line, closureRaw uint32
			fnIdx, err = r.u32()
			if err != nil {
				return nil, nil, err
			}
			classIdx, err = r.u32()
			if err != nil {
				return nil, nil, err
			}
			fileIdx, err = r.u32()
			if err != nil {
				return nil, nil, err
			}
			line, err = r.u32()
			if err != nil {
				return nil, nil, err
			}
			closureRaw, err = r.u32()
			if err != nil {
				return nil, nil, err
			}
			wk := frameWireKey{fnIdx, classIdx, fileIdx, line, closureRaw}
			id, ok := frameIDs[wk]
			if !ok {
				if len(frames) >= wireFrames {
					return nil, nil, fmt.Errorf("pprof: compact stacks exceeds %d unique frames (max frames %d + overflow)", wireFrames, lim.MaxFrames)
				}
				var fn, class, file *string
				fn, err = optionalString(strings, fnIdx)
				if err != nil {
					return nil, nil, err
				}
				class, err = optionalString(strings, classIdx)
				if err != nil {
					return nil, nil, err
				}
				file, err = optionalString(strings, fileIdx)
				if err != nil {
					return nil, nil, err
				}
				frame := Frame{Function: fn, Class: class, File: file, Line: line}
				if closureRaw != nullIndex {
					frame.ClosureLine = u32Ptr(closureRaw)
				}
				id = len(frames)
				frameIDs[wk] = id
				frames = append(frames, frame)
			}
			ids = append(ids, id)
			frameRefs++
		}
		stacks = append(stacks, Stack{FrameIDs: ids, Count: weight, Truncated: flags&1 != 0})
	}
	if !r.done() {
		return nil, nil, fmt.Errorf("pprof: compact stacks payload has trailing bytes")
	}
	return frames, stacks, nil
}

func normalizeDecodeLimits(lim DecodeLimits) DecodeLimits {
	if lim.MaxStacks <= 0 {
		lim.MaxStacks = DefaultMaxStacks
	}
	if lim.MaxFrames <= 0 {
		lim.MaxFrames = DefaultMaxFrames
	}
	if lim.MaxStrings <= 0 {
		lim.MaxStrings = DefaultMaxStrings
	}
	if lim.MaxStringBytes <= 0 {
		lim.MaxStringBytes = DefaultMaxStringBytes
	}
	if lim.MaxFrameRefs <= 0 {
		lim.MaxFrameRefs = DefaultMaxFrameRefs
	}
	return lim
}

// EncodeCompactStacks writes frames/stacks as ADR-13 compact-stacks bytes.
func EncodeCompactStacks(frames []Frame, stacks []Stack) ([]byte, error) {
	strings := []string{}
	index := map[string]uint32{}
	indexOf := func(value *string) uint32 {
		if value == nil {
			return nullIndex
		}
		if id, ok := index[*value]; ok {
			return id
		}
		id := u32len(len(strings))
		strings = append(strings, *value)
		index[*value] = id
		return id
	}
	frameRecords := make([][5]uint32, len(frames))
	for i, frame := range frames {
		cl := uint32(nullIndex)
		if frame.ClosureLine != nil {
			cl = *frame.ClosureLine
		}
		frameRecords[i] = [5]uint32{
			indexOf(frame.Function),
			indexOf(frame.Class),
			indexOf(frame.File),
			frame.Line,
			cl,
		}
	}
	out := []byte{compactSchemaVersion}
	out = appendU32(out, u32len(len(strings)))
	for _, s := range strings {
		out = appendU32(out, u32len(len(s)))
		out = append(out, s...)
	}
	out = appendU32(out, u32len(len(stacks)))
	for _, stack := range stacks {
		out = appendU32(out, stack.Count)
		flag := byte(0)
		if stack.Truncated {
			flag = 1
		}
		out = append(out, flag)
		out = appendU32(out, u32len(len(stack.FrameIDs)))
		for _, frameID := range stack.FrameIDs {
			if frameID < 0 || frameID >= len(frameRecords) {
				return nil, fmt.Errorf("pprof: stack references missing frame %d", frameID)
			}
			rec := frameRecords[frameID]
			out = appendU32(out, rec[0], rec[1], rec[2], rec[3], rec[4])
		}
	}
	return out, nil
}

func optionalString(strings []string, index uint32) (*string, error) {
	if index == nullIndex {
		return nil, nil
	}
	if int(index) >= len(strings) {
		return nil, fmt.Errorf("pprof: string index %d is out of range", index)
	}
	s := strings[index]
	return &s, nil
}

func appendU32(out []byte, values ...uint32) []byte {
	var buf [4]byte
	for _, v := range values {
		binary.BigEndian.PutUint32(buf[:], v)
		out = append(out, buf[:]...)
	}
	return out
}

func u32len(n int) uint32 {
	if n < 0 {
		return 0
	}
	if uint64(n) > uint64(^uint32(0)) { //nolint:gosec // n >= 0
		return ^uint32(0)
	}
	return uint32(n) //nolint:gosec // range-checked above
}

type byteReader struct {
	data []byte
	off  int
}

func (r *byteReader) done() bool { return r.off == len(r.data) }

func (r *byteReader) remaining() int { return len(r.data) - r.off }

func rejectCount(kind string, count uint32, limit, remaining, minPer int) error {
	if int(count) > limit { //nolint:gosec // count compared to configured limit
		return fmt.Errorf("pprof: compact stacks declares %d %s; limit is %d", count, kind, limit)
	}
	need := int64(count) * int64(minPer)
	if need > int64(remaining) {
		return fmt.Errorf("pprof: compact stacks declares %d %s but only %d bytes remain", count, kind, remaining)
	}
	return nil
}

func (r *byteReader) byte() (byte, error) {
	if r.off >= len(r.data) {
		return 0, fmt.Errorf("pprof: compact stacks payload ends unexpectedly")
	}
	b := r.data[r.off]
	r.off++
	return b, nil
}

func (r *byteReader) u32() (uint32, error) {
	b, err := r.bytes(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b), nil
}

func (r *byteReader) bytes(n int) ([]byte, error) {
	if n < 0 || r.off+n > len(r.data) {
		return nil, fmt.Errorf("pprof: compact stacks payload ends unexpectedly")
	}
	b := r.data[r.off : r.off+n]
	r.off += n
	return b, nil
}
