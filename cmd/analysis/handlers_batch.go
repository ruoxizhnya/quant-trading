package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
)

func registerBatchRoutes(router *gin.Engine, batchEngine *backtest.BatchEngine, logger zerolog.Logger) {
	api := router.Group("/api/batch")
	{
		api.POST("", runBatchHandler(batchEngine, logger))
		api.GET("/:batch_id", getBatchReportHandler(batchEngine))
		api.GET("/:batch_id/export/:format", exportBatchReportHandler(batchEngine, logger))
	}
}

func runBatchHandler(batchEngine *backtest.BatchEngine, logger zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req batchRunRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
			return
		}

		if len(req.Tasks) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no tasks provided"})
			return
		}

		config := backtest.DefaultBatchConfig()
		if req.Config.Concurrency > 0 {
			config.Concurrency = req.Config.Concurrency
		}
		if req.Config.RunWF {
			config.RunWF = req.Config.RunWF
		}
		if req.Config.WFTrainDays > 0 {
			config.WFTrainDays = req.Config.WFTrainDays
		}
		if req.Config.WFTestDays > 0 {
			config.WFTestDays = req.Config.WFTestDays
		}
		if req.Config.WFStepDays > 0 {
			config.WFStepDays = req.Config.WFStepDays
		}
		batchEngine.SetConfig(config)

		ctx := c.Request.Context()
		report, err := batchEngine.Run(ctx, req.Tasks)
		if err != nil {
			logger.Error().Err(err).Int("tasks", len(req.Tasks)).Msg("Batch backtest failed")
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, report)
	}
}

func getBatchReportHandler(batchEngine *backtest.BatchEngine) gin.HandlerFunc {
	return func(c *gin.Context) {
		batchID := c.Param("batch_id")
		report, err := batchEngine.GetReport(batchID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "batch report not found"})
			return
		}
		c.JSON(http.StatusOK, report)
	}
}

func exportBatchReportHandler(batchEngine *backtest.BatchEngine, logger zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		batchID := c.Param("batch_id")
		format := c.Param("format")

		report, err := batchEngine.GetReport(batchID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "batch report not found"})
			return
		}

		switch format {
		case "json":
			c.Header("Content-Type", "application/json")
			c.Header("Content-Disposition", "attachment; filename=batch_"+batchID+".json")
			c.JSON(http.StatusOK, report)
		case "csv":
			c.Header("Content-Type", "text/csv")
			c.Header("Content-Disposition", "attachment; filename=batch_"+batchID+".csv")
			// For CSV export we return the report as JSON since CSV serialization
			// would need a writer interface. Clients can use /api/batch/:id/export/csv
			// and we stream CSV in a real implementation.
			c.JSON(http.StatusOK, gin.H{"message": "CSV export not yet implemented, use JSON"})
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported format: " + format})
		}
	}
}

type batchRunRequest struct {
	Tasks  []backtest.BatchTask `json:"tasks" binding:"required,min=1"`
	Config batchConfigRequest   `json:"config"`
}

type batchConfigRequest struct {
	Concurrency int  `json:"concurrency"`
	RunWF       bool `json:"run_walk_forward"`
	WFTrainDays int  `json:"wf_train_days"`
	WFTestDays  int  `json:"wf_test_days"`
	WFStepDays  int  `json:"wf_step_days"`
}
