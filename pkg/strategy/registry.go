// Package strategy provides strategy management and plugin architecture.
package strategy

import (
	"fmt"
	"plugin"
	"reflect"
	"sync"
)

// Registry manages strategy registration and retrieval.
type Registry struct {
	strategies map[string]Strategy
	mu         sync.RWMutex
}

// NewRegistry creates a new strategy registry.
func NewRegistry() *Registry {
	return &Registry{
		strategies: make(map[string]Strategy),
	}
}

// Register registers a strategy.
// Returns an error if a strategy with the same name is already registered.
func (r *Registry) Register(s Strategy) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := s.Name()
	if name == "" {
		return fmt.Errorf("strategy name cannot be empty")
	}

	if _, exists := r.strategies[name]; exists {
		return fmt.Errorf("strategy already registered: %s", name)
	}

	r.strategies[name] = s
	return nil
}

// Get retrieves a strategy by name.
// Returns nil if the strategy is not found.
func (r *Registry) Get(name string) (Strategy, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	s, exists := r.strategies[name]
	if !exists {
		return nil, fmt.Errorf("strategy not found: %s", name)
	}

	return s, nil
}

// List returns a list of all registered strategy names.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.strategies))
	for name := range r.strategies {
		names = append(names, name)
	}

	return names
}

// ListWithInfo returns strategy names with their descriptions and parameters.
func (r *Registry) ListWithInfo() []StrategyInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]StrategyInfo, 0, len(r.strategies))
	for _, s := range r.strategies {
		infos = append(infos, StrategyInfo{
			Name:        s.Name(),
			Description: s.Description(),
			Parameters:  s.Parameters(),
		})
	}

	return infos
}

// StrategyInfo contains information about a strategy.
type StrategyInfo struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  []Parameter `json:"parameters"`
}

// DefaultRegistry is the global strategy registry instance.
var DefaultRegistry = NewRegistry()

// GlobalRegister registers a strategy with the default registry.
func GlobalRegister(s Strategy) error {
	return DefaultRegistry.Register(s)
}

// GlobalGet retrieves a strategy from the default registry.
func GlobalGet(name string) (Strategy, error) {
	return DefaultRegistry.Get(name)
}

// GlobalList returns all registered strategy names from the default registry.
func GlobalList() []string {
	return DefaultRegistry.List()
}

// GlobalListWithInfo returns all registered strategies with info from the default registry.
func GlobalListWithInfo() []StrategyInfo {
	return DefaultRegistry.ListWithInfo()
}

// LoadPlugins loads strategies from dynamically linked plugin files.
// This enables adding new strategies without rebuilding the main binary.
func LoadPlugins(pluginPaths []string) error {
	for _, path := range pluginPaths {
		plug, err := plugin.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open plugin %s: %w", path, err)
		}

		symStrategy, err := plug.Lookup("Strategy")
		if err != nil {
			return fmt.Errorf("plugin %s does not export Strategy symbol: %w", path, err)
		}

		strategy, ok := symStrategy.(Strategy)
		if !ok {
			return fmt.Errorf("plugin %s Strategy symbol does not implement Strategy interface", path)
		}

		if err := GlobalRegister(strategy); err != nil {
			return fmt.Errorf("failed to register plugin strategy %s: %w", path, err)
		}
	}

	return nil
}

// AutoRegister registers all built-in strategies.
// This is called at startup to register hardcoded strategies.
func AutoRegister() error {
	// Register built-in strategies here
	// Import each plugin to trigger init() registration

	// For now, strategies register themselves via init() in their respective files
	// This function can be called to ensure all strategies are registered
	return nil
}

// RegisterByReflection registers all Strategy implementations found in the given slice.
// This is useful for registering strategies defined in the same binary.
func RegisterByReflection(strategies []Strategy) error {
	for _, s := range strategies {
		if err := GlobalRegister(s); err != nil {
			return err
		}
	}
	return nil
}

// strategyPlugin is a helper to make a strategy available for plugin loading.
type strategyPlugin struct {
	strategy Strategy
}

// NewPlugin creates a plugin symbol for dynamic loading.
// Usage in a plugin .go file:
//
//	var Strategy = strategy.NewPlugin(&MyStrategy{})
func NewPlugin(s Strategy) Strategy {
	return s
}

// Type checking at compile time
var _ Strategy = (*baseStrategy)(nil)

type baseStrategy struct {
	name        string
	description string
	parameters  []Parameter
}

func (b *baseStrategy) Name() string            { return b.name }
func (b *baseStrategy) Description() string     { return b.description }
func (b *baseStrategy) Parameters() []Parameter { return b.parameters }

// paramHelper returns reflect.Value for a parameter, handling type conversions.
func paramHelper(params map[string]any, name string, defaultVal any) any {
	if val, ok := params[name]; ok {
		return val
	}
	return defaultVal
}

// getParamInt safely extracts an int from params map.
func getParamInt(params map[string]any, name string, defaultVal int) int {
	if val, ok := params[name]; ok {
		switch v := val.(type) {
		case float64:
			return int(v)
		case int:
			return v
		case int64:
			return int(v)
		}
	}
	return defaultVal
}

// getParamFloat safely extracts a float64 from params map.
func getParamFloat(params map[string]any, name string, defaultVal float64) float64 {
	if val, ok := params[name]; ok {
		switch v := val.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		case int64:
			return float64(v)
		}
	}
	return defaultVal
}

// getParamString safely extracts a string from params map.
func getParamString(params map[string]any, name string, defaultVal string) string {
	if val, ok := params[name]; ok {
		if v, ok := val.(string); ok {
			return v
		}
	}
	return defaultVal
}

func init() {
	// Auto-register is called at startup
	// Individual strategy packages call GlobalRegister in their init()
}
