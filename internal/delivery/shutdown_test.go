package delivery_test

import (
	"context"
	"syscall"
	"testing"
	"time"

	"github.com/tracewick/agent/internal/delivery"
)

func TestNotifyAndWithGrace(t *testing.T) {
	t.Parallel()
	parent, stop := delivery.Notify(context.Background(), syscall.SIGUSR1)
	defer stop()

	graceCtx, cancel := delivery.WithGrace(parent, 20*time.Millisecond)
	defer cancel()

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-parent.Done():
	case <-time.After(time.Second):
		t.Fatal("parent not cancelled")
	}
	select {
	case <-graceCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("grace child not cancelled")
	}
}

func TestWithGraceDefaultsAndEarlyCancel(t *testing.T) {
	t.Parallel()
	parent, stopParent := context.WithCancel(context.Background())
	ctx, cancel := delivery.WithGrace(parent, 0) // default grace
	cancel()                                     // early
	select {
	case <-ctx.Done():
	default:
		t.Fatal("expected cancel")
	}
	stopParent()

	parent2, stop2 := context.WithCancel(context.Background())
	defer stop2()
	ctx2, cancel2 := delivery.WithGrace(parent2, time.Hour)
	defer cancel2()
	stop2()
	// Parent cancelled but grace is long — child should still be live briefly.
	select {
	case <-ctx2.Done():
		t.Fatal("child should still be running during grace")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestNotifyDefaultSignals(t *testing.T) {
	t.Parallel()
	ctx, stop := delivery.Notify(context.Background())
	stop()
	select {
	case <-ctx.Done():
		// NotifyContext may or may not cancel on stop depending on Go version;
		// stop must be safe to call.
	default:
	}
}
