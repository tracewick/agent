package aggregate_test

import (
	"testing"

	"github.com/tracewick/agent/internal/aggregate"
	"github.com/tracewick/agent/internal/spool"
)

func TestGroupBySameKeyAndWindow(t *testing.T) {
	t.Parallel()

	w, err := aggregate.NewWindow(10)
	if err != nil {
		t.Fatal(err)
	}
	a := file("blog", spool.EventWall, 10_000_000, 2_000, 2_100, map[string]string{"env": "prod"})
	b := file("blog", spool.EventWall, 10_000_000, 1_000, 2_500, map[string]string{"env": "prod"})
	c := file("blog", spool.EventWall, 10_000_000, 10_000_000_000, 10_000_000_100, map[string]string{"env": "prod"})

	hash := aggregate.Hash("blog", map[string]string{"env": "prod"}, spool.EventWall, 10_000_000)
	groups, err := aggregate.GroupBy(w, []aggregate.Member{
		{File: a, Name: "00000000000000000001-" + hash + "-aaaaaaaaaaaaaaaa.spool"},
		{File: b, HintHash: hash},
		{File: c},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %d %#v", len(groups), groups)
	}
	// sorted by group key; window 0 then window 1
	if groups[0].WindowID != 0 || len(groups[0].Files) != 2 {
		t.Fatalf("%#v", groups[0])
	}
	if groups[0].StartNs != 1_000 || groups[0].EndNs != 2_500 {
		t.Fatalf("span = %d..%d", groups[0].StartNs, groups[0].EndNs)
	}
	if groups[1].WindowID != 1 || len(groups[1].Files) != 1 {
		t.Fatalf("%#v", groups[1])
	}
}

func TestGroupByRejectsHintMismatch(t *testing.T) {
	t.Parallel()

	w, _ := aggregate.NewWindow(10)
	f := file("blog", spool.EventWall, 10, 1, 2, map[string]string{"env": "prod"})
	_, err := aggregate.GroupBy(w, []aggregate.Member{{
		File: f,
		Name: "00000000000000000001-0123456789abcdef-aaaaaaaaaaaaaaaa.spool",
	}})
	if err == nil {
		t.Fatal("expected hint mismatch")
	}
}

func TestGroupByValidation(t *testing.T) {
	t.Parallel()

	if _, err := aggregate.GroupBy(nil, nil); err == nil {
		t.Fatal("nil window")
	}
	w, _ := aggregate.NewWindow(10)
	if _, err := aggregate.GroupBy(w, []aggregate.Member{{}}); err == nil {
		t.Fatal("nil file")
	}
	if _, err := aggregate.GroupBy(w, []aggregate.Member{{
		File: &spool.File{Application: "a", EventType: spool.EventWall, PeriodNs: 0},
	}}); err == nil {
		t.Fatal("bad key from file")
	}
	if aggregate.AggregationKeyHintFromName("plain-name.spool") != "" {
		t.Fatal("expected empty hint")
	}
	if aggregate.AggregationKeyHintFromName("ab/00000000000000000001-0123456789abcdef-aaaaaaaaaaaaaaaa.spool") != "0123456789abcdef" {
		t.Fatal("shard path hint")
	}
}

func file(app string, event spool.EventType, period, start, end int64, labels map[string]string) *spool.File {
	return &spool.File{
		Application: app,
		EventType:   event,
		PeriodNs:    period,
		StartNs:     start,
		EndNs:       end,
		Labels:      labels,
		Format:      spool.FormatCompactStacks,
	}
}
