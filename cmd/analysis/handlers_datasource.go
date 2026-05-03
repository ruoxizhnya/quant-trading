package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
)

func registerDatasourceRoutes(router *gin.Engine, engine *backtest.Engine, logger zerolog.Logger) {
	ds := router.Group("/api/datasource")
	{
		ds.GET("/status", func(c *gin.Context) {
			adapter := engine.DataAdapter()
			if adapter == nil {
				c.JSON(http.StatusOK, gin.H{
					"enabled": false,
					"mode":    "direct",
					"source":  "http",
				})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"enabled": true,
				"primary": adapter.Primary(),
				"stopped": adapter.Stopped(),
			})
		})

		ds.POST("/switch", func(c *gin.Context) {
			var req struct {
				Name  string `json:"name" binding:"required"`
				Type  string `json:"type" binding:"required"`
				URL   string `json:"url"`
				Token string `json:"token"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			var newProvider marketdata.Provider

			switch req.Type {
			case "http":
				if req.URL == "" {
					c.JSON(http.StatusBadRequest, gin.H{"error": "url required for http type"})
					return
				}
				newProvider = marketdata.NewHTTPProvider(req.URL, logger)
			case "inmemory":
				newProvider = marketdata.NewInMemoryProvider()
			default:
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unsupported runtime switch type: %q (only http/inmemory)", req.Type)})
				return
			}

			if err := engine.SwitchDataSource(c.Request.Context(), req.Name, newProvider); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			logger.Info().
				Str("name", req.Name).
				Str("type", req.Type).
				Msg("Data source switched via API")

			c.JSON(http.StatusOK, gin.H{
				"message": "data source switched",
				"name":    req.Name,
				"type":    req.Type,
			})
		})

		ds.GET("/health", func(c *gin.Context) {
			adapter := engine.DataAdapter()
			if adapter == nil {
				c.JSON(http.StatusOK, gin.H{
					"status": "ok",
					"mode":   "direct (no adapter)",
				})
				return
			}
			err := adapter.CheckConnectivity(c.Request.Context())
			if err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"status": "unhealthy",
					"error":  err.Error(),
				})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"status":  "healthy",
				"primary": adapter.Primary(),
			})
		})
	}
}
