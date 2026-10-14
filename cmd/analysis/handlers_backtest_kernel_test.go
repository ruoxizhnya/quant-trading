// K10 · handler 层的内核路由验收腿（POST /api/backtest 两条分支）。
//
// 覆盖任务验收的 HTTP 维度：
//  1. 正证据（同步）：注册 fake → kernel=fake 走 fake，200 响应带 fake
//     marker，且经 SaveSyncResult 落库（同 schema 同路径）；
//  2. 反证腿（同步）：kernel=nautilus（未注册）→ 400 点名内核名，
//     且**库内零行** —— 没有任何内核产物（证明没有静默回退 go）；
//  3. 回归腿（同步）：不带 kernel → 请求穿过路由进入 go 引擎（以引擎
//     的「日历未同步」409 收场为证），与 K10 前行为一致；
//  4. 正证据（异步）：kernel=fake → 202 受理，job 完成且 result 带
//     fake marker（落库同 schema）；
//  5. 反证腿（异步）：kernel=nautilus → 202 受理（路由裁定在执行期），
//     job 终态 failed + error_msg 点名内核，result 为空；
//  6. 参数维度 fail-closed（异步）：engine_params 未知名 → 400 点名，
//     与同步路径同一道 ValidateRunSpec 闸门（K9 原则不放松）。
//
// 破坏验证（开发期实测）：把 EngineRegistry.Resolve 改成「未知名回退
// go」，腿 2 的 400/零行断言与腿 5 的 error_msg 断言立刻红（已还原）。

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
)

// kernelFakeRunner 带 marker 的 EngineRunner stub —— 产物响应写 marker，
// 用于证明「这次是 fake 跑的，不是 go」。
type kernelFakeRunner struct {
	mu    sync.Mutex
	reqs  []contracts.BacktestRequest
	calls int
}

func (f *kernelFakeRunner) RunBacktest(ctx context.Context, req contracts.BacktestRequest) (*contracts.BacktestResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.reqs = append(f.reqs, req)
	return &contracts.BacktestResponse{
		ID:       "bt-fake",
		Status:   contracts.StatusCompleted,
		Strategy: "from-fake",
	}, nil
}

func (f *kernelFakeRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// memJobStore 是 backtest.JobStore 的内存实现（handler 层测试专用）。
// 与 pkg/backtest/job 的 mockJobStore 同构：读路径返回深拷贝，
// 避免 job goroutine 与测试 goroutine 共享可变 map（P0-6 的教训）。
type memJobStore struct {
	mu    sync.Mutex
	jobs  map[string]map[string]any
	order []string
}

func newMemJobStore() *memJobStore {
	return &memJobStore{jobs: make(map[string]map[string]any)}
}

func cloneJobMapForTest(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		switch x := v.(type) {
		case map[string]any:
			dst[k] = cloneJobMapForTest(x)
		case []byte:
			cp := make([]byte, len(x))
			copy(cp, x)
			dst[k] = cp
		case time.Time:
			dst[k] = x
		case *time.Time:
			if x != nil {
				t := *x
				dst[k] = &t
			}
		default:
			dst[k] = v
		}
	}
	return dst
}

func (m *memJobStore) CreateBacktestJob(ctx context.Context, job map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, _ := job["id"].(string)
	m.jobs[id] = cloneJobMapForTest(job)
	m.order = append(m.order, id)
	return nil
}

func (m *memJobStore) UpdateJobStarted(ctx context.Context, jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[jobID]; ok {
		j["status"] = "running"
		now := time.Now()
		j["started_at"] = now
	}
	return nil
}

func (m *memJobStore) UpdateJobCompleted(ctx context.Context, jobID string, result []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[jobID]; ok {
		j["status"] = "completed"
		j["result"] = result
		now := time.Now()
		j["completed_at"] = now
	}
	return nil
}

func (m *memJobStore) UpdateJobFailed(ctx context.Context, jobID string, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[jobID]; ok {
		j["status"] = "failed"
		j["error_msg"] = errMsg
	}
	return nil
}

func (m *memJobStore) GetBacktestJob(ctx context.Context, jobID string) (map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok {
		return nil, nil
	}
	return cloneJobMapForTest(j), nil
}

func (m *memJobStore) ListBacktestJobs(ctx context.Context, limit int) ([]map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []map[string]any
	for i := len(m.order) - 1; i >= 0 && len(out) < limit; i-- {
		if j, ok := m.jobs[m.order[i]]; ok {
			out = append(out, cloneJobMapForTest(j))
		}
	}
	return out, nil
}

func (m *memJobStore) DeleteBacktestJob(ctx context.Context, jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.jobs, jobID)
	for i, id := range m.order {
		if id == jobID {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	return nil
}

func (m *memJobStore) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.jobs)
}

// newBacktestKernelRouter 装配：go 内核 = 内存引擎（newReadPathEngine），
// fake 内核 = kernelFakeRunner；job 服务持同一注册表。返回 router、
// store（落库断言用）与 fake runner（调用断言用）。
func newBacktestKernelRouter(t *testing.T) (*gin.Engine, *memJobStore, *kernelFakeRunner) {
	t.Helper()
	eng := newReadPathEngine(t)
	store := newMemJobStore()
	fake := &kernelFakeRunner{}
	reg := contracts.NewEngineRegistry()
	reg.Register(contracts.DefaultKernel, eng)
	reg.Register("fake", fake)
	jobSvc := backtest.NewJobService(store, reg, zerolog.Nop())
	r := gin.New()
	registerBacktestRoutes(r, eng, reg, jobSvc, nil, zerolog.Nop())
	return r, store, fake
}

func postBacktestKernel(t *testing.T, r *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/backtest", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// waitKernelJobSettled 轮询直到 job 离开 pending/running（job 在后台
// goroutine 执行；testify Eventually 内部是 ticker，不是裸 sleep）。
func waitKernelJobSettled(t *testing.T, store *memJobStore, jobID string) map[string]any {
	t.Helper()
	var got map[string]any
	require.Eventually(t, func() bool {
		m, err := store.GetBacktestJob(context.Background(), jobID)
		if err != nil || m == nil {
			return false
		}
		s, _ := m["status"].(string)
		if s == "completed" || s == "failed" {
			got = m
			return true
		}
		return false
	}, 5*time.Second, 10*time.Millisecond, "job 未在预期时间内收尾")
	return got
}

// 腿 1（正证据 · 同步）：kernel=fake → 走 fake，响应带 marker 且落库。
func TestBacktestPost_FakeKernelRunsAndPersists(t *testing.T) {
	r, store, fake := newBacktestKernelRouter(t)

	w := postBacktestKernel(t, r, `{"strategy":"momentum","stock_pool":["600000.SH"],"start_date":"2024-01-02","end_date":"2024-02-02","kernel":"fake"}`)

	require.Equal(t, http.StatusOK, w.Code, "fake 内核的同步请求必须 200, body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "from-fake", "响应必须是 fake 的产物")
	assert.Equal(t, 1, fake.callCount(), "fake runner 必须被调用一次")

	// 落库同 schema：经 SaveSyncResult 持久化，result JSON 带 fake marker。
	require.Equal(t, 1, store.count(), "fake 结果必须落库（同一 JobStore 路径）")
	stored, err := store.GetBacktestJob(context.Background(), "bt-fake")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, "completed", stored["status"])
	resultJSON, _ := stored["result"].([]byte)
	assert.Contains(t, string(resultJSON), "from-fake", "落库的必须是 fake 的产物")
}

// 腿 2（反证腿 · 同步）：kernel=nautilus（未注册）→ 400 点名内核名，
// 库内零行 —— 没有任何内核产物，证明没有静默回退 go。
func TestBacktestPost_UnknownKernelFailClosed(t *testing.T) {
	r, store, fake := newBacktestKernelRouter(t)

	w := postBacktestKernel(t, r, `{"strategy":"momentum","stock_pool":["600000.SH"],"start_date":"2024-01-02","end_date":"2024-02-02","kernel":"nautilus"}`)

	require.Equal(t, http.StatusBadRequest, w.Code, "未注册内核必须 400, body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "nautilus", "错误必须点名被拒的内核名")
	assert.Contains(t, w.Body.String(), "not registered", "错误必须说明未注册")

	// 反证本体：库内零行 + fake 零调用。若路由回退 go，这里会出现一个
	// go 引擎产物（内存引擎会以日历错误收场并落库 failed 行）。
	assert.Equal(t, 0, store.count(), "被拒请求不得在库内留下任何内核产物")
	assert.Equal(t, 0, fake.callCount(), "fake runner 与本腿无关")
}

// 腿 3（回归腿 · 同步）：不带 kernel → 经 EffectiveKernel 解析到 go，
// 请求穿过路由进入引擎（内存引擎无日历 → 409），与 K10 前行为一致。
func TestBacktestPost_NoKernelStillReachesGoEngine(t *testing.T) {
	r, store, _ := newBacktestKernelRouter(t)

	w := postBacktestKernel(t, r, `{"strategy":"momentum","stock_pool":["600000.SH"],"start_date":"2024-01-02","end_date":"2024-02-02"}`)

	// 409 = 引擎的「日历未同步」分类错误 —— 请求确实穿过了路由闸门
	// 进入了 go 引擎（而不是被路由层以 400 拦死）。
	require.Equal(t, http.StatusConflict, w.Code, "不带 kernel 必须到达 go 引擎（日历错误 409）, body=%s", w.Body.String())
	assert.Equal(t, 0, store.count(), "引擎执行失败不得落库（SaveSyncResult 只在成功路径）")
}

// 腿 4（正证据 · 异步）：kernel=fake → 202 受理，job 完成且落库带
// fake marker（与 go 内核同一条 UpdateJobCompleted 路径）。
func TestBacktestPost_AsyncKernelFakeCompletes(t *testing.T) {
	r, store, fake := newBacktestKernelRouter(t)

	w := postBacktestKernel(t, r, `{"strategy_id":"momentum","universe":"600000.SH,600001.SH","start_date":"2024-01-02","end_date":"2024-02-02","kernel":"fake"}`)

	require.Equal(t, http.StatusAccepted, w.Code, "带 kernel 的异步请求必须受理, body=%s", w.Body.String())
	var created struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.NotEmpty(t, created.JobID)

	got := waitKernelJobSettled(t, store, created.JobID)
	assert.Equal(t, "completed", got["status"], "fake 内核的 job 必须 completed")
	assert.Equal(t, 1, fake.callCount(), "fake runner 必须被调用一次")

	resultJSON, _ := got["result"].([]byte)
	assert.Contains(t, string(resultJSON), "from-fake", "落库的必须是 fake 的产物")
	var params map[string]any
	require.NoError(t, json.Unmarshal(got["params"].([]byte), &params))
	assert.Equal(t, "fake", params["kernel"], "kernel 必须经 params 保留键落库")
}

// 腿 5（反证腿 · 异步）：kernel=nautilus（未注册）→ 202 受理（路由
// 裁定在执行期），job 终态 failed + error_msg 点名内核，result 为空。
func TestBacktestPost_AsyncUnknownKernelJobFails(t *testing.T) {
	r, store, fake := newBacktestKernelRouter(t)

	w := postBacktestKernel(t, r, `{"strategy_id":"momentum","universe":"600000.SH,600001.SH","start_date":"2024-01-02","end_date":"2024-02-02","kernel":"nautilus"}`)

	require.Equal(t, http.StatusAccepted, w.Code, "路由裁定在执行期，创建请求本身受理, body=%s", w.Body.String())
	var created struct {
		JobID string `json:"job_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.NotEmpty(t, created.JobID)

	got := waitKernelJobSettled(t, store, created.JobID)
	assert.Equal(t, "failed", got["status"], "未注册内核的 job 必须 failed")
	errMsg, _ := got["error_msg"].(string)
	assert.Contains(t, errMsg, "nautilus", "error_msg 必须点名被拒的内核名")
	assert.Contains(t, errMsg, "not registered")
	assert.Empty(t, got["result"], "库内不得留下任何内核产物")
	assert.Equal(t, 0, fake.callCount(), "fake runner 与本腿无关")
}

// 腿 6（参数维度 · 异步）：engine_params 未知名 → 400 点名，与同步路径
// 同一道 ValidateRunSpec 闸门 —— K9 的 "rejected, not ignored" 原则在
// 内核路由接通后不放松。
func TestBacktestPost_AsyncRejectsUnknownEngineParams(t *testing.T) {
	r, _, _ := newBacktestKernelRouter(t)

	for _, tc := range []struct{ name, body, wantKey string }{
		{
			"go kernel unknown param",
			`{"strategy_id":"momentum","universe":"hs300","start_date":"2024-01-02","end_date":"2024-02-02","kernel":"go","engine_params":{"not_a_param":1}}`,
			"not_a_param",
		},
		{
			"fake kernel accepts no params",
			`{"strategy_id":"momentum","universe":"hs300","start_date":"2024-01-02","end_date":"2024-02-02","kernel":"fake","engine_params":{"anything":1}}`,
			"anything",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := postBacktestKernel(t, r, tc.body)
			require.Equal(t, http.StatusBadRequest, w.Code, "未知名 engine_params 必须 400, body=%s", w.Body.String())
			assert.Contains(t, w.Body.String(), tc.wantKey, "错误必须点名未知键")
			assert.Contains(t, w.Body.String(), "rejected, not ignored", "必须保持 fail-closed 拒绝措辞")
		})
	}
}
