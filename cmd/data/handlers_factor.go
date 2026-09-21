package main

import (
	"github.com/ruoxizhnya/quant-trading/internal/httpserver"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// citationTuple is the ADR-022 §5 evidence coordinate
// {source, dataset, key, as_of, content_hash}. A hash with no ingest.raw
// record keeps only content_hash: the absence of the other four fields is the
// "response not archived" signal — never padded with invented values — the
// same first-class semantics as GET /api/evidence/{hash}'s 404 (ODR-061 §4).
type citationTuple struct {
	Source      string     `json:"source,omitempty"`
	Dataset     string     `json:"dataset,omitempty"`
	Key         string     `json:"key,omitempty"`
	AsOf        *time.Time `json:"as_of,omitempty"`
	ContentHash string     `json:"content_hash"`
}

// factorCacheResponse is the wire form of a factor_cache read: the entry with
// its stored citation (hash-only) replaced by the expanded five-tuples. The
// outer Citation field shadows the embedded entry's raw form.
type factorCacheResponse struct {
	domain.FactorCacheEntry
	Citation []citationTuple `json:"citation"`
}

// expandCitation resolves a stored citation ([{"content_hash":...}]) into
// ADR-022 §5 five-tuples by looking each hash up in ingest.raw. ok=false means
// the stored citation is not parseable as the hash-array form; the caller must
// degrade instead of failing the request — a citation defect must never turn a
// factor read into a 5xx (ODR-061 §4).
func expandCitation(ctx context.Context, store *storage.PostgresStore, citation json.RawMessage) ([]citationTuple, bool) {
	var refs []struct {
		ContentHash string `json:"content_hash"`
	}
	if err := json.Unmarshal(citation, &refs); err != nil {
		return nil, false
	}
	tuples := make([]citationTuple, 0, len(refs))
	for _, ref := range refs {
		tuple := citationTuple{ContentHash: ref.ContentHash}
		raw, err := store.GetRawIngest(ctx, ref.ContentHash)
		if err != nil {
			// Enrichment is best-effort: the hash coordinate itself remains
			// truthful, so degrade to hash-only instead of failing the read.
			logging.Logger.Warn().Err(err).
				Str("content_hash", ref.ContentHash).
				Msg("citation expansion failed; emitting hash-only tuple")
		} else if raw != nil {
			tuple.Source = raw.Source
			tuple.Dataset = raw.Dataset
			tuple.Key = raw.Key
			tuple.AsOf = raw.AsOf
		}
		tuples = append(tuples, tuple)
	}
	return tuples, true
}

func getFactorHandler(store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		factorName := domain.FactorType(c.Param("factor_name"))
		symbol := c.Query("symbol")
		dateStr := c.Query("date")

		if symbol == "" {
			httpserver.Fail(c, http.StatusBadRequest, "symbol query param is required")
			return
		}
		if dateStr == "" {
			httpserver.Fail(c, http.StatusBadRequest, "date query param is required (YYYYMMDD)")
			return
		}

		date, err := time.Parse("20060102", dateStr)
		if err != nil {
			httpserver.Fail(c, http.StatusBadRequest, "invalid date format, use YYYYMMDD")
			return
		}

		entry, err := store.GetFactorCache(ctx, symbol, date, factorName)
		if err != nil {
			httpserver.Error(c, http.StatusInternalServerError, err)
			return
		}
		if entry == nil {
			httpserver.Fail(c, http.StatusNotFound, "factor cache entry not found")
			return
		}

		tuples, ok := expandCitation(ctx, store, entry.Citation)
		if !ok {
			// Stored citation exists but is not the hash-array form: pass the
			// entry through unchanged rather than failing the read.
			c.JSON(http.StatusOK, entry)
			return
		}
		c.JSON(http.StatusOK, factorCacheResponse{FactorCacheEntry: *entry, Citation: tuples})
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
			httpserver.Fail(c, http.StatusBadRequest, "date field required (YYYYMMDD)")
			return
		}

		date, err := time.Parse("20060102", req.Date)
		if err != nil {
			httpserver.Fail(c, http.StatusBadRequest, "invalid date format, use YYYYMMDD")
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
		case domain.FactorGrossMarginTrend:
			computeErr = fc.ComputeGrossMarginTrendFactor(ctx, date)
		case domain.FactorContractLiabilityRatio:
			computeErr = fc.ComputeContractLiabilityRatioFactor(ctx, date)
		case domain.FactorOCFToNetProfit:
			computeErr = fc.ComputeOCFToNetProfitFactor(ctx, date)
		case domain.FactorROEDuPontLeverage:
			computeErr = fc.ComputeROEDuPontLeverageFactor(ctx, date)
		case domain.FactorInventoryTurnoverDelta:
			computeErr = fc.ComputeInventoryTurnoverDeltaFactor(ctx, date)
		default:
			httpserver.Failf(c, http.StatusBadRequest, "unsupported factor: %s", factorName)
			return
		}

		if computeErr != nil {
			httpserver.Error(c, http.StatusInternalServerError, computeErr)
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
			httpserver.Fail(c, http.StatusBadRequest, "date field required (YYYYMMDD)")
			return
		}

		date, err := time.Parse("20060102", req.Date)
		if err != nil {
			httpserver.Fail(c, http.StatusBadRequest, "invalid date format, use YYYYMMDD")
			return
		}

		report, err := fc.ComputeAllFactors(ctx, date, 20, true)
		if err != nil {
			httpserver.Error(c, http.StatusInternalServerError, err)
			return
		}

		// AUD-15: the response names what was computed and what was skipped.
		// Returning a flat "all factors computed and cached" while five of them
		// produced nothing is how an empty fundamentals_detail went unnoticed:
		// the count matters as much as the error.
		skipped := make([]gin.H, 0, len(report.Skipped))
		for _, s := range report.Skipped {
			skipped = append(skipped, gin.H{"factor": s.Name, "reason": s.Reason.Error()})
		}
		message := "all factors computed and cached"
		if len(skipped) > 0 {
			message = fmt.Sprintf("%d of %d factors computed, %d skipped",
				len(report.Computed), len(report.Computed)+len(skipped), len(skipped))
		}

		c.JSON(http.StatusOK, gin.H{
			"message":  message,
			"date":     req.Date,
			"computed": report.Computed,
			"skipped":  skipped,
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
			httpserver.Fail(c, http.StatusBadRequest, "start_date and end_date query params required (YYYYMMDD)")
			return
		}

		startDate, err := time.Parse("20060102", startDateStr)
		if err != nil {
			httpserver.Fail(c, http.StatusBadRequest, "invalid start_date format, use YYYYMMDD")
			return
		}
		endDate, err := time.Parse("20060102", endDateStr)
		if err != nil {
			httpserver.Fail(c, http.StatusBadRequest, "invalid end_date format, use YYYYMMDD")
			return
		}

		returns, err := fa.GetFactorReturnsTimeSeries(ctx, factorName, startDate, endDate)
		if err != nil {
			httpserver.Error(c, http.StatusInternalServerError, err)
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
			httpserver.Fail(c, http.StatusBadRequest, "start_date and end_date query params required (YYYYMMDD)")
			return
		}

		startDate, err := time.Parse("20060102", startDateStr)
		if err != nil {
			httpserver.Fail(c, http.StatusBadRequest, "invalid start_date format, use YYYYMMDD")
			return
		}
		endDate, err := time.Parse("20060102", endDateStr)
		if err != nil {
			httpserver.Fail(c, http.StatusBadRequest, "invalid end_date format, use YYYYMMDD")
			return
		}

		icEntries, err := fa.GetICTimeSeries(ctx, factorName, startDate, endDate)
		if err != nil {
			httpserver.Error(c, http.StatusInternalServerError, err)
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
			httpserver.Fail(c, http.StatusBadRequest, "start_date and end_date fields required (YYYYMMDD)")
			return
		}

		startDate, err := time.Parse("20060102", req.StartDate)
		if err != nil {
			httpserver.Fail(c, http.StatusBadRequest, "invalid start_date format, use YYYYMMDD")
			return
		}
		endDate, err := time.Parse("20060102", req.EndDate)
		if err != nil {
			httpserver.Fail(c, http.StatusBadRequest, "invalid end_date format, use YYYYMMDD")
			return
		}

		tradingDays, err := fa.GetTradingDaysForRange(ctx, startDate, endDate)
		if err != nil {
			httpserver.Error(c, http.StatusInternalServerError, err)
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
			httpserver.Error(c, http.StatusBadRequest, err)
			return
		}
		if req.StartDate == "" || req.EndDate == "" {
			httpserver.Fail(c, http.StatusBadRequest, "start_date and end_date fields required (YYYYMMDD)")
			return
		}
		if req.ForwardDays <= 0 {
			req.ForwardDays = 20
		}

		startDate, err := time.Parse("20060102", req.StartDate)
		if err != nil {
			httpserver.Fail(c, http.StatusBadRequest, "invalid start_date format, use YYYYMMDD")
			return
		}
		endDate, err := time.Parse("20060102", req.EndDate)
		if err != nil {
			httpserver.Fail(c, http.StatusBadRequest, "invalid end_date format, use YYYYMMDD")
			return
		}

		tradingDays, err := fa.GetTradingDaysForRange(ctx, startDate, endDate)
		if err != nil {
			httpserver.Error(c, http.StatusInternalServerError, err)
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
