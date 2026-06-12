// Package strategy provides strategy management and plugin architecture.
package strategy

import (
	"sync"
)

// BaseStrategy provides common functionality for all strategies.
// It includes thread-safe parameter storage and common utility methods.
// Strategies can embed this struct to inherit base functionality.
type BaseStrategy struct {
	mu     sync.RWMutex
	name   string
	desc   string
	params map[string]any
}

// NewBaseStrategy creates a new BaseStrategy with the given name and description.
func NewBaseStrategy(name, description string) *BaseStrategy {
	return &BaseStrategy{
		name:   name,
		desc:   description,
		params: make(map[string]any),
	}
}

// Name returns the strategy name.
func (b *BaseStrategy) Name() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.name
}

// Description returns the strategy description.
func (b *BaseStrategy) Description() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.desc
}

// SetName sets the strategy name (thread-safe).
func (b *BaseStrategy) SetName(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.name = name
}

// SetDescription sets the strategy description (thread-safe).
func (b *BaseStrategy) SetDescription(desc string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.desc = desc
}

// GetParam retrieves a parameter value by name (thread-safe).
func (b *BaseStrategy) GetParam(key string) (any, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	val, ok := b.params[key]
	return val, ok
}

// SetParam sets a parameter value by name (thread-safe).
func (b *BaseStrategy) SetParam(key string, value any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.params[key] = value
}

// GetParamInt retrieves an int parameter with fallback to default.
func (b *BaseStrategy) GetParamInt(key string, defaultVal int) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if val, ok := b.params[key]; ok {
		switch v := val.(type) {
		case float64:
			return int(v)
		case int:
			return v
		}
	}
	return defaultVal
}

// GetParamFloat retrieves a float64 parameter with fallback to default.
func (b *BaseStrategy) GetParamFloat(key string, defaultVal float64) float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if val, ok := b.params[key]; ok {
		switch v := val.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		}
	}
	return defaultVal
}

// GetParamString retrieves a string parameter with fallback to default.
func (b *BaseStrategy) GetParamString(key string, defaultVal string) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if val, ok := b.params[key]; ok {
		if v, ok := val.(string); ok {
			return v
		}
	}
	return defaultVal
}

// GetParamBool retrieves a bool parameter with fallback to default.
func (b *BaseStrategy) GetParamBool(key string, defaultVal bool) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if val, ok := b.params[key]; ok {
		if v, ok := val.(bool); ok {
			return v
		}
	}
	return defaultVal
}

// Lock acquires the write lock for thread-safe parameter updates.
func (b *BaseStrategy) Lock() {
	b.mu.Lock()
}

// Unlock releases the write lock.
func (b *BaseStrategy) Unlock() {
	b.mu.Unlock()
}

// RLock acquires the read lock for thread-safe parameter access.
func (b *BaseStrategy) RLock() {
	b.mu.RLock()
}

// RUnlock releases the read lock.
func (b *BaseStrategy) RUnlock() {
	b.mu.RUnlock()
}

// ClearParams removes all stored parameters.
func (b *BaseStrategy) ClearParams() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.params = make(map[string]any)
}

// CloneParams returns a copy of all parameters.
func (b *BaseStrategy) CloneParams() map[string]any {
	b.mu.RLock()
	defer b.mu.RUnlock()
	clone := make(map[string]any, len(b.params))
	for k, v := range b.params {
		clone[k] = v
	}
	return clone
}

// ─── P1-24 (ADR-020 §4) Sub-interface defaults ────────────────────────
//
// The Strategy interface was decomposed into 4 sub-interfaces
// (StrategyCore / Configurable / SignalGenerator / ResourceManaged).
// BaseStrategy provides default implementations of Configure() and
// Cleanup() so that any struct embedding *BaseStrategy satisfies the
// composite `Strategy` interface without writing boilerplate:
//
//   - Configure(): stores the params in the b.params map; subclasses
//     that need typed/semantic validation (range checks, enum choices)
//     override this. The default is "accept anything, store as-is".
//   - Cleanup(): no-op (BaseStrategy holds no resources). Stateful
//     strategies (cached features, open connections) override.
//
// Name() and Description() are already implemented above; they
// satisfy the StrategyCore sub-interface.

// Configure stores the parameter map verbatim. Subclasses typically
// override this to add validation and copy values into a typed config
// struct. The default behavior is "be permissive" — this matches
// pre-P1-24 behavior where some strategies had no validation at all.
func (b *BaseStrategy) Configure(params map[string]interface{}) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.params == nil {
		b.params = make(map[string]any, len(params))
	}
	for k, v := range params {
		b.params[k] = v
	}
	return nil
}

// Cleanup is a no-op for BaseStrategy (it holds no resources).
// Stateful strategies should override to release caches, file handles,
// open connections, etc. Safe to call multiple times.
func (b *BaseStrategy) Cleanup() {
	// Default: no resources to release.
	// Subclasses with state (caches, connections) override.
}

// Parameters returns an empty schema by default. Subclasses that have
// tunable parameters override this to expose their schema. Returning
// an empty slice (not nil) is the contract — the registry uses it to
// decide "this strategy has no parameters to configure".
func (b *BaseStrategy) Parameters() []Parameter {
	return []Parameter{}
}
