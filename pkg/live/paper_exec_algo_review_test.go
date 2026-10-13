package live

// K17-RV4 · paper 侧口径 A：取消发生在某交易日内时，该日**已到点**的子单
// 仍照常报送，下一交易日的日循环顶部（paper_session.go 的 ctx.Err 检查）
// 才中止。与回测侧同口径（pkg/backtest/exec_algo_review_test.go 的
// TestExecAlgo_CancelSameDayDueSliceStillSubmitted；两侧注释见各自
// dispatch 报送点）。
import (
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

// k17rv4PaperProbe 窗口首日挂 TWAP 5 片；第 cancelOnDay 日取消 ctx 后不再动作。
type k17rv4PaperProbe struct {
	*strategy.BaseStrategy
	cancel      context.CancelFunc
	cancelOnDay int
}

func (p *k17rv4PaperProbe) GenerateSignals(_ context.Context, bars map[string][]domain.OHLCV, _ *domain.Portfolio) ([]strategy.Signal, error) {
	series := bars["000001.SZ"]
	switch len(series) {
	case 1:
		return []strategy.Signal{{
			Symbol: "000001.SZ", Action: "buy", Direction: domain.DirectionLong,
			Strength: 1.0, Date: series[0].Date, ExecAlgo: "twap", ExecSlices: 5,
		}}, nil
	case p.cancelOnDay:
		// 取消注入点：晚于日循环顶部的 ctx 检查、早于当日 exec-algo 报送。
		p.cancel()
	}
	return nil, nil
}

func (p *k17rv4PaperProbe) Weight(strategy.Signal, float64) float64 { return 1 }

// runK17RV4Paper 跑一段 paper 回放，返回该 symbol 的期末持仓量与 Run 的 error。
func runK17RV4Paper(t *testing.T, name string, cancelOnDay int) (float64, error) {
	t.Helper()
	start := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	trader := NewMockTrader(MockTraderConfig{
		InitialCash:           1_000_000,
		SlippageModel:         "impact",
		ImpactSigma:           0.05,
		ImpactLiquidityFactor: 1.0,
	}, zerolog.Nop())
	s, err := NewPaperSession(PaperSessionConfig{
		Provider: execAlgoPaperBars(20, start),
		Clock:    clock.NewVirtualClock(start),
		Trader:   trader,
		Logger:   zerolog.Nop(),
	})
	require.NoError(t, err)

	probe := &k17rv4PaperProbe{
		BaseStrategy: strategy.NewBaseStrategy(name, "K17-RV4 paper probe"),
		cancelOnDay:  cancelOnDay,
	}
	ctx := context.Background()
	if cancelOnDay > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		probe.cancel = cancel
	}

	_, runErr := s.Run(ctx, []string{"000001.SZ"}, start, start.AddDate(0, 0, 19), probe)

	positions, perr := trader.GetPositions(context.Background())
	require.NoError(t, perr)
	var qty float64
	for _, pos := range positions {
		if pos.Symbol == "000001.SZ" {
			qty = pos.Quantity
		}
	}
	return qty, runErr
}

// TestPaperExecAlgo_CancelSameDayDueSliceStillSubmitted 钉住 paper 侧口径 A。
//
// 破坏腿（改成「报送点判 ctx」→ 本测试红）：在 dispatchExecAlgoChildren
// 的报送前加 `if ctx.Err() != nil { continue }`，取消当日的片消失，
// 入场量会从 3/5 掉到 2/5。
func TestPaperExecAlgo_CancelSameDayDueSliceStillSubmitted(t *testing.T) {
	// 对照：不取消 → 报满 5 片。
	full, errFull := runK17RV4Paper(t, "k17rv4_paper_control", 0)
	require.NoError(t, errFull)
	require.Positive(t, full, "对照运行（无取消）应报满 5 片")

	// 第 3 日取消：当日到点片仍报送 → 恰好 3/5 入场。
	cancelled, errCancelled := runK17RV4Paper(t, "k17rv4_paper_cancel", 3)
	require.ErrorIs(t, errCancelled, context.Canceled, "取消在下一交易日日循环顶部生效")
	assert.InDelta(t, full*3.0/5.0, cancelled, 1e-6,
		"取消当日已到点片必须仍报送（当日即停会是 2/5 的入场量）")
}
