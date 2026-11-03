package delivery

import (
	"fmt"
	"time"
)

const (
	// DefaultStaleClaim matches the spool source / PHP library default (10 minutes).
	DefaultStaleClaim = 10 * time.Minute
	// DefaultOwnershipMargin is subtracted from staleClaimAge so ack/release finishes early.
	DefaultOwnershipMargin = time.Minute
)

// Ownership tracks the claim ownership budget: time from claim to ack/release
// must stay strictly below staleClaimAge with margin (see spool-format claim
// protocol and AGENTS.md).
type Ownership struct {
	StaleClaim time.Duration
	Margin     time.Duration
}

// NewOwnership validates durations. Zero StaleClaim/Margin select defaults.
func NewOwnership(staleClaim, margin time.Duration) (Ownership, error) {
	if staleClaim == 0 {
		staleClaim = DefaultStaleClaim
	}
	if margin == 0 {
		margin = DefaultOwnershipMargin
	}
	if staleClaim <= 0 {
		return Ownership{}, fmt.Errorf("delivery: stale claim age must be positive, got %s", staleClaim)
	}
	if margin < 0 {
		return Ownership{}, fmt.Errorf("delivery: ownership margin must be non-negative, got %s", margin)
	}
	if margin >= staleClaim {
		return Ownership{}, fmt.Errorf("delivery: ownership margin %s must be less than stale claim age %s", margin, staleClaim)
	}
	return Ownership{StaleClaim: staleClaim, Margin: margin}, nil
}

// Budget is the maximum wall time a reader may hold a claim (staleClaim − margin).
func (o Ownership) Budget() time.Duration {
	return o.StaleClaim - o.Margin
}

// Remaining is Budget minus elapsed since claimedAt. Negative means exhausted.
func (o Ownership) Remaining(claimedAt, now time.Time) time.Duration {
	if claimedAt.IsZero() {
		return o.Budget()
	}
	elapsed := now.Sub(claimedAt)
	return o.Budget() - elapsed
}

// Exhausted reports whether the ownership budget is used up.
func (o Ownership) Exhausted(claimedAt, now time.Time) bool {
	return o.Remaining(claimedAt, now) <= 0
}

// WithinHTTP reports whether an HTTP timeout fits in the remaining budget.
func (o Ownership) WithinHTTP(claimedAt, now time.Time, httpTimeout time.Duration) bool {
	return o.Remaining(claimedAt, now) > httpTimeout
}
