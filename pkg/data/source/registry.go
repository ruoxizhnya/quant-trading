package source

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Registry manages a set of DataSourceAdapter instances and per-data-type
// fallback chains. It is the single entry point used by ETL pipelines.
//
// Concurrency: Registry is safe for concurrent use. Adapters are expected
// to be concurrency-safe themselves; the Registry holds a RWMutex over the
// adapter map and chain map.
//
// Fallback semantics:
//
//   - For a given data type, the Registry tries adapters in priority order
//     (1, 2, 3, ...). The first adapter that returns data wins.
//   - Adapters that return IsRetryable(err)==true are skipped, and the next
//     adapter is tried. Adapters that return other errors (e.g. validation)
//     propagate immediately because retrying with a different source won't
//     help.
//   - All retries are exhausted → the last error is wrapped and returned.
//
// Persistence: the Registry does not write to data_source_registry /
// data_fallback_chain on its own. Instead, InitializeFromDB seeds it from
// the persisted configuration. Adapters self-register at construction.
type Registry struct {
	mu sync.RWMutex

	// adapters maps name → adapter.
	adapters map[string]DataSourceAdapter

	// chains maps data_type → ordered list of adapter names.
	// The list is sorted by priority (ascending); index 0 is the primary.
	chains map[string][]string
}

// NewRegistry constructs an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		adapters: make(map[string]DataSourceAdapter),
		chains:   make(map[string][]string),
	}
}

// Register adds an adapter under its Name().
//
// If an adapter with the same name is already registered, the new one
// replaces it. This is useful for hot reload of configuration.
func (r *Registry) Register(a DataSourceAdapter) error {
	if a == nil {
		return errors.New("registry: nil adapter")
	}
	name := a.Name()
	if name == "" {
		return errors.New("registry: adapter Name() is empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[name] = a

	// Add the adapter to the chain for every data type it supports,
	// appending to the end (lower priority than existing members).
	for _, dt := range a.SupportedTypes() {
		r.chains[dt] = appendUnique(r.chains[dt], name)
	}
	return nil
}

// SetChain overrides the fallback chain for a data type entirely.
// Useful for tests and for explicit configuration in code.
func (r *Registry) SetChain(dataType string, names []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]string, len(names))
	copy(cp, names)
	r.chains[dataType] = cp
}

// GetAdapter returns the adapter registered under name (or nil).
func (r *Registry) GetAdapter(name string) DataSourceAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.adapters[name]
}

// ListAdapters returns all registered adapter names in sorted order.
// Stable for tests.
func (r *Registry) ListAdapters() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.adapters))
	for k := range r.adapters {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ListChains returns a snapshot of (data_type, chain) pairs.
func (r *Registry) ListChains() map[string][]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string][]string, len(r.chains))
	for k, v := range r.chains {
		cp := make([]string, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

// Fetch retrieves data using the fallback chain for req.DataType.
//
// Behavior:
//   - If the chain is empty, ErrUnsupported is returned.
//   - Each enabled adapter in the chain is tried in order.
//   - IsRetryable(err) → skip and try next.
//   - Other errors → return immediately (caller can decide).
//   - Success → return the FetchResponse (the Source field is set).
//
// If all adapters in the chain fail with retryable errors, Fetch returns
// the last error wrapped with the tried adapter names for diagnostics.
func (r *Registry) Fetch(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	if err := Validate(req); err != nil {
		return nil, err
	}

	r.mu.RLock()
	chain := append([]string(nil), r.chains[req.DataType]...)
	r.mu.RUnlock()

	if len(chain) == 0 {
		return nil, fmt.Errorf("%w: %s (no chain configured)", ErrUnsupported, req.DataType)
	}

	var lastErr error
	tried := make([]string, 0, len(chain))
	for _, name := range chain {
		r.mu.RLock()
		a := r.adapters[name]
		r.mu.RUnlock()
		if a == nil {
			lastErr = fmt.Errorf("registry: adapter %q not registered", name)
			tried = append(tried, name)
			continue
		}
		if !a.Enabled() {
			continue
		}
		// Verify the adapter actually claims to support this data type
		// (defensive: chains and SupportedTypes can drift).
		supported := false
		for _, dt := range a.SupportedTypes() {
			if dt == req.DataType {
				supported = true
				break
			}
		}
		if !supported {
			continue
		}

		start := time.Now()
		resp, err := a.Fetch(ctx, req)
		if err == nil && resp != nil {
			if resp.Source == "" {
				resp.Source = a.Name()
			}
			resp.FetchedAt = start
			return resp, nil
		}
		lastErr = err
		tried = append(tried, name)
		if !IsRetryable(err) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("registry: all adapters exhausted for %s (tried %v): %w",
		req.DataType, tried, lastErr)
}

// HealthCheck runs HealthCheck on every registered adapter and returns a
// map name→error. A nil error means healthy.
func (r *Registry) HealthCheck(ctx context.Context) map[string]error {
	r.mu.RLock()
	names := make([]string, 0, len(r.adapters))
	snapshot := make(map[string]DataSourceAdapter, len(r.adapters))
	for n, a := range r.adapters {
		names = append(names, n)
		snapshot[n] = a
	}
	r.mu.RUnlock()
	sort.Strings(names)

	// CR-20 (ODR-012): the previous loop called `a.HealthCheck(ctx)` serially
	// in the registry's read-lock-free section. For 9 adapters each with a
	// 500ms-cold TCP/HTTP probe, the API call to /api/datasource/registry/health
	// took 4.5s. Run them concurrently under a single shared context, but
	// cap parallelism at 8 so we don't open 100 TCP connections if a future
	// config files in 50 adapters. The map write is safe — each goroutine
	// writes a distinct key.
	const maxParallel = 8
	out := make(map[string]error, len(names))
	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)
	sem := make(chan struct{}, maxParallel)

	for _, n := range names {
		a := snapshot[n]
		if a == nil {
			out[n] = errors.New("nil adapter")
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(name string, adapter DataSourceAdapter) {
			defer wg.Done()
			defer func() { <-sem }()
			err := adapter.HealthCheck(ctx)
			mu.Lock()
			out[name] = err
			mu.Unlock()
		}(n, a)
	}
	wg.Wait()
	return out
}

// appendUnique appends v to s if not already present (order-preserving).
func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}
