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

// SplitAdmitGrace returns two child contexts for shutdown:
//   - admit is cancelled as soon as parent is cancelled (stop new claims);
//   - work is cancelled after grace (finish in-flight push), or immediately when grace is 0.
//
// grace < 0 uses DefaultShutdownGrace. grace == 0 cancels work immediately with admit.
func SplitAdmitGrace(parent context.Context, grace time.Duration) (admit, work context.Context, cancel context.CancelFunc) {
	if grace < 0 {
		grace = DefaultShutdownGrace
	}
	admit, cancelAdmit := context.WithCancel(context.WithoutCancel(parent))
	work, cancelWork := context.WithCancel(context.WithoutCancel(parent))
	go func() {
		select {
		case <-parent.Done():
			cancelAdmit()
			if grace == 0 {
				cancelWork()
				return
			}
			timer := time.NewTimer(grace)
			defer timer.Stop()
			select {
			case <-timer.C:
				cancelWork()
			case <-work.Done():
			}
		case <-admit.Done():
		}
	}()
	return admit, work, func() {
		cancelAdmit()
		cancelWork()
	}
}

// WithGrace returns a child cancelled grace after parent cancellation.
// Deprecated for agent shutdown: prefer SplitAdmitGrace so new claims stop immediately.
// grace < 0 → DefaultShutdownGrace; grace == 0 → cancel immediately when parent is done.
func WithGrace(parent context.Context, grace time.Duration) (context.Context, context.CancelFunc) {
	_, work, cancel := SplitAdmitGrace(parent, grace)
	return work, cancel
}
