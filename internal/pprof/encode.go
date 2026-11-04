package pprof

import (
	"errors"
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

	// DefaultMaxEncodedBytes matches delivery.DefaultMaxAggregatedPprof.
	DefaultMaxEncodedBytes = 16 << 20
	// DefaultMaxNameBytes caps one derived function name before materialization.
	DefaultMaxNameBytes = 64 << 10
)

// ErrEncodeBudget is returned when EncodeLimited would exceed string-table,
// derived-name, or protobuf size budgets (before finishing a giant allocation).
var ErrEncodeBudget = errors.New("pprof: encode budget exceeded")

// EncodeLimits caps resources used while building a pprof blob.
type EncodeLimits struct {
	MaxStringBytes  int // sum of string-table entry lengths
	MaxEncodedBytes int // total protobuf output size
	MaxNameBytes    int // single derived Frame.Name() length
}

// DefaultEncodeLimits returns v1 encode budgets aligned with delivery defaults.
func DefaultEncodeLimits() EncodeLimits {
	return EncodeLimits{
		MaxStringBytes:  DefaultMaxStringBytes,
		MaxEncodedBytes: DefaultMaxEncodedBytes,
		MaxNameBytes:    DefaultMaxNameBytes,
	}
}

// Encode writes an uncompressed pprof profile.proto blob with default budgets.
func Encode(p *Profile) ([]byte, error) {
	return EncodeLimited(p, DefaultEncodeLimits())
}

// EncodeLimited writes pprof bytes while enforcing lim before large allocations.
func EncodeLimited(p *Profile, lim EncodeLimits) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("pprof: nil profile")
	}
	lim = normalizeEncodeLimits(lim)
	budget := &encodeBudget{max: lim.MaxEncodedBytes}
	strings := newStringTable(lim.MaxStringBytes)
	head := &protoWriter{budget: budget}
	periodVT, err := valueType(strings, string(p.EventType), "nanoseconds")
	if err != nil {
		return nil, err
	}
	if wErr := head.bytes(fieldProfileSampleType, periodVT); wErr != nil {
		return nil, wErr
	}

	type funcKey struct {
		nameIdx, fileIdx uint64
	}
	functionIDs := map[funcKey]uint64{}
	functions := &protoWriter{budget: budget}
	locationIDs := map[any]uint64{}
	locations := &protoWriter{budget: budget}

	function := func(name string, file *string, startLine uint32) (uint64, error) {
		if len(name) > lim.MaxNameBytes {
			return 0, fmt.Errorf("%w: function name length %d exceeds %d", ErrEncodeBudget, len(name), lim.MaxNameBytes)
		}
		fileStr := ""
		if file != nil {
			fileStr = *file
		}
		nameIdx, idxErr := strings.index(name)
		if idxErr != nil {
			return 0, idxErr
		}
		fileIdx, idxErr := strings.index(fileStr)
		if idxErr != nil {
			return 0, idxErr
		}
		key := funcKey{nameIdx, fileIdx}
		if id, ok := functionIDs[key]; ok {
			return id, nil
		}
		id := uint64(len(functionIDs) + 1)
		functionIDs[key] = id
		// Nested writers omit budget; the parent section accounts for bytes once.
		body := &protoWriter{}
		_ = body.varint(fieldFunctionID, id)
		_ = body.varint(fieldFunctionName, nameIdx)
		_ = body.varint(fieldFunctionSystemName, nameIdx)
		_ = body.varint(fieldFunctionFilename, fileIdx)
		_ = body.varint(fieldFunctionStartLine, uint64(startLine))
		if bErr := functions.bytes(fieldProfileFunction, body.bytesOut()); bErr != nil {
			return 0, bErr
		}
		return id, nil
	}

	location := func(key any, functionID uint64, line uint32) (uint64, error) {
		if id, ok := locationIDs[key]; ok {
			return id, nil
		}
		id := uint64(len(locationIDs) + 1)
		locationIDs[key] = id
		lineMsg := &protoWriter{}
		_ = lineMsg.varint(fieldLineFunctionID, functionID)
		_ = lineMsg.varint(fieldLineLine, uint64(line))
		body := &protoWriter{}
		_ = body.varint(fieldLocationID, id)
		_ = body.bytes(fieldLocationLine, lineMsg.bytesOut())
		if bErr := locations.bytes(fieldProfileLocation, body.bytesOut()); bErr != nil {
			return 0, bErr
		}
		return id, nil
	}

	locForFrame := func(key any, name string, file *string, line, start uint32) (uint64, error) {
		fnID, fnErr := function(name, file, start)
		if fnErr != nil {
			return 0, fnErr
		}
		return location(key, fnID, line)
	}

	var spanID string
	if p.Labels != nil {
		spanID = p.Labels[sampleLabelSpanID]
	}

	samples := &protoWriter{budget: budget}
	for _, stack := range p.Stacks {
		ids := make([]uint64, 0, len(stack.FrameIDs)+1)
		for _, frameID := range stack.FrameIDs {
			if frameID < 0 || frameID >= len(p.Frames) {
				id, locErr := locForFrame(UnknownFrame, UnknownFrame, nil, 0, 0)
				if locErr != nil {
					return nil, locErr
				}
				ids = append(ids, id)
				continue
			}
			frame := p.Frames[frameID]
			nameLen := frame.EstimatedNameBytes()
			if nameLen > lim.MaxNameBytes {
				return nil, fmt.Errorf("%w: derived name length %d exceeds %d", ErrEncodeBudget, nameLen, lim.MaxNameBytes)
			}
			// Materialize within MaxNameBytes; string-table budget is enforced only
			// when index() inserts a *new* unique entry (reuse costs zero bytes).
			name := frame.Name()
			if len(name) > lim.MaxNameBytes {
				return nil, fmt.Errorf("%w: function name length %d exceeds %d", ErrEncodeBudget, len(name), lim.MaxNameBytes)
			}
			id, locErr := locForFrame(frameID, name, frame.File, frame.Line, startLine(frame))
			if locErr != nil {
				return nil, locErr
			}
			ids = append(ids, id)
		}
		if stack.Truncated {
			id, locErr := locForFrame(TruncatedFrame, TruncatedFrame, nil, 0, 0)
			if locErr != nil {
				return nil, locErr
			}
			ids = append([]uint64{id}, ids...)
		}
		if len(ids) == 0 {
			id, locErr := locForFrame(UnknownFrame, UnknownFrame, nil, 0, 0)
			if locErr != nil {
				return nil, locErr
			}
			ids = append(ids, id)
		}
		// pprof lists locations leaf-first (innermost outwards).
		reversed := make([]uint64, len(ids))
		for i, id := range ids {
			reversed[len(ids)-1-i] = id
		}
		sample := &protoWriter{}
		_ = sample.packed(fieldSampleLocationID, reversed)
		sampleVal, valErr := sampleValue(stack.Count, p.PeriodNs)
		if valErr != nil {
			return nil, valErr
		}
		_ = sample.packed(fieldSampleValue, []uint64{sampleVal})
		if spanID != "" {
			label := &protoWriter{}
			keyIdx, idxErr := strings.index(sampleLabelSpanID)
			if idxErr != nil {
				return nil, idxErr
			}
			strIdx, idxErr := strings.index(spanID)
			if idxErr != nil {
				return nil, idxErr
			}
			_ = label.varint(fieldLabelKey, keyIdx)
			_ = label.varint(fieldLabelStr, strIdx)
			_ = sample.bytes(fieldSampleLabel, label.bytesOut())
		}
		if bErr := samples.bytes(fieldProfileSample, sample.bytesOut()); bErr != nil {
			return nil, bErr
		}
	}

	tail := &protoWriter{budget: budget}
	for _, s := range strings.all() {
		if bErr := tail.bytes(fieldProfileStringTable, []byte(s)); bErr != nil {
			return nil, bErr
		}
	}
	if vErr := tail.varint(fieldProfileTimeNanos, u64bits(p.StartNs)); vErr != nil {
		return nil, vErr
	}
	if vErr := tail.varint(fieldProfileDurationNs, u64bits(p.DurationNs())); vErr != nil {
		return nil, vErr
	}
	if bErr := tail.bytes(fieldProfilePeriodType, periodVT); bErr != nil {
		return nil, bErr
	}
	if vErr := tail.varint(fieldProfilePeriod, u64bits(p.PeriodNs)); vErr != nil {
		return nil, vErr
	}

	out := head.bytesOut()
	out = append(out, samples.bytesOut()...)
	out = append(out, locations.bytesOut()...)
	out = append(out, functions.bytesOut()...)
	out = append(out, tail.bytesOut()...)
	return out, nil
}

func normalizeEncodeLimits(lim EncodeLimits) EncodeLimits {
	if lim.MaxStringBytes <= 0 {
		lim.MaxStringBytes = DefaultMaxStringBytes
	}
	if lim.MaxEncodedBytes <= 0 {
		lim.MaxEncodedBytes = DefaultMaxEncodedBytes
	}
	if lim.MaxNameBytes <= 0 {
		lim.MaxNameBytes = DefaultMaxNameBytes
	}
	return lim
}

func startLine(frame Frame) uint32 {
	if frame.ClosureLine == nil {
		return 0
	}
	return *frame.ClosureLine
}

func valueType(strings *stringTable, typ, unit string) ([]byte, error) {
	w := &protoWriter{} // unbudgeted scratch; counted when copied into the profile head/tail
	typeIdx, err := strings.index(typ)
	if err != nil {
		return nil, err
	}
	unitIdx, err := strings.index(unit)
	if err != nil {
		return nil, err
	}
	_ = w.varint(fieldValueTypeType, typeIdx)
	_ = w.varint(fieldValueTypeUnit, unitIdx)
	return w.bytesOut(), nil
}

// u64bits reinterprets a signed int64 as the unsigned bit pattern protobuf expects.
func u64bits(v int64) uint64 {
	return uint64(v) //nolint:gosec // intentional two's-complement wire encoding
}

// sampleValue multiplies stack weight by period for pprof sample.value (int64).
// Rejects products that would exceed math.MaxInt64 (unsigned wrap or signed negation).
func sampleValue(count uint64, periodNs int64) (uint64, error) {
	if periodNs < 0 {
		return 0, fmt.Errorf("pprof: period_ns must be non-negative, got %d", periodNs)
	}
	if count == 0 || periodNs == 0 {
		return 0, nil
	}
	period := uint64(periodNs) //nolint:gosec // periodNs >= 0
	const maxInt64 = ^uint64(0) >> 1
	if count > maxInt64/period {
		return 0, fmt.Errorf("pprof: sample value overflow: count %d × period %d exceeds int64", count, periodNs)
	}
	return count * period, nil
}

type encodeBudget struct {
	max int
	n   int
}

func (b *encodeBudget) add(n int) error {
	if b == nil || b.max <= 0 {
		return nil
	}
	if n < 0 || b.n+n > b.max {
		return fmt.Errorf("%w: protobuf buffer would exceed %d bytes", ErrEncodeBudget, b.max)
	}
	b.n += n
	return nil
}

type stringTable struct {
	strings  []string
	indexOf  map[string]uint64
	byteLen  int
	maxBytes int
}

func newStringTable(maxBytes int) *stringTable {
	return &stringTable{
		strings:  []string{""},
		indexOf:  map[string]uint64{"": 0},
		maxBytes: maxBytes,
	}
}

func (t *stringTable) remaining() int {
	if t.maxBytes <= 0 {
		return 1 << 30
	}
	left := t.maxBytes - t.byteLen
	if left < 0 {
		return 0
	}
	return left
}

func (t *stringTable) index(value string) (uint64, error) {
	if id, ok := t.indexOf[value]; ok {
		return id, nil
	}
	if t.maxBytes > 0 && t.byteLen+len(value) > t.maxBytes {
		return 0, fmt.Errorf("%w: string table would exceed %d bytes", ErrEncodeBudget, t.maxBytes)
	}
	id := uint64(len(t.strings)) //nolint:gosec // string table stays well below 2^64
	t.indexOf[value] = id
	t.strings = append(t.strings, value)
	t.byteLen += len(value)
	return id, nil
}

func (t *stringTable) all() []string { return t.strings }

type protoWriter struct {
	buf    []byte
	budget *encodeBudget
}

func (w *protoWriter) bytesOut() []byte { return w.buf }

func (w *protoWriter) varint(field int, value uint64) error {
	if value == 0 {
		return nil
	}
	return w.appendRaw(appendVarint(appendVarint(nil, wireTag(field, 0)), value))
}

func (w *protoWriter) bytes(field int, value []byte) error {
	hdr := appendVarint(appendVarint(nil, wireTag(field, 2)), uint64(len(value))) //nolint:gosec // length
	if err := w.appendRaw(hdr); err != nil {
		return err
	}
	return w.appendRaw(value)
}

func (w *protoWriter) packed(field int, values []uint64) error {
	if len(values) == 0 {
		return nil
	}
	payload := make([]byte, 0, len(values)*2)
	for _, v := range values {
		payload = appendVarint(payload, v)
	}
	return w.bytes(field, payload)
}

func (w *protoWriter) appendRaw(b []byte) error {
	if err := w.budget.add(len(b)); err != nil {
		return err
	}
	w.buf = append(w.buf, b...)
	return nil
}

func wireTag(field, wire int) uint64 {
	return uint64(field<<3 | wire) //nolint:gosec // protobuf field numbers are tiny constants
}

func appendVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}
