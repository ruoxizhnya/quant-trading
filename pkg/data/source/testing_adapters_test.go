package source

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// mockAdapter is a configurable DataSourceAdapter used across the
// registry/ETL/reconcile tests. It records every Fetch invocation
// and returns whatever the test set up via the response / err fields.
//
// Concurrency: safe for concurrent Fetch (the Registry can call from
// multiple goroutines). The call counter is atomic-equivalent under mu.
type mockAdapter struct {
	AdapterBase

	mu sync.Mutex

	// supported is the data types this mock serves.
	supported []string

	// response is returned by Fetch when err is nil.
	response *FetchResponse

	// err is returned by Fetch when non-nil. If err is wrapped with
	// ErrRateLimited or ErrUpstreamUnavailable, the registry treats
	// it as retryable.
	err error

	// calls records every Fetch invocation in order. Tests assert on
	// the call count and the request payload.
	calls []FetchRequest
}

// newMockAdapter constructs a mock adapter with sensible defaults.
func newMockAdapter(name string, supported []string) *mockAdapter {
	return &mockAdapter{
		AdapterBase: NewAdapterBase(name, true),
		supported:   supported,
	}
}

// Type implements DataSourceAdapter.Type.
func (m *mockAdapter) Type() AdapterType { return AdapterTypeHTTP }

// SupportedTypes implements DataSourceAdapter.SupportedTypes.
func (m *mockAdapter) SupportedTypes() []string { return m.supported }

// Schema implements DataSourceAdapter.Schema.
func (m *mockAdapter) Schema(dataType string) (DataSchema, error) {
	for _, s := range m.supported {
		if s == dataType {
			return DataSchema{DataType: dataType}, nil
		}
	}
	return DataSchema{}, fmt.Errorf("%w: mock %s does not serve %s", ErrUnsupported, m.Name(), dataType)
}

// Fetch implements DataSourceAdapter.Fetch.
func (m *mockAdapter) Fetch(ctx context.Context, req FetchRequest) (*FetchResponse, error) {
	m.mu.Lock()
	m.calls = append(m.calls, req)
	resp := m.response
	err := m.err
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return &FetchResponse{Source: m.Name(), Items: nil, FetchedAt: time.Now().UTC()}, nil
	}
	// Return a copy so the test can mutate the response between calls
	// without affecting what other goroutines see.
	cp := *resp
	cp.Source = m.Name()
	if cp.FetchedAt.IsZero() {
		cp.FetchedAt = time.Now().UTC()
	}
	return &cp, nil
}

// HealthCheck implements DataSourceAdapter.HealthCheck.
func (m *mockAdapter) HealthCheck(ctx context.Context) error {
	return nil
}

// RateLimit implements DataSourceAdapter.RateLimit.
func (m *mockAdapter) RateLimit() RateLimitConfig {
	return RateLimitConfig{RequestsPerMinute: 1000, Burst: 100}
}

// setResponse swaps the response and clears any prior error.
func (m *mockAdapter) setResponse(resp *FetchResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.response = resp
	m.err = nil
}

// setErr swaps the error and clears any prior response.
func (m *mockAdapter) setErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
	m.response = nil
}

// callCount returns the number of Fetch invocations so far.
func (m *mockAdapter) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// lastCall returns the most recent Fetch invocation, or false if none.
func (m *mockAdapter) lastCall() (FetchRequest, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		return FetchRequest{}, false
	}
	return m.calls[len(m.calls)-1], true
}
