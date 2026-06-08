package source

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// L1: validate FetchRequest input handling.

func TestValidate_OK(t *testing.T) {
	req := FetchRequest{
		DataType:  DataTypeOHLCDaily,
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
	}
	if err := Validate(req); err != nil {
		t.Fatalf("expected nil error for valid request, got %v", err)
	}
}

func TestValidate_EmptyDataType(t *testing.T) {
	req := FetchRequest{
		StartDate: time.Now().UTC(),
		EndDate:   time.Now().UTC(),
	}
	err := Validate(req)
	if err == nil {
		t.Fatal("expected error for empty DataType, got nil")
	}
	if !strings.Contains(err.Error(), "empty DataType") {
		t.Errorf("error message %q should mention empty DataType", err)
	}
}

func TestValidate_EndBeforeStart(t *testing.T) {
	req := FetchRequest{
		DataType:  DataTypeOHLCDaily,
		StartDate: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	err := Validate(req)
	if err == nil {
		t.Fatal("expected error for EndDate < StartDate, got nil")
	}
	if !strings.Contains(err.Error(), "EndDate") {
		t.Errorf("error message %q should mention EndDate", err)
	}
}

// L1: IsRetryable classification.

func TestIsRetryable_Retryable(t *testing.T) {
	cases := []error{
		ErrRateLimited,
		ErrUpstreamUnavailable,
		fmt.Errorf("wrapped: %w", ErrRateLimited),
		fmt.Errorf("wrapped: %w", ErrUpstreamUnavailable),
	}
	for i, err := range cases {
		if !IsRetryable(err) {
			t.Errorf("case %d: expected IsRetryable(%v)=true", i, err)
		}
	}
}

func TestIsRetryable_NonRetryable(t *testing.T) {
	cases := []error{
		nil,
		errors.New("generic error"),
		ErrUnsupported,
		fmt.Errorf("validation: bad date"),
	}
	for i, err := range cases {
		if IsRetryable(err) {
			t.Errorf("case %d: expected IsRetryable(%v)=false", i, err)
		}
	}
}

// L1: AdapterBase.

func TestAdapterBase_Name(t *testing.T) {
	b := NewAdapterBase("tushare", true)
	if got := b.Name(); got != "tushare" {
		t.Errorf("Name() = %q, want %q", got, "tushare")
	}
}

func TestAdapterBase_Enabled(t *testing.T) {
	b := NewAdapterBase("x", false)
	if b.Enabled() {
		t.Error("Enabled() = true, want false")
	}
	b.SetEnabled(true)
	if !b.Enabled() {
		t.Error("Enabled() = false after SetEnabled(true), want true")
	}
}

// L1: interface compliance for every concrete adapter.
//
// Catches drift when someone adds a method to DataSourceAdapter but
// forgets to update one of the implementations.

func TestInterfaceCompliance(t *testing.T) {
	// Build one of each adapter. Disabled is fine for this test — we
	// only check that the type satisfies the interface.
	adapters := []DataSourceAdapter{
		NewTushareAdapter(nil),
		NewMootdxAdapter(nil),
		NewEastmoneyAdapter(NewEastmoneyClient()),
		NewEastmoneySectorsAdapter(NewEastmoneyClient()),
		NewEastmoneyTopListAdapter(NewEastmoneyClient()),
		NewJuchaoAdapter(),
		NewXueqiuAdapter(),
		NewAlphaVantageAdapter(""),
		NewYahooFinanceAdapter(),
	}
	seen := make(map[string]bool)
	for _, a := range adapters {
		if a == nil {
			t.Fatalf("nil adapter in test set")
		}
		if a.Name() == "" {
			t.Errorf("adapter %T has empty Name()", a)
		}
		if seen[a.Name()] {
			t.Errorf("duplicate adapter name %q — registry would overwrite it", a.Name())
		}
		seen[a.Name()] = true
		// Exercise the remaining methods so an interface drift surfaces.
		_ = a.Type()
		for _, dt := range a.SupportedTypes() {
			_, _ = a.Schema(dt)
		}
		_ = a.RateLimit()
		// HealthCheck should return without panicking on a freshly
		// constructed adapter. A network call to a real upstream is
		// expected to fail; nil or error are both acceptable.
		_ = a.HealthCheck(context.Background())
	}
}

// L1: Deduplicate semantics.

// L1: Deduplicate semantics.
//
// Note: Deduplicate / DeduplicateWithCount are already covered by
// TestUnifiedDataPoint_Deduplicate in source_test.go. We keep a single
// focused test here that asserts the "first wins" policy used by the
// registry to keep higher-priority sources.

func TestDeduplicate_FirstWins(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	pts := []UnifiedDataPoint{
		{Symbol: "600519.SH", TradeTime: now, DataType: DataTypeOHLCDaily, Data: map[string]interface{}{"close": 100.0}, Source: "tushare"},
		{Symbol: "600519.SH", TradeTime: now, DataType: DataTypeOHLCDaily, Data: map[string]interface{}{"close": 100.0}, Source: "mootdx"},
	}
	got, n := DeduplicateWithCount(pts)
	if n != 1 {
		t.Errorf("skipped = %d, want 1", n)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Source != "tushare" {
		t.Errorf("got[0].Source = %q, want %q (first wins)", got[0].Source, "tushare")
	}
}

// L2: registry fallback semantics with mock adapters.
//
// The classic case: primary returns ErrUpstreamUnavailable, the next
// adapter in the chain returns data. The registry should pick up the
// fallback transparently and return its data.

func TestRegistry_FallbackOnUpstreamUnavailable(t *testing.T) {
	reg := NewRegistry()
	primary := newMockAdapter("primary", []string{DataTypeOHLCDaily})
	primary.setErr(fmt.Errorf("%w: primary 503", ErrUpstreamUnavailable))
	secondary := newMockAdapter("secondary", []string{DataTypeOHLCDaily})
	secondary.setResponse(&FetchResponse{
		Items: []DataItem{{Symbol: "600519.SH", Data: map[string]interface{}{"close": 100.0}}},
	})
	if err := reg.Register(primary); err != nil {
		t.Fatalf("register primary: %v", err)
	}
	if err := reg.Register(secondary); err != nil {
		t.Fatalf("register secondary: %v", err)
	}
	reg.SetChain(DataTypeOHLCDaily, []string{"primary", "secondary"})

	resp, err := reg.Fetch(context.Background(), FetchRequest{
		DataType:  DataTypeOHLCDaily,
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if resp.Source != "secondary" {
		t.Errorf("resp.Source = %q, want %q", resp.Source, "secondary")
	}
	if len(resp.Items) != 1 {
		t.Errorf("len(Items) = %d, want 1", len(resp.Items))
	}
	if primary.callCount() != 1 {
		t.Errorf("primary call count = %d, want 1", primary.callCount())
	}
	if secondary.callCount() != 1 {
		t.Errorf("secondary call count = %d, want 1", secondary.callCount())
	}
}

func TestRegistry_FallbackStopsOnNonRetryable(t *testing.T) {
	reg := NewRegistry()
	primary := newMockAdapter("primary", []string{DataTypeOHLCDaily})
	primary.setErr(errors.New("validation: bad payload"))
	secondary := newMockAdapter("secondary", []string{DataTypeOHLCDaily})
	secondary.setResponse(&FetchResponse{
		Items: []DataItem{{Symbol: "600519.SH"}},
	})
	if err := reg.Register(primary); err != nil {
		t.Fatalf("register primary: %v", err)
	}
	if err := reg.Register(secondary); err != nil {
		t.Fatalf("register secondary: %v", err)
	}
	reg.SetChain(DataTypeOHLCDaily, []string{"primary", "secondary"})

	_, err := reg.Fetch(context.Background(), FetchRequest{
		DataType:  DataTypeOHLCDaily,
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("Fetch: expected error from primary, got nil")
	}
	if secondary.callCount() != 0 {
		t.Errorf("secondary should not have been called for non-retryable error, got %d", secondary.callCount())
	}
}

func TestRegistry_DisabledAdapterSkipped(t *testing.T) {
	reg := NewRegistry()
	primary := newMockAdapter("primary", []string{DataTypeOHLCDaily})
	primary.SetEnabled(false)
	primary.setResponse(&FetchResponse{Items: []DataItem{{Symbol: "X"}}})
	secondary := newMockAdapter("secondary", []string{DataTypeOHLCDaily})
	secondary.setResponse(&FetchResponse{Items: []DataItem{{Symbol: "Y"}}})
	_ = reg.Register(primary)
	_ = reg.Register(secondary)
	reg.SetChain(DataTypeOHLCDaily, []string{"primary", "secondary"})

	resp, err := reg.Fetch(context.Background(), FetchRequest{
		DataType:  DataTypeOHLCDaily,
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if resp.Source != "secondary" {
		t.Errorf("resp.Source = %q, want %q (disabled primary should be skipped)", resp.Source, "secondary")
	}
	if primary.callCount() != 0 {
		t.Errorf("disabled primary should not be called, got %d", primary.callCount())
	}
}

func TestRegistry_EmptyChainReturnsErrUnsupported(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.Fetch(context.Background(), FetchRequest{
		DataType:  "unknown_type",
		StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("expected error for empty chain, got nil")
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

func TestRegistry_SetChainOverwrites(t *testing.T) {
	reg := NewRegistry()
	a := newMockAdapter("a", []string{DataTypeOHLCDaily})
	b := newMockAdapter("b", []string{DataTypeOHLCDaily})
	_ = reg.Register(a)
	_ = reg.Register(b)

	// SetChain replaces the auto-built chain entirely.
	reg.SetChain(DataTypeOHLCDaily, []string{"b"})
	chains := reg.ListChains()
	if got := chains[DataTypeOHLCDaily]; len(got) != 1 || got[0] != "b" {
		t.Errorf("chain = %v, want [b]", got)
	}
}
