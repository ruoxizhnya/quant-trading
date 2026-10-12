package main

// MacroHandler serves the /api/macro/* read-only endpoints (P2-1,
// TASKS.md §五 leftover ruling — the domestic macro indicator layer).
//
// Endpoints (all GET, no RBAC — same public read posture as the Evidence
// and Industry APIs):
//
//	GET /api/macro/series?indicator=CPI_YOY&freq=M&start_date=&end_date=&limit=
//
// Contract notes (identical posture to IndustryHandler):
//   - Empty result is a first-class 200 with an empty array — "no rows" is
//     an answer, not a sentinel error.
//   - Results are newest-first (obs_date DESC) and capped at `limit`
//     (default 1000 / max 5000; R4 of P2-1-R2). When the cap is hit the
//     response carries truncated=true + a hint — we do NOT silently fetch
//     one extra row to sniff whether more data exists (OPTION-A: report
//     "may be truncated", let the caller narrow start_date/end_date).
//   - Bad parameters fail fast with 400 before any store access (parse, then
//     validate, then query) — a malformed date never reaches the database.
//   - The store may be a zero-value *storage.PostgresStore (nil pool) when
//     the service is not wired to a database; routes still register and
//     requests answer 503 (deps_test.newMinimalDeps constraint).
//   - ⚠️ deps.Store is an existing ServerDeps field — this handler adds no
//     new ServerDeps field (deps_test.go pins the field count).
//
// Design follows the "pattern B" handler style of handlers_evidence.go /
// handlers_industry.go: struct + functional options + New + RegisterRoutes.

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"github.com/ruoxizhnya/quant-trading/pkg/httpserver"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// MacroHandler serves the /api/macro/* endpoints.
type MacroHandler struct {
	store  *storage.PostgresStore
	logger zerolog.Logger
}

// MacroHandlerOption configures a MacroHandler.
type MacroHandlerOption func(*MacroHandler)

// WithMacroLogger overrides the default logger.
func WithMacroLogger(l zerolog.Logger) MacroHandlerOption {
	return func(h *MacroHandler) { h.logger = l }
}

// NewMacroHandler constructs a MacroHandler backed by store. Panics if
// store is nil — wiring bug, fail loud at startup (same contract as
// NewIndustryHandler). A non-nil store with a nil pool is tolerated:
// requests answer 503 instead of panicking.
func NewMacroHandler(store *storage.PostgresStore, logger zerolog.Logger, opts ...MacroHandlerOption) *MacroHandler {
	if store == nil {
		panic("analysis: NewMacroHandler called with nil PostgresStore")
	}
	h := &MacroHandler{
		store:  store,
		logger: logger.With().Str("component", "macro-handler").Logger(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// RegisterRoutes mounts the /api/macro/* endpoints on the router.
func (h *MacroHandler) RegisterRoutes(router *gin.Engine) {
	api := router.Group("/api")
	api.GET("/macro/series", h.handleSeries)
}

// storeReady reports whether the backing store can serve queries. A
// zero-value store (nil pool) means the service started without a
// database — 503, not a nil-dereference panic at request time.
func (h *MacroHandler) storeReady() bool {
	return h.store != nil && h.store.DB() != nil
}

// macroResponse is the shared envelope: total + data + (limit/truncated/hint
// since P2-1-R2 R4). total counts the rows actually returned (capped at
// limit). Consistent with the industry endpoints' {total, ...} shape.
type macroResponse struct {
	Total     int    `json:"total"`
	Data      any    `json:"data"`
	Limit     int    `json:"limit"`
	Truncated bool   `json:"truncated,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// macroOK 写 200 响应。nil 切片归一为 []：JSON null 会让严格客户端
// （TS 的 rows.length / rows.map）直接炸——空数组才是「空结果」的正确形状，
// null 是「出错」的形状（同 industryOK）。
//
// R4：len(rows)==limit 时标记 truncated + hint——不隐式多取 1 行嗅探
// 「是否真的还有更多」（OPTION-A），把「达到上限」如实上报为「可能被
// 截断」，由调用方决定是否用日期区间下钻。
func macroOK[T any](c *gin.Context, rows []T, limit int) {
	if rows == nil {
		rows = []T{}
	}
	resp := macroResponse{Total: len(rows), Data: rows, Limit: limit}
	if len(rows) == limit {
		resp.Truncated = true
		resp.Hint = fmt.Sprintf("结果达到 limit=%d，可能被截断；用 start_date/end_date 缩小范围获取更旧的数据", limit)
	}
	c.JSON(http.StatusOK, resp)
}

// handleSeries serves GET /api/macro/series?indicator=&freq=&start_date=&end_date=&limit=.
//
// indicator is required (catalog: CPI_YOY / PPI_YOY / PMI / M2 / M2_YOY /
// TSF_INC / LPR_1Y / LPR_5Y / USDCNY_CLOSE — see pkg/data/macro.go
// macroIndicatorSpecs). freq is optional (M/D; omit to accept both).
// start_date / end_date are inclusive YYYY-MM-DD bounds (parsed by
// parseDateQuery, shared with handlers_industry.go). limit is optional
// (1..5000, default 1000) and is validated before any store access —
// values outside the range (including 0 and non-numeric) are a 400.
func (h *MacroHandler) handleSeries(c *gin.Context) {
	if !h.storeReady() {
		httpserver.Fail(c, http.StatusServiceUnavailable, "macro data layer unavailable")
		return
	}

	indicator := c.Query("indicator")
	if indicator == "" {
		httpserver.Fail(c, http.StatusBadRequest, "indicator 必填（指标目录见 pkg/data/macro.go macroIndicatorSpecs）")
		return
	}

	f := storage.MacroSeriesFilter{
		Indicator: indicator,
		Freq:      c.Query("freq"),
	}
	limit := storage.MacroSeriesDefaultLimit
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > storage.MacroSeriesMaxLimit {
			httpserver.Fail(c, http.StatusBadRequest, fmt.Sprintf(
				"limit 必须是 1..%d 的整数（默认 %d）",
				storage.MacroSeriesMaxLimit, storage.MacroSeriesDefaultLimit))
			return
		}
		limit = n
	}
	f.Limit = limit
	start, ok := parseDateQuery(c, "start_date")
	if !ok {
		return
	}
	f.StartDate = start
	end, ok := parseDateQuery(c, "end_date")
	if !ok {
		return
	}
	f.EndDate = end
	if f.StartDate != nil && f.EndDate != nil && f.StartDate.After(*f.EndDate) {
		httpserver.Fail(c, http.StatusBadRequest, "start_date 晚于 end_date")
		return
	}

	rows, err := h.store.ListMacroSeries(c.Request.Context(), f)
	if err != nil {
		h.logger.Error().Err(err).Str("indicator", indicator).Msg("failed to list macro series")
		httpserver.Fail(c, http.StatusInternalServerError, "failed to list macro series")
		return
	}
	macroOK(c, rows, f.Limit)
}
