// Package live — 退市 + 北交所 30% 涨跌停 (P2-13).
//
// 监管依据:
//   - 《上海证券交易所股票上市规则》(2024 修订) §13.1.1 / 《深圳证券交易所
//     股票上市规则》(2024 修订) §13.1.1: 退市分为交易类退市、财务类退市、
//     规范类退市、重大违法退市 4 类, 触发后先进入退市整理期 (15 个交易日),
//     整理期首日不设涨跌幅, 其后 ±10% (主板) / ±20% (创业板/科创板) /
//     ±30% (北交所), 整理期满后摘牌。
//   - 《北京证券交易所股票上市规则》(2024) §10.1: 北交所股票退市整理期
//     仍为 15 个交易日, 期间涨跌幅 ±30%。
//   - 《证券业从业人员执业行为准则》: 退市整理期券商应通过短信 / 客户端
//     推送 + 限制买入 (allow_sell_only) 强制告知客户强制清仓, 客户持仓
//     在退市摘牌日 (delisted_date) 自动清零。
//
// 设计目标:
//   - 维护一份 stock → StockState 映射 (Listed / Suspended / Delisting /
//     Delisted), delisted_date 字段记录摘牌日 (摘牌当日 15:00 后此股票
//     不再可在交易所交易, 持仓由券商统一现金清算)。
//   - 提供 ForcedLiquidator: 当某只股票进入 Delisting 状态且距摘牌 ≤
//     LiquidationWindow (默认 5 个交易日) 时, 强制调用 LiveTrader
//     EmergencyFlatten 卖出持仓, 走 "BypassedT1" 通道 (类似紧急平仓)。
//   - 不在这里实现券商摘牌后的现金清算 (cfd 现金返还), 仅记录
//     LiquidationRecord, 真实结算交给清算所 / broker.
//
// 注: BSE 30% 涨跌停已在 board.go 落地, 本文件聚焦 "退市时间线 +
// 强制清仓" 维度的实现。
package live

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// ============================================================
// 状态枚举
// ============================================================

// StockState 表示一只股票当前的上市状态。
//
// 状态转换 (单向, 不允许逆向):
//   Listed  → Suspended    (临时停牌, 复牌后回 Listed)
//   Listed  → Delisting    (触发退市, 进入整理期)
//   Suspended → Delisting  (停牌期间触发退市, 复牌即整理期首日)
//   Delisting → Delisted   (整理期满, 摘牌)
type StockState string

const (
	// StockStateListed 正常上市 (default).
	StockStateListed StockState = "listed"
	// StockStateSuspended 临时停牌 (复牌后回 Listed, 不视为退市).
	StockStateSuspended StockState = "suspended"
	// StockStateDelisting 退市整理期 (15 个交易日, 期间仍可交易).
	StockStateDelisting StockState = "delisting"
	// StockStateDelisted 已摘牌 (delisted_date 当日 15:00 后停止交易).
	StockStateDelisted StockState = "delisted"
)

// String 返回 StockState 的字符串表示 (调试用).
func (s StockState) String() string { return string(s) }

// IsTerminal reports whether the state is final (no further transitions).
func (s StockState) IsTerminal() bool {
	return s == StockStateDelisted
}

// ============================================================
// 状态记录
// ============================================================

// StockStateRecord 描述一只股票的最新上市状态及其退市时间线.
//
// 字段语义:
//   - Symbol: ts_code, 如 "600000.SH".
//   - State:  当前状态 (StockState*).
//   - Reason: 触发原因 (如 "财务类退市-连续亏损", "交易类退市-面值退市",
//     "重大违法强制退市"). 留空表示 Listed / Suspended 状态.
//   - DelistingPeriodStart: 退市整理期首日 (即 *ST → 整理期第一天).
//   - DelistingPeriodEnd:   退市整理期末日 (摘牌前最后可交易日, 整理期 15 天).
//   - DelistedDate:         摘牌日 (delisted_date 当日 15:00 后停止交易,
//     持仓由清算所自动现金清算). 当 State == Delisted 时此字段必填.
//   - UpdatedAt:            本记录最后一次更新 (调试用).
type StockStateRecord struct {
	Symbol               string     `json:"symbol"`
	State                StockState `json:"state"`
	Reason               string     `json:"reason,omitempty"`
	DelistingPeriodStart time.Time  `json:"delisting_period_start,omitempty"`
	DelistingPeriodEnd   time.Time  `json:"delisting_period_end,omitempty"`
	DelistedDate         time.Time  `json:"delisted_date,omitempty"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// IsInDelistingWindow reports whether a record is in delisting state AND
// the delisted_date is within window days from now.
//
// Used by the forced liquidator to decide whether to issue a flatten order
// for a held position. The window is usually 5 trading days — by then the
// client should have closed the position themselves; if not, we force it.
func (r *StockStateRecord) IsInDelistingWindow(now time.Time, window time.Duration) bool {
	if r == nil {
		return false
	}
	if r.State != StockStateDelisting {
		return false
	}
	if r.DelistedDate.IsZero() {
		return false
	}
	// If now is past the window AND before/equal the delisted_date,
	// we're inside the forced-liquidation window.
	return now.Before(r.DelistedDate) && r.DelistedDate.Sub(now) <= window
}

// ============================================================
// 配置
// ============================================================

// StockStateConfig 配置 StockStateRegistry + ForcedLiquidator.
type StockStateConfig struct {
	// LiquidationWindow 距摘牌多少天内强制清仓.
	// 默认 5 个自然日 (≈ 3 个交易日, 留点 buffer 让客户先自行处理).
	// < 0 → 关闭强制清仓 (只记录状态, 不主动卖出).
	LiquidationWindow time.Duration
	// Now 时钟注入 (测试用). nil → time.Now.
	Now func() time.Time
}

// DefaultStockStateConfig returns sane defaults.
func DefaultStockStateConfig() StockStateConfig {
	return StockStateConfig{
		LiquidationWindow: 5 * 24 * time.Hour,
	}
}

// ============================================================
// Registry (thread-safe state map)
// ============================================================

// StockStateRegistry 维护 stock → StockStateRecord 映射, 支持线程安全
// 的状态更新 + 查询 + 列表.
//
// 内存数据, 不持久化: 退市事件是低频 (年 < 50 起), 启动时由外部
// (load-on-startup) 灌入即可, 进程重启不丢失 (上游 db 是 source of truth).
type StockStateRegistry struct {
	mu      sync.RWMutex
	records map[string]StockStateRecord
	cfg     StockStateConfig
	logger  zerolog.Logger
}

// NewStockStateRegistry creates an empty registry.
func NewStockStateRegistry(cfg StockStateConfig, logger zerolog.Logger) *StockStateRegistry {
	if cfg.LiquidationWindow == 0 {
		cfg.LiquidationWindow = 5 * 24 * time.Hour
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &StockStateRegistry{
		records: make(map[string]StockStateRecord),
		cfg:     cfg,
		logger:  logger.With().Str("component", "stock_state_registry").Logger(),
	}
}

// SetState applies a state transition for a symbol. The function enforces
// the legal transition graph and returns ErrIllegalStateTransition on
// invalid moves. Zero-value fields (DelistedDate / DelistingPeriod*)
// are auto-populated from the supplied delistedDate argument when the
// state is Delisting / Delisted.
//
// Example:
//
//	// Listed → Delisting (整理期 2026-07-01 ~ 2026-07-21, 摘牌 2026-07-22)
//	registry.SetState("600000.SH", StockStateDelisting,
//	    "财务类退市-连续亏损",
//	    time.Date(2026, 7, 22, 15, 0, 0, 0, time.UTC))
func (r *StockStateRegistry) SetState(symbol string, state StockState, reason string, delistedDate time.Time) error {
	if symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if !isValidStockState(state) {
		return fmt.Errorf("invalid state: %q", state)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	prev, exists := r.records[symbol]
	now := r.cfg.Now()

	// Legal transition check.
	if exists && !isLegalTransition(prev.State, state) {
		return fmt.Errorf("%w: %s: %s → %s",
			ErrIllegalStateTransition, symbol, prev.State, state)
	}

	rec := StockStateRecord{
		Symbol:     symbol,
		State:      state,
		Reason:     reason,
		UpdatedAt:  now,
	}

	// Auto-populate delisting timeline when entering Delisting / Delisted.
	if state == StockStateDelisting || state == StockStateDelisted {
		if delistedDate.IsZero() {
			return fmt.Errorf("delisted_date is required for state=%s", state)
		}
		rec.DelistedDate = delistedDate
		// 整理期 15 个交易日 (≈ 21 自然日, 留 6 天 buffer 给周末).
		rec.DelistingPeriodStart = delistedDate.Add(-21 * 24 * time.Hour)
		rec.DelistingPeriodEnd = delistedDate.Add(-24 * time.Hour)
		// If we're moving directly to Delisted (skipping the整理期,
		// e.g. 重大违法即时摘牌), still populate for consistency.
		if state == StockStateDelisted {
			rec.DelistingPeriodStart = delistedDate
			rec.DelistingPeriodEnd = delistedDate
		}
	} else if exists {
		// Listed / Suspended — preserve any prior timeline if we're
		// transitioning back from Delisting (legal? no — Delisting
		// is one-way, so this branch is only Listed ↔ Suspended).
		rec.DelistingPeriodStart = prev.DelistingPeriodStart
		rec.DelistingPeriodEnd = prev.DelistingPeriodEnd
		rec.DelistedDate = prev.DelistedDate
	}

	r.records[symbol] = rec
	r.logger.Info().
		Str("symbol", symbol).
		Str("from", string(prev.State)).
		Str("to", string(state)).
		Time("delisted_date", rec.DelistedDate).
		Msg("stock state updated")
	return nil
}

// GetState returns a snapshot of a symbol's record.
func (r *StockStateRegistry) GetState(symbol string) (StockStateRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.records[symbol]
	return rec, ok
}

// ListByState returns all records in the given state, sorted by
// delisted_date (ascending). Empty state matches all records.
//
// Used by the forced liquidator to enumerate all symbols currently in
// the delisting window and emit flatten orders for each held position.
func (r *StockStateRegistry) ListByState(state StockState) []StockStateRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]StockStateRecord, 0, len(r.records))
	for _, rec := range r.records {
		if state == "" || rec.State == state {
			out = append(out, rec)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].DelistedDate.Before(out[j].DelistedDate)
	})
	return out
}

// AllSymbols returns all registered symbols (for debugging / inspection).
func (r *StockStateRegistry) AllSymbols() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.records))
	for s := range r.records {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Count returns the number of registered records.
func (r *StockStateRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.records)
}

// Delete removes a symbol from the registry. Used when a *ST stock
// successfully petitions and returns to Listed (rare, but allowed).
func (r *StockStateRegistry) Delete(symbol string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.records, symbol)
}

// ============================================================
// Forced liquidator
// ============================================================

// LiquidationAction 记录一次强制清仓的结果.
//
// Sold: 成功卖出 (由 EmergencyFlatten 内部走 BypassedT1 通道).
// Skipped: 跳过原因 (e.g. 持仓为 0 / 不在强制清仓窗口 / 券商拒绝).
type LiquidationAction struct {
	Symbol   string    `json:"symbol"`
	State    StockState `json:"state"`
	DelistedDate time.Time `json:"delisted_date"`
	Quantity  float64   `json:"quantity"`     // 强制清仓的股数
	Sold      bool      `json:"sold"`
	Reason    string    `json:"reason"`       // 卖出失败 / 跳过的原因
	Timestamp time.Time `json:"timestamp"`
}

// LiquidationResult is the aggregated result of one Scan() call.
type LiquidationResult struct {
	ScannedAt   time.Time             `json:"scanned_at"`
	Actions     []LiquidationAction   `json:"actions"`
	TotalSold   int                   `json:"total_sold"`
	TotalSkipped int                  `json:"total_skipped"`
}

// ForcedLiquidator 在退市整理期末期强制卖出所有持仓.
//
// 工作原理:
//  1. Scan(ctx, trader) 枚举 registry 中所有 Delisting 状态 + 距
//     摘牌 ≤ LiquidationWindow 的股票.
//  2. 对每只股票查询 trader 持仓 (trader.GetPositions), 若 qty > 0
//     则调用 EmergencyFlatten (bypassed_t1, reason = "delisting_<symbol>")
//     强制清仓.
//  3. 持仓为 0 → action 记为 sold=false, reason="no_position", 不报错.
//
// 调用方通常用 ticker 每 60s 调一次, 整理期末期如果一直有客户买入
// 也会被立刻识别 + 清仓.
type ForcedLiquidator struct {
	registry *StockStateRegistry
	cfg      StockStateConfig
	logger   zerolog.Logger
}

// NewForcedLiquidator creates a liquidator bound to a registry.
func NewForcedLiquidator(registry *StockStateRegistry, cfg StockStateConfig, logger zerolog.Logger) *ForcedLiquidator {
	if cfg.LiquidationWindow == 0 {
		cfg.LiquidationWindow = 5 * 24 * time.Hour
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &ForcedLiquidator{
		registry: registry,
		cfg:      cfg,
		logger:   logger.With().Str("component", "forced_liquidator").Logger(),
	}
}

// Scan walks the registry, finds delisting stocks inside the forced
// liquidation window, and flattens any held positions via the supplied
// LiveTrader.
//
// This call is synchronous and stateless — safe to invoke from a
// ticker. The function does NOT start a goroutine.
func (f *ForcedLiquidator) Scan(ctx context.Context, trader LiveTrader) (*LiquidationResult, error) {
	if trader == nil {
		return nil, fmt.Errorf("trader is required")
	}
	now := f.cfg.Now()
	candidates := f.registry.ListByState(StockStateDelisting)
	if len(candidates) == 0 {
		return &LiquidationResult{ScannedAt: now}, nil
	}

	positions, err := trader.GetPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch positions: %w", err)
	}
	posBySymbol := make(map[string]float64, len(positions))
	for _, p := range positions {
		posBySymbol[p.Symbol] = p.Quantity
	}

	res := &LiquidationResult{ScannedAt: now}
	for _, rec := range candidates {
		action := LiquidationAction{
			Symbol:       rec.Symbol,
			State:        rec.State,
			DelistedDate: rec.DelistedDate,
			Timestamp:    now,
		}
		// Check the forced-liquidation window. Records outside the
		// window are reported as skipped (with reason="outside_window")
		// so the operator can audit "delisting stocks NOT yet in the
		// forced-flatten window" via /api/liquidation/actions.
		if !rec.IsInDelistingWindow(now, f.cfg.LiquidationWindow) {
			action.Sold = false
			action.Quantity = 0
			action.Reason = "outside_window"
			res.Actions = append(res.Actions, action)
			res.TotalSkipped++
			continue
		}
		// ctx cancellation check between iterations.
		if err := ctx.Err(); err != nil {
			return res, err
		}

		qty := posBySymbol[rec.Symbol]
		action.Quantity = qty

		if qty <= 0 {
			action.Sold = false
			action.Reason = "no_position"
			res.Actions = append(res.Actions, action)
			res.TotalSkipped++
			continue
		}

		reason := fmt.Sprintf("forced_liquidation_delisting:%s:%s",
			rec.Symbol, rec.DelistedDate.Format("2006-01-02"))
		flatten, err := trader.EmergencyFlatten(ctx, reason)
		if err != nil {
			action.Sold = false
			action.Reason = fmt.Sprintf("flatten_error: %v", err)
			res.Actions = append(res.Actions, action)
			res.TotalSkipped++
			f.logger.Warn().
				Err(err).
				Str("symbol", rec.Symbol).
				Msg("forced liquidation flatten failed")
			continue
		}

		// Inspect the EmergencyFlattenResult to confirm this symbol
		// was actually sold.
		sold := false
		for _, so := range flatten.Sold {
			if so.Symbol == rec.Symbol {
				sold = true
				action.Quantity = so.Quantity
				break
			}
		}
		action.Sold = sold
		if !sold {
			action.Reason = "flatten_returned_no_sell"
		}
		res.Actions = append(res.Actions, action)
		if sold {
			res.TotalSold++
		} else {
			res.TotalSkipped++
		}
	}

	f.logger.Info().
		Int("candidates", len(candidates)).
		Int("sold", res.TotalSold).
		Int("skipped", res.TotalSkipped).
		Msg("forced liquidation scan complete")
	return res, nil
}

// ============================================================
// Errors
// ============================================================

// ErrIllegalStateTransition 表示尝试了非法的状态转换.
var ErrIllegalStateTransition = fmt.Errorf("illegal state transition")

// isValidStockState reports whether s is one of the known states.
func isValidStockState(s StockState) bool {
	switch s {
	case StockStateListed, StockStateSuspended, StockStateDelisting, StockStateDelisted:
		return true
	}
	return false
}

// isLegalTransition encodes the legal transition graph.
//
//	listed    → suspended, delisting, delisted (jump-to-delisted, 重大违法)
//	suspended → listed, delisting
//	delisting → delisted
//	delisted  → (terminal)
func isLegalTransition(from, to StockState) bool {
	switch from {
	case StockStateListed:
		return to == StockStateSuspended || to == StockStateDelisting || to == StockStateDelisted
	case StockStateSuspended:
		return to == StockStateListed || to == StockStateDelisting
	case StockStateDelisting:
		return to == StockStateDelisted
	case StockStateDelisted:
		return false
	}
	return false
}
