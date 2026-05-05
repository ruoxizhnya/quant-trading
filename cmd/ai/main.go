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
