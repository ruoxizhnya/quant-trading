package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

type MigrationConfig struct {
	MigrationsPath string
	DatabaseURL    string
	SchemaName     string
}

type MigrationManager struct {
	config MigrationConfig
	pool   *pgxpool.Pool
	logger zerolog.Logger
}

func NewMigrationManager(config MigrationConfig, pool *pgxpool.Pool, logger zerolog.Logger) *MigrationManager {
	if config.MigrationsPath == "" {
		config.MigrationsPath = "./migrations"
	}
	if config.SchemaName == "" {
		config.SchemaName = "public"
	}
	return &MigrationManager{
		config: config,
		pool:   pool,
		logger: logger.With().Str("component", "migration_manager").Logger(),
	}
}

func (mm *MigrationManager) RunMigrations(ctx context.Context) error {
	mm.logger.Info().
		Str("path", mm.config.MigrationsPath).
		Str("schema", mm.config.SchemaName).
		Msg("Starting database migrations")

	start := time.Now()

	instance, err := migrate.New(
		fmt.Sprintf("file://%s", mm.config.MigrationsPath),
		mm.config.DatabaseURL,
	)
	if err != nil {
		return fmt.Errorf("failed to create migration instance: %w", err)
	}
	defer instance.Close()

	currentVersion, dirty, _ := instance.Version()
	mm.logger.Info().
		Uint("current_version", currentVersion).
		Bool("dirty", dirty).
		Msg("Current database version")

	if err := instance.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migration up failed: %w", err)
	}

	version, _, _ := instance.Version()
	duration := time.Since(start)

	mm.logger.Info().
		Uint("version", version).
		Dur("duration", duration).
		Msg("Database migrations completed successfully")

	return nil
}

func (mm *MigrationManager) RunMigrationsDown(ctx context.Context, steps int) error {
	mm.logger.Info().Int("steps", steps).Msg("Rolling back migrations")

	instance, err := migrate.New(
		fmt.Sprintf("file://%s", mm.config.MigrationsPath),
		mm.config.DatabaseURL,
	)
	if err != nil {
		return fmt.Errorf("failed to create migration instance: %w", err)
	}
	defer instance.Close()

	if err := instance.Steps(-steps); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migration down failed: %w", err)
	}

	version, _, _ := instance.Version()
	mm.logger.Info().Uint("version", version).Msg("Rollback completed")
	return nil
}

func (mm *MigrationManager) GetVersion() (uint, bool, error) {
	instance, err := migrate.New(
		fmt.Sprintf("file://%s", mm.config.MigrationsPath),
		mm.config.DatabaseURL,
	)
	if err != nil {
		return 0, false, fmt.Errorf("failed to create migration instance: %w", err)
	}
	defer instance.Close()

	return instance.Version()
}

func (mm *MigrationManager) CreateMigration(name string, direction string) error {
	if direction != "up" && direction != "down" {
		return fmt.Errorf("direction must be 'up' or 'down', got '%s'", direction)
	}

	version, _, err := mm.GetVersion()
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	nextVersion := version + 1

	timestamp := time.Now().Format("20060102150405")
	dirName := fmt.Sprintf("%s_%d_%s", timestamp, nextVersion, name)

	migrationDir := filepath.Join(mm.config.MigrationsPath, dirName)
	if err := os.MkdirAll(migrationDir, 0755); err != nil {
		return fmt.Errorf("failed to create migration directory: %w", err)
	}

	var fileName string
	var template string
	if direction == "up" || direction == "" {
		fileName = "up.sql"
		template = fmt.Sprintf(`-- Migration %d: %s (%s)
-- This is an auto-generated migration file.
-- Modify the SQL below to implement your schema changes.

BEGIN;

-- TODO: Add your migration SQL here

COMMIT;
`, nextVersion, name, timestamp)
	} else {
		fileName = "down.sql"
		template = fmt.Sprintf(`-- Rollback for migration %d: %s (%s)
-- This is the rollback script for the above migration.

BEGIN;

-- TODO: Add your rollback SQL here

COMMIT;
`, nextVersion, name, timestamp)
	}

	filePath := filepath.Join(migrationDir, fileName)
	if err := os.WriteFile(filePath, []byte(template), 0644); err != nil {
		return fmt.Errorf("failed to write migration file: %w", err)
	}

	mm.logger.Info().
		Str("path", filePath).
		Uint("version", nextVersion).
		Msg("Migration file created")

	return nil
}

func (mm *MigrationManager) ListMigrations() ([]string, error) {
	entries, err := os.ReadDir(mm.config.MigrationsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations directory: %w", err)
	}

	var migrations []string
	for _, entry := range entries {
		if entry.IsDir() && len(entry.Name()) >= 15 { // minimum format: YYYYMMDDHHMMSS_1_name
			migrations = append(migrations, entry.Name())
		}
	}

	mm.logger.Info().
		Int("count", len(migrations)).
		Str("path", mm.config.MigrationsPath).
		Msg("Available migrations")

	return migrations, nil
}

func (mm *MigrationManager) ForceVersion(version uint) error {
	instance, err := migrate.New(
		fmt.Sprintf("file://%s", mm.config.MigrationsPath),
		mm.config.DatabaseURL,
	)
	if err != nil {
		return fmt.Errorf("failed to create migration instance: %w", err)
	}
	defer instance.Close()

	if err := instance.Force(int(version)); err != nil {
		return fmt.Errorf("failed to force version: %w", err)
	}

	mm.logger.Info().Uint("version", version).Msg("Database version forced")
	return nil
}
