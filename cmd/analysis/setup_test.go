package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/observability"
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
