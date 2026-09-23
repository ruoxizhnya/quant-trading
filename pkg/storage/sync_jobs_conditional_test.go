package storage

// AUD-49 的存储层取证：`UpdateSyncJobIfStatus` 的条件**真的在 SQL 里**。
//
// 为什么非要打真库：条件语义可以在 Go 里假装实现（pkg/sync 的 mock 就实现了
// 一份），而真实现走的是 `WHERE id = $1 AND status = ANY($16::text[])`。
// 这一条 SQL 写错的方式很多 —— 参数类型没对上、`ANY` 漏了、条件被顺手删掉 ——
// 而**所有上层单测都会照绿**，因为它们打的是 mock。这正是本仓反复踩的
// 「两个机制各自对、接起来就错」：mock 对、SQL 错，接口照样返回 200。
//
// 所以这里断言的是「条件落空时那一行真的没变」，而不是「函数返回了 false」——
// 后者在 mock 上永远成立，证明不了 SQL。

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	sync "github.com/ruoxizhnya/quant-trading/pkg/sync/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func uniqueSyncJobID(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("aud49_%d", time.Now().UnixNano())
}

// newSyncJobForTest 插入一行并注册清理。params 必须是合法 JSON ——
// 列类型是 JSONB NOT NULL，塞个空字符串会在 CreateSyncJob 就炸。
func newSyncJobForTest(t *testing.T, store *PostgresStore, id string, status sync.JobStatus) *sync.Job {
	t.Helper()
	ctx := context.Background()

	job := &sync.Job{
		ID:         id,
		JobType:    sync.JobTypeOHLCVAll,
		Status:     status,
		Params:     json.RawMessage(`{"symbols":["600000.SH"]}`),
		CreatedAt:  time.Now(),
		MaxRetries: 3,
	}
	require.NoError(t, store.CreateSyncJob(ctx, job))
	t.Cleanup(func() { store.DB().Exec(context.Background(), "DELETE FROM sync_jobs WHERE id=$1", id) })
	return job
}

// 正向：条件满足时写进去，并且是**整行**写进去（不只是 status）。
func TestUpdateSyncJobIfStatus_WritesWhenConditionMatches(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	id := uniqueSyncJobID(t)
	job := newSyncJobForTest(t, store, id, sync.JobStatusPending)

	job.Status = sync.JobStatusRunning
	started := time.Now()
	job.StartedAt = &started
	job.WorkerID = "worker-0"
	applied, err := store.UpdateSyncJobIfStatus(ctx, job, sync.JobStatusPending)
	require.NoError(t, err)
	require.True(t, applied, "来源状态匹配时必须写进去")

	got, err := store.GetSyncJob(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, sync.JobStatusRunning, got.Status)
	assert.Equal(t, "worker-0", got.WorkerID)
	assert.NotNil(t, got.StartedAt, "整行更新，不只是 status")
}

// 负向（这条才是 AUD-49 的核心）：行已经是 cancelled 时，
// 一份还带着 status=running 的进度上报**必须一个字节都写不进去**。
// 原始实现是无条件 UPDATE，于是这里会把 cancelled 复活成 running。
func TestUpdateSyncJobIfStatus_ProgressCannotResurrectCancelledRow(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	id := uniqueSyncJobID(t)
	job := newSyncJobForTest(t, store, id, sync.JobStatusRunning)

	// 用户取消：把行结算成 cancelled，进度停在 20。
	job.Status = sync.JobStatusCancelled
	now := time.Now()
	job.CompletedAt = &now
	job.ProcessedItems = 20
	job.TotalItems = 100
	job.ProgressPercent = 20
	applied, err := store.UpdateSyncJobIfStatus(ctx, job, sync.JobStatusRunning)
	require.NoError(t, err)
	require.True(t, applied)

	// worker 手里那份还是 running（它不知道被取消了），带着它写回去。
	stale := &sync.Job{
		ID:              id,
		JobType:         sync.JobTypeOHLCVAll,
		Status:          sync.JobStatusRunning, // ← 内存里的旧状态
		Params:          job.Params,
		ProgressPercent: 30,
		TotalItems:      100,
		ProcessedItems:  30,
		CreatedAt:       job.CreatedAt,
		MaxRetries:      3,
	}
	applied, err = store.UpdateSyncJobIfStatus(ctx, stale, sync.JobStatusRunning)
	require.NoError(t, err)
	assert.False(t, applied, "条件落空时必须报告没写进去")

	got, err := store.GetSyncJob(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, sync.JobStatusCancelled, got.Status,
		"cancelled 被进度上报写回了 %s —— 这就是 AUD-49 现场看到的现象", got.Status)
	assert.Equal(t, 20, got.ProcessedItems, "条件落空时整行都不该动")
	assert.Equal(t, 20, got.ProgressPercent)
}

// 多来源状态：pending 和 running 都允许被取消（`ANY` 要真的按列表匹配）。
func TestUpdateSyncJobIfStatus_AcceptsAnyListedStatus(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	for _, from := range []sync.JobStatus{sync.JobStatusPending, sync.JobStatusRunning} {
		id := uniqueSyncJobID(t)
		job := newSyncJobForTest(t, store, id, from)

		job.Status = sync.JobStatusCancelled
		now := time.Now()
		job.CompletedAt = &now
		applied, err := store.UpdateSyncJobIfStatus(ctx, job,
			sync.JobStatusPending, sync.JobStatusRunning)
		require.NoError(t, err)
		assert.True(t, applied, "来源是 %s 时应在允许列表里", from)

		got, err := store.GetSyncJob(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, sync.JobStatusCancelled, got.Status)
	}
}

// 不在列表里的状态必须落空 —— 否则「允许列表」只是装饰。
func TestUpdateSyncJobIfStatus_RejectsUnlistedStatus(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	id := uniqueSyncJobID(t)
	job := newSyncJobForTest(t, store, id, sync.JobStatusCompleted)

	job.Status = sync.JobStatusRunning
	applied, err := store.UpdateSyncJobIfStatus(ctx, job, sync.JobStatusPending, sync.JobStatusRunning)
	require.NoError(t, err)
	assert.False(t, applied, "completed 不在允许列表里")

	got, err := store.GetSyncJob(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, sync.JobStatusCompleted, got.Status, "已完成的记录不该被改写")
}

// 空 from 是错误而不是「匹配所有」：`ANY('{}')` 匹配不到任何行，
// 一个忘了传参的调用会退化成静默空操作。
func TestUpdateSyncJobIfStatus_EmptyFromIsAnError(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	id := uniqueSyncJobID(t)
	job := newSyncJobForTest(t, store, id, sync.JobStatusRunning)

	applied, err := store.UpdateSyncJobIfStatus(ctx, job)
	require.Error(t, err)
	assert.False(t, applied)
	assert.Contains(t, err.Error(), "no allowed source status")
}

// 行不存在时报告「没写进去」而不是报错 —— 调用方只需要知道条件不成立。
func TestUpdateSyncJobIfStatus_MissingRowReportsNotApplied(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	ghost := &sync.Job{
		ID:         "aud49_does_not_exist",
		JobType:    sync.JobTypeOHLCVAll,
		Status:     sync.JobStatusRunning,
		Params:     json.RawMessage(`{}`),
		CreatedAt:  time.Now(),
		MaxRetries: 3,
	}
	applied, err := store.UpdateSyncJobIfStatus(ctx, ghost, sync.JobStatusRunning)
	require.NoError(t, err)
	assert.False(t, applied)
}
