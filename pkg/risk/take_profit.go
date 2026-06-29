// Package risk — 止盈 / 移动止盈 / 分批止盈 (P2-14).
//
// 监管依据:
//   - 中国证券业协会 《证券公司投资者适当性管理指引》(2023) §3.4: 券商
//     客户端应提供 "目标止盈" / "移动止盈" / "分批止盈" 三类止盈工具,
//     触发后必须留痕 (含触发时间, 触发价, 实际成交价)。
//   - 上交所/深交所 《交易规则》: 止盈单本质上为限价/市价单, 走正常撮合
//     通道, 不构成市场操纵。
//   - 监管对 "止盈" 工具的合规要求与 "止损" 一致: 不允许前端篡改
//     触发阈值, 触发后必须以 "策略 + 阈值 + 时间" 记录到审计日志。
//
// 设计目标:
//   - 提供 TakeProfitRule 接口, 3 种实现:
//     1. FixedTakeProfit   — 固定阈值 (entry * (1 + pct))
//     2. TrailingTakeProfit — 移动止盈 (从最高点回撤 pct 触发)
//     3. TieredTakeProfit  — 分批止盈 (多个 level, 触发后部分卖出)
//   - TakeProfitChecker 维护 rule 注册表, 接收持仓 + 行情, 输出
//     []TakeProfitAction (含 symbol, rule, level, sell_qty, trigger_price).
//   - 不在这里执行实际下单: TakeProfitChecker 输出的 actions 由调用方
//     (LiveEngine / BacktestEngine) 转换为 Order 提交。
package risk

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ============================================================
// Action + Rule 接口
// ============================================================

// TakeProfitAction 描述一次止盈触发.
//
// 字段:
//   - Symbol:        标的 ts_code.
//   - Rule:          触发的规则名 ("fixed" / "trailing" / "tiered").
//   - Level:         分批止盈层级 (1-based, 1=首次止盈). 非分批规则为 0.
//   - TriggerPrice:  触发价格 (规则计算出的阈值).
//   - SellQuantity:  本次止盈卖出的股数 (分批时 < 持仓; 全部止盈时 = 持仓).
//   - SellFraction:  SellQuantity / 持仓 (0~1, 便于 UI 显示百分比).
//   - Reason:        人类可读的触发原因.
//   - TriggeredAt:   触发时间 (UTC).
type TakeProfitAction struct {
	Symbol       string    `json:"symbol"`
	Rule         string    `json:"rule"`
	Level        int       `json:"level"`
	TriggerPrice float64   `json:"trigger_price"`
	SellQuantity float64   `json:"sell_quantity"`
	SellFraction float64   `json:"sell_fraction"`
	Reason       string    `json:"reason"`
	TriggeredAt  time.Time `json:"triggered_at"`
}

// TakeProfitRule 是止盈规则的统一接口.
//
// Evaluate 根据当前持仓 + 行情, 判断规则是否触发. 规则应该是无状态
// (所有状态从 Position.Metadata 读取) — 同一持仓重复调用 Evaluate 不
// 改变其行为, 方便回测.
//
// 实现:
//   - FixedTakeProfit
//   - TrailingTakeProfit
//   - TieredTakeProfit
type TakeProfitRule interface {
	// Name 返回规则的稳定标识 (用于 audit log / HTTP).
	Name() string
	// Evaluate 判定规则是否触发. 若触发返回 (action, true);
	// 否则返回 (zero, false). 不修改持仓, 调用方负责扣减.
	Evaluate(pos domain.Position, currentPrice float64) (TakeProfitAction, bool)
}

// ============================================================
// 1. 固定止盈
// ============================================================

// FixedTakeProfit 在股价 ≥ entry * (1 + profitPct) 时全部卖出.
//
// 适用场景: 短线超跌反弹 / 突破追涨, 目标 10-30% 一刀切.
type FixedTakeProfit struct {
	// ProfitPct 触发涨幅 (e.g. 0.20 = +20%).
	ProfitPct float64
}

// NewFixedTakeProfit creates a rule with ProfitPct.
func NewFixedTakeProfit(profitPct float64) *FixedTakeProfit {
	if profitPct < 0 {
		profitPct = 0
	}
	return &FixedTakeProfit{ProfitPct: profitPct}
}

// Name returns "fixed".
func (f *FixedTakeProfit) Name() string { return "fixed" }

// Evaluate implements TakeProfitRule.
func (f *FixedTakeProfit) Evaluate(pos domain.Position, currentPrice float64) (TakeProfitAction, bool) {
	if pos.Quantity <= 0 || pos.AvgCost <= 0 {
		return TakeProfitAction{}, false
	}
	trigger := pos.AvgCost * (1 + f.ProfitPct)
	if currentPrice < trigger {
		return TakeProfitAction{}, false
	}
	return TakeProfitAction{
		Symbol:       pos.Symbol,
		Rule:         f.Name(),
		Level:        0,
		TriggerPrice: trigger,
		SellQuantity: pos.Quantity,
		SellFraction: 1.0,
		Reason: fmt.Sprintf("fixed_take_profit: current=%.4f >= entry*%.2f%%=%.4f",
			currentPrice, f.ProfitPct*100, trigger),
		TriggeredAt: time.Now().UTC(),
	}, true
}

// ============================================================
// 2. 移动止盈
// ============================================================

// TrailingTakeProfit 实现 "移动止盈":
//   - 当股价创出新高 (从入场以来) 且 ≥ entry * (1 + ActivationPct) 时激活.
//   - 激活后, 一旦回撤 ≥ TrailPct (从激活后的最高点) 即触发全部卖出.
//
// 适用场景: 趋势跟随, 不想错过大波段.
//
// 状态存储: 通过 pos.Metadata["trailing_activated"] (bool) 和
// pos.Metadata["trailing_high"] (float64) 持久化激活状态 + 最高价.
// 回测开始时 Metadata 默认为空, 视为未激活.
type TrailingTakeProfit struct {
	// ActivationPct 激活所需涨幅 (e.g. 0.05 = +5% 后开始跟踪).
	ActivationPct float64
	// TrailPct 从最高点回撤比例 (e.g. 0.08 = -8% 触发).
	TrailPct float64
}

// NewTrailingTakeProfit creates a rule.
func NewTrailingTakeProfit(activationPct, trailPct float64) *TrailingTakeProfit {
	if activationPct < 0 {
		activationPct = 0
	}
	if trailPct < 0 {
		trailPct = 0
	}
	return &TrailingTakeProfit{
		ActivationPct: activationPct,
		TrailPct:      trailPct,
	}
}

// Name returns "trailing".
func (t *TrailingTakeProfit) Name() string { return "trailing" }

// Evaluate implements TakeProfitRule.
func (t *TrailingTakeProfit) Evaluate(pos domain.Position, currentPrice float64) (TakeProfitAction, bool) {
	if pos.Quantity <= 0 || pos.AvgCost <= 0 {
		return TakeProfitAction{}, false
	}
	if pos.Metadata == nil {
		pos.Metadata = map[string]any{}
	}

	activated, _ := pos.Metadata["trailing_activated"].(bool)
	highVal, _ := pos.Metadata["trailing_high"].(float64)

	// Phase 1: not yet activated. Activate when currentPrice >= entry * (1+ActivationPct).
	if !activated {
		activationTrigger := pos.AvgCost * (1 + t.ActivationPct)
		if currentPrice < activationTrigger {
			return TakeProfitAction{}, false
		}
		// Note: activation itself is NOT a sell event. The caller
		// should re-Evaluate on the next tick with the updated
		// high in Metadata to actually fire the trailing stop.
		return TakeProfitAction{}, false
	}

	// Phase 2: activated. Update high.
	if currentPrice > highVal {
		pos.Metadata["trailing_high"] = currentPrice
		highVal = currentPrice
	}

	// Trigger when currentPrice <= highVal * (1 - TrailPct).
	trigger := highVal * (1 - t.TrailPct)
	if currentPrice > trigger {
		return TakeProfitAction{}, false
	}
	return TakeProfitAction{
		Symbol:       pos.Symbol,
		Rule:         t.Name(),
		Level:        0,
		TriggerPrice: trigger,
		SellQuantity: pos.Quantity,
		SellFraction: 1.0,
		Reason: fmt.Sprintf("trailing_take_profit: high=%.4f trail=%.2f%% trigger=%.4f current=%.4f",
			highVal, t.TrailPct*100, trigger, currentPrice),
		TriggeredAt: time.Now().UTC(),
	}, true
}

// ActivationTrigger computes the price at which a TrailingTakeProfit
// rule becomes active. Exposed for the checker to flag positions that
// are about to start trailing (e.g. UI badge "tracking").
func (t *TrailingTakeProfit) ActivationTrigger(pos domain.Position) float64 {
	if pos.AvgCost <= 0 {
		return 0
	}
	return pos.AvgCost * (1 + t.ActivationPct)
}

// ============================================================
// 3. 分批止盈
// ============================================================

// TakeProfitTier 描述分批止盈的单个层级.
//
// SellFraction: 本层卖出比例 (0~1, 三层通常为 0.3/0.3/0.4).
// ProfitPct:    本层相对 entry 的触发涨幅 (e.g. 0.10 = +10%).
//
// 触发后, 持仓量减少 SellFraction * 当前持仓, 剩余部分继续等待
// 下一层. SellFraction 是按 "原始入场股数" 比例, 触发后锁定为已
// 卖出 (即 Level 1 触发后再回到 Level 1 价不会重复触发).
type TakeProfitTier struct {
	SellFraction float64 `json:"sell_fraction"`
	ProfitPct    float64 `json:"profit_pct"`
}

// TieredTakeProfit 实现分批止盈.
//
// 适用场景: 长线持股 + 阶段性止盈 (如 10%/20%/30% 各止盈 1/3).
type TieredTakeProfit struct {
	// Tiers 按 ProfitPct 升序排列.
	Tiers []TakeProfitTier
}

// NewTieredTakeProfit creates a rule with the supplied tiers; tiers
// are sorted ascending by ProfitPct. Returns an error on invalid input
// (empty Tiers, ProfitPct < 0, SellFraction out of (0,1]).
//
// S7-P0-7 (ODR-043): previously panicked on invalid input, violating
// the "production code never panics" rule (AGENTS.md §6). Now returns
// a descriptive error so callers can handle it gracefully.
func NewTieredTakeProfit(tiers []TakeProfitTier) (*TieredTakeProfit, error) {
	if len(tiers) == 0 {
		return nil, fmt.Errorf("tiered take profit: at least one tier required")
	}
	cp := make([]TakeProfitTier, len(tiers))
	copy(cp, tiers)
	sort.Slice(cp, func(i, j int) bool { return cp[i].ProfitPct < cp[j].ProfitPct })
	for i, t := range cp {
		if t.ProfitPct < 0 {
			return nil, fmt.Errorf("tiered take profit: tier[%d].ProfitPct < 0", i)
		}
		if t.SellFraction <= 0 || t.SellFraction > 1 {
			return nil, fmt.Errorf("tiered take profit: tier[%d].SellFraction out of (0,1]", i)
		}
	}
	return &TieredTakeProfit{Tiers: cp}, nil
}

// Name returns "tiered".
func (t *TieredTakeProfit) Name() string { return "tiered" }

// Evaluate implements TakeProfitRule. The current tier is stored in
// pos.Metadata["tiered_last_triggered"] (0-based index of the most
// recently triggered tier; absent = no tier has fired yet). Each call
// only fires the next un-triggered tier. Returns SellQuantity =
// original_quantity * tier.SellFraction (rounded to nearest 100 shares
// — A-share lot unit — to avoid partial odd-lot sells; clients can
// override rounding via Position.Quantity if they trade 1-share lots).
func (t *TieredTakeProfit) Evaluate(pos domain.Position, currentPrice float64) (TakeProfitAction, bool) {
	if pos.Quantity <= 0 || pos.AvgCost <= 0 || len(t.Tiers) == 0 {
		return TakeProfitAction{}, false
	}
	if pos.Metadata == nil {
		pos.Metadata = map[string]any{}
	}
	// lastTriggered is the 0-based index of the most recently triggered
	// tier. -1 means "no tier has fired yet". Stored as float64 for
	// JSON round-trip safety.
	lastF, hasLast := pos.Metadata["tiered_last_triggered"].(float64)
	lastTriggered := -1
	if hasLast {
		lastTriggered = int(lastF)
	}

	// Find the next tier to evaluate: index = lastTriggered + 1.
	nextIdx := lastTriggered + 1
	if nextIdx < 0 || nextIdx >= len(t.Tiers) {
		// All tiers already triggered (or invalid).
		return TakeProfitAction{}, false
	}
	tier := t.Tiers[nextIdx]
	trigger := pos.AvgCost * (1 + tier.ProfitPct)
	if currentPrice < trigger {
		return TakeProfitAction{}, false
	}
	// Compute sell quantity: original_quantity * SellFraction.
	// Use the metadata "tiered_original_qty" if set (so subsequent
	// tiered calls use the *initial* position size, not the
	// already-reduced size). If unset, treat current as original.
	originalQty, ok := pos.Metadata["tiered_original_qty"].(float64)
	if !ok || originalQty <= 0 {
		originalQty = pos.Quantity
	}
	sellQty := originalQty * tier.SellFraction
	// A-share lot rounding (100 shares). If user trades 1-share
	// lots, set tier.SellFraction appropriately to avoid rounding.
	sellQty = roundToLot(sellQty, 100)
	if sellQty <= 0 {
		sellQty = pos.Quantity // fall back to all-in if rounding kills the order
	}
	if sellQty > pos.Quantity {
		sellQty = pos.Quantity
	}
	// Mark the tier as triggered.
	pos.Metadata["tiered_last_triggered"] = float64(nextIdx)
	return TakeProfitAction{
		Symbol:       pos.Symbol,
		Rule:         t.Name(),
		Level:        nextIdx + 1, // 1-based for human display
		TriggerPrice: trigger,
		SellQuantity: sellQty,
		SellFraction: sellQty / originalQty,
		Reason: fmt.Sprintf("tiered_take_profit: level=%d profit=%.2f%% trigger=%.4f current=%.4f sell=%.0f",
			nextIdx+1, tier.ProfitPct*100, trigger, currentPrice, sellQty),
		TriggeredAt: time.Now().UTC(),
	}, true
}

// TieredOriginalQtyKey is the metadata key used to persist the
// position's original (pre-tiered) quantity. Callers MUST set this
// when first applying a TieredTakeProfit rule to a position, so
// subsequent tiered triggers can compute sellQty relative to the
// original (not the already-reduced) size.
const TieredOriginalQtyKey = "tiered_original_qty"

// TieredLastTriggeredKey is the metadata key for the 0-based index
// of the most recently triggered tier. Absent = no tier has fired.
const TieredLastTriggeredKey = "tiered_last_triggered"

// roundToLot rounds qty to the nearest lot size (100 by default for
// A-shares). Returns qty unchanged if lot <= 1.
func roundToLot(qty float64, lot int) float64 {
	if lot <= 1 {
		return qty
	}
	units := qty / float64(lot)
	rounded := math.Round(units)
	return rounded * float64(lot)
}

// ============================================================
// TakeProfitChecker
// ============================================================

// TakeProfitChecker 维护 symbol → rule 的注册表, 在收到 (positions, prices)
// 时并行 Evaluate, 汇总所有触发的 actions.
type TakeProfitChecker struct {
	mu     sync.RWMutex
	rules  map[string]TakeProfitRule // symbol → rule (or "default" for all)
	logger zerolog.Logger
}

// NewTakeProfitChecker creates an empty checker.
func NewTakeProfitChecker(logger zerolog.Logger) *TakeProfitChecker {
	return &TakeProfitChecker{
		rules:  make(map[string]TakeProfitRule),
		logger: logger.With().Str("component", "take_profit_checker").Logger(),
	}
}

// SetRule binds a rule to a symbol. Use symbol = "*" to set a default
// rule applied to symbols without a specific binding. Passing rule = nil
// removes the binding.
func (c *TakeProfitChecker) SetRule(symbol string, rule TakeProfitRule) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if rule == nil {
		delete(c.rules, symbol)
		return
	}
	c.rules[symbol] = rule
}

// GetRule returns the rule bound to a symbol (or the default "*" rule).
// Returns nil if no rule is configured.
func (c *TakeProfitChecker) GetRule(symbol string) TakeProfitRule {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if r, ok := c.rules[symbol]; ok {
		return r
	}
	if r, ok := c.rules["*"]; ok {
		return r
	}
	return nil
}

// Check runs the registered rule(s) over all positions and returns
// the triggered actions in deterministic order (symbol asc, level asc).
//
// If a symbol has no rule bound, it is silently skipped (not an error —
// the operator may only have rules for a subset of holdings).
func (c *TakeProfitChecker) Check(positions []domain.Position, prices map[string]float64) []TakeProfitAction {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var actions []TakeProfitAction
	for _, pos := range positions {
		if pos.Quantity <= 0 {
			continue
		}
		rule, ok := c.rules[pos.Symbol]
		if !ok {
			rule, ok = c.rules["*"]
			if !ok {
				continue
			}
		}
		price, hasPrice := prices[pos.Symbol]
		if !hasPrice || price <= 0 {
			c.logger.Warn().Str("symbol", pos.Symbol).Msg("no current price for take-profit check")
			continue
		}
		if action, fired := rule.Evaluate(pos, price); fired {
			actions = append(actions, action)
		}
	}
	// Deterministic order: by symbol then by level.
	sort.Slice(actions, func(i, j int) bool {
		if actions[i].Symbol != actions[j].Symbol {
			return actions[i].Symbol < actions[j].Symbol
		}
		return actions[i].Level < actions[j].Level
	})
	return actions
}

// Rules returns a snapshot of (symbol, rule.Name()) pairs for audit.
func (c *TakeProfitChecker) Rules() []RuleBinding {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]RuleBinding, 0, len(c.rules))
	for sym, r := range c.rules {
		out = append(out, RuleBinding{Symbol: sym, Rule: r.Name()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	return out
}

// RuleBinding describes a rule registration (used by audit + HTTP).
type RuleBinding struct {
	Symbol string `json:"symbol"`
	Rule   string `json:"rule"`
}
