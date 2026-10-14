package main

import (
	"github.com/ruoxizhnya/quant-trading/pkg/httpserver"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

func registerFactorRoutes(router *gin.Engine, factorAttributor *data.FactorAttributor, logger zerolog.Logger) {
	factorAPI := router.Group("/api/factor")
	{
		factorAPI.GET("/returns/:factor", func(c *gin.Context) {
			factorStr := c.Param("factor")
			factorType, ok := domain.ParseFactorType(factorStr)
			if !ok {
				httpserver.Failf(c, http.StatusBadRequest, "invalid factor type: %s", factorStr)
				return
			}

			startDateStr := c.Query("start_date")
			endDateStr := c.Query("end_date")
			if startDateStr == "" || endDateStr == "" {
				httpserver.Fail(c, http.StatusBadRequest, "start_date and end_date required (YYYY-MM-DD)")
				return
			}

			startDate, err := time.Parse("2006-01-02", startDateStr)
			if err != nil {
				httpserver.Fail(c, http.StatusBadRequest, "invalid start_date format")
				return
			}
			endDate, err := time.Parse("2006-01-02", endDateStr)
			if err != nil {
				httpserver.Fail(c, http.StatusBadRequest, "invalid end_date format")
				return
			}

			returns, err := factorAttributor.GetFactorReturnsTimeSeries(c.Request.Context(), factorType, startDate, endDate)
			if err != nil {
				httpserver.Error(c, http.StatusInternalServerError, err)
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"factor":     factorStr,
				"start_date": startDateStr,
				"end_date":   endDateStr,
				"total":      len(returns),
				"data":       returns,
			})
		})

		factorAPI.GET("/ic/:factor", func(c *gin.Context) {
			factorStr := c.Param("factor")
			factorType, ok := domain.ParseFactorType(factorStr)
			if !ok {
				httpserver.Failf(c, http.StatusBadRequest, "invalid factor type: %s", factorStr)
				return
			}

			startDateStr := c.Query("start_date")
			endDateStr := c.Query("end_date")
			if startDateStr == "" || endDateStr == "" {
				httpserver.Fail(c, http.StatusBadRequest, "start_date and end_date required (YYYY-MM-DD)")
				return
			}

			startDate, err := time.Parse("2006-01-02", startDateStr)
			if err != nil {
				httpserver.Fail(c, http.StatusBadRequest, "invalid start_date format")
				return
			}
			endDate, err := time.Parse("2006-01-02", endDateStr)
			if err != nil {
				httpserver.Fail(c, http.StatusBadRequest, "invalid end_date format")
				return
			}

			icEntries, err := factorAttributor.GetICTimeSeries(c.Request.Context(), factorType, startDate, endDate)
			if err != nil {
				httpserver.Error(c, http.StatusInternalServerError, err)
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"factor":     factorStr,
				"start_date": startDateStr,
				"end_date":   endDateStr,
				"total":      len(icEntries),
				"data":       icEntries,
			})
		})

		factorAPI.POST("/compute-returns", func(c *gin.Context) {
			var req struct {
				Factor    string `json:"factor" binding:"required"`
				TradeDate string `json:"trade_date" binding:"required"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				httpserver.Error(c, http.StatusBadRequest, err)
				return
			}

			factorType, ok := domain.ParseFactorType(req.Factor)
			if !ok {
				httpserver.Failf(c, http.StatusBadRequest, "invalid factor type: %s", req.Factor)
				return
			}

			tradeDate, err := time.Parse("2006-01-02", req.TradeDate)
			if err != nil {
				httpserver.Fail(c, http.StatusBadRequest, "invalid trade_date format (YYYY-MM-DD)")
				return
			}

			logger.Info().
				Str("factor", req.Factor).
				Time("date", tradeDate).
				Msg("Computing factor quintile returns")

			if err := factorAttributor.ComputeFactorReturns(c.Request.Context(), factorType, tradeDate); err != nil {
				logger.Error().Err(err).Msg("Failed to compute factor returns")
				httpserver.Error(c, http.StatusInternalServerError, err)
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"message":    "factor returns computed successfully",
				"factor":     req.Factor,
				"trade_date": req.TradeDate,
			})
		})

		factorAPI.POST("/compute-ic", func(c *gin.Context) {
			var req struct {
				Factor      string `json:"factor" binding:"required"`
				TradeDate   string `json:"trade_date" binding:"required"`
				ForwardDays int    `json:"forward_days"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				httpserver.Error(c, http.StatusBadRequest, err)
				return
			}

			factorType, ok := domain.ParseFactorType(req.Factor)
			if !ok {
				httpserver.Failf(c, http.StatusBadRequest, "invalid factor type: %s", req.Factor)
				return
			}

			tradeDate, err := time.Parse("2006-01-02", req.TradeDate)
			if err != nil {
				httpserver.Fail(c, http.StatusBadRequest, "invalid trade_date format (YYYY-MM-DD)")
				return
			}

			forwardDays := req.ForwardDays
			if forwardDays <= 0 {
				forwardDays = 20
			}

			logger.Info().
				Str("factor", req.Factor).
				Time("date", tradeDate).
				Int("forward_days", forwardDays).
				Msg("Computing factor IC")

			icEntry, err := factorAttributor.ComputeIC(c.Request.Context(), factorType, tradeDate, forwardDays)
			if err != nil {
				logger.Error().Err(err).Msg("Failed to compute IC")
				httpserver.Error(c, http.StatusInternalServerError, err)
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"message":      "IC computed successfully",
				"factor":       req.Factor,
				"trade_date":   req.TradeDate,
				"forward_days": forwardDays,
				"ic":           icEntry.IC,
				"p_value":      icEntry.PValue,
				"top_ic":       icEntry.TopIC,
			})
		})

		factorAPI.GET("/list", func(c *gin.Context) {
			// D3 起因子清单 = 注册表投影（库管理）：新增/停用因子只改
			// 库里定义，本接口零改动。kernels 字段让调用方区分「可计算」
			// 与「名册在册、内核待定」（pending）——清单诚实优先。
			defs := domain.BuiltinFactorDefinitions
			factors := make([]string, 0, len(defs))
			kernels := make(map[string]string, len(defs))
			for _, d := range defs {
				factors = append(factors, string(d.Name))
				kernels[string(d.Name)] = string(d.Kernel)
			}
			c.JSON(http.StatusOK, gin.H{
				"total":   len(factors),
				"factors": factors,
				"kernels": kernels,
			})
		})
	}
}
