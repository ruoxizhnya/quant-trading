package sync

// AUD-49 的护栏：`POST /api/sync/jobs/:id/cancel` 必须真的停住一个在跑的任务，
// 而且停住之后**不能被写回去**。
//
// 这个缺陷有两层，缺任何一层测试都是假的：
//  ① **执行器收不到信号**：`workerLoop` 用的是 `context.Background()`，
//     根本不存在 per-job 的取消句柄。于是 `CancelJob` 只能改数据库，
//     执行器继续对着 Tushare 跑几小时。
//  ② **进度上报把终态复活**：worker 手里那份 `*Job` 是内存副本，带着
//     `status=running`；`ReportProgress` 每秒无条件把它整份写回去，
//     下一次上报必然把 `cancelled` 覆盖成 `running`。
//
// 所以下面既有「执行器是否真的停了」的断言，也有「过了上报窗口之后行还是
// cancelled 吗」的断言。只断言 ① 会漏掉 ②，只断言 ② 会漏掉 ① ——
// 两条都是生产上真实发生过的。

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockingExecutor 一直上报进度直到 ctx 被取消，模仿真实的 OHLCV 同步：
// 它遍历几千个 symbol，**只会因为有人叫它停才停**。
type blockingExecutor struct {
	jobType JobType

	startOnce   sync.Once
	started     chan struct{}
	stoppedOnce sync.Once
	stopped     chan struct{}
}

func newBlockingExecutor(jobType JobType) *blockingExecutor {
	return &blockingExecutor{
		jobType: jobType,
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

func (e *blockingExecutor) Execute(ctx context.Context, job *Job, progress ProgressReporter) (any, error) {
	e.startOnce.Do(func() { close(e.started) })

	processed := 0
	for {
		select {
		case <-ctx.Done():
			e.stoppedOnce.Do(func() { close(e.stopped) })
			return nil, ctx.Err()
		case <-time.After(5 * time.Millisecond):
			processed++
			progress.ReportProgress(processed, 5907, 0)
		}
	}
}

func (e *blockingExecutor) JobType() JobType { return e.jobType }

// cancelHarness 装出与 cmd/data 生产接线一致的组合：
// JobService + Queue + WorkerPool，且 canceller 已接上。
// 生产接线在 cmd/data/sync_handlers.go:NewSyncHandler，由 internal/repoguard 钉住。
type cancelHarness struct {
	store      *mockJobStore
	queue      *Queue
	jobService *JobService
	pool       *WorkerPool
}

func newCancelHarness(workers int) *cancelHarness {
	store := newMockJobStore()
	queue := NewQueue(store)
	jobService := NewJobService(store)
	jobService.SetPendingNotifier(queue.NotifyJobAvailable)
	pool := NewWorkerPool(queue, workers)
	jobService.SetRunningCanceller(pool.Cancel)
	return &cancelHarness{store: store, queue: queue, jobService: jobService, pool: pool}
}

// TestCancelRunningJob_StopsTheExecutorAndStaysCancelled 是 AUD-49 的主回归测试。
func TestCancelRunningJob_StopsTheExecutorAndStaysCancelled(t *testing.T) {
	h := newCancelHarness(1)
	exec := newBlockingExecutor(JobTypeOHLCVAll)
	h.pool.RegisterExecutor(exec)

	ctx := context.Background()
	job, err := h.jobService.CreateJob(ctx, JobTypeOHLCVAll, map[string]any{"symbols": []string{"600000.SH"}})
	require.NoError(t, err)

	h.pool.Start()
	defer h.pool.Stop()

	select {
	case <-exec.started:
	case <-time.After(5 * time.Second):
		t.Fatal("执行器从未启动")
	}

	// 等它真的在跑、并且已经上报过进度 —— 否则「进度上报复活终态」这条路
	// 根本不会被走到，测试是假绿。
	require.Eventually(t, func() bool {
		got, _ := h.store.GetSyncJob(ctx, job.ID)
		return got != nil && got.Status == JobStatusRunning && got.ProcessedItems > 0
	}, 5*time.Second, 10*time.Millisecond, "任务应已进入 running 并有进度")

	require.NoError(t, h.jobService.CancelJob(ctx, job.ID))

	// ① 执行器必须真的收到取消信号。这是缺陷的第一层：只改行、不停执行器。
	select {
	case <-exec.stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("执行器没有观察到 ctx 取消 —— 取消只改了数据库，没有传到正在跑的那个 goroutine")
	}

	// 记录取消那一刻的进度，用来验证「取消之后不再有进度落库」。
	atCancel, err := h.store.GetSyncJob(ctx, job.ID)
	require.NoError(t, err)

	// ② 等过一个进度上报窗口（节流是 1s），确认没有任何东西把它写回去。
	//    现场看到的现象正是这两条：接口返回 200，而 processed_items 继续往上走。
	//
	//    注意分工：这里断的是「执行器停了之后确实不再写」；而「取消与进度上报
	//    同时发生、上报把终态覆盖掉」那条竞争由
	//    TestQueue_ProgressUpdateCannotResurrectSettledJob 精确覆盖 ——
	//    那是唯一能确定性复现该交错的地方。
	time.Sleep(1500 * time.Millisecond)

	got, err := h.store.GetSyncJob(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, JobStatusCancelled, got.Status,
		"取消之后行变成了 %s —— 进度上报把终态复活了，这正是 AUD-49", got.Status)
	assert.Equal(t, atCancel.ProcessedItems, got.ProcessedItems,
		"取消之后 processed_items 还在往上走（%d -> %d）—— 现场看到的就是这个",
		atCancel.ProcessedItems, got.ProcessedItems)
	assert.Nil(t, got.ScheduledAt, "取消的任务不该被重新排进队列")
}

// 在飞的任务必须能在池子里被找到 —— 没有这个句柄，「取消」在原理上就做不到，
// 无论数据库怎么写。这条钉的是注册/注销本身。
func TestWorkerPool_RegistersInFlightJobForCancellation(t *testing.T) {
	h := newCancelHarness(1)
	exec := newBlockingExecutor(JobTypeOHLCVAll)
	h.pool.RegisterExecutor(exec)

	ctx := context.Background()
	job, err := h.jobService.CreateJob(ctx, JobTypeOHLCVAll, nil)
	require.NoError(t, err)

	assert.Equal(t, 0, h.pool.RunningCount(), "还没启动就不该有在飞的任务")

	h.pool.Start()
	defer h.pool.Stop()

	require.Eventually(t, func() bool { return h.pool.RunningCount() == 1 },
		5*time.Second, 10*time.Millisecond, "执行中的任务必须登记在册，否则取消无从下手")

	assert.True(t, h.pool.Cancel(job.ID), "在飞的任务应该能被取消")

	// 跑完之后句柄要摘掉，否则注册表会一直长，而且「还在跑吗」会撒谎。
	require.Eventually(t, func() bool { return h.pool.RunningCount() == 0 },
		5*time.Second, 10*time.Millisecond, "任务结束后必须注销，否则注册表泄漏")
}

func TestWorkerPool_CancelUnknownJobIsFalse(t *testing.T) {
	h := newCancelHarness(1)
	assert.False(t, h.pool.Cancel("never-registered"),
		"池子里没有这个任务时必须返回 false —— 调用方靠它区分「真打断了」和「只是改了行」")
}

// pending 的任务没有在飞的执行体，取消不该去打扰 worker pool。
func TestJobService_CancelPendingJobDoesNotTouchThePool(t *testing.T) {
	h := newCancelHarness(1)
	ctx := context.Background()

	seedJob(t, h.store, "queued", JobStatusPending, 0, 100)

	called := false
	h.jobService.SetRunningCanceller(func(string) bool { called = true; return false })

	require.NoError(t, h.jobService.CancelJob(ctx, "queued"))
	assert.False(t, called, "pending 的任务没有执行体，不该调用 canceller")

	got, err := h.store.GetSyncJob(ctx, "queued")
	require.NoError(t, err)
	assert.Equal(t, JobStatusCancelled, got.Status)
}

// 上一进程遗留的 `running` 行：没人真的在跑它，取消必须成功，
// 但要留下警告 —— 「标成 cancelled 了却没人被打断」是值得记一笔的事实。
func TestJobService_CancelRunningJobNotOwnedHereStillSucceeds(t *testing.T) {
	h := newCancelHarness(1)
	ctx := context.Background()

	seedJob(t, h.store, "orphan", JobStatusRunning, 910, 5907)

	called := false
	h.jobService.SetRunningCanceller(func(string) bool { called = true; return false })

	require.NoError(t, h.jobService.CancelJob(ctx, "orphan"),
		"上一进程遗留的 running 行必须能取消 —— 它本来就没人执行")
	assert.True(t, called, "行说 running，就应该去池子里找一找")

	got, err := h.store.GetSyncJob(ctx, "orphan")
	require.NoError(t, err)
	assert.Equal(t, JobStatusCancelled, got.Status)
}

// 负向：已经终态的任务不能被「取消」，否则会把 completed 改写成 cancelled。
func TestJobService_CancelTerminalJobIsRejected(t *testing.T) {
	h := newCancelHarness(1)
	ctx := context.Background()

	for _, status := range []JobStatus{JobStatusCompleted, JobStatusFailed, JobStatusCancelled} {
		id := "settled-" + string(status)
		seedJob(t, h.store, id, status, 100, 100)

		err := h.jobService.CancelJob(ctx, id)
		require.Error(t, err, "%s 的任务不该能再被取消", status)
		assert.Contains(t, err.Error(), "terminal")

		got, _ := h.store.GetSyncJob(ctx, id)
		assert.Equal(t, status, got.Status, "被拒绝的取消不能改动已有状态")
	}
}

// ---- 第二层（进度上报复活终态）的窄测：直接打在写路径上 ----

// settleExternally 模拟「别人把这一行结算了」—— 用户点了取消、或另一个 worker
// 抢先处理。worker 手里的内存副本对此一无所知，这正是缺陷的成因。
func settleExternally(t *testing.T, store *mockJobStore, id string, status JobStatus) {
	t.Helper()
	ctx := context.Background()
	job, err := store.GetSyncJob(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, job)
	job.Status = status
	applied, err := store.UpdateSyncJobIfStatus(ctx, job, JobStatusRunning)
	require.NoError(t, err)
	require.True(t, applied, "测试夹具没能把 %s 结算成 %s", id, status)
}

func TestQueue_ProgressUpdateCannotResurrectSettledJob(t *testing.T) {
	store := newMockJobStore()
	queue := NewQueue(store)
	ctx := context.Background()

	seedJob(t, store, "job-1", JobStatusRunning, 10, 100)
	inMemory, err := store.GetSyncJob(ctx, "job-1")
	require.NoError(t, err)

	// 正常情况：还在 running，进度写得进去。
	inMemory.UpdateProgress(20, 100, 0)
	applied, err := queue.UpdateRunningJob(ctx, inMemory)
	require.NoError(t, err)
	require.True(t, applied, "running 的行应该能正常写进度")

	// 用户取消。
	settleExternally(t, store, "job-1", JobStatusCancelled)

	// worker 那份还是 running（它不知道），下一次进度上报带着它写回去。
	// 原始实现是无条件 UPDATE，于是 cancelled 在这里被复活成 running。
	inMemory.UpdateProgress(30, 100, 0)
	applied, err = queue.UpdateRunningJob(ctx, inMemory)
	require.NoError(t, err)
	assert.False(t, applied, "行已终态，进度上报必须被拒 —— 这就是 AUD-49 的复活路径")

	got, err := store.GetSyncJob(ctx, "job-1")
	require.NoError(t, err)
	assert.Equal(t, JobStatusCancelled, got.Status, "cancelled 被进度上报写回了 running")
	assert.Equal(t, 20, got.ProcessedItems, "被拒的那次写不该落库")
}

// 重试是最危险的一条复活路径：它把行改成 pending，
// 于是 worker 会**再跑一遍**用户已经取消的任务。
func TestQueue_RetryLaterDoesNotRequeueSettledJob(t *testing.T) {
	store := newMockJobStore()
	queue := NewQueue(store)
	ctx := context.Background()

	seedJob(t, store, "job-1", JobStatusRunning, 10, 100)
	inMemory, err := store.GetSyncJob(ctx, "job-1")
	require.NoError(t, err)

	settleExternally(t, store, "job-1", JobStatusCancelled)

	applied, err := queue.RetryLater(ctx, inMemory, "executor failed")
	require.NoError(t, err)
	assert.False(t, applied, "已结算的任务不能被重新排进队列")

	got, err := store.GetSyncJob(ctx, "job-1")
	require.NoError(t, err)
	assert.Equal(t, JobStatusCancelled, got.Status,
		"被取消的任务被改成了 %s —— 它会被 worker 再跑一遍", got.Status)
	assert.Nil(t, got.ScheduledAt)
}

// 失败也不能盖掉 cancelled：把用户主动停的任务记成 failed 是在甩锅给数据源。
func TestQueue_FailJobDoesNotOverwriteSettledJob(t *testing.T) {
	store := newMockJobStore()
	queue := NewQueue(store)
	ctx := context.Background()

	seedJob(t, store, "job-1", JobStatusRunning, 10, 100)
	inMemory, err := store.GetSyncJob(ctx, "job-1")
	require.NoError(t, err)

	settleExternally(t, store, "job-1", JobStatusCancelled)

	applied, err := queue.FailJob(ctx, inMemory, "ctx canceled")
	require.NoError(t, err)
	assert.False(t, applied)

	got, err := store.GetSyncJob(ctx, "job-1")
	require.NoError(t, err)
	assert.Equal(t, JobStatusCancelled, got.Status, "取消被改写成了失败")
	assert.NotContains(t, got.ErrorMessage, "ctx canceled")
}

// 完成同理：任务在收尾时被取消，不能报成功。
func TestQueue_CompleteJobDoesNotOverwriteSettledJob(t *testing.T) {
	store := newMockJobStore()
	queue := NewQueue(store)
	ctx := context.Background()

	seedJob(t, store, "job-1", JobStatusRunning, 99, 100)
	inMemory, err := store.GetSyncJob(ctx, "job-1")
	require.NoError(t, err)

	settleExternally(t, store, "job-1", JobStatusCancelled)

	applied, err := queue.CompleteJob(ctx, inMemory, []byte(`{"ok":true}`))
	require.NoError(t, err)
	assert.False(t, applied, "已结算的任务不能被报成完成")

	got, err := store.GetSyncJob(ctx, "job-1")
	require.NoError(t, err)
	assert.Equal(t, JobStatusCancelled, got.Status)
	assert.NotEqual(t, 100, got.ProgressPercent)
}

// ---- 领取（dequeue）也是竞争窗口 ----

// cancelOnListStore 在「列出 pending」返回的瞬间把目标结算掉，
// 复现 list 与 claim 之间那个窗口。这是无条件 claim 唯一能静默吃掉取消的地方。
type cancelOnListStore struct {
	*mockJobStore
	victim string
}

func (s *cancelOnListStore) ListSyncJobs(ctx context.Context, status JobStatus, limit int) ([]*Job, error) {
	jobs, err := s.mockJobStore.ListSyncJobs(ctx, status, limit)
	if err != nil || len(jobs) == 0 {
		return jobs, err
	}
	if victim, gerr := s.mockJobStore.GetSyncJob(ctx, s.victim); gerr == nil && victim != nil {
		victim.Status = JobStatusCancelled
		if _, uerr := s.mockJobStore.UpdateSyncJobIfStatus(ctx, victim, JobStatusPending); uerr != nil {
			return nil, uerr
		}
	}
	return jobs, nil
}

func TestQueue_DequeueDoesNotClaimJobCancelledInBetween(t *testing.T) {
	base := newMockJobStore()
	store := &cancelOnListStore{mockJobStore: base, victim: "job-1"}
	queue := NewQueue(store)
	ctx := context.Background()

	seedJob(t, base, "job-1", JobStatusPending, 0, 100)

	job, err := queue.Dequeue(ctx)
	require.NoError(t, err)
	assert.Nil(t, job,
		"取消落在 list 与 claim 之间时，worker 不该把这一行抢成 running —— 它会跑一个用户已经停掉的任务")

	got, err := base.GetSyncJob(ctx, "job-1")
	require.NoError(t, err)
	assert.Equal(t, JobStatusCancelled, got.Status, "取消被 claim 吃掉了")
	assert.Nil(t, got.StartedAt, "没被领走的任务不该有 started_at")
}

// ---- 存储层的条件写：空 from 必须是错误，不能退化成「无条件」 ----

func TestQueue_ConditionalWriteRejectsEmptyFrom(t *testing.T) {
	store := newMockJobStore()
	ctx := context.Background()

	seedJob(t, store, "job-1", JobStatusRunning, 10, 100)
	job, err := store.GetSyncJob(ctx, "job-1")
	require.NoError(t, err)

	// 真实存储层用 `ANY('{}')`，那会匹配不到任何行 —— 一个忘了传 from 的调用
	// 会变成静默空操作。所以两边都要求「至少给一个来源状态」。
	applied, err := store.UpdateSyncJobIfStatus(ctx, job)
	require.Error(t, err, "空 from 必须报错，否则忘记传参会静默变成空操作")
	assert.False(t, applied)
	assert.Contains(t, err.Error(), "no allowed source status")
}

// stolenByAnotherWriterStore 让条件写**永远落空**，同时把行结算成 completed，
// 模拟「CancelJob 读到 running 之后、写之前，另一个写者先下手了」。
// 这是 CancelJob 里 `!applied` 那条分支，靠真实时序很难稳定复现。
type stolenByAnotherWriterStore struct {
	*mockJobStore
}

func (s *stolenByAnotherWriterStore) UpdateSyncJobIfStatus(
	ctx context.Context, job *Job, from ...JobStatus,
) (bool, error) {
	current, err := s.mockJobStore.GetSyncJob(ctx, job.ID)
	if err != nil || current == nil {
		return false, err
	}
	current.Status = JobStatusCompleted
	if _, err := s.mockJobStore.UpdateSyncJobIfStatus(ctx, current, from...); err != nil {
		return false, err
	}
	return false, nil
}

func TestJobService_CancelReportsConcurrentSettlement(t *testing.T) {
	base := newMockJobStore()
	store := &stolenByAnotherWriterStore{mockJobStore: base}
	jobService := NewJobService(store)
	jobService.SetRunningCanceller(func(string) bool { return true })

	seedJob(t, base, "job-1", JobStatusRunning, 10, 100)

	err := jobService.CancelJob(context.Background(), "job-1")
	require.Error(t, err, "写没落下去就不能报成功")
	assert.Contains(t, err.Error(), "terminal",
		"错误信息要说清真实状态，而不是笼统的「取消失败」：%v", err)

	got, err := base.GetSyncJob(context.Background(), "job-1")
	require.NoError(t, err)
	assert.Equal(t, JobStatusCompleted, got.Status, "抢先的写者赢了，取消不该把它改写掉")
}
