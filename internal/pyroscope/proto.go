package pyroscope

// Minimal protobuf writer for the field types PushRequest needs (length-delimited
// bytes / embedded messages). Matches Tracewick\Encoder\Pprof\ProtobufWriter behaviour
// for those fields: empty length-delimited values are still written.
type protoWriter struct {
	buf []byte
}

func (w *protoWriter) bytes(field uint64, value []byte) {
	w.buf = append(w.buf, encodeVarint(field<<3|2)...)
	w.buf = append(w.buf, encodeVarint(uint64(len(value)))...)
	w.buf = append(w.buf, value...)
}

func (w *protoWriter) bytesString(field uint64, value string) {
	w.bytes(field, []byte(value))
}

func (w *protoWriter) message(field uint64, inner *protoWriter) {
	w.bytes(field, inner.buf)
}

func encodeVarint(v uint64) []byte {
	var out []byte
	for v > 0x7f {
		out = append(out, byte(v)&0x7f|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}
