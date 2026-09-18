package main

import (
	"github.com/ruoxizhnya/quant-trading/internal/httpserver"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

func healthHandler(store *storage.PostgresStore, cache storage.Cache) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		if err := store.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy", "error": "database"})
			return
		}
		if err := cache.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy", "error": "cache"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	}
}

func listStocksHandler(store *storage.PostgresStore, cache storage.Cache) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		exchange := c.Query("exchange")

		// Check cache first
		if cached, err := cache.GetCachedStocks(ctx, exchange); err == nil && cached != nil {
			c.JSON(http.StatusOK, gin.H{"stocks": cached, "source": "cache"})
			return
		}

		stocks, err := store.GetStocks(ctx, exchange)
		if err != nil {
			httpserver.Error(c, http.StatusInternalServerError, err)
			return
		}

		// Cache the result
		if len(stocks) > 0 {
			cache.CacheStocks(ctx, exchange, stocks)
		}

		c.JSON(http.StatusOK, gin.H{"stocks": stocks, "source": "database"})
	}
}

func getStockHandler(store *storage.PostgresStore, cache storage.Cache) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		symbol := c.Param("symbol")

		// Check cache first
		if cached, err := cache.GetCachedStock(ctx, symbol); err == nil && cached != nil {
			c.JSON(http.StatusOK, gin.H{"stock": cached, "source": "cache"})
			return
		}

		stock, err := store.GetStock(ctx, symbol)
		if err != nil {
			httpserver.Error(c, http.StatusInternalServerError, err)
			return
		}
		if stock == nil {
			httpserver.Fail(c, http.StatusNotFound, "stock not found")
			return
		}

		// Cache the result
		cache.CacheStock(ctx, stock)

		c.JSON(http.StatusOK, gin.H{"stock": stock, "source": "database"})
	}
}

func stocksCountHandler(store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		var count int
		err := store.DB().QueryRow(ctx, "SELECT COUNT(*) FROM stocks").Scan(&count)
		if err != nil {
			httpserver.Wrap(c, http.StatusInternalServerError, err, "failed to count stocks: ")
			return
		}

		var latestDate *string
		store.DB().QueryRow(ctx, "SELECT MAX(trade_date) FROM ohlcv_daily_qfq").Scan(&latestDate)

		resp := gin.H{"count": count}
		if latestDate != nil {
			resp["latest_date"] = *latestDate
		}
		c.JSON(http.StatusOK, resp)
	}
}

func marketIndexHandler(store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		// Return available index data from database.
		// sh300=000300.SH, sse=000001.SH, cyb=399006.SZ
		type indexData struct {
			Code   string  `json:"code"`
			Name   string  `json:"name"`
			Close  float64 `json:"close"`
			Change float64 `json:"change"`
			Pct    float64 `json:"pct"`
		}

		// Try to get latest close for each index from ohlcv data
		indices := []string{"000300.SH", "000001.SH", "399006.SZ"}
		codeToName := map[string]string{
			"000300.SH": "沪深300",
			"000001.SH": "上证指数",
			"399006.SZ": "创业板指",
		}

		result := make([]indexData, 0, len(indices))
		for _, code := range indices {
			var close *float64
			var change, pct float64
			row := store.DB().QueryRow(ctx,
				`SELECT close FROM ohlcv_daily_qfq WHERE symbol=$1 ORDER BY trade_date DESC LIMIT 1`, code)
			if err := row.Scan(&close); err == nil && close != nil {
				// Get previous close for change/pct
				var prevClose *float64
				store.DB().QueryRow(ctx,
					`SELECT close FROM ohlcv_daily_qfq WHERE symbol=$1 ORDER BY trade_date DESC LIMIT 1 OFFSET 1`, code).Scan(&prevClose)
				if prevClose != nil && *prevClose > 0 {
					change = *close - *prevClose
					pct = (change / *prevClose) * 100
				}
				result = append(result, indexData{
					Code:   code,
					Name:   codeToName[code],
					Close:  *close,
					Change: change,
					Pct:    pct,
				})
			}
		}

		c.JSON(http.StatusOK, gin.H{"indices": result})
	}
}
func screenStocksHandler(store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		var req domain.ScreenRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpserver.Error(c, http.StatusBadRequest, err)
			return
		}

		var date *time.Time
		if req.Date != "" {
			t, err := time.Parse("20060102", req.Date)
			if err != nil {
				httpserver.Fail(c, http.StatusBadRequest, "invalid date format, use YYYYMMDD")
				return
			}
			date = &t
		}

		limit := req.Limit
		if limit <= 0 {
			limit = 100
		}

		results, err := store.ScreenFundamentals(ctx, req.Filters, date, limit)
		if err != nil {
			httpserver.Error(c, http.StatusInternalServerError, err)
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"count":   len(results),
			"results": results,
		})
	}
}

// ---- End Fundamental Data Handlers ----

func getTradingCalendarHandler(store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		startStr := c.Query("start")
		endStr := c.Query("end")

		if startStr == "" || endStr == "" {
			httpserver.Fail(c, http.StatusBadRequest, "start and end query params required")
			return
		}

		startDate, err := time.Parse("2006-01-02", startStr)
		if err != nil {
			httpserver.Fail(c, http.StatusBadRequest, "invalid start date format, use YYYY-MM-DD")
			return
		}
		endDate, err := time.Parse("2006-01-02", endStr)
		if err != nil {
			httpserver.Fail(c, http.StatusBadRequest, "invalid end date format, use YYYY-MM-DD")
			return
		}

		days, err := store.GetTradingDates(ctx, startDate, endDate)
		if err != nil {
			httpserver.Error(c, http.StatusInternalServerError, err)
			return
		}

		dayStrs := make([]string, len(days))
		for i, d := range days {
			dayStrs[i] = d.Format("2006-01-02")
		}
		c.JSON(http.StatusOK, gin.H{"trading_days": dayStrs})
	}
}

type syncCalendarRequest struct {
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Exchange  string `json:"exchange"` // "SSE", "SZSE", or "both" (default: "both")
}
