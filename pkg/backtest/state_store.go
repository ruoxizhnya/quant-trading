package backtest

// P1-18 (ODR-013 Sprint 6): StateStore interface + LRU eviction.
//
// The Engine previously held completed backtest states in an
// unbounded `backtests map[string]*BacktestState`. Under load (e.g.
// a CI suite running 10k batch backtests) the map grew without
// bound and eventually OOM'd. This file introduces:
//
//   - StateStore interface     — Get / Put / Delete / Len / Evict
//   - LRUStateStore            — in-memory implementation with
//                                 bounded size (default 1000)
//   - NoopStateStore           — never evicts; useful for tests
//
// The Engine continues to use the in-memory state pointer for the
// duration of a single run (so the running goroutine can read its
// own state without a hop), but completion / persistence is now
// routed through the StateStore so old runs can be evicted.
//
// Persistence (PG) is deliberately NOT implemented in this PR —
// the P1-18 spec calls for the interface + LRU; a follow-up can
// add a PersistedStateStore that mirrors writes to the
// backtest_jobs table.

import (
	"container/list"
	"sync"
)

// StateStore is the contract for a backtest state cache with
// optional eviction semantics.
//
// Implementations must be safe for concurrent use. The Engine calls
// Put once at the start of a backtest and (typically) Delete on
// eviction; Get is used by the API layer's result/trades/equity
// handlers.
type StateStore interface {
	// Get returns the state for id, or (nil, false) if no such state.
	Get(id string) (state *BacktestState, ok bool)
	// Put inserts (or overwrites) the state for id.
	Put(id string, state *BacktestState)
	// Delete removes the state for id. No-op if absent.
	Delete(id string)
	// Len returns the number of currently stored states.
	Len() int
	// Evict drops the oldest states until at most keepN remain.
	// keepN=0 means evict all. Returns the number of entries removed.
	Evict(keepN int) (evicted int)
}

// ------------------------------------------------------------------------------
// LRUStateStore
// ------------------------------------------------------------------------------

// lruEntry wraps a stored *BacktestState for the doubly-linked list.
type lruEntry struct {
	id    string
	state *BacktestState
}

// LRUStateStore is an in-memory StateStore with LRU eviction.
//
// The default capacity is 1000 entries (per P1-18 spec). When a
// Put would exceed the capacity, the least-recently-used entry is
// evicted. Reads via Get also promote the entry to MRU.
//
// Concurrency: a single sync.Mutex protects both the map and the
// list. LRU operations are O(1) but require the lock; the
// contention is negligible because the API call rate is at most
// one Engine.Run per second per user.
type LRUStateStore struct {
	mu       sync.Mutex
	capacity int
	items    map[string]*list.Element // id → *list.Element of lruEntry
	order    *list.List               // front = MRU, back = LRU
}

// NewLRUStateStore returns an LRUStateStore with the given capacity.
// A capacity of 0 or less is treated as unbounded (no eviction),
// but in that case a NoopStateStore is more efficient.
func NewLRUStateStore(capacity int) *LRUStateStore {
	return &LRUStateStore{
		capacity: capacity,
		items:    make(map[string]*list.Element),
		order:    list.New(),
	}
}

// Get returns the state for id. The entry is promoted to MRU on hit.
func (s *LRUStateStore) Get(id string) (*BacktestState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	elem, ok := s.items[id]
	if !ok {
		return nil, false
	}
	s.order.MoveToFront(elem)
	return elem.Value.(*lruEntry).state, true
}

// Put inserts or overwrites the state for id. If the store is at
// capacity, the LRU entry is evicted to make room.
func (s *LRUStateStore) Put(id string, state *BacktestState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if elem, ok := s.items[id]; ok {
		// Overwrite: update the value and promote.
		elem.Value.(*lruEntry).state = state
		s.order.MoveToFront(elem)
		return
	}

	elem := s.order.PushFront(&lruEntry{id: id, state: state})
	s.items[id] = elem

	if s.capacity > 0 && s.order.Len() > s.capacity {
		// Evict the LRU entry.
		oldest := s.order.Back()
		if oldest != nil {
			s.order.Remove(oldest)
			delete(s.items, oldest.Value.(*lruEntry).id)
		}
	}
}

// Delete removes the entry for id. No-op if absent.
func (s *LRUStateStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	elem, ok := s.items[id]
	if !ok {
		return
	}
	s.order.Remove(elem)
	delete(s.items, id)
}

// Len returns the current number of stored states.
func (s *LRUStateStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.order.Len()
}

// Evict drops the oldest states until at most keepN remain.
// keepN=0 means evict all. Returns the number of entries removed.
//
// This is the bulk-eviction primitive used by periodic GC sweeps
// (e.g. a goroutine that runs every 5 minutes to keep memory
// usage flat under bursty batch loads).
func (s *LRUStateStore) Evict(keepN int) (evicted int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for s.order.Len() > keepN {
		oldest := s.order.Back()
		if oldest == nil {
			break
		}
		s.order.Remove(oldest)
		delete(s.items, oldest.Value.(*lruEntry).id)
		evicted++
	}
	return evicted
}

// Capacity returns the configured maximum. 0 means unbounded.
func (s *LRUStateStore) Capacity() int {
	return s.capacity
}

// ------------------------------------------------------------------------------
// NoopStateStore (testing / dev)
// ------------------------------------------------------------------------------

// NoopStateStore is a StateStore that never evicts. Useful in
// tests where state assertions depend on the map growing without
// interference, or for short-lived CLI runs that don't need LRU
// semantics.
type NoopStateStore struct {
	mu    sync.RWMutex
	items map[string]*BacktestState
}

// NewNoopStateStore returns an unbounded StateStore.
func NewNoopStateStore() *NoopStateStore {
	return &NoopStateStore{items: make(map[string]*BacktestState)}
}

// Get implements StateStore.
func (s *NoopStateStore) Get(id string) (*BacktestState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.items[id]
	return st, ok
}

// Put implements StateStore.
func (s *NoopStateStore) Put(id string, state *BacktestState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[id] = state
}

// Delete implements StateStore.
func (s *NoopStateStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, id)
}

// Len implements StateStore.
func (s *NoopStateStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items)
}

// Evict implements StateStore. Since NoopStateStore never evicts,
// Evict(0) clears the map and returns the cleared count. Evict(N>0)
// is a no-op.
func (s *NoopStateStore) Evict(keepN int) int {
	if keepN != 0 {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.items)
	s.items = make(map[string]*BacktestState)
	return n
}
