package spool

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Wire constants match Tracewick\Sink\SpoolFile in tracewick/tracewick.
const (
	Magic               = "TWSP"
	Version2            = 2
	FormatCompactStacks = "compact-stacks"
	MaxHeaderBytes      = 1024 * 1024
	preambleBytes       = 9
)

// EventType is the sampling clock for a profile window (wall|cpu).
type EventType string

const (
	EventWall EventType = "wall"
	EventCPU  EventType = "cpu"
)

// File is one spool v2 blob (SpoolFile in the main repo): JSON header plus compact-stacks bytes.
type File struct {
	Application string
	EventType   EventType
	PeriodNs    int64
	StartNs     int64
	EndNs       int64
	Labels      map[string]string
	Format      string
	Payload     []byte
	Version     uint8
}

// Parse reads a version-2 spool file (SpoolFile::fromBytes). Unknown header keys are ignored.
func Parse(data []byte) (*File, error) {
	if len(data) < preambleBytes || string(data[:4]) != Magic {
		return nil, errors.New("the file does not start with the spool magic bytes")
	}

	version := data[4]
	if version != Version2 {
		return nil, fmt.Errorf("spool format version %d is not supported", version)
	}

	headerLen := binary.BigEndian.Uint32(data[5:9])
	if headerLen > MaxHeaderBytes {
		return nil, fmt.Errorf("the header of %d bytes exceeds the limit of %d bytes", headerLen, MaxHeaderBytes)
	}
	if len(data) < preambleBytes+int(headerLen) {
		return nil, errors.New("the file ends inside the header")
	}

	var header map[string]json.RawMessage
	if err := json.Unmarshal(data[preambleBytes:preambleBytes+int(headerLen)], &header); err != nil {
		return nil, fmt.Errorf("the header is not valid JSON: %w", err)
	}

	payload := data[preambleBytes+int(headerLen):]
	payloadSize, err := headerInt(header, "payload_size")
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) != payloadSize {
		return nil, fmt.Errorf("the payload has %d bytes, the header announces %d", len(payload), payloadSize)
	}

	application, err := headerString(header, "application")
	if err != nil {
		return nil, err
	}
	eventRaw, err := headerString(header, "event_type")
	if err != nil {
		return nil, err
	}
	eventType := EventType(eventRaw)
	if eventType != EventWall && eventType != EventCPU {
		return nil, fmt.Errorf("unknown event type %q", eventRaw)
	}
	periodNs, err := headerInt(header, "period_ns")
	if err != nil {
		return nil, err
	}
	if periodNs < 1 {
		return nil, errors.New("the sampling period must be at least 1 ns")
	}
	startNs, err := headerInt(header, "start_ns")
	if err != nil {
		return nil, err
	}
	endNs, err := headerInt(header, "end_ns")
	if err != nil {
		return nil, err
	}
	if endNs < startNs {
		return nil, errors.New("the profile window ends before it starts")
	}
	format, err := headerString(header, "format")
	if err != nil {
		return nil, err
	}
	if format != FormatCompactStacks {
		return nil, fmt.Errorf("payload format %q is not supported", format)
	}
	labels, err := headerLabels(header)
	if err != nil {
		return nil, err
	}

	out := &File{
		Application: application,
		EventType:   eventType,
		PeriodNs:    periodNs,
		StartNs:     startNs,
		EndNs:       endNs,
		Labels:      labels,
		Format:      format,
		Payload:     append([]byte(nil), payload...),
		Version:     version,
	}
	return out, nil
}

func headerString(header map[string]json.RawMessage, key string) (string, error) {
	raw, ok := header[key]
	if !ok {
		return "", fmt.Errorf("header key %q must be a string", key)
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("header key %q must be a string", key)
	}
	return v, nil
}

func headerInt(header map[string]json.RawMessage, key string) (int64, error) {
	raw, ok := header[key]
	if !ok {
		return 0, fmt.Errorf("header key %q must be a non-negative 64-bit integer", key)
	}
	var v int64
	if err := json.Unmarshal(raw, &v); err != nil || v < 0 {
		return 0, fmt.Errorf("header key %q must be a non-negative 64-bit integer", key)
	}
	return v, nil
}

func headerLabels(header map[string]json.RawMessage) (map[string]string, error) {
	raw, ok := header["labels"]
	if !ok {
		return nil, errors.New(`header key "labels" must be an object`)
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || trimmed[0] != '{' {
		return nil, errors.New(`header key "labels" must be an object`)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, errors.New(`header key "labels" must be an object`)
	}
	out := make(map[string]string, len(obj))
	for k, v := range obj {
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return nil, fmt.Errorf("label %q must have a string value", k)
		}
		out[k] = s
	}
	return out, nil
}
