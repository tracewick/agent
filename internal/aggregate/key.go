package aggregate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"

	"github.com/tracewick/agent/internal/spool"
)

// Key is the ADR-12 aggregation key: (application, labels, event_type, period_ns).
type Key struct {
	Application string
	EventType   spool.EventType
	PeriodNs    int64
	Labels      map[string]string
}

// KeyFromFile builds a Key from a parsed SpoolFile header.
func KeyFromFile(f *spool.File) (Key, error) {
	if f == nil {
		return Key{}, fmt.Errorf("aggregate: nil spool file")
	}
	if f.Application == "" {
		return Key{}, fmt.Errorf("aggregate: application is required")
	}
	if f.PeriodNs < 1 {
		return Key{}, fmt.Errorf("aggregate: period_ns must be at least 1")
	}
	switch f.EventType {
	case spool.EventWall, spool.EventCPU:
	default:
		return Key{}, fmt.Errorf("aggregate: unknown event type %q", f.EventType)
	}
	labels := maps.Clone(f.Labels)
	if labels == nil {
		labels = map[string]string{}
	}
	return Key{
		Application: f.Application,
		EventType:   f.EventType,
		PeriodNs:    f.PeriodNs,
		Labels:      labels,
	}, nil
}

// Equal reports whether two keys match after sorting label keys (exact string values).
func (k Key) Equal(other Key) bool {
	if k.Application != other.Application || k.EventType != other.EventType || k.PeriodNs != other.PeriodNs {
		return false
	}
	return maps.Equal(k.Labels, other.Labels)
}

// Hash returns the first 16 hex chars of SHA-256 over the PHP-compatible NUL-joined parts.
//
// Canon (SpoolAggregationKey::hash): ksort(labels, SORT_STRING); parts =
// application, eventType.value, periodNs decimal, then each key=value; implode("\0").
func (k Key) Hash() string {
	return Hash(k.Application, k.Labels, k.EventType, k.PeriodNs)
}

// Hash is the package-level form of Key.Hash (parity with PHP SpoolAggregationKey::hash).
func Hash(application string, labels map[string]string, event spool.EventType, periodNs int64) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys) // matches PHP SORT_STRING for ASCII keys

	parts := make([]string, 0, 3+len(keys))
	parts = append(parts, application, string(event), strconv.FormatInt(periodNs, 10))
	for _, key := range keys {
		parts = append(parts, key+"="+labels[key])
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])[:16]
}

// Shard returns the two-hex shard directory prefix (first two chars of the hash).
func Shard(aggregationKeyHash string) string {
	if len(aggregationKeyHash) < 2 {
		return aggregationKeyHash
	}
	return aggregationKeyHash[:2]
}
