package main

// Tests for cmd/strategy's configuration loading.
//
// This package had NO test file before AUD-36, which is part of why the
// missing AutomaticEnv survived: there was nothing to make the gap
// visible. The registration called that out explicitly — "加之前先补一条
// 「env 覆盖真的生效」的测试，否则改完无法证伪".
//
// These tests deliberately do NOT call t.Parallel(): loadConfig drives the
// *global* viper singleton and mutates zerolog's global level, so running
// them concurrently would race on shared state (same reasoning as
// cmd/data/setup_test.go).

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadConfig_ReadsYAML pins the baseline: with no env set, the values
// come from config/strategy-service.yaml (found via the "../../config"
// search path, since the test cwd is cmd/strategy/).
//
// 2026-09-25: redis.url 的基线值从 `redis://redis:6379` 改成
// `redis://localhost:6379` —— 部署形态改为「数据库/缓存跑宿主机、服务跑容器」后，
// config/*.yaml 的口径统一为**宿主机视角**（容器视角由 docker-compose 的 env
// 注入）。data_service.url 保持容器名不变（服务全在容器里），所以下面那条没动。
func TestLoadConfig_ReadsYAML(t *testing.T) {
	viper.Reset()

	cfg, err := loadConfig(zerolog.Nop())
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "0.0.0.0", cfg.Server.Host)
	assert.Equal(t, 8082, cfg.Server.Port)
	assert.Equal(t, "release", cfg.Server.GinMode)
	assert.Equal(t, "redis://localhost:6379", cfg.Redis.URL)
	assert.Equal(t, "http://data-service:8081", cfg.DataService.URL)
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.Equal(t, "json", cfg.Logging.Format)
}

// TestLoadConfig_EnvOverridesAreApplied is the AUD-36 guard.
//
// Before the fix, loadConfig never called AutomaticEnv, so every env var
// docker-compose sets for this service was ignored. The test asserts the
// **parsed Config struct**, not viper.Get: loadConfig goes through
// viper.Unmarshal, and env values are only visible to Unmarshal for keys
// that viper already knows about (from the config file or SetDefault).
// Asserting only viper.GetString would pass even if Unmarshal silently
// dropped the override — i.e. it would pin the wrong thing.
func TestLoadConfig_EnvOverridesAreApplied(t *testing.T) {
	viper.Reset()

	t.Setenv("SERVER_HOST", "127.0.0.1")
	t.Setenv("SERVER_PORT", "9999")
	t.Setenv("SERVER_GIN_MODE", "test")
	t.Setenv("REDIS_URL", "redis://example:6380")
	t.Setenv("DATA_SERVICE_URL", "http://example:9999")
	t.Setenv("LOGGING_LEVEL", "debug")
	t.Setenv("LOGGING_FORMAT", "text")

	cfg, err := loadConfig(zerolog.Nop())
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "127.0.0.1", cfg.Server.Host, "SERVER_HOST")
	assert.Equal(t, 9999, cfg.Server.Port, "SERVER_PORT")
	assert.Equal(t, "test", cfg.Server.GinMode, "SERVER_GIN_MODE")
	assert.Equal(t, "redis://example:6380", cfg.Redis.URL, "REDIS_URL")
	assert.Equal(t, "http://example:9999", cfg.DataService.URL, "DATA_SERVICE_URL")
	assert.Equal(t, "debug", cfg.Logging.Level, "LOGGING_LEVEL")
	assert.Equal(t, "text", cfg.Logging.Format, "LOGGING_FORMAT")
}
