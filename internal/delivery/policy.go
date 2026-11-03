package delivery

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"time"

	"github.com/tracewick/agent/internal/pyroscope"
)

// Action is what the agent must do with claimed spool files after a push attempt.
type Action int

const (
	// ActionAck deletes the claim after HTTP 2xx.
	ActionAck Action = iota
	// ActionRetry releases the claim and retries later (408/429/5xx/transport).
	ActionRetry
	// ActionReject moves the claim to rejected/ (400 / other bad payload).
	ActionReject
	// ActionPauseAuth releases and pauses delivery until auth is fixed (401/403).
	ActionPauseAuth
	// ActionPauseEndpoint releases and pauses for a bad endpoint (404).
	ActionPauseEndpoint
	// ActionShrinkBatch shrinks the next upload batch and retries (413).
	// When the batch is already a single profile, the caller must reject instead.
	ActionShrinkBatch
)

func (a Action) String() string {
	switch a {
	case ActionAck:
		return "ack"
	case ActionRetry:
		return "retry"
	case ActionReject:
		return "reject"
	case ActionPauseAuth:
		return "pause_auth"
	case ActionPauseEndpoint:
		return "pause_endpoint"
	case ActionShrinkBatch:
		return "shrink_batch"
	default:
		return fmt.Sprintf("action(%d)", int(a))
	}
}

// Decision is the classified outcome of one push attempt.
type Decision struct {
	Action Action
	// RetryAfter is a lower bound for the next attempt when ActionRetry (from Retry-After or 0).
	RetryAfter time.Duration
	Reason     string
}

// Classify maps a Push error (or nil) to a spool action. Unlike PHP drain,
// 401/403/404 keep spool contents (pause) instead of rejecting as trash.
func Classify(err error) Decision {
	if err == nil {
		return Decision{Action: ActionAck, Reason: "HTTP 2xx"}
	}
	var st *pyroscope.StatusError
	if errors.As(err, &st) {
		return ClassifyStatus(st.StatusCode, st.RetryAfter, st.Error())
	}
	if isTransientNet(err) {
		return Decision{Action: ActionRetry, Reason: err.Error()}
	}
	return Decision{Action: ActionRetry, Reason: err.Error()}
}

// ClassifyStatus maps an HTTP status code to a spool action.
func ClassifyStatus(status int, retryAfter time.Duration, reason string) Decision {
	if reason == "" {
		reason = fmt.Sprintf("HTTP %d", status)
	}
	switch {
	case status >= 200 && status <= 299:
		return Decision{Action: ActionAck, Reason: reason}
	case status == 408 || status == 429 || status >= 500:
		return Decision{Action: ActionRetry, RetryAfter: retryAfter, Reason: reason}
	case status == 401 || status == 403:
		return Decision{Action: ActionPauseAuth, Reason: reason}
	case status == 404:
		return Decision{Action: ActionPauseEndpoint, Reason: reason}
	case status == 413:
		return Decision{Action: ActionShrinkBatch, Reason: reason}
	case status == 400:
		return Decision{Action: ActionReject, Reason: reason}
	default:
		// Other 4xx: treat as bad payload (PHP Rejected), not configuration.
		return Decision{Action: ActionReject, Reason: reason}
	}
}

// For413 returns ActionReject when a 413 applies to a single-profile batch.
func For413(batchProfiles int) Action {
	if batchProfiles <= 1 {
		return ActionReject
	}
	return ActionShrinkBatch
}

func isTransientNet(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	var op *net.OpError
	return errors.As(err, &op)
}

// Backoff returns how long to wait before the next retry.
// attempt is 0-based. When hint (Retry-After) is positive it wins over exponential backoff.
func Backoff(attempt int, hint, base, maxDelay time.Duration, rng *rand.Rand) time.Duration {
	if hint > 0 {
		if maxDelay > 0 && hint > maxDelay {
			return maxDelay
		}
		return hint
	}
	if base <= 0 {
		base = time.Second
	}
	if maxDelay <= 0 {
		maxDelay = 5 * time.Minute
	}
	if attempt < 0 {
		attempt = 0
	}
	// Cap shift to avoid Duration overflow.
	shift := attempt
	if shift > 20 {
		shift = 20
	}
	d := base << shift
	if d > maxDelay || d <= 0 {
		d = maxDelay
	}
	if rng == nil {
		return d
	}
	// Full jitter in [0, d]; d is positive after clamping to maxDelay.
	return time.Duration(rng.Int63n(int64(d) + 1))
}
