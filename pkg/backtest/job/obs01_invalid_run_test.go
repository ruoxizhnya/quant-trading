package job

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// OBS-01：作业层不得把无效运行持久化成 completed。

// invalidRunner 返回一次「无效运行」的响应（空票池 / 0 成交）。
type invalidRunner struct{}

func (invalidRunner) RunBacktest(ctx context.Context, req contracts.BacktestRequest) (*contracts.BacktestResponse, error) {
	return &contracts.BacktestResponse{
		Status:          "invalid",
		InvalidReasons:  []string{"empty_universe", "zero_trades"},
		UniverseMaxSize: intPtr(0),
		TotalTrades:     0,
	}, nil
}

func waitJobSettled(t *testing.T, store *mockJobStore, jobID string) map[string]any {
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
	}, 3*time.Second, 10*time.Millisecond, "作业未在预期时间内收尾")
	return got
}

func TestJobService_InvalidRunMarkedFailed(t *testing.T) {
	store := newMockJobStore()
	svc := NewJobService(store, invalidRunner{}, zerolog.Nop())

	job, err := svc.CreateJob(context.Background(), CreateJobRequest{
		StrategyID: "momentum",
		Universe:   "CSI300",
		StartDate:  "2024-01-02",
		EndDate:    "2024-02-02",
	})
	require.NoError(t, err)

	got := waitJobSettled(t, store, job.ID)
	assert.Equal(t, "failed", got["status"], "无效运行不得持久化成 completed")
	errMsg, _ := got["error_msg"].(string)
	assert.Contains(t, errMsg, "invalid run")
	assert.Contains(t, errMsg, "empty_universe")
}

func TestJobService_ValidRunStillCompleted(t *testing.T) {
	store := newMockJobStore()
	svc := NewJobService(store, &fakeRunner{}, zerolog.Nop())

	job, err := svc.CreateJob(context.Background(), CreateJobRequest{
		StrategyID: "momentum",
		Universe:   "600000.SH,600001.SH",
		StartDate:  "2020-01-01",
		EndDate:    "2023-12-31",
	})
	require.NoError(t, err)

	got := waitJobSettled(t, store, job.ID)
	assert.Equal(t, "completed", got["status"], "正常作业照旧 completed —— 护栏不误伤")
}

// intPtr 返回 int 的指针（OBS-01 审查修复：UniverseMaxSize 改为 *int，
// nil = 未评估、0 = 明确为空 —— 未知不等于空）。
func intPtr(v int) *int { return &v }
