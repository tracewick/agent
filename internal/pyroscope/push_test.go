package pyroscope_test

import (
	"regexp"
	"testing"

	"github.com/tracewick/agent/internal/pyroscope"
	"github.com/tracewick/agent/internal/spool"
)

func TestEncodeBuildsPushRequestWithSeriesLabelsAndRawProfile(t *testing.T) {
	t.Parallel()

	body, err := pyroscope.Encode("blog", spool.EventWall, map[string]string{
		"http.route": "/posts/{id}",
		"span_id":    "00f067aa0ba902b7",
		"region":     "eu",
	}, []byte("pprof\x00bytes"), "734fd599-6865-419e-9475-932762d8f469")
	if err != nil {
		t.Fatal(err)
	}

	labels, raw, id := decodePush(t, body)
	want := map[string]string{
		"__name__":      "wall",
		"service_name":  "blog",
		"pyroscope_spy": "tracewick",
		"http_route":    "/posts/{id}",
		"region":        "eu",
	}
	if len(labels) != len(want) {
		t.Fatalf("labels = %#v", labels)
	}
	for k, v := range want {
		if labels[k] != v {
			t.Fatalf("label %q = %q want %q (all %#v)", k, labels[k], v, labels)
		}
	}
	if _, ok := labels["span_id"]; ok {
		t.Fatal("span_id must not be a series label")
	}
	if string(raw) != "pprof\x00bytes" {
		t.Fatalf("raw = %q", raw)
	}
	if id != "734fd599-6865-419e-9475-932762d8f469" {
		t.Fatalf("id = %q", id)
	}
}

func TestEncodeMapsCPUToProcessCPU(t *testing.T) {
	t.Parallel()

	name, err := pyroscope.SeriesName(spool.EventCPU)
	if err != nil || name != "process_cpu" {
		t.Fatalf("cpu series = %q %v", name, err)
	}
	wall, err := pyroscope.SeriesName(spool.EventWall)
	if err != nil || wall != "wall" {
		t.Fatalf("wall series = %q %v", wall, err)
	}

	body, err := pyroscope.Encode("app", spool.EventCPU, nil, []byte("x"), "id")
	if err != nil {
		t.Fatal(err)
	}
	labels, _, _ := decodePush(t, body)
	if labels["__name__"] != "process_cpu" {
		t.Fatalf("__name__ = %q", labels["__name__"])
	}
}

func TestNewSampleIDIsUUIDv4(t *testing.T) {
	t.Parallel()

	id, err := pyroscope.NewSampleID()
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !re.MatchString(id) {
		t.Fatalf("id = %q", id)
	}
}

func TestEncodeRejectsEmptyApplicationAndUnknownEvent(t *testing.T) {
	t.Parallel()

	if _, err := pyroscope.Encode("", spool.EventWall, nil, nil, "id"); err == nil {
		t.Fatal("expected empty application error")
	}
	if _, err := pyroscope.Encode("app", spool.EventType("gpu"), nil, nil, "id"); err == nil {
		t.Fatal("expected unknown event error")
	}
	if _, err := pyroscope.Encode("app", spool.EventWall, nil, nil, ""); err == nil {
		t.Fatal("expected empty sample id error")
	}
}

func TestLabelKeyReplacesDots(t *testing.T) {
	t.Parallel()
	if got := pyroscope.LabelKey("http.route"); got != "http_route" {
		t.Fatalf("got %q", got)
	}
}

func decodePush(t *testing.T, body []byte) (labels map[string]string, raw []byte, id string) {
	t.Helper()
	msgs := protoMessages(t, body, 1)
	if len(msgs) != 1 {
		t.Fatalf("want 1 series, got %d", len(msgs))
	}
	series := msgs[0]
	labels = map[string]string{}
	for _, label := range protoMessages(t, series, 1) {
		labels[protoString(t, label, 1)] = protoString(t, label, 2)
	}
	samples := protoMessages(t, series, 2)
	if len(samples) != 1 {
		t.Fatalf("want 1 sample, got %d", len(samples))
	}
	raw = protoBytes(t, samples[0], 1)
	id = protoString(t, samples[0], 2)
	return labels, raw, id
}

func protoMessages(t *testing.T, buf []byte, field int) [][]byte {
	t.Helper()
	var out [][]byte
	for len(buf) > 0 {
		tag, n := consumeVarint(t, buf)
		buf = buf[n:]
		wire := tag & 7
		num := int(tag >> 3)
		switch wire {
		case 2:
			ln, m := consumeVarint(t, buf)
			buf = buf[m:]
			if ln > uint64(len(buf)) {
				t.Fatalf("truncated length-delimited field %d", num)
			}
			val := buf[:ln]
			buf = buf[ln:]
			if num == field {
				out = append(out, val)
			}
		case 0:
			_, m := consumeVarint(t, buf)
			buf = buf[m:]
		default:
			t.Fatalf("unsupported wire type %d", wire)
		}
	}
	return out
}

func protoString(t *testing.T, buf []byte, field int) string {
	t.Helper()
	return string(protoBytes(t, buf, field))
}

func protoBytes(t *testing.T, buf []byte, field int) []byte {
	t.Helper()
	msgs := protoMessages(t, buf, field)
	if len(msgs) == 0 {
		t.Fatalf("missing field %d", field)
	}
	return msgs[0]
}

func consumeVarint(t *testing.T, buf []byte) (value uint64, size int) {
	t.Helper()
	var x uint64
	var s uint
	for i, b := range buf {
		if b < 0x80 {
			if i > 9 {
				t.Fatal("varint too long")
			}
			return x | uint64(b)<<s, i + 1
		}
		x |= uint64(b&0x7f) << s
		s += 7
	}
	t.Fatal("truncated varint")
	return 0, 0
}
