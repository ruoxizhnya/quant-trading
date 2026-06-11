package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/agents"
)

func main() {
	port := os.Getenv("AI_SERVICE_PORT")
	if port == "" {
		port = "8086"
	}

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())

	// Sprint 6 P0-9 (ODR-013, ADR-017 §2): per-user token-bucket
	// rate limit. Defaults: 10 req/min sustained, burst 10. Both
	// knobs are env-overridable so a CI smoke test can crank
	// the limit up while an incident response can drop it to 0.
	ratePerMin := envInt("AI_RATE_LIMIT_PER_MIN", 10)
	rateBurst := envInt("AI_RATE_LIMIT_BURST", 10)
	router.Use(newRateLimiter(ratePerMin, rateBurst).middleware())
	log.Printf("AI rate limit: %d req/min sustained, burst %d", ratePerMin, rateBurst)

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "healthy",
			"service":   "ai-research",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	})

	// Research Agent endpoints
	researchAgent := agents.NewResearchAgent()
	api := router.Group("/api")
	{
		api.POST("/research/hypothesis", func(c *gin.Context) {
			var req struct {
				Topic string `json:"topic" binding:"required"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			hypothesis, err := researchAgent.GenerateHypothesis(c.Request.Context(), req.Topic)
			if err != nil {
				c.JSON(http.StatusOK, gin.H{
					"error": err.Error(),
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"hypothesis": hypothesis,
			})
		})

		api.POST("/research/validate", func(c *gin.Context) {
			var req struct {
				Formula string `json:"formula" binding:"required"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			expr, err := researchAgent.ValidateFormula(req.Formula)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"valid":  true,
				"inputs": expr.Inputs,
				"ast":    expr.AST.String(),
			})
		})
	}

	// Start server
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: router,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutting down AI Research Service...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("Server forced to shutdown: %v", err)
		}
	}()

	log.Printf("AI Research Service starting on port %s", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed to start: %v", err)
	}
}
