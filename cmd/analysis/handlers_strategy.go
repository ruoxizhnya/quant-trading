package main

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

func registerStrategyRoutes(router *gin.Engine, strategyDB *strategy.StrategyDB) {
	router.GET("/api/strategies", func(c *gin.Context) {
		strategyType := c.Query("type")
		activeOnly := c.Query("active") == "true"
		if strategyType != "" || activeOnly {
			configs, err := strategyDB.List(c.Request.Context(), strategyType, activeOnly)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"strategies": configs})
			return
		}
		infos, err := strategyDB.ListWithDB(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"strategies": infos})
	})

	router.POST("/api/strategies", func(c *gin.Context) {
		var req struct {
			StrategyID   string `json:"strategy_id" binding:"required"`
			Name         string `json:"name" binding:"required"`
			Description  string `json:"description"`
			StrategyType string `json:"strategy_type" binding:"required"`
			Params       any    `json:"params"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		paramsJSON := "{}"
		if req.Params != nil {
			b, _ := json.Marshal(req.Params)
			paramsJSON = string(b)
		}
		cfg := &domain.StrategyConfig{
			StrategyID:   req.StrategyID,
			Name:         req.Name,
			Description:  req.Description,
			StrategyType: req.StrategyType,
			Params:       paramsJSON,
			IsActive:     true,
		}
		if err := strategyDB.Create(c.Request.Context(), cfg); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "strategy saved", "strategy_id": req.StrategyID})
	})

	router.GET("/api/strategies/:id", func(c *gin.Context) {
		id := c.Param("id")
		cfg, err := strategyDB.Get(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if cfg == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "strategy not found"})
			return
		}
		c.JSON(http.StatusOK, cfg)
	})

	router.PUT("/api/strategies/:id", func(c *gin.Context) {
		id := c.Param("id")
		var req struct {
			Name         string `json:"name"`
			Description  string `json:"description"`
			StrategyType string `json:"strategy_type"`
			Params       any    `json:"params"`
			IsActive     *bool  `json:"is_active"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		cfg, err := strategyDB.Get(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if cfg == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "strategy not found"})
			return
		}
		if req.Name != "" {
			cfg.Name = req.Name
		}
		if req.Description != "" {
			cfg.Description = req.Description
		}
		if req.StrategyType != "" {
			cfg.StrategyType = req.StrategyType
		}
		if req.Params != nil {
			b, _ := json.Marshal(req.Params)
			cfg.Params = string(b)
		}
		if req.IsActive != nil {
			cfg.IsActive = *req.IsActive
		}
		if err := strategyDB.Create(c.Request.Context(), cfg); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "strategy updated", "strategy_id": id})
	})

	router.DELETE("/api/strategies/:id", func(c *gin.Context) {
		id := c.Param("id")
		if err := strategyDB.Delete(c.Request.Context(), id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "strategy deleted", "strategy_id": id})
	})
}
