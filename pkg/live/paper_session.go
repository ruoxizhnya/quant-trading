package live

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/clock"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
)

// CurrentBarSource 是「能从当根 bar 定价」的 paper trader 可选实现。
//
// MockTrader 实现它；PaperSession 在 Run 前把「(当前日, symbol) → Quote」
// 的闭包注入进去，于是 MockTrader 的市价单参考价取当根 bar 的 Close、
// 成本核取当根 bar 的高低/量。真实券商不实现它（它有自己的行情），
// PaperSession 检测不到就跳过 —— 这是「实盘-ready 边界」而不是缺陷。
type CurrentBarSource interface {
	SetCurrentBarProvider(func(symbol string) (Quote, bool))
}

// PaperFill 是 paper 回放产出的一条成交。字段刻意最小化且**确定性**：
// 不含 OrderID / 墙钟时间戳，因为那些由 id.OrderID() / time.Now() 生成、
// 逐次运行不同 —— 成交流要可复现，就不能把非确定量放进来。
type PaperFill struct {
	Date   time.Time        // 成交所在的交易日（回放日，非墙钟）
	Symbol string           //
	Side   domain.Direction // DirectionLong = 买 / DirectionClose = 卖
	Qty    float64          // 成交量（股）
	Price  float64          // 实际成交价（含滑点/冲击）
	Fee    float64          // 该笔总费用（佣金 + 过户费 + 卖出印花税）
}

// PaperSessionConfig 是 PaperSession 的依赖注入集。**禁全局单例**：
// provider / clock / trader / 执行配置全部由调用方注入，测试可替换。
type PaperSessionConfig struct {
	// Provider 提供交易日历与批量日线（与回测同一份 marketdata.Provider）。
	Provider marketdata.Provider
	// Clock 是回放时间源。必须注入（通常 NewVirtualClock(start)）——
	// 它是「回放与回测同一时间观」的保证，缺省不得静默用墙钟。
	Clock clock.Clock
	// Trader 是撮合/代持通道（paper 下通常是 *MockTrader）。
	Trader LiveTrader
	// ExecutionConfig 是执行配置（成本模型参数来源）。
	ExecutionConfig domain.ExecutionConfig
	// Feed 可选；nil 时由 Provider + Clock 自动构造一个 BarReplayFeed。
	Feed *BarReplayFeed
	// Logger 可选；零值时用 zerolog.Nop()。
	Logger zerolog.Logger
	// SignalCollector 可选；非 nil 时每个交易日生成 domain.Signal 后回调
	// （K5 切片 2 的信号对账用：把 paper 侧信号序列导出来，与回测侧
	// getSignalsFromLocalStrategy 产出的信号做 ReconcileSignals 比对）。
	// nil = 不收集（零行为变化）。
	SignalCollector func(date time.Time, signals []domain.Signal)
}

// PaperSession 是 paper 回放的编排器：把「已落库的日线 bar」按时间回放、
// 喂给策略、经 LiveTrader 撮合、产出成交流。
//
// ─── 与既有 LiveEngine 的关系（裁决） ────────────────────────────
//
// 不复用 LiveEngine。LiveEngine 是**实时**编排器：Start 会起 goroutine、
// 接 DataFeed 的 push 回调、按分钟 ticker mark-to-market —— 它的时间是
// 墙钟、节奏不可控、结果不可复现。PaperSession 是**确定性回放**编排器：
// 时间由 VirtualClock 逐步推进、逐日同步撮合、无 goroutine。两者对「时间」
// 的假设根本不同，硬塞进一个类型只会让 LiveEngine 要么多一套模式开关、
// 要么把确定性需求漏进实时路径。故 K5 只让二者共享**零件**（DataFeed
// 契约、LiveTrader、成本核），不共享编排。切片 2 的同构对账会以 PaperSession
// 的成交流为 paper 侧输入、回测成交流为另一侧 —— 那时才是二者汇合点。
//
// ─── 与 LiveBridge 的关系（裁决） ────────────────────────────────
//
// 不 import pkg/backtest/execution.LiveBridge：live_bridge.go 已经
// import pkg/live，若 pkg/live 反向 import pkg/backtest/execution 即构成
// **import cycle**。故本文件直接驱动 LiveTrader，并复用 LiveBridge 的
// 信号→委托换算口径（strength→quantity，见 signalQuantity），把「同一
// 口径」写进注释而非写第二份实现。这是循环依赖下的最小让步。
type PaperSession struct {
	provider marketdata.Provider
	feed     *BarReplayFeed
	trader   LiveTrader
	execCfg  domain.ExecutionConfig
	logger   zerolog.Logger

	// currentBars 是「当前回放日」的 bar 快照（symbol → Quote）。撮合前
	// 更新，经 CurrentBarSource 钩子喂给 trader 定价/算成本。
	currentBars map[string]Quote

	// signalCollector 是 K5 切片 2 的信号对账钩子（可选）。
	signalCollector func(date time.Time, signals []domain.Signal)
}

// NewPaperSession 依赖注入构造。缺 Provider / Clock / Trader 一律 fail-loud，
// 不静默补默认值（一个悄悄用墙钟的 paper 回放会破坏确定性，且现场难查）。
func NewPaperSession(cfg PaperSessionConfig) (*PaperSession, error) {
	if cfg.Provider == nil {
		return nil, fmt.Errorf("paper session: Provider is required")
	}
	if cfg.Clock == nil {
		return nil, fmt.Errorf("paper session: Clock is required (inject a VirtualClock for deterministic replay)")
	}
	if cfg.Trader == nil {
		return nil, fmt.Errorf("paper session: Trader is required")
	}
	feed := cfg.Feed
	if feed == nil {
		feed = NewBarReplayFeed(cfg.Provider, cfg.Clock)
	}
	logger := cfg.Logger
	if logger.GetLevel() == zerolog.Disabled {
		// 零值 Logger（或显式 Disabled）落到 Nop，避免在 nil writer 上写日志。
		logger = zerolog.Nop()
	}
	return &PaperSession{
		provider:        cfg.Provider,
		feed:            feed,
		trader:          cfg.Trader,
		execCfg:         cfg.ExecutionConfig,
		logger:          logger,
		currentBars:     map[string]Quote{},
		signalCollector: cfg.SignalCollector,
	}, nil
}

// Run 跑完整段回放，产出按 (Date, Symbol) 排序的成交流。
//
// ─── 日循环（读法 A 的核心：与回测语义一致） ──────────────────────
//
//  1. 交易日列表来自 provider.GetTradingDays（升序）；
//  2. 用 feed.Replay **一次**物化整段（内部只调一次 BulkLoadOHLCV），
//     按 (交易日升序, symbol 字典序) 逐根推进 VirtualClock；这样就与回测
//     吃**同一批** bar；
//  3. 组合起点为该 trader 的 InitialCapital（由构造它的 MockTraderConfig
//     决定），每日从 trader 快照出 portfolio；
//  4. 对每个交易日 d（升序）：窗口 = **只含 Date <= d 的 bars**（与
//     pkg/backtest/engine.go:getSignalsFromLocalStrategy 的 marketData
//     累积语义一致，也是防前视的那条线）：
//     - 流式策略（实现 strategy.BarHandler）→ 当日各 symbol 的 bar 按
//     symbol 字典序逐根 OnBar，再 Signals()（取走即清空）；
//     - 批式策略 → GenerateSignals(ctx, 窗口, portfolio)；
//     - 信号经 LiveTrader 撮合，记 PaperFill{Date, Symbol, Side, Qty,
//     Price, Fee}，并 roll T+1。
//
// 返回：按 (Date, Symbol) 排序的成交流。单笔撮合失败（如现金不足、
// T+1 违规）记日志跳过，不中断整段回放 —— 与 LiveBridge.ExecuteSignals
// 的「单信号失败不中断后续」一致。
func (s *PaperSession) Run(ctx context.Context, symbols []string, start, end time.Time, strat strategy.Strategy) ([]PaperFill, error) {
	if strat == nil {
		return nil, fmt.Errorf("paper session: nil strategy")
	}
	if len(symbols) == 0 {
		return nil, fmt.Errorf("paper session: no symbols")
	}

	// 1) 交易日历（升序）。
	days, err := s.provider.GetTradingDays(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("paper session: get trading days: %w", err)
	}

	// 2) 一次物化 + 按日归档（回放内部只 BulkLoadOHLCV 一次）。
	barsByDay := map[string]map[string]domain.OHLCV{}
	if err := s.feed.Replay(ctx, symbols, start, end, func(q marketdata.Quote) error {
		key := dayKey(q.Timestamp)
		if barsByDay[key] == nil {
			barsByDay[key] = map[string]domain.OHLCV{}
		}
		barsByDay[key][q.Symbol] = quoteToOHLCV(q)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("paper session: replay: %w", err)
	}

	// 日历为空则退化为「数据里出现过的交易日」——库没填日历不该让回放
	// 静默产出零成交（那会读成「策略没信号」而非「没数据」）。
	dayKeys := dayKeysFromCalendar(days)
	if len(dayKeys) == 0 {
		dayKeys = sortedKeys(barsByDay)
	}

	// 3) 注入当根 bar 来源（trader 若支持）。
	if cbs, ok := s.trader.(CurrentBarSource); ok {
		cbs.SetCurrentBarProvider(func(symbol string) (Quote, bool) {
			q, ok := s.currentBars[symbol]
			return q, ok
		})
	}

	// 4) 日循环。
	window := map[string][]domain.OHLCV{}
	fills := make([]PaperFill, 0)
	for _, key := range dayKeys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		d := parseDayKey(key)
		todays := barsByDay[key]

		// 窗口累积：把当天新 bar 并入 Date<=d 的历史（防前视的物化点）。
		for _, sym := range sortedKeys(todays) {
			window[sym] = append(window[sym], todays[sym])
		}

		// 当根 bar 快照，供 trader 定价/算成本。
		s.currentBars = quotesFromBars(todays)

		// 信号。
		stratSignals, err := s.generateSignals(ctx, strat, window, todays)
		if err != nil {
			return nil, fmt.Errorf("paper session: signals at %s: %w", key, err)
		}
		domainSignals := toDomainSignals(stratSignals, d)

		// K5 切片 2：信号对账钩子（在撮合前导出，此时信号尚未受 T+1/现金
		// 约束影响，是与回测 getSignalsFromLocalStrategy 产出的信号同构的
		// 那一层）。
		if s.signalCollector != nil {
			s.signalCollector(d, domainSignals)
		}

		// 撮合。
		for _, sig := range domainSignals {
			fill, err := s.executeSignal(ctx, d, sig)
			if err != nil {
				s.logger.Warn().Err(err).
					Str("date", key).Str("symbol", sig.Symbol).
					Msg("paper session: order skipped")
				continue
			}
			if fill != nil {
				fills = append(fills, *fill)
			}
		}

		// T+1 结算滚动（MockTrader 实现 AdvanceDay）。
		if adv, ok := s.trader.(interface{ AdvanceDay() }); ok {
			adv.AdvanceDay()
		}
	}

	// 最终定序：按 (Date, Symbol) 升序。信号顺序不保证符号有序（尤其
	// 批式策略返回的切片），成交流必须确定性有序。
	sort.SliceStable(fills, func(i, j int) bool {
		if !fills[i].Date.Equal(fills[j].Date) {
			return fills[i].Date.Before(fills[j].Date)
		}
		return fills[i].Symbol < fills[j].Symbol
	})

	s.logger.Info().
		Int("symbols", len(symbols)).
		Int("days", len(dayKeys)).
		Int("fills", len(fills)).
		Msg("paper session replay complete")
	return fills, nil
}

// generateSignals 按策略形态选执行模式（与回测 getSignalsFromLocalStrategy
// 同一判定顺序：优先流式 BarHandler，否则批式 GenerateSignals）。
func (s *PaperSession) generateSignals(
	ctx context.Context,
	strat strategy.Strategy,
	window map[string][]domain.OHLCV,
	todays map[string]domain.OHLCV,
) ([]strategy.Signal, error) {
	if bh, ok := strat.(strategy.BarHandler); ok {
		// 流式：只喂「当日」新 bar（停牌则当日无 bar，不重复喂旧 bar ——
		// 与回测同日同一理由：重复喂会让递推状态吞下陈旧 bar）。
		for _, sym := range sortedKeys(todays) {
			if err := bh.OnBar(ctx, todays[sym]); err != nil {
				return nil, fmt.Errorf("OnBar %s: %w", sym, err)
			}
		}
		return bh.Signals(), nil
	}
	// 批式：给「截至当日」的累积窗口 + 当前组合。
	return strat.GenerateSignals(ctx, window, s.portfolioSnapshot(ctx))
}

// portfolioSnapshot 从 trader 快照当前组合（现金 + 持仓）。组合的唯一
// 事实源是 trader，避免 PaperSession 自己再维护一份会与撮合分叉的账。
func (s *PaperSession) portfolioSnapshot(ctx context.Context) *domain.Portfolio {
	p := &domain.Portfolio{Positions: map[string]domain.Position{}}
	if acct, err := s.trader.GetAccount(ctx); err == nil && acct != nil {
		p.Cash = acct.Cash
		p.TotalValue = acct.TotalAssets
	}
	if positions, err := s.trader.GetPositions(ctx); err == nil {
		for _, pi := range positions {
			p.Positions[pi.Symbol] = domain.Position{
				Symbol:        pi.Symbol,
				Quantity:      pi.Quantity,
				AvgCost:       pi.AvgCost,
				CurrentPrice:  pi.CurrentPrice,
				MarketValue:   pi.MarketValue,
				UnrealizedPnL: pi.UnrealizedPnL,
			}
		}
	}
	return p
}

// executeSignal 把一个信号换成委托并撮合，产出 PaperFill。
//
// 换算口径刻意与 pkg/backtest/execution.LiveBridge.ExecuteSignal 一致
// （strength→quantity = 100*max(1, strength*10)，封顶 10000），因为
// 循环依赖使本包无法直接复用 LiveBridge。市价单 price=0；限价单缺价时
// 退回当根 bar 的 Close。
func (s *PaperSession) executeSignal(ctx context.Context, d time.Time, sig domain.Signal) (*PaperFill, error) {
	orderType := sig.OrderType
	if orderType == "" {
		orderType = domain.OrderTypeMarket
	}
	price := 0.0
	if orderType == domain.OrderTypeLimit {
		price = sig.LimitPrice
		if price <= 0 {
			if q, ok := s.currentBars[sig.Symbol]; ok {
				price = q.Close
			}
		}
	}
	quantity := signalQuantity(sig.Strength)

	result, err := s.trader.SubmitOrder(ctx, sig.Symbol, sig.Direction, orderType, quantity, price)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}

	fillPrice := result.FillPrice
	if fillPrice <= 0 {
		fillPrice = result.Price // 兜底：未回填 FillPrice 的 trader
	}
	return &PaperFill{
		Date:   d,
		Symbol: sig.Symbol,
		Side:   result.Direction,
		Qty:    result.FilledQty,
		Price:  fillPrice,
		Fee:    result.Fee,
	}, nil
}

// signalQuantity 复刻 LiveBridge 的 strength→quantity 换算。
func signalQuantity(strength float64) float64 {
	quantity := 100.0
	if strength > 0 {
		scaled := 1.0
		if strength*10 > 1.0 {
			scaled = strength * 10
		}
		quantity = 100.0 * scaled
		if quantity > 10000 {
			quantity = 10000
		}
	}
	return quantity
}

// toDomainSignals 把 strategy.Signal 换成 domain.Signal。
//
// 这是 pkg/backtest/engine.go:convertStrategySignals 的等价物（那份是
// 未导出函数，跨包不可复用）。语义逐条对齐：跳过 "hold"；方向优先取
// Direction，否则由 Action(buy/sell) 推导；日期缺省用当日；limit price
// 缺省用 signal.Price；orderType 缺省 market。任何偏离都会让 paper 与
// 回测对同一策略产出不同的下单量，破坏同构，故此处保守复制。
func toDomainSignals(signals []strategy.Signal, defaultDate time.Time) []domain.Signal {
	out := make([]domain.Signal, 0, len(signals))
	for _, sg := range signals {
		if sg.Action == "hold" {
			continue
		}
		dir := sg.Direction
		if dir == "" || dir == domain.DirectionHold {
			switch sg.Action {
			case "buy":
				dir = domain.DirectionLong
			case "sell":
				dir = domain.DirectionClose
			default:
				continue
			}
		}
		sigDate := defaultDate
		if t, ok := sg.Date.(time.Time); ok && !t.IsZero() {
			sigDate = t
		}
		limitPrice := sg.LimitPrice
		if limitPrice == 0 {
			limitPrice = sg.Price
		}
		orderType := sg.OrderType
		if orderType == "" {
			orderType = domain.OrderTypeMarket
		}
		out = append(out, domain.Signal{
			Symbol:         sg.Symbol,
			Date:           sigDate,
			Direction:      dir,
			Strength:       sg.Strength,
			CompositeScore: sg.Strength,
			Factors:        sg.Factors,
			Metadata:       sg.Metadata,
			LimitPrice:     limitPrice,
			OrderType:      orderType,
		})
	}
	return out
}

// ─── 小工具（确定性排序 / 日期键） ─────────────────────────────────

// sortedKeys 返回 map 的键的字典序切片。map 遍历序随机，任何「按 map 顺序
// 产出」的地方都必须先排序，否则同一输入两次运行结果不同。
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func dayKey(t time.Time) string { return t.UTC().Format("2006-01-02") }

func parseDayKey(key string) time.Time {
	t, err := time.Parse("2006-01-02", key)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// dayKeysFromCalendar 把交易日历规整为去重、升序的日期键。
func dayKeysFromCalendar(days []time.Time) []string {
	seen := map[string]bool{}
	keys := make([]string, 0, len(days))
	for _, d := range days {
		k := dayKey(d)
		if seen[k] {
			continue
		}
		seen[k] = true
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func quoteToOHLCV(q marketdata.Quote) domain.OHLCV {
	return domain.OHLCV{
		Symbol: q.Symbol,
		Date:   q.Timestamp,
		Open:   q.Open,
		High:   q.High,
		Low:    q.Low,
		Close:  q.Close,
		Volume: float64(q.Volume),
	}
}

func quotesFromBars(bars map[string]domain.OHLCV) map[string]Quote {
	out := make(map[string]Quote, len(bars))
	for sym, bar := range bars {
		out[sym] = barToQuote(bar)
	}
	return out
}
