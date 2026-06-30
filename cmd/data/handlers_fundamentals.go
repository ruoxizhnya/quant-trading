package main

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// Read-only fundamental data handlers. The matching sync handlers
// (syncFundamentalHandler / syncFundamentalsHandler) live in
// handlers_sync.go alongside the rest of the /sync/* surface.

func getFundamentalHandler(store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		symbol := c.Param("symbol")
		dateStr := c.Query("date")

		if dateStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "date query parameter required"})
			return
		}

		date, err := time.Parse("20060102", dateStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, use YYYYMMDD"})
			return
		}

		fundamental, err := store.GetFundamental(ctx, symbol, date)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if fundamental == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "fundamental data not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"fundamental": fundamental})
	}
}

// ---- Fundamental Data Handlers (stock_fundamentals) ----

func getFundamentalsHandler(store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		symbol := c.Param("symbol")

		fundamental, err := store.GetFundamentalDataLatest(ctx, symbol)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if fundamental == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "fundamental data not found for symbol"})
			return
		}

		c.JSON(http.StatusOK, fundamental)
	}
}

func getFundamentalsHistoryHandler(store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		symbol := c.Param("symbol")
		startStr := c.Query("start_date")
		endStr := c.Query("end_date")

		var startDate, endDate *time.Time
		if startStr != "" {
			t, err := time.Parse("20060102", startStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start_date format, use YYYYMMDD"})
				return
			}
			startDate = &t
		}
		if endStr != "" {
			t, err := time.Parse("20060102", endStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end_date format, use YYYYMMDD"})
				return
			}
			endDate = &t
		}

		history, err := store.GetFundamentalDataHistory(ctx, symbol, startDate, endDate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"history": history})
	}
}
