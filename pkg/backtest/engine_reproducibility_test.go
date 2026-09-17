package backtest

// 回测可复现性的取证：同一份输入跑两次，结果必须逐字节相同。
//
// 为什么值得测：回测结论是拿来做决策的，一次跑赢、再跑一次变输，
// 那这个数字就没有意义。Go 里最常见的非确定性来源是 map 遍历顺序 ——
// 策略遍历 bars map、给候选排序（sort.Slice 对相同 key 不稳定）、
// 再截 top-N，一条链路下来，谁先谁后每次都可能不同。
//
// 这个测试不假设结论：发现不一致就报出第一个分歧点，而不是"大概一样"。

import (
	"context"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy/examples"
)

// runOnce 用一个全新的引擎跑一遍回测，模拟「今天跑一次、明天再跑一次」。
func runOnce(t *testing.T, nDays int, topN int) *BacktestResponse {
	t.Helper()

	eng, _, symbols := buildSyntheticEngine(t, 20, nDays, 1)

	ms := examples.NewMomentumStrategy()
	params := map[string]interface{}{
		"lookback_days":       20,
		"top_n":               topN,
		"max_positions":       topN,
		"rebalance_frequency": "daily",
	}
	if err := ms.Configure(params); err != nil {
		t.Fatalf("configure momentum: %v", err)
	}
	if err := strategy.GlobalRegister(ms); err != nil {
		existing, getErr := strategy.DefaultRegistry.Get("momentum")
		if getErr != nil {
			t.Fatalf("momentum 既注册不上也取不出：register=%v get=%v", err, getErr)
		}
		cfg, ok := existing.(strategy.Configurable)
		if !ok {
			t.Fatal("已注册的 momentum 不可配置")
		}
		if err := cfg.Configure(params); err != nil {
			t.Fatalf("reconfigure momentum: %v", err)
		}
	}

	start := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	resp, err := eng.RunBacktest(context.Background(), BacktestRequest{
		Strategy:       "momentum",
		StockPool:      symbols,
		StartDate:      start.Format("2006-01-02"),
		EndDate:        start.AddDate(0, 0, nDays-1).Format("2006-01-02"),
		InitialCapital: 1_000_000,
		RiskFreeRate:   0.03,
	})
	if err != nil {
		t.Fatalf("回测失败：%v", err)
	}
	if resp.Status != "completed" {
		t.Fatalf("回测未完成：status=%s err=%s", resp.Status, resp.Error)
	}
	return resp
}

func TestEngine_SameInputProducesSameResult(t *testing.T) {
	const nDays = 252
	a := runOnce(t, nDays, 5)
	b := runOnce(t, nDays, 5)

	// 标量指标：确定性计算在相同输入下必须逐位相同。
	scalars := []struct {
		name string
		x, y float64
	}{
		{"TotalReturn", a.TotalReturn, b.TotalReturn},
		{"AnnualReturn", a.AnnualReturn, b.AnnualReturn},
		{"SharpeRatio", a.SharpeRatio, b.SharpeRatio},
		{"SortinoRatio", a.SortinoRatio, b.SortinoRatio},
		{"MaxDrawdown", a.MaxDrawdown, b.MaxDrawdown},
		{"WinRate", a.WinRate, b.WinRate},
		{"AvgHoldingDays", a.AvgHoldingDays, b.AvgHoldingDays},
		{"CalmarRatio", a.CalmarRatio, b.CalmarRatio},
	}
	for _, s := range scalars {
		if s.x != s.y {
			t.Errorf("%s 不可复现：第一次 %.10g，第二次 %.10g（差 %.3g）",
				s.name, s.x, s.y, s.x-s.y)
		}
	}
	if a.TotalTrades != b.TotalTrades {
		t.Errorf("成交笔数不可复现：%d vs %d", a.TotalTrades, b.TotalTrades)
	}

	// 净值曲线：逐点比对。
	if len(a.PortfolioValues) != len(b.PortfolioValues) {
		t.Fatalf("净值曲线长度不同：%d vs %d", len(a.PortfolioValues), len(b.PortfolioValues))
	}
	for i := range a.PortfolioValues {
		if a.PortfolioValues[i].TotalValue != b.PortfolioValues[i].TotalValue {
			t.Fatalf("净值第 %d 点不可复现（%s）：%.10g vs %.10g",
				i, a.PortfolioValues[i].Date.Format("2006-01-02"),
				a.PortfolioValues[i].TotalValue, b.PortfolioValues[i].TotalValue)
		}
	}

	// 成交序列：ID 可能自带随机成分，不算结果，其余逐笔比对。
	if len(a.Trades) != len(b.Trades) {
		t.Fatalf("成交条数不同：%d vs %d", len(a.Trades), len(b.Trades))
	}
	for i := range a.Trades {
		x, y := a.Trades[i], b.Trades[i]
		if x.Symbol != y.Symbol || x.Direction != y.Direction ||
			x.Quantity != y.Quantity || x.Price != y.Price ||
			!x.Timestamp.Equal(y.Timestamp) {
			t.Fatalf("第 %d 笔成交不可复现：\n  第一次 %s %s %.4f@%.4f %s\n  第二次 %s %s %.4f@%.4f %s",
				i, x.Symbol, x.Direction, x.Quantity, x.Price, x.Timestamp.Format("2006-01-02"),
				y.Symbol, y.Direction, y.Quantity, y.Price, y.Timestamp.Format("2006-01-02"))
		}
	}

	t.Logf("两次独立回测逐位一致：%d 笔成交、%d 个净值点、收益 %.6f",
		len(a.Trades), len(a.PortfolioValues), a.TotalReturn)
}

// TestEngine_TopNSelectionIsOrderIndependent 单独盯住最可疑的一环：
// top-N 截断。momentum 用 sort.Slice（不稳定排序）给候选排名，
// 若两只票动量相同，谁进前 N 取决于 map 遍历顺序 —— 每次跑都可能不同。
//
// 构造方式：所有票走出完全相同的价格路径（动量值相同），
// 于是「选哪 N 只」完全由排序稳定性决定。
func TestEngine_TopNSelectionIsOrderIndependent(t *testing.T) {
	a := runOnce(t, 60, 5)
	b := runOnce(t, 60, 5)

	symbolsA := map[string]int{}
	for _, tr := range a.Trades {
		symbolsA[tr.Symbol]++
	}
	symbolsB := map[string]int{}
	for _, tr := range b.Trades {
		symbolsB[tr.Symbol]++
	}

	if len(symbolsA) != len(symbolsB) {
		t.Errorf("两次回测选中的股票数不同：%d vs %d —— 排序不稳定会让 top-N 每次挑到不同的票",
			len(symbolsA), len(symbolsB))
	}
	for sym, n := range symbolsA {
		if symbolsB[sym] != n {
			t.Errorf("股票 %s 成交次数不可复现：%d vs %d", sym, n, symbolsB[sym])
		}
	}
}
