package strategy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// StrategyDB provides database-backed strategy configuration management.
type StrategyDB struct {
	store *storage.PostgresStore
}

// NewStrategyDB creates a new StrategyDB backed by the given store.
func NewStrategyDB(store *storage.PostgresStore) *StrategyDB {
	return &StrategyDB{store: store}
}

// Create creates or updates a strategy config.
func (db *StrategyDB) Create(ctx context.Context, cfg *domain.StrategyConfig) error {
	// Ensure params is valid JSON
	if cfg.Params == "" {
		cfg.Params = "{}"
	}
	var params map[string]any
	if err := json.Unmarshal([]byte(cfg.Params), &params); err != nil {
		return fmt.Errorf("invalid params JSON: %w", err)
	}
	cfg.IsActive = true
	return db.store.SaveStrategyConfig(ctx, cfg)
}

// Get retrieves a strategy config by strategy_id.
func (db *StrategyDB) Get(ctx context.Context, strategyID string) (*domain.StrategyConfig, error) {
	return db.store.GetStrategyConfig(ctx, strategyID)
}

// List returns all strategy configs, optionally filtered by type and active status.
func (db *StrategyDB) List(ctx context.Context, strategyType string, activeOnly bool) ([]*domain.StrategyConfig, error) {
	return db.store.ListStrategyConfigs(ctx, strategyType, activeOnly)
}

// Delete soft-deletes a strategy (sets is_active=false).
func (db *StrategyDB) Delete(ctx context.Context, strategyID string) error {
	return db.store.DeleteStrategyConfig(ctx, strategyID)
}

// ListWithDB combines registry strategies with DB strategies.
// DB entries override registry entries with the same name.
func (db *StrategyDB) ListWithDB(ctx context.Context) ([]StrategyInfo, error) {
	// Get all strategies from the registry
	registryInfos := DefaultRegistry.ListWithInfo()

	// Get all active DB strategies
	dbConfigs, err := db.store.ListStrategyConfigs(ctx, "", true)
	if err != nil {
		return nil, fmt.Errorf("failed to list DB strategies: %w", err)
	}

	// Build a map of DB strategy_id -> StrategyConfig
	dbMap := make(map[string]*domain.StrategyConfig)
	for _, cfg := range dbConfigs {
		dbMap[cfg.StrategyID] = cfg
	}

	// Merge: use DB config to override registry entries
	seen := make(map[string]bool)
	var result []StrategyInfo

	// First, add all DB entries (they take precedence)
	for _, cfg := range dbConfigs {
		var params []Parameter
		if cfg.Params != "" {
			_ = json.Unmarshal([]byte(cfg.Params), &params)
		}
		result = append(result, StrategyInfo{
			Name:        cfg.StrategyID,
			Description: cfg.Description,
			Parameters:  params,
		})
		seen[cfg.StrategyID] = true
	}

	// Then add registry entries that aren't in DB
	for _, info := range registryInfos {
		if !seen[info.Name] {
			result = append(result, info)
			seen[info.Name] = true
		}
	}

	return result, nil
}

// GetStrategyParams returns the effective params for a strategy.
// It checks DB first, then falls back to the registry.
func (db *StrategyDB) GetStrategyParams(ctx context.Context, strategyID string) (map[string]any, error) {
	// Try DB first
	cfg, err := db.store.GetStrategyConfig(ctx, strategyID)
	if err != nil {
		return nil, err
	}
	if cfg != nil && cfg.Params != "" {
		var params map[string]any
		if err := json.Unmarshal([]byte(cfg.Params), &params); err != nil {
			return nil, fmt.Errorf("invalid params JSON for %s: %w", strategyID, err)
		}
		return params, nil
	}

	// Fall back to registry
	s, err := DefaultRegistry.Get(strategyID)
	if err != nil {
		return nil, fmt.Errorf("strategy not found: %s", strategyID)
	}
	params := make(map[string]any)
	for _, p := range s.Parameters() {
		params[p.Name] = p.Default
	}
	return params, nil
}
