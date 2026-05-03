package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
)

func registerBacktestRoutes(router *gin.Engine, engine *backtest.Engine, jobService *backtest.JobService, logger zerolog.Logger) {
	api := router.Group("/api/backtest")
	{
		api.GET("", func(c *gin.Context) {
			limit := 20
			if l, err := strconv.Atoi(c.Query("limit")); err == nil && l > 0 && l <= 100 {
				limit = l
			}
			jobs, err := jobService.ListJobs(c.Request.Context(), limit)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"jobs": jobs, "total": len(jobs)})
		})

		api.POST("", func(c *gin.Context) {
			bodyBytes, err := io.ReadAll(c.Request.Body)
			if err != nil || len(bodyBytes) == 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "empty request body"})
				return
			}

			var jobReq backtest.CreateJobRequest
			if json.Unmarshal(bodyBytes, &jobReq) == nil && jobReq.StrategyID != "" && jobReq.Universe != "" {
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
				return
			}

			var req backtest.BacktestRequest
			if json.Unmarshal(bodyBytes, &req) == nil && req.Strategy != "" {
				ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
				defer cancel()
				result, err := engine.RunBacktest(ctx, req)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "backtest failed", "details": err.Error()})
					return
				}
				if saveErr := jobService.SaveSyncResult(c.Request.Context(), result); saveErr != nil {
					logger.Warn().Err(saveErr).Str("backtest_id", result.ID).Msg("Failed to persist backtest result to DB")
				}
				c.JSON(http.StatusOK, result)
				return
			}

			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: must provide strategy+stock_pool (old format) or strategy_id+universe (new format)"})
		})

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

		api.GET("/:id/report", func(c *gin.Context) {
			backtestID := c.Param("id")
			status, err := engine.GetBacktestStatus(backtestID)
			if err == nil && status == "completed" {
				result, err := engine.GetBacktestResult(backtestID)
				if err == nil && result != nil {
					params, _ := engine.GetBacktestParams(backtestID)
					resp := backtest.BacktestResponse{
						ID:              backtestID,
						Status:          "completed",
						Strategy:        params.StrategyName,
						StartDate:       result.StartDate.Format("2006-01-02"),
						EndDate:         result.EndDate.Format("2006-01-02"),
						TotalReturn:     result.TotalReturn,
						AnnualReturn:    result.AnnualReturn,
						SharpeRatio:     result.SharpeRatio,
						SortinoRatio:    result.SortinoRatio,
						MaxDrawdown:     result.MaxDrawdown,
						MaxDrawdownDate: result.MaxDrawdownDate.Format("2006-01-02"),
						WinRate:         result.WinRate,
						TotalTrades:     result.TotalTrades,
						WinTrades:       result.WinTrades,
						LoseTrades:      result.LoseTrades,
						AvgHoldingDays:  result.AvgHoldingDays,
						CalmarRatio:     result.CalmarRatio,
						StockPool:       params.StockPool,
						InitialCapital:  params.InitialCapital,
						PortfolioValues: result.PortfolioValues,
						Trades:          result.Trades,
					}
					c.JSON(http.StatusOK, resp)
					return
				}
			}

			job, err := jobService.GetJob(c.Request.Context(), backtestID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if job == nil || job.Status != "completed" {
				c.JSON(http.StatusNotFound, gin.H{"error": "backtest not found or not completed"})
				return
			}
			var report map[string]any
			if err := json.Unmarshal(job.Result, &report); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse stored result"})
				return
			}
			c.JSON(http.StatusOK, report)
		})

		api.GET("/:id/trades", func(c *gin.Context) {
			backtestID := c.Param("id")
			trades, err := engine.GetBacktestTrades(backtestID)
			if err == nil {
				c.JSON(http.StatusOK, gin.H{
					"backtest_id": backtestID,
					"total":       len(trades),
					"trades":      trades,
				})
				return
			}

			job, err := jobService.GetJob(c.Request.Context(), backtestID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if job == nil || job.Status != "completed" {
				c.JSON(http.StatusNotFound, gin.H{"error": "backtest not found or not completed"})
				return
			}
			var stored backtest.BacktestResponse
			if err := json.Unmarshal(job.Result, &stored); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse stored result"})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"backtest_id": backtestID,
				"total":       len(stored.Trades),
				"trades":      stored.Trades,
			})
		})

		api.GET("/:id/equity", func(c *gin.Context) {
			backtestID := c.Param("id")
			equity, err := engine.GetBacktestEquity(backtestID)
			if err == nil {
				c.JSON(http.StatusOK, gin.H{
					"backtest_id":  backtestID,
					"total_points": len(equity),
					"equity_curve": equity,
				})
				return
			}

			job, err := jobService.GetJob(c.Request.Context(), backtestID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if job == nil || job.Status != "completed" {
				c.JSON(http.StatusNotFound, gin.H{"error": "backtest not found or not completed"})
				return
			}
			var stored backtest.BacktestResponse
			if err := json.Unmarshal(job.Result, &stored); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse stored result"})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"backtest_id":  backtestID,
				"total_points": len(stored.PortfolioValues),
				"equity_curve": stored.PortfolioValues,
			})
		})
	}

	registerBacktestLegacyRedirects(router)
}

func registerBacktestLegacyRedirects(router *gin.Engine) {
	legacy := router.Group("/backtest")
	{
		legacy.GET("", func(c *gin.Context) {
			c.Request.URL.Path = "/api/backtest"
			router.HandleContext(c)
		})
		legacy.POST("", func(c *gin.Context) {
			c.Request.URL.Path = "/api/backtest"
			router.HandleContext(c)
		})
		legacy.GET("/:id", func(c *gin.Context) {
			c.Request.URL.Path = "/api/backtest/" + c.Param("id")
			router.HandleContext(c)
		})
		legacy.GET("/:id/report", func(c *gin.Context) {
			c.Request.URL.Path = "/api/backtest/" + c.Param("id") + "/report"
			router.HandleContext(c)
		})
		legacy.GET("/:id/trades", func(c *gin.Context) {
			c.Request.URL.Path = "/api/backtest/" + c.Param("id") + "/trades"
			router.HandleContext(c)
		})
		legacy.GET("/:id/equity", func(c *gin.Context) {
			c.Request.URL.Path = "/api/backtest/" + c.Param("id") + "/equity"
			router.HandleContext(c)
		})
	}
}
