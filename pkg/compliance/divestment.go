package compliance

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// ============================================================
// P2-7: 减持规则引擎
//
// 监管依据:
//   - 证监会《上市公司股东、董监高减持股份的若干规定》(2017-05-27 颁布,
//     2024-04-12 最新修订, 简称"减持规定")
//   - 深交所《上市公司股东及董事、监事、高级管理人员减持股份实施细则》
//   - 上交所《上市公司股东及董监高减持股份实施细则》
//   - 证监会《关于进一步规范上市公司董事、监事、高级管理人员买卖本公司
//     股票行为的通知》(证监公司字 [2007] 56 号)
//   - 证监会《上市公司证券发行注册管理办法》(定增 6/18 月限售)
//
// 5 类主体 + 5 类限制:
//   - controlling  (控股股东 / 实控人) — 公开承诺期 + 90 日 ≤ 1% (集中竞价)
//   - director     (董监高) — 任期 + 任期内每年 ≤ 25%
//   - major_5pct   (5% 以上股东) — 90 日 ≤ 1% (集中竞价) + 90 日 ≤ 2% (大宗)
//   - pre_ipo      (Pre-IPO 股东) — 上市后 36 个月限售
//   - placement    (定增对象) — 6 月 / 18 月限售 (战略/控股额外 18 月)
// ============================================================

// ShareholderType classifies the subject of a divestment check.
type ShareholderType string

const (
	HolderTypeControlling ShareholderType = "controlling" // 控股股东 / 实控人
	HolderTypeDirector    ShareholderType = "director"    // 董 / 监 / 高
	HolderTypeMajor5Pct   ShareholderType = "major_5pct"  // 5% 以上股东
	HolderTypePreIPO      ShareholderType = "pre_ipo"     // Pre-IPO 股东
	HolderTypePlacement   ShareholderType = "placement"   // 定增对象

	// HolderTypeUnset 未设置: 用于拒绝检查 + 让调用方显式补全。
	HolderTypeUnset ShareholderType = ""
)

// AllHolderTypes returns every recognized holder type (used by the HTTP
// /api/compliance/divestment/holder-types endpoint to drive UI dropdowns).
func AllHolderTypes() []ShareholderType {
	return []ShareholderType{
		HolderTypeControlling, HolderTypeDirector, HolderTypeMajor5Pct,
		HolderTypePreIPO, HolderTypePlacement,
	}
}

// ReductionMethod distinguishes the three A-share off-load channels.
type ReductionMethod string

const (
	MethodAuction    ReductionMethod = "auction"    // 集中竞价 (二级市场)
	MethodBlockTrade ReductionMethod = "block"      // 大宗交易
	MethodAgreement  ReductionMethod = "agreement"  // 协议转让 (单笔 ≥ 5%)

	// MethodUnset == "" — caller forgot to fill; reduction will be rejected.
	MethodUnset ReductionMethod = ""
)

// AllReductionMethods for HTTP-driven UI dropdowns.
func AllReductionMethods() []ReductionMethod {
	return []ReductionMethod{MethodAuction, MethodBlockTrade, MethodAgreement}
}

// LockupPeriod marks a span during which the holder cannot reduce
// any shares. Multiple periods are unioned (overlap collapses).
//
// 监管要求 Pre-IPO 36 月 / 定增竞价 6 月 / 战略定增 18 月 / 控股股东
// 公开承诺期 (招股书披露) / 董监高任期内。
type LockupPeriod struct {
	StartAt  time.Time `json:"start_at"`  // 锁定期起点
	EndAt    time.Time `json:"end_at"`    // 锁定期终点 (含)
	Reason   string    `json:"reason"`    // 中文原因 (用于 UI 提示)
	Source   string    `json:"source"`    // 法规引用 (用于审计)
	Code     string    `json:"code"`      // 法规代码, e.g. "CSRC-2024-1-4-1"
	Priority int       `json:"priority"`  // 高优先级覆盖低优先级, 同优先级 union
}

// IsActive reports whether this lockup is in force at the given moment.
func (l LockupPeriod) IsActive(t time.Time) bool {
	return !t.Before(l.StartAt) && !t.After(l.EndAt)
}

// Overlaps reports whether the two lockups intersect.
// 区间为半开半闭: [Start, End) — touching at endpoints does NOT count
// as overlap (one ends, the next begins; no day is "owned" by both).
// 与 A 股监管"解禁日 = 锁定期后第一天"的实务一致。
func (l LockupPeriod) Overlaps(o LockupPeriod) bool {
	return l.StartAt.Before(o.EndAt) && o.StartAt.Before(l.EndAt)
}

// String formats a lockup for log / UI display.
func (l LockupPeriod) String() string {
	return fmt.Sprintf("[%s → %s] %s (%s)", l.StartAt.Format("2006-01-02"),
		l.EndAt.Format("2006-01-02"), l.Reason, l.Code)
}

// Reduction is a completed (or pending) reduction in a holder's history.
// Used to compute "已使用 capacity" within a rolling window.
type Reduction struct {
	Symbol   string          `json:"symbol"`     // 股票代码
	Method   ReductionMethod `json:"method"`     // 减持方式
	Quantity float64         `json:"quantity"`   // 实际减持股数
	At       time.Time       `json:"at"`         // 成交时间
	Price    float64         `json:"price"`      // 实际成交均价 (元)
}

// ShareholderProfile describes the subject of a divestment check.
type ShareholderProfile struct {
	UserID        string          `json:"user_id"`         // 股东账户 ID
	Symbol        string          `json:"symbol"`          // 股票代码
	HolderType    ShareholderType `json:"holder_type"`     // 5 类主体之一
	HoldingsPct   float64         `json:"holdings_pct"`    // 持股比例 (%), 0–100
	HoldingsShare float64         `json:"holdings_share"`  // 持股数量 (股)
	AcquiredAt    time.Time       `json:"acquired_at"`     // 首笔买入时间
	Lockups       []LockupPeriod  `json:"lockups"`         // 限售期列表 (可空)

	// Optional: 持股平台标识 (用于公司内部股东台账, 影响控股股东判定)
	IsController bool   `json:"is_controller"`
	ControllerID string `json:"controller_id,omitempty"` // 实控人 ID
}

// ReductionPlan is a proposed reduction.
type ReductionPlan struct {
	Symbol   string          `json:"symbol"`
	Quantity float64         `json:"quantity"` // 拟减持股数
	Method   ReductionMethod `json:"method"`
	At       time.Time       `json:"at"`
}

// DivestmentCheckResult is the full output of a divestment check.
type DivestmentCheckResult struct {
	Allowed       bool            `json:"allowed"`
	UserID        string          `json:"user_id"`
	Symbol        string          `json:"symbol"`
	HolderType    ShareholderType `json:"holder_type"`
	Method        ReductionMethod `json:"method"`
	CheckedAt     time.Time       `json:"checked_at"`

	// 拟减持 vs 实际可减持: Quantity == 0 means fully blocked.
	RequestedQty float64 `json:"requested_qty"`
	ApprovedQty  float64 `json:"approved_qty"`

	// 本期 (窗口) 容量状态 — 都用占公司股份比例 (%) 表示
	WindowStart    time.Time `json:"window_start"`     // 滚动窗起点
	WindowEnd      time.Time `json:"window_end"`       // 滚动窗终点
	WindowCapPct   float64   `json:"window_cap_pct"`   // 窗口期内总容量 (%)
	WindowUsedPct  float64   `json:"window_used_pct"`  // 窗口期内已用 (%)
	WindowRemainPct float64  `json:"window_remain_pct"`// 窗口期内剩余 (%)
	// HoldPct    是 ReducePlan 执行后还持的比例 (%)
	HoldPctAfter float64 `json:"hold_pct_after"`

	Reasons  []string       `json:"reasons"`   // 通过 / 拒绝原因 (audit)
	Lockups  []LockupPeriod `json:"lockups"`   // 命中的限售期 (透明披露)
	Warnings []string       `json:"warnings"`  // 软告警 (不阻塞, 仅提示)
}

// ============================================================
// 默认规则: 与监管文件保持一致, 同时允许 viper 注入覆盖
// ============================================================

// DivestmentRule captures a single holder-type's reduction limits.
type DivestmentRule struct {
	HolderType ShareholderType

	// 集中竞价 (二级市场) 滚动窗口容量
	AuctionWindowDays   int     // 默认 90
	AuctionWindowCapPct float64 // 默认 1.0%

	// 大宗交易滚动窗口容量
	BlockWindowDays   int     // 默认 90
	BlockWindowCapPct float64 // 默认 2.0%

	// 协议转让单笔下限 (%)
	AgreementMinPct float64 // 默认 5.0%

	// 董监高: 任期内每年 (365 日) 上限 (%)
	DirectorAnnualCapPct float64 // 默认 25.0%

	// 上市后强制锁定 (月)
	PreIPOLockupMonths int // 默认 36

	// 定增锁定 (月): 竞价 6 / 战略 18
	PlacementAuctionMonths   int // 默认 6
	PlacementStrategicMonths int // 默认 18
	// PlacementControllerMonths 控股股东 / 实控人参与的定增, 额外 18 月
	PlacementControllerMonths int // 默认 18

	// 法规引用
	Source string
}

// DefaultDivestmentRules returns the per-holder-type rules matching
// the 2024 CSRC "减持规定" + 深沪实施细则.
func DefaultDivestmentRules() map[ShareholderType]*DivestmentRule {
	common := DivestmentRule{
		AuctionWindowDays:         90,
		AuctionWindowCapPct:       1.0,
		BlockWindowDays:           90,
		BlockWindowCapPct:         2.0,
		AgreementMinPct:           5.0,
		DirectorAnnualCapPct:      25.0,
		PreIPOLockupMonths:        36,
		PlacementAuctionMonths:    6,
		PlacementStrategicMonths:  18,
		PlacementControllerMonths: 18,
		Source: "CSRC-2024-减持规定",
	}
	return map[ShareholderType]*DivestmentRule{
		HolderTypeControlling: {
			HolderType:                HolderTypeControlling,
			AuctionWindowDays:         common.AuctionWindowDays,
			AuctionWindowCapPct:       common.AuctionWindowCapPct,
			BlockWindowDays:           common.BlockWindowDays,
			BlockWindowCapPct:         common.BlockWindowCapPct,
			AgreementMinPct:           common.AgreementMinPct,
			PreIPOLockupMonths:        common.PreIPOLockupMonths,
			PlacementControllerMonths: common.PlacementControllerMonths,
			Source: "CSRC-2024-减持规定 第 1 条 / 第 3 条",
		},
		HolderTypeDirector: {
			HolderType:           HolderTypeDirector,
			AuctionWindowDays:    common.AuctionWindowDays,
			AuctionWindowCapPct:  common.AuctionWindowCapPct,
			BlockWindowDays:      common.BlockWindowDays,
			BlockWindowCapPct:    common.BlockWindowCapPct,
			AgreementMinPct:      common.AgreementMinPct,
			DirectorAnnualCapPct: common.DirectorAnnualCapPct,
			Source:               "CSRC-2007-56号 + CSRC-2024-减持规定 第 9 条",
		},
		HolderTypeMajor5Pct: {
			HolderType:           HolderTypeMajor5Pct,
			AuctionWindowDays:    common.AuctionWindowDays,
			AuctionWindowCapPct:  common.AuctionWindowCapPct,
			BlockWindowDays:      common.BlockWindowDays,
			BlockWindowCapPct:    common.BlockWindowCapPct,
			AgreementMinPct:      common.AgreementMinPct,
			Source:               "CSRC-2024-减持规定 第 1 条",
		},
		HolderTypePreIPO: {
			HolderType:         HolderTypePreIPO,
			PreIPOLockupMonths: common.PreIPOLockupMonths,
			// Pre-IPO 股东限售期内不区分方式, 全部禁止
			AuctionWindowCapPct: 0,
			BlockWindowCapPct:   0,
			Source:              "CSRC-2024-减持规定 第 5 条 + 各板块上市规则",
		},
		HolderTypePlacement: {
			HolderType:                HolderTypePlacement,
			AuctionWindowDays:         common.AuctionWindowDays,
			AuctionWindowCapPct:       common.AuctionWindowCapPct,
			BlockWindowDays:           common.BlockWindowDays,
			BlockWindowCapPct:         common.BlockWindowCapPct,
			AgreementMinPct:           common.AgreementMinPct,
			PlacementAuctionMonths:    common.PlacementAuctionMonths,
			PlacementStrategicMonths:  common.PlacementStrategicMonths,
			PlacementControllerMonths: common.PlacementControllerMonths,
			Source:                    "CSRC-2023-注册管理办法 第 59 条",
		},
	}
}

// ============================================================
// DivestmentChecker — 减持检查器
// ============================================================

// DivestmentChecker runs reduction-plan checks against the per-type
// rules + a holder's history of recent reductions.
type DivestmentChecker struct {
	mu    sync.RWMutex
	rules map[ShareholderType]*DivestmentRule
	now   func() time.Time
}

// NewDivestmentChecker constructs a checker with defaults and a
// customisable clock (tests pass a fixed clock).
func NewDivestmentChecker(now func() time.Time) *DivestmentChecker {
	if now == nil {
		now = time.Now
	}
	return &DivestmentChecker{
		rules: DefaultDivestmentRules(),
		now:   now,
	}
}

// Rules returns a deep copy of the current rule set (snapshot).
func (c *DivestmentChecker) Rules() map[ShareholderType]*DivestmentRule {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[ShareholderType]*DivestmentRule, len(c.rules))
	for k, v := range c.rules {
		copy := *v
		out[k] = &copy
	}
	return out
}

// SetRule replaces a single holder type's rule. Pass nil to remove.
func (c *DivestmentChecker) SetRule(t ShareholderType, r *DivestmentRule) error {
	if t == HolderTypeUnset {
		return fmt.Errorf("holder type must not be unset")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if r == nil {
		delete(c.rules, t)
		return nil
	}
	if r.HolderType != t {
		return fmt.Errorf("rule holder_type %q does not match key %q", r.HolderType, t)
	}
	c.rules[t] = r
	return nil
}

// ResetRules restores the rule set to the regulatory defaults.
func (c *DivestmentChecker) ResetRules() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rules = DefaultDivestmentRules()
}

// Check runs the full divestment evaluation.
//
// Algorithm:
//  1. 输入校验 (holder type, method 必填, quantity > 0)
//  2. 命中限售期 → 拒绝 (无 allowed_qty)
//  3. 滚动窗口累加 + 算 remaining capacity
//  4. Director 附加: 12 个月年内 25% 上限
//  5. 算 approved_qty = min(requested, remaining, holdings)
//  6. 输出结构化 reasons
func (c *DivestmentChecker) Check(
	profile ShareholderProfile,
	plan ReductionPlan,
	recent []Reduction,
) DivestmentCheckResult {
	now := c.now()
	result := DivestmentCheckResult{
		UserID:     profile.UserID,
		Symbol:     plan.Symbol,
		HolderType: profile.HolderType,
		Method:     plan.Method,
		CheckedAt:  now,
		RequestedQty: plan.Quantity,
		Lockups:   []LockupPeriod{},
		Reasons:   []string{},
		Warnings:  []string{},
	}
	if profile.Symbol != "" && plan.Symbol != "" && profile.Symbol != plan.Symbol {
		result.Allowed = false
		result.Reasons = append(result.Reasons, fmt.Sprintf(
			"profile.symbol=%q 与 plan.symbol=%q 不一致", profile.Symbol, plan.Symbol))
		return result
	}

	// 1) 输入校验
	if profile.HolderType == HolderTypeUnset {
		result.Reasons = append(result.Reasons, "股东类型未设置")
		return result
	}
	if plan.Method == MethodUnset {
		result.Reasons = append(result.Reasons, "减持方式未设置")
		return result
	}
	if plan.Quantity <= 0 {
		result.Reasons = append(result.Reasons, "拟减持数量必须为正数")
		return result
	}
	if profile.HoldingsShare > 0 && plan.Quantity > profile.HoldingsShare {
		result.Reasons = append(result.Reasons, fmt.Sprintf(
			"拟减持 %.0f 股超过实际持股 %.0f 股", plan.Quantity, profile.HoldingsShare))
		return result
	}

	rule, ok := c.lookupRule(profile.HolderType)
	if !ok || rule == nil {
		result.Reasons = append(result.Reasons, fmt.Sprintf(
			"股东类型 %q 缺少规则", profile.HolderType))
		return result
	}

	// 2) 限售期检查
	activeLockups := c.activeLockupsAt(profile.Lockups, plan.At)
	if len(activeLockups) > 0 {
		result.Lockups = activeLockups
		result.Reasons = append(result.Reasons, fmt.Sprintf(
			"命中 %d 条限售期, 不得减持:", len(activeLockups)))
		for _, l := range activeLockups {
			result.Reasons = append(result.Reasons, "  - "+l.String())
		}
		return result
	}

	// 3) 协议转让单笔下限
	if plan.Method == MethodAgreement && profile.HoldingsPct > 0 {
		pctThisTrade := (plan.Quantity / profile.HoldingsShare) * profile.HoldingsPct
		if pctThisTrade < rule.AgreementMinPct {
			result.Reasons = append(result.Reasons, fmt.Sprintf(
				"协议转让单笔须 ≥ %.1f%% 股份, 本次拟减持 %.4f%%", rule.AgreementMinPct, pctThisTrade))
			return result
		}
	}

	// 4) 滚动窗口: 按 method 决定 cap + window days
	windowDays := rule.AuctionWindowDays
	windowCap := rule.AuctionWindowCapPct
	if plan.Method == MethodBlockTrade {
		windowDays = rule.BlockWindowDays
		windowCap = rule.BlockWindowCapPct
	}
	if windowDays <= 0 || windowCap <= 0 {
		result.Reasons = append(result.Reasons, fmt.Sprintf(
			"股东类型 %q + 方式 %q 不允许减持 (窗口容量为 0)", profile.HolderType, plan.Method))
		return result
	}
	windowStart := plan.At.AddDate(0, 0, -windowDays)
	result.WindowStart = windowStart
	result.WindowEnd = plan.At
	result.WindowCapPct = windowCap

	// 累计本窗口内同 method 的历史减持
	usedPct := c.windowUsedPct(recent, plan, windowStart, plan.At, profile.HoldingsShare, profile.HoldingsPct)
	result.WindowUsedPct = round4(usedPct)

	// 5) Director 附加: 年内 25% 上限 (含本次拟减持)
	directorAnnualCap := math.Inf(+1)
	if profile.HolderType == HolderTypeDirector {
		directorAnnualCap = rule.DirectorAnnualCapPct
		annualStart := plan.At.AddDate(-1, 0, 0)
		annualUsed := c.windowUsedPct(recent, plan, annualStart, plan.At, profile.HoldingsShare, profile.HoldingsPct)
		// 本次拟减持占比 = plan.Quantity / totalShares
		proposedPct := 0.0
		if total := c.totalSharesFromPct(profile, recent); total > 0 {
			proposedPct = (plan.Quantity / total) * 100.0
		}
		if annualUsed+proposedPct > directorAnnualCap {
			result.Reasons = append(result.Reasons, fmt.Sprintf(
				"董监高年度 25%% 上限已用 %.4f%% + 本次拟减持 %.4f%% = %.4f%%",
				annualUsed, proposedPct, annualUsed+proposedPct))
			return result
		}
	}

	// 综合剩余容量
	remaining := math.Min(windowCap-usedPct, directorAnnualCap-usedPct)
	if remaining < 0 {
		remaining = 0
	}
	result.WindowRemainPct = round4(remaining)

	// 6) approved_qty = min(requested, capacity-based, holdings)
	approvedByPct := (remaining / 100.0) * c.totalSharesFromPct(profile, recent)
	approved := plan.Quantity
	if profile.HoldingsShare > 0 {
		// 已知持仓数: 直接按 (剩余%) × 流通股本 算
		// totalSharesFromPct = holdingsShare / (holdingsPct/100)
		total := profile.HoldingsShare / (profile.HoldingsPct / 100.0)
		approvedByPct = (remaining / 100.0) * total
	}
	if approved > approvedByPct {
		approved = approvedByPct
	}
	if profile.HoldingsShare > 0 && approved > profile.HoldingsShare {
		approved = profile.HoldingsShare
	}
	// 取整: 减持以"手"为单位, 1 手 = 100 股
	approved = math.Round(approved/100) * 100
	result.ApprovedQty = approved

	// 减持后剩余比例
	if profile.HoldingsShare > 0 {
		result.HoldPctAfter = round4(profile.HoldingsPct - (approved/profile.HoldingsShare)*profile.HoldingsPct)
	}

	if result.ApprovedQty < plan.Quantity {
		result.Reasons = append(result.Reasons, fmt.Sprintf(
			"按窗口剩余容量仅可减持 %.0f 股 (拟减持 %.0f 股)", result.ApprovedQty, plan.Quantity))
		result.Warnings = append(result.Warnings, "已自动截短 approved_qty 到窗口剩余容量")
		// 截短属于部分通过 — 仍然 Allowed = true (但 ApprovedQty < RequestedQty)
	} else {
		result.Reasons = append(result.Reasons, "通过: 拟减持量在窗口剩余容量内")
	}

	// 7) 减持后降至 5% 以下 / 退市敏感度告警
	if profile.HoldingsPct > 0 {
		post := result.HoldPctAfter
		if post > 0 && post < 5.0 && profile.HoldingsPct >= 5.0 {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"本次减持后持股 %.4f%% < 5%%, 触发举牌义务 (1%% 线 / 5%% 线)", post))
		}
		if post > 0 && post < 1.0 && profile.HoldingsPct >= 1.0 {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"本次减持后持股 %.4f%% < 1%%, 触发举牌义务 (1%% 线)", post))
		}
	}

	result.Allowed = true
	return result
}

// ============================================================
// 内部 helpers
// ============================================================

func (c *DivestmentChecker) lookupRule(t ShareholderType) (*DivestmentRule, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.rules[t]
	_ = ok
	if r == nil {
		return nil, false
	}
	copy := *r
	return &copy, true
}

func (c *DivestmentChecker) activeLockupsAt(lockups []LockupPeriod, t time.Time) []LockupPeriod {
	active := make([]LockupPeriod, 0, len(lockups))
	for _, l := range lockups {
		if l.IsActive(t) {
			active = append(active, l)
		}
	}
	sort.Slice(active, func(i, j int) bool {
		if active[i].Priority != active[j].Priority {
			return active[i].Priority > active[j].Priority
		}
		return active[i].StartAt.Before(active[j].StartAt)
	})
	return active
}

// windowUsedPct 累计窗口内同 method / 同 symbol 的历史减持占公司股份比例。
//
// 公式: ∑ (quantity_i / totalShares) × 100%
//  totalShares 优先用 holdingsShare / (holdingsPct/100) 反推; 推不出时
//  用 totalSharesFromPct 占位。
func (c *DivestmentChecker) windowUsedPct(
	history []Reduction,
	plan ReductionPlan,
	winStart, winEnd time.Time,
	holdingsShare, holdingsPct float64,
) float64 {
	totalShares := c.totalSharesFromPct(
		ShareholderProfile{Symbol: plan.Symbol, HoldingsShare: holdingsShare, HoldingsPct: holdingsPct},
		history,
	)
	if totalShares <= 0 {
		return 0
	}
	var used float64
	for _, r := range history {
		if r.Symbol != plan.Symbol {
			continue
		}
		if r.Method != plan.Method {
			continue
		}
		if r.At.Before(winStart) || r.At.After(winEnd) {
			continue
		}
		used += r.Quantity
	}
	return (used / totalShares) * 100.0
}

// totalSharesFromPct 反推公司总股本: holdingsShare / (holdingsPct/100)。
// 若无 holdings 信息 → 返回 0 (caller 把 used 视为 0, 不阻塞)。
func (c *DivestmentChecker) totalSharesFromPct(p ShareholderProfile, _ []Reduction) float64 {
	if p.HoldingsShare <= 0 || p.HoldingsPct <= 0 {
		return 0
	}
	return p.HoldingsShare / (p.HoldingsPct / 100.0)
}

// round4 保留 4 位小数 (UI 展示 + diff 友好)
func round4(f float64) float64 {
	return math.Round(f*10000) / 10000
}

// ============================================================
// 静态分析辅助: 给 "股东类型 + 方式" 的人类可读标签
// ============================================================

// HolderTypeLabel 返回中文 UI 标签。
func HolderTypeLabel(t ShareholderType) string {
	switch t {
	case HolderTypeControlling:
		return "控股股东 / 实控人"
	case HolderTypeDirector:
		return "董事 / 监事 / 高级管理人员"
	case HolderTypeMajor5Pct:
		return "5% 以上股东"
	case HolderTypePreIPO:
		return "Pre-IPO 股东"
	case HolderTypePlacement:
		return "定增对象"
	default:
		return string(t)
	}
}

// MethodLabel 返回中文 UI 标签。
func MethodLabel(m ReductionMethod) string {
	switch m {
	case MethodAuction:
		return "集中竞价"
	case MethodBlockTrade:
		return "大宗交易"
	case MethodAgreement:
		return "协议转让"
	default:
		return string(m)
	}
}

// String 实现 Stringer 便于 log。
func (t ShareholderType) String() string { return HolderTypeLabel(t) }

// String 实现 Stringer 便于 log。
func (m ReductionMethod) String() string { return MethodLabel(m) }
