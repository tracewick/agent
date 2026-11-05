package pyroscope

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/tracewick/agent/internal/spool"
)

// Overridden in tests.
var readRandom io.Reader = rand.Reader

// Wire constants match Tracewick\Sink\PyroscopePushRequest in tracewick/tracewick.
const (
	Path      = "/push.v1.PusherService/Push"
	MediaType = "application/proto"
	SpyLabel  = "pyroscope_spy"
	SpyName   = "tracewick"
)

const (
	pushRequestSeries uint64 = 1
	seriesLabels      uint64 = 1
	seriesSamples     uint64 = 2
	labelName         uint64 = 1
	labelValue        uint64 = 2
	sampleRawProfile  uint64 = 1
	sampleID          uint64 = 2
)

// Encode builds a push.v1 PushRequest wrapping one raw pprof profile.
//
// Series labels: __name__, service_name, pyroscope_spy, then profile labels
// (dots → underscores). span_id is omitted from series labels (it stays inside pprof).
// Profile label keys are emitted in sorted order for a stable wire encoding.
func Encode(application string, event spool.EventType, profileLabels map[string]string, rawProfile []byte, sampleIDValue string) ([]byte, error) {
	if application == "" {
		return nil, fmt.Errorf("pyroscope: application (service_name) is required")
	}
	if sampleIDValue == "" {
		return nil, fmt.Errorf("pyroscope: sample id is required")
	}
	name, err := SeriesName(event)
	if err != nil {
		return nil, err
	}

	labels := map[string]string{
		"__name__":     name,
		"service_name": application,
		SpyLabel:       SpyName,
	}
	keys := make([]string, 0, len(profileLabels))
	for key, value := range profileLabels {
		if key == "span_id" {
			continue
		}
		mapped := LabelKey(key)
		labels[mapped] = value
		keys = append(keys, mapped)
	}
	sort.Strings(keys)

	series := &protoWriter{}
	writeLabel := func(k, v string) {
		pair := &protoWriter{}
		pair.bytesString(labelName, k)
		pair.bytesString(labelValue, v)
		series.message(seriesLabels, pair)
	}
	writeLabel("__name__", labels["__name__"])
	writeLabel("service_name", labels["service_name"])
	writeLabel(SpyLabel, labels[SpyLabel])
	for _, k := range keys {
		if k == "__name__" || k == "service_name" || k == SpyLabel {
			continue
		}
		writeLabel(k, labels[k])
	}

	sample := &protoWriter{}
	sample.bytes(sampleRawProfile, rawProfile)
	sample.bytesString(sampleID, sampleIDValue)
	series.message(seriesSamples, sample)

	out := &protoWriter{}
	out.message(pushRequestSeries, series)
	return out.buf, nil
}

// SeriesName returns the Grafana profile-type name for series __name__.
func SeriesName(event spool.EventType) (string, error) {
	switch event {
	case spool.EventCPU:
		return "process_cpu", nil
	case spool.EventWall:
		return "wall", nil
	default:
		return "", fmt.Errorf("pyroscope: unknown event type %q", event)
	}
}

// LabelKey maps profile label keys the way Pyroscope expects (dots → underscores).
func LabelKey(key string) string {
	return strings.ReplaceAll(key, ".", "_")
}

// NewSampleID returns an RFC 4122 UUID v4 for RawSample.ID.
func NewSampleID() (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(readRandom, b[:]); err != nil {
		return "", fmt.Errorf("pyroscope: sample id: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32], nil
}
