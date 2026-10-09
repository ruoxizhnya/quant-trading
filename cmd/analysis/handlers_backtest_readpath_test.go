package main

// OBS-01 切片 2 的 handler 层行为腿：GET /api/backtest/:id/report 必须
// **透传**引擎内存态里的真实状态（含 "invalid"）并附上 invalid_reasons，
// 而不是像缺陷前那样硬编码 status="completed"。
//
// 为什么能在**没有真库**的情况下建出「带内存态的引擎」：Engine.StateStore()
// 是导出的，且 NewEngineWithOptions / NewNoopStateStore /
// marketdata.NewInMemoryProvider 也都有导出构造器 —— 直接 Put 一个 "invalid"
// 状态即可命中 in-memory 读路径，无需跑真实回测（真库只读是别处的约束，
// 这里干脆不碰库）。
//
// 这条腿钉住的正是缺陷本体：把 buildResponseFromState 里的 `Status: status`
// 改回 `Status: "completed"`（或把闸门收回只认 "completed"），这里立刻红。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
)

// newReadPathEngine 构造一个带 NoopStateStore 的内存引擎（不触库、不跑回测）。
func newReadPathEngine(t *testing.T) *backtest.Engine {
	t.Helper()
	eng, err := backtest.NewEngineWithOptions(
		backtest.Config{},
		marketdata.NewInMemoryProvider(),
		backtest.WithStateStore(backtest.NewNoopStateStore()),
		backtest.WithLogger(zerolog.Nop()),
	)
	require.NoError(t, err)
	return eng
}

// putBacktestState 把一个终态（completed / invalid）直接塞进引擎的 state store。
func putBacktestState(eng *backtest.Engine, id, status string, reasons []string) {
	day := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	eng.StateStore().Put(id, &backtest.BacktestState{
		ID:     id,
		Status: status,
		Params: domain.BacktestParams{StrategyName: "momentum", StartDate: day, EndDate: day},
		Result: &domain.BacktestResult{
			StartDate:      day,
			EndDate:        day,
			InvalidReasons: reasons,
		},
	})
}

func getReport(t *testing.T, eng *backtest.Engine, id string) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	// jobService 传 nil：这两条腿都命中内存读路径，不碰 job 服务。
	registerBacktestRoutes(r, eng, nil, zerolog.Nop())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/backtest/"+id+"/report", nil))
	return w
}

// 核心腿：无效运行在 GET /:id/report 上必须 status=invalid + invalid_reasons，
// 且可读（200）—— 不能是硬编码的 completed，也不能是 404。
func TestBacktestReport_TransmitsInvalidStatus(t *testing.T) {
	eng := newReadPathEngine(t)
	reasons := []string{"empty_universe", "zero_trades", "garbage_metric"}
	putBacktestState(eng, "bt-invalid", "invalid", reasons)

	w := getReport(t, eng, "bt-invalid")
	require.Equal(t, http.StatusOK, w.Code, "无效运行必须可读（200），实际 body=%s", w.Body.String())

	var got struct {
		ID             string   `json:"id"`
		Status         string   `json:"status"`
		InvalidReasons []string `json:"invalid_reasons"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got), "响应不是 JSON: %s", w.Body.String())
	assert.Equal(t, "invalid", got.Status, "GET report 必须透传 invalid，不能硬编码 completed")
	assert.Equal(t, reasons, got.InvalidReasons, "响应必须带上 InvalidReasons")
	assert.Equal(t, "bt-invalid", got.ID)
}

// 反证腿：有效运行在 GET /:id/report 上仍 status=completed、无 invalid_reasons
// —— 正常路径行为一字不变。
func TestBacktestReport_CompletedStaysCompleted(t *testing.T) {
	eng := newReadPathEngine(t)
	putBacktestState(eng, "bt-ok", "completed", nil)

	w := getReport(t, eng, "bt-ok")
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var got struct {
		Status         string   `json:"status"`
		InvalidReasons []string `json:"invalid_reasons"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "completed", got.Status, "正常路径行为必须与改前一致")
	assert.Empty(t, got.InvalidReasons, "有效运行不得带 invalid_reasons")
}
