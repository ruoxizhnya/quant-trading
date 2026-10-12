package main

// MacroHandler serves the /api/macro/* read-only endpoints (P2-1,
// TASKS.md §五 leftover ruling — the domestic macro indicator layer).
//
// Endpoints (all GET, no RBAC — same public read posture as the Evidence
// and Industry APIs):
//
//	GET /api/macro/series?indicator=CPI_YOY&freq=M&start_date=&end_date=
//
// Contract notes (identical posture to IndustryHandler):
//   - Empty result is a first-class 200 with an empty array — "no rows" is
//     an answer, not a sentinel error.
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
	"net/http"

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

// macroResponse is the shared envelope: total + data. Consistent with the
// industry endpoints' {total, ...} shape.
type macroResponse struct {
	Total int `json:"total"`
	Data  any `json:"data"`
}

// macroOK 写 200 响应。nil 切片归一为 []：JSON null 会让严格客户端
// （TS 的 rows.length / rows.map）直接炸——空数组才是「空结果」的正确形状，
// null 是「出错」的形状（同 industryOK）。
func macroOK[T any](c *gin.Context, rows []T) {
	if rows == nil {
		rows = []T{}
	}
	c.JSON(http.StatusOK, macroResponse{Total: len(rows), Data: rows})
}

// handleSeries serves GET /api/macro/series?indicator=&freq=&start_date=&end_date=.
//
// indicator is required (catalog: CPI_YOY / PPI_YOY / PMI / M2 / M2_YOY /
// TSF_INC / LPR_1Y / LPR_5Y / USDCNY_CLOSE — see pkg/data/macro.go
// macroIndicatorSpecs). freq is optional (M/D; omit to accept both).
// start_date / end_date are inclusive YYYY-MM-DD bounds (parsed by
// parseDateQuery, shared with handlers_industry.go).
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
	macroOK(c, rows)
}
