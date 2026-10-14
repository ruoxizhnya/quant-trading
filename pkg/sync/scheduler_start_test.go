package sync

// scheduler_start_test.go —— SYNC-01 的行为腿。
//
// 守什么：`Scheduler.Start()` 必须真的把库里 **active** 的排程接进 cron 触发循环，
// 判据是「队列里出现了由排程产生的作业行」，**不是**「Stats() 的计数变了」。
//
// 为什么判据必须是作业行：`ToggleSchedule` 在激活时会 `cron.AddFunc` 并写进
// `s.entries`，于是 `Stats().ActiveSchedules` **在调度器从未启动的状态下也会变 1**。
// 拿计数当验收的测试在这个缺陷存在时照样绿 —— 那正是本文件要消灭的东西
// （同族教训见 AGENTS.md §14 的 LiveEngine 条目：路径只被测试跑过，从未被真实请求跑过）。
//
// 本包此前 **没有任何** `Start()` 的测试，而生产里也没有调用方（SYNC-01）。

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeScheduleStore 是 ScheduleStore 的内存实现。返回**副本**，镜像真库的语义：
// 调用方拿到的不是存储里那个对象，改它不会串到别的读者（更宽松的 fake 会让测试
// 通过生产并不具备的行为）。
type fakeScheduleStore struct {
	mu     sync.Mutex
	byID   map[int]*Schedule
	nextID int
}

func newFakeScheduleStore(schedules ...*Schedule) *fakeScheduleStore {
	s := &fakeScheduleStore{byID: make(map[int]*Schedule), nextID: 1}
	for _, sc := range schedules {
		s.add(sc)
	}
	return s
}

func (f *fakeScheduleStore) add(sc *Schedule) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if sc.ID == 0 {
		sc.ID = f.nextID
		f.nextID++
	} else if sc.ID >= f.nextID {
		f.nextID = sc.ID + 1
	}
	cp := *sc
	f.byID[cp.ID] = &cp
}

func (f *fakeScheduleStore) CreateSyncSchedule(_ context.Context, schedule *Schedule) error {
	f.add(schedule)
	return nil
}

func (f *fakeScheduleStore) GetSyncSchedule(_ context.Context, id int) (*Schedule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sc, ok := f.byID[id]
	if !ok {
		return nil, nil
	}
	cp := *sc
	return &cp, nil
}

func (f *fakeScheduleStore) GetSyncScheduleByName(ctx context.Context, name string) (*Schedule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, sc := range f.byID {
		if sc.Name == name {
			cp := *sc
			return &cp, nil
		}
	}
	_ = ctx
	return nil, nil
}

func (f *fakeScheduleStore) UpdateSyncSchedule(_ context.Context, schedule *Schedule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *schedule
	f.byID[cp.ID] = &cp
	return nil
}

func (f *fakeScheduleStore) DeleteSyncSchedule(_ context.Context, id int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.byID, id)
	return nil
}

// ListSyncSchedules 的 activeOnly 是承重的：loadAndScheduleAll 靠它只加载启用中的排程。
func (f *fakeScheduleStore) ListSyncSchedules(_ context.Context, activeOnly bool) ([]*Schedule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*Schedule, 0, len(f.byID))
	for _, sc := range f.byID {
		if activeOnly && !sc.IsActive {
			continue
		}
		cp := *sc
		out = append(out, &cp)
	}
	return out, nil
}

func (f *fakeScheduleStore) activeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, sc := range f.byID {
		if sc.IsActive {
			n++
		}
	}
	return n
}

// countScheduledJobs 数出由排程路径产生的作业。用 JobType + Params 做锚点，
// 避免把测试自己塞进 store 的其它作业算进来。
func (m *mockJobStore) countScheduledJobs(jobType JobType) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, j := range m.jobs {
		if j.JobType == jobType {
			n++
		}
	}
	return n
}

func tickSchedule(name string, active bool) *Schedule {
	return &Schedule{
		Name:           name,
		JobType:        "ohlcv",
		CronExpression: "@every 40ms",
		Params:         json.RawMessage(`{"symbols":["600000.SH"]}`),
		IsActive:       active,
	}
}

// TestSchedulerStartFiresActiveSchedule 是 SYNC-01 的主验收：Start() 之后，
// active 排程必须**产生作业行**。
func TestSchedulerStartFiresActiveSchedule(t *testing.T) {
	jobStore := newMockJobStore()
	store := newFakeScheduleStore(tickSchedule("fire-me", true))

	s := NewScheduler(store, NewQueue(jobStore))
	s.RegisterJobType("ohlcv", JobTypeOHLCV)

	require.NoError(t, s.Start(), "Start() 不应失败")
	defer s.Stop()

	require.Eventually(t, func() bool {
		return jobStore.countScheduledJobs(JobTypeOHLCV) > 0
	}, 3*time.Second, 10*time.Millisecond,
		"Start() 之后 3 秒内没有产生任何作业行 —— 排程结构上不会触发（SYNC-01 的缺陷形状）")

	// 计数与 next_run 只能作为**辅助**断言：它们在缺陷状态下也可能为真。
	stats := s.Stats()
	assert.Equal(t, 1, stats.ActiveSchedules, "active 排程应被注册进 cron")
	assert.NotEmpty(t, stats.NextRuns, "cron 已启动却算不出下次执行时间，说明没真正进入调度循环")
}

// TestSchedulerStartSkipsInactiveSchedules 钉住 `ListSyncSchedules(activeOnly=true)`
// 这条语义：未启用的排程不该被加载并跑起来。
func TestSchedulerStartSkipsInactiveSchedules(t *testing.T) {
	jobStore := newMockJobStore()
	store := newFakeScheduleStore(tickSchedule("paused", false))

	s := NewScheduler(store, NewQueue(jobStore))
	s.RegisterJobType("ohlcv", JobTypeOHLCV)

	require.NoError(t, s.Start())
	defer s.Stop()

	assert.Equal(t, 0, s.Stats().ActiveSchedules, "inactive 排程不该进 cron")
	assert.Never(t, func() bool {
		return jobStore.countScheduledJobs(JobTypeOHLCV) > 0
	}, 400*time.Millisecond, 20*time.Millisecond,
		"inactive 排程被加载并触发了 —— activeOnly 过滤失效")
}

// TestToggleScheduleWithoutStartDoesNotFire 是本组测试里最重要的一条**反证腿**：
// 它证明「只看 Stats() 计数」的验收会骗人 —— 未 Start 的调度器里 toggle 会让计数变 1，
// 而那一格永远不会跑。这条测试**不依赖**生产有没有接线（它直接构造未启动的调度器），
// 所以它恒绿；它的用处是把"为什么验收必须是作业行"这件事钉在代码里，防止将来有人
// 把验收改成断计数。
func TestToggleScheduleWithoutStartDoesNotFire(t *testing.T) {
	jobStore := newMockJobStore()
	sc := tickSchedule("toggled", false)
	store := newFakeScheduleStore(sc)

	s := NewScheduler(store, NewQueue(jobStore))
	s.RegisterJobType("ohlcv", JobTypeOHLCV)
	// 故意**不**调用 s.Start() —— 这就是 SYNC-01 描述的生产状态。

	require.NoError(t, s.ToggleSchedule(context.Background(), sc.ID, true))
	require.Equal(t, 1, s.Stats().ActiveSchedules,
		"前置事实：未启动的调度器里 toggle 也会把计数抬到 1")
	require.Equal(t, 1, store.activeCount(), "库里该排程确实已是 active")

	assert.Never(t, func() bool {
		return jobStore.countScheduledJobs(JobTypeOHLCV) > 0
	}, 400*time.Millisecond, 20*time.Millisecond,
		"未 Start 的调度器居然跑了作业 —— 那本文件的立论（计数不等于在跑）就失效了，须重审")
}

// TestToggleScheduleOffClearsNextRunAt 是 SYNC-03 的主验收：停用之后，
// 库里的 next_run_at 必须与 is_active=false **同一次 UPDATE** 一起清掉 ——
// 它指向的运行永远不会发生，残留就是让界面把幻想当事实展示。
func TestToggleScheduleOffClearsNextRunAt(t *testing.T) {
	jobStore := newMockJobStore()
	sc := tickSchedule("toggle-next-run", true)
	store := newFakeScheduleStore(sc)

	s := NewScheduler(store, NewQueue(jobStore))
	s.RegisterJobType("ohlcv", JobTypeOHLCV)

	ctx := context.Background()
	require.NoError(t, s.ToggleSchedule(ctx, sc.ID, true))

	activated, err := store.GetSyncSchedule(ctx, sc.ID)
	require.NoError(t, err)
	require.NotNil(t, activated.NextRunAt, "前置事实：激活后 next_run_at 已落库 —— 没有它本测试测不到清除")

	require.NoError(t, s.ToggleSchedule(ctx, sc.ID, false))

	deactivated, err := store.GetSyncSchedule(ctx, sc.ID)
	require.NoError(t, err)
	require.NotNil(t, deactivated, "停用后从 store 读回应仍能拿到该排程")
	assert.False(t, deactivated.IsActive, "toggle false 后 is_active 必须是 false")
	assert.Nil(t, deactivated.NextRunAt,
		"停用后 next_run_at 必须是 NULL —— 残留值指向一个永远不会发生的运行（SYNC-03）")

	// Stats 读 cron entry，本来就空 —— 确认清除语句没有反过来污染它。
	assert.Empty(t, s.Stats().NextRuns, "停用后 cron entry 应已移除，NextRuns 不该有残留")
}

// 反向腿：激活路径不能被清除语句误伤 —— toggle true 后 next_run_at 必须还在。
func TestToggleScheduleOnStillSetsNextRunAt(t *testing.T) {
	jobStore := newMockJobStore()
	sc := tickSchedule("toggle-on", false)
	store := newFakeScheduleStore(sc)

	s := NewScheduler(store, NewQueue(jobStore))
	s.RegisterJobType("ohlcv", JobTypeOHLCV)

	ctx := context.Background()
	require.NoError(t, s.ToggleSchedule(ctx, sc.ID, true))

	activated, err := store.GetSyncSchedule(ctx, sc.ID)
	require.NoError(t, err)
	assert.True(t, activated.IsActive)
	assert.NotNil(t, activated.NextRunAt,
		"激活后 next_run_at 必须落库 —— 清除逻辑若误伤激活路径，界面将无下次运行可展示")
}
