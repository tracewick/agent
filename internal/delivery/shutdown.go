package delivery

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// DefaultShutdownGrace is how long in-flight work may finish after SIGTERM/SIGINT.
const DefaultShutdownGrace = 30 * time.Second

// Notify cancels the returned context when one of signals is received (default: SIGTERM, SIGINT).
// The CancelFunc stops listening; call it on exit.
func Notify(parent context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
	if len(signals) == 0 {
		signals = []os.Signal{syscall.SIGTERM, syscall.SIGINT}
	}
	return signal.NotifyContext(parent, signals...)
}

// WithGrace returns a child context that is cancelled either when parent is cancelled
// or when grace elapses after parent cancellation starts being observed.
// Callers typically: ctx, stop := Notify(...); defer stop(); workCtx, cancel := WithGrace(ctx, grace); defer cancel().
func WithGrace(parent context.Context, grace time.Duration) (context.Context, context.CancelFunc) {
	if grace <= 0 {
		grace = DefaultShutdownGrace
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	go func() {
		select {
		case <-parent.Done():
			timer := time.NewTimer(grace)
			defer timer.Stop()
			select {
			case <-timer.C:
				cancel()
			case <-ctx.Done():
			}
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}
