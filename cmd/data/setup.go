package main

// setup.go contains the composition-root builders for the data
// service. S7-P2-5 (ODR-043): these were inlined in the 1713-line
// main.go; extracting them makes main() a thin orchestrator and
// gives each builder a single, testable responsibility.
//
// All builders read from the global viper instance that loadConfig()
// populates — this matches the existing config pattern in cmd/data
// and is intentionally NOT refactored to pass *viper.Viper around
// (that would be a separate task).

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"

	"github.com/ruoxizhnya/quant-trading/internal/httpserver"
	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/data/equitydeep"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// loadConfig reads the data-service YAML config (config/data-service.yaml)
// and populates the global viper instance. Sets sensible defaults for
// server, logging, database, and tushare keys.
func loadConfig() error {
	viper.SetConfigName("data-service")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./config")
	viper.AddConfigPath("../config")
	viper.AddConfigPath("../../config")

	// Set defaults
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.port", 8081)
	viper.SetDefault("logging.level", "info")
	viper.SetDefault("logging.format", "json")
	viper.SetDefault("database.sslmode", "disable")
	viper.SetDefault("tushare.max_retries", 3)
	// Empty means "auto-discover contracts/field_dictionary.yaml next to the
	// config dir". Override with an absolute path (or EQUITYDEEP_FIELD_DICTIONARY).
	viper.SetDefault("equitydeep.field_dictionary", "")

	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	return nil
}

// requestLogger returns a gin middleware that logs every request with
// method, path, query, status, latency, and client IP. Uses the
// package-level logging.Logger initialised by logging.Init.
func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		logger := logging.Logger
		if status >= 500 {
			logger.Error().
				Str("method", c.Request.Method).
				Str("path", path).
				Str("query", query).
				Int("status", status).
				Dur("latency", latency).
				Str("client_ip", c.ClientIP()).
				Msg("Request failed")
		} else if status >= 400 {
			logger.Warn().
				Str("method", c.Request.Method).
				Str("path", path).
				Str("query", query).
				Int("status", status).
				Dur("latency", latency).
				Str("client_ip", c.ClientIP()).
				Msg("Request error")
		} else {
			logger.Info().
				Str("method", c.Request.Method).
				Str("path", path).
				Str("query", query).
				Int("status", status).
				Dur("latency", latency).
				Str("client_ip", c.ClientIP()).
				Msg("Request")
		}
	}
}

// initStore connects to PostgreSQL using the database.* viper keys.
// Fatal-exits the process on connection failure (data-service cannot
// operate without a database).
func initStore(ctx context.Context, logger zerolog.Logger) *storage.PostgresStore {
	dbConnString := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		viper.GetString("database.user"),
		viper.GetString("database.password"),
		viper.GetString("database.host"),
		viper.GetInt("database.port"),
		viper.GetString("database.database"),
		viper.GetString("database.sslmode"),
	)

	store, err := storage.NewPostgresStore(ctx, dbConnString)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to connect to PostgreSQL")
	}
	return store
}

// initCache connects to Redis using the redis.url viper key.
func initCache(logger zerolog.Logger) storage.Cache {
	cache, err := storage.NewCache(viper.GetString("redis.url"))
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to connect to Redis")
	}
	return cache
}

// buildDataCache creates the cache-aside layer that wraps Redis +
// PostgreSQL for OHLCV and stock lookups.
func buildDataCache(cache storage.Cache, store *storage.PostgresStore) *data.DataCache {
	return data.NewDataCache(cache, store)
}

// buildTushareClient creates the Tushare API client from tushare.*
// viper keys. Warns (but does not fail) if the token is empty — sync
// endpoints will fail at call time, but read endpoints still work.
func buildTushareClient(store *storage.PostgresStore, cache storage.Cache, logger zerolog.Logger) *data.TushareClient {
	tushareToken := viper.GetString("tushare.token")
	if tushareToken == "" {
		logger.Warn().Msg("TUSHARE_TOKEN is not set; sync endpoints will fail")
	}
	return data.NewTushareClient(
		tushareToken,
		viper.GetString("tushare.base_url"),
		viper.GetInt("tushare.max_retries"),
		store,
		cache,
	)
}

// buildEquityDeepDictionary loads contracts/field_dictionary.yaml, the
// whitelist and unit-conversion table that the equitydeep ingest door needs to
// normalize anything. There is deliberately no fallback mapping: without the
// dictionary the endpoint answers 503 rather than guessing, because a guessed
// raw_name → field_code mapping or unit scale would silently corrupt the
// numbers it produced.
//
// The contracts directory is not embedded in the binary, so the path is either
// configured explicitly or discovered relative to the working directory using
// the same search order loadConfig uses for config/.
func buildEquityDeepDictionary(logger zerolog.Logger) *equitydeep.Dictionary {
	configured := viper.GetString("equitydeep.field_dictionary")
	candidates := []string{
		"./contracts/field_dictionary.yaml",
		"../contracts/field_dictionary.yaml",
		"../../contracts/field_dictionary.yaml",
	}
	if configured != "" {
		candidates = []string{configured}
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		dict, err := equitydeep.LoadDictionary(path)
		if err != nil {
			logger.Error().Err(err).Str("path", path).Msg("Failed to load field dictionary")
			return nil
		}
		logger.Info().Str("path", path).Msg("Field dictionary loaded")
		return dict
	}

	logger.Warn().
		Str("searched", strings.Join(candidates, ", ")).
		Msg("Field dictionary not found; POST /api/ingest/equitydeep will answer 503")
	return nil
}

// rateLimitPerMinute returns the gateway rate limit (requests per
// ClientIP per minute window) from rate_limit.per_minute, defaulting
// to 100. Env-overridable via RATE_LIMIT_PER_MINUTE through viper
// AutomaticEnv, mirroring the AI_RATE_LIMIT_PER_MIN pattern (ODR-013).
func rateLimitPerMinute() int {
	if n := viper.GetInt("rate_limit.per_minute"); n > 0 {
		return n
	}
	return 100
}

// applyGinMode sets gin's process-wide run mode from server.gin_mode.
//
// AUD-29 (ODR-065): must run exactly once, before any router is built.
// It used to happen inside buildRouter, keyed off logging.level — see
// internal/httpserver/ginmode.go for why that was the wrong key and the
// wrong place.
func applyGinMode(logger zerolog.Logger) {
	raw := viper.GetString(httpserver.ConfigKeyGinMode)
	applied, recognized := httpserver.ApplyGinMode(raw)
	if !recognized {
		logger.Warn().
			Str(httpserver.ConfigKeyGinMode, raw).
			Str("applied", applied).
			Msg("unrecognized gin mode; falling back to release")
	}
}

// buildRouter creates the gin router with recovery, CORS, rate-limiting,
// and request-logging middleware.
//
// gin's run mode is deliberately NOT set here. It is a process-wide global
// applied once at startup by applyGinMode (AUD-29); tests call buildRouter
// directly, so mutating the global here would race with parallel tests.
func buildRouter() *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(httpserver.CORS(httpserver.AllowedOrigins(viper.GetViper())))
	router.Use(newRateLimiter(rateLimitPerMinute(), time.Minute).middleware())
	router.Use(requestLogger())
	return router
}

// startHTTPServer creates and starts the HTTP server in a goroutine.
// The caller is responsible for graceful shutdown via waitForShutdown +
// gracefulShutdown.
func startHTTPServer(router *gin.Engine, logger zerolog.Logger) *http.Server {
	addr := fmt.Sprintf("%s:%d",
		viper.GetString("server.host"),
		viper.GetInt("server.port"),
	)

	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info().Str("addr", addr).Msg("HTTP server starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("HTTP server failed")
		}
	}()

	return srv
}

// waitForShutdown blocks until SIGINT or SIGTERM is received, then
// returns the signal for logging.
func waitForShutdown() os.Signal {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	return <-quit
}

// gracefulShutdown drains the HTTP server with a 30s timeout.
func gracefulShutdown(srv *http.Server, logger zerolog.Logger) {
	logger.Info().Msg("Shutting down data service...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("Server forced to shutdown")
	}

	logger.Info().Msg("Data service stopped")
}
