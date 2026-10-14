// K13 · handler-level kernel-routing tests for the NautilusTrader sidecar.
//
// Mirrors the K10 acceptance shape (handlers_backtest_kernel_test.go) for the
// second, externally-implemented kernel:
//
//  1. 正证据（同步）：注册 nautilus 侧车 stub → kernel=nautilus 走侧车，200 响应
//     带侧车产物，且经同一 SaveSyncResult 路径落库；
//  2. 反证腿（侧车未启动）：kernel=nautilus 显式失败 + 库内零行，且 **go 内核
//     零调用** —— 证明没有静默回退（FR-1.3）；
//  3. 环境门（E2E）：NT_SIDECAR_URL 指向真实侧车时，经真库跑通并落库。
//
// 破坏验证（开发期实测）：把 NautilusRunner.RunBacktest 的失败分支改成
// 「返回空 completed response + nil error」，两条正/反证腿立刻红（已还原）。

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/nautilus"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// sidecarResponse is a minimal sidecar reply the stub returns.
const ntSidecarCompleted = `{
	"id": "nt-run-k13",
	"status": "completed",
	"strategy": "buy_and_hold",
	"start_date": "2024-01-02",
	"end_date": "2024-06-28",
	"initial_capital": 1000000,
	"total_return": 0.042,
	"sharpe_ratio": 1.25,
	"sortino_ratio": 1.9,
	"total_orders": 2
}`

// trackingRunner stands in for the default go kernel and counts invocations, so
// a test can prove the request never fell back to it.
type trackingRunner struct {
	mu    sync.Mutex
	calls int
}

func (r *trackingRunner) RunBacktest(context.Context, contracts.BacktestRequest) (*contracts.BacktestResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return &contracts.BacktestResponse{ID: "go-kernel-product", Status: contracts.StatusCompleted, Strategy: "from-go"}, nil
}

func (r *trackingRunner) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// newNautilusRouter wires go (tracking stub) + nautilus (real Runner → sidecarURL)
// into a registry behind the real /api/backtest handler.
func newNautilusRouter(t *testing.T, sidecarURL string) (*gin.Engine, *memJobStore, *trackingRunner) {
	t.Helper()
	eng := newReadPathEngine(t)
	store := newMemJobStore()
	goKernel := &trackingRunner{}

	reg := contracts.NewEngineRegistry()
	reg.Register(contracts.DefaultKernel, goKernel)
	reg.Register(nautilus.KernelName, nautilus.NewRunner(sidecarURL, nil))

	jobSvc := backtest.NewJobService(store, reg, zerolog.Nop())
	r := gin.New()
	registerBacktestRoutes(r, eng, reg, jobSvc, nil, zerolog.Nop())
	return r, store, goKernel
}

func stubSidecar(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, ntSidecarCompleted)
	}))
	t.Cleanup(srv.Close)
	return srv
}

const nautilusPostBody = `{"strategy":"buy_and_hold","stock_pool":["600000.SH","000001.SZ"],"start_date":"2024-01-02","end_date":"2024-06-28","kernel":"nautilus"}`

// 腿 1（正证据 · 同步）：kernel=nautilus → 走侧车，200 带侧车产物且落库。
func TestBacktestPost_NautilusKernelRunsAndPersists(t *testing.T) {
	srv := stubSidecar(t)
	r, store, goKernel := newNautilusRouter(t, srv.URL)

	w := postBacktestKernel(t, r, nautilusPostBody)

	require.Equal(t, http.StatusOK, w.Code, "nautilus 内核必须 200, body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "nt-run-k13", "响应必须是侧车产物")
	assert.Equal(t, 0, goKernel.callCount(), "go 内核不得被触碰")

	// 落库同 schema：经 SaveSyncResult 持久化，result JSON 带侧车 id 与指标。
	require.Equal(t, 1, store.count(), "侧车结果必须落库（同一 JobStore 路径）")
	stored, err := store.GetBacktestJob(context.Background(), "nt-run-k13")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, "completed", stored["status"])
	resultJSON, _ := stored["result"].([]byte)
	assert.Contains(t, string(resultJSON), "nt-run-k13", "落库的必须是侧车产物")
	assert.Contains(t, string(resultJSON), "0.042", "NT 的指标必须原样透传")
}

// 腿 2（反证腿 · 侧车未启动）：kernel=nautilus（已注册但不可达）→ 显式错误
// （5xx）+ 库内零行 + go 内核零调用 —— 不得静默回退。
func TestBacktestPost_NautilusSidecarDownFailClosed(t *testing.T) {
	srv := stubSidecar(t)
	deadURL := srv.URL
	srv.Close() // 侧车进程停止

	r, store, goKernel := newNautilusRouter(t, deadURL)

	w := postBacktestKernel(t, r, nautilusPostBody)

	// 5xx：侧车不可用（ErrCodeUnavailable → 503）。关键是非 2xx —— 调用方
	// 必须知道自己点名的内核没跑，而不是收到一份默认内核算出来的结果。
	require.GreaterOrEqual(t, w.Code, 500, "侧车不可用必须显式失败, body=%s", w.Body.String())
	assert.NotEqual(t, http.StatusOK, w.Code)

	// 反证本体：库内零行 + go 内核零调用。若该请求回退 go，这里会出现一个
	// go 产物（trackingRunner 会返回 "go-kernel-product" 并落库）。
	assert.Equal(t, 0, store.count(), "被拒请求不得在库内留下任何内核产物")
	assert.Equal(t, 0, goKernel.callCount(), "不得静默回退到 go 内核")
}

// 腿 3（回归腿）：不带 kernel → 仍走 go 内核（trackingRunner 被调用）。
// 确认 K13 的注册没有改变默认路由。
func TestBacktestPost_NautilusRegistrationKeepsGoDefault(t *testing.T) {
	srv := stubSidecar(t)
	r, _, goKernel := newNautilusRouter(t, srv.URL)

	w := postBacktestKernel(t, r,
		`{"strategy":"buy_and_hold","stock_pool":["600000.SH"],"start_date":"2024-01-02","end_date":"2024-06-28"}`)

	require.Equal(t, http.StatusOK, w.Code, "不带 kernel 必须走 go, body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "from-go")
	assert.Equal(t, 1, goKernel.callCount())
}

// 腿 4（参数维度 · fail-closed）：nautilus 未登记任何 engine_params →
// 未知名一律 400，与 go 内核同一道 ValidateRunSpec 闸门。
func TestBacktestPost_NautilusRejectsUnknownEngineParams(t *testing.T) {
	srv := stubSidecar(t)
	r, _, goKernel := newNautilusRouter(t, srv.URL)

	w := postBacktestKernel(t, r,
		`{"strategy":"buy_and_hold","stock_pool":["600000.SH"],"start_date":"2024-01-02","end_date":"2024-06-28","kernel":"nautilus","engine_params":{"slippage":1}}`)

	require.Equal(t, http.StatusBadRequest, w.Code, "未知名 engine_params 必须 400, body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "slippage")
	assert.Contains(t, w.Body.String(), "rejected, not ignored")
	assert.Equal(t, 0, goKernel.callCount(), "参数被拒时不得有任何内核执行")
}

// 腿 5（E2E · 环境门）：NT_SIDECAR_URL 指向真实侧车时，经真库跑通并落库。
//
// 默认 `go test ./...` 会 skip（未设环境变量）；显式跑：
//
//	NT_SIDECAR_URL=http://127.0.0.1:9100 \
//	NT_E2E_SYMBOLS=600000.SH NT_E2E_START=2024-01-02 NT_E2E_END=2024-12-31 \
//	go test ./cmd/analysis/ -run NautilusKernelEndToEnd -v
func TestBacktestPost_NautilusKernelEndToEnd(t *testing.T) {
	sidecarURL := strings.TrimSpace(os.Getenv(nautilus.URLEnvVar))
	if sidecarURL == "" {
		t.Skipf("%s not set — start nt-sidecar and set it to run the end-to-end leg", nautilus.URLEnvVar)
	}

	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@127.0.0.1:5432/quant_trading?sslmode=disable"
	}
	store, err := storage.NewPostgresStore(context.Background(), dsn)
	if err != nil {
		t.Skipf("skipping: no reachable Postgres for the end-to-end leg (%v)", err)
	}
	t.Cleanup(store.Close)

	reg := contracts.NewEngineRegistry()
	reg.Register(contracts.DefaultKernel, &trackingRunner{})
	reg.Register(nautilus.KernelName, nautilus.NewRunner(sidecarURL, nil))
	jobSvc := backtest.NewJobService(store, reg, zerolog.Nop())

	eng := newReadPathEngine(t)
	r := gin.New()
	registerBacktestRoutes(r, eng, reg, jobSvc, nil, zerolog.Nop())

	symbols := os.Getenv("NT_E2E_SYMBOLS")
	if symbols == "" {
		symbols = "600000.SH"
	}
	start := os.Getenv("NT_E2E_START")
	if start == "" {
		start = "2024-01-02"
	}
	end := os.Getenv("NT_E2E_END")
	if end == "" {
		end = "2024-12-31"
	}

	body := map[string]any{
		"strategy":   "buy_and_hold",
		"stock_pool": strings.Split(symbols, ","),
		"start_date": start,
		"end_date":   end,
		"kernel":     nautilus.KernelName,
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)

	w := postBacktestKernel(t, r, string(raw))
	require.Equal(t, http.StatusOK, w.Code, "真实侧车必须跑通, body=%s", w.Body.String())

	var resp contracts.BacktestResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, contracts.StatusCompleted, resp.Status)
	require.NotEmpty(t, resp.ID, "侧车必须返回 run id")
	t.Cleanup(func() {
		if _, err := store.DB().Exec(context.Background(),
			`DELETE FROM backtest_jobs WHERE id = $1`, resp.ID); err != nil {
			t.Logf("cleanup: 删除 backtest_jobs(%s) 失败: %v", resp.ID, err)
		}
	})

	// 落库断言：真库里能读到这次运行，且带 NT 的指标。
	stored, err := store.GetBacktestJob(context.Background(), resp.ID)
	require.NoError(t, err)
	require.NotNil(t, stored, "运行结果必须落库")
	assert.Equal(t, "completed", stored["status"])
	resultJSON, _ := stored["result"].([]byte)
	assert.Contains(t, string(resultJSON), resp.ID)
	t.Logf("E2E ok: id=%s total_return=%.6f sharpe=%.4f",
		resp.ID, resp.TotalReturn, resp.SharpeRatio)
}
