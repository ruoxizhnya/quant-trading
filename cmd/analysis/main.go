package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	_ "github.com/ruoxizhnya/quant-trading/pkg/strategy/plugins"
	"github.com/spf13/viper"
)

var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

type strategyEngineAdapter struct {
	engine *backtest.Engine
}

func (a *strategyEngineAdapter) RunBacktest(
	ctx context.Context,
	strategyName string,
	stockPool []string,
	startDate, endDate string,
) (*domain.BacktestResult, error) {
	req := backtest.BacktestRequest{
		Strategy:  strategyName,
		StockPool: stockPool,
		StartDate: startDate,
		EndDate:   endDate,
	}
	resp, err := a.engine.RunBacktest(ctx, req)
	if err != nil {
		return nil, err
	}
	return &domain.BacktestResult{
		TotalReturn:    resp.TotalReturn,
		AnnualReturn:   resp.AnnualReturn,
		SharpeRatio:    resp.SharpeRatio,
		SortinoRatio:   resp.SortinoRatio,
		MaxDrawdown:    resp.MaxDrawdown,
		WinRate:        resp.WinRate,
		TotalTrades:    resp.TotalTrades,
		WinTrades:      resp.WinTrades,
		LoseTrades:     resp.LoseTrades,
		AvgHoldingDays: resp.AvgHoldingDays,
		CalmarRatio:    resp.CalmarRatio,
	}, nil
}

func main() {
	logger := initLogger()

	v := viper.New()
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config/analysis-service.yaml"
	}
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := v.ReadInConfig(); err != nil {
		logger.Fatal().Err(err).Msg("Failed to read config file")
	}

	logLevel := v.GetString("logging.level")
	level, err := zerolog.ParseLevel(logLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	dataServiceURL := v.GetString("data_service.url")
	if dataServiceURL == "" {
		dataServiceURL = "http://localhost:8081"
	}
	httpProvider := marketdata.NewHTTPProvider(dataServiceURL, logger)

	engine, err := backtest.NewEngine(v, httpProvider, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize backtest engine")
	}

	dbURL := v.GetString("database.url")
	if dbURL == "" || strings.Contains(dbURL, "${") {
		dbUser := v.GetString("database.user")
		dbPassword := v.GetString("database.password")
		dbHost := v.GetString("database.host")
		dbPort := v.GetInt("database.port")
		dbName := v.GetString("database.database")
		dbSSLMode := v.GetString("database.sslmode")
		if dbHost == "" {
			dbHost = "localhost"
		}
		if dbPort == 0 {
			dbPort = 5432
		}
		if dbName == "" {
			dbName = "quant_trading"
		}
		if dbSSLMode == "" {
			dbSSLMode = "disable"
		}
		dbURL = fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
			url.PathEscape(dbUser), url.PathEscape(dbPassword), dbHost, dbPort, dbName, dbSSLMode)
	}
	store, err := storage.NewPostgresStore(context.Background(), dbURL)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize postgres store")
	}
	engine.SetStore(store)

	pgProvider := marketdata.NewPostgresProvider(store, logger)
	dataAdapter := marketdata.NewDataAdapter(nil, pgProvider, httpProvider, logger)
	engine.SetDataAdapter(dataAdapter)

	jobService := backtest.NewJobService(store, engine)
	logger.Info().Msg("Job service initialized")

	wfEngine := backtest.NewWalkForwardEngine(engine, store)
	logger.Info().Msg("Walk-forward engine initialized")

	factorAttributor := data.NewFactorAttributor(store)
	logger.Info().Msg("Factor attribution service initialized")

	copilotService := strategy.NewCopilotService()
	logger.Info().Bool("ai_configured", copilotService.IsConfigured()).Msg("Copilot service initialized")

	copilotRunner := &strategyEngineAdapter{engine: engine}

	strategyDB := strategy.NewStrategyDB(store)
	if err := store.SeedStrategies(context.Background()); err != nil {
		logger.Warn().Err(err).Msg("failed to seed built-in strategies")
	} else {
		logger.Info().Msg("strategy DB seeded")
	}

	if v.GetString("logging.format") == "json" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(corsMiddleware())
	router.Use(newRateLimiter(100, time.Minute).middleware())
	router.Use(requestLogger(logger))

	registerRoutes(router, engine, jobService, wfEngine, strategyDB, copilotService, copilotRunner, factorAttributor, logger)

	host := v.GetString("server.host")
	port := v.GetInt("server.port")
	addr := fmt.Sprintf("%s:%d", host, port)

	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		logger.Info().
			Str("address", addr).
			Msg("Analysis Service starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("Server failed")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info().Msg("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error().Err(err).Msg("Server forced to shutdown")
	}

	logger.Info().Msg("Server exited")
}

func initLogger() zerolog.Logger {
	return zerolog.New(os.Stdout).With().Timestamp().Logger()
}

func requestLogger(logger zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Info().
			Str("method", c.Request.Method).
			Str("path", c.Request.URL.Path).
			Int("status", c.Writer.Status()).
			Dur("latency", time.Since(start)).
			Msg("request")
	}
}

func registerRoutes(router *gin.Engine, engine *backtest.Engine, jobService *backtest.JobService, wfEngine *backtest.WalkForwardEngine, strategyDB *strategy.StrategyDB, copilotService *strategy.CopilotService, copilotRunner strategy.BacktestRunner, factorAttributor *data.FactorAttributor, logger zerolog.Logger) {
	router.Static("/static", "./cmd/analysis/static")

	router.GET("/", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./cmd/analysis/static/index.html")
	})
	router.GET("/screen", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./cmd/analysis/static/screen.html")
	})
	router.GET("/screen.html", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./cmd/analysis/static/screen.html")
	})
	router.GET("/dashboard", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./cmd/analysis/static/dashboard.html")
	})
	router.GET("/dashboard.html", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./cmd/analysis/static/dashboard.html")
	})
	router.GET("/copilot", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./cmd/analysis/static/copilot.html")
	})
	router.GET("/copilot.html", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./cmd/analysis/static/copilot.html")
	})
	router.GET("/strategy-selector", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./cmd/analysis/static/strategy-selector.html")
	})
	router.GET("/strategy-selector.html", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./cmd/analysis/static/strategy-selector.html")
	})
	router.GET("/index.html", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./cmd/analysis/static/index.html")
	})

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"service":   "analysis-service",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	})

	router.GET("/api/v1", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"service": "analysis-service",
			"version": "1.0.0",
			"endpoints": []string{
				"GET  /health",
				"POST /backtest",
				"GET  /backtest/:id/report",
				"GET  /backtest/:id/trades",
				"GET  /backtest/:id/equity",
			},
		})
	})

	registerProxyRoutes(router, httpClient, logger)
	registerBacktestRoutes(router, engine, jobService, logger)
	registerWalkForwardRoutes(router, wfEngine, logger)
	registerStrategyRoutes(router, strategyDB)
	registerCopilotRoutes(router, copilotService, copilotRunner)
	registerDatasourceRoutes(router, engine, logger)
	registerFactorRoutes(router, factorAttributor, logger)
}
