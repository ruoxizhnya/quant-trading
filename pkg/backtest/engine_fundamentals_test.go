package backtest

import (
	"context"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy/expression"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// P2-12：引擎侧的基本面注入。
//
// 注入靠两个类型断言（FactorAware 的老范式 + FundamentalAware 的新范式），
// 断言落空就静默跳过 —— 这类「静默」最容易在重构后变成「数据莫名不见了」，
// 所以这里显式钉住三段行为：存得进、取得出、不要的不查。

func TestEngine_SetFundamentals_RoundTrip(t *testing.T) {
	eng := newTestEngine(t)

	pe := 12.5
	want := map[string]strategy.FundamentalSeries{
		"600519.SH": {{Symbol: "600519.SH", Date: time.Now(), PE: &pe}},
	}
	eng.SetFundamentals(want)

	got := eng.fundamentalsSnapshot()
	require.NotNil(t, got, "存进去就得取得出来")
	assert.Equal(t, 12.5, *got["600519.SH"][0].PE)
}

// 没实现 FundamentalAware 的策略不该触发财报预热 —— 纯价量策略不该为
// 估值数据付查询成本。
func TestEngine_WarmFundamentals_SkippedWhenStrategyNotAware(t *testing.T) {
	eng := newTestEngine(t)
	require.NoError(t, strategy.DefaultRegistry.Register(&plainStrategy{name: "t_plain"}))

	err := eng.warmFundamentalsIfNeeded(context.Background(), "t_plain", []string{"A"}, time.Now())
	require.NoError(t, err)
	assert.Nil(t, eng.fundamentalsSnapshot(), "不要财报的策略，就不该有财报缓存")
}

// 实现了但要不到数据（没有 PostgresStore）时：不报错、不阻断，
// 缓存为空 —— 后续策略用到 pe 会由数据层明确报错，而不是引擎假装没事。
func TestEngine_WarmFundamentals_NoStoreLeavesEmpty(t *testing.T) {
	eng := newTestEngine(t)

	s, err := expression.NewExpressionStrategy("t_value_aware", expression.ExpressionStrategyConfig{
		SignalCfg: expression.SignalConfig{
			Expression: "cs_rank(neg(pe)) > 0.8",
			Action:     "buy",
			Direction:  domain.DirectionLong,
		},
	})
	require.NoError(t, err)
	require.NoError(t, strategy.DefaultRegistry.Register(s))

	// 编译期保证：这个策略确实是要财报的，否则上面的测试等于什么都没测。
	var _ strategy.FundamentalAware = s

	require.NoError(t, eng.warmFundamentalsIfNeeded(
		context.Background(), "t_value_aware", []string{"A"}, time.Now()))
	assert.Nil(t, eng.fundamentalsSnapshot())
}

// 走外部 strategy-service 的策略（本地注册表里没有）不参与预热。
func TestEngine_WarmFundamentals_UnknownStrategyIsNoop(t *testing.T) {
	eng := newTestEngine(t)
	pe := 10.0
	eng.SetFundamentals(map[string]strategy.FundamentalSeries{
		"A": {{Symbol: "A", Date: time.Now(), PE: &pe}},
	})

	require.NoError(t, eng.warmFundamentalsIfNeeded(
		context.Background(), "definitely_not_registered", []string{"A"}, time.Now()))
	assert.Len(t, eng.fundamentalsSnapshot(), 1, "不该被抹掉")
}

type plainStrategy struct {
	*strategy.BaseStrategy
	name string
}

func (p *plainStrategy) Name() string { return p.name }

func (p *plainStrategy) GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]strategy.Signal, error) {
	return nil, nil
}

func (p *plainStrategy) Weight(sig strategy.Signal, portfolioValue float64) float64 { return 0 }
