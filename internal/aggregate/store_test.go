package aggregate_test

import (
	"testing"

	"github.com/tracewick/agent/internal/aggregate"
)

func TestMemoryStoreGetAddDelete(t *testing.T) {
	t.Parallel()
	s := aggregate.NewMemoryStore()
	if s.Get("missing") != 0 {
		t.Fatal("missing")
	}
	if s.Increment("a") != 1 || s.Get("a") != 1 {
		t.Fatal("inc")
	}
	if s.Add("a", 2) != 3 {
		t.Fatal("add")
	}
	if s.Add("a", -3) != 0 || s.Get("a") != 0 {
		t.Fatal("zero deletes")
	}
	_ = s.Increment("b")
	s.Delete("b")
	if s.Get("b") != 0 || s.Len() != 0 {
		t.Fatal("delete")
	}
}

func TestNullStoreMethods(t *testing.T) {
	t.Parallel()
	var n aggregate.NullStore
	if n.Get("x") != 0 || n.Increment("x") != 0 || n.Add("x", 5) != 0 {
		t.Fatal("null counters")
	}
	n.Delete("x")
}
