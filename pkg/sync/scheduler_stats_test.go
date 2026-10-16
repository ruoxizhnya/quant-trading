package sync

// scheduler_stats_test.go —— SYNC-04 的可观测腿。
//
// 守什么：`Stats()` 必须能区分「`Start()` 启动失败」与「启动成功但确实没有
// active 排程」。现状两种情形的旧字段完全同形（active_schedules=0 /
// total_entries=0 / next_runs 空 —— loadAndScheduleAll 报错时提前返回，
// cron.Start() 根本没执行），前端只能显示一个无法被当场质问的 0/0。
//
// 判据是新增的 is_running / start_error；两条旧计数被断言**两组都是 0**，
// 这钉住工单④「既有字段不改名、不改语义」——防止有人反过来靠改计数
// 的语义来"造出"可区分性。

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errListScheduleStore 让 ListSyncSchedules 恒失败，其余方法走内嵌的内存实现。
// 失败注入点选在 loadAndScheduleAll 的第一个 store 调用上，与生产里
// 「库不可达/读排程报错」的形状一致。
type errListScheduleStore struct {
	*fakeScheduleStore
	err error
}

func (e *errListScheduleStore) ListSyncSchedules(_ context.Context, _ bool) ([]*Schedule, error) {
	return nil, e.err
}

// flakyListScheduleStore 只在第一次 ListSyncSchedules 时失败 ——
// 用于「重试成功后旧原因必须被清空」这条腿。
type flakyListScheduleStore struct {
	*fakeScheduleStore
	failNext bool
}

func (f *flakyListScheduleStore) ListSyncSchedules(_ context.Context, activeOnly bool) ([]*Schedule, error) {
	if f.failNext {
		f.failNext = false
		return nil, errors.New("transient: connection reset")
	}
	return f.fakeScheduleStore.ListSyncSchedules(context.Background(), activeOnly)
}

// TestStatsDistinguishesStartFailureFromEmptySchedules 是本单的主验收（⑤.1）：
// 失败组与对照组的核心判据是「两组 stats 可区分」，且区分只能来自新字段。
func TestStatsDistinguishesStartFailureFromEmptySchedules(t *testing.T) {
	// 失败组：读 active 排程报错 ⇒ Start() 返回 error，cron 未启动。
	failing := &errListScheduleStore{
		fakeScheduleStore: newFakeScheduleStore(),
		err:               errors.New("db down: connection refused"),
	}
	sFail := NewScheduler(failing, NewQueue(newMockJobStore()))
	startErr := sFail.Start()
	require.Error(t, startErr, "前置事实：store 读排程失败必须让 Start() 返回错误（不吞掉）")

	// 对照组：store 正常，只是没有任何 active 排程。
	sOK := NewScheduler(newFakeScheduleStore(), NewQueue(newMockJobStore()))
	require.NoError(t, sOK.Start())
	defer sOK.Stop()

	bad, good := sFail.Stats(), sOK.Stats()

	// 旧字段两组同形（0/0）—— 这正是 SYNC-04 要消灭的状态；钉住它，
	// 防止「把 total_entries 改成 -1」之类的伪修复。
	assert.Equal(t, 0, bad.ActiveSchedules)
	assert.Equal(t, 0, bad.TotalEntries)
	assert.Empty(t, bad.NextRuns)
	assert.Equal(t, good.ActiveSchedules, bad.ActiveSchedules, "旧计数字段语义不得被挪用（工单④）")
	assert.Equal(t, good.TotalEntries, bad.TotalEntries, "旧计数字段语义不得被挪用（工单④）")

	// 新字段必须把两组分开 —— 核心判据。
	assert.False(t, bad.IsRunning, "Start() 失败的调度器不能报运行中")
	require.NotEmpty(t, bad.StartError, "启动失败必须带原因，否则 0/0 依旧不可质问")
	assert.Contains(t, bad.StartError, "db down: connection refused",
		"start_error 必须承载底层原因文本，不是占位描述")

	assert.True(t, good.IsRunning, "Start() 成功后必须报运行中")
	assert.Empty(t, good.StartError, "运行中的调度器不该带失败原因")
}

// TestStartAfterFailureClearsStartError 钉住工单③.2 的成功路径语义：
// 重试成功后「置运行中」与「清空原因」必须同时发生 —— 只置其一，
// 界面就会永远挂着一个已经过期的失败理由。
func TestStartAfterFailureClearsStartError(t *testing.T) {
	store := &flakyListScheduleStore{fakeScheduleStore: newFakeScheduleStore(), failNext: true}
	s := NewScheduler(store, NewQueue(newMockJobStore()))

	require.Error(t, s.Start())
	require.NotEmpty(t, s.Stats().StartError, "前置事实：首次启动失败已记录原因")

	require.NoError(t, s.Start())
	defer s.Stop()

	stats := s.Stats()
	assert.True(t, stats.IsRunning)
	assert.Empty(t, stats.StartError, "重试成功后必须清空旧失败原因")
}

// TestStopClearsIsRunning 把 is_running 钉成**活性**读数而不是「最后一次
// Start 成功过」—— 工单②要求照抄的形状是 `WorkerStats.IsRunning`
// （`wp.ctx.Err() == nil`，Stop 后即假）。cron 已停还报运行中，字段名
// 就骗了读它的人，而「每个数字可被当场质问」正是本单的立单理由。
func TestStopClearsIsRunning(t *testing.T) {
	s := NewScheduler(newFakeScheduleStore(), NewQueue(newMockJobStore()))
	require.NoError(t, s.Start())
	require.True(t, s.Stats().IsRunning, "前置事实：Start 成功后报运行中")

	s.Stop()

	stats := s.Stats()
	assert.False(t, stats.IsRunning, "Stop 之后不能再报运行中 —— is_running 必须反映活性")
	assert.Empty(t, stats.StartError, "正常停止不是启动失败，不该凭空造出一个 start_error")
}
