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
