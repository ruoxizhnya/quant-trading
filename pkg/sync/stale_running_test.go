package sync

// AUD-50 的护栏：重启后 `running` 的 job 必须被回收。
//
// 为什么值得单独一个文件：这个缺陷的形态是「**函数写对了、没人调用**」。
// 所以这里有一条测试不测函数本身，而是**从 WorkerPool.Start() 进去**，
// 断言那一行真的被改掉了 —— 只有它能抓住「CleanupStaleRunning 躺在那里
// 但 Start 忘了调」这个形态。单测函数本身是抓不住的。

import (
	"context"
	"strings"
	"testing"
	"time"
)

func seedJob(t *testing.T, store *mockJobStore, id string, status JobStatus, processed, total int) {
	t.Helper()
	job := &Job{
		ID:             id,
		JobType:        JobTypeOHLCVAll,
		Status:         status,
		ProcessedItems: processed,
		TotalItems:     total,
		MaxRetries:     3,
		CreatedAt:      time.Now(),
	}
	if err := store.CreateSyncJob(context.Background(), job); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func TestQueue_CleanupStaleRunning_MarksRunningAsFailed(t *testing.T) {
	store := newMockJobStore()
	queue := NewQueue(store)
	ctx := context.Background()

	seedJob(t, store, "stuck", JobStatusRunning, 910, 5907)

	n, err := queue.CleanupStaleRunning(ctx)
	if err != nil {
		t.Fatalf("CleanupStaleRunning: %v", err)
	}
	if n != 1 {
		t.Fatalf("应回收 1 条，实际 %d", n)
	}

	got, err := store.GetSyncJob(ctx, "stuck")
	if err != nil {
		t.Fatalf("读回: %v", err)
	}
	if got.Status != JobStatusFailed {
		t.Fatalf("status 应为 failed，实际 %s", got.Status)
	}
	// 不能标成 cancelled：没人主动停它，是被中断的。这个区别对后来读台账的人有意义。
	if got.Status == JobStatusCancelled {
		t.Fatal("被中断的任务不该记成「有人取消了」")
	}
	if got.CompletedAt == nil {
		t.Fatal("终态必须有 completed_at，否则它会一直挂在「进行中」的看板上")
	}
	if !strings.Contains(got.ErrorMessage, "AUD-50") {
		t.Fatalf("回收了但没说清原因，下一个人只会看到一条普通的 failed：%q", got.ErrorMessage)
	}
	// 进度要留着 —— 它记录了「中断时已经同步到哪」，是人工续跑的依据。
	if got.ProcessedItems != 910 || got.TotalItems != 5907 {
		t.Fatalf("回收不该抹掉进度：%d/%d", got.ProcessedItems, got.TotalItems)
	}
}

// 负向：只动 running，别的一律不碰。
// 只钉「running 会被改」而不钉「别的不会被改」，一次顺手扩大范围就没人发现。
func TestQueue_CleanupStaleRunning_LeavesOtherStatusesAlone(t *testing.T) {
	store := newMockJobStore()
	queue := NewQueue(store)
	ctx := context.Background()

	seedJob(t, store, "pending-1", JobStatusPending, 0, 100)
	seedJob(t, store, "done-1", JobStatusCompleted, 100, 100)
	seedJob(t, store, "failed-1", JobStatusFailed, 40, 100)
	seedJob(t, store, "cancelled-1", JobStatusCancelled, 10, 100)
	seedJob(t, store, "running-1", JobStatusRunning, 20, 100)

	n, err := queue.CleanupStaleRunning(ctx)
	if err != nil {
		t.Fatalf("CleanupStaleRunning: %v", err)
	}
	if n != 1 {
		t.Fatalf("只有 1 条 running，却动了 %d 条", n)
	}

	for _, tc := range []struct {
		id   string
		want JobStatus
	}{
		{"pending-1", JobStatusPending},
		{"done-1", JobStatusCompleted},
		{"failed-1", JobStatusFailed},
		{"cancelled-1", JobStatusCancelled},
		{"running-1", JobStatusFailed},
	} {
		got, err := store.GetSyncJob(ctx, tc.id)
		if err != nil {
			t.Fatalf("读回 %s: %v", tc.id, err)
		}
		if got.Status != tc.want {
			t.Fatalf("%s 的状态被误改：want %s got %s", tc.id, tc.want, got.Status)
		}
	}

	// pending 那条的 error_message 不该被写上「被中断」—— 它还没开始跑。
	pending, _ := store.GetSyncJob(ctx, "pending-1")
	if strings.Contains(pending.ErrorMessage, "AUD-50") {
		t.Fatal("给还没开始跑的 pending 任务写了「被中断」，这是谎")
	}
}

func TestQueue_CleanupStaleRunning_EmptyStoreIsNotAnError(t *testing.T) {
	queue := NewQueue(newMockJobStore())
	n, err := queue.CleanupStaleRunning(context.Background())
	if err != nil {
		t.Fatalf("空库不该报错：%v", err)
	}
	if n != 0 {
		t.Fatalf("空库应回收 0 条，实际 %d", n)
	}
}

// 这条才是 AUD-50 的主护栏：**从 Start() 进去**。
//
// 把 CleanupStaleRunning 从 Start() 里摘掉，上面三条单测**依然全绿** ——
// 因为函数本身没坏。只有这一条会红。
func TestWorkerPool_StartRecoversInterruptedJob(t *testing.T) {
	store := newMockJobStore()
	queue := NewQueue(store)
	ctx := context.Background()

	// 模拟「上一个进程跑到一半被 docker restart 干掉」留下的现场。
	seedJob(t, store, "interrupted", JobStatusRunning, 910, 5907)

	pool := NewWorkerPool(queue, 1)
	pool.Start()
	defer pool.Stop()

	got, err := store.GetSyncJob(ctx, "interrupted")
	if err != nil {
		t.Fatalf("读回: %v", err)
	}
	if got.Status == JobStatusRunning {
		t.Fatal("Start() 之后这一行还是 running —— worker 只捞 pending，" +
			"它会永久搁浅在「进行中」，而日志里只有健康检查（AUD-50）")
	}
	if got.Status != JobStatusFailed {
		t.Fatalf("应回收成 failed，实际 %s", got.Status)
	}
	if !strings.Contains(got.ErrorMessage, "AUD-50") {
		t.Fatalf("回收了但没说清原因：%q", got.ErrorMessage)
	}
}

// 回收不能把正常待跑的任务一起吃掉 —— 否则「修好重启残留」会变成「重启即丢任务」。
func TestWorkerPool_StartDoesNotEatPendingJobs(t *testing.T) {
	store := newMockJobStore()
	queue := NewQueue(store)
	ctx := context.Background()

	seedJob(t, store, "queued", JobStatusPending, 0, 10)

	// 一个什么都不做的 executor，只为让 worker 能正常起停。
	pool := NewWorkerPool(queue, 1)
	pool.RegisterExecutor(&noopExecutor{})
	pool.Start()
	defer pool.Stop()

	got, err := store.GetSyncJob(ctx, "queued")
	if err != nil {
		t.Fatalf("读回: %v", err)
	}
	if got.Status == JobStatusFailed {
		t.Fatal("pending 任务被 Start() 的回收误杀了")
	}
}

type noopExecutor struct{}

func (n *noopExecutor) JobType() JobType { return JobTypeOHLCVAll }

func (n *noopExecutor) Execute(_ context.Context, _ *Job, _ ProgressReporter) (any, error) {
	return nil, nil
}
