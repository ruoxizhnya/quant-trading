package backtest

// D6 · 多频率进回测策略视野（ADR-031 蓝图 §4.3 裁定 4）。
//
// 本文件是引擎侧的日内数据通道：**启动前快照预加载 + 分钟粒度 PIT 闸门
// + 具名日内特征供给**。三段分工：
//
//  1. 快照预加载（warmIntradayIfNeeded）：回测启动前经规范层 Router
//     （pkg/marketdata，D1 产物）取 1m bar 存进引擎 L1；回测期间只读，
//     **不给运行时查询** —— 与日线 warmCache、财报 warmFundamentals
//     同一红线（可复现 / 防前视 / 双核输入一致）。
//  2. 闸门（provider 内集中执行）：特征只许由 VisibleAt <= asOf 的 bar
//     算出（pkg/domain/market 的 OHLCV.VisibleBy，10:00 的 1m bar
//     10:01 才可见）。
//  3. 供给（snapshotIntradayProvider）：实现 strategy.IntradayFeatureProvider，
//     按回放日绑定，经 strategy.IntradayAware 注入策略。
//
// 红线（D6 验收②的护栏对象）：策略运行时禁止直接查 marketdata.Provider。
// 结构执法在 internal/repoguard（pkg/strategy 不许 import pkg/marketdata）
// 加本文件的取数收敛 —— 引擎侧 provider 访问点只有 warmIntradayIfNeeded
// 一条，且它只发生在启动前。

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/domain/market"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// intradayWarmChunkDays 是日内快照预热的分片长度（自然日）。
//
// 为什么分片：Router.Bars 对日内频率有单次跨度上限（REV-01 的
// maxIntradaySpan = 366 自然日，超限 fail-loud 而不是截断）。回测区间
// 动辄跨年，一次取全区间会被 Router 拒。分片值取 365：留一天余量，
// 不必与 marketdata 的未导出常量数字耦合（那里改上限这里不用动）。
//
// 按自然日分片而不是固定行数：bar 数随市场而变（A 股 240 根/日、
// 美股 390 根/日），自然日是「同一把尺」。
const intradayWarmChunkDays = 365

// 内置日内特征目录（SPEC.md「日内特征目录」的代码侧锚点）。
//
// 扩展纪律：加特征 = 本表加名 + intradayFeatureFuncs 加实现 + SPEC
// 目录同步 + 手算样本测试。词表封闭，provider 对目录外名字一律
// 「不可得」（不发明特征）。
const featureOpenMomentum30m = "open_momentum_30m"

var intradayFeatureCatalog = []string{featureOpenMomentum30m}

// 开盘半小时窗口（Asia/Shanghai，相对当日零点的偏移）：09:30–10:00。
// 集合竞价打点（09:15–09:25 成交）在窗口外，不进特征 —— 特征的
// 「开」取 09:30 首笔连续竞价 bar 的开盘价，不是竞价成交价。
const (
	openMomentumWindowStart = 9*time.Hour + 30*time.Minute
	openMomentumWindowEnd   = 10 * time.Hour
)

// intradayFeatureFunc 是一个日内特征的计算：dayStart 是回放日零点
// （Asia/Shanghai），bars 是**当日该 symbol 的分钟 bar**（任意频率，
// 1m/15m 同构 —— 对拍依据见 TestSnapshotIntradayProvider_FeatureParity），
// asOf 是分钟粒度回放时刻。返回 (值, 此刻是否可得)。
type intradayFeatureFunc func(dayStart time.Time, bars []domain.OHLCV, asOf time.Time) (float64, bool)

// intradayFeatureFuncs 是特征名 → 实现的封闭映射。
var intradayFeatureFuncs = map[string]intradayFeatureFunc{
	// open_momentum_30m：开盘半小时动量 = 窗口末 bar 收 / 窗口首 bar 开 - 1。
	// 1m 口径 = close(09:59)/open(09:30)-1；15m 口径 = 09:45 bucket 收 /
	// 09:30 bucket 开 - 1。按 D1 聚合规则（收=末笔收、开=首笔开），
	// 两个频率算出同一个数 —— 这正是「多频率输入进双核翻译点有对照
	// 验证」的对拍锚点。
	//
	// 可得条件（两条，缺一不可）：
	//  1. **窗口闭合**：存在一根周期跨过 10:00 的 bar —— 1m 即 09:59
	//     （VisibleAt 10:00），15m 即 09:45 bucket（VisibleAt 10:00）。
	//    没有它 = 半窗（半日停牌 / 数据断档）：半窗**不得**给出特征，
	//    那是个看起来完整的残缺样本；
	//  2. **已可见**：首、末两根 bar 的 VisibleAt 都不晚于 asOf。
	featureOpenMomentum30m: openMomentum30m,
}

func openMomentum30m(dayStart time.Time, bars []domain.OHLCV, asOf time.Time) (float64, bool) {
	winStart := dayStart.Add(openMomentumWindowStart)
	winEnd := dayStart.Add(openMomentumWindowEnd)
	first, last, ok := windowEdgeBars(bars, winStart, winEnd)
	if !ok {
		return 0, false // 当日窗口内无 bar（全日停牌）
	}
	// 闭合条件：末 bar 的周期必须跨过窗口终点（VisibleAt >= 10:00）。
	if last.VisibleAt.Before(winEnd) {
		return 0, false // 半窗：09:45 起停牌之类，不给残缺样本
	}
	// 两根都已可见。首 bar 恒早于末 bar 可见（stamp 升序 ⇒ VisibleAt
	// 升序），仍逐一判 —— 闸门判定不该靠排序不变量兜底。
	if !first.VisibleBy(asOf) || !last.VisibleBy(asOf) {
		return 0, false
	}
	if first.Open <= 0 {
		return 0, false // 开盘价非正：数据异常，不当成 0 动量
	}
	return last.Close/first.Open - 1, true
}

// windowEdgeBars 返回 bars 中 stamp 落在 [winStart, winEnd) 的**最早**
// 与**最晚**两根 bar。
//
// 不假设 bars 有序：首=最早、末=最晚按时间戳裁决，乱序输入也给确定
// 结果（Router 与 warm 路径本就有序，这里是防御性正确）。
func windowEdgeBars(bars []domain.OHLCV, winStart, winEnd time.Time) (first, last domain.OHLCV, ok bool) {
	for _, b := range bars {
		if b.Date.Before(winStart) || !b.Date.Before(winEnd) {
			continue
		}
		if !ok {
			first, last, ok = b, b, true
			continue
		}
		if b.Date.Before(first.Date) {
			first = b
		}
		if b.Date.After(last.Date) {
			last = b
		}
	}
	return first, last, ok
}

// ─── 引擎侧状态与注入 ────────────────────────────────────────────────

// SetIntradaySnapshot 让调用方直接注入日内分钟快照，跳过 Router 预热
// （D6；范式同 SetFundamentals / SetListingWindows）。
//
// 给测试和「没有 Router / 分钟数据在别处」的场景留的门。约定：
//   - map 键 = symbol；值为该 symbol **按 ts 升序**的 1m（或 15m）bar，
//     Frequency / VisibleAt 必须按规范层填好（闸门靠 VisibleAt 裁决，
//     没填 = 恒不可见，不会静默放行）；
//   - 传 nil = 没有这份数据：IntradayAware 策略用到特征时明确失败
//     （provider 恒不可得），不静默当 0。
func (e *Engine) SetIntradaySnapshot(bars map[string][]domain.OHLCV) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.intraday = bars
}

// SetRouter 注入规范层透明路由（D1 产物）—— D6 日内快照预加载的唯一
// 取数通道。nil = 关闭预热（策略用到日内特征会明确失败）。
func (e *Engine) SetRouter(r *marketdata.Router) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.router = r
	if r != nil {
		e.logger.Info().Msg("marketdata.Router attached — intraday snapshot warm-up enabled (D6)")
	}
}

// intradaySnapshot 返回当前日内快照（并发安全，只读语义同
// fundamentalsSnapshot：回测期间每个交易日对齐一次，不复制）。
func (e *Engine) intradaySnapshot() map[string][]domain.OHLCV {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.intraday
}

// warmIntradayIfNeeded 预热日内分钟快照（D6；范式同
// warmFundamentalsIfNeeded）。
//
// 三个「不」：
//  1. 策略不要就不查 —— 只有实现了 strategy.IntradayAware 的策略才预热，
//     纯日线策略不为分钟数据付取数/内存成本（回归红线：数值不变）。
//  2. 只走 Router —— 不经 provider 直查物理表（repoguard 护栏 +
//     本文件是引擎侧唯一 provider 访问点）。
//  3. 失败不阻断 —— 没有快照只是让日内特征策略明确报错，不是回测失败
//     （同财报预热语义）。
func (e *Engine) warmIntradayIfNeeded(ctx context.Context, strategyName string, symbols []string, start, end time.Time) error {
	strat, err := strategy.DefaultRegistry.Get(strategyName)
	if err != nil {
		return nil // 走外部 strategy-service，与本地日内预热无关
	}
	if _, ok := strat.(strategy.IntradayAware); !ok {
		return nil // 纯日线策略：不付这份成本
	}

	e.mu.RLock()
	router := e.router
	e.mu.RUnlock()
	if router == nil {
		// 没有 Router：不报错、不阻断。策略用到特征时由 provider 恒
		// 不可得 ⇒ 明确失败（缺失≠0），与「没传财报」同口径。
		return nil
	}

	// 交易日区间 → 取数区间：引擎的 start/end 是 **UTC 零点的交易日**，
	// 而 1m 路由按 ts BETWEEN 取数（D1 口径）—— 直接把 00:00 UTC
	// 当上界，会把当日 09:30 CST（= 01:30 UTC）之后的全部分钟 bar
	// 挡在门外（自灌自证测试当场抓到的缺口）。展开到
	// [start, end+1d) 覆盖每一天的完整交易时段。
	endExclusive := end.AddDate(0, 0, 1)

	snapshot, totalBars, err := loadIntradaySnapshot(ctx, router, symbols, start, endExclusive)
	if err != nil {
		return err
	}

	e.mu.Lock()
	e.intraday = snapshot
	e.mu.Unlock()

	e.logger.Info().
		Int("symbols", len(snapshot)).
		Int("minute_bars", totalBars).
		Time("start", start).
		Time("end", end).
		Msg("Intraday snapshot warm-up completed (D6)")
	return nil
}

// loadIntradaySnapshot 经 Router 分片取 [start, endExclusive) 的 1m bar
// （生产版自灌自证）。
//
// 分片理由见 intradayWarmChunkDays。逐 symbol 顺序取（不对 symbols
// 并行）：单标的全区间分钟量级已是 8.4MB 级（REV-01 实测口径），并行
// 会让峰值内存随并发数叠加 —— 预热是启动前一次性成本，顺序取换确定
// 的峰值，值得。
//
// 半开区间映射：Router 的 BETWEEN 是闭区间，末片上界退 1ns。分钟 bar
// 的 stamp 在实际数据里到秒（PG TIMESTAMPTZ 可到微秒），退 1ns 不漏
// 任何真实 bar，却把「次日零点整」这个不存在的 stamp 排除在语义外。
func loadIntradaySnapshot(
	ctx context.Context, router *marketdata.Router, symbols []string, start, endExclusive time.Time,
) (map[string][]domain.OHLCV, int, error) {
	snapshot := make(map[string][]domain.OHLCV, len(symbols))
	total := 0
	for _, sym := range symbols {
		for cs := start; cs.Before(endExclusive); cs = cs.AddDate(0, 0, intradayWarmChunkDays) {
			if err := ctx.Err(); err != nil {
				return nil, 0, err
			}
			ce := cs.AddDate(0, 0, intradayWarmChunkDays-1)
			if ce.After(endExclusive) {
				ce = endExclusive.Add(-time.Nanosecond)
			}
			bars, err := router.Bars(ctx, market.Freq1m, sym, cs, ce)
			if err != nil {
				return nil, 0, fmt.Errorf("backtest: intraday snapshot warm-up for %s [%s, %s]: %w",
					sym, cs.Format(time.RFC3339), ce.Format(time.RFC3339), err)
			}
			if len(bars) == 0 {
				continue // 停牌 / 无数据：缺失，不零填充
			}
			snapshot[sym] = append(snapshot[sym], bars...)
			total += len(bars)
		}
	}
	return snapshot, total, nil
}

// intradayFeatureProvider 构造「绑定回放日」的日内特征供给（引擎每个
// 交易日调一次，供 LocalSignalProvider 注入 IntradayAware 策略）。
//
// 为什么按日绑定而不是把整份快照交给策略：provider 只暴露**当日**
// 的分钟特征（dayRange 二分切片），策略拿不到其它交易日的 bar ——
// 这是「启动前快照」与「不许偷看别的日子」两道红线的交点。
func (e *Engine) intradayFeatureProvider(day time.Time) strategy.IntradayFeatureProvider {
	return &snapshotIntradayProvider{
		snapshot: e.intradaySnapshot(),
		dayStart: shanghaiDayStart(day),
		dayEnd:   shanghaiDayStart(day).AddDate(0, 0, 1),
	}
}

// shanghaiDayStart 返回时刻所属 Shanghai 自然日的起点。
// 引擎日循环的 date 是 UTC 零点（= Shanghai 08:00 同日），折算后
// 日历日不变。
func shanghaiDayStart(day time.Time) time.Time {
	d := day.In(market.AsiaShanghai)
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, market.AsiaShanghai)
}

// snapshotIntradayProvider 是 strategy.IntradayFeatureProvider 的快照实现
// （D6）。持有启动前预加载的快照引用 + 回放日边界；无网、无库、无
// provider 句柄 —— 结构上不可能运行时取数。
type snapshotIntradayProvider struct {
	snapshot map[string][]domain.OHLCV
	dayStart time.Time
	dayEnd   time.Time
}

// 编译期契约：实现必须是 strategy.IntradayFeatureProvider。
var _ strategy.IntradayFeatureProvider = (*snapshotIntradayProvider)(nil)

func (p *snapshotIntradayProvider) Features() []string {
	return append([]string(nil), intradayFeatureCatalog...)
}

func (p *snapshotIntradayProvider) IntradayFeature(
	ctx context.Context, symbol, name string, asOf time.Time,
) (float64, bool) {
	// ctx 只为取消；本实现零 IO，取一次取消态即可（热路径每 symbol
	// 每特征一次，不重复建 timer）。
	select {
	case <-ctx.Done():
		return 0, false
	default:
	}

	fn, ok := intradayFeatureFuncs[name]
	if !ok {
		return 0, false // 目录外名字：不可得（不发明特征）
	}
	bars := p.dayBars(symbol)
	if len(bars) == 0 {
		return 0, false
	}
	return fn(p.dayStart, bars, asOf)
}

// dayBars 返回快照中 symbol 属于回放日的 bar 段（二分查找，要求快照
// 按 ts 升序 —— Router 与 loadIntradaySnapshot 保证，约定见
// SetIntradaySnapshot 注释）。
//
// 二分而不是每日线性扫：整份快照是全区间历史，每个交易日对每个 symbol
// 线性扫一遍是 O(总 bar 数 × 交易日数)，一个 250 日 × 300 标的的回测
// 会扫出数十亿次比较；二分把每个 symbol 每天的代价降到 O(log n)。
func (p *snapshotIntradayProvider) dayBars(symbol string) []domain.OHLCV {
	all := p.snapshot[symbol]
	if len(all) == 0 {
		return nil
	}
	lo := sort.Search(len(all), func(i int) bool { return !all[i].Date.Before(p.dayStart) })
	hi := sort.Search(len(all), func(i int) bool { return !all[i].Date.Before(p.dayEnd) })
	if lo >= hi {
		return nil
	}
	return all[lo:hi]
}
