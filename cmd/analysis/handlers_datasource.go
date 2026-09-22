package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
)

// registerDatasourceRoutes exposes the read-only data-source observability
// endpoints. The runtime-switch gate (POST /api/datasource/switch) was
// retired in ODR-059: it accepted an arbitrary provider URL — a path that
// could point the engine at a non-L0 service, against ADR-022 §1's
// "external sources are ingested at the single L0 door" principle — and it
// was non-functional in production anyway (the adapter is wired with a nil
// EventBus, so SetPrimary panicked). The read source is fixed at startup
// from `data_service.url`.
func registerDatasourceRoutes(router *gin.Engine, engine *backtest.Engine) {
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
