package pprof

import (
	"fmt"
)

const (
	// MediaType is the Content-Type for uncompressed pprof protobuf (ADR 4).
	MediaType = "application/x-protobuf"

	TruncatedFrame = "[truncated]"
	UnknownFrame   = "[unknown]"

	sampleLabelSpanID = "span_id"

	fieldProfileSampleType  = 1
	fieldProfileSample      = 2
	fieldProfileLocation    = 4
	fieldProfileFunction    = 5
	fieldProfileStringTable = 6
	fieldProfileTimeNanos   = 9
	fieldProfileDurationNs  = 10
	fieldProfilePeriodType  = 11
	fieldProfilePeriod      = 12

	fieldValueTypeType = 1
	fieldValueTypeUnit = 2

	fieldSampleLocationID = 1
	fieldSampleValue      = 2
	fieldSampleLabel      = 3

	fieldLabelKey = 1
	fieldLabelStr = 2

	fieldLocationID   = 1
	fieldLocationLine = 4

	fieldLineFunctionID = 1
	fieldLineLine       = 2

	fieldFunctionID         = 1
	fieldFunctionName       = 2
	fieldFunctionSystemName = 3
	fieldFunctionFilename   = 4
	fieldFunctionStartLine  = 5
)

// Encode writes an uncompressed pprof profile.proto blob (PprofEncoder).
func Encode(p *Profile) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("pprof: nil profile")
	}
	strings := newStringTable()
	head := &protoWriter{}
	head.bytes(fieldProfileSampleType, valueType(strings, string(p.EventType), "nanoseconds"))

	functionIDs := map[string]uint64{}
	functions := &protoWriter{}
	locationIDs := map[any]uint64{}
	locations := &protoWriter{}

	function := func(name string, file *string, startLine uint32) uint64 {
		fileStr := ""
		if file != nil {
			fileStr = *file
		}
		key := name + "\x00" + fileStr
		if id, ok := functionIDs[key]; ok {
			return id
		}
		id := uint64(len(functionIDs) + 1)
		functionIDs[key] = id
		body := &protoWriter{}
		body.varint(fieldFunctionID, id)
		body.varint(fieldFunctionName, strings.index(name))
		body.varint(fieldFunctionSystemName, strings.index(name))
		body.varint(fieldFunctionFilename, strings.index(fileStr))
		body.varint(fieldFunctionStartLine, uint64(startLine))
		functions.bytes(fieldProfileFunction, body.bytesOut())
		return id
	}

	location := func(key any, functionID uint64, line uint32) uint64 {
		if id, ok := locationIDs[key]; ok {
			return id
		}
		id := uint64(len(locationIDs) + 1)
		locationIDs[key] = id
		lineMsg := &protoWriter{}
		lineMsg.varint(fieldLineFunctionID, functionID)
		lineMsg.varint(fieldLineLine, uint64(line))
		body := &protoWriter{}
		body.varint(fieldLocationID, id)
		body.bytes(fieldLocationLine, lineMsg.bytesOut())
		locations.bytes(fieldProfileLocation, body.bytesOut())
		return id
	}

	var spanID string
	if p.Labels != nil {
		spanID = p.Labels[sampleLabelSpanID]
	}

	samples := &protoWriter{}
	for _, stack := range p.Stacks {
		ids := make([]uint64, 0, len(stack.FrameIDs)+1)
		for _, frameID := range stack.FrameIDs {
			if frameID < 0 || frameID >= len(p.Frames) {
				ids = append(ids, location(UnknownFrame, function(UnknownFrame, nil, 0), 0))
				continue
			}
			frame := p.Frames[frameID]
			ids = append(ids, location(frameID, function(frame.Name(), frame.File, startLine(frame)), frame.Line))
		}
		if stack.Truncated {
			ids = append([]uint64{location(TruncatedFrame, function(TruncatedFrame, nil, 0), 0)}, ids...)
		}
		if len(ids) == 0 {
			ids = append(ids, location(UnknownFrame, function(UnknownFrame, nil, 0), 0))
		}
		// pprof lists locations leaf-first (innermost outwards).
		reversed := make([]uint64, len(ids))
		for i, id := range ids {
			reversed[len(ids)-1-i] = id
		}
		sample := &protoWriter{}
		sample.packed(fieldSampleLocationID, reversed)
		sample.packed(fieldSampleValue, []uint64{uint64(stack.Count) * u64bits(p.PeriodNs)})
		if spanID != "" {
			label := &protoWriter{}
			label.varint(fieldLabelKey, strings.index(sampleLabelSpanID))
			label.varint(fieldLabelStr, strings.index(spanID))
			sample.bytes(fieldSampleLabel, label.bytesOut())
		}
		samples.bytes(fieldProfileSample, sample.bytesOut())
	}

	tail := &protoWriter{}
	periodType := valueType(strings, string(p.EventType), "nanoseconds")
	for _, s := range strings.all() {
		tail.bytes(fieldProfileStringTable, []byte(s))
	}
	tail.varint(fieldProfileTimeNanos, u64bits(p.StartNs))
	tail.varint(fieldProfileDurationNs, u64bits(p.DurationNs()))
	tail.bytes(fieldProfilePeriodType, periodType)
	tail.varint(fieldProfilePeriod, u64bits(p.PeriodNs))

	out := head.bytesOut()
	out = append(out, samples.bytesOut()...)
	out = append(out, locations.bytesOut()...)
	out = append(out, functions.bytesOut()...)
	out = append(out, tail.bytesOut()...)
	return out, nil
}

func startLine(frame Frame) uint32 {
	if frame.ClosureLine == nil {
		return 0
	}
	return *frame.ClosureLine
}

func valueType(strings *stringTable, typ, unit string) []byte {
	w := &protoWriter{}
	w.varint(fieldValueTypeType, strings.index(typ))
	w.varint(fieldValueTypeUnit, strings.index(unit))
	return w.bytesOut()
}

// u64bits reinterprets a signed int64 as the unsigned bit pattern protobuf expects.
func u64bits(v int64) uint64 {
	return uint64(v) //nolint:gosec // intentional two's-complement wire encoding
}

type stringTable struct {
	strings []string
	indexOf map[string]uint64
}

func newStringTable() *stringTable {
	return &stringTable{
		strings: []string{""},
		indexOf: map[string]uint64{"": 0},
	}
}

func (t *stringTable) index(value string) uint64 {
	if id, ok := t.indexOf[value]; ok {
		return id
	}
	id := uint64(len(t.strings)) //nolint:gosec // string table stays well below 2^64
	t.indexOf[value] = id
	t.strings = append(t.strings, value)
	return id
}

func (t *stringTable) all() []string { return t.strings }

type protoWriter struct {
	buf []byte
}

func (w *protoWriter) bytesOut() []byte { return w.buf }

func (w *protoWriter) varint(field int, value uint64) {
	if value == 0 {
		return
	}
	w.buf = appendVarint(w.buf, wireTag(field, 0))
	w.buf = appendVarint(w.buf, value)
}

func (w *protoWriter) bytes(field int, value []byte) {
	w.buf = appendVarint(w.buf, wireTag(field, 2))
	w.buf = appendVarint(w.buf, uint64(len(value))) //nolint:gosec // length of in-memory buffer
	w.buf = append(w.buf, value...)
}

func wireTag(field, wire int) uint64 {
	return uint64(field<<3 | wire) //nolint:gosec // protobuf field numbers are tiny constants
}

func (w *protoWriter) packed(field int, values []uint64) {
	if len(values) == 0 {
		return
	}
	payload := make([]byte, 0, len(values)*2)
	for _, v := range values {
		payload = appendVarint(payload, v)
	}
	w.bytes(field, payload)
}

func appendVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}
