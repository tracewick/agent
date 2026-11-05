package aggregate

import "sync"

// CounterStore is the CardinalityGate backing store (PHP Diagnostics\CounterStore).
type CounterStore interface {
	Get(key string) int64
	Increment(key string) int64
	Add(key string, delta int64) int64
	Delete(key string)
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
	return s.Add(key, 1)
}

// Add applies delta and returns the new value (missing keys start at 0).
func (s *MemoryStore) Add(key string, delta int64) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] += delta
	if s.data[key] == 0 {
		delete(s.data, key)
		return 0
	}
	return s.data[key]
}

// Delete removes a key.
func (s *MemoryStore) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
}

// Len returns how many keys are stored (tests).
func (s *MemoryStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.data)
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

// Add is a no-op and returns 0.
func (NullStore) Add(string, int64) int64 { return 0 }

// Delete is a no-op.
func (NullStore) Delete(string) {}
