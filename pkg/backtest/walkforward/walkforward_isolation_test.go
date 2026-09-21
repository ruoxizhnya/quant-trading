package walkforward

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubRunner records the backtest requests it receives, so we can verify that
// each window is served by its own instance.
type stubRunner struct {
	id    int
	mu    sync.Mutex
	calls []string
}

func (s *stubRunner) RunBacktest(_ context.Context, req contracts.BacktestRequest) (*contracts.BacktestResponse, error) {
	s.mu.Lock()
	s.calls = append(s.calls, req.StartDate+"~"+req.EndDate)
	s.mu.Unlock()
	return &contracts.BacktestResponse{
		ID:           "stub",
		Status:       "completed",
		SharpeRatio:  1.0,
		AnnualReturn: 0.1,
	}, nil
}

// 回归：WalkForwardEngine 此前持有一个**共享**的 runner 实例，所有窗口复用同一个
// Engine。而 Engine 持有 per-instance 的引擎级缓存（OHLCV / 因子 / 基本面 /
// 上市日历），并发窗口因此互相污染 —— 因子缓存尤其是「整体替换」语义
// （f.cache = combined），并发窗口 B 的 Warm 会直接覆盖窗口 A 正在读取的缓存，
// 窗口 A 于是读到别的窗口日期区间的因子值。
//
// 代码注释当时还声称「窗口之间互不共享状态」，与实现正好相反。
func TestRunWindowsParallel_EachWindowGetsOwnRunner(t *testing.T) {
	var mu sync.Mutex
	var created []*stubRunner
	nextID := 0

	factory := func() (contracts.EngineRunner, error) {
		mu.Lock()
		defer mu.Unlock()
		nextID++
		r := &stubRunner{id: nextID}
		created = append(created, r)
		return r, nil
	}

	wf := NewWalkForwardEngine(factory, nil, zerolog.Nop())

	base := time.Date(2022, 1, 3, 0, 0, 0, 0, time.UTC)
	windows := []wfWindow{
		{
			trainStart: base,
			trainEnd:   base.AddDate(1, 0, 0),
			testStart:  base.AddDate(1, 0, 1),
			testEnd:    base.AddDate(1, 3, 0),
		},
		{
			trainStart: base.AddDate(0, 6, 0),
			trainEnd:   base.AddDate(1, 6, 0),
			testStart:  base.AddDate(1, 6, 1),
			testEnd:    base.AddDate(1, 9, 0),
		},
		{
			trainStart: base.AddDate(1, 0, 0),
			trainEnd:   base.AddDate(2, 0, 0),
			testStart:  base.AddDate(2, 0, 1),
			testEnd:    base.AddDate(2, 3, 0),
		},
	}

	req := WalkForwardRequest{Strategy: "stub", InitialCapital: 1000000}
	results := wf.runWindowsParallel(context.Background(), req, windows)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, created, 3, "每个窗口必须构造自己的 runner，不能复用同一实例")
	assert.Len(t, results, 3, "三个窗口都应产出结果")

	// 每个实例只应服务自己那一个窗口的 train + test（共 2 次调用）。
	// 若出现跨窗口复用，某个实例会收到 4 次及以上。
	for _, r := range created {
		assert.Len(t, r.calls, 2, "runner #%d 只应服务一个窗口的 train + test，实际 %v", r.id, r.calls)
	}

	// 实例互不相同
	seen := make(map[int]bool, len(created))
	for _, r := range created {
		assert.False(t, seen[r.id], "runner #%d 被多个窗口复用", r.id)
		seen[r.id] = true
	}
}

// 对照组：证明上面的断言**真的能抓到**共享 runner。
//
// 假的护栏比没护栏更糟 —— 它会让人以为有人看着。这里故意注入一个复用同一实例
// 的工厂，确认它确实会让单个 runner 收到 3 个窗口 × 2 次 = 6 次调用，
// 从而在上一个测试的 `assert.Len(r.calls, 2)` 处失败。
func TestRunWindowsParallel_SharedRunnerIsActuallyDetected(t *testing.T) {
	// 工厂本身会被 4 个 worker 并发调用，必须用锁保证「只创建一个实例」，
	// 否则两个 goroutine 会各自建出一个 runner，调用被分散计数。
	var mu sync.Mutex
	var shared *stubRunner
	factory := func() (contracts.EngineRunner, error) {
		mu.Lock()
		defer mu.Unlock()
		if shared == nil {
			shared = &stubRunner{id: 1}
		}
		return shared, nil
	}

	wf := NewWalkForwardEngine(factory, nil, zerolog.Nop())

	base := time.Date(2022, 1, 3, 0, 0, 0, 0, time.UTC)
	windows := []wfWindow{
		{trainStart: base, trainEnd: base.AddDate(1, 0, 0), testStart: base.AddDate(1, 0, 1), testEnd: base.AddDate(1, 3, 0)},
		{trainStart: base.AddDate(0, 6, 0), trainEnd: base.AddDate(1, 6, 0), testStart: base.AddDate(1, 6, 1), testEnd: base.AddDate(1, 9, 0)},
		{trainStart: base.AddDate(1, 0, 0), trainEnd: base.AddDate(2, 0, 0), testStart: base.AddDate(2, 0, 1), testEnd: base.AddDate(2, 3, 0)},
	}

	req := WalkForwardRequest{Strategy: "stub", InitialCapital: 1000000}
	wf.runWindowsParallel(context.Background(), req, windows)

	mu.Lock()
	defer mu.Unlock()
	require.NotNil(t, shared)
	assert.Len(t, shared.calls, 6,
		"共享 runner 会被 3 个窗口各调用 2 次 —— 这正是上一个测试要防的情况")
}
