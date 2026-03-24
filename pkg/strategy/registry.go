// Package strategy provides strategy management and plugin architecture.
package strategy

import (
	"fmt"
	"plugin"
	"sync"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// StrategyFactory is a function that creates a new Strategy instance.
// Used for hot-reloadable strategies.
type StrategyFactory func() domain.Strategy

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

// Logger for the default registry
var defaultLogger zerolog.Logger

// Init initializes the default registry with a logger.
func Init(logger zerolog.Logger) {
	defaultLogger = logger.With().Str("component", "strategy_registry").Logger()
	DefaultRegistry = NewRegistry()
}

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
	// Strategies register themselves via init() in their respective files
	return nil
}

// RegisterByReflection registers all Strategy implementations found in the given slice.
func RegisterByReflection(strategies []Strategy) error {
	for _, s := range strategies {
		if err := GlobalRegister(s); err != nil {
			return err
		}
	}
	return nil
}

// NewPlugin creates a plugin symbol for dynamic loading.
func NewPlugin(s Strategy) Strategy {
	return s
}

// ─── Backward Compatibility with domain.Strategy ──────────────────────────────────

// oldRegistry is a separate registry for domain.Strategy implementations (backward compat).
type oldRegistry struct {
	factories  map[string]StrategyFactory
	instances  map[string]domain.Strategy
	mu         sync.RWMutex
	logger     zerolog.Logger
}

func newOldRegistry(logger zerolog.Logger) *oldRegistry {
	return &oldRegistry{
		factories: make(map[string]StrategyFactory),
		instances: make(map[string]domain.Strategy),
		logger:    logger.With().Str("component", "strategy_registry").Logger(),
	}
}

// DefaultOldRegistry is the global registry for domain.Strategy (backward compat).
var DefaultOldRegistry *oldRegistry

// OldInit initializes the old registry for backward compatibility.
func OldInit(logger zerolog.Logger) {
	DefaultOldRegistry = newOldRegistry(logger)
}

// Register registers a strategy factory with the given name.
// This is the old API for backward compatibility with cmd/strategy service.
func Register(name string, factory StrategyFactory) {
	if DefaultOldRegistry == nil {
		panic("old registry not initialized, call OldInit first")
	}
	DefaultOldRegistry.mu.Lock()
	defer DefaultOldRegistry.mu.Unlock()

	if _, exists := DefaultOldRegistry.factories[name]; exists {
		DefaultOldRegistry.logger.Info().Str("strategy", name).Msg("hot-swapping strategy")
	} else {
		DefaultOldRegistry.logger.Info().Str("strategy", name).Msg("registering new strategy")
	}

	DefaultOldRegistry.factories[name] = factory
	DefaultOldRegistry.instances[name] = factory()
}

// GetStrategy retrieves a strategy by name.
// Returns an error if the strategy is not found.
func GetStrategy(name string) (domain.Strategy, error) {
	if DefaultOldRegistry == nil {
		return nil, fmt.Errorf("registry not initialized")
	}
	DefaultOldRegistry.mu.RLock()
	defer DefaultOldRegistry.mu.RUnlock()

	instance, exists := DefaultOldRegistry.instances[name]
	if !exists {
		return nil, fmt.Errorf("strategy not found: %s", name)
	}

	return instance, nil
}

// ListStrategies returns a list of all registered strategy names.
func ListStrategies() []string {
	if DefaultOldRegistry == nil {
		return nil
	}
	DefaultOldRegistry.mu.RLock()
	defer DefaultOldRegistry.mu.RUnlock()

	names := make([]string, 0, len(DefaultOldRegistry.factories))
	for name := range DefaultOldRegistry.factories {
		names = append(names, name)
	}

	return names
}

// ReloadStrategy reloads a specific strategy by recreating its instance.
func ReloadStrategy(name string) error {
	if DefaultOldRegistry == nil {
		return fmt.Errorf("registry not initialized")
	}
	DefaultOldRegistry.mu.Lock()
	defer DefaultOldRegistry.mu.Unlock()

	factory, exists := DefaultOldRegistry.factories[name]
	if !exists {
		return fmt.Errorf("strategy not registered: %s", name)
	}

	DefaultOldRegistry.logger.Info().Str("strategy", name).Msg("reloading strategy")

	if instance, exists := DefaultOldRegistry.instances[name]; exists {
		instance.Cleanup()
	}

	DefaultOldRegistry.instances[name] = factory()

	return nil
}

// ReloadAll reloads all registered strategies.
func ReloadAll() error {
	if DefaultOldRegistry == nil {
		return fmt.Errorf("registry not initialized")
	}
	DefaultOldRegistry.mu.Lock()
	defer DefaultOldRegistry.mu.Unlock()

	DefaultOldRegistry.logger.Info().Msg("reloading all strategies")

	for name, factory := range DefaultOldRegistry.factories {
		if instance, exists := DefaultOldRegistry.instances[name]; exists {
			instance.Cleanup()
		}
		DefaultOldRegistry.instances[name] = factory()
	}

	return nil
}

// ReloadAllStrategies is an alias for ReloadAll (for backward compatibility).
func ReloadAllStrategies() error {
	return ReloadAll()
}

// GetStrategyInfo returns basic information about a strategy.
func GetStrategyInfo(name string) (*StrategyInfo, error) {
	if DefaultOldRegistry == nil {
		return nil, fmt.Errorf("registry not initialized")
	}
	DefaultOldRegistry.mu.RLock()
	defer DefaultOldRegistry.mu.RUnlock()

	instance, exists := DefaultOldRegistry.instances[name]
	if !exists {
		return nil, fmt.Errorf("strategy not found: %s", name)
	}

	return &StrategyInfo{
		Name:        instance.Name(),
		Description: instance.Description(),
	}, nil
}

func init() {
	// Auto-register is called at startup
}
