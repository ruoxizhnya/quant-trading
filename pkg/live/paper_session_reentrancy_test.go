package live

// K17-R2：paper 侧两件鲁棒性（TASKS.md K17-R2 ③ 要求 2/3）。
//
//   - R2 重入护栏：PaperSession 语义是「整段确定性回放恰好一次」。旧实现
//     无护栏——第二次 Run 撞父单号（Dispatcher.Attach duplicate），在
//     K17-R1 之前表现为 warn+skip（信号静默消失），之后表现为 ambiguous
//     的 duplicate 错误；两者都不说「你在重入一个一次性会话」。新实现
//     在 Run 入口显式拒绝。
//   - R3 ctx 贯通：attachExecAlgo 此前用 context.Background()——Run 的
//     取消决定到不了挂载点（回测侧 attachExecAlgoSignal 已带 ctx，两侧
//     不一致）。贯通后，挂载前一刻取消 ⇒ Run 立即失败、零成交；旧实现
//     则当日子单照常报送、带着已取消的 ctx 跑到次一交易日才中止。
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

// TestPaperSession_RunReentrancyRejected 是 K17-R2-R2 的验收：第二次 Run
// 必须显式 error（fail-loud），不得静默重放或撞父单号。
func TestPaperSession_RunReentrancyRejected(t *testing.T) {
	start := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	prov := execAlgoPaperBars(10, start)
	trader := NewMockTrader(MockTraderConfig{InitialCash: 1_000_000}, zerolog.Nop())
	s, err := NewPaperSession(PaperSessionConfig{
		Provider: prov,
		Clock:    clock.NewVirtualClock(start),
		Trader:   trader,
	})
	require.NoError(t, err)

	probe := &execAlgoPaperProbe{
		BaseStrategy: strategy.NewBaseStrategy("k17r2_reentry", "K17-R2 reentrancy probe"),
		strength:     1.0, // 无 exec_algo：整单路径，第一次 Run 正常完成
	}
	ctx := context.Background()
	fills1, err := s.Run(ctx, []string{"000001.SZ"}, start, start.AddDate(0, 0, 9), probe)
	require.NoError(t, err)
	assert.NotEmpty(t, fills1, "第一次回放正常产出成交")

	fills2, err := s.Run(ctx, []string{"000001.SZ"}, start, start.AddDate(0, 0, 9), probe)
	require.Error(t, err, "重入必须显式拒绝——旧口径：撞父单号 warn+skip / duplicate 错误，都不点破「重入」")
	assert.Contains(t, err.Error(), "already executed", "错误要点名「一次性会话已执行」")
	assert.Nil(t, fills2, "重入调用不产出成交流")
}

// TestPaperSession_AttachHonorsRunContext 是 K17-R2-R3 的取消传播断言：
// Run 的 ctx 在挂载前一刻被取消（SignalCollector 钩子）——挂载必须显式
// 拒绝，Run 立即失败、零成交。
func TestPaperSession_AttachHonorsRunContext(t *testing.T) {
	start := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	prov := execAlgoPaperBars(20, start)
	trader := NewMockTrader(MockTraderConfig{InitialCash: 1_000_000}, zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := NewPaperSession(PaperSessionConfig{
		Provider: prov,
		Clock:    clock.NewVirtualClock(start),
		Trader:   trader,
		SignalCollector: func(_ time.Time, signals []domain.Signal) {
			for _, sig := range signals {
				if sig.ExecAlgo != "" {
					cancel() // 挂载前一刻取消（钩子在信号循环之前）
				}
			}
		},
	})
	require.NoError(t, err)

	probe := &execAlgoPaperProbe{
		BaseStrategy: strategy.NewBaseStrategy("k17r2_ctx", "K17-R2 ctx probe"),
		execAlgo:     "twap", slices: 7, strength: 1.0,
	}
	fills, err := s.Run(ctx, []string{"000001.SZ"}, start, start.AddDate(0, 0, 19), probe)

	require.Error(t, err, "ctx 已取消，挂载必须显式拒绝（旧实现用 Background ⇒ 挂载照常成功）")
	assert.ErrorIs(t, err, context.Canceled, "错误链穿透到取消原因")
	assert.Contains(t, err.Error(), "attach cancelled",
		"错误必须点名「挂载因取消被拒」——旧实现的失败模式是日循环入口的裸 context canceled，与挂载无关")

	// 行为事实（比 fills 更硬的判据）：Run 中途 return nil 会丢弃已累计的
	// 成交流，但 trader 的账丢不掉——旧实现当日子单已报送 ⇒ 已建仓；
	// 新实现取消先于任何报送 ⇒ 零持仓。
	positions, posErr := trader.GetPositions(context.Background())
	require.NoError(t, posErr)
	assert.Empty(t, positions, "取消必须先于任何子单报送（旧实现当日子单已成交建仓 ⇒ 非空）")
	_ = fills
}
