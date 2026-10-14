// D7 · 市场 → 内核 路由的 handler 级验收腿（同步 POST /api/backtest）。
//
// 与 K10（handlers_backtest_kernel_test.go）/ K13（handlers_backtest_nautilus_test.go）
// 同一副形状，但路由键从「显式 kernel」换成「市场 → 内核」：
//
//  1. 正证据（端到端样例 run）：market=US → 路由到 nautilus 侧车 → 200 带侧车
//     产物并落库；go 内核零调用（证明市场路由真的生效，不是「反正 go 也能跑」）。
//  2. 反证腿（未注册市场）：market=HK → 400 点名 HK + 已登记集合；两个 runner
//     零调用 + 库内零行 —— 没有静默回退。
//  3. 破坏腿（删掉市场映射配置）：路由表为空（= kernel_routing.markets 被删）
//     → 带 market 的请求 400；**不带 market 的存量请求照常走 go**（删配置不能
//     连带打掉存量链路）。
//  4. 契约腿：CN → go 内核；market 与 kernel 双写不一致 → 400（拒绝而非替
//     调用方挑一个）。
//  5. 外壳一致性腿：国外线与国内线的回执**同一副字段集**，且侧车的数据面
//     （equity_curve → portfolio_values、trades）经别名表落进冻结字段 ——
//     前端/审计不感知内核差异。
//
// 破坏验证（开发期实测）：把 ResolveRunTarget 的 market 分支换成直接
// `return kernel, nil`（即忽略市场），腿 1 立刻红（nautilus 从未被调用）——已还原。

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/nautilus"
)

// ntSidecarFullReceipt 是一份**完整**的 ResultSpec 形状侧车回执：它带数据面
// （equity_curve / trades），用来验别名映射真的在这些字段上生效。
const ntSidecarFullReceipt = `{
	"id": "nt-us-run-d7",
	"status": "completed",
	"strategy": "native_sma_cross",
	"start_date": "2024-01-02",
	"end_date": "2024-03-01",
	"initial_capital": 1000000,
	"total_return": 0.0123,
	"sharpe_ratio": 1.5,
	"sortino_ratio": 2.0,
	"total_orders": 4,
	"total_positions": 2,
	"iterations": 60,
	"equity_curve": [
		{"date": "2024-01-02", "equity": 1000000.0, "cash": 1000000.0, "positions": 0.0},
		{"date": "2024-01-03", "equity": 1001230.0, "cash": 0.0, "positions": 1001230.0}
	],
	"trades": [
		{"symbol": "AAPL", "side": "BUY", "qty": 1000, "price": 100.0, "commission": 0.0,
		 "transfer_fee": 0.0, "stamp_tax": 0.0, "ts": "2024-01-02T00:00:00Z"}
	]
}`

// stubSidecarWithBody 起一个返回固定回执的假侧车（与 K13 的 stubSidecar 同形，
// 区别只在回执体可指定 —— D7 需要带数据面的完整回执）。
func stubSidecarWithBody(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// newMarketRoutingRouter 装配 go（tracking stub）+ nautilus（真 Runner → 假侧车）
// 两个内核，加一张市场路由表，挂在真 handler 后面。
func newMarketRoutingRouter(
	t *testing.T, sidecarURL string, markets map[string]string,
) (*gin.Engine, *memJobStore, *trackingRunner) {
	t.Helper()
	router, err := contracts.NewMarketRouter(markets)
	require.NoError(t, err)

	eng := newReadPathEngine(t)
	store := newMemJobStore()
	goKernel := &trackingRunner{}

	reg := contracts.NewEngineRegistry()
	reg.Register(contracts.DefaultKernel, goKernel)
	reg.Register(nautilus.KernelName, nautilus.NewRunner(sidecarURL, nil))

	jobSvc := backtest.NewJobService(store, reg, zerolog.Nop())
	r := gin.New()
	registerBacktestRoutes(r, eng, reg, jobSvc, router, zerolog.Nop())
	return r, store, goKernel
}

// 腿 1（正证据 · 端到端样例 run）：market=US → nautilus 侧车，落库同 schema。
func TestBacktestPost_MarketUSRoutesToNautilusSidecar(t *testing.T) {
	srv := stubSidecarWithBody(t, ntSidecarFullReceipt)
	r, store, goKernel := newMarketRoutingRouter(t, srv,
		map[string]string{"CN": contracts.DefaultKernel, "US": nautilus.KernelName})

	w := postBacktestKernel(t, r,
		`{"strategy":"native_sma_cross","stock_pool":["AAPL","MSFT"],`+
			`"start_date":"2024-01-02","end_date":"2024-03-01","market":"US"}`)

	require.Equal(t, http.StatusOK, w.Code, "market=US 必须走通, body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "nt-us-run-d7", "响应必须是侧车产物")
	assert.Equal(t, 0, goKernel.callCount(), "market 路由生效时 go 内核不得被触碰")

	require.Equal(t, 1, store.count(), "侧车结果必须落库（同一 JobStore 路径）")
	stored, err := store.GetBacktestJob(context.Background(), "nt-us-run-d7")
	require.NoError(t, err)
	require.NotNil(t, stored)
	resultJSON, _ := stored["result"].([]byte)
	assert.Contains(t, string(resultJSON), "0.0123", "NT 指标必须原样透传")
}

// 腿 2（反证腿 · 未注册市场）：400 + 点名 + 零 runner 调用 + 库内零行。
func TestBacktestPost_UnregisteredMarketFailsClosed(t *testing.T) {
	srv := stubSidecarWithBody(t, ntSidecarFullReceipt)
	r, store, goKernel := newMarketRoutingRouter(t, srv,
		map[string]string{"CN": contracts.DefaultKernel, "US": nautilus.KernelName})

	w := postBacktestKernel(t, r,
		`{"strategy":"native_sma_cross","stock_pool":["0700.HK"],`+
			`"start_date":"2024-01-02","end_date":"2024-03-01","market":"HK"}`)

	require.Equal(t, http.StatusBadRequest, w.Code, "未注册市场必须 400, body=%s", w.Body.String())
	body := w.Body.String()
	assert.Contains(t, body, "HK", "错误必须点名被拒的市场")
	assert.Contains(t, body, "CN", "错误必须给出已登记市场集合")
	assert.Contains(t, body, "no silent fallback", "错误必须说明是拒绝而非回退")

	// 反证本体：go 内核零调用 + 库内零行。若该请求回退 go，这里会出现一个
	// go 产物（trackingRunner 返回 "go-kernel-product" 并落库）。
	assert.Equal(t, 0, goKernel.callCount(), "未注册市场不得回退 go 内核")
	assert.Equal(t, 0, store.count(), "被拒请求不得在库内留下任何内核产物")
}

// 腿 3（破坏腿 · 删掉市场映射配置）：空表 → 带 market 400，不带 market 照旧。
func TestBacktestPost_DeletedMarketConfigRefusesMarketButKeepsLegacyPath(t *testing.T) {
	srv := stubSidecarWithBody(t, ntSidecarFullReceipt)
	// nil 市场表 = kernel_routing.markets 整节被删。
	r, store, goKernel := newMarketRoutingRouter(t, srv, nil)

	w := postBacktestKernel(t, r,
		`{"strategy":"native_sma_cross","stock_pool":["AAPL"],`+
			`"start_date":"2024-01-02","end_date":"2024-03-01","market":"US"}`)
	require.Equal(t, http.StatusBadRequest, w.Code, "配置被删后带市场必须 fail-closed")
	assert.Contains(t, w.Body.String(), "(none)", "已登记集合为空必须显式呈现")
	assert.Equal(t, 0, store.count())

	// 存量链路不受影响：不带 market 的老请求仍走 go 内核。
	w2 := postBacktestKernel(t, r,
		`{"strategy":"buy_and_hold","stock_pool":["600000.SH"],`+
			`"start_date":"2024-01-02","end_date":"2024-06-28"}`)
	require.Equal(t, http.StatusOK, w2.Code, "删掉市场配置不得打掉存量链路, body=%s", w2.Body.String())
	assert.Equal(t, 1, goKernel.callCount(), "不带 market 的请求必须照旧走 go")
}

// 腿 4a（契约腿）：market=CN → go 内核（市场表把 CN 指向 go）。
func TestBacktestPost_MarketCNRoutesToGoKernel(t *testing.T) {
	srv := stubSidecarWithBody(t, ntSidecarFullReceipt)
	r, _, goKernel := newMarketRoutingRouter(t, srv,
		map[string]string{"CN": contracts.DefaultKernel, "US": nautilus.KernelName})

	w := postBacktestKernel(t, r,
		`{"strategy":"buy_and_hold","stock_pool":["600000.SH"],`+
			`"start_date":"2024-01-02","end_date":"2024-06-28","market":"CN"}`)

	require.Equal(t, http.StatusOK, w.Code, "market=CN 必须走 go, body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "from-go")
	assert.Equal(t, 1, goKernel.callCount())
}

// 腿 4b（契约腿）：market 与 kernel 双写不一致 → 400（拒绝而非挑一个）。
func TestBacktestPost_ConflictingMarketAndKernelIsRefused(t *testing.T) {
	srv := stubSidecarWithBody(t, ntSidecarFullReceipt)
	r, store, goKernel := newMarketRoutingRouter(t, srv,
		map[string]string{"CN": contracts.DefaultKernel, "US": nautilus.KernelName})

	w := postBacktestKernel(t, r,
		`{"strategy":"native_sma_cross","stock_pool":["AAPL"],`+
			`"start_date":"2024-01-02","end_date":"2024-03-01","market":"US","kernel":"go"}`)

	require.Equal(t, http.StatusBadRequest, w.Code, "冲突必须 400, body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "refusing to pick one")
	assert.Equal(t, 0, goKernel.callCount())
	assert.Equal(t, 0, store.count())
}

// 腿 5（外壳一致性）：侧车数据面经别名落进冻结字段；回执**不得**携带任何
// 内核专属字段（泄漏 = 外壳开始分叉，前端/审计就得感知内核差异）。
//
// 为什么不比「两线回执的键集相等」：两层证据都不成立——`BacktestResponse`
// 的字段全带 `omitempty`，键集反映的是**这次跑出了什么值**，不是 schema；
// 而字段集本身是同一个 Go 类型，比较它只是把编译期事实重述一遍（伪判据）。
// 真正会分叉的是「内核专属字段有没有漏进共享外壳」，这才是这条腿断言的东西。
func TestBacktestPost_ShellSchemaIsSharedAcrossKernels(t *testing.T) {
	srv := stubSidecarWithBody(t, ntSidecarFullReceipt)
	r, _, _ := newMarketRoutingRouter(t, srv,
		map[string]string{"CN": contracts.DefaultKernel, "US": nautilus.KernelName})

	// 国外线（nautilus）：回执必须带上侧车的数据面。
	foreign := postBacktestKernel(t, r,
		`{"strategy":"native_sma_cross","stock_pool":["AAPL","MSFT"],`+
			`"start_date":"2024-01-02","end_date":"2024-03-01","market":"US"}`)
	require.Equal(t, http.StatusOK, foreign.Code, "body=%s", foreign.Body.String())

	var ntResp contracts.BacktestResponse
	require.NoError(t, json.Unmarshal(foreign.Body.Bytes(), &ntResp))
	require.Len(t, ntResp.PortfolioValues, 2,
		"侧车 equity_curve 必须经别名落进 PortfolioValues（丢了 = 数据面在外壳边界蒸发）")
	assert.Equal(t, 1001230.0, ntResp.PortfolioValues[1].TotalValue)
	require.Len(t, ntResp.Trades, 1, "侧车 trades 必须落进冻结的 Trades 字段")
	assert.Equal(t, "AAPL", ntResp.Trades[0].Symbol)
	assert.InDelta(t, 0.0123, ntResp.TotalReturn, 1e-12)

	// 外壳不得泄漏内核专属字段：侧车的 wire 名与执行机制量都不许出现在共享
	// 外壳上（它们该被别名吸收或被丢弃，见 sidecar 侧 test_d7_native_mode 的
	// SIDECAR_WIRE_ALIASES / SIDECAR_ONLY_KEYS 登记表）。
	ntJSON := marshalJSON(t, ntResp)
	for _, leaked := range []string{
		"nt_stats", "equity_curve", "total_orders", "total_positions", "iterations",
		"kernel", "market",
	} {
		assert.NotContains(t, ntJSON, `"`+leaked+`"`,
			"内核专属字段 %q 漏进了共享外壳——前端/审计会被迫感知内核差异", leaked)
	}

	// 国内线（go）：同一条序列化路径、同一副外壳（同一个 Go 类型），且同样
	// 不携带内核专属字段。
	domestic := postBacktestKernel(t, r,
		`{"strategy":"buy_and_hold","stock_pool":["600000.SH"],`+
			`"start_date":"2024-01-02","end_date":"2024-06-28","market":"CN"}`)
	require.Equal(t, http.StatusOK, domestic.Code, "body=%s", domestic.Body.String())

	var goResp contracts.BacktestResponse
	require.NoError(t, json.Unmarshal(domestic.Body.Bytes(), &goResp))
	goJSON := marshalJSON(t, goResp)
	for _, leaked := range []string{"nt_stats", "equity_curve", "kernel", "market"} {
		assert.NotContains(t, goJSON, `"`+leaked+`"`, "国内线外壳同样不得携带 %q", leaked)
	}

	// 两条线的回执都能被**同一个**冻结类型无损解出——这就是「外壳共用」的可检形式。
	assert.Equal(t, "from-go", goResp.Strategy)
	assert.Equal(t, "completed", goResp.Status)
}

// marshalJSON 把响应体序列化成字符串（键集/泄漏断言用）。
func marshalJSON(t *testing.T, resp contracts.BacktestResponse) string {
	t.Helper()
	raw, err := json.Marshal(resp)
	require.NoError(t, err)
	return string(raw)
}

// 腿 6（E2E · 环境门）：market=US → **真实** NT 侧车（`--market US`）→ 真
// NautilusTrader 引擎 → 回执经外壳落回。这是 D7 验收的「端到端样例 run」。
//
// 默认 `go test ./...` 会 skip（未设环境变量）；显式跑：
//
//	# 先起一个 US 档案的侧车（独立的 catalog，见 docs 仓 nt-sidecar-operations.md）
//	python -m sidecar --port 19100 --market US --catalog <us-catalog>
//	NT_SIDECAR_URL=http://127.0.0.1:19100 \
//	D7_E2E_SYMBOLS=AAPL,MSFT D7_E2E_START=2024-01-02 D7_E2E_END=2024-03-01 \
//	go test ./cmd/analysis/ -run D7MarketEndToEnd -v
func TestBacktestPost_D7MarketEndToEnd(t *testing.T) {
	sidecarURL := strings.TrimSpace(os.Getenv(nautilus.URLEnvVar))
	if sidecarURL == "" {
		t.Skipf("%s not set — start a `--market US` sidecar and set it to run the end-to-end leg",
			nautilus.URLEnvVar)
	}

	symbols := envOr("D7_E2E_SYMBOLS", "AAPL,MSFT")
	start := envOr("D7_E2E_START", "2024-01-02")
	end := envOr("D7_E2E_END", "2024-03-01")

	pool, err := json.Marshal(strings.Split(symbols, ","))
	require.NoError(t, err)

	r, store, goKernel := newMarketRoutingRouter(t, sidecarURL,
		map[string]string{"CN": contracts.DefaultKernel, "US": nautilus.KernelName})

	body := `{"strategy":"native_sma_cross","stock_pool":` + string(pool) +
		`,"start_date":"` + start + `","end_date":"` + end + `","market":"US"}`
	w := postBacktestKernel(t, r, body)
	require.Equal(t, http.StatusOK, w.Code, "真实侧车必须跑通, body=%s", w.Body.String())
	assert.Equal(t, 0, goKernel.callCount(), "market=US 时 go 内核不得被触碰")

	var resp contracts.BacktestResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, contracts.StatusCompleted, resp.Status, "body=%s", w.Body.String())
	require.NotEmpty(t, resp.ID, "侧车必须返回 run id")
	require.NotEmpty(t, resp.PortfolioValues, "回执必须带数据面（equity_curve → portfolio_values）")

	// 落库同 schema：真跑的一次必须能在同一 JobStore 路径上读回来。
	require.Equal(t, 1, store.count())
	stored, err := store.GetBacktestJob(context.Background(), resp.ID)
	require.NoError(t, err)
	require.NotNil(t, stored, "运行结果必须落库")

	t.Logf("D7 E2E ok: run_id=%s market=US kernel=nautilus trades=%d equity_points=%d total_return=%.6f",
		resp.ID, len(resp.Trades), len(resp.PortfolioValues), resp.TotalReturn)
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
