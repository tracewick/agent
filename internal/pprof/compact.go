package pprof

import (
	"encoding/binary"
	"fmt"
)

const (
	compactSchemaVersion = 1
	nullIndex            = 0xFFFFFFFF
)

// DecodeCompactStacks parses an ADR-13 compact-stacks payload into frames and stacks.
func DecodeCompactStacks(data []byte) (frames []Frame, stacks []Stack, err error) {
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
	strings := make([]string, 0, stringCount)
	for i := uint32(0); i < stringCount; i++ {
		var length uint32
		length, err = r.u32()
		if err != nil {
			return nil, nil, err
		}
		var s []byte
		s, err = r.bytes(int(length)) //nolint:gosec // length bounded by remaining payload
		if err != nil {
			return nil, nil, err
		}
		strings = append(strings, string(s))
	}
	var stackCount uint32
	stackCount, err = r.u32()
	if err != nil {
		return nil, nil, err
	}
	frameIDs := map[string]int{}
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
		ids := make([]int, 0, frameCount)
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
			key := compactFrameKey(fn, class, file, line, closureRaw)
			id, ok := frameIDs[key]
			if !ok {
				id = len(frames)
				frameIDs[key] = id
				frames = append(frames, frame)
			}
			ids = append(ids, id)
		}
		stacks = append(stacks, Stack{FrameIDs: ids, Count: weight, Truncated: flags&1 != 0})
	}
	if !r.done() {
		return nil, nil, fmt.Errorf("pprof: compact stacks payload has trailing bytes")
	}
	return frames, stacks, nil
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

func compactFrameKey(fn, class, file *string, line, closureRaw uint32) string {
	return deref(fn) + "\x00" + deref(class) + "\x00" + deref(file) + "\x00" +
		fmt.Sprintf("%d\x00%d", line, closureRaw)
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
