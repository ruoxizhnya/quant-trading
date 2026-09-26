package main

// AUD-60 的行为腿：回测 handler 必须把引擎返回的**带类别**错误映射成对应状态，
// 不能一律拍平成 500。
//
// 为什么只测「日期不可解析」这一条路：`parseBacktestDateRange` 是 RunBacktest 里
// **唯一**在任何 store / provider 访问之前就返回的分支，所以它不需要真库就能把
// 「引擎返回 AppError」这个形状喂给 handler。
//
// 另一条路（DATA_QUALITY → 409，即"日历没同步"）需要真库才能触发 —— 它的覆盖分两处：
//   - internal/httpserver/errors_test.go 的映射表（含「未知即 500」的对照腿）；
//   - 真环境的 A/B：请求一个库里没有日历的区间（如 2030 年）应得 409，
//     而区间有日历时应得 200。
// 之所以不在这里硬造，是因为 zero-value Engine 的 provider 是 nil，
// 走到 checkCalendarExists 的 provider 回退会 panic —— 造出来的会是一条假腿。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
)

// gin 的 mode 由 TestMain 设一次（AUD-12）—— 这里不调 gin.SetMode。

func postBacktest(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	// jobService 传 nil：这条腿只走同步回测分支，不碰 job 服务。
	registerBacktestRoutes(r, &backtest.Engine{}, nil, zerolog.Nop())

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/backtest", strings.NewReader(body)))
	return w
}

func TestBacktestHandler_MapsEngineCategoryToStatus(t *testing.T) {
	const body = `{"strategy":"momentum","stock_pool":["600000.SH"],"start_date":"2024-13-99","end_date":"2024-01-31"}`

	w := postBacktest(t, body)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("引擎的 INVALID_INPUT 应当映射成 400，实际 %d（响应体 %s）", w.Code, w.Body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应不是 JSON: %v (%s)", err, w.Body.String())
	}

	// 这一条是缺陷本体：修之前这里恒为 "internal"，客户端只读得到
	// 「服务器内部错误」——而真正该看的是「start_date 有问题」。
	if got["code"] != "bad_request" {
		t.Fatalf("错误码应当是 bad_request（缺陷前恒为 internal），实际 %v", got["code"])
	}
	msg, _ := got["error"].(string)
	if !strings.Contains(msg, "start_date") {
		t.Fatalf("4xx 要把原因说给用户听（应提到 start_date），实际 %q", msg)
	}
}
