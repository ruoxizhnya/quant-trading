package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

func getIndexConstituentsHandler(tc *data.TushareClient, store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		indexCode := c.Param("code")
		date := c.Query("date")

		// Validate supported indexes
		if indexCode != "000300.SH" && indexCode != "000500.SH" && indexCode != "000852.SH" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported index code, supported: 000300.SH (CSI 300), 000500.SH (CSI 500), 000852.SH (CSI 800)"})
			return
		}

		constituents, err := tc.GetIndexConstituents(ctx, indexCode, date)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"index_code": indexCode, "constituents": constituents})
	}
}

// syncIndexConstituentsHandler fetches index constituents from Tushare and saves to DB.
// POST /sync/index-constituents/:index_code
func syncIndexConstituentsHandler(tc *data.TushareClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		indexCode := c.Param("index_code")

		// Validate supported indexes
		if indexCode != "000300.SH" && indexCode != "000500.SH" && indexCode != "000852.SH" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported index code, supported: 000300.SH (CSI 300), 000500.SH (CSI 500), 000852.SH (CSI 800)"})
			return
		}

		// Fetch latest constituents (no specific date = latest)
		constituents, err := tc.FetchIndexConstituents(ctx, indexCode, "")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":    "index constituents synced successfully",
			"index_code": indexCode,
			"count":      len(constituents),
		})
	}
}

type syncStocksRequest struct {
	Exchange   string `json:"exchange"`
	ListStatus string `json:"list_status"`
}

func syncStocksHandler(tc *data.TushareClient, store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		var req syncStocksRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			req.ListStatus = "L"
		}

		stocks, err := tc.FetchStocks(ctx, req.Exchange, req.ListStatus)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "stocks synced successfully",
			"count":   len(stocks),
		})
	}
}

type syncOHLCVRequest struct {
	Symbols   []string `json:"symbols"`
	StartDate string   `json:"start_date"`
	EndDate   string   `json:"end_date"`
}

func syncOHLCVHandler(tc *data.TushareClient, store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		var req syncOHLCVRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if len(req.Symbols) == 0 {
			// Fall back to all stocks from DB
			allStocks, err := store.GetAllStocks(ctx)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch stocks from DB: " + err.Error()})
				return
			}
			for _, s := range allStocks {
				req.Symbols = append(req.Symbols, s.Symbol)
			}
			logging.Logger.Info().Int("count", len(req.Symbols)).Msg("No symbols provided, fetched all from DB")
		}

		totalCount := 0
		for _, symbol := range req.Symbols {
			logging.Logger.Info().Str("symbol", symbol).Str("start", req.StartDate).Str("end", req.EndDate).Msg("fetching OHLCV")
			ohlcv, err := tc.FetchDailyOHLCV(ctx, symbol, req.StartDate, req.EndDate)
			logging.Logger.Info().Err(err).Str("symbol", symbol).Int("count", len(ohlcv)).Msg("OHLCV fetch result")
			if err != nil {
				logging.Logger.Warn().Err(err).Str("symbol", symbol).Msg("Failed to sync OHLCV")
				continue
			}
			totalCount += len(ohlcv)
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "OHLCV synced successfully",
			"count":   totalCount,
		})
	}
}

type syncAllOHLCVRequest struct {
	StartDate    string `json:"start_date"`
	EndDate      string `json:"end_date"`
	BatchSize    int    `json:"batch_size"`
	SkipExisting bool   `json:"skip_existing"`
}

// syncAllOHLCVHandler reads all stocks from DB and syncs OHLCV in batches.
// POST /sync/ohlcv/all
// Runs asynchronously — returns immediately and processes in background.
func syncAllOHLCVHandler(tc *data.TushareClient, store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req syncAllOHLCVRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Defaults
		if req.BatchSize <= 0 {
			req.BatchSize = 10
		}
		if req.EndDate == "" {
			req.EndDate = time.Now().Format("20060102")
		}
		if req.StartDate == "" {
			req.StartDate = time.Now().AddDate(-1, 0, 0).Format("20060102")
		}

		// Fetch all stocks from DB (non-blocking context)
		ctx := context.Background()
		stocks, err := store.GetAllStocks(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch stocks: " + err.Error()})
			return
		}

		if len(stocks) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no stocks found in DB. Run POST /sync/stocks first."})
			return
		}

		logging.Logger.Info().
			Int("total_stocks", len(stocks)).
			Int("batch_size", req.BatchSize).
			Str("start_date", req.StartDate).
			Str("end_date", req.EndDate).
			Bool("skip_existing", req.SkipExisting).
			Msg("Bulk OHLCV sync started in background")

		// Return immediately — process in background with independent context
		c.JSON(http.StatusAccepted, gin.H{
			"message":      "bulk OHLCV sync started",
			"total_stocks": len(stocks),
		})

		// Background processing
		go func() {
			bgCtx := context.Background()
			totalSynced := 0
			totalSkipped := 0
			totalFailed := 0

			for i := 0; i < len(stocks); i += req.BatchSize {
				end := i + req.BatchSize
				if end > len(stocks) {
					end = len(stocks)
				}
				batch := stocks[i:end]

				for _, stock := range batch {
					if req.SkipExisting {
						hasData, err := store.HasOHLCVData(bgCtx, stock.Symbol)
						if err != nil {
							logging.Logger.Warn().Err(err).Str("symbol", stock.Symbol).Msg("Error checking OHLCV data")
						}
						if hasData {
							totalSkipped++
							logging.Logger.Debug().Str("symbol", stock.Symbol).Msg("Skipping - already has data")
							continue
						}
					}

					logging.Logger.Info().Str("symbol", stock.Symbol).Str("start", req.StartDate).Str("end", req.EndDate).Msg("fetching OHLCV")
					ohlcv, err := tc.FetchDailyOHLCV(bgCtx, stock.Symbol, req.StartDate, req.EndDate)
					if err != nil {
						totalFailed++
						logging.Logger.Warn().Err(err).Str("symbol", stock.Symbol).Msg("Failed to sync OHLCV")
						continue
					}
					totalSynced += len(ohlcv)
					logging.Logger.Info().Str("symbol", stock.Symbol).Int("count", len(ohlcv)).Msg("OHLCV synced")
				}

				logging.Logger.Info().
					Int("batch", (i/req.BatchSize)+1).
					Int("progress", end).
					Int("total", len(stocks)).
					Int("synced", totalSynced).
					Int("skipped", totalSkipped).
					Int("failed", totalFailed).
					Msg("Batch complete")
			}

			logging.Logger.Info().
				Int("total_stocks", len(stocks)).
				Int("records_synced", totalSynced).
				Int("skipped", totalSkipped).
				Int("failed", totalFailed).
				Msg("Bulk OHLCV sync completed")
		}()
	}
}

// syncFundamentalRequest is the shared request shape for both the
// single-symbol (/sync/fundamental) and bulk (/sync/fundamentals)
// sync endpoints. S7-P2-5: the former duplicate syncFundamentalsRequest
// (plural) type was consolidated into this one — they were structurally
// identical.
type syncFundamentalRequest struct {
	Symbols []string `json:"symbols"`
	Date    string   `json:"date"` // YYYYMMDD — if provided, fetch for that specific date; otherwise fetch recent
}

// syncFundamentalHandler fetches fundamental data for the requested
// symbols on a single date. POST /sync/fundamental
func syncFundamentalHandler(tc *data.TushareClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		var req syncFundamentalRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if len(req.Symbols) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "symbols array is required"})
			return
		}
		if req.Date == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "date is required"})
			return
		}

		totalCount := 0
		for _, symbol := range req.Symbols {
			fundamentals, err := tc.FetchFundamentals(ctx, symbol, req.Date)
			if err != nil {
				logging.Logger.Warn().Err(err).Str("symbol", symbol).Msg("Failed to sync fundamentals")
				continue
			}
			totalCount += len(fundamentals)
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Fundamentals synced successfully",
			"count":   totalCount,
		})
	}
}

// syncFundamentalsHandler is the bulk variant: with no symbols in the
// body it walks every stock in the DB. Note that the route registered
// in main.go actually calls the method version
// (syncHandler.syncFundamentalsHandler in sync_handlers.go); this
// package-level function is retained from the pre-split codebase as
// the reference implementation.
func syncFundamentalsHandler(tc *data.TushareClient, store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		var req syncFundamentalRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			// If no body, use empty request (sync all stocks)
			req = syncFundamentalRequest{}
		}

		var symbols []string
		if len(req.Symbols) > 0 {
			symbols = req.Symbols
		} else {
			// Fetch all stocks from DB
			allStocks, err := store.GetAllStocks(ctx)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch stocks from DB: " + err.Error()})
				return
			}
			for _, s := range allStocks {
				symbols = append(symbols, s.Symbol)
			}
			logging.Logger.Info().Int("count", len(symbols)).Msg("No symbols provided, fetched all from DB")
		}

		if len(symbols) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no stocks found in DB. Run POST /sync/stocks first."})
			return
		}

		totalCount := 0
		totalSynced := 0
		totalFailed := 0

		// Process in batches of 10 to respect rate limits
		batchSize := 10
		for i := 0; i < len(symbols); i += batchSize {
			end := i + batchSize
			if end > len(symbols) {
				end = len(symbols)
			}
			batch := symbols[i:end]

			for _, symbol := range batch {
				records, err := tc.FetchFundamentalsData(ctx, symbol, req.Date, req.Date)
				if err != nil {
					totalFailed++
					logging.Logger.Warn().Err(err).Str("symbol", symbol).Msg("Failed to sync fundamentals data")
					continue
				}
				totalSynced++
				totalCount += len(records)
			}

			logging.Logger.Info().
				Int("batch", (i/batchSize)+1).
				Int("progress", end).
				Int("total", len(symbols)).
				Msg("Fundamentals batch complete")
		}

		c.JSON(http.StatusOK, gin.H{
			"message":       "Fundamentals synced successfully",
			"stocks_synced": totalSynced,
			"records_saved": totalCount,
			"failed_stocks": totalFailed,
		})
	}
}

func syncCalendarHandler(tc *data.TushareClient, store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req syncCalendarRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			// Use defaults if no body provided
			req.StartDate = time.Now().AddDate(-1, 0, 0).Format("20060102")
			req.EndDate = time.Now().Format("20060102")
		}

		if req.Exchange == "" {
			req.Exchange = "both"
		}
		if req.StartDate == "" {
			req.StartDate = time.Now().AddDate(-1, 0, 0).Format("20060102")
		}
		if req.EndDate == "" {
			req.EndDate = time.Now().Format("20060102")
		}

		if _, err := time.Parse("20060102", req.StartDate); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start_date format, use YYYYMMDD"})
			return
		}
		if _, err := time.Parse("20060102", req.EndDate); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end_date format, use YYYYMMDD"})
			return
		}

		startFormatted := fmt.Sprintf("%s-%s-%s", req.StartDate[:4], req.StartDate[4:6], req.StartDate[6:8])
		endFormatted := fmt.Sprintf("%s-%s-%s", req.EndDate[:4], req.EndDate[4:6], req.EndDate[6:8])

		ctx := c.Request.Context()

		// Determine which exchanges to sync
		exchanges := []string{req.Exchange}
		if req.Exchange == "both" {
			exchanges = []string{"SSE", "SZSE"}
		}

		var allEntries []storage.TradingCalendarEntry
		exchangeResults := make(map[string]struct {
			count    int
			trading  int
			holidays int
		})

		for _, exchange := range exchanges {
			entries, err := tc.FetchTradingCalendar(ctx, exchange, startFormatted, endFormatted)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to fetch %s calendar from Tushare: %v", exchange, err)})
				return
			}

			tradingCount := 0
			for _, e := range entries {
				if e.IsTradingDay {
					tradingCount++
				}
			}

			exchangeResults[exchange] = struct {
				count    int
				trading  int
				holidays int
			}{
				count:    len(entries),
				trading:  tradingCount,
				holidays: len(entries) - tradingCount,
			}

			allEntries = append(allEntries, entries...)
		}

		if len(allEntries) == 0 {
			c.JSON(http.StatusOK, gin.H{"message": "no calendar entries returned from Tushare", "count": 0})
			return
		}

		// Save all entries to database in one batch
		domainEntries := make([]*storage.TradingCalendarEntry, len(allEntries))
		for i := range allEntries {
			domainEntries[i] = &allEntries[i]
		}
		if err := store.SaveTradingCalendarBatch(ctx, domainEntries); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save calendar: " + err.Error()})
			return
		}

		totalCount := 0
		totalTrading := 0
		for _, r := range exchangeResults {
			totalCount += r.count
			totalTrading += r.trading
		}

		c.JSON(http.StatusOK, gin.H{
			"message":          "calendar synced successfully",
			"count":            totalCount,
			"trading_days":     totalTrading,
			"holidays":         totalCount - totalTrading,
			"start_date":       startFormatted,
			"end_date":         endFormatted,
			"exchanges_synced": exchanges,
			"by_exchange":      exchangeResults,
		})
	}
}

// warmCacheRequest is the POST body for the cache warm endpoint.
type warmCacheRequest struct {
	Symbols   []string `json:"symbols"`
	StartDate string   `json:"start_date"` // YYYYMMDD
	EndDate   string   `json:"end_date"`   // YYYYMMDD
}

// warmCacheHandler pre-fetches OHLCV data for the given stock universe into Redis.
// This is called by the backtest engine before running a backtest to ensure
// all required data is cached and the backtest loop hits Redis instead of PostgreSQL.
func syncDividendsHandler(tc *data.TushareClient, store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		var req syncDividendsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			// If no body, use empty request (sync all stocks)
			req = syncDividendsRequest{}
		}

		var symbols []string
		if len(req.Symbols) > 0 {
			symbols = req.Symbols
		} else {
			// Fetch all stocks from DB
			allStocks, err := store.GetAllStocks(ctx)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch stocks from DB: " + err.Error()})
				return
			}
			for _, s := range allStocks {
				symbols = append(symbols, s.Symbol)
			}
			logging.Logger.Info().Int("count", len(symbols)).Msg("No symbols provided, fetched all from DB for dividend sync")
		}

		if len(symbols) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no stocks found in DB. Run POST /sync/stocks first."})
			return
		}

		totalRecords := 0
		totalSynced := 0
		totalFailed := 0

		// Process in batches of 10 to respect Tushare rate limits (~200 req/min)
		batchSize := 10
		for i := 0; i < len(symbols); i += batchSize {
			end := i + batchSize
			if end > len(symbols) {
				end = len(symbols)
			}
			batch := symbols[i:end]

			for _, symbol := range batch {
				records, err := tc.FetchDividends(ctx, symbol, req.StartDate, req.EndDate)
				if err != nil {
					totalFailed++
					logging.Logger.Warn().Err(err).Str("symbol", symbol).Msg("Failed to sync dividends")
					continue
				}
				totalSynced++
				totalRecords += len(records)
			}

			logging.Logger.Info().
				Int("batch", (i/batchSize)+1).
				Int("progress", end).
				Int("total", len(symbols)).
				Msg("Dividend sync batch complete")
		}

		c.JSON(http.StatusOK, gin.H{
			"message":       "Dividends synced successfully",
			"stocks_synced": totalSynced,
			"records_saved": totalRecords,
			"failed_stocks": totalFailed,
		})
	}
}

// ---- Split Sync Handlers ----

type syncSplitsRequest struct {
	Symbols   []string `json:"symbols"`
	StartDate string   `json:"start_date"` // YYYYMMDD
	EndDate   string   `json:"end_date"`   // YYYYMMDD
}

// syncSplitsHandler syncs split/rights-issue data from Tushare for the given symbols (or all stocks if not specified).
// POST /sync/splits
func syncSplitsHandler(tc *data.TushareClient, store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		var req syncSplitsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			req = syncSplitsRequest{}
		}

		var symbols []string
		if len(req.Symbols) > 0 {
			symbols = req.Symbols
		} else {
			allStocks, err := store.GetAllStocks(ctx)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch stocks from DB: " + err.Error()})
				return
			}
			for _, s := range allStocks {
				symbols = append(symbols, s.Symbol)
			}
			logging.Logger.Info().Int("count", len(symbols)).Msg("No symbols provided, fetched all from DB for split sync")
		}

		if len(symbols) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no stocks found in DB. Run POST /sync/stocks first."})
			return
		}

		totalRecords := 0
		totalSynced := 0
		totalFailed := 0

		batchSize := 10
		for i := 0; i < len(symbols); i += batchSize {
			end := i + batchSize
			if end > len(symbols) {
				end = len(symbols)
			}
			batch := symbols[i:end]

			for _, symbol := range batch {
				records, err := tc.FetchSplits(ctx, symbol, req.StartDate, req.EndDate)
				if err != nil {
					totalFailed++
					logging.Logger.Warn().Err(err).Str("symbol", symbol).Msg("Failed to sync splits")
					continue
				}
				totalSynced++
				totalRecords += len(records)
			}

			logging.Logger.Info().
				Int("batch", (i/batchSize)+1).
				Int("progress", end).
				Int("total", len(symbols)).
				Msg("Split sync batch complete")
		}

		c.JSON(http.StatusOK, gin.H{
			"message":       "Splits synced successfully",
			"stocks_synced": totalSynced,
			"records_saved": totalRecords,
			"failed_stocks": totalFailed,
		})
	}
}

// ---- Factor Cache Handlers ----

// getFactorHandler retrieves a factor z-score from the cache.
// GET /factors/:factor_name?symbol=...&date=...
