package aggregate

import (
	"testing"

	"github.com/tracewick/agent/internal/spool"
)

func TestGroupByHashCollisionSeam(t *testing.T) {
	prev := hashOf
	hashOf = func(Key) string { return "deadbeefdeadbeef" }
	t.Cleanup(func() { hashOf = prev })

	w, err := NewWindow(10)
	if err != nil {
		t.Fatal(err)
	}
	a := &spool.File{Application: "a", EventType: spool.EventWall, PeriodNs: 1, StartNs: 1, EndNs: 2, Labels: map[string]string{}}
	b := &spool.File{Application: "b", EventType: spool.EventWall, PeriodNs: 1, StartNs: 1, EndNs: 3, Labels: map[string]string{}}
	_, err = GroupBy(w, []Member{{File: a}, {File: b}})
	if err == nil {
		t.Fatal("expected collision error")
	}
}

func TestGroupByEmpty(t *testing.T) {
	w, _ := NewWindow(10)
	groups, err := GroupBy(w, nil)
	if err != nil || len(groups) != 0 {
		t.Fatalf("%v %#v", err, groups)
	}
}
