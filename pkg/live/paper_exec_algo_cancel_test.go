package live

// K18：exec-algo 外部仓位变动取消活跃父单 —— paper 侧端到端测试
// （TASKS.md K18 ⑤「paper 侧同构断言」）。
//
// paper 回放的日循环不含止损/拆股/退市处理，外部改仓表现为「trader 实际
// 持仓与本会话算法父单的累计成交量不再一致」——本用例中由策略在窗口中段
// （日 3）发出平仓信号制造该变动。验收判据（与回测侧同语义）：
//  1. 平仓前的子单照报；**平仓后剩余子单不再报送**；
//  2. 平仓后不再产生买入成交（不把刚平的仓位买回）；
//  3. 日志带 cancel-reason（`cancelled mid-window` / `external_position_change`），
//     且**不再**出现会话结束的撤销告警（已取消父单不进 Unfinished）。
//
// 破坏腿（去对账钩子 → 断言红）：把 paper_exec_algo.go
// dispatchExecAlgoChildren 里的 reconcileExternalPositionChanges 调用去掉，
// 则日 3..6 的子单照报、平仓后买入 > 0，判据 1/2 全红。
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

// k18PaperProbe 批式探针：窗口首日（bar 数=1）发 TWAP 7 片 Long 信号；
// 窗口第 4 日（bar 数=4，即日 3）发一条平仓信号 —— 制造「窗口中段外部
// 仓位变动」。
type k18PaperProbe struct {
	*strategy.BaseStrategy
	midWindowClose bool
}

func (p *k18PaperProbe) GenerateSignals(_ context.Context, bars map[string][]domain.OHLCV, _ *domain.Portfolio) ([]strategy.Signal, error) {
	series := bars["000001.SZ"]
	switch len(series) {
	case 1:
		return []strategy.Signal{{
			Symbol: "000001.SZ", Action: "buy", Direction: domain.DirectionLong,
			Strength: 1.0, Date: series[0].Date, ExecAlgo: "twap", ExecSlices: 7,
		}}, nil
	case 4:
		if !p.midWindowClose {
			return nil, nil
		}
		return []strategy.Signal{{
			Symbol: "000001.SZ", Action: "sell", Strength: 1.0, Date: series[3].Date,
		}}, nil
	}
	return nil, nil
}

func (p *k18PaperProbe) Weight(strategy.Signal, float64) float64 { return 1 }

// runK18Paper 跑一段 paper 回放，返回成交流与日志缓冲。
func runK18Paper(t *testing.T, probe *k18PaperProbe) ([]PaperFill, *bytes.Buffer) {
	t.Helper()
	start := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	prov := execAlgoPaperBars(20, start)
	logSink := &bytes.Buffer{}
	trader := NewMockTrader(MockTraderConfig{
		InitialCash:           1_000_000,
		SlippageModel:         "impact",
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

// TestPaperExecAlgo_CancelOnExternalPositionChange 是 K18 的 paper 侧正证据。
func TestPaperExecAlgo_CancelOnExternalPositionChange(t *testing.T) {
	closeDay := time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC) // 日 3

	// ── 运行 A：窗口内无人动仓（对照）——算法单应报满 7 片 ──
	fillsA, logBufA := runK18Paper(t, &k18PaperProbe{
		BaseStrategy: strategy.NewBaseStrategy("k18_paper_nochange", "K18 paper no-change control"),
	})
	var buysA int
	for _, f := range fillsA {
		if f.Side == domain.DirectionLong {
			buysA++
		}
	}
	require.Equal(t, 7, buysA, "窗口内无外部改仓 → 算法单报满 7 片")
	assert.NotContains(t, logBufA.String(), "cancelled mid-window")

	// ── 运行 B：日 3 平仓信号（外部仓位变动）→ 后续子单中止 ──
	fillsB, logB := runK18Paper(t, &k18PaperProbe{
		BaseStrategy:   strategy.NewBaseStrategy("k18_paper_cancel", "K18 paper cancel probe"),
		midWindowClose: true,
	})

	var buys, sells int
	var buyAfterClose int
	for _, f := range fillsB {
		switch f.Side {
		case domain.DirectionLong:
			buys++
			if f.Date.After(closeDay) {
				buyAfterClose++
			}
		case domain.DirectionClose:
			sells++
		}
	}

	// 判据 1：平仓后的子单不再报送。
	assert.Less(t, buys, 7, "外部改仓后剩余子单必须中止（不应报满 7 片）")
	for i, f := range fillsB {
		if f.Side != domain.DirectionLong {
			continue
		}
		assert.False(t, f.Date.After(closeDay),
			"第 %d 条买入成交（%s）落在平仓日之后 —— 平仓后仍在续买（K18 语义漏洞）",
			i, f.Date.Format("2006-01-02"))
	}
	require.Equal(t, 1, sells, "应有且仅有一条平仓卖出成交")

	// 判据 2：平仓后不再产生买入成交。
	assert.Zero(t, buyAfterClose, "平仓日之后不得再有买入成交（平仓后应空仓）")

	// 判据 3：日志带显式 cancel-reason；已取消父单不再触发会话结束撤销告警。
	assert.Contains(t, logB.String(), "cancelled mid-window", "外部改仓取消应带显式 cancel-reason 日志")
	assert.Contains(t, logB.String(), "cancel_reason")
	assert.Contains(t, logB.String(), "external_position_change", "cancel-reason 应为 external_position_change")
	assert.NotContains(t, logB.String(), "unfinished at session end",
		"已取消父单不得再触发会话结束撤销告警")

	t.Logf("K18 paper e2e：无改仓买入 %d 笔 / 有改仓买入 %d 笔（平仓日 %s），平仓后买入 %d 笔",
		buysA, buys, closeDay.Format("2006-01-02"), buyAfterClose)
}
