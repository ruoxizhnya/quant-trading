package live

// K17-R1：paper 侧挂载失败 fail-loud + 两侧口径一致（TASKS.md K17-R1
// ③ 要求 3「同一判定同一处置，补测试钉住两侧一致」）。
//
// 判定同源：分类判据是 execalgo.ClassifyAttachError（execalgo 单点实现，
// 回测侧同一个函数）；处置按各侧可见性通道映射——回测走既有
// invalid_reasons（run 判 invalid），paper 无结果通道 → Run 显式 error。
// 本测试钉住 paper 侧：拼错算法名的回放不再「warn + skip 跑完、成交静
// 默消失」，而是显式拒绝整段回放。
import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/clock"
	"github.com/ruoxizhnya/quant-trading/pkg/execalgo"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// TestPaperExecAlgo_UnknownStyleFailsLoud 与回测侧
// TestExecAlgoAttach_BacktestUnknownStyleRunInvalid 同一误配置（"twp"）：
// 两侧都被拒绝、都不产出成交，判定同源（errors.Is 穿透到同一 sentinel）。
func TestPaperExecAlgo_UnknownStyleFailsLoud(t *testing.T) {
	start := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	prov := execAlgoPaperBars(20, start)
	trader := NewMockTrader(MockTraderConfig{InitialCash: 1_000_000}, zerolog.Nop())
	s, err := NewPaperSession(PaperSessionConfig{
		Provider: prov,
		Clock:    clock.NewVirtualClock(start),
		Trader:   trader,
	})
	require.NoError(t, err)

	probe := &execAlgoPaperProbe{
		BaseStrategy: strategy.NewBaseStrategy("k17r1_paper_unknown", "K17-R1 paper unknown-style probe"),
		execAlgo:     "twp", slices: 3, strength: 1.0, // 拼错的 TWAP
	}
	fills, err := s.Run(context.Background(), []string{"000001.SZ"}, start, start.AddDate(0, 0, 19), probe)

	// fail-loud：不再 warn+skip 跑完（旧口径下 err 为 nil、成交静默消失）。
	require.Error(t, err, "未知算法名必须显式拒绝整段回放，不得静默跳过信号")
	assert.ErrorIs(t, err, execalgo.ErrUnknownAlgo,
		"判定同源：错误链穿透到与回测侧同一 sentinel")
	assert.Contains(t, err.Error(), "rejected")
	assert.Empty(t, fills, "被拒信号不产出任何成交")
}
