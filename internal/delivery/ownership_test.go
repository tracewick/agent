package delivery_test

import (
	"testing"
	"time"

	"github.com/tracewick/agent/internal/delivery"
)

func TestOwnershipBudget(t *testing.T) {
	t.Parallel()
	o, err := delivery.NewOwnership(10*time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if o.Budget() != 9*time.Minute {
		t.Fatal(o.Budget())
	}
	claimed := time.Unix(1_000, 0)
	now := claimed.Add(8 * time.Minute)
	if o.Exhausted(claimed, now) || o.Remaining(claimed, now) != time.Minute {
		t.Fatalf("remaining = %s", o.Remaining(claimed, now))
	}
	if !o.WithinHTTP(claimed, now, 30*time.Second) {
		t.Fatal("http fits")
	}
	if o.WithinHTTP(claimed, now, 2*time.Minute) {
		t.Fatal("http too long")
	}
	if !o.Exhausted(claimed, claimed.Add(9*time.Minute)) {
		t.Fatal("exhausted")
	}
	def, err := delivery.NewOwnership(0, 0)
	if err != nil || def.StaleClaim != delivery.DefaultStaleClaim {
		t.Fatalf("%v %#v", err, def)
	}
	if def.Remaining(time.Time{}, time.Now()) != def.Budget() {
		t.Fatal("zero claimedAt")
	}
}

func TestOwnershipValidation(t *testing.T) {
	t.Parallel()
	if _, err := delivery.NewOwnership(-time.Second, time.Second); err == nil {
		t.Fatal("negative stale")
	}
	if _, err := delivery.NewOwnership(time.Minute, -time.Second); err == nil {
		t.Fatal("negative margin")
	}
	if _, err := delivery.NewOwnership(time.Minute, time.Minute); err == nil {
		t.Fatal("margin >= stale")
	}
}
