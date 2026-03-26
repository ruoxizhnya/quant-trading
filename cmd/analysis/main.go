// Package main is the entry point for the Analysis Service.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	_ "github.com/ruoxizhnya/quant-trading/pkg/strategy/plugins"
)

func main() {
	// Initialize logger
	logger := initLogger()

	// Load configuration
	v := viper.New()
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config/analysis-service.yaml"
	}
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err != nil {
		logger.Fatal().Err(err).Msg("Failed to read config file")
	}

	// Configure zerolog from config
	logLevel := v.GetString("logging.level")
	level, err := zerolog.ParseLevel(logLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	// Initialize backtest engine
	engine, err := backtest.NewEngine(v, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize backtest engine")
	}

	// Initialize Postgres store and job service
	dbURL := v.GetString("database.url")
	if dbURL == "" {
		logger.Fatal().Msg("database.url not configured")
	}
	store, err := storage.NewPostgresStore(context.Background(), dbURL)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize postgres store")
	}
	jobService := backtest.NewJobService(store, engine)
	logger.Info().Msg("Job service initialized")

	// Setup Gin router
	if v.GetString("logging.format") == "json" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(requestLogger(logger))

	// Register routes
	registerRoutes(router, engine, jobService, logger)

	// Get server config
	host := v.GetString("server.host")
	port := v.GetInt("server.port")
	addr := fmt.Sprintf("%s:%d", host, port)

	// Create HTTP server
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Info().
			Str("address", addr).
			Msg("Analysis Service starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("Server failed")
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info().Msg("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error().Err(err).Msg("Server forced to shutdown")
	}

	logger.Info().Msg("Server exited")
}

// ── Copilot Handlers ──────────────────────────────────────────

const copilotSystemPrompt = `You are an expert Go programmer specializing in quantitative trading strategies. Your task is to generate a valid Go file that implements a trading Strategy.

## Strategy Interface
The Strategy interface (from github.com/ruoxizhnya/quant-trading/pkg/strategy) is:

type Strategy interface {
    Name() string
    Description() string
    Parameters() []Parameter
    GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]Signal, error)
}

type Parameter struct {
    Name        string
    Type        string  // "int", "float", "string", "bool"
    Default     any
    Description string
    Min        float64
    Max        float64
}

type Signal struct {
    Symbol   string
    Action   string  // "buy", "sell", "hold"
    Strength float64 // 0.0-1.0
    Price    float64
}

## Key Types (from github.com/ruoxizhnya/quant-trading/pkg/domain)
- type OHLCV struct { Symbol, Date time.Time, Open, High, Low, Close, Volume float64 }
- type Portfolio struct { Cash float64; Positions map[string]Position }
- type Position struct { Symbol string; Quantity float64; CurrentPrice, AvgCost float64 }

## Requirements for the generated code:
1. Package name: plugins
2. Use package-level struct with exported config and strategy structs
3. Implement ALL interface methods: Name(), Description(), Parameters(), GenerateSignals()
4. Include Configure(map[string]any) method for parameter injection
5. Include init() that calls strategy.GlobalRegister(&yourStrategy{})
6. Add Chinese comments explaining the strategy logic (use // comments)
7. Validate parameters in GenerateSignals() (check nil bars, bounds, etc.)
8. Return "hold" signals by default; only "buy" or "sell" when clear signal
9. Use meaningful variable names in Chinese pinyin or English
10. Output ONLY the Go code in a code block, no explanations before or after

## File structure template:
package plugins

import (
    "context"
    "sort"
    "time"

    "github.com/ruoxizhnya/quant-trading/pkg/domain"
    "github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// StrategyConfig holds configuration for this strategy.
type StrategyConfig struct {
    // Add your parameters here
}

// strategyImpl implements the Strategy interface.
type strategyImpl struct {
    name        string
    description string
    params      StrategyConfig
}

func (s *strategyImpl) Name() string { return "your_strategy_name" }
func (s *strategyImpl) Description() string { return "中文描述" }
func (s *strategyImpl) Parameters() []strategy.Parameter { /* return params */ }
func (s *strategyImpl) Configure(params map[string]any) { /* inject params */ }
func (s *strategyImpl) GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]strategy.Signal, error) { /* implement logic */ }

func init() {
    strategy.GlobalRegister(&strategyImpl{name: "your_strategy_name", description: "中文描述"})
}

## IMPORTANT:
- Output only the complete Go code block, nothing else
- The code must compile and be syntactically valid
- Strategy name should be slug-style lowercase (e.g., "rsi_mean_reversion")
- Include proper imports
- Use time.Time for dates
`

type copilotRequest struct {
	Prompt string `json:"prompt" binding:"required"`
}

type copilotResponse struct {
	Code         string `json:"code"`
	StrategyName string `json:"strategy_name"`
	Description  string `json:"description"`
}

type saveRequest struct {
	Code         string `json:"code" binding:"required"`
	StrategyName string `json:"strategy_name" binding:"required"`
}

func generateStrategyHandler(c *gin.Context) {
	var req copilotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "prompt is required"})
		return
	}

	apiKey := os.Getenv("AI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI_API_KEY or OPENAI_API_KEY environment variable not set"})
		return
	}

	// Build OpenAI-compatible request
	payload := map[string]any{
		"model": "gpt-4o-mini",
		"messages": []map[string]string{
			{"role": "system", "content": copilotSystemPrompt},
			{"role": "user", "content": req.Prompt},
		},
		"max_tokens": 2000,
	}

	payloadBytes, _ := json.Marshal(payload)
	aiURL := os.Getenv("AI_API_URL")
	if aiURL == "" {
		aiURL = "https://api.openai.com/v1/chat/completions"
	}

	reqCtx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, "POST", aiURL, bytes.NewReader(payloadBytes))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request: " + err.Error()})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": "AI request failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to parse AI response: " + err.Error()})
		return
	}

	// Extract content from OpenAI-style response
	choices, ok := result["choices"].([]any)
	if !ok || len(choices) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty response from AI model"})
		return
	}

	choice0, ok := choices[0].(map[string]any)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "malformed AI response"})
		return
	}

	msg, ok := choice0["message"].(map[string]any)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "malformed AI message"})
		return
	}

	content, ok := msg["content"].(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "AI returned non-text content"})
		return
	}

	// Parse strategy name from code
	strategyName := extractStrategyName(content)
	description := extractDescription(content)

	c.JSON(http.StatusOK, copilotResponse{
		Code:         content,
		StrategyName: strategyName,
		Description:  description,
	})
}

func extractStrategyName(code string) string {
	// Try to extract Name() return value
	for _, line := range strings.Split(code, "\n") {
		if strings.Contains(line, "func (s *") && strings.Contains(line, "Name()") {
			continue
		}
		if strings.Contains(line, `return "`) {
			name := strings.TrimSpace(line)
			name = strings.TrimPrefix(name, "return \"")
			name = strings.TrimSuffix(name, "\"")
			if len(name) > 0 && len(name) < 60 {
				return name
			}
		}
	}
	// Try GlobalRegister
	re := regexp.MustCompile(`GlobalRegister\s*\(\s*&[a-zA-Z]+\{\s*name:\s*"([^"]+)"`)
	m := re.FindStringSubmatch(code)
	if len(m) > 1 {
		return m[1]
	}
	return "generated_strategy"
}

func extractDescription(code string) string {
	for _, line := range strings.Split(code, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Description()") || strings.Contains(line, "description") {
			continue
		}
		if strings.Contains(line, `return "`) {
			desc := strings.TrimSpace(line)
			desc = strings.TrimPrefix(desc, "return \"")
			desc = strings.TrimSuffix(desc, "\"")
			if len(desc) > 5 && len(desc) < 200 {
				return desc
			}
		}
	}
	return ""
}

func saveStrategyHandler(c *gin.Context) {
	var req saveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code and strategy_name are required"})
		return
	}

	// Sanitize filename
	safeName := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == 0 {
			return '_'
		}
		return r
	}, req.StrategyName)

	filename := safeName + ".go"
	dir := "./generated_strategies"
	if err := os.MkdirAll(dir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create directory: " + err.Error()})
		return
	}

	filepath := dir + "/" + filename
	if _, err := os.Stat(filepath); err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "strategy file already exists: " + filepath})
		return
	}
	if err := os.WriteFile(filepath, []byte(req.Code), 0600); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to write file: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":       "strategy saved successfully",
		"strategy_name": req.StrategyName,
		"file":          filepath,
	})
}

// initLogger initializes the zerolog logger.
func initLogger() zerolog.Logger {
	zerolog.TimeFieldFormat = time.RFC3339Nano
	return log.Output(zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: "2006-01-02 15:04:05.000",
	})
}

// requestLogger returns a Gin middleware for logging requests.
func requestLogger(logger zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		event := logger.Info()
		if status >= 400 && status < 500 {
			event = logger.Warn()
		} else if status >= 500 {
			event = logger.Error()
		}

		event.
			Str("method", c.Request.Method).
			Str("path", path).
			Str("query", query).
			Int("status", status).
			Dur("latency", latency).
			Str("client_ip", c.ClientIP()).
			Int("body_size", c.Writer.Size()).
			Msg("Request")
	}
}

// registerRoutes registers all HTTP routes.
func registerRoutes(router *gin.Engine, engine *backtest.Engine, jobService *backtest.JobService, logger zerolog.Logger) {
	// Serve UI
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

	router.GET("/index.html", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("./cmd/analysis/static/index.html")
	})

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"service":   "analysis-service",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	})

	// OHLCV proxy for UI (avoids CORS issues)
	router.GET("/ohlcv/:symbol", func(c *gin.Context) {
		symbol := c.Param("symbol")
		startDate := c.Query("start_date")
		endDate := c.Query("end_date")
		if startDate == "" || endDate == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "start_date and end_date required (YYYYMMDD)"})
			return
		}
		// Proxy to data service
		dataURL := fmt.Sprintf("http://data-service:8081/ohlcv/%s?start_date=%s&end_date=%s", symbol, startDate, endDate)
		resp, err := http.Get(dataURL)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "data service unavailable: " + err.Error()})
			return
		}
		defer resp.Body.Close()
		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "invalid response from data service"})
			return
		}
		c.JSON(resp.StatusCode, result)
	})

	// Screen proxy for UI (proxies to data service)
	router.POST("/screen", func(c *gin.Context) {
		var reqBody map[string]interface{}
		if err := json.NewDecoder(c.Request.Body).Decode(&reqBody); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		bodyBytes, _ := json.Marshal(reqBody)
		dataURL := "http://data-service:8081/screen"
		resp, err := http.Post(dataURL, "application/json", bytes.NewReader(bodyBytes))
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "data service unavailable: " + err.Error()})
			return
		}
		defer resp.Body.Close()
		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "invalid response from data service"})
			return
		}
		c.JSON(resp.StatusCode, result)
	})

	// Proxy: stocks/count → data-service
	router.GET("/stocks/count", func(c *gin.Context) {
		resp, err := http.Get("http://data-service:8081/stocks/count")
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "data service unavailable"})
			return
		}
		defer resp.Body.Close()
		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "invalid response from data service"})
			return
		}
		c.JSON(resp.StatusCode, result)
	})

	// Proxy: market/index → data-service
	router.GET("/market/index", func(c *gin.Context) {
		resp, err := http.Get("http://data-service:8081/market/index")
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "data service unavailable"})
			return
		}
		defer resp.Body.Close()
		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "invalid response from data service"})
			return
		}
		c.JSON(resp.StatusCode, result)
	})

	// Strategy list endpoint
	router.GET("/api/strategies", func(c *gin.Context) {
		strategies := strategy.DefaultRegistry.ListWithInfo()
		c.JSON(http.StatusOK, gin.H{"strategies": strategies})
	})

	// Backtest endpoints
	api := router.Group("/backtest")
	{
		// Run a backtest — async via job service
		api.POST("", func(c *gin.Context) {
			var jobReq backtest.CreateJobRequest
			if err := c.ShouldBindJSON(&jobReq); err != nil {
				// Fallback: try old synchronous format
				var req backtest.BacktestRequest
				if err2 := c.ShouldBindJSON(&req); err2 == nil {
					ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
					defer cancel()
					result, err := engine.RunBacktest(ctx, req)
					if err != nil {
						c.JSON(http.StatusInternalServerError, gin.H{"error": "backtest failed", "details": err.Error()})
						return
					}
					c.JSON(http.StatusOK, result)
					return
				}
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "details": err.Error()})
				return
			}

			logger.Info().
				Str("strategy", jobReq.StrategyID).
				Str("start_date", jobReq.StartDate).
				Str("end_date", jobReq.EndDate).
				Str("universe", jobReq.Universe).
				Msg("Creating backtest job")

			job, err := jobService.CreateJob(c.Request.Context(), jobReq)
			if err != nil {
				logger.Error().Err(err).Msg("Failed to create job")
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create job", "details": err.Error()})
				return
			}

			c.JSON(http.StatusAccepted, gin.H{"job_id": job.ID, "status": job.Status})
		})

		// Get backtest job status and result
		api.GET("/:id", func(c *gin.Context) {
			jobID := c.Param("id")

			job, err := jobService.GetJob(c.Request.Context(), jobID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if job == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
				return
			}

			c.JSON(http.StatusOK, job)
		})

		// Get backtest report
		api.GET("/:id/report", func(c *gin.Context) {
			backtestID := c.Param("id")

			status, err := engine.GetBacktestStatus(backtestID)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"error": err.Error(),
				})
				return
			}

			if status != "completed" {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":  "backtest not completed",
					"status": status,
				})
				return
			}

			result, err := engine.GetBacktestResult(backtestID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, result)
		})

		// Get backtest trades
		api.GET("/:id/trades", func(c *gin.Context) {
			backtestID := c.Param("id")

			trades, err := engine.GetBacktestTrades(backtestID)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"error": err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"backtest_id": backtestID,
				"total":       len(trades),
				"trades":      trades,
			})
		})

		// Get backtest equity curve
		api.GET("/:id/equity", func(c *gin.Context) {
			backtestID := c.Param("id")

			equity, err := engine.GetBacktestEquity(backtestID)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{
					"error": err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"backtest_id": backtestID,
				"total_points": len(equity),
				"equity_curve": equity,
			})
		})
	}

	// API info
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

	// ── Strategy Copilot ──────────────────────────────────────
	copilot := router.Group("/api/copilot")
	{
		copilot.POST("/generate", generateStrategyHandler)
		copilot.POST("/save", saveStrategyHandler)
	}
}
