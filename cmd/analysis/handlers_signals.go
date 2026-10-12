package main

// K8（TASKS K8 ③.4）：外部模型信号 HTTP 写入门。
//
// 端点：
//   POST /api/signals —— 批量写入 ExternalSignal（外部 ML 模型进程外产出，
//   经此门注入内核；消费侧是 strategy.SignalStrategy，按 as_of 防前视拉取）。
//
// 防御口径（fail-loud，与 pkg/strategy 契约同一份口径）：
//   - ValidateExternalSignal 逐条校验（带索引 400，脏行不落库）；
//   - 未来 as_of 拒绝 400 —— strategy.DayOf 归一后与今天比较，与
//     RejectFutureSignals / SignalStore.ListFor 是同一「未来信号物理不可见」
//     语义（写入门拒绝是第一道闸，契约守卫是第二道）；
//   - SignalContentHash 幂等去重在 SignalStore.Save（ON CONFLICT DO NOTHING），
//     重复注入不重复落库，客户端可安全重试。
//
// 敏感写操作：requireTrader（trader/admin）——与 ExecutionHandler 的
// AUD-02 同款 RBAC 模式。
//
// handler 样板沿用 handlers_evidence.go 的 "pattern B"（struct + functional
// options + RegisterRoutes）。

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"github.com/ruoxizhnya/quant-trading/pkg/auth"
	"github.com/ruoxizhnya/quant-trading/pkg/httpserver"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// SignalsHandler serves the POST /api/signals write door.
type SignalsHandler struct {
	store   strategy.SignalStore
	authSvc *auth.Service
	logger  zerolog.Logger
}

// SignalsHandlerOption configures a SignalsHandler.
type SignalsHandlerOption func(*SignalsHandler)

// WithSignalsAuth enables RBAC on the signal write door (trader or admin;
// viewer gets 403). When svc is nil or disabled this is a no-op
// (open-access mode) — same contract as WithExecutionAuth.
func WithSignalsAuth(svc *auth.Service) SignalsHandlerOption {
	return func(h *SignalsHandler) { h.authSvc = svc }
}

// NewSignalsHandler constructs a SignalsHandler backed by store.
//
// store 可为 nil：registerRoutes 必须容忍零值 ServerDeps 不 panic
// （deps_test.newMinimalDeps 约束），store 缺失推迟到请求时 503 ——
// 路由照常注册，探针/路由表不因未接库而漂移。
func NewSignalsHandler(store strategy.SignalStore, logger zerolog.Logger, opts ...SignalsHandlerOption) *SignalsHandler {
	h := &SignalsHandler{
		store:  store,
		logger: logger.With().Str("component", "signals_handler").Logger(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// RegisterRoutes wires POST /api/signals.
func (h *SignalsHandler) RegisterRoutes(router *gin.Engine) {
	router.POST("/api/signals", h.requireTrader(), h.createSignals)
}

// requireTrader returns the RBAC middleware for the signal write door,
// or a no-op middleware when auth is not wired/enabled（与
// ExecutionHandler.requireTrader 同款：handler 链形状跨配置一致）.
func (h *SignalsHandler) requireTrader() gin.HandlerFunc {
	if h.authSvc == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return h.authSvc.RequireRole(auth.RoleTrader, auth.RoleAdmin)
}

// createSignalsRequest is the batch write body: {"signals": [...]}.
type createSignalsRequest struct {
	Signals []strategy.ExternalSignal `json:"signals"`
}

// createSignalsResponse reports how many signals were accepted.
// 幂等去重在存储层（content_hash ON CONFLICT DO NOTHING），accepted 计
// 「通过校验并提交」的条数；重复注入同批也报 accepted（未新增行）。
type createSignalsResponse struct {
	Accepted int `json:"accepted"`
}

// createSignals handles POST /api/signals.
//
// 校验先于落库：全批校验通过才开始 Save，避免「半批脏写」——单条校验失败
// 即 400 整批拒绝，客户端修正后整批重发（幂等去重保证重发安全）。
func (h *SignalsHandler) createSignals(c *gin.Context) {
	if h.store == nil {
		httpserver.Fail(c, http.StatusServiceUnavailable, "signal store unavailable")
		return
	}

	var req createSignalsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpserver.Fail(c, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if len(req.Signals) == 0 {
		httpserver.Fail(c, http.StatusBadRequest, "signals batch is empty")
		return
	}

	now := time.Now()
	for i, sig := range req.Signals {
		if err := strategy.ValidateExternalSignal(sig); err != nil {
			httpserver.Fail(c, http.StatusBadRequest, fmt.Sprintf("signals[%d]: %v", i, err))
			return
		}
		// 未来 as_of 拒绝（fail-loud）：DayOf 口径与 RejectFutureSignals
		// / SignalStore.ListFor 一致 —— 「未来」的判定只有一份。
		if strategy.DayOf(sig.AsOf).After(strategy.DayOf(now)) {
			httpserver.Fail(c, http.StatusBadRequest,
				fmt.Sprintf("signals[%d]: as_of %s 是未来日期（未来信号物理不可见）",
					i, sig.AsOf.UTC().Format(time.RFC3339)))
			return
		}
	}

	for i, sig := range req.Signals {
		if err := h.store.Save(c.Request.Context(), sig); err != nil {
			httpserver.Fail(c, http.StatusInternalServerError,
				fmt.Sprintf("signals[%d]: save failed: %v", i, err))
			return
		}
	}

	h.logger.Info().Int("accepted", len(req.Signals)).Msg("external signals accepted")
	c.JSON(http.StatusCreated, createSignalsResponse{Accepted: len(req.Signals)})
}
