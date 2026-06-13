// Package domain — 公司行为 (Corporate Actions) 引擎 (P2-15).
//
// 监管依据:
//   - 中国证监会 《上市公司监管指引第 3 号——上市公司现金分红》 (2023 修订):
//     现金分红须在股权登记日次日 (除息日 ex-date) 实际从股价中扣除。
//   - 《上海证券交易所交易规则》 §3.4: 送股 / 转增股本在除权日 (ex-date)
//     实际到账, 股价按除权参考价 = (收盘价 - 每股现金红利 + 新股配股
//     价格 × 配股比例) / (1 + 送股比例 + 转增比例 + 配股比例) 计算。
//   - 《上市公司证券发行管理办法》 (2020 修订) §5: 配股 / 增发须在
//     缴款日完成资金扣划, 逾期未缴款视为放弃, 不予补缴。
//
// 设计目标:
//   - 提供统一的 CorporateAction 接口 + 5 种实现 (CashDividend /
//     BonusShare / Split / RightsIssue / Placement).
//   - 每个 action 暴露: Type, Symbol, ExDate (除权除息日), RecordDate
//     (股权登记日), PayDate (实际到账日), Apply(pos) → (newPos, cashDelta).
//   - ActionEngine 维护 symbol → []CorporateAction 队列, 在收到
//     (positions, prices, asOf) 时找到当日 ex-date 的 action 并应用.
package domain

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// ============================================================
// Action 接口 + 通用字段
// ============================================================

// ActionType 区分 5 类公司行为.
type ActionType string

const (
	// ActionCashDividend 现金分红 (派息).
	// 公式: 每股派 cash_per_share CNY → 持仓获得 cash = qty * cash_per_share,
	// ex-date 开盘参考价 = 收盘价 - cash_per_share.
	ActionCashDividend ActionType = "cash_dividend"
	// ActionBonusShare 送股 / 红股.
	// 公式: 每 10 股送 bonus_per_10 股 → 持仓增加 qty * bonus_per_10 / 10.
	// ex-date 开盘参考价 = 收盘价 / (1 + bonus_per_10 / 10).
	ActionBonusShare ActionType = "bonus_share"
	// ActionSplit 拆股 / 并股.
	// 公式: split_ratio = new_shares / old_shares (e.g. 2 = 1→2 拆).
	// 持仓 qty *= split_ratio, avg_cost /= split_ratio.
	// ex-date 开盘参考价 = 收盘价 / split_ratio.
	ActionSplit ActionType = "split"
	// ActionRightsIssue 配股 (向原股东配售新股, 折价).
	// 公式: 每 10 股配 rights_per_10 股, 价格 rights_price_per_share.
	// ex-date 开盘参考价 = (收盘价 + rights_price_per_share * rights_per_10 / 10)
	//                  / (1 + rights_per_10 / 10).
	// 持仓股东可在缴款日 (pay_date) 缴款配股, 逾期放弃.
	ActionRightsIssue ActionType = "rights_issue"
	// ActionPlacement 增发 (向特定对象 / 公开增发新股, 通常折价).
	// 增发不直接增加原股东持仓, 但会稀释原股东持股比例.
	// 持仓不变, 但 ex-date 开盘参考价需要调整 (市场下跌压力).
	ActionPlacement ActionType = "placement"
)

// CorporateAction 是公司行为的统一接口.
//
// 字段:
//   - Type:        ActionType
//   - Symbol:      ts_code
//   - ExDate:      除权除息日 (此日开盘后股价已调整, 持仓 + 现金已结算)
//   - RecordDate:  股权登记日 (此日收盘后持股的股东享有本次 action)
//   - PayDate:     实际到账日 (现金分红 / 配股缴款日). 可选.
//
// Apply 是纯函数: 给定持仓 (含 cash), 返回新的持仓 + 现金变动.
// 实现必须是 *位置无关* 的 (不修改 pos, 返回新 pos). Cash 增量
// (正=收到现金, 负=支付现金) 由调用方累加.
type CorporateAction interface {
	// Type returns the action type discriminator.
	Type() ActionType
	// Symbol returns the ts_code.
	Symbol() string
	// ExDate returns the ex-date.
	ExDate() time.Time
	// RecordDate returns the record date.
	RecordDate() time.Time
	// PayDate returns the pay date (zero if N/A).
	PayDate() time.Time
	// Apply returns (newPos, cashDelta) given the original position
	// and the previous close price (used for ex-date reference price
	// calculations). Pure: does not modify inputs.
	Apply(pos Position, prevClose float64) (Position, float64)
	// Description returns a human-readable description for audit.
	Description() string
}

// ApplyResult describes the outcome of applying one action to one position.
type ApplyResult struct {
	Action     CorporateAction `json:"-"`
	Symbol     string          `json:"symbol"`
	OldPos     Position        `json:"old_position"`
	NewPos     Position        `json:"new_position"`
	CashDelta  float64         `json:"cash_delta"`
	Applied    bool            `json:"applied"`
	SkipReason string          `json:"skip_reason,omitempty"`
}

// ============================================================
// 1. 现金分红 (Cash Dividend)
// ============================================================

// CashDividend 派发现金红利.
//
// 公式: 每股派 cashPerShare CNY. 持仓股东在 ex-date 收到
// qty * cashPerShare (税前; 含税场景下应预扣 20% 个人所得税, 暂
// 不在引擎内处理, 留待 tax 模块). 同时 ex-date 开盘参考价下调
// cashPerShare.
type CashDividend struct {
	SymbolValue  string
	ExDateValue  time.Time
	RecordValue  time.Time
	PayValue     time.Time
	CashPerShare float64
}

// NewCashDividend creates a cash dividend action.
func NewCashDividend(symbol string, exDate, recordDate, payDate time.Time, cashPerShare float64) *CashDividend {
	return &CashDividend{
		SymbolValue:  symbol,
		ExDateValue:  exDate,
		RecordValue:  recordDate,
		PayValue:     payDate,
		CashPerShare: cashPerShare,
	}
}

func (c *CashDividend) Type() ActionType           { return ActionCashDividend }
func (c *CashDividend) Symbol() string             { return c.SymbolValue }
func (c *CashDividend) ExDate() time.Time          { return c.ExDateValue }
func (c *CashDividend) RecordDate() time.Time      { return c.RecordValue }
func (c *CashDividend) PayDate() time.Time         { return c.PayValue }
func (c *CashDividend) Description() string {
	return fmt.Sprintf("cash_dividend: %s ex=%s record=%s pay=%s %.4f CNY/share",
		c.SymbolValue, c.ExDateValue.Format("2006-01-02"),
		c.RecordValue.Format("2006-01-02"), c.PayValue.Format("2006-01-02"),
		c.CashPerShare)
}

// Apply: qty * cashPerShare 现金入账; 持仓本身不变 (除权后股价
// 调整由 data-feed / broker 同步, 不在此处理).
func (c *CashDividend) Apply(pos Position, _ float64) (Position, float64) {
	if pos.Quantity <= 0 || c.CashPerShare <= 0 {
		return pos, 0
	}
	cash := pos.Quantity * c.CashPerShare
	// 调整 entry_date / metadata 不变; 只返回 cash 增量.
	return pos, cash
}

// ============================================================
// 2. 送股 (Bonus Share)
// ============================================================

// BonusShare 送股 (e.g. 10 送 5 = 每 10 股送 5 股).
//
// 公式: bonusPer10 = 5 → 持股 1000 股 → 新增 500 股, 持仓均价
// 自动下调: avg_cost = avg_cost / (1 + bonusPer10 / 10).
type BonusShare struct {
	SymbolValue string
	ExDateValue time.Time
	RecordValue time.Time
	PayValue    time.Time
	BonusPer10  float64
}

// NewBonusShare creates a bonus share action.
func NewBonusShare(symbol string, exDate, recordDate, payDate time.Time, bonusPer10 float64) *BonusShare {
	return &BonusShare{
		SymbolValue: symbol,
		ExDateValue: exDate,
		RecordValue: recordDate,
		PayValue:    payDate,
		BonusPer10:  bonusPer10,
	}
}

func (b *BonusShare) Type() ActionType      { return ActionBonusShare }
func (b *BonusShare) Symbol() string        { return b.SymbolValue }
func (b *BonusShare) ExDate() time.Time     { return b.ExDateValue }
func (b *BonusShare) RecordDate() time.Time { return b.RecordValue }
func (b *BonusShare) PayDate() time.Time    { return b.PayValue }
func (b *BonusShare) Description() string {
	return fmt.Sprintf("bonus_share: %s ex=%s 10送%.2f",
		b.SymbolValue, b.ExDateValue.Format("2006-01-02"), b.BonusPer10)
}

// Apply: 持仓数量增加, avg_cost 按比例下调, cash 不变.
func (b *BonusShare) Apply(pos Position, _ float64) (Position, float64) {
	if pos.Quantity <= 0 || b.BonusPer10 <= 0 {
		return pos, 0
	}
	ratio := 1 + b.BonusPer10/10
	newPos := pos
	newPos.Quantity = pos.Quantity * ratio
	if pos.AvgCost > 0 {
		newPos.AvgCost = pos.AvgCost / ratio
	}
	// 同步更新 MarketValue / UnrealizedPnL (调用方通常会用最新价
	// 重新计算, 这里给一个粗略估算).
	if pos.CurrentPrice > 0 {
		newPos.MarketValue = newPos.Quantity * newPos.CurrentPrice
		newPos.UnrealizedPnL = newPos.MarketValue - newPos.AvgCost*newPos.Quantity
	}
	return newPos, 0
}

// ============================================================
// 3. 拆股 (Split)
// ============================================================

// CorporateActionSplit 拆股 / 并股. splitRatio = new_shares / old_shares.
//
//   拆股 (splitRatio > 1, e.g. 2): 1 股 → 2 股, 均价减半.
//   并股 (splitRatio < 1, e.g. 0.5): 2 股 → 1 股, 均价翻倍.
//
// Named "CorporateActionSplit" to avoid clash with the historical
// "Split" struct in types.go (which represents a Tushare-style
// split record).
type CorporateActionSplit struct {
	SymbolValue  string
	ExDateValue  time.Time
	RecordValue  time.Time
	PayValue     time.Time
	SplitRatio   float64
}

// NewSplit creates a split action. splitRatio must be > 0.
func NewSplit(symbol string, exDate, recordDate, payDate time.Time, splitRatio float64) *CorporateActionSplit {
	return &CorporateActionSplit{
		SymbolValue: symbol,
		ExDateValue: exDate,
		RecordValue: recordDate,
		PayValue:    payDate,
		SplitRatio:  splitRatio,
	}
}

func (s *CorporateActionSplit) Type() ActionType      { return ActionSplit }
func (s *CorporateActionSplit) Symbol() string        { return s.SymbolValue }
func (s *CorporateActionSplit) ExDate() time.Time     { return s.ExDateValue }
func (s *CorporateActionSplit) RecordDate() time.Time { return s.RecordValue }
func (s *CorporateActionSplit) PayDate() time.Time    { return s.PayValue }
func (s *CorporateActionSplit) Description() string {
	return fmt.Sprintf("split: %s ex=%s ratio=%.4f",
		s.SymbolValue, s.ExDateValue.Format("2006-01-02"), s.SplitRatio)
}

// Apply: qty *= ratio, avg_cost /= ratio, cash 不变.
func (s *CorporateActionSplit) Apply(pos Position, _ float64) (Position, float64) {
	if pos.Quantity <= 0 || s.SplitRatio <= 0 {
		return pos, 0
	}
	newPos := pos
	newPos.Quantity = pos.Quantity * s.SplitRatio
	if pos.AvgCost > 0 {
		newPos.AvgCost = pos.AvgCost / s.SplitRatio
	}
	if pos.CurrentPrice > 0 {
		newPos.MarketValue = newPos.Quantity * newPos.CurrentPrice
		newPos.UnrealizedPnL = newPos.MarketValue - newPos.AvgCost*newPos.Quantity
	}
	return newPos, 0
}

// ============================================================
// 4. 配股 (Rights Issue)
// ============================================================

// RightsIssue 配股 (向原股东配售新股, 折价).
//
// 公式: 每 10 股配 rightsPer10 股, 价格 rightsPricePerShare.
// ex-date 参考价 = (prevClose + rightsPricePerShare * rightsPer10 / 10) /
//                (1 + rightsPer10 / 10).
// 持仓不变, 但 ex-date 起股东可在 pay_date 之前缴款 (call 券商);
// 逾期未缴款视为放弃, 持股数不变, 但 ex-date 参考价已调整.
type RightsIssue struct {
	SymbolValue        string
	ExDateValue        time.Time
	RecordValue        time.Time
	PayValue           time.Time
	RightsPer10        float64
	RightsPricePerShare float64
}

// NewRightsIssue creates a rights issue action.
func NewRightsIssue(symbol string, exDate, recordDate, payDate time.Time, rightsPer10, rightsPricePerShare float64) *RightsIssue {
	return &RightsIssue{
		SymbolValue:        symbol,
		ExDateValue:        exDate,
		RecordValue:        recordDate,
		PayValue:           payDate,
		RightsPer10:        rightsPer10,
		RightsPricePerShare: rightsPricePerShare,
	}
}

func (r *RightsIssue) Type() ActionType      { return ActionRightsIssue }
func (r *RightsIssue) Symbol() string        { return r.SymbolValue }
func (r *RightsIssue) ExDate() time.Time     { return r.ExDateValue }
func (r *RightsIssue) RecordDate() time.Time { return r.RecordValue }
func (r *RightsIssue) PayDate() time.Time    { return r.PayValue }
func (r *RightsIssue) Description() string {
	return fmt.Sprintf("rights_issue: %s ex=%s 10配%.2f @ %.4f CNY",
		r.SymbolValue, r.ExDateValue.Format("2006-01-02"),
		r.RightsPer10, r.RightsPricePerShare)
}

// ExRefPrice returns the ex-date reference price per share.
func (r *RightsIssue) ExRefPrice(prevClose float64) float64 {
	if r.RightsPer10 <= 0 || prevClose <= 0 {
		return prevClose
	}
	ratio := r.RightsPer10 / 10
	return (prevClose + r.RightsPricePerShare*ratio) / (1 + ratio)
}

// Apply: 默认按 "未缴款" 处理 — 持仓不变, cash 不变 (放弃配股).
// 调用方 (券商客户端) 应在 pay_date 之前弹窗提示, 由用户决定
// 是否缴款; 缴款时再调用一次 ApplyPaid() 增加持仓.
func (r *RightsIssue) Apply(pos Position, _ float64) (Position, float64) {
	// 默认行为: 持仓不变 (放弃配股).
	return pos, 0
}

// ApplyPaid 实现 "已缴款配股" — 持仓增加 rights_per_10 / 10 比例, cash 减少对应金额.
func (r *RightsIssue) ApplyPaid(pos Position, _ float64) (Position, float64) {
	if pos.Quantity <= 0 || r.RightsPer10 <= 0 || r.RightsPricePerShare <= 0 {
		return pos, 0
	}
	ratio := r.RightsPer10 / 10
	newShares := pos.Quantity * ratio
	cost := newShares * r.RightsPricePerShare
	newPos := pos
	newPos.Quantity = pos.Quantity + newShares
	// 新 avg_cost = (原成本 + 新股成本) / 新股数.
	if newPos.Quantity > 0 {
		newPos.AvgCost = (pos.AvgCost*pos.Quantity + cost) / newPos.Quantity
	}
	if pos.CurrentPrice > 0 {
		newPos.MarketValue = newPos.Quantity * newPos.CurrentPrice
		newPos.UnrealizedPnL = newPos.MarketValue - newPos.AvgCost*newPos.Quantity
	}
	return newPos, -cost // cash 减少
}

// ============================================================
// 5. 增发 (Placement)
// ============================================================

// Placement 增发 (公开 / 定向). 不直接增加原股东持仓, 但 ex-date
// 开盘参考价会调整 (市场稀释预期). 引擎在 Apply 中只记录 "增发
// 事件发生", 持仓不变; UI 端可读 Description() 提示.
type Placement struct {
	SymbolValue    string
	ExDateValue    time.Time
	RecordValue    time.Time
	PayValue       time.Time
	NewShares      float64 // 增发新股数
	PricePerShare  float64 // 增发价格 (折价)
}

// NewPlacement creates a placement action.
func NewPlacement(symbol string, exDate, recordDate, payDate time.Time, newShares, pricePerShare float64) *Placement {
	return &Placement{
		SymbolValue:   symbol,
		ExDateValue:   exDate,
		RecordValue:   recordDate,
		PayValue:      payDate,
		NewShares:     newShares,
		PricePerShare: pricePerShare,
	}
}

func (p *Placement) Type() ActionType      { return ActionPlacement }
func (p *Placement) Symbol() string        { return p.SymbolValue }
func (p *Placement) ExDate() time.Time     { return p.ExDateValue }
func (p *Placement) RecordDate() time.Time { return p.RecordValue }
func (p *Placement) PayDate() time.Time    { return p.PayValue }
func (p *Placement) Description() string {
	return fmt.Sprintf("placement: %s ex=%s new_shares=%.0f @ %.4f CNY",
		p.SymbolValue, p.ExDateValue.Format("2006-01-02"),
		p.NewShares, p.PricePerShare)
}

// Apply: 持仓不变, cash 不变. UI 端应基于 NewShares 提示股东
// "持股比例稀释 = pos.Quantity / (pos.Quantity + NewShares)".
func (p *Placement) Apply(pos Position, _ float64) (Position, float64) {
	return pos, 0
}

// ============================================================
// ActionEngine — 多 action 调度
// ============================================================

// ActionEngine 维护 symbol → []CorporateAction 队列.
//
// ApplyAll 在 (asOf, positions, prevCloses) 给定时:
//  1. 找出所有 ex_date <= asOf 且尚未 apply 的 action (per symbol).
//  2. 对每个 action 调用 Apply(pos, prevClose), 累计 cash 增量.
//  3. 返回 updated positions + 累计 cash 增量 + 每个 action 的
//     ApplyResult (含 SkipReason).
//
// 引擎是无状态的: 不会自动跳过已应用的 action, 状态由调用方
// 持有 (生产场景应该是数据库 / Redis 持久化).
type ActionEngine struct {
	// 已应用历史 (callers 可注入; nil = 不去重, 全部应用).
	appliedLog map[string]bool // key = "symbol:ex_date:action_type"
}

// NewActionEngine creates an empty engine.
func NewActionEngine() *ActionEngine {
	return &ActionEngine{appliedLog: make(map[string]bool)}
}

// actionKey returns the dedup key for an action.
func actionKey(a CorporateAction) string {
	return fmt.Sprintf("%s:%s:%s", a.Symbol(), a.ExDate().Format("2006-01-02"), a.Type())
}

// actionApplyOrder returns the A-share standard apply order for an
// action type. Lower = applied first. Used by ActionEngine.ApplyAll
// to ensure "split + cash dividend on same ex-date" runs in the
// correct sequence (split first, then cash on new qty).
func actionApplyOrder(t ActionType) int {
	switch t {
	case ActionSplit:
		return 0
	case ActionBonusShare:
		return 1
	case ActionRightsIssue:
		return 2
	case ActionCashDividend:
		return 3
	case ActionPlacement:
		return 4
	}
	return 99
}

// MarkApplied marks an action as already applied (so future ApplyAll
// calls skip it). Idempotent.
func (e *ActionEngine) MarkApplied(a CorporateAction) {
	if e.appliedLog == nil {
		e.appliedLog = make(map[string]bool)
	}
	e.appliedLog[actionKey(a)] = true
}

// IsApplied reports whether an action has been marked applied.
func (e *ActionEngine) IsApplied(a CorporateAction) bool {
	if e.appliedLog == nil {
		return false
	}
	return e.appliedLog[actionKey(a)]
}

// ApplyResultOf bundles the per-position outcome of one action.
type ApplyOutcome struct {
	Action   CorporateAction  `json:"-"`
	Position ApplyResult      `json:"position"`
}

// ApplyAll applies all actions in `actions` whose ex_date <= asOf and
// that haven't been marked applied yet. Returns:
//
//   - newPositions: per-symbol updated positions (input pos unchanged if
//     no actions triggered for that symbol)
//   - totalCashDelta: sum of cash deltas across all actions
//   - outcomes: detailed per-action results (for audit)
//
// The function does NOT mutate the input positions or actions.
func (e *ActionEngine) ApplyAll(
	asOf time.Time,
	positions []Position,
	prevCloses map[string]float64,
	actions []CorporateAction,
) (newPositions []Position, totalCashDelta float64, outcomes []ApplyOutcome) {
	// Build a copy of positions (we will mutate copies, not input).
	newPositions = make([]Position, len(positions))
	copy(newPositions, positions)

	// Sort actions by ex_date asc, then by A-share standard apply
	// order (changes share count first, then cash settlement):
	//   1. Split          (qty *= ratio, cost /= ratio)
	//   2. BonusShare     (qty *= 1+ratio, cost /= 1+ratio)
	//   3. RightsIssue    (applied separately via ApplyPaid)
	//   4. CashDividend   (cash += qty * per_share, AFTER qty changes)
	//   5. Placement      (no position change, just metadata)
	// This ordering matters for "split + cash div" on the same
	// ex_date: split must run first so cash div pays on new qty.
	sorted := make([]CorporateAction, len(actions))
	copy(sorted, actions)
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].ExDate().Equal(sorted[j].ExDate()) {
			return sorted[i].ExDate().Before(sorted[j].ExDate())
		}
		return actionApplyOrder(sorted[i].Type()) < actionApplyOrder(sorted[j].Type())
	})

	posBySymbol := make(map[string]int, len(newPositions))
	for i, p := range newPositions {
		posBySymbol[p.Symbol] = i
	}

	for _, act := range sorted {
		idx, ok := posBySymbol[act.Symbol()]
		if !ok {
			// No position for this symbol; record the action as
			// "applied" so we don't re-process, but produce no
			// cash delta (nothing to pay / receive).
			e.MarkApplied(act)
			outcomes = append(outcomes, ApplyOutcome{
				Action: act,
				Position: ApplyResult{
					Action:     act,
					Symbol:     act.Symbol(),
					Applied:    false,
					SkipReason: "no_position",
				},
			})
			continue
		}

		// Skip if not yet ex-date.
		if act.ExDate().After(asOf) {
			outcomes = append(outcomes, ApplyOutcome{
				Action: act,
				Position: ApplyResult{
					Action:     act,
					Symbol:     act.Symbol(),
					Applied:    false,
					SkipReason: "ex_date_in_future",
				},
			})
			continue
		}

		// Skip if already applied.
		if e.IsApplied(act) {
			outcomes = append(outcomes, ApplyOutcome{
				Action: act,
				Position: ApplyResult{
					Action:     act,
					Symbol:     act.Symbol(),
					Applied:    false,
					SkipReason: "already_applied",
				},
			})
			continue
		}

		prevClose := prevCloses[act.Symbol()]
		newPos, cashDelta := act.Apply(newPositions[idx], prevClose)
		totalCashDelta += cashDelta
		newPositions[idx] = newPos
		e.MarkApplied(act)

		outcomes = append(outcomes, ApplyOutcome{
			Action: act,
			Position: ApplyResult{
				Action:    act,
				Symbol:    act.Symbol(),
				OldPos:    positions[idx],
				NewPos:    newPos,
				CashDelta: cashDelta,
				Applied:   true,
			},
		})
	}
	return newPositions, totalCashDelta, outcomes
}

// ============================================================
// 工具函数
// ============================================================

// roundToShares rounds qty down to integer shares (A-share lot unit
// is 100, but bonus shares can result in odd lots; we round to 1 to
// avoid losing shares).
func roundToShares(qty float64) float64 {
	return math.Round(qty)
}
