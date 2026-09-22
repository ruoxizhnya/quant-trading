package main

import (
	"context"
	"github.com/ruoxizhnya/quant-trading/internal/httpserver"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

func getOHLCVHandler(dc *data.DataCache) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		symbol := c.Param("symbol")
		startDateStr := c.Query("start_date")
		endDateStr := c.Query("end_date")

		if startDateStr == "" || endDateStr == "" {
			httpserver.Fail(c, http.StatusBadRequest, "start_date and end_date query params required (YYYYMMDD)")
			return
		}

		// Use DataCache for cache-aside access — same key format as cache warm endpoint
		ohlcv, err := dc.GetOHLCV(ctx, symbol, startDateStr, endDateStr)
		if err != nil {
			httpserver.Error(c, http.StatusInternalServerError, err)
			return
		}

		c.JSON(http.StatusOK, gin.H{"ohlcv": ohlcv})
	}
}

func bulkOHLCVHandler(dc *data.DataCache) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		var req struct {
			Symbols   []string `json:"symbols"`
			StartDate string   `json:"start_date"`
			EndDate   string   `json:"end_date"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			httpserver.Wrap(c, http.StatusBadRequest, err, "invalid request body: ")
			return
		}
		if len(req.Symbols) == 0 {
			httpserver.Fail(c, http.StatusBadRequest, "symbols is required")
			return
		}

		// Fetch all symbols in parallel via DataCache (Redis → PostgreSQL fallback)
		type result struct {
			Symbol string         `json:"symbol"`
			OHLCV  []domain.OHLCV `json:"ohlcv"`
			Error  string         `json:"error,omitempty"`
		}
		results := make([]result, len(req.Symbols))

		var wg sync.WaitGroup
		var mu sync.Mutex
		for i, symbol := range req.Symbols {
			wg.Add(1)
			go func(idx int, sym string) {
				defer wg.Done()
				ohlcv, err := dc.GetOHLCV(ctx, sym, req.StartDate, req.EndDate)
				r := result{Symbol: sym}
				if err != nil {
					r.Error = err.Error()
				} else {
					r.OHLCV = ohlcv
				}
				mu.Lock()
				results[idx] = r
				mu.Unlock()
			}(i, symbol)
		}
		wg.Wait()

		c.JSON(http.StatusOK, gin.H{"results": results})
	}
}
func warmCacheHandler(dc *data.DataCache) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req warmCacheRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpserver.Error(c, http.StatusBadRequest, err)
			return
		}
		if len(req.Symbols) == 0 {
			httpserver.Fail(c, http.StatusBadRequest, "symbols array is required")
			return
		}
		if req.StartDate == "" || req.EndDate == "" {
			httpserver.Fail(c, http.StatusBadRequest, "start_date and end_date are required (YYYYMMDD)")
			return
		}

		ctx := c.Request.Context()
		if err := dc.WarmCache(ctx, req.Symbols, req.StartDate, req.EndDate); err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				httpserver.Fail(c, http.StatusGatewayTimeout, "cache warm-up timed out")
				return
			}
			httpserver.Error(c, http.StatusInternalServerError, err)
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":    "cache warmed successfully",
			"symbols":    len(req.Symbols),
			"start_date": req.StartDate,
			"end_date":   req.EndDate,
		})
	}
}

// ---- Dividend Sync Handlers ----

type syncDividendsRequest struct {
	Symbols   []string `json:"symbols"`
	StartDate string   `json:"start_date"` // YYYYMMDD
	EndDate   string   `json:"end_date"`   // YYYYMMDD
}

// syncDividendsHandler syncs dividend data from Tushare for the given symbols (or all stocks if not specified).
// POST /sync/dividends
