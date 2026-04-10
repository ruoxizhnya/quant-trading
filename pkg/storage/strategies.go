package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// SaveStrategyConfig upserts a strategy config.
func (s *PostgresStore) SaveStrategyConfig(ctx context.Context, cfg *domain.StrategyConfig) error {
	query := `
		INSERT INTO strategies (strategy_id, name, description, strategy_type, params, is_active, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (strategy_id) DO UPDATE SET
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			strategy_type = EXCLUDED.strategy_type,
			params = EXCLUDED.params,
			is_active = EXCLUDED.is_active,
			updated_at = NOW()
	`
	_, err := s.pool.Exec(ctx, query,
		cfg.StrategyID, cfg.Name, cfg.Description, cfg.StrategyType, cfg.Params, cfg.IsActive,
	)
	if err != nil {
		return fmt.Errorf("failed to save strategy config: %w", err)
	}
	return nil
}

// GetStrategyConfig retrieves a strategy config by strategy_id.
func (s *PostgresStore) GetStrategyConfig(ctx context.Context, strategyID string) (*domain.StrategyConfig, error) {
	query := `
		SELECT id, strategy_id, name, description, strategy_type, params, is_active, created_at, updated_at
		FROM strategies WHERE strategy_id = $1
	`
	var cfg domain.StrategyConfig
	err := s.pool.QueryRow(ctx, query, strategyID).Scan(
		&cfg.ID, &cfg.StrategyID, &cfg.Name, &cfg.Description,
		&cfg.StrategyType, &cfg.Params, &cfg.IsActive, &cfg.CreatedAt, &cfg.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get strategy config: %w", err)
	}
	return &cfg, nil
}

// ListStrategyConfigs returns all strategy configs, optionally filtered.
func (s *PostgresStore) ListStrategyConfigs(ctx context.Context, strategyType string, activeOnly bool) ([]*domain.StrategyConfig, error) {
	var query string
	var args []interface{}

	if strategyType != "" && activeOnly {
		query = `
			SELECT id, strategy_id, name, description, strategy_type, params, is_active, created_at, updated_at
			FROM strategies WHERE strategy_type = $1 AND is_active = TRUE
			ORDER BY strategy_id ASC
		`
		args = []interface{}{strategyType}
	} else if strategyType != "" {
		query = `
			SELECT id, strategy_id, name, description, strategy_type, params, is_active, created_at, updated_at
			FROM strategies WHERE strategy_type = $1
			ORDER BY strategy_id ASC
		`
		args = []interface{}{strategyType}
	} else if activeOnly {
		query = `
			SELECT id, strategy_id, name, description, strategy_type, params, is_active, created_at, updated_at
			FROM strategies WHERE is_active = TRUE
			ORDER BY strategy_id ASC
		`
	} else {
		query = `
			SELECT id, strategy_id, name, description, strategy_type, params, is_active, created_at, updated_at
			FROM strategies ORDER BY strategy_id ASC
		`
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list strategy configs: %w", err)
	}
	defer rows.Close()

	var results []*domain.StrategyConfig
	for rows.Next() {
		var cfg domain.StrategyConfig
		if err := rows.Scan(
			&cfg.ID, &cfg.StrategyID, &cfg.Name, &cfg.Description,
			&cfg.StrategyType, &cfg.Params, &cfg.IsActive, &cfg.CreatedAt, &cfg.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan strategy config row: %w", err)
		}
		results = append(results, &cfg)
	}
	return results, rows.Err()
}

// DeleteStrategyConfig soft-deletes a strategy (sets is_active=false).
func (s *PostgresStore) DeleteStrategyConfig(ctx context.Context, strategyID string) error {
	query := `UPDATE strategies SET is_active = FALSE, updated_at = NOW() WHERE strategy_id = $1`
	result, err := s.pool.Exec(ctx, query, strategyID)
	if err != nil {
		return fmt.Errorf("failed to delete strategy config: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("strategy not found: %s", strategyID)
	}
	return nil
}

// SeedStrategies seeds the 3 built-in strategies if they don't exist.
func (s *PostgresStore) SeedStrategies(ctx context.Context) error {
	builtins := []struct {
		strategyID, name, desc, strategyType, params string
	}{
		{
			strategyID:   "momentum",
			name:         "Momentum Strategy",
			desc:         "Classic momentum strategy using 20-day lookback period",
			strategyType: "momentum",
			params:       `{"lookback_days": 20, "long_threshold": 0.0, "short_threshold": 0.0}`,
		},
		{
			strategyID:   "value",
			name:         "Value Strategy",
			desc:         "Value factor strategy using EP (earnings price ratio)",
			strategyType: "value",
			params:       `{"factor": "ep"}`,
		},
		{
			strategyID:   "quality",
			name:         "Quality Strategy",
			desc:         "Quality factor strategy using ROE (return on equity)",
			strategyType: "quality",
			params:       `{"min_roe": 0.0}`,
		},
	}

	for _, b := range builtins {
		cfg := &domain.StrategyConfig{
			StrategyID:   b.strategyID,
			Name:         b.name,
			Description:  b.desc,
			StrategyType: b.strategyType,
			Params:       b.params,
			IsActive:     true,
		}
		if err := s.SaveStrategyConfig(ctx, cfg); err != nil {
			return fmt.Errorf("failed to seed strategy %s: %w", b.strategyID, err)
		}
	}

	s.logger.Info().Msg("built-in strategies seeded")
	return nil
}
