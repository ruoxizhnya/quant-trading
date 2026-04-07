package strategy

import (
	"context"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// mockStrategy implements Strategy for testing.
type mockStrategy struct {
	name        string
	description string
	params      []Parameter
	signals     []Signal
	signalsErr  error
}

func (m *mockStrategy) Name() string            { return m.name }
func (m *mockStrategy) Description() string     { return m.description }
func (m *mockStrategy) Parameters() []Parameter { return m.params }
func (m *mockStrategy) Configure(params map[string]interface{}) error {
	return nil
}
func (m *mockStrategy) GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]Signal, error) {
	return m.signals, m.signalsErr
}
func (m *mockStrategy) Weight(signal Signal, portfolioValue float64) float64 {
	return 0.1
}
func (m *mockStrategy) Cleanup() {}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()

	// Test registering a strategy
	s := &mockStrategy{
		name:        "test_strategy",
		description: "A test strategy",
		params:      []Parameter{},
	}

	err := r.Register(s)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Test registering duplicate strategy
	err = r.Register(s)
	if err == nil {
		t.Fatal("expected error when registering duplicate strategy")
	}
}

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry()

	s := &mockStrategy{
		name:        "test_strategy",
		description: "A test strategy",
		params:      []Parameter{},
	}

	r.Register(s)

	// Test getting existing strategy
	got, err := r.Get("test_strategy")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Name() != "test_strategy" {
		t.Fatalf("expected test_strategy, got %s", got.Name())
	}

	// Test getting non-existent strategy
	_, err = r.Get("non_existent")
	if err == nil {
		t.Fatal("expected error when getting non-existent strategy")
	}
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry()

	// Empty registry
	names := r.List()
	if len(names) != 0 {
		t.Fatalf("expected empty list, got %v", names)
	}

	// Register strategies
	r.Register(&mockStrategy{name: "strategy1", description: "Strategy 1", params: []Parameter{}})
	r.Register(&mockStrategy{name: "strategy2", description: "Strategy 2", params: []Parameter{}})

	names = r.List()
	if len(names) != 2 {
		t.Fatalf("expected 2 strategies, got %d", len(names))
	}
}

func TestRegistry_ListWithInfo(t *testing.T) {
	r := NewRegistry()

	r.Register(&mockStrategy{
		name:        "momentum",
		description: "Momentum strategy",
		params: []Parameter{
			{Name: "lookback_days", Type: "int", Default: 20, Description: "Lookback period"},
		},
	})

	infos := r.ListWithInfo()
	if len(infos) != 1 {
		t.Fatalf("expected 1 strategy info, got %d", len(infos))
	}

	info := infos[0]
	if info.Name != "momentum" {
		t.Fatalf("expected momentum, got %s", info.Name)
	}
	if info.Description != "Momentum strategy" {
		t.Fatalf("expected 'Momentum strategy', got %s", info.Description)
	}
	if len(info.Parameters) != 1 {
		t.Fatalf("expected 1 parameter, got %d", len(info.Parameters))
	}
	if info.Parameters[0].Name != "lookback_days" {
		t.Fatalf("expected lookback_days, got %s", info.Parameters[0].Name)
	}
}

func TestGlobalRegistry(t *testing.T) {
	// Clear global registry
	DefaultRegistry = NewRegistry()

	s := &mockStrategy{
		name:        "global_test",
		description: "Global test strategy",
		params:      []Parameter{},
	}

	// Test global register
	err := GlobalRegister(s)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Test global get
	got, err := GlobalGet("global_test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Name() != "global_test" {
		t.Fatalf("expected global_test, got %s", got.Name())
	}

	// Test global list
	names := GlobalList()
	if len(names) != 1 {
		t.Fatalf("expected 1 strategy, got %d", len(names))
	}
}

func TestRegisterByReflection(t *testing.T) {
	// Save original global registry and create a fresh one
	orig := DefaultRegistry
	DefaultRegistry = NewRegistry()

	strategies := []Strategy{
		&mockStrategy{name: "strategy_a", description: "Strategy A", params: []Parameter{}},
		&mockStrategy{name: "strategy_b", description: "Strategy B", params: []Parameter{}},
	}

	err := RegisterByReflection(strategies)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	names := DefaultRegistry.List()
	if len(names) != 2 {
		t.Fatalf("expected 2 strategies, got %d", len(names))
	}

	// Restore original
	DefaultRegistry = orig
}
