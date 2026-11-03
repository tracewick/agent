package aggregate

import "sync"

// CounterStore is the CardinalityGate backing store (PHP Diagnostics\CounterStore).
type CounterStore interface {
	Get(key string) int64
	Increment(key string) int64
}

// MemoryStore is a process-local store (PHP ArrayCounterStore).
type MemoryStore struct {
	mu   sync.Mutex
	data map[string]int64
}

// NewMemoryStore returns an empty in-memory counter store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: map[string]int64{}}
}

// Get returns the counter value (0 if missing).
func (s *MemoryStore) Get(key string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[key]
}

// Increment adds one and returns the new value.
func (s *MemoryStore) Increment(key string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key]++
	return s.data[key]
}

// All returns a copy of all counters (tests / diagnostics).
func (s *MemoryStore) All() map[string]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int64, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out
}

// NullStore never increments and always returns 0 — CardinalityGate never collapses.
type NullStore struct{}

// Get always returns 0.
func (NullStore) Get(string) int64 { return 0 }

// Increment is a no-op and returns 0.
func (NullStore) Increment(string) int64 { return 0 }
