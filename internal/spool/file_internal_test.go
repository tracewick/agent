package spool

import (
	"encoding/json"
	"testing"
)

func TestHeaderLabelsInvalidObjectJSON(t *testing.T) {
	t.Parallel()

	_, err := headerLabels(map[string]json.RawMessage{
		"labels": json.RawMessage(`{`),
	})
	if err == nil {
		t.Fatal("expected labels object unmarshal error")
	}
}

func TestHeaderStringMissingKey(t *testing.T) {
	t.Parallel()

	_, err := headerString(map[string]json.RawMessage{}, "application")
	if err == nil {
		t.Fatal("expected missing key error")
	}
}
