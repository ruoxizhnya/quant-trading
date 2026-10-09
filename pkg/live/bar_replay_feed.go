package live

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/clock"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
)

// ReplayProvider 是 BarReplayFeed 需要的最小行情面：**一次**物化整段窗口。
//
// 刻意收窄而不是直接依赖整个 marketdata.Provider —— 回放只需要
// BulkLoadOHLCV，收窄后测试不必伪造 Provider 的另外十个方法，也让
// 「回放只做一次批量取数、不得 per-bar 取数」这条约束在类型上就成立
// （接口里根本没有单 bar 取数方法）。marketdata.Provider 天然满足它。
type ReplayProvider interface {
	BulkLoadOHLCV(ctx context.Context, symbols []string, start, end time.Time) (map[string][]domain.OHLCV, error)
}

// BarReplayFeed 按交易日顺序回放已落库的日线 bar（读法 A 的 DataSource）。
//
// ─── 读法 A（写在代码里的判据） ────────────────────────────────────
//
// paper 与回测必须吃**同一批输入**，差异才只可能来自执行机制（时钟 /
// broker / 成本）⇒ 对账才可归因。所以：
//   - 回放与回测**同一批**日线 bar（同一段窗口、同一份 BulkLoadOHLCV）；
//   - **不造 tick→bar 的假聚合层**：一根日线 bar 直接包成 Quote
//     （O/H/L/C/Volume 直置）。UC3 的「feed 推送 → bar 聚合 → OnBar」
//     对日线产品的正确读法是「按时间把 bar 逐根喂给同一个 OnBar」，
//     凭空造一个 tick 聚合器反而引入了回测没有的一层，破坏同构。
//
// ─── 确定性（本切片的核心前提） ───────────────────────────────────
//
// Replay 是**同步 pull** 驱动：不用 goroutine / ticker / time.Now，
// 节奏由注入的 clock.Clock（paper 回放里是 VirtualClock）推进。因此
// 同一输入两次回放必然产出逐根一致的 bar 序列 —— 这是确定性同构测试
// 能立起来的前提。
//
// Bid/Ask 裁决：一根日线 bar 没有盘口信息，故置 Bid = Ask = Close。
// 含义是「中间价 = 收盘价、价差为零」，执行成本（滑点/冲击）只由
// pkg/execution 的成本核一处施加 —— 这与回测把 quote.Close 当参考价
// 完全一致，正是「基础价对齐、成本集中」的同构做法。
type BarReplayFeed struct {
	provider ReplayProvider
	clk      clock.Clock

	mu         sync.RWMutex
	subscribed map[string]bool
	snapshots  map[string]marketdata.Quote
	callback   func(marketdata.Quote)
}

// NewBarReplayFeed 构造一个回放型数据源。provider 提供批量日线，clk 决定
// 回放的时间推进（回测/paper 注入 VirtualClock；实盘场景若复用本类型，
// 注入 LiveClock，此时 Advance 会返回 ErrLiveClockImmutable —— 回放型
// 数据源本就只服务确定性回放，这是有意为之）。
func NewBarReplayFeed(p ReplayProvider, clk clock.Clock) *BarReplayFeed {
	return &BarReplayFeed{
		provider:   p,
		clk:        clk,
		subscribed: make(map[string]bool),
		snapshots:  make(map[string]marketdata.Quote),
	}
}

// Subscribe 记录订阅的 symbol（DataFeed 契约）。回放的标的集合由 Replay
// 的 symbols 参数显式给定，此处只维护订阅态供 GetQuote 语义使用。
func (f *BarReplayFeed) Subscribe(symbols []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range symbols {
		f.subscribed[s] = true
	}
	return nil
}

// Unsubscribe 取消订阅。
func (f *BarReplayFeed) Unsubscribe(symbols []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range symbols {
		delete(f.subscribed, s)
	}
	return nil
}

// GetQuote 返回某 symbol 最近一次回放到的快照（DataFeed 契约）。
//
// 尚未回放过该 symbol 时返回 error（而不是零值 Quote）——一个空 Quote
// 会被下游读成「价格=0」，与「还没有数据」是两回事。
func (f *BarReplayFeed) GetQuote(symbol string) (marketdata.Quote, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	q, ok := f.snapshots[symbol]
	if !ok {
		return marketdata.Quote{}, fmt.Errorf("bar replay feed: no replay snapshot for symbol %q", symbol)
	}
	return q, nil
}

// SetCallback 存储推送回调（DataFeed 契约）。
//
// 注意：Replay 用的是它自己的显式 cb 参数，**不**调用这里存储的 callback。
// 保留该字段只为满足 DataFeed 接口、保住 UC3 的「DataSource 替换边界」；
// 若 Replay 也去调 callback，同一根 bar 会被两条路径各消费一次。push 语义
// 与 pull 语义在同一个 feed 上并存时，消费者必须只选其一。
func (f *BarReplayFeed) SetCallback(callback func(marketdata.Quote)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callback = callback
}

// Replay 按 (交易日升序, symbol 字典序) 逐根 bar 同步回调；cb 返回
// error 或时钟倒退时立即停止（不再喂后续 bar）。
//
// 流程：
//  1. **一次** BulkLoadOHLCV 物化整段（与回测同一批），禁止 per-bar 取数；
//  2. 把 (date, symbol) 排成全序 —— 排序理由见下；
//  3. 逐根：clk.Advance(bar.Date)（倒退 → 返回 error 并停止），
//     包成 Quote，更新快照，调 cb。
//
// 为什么必须显式排序：BulkLoadOHLCV 返回 map[string][]domain.OHLCV，
// Go 的 map 遍历顺序是**随机的**。若按 map 遍历序喂 bar，同一份输入两次
// 回放会喂出不同的序列，任何有状态算子（OnBar 递推）的结果都不可复现
// —— 这与 pkg/backtest/engine.go 的 sortedKeys 是同一条理由。定序是
// 可复现性的前提。
func (f *BarReplayFeed) Replay(ctx context.Context, symbols []string, start, end time.Time, cb func(marketdata.Quote) error) error {
	if len(symbols) == 0 {
		return fmt.Errorf("bar replay feed: no symbols to replay")
	}
	if cb == nil {
		return fmt.Errorf("bar replay feed: nil callback")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// 1) 一次物化整段窗口。
	all, err := f.provider.BulkLoadOHLCV(ctx, symbols, start, end)
	if err != nil {
		return fmt.Errorf("bar replay feed: bulk load failed: %w", err)
	}

	// 2) 展平 + 定序：(date 升序, symbol 字典序)。同日同 symbol 多根时
	//    用稳定排序保持提供方给出的相对次序。
	type item struct {
		bar domain.OHLCV
	}
	items := make([]item, 0)
	for _, sym := range symbols {
		for _, bar := range all[sym] {
			// 用请求的 symbol 兜底：个别 provider 可能不填 bar.Symbol。
			if bar.Symbol == "" {
				bar.Symbol = sym
			}
			items = append(items, item{bar: bar})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		ti, tj := items[i].bar.Date, items[j].bar.Date
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return items[i].bar.Symbol < items[j].bar.Symbol
	})

	// 3) 逐根推进时钟 + 回调。
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		bar := it.bar
		if err := f.clk.Advance(bar.Date); err != nil {
			// 时间倒退：立即停止，不喂后续 bar。半推进的序列比不推进
			// 更难查，故此处直接返回，绝不断言「继续喂也无所谓」。
			return fmt.Errorf("bar replay feed: clock advance failed at %s %s: %w",
				bar.Symbol, bar.Date.Format(time.RFC3339), err)
		}
		q := barToQuote(bar)
		f.mu.Lock()
		f.snapshots[q.Symbol] = q
		f.mu.Unlock()
		if err := cb(q); err != nil {
			return err
		}
	}
	return nil
}

// barToQuote 把一根日线 bar 直接包成 Quote（不做任何 tick 聚合）。
func barToQuote(bar domain.OHLCV) marketdata.Quote {
	return marketdata.Quote{
		Symbol:    bar.Symbol,
		Timestamp: bar.Date,
		Open:      bar.Open,
		High:      bar.High,
		Low:       bar.Low,
		Close:     bar.Close,
		Volume:    int64(bar.Volume),
		// Bid/Ask = Close：日线无盘口，价差置零（见类型注释的裁决）。
		Bid: bar.Close,
		Ask: bar.Close,
	}
}

// 编译期合规检查：BarReplayFeed 必须满足 DataFeed 契约（保住 UC3 的
// 「DataSource 替换边界」）。从 DataFeed 删任一方法，本行即编译失败。
var _ DataFeed = (*BarReplayFeed)(nil)
