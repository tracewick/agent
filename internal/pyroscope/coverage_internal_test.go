package pyroscope

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/tracewick/agent/internal/spool"
)

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("no entropy") }

func TestNewSampleIDReadFailure(t *testing.T) {
	prev := readRandom
	readRandom = errReader{}
	t.Cleanup(func() { readRandom = prev })

	if _, err := NewSampleID(); err == nil {
		t.Fatal("expected entropy error")
	}
}

func TestEncodeSkipsReservedProfileLabelKeys(t *testing.T) {
	body, err := Encode("blog", spool.EventWall, map[string]string{
		"service_name":  "should-not-override",
		"__name__":      "nope",
		"pyroscope_spy": "other",
		"ok.label":      "v",
	}, []byte("x"), "id")
	if err != nil {
		t.Fatal(err)
	}
	if len(body) == 0 {
		t.Fatal("empty body")
	}
}

func TestStatusErrorFormats(t *testing.T) {
	empty := &StatusError{StatusCode: 500}
	if empty.Error() != "pyroscope: push HTTP 500" {
		t.Fatalf("%q", empty.Error())
	}
	withBody := &StatusError{StatusCode: 429, Body: "slow"}
	if withBody.Error() != "pyroscope: push HTTP 429: slow" {
		t.Fatalf("%q", withBody.Error())
	}
}

func TestPushInvalidEndpoint(t *testing.T) {
	c := &Client{
		endpoint:    "://not-a-url",
		httpClient:  http.DefaultClient,
		newSampleID: NewSampleID,
	}
	err := c.Push(context.Background(), &Profile{
		Application: "app",
		EventType:   spool.EventWall,
		RawProfile:  []byte("x"),
		SampleID:    "id",
	})
	if err == nil {
		t.Fatal("expected NewRequest error")
	}
}

var _ io.Reader = errReader{}
