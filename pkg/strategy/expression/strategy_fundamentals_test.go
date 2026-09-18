package expression

import (
	"context"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// P2-12：把 PE / PB / ROE 接进表达式引擎。
//
// 「不给假数字」这条底线在 P2-12 之后换了个地方守：
//   - 生成层（pkg/ai/yaml）现在敢给 value / quality 出表达式了；
//   - 但**没有财报数据时，用 pe 必须报错**，绝不能静默返回 0。
//
// 这两个测试守的就是后半句。

func TestExpressionStrategy_ValueExprFailsWithoutFundamentals(t *testing.T) {
	s, err := NewExpressionStrategy("value_no_data", ExpressionStrategyConfig{
		SignalCfg: SignalConfig{
			Expression: "cs_rank(neg(pe)) > 0.8",
			Action:     "buy",
			Direction:  domain.DirectionLong,
		},
		SizingCfg: SizingConfig{Method: SizingEqual, MaxPerStock: 0.5, MaxTotal: 1.0},
	})
	if err != nil {
		t.Fatalf("NewExpressionStrategy: %v", err)
	}

	_, err = s.GenerateSignals(context.Background(),
		map[string][]domain.OHLCV{"A": makeBarsN("A", 5)}, nil)
	if err == nil {
		t.Fatal("没注入财报却用了 pe：必须报错，不能静默跑出一堆假信号")
	}
	t.Logf("预期内的失败：%v", err)
}

func TestExpressionStrategy_ValueExprWorksWithFundamentals(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := map[string][]domain.OHLCV{
		"CHEAP": makeBarsN("CHEAP", 5),
		"PRICY": makeBarsN("PRICY", 5),
	}
	records := map[string]strategy.FundamentalSeries{
		"CHEAP": {{Symbol: "CHEAP", Date: base, PE: fptr(8), PB: fptr(1), ROE: fptr(20)}},
		"PRICY": {{Symbol: "PRICY", Date: base, PE: fptr(60), PB: fptr(9), ROE: fptr(3)}},
	}

	s, err := NewExpressionStrategy("value_with_data", ExpressionStrategyConfig{
		SignalCfg: SignalConfig{
			Expression: "cs_rank(neg(pe)) > 0.5",
			Action:     "buy",
			Direction:  domain.DirectionLong,
		},
		SizingCfg: SizingConfig{Method: SizingEqual, MaxPerStock: 0.5, MaxTotal: 1.0},
	})
	if err != nil {
		t.Fatalf("NewExpressionStrategy: %v", err)
	}
	s.SetFundamentals(records)

	signals, err := s.GenerateSignals(context.Background(), bars, nil)
	if err != nil {
		t.Fatalf("注入财报后应当能跑：%v", err)
	}
	if len(signals) != 1 || signals[0].Symbol != "CHEAP" {
		t.Fatalf("低 PE 的那只该被选中，got %d signals: %+v", len(signals), signals)
	}
}

// TestExpressionStrategy_FundamentalsInjectedByEngine：引擎那侧的注入
// 靠类型断言，接口没实现上就静默跳过 —— 这个断言别丢。
func TestExpressionStrategy_ImplementsFundamentalAware(t *testing.T) {
	s, err := NewExpressionStrategy("t", defaultExpressionStrategyConfig())
	if err != nil {
		t.Fatalf("NewExpressionStrategy: %v", err)
	}
	var _ strategy.FundamentalAware = s

	got := map[string]strategy.FundamentalSeries{
		"A": {{Symbol: "A", Date: time.Now(), PE: fptr(10)}},
	}
	s.SetFundamentals(got)
	s.RLock()
	defer s.RUnlock()
	if s.fundamentals == nil {
		t.Fatal("SetFundamentals 之后 fundamentals 仍为 nil")
	}
	if _, ok := s.fundamentals["A"]; !ok {
		t.Error("注入的财报没留住")
	}
}
