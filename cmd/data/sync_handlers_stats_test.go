package main

// sync_handlers_stats_test.go —— SYNC-04 的 HTTP 面腿（工单⑤.2）。
//
// 守什么：GET /api/sync/scheduler/stats 的**实际 JSON** 必须带上
// is_running / start_error —— 只改 struct 不改透传等于没改。
// 走真实的 SyncHandler.StartScheduler + getSchedulerStatsHandler 路径，
// 不直接调 Scheduler.Stats()（那是 pkg/sync 的腿）。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/sync"
)

// statsScheduleStore 是最小的 ScheduleStore 替身：listErr 非空时
// ListSyncSchedules 失败（模拟启动时读不到排程），否则返回空 active 列表。
type statsScheduleStore struct {
	listErr error
}

func (s *statsScheduleStore) CreateSyncSchedule(context.Context, *sync.Schedule) error { return nil }
func (s *statsScheduleStore) GetSyncSchedule(context.Context, int) (*sync.Schedule, error) {
	return nil, nil
}
func (s *statsScheduleStore) GetSyncScheduleByName(context.Context, string) (*sync.Schedule, error) {
	return nil, nil
}
func (s *statsScheduleStore) UpdateSyncSchedule(context.Context, *sync.Schedule) error {
	return nil
}
func (s *statsScheduleStore) DeleteSyncSchedule(context.Context, int) error { return nil }
func (s *statsScheduleStore) ListSyncSchedules(context.Context, bool) ([]*sync.Schedule, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return nil, nil
}

// statsJobStore 是 Queue 需要的 JobStore 空实现；stats 路径不产生作业。
type statsJobStore struct{}

func (statsJobStore) CreateSyncJob(context.Context, *sync.Job) error { return nil }
func (statsJobStore) GetSyncJob(context.Context, string) (*sync.Job, error) {
	return nil, nil
}
func (statsJobStore) UpdateSyncJobIfStatus(context.Context, *sync.Job, ...sync.JobStatus) (bool, error) {
	return false, nil
}
func (statsJobStore) ListSyncJobs(context.Context, sync.JobStatus, int) ([]*sync.Job, error) {
	return nil, nil
}
func (statsJobStore) ListSyncJobsByType(context.Context, sync.JobType, int) ([]*sync.Job, error) {
	return nil, nil
}
func (statsJobStore) DeleteSyncJob(context.Context, string) error { return nil }

func statsRouter(h *SyncHandler) *gin.Engine {
	// gin 的 mode 由 TestMain 设一次（AUD-28）—— 别在这里调 gin.SetMode。
	router := gin.New()
	router.GET("/api/sync/scheduler/stats", h.getSchedulerStatsHandler)
	return router
}

func getSchedStatsJSON(t *testing.T, h *SyncHandler) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	router := statsRouter(h)
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/sync/scheduler/stats", nil))
	require.Equal(t, http.StatusOK, w.Code, "stats 端点必须原样 200（本单不改状态码语义）")
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body),
		"响应必须是 JSON object —— 现有前端按 object 解析，不得改包装形状（工单④）")
	// 旧字段必须还在且仍是数字（防改名 / 防挪用）。
	for _, k := range []string{"active_schedules", "total_entries"} {
		_, ok := body[k].(float64)
		assert.Truef(t, ok, "旧字段 %s 缺席或类型变了（工单④：不改名不改语义）", k)
	}
	return body
}

// TestSchedulerStatsHandler_FailedStartReportsReason 钉住失败成因可见：
// StartScheduler 失败后，响应体报未运行 + 底层原因文本。
func TestSchedulerStatsHandler_FailedStartReportsReason(t *testing.T) {
	injected := errors.New("stats test: schedules unreadable")
	h := &SyncHandler{scheduler: sync.NewScheduler(&statsScheduleStore{listErr: injected}, sync.NewQueue(statsJobStore{}))}
	require.Error(t, h.StartScheduler(), "前置事实：注入的读失败必须让 StartScheduler 报错（fail-soft 不吞错）")

	body := getSchedStatsJSON(t, h)
	running, ok := body["is_running"].(bool)
	require.True(t, ok, "实际 JSON 必须含布尔字段 is_running（handler 透传，不是只改了 struct）")
	assert.False(t, running, "启动失败的调度器在 HTTP 面不能报运行中")
	startErr, ok := body["start_error"].(string)
	require.True(t, ok, "启动失败时 JSON 必须带 start_error")
	assert.Contains(t, startErr, "stats test: schedules unreadable",
		"start_error 必须透传底层原因，供前端把「为什么是 0/0」答出来")
}

// TestSchedulerStatsHandler_RunningEmptyDistinguishable 是对照腿：
// 启动成功 + 没有 active 排程 ⇒ is_running=true、start_error 缺席（omitempty），
// 与失败组的 JSON 可区分 —— 旧字段两组同形（0/0），这正是本单存在的理由。
func TestSchedulerStatsHandler_RunningEmptyDistinguishable(t *testing.T) {
	failedH := &SyncHandler{scheduler: sync.NewScheduler(
		&statsScheduleStore{listErr: errors.New("boom")}, sync.NewQueue(statsJobStore{}))}
	require.Error(t, failedH.StartScheduler())

	okH := &SyncHandler{scheduler: sync.NewScheduler(
		&statsScheduleStore{}, sync.NewQueue(statsJobStore{}))}
	require.NoError(t, okH.StartScheduler())
	defer okH.StopScheduler()

	failedBody, okBody := getSchedStatsJSON(t, failedH), getSchedStatsJSON(t, okH)

	assert.Equal(t, failedBody["active_schedules"], okBody["active_schedules"],
		"前置事实：旧计数两组仍同形（0）—— 区分只能来自新字段")
	assert.Equal(t, failedBody["total_entries"], okBody["total_entries"])

	running, has := okBody["is_running"].(bool)
	require.True(t, has)
	assert.True(t, running, "启动成功必须报运行中")
	assert.NotContains(t, okBody, "start_error",
		"运行中不应出现 start_error（omitempty），也不能塞一个空字符串必填字段破坏现有解析（工单④）")
	assert.NotEqual(t, failedBody["is_running"], okBody["is_running"],
		"两组 stats 的 JSON 必须可区分 —— 这是 SYNC-04 的核心判据")
}
