package main

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

func getFactorHandler(store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		factorName := domain.FactorType(c.Param("factor_name"))
		symbol := c.Query("symbol")
		dateStr := c.Query("date")

		if symbol == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "symbol query param is required"})
			return
		}
		if dateStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "date query param is required (YYYYMMDD)"})
			return
		}

		date, err := time.Parse("20060102", dateStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, use YYYYMMDD"})
			return
		}

		entry, err := store.GetFactorCache(ctx, symbol, date, factorName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if entry == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "factor cache entry not found"})
			return
		}

		c.JSON(http.StatusOK, entry)
	}
}

// syncFactorHandler computes and caches a specific factor for all stocks.
// POST /sync/factors/:factor_name
func syncFactorHandler(fc *data.FactorComputer) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		factorName := c.Param("factor_name")

		var req struct {
			Date string `json:"date"` // YYYYMMDD
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.Date == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "date field required (YYYYMMDD)"})
			return
		}

		date, err := time.Parse("20060102", req.Date)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, use YYYYMMDD"})
			return
		}

		factor := domain.FactorType(factorName)
		var computeErr error
		switch factor {
		case domain.FactorMomentum:
			computeErr = fc.ComputeMomentumFactor(ctx, date, 20)
		case domain.FactorValue:
			computeErr = fc.ComputeValueFactor(ctx, date)
		case domain.FactorQuality:
			computeErr = fc.ComputeQualityFactor(ctx, date)
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported factor: " + factorName})
			return
		}

		if computeErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": computeErr.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":     "factor computed and cached",
			"factor_name": factorName,
			"date":        req.Date,
		})
	}
}

// syncAllFactorsHandler computes and caches all factors for all stocks.
// POST /sync/factors/all
func syncAllFactorsHandler(fc *data.FactorComputer) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		var req struct {
			Date string `json:"date"` // YYYYMMDD
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.Date == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "date field required (YYYYMMDD)"})
			return
		}

		date, err := time.Parse("20060102", req.Date)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, use YYYYMMDD"})
			return
		}

		if err := fc.ComputeAllFactors(ctx, date, 20, true); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "all factors computed and cached",
			"date":    req.Date,
		})
	}
}

// ---- Factor Attribution & IC Handlers ----

// getFactorAttributionHandler returns factor quintile returns for a date range.
// GET /api/factors/attribution/:factor_name?start_date=YYYYMMDD&end_date=YYYYMMDD
func getFactorAttributionHandler(fa *data.FactorAttributor) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		factorName := domain.FactorType(c.Param("factor_name"))
		startDateStr := c.Query("start_date")
		endDateStr := c.Query("end_date")

		if startDateStr == "" || endDateStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "start_date and end_date query params required (YYYYMMDD)"})
			return
		}

		startDate, err := time.Parse("20060102", startDateStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start_date format, use YYYYMMDD"})
			return
		}
		endDate, err := time.Parse("20060102", endDateStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end_date format, use YYYYMMDD"})
			return
		}

		returns, err := fa.GetFactorReturnsTimeSeries(ctx, factorName, startDate, endDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"factor": factorName, "returns": returns})
	}
}

// getICHandler returns IC series for a factor over a date range.
// GET /api/factors/ic/:factor_name?start_date=YYYYMMDD&end_date=YYYYMMDD
func getICHandler(fa *data.FactorAttributor) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		factorName := domain.FactorType(c.Param("factor_name"))
		startDateStr := c.Query("start_date")
		endDateStr := c.Query("end_date")

		if startDateStr == "" || endDateStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "start_date and end_date query params required (YYYYMMDD)"})
			return
		}

		startDate, err := time.Parse("20060102", startDateStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start_date format, use YYYYMMDD"})
			return
		}
		endDate, err := time.Parse("20060102", endDateStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end_date format, use YYYYMMDD"})
			return
		}

		icEntries, err := fa.GetICTimeSeries(ctx, factorName, startDate, endDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"factor": factorName, "ic_series": icEntries})
	}
}

type syncAttributionRequest struct {
	StartDate string `json:"start_date"` // YYYYMMDD
	EndDate   string `json:"end_date"`   // YYYYMMDD
}

// syncFactorAttributionHandler computes factor attribution over a date range.
// POST /api/sync/factor-attribution/:factor_name
func syncFactorAttributionHandler(fa *data.FactorAttributor) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		factorName := domain.FactorType(c.Param("factor_name"))

		var req syncAttributionRequest
		if err := c.ShouldBindJSON(&req); err != nil || req.StartDate == "" || req.EndDate == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "start_date and end_date fields required (YYYYMMDD)"})
			return
		}

		startDate, err := time.Parse("20060102", req.StartDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start_date format, use YYYYMMDD"})
			return
		}
		endDate, err := time.Parse("20060102", req.EndDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end_date format, use YYYYMMDD"})
			return
		}

		tradingDays, err := fa.GetTradingDaysForRange(ctx, startDate, endDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		var computed, failed int
		for _, day := range tradingDays {
			if err := fa.ComputeFactorReturns(ctx, factorName, day); err != nil {
				failed++
				logging.Logger.Warn().Err(err).Time("date", day).Str("factor", string(factorName)).Msg("Factor attribution failed for date")
				continue
			}
			computed++
		}

		c.JSON(http.StatusOK, gin.H{
			"message":     "attribution computed",
			"factor_name": factorName,
			"computed":    computed,
			"failed":      failed,
		})
	}
}

type syncICRequest struct {
	StartDate   string `json:"start_date"`   // YYYYMMDD
	EndDate     string `json:"end_date"`     // YYYYMMDD
	ForwardDays int    `json:"forward_days"` // default 20
}

// syncFactorICHandler computes IC for a factor over a date range.
// POST /api/sync/factor-ic/:factor_name
func syncFactorICHandler(fa *data.FactorAttributor) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		factorName := domain.FactorType(c.Param("factor_name"))

		var req syncICRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if req.StartDate == "" || req.EndDate == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "start_date and end_date fields required (YYYYMMDD)"})
			return
		}
		if req.ForwardDays <= 0 {
			req.ForwardDays = 20
		}

		startDate, err := time.Parse("20060102", req.StartDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start_date format, use YYYYMMDD"})
			return
		}
		endDate, err := time.Parse("20060102", req.EndDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end_date format, use YYYYMMDD"})
			return
		}

		tradingDays, err := fa.GetTradingDaysForRange(ctx, startDate, endDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		var computed, failed int
		for _, day := range tradingDays {
			if _, err := fa.ComputeIC(ctx, factorName, day, req.ForwardDays); err != nil {
				failed++
				logging.Logger.Warn().Err(err).Time("date", day).Str("factor", string(factorName)).Msg("IC computation failed for date")
				continue
			}
			computed++
		}

		c.JSON(http.StatusOK, gin.H{
			"message":     "IC computed",
			"factor_name": factorName,
			"computed":    computed,
			"failed":      failed,
		})
	}
}
