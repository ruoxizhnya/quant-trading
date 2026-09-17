package yaml

import (
	"context"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/ai/expression"
	"github.com/ruoxizhnya/quant-trading/pkg/ai/intent"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// P0-5 的第二层根因：意图 → 可执行配置之间缺一层映射。
//
// Generator 只在 intent 自带 `signal_expr` 参数时才产出 expression 段；
// 规则解析（ruleBasedExtract）出来的 intent 只带语义类型（momentum /
// mean_reversion / ...），没有 signal_expr，于是 LoadStrategy 直接拒绝
// —— 它只认 expression 类型。链路就断在这里。
//
// 修法：给「能用价量表达」的意图类型一个确定性的默认表达式。映射写在
// 生成的 YAML 里，人能审阅、能改参数；AI 后续调的是旋钮，不是每次
// 重新发明一个策略。

func TestGenerate_PriceExpressibleTypesProduceLoadableStrategy(t *testing.T) {
	cases := []struct {
		name string
		typ  intent.StrategyType
	}{
		{"momentum", intent.StrategyTypeMomentum},
		{"mean_reversion", intent.StrategyTypeMeanReversion},
		{"trend_following", intent.StrategyTypeTrendFollowing},
		{"breakout", intent.StrategyTypeBreakout},
		{"multi_factor", intent.StrategyTypeMultiFactor},
	}

	g := NewGenerator()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i := &intent.Intent{
				StrategyName: "test_" + tc.name,
				StrategyType: tc.typ,
				Universe:     "csi300",
				Timeframe:    "1d",
			}
			yamlStr := g.Generate(i)
			require.NotEmpty(t, yamlStr)

			s, err := LoadStrategy(yamlStr)
			require.NoError(t, err, "生成的 YAML 必须能加载成可执行策略")
			assert.Equal(t, "test_"+tc.name, s.Name())

			cfg, err := ParseConfig(yamlStr)
			require.NoError(t, err)
			require.NotEmpty(t, cfg.Expression.Signal.Expression,
				"没有表达式就没有可执行内容")

			_, err = expression.NewParser().Parse(cfg.Expression.Signal.Expression)
			assert.NoError(t, err, "默认表达式必须能被表达式引擎解析")
		})
	}
}

// TestGenerate_FundamentalTypesDoNotFakeAnExpression：value / quality
// 要的是 PE / PB / ROE，而表达式引擎只有 OHLCV（open/high/low/close/
// volume/turnover）。给它们套一个无关的价格表达式，会跑出一堆看起来
// 像样、实则答非所问的回测数字 —— 那比直接失败更糟。
func TestGenerate_FundamentalTypesDoNotFakeAnExpression(t *testing.T) {
	for _, typ := range []intent.StrategyType{intent.StrategyTypeValue, intent.StrategyTypeQuality} {
		t.Run(string(typ), func(t *testing.T) {
			i := &intent.Intent{
				StrategyName: "test_" + string(typ),
				StrategyType: typ,
				Universe:     "csi300",
				Timeframe:    "1d",
			}
			_, err := LoadStrategy(NewGenerator().Generate(i))
			require.Error(t, err, "缺基本面数据时必须明确失败，而不是给假数字")
			assert.Contains(t, err.Error(), string(typ), "错误信息要说清是哪个类型")
		})
	}
}

// series 造一段价格序列。默认表达式用的是 20 日窗口，所以期数要够。
func series(symbol string, n int, price func(i int) float64) []domain.OHLCV {
	bars := make([]domain.OHLCV, 0, n)
	base := time.Now().AddDate(0, 0, -n)
	for i := 0; i < n; i++ {
		p := price(i)
		bars = append(bars, domain.OHLCV{
			Symbol: symbol,
			Date:   base.AddDate(0, 0, i),
			Open:   p, High: p, Low: p, Close: p, Volume: 1000,
		})
	}
	return bars
}

// trendBars 造三只走势分明的股票：一路涨 / 一路跌 / 横盘。
func trendBars() map[string][]domain.OHLCV {
	const n = 60
	return map[string][]domain.OHLCV{
		"RISER":  series("RISER", n, func(i int) float64 { return 10 + float64(i)*0.5 }),
		"FALLER": series("FALLER", n, func(i int) float64 { return 40 - float64(i)*0.5 }),
		"FLAT":   series("FLAT", n, func(i int) float64 { return 20 }),
	}
}

// TestDefaultExpressions_ProduceSensibleSignals：语法合法不等于语义有效。
// 默认表达式必须真能算出信号，而且方向要对 —— 动量买涨得最猛的，
// 均值回归买跌得最深的。两个方向相反的断言一起，能挡住「表达式写反了」。
func TestDefaultExpressions_ProduceSensibleSignals(t *testing.T) {
	cases := []struct {
		name     string
		typ      intent.StrategyType
		wantOnly string
	}{
		{"momentum_buys_the_riser", intent.StrategyTypeMomentum, "RISER"},
		{"mean_reversion_buys_the_faller", intent.StrategyTypeMeanReversion, "FALLER"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i := &intent.Intent{StrategyName: "sig_" + tc.name, StrategyType: tc.typ}
			s, err := LoadStrategy(NewGenerator().Generate(i))
			require.NoError(t, err)

			signals, err := s.GenerateSignals(context.Background(), trendBars(), nil)
			require.NoError(t, err)
			require.NotEmpty(t, signals, "默认表达式必须产出信号，不能是个摆设")

			symbols := make([]string, 0, len(signals))
			for _, sig := range signals {
				symbols = append(symbols, sig.Symbol)
			}
			assert.Equal(t, []string{tc.wantOnly}, symbols,
				"选中的标的应该只有 %s，实际是 %v", tc.wantOnly, symbols)
		})
	}
}

// TestGenerate_ExplicitSignalExprWins：显式指定的表达式优先于类型默认。
func TestGenerate_ExplicitSignalExprWins(t *testing.T) {
	i := &intent.Intent{
		StrategyName: "explicit",
		StrategyType: intent.StrategyTypeMomentum,
		Parameters:   []intent.Parameter{{Name: "signal_expr", Value: "cs_rank(volume) > 0.9"}},
	}
	cfg, err := ParseConfig(NewGenerator().Generate(i))
	require.NoError(t, err)
	assert.Equal(t, "cs_rank(volume) > 0.9", cfg.Expression.Signal.Expression)
}
