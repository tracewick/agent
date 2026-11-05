package delivery

import (
	"fmt"
	"math"

	"github.com/tracewick/agent/internal/pprof"
)

const (
	DefaultMaxDecodedBytes     = 32 << 20
	DefaultMaxHeldProfiles     = 256
	DefaultMaxHeldDecodedBytes = 64 << 20
	DefaultMaxAggregatedPprof  = 16 << 20
	DefaultMaxInFlightBytes    = 64 << 20
	DefaultMaxStringBytes      = pprof.DefaultMaxStringBytes
	DefaultMaxFrameRefs        = pprof.DefaultMaxFrameRefs
	minPositive                = 1
)

// Limits caps decode / merge / upload memory before large allocations.
type Limits struct {
	MaxDecodedBytes         int64
	MaxStacks               int
	MaxFrames               int
	MaxStrings              int
	MaxStringBytes          int
	MaxFrameRefs            int
	MaxHeldProfiles         int
	MaxHeldDecodedBytes     int64 // sum of Profile.EstimatedBytes held decoded in one pass
	MaxAggregatedPprofBytes int64
	MaxInFlightBytes        int64 // cap on sum of claimed spool bytes in a pass and on encoded push payload
}

// DefaultLimits returns conservative v1 defaults aligned with BoundedBuilder caps where applicable.
func DefaultLimits() Limits {
	return Limits{
		MaxDecodedBytes:         DefaultMaxDecodedBytes,
		MaxStacks:               pprof.DefaultMaxStacks,
		MaxFrames:               pprof.DefaultMaxFrames,
		MaxStrings:              pprof.DefaultMaxStrings,
		MaxStringBytes:          DefaultMaxStringBytes,
		MaxFrameRefs:            DefaultMaxFrameRefs,
		MaxHeldProfiles:         DefaultMaxHeldProfiles,
		MaxHeldDecodedBytes:     DefaultMaxHeldDecodedBytes,
		MaxAggregatedPprofBytes: DefaultMaxAggregatedPprof,
		MaxInFlightBytes:        DefaultMaxInFlightBytes,
	}
}

// DecodeLimits maps delivery caps onto compact-stacks decode budgets (wire may
// include one overflow stack, frame, and string beyond the builder caps).
func (l *Limits) DecodeLimits() pprof.DecodeLimits {
	return pprof.DecodeLimits{
		MaxStacks:      l.MaxStacks,
		MaxFrames:      l.MaxFrames,
		MaxStrings:     l.MaxStrings,
		MaxStringBytes: l.MaxStringBytes,
		MaxFrameRefs:   l.MaxFrameRefs,
	}
}

// EncodeLimits maps delivery caps onto pprof encode budgets.
func (l *Limits) EncodeLimits() pprof.EncodeLimits {
	return pprof.EncodeLimits{
		MaxStringBytes:  l.MaxStringBytes,
		MaxEncodedBytes: int(l.MaxAggregatedPprofBytes), //nolint:gosec // validated positive
		MaxNameBytes:    pprof.DefaultMaxNameBytes,
	}
}

// Validate checks that every limit is positive and within builder hard caps where shared.
func (l *Limits) Validate() error {
	if l.MaxDecodedBytes < minPositive {
		return fmt.Errorf("delivery: max decoded bytes must be >= %d", minPositive)
	}
	if l.MaxStacks < minPositive || l.MaxStacks > 100_000 {
		return fmt.Errorf("delivery: max stacks must be between %d and %d", minPositive, 100_000)
	}
	if l.MaxFrames < minPositive || l.MaxFrames > 500_000 {
		return fmt.Errorf("delivery: max frames must be between %d and %d", minPositive, 500_000)
	}
	if l.MaxStrings < minPositive || l.MaxStrings > 1_000_000 {
		return fmt.Errorf("delivery: max strings must be between %d and %d", minPositive, 1_000_000)
	}
	if l.MaxStringBytes < minPositive {
		return fmt.Errorf("delivery: max string bytes must be >= %d", minPositive)
	}
	if l.MaxFrameRefs < minPositive {
		return fmt.Errorf("delivery: max frame refs must be >= %d", minPositive)
	}
	if l.MaxHeldProfiles < minPositive {
		return fmt.Errorf("delivery: max held profiles must be >= %d", minPositive)
	}
	if l.MaxHeldDecodedBytes < minPositive {
		return fmt.Errorf("delivery: max held decoded bytes must be >= %d", minPositive)
	}
	if l.MaxAggregatedPprofBytes < minPositive {
		return fmt.Errorf("delivery: max aggregated pprof bytes must be >= %d", minPositive)
	}
	if l.MaxInFlightBytes < minPositive {
		return fmt.Errorf("delivery: max in-flight bytes must be >= %d", minPositive)
	}
	return nil
}

// SampleValue multiplies stack weight by period for pprof sample.value (int64).
func SampleValue(count uint64, periodNs int64) (uint64, error) {
	if periodNs < 0 {
		return 0, fmt.Errorf("delivery: period_ns must be non-negative, got %d", periodNs)
	}
	if count == 0 || periodNs == 0 {
		return 0, nil
	}
	period := uint64(periodNs) //nolint:gosec // periodNs >= 0
	if count > uint64(math.MaxInt64)/period {
		return 0, fmt.Errorf("delivery: sample value overflow: count %d × period %d exceeds int64", count, periodNs)
	}
	return count * period, nil
}

// AcceptDecoded rejects payloads that already exceed the decoded-size budget before further work.
func (l *Limits) AcceptDecoded(n int) error {
	if n < 0 {
		return fmt.Errorf("delivery: decoded size is negative")
	}
	if int64(n) > l.MaxDecodedBytes { //nolint:gosec // n >= 0
		return fmt.Errorf("delivery: decoded payload of %d bytes exceeds limit of %d", n, l.MaxDecodedBytes)
	}
	return nil
}
