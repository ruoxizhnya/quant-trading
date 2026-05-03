package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
)

func registerWalkForwardRoutes(router *gin.Engine, wfEngine *backtest.WalkForwardEngine, logger zerolog.Logger) {
	wf := router.Group("/api/walkforward")
	{
		wf.POST("", runWalkForwardHandler(wfEngine, logger))
		wf.GET("/:strategy_id", getWalkForwardReportHandler(wfEngine))
		wf.GET("", listWalkForwardReportsHandler(wfEngine))
	}
}

func runWalkForwardHandler(wfEngine *backtest.WalkForwardEngine, logger zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req backtest.WalkForwardRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
			return
		}

		if req.WalkForwardParams.TrainDays <= 0 {
			req.WalkForwardParams.TrainDays = 250
		}
		if req.WalkForwardParams.TestDays <= 0 {
			req.WalkForwardParams.TestDays = 60
		}
		if req.WalkForwardParams.StepDays <= 0 {
			req.WalkForwardParams.StepDays = 60
		}
		if req.WalkForwardParams.MinTrainDays <= 0 {
			req.WalkForwardParams.MinTrainDays = req.WalkForwardParams.TrainDays / 2
		}

		report, err := wfEngine.RunWalkForward(c.Request.Context(), req)
		if err != nil {
			logger.Error().Err(err).Str("strategy", req.Strategy).Msg("Walk-forward validation failed")
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, report)
	}
}

func getWalkForwardReportHandler(wfEngine *backtest.WalkForwardEngine) gin.HandlerFunc {
	return func(c *gin.Context) {
		strategyID := c.Param("strategy_id")

		report, err := wfEngine.GetLatestReport(c.Request.Context(), strategyID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if report == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "no walk-forward report found"})
			return
		}
		c.JSON(http.StatusOK, report)
	}
}

func listWalkForwardReportsHandler(wfEngine *backtest.WalkForwardEngine) gin.HandlerFunc {
	return func(c *gin.Context) {
		reports, err := wfEngine.ListReports(c.Request.Context(), 50)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"reports": reports, "count": len(reports)})
	}
}
