package backtest

// P1-12 的回归测试：回测里的「今天」必须来自被回放的日期序列，不能来自墙钟。
//
// 为什么值得专门写测试：这个 bug 在单测里永远看不出来 —— 手写用例里
// Portfolio.UpdatedAt 都是显式给的好日期，走的正是正常分支。只有真跑引擎，
// 才会发现引擎根本没往快照里写日期，于是策略退回 time.Now()，
// weekly / monthly 的成交取决于运行当天是星期几。
//
// 两个测试的分工：
//   - TestEngine_PortfolioSnapshotCarriesBacktestDate：直接盯住契约本身，
//     完全确定性，不依赖今天是周几。
//   - TestEngine_WeeklyRebalanceTradesRegardlessOfWallClock：盯住用户能感知的
//     后果 —— weekly 回测必须有成交。

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/tracker"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy/examples"
)

// asOfSpy 记录引擎每次生成信号时，组合快照里带着的日期。
type asOfSpy struct {
	mu    sync.Mutex
	dates []time.Time
	sym   string
}

func (s *asOfSpy) Name() string        { return "asof-spy" }
func (s *asOfSpy) Description() string { return "记录引擎传进来的组合日期（P1-12 取证用）" }
func (s *asOfSpy) Parameters() []strategy.Parameter { return nil }
func (s *asOfSpy) Configure(map[string]interface{}) error { return nil }
func (s *asOfSpy) Weight(strategy.Signal, float64) float64 { return 0.1 }
func (s *asOfSpy) Cleanup()                                {}

func (s *asOfSpy) GenerateSignals(ctx context.Context, bars map[string][]domain.OHLCV, portfolio *domain.Portfolio) ([]strategy.Signal, error) {
	var d time.Time
	if portfolio != nil {
		d = portfolio.UpdatedAt
	}
	s.mu.Lock()
	s.dates = append(s.dates, d)
	s.mu.Unlock()

	return []strategy.Signal{{
		Symbol:    s.sym,
		Action:    "buy",
		Direction: domain.DirectionLong,
		Strength:  1.0,
		Date:      d,
	}}, nil
}

func (s *asOfSpy) seen() []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]time.Time, len(s.dates))
	copy(out, s.dates)
	return out
}

// TestEngine_PortfolioSnapshotCarriesBacktestDate 盯住契约：
// 策略看到的组合日期，必须逐日等于回测正在回放的交易日。
//
// 这是整个 P1-12 里唯一「跑哪天都成立」的断言 —— 它不比较墙钟，
// 所以周一跑和周四跑结论一样。
func TestEngine_PortfolioSnapshotCarriesBacktestDate(t *testing.T) {
	const nDays = 60
	eng, _, symbols := buildSyntheticEngine(t, 5, nDays, 1)

	spy := &asOfSpy{sym: symbols[0]}
	if err := strategy.GlobalRegister(spy); err != nil {
		t.Fatalf("注册 spy 策略失败：%v", err)
	}

	start := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	resp, err := eng.RunBacktest(context.Background(), BacktestRequest{
		Strategy:       spy.Name(),
		StockPool:      symbols,
		StartDate:      start.Format("2006-01-02"),
		EndDate:        start.AddDate(0, 0, nDays-1).Format("2006-01-02"),
		InitialCapital: 1_000_000,
		RiskFreeRate:   0.03,
	})
	if err != nil {
		t.Fatalf("回测失败：%v", err)
	}
	_ = resp

	seen := spy.seen()
	if len(seen) == 0 {
		t.Fatal("策略一次都没被调用 —— 没法取证组合日期")
	}

	// 契约一：每天都得有日期。零值意味着策略拿不到「今天」，
	// 只能退回墙钟（修复前正是如此）。
	// 契约二：日期必须逐日推进，且等于被回放的那一天。
	for i, d := range seen {
		want := start.AddDate(0, 0, i)
		if d.IsZero() {
			t.Fatalf("第 %d 次调用时组合快照没有日期 —— 策略会退回 time.Now()，"+
				"回测结果将取决于运行当天是星期几", i)
		}
		if !d.Equal(want) {
			t.Fatalf("第 %d 次调用：组合日期=%s，期望回测当天=%s（差 %v）",
				i, d.Format("2006-01-02"), want.Format("2006-01-02"), want.Sub(d))
		}
	}
	if len(seen) != nDays {
		t.Logf("注意：策略被调用 %d 次，交易日 %d 天（热身期不算）", len(seen), nDays)
	}
}

// TestEngine_WeeklyRebalanceTradesRegardlessOfWallClock 盯住后果：
// weekly 回测必须有成交。
//
// 修复前，引擎从不写 Portfolio.UpdatedAt，momentum 退回 time.Now() 判断
// 调仓日 —— 周四跑的时候 252 个交易日 0 笔成交，而回测状态照样 completed，
// 看起来像成功。daily 恒调仓，侥幸不受影响，所以这个问题藏了很久。
func TestEngine_WeeklyRebalanceTradesRegardlessOfWallClock(t *testing.T) {
	const nDays = 252
	eng, _, symbols := buildSyntheticEngine(t, 20, nDays, 1)

	ms := examples.NewMomentumStrategy()
	params := map[string]interface{}{
		"lookback_days":       20,
		"top_n":               5,
		"max_positions":       5,
		"rebalance_frequency": "weekly",
	}
	if err := ms.Configure(params); err != nil {
		t.Fatalf("configure momentum: %v", err)
	}
	// 全局 registry 是单例；已被注册就取出实例重新配置，不是错误。
	if err := strategy.GlobalRegister(ms); err != nil {
		existing, getErr := strategy.DefaultRegistry.Get("momentum")
		if getErr != nil {
			t.Fatalf("momentum 既注册不上也取不出：register=%v get=%v", err, getErr)
		}
		cfg, ok := existing.(strategy.Configurable)
		if !ok {
			t.Fatal("已注册的 momentum 不可配置，无法切到 weekly")
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
		t.Fatalf("weekly 回测失败：%v", err)
	}

	if resp.TotalTrades == 0 {
		t.Fatalf("weekly 回测 0 笔成交：%d 个交易日下来一次都没调仓。"+
			"这说明策略判断调仓日用的不是回测日期（今天是 %s）",
			nDays, time.Now().Weekday())
	}
	t.Logf("weekly：成交 %d 笔（今天是 %s，结果应与星期几无关）",
		resp.TotalTrades, time.Now().Weekday())
}

// TestTracker_AsOfDrivesPortfolioTimestamp 是契约的最内层：
// tracker 记住回测推进到哪一天，快照就带哪一天。
// 没设过 asOf 才用墙钟 —— 那是实时撮合的场景，不是回测。
func TestTracker_AsOfDrivesPortfolioTimestamp(t *testing.T) {
	tr := tracker.NewTracker(1_000_000, 0.0003, 0.0001,
		contracts.DefaultTradingConfig(), zerolog.Nop())

	if got := tr.AsOf(); !got.IsZero() {
		t.Fatalf("新建的 tracker 不该有 asOf，实际 %v", got)
	}

	day := time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC)
	tr.SetAsOf(day)

	if got := tr.AsOf(); !got.Equal(day) {
		t.Fatalf("AsOf=%v，期望 %v", got, day)
	}
	if got := tr.GetPortfolio(nil).UpdatedAt; !got.Equal(day) {
		t.Fatalf("快照 UpdatedAt=%v，期望回测当天 %v —— 策略会据此判断调仓日", got, day)
	}

	// Reset 之后必须回到「没有日期」，否则第二次回测会沿用上一天的日期。
	tr.Reset(1_000_000)
	if got := tr.AsOf(); !got.IsZero() {
		t.Fatalf("Reset 后 asOf 应清零，实际 %v", got)
	}
}
