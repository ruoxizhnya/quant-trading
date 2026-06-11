// Package strategy provides strategy management and plugin architecture.
package strategy

import (
	"context"
	"fmt"
	"plugin"
	"sync"

	"github.com/rs/zerolog"
)

// StrategyFactory is a function that creates a new Strategy instance.
// Used for hot-reloadable strategies.
type StrategyFactory func() Strategy

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

// Deprecated: domain.Strategy is removed in ODR-013 P1-25. The old registry
// is no longer used; retained as a thin wrapper only so the file compiles
// during the transition. It will be deleted in a follow-up commit.
//
// All callers should migrate to the canonical strategy.Strategy interface
// (which is what `Registry` below uses).
type oldRegistry struct {
	factories  map[string]StrategyFactory
	instances  map[string]Strategy
	mu         sync.RWMutex
	logger     zerolog.Logger
}

func newOldRegistry(logger zerolog.Logger) *oldRegistry {
	return &oldRegistry{
		factories: make(map[string]StrategyFactory),
		instances: make(map[string]Strategy),
		logger:    logger.With().Str("component", "strategy_registry_legacy").Logger(),
	}
}

// DefaultOldRegistry is the global registry for the deprecated API.
// It is intentionally uninitialised — old callers will get a clear error.
var DefaultOldRegistry *oldRegistry

// OldInit is a no-op kept only to satisfy the cmd/strategy main entry
// point during the migration window. The old strategy registration API
// (Register/GetStrategy) now returns errors instead of mutating state.
func OldInit(logger zerolog.Logger) {
	logger.Warn().Msg("strategy.OldInit is deprecated (ODR-013 P1-25) and is a no-op")
}

// Register is the deprecated old-API strategy registration. It always
// returns an error directing the caller to use the new Registry.
func Register(name string, factory StrategyFactory) error {
	return fmt.Errorf("strategy.Register(%q) is removed in ODR-013 P1-25; "+
		"use strategy.GlobalRegister with pkg/strategy.Strategy instead", name)
}

// GetStrategy is the deprecated lookup. It always returns an error.
func GetStrategy(name string) (Strategy, error) {
	return nil, fmt.Errorf("strategy.GetStrategy(%q) is removed in ODR-013 P1-25; "+
		"use strategy.GlobalGet with pkg/strategy.Strategy instead", name)
}

// ListStrategies is the deprecated list. It returns an empty slice.
func ListStrategies() []string {
	return nil
}

// ReloadStrategy is the deprecated reload. It always returns an error.
func ReloadStrategy(name string) error {
	return fmt.Errorf("strategy.ReloadStrategy(%q) is removed in ODR-013 P1-25; "+
		"use strategy.GlobalRegister with the new pkg/strategy.Strategy instead", name)
}

// ConfigureStrategy applies parameter overrides to a registered strategy.
// The strategy must implement the Configurable interface (Configure method).
func ConfigureStrategy(name string, params map[string]any) error {
	s, err := DefaultRegistry.Get(name)
	if err != nil {
		return fmt.Errorf("strategy not found: %s", name)
	}
	type configurable interface {
		Configure(params map[string]any) error
	}
	c, ok := s.(configurable)
	if !ok {
		return fmt.Errorf("strategy %s does not support runtime configuration", name)
	}
	if err := c.Configure(params); err != nil {
		return fmt.Errorf("failed to configure strategy %s: %w", name, err)
	}
	if defaultLogger.GetLevel() != zerolog.Disabled {
		defaultLogger.Info().Str("strategy", name).Interface("params", params).Msg("Strategy reconfigured")
	}
	return nil
}

// ReloadFromDB reloads strategy parameters from the database.
// It looks up the strategy in the DB, applies the saved params, and returns the strategy name.
func ReloadFromDB(ctx context.Context, db *StrategyDB, name string) error {
	params, err := db.GetStrategyParams(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to get params for %s: %w", name, err)
	}
	return ConfigureStrategy(name, params)
}

// ReloadAllFromDB reloads all registered strategies from the database.
func ReloadAllFromDB(ctx context.Context, db *StrategyDB) map[string]string {
	names := DefaultRegistry.List()
	results := make(map[string]string)
	for _, name := range names {
		if err := ReloadFromDB(ctx, db, name); err != nil {
			results[name] = "error: " + err.Error()
		} else {
			results[name] = "ok"
		}
	}
	return results
}

// ReloadAll is the deprecated bulk reload. It always returns an error.
func ReloadAll() error {
	return fmt.Errorf("strategy.ReloadAll is removed in ODR-013 P1-25; "+
		"use the new strategy.Registry directly", )
}

// ReloadAllStrategies is an alias for ReloadAll (for backward compatibility).
func ReloadAllStrategies() error {
	return ReloadAll()
}

// GetStrategyInfo is the deprecated info lookup. It always returns an error.
func GetStrategyInfo(name string) (*StrategyInfo, error) {
	return nil, fmt.Errorf("strategy.GetStrategyInfo(%q) is removed in ODR-013 P1-25; "+
		"use the new strategy.Registry.ListWithInfo() instead", name)
}

func init() {
	// Auto-register is called at startup
}
