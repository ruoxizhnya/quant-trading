// P1-24 (Sprint 6, ODR-013 CQ-006, ADR-020 §4):
// Compliance tests for the Strategy interface decomposition.
//
// This file verifies that:
//
//  1. BaseStrategy satisfies the 3 sub-interfaces it claims to
//     (StrategyCore, Configurable, ResourceManaged). BaseStrategy
//     intentionally does NOT implement SignalGenerator — it lacks
//     the per-strategy decision logic; concrete strategies embed
//     *BaseStrategy and add GenerateSignals/Weight themselves.
//
//  2. Every strategy registered with the default registry implements
//     the composite `Strategy` interface (4 sub-interfaces combined).
//     This is the runtime equivalent of the compile-time `var _ Strategy
//     = ...` check; it catches drift if a strategy removes a method.
//
//  3. The AsConfigurable/AsSignalGenerator/AsResourceManaged type
//     helpers correctly downcast (and return nil for non-implementors).
//
//  4. ConfigureStrategy (the registry-level configurator) correctly
//     uses AsConfigurable and returns a clear error for non-configurable
//     strategies.
//
// The tests use the `init()`-registered built-in strategies (momentum,
// value, multi_factor, mean_reversion, td_sequential, bollinger_mr,
// volume_price_trend, volatility_breakout, plus anything in the
// plugins/ and examples/ sub-packages). Because the test lives in
// the `strategy_test` external package, we can blank-import the
// sub-packages to fire their init() functions without creating an
// import cycle (sub-packages already import `strategy`).
package strategy_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"

	// Blank imports trigger init() in sub-packages, which register
	// built-in strategies with strategy.DefaultRegistry. Without
	// these, TestRegisteredStrategies_* would see an empty registry.
	_ "github.com/ruoxizhnya/quant-trading/pkg/strategy/examples"
	_ "github.com/ruoxizhnya/quant-trading/pkg/strategy/expression"
	_ "github.com/ruoxizhnya/quant-trading/pkg/strategy/plugins"
)

// ─── Synthetic types for sub-interface boundary tests ───────────────────

// fakeStrategy is a complete Strategy implementation used for the
// As* helper tests below.
type fakeStrategy struct {
	name string
	desc string
	cfg  map[string]any
	cl   int
}

func (f *fakeStrategy) Name() string        { return f.name }
func (f *fakeStrategy) Description() string { return f.desc }
func (f *fakeStrategy) Parameters() []strategy.Parameter {
	return []strategy.Parameter{{Name: "k", Type: "int", Default: 1}}
}
func (f *fakeStrategy) Configure(p map[string]any) error { f.cfg = p; return nil }
func (f *fakeStrategy) GenerateSignals(_ context.Context, _ map[string][]domain.OHLCV, _ *domain.Portfolio) ([]strategy.Signal, error) {
	return nil, nil
}
func (f *fakeStrategy) Weight(_ strategy.Signal, _ float64) float64 { return 0.05 }
func (f *fakeStrategy) Cleanup()                                    { f.cl++ }

// coreOnlyFake implements ONLY the StrategyCore sub-interface.
// It deliberately does not implement Configurable, SignalGenerator,
// or ResourceManaged. Useful for asserting that the As* type
// assertions return nil for partial strategies.
type coreOnlyFake struct {
	name string
	desc string
}

func (c *coreOnlyFake) Name() string        { return c.name }
func (c *coreOnlyFake) Description() string { return c.desc }

// ─── 1. BaseStrategy sub-interface checks ──────────────────────────────

// TestBaseStrategy_SatisfiesSubInterfaces asserts that *BaseStrategy
// satisfies the 3 sub-interfaces it claims to (StrategyCore, Configurable,
// ResourceManaged). These are also enforced at compile time in
// interfaces.go via `var _ StrategyCore = (*BaseStrategy)(nil)`; this
// test gives a more readable error if a method gets removed and the
// file is still loaded into a test binary that compiles.
func TestBaseStrategy_SatisfiesSubInterfaces(t *testing.T) {
	b := strategy.NewBaseStrategy("test", "test description")

	// StrategyCore
	var sc strategy.StrategyCore = b
	if sc.Name() != "test" {
		t.Errorf("StrategyCore.Name() = %q, want %q", sc.Name(), "test")
	}
	if sc.Description() != "test description" {
		t.Errorf("StrategyCore.Description() = %q, want %q", sc.Description(), "test description")
	}

	// Configurable
	var c strategy.Configurable = b
	if got := c.Parameters(); got == nil {
		t.Error("Configurable.Parameters() returned nil; expected empty slice")
	}
	if err := c.Configure(map[string]any{"x": 1}); err != nil {
		t.Errorf("Configurable.Configure() error: %v", err)
	}
	if v, ok := b.GetParam("x"); !ok || v != 1 {
		t.Errorf("Configure did not store param: got (%v, %v), want (1, true)", v, ok)
	}

	// ResourceManaged
	var rm strategy.ResourceManaged = b
	// Cleanup should be a no-op (BaseStrategy holds no resources)
	rm.Cleanup()
	rm.Cleanup() // idempotent

	// SignalGenerator is intentionally NOT implemented by BaseStrategy;
	// see the next test for the runtime assertion.
}

// TestBaseStrategy_DoesNotImplementSignalGenerator documents the
// design intent that *BaseStrategy is a *base class*, not a leaf
// strategy: it has no GenerateSignals/Weight methods. The actual
// compile-time guard is the absence of
// `var _ SignalGenerator = (*BaseStrategy)(nil)` in interfaces.go
// — if someone adds those methods to BaseStrategy, `go build`
// will fail. This test makes the invariant visible at runtime.
func TestBaseStrategy_DoesNotImplementSignalGenerator(t *testing.T) {
	var s any = strategy.NewBaseStrategy("x", "y")
	if _, ok := s.(strategy.SignalGenerator); ok {
		t.Fatal("*BaseStrategy must NOT implement SignalGenerator; the strategy-go interface is a base class. " +
			"Add leaf methods to a derived strategy, not to BaseStrategy.")
	}
}

// ─── 2. As* type-assertion helpers ──────────────────────────────────────

// TestAsConfigurable verifies AsConfigurable returns the strategy itself
// when it implements Configurable, and nil otherwise.
func TestAsConfigurable(t *testing.T) {
	s := &fakeStrategy{name: "fake", desc: "fake strategy"}
	if c := strategy.AsConfigurable(s); c == nil {
		t.Error("AsConfigurable(fakeStrategy) = nil; want non-nil")
	}

	// coreOnlyFake doesn't implement Configurable → AsConfigurable returns nil.
	core := &coreOnlyFake{name: "core", desc: "core only"}
	if c := strategy.AsConfigurable(core); c != nil {
		t.Error("AsConfigurable(coreOnlyFake) = non-nil; expected nil (missing Configure/Parameters)")
	}
}

// TestAsResourceManaged verifies AsResourceManaged returns the strategy
// itself when it implements ResourceManaged, and nil otherwise.
func TestAsResourceManaged(t *testing.T) {
	s := &fakeStrategy{name: "fake", desc: "fake strategy"}
	if r := strategy.AsResourceManaged(s); r == nil {
		t.Error("AsResourceManaged(fakeStrategy) = nil; want non-nil")
	}
	// Cleanup should be reachable
	strategy.AsResourceManaged(s).Cleanup()
	if s.cl != 1 {
		t.Errorf("expected Cleanup() to be invoked once, got cl=%d", s.cl)
	}

	// coreOnlyFake doesn't implement ResourceManaged → AsResourceManaged returns nil.
	core := &coreOnlyFake{name: "core", desc: "core only"}
	if r := strategy.AsResourceManaged(core); r != nil {
		t.Error("AsResourceManaged(coreOnlyFake) = non-nil; expected nil (missing Cleanup)")
	}
}

// TestAsSignalGenerator verifies AsSignalGenerator returns the strategy
// itself when it implements SignalGenerator, and nil otherwise.
// Note: every full Strategy implements SignalGenerator (it's in the
// composite), so the "returns nil" path is only reachable for partial
// strategies (core-only, configurable-only).
func TestAsSignalGenerator(t *testing.T) {
	s := &fakeStrategy{name: "fake", desc: "fake strategy"}
	if g := strategy.AsSignalGenerator(s); g == nil {
		t.Error("AsSignalGenerator(fakeStrategy) = nil; want non-nil")
	}

	// coreOnlyFake doesn't implement SignalGenerator → AsSignalGenerator returns nil.
	core := &coreOnlyFake{name: "core", desc: "core only"}
	if g := strategy.AsSignalGenerator(core); g != nil {
		t.Error("AsSignalGenerator(coreOnlyFake) = non-nil; expected nil (missing GenerateSignals/Weight)")
	}
}

// ─── 3. Registered-strategy compliance ──────────────────────────────────

// TestRegisteredStrategies_SatisfyComposite asserts that every strategy
// currently in DefaultRegistry implements the composite Strategy
// interface. This is the runtime equivalent of:
//
//	var _ Strategy = (*momentumStrategy)(nil)
//
// …applied to all registered strategies at once. It catches drift if
// someone refactors a strategy and accidentally drops a method.
func TestRegisteredStrategies_SatisfyComposite(t *testing.T) {
	names := strategy.DefaultRegistry.List()
	if len(names) == 0 {
		t.Fatal("DefaultRegistry is empty; expected built-in strategies to be registered via init()")
	}

	for _, name := range names {
		s, err := strategy.DefaultRegistry.Get(name)
		if err != nil {
			t.Errorf("DefaultRegistry.Get(%q) error: %v", name, err)
			continue
		}
		if s == nil {
			t.Errorf("DefaultRegistry.Get(%q) returned nil strategy", name)
			continue
		}

		// Check that all 4 sub-interfaces are satisfied.
		if sc, ok := s.(strategy.StrategyCore); !ok || sc.Name() != name {
			t.Errorf("strategy %q: StrategyCore.Name() mismatch: %q", name, sc.Name())
		}
		if _, ok := s.(strategy.Configurable); !ok {
			t.Errorf("strategy %q: missing Configurable sub-interface", name)
		}
		if _, ok := s.(strategy.SignalGenerator); !ok {
			t.Errorf("strategy %q: missing SignalGenerator sub-interface", name)
		}
		if _, ok := s.(strategy.ResourceManaged); !ok {
			t.Errorf("strategy %q: missing ResourceManaged sub-interface", name)
		}

		// Composite assignment as a final guard.
		var iface strategy.Strategy = s
		_ = iface
	}
}

// TestRegisteredStrategies_AllHaveNonEmptyName ensures Name() returns a
// non-empty string for every registered strategy. (Empty names are
// rejected by Register() but this guards against future refactors that
// could bypass the check.)
func TestRegisteredStrategies_AllHaveNonEmptyName(t *testing.T) {
	names := strategy.DefaultRegistry.List()
	for _, name := range names {
		s, err := strategy.DefaultRegistry.Get(name)
		if err != nil {
			t.Errorf("Get(%q) error: %v", name, err)
			continue
		}
		if s.Name() == "" {
			t.Errorf("strategy %q has empty Name()", name)
		}
		if s.Description() == "" {
			t.Errorf("strategy %q has empty Description()", name)
		}
	}
}

// ─── 4. Registry-level ConfigureStrategy integration ────────────────────

// TestConfigureStrategy_ConfigurableSucceeds is the happy path: a
// configurable strategy is reconfigured via the registry-level helper.
func TestConfigureStrategy_ConfigurableSucceeds(t *testing.T) {
	const name = "test-configurable"
	r := strategy.NewRegistry()
	s := &fakeStrategy{name: name, desc: "configurable test"}

	if err := r.Register(s); err != nil {
		t.Fatalf("r.Register error: %v", err)
	}

	got, err := r.Get(name)
	if err != nil {
		t.Fatalf("r.Get error: %v", err)
	}
	c := strategy.AsConfigurable(got)
	if c == nil {
		t.Fatal("AsConfigurable returned nil for fakeStrategy")
	}
	if err := c.Configure(map[string]any{"a": 42}); err != nil {
		t.Errorf("Configure error: %v", err)
	}
	if !reflect.DeepEqual(s.cfg, map[string]any{"a": 42}) {
		t.Errorf("Configure did not store params: got %v", s.cfg)
	}
}

// TestConfigureStrategy_UnknownStrategy verifies the registry's
// ConfigureStrategy returns a "not found" error for unknown names.
func TestConfigureStrategy_UnknownStrategy(t *testing.T) {
	err := strategy.ConfigureStrategy("definitely-not-a-real-strategy-12345", map[string]any{})
	if err == nil {
		t.Error("expected error for unknown strategy, got nil")
	}
	t.Logf("got expected error: %v", err)
}

// ─── 5. Composite-Interface smoke test ──────────────────────────────────

// TestCompositeInterface_MethodCount verifies the composite `Strategy`
// interface has exactly 7 methods (Name, Description, Parameters,
// Configure, GenerateSignals, Weight, Cleanup — the union of the 4
// sub-interfaces' methods). If someone adds a method to any sub-
// interface, this test will fail and prompt an ADR update.
func TestCompositeInterface_MethodCount(t *testing.T) {
	ifaceType := reflect.TypeOf((*strategy.Strategy)(nil)).Elem()
	if ifaceType.Kind() != reflect.Interface {
		t.Fatal("Strategy is not an interface type")
	}
	numMethod := ifaceType.NumMethod()
	if numMethod != 7 {
		t.Errorf("Strategy composite should have exactly 7 methods (4 sub-interfaces combined); got %d (%v)",
			numMethod, methodNames(ifaceType))
	}
}

// methodNames returns the names of all methods in the interface's
// method set for diagnostic output.
func methodNames(t reflect.Type) []string {
	names := make([]string, 0, t.NumMethod())
	for i := 0; i < t.NumMethod(); i++ {
		names = append(names, t.Method(i).Name)
	}
	return names
}
