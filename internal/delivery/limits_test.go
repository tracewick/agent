package delivery_test

import (
	"math"
	"testing"

	"github.com/tracewick/agent/internal/delivery"
)

func TestDecodeLimitsMapping(t *testing.T) {
	t.Parallel()
	l := delivery.DefaultLimits()
	d := l.DecodeLimits()
	if d.MaxStacks != l.MaxStacks || d.MaxFrames != l.MaxFrames || d.MaxStrings != l.MaxStrings ||
		d.MaxStringBytes != l.MaxStringBytes || d.MaxFrameRefs != l.MaxFrameRefs {
		t.Fatalf("%#v vs %#v", d, l)
	}
}

func TestEncodeLimitsMapping(t *testing.T) {
	t.Parallel()
	l := delivery.DefaultLimits()
	e := l.EncodeLimits()
	if e.MaxStringBytes != l.MaxStringBytes || e.MaxEncodedBytes != int(l.MaxAggregatedPprofBytes) {
		t.Fatalf("%#v vs %#v", e, l)
	}
	if e.MaxNameBytes <= 0 {
		t.Fatal("MaxNameBytes")
	}
}

func TestDefaultLimitsValidate(t *testing.T) {
	t.Parallel()
	l := delivery.DefaultLimits()
	if err := l.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := l.AcceptDecoded(100); err != nil {
		t.Fatal(err)
	}
	if err := l.AcceptDecoded(int(l.MaxDecodedBytes) + 1); err == nil {
		t.Fatal("over")
	}
	if err := l.AcceptDecoded(-1); err == nil {
		t.Fatal("neg")
	}
}

func TestLimitsValidationFailures(t *testing.T) {
	t.Parallel()
	base := delivery.DefaultLimits()
	cases := []delivery.Limits{
		func() delivery.Limits { l := base; l.MaxDecodedBytes = 0; return l }(),
		func() delivery.Limits { l := base; l.MaxStacks = 0; return l }(),
		func() delivery.Limits { l := base; l.MaxStacks = 100_001; return l }(),
		func() delivery.Limits { l := base; l.MaxFrames = 0; return l }(),
		func() delivery.Limits { l := base; l.MaxFrames = 500_001; return l }(),
		func() delivery.Limits { l := base; l.MaxStrings = 0; return l }(),
		func() delivery.Limits { l := base; l.MaxStrings = 1_000_001; return l }(),
		func() delivery.Limits { l := base; l.MaxStringBytes = 0; return l }(),
		func() delivery.Limits { l := base; l.MaxFrameRefs = 0; return l }(),
		func() delivery.Limits { l := base; l.MaxHeldProfiles = 0; return l }(),
		func() delivery.Limits { l := base; l.MaxHeldDecodedBytes = 0; return l }(),
		func() delivery.Limits { l := base; l.MaxAggregatedPprofBytes = 0; return l }(),
		func() delivery.Limits { l := base; l.MaxInFlightBytes = 0; return l }(),
	}
	for i, l := range cases {
		if err := l.Validate(); err == nil {
			t.Fatalf("case %d expected error", i)
		}
	}
}

func TestSampleValue(t *testing.T) {
	t.Parallel()
	v, err := delivery.SampleValue(3, 10)
	if err != nil || v != 30 {
		t.Fatalf("%d %v", v, err)
	}
	if _, negErr := delivery.SampleValue(1, -1); negErr == nil {
		t.Fatal("neg period")
	}
	z, zErr := delivery.SampleValue(0, 10)
	if zErr != nil || z != 0 {
		t.Fatal(z, zErr)
	}
	if _, ovErr := delivery.SampleValue(2, 1<<62); ovErr == nil {
		t.Fatal("int64 overflow")
	}
	if _, ovErr := delivery.SampleValue(math.MaxUint64, math.MaxInt64); ovErr == nil {
		t.Fatal("overflow")
	}
}
