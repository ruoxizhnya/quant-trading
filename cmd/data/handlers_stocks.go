package main

import (
	"github.com/ruoxizhnya/quant-trading/pkg/httpserver"
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

		byStatus, err := store.StockStatusCounts(ctx)
		if err != nil {
			httpserver.Wrap(c, http.StatusInternalServerError, err, "failed to count stocks: ")
			return
		}

		// DATA-03：这个读数原来在此处就地写 SQL，把 date 扫进 `*string`
		// （pgx 收到的目标是 `**string`）并**丢弃** Scan 的 error —— 查询本身成功、
		// 值也回来了，是类型映射不进去，于是 `latest_date` 在任何库里都没出现过，
		// 而 SPEC 与前端类型都声明了它。现在：读失败 ⇒ 500；库里没有行情 ⇒ 显式 null。
		latest, err := store.LatestQuoteDate(ctx)
		if err != nil {
			httpserver.Wrap(c, http.StatusInternalServerError, err, "failed to read latest quote date: ")
			return
		}
		var latestDate *string
		if latest != nil {
			formatted := latest.Format("2006-01-02")
			latestDate = &formatted
		}

		c.JSON(http.StatusOK, stockCountPayload(byStatus, latestDate))
	}
}

// stockCountPayload 把 GROUP BY status 的分组计数摊成 /stocks/count 的响应体（DATA-02），
// 并带上最新行情日期（DATA-03）。
//
// count 由各档**求和**导出，不是第二次 COUNT(*)：同一趟查询、同一个真相，顺带消掉
// 「两次查询之间被并发同步改动，于是各档加起来不等于总数」这个窗口 —— 一个写着
// "含已退市"的总数和一份对不上的拆分，比少一个字段更糟。
//
// nil 折成空 map：Go 里 nil map 序列化成 `null`，而前端按对象读，`null` 会被当成
// "这个字段不存在"而不是"票池是空的"。
//
// latestDate 为 nil 时序列化成**显式 null**，而不是把键删掉：契约声明了这个字段，
// 「库里还没有行情」就该是一个能被当场质问的读数，不是缺席 —— 缺席把「没实现」
// 「读失败」「表是空的」三种情形压成同一个形状，正是 DATA-03 的缺陷。
func stockCountPayload(byStatus map[string]int64, latestDate *string) gin.H {
	out := byStatus
	if out == nil {
		out = map[string]int64{}
	}
	total := int64(0)
	for _, n := range out {
		total += n
	}
	return gin.H{"count": total, "by_status": out, "latest_date": latestDate}
}

// marketIndexCodes 与中文名是「市场概览」的读数面（DATA-07）。
// 名字由 core 给，前端不再各自维护一份映射（同一概念两处真相）。
var (
	marketIndexCodes = []string{"000300.SH", "000001.SH", "399006.SZ"}
	marketIndexNames = map[string]string{
		"000300.SH": "沪深300",
		"000001.SH": "上证指数",
		"399006.SZ": "创业板指",
	}
)

func marketIndexHandler(store *storage.PostgresStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		// DATA-07：原形状是每个代码两趟查询（最新收盘 + 上一期收盘），且两趟的
		// error 都被丢进 `if err == nil` 的分支里 —— 读失败与「这个指数没数据」
		// 同样表现为「这一条不出现」。现在一趟取回，读失败就是 500。
		//
		// 库里这三个代码当前 0 行（工单 ② 实测，且全库没有指数日线表），
		// 所以响应是诚实的 `{"indices":[]}`；补指数日线需要新数据源与建表，
		// 归另立的工单，不在本单夹带。
		bars, err := store.LatestIndexBars(ctx, marketIndexCodes)
		if err != nil {
			httpserver.Wrap(c, http.StatusInternalServerError, err, "failed to read index bars: ")
			return
		}

		byCode := make(map[string]storage.MarketIndexBar, len(bars))
		for _, bar := range bars {
			byCode[bar.Code] = bar
		}
		ordered := make([]storage.MarketIndexBar, 0, len(bars))
		for _, code := range marketIndexCodes {
			if bar, ok := byCode[code]; ok {
				ordered = append(ordered, bar)
			}
		}

		c.JSON(http.StatusOK, marketIndexPayload(ordered))
	}
}

// marketIndexPayload 把库侧读数摊成 /market/index 的响应（DATA-07）。
//
// 单位一律原样透出：volume 是股（DATA-04）、amount 是 CNY（DATA-05），
// 换算（亿 / 万）是展示层的事，在这里除一次就有了第二处真相。
//
// change/pct 在没有上一期收盘时是 null 而不是 0 —— 把「无从算起」显示成
// 「+0.00%」是假数字；amount 在 turnover 为 NULL 时同理。
//
// pct 是**百分数刻度**（1.23 表示 +1.23%），沿用该字段改动前的语义，
// 展示端不要再乘一次 100。
func marketIndexPayload(bars []storage.MarketIndexBar) gin.H {
	entries := make([]gin.H, 0, len(bars))
	for _, bar := range bars {
		var change, pct *float64
		if bar.PrevClose != nil && *bar.PrevClose > 0 {
			delta := bar.Close - *bar.PrevClose
			change = &delta
			rate := delta / *bar.PrevClose * 100
			pct = &rate
		}
		entries = append(entries, gin.H{
			"code":   bar.Code,
			"name":   marketIndexNames[bar.Code],
			"close":  bar.Close,
			"change": change,
			"pct":    pct,
			"volume": bar.Volume,
			"amount": bar.Turnover,
		})
	}
	return gin.H{"indices": entries}
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
