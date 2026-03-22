// Package strategy provides strategy management and implementation.
package strategy

import (
	"fmt"
	"sync"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// StrategyFactory is a function that creates a new Strategy instance.
type StrategyFactory func() domain.Strategy

// Registry manages strategy registration and retrieval with hot-swap support.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]StrategyFactory
	instances map[string]domain.Strategy
	logger    zerolog.Logger
}

// NewRegistry creates a new strategy registry.
func NewRegistry(logger zerolog.Logger) *Registry {
	return &Registry{
		factories: make(map[string]StrategyFactory),
		instances: make(map[string]domain.Strategy),
		logger:    logger.With().Str("component", "strategy_registry").Logger(),
	}
}

// Register registers a strategy factory with the given name.
// If a strategy with this name already exists, it will be replaced (hot-swap).
func (r *Registry) Register(name string, factory StrategyFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if replacing an existing strategy
	if _, exists := r.factories[name]; exists {
		r.logger.Info().Str("strategy", name).Msg("hot-swapping strategy")
	} else {
		r.logger.Info().Str("strategy", name).Msg("registering new strategy")
	}

	r.factories[name] = factory

	// Create a fresh instance
	r.instances[name] = factory()
}

// GetStrategy retrieves a strategy by name.
// Returns an error if the strategy is not found.
func (r *Registry) GetStrategy(name string) (domain.Strategy, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	instance, exists := r.instances[name]
	if !exists {
		return nil, fmt.Errorf("strategy not found: %s", name)
	}

	return instance, nil
}

// ListStrategies returns a list of all registered strategy names.
func (r *Registry) ListStrategies() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}

	return names
}

// ReloadStrategy reloads a specific strategy by recreating its instance.
// This enables hot-swap of individual strategies without restarting the service.
func (r *Registry) ReloadStrategy(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	factory, exists := r.factories[name]
	if !exists {
		return fmt.Errorf("strategy not registered: %s", name)
	}

	r.logger.Info().Str("strategy", name).Msg("reloading strategy")

	// Cleanup old instance if exists
	if instance, exists := r.instances[name]; exists {
		instance.Cleanup()
	}

	// Create new instance
	r.instances[name] = factory()

	return nil
}

// ReloadAll reloads all registered strategies.
func (r *Registry) ReloadAll() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.logger.Info().Msg("reloading all strategies")

	for name, factory := range r.factories {
		// Cleanup old instance if exists
		if instance, exists := r.instances[name]; exists {
			instance.Cleanup()
		}

		// Create new instance
		r.instances[name] = factory()
	}

	return nil
}

// ConfigureStrategy configures a specific strategy with the given config.
func (r *Registry) ConfigureStrategy(name string, config map[string]any) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	instance, exists := r.instances[name]
	if !exists {
		return fmt.Errorf("strategy not found: %s", name)
	}

	if err := instance.Configure(config); err != nil {
		return fmt.Errorf("failed to configure strategy %s: %w", name, err)
	}

	return nil
}

// GetStrategyInfo returns basic information about a strategy.
func (r *Registry) GetStrategyInfo(name string) (*StrategyInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	instance, exists := r.instances[name]
	if !exists {
		return nil, fmt.Errorf("strategy not found: %s", name)
	}

	return &StrategyInfo{
		Name:        instance.Name(),
		Description: instance.Description(),
	}, nil
}

// StrategyInfo contains basic information about a strategy.
type StrategyInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// DefaultRegistry is the global strategy registry instance.
var DefaultRegistry *Registry

// Init initializes the default registry.
func Init(logger zerolog.Logger) {
	DefaultRegistry = NewRegistry(logger)
}

// Register registers a strategy with the default registry.
func Register(name string, factory StrategyFactory) {
	if DefaultRegistry == nil {
		panic("registry not initialized, call Init first")
	}
	DefaultRegistry.Register(name, factory)
}

// GetStrategy retrieves a strategy from the default registry.
func GetStrategy(name string) (domain.Strategy, error) {
	if DefaultRegistry == nil {
		return nil, fmt.Errorf("registry not initialized")
	}
	return DefaultRegistry.GetStrategy(name)
}

// ListStrategies returns all registered strategies from the default registry.
func ListStrategies() []string {
	if DefaultRegistry == nil {
		return nil
	}
	return DefaultRegistry.ListStrategies()
}

// ReloadAllStrategies reloads all strategies in the default registry.
func ReloadAllStrategies() error {
	if DefaultRegistry == nil {
		return fmt.Errorf("registry not initialized")
	}
	return DefaultRegistry.ReloadAll()
}

// GetStrategyInfo returns basic information about a strategy from the default registry.
func GetStrategyInfo(name string) (*StrategyInfo, error) {
	if DefaultRegistry == nil {
		return nil, fmt.Errorf("registry not initialized")
	}
	return DefaultRegistry.GetStrategyInfo(name)
}
