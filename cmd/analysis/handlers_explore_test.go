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
	"github.com/ruoxizhnya/quant-trading/pkg/ai/loop"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/pipeline"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeExploreRunner 顶替真实 pipeline。block 非 nil 时会卡住每次尝试，
// 用来观察「正在跑」这个中间状态，以及叫停到底管不管用。
type fakeExploreRunner struct {
	mu    sync.Mutex
	seqs  []int
	block chan struct{}
}

func (f *fakeExploreRunner) Execute(ctx context.Context, description string, runner pipeline.BacktestRunner) (*pipeline.Result, error) {
	ec := pipeline.ExperimentContextFrom(ctx)
	f.mu.Lock()
	f.seqs = append(f.seqs, ec.Seq)
	f.mu.Unlock()

	if f.block != nil {
		<-f.block
	}
	return &pipeline.Result{
		ID:             "job",
		Status:         pipeline.StageComplete,
		ExperimentID:   int64(ec.Seq + 1),
		BacktestResult: &domain.BacktestResult{SharpeRatio: float64(ec.Seq) * 0.5},
	}, nil
}

func (f *fakeExploreRunner) seen() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]int, len(f.seqs))
	copy(out, f.seqs)
	return out
}

func newExploreRouter(h *ExploreHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h.RegisterExploreRoutes(r.Group("/api"))
	return r
}

func postJSON(r *gin.Engine, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func startRun(t *testing.T, r *gin.Engine, body string) string {
	t.Helper()
	w := postJSON(r, "/api/explore/runs", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	runID, _ := resp["run_id"].(string)
	require.NotEmpty(t, runID)
	return runID
}

// waitDone 轮询直到这一轮跑完（或超时）。异步接口只能这么测。
func waitDone(t *testing.T, r *gin.Engine, runID string) ExploreStatusResponse {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var st ExploreStatusResponse
	for time.Now().Before(deadline) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/explore/runs/"+runID, nil)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &st))
		if !st.Running {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("一轮探索在 5 秒内没跑完，最后状态: %+v", st)
	return st
}

// TestExploreHandler_StartRunsToCompletion：启动 → 轮询 → 拿完整结果。
func TestExploreHandler_StartRunsToCompletion(t *testing.T) {
	tr := &fakeExploreRunner{}
	r := newExploreRouter(NewExploreHandler(tr, nil))

	runID := startRun(t, r, `{"description":"做一个动量策略","max_tries":3}`)
	st := waitDone(t, r, runID)

	assert.Equal(t, runID, st.RunID)
	assert.Equal(t, string(loop.StopExhausted), st.Stopped)
	require.Len(t, st.Attempts, 3)
	for i, a := range st.Attempts {
		assert.Equal(t, i, a.Seq)
		assert.True(t, a.OK)
		assert.NotZero(t, a.ExperimentID, "每次尝试都要落到实验日志里")
	}
	require.NotNil(t, st.BestSeq)
	assert.Equal(t, 2, *st.BestSeq, "Sharpe 递增，最优应是最后一次")
	assert.Equal(t, []int{0, 1, 2}, tr.seen())
}

// TestExploreHandler_StopMidway：叫停要真的停 —— 这是观察页那个按钮的意义。
func TestExploreHandler_StopMidway(t *testing.T) {
	tr := &fakeExploreRunner{block: make(chan struct{})}
	r := newExploreRouter(NewExploreHandler(tr, nil))

	runID := startRun(t, r, `{"description":"做一个动量策略","max_tries":50}`)

	// 等第一次尝试真的卡进去，再叫停。
	require.Eventually(t, func() bool { return len(tr.seen()) == 1 }, 2*time.Second, 10*time.Millisecond)

	w := postJSON(r, "/api/explore/runs/"+runID+"/stop", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	close(tr.block) // 放开当前这次，让循环走到下一步的检查点
	st := waitDone(t, r, runID)

	assert.Equal(t, string(loop.StopCancelled), st.Stopped)
	assert.Len(t, st.Attempts, 1, "叫停后不该再开新的尝试")
	assert.Len(t, tr.seen(), 1, "底层也只该被调用一次")
}

// TestExploreHandler_UnknownRunID：查一个不存在的 run 要 404，不是空结果 ——
// 「没这个 run」和「这个 run 一次都没跑」是两回事。
func TestExploreHandler_UnknownRunID(t *testing.T) {
	r := newExploreRouter(NewExploreHandler(&fakeExploreRunner{}, nil))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/explore/runs/nope", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestExploreHandler_RejectsMissingDescription：缺描述要 400 ——
// 没有方向就没法探索，默默用默认值跑是在浪费一整轮。
func TestExploreHandler_RejectsMissingDescription(t *testing.T) {
	r := newExploreRouter(NewExploreHandler(&fakeExploreRunner{}, nil))

	w := postJSON(r, "/api/explore/runs", `{"max_tries":3}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
