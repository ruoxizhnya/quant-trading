// SignalProvider 契约（K8 · SignalSpec）——引擎取信号的唯一入口。
//
// 设计文档 §2.2：内核不关心信号从哪来（本地注册表 / 外部模型信号表 / 将来
// 阶段 2 的 HTTP、Redis 远程供给），只认这份契约。接口只有一个方法：
//
//	Signals(ctx, scope, asOf) ([]Signal, error)
//
// ─── 契约级语义：未来信号物理不可见 ────────────────────────────────
// 返回的信号必须满足 as_of <= asOf（按 DayOf 日粒度比较）。这是防前视的
// 契约边界——任何实现返回未来信号即违约（验收破坏腿①）。LocalSignalProvider
// 在输出前经 RejectFutureSignals fail-loud 把关；将来的远程实现复用同一守卫。
//
// 落位：pkg/strategy——Signal 类型与注册表都在这里，接口与它们同域；
// pkg/backtest 只依赖接口（依赖方向 backtest → strategy，不反向）。
package strategy

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	apperrors "github.com/ruoxizhnya/quant-trading/pkg/errors"
)

// Scope 是一次取信号调用的输入快照（契约参数包）。
type Scope struct {
	// Strategy 是注册表键（策略名）。
	Strategy string
	// Universe 是本次运行的标的池。
	Universe []string
	// MarketData 是「截至 asOf」的累积行情（symbol → 按日期升序的 OHLCV）。
	MarketData map[string][]domain.OHLCV
	// Portfolio 是调用方在 asOf 时刻的组合快照（批式 GenerateSignals 的
	// 第二参数）。流式路径不用；调用方没有 tracker 时可传 nil。
	Portfolio *domain.Portfolio
}

// SignalProvider 是可插拔信号供给契约（K8 · SignalSpec，设计文档 §2.2）。
type SignalProvider interface {
	// Signals 返回 as_of <= asOf 的信号（DayOf 日粒度）。未来信号物理
	// 不可见是契约级语义——返回未来信号即违约，调用方有权 fail-loud。
	Signals(ctx context.Context, scope Scope, asOf time.Time) ([]Signal, error)
}

// ─── 编译期合规检查（破坏验证腿②）──────────────────────────────────
// 删掉接口任一方法 → 方法表达式守卫编译失败（样板同 streaming.go）。
var (
	_ SignalProvider = (*LocalSignalProvider)(nil)

	_ func(SignalProvider, context.Context, Scope, time.Time) ([]Signal, error) = SignalProvider.Signals
)

// RejectFutureSignals fail-loud 拒绝任何 as_of > asOf 的信号（DayOf 日粒度）。
//
// 与 signal_strategy.go 的 rejectFutureSignals 同一裁决口径：未来信号不是
// 静默跳过而是报错——静默跳过会让「偷看未来」变成「悄悄少下单」，结果错了
// 还查不出来。Date 非 time.Time 或零值的信号跳过（无锚点的信号由调用方归
// asOf 当日，物理不可能是未来）。
func RejectFutureSignals(sigs []Signal, asOf time.Time) error {
	limit := DayOf(asOf)
	for _, s := range sigs {
		d, ok := s.Date.(time.Time)
		if !ok || d.IsZero() {
			continue
		}
		if DayOf(d).After(limit) {
			return fmt.Errorf(
				"strategy: 信号未来日期被拒 (date=%s > 当前回放 %s, symbol=%s)——防前视",
				d.UTC().Format(time.RFC3339), asOf.UTC().Format(time.RFC3339), s.Symbol)
		}
	}
	return nil
}

// LocalSignalProvider 是 SignalProvider 的本地实现（K8 ③.2）——包装现有
// 注册表 + 批式/流式双模式调用，行为与旧 pkg/backtest 的
// getSignalsFromLocalStrategy 逐字节一致（错误消息、错误码、op 均原样保留）。
type LocalSignalProvider struct {
	registry     *Registry
	factorReader FactorZScoreReader
	fundamentals func() map[string]FundamentalSeries
}

// NewLocalSignalProvider 构造本地实现。registry 传 nil 表示调用时取
// DefaultRegistry（延迟取：兼容 Init() 重建 DefaultRegistry 的时序）。
func NewLocalSignalProvider(registry *Registry) *LocalSignalProvider {
	return &LocalSignalProvider{registry: registry}
}

// SetFactorReader 绑定因子 z-score 读取器（引擎装配期绑 method value，
// 对应 FactorAware 注入，同旧实现每调用注入一次）。
func (p *LocalSignalProvider) SetFactorReader(r FactorZScoreReader) {
	p.factorReader = r
}

// SetFundamentalsProvider 绑定基本面快照供给（对应 FundamentalAware 注入）。
func (p *LocalSignalProvider) SetFundamentalsProvider(f func() map[string]FundamentalSeries) {
	p.fundamentals = f
}

// Signals 实现 SignalProvider 契约。
//
// 双模式检测顺序「优先流式 BarHandler，否则批式 SignalGenerator」的结构保证
// （K2 切片 2 原注释）：注册表存的是 Strategy 复合接口，它不嵌入 BarHandler，
// 现有批式策略的类型断言恒假、必然落到批式分支——这是类型系统的结构保证，
// 不靠运行时标志。只有同时实现 Strategy 与 BarHandler 的双模式策略才走流式。
func (p *LocalSignalProvider) Signals(ctx context.Context, scope Scope, asOf time.Time) ([]Signal, error) {
	reg := p.registry
	if reg == nil {
		reg = DefaultRegistry
	}
	strat, err := reg.Get(scope.Strategy)
	if err != nil {
		// fail-loud（OBS-11）：未注册策略名是装配错误。错误消息逐字节
		// 保留旧 getSignals 口径（含 op "getSignals"）。
		return nil, apperrors.Wrap(err, apperrors.ErrCodeNotFound,
			fmt.Sprintf("strategy %q not registered (strategy-service signal fallback removed per OBS-11/ADR-012); available: %v",
				scope.Strategy, ListStrategies()), "getSignals")
	}

	if fa, ok := strat.(FactorAware); ok && p.factorReader != nil {
		fa.SetFactorCache(p.factorReader)
	}
	if fu, ok := strat.(FundamentalAware); ok && p.fundamentals != nil {
		fu.SetFundamentals(p.fundamentals())
	}

	var signals []Signal

	if bh, ok := strat.(BarHandler); ok {
		// ── 流式路径 ─────────────────────────────────────────────
		// 喂序确定性（裁决：按 symbol 字典序）：marketData 是 map，Go 的
		// range 遍历顺序是随机的——同一份输入两次跑会喂出不同的 bar 序列，
		// 任何 per-symbol 递推状态的方向因此不确定，回测不可复现。
		for _, symbol := range sortedSymbols(scope.MarketData) {
			bars := scope.MarketData[symbol]
			if len(bars) == 0 {
				continue
			}
			bar := bars[len(bars)-1]
			// 只喂「当日」bar。marketData 是「截至当日」的累积历史，本方法
			// 每个交易日被调用一次。若当日该 symbol 无新 bar（停牌），其最后
			// 一根是更早的日期——重复喂会让递推状态吞下一根陈旧 bar。故按
			// DayOf 比对，非当日则跳过（不喂 = 这天没有新信息）。
			if !DayOf(bar.Date).Equal(DayOf(asOf)) {
				continue
			}
			if err := bh.OnBar(ctx, bar); err != nil {
				return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal,
					fmt.Sprintf("local streaming strategy %s OnBar failed at %s", scope.Strategy, bar.Date.Format("2006-01-02")),
					"getSignals")
			}
		}

		// 喂完当日全部 bar 后取走信号（取走即清空，防重复消费）。
		// Warmup 处理（裁决：引擎不设 warmup 门，依赖策略自守）：
		// Warmup() 是「静态声明需求」而非引擎维护的计数器，实现方负责
		// 据此自守，供给侧只负责喂 bar + 取走。
		signals = bh.Signals()
	} else {
		// ── 批式路径：一行未改 ───────────────────────────────────
		// 组合快照由调用方在 Scope 里给（旧实现的 extractLatestPrices +
		// tracker.GetPortfolio 提前到了调用方，GetPortfolio 是纯读）。
		signals, err = strat.GenerateSignals(ctx, scope.MarketData, scope.Portfolio)
		if err != nil {
			return nil, apperrors.Wrap(err, apperrors.ErrCodeInternal,
				fmt.Sprintf("local strategy %s failed", scope.Strategy), "getSignals")
		}
	}

	// 契约级防前视把关（破坏验证腿①的对象）：未来信号在这里被拒，
	// 不静默放行——删掉本调用，契约拒绝测试变红。
	if err := RejectFutureSignals(signals, asOf); err != nil {
		return nil, err
	}
	return signals, nil
}

// sortedSymbols 返回 map 的键，按字典序排好（喂序确定性）。generic 小助手
// 各包一份是既有惯例（pkg/backtest、pkg/backtest/tracker、pkg/live 同名同形）。
func sortedSymbols[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
