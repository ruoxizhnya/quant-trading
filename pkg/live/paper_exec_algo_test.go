package live

// K17：exec-algo 接进 **paper** 下单主路径的端到端测试（TASKS.md K17 ⑤）。
//
// 正证据：
//  1. TWAP 拆 7 片的大单 → PaperSession 产出 **7 条** PaperFill（不是 1 条）；
//  2. Σ 子单量 == 同策略无算法运行的整单量（signalQuantity 口径一致）；
//  3. 时间片均匀：7 条成交落在 7 个不同交易日、相邻间隔恰为 1 自然日；
//  4. 大单总冲击成本 < 同量整单（MockTrader 与回测同一份 pkg/execution
//     成本核，impact 分支按子单量定冲击 —— D5 正证据）。
//
// 回归腿：无 exec_algo 的同一策略 → 1 条 PaperFill（默认路径不变）。
// OnFill 回填腿：回放日志无会话结束「CANCELLED」撤销警告。
//
// 注意：策略信号 strength=1.0 → signalQuantity = 1000（与 executeSignal
// 同一换算函数，算法单与整单是同一笔意图的不同执行方式）。
import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/clock"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// execAlgoPaperBars 造 20 个交易日的振荡 bar（close 10.00/10.02 交替，
// Volume=1e6 作 ADV 代理）。
func execAlgoPaperBars(days int, start time.Time) *fakeProvider {
	p := &fakeProvider{days: make([]time.Time, 0, days), bars: map[string][]domain.OHLCV{}}
	for d := 0; d < days; d++ {
		date := start.AddDate(0, 0, d)
		p.days = append(p.days, date)
		c := 10.00
		if d%2 == 1 {
			c = 10.02
		}
		p.bars["000001.SZ"] = append(p.bars["000001.SZ"], domain.OHLCV{
			Symbol: "000001.SZ", Date: date,
			Open: c, High: c + 0.05, Low: c - 0.05, Close: c, Volume: 1_000_000,
		})
	}
	return p
}

// execAlgoPaperProbe 批式探针：窗口首日发一条 Long 信号（exec_algo 可配）。
type execAlgoPaperProbe struct {
	*strategy.BaseStrategy
	execAlgo  string
	slices    int
	profile   []float64
	strength  float64
	wholeDone bool
}

func (p *execAlgoPaperProbe) GenerateSignals(_ context.Context, bars map[string][]domain.OHLCV, _ *domain.Portfolio) ([]strategy.Signal, error) {
	series := bars["000001.SZ"]
	if len(series) != 1 {
		return nil, nil
	}
	return []strategy.Signal{{
		Symbol: "000001.SZ", Action: "buy", Direction: domain.DirectionLong,
		Strength: p.strength, Date: series[0].Date,
		ExecAlgo: p.execAlgo, ExecSlices: p.slices, ExecProfile: p.profile,
	}}, nil
}

func (p *execAlgoPaperProbe) Weight(strategy.Signal, float64) float64 { return 1 }

// runPaperExecAlgo 跑一段 paper 回放，返回成交流与日志缓冲。
func runPaperExecAlgo(t *testing.T, probe *execAlgoPaperProbe) ([]PaperFill, *bytes.Buffer) {
	t.Helper()
	start := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	prov := execAlgoPaperBars(20, start)
	logSink := &bytes.Buffer{}
	trader := NewMockTrader(MockTraderConfig{
		InitialCash:           1_000_000,
		SlippageModel:         "impact", // 与回测同一份 pkg/execution 成本核
		ImpactSigma:           0.05,
		ImpactLiquidityFactor: 1.0,
	}, zerolog.Nop())
	s, err := NewPaperSession(PaperSessionConfig{
		Provider: prov,
		Clock:    clock.NewVirtualClock(start),
		Trader:   trader,
		Logger:   zerolog.New(logSink),
	})
	require.NoError(t, err)

	end := start.AddDate(0, 0, 19)
	fills, err := s.Run(context.Background(), []string{"000001.SZ"}, start, end, probe)
	require.NoError(t, err)
	return fills, logSink
}

// paperImpact 汇总买入 PaperFill 的冲击分量 = Σ (成交价 − 当根参考价) × 数量。
// 参考价按「该笔成交所在交易日的 bar 收盘」取：fixture 起点是 2026-03-02、
// close 按 bar 序号 10.00/10.02 交替且窗口不跨月，故 bar 序号 = 日号 − 2。
func paperImpact(fills []PaperFill) float64 {
	var cost float64
	for _, f := range fills {
		if f.Side != domain.DirectionLong {
			continue
		}
		ref := 10.00
		if (f.Date.Day()-2)%2 != 0 {
			ref = 10.02
		}
		cost += (f.Price - ref) * f.Qty
	}
	return cost
}

// TestPaperExecAlgo_TWAP7Fills 是 paper 侧 K17 ⑤ 的正证据主腿。
func TestPaperExecAlgo_TWAP7Fills(t *testing.T) {
	// 运行 A：无 exec_algo（整单）——量与冲击基准。
	fillsA, _ := runPaperExecAlgo(t, &execAlgoPaperProbe{
		BaseStrategy: strategy.NewBaseStrategy("k17_paper_whole", "K17 paper whole-order probe"),
		strength:     1.0,
	})
	require.Len(t, fillsA, 1, "默认路径：1 条 PaperFill")
	wholeQty := fillsA[0].Qty
	require.Equal(t, 1000.0, wholeQty, "strength=1.0 → signalQuantity=1000")
	wholeImpact := paperImpact(fillsA)
	require.Positive(t, wholeImpact, "impact 模型应给整单正冲击")

	// 运行 B：exec_algo=twap, slices=7。
	fillsB, logB := runPaperExecAlgo(t, &execAlgoPaperProbe{
		BaseStrategy: strategy.NewBaseStrategy("k17_paper_twap7", "K17 paper twap probe"),
		execAlgo:     "twap", slices: 7, strength: 1.0,
	})

	require.Len(t, fillsB, 7, "TWAP 7 片 → 7 条 PaperFill")
	var sum float64
	for i, f := range fillsB {
		sum += f.Qty
		assert.Positive(t, f.Qty, "第 %d 片为正手", i)
		assert.Equal(t, domain.DirectionLong, f.Side)
	}
	assert.Equal(t, wholeQty, sum, "Σ 子单量 == 整单量")

	// 时间片均匀：7 个不同交易日、相邻间隔恰为 1 自然日。
	for i := 1; i < len(fillsB); i++ {
		assert.Equal(t, 24*time.Hour, fillsB[i].Date.Sub(fillsB[i-1].Date),
			"第 %d 片与前一峰间隔应恰为 1 自然日", i)
	}

	// D5 正证据：拆分总冲击 < 整单冲击（期望占比 ≈ 1/√7 ≈ 0.378）。
	splitImpact := paperImpact(fillsB)
	assert.Less(t, splitImpact, wholeImpact, "拆分后总冲击成本必须低于同量整单")
	ratio := splitImpact / wholeImpact
	assert.Less(t, ratio, 0.8, "拆细 7 片后期望占比 ≈ 0.378，got %v", ratio)
	t.Logf("paper D5 冲击对比：整单冲击=%v 拆分冲击=%v 占比=%v", wholeImpact, splitImpact, ratio)

	// OnFill 回填腿：无会话结束撤销警告。
	assert.NotContains(t, logB.String(), "CANCELLED",
		"paper 回放日志出现撤销警告 —— exec-algo 父单进度未正确回填")
	t.Logf("paper e2e：%d 条子单成交 Σ=%v（整单 %v），冲击占比 %v", len(fillsB), sum, wholeQty, ratio)
}

// TestPaperExecAlgo_VWAP7Fills 覆盖 VWAP 经 paper 路径：按 profile 加权、
// Σ 精确等于整单量。
func TestPaperExecAlgo_VWAP7Fills(t *testing.T) {
	fills, logB := runPaperExecAlgo(t, &execAlgoPaperProbe{
		BaseStrategy: strategy.NewBaseStrategy("k17_paper_vwap7", "K17 paper vwap probe"),
		execAlgo:     "vwap", slices: 7, profile: []float64{1, 2, 3, 4, 5, 6, 7}, strength: 1.0,
	})
	require.Len(t, fills, 7)
	var sum float64
	for _, f := range fills {
		sum += f.Qty
	}
	assert.Equal(t, 1000.0, sum)
	assert.Less(t, fills[0].Qty, fills[6].Qty, "VWAP 应按成交量 profile 加权")
	assert.NotContains(t, logB.String(), "CANCELLED")
	t.Logf("paper VWAP 7 片：Σ=%v 首片=%v 末片=%v", sum, fills[0].Qty, fills[6].Qty)
}
