package main

// K9 契约腿（handler 层）：RunSpec 校验在 API 边界的 400 出口。
//
// 钉住的缺陷形态：engine_params 的未知名被 json.Unmarshal 静默吞掉 ——
// 调用方以为参数生效了，实际什么都没发生。fail-closed 要求请求被拒绝
// 并点名未知键。把 handlers_backtest.go 里的 ValidateRunSpec 调用删掉，
// 这里立刻红。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// postBacktestRunSpec 与 handlers_backtest_test.go 的 postBacktest 刻意不同名：
// 那条用零值引擎（测错误映射，不触引擎），这条用 newReadPathEngine（合法请求
// 要真的穿进引擎，零值引擎会在 checkCalendarExists 处 panic）。
func postBacktestRunSpec(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	// jobService 传 nil：这些腿都走旧格式同步分支，不碰 job 服务。
	registerBacktestRoutes(r, newReadPathEngine(t), nil, zerolog.Nop())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/backtest", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

const validRunSpecBody = `{"strategy":"momentum","stock_pool":["600000.SH"],"start_date":"2024-01-01","end_date":"2024-06-30"}`

// 核心腿：engine_params 未知名 → 400，且错误点名该键。
func TestBacktestPost_RejectsUnknownEngineParams(t *testing.T) {
	w := postBacktestRunSpec(t, `{"strategy":"momentum","stock_pool":["600000.SH"],"start_date":"2024-01-01","end_date":"2024-06-30","engine_params":{"not_a_param":1}}`)

	require.Equal(t, http.StatusBadRequest, w.Code, "未知名 engine_params 必须 400, body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "not_a_param", "错误必须点名未知键")
	assert.Contains(t, w.Body.String(), "engine_params", "错误必须点名字段")
}

// 反证腿：合法请求（无 engine_params）不得被校验闸门拦下。
//
// 断言的是「不因 engine_params 被拒」，不是引擎侧的最终状态 —— 内存引擎
// 没有交易日历，最终会以「日历未同步」的分类错误（409）收场，那恰好是
// 「请求已穿过校验进入引擎」的证据，但不是本腿的判据。
func TestBacktestPost_ValidRunSpecPassesValidation(t *testing.T) {
	w := postBacktestRunSpec(t, validRunSpecBody)
	assert.NotEqual(t, http.StatusBadRequest, w.Code, "合法请求不得被 RunSpec 校验拒绝, body=%s", w.Body.String())
	assert.NotContains(t, w.Body.String(), "engine_params", "合法请求不应命中 engine_params 校验错误")
}

// kernel 透传腿（handler 层）：带 kernel 字段的请求同样不得被校验闸门误拒。
// kernel 的取值裁定（未知内核 fail-closed）是 K10 路由的职责，K9 只透传。
func TestBacktestPost_KernelFieldDoesNotTripValidation(t *testing.T) {
	w := postBacktestRunSpec(t, `{"strategy":"momentum","stock_pool":["600000.SH"],"start_date":"2024-01-01","end_date":"2024-06-30","kernel":"nautilus"}`)
	assert.NotEqual(t, http.StatusBadRequest, w.Code, "仅带 kernel 的请求不得被拒, body=%s", w.Body.String())
	assert.NotContains(t, w.Body.String(), "engine_params")
}
