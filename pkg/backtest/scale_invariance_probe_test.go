package backtest

// AUD-53 的确定性取证：**换一个复权口径 / 换一个资金量级，回测数字会动多少，
// 以及动在哪里。**
//
// 为什么必须合成。P2-8 的原取证（pkg/data/tushare_adjustment_integration_test.go）
// 是「真库 + 真源 + 真引擎」，每跑一次要几分钟，而它撞出的两个现象在真数据上
// 没法拆开：
//
//  1. 每只票的价格乘**各自**的正数常数 → 收益差 21.6 个百分点；
//  2. 只改资金（价格一动不动）→ 收益差 20.7 个百分点。
//
// 第 2 条在数学上说不通：资金不进信号、不进权重
// （`weight = clamp(baseWeight × strength, 0.01, 0.10)` 与资金无关，
// `Portfolio.TotalValue` 又是权益口径），它唯一能进的地方是
// `floor(预算/价格)` 的离散化。而那条通道对**均匀缩放价格**同样敏感 ——
// 但均匀缩放只差 1.1pp。两者不能同时成立，所以至少有一条读错了。
//
// 这个文件用**收益序列逐点相同、只有价格水平与资金不同**的合成行情做因子实验，
// 毫秒级、无网络、无库。五条腿：
//
//	                    资金 1e6              资金 1e8
//	水平 ×1（基线）       A                     D
//	水平 ×9.04（统一）     B
//	水平 ×1.7~181（每票）  C  ← hfq 腿的真实形状
//	水平 ×9.04、资金 ×9.04  E  ← 尺度不变量（价格与资金同比例，预期几乎无差）
//
// 已排除的通道（在 pkg/risk/regime_scale_invariance_test.go 里证明）：
// **regime 判定**。它对统一缩放是严格不变量，对每票各自缩放实测 14/14 个
// 窗口判定完全一致 —— 「把各票价格拼成一根长序列」那个环节不是通道。
// （P2-8 登记里的那条猜测**已被推翻**，见 TASKS.md 的 AUD-53 行。）
//
// 有意**不**断言「零差异」：整手是交易所规则，股数必须是整数，所以
// 「同一策略在两种价格水平下给出逐位相同的结果」在数学上不可能。能要求的是
// **敏感度有界**，而不是消失。
//
// ═══════════════════════════════════════════════════════════════════════════
// 2026-09-24 更新：AUD-55 已修，本文件同时是它的**回归护栏**
//
// 根因（AUD-55）在下单侧：`computeEffectiveTarget` 只在 `PendingQty > 0` 时
// 才抵扣已持仓位，于是**无状态**策略（momentum 每天对 top-N 重发 `Long`）
// 每天重发一次全量买单、每天被 `insufficient cash` 拒一次。同一套合成数据，
// 修复前后对照：
//
//	腿                              修复前      修复后
//	水平×1 / 1e6（基线）             +7.08%     −0.0702%
//	水平×9.04（统一）/ 1e6           +8.03%     +2.5310%    （差 0.95 → 2.60pp）
//	水平×1.7~181（每票）/ 1e6        +11.30%    −10.3778%   （差 4.22 → 10.31pp）
//	水平×1 / 1e8（只改资金）          +2.26%     +0.6876%    （差 4.82 → 0.76pp）
//	水平×9.04、资金×9.04（尺度不变）   未测       +0.0215%    （差 0.09pp）
//	拒单（各腿）                     847~1856   0 / 0 / 122 / 215
//
// 三条结论：
//
//  1. **资金侧的通道塌了**：只改资金的差 4.82pp → 0.76pp，资金阶梯转为**收敛**
//     （最大跳幅 0.76pp、高端残差 0.0004pp）、基线腿与只改资金腿**零拒单**。
//     这直接证明 AUD-55 就是 AUD-53 在资金侧的通道，也让
//     `TestBacktestCapitalLadderConverges` 成为它的回归护栏。
//  2. **新增的尺度不变腿差 0.09pp，成交逐笔一致** —— 引擎在真正的不变量下
//     确实是不变量。这条腿给前几条的差异做**归因**（断言 5）。
//  3. **「每票各自缩放」的差没变小反而变大**（4.22 → 10.31pp），这不是回归：
//     那条腿的真通道是**绝对金额约束** —— hfq 价 = qfq 价 × 每股常数（1.7~181），
//     固定资金下高价票连一手都买不起（9 只被抬到一手、最大超买 ×68），
//     于是**票池成分被改写**。修复前那个缺陷恰好把这条效应**冲淡**了
//     （重复下单让买得起的票买过头、收益虚高），所以 10.31pp 是更真实的数字。
//     它对「拿固定资金去跑 hfq 复权价」这个组合的批评成立（P2-8 的 hfq 落库
//     决策见 TASKS.md 的 AUD-53 行），但它**不是引擎缺陷**。
// ═══════════════════════════════════════════════════════════════════════════

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/viper"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/fees"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy/examples"
)

const (
	probeDays    = 520
	probeSymbols = 20

	// P2-8 记下的两个真实幅度（真库 + 真源 + 真引擎，**AUD-55 修复前**）：
	// 每票各自缩放 21.6pp，只改资金 20.7pp。合成数据复现的是**形态**
	// （每票 ≫ 统一；只改资金同样显著），不是那两个具体数字 ——
	// 合成只有 20 只票、且票池与真数据无关。
	//
	// 修复 AUD-55（引擎对已持仓位重复下单）之后的实测值见文件头「修复后」一节。
	// 上界按修复后的值定，留一档余量；本文件全部断言是**确定性**的
	// （无随机、无网络、无库），所以上界收紧到「刚好能挡住硬性回归」即可。
	maxUniformLevelDriftPP = 3.0
	maxCapitalDriftPP      = 1.5
)

// probeBars 生成合成行情。level 只乘价格，**不改动任何一根 K 线的收益率**。
//
// 起始价位按几何级数拉开（3 → 约 380，≈126 倍，贴近 A 股前复权价的真实跨度）：
// 价格水平没有跨度的话，「水平敏感」这件事根本测不出来。
func probeBars(level float64) (map[string][]domain.OHLCV, []string) {
	start := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	symbols := make([]string, probeSymbols)
	data := make(map[string][]domain.OHLCV, probeSymbols)

	for i := 0; i < probeSymbols; i++ {
		sym := fmt.Sprintf("60%04d.SH", 1000+i)
		symbols[i] = sym

		rng := rand.New(rand.NewSource(int64(1000 + i)))
		price := 3.0 * math.Pow(1.29, float64(i)) * level
		bars := make([]domain.OHLCV, 0, probeDays)
		for d := 0; d < probeDays; d++ {
			date := start.AddDate(0, 0, d)
			drift := (rng.Float64() - 0.487) * 0.035
			open := price
			close := open * (1 + drift)
			high := math.Max(open, close) * (1 + rng.Float64()*0.01)
			low := math.Min(open, close) * (1 - rng.Float64()*0.01)
			vol := float64(1_000_000 + rng.Int63n(5_000_000))
			bars = append(bars, domain.OHLCV{
				Symbol: sym, Date: date,
				Open: open, High: high, Low: low, Close: close,
				Volume: vol, Turnover: (open + close) / 2 * vol,
			})
			price = close
		}
		data[sym] = bars
	}
	return data, symbols
}

// probePerSymbolFactors 造「每票各自的常数」，跨度与真数据里的复权基准相当
// （实测 1.7 ~ 181）。这条腿才是 hfq 腿的真实形状 ——
// 均匀缩放腿只是它的对照组。
func probePerSymbolFactors(symbols []string) map[string]float64 {
	out := make(map[string]float64, len(symbols))
	for i, sym := range symbols {
		out[sym] = 1.7 * math.Pow(181.0/1.7, float64(i)/float64(len(symbols)-1))
	}
	return out
}

func scaleBarsBy(bars map[string][]domain.OHLCV, factors map[string]float64) map[string][]domain.OHLCV {
	out := make(map[string][]domain.OHLCV, len(bars))
	for sym, src := range bars {
		c := factors[sym]
		dst := make([]domain.OHLCV, len(src))
		copy(dst, src)
		for i := range dst {
			dst[i].Open *= c
			dst[i].High *= c
			dst[i].Low *= c
			dst[i].Close *= c
			dst[i].Turnover *= c
		}
		out[sym] = dst
	}
	return out
}

// registerProbeMomentum 只注册一次，之后复用 —— 注册表是全局的。
func registerProbeMomentum(t *testing.T) {
	t.Helper()
	ms := examples.NewMomentumStrategy()
	if err := strategy.GlobalRegister(ms); err != nil {
		existing, getErr := strategy.DefaultRegistry.Get("momentum")
		if getErr != nil {
			t.Fatalf("momentum 既注册不上也取不出：register=%v get=%v", err, getErr)
		}
		cfg, ok := existing.(strategy.Configurable)
		if !ok {
			t.Fatal("已注册的 momentum 不可配置")
		}
		if err := cfg.Configure(probeParams()); err != nil {
			t.Fatalf("重配 momentum: %v", err)
		}
		return
	}
	if err := ms.Configure(probeParams()); err != nil {
		t.Fatalf("配置 momentum: %v", err)
	}
}

func probeParams() map[string]interface{} {
	return map[string]interface{}{
		"lookback_days":       20,
		"top_n":               5,
		"max_positions":       5,
		"rebalance_frequency": "daily",
	}
}

// probeOutcome 是一次回测的观测结果，含**拒单计数**。
//
// 拒单计数是这套取证的关键：`NormalizeOrderQuantity` 会把不足一手的订单
// **抬到一手**（100 股），而一手的金额是 `100 × 价格` —— 与仓位预算脱钩。
// 价格一旦高到「一手就超过可用现金」，`Tracker.ExecuteTrade` 就以
// `insufficient cash` 拒掉整笔单，于是**该票在这个口径下被静默移出票池**。
// 这条通道是系统性的（不是路径噪声），也正是「每票各自缩放」比「统一缩放」
// 凶得多的原因 —— 前者只把高基准的那几只票推出可交易集合。
type probeOutcome struct {
	res              *domain.BacktestResult
	insufficientCash int
	executeFailed    int
}

// probeRun 跑一次合成回测，并捕获 Warn 级日志里的拒单。
func probeRun(t *testing.T, bars map[string][]domain.OHLCV, symbols []string, capital float64) probeOutcome {
	t.Helper()
	// 止盈 / 止损乘数沿用默认（3.0）。要关掉出场逻辑、单独看「买入」这一侧的行为，
	// 用 probeRunEx（见 TestEngineDoesNotRebuySymbolsThatStayInTarget 的说明）。
	return probeRunEx(t, bars, symbols, capital, 3.0)
}

// probeRunEx 是 probeRun 的显式版本：可以覆盖**止盈乘数**。
//
// 为什么要这个参数：合成趋势行情里 ATR 很小，默认倍数的止盈会被**每天**触发，
// 于是「买 → 止盈 → 再买」本身就产出几十笔买单 —— 那与「引擎重复下单缺陷」
// 是两回事，混在一起就会把后者读成前者。把止盈乘数调到极大即可把**出场**逻辑
// 关掉，让买单只反映**入场**决策。（上行行情里止损不会触发，故无需覆盖。）
func probeRunEx(
	t *testing.T,
	bars map[string][]domain.OHLCV,
	symbols []string,
	capital, takeProfitMult float64,
) probeOutcome {
	t.Helper()

	prov := marketdata.NewInMemoryProvider()
	for sym, b := range bars {
		prov.LoadOHLCV(sym, b)
	}
	start := bars[symbols[0]][0].Date
	n := len(bars[symbols[0]])
	days := make([]time.Time, 0, n)
	for d := 0; d < n; d++ {
		days = append(days, start.AddDate(0, 0, d))
	}
	prov.SetTradingDays(days)

	stocks := make([]domain.Stock, 0, len(symbols))
	for _, sym := range symbols {
		stocks = append(stocks, domain.Stock{
			Symbol: sym, Name: "P-" + sym, ListDate: start.AddDate(-1, 0, 0),
		})
	}
	prov.LoadStocks(stocks)

	v := viper.New()
	v.Set("backtest.initial_capital", capital)
	v.Set("backtest.commission_rate", fees.DefaultCommissionRate)
	v.Set("backtest.slippage_rate", fees.DefaultSlippageRate)
	v.Set("backtest.risk_free_rate", 0.03)
	v.Set("backtest.seed", 7)
	v.Set("strategy_service.url", "http://localhost:8082")

	// 只留 Warn 及以上：拒单走 logger.Warn()（engine_daily.go「Failed to
	// execute long trade」），Info/Debug 全是逐日噪声。
	var logBuf bytes.Buffer
	logger := zerolog.New(zerolog.SyncWriter(&logBuf)).Level(zerolog.WarnLevel)

	eng, err := NewEngine(v, prov, logger)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	// 合成数据直接灌进引擎的内存缓存，绕开 provider 的按日切片 ——
	// 与 engine_synthetic_bench_test.go 同一套接线。
	eng.LoadOHLCVInMemory(bars)
	eng.SetParallelWorkers(1)

	rm, err := risk.NewRiskManager(risk.RiskManagerConfig{
		TargetVolatility: 0.15, MaxPositionWeight: 0.10, MinPositionWeight: 0.01,
		ATRPeriod: 14, BaseMultiplier: 2.0, BullMultiplier: 1.5, BearMultiplier: 3.0,
		SidewaysMultiplier: 2.0, TakeProfitMult: takeProfitMult, VolLookbackDays: 60,
		AnnualizationFactor: math.Sqrt(252), FastMAPeriod: 50, SlowMAPeriod: 200,
		RegimeVolLookback: 120,
	}, logger)
	if err != nil {
		t.Fatalf("NewRiskManager: %v", err)
	}
	eng.SetRiskManager(rm)

	registerProbeMomentum(t)

	resp, err := eng.RunBacktest(context.Background(), BacktestRequest{
		Strategy:       "momentum",
		StockPool:      symbols,
		StartDate:      start.Format("2006-01-02"),
		EndDate:        days[len(days)-1].Format("2006-01-02"),
		InitialCapital: capital,
		RiskFreeRate:   0.03,
	})
	if err != nil {
		t.Fatalf("RunBacktest(capital=%.0f): %v", capital, err)
	}
	if resp.Status != "completed" {
		t.Fatalf("回测未完成：status=%s err=%s", resp.Status, resp.Error)
	}

	logs := logBuf.String()
	return probeOutcome{
		res: &domain.BacktestResult{
			TotalReturn:     resp.TotalReturn,
			SharpeRatio:     resp.SharpeRatio,
			MaxDrawdown:     resp.MaxDrawdown,
			TotalTrades:     resp.TotalTrades,
			PortfolioValues: resp.PortfolioValues,
			Trades:          resp.Trades,
		},
		insufficientCash: strings.Count(logs, "insufficient cash"),
		executeFailed:    strings.Count(logs, "Failed to execute"),
	}
}

// probeTradeKey 把一笔成交压成「哪天、哪只票、买还是卖」—— 不含数量与价格。
// 数量在两腿之间本来就该差一个比例（不是分歧），要比的是**决策本身**。
func probeTradeKey(tr domain.Trade) string {
	return fmt.Sprintf("%s %s %s", tr.Timestamp.Format("2006-01-02"), tr.Symbol, tr.Direction)
}

// probeFirstDivergentDay 找出两条成交序列**第一次出现不同决策的日期**。
//
// 注意**不能按位置逐笔比**：一条腿只要在某一天多吐出一笔，后面全体错位，
// 按位置比出来的「第 N 笔岔开」是伪分歧（第一版就是这么读错的）。这里改成
// 按「(日期,票,方向) 的集合」比，先找出所有不对称的键，再取最早的那一天。
func probeFirstDivergentDay(a, b []domain.Trade) (day string, asymCount int, diverged bool) {
	count := func(trades []domain.Trade) map[string]int {
		m := make(map[string]int, len(trades))
		for _, tr := range trades {
			m[probeTradeKey(tr)]++
		}
		return m
	}
	ca, cb := count(a), count(b)

	keys := make(map[string]struct{}, len(ca)+len(cb))
	for k := range ca {
		keys[k] = struct{}{}
	}
	for k := range cb {
		keys[k] = struct{}{}
	}

	asym := make([]string, 0)
	for k := range keys {
		if ca[k] != cb[k] {
			asym = append(asym, k)
		}
	}
	if len(asym) == 0 {
		return "", 0, false
	}
	sort.Strings(asym)
	return asym[0][:10], len(asym), true
}

func probeSumQty(trades []domain.Trade) float64 {
	total := 0.0
	for _, tr := range trades {
		total += math.Abs(tr.Quantity)
	}
	return total
}

// budgetClamp 看一个仓位预算在给定价位上会被 `NormalizeOrderQuantity`
// 怎么处理 —— 纯算术，用来把「哪些票被推出可交易集合」列出来。
type budgetClamp struct {
	dropped    int     // 目标不足 1 股 → 直接归零，永不成交
	lifted     int     // 1 ≤ 目标 < 100 股 → 抬到一手
	maxOverBet float64 // 抬升后金额 / 预算 的最大倍率
}

func analyzeBudgetClamp(symbols []string, bars map[string][]domain.OHLCV, budget float64) budgetClamp {
	var out budgetClamp
	out.maxOverBet = 1.0
	for _, sym := range symbols {
		b := bars[sym]
		if len(b) == 0 {
			continue
		}
		price := b[0].Close
		target := budget / price
		switch {
		case target < 1:
			out.dropped++
		case target < risk.LotSize:
			out.lifted++
			if over := risk.LotSize * price / budget; over > out.maxOverBet {
				out.maxOverBet = over
			}
		}
	}
	return out
}

// TestBacktestSensitivityToPriceLevelAndCapital 是本文件的主体。
//
// 断言的是**敏感度有界**，不是「无差异」—— 理由见文件头。
func TestBacktestSensitivityToPriceLevelAndCapital(t *testing.T) {
	const (
		smallCapital = 1_000_000.0
		hugeCapital  = 100_000_000.0
	)

	baseBars, symbols := probeBars(1.0)
	uniformFactors := make(map[string]float64, len(symbols))
	for _, sym := range symbols {
		uniformFactors[sym] = 9.04
	}
	perSymFactors := probePerSymbolFactors(symbols)

	type leg struct {
		name    string
		bars    map[string][]domain.OHLCV
		capital float64
	}
	legs := []leg{
		{"水平×1（基线），资金 1e6     ", baseBars, smallCapital},
		{"水平×9.04（统一），资金 1e6  ", scaleBarsBy(baseBars, uniformFactors), smallCapital},
		{"水平×1.7~181（每票），资金 1e6", scaleBarsBy(baseBars, perSymFactors), smallCapital},
		{"水平×1，资金 1e8（只改资金） ", baseBars, hugeCapital},
		// 第五条腿是**真正的尺度不变量**：价格与资金**同比例**缩放。
		// 前四条腿都在动「预算 / 价格」这个比值（于是改了哪些票买得起、要不要
		// 抬到一手），这一条不动它 —— 整手是 100 股、股数不变，滑点与税率都是
		// 百分比，唯一不随之缩放的是**最低佣金**这类绝对金额费用。
		//
		// 它的作用是给前面几条的差异**归因**（AUD-55 修复后新增）：如果这条腿
		// 也有几个百分点的差，那剩下的敏感度就不是绝对金额约束造成的，
		// 「根因是重复下单」这个结论就还没到底。
		{"水平×9.04，资金 1e6×9.04（尺度不变）", scaleBarsBy(baseBars, uniformFactors), smallCapital * 9.04},
	}

	outcomes := make([]probeOutcome, len(legs))
	for i, l := range legs {
		outcomes[i] = probeRun(t, l.bars, symbols, l.capital)
	}
	base := outcomes[0]

	t.Logf("合成敏感度矩阵（%d 票 × %d 交易日；价格只被正数常数缩放，收益率序列逐点相同）",
		probeSymbols, probeDays)
	for i, l := range legs {
		o := outcomes[i]
		t.Logf("  %s 收益 %10.4f%%  夏普 %7.4f  成交 %4d 笔  累计股数 %13.0f  拒单 %3d",
			l.name, o.res.TotalReturn*100, o.res.SharpeRatio, o.res.TotalTrades,
			probeSumQty(o.res.Trades), o.insufficientCash)
	}

	// 仓位预算上限 = 该腿资金 × MaxPositionWeight(0.10)。逐腿用自己的资金算，
	// 否则「尺度不变腿」会被按基线的预算分类，看起来像被推出票池。
	// 列出每个口径下哪些票会被「一手下限」处理掉 —— 这是整手敏感度的来源。
	t.Logf("  逐票按首日收盘价分类（预算 = 该腿资金 × MaxPositionWeight 0.10）：")
	for _, l := range legs {
		cl := analyzeBudgetClamp(symbols, l.bars, l.capital*0.10)
		t.Logf("    %s 预算 %10.0f  归零(目标<1股) %2d 只  抬到一手(目标<100股) %2d 只  最大超买 ×%.2f",
			l.name, l.capital*0.10, cl.dropped, cl.lifted, cl.maxOverBet)
	}

	drift := func(name string, o probeOutcome) float64 {
		d := math.Abs(o.res.TotalReturn-base.res.TotalReturn) * 100
		t.Logf("  → %s 与基线的收益差 = %.4f 个百分点（拒单 %d 笔）", name, d, o.insufficientCash)
		if day, n, diverged := probeFirstDivergentDay(base.res.Trades, o.res.Trades); diverged {
			t.Logf("      首次决策分岔：%s（不对称的成交键 %d 个）", day, n)
		} else {
			t.Logf("      决策**逐笔一致**（差异只在数量/价格上）")
		}
		return d
	}

	dUniform := drift("统一缩放", outcomes[1])
	dPerSym := drift("每票各自缩放", outcomes[2])
	dCapital := drift("只改资金", outcomes[3])
	dInvariant := drift("尺度不变（价格与资金同比例）", outcomes[4])

	// 断言 1：统一缩放的水平敏感度有上界。
	if dUniform > maxUniformLevelDriftPP {
		t.Errorf("统一价格缩放的收益差 %.4f 个百分点，超过上界 %.1f —— 「水平敏感度有界」这条不成立",
			dUniform, maxUniformLevelDriftPP)
	}

	// 断言 2：资金敏感度有上界。这条是 AUD-53 的核心 —— 只改资金（价格不动、
	// 信号不动、权重不动）就足以让结果动几个百分点。
	//
	// 实测（2026-09-24，AUD-55 修复后）：0.76pp，修复前是 4.82pp 且**不随资金收敛**
	// （1e8→1e9 差 5.17pp、1e9→1e10 差 8.80pp）。上界因此从 12.0 收到 1.5。
	if dCapital > maxCapitalDriftPP {
		t.Errorf("只改资金的收益差 %.4f 个百分点，超过上界 %.1f —— 「资金敏感度有界」这条不成立",
			dCapital, maxCapitalDriftPP)
	}

	// 断言 3（**形状断言，不是数值断言**）：每票各自缩放至少要产生与统一缩放
	// 同量级的影响，因为它同时把跨票相对水平拉开。这条把 P2-8 观测到的形态
	// （每票 ≫ 统一）钉住，防止有人把它当测量误差忽略掉。
	if dPerSym < dUniform {
		t.Errorf("每票各自缩放的差（%.4f）小于统一缩放（%.4f）—— 与 P2-8 真数据上的形态相反，需复核通道是否还接在跨票水平上",
			dPerSym, dUniform)
	}

	// 断言 4（**通道断言**）：通道必须真的被走到 —— 至少有一票被「一手下限」
	// 抬升，且至少有一条腿出现拒单。没有这条，「敏感度有界」可能只是
	// 因为这套合成数据恰好没触发那个环节，护栏就成了摆设。
	perSymClamp := analyzeBudgetClamp(symbols, legs[2].bars, legs[2].capital*0.10)
	if perSymClamp.lifted == 0 {
		t.Errorf("每票各自缩放的腿里没有任何票被抬到一手（dropped=%d lifted=%d）—— 「通道被走到」不成立，这测的是别的东西",
			perSymClamp.dropped, perSymClamp.lifted)
	}
	if base.res.TotalTrades == 0 {
		t.Fatal("基线零成交 —— 这测的是「没跑起来」，不是敏感度")
	}
	t.Logf("  → 通道确认：每票腿里 %d 只票被抬到一手（最大超买 ×%.2f），各腿拒单数已列于上表",
		perSymClamp.lifted, perSymClamp.maxOverBet)

	// 断言 5（**归因断言**，AUD-55 修复后新增）：把价格与资金**同比例**缩放
	// 之后，差异必须塌到接近零。
	//
	// 理由：这条腿不动「预算 / 价格」比 —— 股数逐位相同、整手不触发、
	// 滑点与税率都是百分比。剩下的前四条腿的差异因此可以归因到**绝对金额
	// 约束**（一手 = 100 × 价格、最低佣金），而不是「引擎对水平量级有病态
	// 依赖」。修复前这条腿之外还存在**每天重发全量买单**这条通道，它让差异
	// 不随任何比例收敛；现在那条通道没了，这条断言才立得住。
	if dInvariant > 0.5 {
		t.Errorf("尺度不变腿（价格与资金同比例缩放）的收益差 %.4f pp 超过 0.5 —— 这条腿在"+
			"经济上应当几乎无差（整手、滑点、税率都不变，只剩最低佣金这类绝对金额费用），"+
			"差得多说明绝对金额约束之外仍有通道，AUD-55 的修复没修到根上", dInvariant)
	}
	if dInvariant >= dUniform {
		t.Errorf("尺度不变腿的差（%.4f）不小于「只缩放价格」（%.4f）—— 归因不成立："+
			"若缩放什么都被同样放大，那就不是绝对金额约束在起作用",
			dInvariant, dUniform)
	}
}

// TestBacktestCapitalLadderConverges 判定「只改资金」的漂移是**量化误差**，
// 还是**无标度的混沌**。
//
// 这个区分是 AUD-53 定性的关键。矩阵测试发现：资金 ×100（价格不动）会让收益
// 动几个百分点，而那一腿**一只票都没被抬到一手**（预算足够大，`budget/price > 100`
// 恒成立）—— 所以「一手下限」不是全部通道。
//
// 判定办法：把资金按 10 倍阶梯往上走。如果是量化误差 ——
//
//	整数股 + 整手取整的相对精度 ∝ 一手金额 / 仓位金额 = 100×price / (w×TotalValue)
//
// 那么资金每放大 10 倍，相对量化误差就降 10 倍，结果应当**收敛**到连续极限。
//
// ⚠️ 这个函数换过命题，两次都是实测定的，不是猜的：
//
//   - **2026-09-23**（AUD-53 取证）：实测**不收敛** —— 1e8→1e9 差 5.17pp、
//     1e9→1e10 差 8.80pp，而 1e10 的相对量化已是 1e-4 量级；且**每档**拒单
//     777~1005 笔。当时结论是「路径混沌：不同资金给出的是同一条混沌轨道的
//     不同取法，彼此没有收敛关系」，函数名就叫 `...DoNotConverge...`。
//   - **2026-09-24**（AUD-55 修复后）：拒单**归零**、阶梯**收敛**（最大跳幅
//     0.76pp、高端残差 0.0004pp）。混沌源就是那个「每天重发全量买单」的缺陷；
//     修掉之后剩下的只是可控的整手量化误差。
//
// 所以这条测试现在是 AUD-55 的**回归护栏**：若哪天它重新不收敛、或拒单重新
// 成批出现，说明「重复下单 → 撞现金上限」那条通道又通了。
func TestBacktestCapitalLadderConverges(t *testing.T) {
	bars, symbols := probeBars(1.0)

	capitals := []float64{1e6, 1e7, 1e8, 1e9, 1e10}
	returns := make([]float64, len(capitals))
	trades := make([]int, len(capitals))
	rejects := make([]int, len(capitals))
	lifted := make([]int, len(capitals))
	budget := 0.0
	for i, c := range capitals {
		o := probeRun(t, bars, symbols, c)
		returns[i] = o.res.TotalReturn * 100
		trades[i] = o.res.TotalTrades
		rejects[i] = o.insufficientCash
		// 逐票按首日价、预算 = 资金 × MaxPositionWeight(0.10) 分类。
		lifted[i] = analyzeBudgetClamp(symbols, bars, c*0.10).lifted
		budget = c * 0.10
	}
	_ = budget

	t.Logf("资金阶梯（价格水平固定 ×1；策略信号与权重均与资金无关，唯一入口是下单量化）：")
	for i, c := range capitals {
		rel := 100 * risk.LotSize * bars[symbols[0]][0].Close / (c * 0.10)
		t.Logf("  资金 %8.0e  收益 %10.4f%%  成交 %4d 笔  拒单 %4d  抬一手 %2d 只  相对量化 ≈ %.1e",
			c, returns[i], trades[i], rejects[i], lifted[i], rel)
	}

	// 相邻量级之间的跳幅。
	step := make([]float64, 0, len(capitals)-1)
	for i := 1; i < len(capitals); i++ {
		step = append(step, math.Abs(returns[i]-returns[i-1]))
	}
	t.Logf("  相邻量级跳幅（pp）：")
	for i, d := range step {
		t.Logf("    %.0e → %.0e : %.4f", capitals[i], capitals[i+1], d)
	}
	t.Logf("  高资金端残差 |%.0e − %.0e| = %.4f pp", capitals[len(capitals)-2], capitals[len(capitals)-1],
		step[len(step)-1])

	// 断言 1（**收敛有界**）：阶梯上任何一跳都走不远。修复前最大跳幅 8.80pp。
	worst := 0.0
	for _, d := range step {
		if d > worst {
			worst = d
		}
	}
	if worst > 1.0 {
		t.Errorf("资金阶梯上最大跳幅 %.4f pp —— 这与「量化误差、随资金衰减」不符："+
			"要么重复下单那条通道又通了，要么另有一条绝对金额之外的通道", worst)
	}

	// 断言 2（**收敛形状**）：高资金端残差必须远小于低资金端跳幅。
	// 相对量化误差 ∝ 1/资金，资金每放大 10 倍降 10 倍 —— 收敛的话高端应当塌下去。
	// 修复前这里是反过来的：1e9→1e10 比 1e6→1e7 还大。
	if step[len(step)-1] > step[0]/4 {
		t.Errorf("高资金端残差 %.4f pp 不低于低端跳幅 %.4f pp 的四分之一 —— 不呈现收敛，"+
			"与「整手量化误差」这条归因不符", step[len(step)-1], step[0])
	}
	if step[0] <= 0 {
		t.Fatal("低资金端跳幅为 0 —— 这测的是「资金没起作用」，不是敏感度")
	}

	// 断言 3（**AUD-55 的直接指纹**）：全阶梯**零拒单**。
	//
	// 修复前每一档都是 777~1005 笔：无状态策略每天重发全量买单，撞上现金上限
	// 就被拒 —— 而「哪几笔被拒」由现金路径决定，于是结果随资金跳。
	// 零拒单说明下单量已经被「目标 − 已持」正确抵扣掉了。
	for i, c := range capitals {
		if rejects[i] != 0 {
			t.Errorf("资金 %.0e 一档出现 %d 笔拒单 —— `insufficient cash` 成批出现是"+
				"AUD-55（重复下单）复发的直接指纹", c, rejects[i])
		}
	}
}

// trendBars 造 n 只**同向上行、且排序稳定**的票（各自水平不同），
// 用来保证它们每一天都留在 top-N —— 这是触发「重复下单」的必要条件。
func trendBars(n, days int) (map[string][]domain.OHLCV, []string) {
	start := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	symbols := make([]string, n)
	data := make(map[string][]domain.OHLCV, n)
	for i := 0; i < n; i++ {
		sym := fmt.Sprintf("60%04d.SH", 2000+i)
		symbols[i] = sym
		price := 10.0 * float64(i+1)
		drift := 0.02 + 0.002*float64(i)
		bars := make([]domain.OHLCV, 0, days)
		for d := 0; d < days; d++ {
			open := price
			close := open * (1 + drift)
			bars = append(bars, domain.OHLCV{
				Symbol: sym, Date: start.AddDate(0, 0, d),
				Open: open, High: close * 1.001, Low: open * 0.999, Close: close,
				Volume: 5_000_000, Turnover: (open + close) / 2 * 5_000_000,
			})
			price = close
		}
		data[sym] = bars
	}
	return data, symbols
}

// TestEngineDoesNotRebuySymbolsThatStayInTarget 是 AUD-55 的**回归护栏**。
//
// 逻辑链（全部可从代码读出，这里把它变成可执行断言）：
//
//	momentum 是**无状态**策略 —— 对 top-N 每天都发 `Long`，从不发 `Close`（momentum.go:219-250）。
//	「已持有什么」这件事因此完全落在引擎的目标仓位账上
//	（`computeEffectiveTarget` / `updateTargetPositionAfterTrade`，engine_daily.go）。
//
// **修复前**的缺陷（AUD-55）：`computeEffectiveTarget` 只在 `PendingQty > 0` 时
// 才做「目标 − 已持」抵扣，而 `PendingQty` 是上次成交后写的缓存 —— `PendingQty == 0`
// （恰好达标）与 `PendingQty < 0`（超配）两种情况下抵扣都不生效。于是每天重发
// 全量买单、每天被 `insufficient cash` 拒一次。**实测读数**：3 只票 60 个交易日、
// 出场已关闭，每票仍被买 **5~6 次**、拒单 **103** 笔。
//
// **修复后**（2026-09-24，抵扣改为无条件 + 已持仓实时读 tracker）：每票恰好
// 买 **1** 次、拒单 **0** 笔。
//
// 注意这个测试**关掉出场**（止盈乘数拉到 1e6）：合成趋势行情里 ATR 很小，
// 默认倍数的止盈会被每天触发，「买 → 止盈 → 再买」本身就能造出几十笔买单，
// 与「引擎重复下单」是两回事。混在一起就会把前者读成后者。
func TestEngineDoesNotRebuySymbolsThatStayInTarget(t *testing.T) {
	const (
		days    = 60
		capital = 1e8

		// 把止盈乘数拉到极大 —— 关掉**出场**，只观察入场决策。
		// 第一版没做这件事，读数被「买→止盈→再买」污染：止盈在小 ATR 的
		// 趋势行情里每天触发，本身就能造出几十笔买单。见 probeRunEx 的说明。
		tpOff = 1e6
	)
	bars, symbols := trendBars(3, days)
	o := probeRunEx(t, bars, symbols, capital, tpOff)

	buys, closes := 0, 0
	for _, tr := range o.res.Trades {
		switch tr.Direction {
		case domain.DirectionLong:
			buys++
		case domain.DirectionClose:
			closes++
		}
	}
	t.Logf("3 只持续位于 top-N 的票，资金 %.0e，%d 个交易日，**出场已关闭**：",
		capital, days)
	t.Logf("  成交 %d 笔（买入 %d / 平仓 %d），拒单 %d 笔", len(o.res.Trades), buys, closes, o.insufficientCash)

	multiBuy := 0
	for _, sym := range symbols {
		var n int
		var qty float64
		for _, tr := range o.res.Trades {
			if tr.Symbol == sym && tr.Direction == domain.DirectionLong {
				n++
				qty += tr.Quantity
			}
		}
		t.Logf("  %s：买单 %2d 笔，累计买入 %8.0f 股", sym, n, qty)
		if n != 1 {
			multiBuy++
			t.Errorf("%s 的买单 %d 笔，期望恰好 1 笔 —— 目标与已持相等后不应再下单（AUD-55 复发？）", sym, n)
		}
	}

	// 断言 1（**缺陷本体**）：没有出场的世界里，一只一直在目标集合里的票
	// 只应当被买**一次** —— 第二天起「目标 − 已持」抵扣掉全部已持仓位，
	// 不再产生委托。修复前这里是 5~6 笔（每票），这条断言正是那时的读数，
	// 逐票的笔数断言在上面的循环里。
	if multiBuy != 0 {
		t.Errorf("有 %d 只票被重复买入 —— `computeEffectiveTarget` 又退化回"+
			"「只在 PendingQty > 0 时抵扣」了（AUD-55 复发）", multiBuy)
	}

	// 断言 1b（**拒单指纹**）：修复前 103 笔 `insufficient cash` ——
	// 重复下单把钱耗到买不起，再撞现金上限。修复后应当一笔都没有。
	if o.insufficientCash != 0 {
		t.Errorf("出现 %d 笔 `insufficient cash` 拒单 —— 这与「下单量已被正确抵扣」"+
			"不符，是 AUD-55 复发的直接指纹", o.insufficientCash)
	}

	// 断言 2：既然出场被关掉，运行期间就不该出现任何平仓 —— 只允许**期末强平**
	//（`forceCloseAllPositions`）在最后一个交易日平掉持仓。若中间某天平了仓，
	// 说明「出场已关闭」这个前提不成立，断言 1 的读数仍是被污染的。
	lastDay := bars[symbols[0]][len(bars[symbols[0]])-1].Date
	intraRunExits := 0
	for _, tr := range o.res.Trades {
		if tr.Direction == domain.DirectionClose && tr.Timestamp.Before(lastDay) {
			intraRunExits++
			t.Logf("  ⚠ 运行期间平仓：%s %s", tr.Timestamp.Format("2006-01-02"), tr.Symbol)
		}
	}
	if intraRunExits != 0 {
		t.Errorf("出场已关闭却仍有 %d 笔运行期间的平仓（期末强平不算）—— 前提不成立，本测试的读数不可用",
			intraRunExits)
	}
}
