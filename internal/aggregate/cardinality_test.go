package aggregate_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tracewick/agent/internal/aggregate"
	"github.com/tracewick/agent/internal/spool"
)

func TestCardinalityGateAllowsThenCollapses(t *testing.T) {
	t.Parallel()

	store := aggregate.NewMemoryStore()
	gate, err := aggregate.NewCardinalityGate(store, 2)
	if err != nil {
		t.Fatal(err)
	}

	first := gate.LimitLabels("blog", map[string]string{"env": "prod", "route": "a"}, spool.EventWall, 10)
	second := gate.LimitLabels("blog", map[string]string{"env": "prod", "route": "b"}, spool.EventWall, 10)
	third := gate.LimitLabels("blog", map[string]string{"env": "prod", "route": "c", "service": "api"}, spool.EventWall, 10)

	assertLabels(t, first, map[string]string{"env": "prod", "route": "a"})
	assertLabels(t, second, map[string]string{"env": "prod", "route": "b"})
	assertLabels(t, third, map[string]string{"service": "api", "env": "prod", "cardinality": "other"})

	again := gate.LimitLabels("blog", map[string]string{"env": "prod", "route": "a"}, spool.EventWall, 10)
	assertLabels(t, again, map[string]string{"env": "prod", "route": "a"})

	for key := range store.All() {
		if !strings.HasPrefix(key, "tracewick:agg:") {
			t.Fatalf("unexpected key %q", key)
		}
	}
}

func TestCardinalityNullStoreNeverCollapses(t *testing.T) {
	t.Parallel()

	gate, err := aggregate.NewCardinalityGate(aggregate.NullStore{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		route := string(rune('0' + i))
		got := gate.LimitLabels("blog", map[string]string{"route": route}, spool.EventWall, 10)
		assertLabels(t, got, map[string]string{"route": route})
	}
}

func TestCardinalityRejectsInvalidMaxAndDefaultsStore(t *testing.T) {
	t.Parallel()
	if _, err := aggregate.NewCardinalityGate(nil, 0); err == nil {
		t.Fatal("expected max error")
	}
	gate, err := aggregate.NewCardinalityGate(nil, aggregate.DefaultMaxUniqueKeys)
	if err != nil {
		t.Fatal(err)
	}
	got := gate.LimitLabels("a", nil, spool.EventCPU, 1)
	if got == nil {
		t.Fatal("expected empty map")
	}
}

func TestCardinalityTTLFreesSlots(t *testing.T) {
	t.Parallel()
	gate, err := aggregate.NewCardinalityGate(aggregate.NewMemoryStore(), 1)
	if err != nil {
		t.Fatal(err)
	}
	gate.SetTTL(time.Minute)
	now := time.Unix(1_000, 0)
	gate.SetNow(func() time.Time { return now })

	first := gate.LimitLabels("blog", map[string]string{"route": "a"}, spool.EventWall, 10)
	assertLabels(t, first, map[string]string{"route": "a"})
	second := gate.LimitLabels("blog", map[string]string{"route": "b"}, spool.EventWall, 10)
	assertLabels(t, second, map[string]string{"cardinality": "other"})

	now = now.Add(2 * time.Minute) // expire "a"
	third := gate.LimitLabels("blog", map[string]string{"route": "b"}, spool.EventWall, 10)
	assertLabels(t, third, map[string]string{"route": "b"})
}

func TestCardinalityAdmitSerialized(t *testing.T) {
	t.Parallel()
	gate, err := aggregate.NewCardinalityGate(aggregate.NewMemoryStore(), 2)
	if err != nil {
		t.Fatal(err)
	}
	h1 := aggregate.Hash("app", map[string]string{"r": "1"}, spool.EventCPU, 1)
	h2 := aggregate.Hash("app", map[string]string{"r": "2"}, spool.EventCPU, 1)
	h3 := aggregate.Hash("app", map[string]string{"r": "3"}, spool.EventCPU, 1)
	if gate.Admit(h1) || gate.Admit(h2) {
		t.Fatal("first two must fit")
	}
	if !gate.Admit(h3) {
		t.Fatal("third must overflow")
	}
	if gate.Admit(h1) {
		t.Fatal("known key must not overflow")
	}
}

func assertLabels(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("got %#v want %#v", got, want)
		}
	}
}
