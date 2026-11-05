package aggregate_test

import (
	"testing"

	"github.com/tracewick/agent/internal/aggregate"
)

func TestWindowBucketsByStartTime(t *testing.T) {
	t.Parallel()

	w, err := aggregate.NewWindow(10)
	if err != nil {
		t.Fatal(err)
	}
	if w.IDFor(0) != 0 || w.IDFor(9_999_999_999) != 0 || w.IDFor(10_000_000_000) != 1 {
		t.Fatalf("ids = %d %d %d", w.IDFor(0), w.IDFor(9_999_999_999), w.IDFor(10_000_000_000))
	}
	if !w.IsClosed(0, 10_000_000_000) {
		t.Fatal("window 0 should be closed")
	}
	if w.IsClosed(1, 10_000_000_000) {
		t.Fatal("window 1 should be open")
	}
}

func TestWindowRejectsOutOfRange(t *testing.T) {
	t.Parallel()
	if _, err := aggregate.NewWindow(9); err == nil {
		t.Fatal("expected error")
	}
	if _, err := aggregate.NewWindow(16); err == nil {
		t.Fatal("expected error")
	}
	if _, err := aggregate.NewWindow(aggregate.DefaultWindowSeconds); err != nil {
		t.Fatal(err)
	}
}

func TestWindowZeroDurationIDFor(t *testing.T) {
	t.Parallel()
	w := &aggregate.Window{}
	if w.IDFor(99) != 0 {
		t.Fatal(w.IDFor(99))
	}
}
