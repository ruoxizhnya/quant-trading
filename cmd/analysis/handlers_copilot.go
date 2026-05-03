package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

type copilotRequest struct {
	Description string `json:"description"`
	Language    string `json:"language"`
}

type copilotResponse struct {
	Code         string `json:"code"`
	Language     string `json:"language"`
	Explanation  string `json:"explanation"`
	StrategyName string `json:"strategy_name"`
}

type saveRequest struct {
	Code         string `json:"code" binding:"required"`
	StrategyName string `json:"strategy_name" binding:"required"`
}

func generateStrategyHandler(c *gin.Context) {
	var req copilotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Description == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "description is required"})
		return
	}

	apiKey := os.Getenv("AI_API_KEY")
	apiURL := os.Getenv("AI_API_URL")
	if apiKey == "" || apiURL == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI API not configured (set AI_API_KEY and AI_API_URL)"})
		return
	}

	prompt := fmt.Sprintf(`You are an expert quantitative trading strategy developer for A-share (Chinese stock market).
Given the following strategy description, generate a complete Go strategy implementation.

Strategy Description: %s

Requirements:
1. The strategy must implement the strategy.Strategy interface with these methods:
   - Name() string
   - Description() string
   - Parameters() []strategy.Parameter
   - Configure(params map[string]interface{}) error
   - GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]strategy.Signal, error)
   - Weight(signal strategy.Signal, portfolioValue float64) float64
   - Cleanup()

2. Use domain.OHLCV for price data (fields: Symbol, Date, Open, High, Low, Close, Volume)
3. Use domain.Portfolio for portfolio state (fields: Cash, Positions map[string]domain.Position, UpdatedAt time.Time)
4. Return []strategy.Signal with Action "buy" or "sell", Symbol, Strength (0-1), and Reason

Generate ONLY the Go code, no explanations. Use package "plugins".`, req.Description)

	aiReqBody := map[string]interface{}{
		"model": "gpt-4",
		"messages": []map[string]string{
			{"role": "system", "content": "You are an expert quantitative trading strategy developer."},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.7,
		"max_tokens":  2000,
	}

	jsonBody, _ := json.Marshal(aiReqBody)
	httpReq, err := http.NewRequestWithContext(c.Request.Context(), "POST", apiURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create AI request"})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "AI API request failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	var aiResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&aiResp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse AI response"})
		return
	}

	code := extractCodeFromAIResponse(aiResp)
	strategyName := extractStrategyName(code)
	description := extractDescription(code)

	c.JSON(http.StatusOK, copilotResponse{
		Code:         code,
		Language:     "go",
		Explanation:  description,
		StrategyName: strategyName,
	})
}

func extractCodeFromAIResponse(resp map[string]interface{}) string {
	choices, ok := resp["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return ""
	}
	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		return ""
	}
	message, ok := choice["message"].(map[string]interface{})
	if !ok {
		return ""
	}
	content, ok := message["content"].(string)
	if !ok {
		return ""
	}

	re := regexp.MustCompile("(?s)```go\\s*\n(.*?)```")
	matches := re.FindStringSubmatch(content)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	re2 := regexp.MustCompile("(?s)```\\s*\n(.*?)```")
	matches2 := re2.FindStringSubmatch(content)
	if len(matches2) > 1 {
		return strings.TrimSpace(matches2[1])
	}

	return strings.TrimSpace(content)
}

func extractStrategyName(code string) string {
	re := regexp.MustCompile(`func\s*\(\s*s\s*\*\s*(\w+)\s*\)\s*Name\(\)\s*string`)
	matches := re.FindStringSubmatch(code)
	if len(matches) > 1 {
		structName := matches[1]
		nameRe := regexp.MustCompile(fmt.Sprintf(`func\s*\(\s*s\s*\*\s*%s\s*\)\s*Name\(\)\s*string\s*\{\s*return\s*"([^"]+)"`, structName))
		nameMatches := nameRe.FindStringSubmatch(code)
		if len(nameMatches) > 1 {
			return nameMatches[1]
		}
	}
	return "custom_strategy"
}

func extractDescription(code string) string {
	re := regexp.MustCompile(`func\s*\(\s*s\s*\*\s*\w+\s*\)\s*Description\(\)\s*string\s*\{\s*return\s*"([^"]+)"`)
	matches := re.FindStringSubmatch(code)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func saveStrategyHandler(c *gin.Context) {
	var req saveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	filename := fmt.Sprintf("strategy_%s.go", req.StrategyName)
	dir := "./pkg/strategy/plugins"
	if err := os.MkdirAll(dir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create directory"})
		return
	}

	filePath := fmt.Sprintf("%s/%s", dir, filename)
	if err := os.WriteFile(filePath, []byte(req.Code), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save strategy: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":       "strategy saved",
		"strategy_name": req.StrategyName,
		"file_path":     filePath,
	})
}

func registerCopilotRoutes(router *gin.Engine, copilotService *strategy.CopilotService, copilotRunner strategy.BacktestRunner) {
	copilot := router.Group("/api/copilot")
	{
		copilot.POST("/generate", func(c *gin.Context) {
			if !copilotService.IsConfigured() {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI not configured (set AI_API_KEY and AI_API_URL)"})
				return
			}
			var req strategy.GenerateParams
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
				return
			}
			result := copilotService.Generate(c.Request.Context(), req, copilotRunner)
			c.JSON(http.StatusAccepted, gin.H{
				"job_id": result.JobID,
				"status": result.Status,
			})
		})

		copilot.GET("/generate/:job_id", func(c *gin.Context) {
			jobID := c.Param("job_id")
			result := copilotService.GetJob(jobID)
			if result == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
				return
			}
			result.Lock()
			status := result.Status
			code := result.Code
			buildErr := result.BuildErr
			btResult := result.BacktestResult
			btErr := result.BacktestErr
			strategyName := result.StrategyName
			result.Unlock()

			resp := gin.H{
				"job_id": jobID,
				"status": status,
			}
			if code != "" {
				resp["generated_code"] = code
			}
			if buildErr != "" {
				resp["build_error"] = buildErr
			}
			if strategyName != "" {
				resp["strategy_name"] = strategyName
			}
			if btErr != "" {
				resp["backtest_error"] = btErr
			}
			if btResult != nil {
				resp["backtest_result"] = btResult
			}
			c.JSON(http.StatusOK, resp)
		})

		copilot.GET("/stats", func(c *gin.Context) {
			generated, buildable, backtested := copilotService.Stats()
			rate := copilotService.AcceptanceRate()
			c.JSON(http.StatusOK, gin.H{
				"generated":      generated,
				"buildable":      buildable,
				"backtest_valid": backtested,
				"acceptance_rate": rate,
			})
		})

		copilot.POST("/save", saveStrategyHandler)
	}

	_ = context.Background()
}
