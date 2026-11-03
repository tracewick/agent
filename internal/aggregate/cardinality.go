package aggregate

import (
	"fmt"

	"github.com/tracewick/agent/internal/spool"
)

// CardinalityGate caps distinct ADR-12 keys; overflow keeps base labels and sets cardinality=other.
type CardinalityGate struct {
	store         CounterStore
	maxUniqueKeys int
}

const (
	DefaultMaxUniqueKeys = 256
	CollapsedLabel       = "cardinality"
	CollapsedValue       = "other"
	metaUnique           = "tracewick:agg:unique_count"
	seenPrefix           = "tracewick:agg:seen:"
)

// BaseLabelKeys are preserved on overflow (PHP CardinalityGate::BASE_LABEL_KEYS).
var BaseLabelKeys = []string{"service", "env"}

// NewCardinalityGate returns a gate. store defaults to MemoryStore; maxUniqueKeys defaults to 256.
func NewCardinalityGate(store CounterStore, maxUniqueKeys int) (*CardinalityGate, error) {
	if maxUniqueKeys < 1 {
		return nil, fmt.Errorf("aggregate: max unique keys must be at least 1, got %d", maxUniqueKeys)
	}
	if store == nil {
		store = NewMemoryStore()
	}
	return &CardinalityGate{store: store, maxUniqueKeys: maxUniqueKeys}, nil
}

// LimitLabels applies the gate. Known keys stay unchanged; overflow collapses to base + cardinality=other.
func (g *CardinalityGate) LimitLabels(application string, labels map[string]string, event spool.EventType, periodNs int64) map[string]string {
	hash := Hash(application, labels, event, periodNs)
	seenKey := seenPrefix + hash
	if g.store.Get(seenKey) > 0 {
		g.store.Increment(seenKey)
		return cloneLabels(labels)
	}
	if g.store.Get(metaUnique) >= int64(g.maxUniqueKeys) {
		return overflowLabels(labels)
	}
	g.store.Increment(metaUnique)
	g.store.Increment(seenKey)
	return cloneLabels(labels)
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
