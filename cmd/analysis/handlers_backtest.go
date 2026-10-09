package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ruoxizhnya/quant-trading/internal/httpserver"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/reporting"
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
				httpserver.Error(c, http.StatusInternalServerError, err)
				return
			}
			c.JSON(http.StatusOK, gin.H{"jobs": jobs, "total": len(jobs)})
		})

		api.POST("", func(c *gin.Context) {
			bodyBytes, err := io.ReadAll(c.Request.Body)
			if err != nil || len(bodyBytes) == 0 {
				httpserver.Fail(c, http.StatusBadRequest, "empty request body")
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
					httpserver.FailCause(c, http.StatusInternalServerError, "failed to create job", err)
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
					// 引擎返回的是**带类别**的错误（pkg/errors），别把它拍平成 500 ——
					// 见 httpserver.StatusForAppError 的注释（AUD-60）。
					// 4xx 走 Error：把原因原样给用户（那本来就是给他看的）；
					// 5xx 走 FailCause：只回静态文案，真实原因只进日志
					// （P1-5 的信息泄露收口 —— DB 报错、连接串、内部路径不能出去）。
					if status := httpserver.StatusForAppError(err); status < http.StatusInternalServerError {
						httpserver.Error(c, status, err)
					} else {
						httpserver.FailCause(c, status, "backtest failed", err)
					}
					return
				}
				if saveErr := jobService.SaveSyncResult(c.Request.Context(), result); saveErr != nil {
					logger.Warn().Err(saveErr).Str("backtest_id", result.ID).Msg("Failed to persist backtest result to DB")
				}
				c.JSON(http.StatusOK, result)
				return
			}

			httpserver.Fail(c, http.StatusBadRequest, "invalid request body: must provide strategy+stock_pool (old format) or strategy_id+universe (new format)")
		})

		api.GET("/:id", func(c *gin.Context) {
			jobID := c.Param("id")
			job, err := jobService.GetJob(c.Request.Context(), jobID)
			if err != nil {
				httpserver.Error(c, http.StatusInternalServerError, err)
				return
			}
			if job == nil {
				httpserver.Fail(c, http.StatusNotFound, "job not found")
				return
			}
			c.JSON(http.StatusOK, job)
		})

		api.GET("/:id/report", func(c *gin.Context) {
			backtestID := c.Param("id")
			// OBS-01（切片 2）：透传引擎内存态里的真实状态（含 "invalid"），
			// 不再硬编码 "completed"。共用 buildResponseFromState 使其与
			// lookupBacktestResponse 口径一致。
			if resp, ok := buildResponseFromState(engine, backtestID); ok {
				c.JSON(http.StatusOK, resp)
				return
			}

			job, err := jobService.GetJob(c.Request.Context(), backtestID)
			if err != nil {
				httpserver.Error(c, http.StatusInternalServerError, err)
				return
			}
			if job == nil || job.Status != "completed" {
				httpserver.Fail(c, http.StatusNotFound, "backtest not found or not completed")
				return
			}
			var report map[string]any
			if err := json.Unmarshal(job.Result, &report); err != nil {
				httpserver.Fail(c, http.StatusInternalServerError, "failed to parse stored result")
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
				httpserver.Error(c, http.StatusInternalServerError, err)
				return
			}
			if job == nil || job.Status != "completed" {
				httpserver.Fail(c, http.StatusNotFound, "backtest not found or not completed")
				return
			}
			var stored backtest.BacktestResponse
			if err := json.Unmarshal(job.Result, &stored); err != nil {
				httpserver.Fail(c, http.StatusInternalServerError, "failed to parse stored result")
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
				httpserver.Error(c, http.StatusInternalServerError, err)
				return
			}
			if job == nil || job.Status != "completed" {
				httpserver.Fail(c, http.StatusNotFound, "backtest not found or not completed")
				return
			}
			var stored backtest.BacktestResponse
			if err := json.Unmarshal(job.Result, &stored); err != nil {
				httpserver.Fail(c, http.StatusInternalServerError, "failed to parse stored result")
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"backtest_id":  backtestID,
				"total_points": len(stored.PortfolioValues),
				"equity_curve": stored.PortfolioValues,
			})
		})

		api.GET("/:id/export/:format", func(c *gin.Context) {
			backtestID := c.Param("id")
			format := c.Param("format")
			if format != "html" {
				httpserver.Fail(c, http.StatusBadRequest, "unsupported format, only 'html' is supported (use browser Print → PDF for PDF export)")
				return
			}

			resp, lookupErr := lookupBacktestResponse(c, backtestID, engine, jobService, logger)
			if lookupErr != nil {
				// lookupBacktestResponse already wrote the error response
				return
			}

			opts := reporting.HTMLReportOptions{
				Theme:              c.DefaultQuery("theme", "light"),
				FooterNote:         c.Query("footer"),
				IncludeEquityChart: c.Query("equity") != "0", // default true
				IncludeTrades:      c.Query("trades") != "0", // default true
			}
			body, contentType, err := reporting.RenderHTML(resp, opts)
			if err != nil {
				logger.Error().Err(err).Str("backtest_id", backtestID).Msg("Failed to render HTML report")
				httpserver.Fail(c, http.StatusInternalServerError, "failed to render report")
				return
			}

			filename := fmt.Sprintf("backtest-%s-%s.html", backtestID, time.Now().Format("20060102"))
			c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
			c.Header("X-Backtest-Id", backtestID)
			c.Header("X-Backtest-Strategy", resp.Strategy)
			c.Data(http.StatusOK, contentType, body)
		})

		// P2-2 (ODR-027): multi-strategy comparison endpoint.
		// Query string: ?ids=bt-1,bt-2,bt-3
		// The same in-memory-first / DB-fallback resolver used by
		// `/report` is applied per-ID, so a freshly-completed backtest
		// (still in the Engine's state store) and a historical one
		// (DB only) can be compared on equal footing.
		api.GET("/compare", func(c *gin.Context) {
			rawIDs := c.Query("ids")
			if rawIDs == "" {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "missing 'ids' query parameter (comma-separated list of 2-8 backtest IDs)",
				})
				return
			}
			ids := strings.Split(rawIDs, ",")
			for i := range ids {
				ids[i] = strings.TrimSpace(ids[i])
			}
			resolver := reporting.NewCompareResolver(engine, jobService, logger)
			report, err := reporting.CompareReports(c.Request.Context(), ids, resolver)
			if err != nil {
				// Min/Max count errors are user-facing (400).
				// Anything else is an internal failure (500).
				if strings.Contains(err.Error(), "at least") || strings.Contains(err.Error(), "at most") || strings.Contains(err.Error(), "distinct") {
					httpserver.Error(c, http.StatusBadRequest, err)
					return
				}
				logger.Error().Err(err).Strs("ids", ids).Msg("Compare failed")
				httpserver.FailCause(c, http.StatusInternalServerError, "compare failed", err)
				return
			}
			// Partial-resolution is not an error — the payload itself
			// carries a `Missing` list so the UI can render the
			// "loaded N of M" banner.
			c.JSON(http.StatusOK, report)
		})
	}

	registerBacktestLegacyRedirects(router)
}

// buildResponseFromState reconstructs the API response for a backtest whose
// state is still resident in the engine's in-memory StateStore.
//
// OBS-01（切片 2）—— 为什么把它收敛成**唯一一处**：
// GET /:id/report 与 lookupBacktestResponse（GET /:id/export/:format 用）
// 原先各自内联了一份逐字段重建，且都把闸门写成 `status == "completed"`、
// 把 Status 硬编码成 "completed"。这正是 OBS-01 读路径的病根：写路径
// （引擎 buildBacktestResponse）学会说 "invalid" 之后，两条读路径仍会把
// 同一份结果说成 "completed"（或把无效运行直接挡在门外、UI 读不到原因）。
// 收敛到一个函数后，状态与 InvalidReasons 的透传只有一处实现，两条读路径
// 不可能再各自漂移。
//
// 返回 (resp, true) 表示内存里有可读结果（"completed" 或 "invalid"）；
// (zero, false) 表示内存里没有 / 状态不可读，调用方应退回 DB job 路径。
// "invalid" 必须放行 —— 否则「为什么无效」在 UI 上不可见（与
// Engine.GetBacktestResult 的闸门是同一裁决）。
func buildResponseFromState(engine *backtest.Engine, backtestID string) (backtest.BacktestResponse, bool) {
	status, err := engine.GetBacktestStatus(backtestID)
	if err != nil || (status != "completed" && status != "invalid") {
		return backtest.BacktestResponse{}, false
	}
	result, err := engine.GetBacktestResult(backtestID)
	if err != nil || result == nil {
		return backtest.BacktestResponse{}, false
	}
	params, _ := engine.GetBacktestParams(backtestID)
	return backtest.BacktestResponse{
		ID:              backtestID,
		Status:          status,
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
		InvalidReasons:  result.InvalidReasons,
		UniverseMaxSize: result.UniverseMaxSize,
	}, true
}

// lookupBacktestResponse fetches a backtest result by ID, falling back from
// in-memory (Engine) to stored job (JobService) when the in-memory copy
// has been evicted. On error, writes the error response to the gin context
// and returns the error to the caller (which should just `return`).
func lookupBacktestResponse(c *gin.Context, backtestID string, engine *backtest.Engine, jobService *backtest.JobService, logger zerolog.Logger) (backtest.BacktestResponse, error) {
	if resp, ok := buildResponseFromState(engine, backtestID); ok {
		return resp, nil
	}

	job, err := jobService.GetJob(c.Request.Context(), backtestID)
	if err != nil {
		logger.Error().Err(err).Str("backtest_id", backtestID).Msg("Failed to load backtest job")
		httpserver.Error(c, http.StatusInternalServerError, err)
		return backtest.BacktestResponse{}, err
	}
	if job == nil || job.Status != "completed" {
		httpserver.Fail(c, http.StatusNotFound, "backtest not found or not completed")
		return backtest.BacktestResponse{}, err
	}
	var stored backtest.BacktestResponse
	if err := json.Unmarshal(job.Result, &stored); err != nil {
		httpserver.Fail(c, http.StatusInternalServerError, "failed to parse stored result")
		return backtest.BacktestResponse{}, err
	}
	if stored.ID == "" {
		stored.ID = backtestID
	}
	return stored, nil
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
