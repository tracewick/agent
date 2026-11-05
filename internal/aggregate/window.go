package aggregate

import "fmt"

// Window is the ADR-12 wall-clock merge window (10–15 s, default 15).
// It defines merge compatibility only; drain/agent may POST open windows (multi-POST OK).
type Window struct {
	DurationNs int64
}

const (
	DefaultWindowSeconds = 15
	MinWindowSeconds     = 10
	MaxWindowSeconds     = 15
)

// NewWindow validates seconds and returns a Window.
func NewWindow(seconds int) (*Window, error) {
	if seconds < MinWindowSeconds || seconds > MaxWindowSeconds {
		return nil, fmt.Errorf(
			"aggregate: window must be between %d and %d seconds, got %d",
			MinWindowSeconds,
			MaxWindowSeconds,
			seconds,
		)
	}
	return &Window{DurationNs: int64(seconds) * 1_000_000_000}, nil
}

// IDFor returns the bucket id for a profile start time (nanoseconds).
func (w *Window) IDFor(startNs int64) int64 {
	if w.DurationNs <= 0 {
		return 0
	}
	return startNs / w.DurationNs
}

// IsClosed reports whether the bucket is fully in the past relative to nowNs.
func (w *Window) IsClosed(windowID, nowNs int64) bool {
	return windowID < w.IDFor(nowNs)
}
