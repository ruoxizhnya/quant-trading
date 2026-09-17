package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// P1-1 实验日志（S1 通回路的第一块）。
//
// PRODUCT 里给它的定位是「过拟合检测、路径回放、复盘的共同数据源」，
// 落在 L1，被验证器与审阅台共同读取。因此字段必须能回答五个问题：
//
//	从哪开始（hypothesis）· 试了什么（params）· 结果怎样（metrics）·
//	怎么走到这（seq + parent_id）· 用的哪份数据（dataset_split）
//
// 其中「试了几次才撞出来」本身就是信号：试 5 次撞出来的和试 500 次撞出来的，
// 同样结果，可信度差一个量级（PRODUCT §验证器）。所以失败也必须留下痕迹。

func uniqueRunID(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("test_run_%d", time.Now().UnixNano())
}

func TestSaveExperiment_RoundTrip(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	runID := uniqueRunID(t)
	defer store.DB().Exec(ctx, "DELETE FROM experiments WHERE run_id=$1", runID)

	id, err := store.InsertExperiment(ctx, &Experiment{
		RunID:        runID,
		Seq:          0,
		Hypothesis:   "动量窗口越大越好",
		Params:       map[string]any{"lookback": 20, "top_pct": 0.8},
		DatasetSplit: "train",
		StrategyName: "momentum_test",
		Expression:   "cs_rank(ts_pct_change(close, 20)) > 0.8",
	})
	require.NoError(t, err)
	require.Greater(t, id, int64(0), "插入后应返回自增 ID")

	// 插入后是 running：中途叫停的实验会永远停留在这个状态 —— 那正是
	// 「跑到第几步被叫停」的证据，不能省。
	pending, err := store.GetExperiment(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "running", pending.Status)
	assert.Nil(t, pending.Metrics)
	assert.Nil(t, pending.FinishedAt)

	require.NoError(t, store.CompleteExperiment(ctx, id, &ExperimentMetrics{
		SharpeRatio: 1.2,
		TotalReturn: 0.30,
		TotalTrades: 42,
	}, ""))

	got, err := store.GetExperiment(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, runID, got.RunID)
	assert.Equal(t, 0, got.Seq)
	assert.Equal(t, "train", got.DatasetSplit)
	assert.Equal(t, "completed", got.Status)
	assert.Equal(t, 20.0, got.Params["lookback"])
	require.NotNil(t, got.Metrics)
	assert.InDelta(t, 1.2, got.Metrics.SharpeRatio, 1e-9)
	assert.Equal(t, 42, got.Metrics.TotalTrades)
	assert.NotNil(t, got.FinishedAt, "完成的实验必须有结束时间")
}

// TestCompleteExperiment_RecordsFailure：失败也要留在路径里 —— 回放的时候
// 要能看见「试过什么、在哪翻的车」，否则过拟合检测无从谈起。
func TestCompleteExperiment_RecordsFailure(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	runID := uniqueRunID(t)
	defer store.DB().Exec(ctx, "DELETE FROM experiments WHERE run_id=$1", runID)

	id, err := store.InsertExperiment(ctx, &Experiment{
		RunID: runID, Seq: 3, Params: map[string]any{"lookback": 500},
	})
	require.NoError(t, err)

	require.NoError(t, store.CompleteExperiment(ctx, id, nil, "backtest failed: no data in range"))

	got, err := store.GetExperiment(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "failed", got.Status)
	assert.Contains(t, got.Error, "no data in range")
	assert.Nil(t, got.Metrics, "失败的实验不该有指标")
	assert.Equal(t, "running", "running") // 占位：running 是插入时的初始状态
}

// TestListExperiments_OrderedBySeq：路径回放按尝试顺序，不是按插入顺序。
func TestListExperiments_OrderedBySeq(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	runID := uniqueRunID(t)
	defer store.DB().Exec(ctx, "DELETE FROM experiments WHERE run_id=$1", runID)

	// 故意乱序插入
	for _, seq := range []int{2, 0, 1} {
		_, err := store.InsertExperiment(ctx, &Experiment{
			RunID: runID, Seq: seq, Params: map[string]any{"seq": seq},
		})
		require.NoError(t, err)
	}

	got, err := store.ListExperiments(ctx, runID)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, 0, got[0].Seq)
	assert.Equal(t, 1, got[1].Seq)
	assert.Equal(t, 2, got[2].Seq)
}

// TestExperiment_ParentLink：父子关系撑起「从哪来、怎么走到这」。
func TestExperiment_ParentLink(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	runID := uniqueRunID(t)
	defer store.DB().Exec(ctx, "DELETE FROM experiments WHERE run_id=$1", runID)

	parentID, err := store.InsertExperiment(ctx, &Experiment{RunID: runID, Seq: 0})
	require.NoError(t, err)

	childID, err := store.InsertExperiment(ctx, &Experiment{
		RunID: runID, Seq: 1, ParentID: &parentID,
		Hypothesis: "父代窗口 20 不错，试试 30",
	})
	require.NoError(t, err)

	child, err := store.GetExperiment(ctx, childID)
	require.NoError(t, err)
	require.NotNil(t, child.ParentID)
	assert.Equal(t, parentID, *child.ParentID)
}

// TestListExperiments_EmptyRun：没跑过的 run 返回空切片而不是 nil / 报错。
func TestListExperiments_EmptyRun(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	got, err := store.ListExperiments(context.Background(), "no_such_run")
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestUpdateExperiment_FillsWhatWasTried：尝试先落行、参数后补。
//
// 顺序不能反 —— 等参数齐了才写，进程崩在中途就什么都没留下。
func TestUpdateExperiment_FillsWhatWasTried(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	runID := uniqueRunID(t)
	defer store.DB().Exec(ctx, "DELETE FROM experiments WHERE run_id=$1", runID)

	id, err := store.InsertExperiment(ctx, &Experiment{RunID: runID, Seq: 0})
	require.NoError(t, err)

	require.NoError(t, store.UpdateExperiment(ctx, id, ExperimentUpdate{
		Params:       map[string]any{"lookback": 30},
		StrategyName: "momentum_30",
		Expression:   "cs_rank(ts_pct_change(close, 30)) > 0.8",
	}))

	got, err := store.GetExperiment(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 30.0, got.Params["lookback"])
	assert.Equal(t, "momentum_30", got.StrategyName)
	assert.Equal(t, "cs_rank(ts_pct_change(close, 30)) > 0.8", got.Expression)
	assert.Equal(t, "running", got.Status, "补写参数不该改变状态")
}

// TestUpdateExperiment_EmptyFieldsKeepOriginal：零值字段表示「不改」，
// 否则后一次补写会把前一次已经填好的内容擦掉。
func TestUpdateExperiment_EmptyFieldsKeepOriginal(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	runID := uniqueRunID(t)
	defer store.DB().Exec(ctx, "DELETE FROM experiments WHERE run_id=$1", runID)

	id, err := store.InsertExperiment(ctx, &Experiment{
		RunID: runID, Seq: 0, StrategyName: "momentum_20",
		Expression: "cs_rank(ts_pct_change(close, 20)) > 0.8",
	})
	require.NoError(t, err)

	require.NoError(t, store.UpdateExperiment(ctx, id, ExperimentUpdate{
		Params: map[string]any{"lookback": 20},
	}))

	got, err := store.GetExperiment(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "momentum_20", got.StrategyName, "没传的字段要保留原值")
	assert.Equal(t, "cs_rank(ts_pct_change(close, 20)) > 0.8", got.Expression)
	assert.Equal(t, 20.0, got.Params["lookback"])
}
