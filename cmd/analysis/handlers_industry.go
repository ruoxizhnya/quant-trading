package main

// IndustryHandler serves the /api/industry/* read-only endpoints (P2-2,
// TASKS.md §五 leftover ruling — the industry-chain static data layer).
//
// Endpoints (all GET, no RBAC — this is a public read door over a static
// dataset, same posture as the Evidence API):
//
//	GET /api/industry/tree                      申万行业树（level / parent_code 可选）
//	GET /api/industry/edges                     上下游/环节映射（upstream/downstream/relation 至少一个）
//	GET /api/industry/vitality                  行业景气指标（industry_code 必填）
//	GET /api/industry/etf/:etf_code/constituents  ETF 成分（obs_date 可选，默认最新报告期）
//
// Contract notes:
//   - Empty result is a first-class 200 with an empty array — "no rows" is
//     an answer, not a sentinel error.
//   - Bad parameters fail fast with 400 before any store access (parse, then
//     validate, then query) — a malformed level never reaches the database.
//   - The store may be a zero-value *storage.PostgresStore (nil pool) when
//     the service is not wired to a database; routes still register and
//     requests answer 503 (deps_test.newMinimalDeps constraint), matching
//     the SignalsHandler posture.
//
// Design follows the "pattern B" handler style of handlers_evidence.go /
// handlers_signals.go: struct + functional options + New + RegisterRoutes.

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"github.com/ruoxizhnya/quant-trading/pkg/httpserver"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// IndustryHandler serves the /api/industry/* endpoints.
type IndustryHandler struct {
	store  *storage.PostgresStore
	logger zerolog.Logger
}

// IndustryHandlerOption configures an IndustryHandler.
type IndustryHandlerOption func(*IndustryHandler)

// WithIndustryLogger overrides the default logger.
func WithIndustryLogger(l zerolog.Logger) IndustryHandlerOption {
	return func(h *IndustryHandler) { h.logger = l }
}

// NewIndustryHandler constructs an IndustryHandler backed by store. Panics if
// store is nil — wiring bug, fail loud at startup (same contract as
// NewEvidenceHandler). A non-nil store with a nil pool is tolerated: requests
// answer 503 instead of panicking (see RegisterRoutes callers).
func NewIndustryHandler(store *storage.PostgresStore, logger zerolog.Logger, opts ...IndustryHandlerOption) *IndustryHandler {
	if store == nil {
		panic("analysis: NewIndustryHandler called with nil PostgresStore")
	}
	h := &IndustryHandler{
		store:  store,
		logger: logger.With().Str("component", "industry-handler").Logger(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// RegisterRoutes mounts the /api/industry/* endpoints on the router.
func (h *IndustryHandler) RegisterRoutes(router *gin.Engine) {
	api := router.Group("/api")
	api.GET("/industry/tree", h.handleTree)
	api.GET("/industry/edges", h.handleEdges)
	api.GET("/industry/vitality", h.handleVitality)
	api.GET("/industry/etf/:etf_code/constituents", h.handleETFConstituents)
}

// storeReady reports whether the backing store can serve queries. A zero-value
// store (nil pool) means the service started without a database — 503, not a
// nil-dereference panic at request time.
func (h *IndustryHandler) storeReady() bool {
	return h.store != nil && h.store.DB() != nil
}

// industryResponse is the shared envelope: total + data. Consistent with the
// factor endpoints' {total, ...} shape.
type industryResponse struct {
	Total int `json:"total"`
	Data  any `json:"data"`
}

// industryOK 写 200 响应。nil 切片归一为 []：JSON null 会让严格客户端
// （TS 的 rows.length / rows.map）直接炸——空数组才是「空结果」的正确形状，
// null 是「出错」的形状。
func industryOK[T any](c *gin.Context, rows []T) {
	if rows == nil {
		rows = []T{}
	}
	c.JSON(http.StatusOK, industryResponse{Total: len(rows), Data: rows})
}

// handleTree serves GET /api/industry/tree?level=&parent_code=.
func (h *IndustryHandler) handleTree(c *gin.Context) {
	if !h.storeReady() {
		httpserver.Fail(c, http.StatusServiceUnavailable, "industry data layer unavailable")
		return
	}

	var f storage.IndustryTreeFilter
	if v := c.Query("level"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			httpserver.Fail(c, http.StatusBadRequest, "level 必须为 >= 1 的整数（申万层级 1/2/3）")
			return
		}
		f.Level = &n
	}
	if v, ok := c.GetQuery("parent_code"); ok {
		f.ParentCode = &v // 含空串：显式查一级行业（parent_code 哨兵为 ''）
	}

	nodes, err := h.store.ListIndustryTree(c.Request.Context(), f)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to list industry tree")
		httpserver.Fail(c, http.StatusInternalServerError, "failed to list industry tree")
		return
	}
	industryOK(c, nodes)
}

// handleEdges serves GET /api/industry/edges?upstream=&downstream=&relation=.
// At least one filter is required — the edge table is a graph whose size grows
// with the mapping, so unfiltered scans are rejected at the door (the store
// enforces the same rule for non-HTTP callers).
func (h *IndustryHandler) handleEdges(c *gin.Context) {
	if !h.storeReady() {
		httpserver.Fail(c, http.StatusServiceUnavailable, "industry data layer unavailable")
		return
	}

	f := storage.SupplyChainEdgeFilter{
		Upstream:   c.Query("upstream"),
		Downstream: c.Query("downstream"),
		Relation:   c.Query("relation"),
	}
	if f.Upstream == "" && f.Downstream == "" && f.Relation == "" {
		httpserver.Fail(c, http.StatusBadRequest,
			"至少提供一个过滤条件（upstream / downstream / relation）——边表不接受无过滤全表扫描")
		return
	}

	edges, err := h.store.ListSupplyChainEdges(c.Request.Context(), f)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to list supply chain edges")
		httpserver.Fail(c, http.StatusInternalServerError, "failed to list supply chain edges")
		return
	}
	industryOK(c, edges)
}

// handleVitality serves GET /api/industry/vitality?industry_code=&metric=&start_date=&end_date=.
func (h *IndustryHandler) handleVitality(c *gin.Context) {
	if !h.storeReady() {
		httpserver.Fail(c, http.StatusServiceUnavailable, "industry data layer unavailable")
		return
	}

	f := storage.IndustryVitalityFilter{IndustryCode: c.Query("industry_code")}
	if f.IndustryCode == "" {
		httpserver.Fail(c, http.StatusBadRequest, "industry_code 必填")
		return
	}
	f.Metric = c.Query("metric")

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

	rows, err := h.store.ListIndustryVitality(c.Request.Context(), f)
	if err != nil {
		h.logger.Error().Err(err).Str("industry_code", f.IndustryCode).Msg("failed to list industry vitality")
		httpserver.Fail(c, http.StatusInternalServerError, "failed to list industry vitality")
		return
	}
	industryOK(c, rows)
}

// handleETFConstituents serves GET /api/industry/etf/:etf_code/constituents?obs_date=.
// obs_date is optional; the default is the latest report period (PIT).
func (h *IndustryHandler) handleETFConstituents(c *gin.Context) {
	if !h.storeReady() {
		httpserver.Fail(c, http.StatusServiceUnavailable, "industry data layer unavailable")
		return
	}

	f := storage.ETFConstituentFilter{ETFCode: c.Param("etf_code")}
	if f.ETFCode == "" {
		httpserver.Fail(c, http.StatusBadRequest, "etf_code 不可为空")
		return
	}
	obsDate, ok := parseDateQuery(c, "obs_date")
	if !ok {
		return
	}
	f.ObsDate = obsDate

	rows, err := h.store.ListETFConstituents(c.Request.Context(), f)
	if err != nil {
		h.logger.Error().Err(err).Str("etf_code", f.ETFCode).Msg("failed to list ETF constituents")
		httpserver.Fail(c, http.StatusInternalServerError, "failed to list ETF constituents")
		return
	}
	industryOK(c, rows)
}

// parseDateQuery parses an optional YYYY-MM-DD query parameter. On a format
// error it writes the 400 itself and returns ok=false — callers propagate the
// signal by returning early. Absent parameter → (nil, true).
func parseDateQuery(c *gin.Context, name string) (*time.Time, bool) {
	v := c.Query(name)
	if v == "" {
		return nil, true
	}
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		httpserver.Fail(c, http.StatusBadRequest, name+" 必须为 YYYY-MM-DD 格式")
		return nil, false
	}
	return &t, true
}
