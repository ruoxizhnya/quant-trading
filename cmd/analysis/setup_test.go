package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/gene_pool"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/observability"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadConfig_ReadsYAML verifies that loadConfig reads a YAML config
// file, applies env var overrides, and sets the global zerolog level.
// S7-P2-3: extracted from main() — this test locks in the contract.
func TestLoadConfig_ReadsYAML(t *testing.T) {
	// Create a temp config file with known values.
	dir := t.TempDir()
	configPath := filepath.Join(dir, "test-config.yaml")
	err := os.WriteFile(configPath, []byte(`
logging:
  level: debug
server:
  host: 127.0.0.1
  port: 9999
data_service:
  url: http://test-data:8081
`), 0644)
	require.NoError(t, err)

	t.Setenv("CONFIG_PATH", configPath)
	logger := zerolog.Nop()
	v := loadConfig(logger)

	require.NotNil(t, v)
	assert.Equal(t, "debug", v.GetString("logging.level"))
	assert.Equal(t, "127.0.0.1", v.GetString("server.host"))
	assert.Equal(t, 9999, v.GetInt("server.port"))
	assert.Equal(t, "http://test-data:8081", v.GetString("data_service.url"))
	// Verify global log level was set.
	assert.Equal(t, zerolog.DebugLevel, zerolog.GlobalLevel())
}

// TestLoadConfig_DefaultsToInfoLevel verifies that an invalid log level
// in the config falls back to InfoLevel.
func TestLoadConfig_DefaultsToInfoLevel(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "bad-level.yaml")
	err := os.WriteFile(configPath, []byte(`
logging:
  level: not-a-real-level
`), 0644)
	require.NoError(t, err)

	t.Setenv("CONFIG_PATH", configPath)
	logger := zerolog.Nop()
	v := loadConfig(logger)

	require.NotNil(t, v)
	// Invalid level should fall back to InfoLevel.
	assert.Equal(t, zerolog.InfoLevel, zerolog.GlobalLevel())
}

// TestInitMetrics_ReturnsNonNil verifies that initMetrics constructs a
// valid Metrics, registers it, and wires it into the httpClient transport.
func TestInitMetrics_ReturnsNonNil(t *testing.T) {
	logger := zerolog.Nop()
	m := initMetrics(logger)

	require.NotNil(t, m)
	// Verify the metrics was wired into the httpClient transport.
	if transport, ok := httpClient.Transport.(*observability.HTTPTransport); ok {
		assert.NotNil(t, transport.Metrics, "httpClient transport should have metrics wired in")
	}
}

// TestDataServices_StructFields verifies the dataServices struct has
// the expected fields. This is a compile-time check that documents the
// struct contract.
func TestDataServices_StructFields(t *testing.T) {
	ds := &dataServices{}
	// Verify all expected fields exist and are nil by default.
	assert.Nil(t, ds.DataAdapter)
	assert.Nil(t, ds.JobService)
	assert.Nil(t, ds.WFEngine)
	assert.Nil(t, ds.BatchEngine)
	assert.Nil(t, ds.FactorAttributor)
}

// TestStartHTTPServer_ReturnsServer verifies that startHTTPServer
// returns a non-nil *http.Server with the correct address and timeouts
// from config. The server is started in a goroutine; we immediately
// call Close to stop it.
func TestStartHTTPServer_ReturnsServer(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "server-config.yaml")
	err := os.WriteFile(configPath, []byte(`
server:
  host: 127.0.0.1
  port: 0
`), 0644)
	require.NoError(t, err)

	t.Setenv("CONFIG_PATH", configPath)
	logger := zerolog.Nop()
	v := loadConfig(logger)

	// Create a minimal router.
	gin.SetMode(gin.TestMode)
	router := gin.New()

	srv := startHTTPServer(router, v, logger)
	require.NotNil(t, srv)
	assert.Equal(t, "127.0.0.1:0", srv.Addr)
	assert.Equal(t, 30*time.Second, srv.ReadTimeout)
	assert.Equal(t, 60*time.Second, srv.WriteTimeout)
	assert.Equal(t, 120*time.Second, srv.IdleTimeout)

	// Shut down the server to clean up the goroutine.
	require.NoError(t, srv.Close())
}

// ──────────────────────────────────────────────────────────────────────
// buildToolsRegistry tests (S7-P3-4 / Hermes Phase 1.7)
// ──────────────────────────────────────────────────────────────────────

// stubWFRunner satisfies builtin.WalkForwardRunner for registry
// construction tests. Execute is never called — we only verify tool
// registration, not execution.
type stubWFRunner struct{}

func (stubWFRunner) RunWalkForward(_ context.Context, _ string, _ []string, _, _ string, _ domain.WalkForwardParams) (*domain.WalkForwardReport, error) {
	return &domain.WalkForwardReport{}, nil
}

// TestBuildToolsRegistry_RegistersAll17Tools verifies that
// buildToolsRegistry registers all 17 tools (8 original S7-P3-3 tools +
// 8 Hermes Phase 1 tools + 1 Hermes Phase 2.2 tool) with the correct
// names. This is the wiring-level test — individual tool behavior is
// covered in pkg/tools/builtin/*_test.go.
//
// Reuses stubBacktestRunner from handlers_pipeline_test.go (same package).
func TestBuildToolsRegistry_RegistersAll17Tools(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Minimal viper config — only the keys buildToolsRegistry reads.
	v := newTestViper(t)

	// Gene pools with nil internal pgxpool — construction is safe, only
	// Execute would panic (which we never call in this test).
	factorPool := gene_pool.NewFactorPool(nil)
	strategyPool := gene_pool.NewStrategyPool(nil)

	reg := buildToolsRegistry(v, &stubBacktestRunner{}, stubWFRunner{}, factorPool, strategyPool, zerolog.Nop())
	require.NotNil(t, reg)

	tools := reg.List()
	assert.Len(t, tools, 17, "registry should contain exactly 17 tools (8 original + 8 Hermes Phase 1 + 1 Phase 2.2)")

	// Collect names into a set for O(1) lookup.
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name] = true
	}

	// Verify all 17 expected tool names are present.
	expectedTools := []string{
		// ── Original 8 (S7-P3-3) ──
		"backtest.run",
		"factor.compute",
		"factor.evaluate",
		"data.ohlcv",
		"data.stocks",
		"data.fundamentals",
		"strategy.list",
		"strategy.get",
		// ── Hermes Phase 1 additions (8) ──
		"validate_factor",       // Phase 1.1 (L1 gate)
		"compute_factor_ic",     // Phase 1.2 (L2 gate)
		"walk_forward_validate", // Phase 1.3 (L4 gate)
		"list_factors",          // Phase 1.4 (gene pool)
		"save_factor",           // Phase 1.4 (gene pool)
		"list_strategies",       // Phase 1.5 (gene pool)
		"save_strategy",         // Phase 1.5 (gene pool)
		"summarize_backtest",    // Phase 1.6
		// ── Hermes Phase 2.2 additions (1) ──
		"get_strategy_lineage", // Phase 2.2 (gene pool lineage)
	}
	for _, name := range expectedTools {
		assert.True(t, names[name], "tool %q should be registered", name)
	}
}

// TestBuildToolsRegistry_NoDuplicateNames ensures there are no accidental
// name collisions in the registry (each tool name must be unique).
func TestBuildToolsRegistry_NoDuplicateNames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	v := newTestViper(t)
	factorPool := gene_pool.NewFactorPool(nil)
	strategyPool := gene_pool.NewStrategyPool(nil)

	reg := buildToolsRegistry(v, &stubBacktestRunner{}, stubWFRunner{}, factorPool, strategyPool, zerolog.Nop())
	require.NotNil(t, reg)

	tools := reg.List()
	seen := make(map[string]bool, len(tools))
	for _, tool := range tools {
		if seen[tool.Name] {
			t.Errorf("duplicate tool name: %q", tool.Name)
		}
		seen[tool.Name] = true
	}
}

// newTestViper creates a viper config with the minimal keys needed by
// buildToolsRegistry (server.port, data_service.url).
func newTestViper(t *testing.T) *viper.Viper {
	t.Helper()
	v := viper.New()
	v.Set("server.port", 9999)
	v.Set("data_service.url", "http://test-data:8081")
	return v
}
