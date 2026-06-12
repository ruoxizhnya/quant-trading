// Package strategy provides strategy management and plugin architecture.
//
// P1-24 (Sprint 6, ODR-013 CQ-006, ADR-020): Strategy interface
// decomposition following the Interface Segregation Principle (ISP).
//
// The previous monolithic `Strategy` interface (7 methods) forced every
// implementer to provide Name/Description/Parameters/Configure/
// GenerateSignals/Weight/Cleanup even when not all were needed (e.g. a
// parameterless signal-only strategy has no meaningful Parameters/Configure).
// This file decomposes it into 4 single-responsibility sub-interfaces plus
// a backward-compatible composite.
//
// Design rationale (per ADR-020 §4):
//
//   - StrategyCore     — identity (Name, Description); every strategy has these
//   - Configurable     — runtime parameter schema + setter; optional
//   - SignalGenerator  — signal production + sizing; every trading strategy has these
//   - ResourceManaged  — teardown; optional (no-op default for stateless strategies)
//
// The composite `Strategy` interface embeds all 4, so existing code and
// implementations remain valid (no migration needed). New code can take
// dependencies on a narrower sub-interface, e.g. a parameter-setter CLI
// accepts `Configurable` rather than `Strategy`.
//
// Type-assertion helpers (`AsConfigurable`, `AsSignalGenerator`,
// `AsResourceManaged`) provide safe, named downcasting without
// scattering `interface{ ... }` literals.
package strategy

import (
	"context"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// StrategyCore captures a strategy's identity — its name and a
// human-readable description. Every strategy has these; they are
// required by the registry for listing / lookup.
//
// Implementations should treat Name as a stable, unique identifier
// (used as the registry key) and Description as free-form text.
type StrategyCore interface {
	// Name returns the unique strategy name (registry key).
	Name() string
	// Description returns a human-readable description of the
	// strategy's logic. Free-form, may be long.
	Description() string
}

// Configurable is implemented by strategies that expose a runtime
// parameter schema and accept dynamic reconfiguration. Parameterless
// strategies (e.g. a hard-coded crossover strategy) may omit this
// interface; the registry's `ConfigureStrategy` returns an error
// for non-configurable strategies.
//
// Parameters() describes the schema (name, type, default, min/max)
// for UI / YAML generation. Configure() applies a partial update;
// unknown keys are typically ignored (forward-compat).
type Configurable interface {
	// Parameters returns the parameter schema.
	Parameters() []Parameter
	// Configure applies a partial parameter update. Returns an
	// error for invalid values (out of range, wrong type).
	Configure(params map[string]interface{}) error
}

// SignalGenerator produces trading signals from a market snapshot.
// This is the "core" of a strategy — Name/Description are metadata,
// GenerateSignals/Weight are the actual decision logic.
//
// Weight is the position-sizing function; given a signal and the
// portfolio's current value, return a fraction in [0, 1] (long-only)
// or [-1, 1] (long/short). Implementations should clamp their output
// to a sane range.
type SignalGenerator interface {
	// GenerateSignals inspects the bars map (symbol → OHLCV history)
	// and the current portfolio, returning buy/sell signals.
	GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]Signal, error)
	// Weight returns the position weight (0..1 or -1..1) for a signal
	// given the current portfolio value.
	Weight(signal Signal, portfolioValue float64) float64
}

// ResourceManaged is the optional cleanup hook. Stateless strategies
// (pure functions over the bars input) can omit it; stateful strategies
// (cached features, open connections, file handles) should release them
// in Cleanup().
//
// The engine calls Cleanup() once before discarding a strategy
// (e.g. on registry Reset or process shutdown).
type ResourceManaged interface {
	// Cleanup releases any resources held by the strategy.
	// Idempotent — calling twice is a no-op.
	Cleanup()
}

// Strategy is the composite interface embedding all 4 sub-interfaces.
// This is the canonical type for "a fully-featured trading strategy";
// it preserves the original 7-method surface so existing implementations
// (and the registry, plugin loader, and 30+ test files) work without
// any changes.
//
// New code is encouraged to depend on the narrowest applicable
// sub-interface instead (e.g. a CLI's parameter editor takes
// `Configurable`, not `Strategy`).
type Strategy interface {
	StrategyCore
	Configurable
	SignalGenerator
	ResourceManaged
}

// AsConfigurable returns the strategy as a Configurable, or nil if
// the strategy does not support runtime configuration.
//
// Example:
//
//	if c := strategy.AsConfigurable(s); c != nil {
//	    if err := c.Configure(params); err != nil { ... }
//	}
//
// The argument is typed as `any` (not `Strategy`) so the helper
// can be used on partial strategies (e.g. a "core-only" strategy
// that only satisfies StrategyCore) — type assertions on `any`
// succeed or fail at runtime, which is the whole point of the
// As* family. Callers that have a concrete `Strategy` value
// can pass it directly.
func AsConfigurable(s any) Configurable {
	if c, ok := s.(Configurable); ok {
		return c
	}
	return nil
}

// AsSignalGenerator returns the strategy as a SignalGenerator, or nil.
// Provided for symmetry with AsConfigurable; in practice, all
// strategies implement SignalGenerator (it's part of the composite).
func AsSignalGenerator(s any) SignalGenerator {
	if g, ok := s.(SignalGenerator); ok {
		return g
	}
	return nil
}

// AsResourceManaged returns the strategy as a ResourceManaged, or nil.
// Strategies that hold no resources can omit this; the caller should
// nil-check before invoking Cleanup.
func AsResourceManaged(s any) ResourceManaged {
	if r, ok := s.(ResourceManaged); ok {
		return r
	}
	return nil
}

// ─── Compile-time interface satisfaction checks ────────────────────
//
// These vars fail at build time if BaseStrategy drifts out of
// compliance with the sub-interfaces it does satisfy. Note that
// BaseStrategy does NOT implement SignalGenerator or the composite
// Strategy — those require GenerateSignals/Weight which are provided
// by the concrete strategy (e.g. momentumStrategy) that embeds
// *BaseStrategy. To re-verify after editing BaseStrategy, run
// `go build ./pkg/strategy/...`.
//
// To verify a concrete strategy implements the full composite
// interface, see `pkg/strategy/interfaces_compliance_test.go` (P1-24)
// which asserts every registered strategy satisfies Strategy.

var (
	_ StrategyCore    = (*BaseStrategy)(nil)
	_ Configurable    = (*BaseStrategy)(nil)
	_ ResourceManaged = (*BaseStrategy)(nil) // satisfied by (*BaseStrategy).Cleanup
)
