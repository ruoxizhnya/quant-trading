// K15 双跑对齐 · Go 侧权重回放执行器。
//
// 它是 nt-sidecar/sidecar/weight_replay.py 的 Go 镜像：把同一份目标权重
// 信号流翻译成调仓订单。**决策规则逐字复刻侧车**（目标仓位公式 / 涨跌停
// 不追 / T+1 卖出截断 / 参与率 / 买入整手），**撮合与记账走 Go 内核既有
// 实现**（Tracker.ExecuteTrade：T+1 执行层兜底、现金约束、ComputeFees 全
// 三项费率）——不重抄内核机制，两内核的差异只可能来自执行机制本身。
//
// 口径来源（与 weight_replay.py 逐条对拍）：
//
//	target_qty = floor(weight × capital_base / close / lot) × lot   恒定名义额
//	涨跌停     = prev_close ± limit_rate（half-away-from-zero 到分），触停不追
//	T+1        = 决策层卖出 ≤ 日初持仓；Tracker 执行层再兜底一次
//	参与率     = floor(volume × D_max) − 当日已成交（每日一单下与 Tracker
//	             的 liquidityFactor 单笔检查同参等价，双层 0.10）
//	买入整手   / 卖出允许零股（A 股规则，两侧同）
package main

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/rs/zerolog"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/contracts"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest/tracker"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// wrConfig 是回放器的口径参数——除 capital/signals 外全部有与侧车一致的
// 默认值；显式列出是为了让 diff 报告能引用「实验用的是哪套口径」。
type wrConfig struct {
	CapitalBase float64 // 恒定名义额的基准（期初本金）
	LimitRate   float64 // 主板 ±10%
	LotSize     int     // 买入整手 100 股
	DMax        float64 // 每日参与率上限
}

func defaultWRConfig(capital float64) wrConfig {
	return wrConfig{CapitalBase: capital, LimitRate: 0.10, LotSize: 100, DMax: 0.10}
}

// wrResult 是 Go 侧回放产物：净值曲线 + 成交明细（与 NT 侧 wire 同形，
// 两侧同喂 metrics.GenerateBacktestResult）。
type wrResult struct {
	PortfolioValues []domain.PortfolioValue
	Trades          []domain.Trade
}

// wrReplay 把信号流回放成 Go 内核的成交与净值序列。
//
// bars 按 symbol 分组、各自日期升序；逐**交易日**（多标的日历并集）循环，
// 日内按 symbol 定序处理——与 NT 为每个 symbol 建一个策略实例、按数据驱动
// 的消费顺序同构。
func wrReplay(
	cfg wrConfig,
	signals []contracts.SignalWeight,
	commissionRate float64,
	tradingCfg contracts.TradingConfig,
	bars map[string][]domain.OHLCV,
	logger zerolog.Logger,
) (*wrResult, error) {
	if cfg.CapitalBase <= 0 {
		return nil, fmt.Errorf("capital base must be positive, got %v", cfg.CapitalBase)
	}
	if len(bars) == 0 {
		return nil, fmt.Errorf("no bars supplied to the Go replay")
	}
	// K14-R1 A 的分摊口径：NT 为每个 symbol 各建一个策略实例，实例的
	// capital_base = initial_capital / N（组合级资金不超配）。Go 侧必须用
	// 同一个分摊基数，否则目标仓位整体放大 N 倍，双跑从公式层就不可比。
	capitalPerSymbol := cfg.CapitalBase / float64(len(bars))
	// 刻意 slippage=0：NT 的 bar 撮合无滑点，两侧必须同参（否则差异混入
	// 滑点口径而不是执行机制）。
	tr := tracker.NewTracker(cfg.CapitalBase, commissionRate, 0, tradingCfg, logger)

	// per-symbol day state —— 与侧车 WeightReplay 的 per-instance 状态同构。
	prevClose := make(map[string]float64)
	lastDay := make(map[string]time.Time)
	sellable := make(map[string]float64)
	tradedToday := make(map[string]float64)
	startOfDay := func(sym string, day time.Time) {
		if last, ok := lastDay[sym]; !ok || !day.Equal(last) {
			lastDay[sym] = day
			if pos, exists := tr.GetPosition(sym); exists && pos.Quantity > 0 {
				sellable[sym] = pos.Quantity // 日初持仓 = 昨日收盘持仓（T+1 口径）
			} else {
				sellable[sym] = 0
			}
			tradedToday[sym] = 0
		}
	}

	symbols := make([]string, 0, len(bars))
	for sym := range bars {
		symbols = append(symbols, sym)
	}
	sort.Strings(symbols)

	res := &wrResult{}
	for _, day := range sortedCalendar(bars) {
		// T+1 settlement rollover，与引擎每日循环同序（SetAsOf 先行）。
		tr.SetAsOf(day)
		tr.AdvanceDay(day)

		for _, sym := range symbols {
			bar := barOn(bars[sym], day)
			if bar == nil {
				continue // 停牌日：该 symbol 无 bar，侧车实例同样不被驱动
			}
			startOfDay(sym, day)
			price := bar.Close
			dayStart := dayOf(bar.Date)

			weight, ok := targetWeight(signals, sym, day)
			if ok {
				target := roundDownToLot(weight*capitalPerSymbol/price, cfg.LotSize)
				current := currentQty(tr, sym)
				delta := target - current

				switch {
				case delta > 0:
					// 触及涨停不追买（prev_close ±10%，首根 bar 无 prev_close 不检查）。
					if pc, seen := prevClose[sym]; seen {
						upper, _ := backtest.LimitPrices(pc, cfg.LimitRate)
						if price >= upper {
							break // no chase buy
						}
					}
					qty := math.Min(delta, participationCap(bar.Volume, cfg.DMax)-tradedToday[sym])
					qty = roundDownToLot(qty, cfg.LotSize)
					if qty > 0 {
						fill, err := tr.ExecuteTrade(sym, domain.DirectionLong, qty, price, dayStart, nil)
						if err != nil {
							// 与侧车 _resolve_unfilled 同语义：拒单记档并停发
							// 后续（目标不可达，逐 bar 重试只会重复拒单）。
							return nil, fmt.Errorf("go replay: buy %s failed on %s: %w", sym, day.Format("2006-01-02"), err)
						}
						tradedToday[sym] += fill.FilledQty
					}
				case delta < 0:
					// 触及跌停不追卖。
					if pc, seen := prevClose[sym]; seen {
						_, lower := backtest.LimitPrices(pc, cfg.LimitRate)
						if price <= lower {
							break // no chase sell
						}
					}
					// T+1：卖出 ≤ 日初可卖；参与率预算同买侧。
					want := math.Min(-delta, sellable[sym])
					qty := math.Min(want, participationCap(bar.Volume, cfg.DMax)-tradedToday[sym])
					if qty > 0 {
						fill, err := tr.ExecuteTrade(sym, domain.DirectionShort, qty, price, dayStart, nil)
						if err != nil {
							// Tracker 的 T+1 兜底只缩减、不拒绝缩减后的量；
							// 走到这里说明现金/状态异常，fail-loud。
							return nil, fmt.Errorf("go replay: sell %s failed on %s: %w", sym, day.Format("2006-01-02"), err)
						}
						tradedToday[sym] += fill.FilledQty
						sellable[sym] = math.Max(0, sellable[sym]-fill.FilledQty)
					}
				}
			}
			prevClose[sym] = price
		}

		// 每日收盘 mark-to-market（与 tracker.RecordDailyValue 同源）。
		closes := closingPrices(bars, day)
		tr.RecordDailyValue(dayStart(day), closes)
	}

	res.PortfolioValues = tr.GetPortfolioValues()
	res.Trades = tr.GetTrades()
	return res, nil
}

// targetWeight 返回 as_of ≤ 当日的最新权重（与侧车 SignalStream.as_of 同一
// 语义：未来信号物理不可见；同 symbol 同日多条时后一条胜出）。
func targetWeight(signals []contracts.SignalWeight, symbol string, day time.Time) (float64, bool) {
	best := time.Time{}
	found := false
	weight := 0.0
	for _, sig := range signals {
		if sig.Symbol != symbol {
			continue
		}
		asOf, err := time.Parse("2006-01-02", sig.AsOf)
		if err != nil || asOf.After(day) {
			continue
		}
		if !found || !asOf.Before(best) {
			best = asOf
			weight = sig.Weight
			found = true
		}
	}
	return weight, found
}

func barOn(series []domain.OHLCV, day time.Time) *domain.OHLCV {
	want := dayOf(day)
	for i := range series {
		if dayOf(series[i].Date).Equal(want) {
			return &series[i]
		}
	}
	return nil
}

func closingPrices(bars map[string][]domain.OHLCV, day time.Time) map[string]float64 {
	out := make(map[string]float64, len(bars))
	for sym, series := range bars {
		if bar := barOn(series, day); bar != nil {
			out[sym] = bar.Close
		}
	}
	return out
}

func currentQty(tr *tracker.Tracker, sym string) float64 {
	pos, exists := tr.GetPosition(sym)
	if !exists || pos == nil {
		return 0
	}
	return pos.Quantity
}

// roundDownToLot 买入整手（A 股 100 股整数倍），与 ashare.round_down_to_lot 一致。
func roundDownToLot(qty float64, lot int) float64 {
	if lot <= 0 || qty <= 0 {
		return 0
	}
	return math.Floor(qty/float64(lot)) * float64(lot)
}

// participationCap 与 ashare.participation_cap 一致：floor(D_max × volume)。
func participationCap(volume, dMax float64) float64 {
	if dMax <= 0 || volume <= 0 {
		return 0
	}
	return math.Floor(dMax * volume)
}

// sortedCalendar 多标的交易日并集（升序）——NT 侧合成曲线用同一日历。
func sortedCalendar(bars map[string][]domain.OHLCV) []time.Time {
	seen := make(map[time.Time]struct{})
	for _, series := range bars {
		for _, bar := range series {
			seen[dayOf(bar.Date)] = struct{}{}
		}
	}
	days := make([]time.Time, 0, len(seen))
	for d := range seen {
		days = append(days, d)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	return days
}

func dayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func dayStart(day time.Time) time.Time {
	return dayOf(day)
}
