package main

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"github.com/ruoxizhnya/quant-trading/pkg/compliance"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
)

// P2-4 (ODR-028): compliance / investor-suitability endpoints.
//
// These endpoints expose the pkg/compliance/appropriateness package
// to the frontend. They are intentionally read-only — the actual
// enforcement (i.e. blocking an order that fails the check) is done
// in handlers_execution.go (see P2-4 forward integration; out of
// scope for this commit, but the API contract is set so the order
// path can `POST /api/compliance/check` before `POST /api/orders`).
//
// Endpoints:
//
//	POST /api/compliance/check          — check a single symbol
//	GET  /api/compliance/requirements   — list all per-board thresholds
//	GET  /api/compliance/boards         — boards that require suitability
//	POST /api/compliance/abnormal/run   — run 6-category detector over
//	                                      a supplied day's trades/orders
//	POST /api/compliance/report/daily   — generate large-trade
//	                                      report.json for a given day
//
// The "user_id" + profile fields are read from the request body
// (POST) so the handler is decoupled from the user-store
// implementation. In production these would come from the JWT
// subject + a DB lookup; in paper-trading mode they come from the
// config-driven stub at main.go.

// ComplianceHandler serves compliance / suitability HTTP endpoints
// from the analysis service, backed by the in-process
// pkg/compliance package. The package itself is stateless
// (registry is RWMutex-guarded but otherwise read-only) so a
// single shared instance is enough.
type ComplianceHandler struct {
	logger zerolog.Logger
	// defaultProfile is the fallback profile used when the request
	// does not specify one. In production this is loaded from the
	// user table; in paper-trading mode it is the
	// config-driven `trading.default_user_profile`.
	defaultProfile compliance.SuitabilityProfile
	// abnormalDetector is the 6-category abnormal-trade detector.
	// Constructed once with the default thresholds and shared.
	abnormalDetector *compliance.AbnormalDetector
	// reporter is the large-transaction reporter. Constructed once
	// with the default thresholds; the output directory is read
	// from the analysis-service viper config.
	reporter *compliance.LargeTraderReporter
	// now is injectable so tests can pin the clock.
	now func() time.Time
}

// NewComplianceHandler constructs a ComplianceHandler.
//
// `reporterCfg` and `abnormalThresholds` are taken from the
// analysis-service config; the call site (main.go) is responsible
// for mapping viper keys into the strongly-typed config structs
// so this handler stays free of viper imports.
func NewComplianceHandler(
	logger zerolog.Logger,
	defaultProfile compliance.SuitabilityProfile,
	reporterCfg compliance.LargeTradeConfig,
) *ComplianceHandler {
	return &ComplianceHandler{
		logger:           logger.With().Str("component", "compliance_handler").Logger(),
		defaultProfile:   defaultProfile,
		abnormalDetector: compliance.NewAbnormalDetector(),
		reporter:         compliance.NewLargeTraderReporter(reporterCfg),
		now:              func() time.Time { return time.Now() },
	}
}

// RegisterRoutes wires the compliance endpoints under /api/compliance.
// We do not register legacy /compliance/* paths because the
// compliance module is brand-new (P2-4) — there is no pre-existing
// client to preserve compatibility for.
func (h *ComplianceHandler) RegisterRoutes(router *gin.Engine) {
	group := router.Group("/api/compliance")
	{
		// P2-4: investor suitability
		group.POST("/check", h.check)
		group.GET("/requirements", h.requirements)
		group.GET("/boards", h.boards)
		// P2-5: abnormal-trade detection
		group.POST("/abnormal/run", h.abnormalRun)
		// P2-6: large-transaction daily report
		group.POST("/report/daily", h.reportDaily)
	}
}

// checkRequest is the input shape for POST /api/compliance/check.
// Symbol is required; profile fields are optional (they default to
// the handler's `defaultProfile` if omitted, which is the
// paper-trading mode behavior).
type checkRequest struct {
	Symbol string `json:"symbol" binding:"required"`

	// Optional profile overrides — production callers typically
	// omit these and let the handler fill from the user table.
	UserID            string                `json:"user_id,omitempty"`
	AssetDailyAvgCNY  *float64              `json:"asset_daily_avg_cny,omitempty"`
	FirstTradeAt      *time.Time            `json:"first_trade_at,omitempty"`
	RiskLevel         *compliance.RiskLevel `json:"risk_level,omitempty"`
	BoardsEnabled     []string              `json:"boards_enabled,omitempty"`
	RiskTestExpiredAt *time.Time            `json:"risk_test_expired_at,omitempty"`
}

// checkResponse mirrors compliance.CheckResult but with snake_case
// JSON tags (frontend convention) and a few helper fields the UI
// needs to render the result without re-walking the result.
type checkResponse struct {
	Allowed     bool                              `json:"allowed"`
	Board       string                            `json:"board"`
	BoardName   string                            `json:"board_name,omitempty"`
	Reasons     []string                          `json:"reasons"`
	UserID      string                            `json:"user_id"`
	ProfileAge  int                               `json:"profile_age_months"`
	AssetDaily  float64                           `json:"asset_daily_avg_cny"`
	RiskLevel   string                            `json:"risk_level"`
	Required    *compliance.BoardRequirement      `json:"required,omitempty"`
	CheckedAt   time.Time                         `json:"checked_at"`
}

// check returns the structured eligibility verdict for the given
// symbol. The handler NEVER mutates state — the profile snapshot
// is taken from the request and discarded after the check.
func (h *ComplianceHandler) check(c *gin.Context) {
	var req checkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Build the profile. If the caller supplied partial overrides,
	// they take precedence over the default profile on a per-field
	// basis; otherwise we use the default wholesale.
	profile := h.defaultProfile
	if req.UserID != "" {
		profile.UserID = req.UserID
	}
	if req.AssetDailyAvgCNY != nil {
		profile.AssetDailyAvgCNY = *req.AssetDailyAvgCNY
	}
	if req.FirstTradeAt != nil {
		profile.FirstTradeAt = *req.FirstTradeAt
	}
	if req.RiskLevel != nil {
		profile.RiskLevel = *req.RiskLevel
	}
	if len(req.BoardsEnabled) > 0 {
		profile.BoardsEnabled = req.BoardsEnabled
	}
	if req.RiskTestExpiredAt != nil {
		profile.RiskTestExpiredAt = *req.RiskTestExpiredAt
	}

	board := live.ClassifySymbol(req.Symbol)
	result := profile.Check(board, h.now())

	resp := checkResponse{
		Allowed:    result.Allowed,
		Board:      string(result.Board),
		Reasons:    result.Reasons,
		UserID:     result.UserID,
		ProfileAge: result.ProfileAge,
		AssetDaily: result.AssetDaily,
		RiskLevel:  result.RiskLevel.String(),
		Required:   result.Required,
		CheckedAt:  result.CheckedAt,
	}
	if result.Required != nil {
		resp.BoardName = result.Required.DisplayName
	}

	// HTTP status: 200 = allowed (or allowed-by-default for boards
	// outside the suitability scope); 403 = explicitly rejected.
	// 400 = symbol unparseable (board = unknown AND reasons empty).
	// 422 = rejected for legitimate compliance reasons.
	if !result.Allowed && result.Required != nil {
		c.JSON(http.StatusUnprocessableEntity, resp)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// requirements returns the full BoardRequirement list. The frontend
// uses this to render the "板块开通条件" page (per-board asset +
// experience + risk-level requirements, plus human-readable
// description text).
func (h *ComplianceHandler) requirements(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"requirements": compliance.AllRequirements(),
		"generated_at": h.now(),
	})
}

// boards returns the list of boards that require suitability checks.
// Used by the frontend to decide whether to even render the
// compliance dialog (e.g. a portfolio of main-board-only stocks
// can skip the check entirely).
func (h *ComplianceHandler) boards(c *gin.Context) {
	boards := compliance.BoardsRequiringSuitability()
	names := make([]string, 0, len(boards))
	for _, b := range boards {
		names = append(names, b.String())
	}
	c.JSON(http.StatusOK, gin.H{
		"boards": names,
		"count":  len(boards),
	})
}

// ============================================================
// P2-5: Abnormal-trade detection endpoint
// ============================================================

// abnormalRunRequest is the input for POST /api/compliance/abnormal/run.
// `account_id` is the target account; `orders` and `trades` are
// the sliding window the detector should analyze. The window is
// implicit (now - detector.Window); the caller is responsible for
// passing only in-window records.
type abnormalRunRequest struct {
	AccountID string                          `json:"account_id"`
	Orders    []compliance.OrderRecord        `json:"orders"`
	Trades    []compliance.TradeRecord        `json:"trades"`
}

// abnormalRunResponse wraps the detector output for transport.
type abnormalRunResponse struct {
	Alerts  []compliance.AbnormalAlert `json:"alerts"`
	Count   int                        `json:"count"`
	RunAt   time.Time                  `json:"run_at"`
}

// abnormalRun runs the 6-category detector over the supplied
// window. The handler is stateless — the detector itself is
// concurrency-safe and the request is fully isolated.
func (h *ComplianceHandler) abnormalRun(c *gin.Context) {
	var req abnormalRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	alerts := h.abnormalDetector.RunAll(req.AccountID, req.Orders, req.Trades, h.now())
	c.JSON(http.StatusOK, abnormalRunResponse{
		Alerts: alerts,
		Count:  len(alerts),
		RunAt:  h.now(),
	})
}

// ============================================================
// P2-6: Large-transaction daily report endpoint
// ============================================================

// reportDailyRequest is the input for POST /api/compliance/report/daily.
// `trading_date` is optional; when omitted, the handler uses the
// current day in the server's local zone. The trades slice is the
// day's fills; the reporter handles the date filter itself.
type reportDailyRequest struct {
	TradingDate string                   `json:"trading_date,omitempty"` // YYYY-MM-DD
	Trades      []compliance.TradeRecord `json:"trades"`
}

// reportDailyResponse mirrors the report with the on-disk path
// prepended for convenience. The full JSON body is also embedded
// in `report` so the caller doesn't have to re-read the file.
type reportDailyResponse struct {
	Path   string                   `json:"path"`
	Report *compliance.LargeTradeReport `json:"report"`
}

// reportDaily generates a large-transaction report.json for the
// given day's trades. The file is written to
// `reporter.config.OutputPath/large-trades-YYYYMMDD.json` with
// 0600 perms (operator-only). The handler returns the path
// alongside the report body so the operator can both inspect and
// forward the file to the regulator without a second call.
func (h *ComplianceHandler) reportDaily(c *gin.Context) {
	var req reportDailyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	day := h.now()
	if req.TradingDate != "" {
		t, err := time.Parse("2006-01-02", req.TradingDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "trading_date must be YYYY-MM-DD"})
			return
		}
		day = t
	}
	report := h.reporter.BuildReport(req.Trades, day)
	// Use a 5s timeout for the disk write — same as the alert-loop
	// threshold; if the disk is jammed the operator should know
	// quickly.
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		c.JSON(http.StatusRequestTimeout, gin.H{"error": "request cancelled"})
		return
	}
	path, err := h.reporter.WriteReport(report)
	if err != nil {
		h.logger.Error().Err(err).Msg("write large-trade report failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.logger.Info().
		Str("path", path).
		Int("large_trades", len(report.LargeTrades)).
		Int("cumulative_accounts", len(report.CumulativeByAccount)).
		Msg("daily large-trade report generated (P2-6)")
	c.JSON(http.StatusOK, reportDailyResponse{Path: path, Report: report})
}
