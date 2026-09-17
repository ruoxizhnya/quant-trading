package loop

import (
	"context"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/ai"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/intent"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/pipeline"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/search"
	yamlgen "github.com/ruoxizhnya/quant-trading/pkg/ai/yaml"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubBacktest 顶替真实回测引擎：真回测要查行情、跑撮合，一轮几十次下来太慢，
// 而这里要验证的是「控制器 × pipeline × 实验日志」三者串不串得起来。
type stubBacktest struct{ n int }

func (s *stubBacktest) RunBacktest(ctx context.Context, name string, pool []string, startDate, endDate string) (*domain.BacktestResult, error) {
	s.n++
	return &domain.BacktestResult{
		SharpeRatio: float64(s.n) * 0.25,
		TotalReturn: 0.02 * float64(s.n),
		TotalTrades: s.n * 3,
	}, nil
}

// TestRun_RealPipelineAndRealDB 是 P1-2 的验收：真 pipeline + 真库跑一轮
// 多次尝试，然后从库里把整条路径回放出来。
//
// 这也是 P1-1c 缺的那一半 —— 单次尝试能回放早就有了，这里要证明**多步**
// 也讲得通：seq 连续、状态齐全、父代指得对。
//
// DB 没起就 skip（与 pkg/storage 的约定一致）：连不上库是环境问题。
func TestRun_RealPipelineAndRealDB(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewPostgresStore(ctx,
		"postgres://postgres:postgres@localhost:5432/quant_trading?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: cannot connect to DB: %v", err)
	}
	defer store.Close()

	runID := "itest_loop_run"
	defer store.DB().Exec(ctx, "DELETE FROM experiments WHERE run_id=$1", runID)

	p := pipeline.NewPipelineWithDeps(
		intent.NewParser(), yamlgen.NewGenerator(), &ai.MockClient{},
		pipeline.WithExperimentSink(store),
	)
	space := &search.SearchSpace{Params: []search.ParamDef{
		{Name: "lookback", Type: "int", Min: 10, Max: 60},
	}}
	ctrl := NewController(p, NewTPEProposer(space, 42), &stubBacktest{})

	const tries = 5
	out, err := ctrl.Run(ctx, Config{
		RunID: runID, Description: "做一个动量策略", MaxTries: tries,
	})
	require.NoError(t, err)
	require.Len(t, out.Tries, tries)
	assert.Equal(t, StopExhausted, out.Stopped)

	// 从库里回放 —— 这是「能回放一条完整探索路径」的实证。
	rows, err := store.ListExperiments(ctx, runID)
	require.NoError(t, err)
	require.Len(t, rows, tries, "跑几次就该有几行，一次都不能少")

	seenParent := 0
	for i, r := range rows {
		assert.Equal(t, i, r.Seq, "seq 必须连续，回放才有意义")
		assert.Equal(t, runID, r.RunID)
		assert.Equal(t, storage.ExperimentStatusCompleted, r.Status, "第 %d 次没跑完", i)
		require.NotNil(t, r.Metrics, "第 %d 次没有指标", i)
		assert.NotEmpty(t, r.Expression, "第 %d 次看不出跑了什么信号", i)
		assert.NotEmpty(t, r.Hypothesis, "第 %d 次没说为什么试这个", i)
		if r.ParentID != nil {
			seenParent++
		}
	}
	assert.Positive(t, seenParent, "TPE 阶段应该产生父子链 —— 否则路径讲不出「为什么转到下一步」")
}
