package backtest

// K17-R1：exec-algo 挂载失败不得静默消失 —— 回测侧 e2e（TASKS.md
// K17-R1 ③ 要求 1/2/4 与 ⑤ 验收）。
//
// 判定（形态裁决，理由写在 attachExecAlgoSignal 注释）：未知算法名是
// 静态误配置 → 信号校验期拒绝（rejected, not ignored），该 run 走既有
// invalid_reasons 通道判 invalid——不选「直接 fail 该信号」，后者跑完
// 依然没人知道少了交易。其余挂载失败保留「单信号失败不中断整场」，
// 但计数进运行诊断并以 exec_algo_attach_failed 呈现在结果里。
//
// 复用 exec_algo_e2e_test.go 的 harness（合成 bar / spy 撮合层 /
// 探针策略），这里只测失败腿；正路径三条 e2e 在原文件，零改动。
import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// runExecAlgoAttachProbe 跑一次回测，不做状态断言（失败腿的状态正是
// 被测对象），返回响应与撮合层观测。
func runExecAlgoAttachProbe(t *testing.T, eng *Engine, stratName string, spy *recordingExecService) (*BacktestResponse, []recordedOrder) {
	t.Helper()
	eng.SetExecutionService(spy)
	resp, err := eng.RunBacktest(context.Background(), BacktestRequest{
		Strategy:       stratName,
		StockPool:      []string{"000001.SZ", "600000.SH"},
		StartDate:      "2026-03-02",
		EndDate:        "2026-03-21",
		InitialCapital: 1_000_000,
		RiskFreeRate:   0.03,
	})
	require.NoError(t, err, "挂载失败不中断整场（单信号失败不中断既有约定）")
	return resp, spy.orders()
}

// TestExecAlgoAttach_BacktestUnknownStyleRunInvalid 是 K17-R1 要求 1 的
// 主腿：拼错算法名（"twp"）→ 信号校验期拒绝、不下单，run 判 invalid
// 且带稳定 Code——而不是「回测照常跑完、交易静默消失、只留一条 Warn」。
func TestExecAlgoAttach_BacktestUnknownStyleRunInvalid(t *testing.T) {
	logSink := &bytes.Buffer{}
	eng, _, _ := buildExecAlgoEngine(t, logSink)
	spy := &recordingExecService{inner: NewBacktestExecutionService(impactExecConfig())}
	probe := &execAlgoProbeStrategy{
		BaseStrategy: &strategy.BaseStrategy{}, name: "k17r1_unknown_style",
		symbol: "000001.SZ", execAlgo: "twp", slices: 3, strength: 1.0, // 拼错的 TWAP
		registerFn: func(s *execAlgoProbeStrategy) error { return strategy.DefaultRegistry.Register(s) },
	}
	probe.register(t)

	resp, orders := runExecAlgoAttachProbe(t, eng, "k17r1_unknown_style", spy)

	// 交易没有被静默执行，也没有被降级成整单直下——信号被拒。
	assert.Empty(t, orders, "未知算法名的信号必须被拒绝（既不拆单也不整单直下）")
	assert.Equal(t, 0, resp.TotalTrades)

	// 结果可见：状态 invalid + 稳定 Code（自动化层按 Code 判断）。
	assert.Equal(t, "invalid", resp.Status, "误配置的 run 不得自称 completed")
	assert.Contains(t, resp.InvalidReasons, contracts.InvalidExecAlgoUnknownStyle)
	assert.NotContains(t, resp.InvalidReasons, contracts.InvalidExecAlgoAttachFailed,
		"分类精度：未知算法名归 unknown_style，不落入 attach_failed")

	// 诊断可见：运行日志带分类与计数（人类可读的现场记录）。
	assert.Contains(t, logSink.String(), "failure_kind")
	assert.Contains(t, logSink.String(), "unknown_style")
	assert.Contains(t, logSink.String(), "attach failures")
}

// execAlgoDupStrategy 同日同 symbol 同方向发两条 exec-algo 信号——第二条
// 撞父单号（EA-日期-symbol-方向 纯函数派生），触发 Dispatcher.Attach 的
// ErrDuplicateParent。
type execAlgoDupStrategy struct {
	*strategy.BaseStrategy
	name string
}

func (s *execAlgoDupStrategy) Name() string { return s.name }

func (s *execAlgoDupStrategy) GenerateSignals(_ context.Context, bars map[string][]domain.OHLCV, _ *domain.Portfolio) ([]strategy.Signal, error) {
	series := bars["000001.SZ"]
	if len(series) != 1 {
		return nil, nil
	}
	sig := strategy.Signal{
		Symbol: "000001.SZ", Action: "buy", Direction: domain.DirectionLong,
		Strength: 1.0, Date: series[0].Date, ExecAlgo: "twap", ExecSlices: 2,
	}
	return []strategy.Signal{sig, sig}, nil
}

func (s *execAlgoDupStrategy) Weight(_ strategy.Signal, _ float64) float64 { return 1 }

// TestExecAlgoAttach_BacktestDuplicateParentCounted 是 K17-R1 要求 2 的
// 主腿：父单号重复 → 第二条信号被拒，run 不中断，但失败计数以
// exec_algo_attach_failed 呈现在结果里（禁止「跑完没人知道少了交易」）。
func TestExecAlgoAttach_BacktestDuplicateParentCounted(t *testing.T) {
	logSink := &bytes.Buffer{}
	eng, _, _ := buildExecAlgoEngine(t, logSink)
	spy := &recordingExecService{inner: NewBacktestExecutionService(impactExecConfig())}
	dup := &execAlgoDupStrategy{name: "k17r1_dup_parent"}
	require.NoError(t, strategy.DefaultRegistry.Register(dup))

	resp, orders := runExecAlgoAttachProbe(t, eng, "k17r1_dup_parent", spy)

	// 第一条信号照常拆 2 片；第二条的 2 片不存在——重复挂载被拒。
	require.Len(t, orders, 2, "重复父单只保留第一条信号的计划")
	for _, rec := range orders {
		assert.Equal(t, "EA-20260302-000001.SZ-long", rec.Order.ID[:len("EA-20260302-000001.SZ-long")],
			"子单属于第一个父单")
	}

	// 结果可见：invalid + attach_failed Code；分类精度（不含 unknown_style）。
	assert.Equal(t, "invalid", resp.Status)
	assert.Contains(t, resp.InvalidReasons, contracts.InvalidExecAlgoAttachFailed)
	assert.NotContains(t, resp.InvalidReasons, contracts.InvalidExecAlgoUnknownStyle)

	// 诊断可见：duplicate_parent 计数 + 样例点名。
	assert.Contains(t, logSink.String(), "duplicate_parent")
	assert.Contains(t, logSink.String(), "EA-20260302-000001.SZ-long")
	assert.NotContains(t, logSink.String(), "CANCELLED",
		"第一条父单全部成交，不触发会话结束撤销")
}
