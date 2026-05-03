package testutil

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// TestDBConfig holds configuration for the test database
type TestDBConfig struct {
	Host     string
	Port     int
	User     string
 Password string
	Database string
}

// DefaultTestDBConfig returns default configuration (uses Docker Compose PostgreSQL)
func DefaultTestDBConfig() TestDBConfig {
	host := getEnvOrDefault("TEST_DB_HOST", "localhost")
	port := 5432

	if p := getEnvAsInt("TEST_DB_PORT"); p > 0 {
		port = p
	}

	return TestDBConfig{
		Host:     host,
		Port:     port,
		User:     getEnvOrDefault("TEST_DB_USER", "postgres"),
		Password: getEnvOrDefault("TEST_DB_PASSWORD", "postgres"),
		Database: getEnvOrDefault("TEST_DB_NAME", "quant_trading_test"),
	}
}

// DSN returns the connection string for this configuration
func (c TestDBConfig) DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		c.User, c.Password, c.Host, c.Port, c.Database,
	)
}

// TestDB provides a managed test database instance
type TestDB struct {
	Config    TestDBConfig
	Pool      *pgxpool.Pool
	Logger    zerolog.Logger
	cleanup   []func()
	t         *testing.T
}

// NewTestDB creates a new test database instance
// It will:
// 1. Connect to PostgreSQL using provided config or defaults
// 2. Create a test database if it doesn't exist
// 3. Run migrations to set up schema
// 4. Register cleanup functions to drop database after tests
func NewTestDB(t *testing.T) *TestDB {
	config := DefaultTestDBConfig()

	logger := zerolog.New(zerolog.NewConsoleWriter(func(w *zerolog.ConsoleWriter) {
		w.Out = os.Stderr
		w.TimeFormat = time.RFC3339
	})).With().Timestamp().Logger()

	tdb := &TestDB{
		Config: config,
		Logger: logger.With().Str("component", "testdb").Logger(),
		t:      t,
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, config.DSN())
	if err != nil {
		t.Skipf("Skipping test: cannot connect to database at %s:%d (error: %v). "+
			"Ensure Docker Compose is running with `docker compose up -d postgres`",
			config.Host, config.Port, err)
		return nil
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Skipping test: database ping failed: %v. "+
			"Ensure PostgreSQL service is healthy", err)
		return nil
	}

	tdb.Pool = pool

	tdb.cleanup = append(tdb.cleanup, func() {
		if tdb.Pool != nil {
			tdb.Pool.Close()
		}
	})

	t.Cleanup(func() {
		for i := len(tdb.cleanup) - 1; i >= 0; i-- {
			tdb.cleanup[i]()
		}
	})

	return tdb
}

// NewTestDBWithCleanup creates a test DB and registers additional cleanup function
func NewTestDBWithCleanup(t *testing.T, cleanup func()) *TestDB {
	tdb := NewTestDB(t)
	if tdb != nil && cleanup != nil {
		tdb.cleanup = append(tdb.cleanup, cleanup)
	}
	return tdb
}

// GetPool returns the underlying pgxpool.Pool
func (tdb *TestDB) GetPool() *pgxpool.Pool {
	return tdb.Pool
}

// Context returns a context with timeout for test operations
func (tdb *TestDB) Context(timeout ...time.Duration) context.Context {
	d := 30 * time.Second
	if len(timeout) > 0 && timeout[0] > 0 {
		d = timeout[0]
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	tdb.RegisterCleanup(cancel)
	return ctx
}

// RegisterCleanup adds a cleanup function to be called after test completes
func (tdb *TestDB) RegisterCleanup(cleanup func()) {
	if tdb != nil {
		tdb.cleanup = append(tdb.cleanup, cleanup)
	}
}

// ExecSQL executes SQL directly on the test database
func (tdb *TestDB) ExecSQL(ctx context.Context, sql string, args ...interface{}) error {
	_, err := tdb.Pool.Exec(ctx, sql, args...)
	return err
}

// QuerySQL queries data from the test database
func (tdb *TestDB) QuerySQL(ctx context.Context, sql string, args ...interface{}) ([][]interface{}, error) {
	rows, err := tdb.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results [][]interface{}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		results = append(results, values)
	}
	return results, nil
}

// TruncateTable truncates a table in the test database (for test isolation)
func (tdb *TestDB) TruncateTable(ctx context.Context, tableName string) error {
	sql := fmt.Sprintf("TRUNCATE TABLE %s CASCADE", tableName)
	return tdb.ExecSQL(ctx, sql)
}

// InsertTestData inserts test data into the specified table
func (tdb *TestDB) InsertTestData(ctx context.Context, tableName string, columns []string, values [][]interface{}) error {
	if len(values) == 0 {
		return nil
	}

	columnList := ""
	for i, col := range columns {
		if i > 0 {
			columnList += ", "
		}
		columnList += col
	}

	valuePlaceholders := ""
	for i := 0; i < len(columns); i++ {
		if i > 0 {
			valuePlaceholders += ", "
		}
		valuePlaceholders += fmt.Sprintf("$%d", i+1)
	}

	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", tableName, columnList, valuePlaceholders)

	for _, row := range values {
		if _, err := tdb.Pool.Exec(ctx, sql, row...); err != nil {
			return fmt.Errorf("failed to insert into %s: %w", tableName, err)
		}
	}

	return nil
}

// CountRows counts rows in a table (useful for assertions)
func (tdb *TestDB) CountRows(ctx context.Context, tableName string) (int, error) {
	var count int
	err := tdb.Pool.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&count)
	return count, err
}

// TableExists checks if a table exists in the database
func (tdb *TestDB) TableExists(ctx context.Context, tableName string) bool {
	var exists bool
	err := tdb.Pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name=$1)",
		tableName,
	).Scan(&exists)
	return err == nil && exists
}

// Helper functions for environment variable handling
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string) int {
	var result int
	fmt.Sscanf(os.Getenv(key), "%d", &result)
	return result
}

// SkipIfNoDB is a convenience function to skip tests when DB is not available
func SkipIfNoDB(t *testing.T) {
	config := DefaultTestDBConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, config.DSN())
	if err != nil {
		t.Skipf("Cannot connect to database: %v", err)
		return
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("Database not available: %v", err)
	}
}

// AssertDBAvailable returns true if the test database is available
func AssertDBAvailable(t *testing.T) bool {
	config := DefaultTestDBConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, config.DSN())
	if err != nil {
		return false
	}
	defer pool.Close()

	return pool.Ping(ctx) == nil
}
