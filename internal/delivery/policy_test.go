package delivery_test

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/tracewick/agent/internal/delivery"
	"github.com/tracewick/agent/internal/pyroscope"
)

func TestClassifyStatusTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status int
		want   delivery.Action
	}{
		{200, delivery.ActionAck},
		{204, delivery.ActionAck},
		{408, delivery.ActionRetry},
		{429, delivery.ActionRetry},
		{500, delivery.ActionRetry},
		{503, delivery.ActionRetry},
		{401, delivery.ActionPauseAuth},
		{403, delivery.ActionPauseAuth},
		{404, delivery.ActionPauseEndpoint},
		{302, delivery.ActionPauseEndpoint},
		{301, delivery.ActionPauseEndpoint},
		{413, delivery.ActionShrinkBatch},
		{400, delivery.ActionReject},
		{418, delivery.ActionReject},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d", tc.status), func(t *testing.T) {
			t.Parallel()
			d := delivery.ClassifyStatus(tc.status, 2*time.Second, "")
			if d.Action != tc.want {
				t.Fatalf("action = %v want %v", d.Action, tc.want)
			}
			if d.Reason != fmt.Sprintf("HTTP %d", tc.status) {
				t.Fatalf("empty reason default = %q", d.Reason)
			}
			if tc.want == delivery.ActionRetry && d.RetryAfter != 2*time.Second {
				t.Fatalf("retry-after = %s", d.RetryAfter)
			}
			labeled := delivery.ClassifyStatus(tc.status, 0, "labeled")
			if labeled.Reason != "labeled" {
				t.Fatalf("explicit reason = %q", labeled.Reason)
			}
		})
	}
}

func TestClassifyErrors(t *testing.T) {
	t.Parallel()
	if d := delivery.Classify(nil); d.Action != delivery.ActionAck {
		t.Fatal(d)
	}
	st := &pyroscope.StatusError{StatusCode: 429, Body: "slow", RetryAfter: 3 * time.Second}
	d := delivery.Classify(st)
	if d.Action != delivery.ActionRetry || d.RetryAfter != 3*time.Second {
		t.Fatalf("%#v", d)
	}
	wrapped := fmt.Errorf("push: %w", st)
	if delivery.Classify(wrapped).Action != delivery.ActionRetry {
		t.Fatal("unwrap")
	}
	netErr := &net.OpError{Op: "dial", Err: errors.New("refused")}
	if delivery.Classify(netErr).Action != delivery.ActionRetry {
		t.Fatal("net")
	}
	if delivery.Classify(tempNet{}).Action != delivery.ActionRetry {
		t.Fatal("net.Error")
	}
	if delivery.Classify(errors.New("weird")).Action != delivery.ActionRetry {
		t.Fatal("default retry")
	}
}

func TestFor413AndActionString(t *testing.T) {
	t.Parallel()
	if delivery.For413(1) != delivery.ActionReject || delivery.For413(2) != delivery.ActionShrinkBatch {
		t.Fatal("413")
	}
	for _, a := range []delivery.Action{
		delivery.ActionAck, delivery.ActionRetry, delivery.ActionReject,
		delivery.ActionPauseAuth, delivery.ActionPauseEndpoint, delivery.ActionShrinkBatch,
		delivery.Action(99),
	} {
		if a.String() == "" {
			t.Fatal(a)
		}
	}
}

func TestBackoff(t *testing.T) {
	t.Parallel()
	if got := delivery.Backoff(0, 5*time.Second, time.Second, time.Minute, nil); got != 5*time.Second {
		t.Fatalf("hint = %s", got)
	}
	if got := delivery.Backoff(0, time.Hour, time.Second, time.Minute, nil); got != time.Hour {
		t.Fatalf("Retry-After must not be clipped by maxDelay, got %s", got)
	}
	rng := rand.New(rand.NewSource(1)) //nolint:gosec // deterministic test jitter
	d := delivery.Backoff(0, 0, time.Second, time.Minute, rng)
	if d < 0 || d > time.Second {
		t.Fatalf("jitter = %s", d)
	}
	// Int63n(d+1) with d=1ns must not panic; a +→− mutant calls Int63n(0).
	for i := 0; i < 20; i++ {
		_ = delivery.Backoff(0, 0, time.Nanosecond, time.Second, rand.New(rand.NewSource(int64(i)))) //nolint:gosec // seeds
	}
	if got := delivery.Backoff(100, 0, time.Second, 10*time.Second, nil); got != 10*time.Second {
		t.Fatalf("max = %s", got)
	}
	if got := delivery.Backoff(-1, 0, 0, 0, nil); got != time.Second {
		// base defaults to 1s, attempt 0, no jitter
		t.Fatalf("defaults = %s", got)
	}
	_ = http.StatusOK
}

type tempNet struct{}

func (tempNet) Error() string   { return "temporary" }
func (tempNet) Timeout() bool   { return true }
func (tempNet) Temporary() bool { return true }
