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
// AUD-35 guard: the env name for `logging.level` is LOGGING_LEVEL
// ──────────────────────────────────────────────────────────────────────

// writeLoggingConfig writes a minimal config with a known logging.level
// and points CONFIG_PATH at it.
func writeLoggingConfig(t *testing.T) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "logging.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("logging:\n  level: info\n"), 0644))
	t.Setenv("CONFIG_PATH", configPath)
}

// TestLoadConfig_LoggingLevelEnvNameIsHonoured is the AUD-35 guard.
//
// docker-compose.yml and deploy/k8s/configmap.yaml used to set `LOG_LEVEL` /
// `LOG_FORMAT`. No code path reads those names: loadConfig applies
// AutomaticEnv + SetEnvKeyReplacer(".", "_"), which maps the config key
// `logging.level` to the env name **LOGGING_LEVEL**. The deploy files have
// been renamed; this pins the name that actually reaches the config.
//
// Note the precedence being asserted: env beats the config file in viper, so
// a non-zero override here is proof the env name was consulted — not that the
// file happened to agree.
func TestLoadConfig_LoggingLevelEnvNameIsHonoured(t *testing.T) {
	writeLoggingConfig(t)
	t.Setenv("LOGGING_LEVEL", "debug")

	v := loadConfig(zerolog.Nop())

	require.NotNil(t, v)
	assert.Equal(t, "debug", v.GetString("logging.level"),
		"LOGGING_LEVEL 必须覆盖 yaml 里的 logging.level（viper 里 env 优先于配置文件）")
}

// TestLoadConfig_LogLevelIsNotASecondEntry is the other half of the AUD-35
// guard: the retired name must stay dead.
//
// AUD-29 removed the k8s `GIN_MODE` key for exactly this reason — it was a
// second entry to the same decision, discoverable only by reading gin's
// source. The same argument applies here: if someone later "helpfully" adds
// BindEnv("logging.level", "LOG_LEVEL"), this test fails and points them at
// the precedent instead of quietly re-creating two names for one knob.
func TestLoadConfig_LogLevelIsNotASecondEntry(t *testing.T) {
	writeLoggingConfig(t)
	t.Setenv("LOG_LEVEL", "debug") // the retired name

	v := loadConfig(zerolog.Nop())

	require.NotNil(t, v)
	assert.Equal(t, "info", v.GetString("logging.level"),
		"LOG_LEVEL 是已退役的名字（AUD-35）；它不许成为第二个入口")
}
