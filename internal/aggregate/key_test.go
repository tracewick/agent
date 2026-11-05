package aggregate_test

import (
	"testing"

	"github.com/tracewick/agent/internal/aggregate"
	"github.com/tracewick/agent/internal/spool"
)

func TestHashMatchesPHPGolden(t *testing.T) {
	t.Parallel()

	got := aggregate.Hash("blog", map[string]string{"b": "2", "a": "1"}, spool.EventWall, 10_000_000)
	if got != "6a8868a5e515dd6e" {
		t.Fatalf("hash = %q", got)
	}
	same := aggregate.Hash("blog", map[string]string{"a": "1", "b": "2"}, spool.EventWall, 10_000_000)
	if same != got {
		t.Fatalf("label order changed hash: %q vs %q", same, got)
	}
	if aggregate.Shard(got) != "6a" {
		t.Fatalf("shard = %q", aggregate.Shard(got))
	}

	base := aggregate.Hash("blog", map[string]string{"env": "prod"}, spool.EventWall, 10_000_000)
	if base != "f3078d2c2f69f26f" {
		t.Fatalf("base hash = %q", base)
	}
	if aggregate.Shard(base) != "f3" {
		t.Fatalf("shard = %q", aggregate.Shard(base))
	}
	if aggregate.Hash("shop", map[string]string{"env": "prod"}, spool.EventWall, 10_000_000) == base {
		t.Fatal("application should change hash")
	}
	if aggregate.Hash("blog", map[string]string{"env": "prod"}, spool.EventCPU, 10_000_000) == base {
		t.Fatal("event should change hash")
	}
	if aggregate.Hash("blog", map[string]string{"env": "prod"}, spool.EventWall, 20_000_000) == base {
		t.Fatal("period should change hash")
	}
	if aggregate.Hash("blog", map[string]string{"env": "staging"}, spool.EventWall, 10_000_000) == base {
		t.Fatal("label value should change hash")
	}
}

func TestKeyFromFileAndEqual(t *testing.T) {
	t.Parallel()

	f := &spool.File{
		Application: "blog",
		EventType:   spool.EventWall,
		PeriodNs:    10,
		Labels:      map[string]string{"env": "prod"},
		StartNs:     1,
		EndNs:       2,
	}
	k, err := aggregate.KeyFromFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if k.Hash() != aggregate.Hash("blog", map[string]string{"env": "prod"}, spool.EventWall, 10) {
		t.Fatal("key hash mismatch")
	}
	other, _ := aggregate.KeyFromFile(f)
	if !k.Equal(other) {
		t.Fatal("expected equal")
	}
	f2 := *f
	f2.Labels = map[string]string{"env": "dev"}
	other2, _ := aggregate.KeyFromFile(&f2)
	if k.Equal(other2) {
		t.Fatal("expected not equal")
	}

	if _, e := aggregate.KeyFromFile(nil); e == nil {
		t.Fatal("expected nil file error")
	}
	if _, e := aggregate.KeyFromFile(&spool.File{EventType: spool.EventWall, PeriodNs: 1}); e == nil {
		t.Fatal("expected empty application error")
	}
	if _, e := aggregate.KeyFromFile(&spool.File{Application: "a", EventType: spool.EventWall, PeriodNs: 0}); e == nil {
		t.Fatal("expected period error")
	}
	if _, e := aggregate.KeyFromFile(&spool.File{Application: "a", EventType: "gpu", PeriodNs: 1}); e == nil {
		t.Fatal("expected event error")
	}
	nilLabels, e := aggregate.KeyFromFile(&spool.File{Application: "a", EventType: spool.EventCPU, PeriodNs: 1})
	if e != nil || nilLabels.Labels == nil {
		t.Fatalf("%v %#v", e, nilLabels)
	}

	base := aggregate.Key{Application: "a", EventType: spool.EventWall, PeriodNs: 1, Labels: map[string]string{}}
	if base.Equal(aggregate.Key{Application: "b", EventType: spool.EventWall, PeriodNs: 1, Labels: map[string]string{}}) {
		t.Fatal("application")
	}
	if base.Equal(aggregate.Key{Application: "a", EventType: spool.EventCPU, PeriodNs: 1, Labels: map[string]string{}}) {
		t.Fatal("event")
	}
	if base.Equal(aggregate.Key{Application: "a", EventType: spool.EventWall, PeriodNs: 2, Labels: map[string]string{}}) {
		t.Fatal("period")
	}
}

func TestShardShort(t *testing.T) {
	t.Parallel()
	if aggregate.Shard("a") != "a" {
		t.Fatal(aggregate.Shard("a"))
	}
}
