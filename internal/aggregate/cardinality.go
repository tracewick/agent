package aggregate

import (
	"fmt"
	"sync"
	"time"

	"github.com/tracewick/agent/internal/spool"
)

// CardinalityGate caps distinct ADR-12 keys; overflow keeps base labels and sets cardinality=other.
// Admit is serialized so parallel push (future) cannot race the unique-key counter.
type CardinalityGate struct {
	mu              sync.Mutex
	store           CounterStore // optional diagnostic mirror (PHP CounterStore keys)
	maxUniqueKeys   int
	ttl             time.Duration
	now             func() time.Time
	seen            map[string]time.Time // hash → last Admit
	disableOverflow bool                 // NullStore: never collapse
}

const (
	DefaultMaxUniqueKeys = 256
	// DefaultCardinalityTTL forgets idle keys so a long-lived agent does not
	// permanently collapse every new route after the first maxUniqueKeys.
	DefaultCardinalityTTL = time.Hour
	CollapsedLabel        = "cardinality"
	CollapsedValue        = "other"
	metaUnique            = "tracewick:agg:unique_count"
	seenPrefix            = "tracewick:agg:seen:"
)

// BaseLabelKeys are preserved on overflow (PHP CardinalityGate::BASE_LABEL_KEYS).
var BaseLabelKeys = []string{"service", "env"}

// NewCardinalityGate returns a gate. store defaults to MemoryStore; maxUniqueKeys defaults to 256.
// NullStore disables overflow (never collapses). Idle keys expire after DefaultCardinalityTTL.
func NewCardinalityGate(store CounterStore, maxUniqueKeys int) (*CardinalityGate, error) {
	if maxUniqueKeys < 1 {
		return nil, fmt.Errorf("aggregate: max unique keys must be at least 1, got %d", maxUniqueKeys)
	}
	disable := false
	if store == nil {
		store = NewMemoryStore()
	} else if _, ok := store.(NullStore); ok {
		disable = true
	}
	return &CardinalityGate{
		store:           store,
		maxUniqueKeys:   maxUniqueKeys,
		ttl:             DefaultCardinalityTTL,
		now:             time.Now,
		seen:            map[string]time.Time{},
		disableOverflow: disable,
	}, nil
}

// SetTTL overrides the idle eviction window (tests). Zero keeps DefaultCardinalityTTL.
func (g *CardinalityGate) SetTTL(ttl time.Duration) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if ttl > 0 {
		g.ttl = ttl
	}
}

// SetNow overrides the clock (tests).
func (g *CardinalityGate) SetNow(now func() time.Time) {
	if g == nil || now == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.now = now
}

// Admit records hash under one critical section. overflow means the key is new and
// the unique-key budget is exhausted (caller should collapse labels).
func (g *CardinalityGate) Admit(hash string) (overflow bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.disableOverflow {
		return false
	}
	now := g.now()
	g.purgeExpiredLocked(now)
	if _, ok := g.seen[hash]; ok {
		g.seen[hash] = now
		_ = g.store.Increment(seenPrefix + hash)
		return false
	}
	if len(g.seen) >= g.maxUniqueKeys {
		return true
	}
	g.seen[hash] = now
	_ = g.store.Increment(metaUnique)
	_ = g.store.Increment(seenPrefix + hash)
	return false
}

// LimitLabels applies the gate. Known keys stay unchanged; overflow collapses to base + cardinality=other.
func (g *CardinalityGate) LimitLabels(application string, labels map[string]string, event spool.EventType, periodNs int64) map[string]string {
	hash := Hash(application, labels, event, periodNs)
	if g.Admit(hash) {
		return overflowLabels(labels)
	}
	return cloneLabels(labels)
}

func (g *CardinalityGate) purgeExpiredLocked(now time.Time) {
	if g.ttl <= 0 || len(g.seen) == 0 {
		return
	}
	for hash, last := range g.seen {
		if now.Sub(last) < g.ttl {
			continue
		}
		delete(g.seen, hash)
	}
}

func overflowLabels(labels map[string]string) map[string]string {
	kept := make(map[string]string, len(BaseLabelKeys)+1)
	for _, key := range BaseLabelKeys {
		if v, ok := labels[key]; ok {
			kept[key] = v
		}
	}
	kept[CollapsedLabel] = CollapsedValue
	return kept
}

func cloneLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(labels))
	for k, v := range labels {
		out[k] = v
	}
	return out
}
