package aggregate_test

import (
	"strings"
	"testing"

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
