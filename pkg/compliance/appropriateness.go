// Package compliance — A-share investor suitability & abnormal-trade
// detection (P2-4, P2-5, P2-6).
//
// 监管依据:
//   - 《证券期货投资者适当性管理办法》(2017-07-01 施行, 2023 修订)
//   - 深交所《创业板投资者适当性管理实施办法》(2020-04-28 注册制改革)
//   - 上交所《科创板投资者适当性管理实施细则》(2019-03 发布, 2020 修订)
//   - 北交所《投资者适当性管理细则》(2021-09-03 发布, 2023 修订)
//   - 证监会《证券市场操纵行为认定指引(试行)》(证券法 55 条)
//
// 设计目标:
//   - 自包含: 不依赖外部 DB / HTTP, 纯函数式判定 + 接口注入
//   - 审计友好: 每次判定都返回结构化 Reason, 监管回溯可读
//   - 可配置: 阈值从 config 注入, 测试时硬编码不污染生产
//
// 三大模块:
//   - appropriateness.go (P2-4) — 投资者适当性, 准入门槛
//   - abnormal_trade.go  (P2-5) — 异常交易监控, 6 类行为检测
//   - reporter.go        (P2-6) — 大额交易报告, 日终汇总
package compliance

import (
	"fmt"
	"sync"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/live"
)

// ============================================================
// P2-4: 投资者适当性
// ============================================================

// BoardRequirement captures the per-board eligibility threshold that
// the user must meet to trade a particular A-share board.
//
// 资产门槛按"申请开通前 20 个交易日日均"计算 (净资产 + 现金,
// 不含融资融券负债); 交易经验按"首笔 A 股交易起算"满 24 个月
// 计算。RiskLevel 字段映射到投资者风险测评等级 (C1-C5); 用户的
// RiskLevel 必须 ≥ 板级要求才能准入。
type BoardRequirement struct {
	Board             live.Board
	AssetThresholdCNY float64   // 20日日均资产下限 (CNY)
	ExperienceMonths  int       // 交易经验下限 (月)
	RiskLevel         RiskLevel // 投资者风险测评等级下限
	DisplayName       string    // 监管文件中的中文名
	Description       string    // 给前端的提示文案
}

// RiskLevel maps to the 5-tier investor risk profile (C1=保守, C5=激进).
// 见《证券期货投资者适当性管理办法》第二十条:
//
//	投资者风险等级: C1(保守)/C2(谨慎)/C3(稳健)/C4(积极)/C5(激进)
type RiskLevel int

const (
	// RiskLevelUnset — 未做风险测评 (默认拒绝所有受限板).
	RiskLevelUnset RiskLevel = 0
	// RiskLevelConservative — C1 保守.
	RiskLevelConservative RiskLevel = 1
	// RiskLevelCautious — C2 谨慎.
	RiskLevelCautious RiskLevel = 2
	// RiskLevelSteady — C3 稳健.
	RiskLevelSteady RiskLevel = 3
	// RiskLevelAggressive — C4 积极.
	RiskLevelAggressive RiskLevel = 4
	// RiskLevelSpeculative — C5 激进.
	RiskLevelSpeculative RiskLevel = 5
)

func (r RiskLevel) String() string {
	switch r {
	case RiskLevelConservative:
		return "C1-保守"
	case RiskLevelCautious:
		return "C2-谨慎"
	case RiskLevelSteady:
		return "C3-稳健"
	case RiskLevelAggressive:
		return "C4-积极"
	case RiskLevelSpeculative:
		return "C5-激进"
	default:
		return "未测评"
	}
}

// 默认门槛 (P2-4 任务规格书中给出, 实际生产可通过 config 覆盖).
var DefaultBoardRequirements = []BoardRequirement{
	{
		Board:             live.BoardChiNext,
		AssetThresholdCNY: 100_000, // 10 万
		ExperienceMonths:  24,
		RiskLevel:         RiskLevelSteady, // C3 稳健
		DisplayName:       "创业板",
		Description:       "申请开通前 20 个交易日证券账户及资金账户内的资产日均不低于人民币 10 万元；参与证券交易 24 个月以上；风险等级 C3 稳健及以上。",
	},
	{
		Board:             live.BoardSTAR,
		AssetThresholdCNY: 500_000, // 50 万
		ExperienceMonths:  24,
		RiskLevel:         RiskLevelAggressive, // C4 积极
		DisplayName:       "科创板",
		Description:       "申请开通前 20 个交易日证券账户及资金账户内的资产日均不低于人民币 50 万元；参与证券交易 24 个月以上；风险等级 C4 积极及以上。",
	},
	{
		Board:             live.BoardBSE,
		AssetThresholdCNY: 1_000_000, // 100 万
		ExperienceMonths:  24,
		RiskLevel:         RiskLevelAggressive, // C4 积极
		DisplayName:       "北交所",
		Description:       "申请开通前 20 个交易日证券账户及资金账户内的资产日均不低于人民币 100 万元；参与证券交易 24 个月以上；风险等级 C4 积极及以上。",
	},
}

// BoardsRequiringSuitability returns the set of boards whose trades
// must be pre-checked by the InvestorSuitability policy.
//
// 主板 / 中小板 / ETF / 债券 / 基金 / 指数 / B 股 / 优先股 都不在
// 适当性管理范围内 (门槛为 0).
func BoardsRequiringSuitability() []live.Board {
	return []live.Board{live.BoardChiNext, live.BoardSTAR, live.BoardBSE}
}

// SuitabilityProfile is the user-side state checked against the
// board's BoardRequirement. The fields are immutable from the
// compliance module's perspective — they come from the user table
// (or, in paper-trading mode, from a config-driven stub).
type SuitabilityProfile struct {
	UserID            string    `json:"user_id"`
	AssetDailyAvgCNY  float64   // 20 个交易日日均资产 (CNY)
	FirstTradeAt      time.Time // 首笔 A 股交易时间; 用于计算经验月数
	RiskLevel         RiskLevel // 投资者风险测评等级
	BoardsEnabled     []string  // 已开通的板块列表 (overrides above if non-empty)
	RiskTestExpiredAt time.Time // 风险测评过期时间; 过期后按 RiskLevelUnset 处理
}

// ExperienceMonths returns the number of full calendar months since
// the user's first A-share trade, rounded DOWN. A 23-month-29-day
// window rounds to 23, not 24 — strict regulatory math.
func (p *SuitabilityProfile) ExperienceMonths(now time.Time) int {
	if p.FirstTradeAt.IsZero() {
		return 0
	}
	if now.Before(p.FirstTradeAt) {
		return 0
	}
	years := now.Year() - p.FirstTradeAt.Year()
	months := int(now.Month()) - int(p.FirstTradeAt.Month())
	total := years*12 + months
	if now.Day() < p.FirstTradeAt.Day() {
		total--
	}
	if total < 0 {
		return 0
	}
	return total
}

// IsBoardEnabled reports whether the user has explicitly opted in
// to a particular board. When BoardsEnabled is non-empty, the
// eligibility gate accepts the board ONLY if it is in this list —
// even if the user meets all numeric thresholds.
func (p *SuitabilityProfile) IsBoardEnabled(board live.Board) bool {
	if len(p.BoardsEnabled) == 0 {
		// Empty whitelist means "all of them" — but only for users
		// that have not yet had their board permissions recorded
		// (legacy accounts from before this field was added).
		return true
	}
	for _, b := range p.BoardsEnabled {
		if b == board.String() {
			return true
		}
	}
	return false
}

// CheckResult is the structured outcome of a suitability check.
// It is intentionally a value (not error) so the caller can pass it
// up the chain (e.g. risk manager) and render the same payload to
// both logs and the UI.
type CheckResult struct {
	Allowed     bool       // true = 用户有交易该板权限
	Board       live.Board // 板块
	Reasons     []string   // 不通过原因 (Allowed=true 时为空)
	UserID      string     // 用户 ID (审计用)
	ProfileAge  int        // 用户经验月数 (审计用)
	AssetDaily  float64    // 用户 20 日日均资产 (审计用)
	RiskLevel   RiskLevel  // 用户风险等级
	Required    *BoardRequirement // 板级要求 (审计 + UI 文案)
	CheckedAt   time.Time  // 判定时间
}

// IsAllowed returns true if the user may trade `symbol` on the given
// board. This is a thin alias for Check(...) with a single-symbol
// board pre-classification — production callers usually use
// CheckSymbol(...) instead, which is more ergonomic.
func (p *SuitabilityProfile) IsAllowed(board live.Board, now time.Time) CheckResult {
	return p.Check(board, now)
}

// Check runs the three-gate eligibility test for `board` against
// the user's profile. All three gates must pass:
//
//	1. RiskTestExpiredAt must be in the future (测评分未过期)
//	2. RiskLevel >= BoardRequirement.RiskLevel
//	3. AssetDailyAvgCNY >= BoardRequirement.AssetThresholdCNY
//	4. ExperienceMonths >= BoardRequirement.ExperienceMonths
//	5. BoardsEnabled includes the board (when whitelist is non-empty)
//
// The check is order-stable: reasons appear in regulatory order
// (risk first, then asset, then experience) so the audit log is
// reproducible.
func (p *SuitabilityProfile) Check(board live.Board, now time.Time) CheckResult {
	req := LookupRequirement(board)
	result := CheckResult{
		Board:      board,
		UserID:     p.UserID,
		ProfileAge: p.ExperienceMonths(now),
		AssetDaily: p.AssetDailyAvgCNY,
		RiskLevel:  p.RiskLevel,
		Required:   req,
		CheckedAt:  now,
	}
	if req == nil {
		// Board not in the suitability scope (e.g. main board, ETF).
		// 准入 is implicit; return Allowed=true with no reasons.
		result.Allowed = true
		return result
	}

	// Gate 1: risk test validity. An expired test is treated as
	// RiskLevelUnset and blocks all restricted boards.
	if !p.RiskTestExpiredAt.IsZero() && now.After(p.RiskTestExpiredAt) {
		result.Reasons = append(result.Reasons,
			fmt.Sprintf("风险测评已过期 (到期日 %s); 请重新测评后开通权限",
				p.RiskTestExpiredAt.Format("2006-01-02")))
	}

	// Gate 2: risk level.
	if p.RiskLevel < req.RiskLevel {
		result.Reasons = append(result.Reasons,
			fmt.Sprintf("风险等级不足: 当前 %s, 板块要求 ≥ %s",
				p.RiskLevel, req.RiskLevel))
	}

	// Gate 3: asset threshold.
	if p.AssetDailyAvgCNY < req.AssetThresholdCNY {
		result.Reasons = append(result.Reasons,
			fmt.Sprintf("资产门槛不足: 当前 20 日日均 %.2f CNY, 板块要求 ≥ %.2f CNY",
				p.AssetDailyAvgCNY, req.AssetThresholdCNY))
	}

	// Gate 4: experience months.
	if p.ExperienceMonths(now) < req.ExperienceMonths {
		result.Reasons = append(result.Reasons,
			fmt.Sprintf("交易经验不足: 当前 %d 个月, 板块要求 ≥ %d 个月",
				p.ExperienceMonths(now), req.ExperienceMonths))
	}

	// Gate 5: explicit opt-in (whitelist).
	if !p.IsBoardEnabled(board) {
		result.Reasons = append(result.Reasons,
			fmt.Sprintf("未开通 %s 权限 (BoardsEnabled 白名单不含 %s)",
				req.DisplayName, board))
	}

	result.Allowed = len(result.Reasons) == 0
	return result
}

// CheckSymbol resolves a ts_code to a board and runs the check. This
// is the entry point used by the order-precheck handler — it does
// the board classification for the caller so the caller doesn't
// have to import pkg/live.
func (p *SuitabilityProfile) CheckSymbol(symbol string, now time.Time) CheckResult {
	board := live.ClassifySymbol(symbol)
	result := p.Check(board, now)
	return result
}

// ============================================================
// LookupRequirement + registry (concurrency-safe)
// ============================================================

var (
	registryMu sync.RWMutex
	registry   = buildDefaultRegistry()
)

func buildDefaultRegistry() map[live.Board]*BoardRequirement {
	out := make(map[live.Board]*BoardRequirement, len(DefaultBoardRequirements))
	for i := range DefaultBoardRequirements {
		req := DefaultBoardRequirements[i]
		// Copy by value so taking &req is safe (no loop-variable alias).
		c := req
		out[req.Board] = &c
	}
	return out
}

// LookupRequirement returns the registered BoardRequirement for the
// given board, or nil if the board is not in the suitability scope
// (e.g. main board — no check required).
func LookupRequirement(board live.Board) *BoardRequirement {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry[board]
}

// SetRequirement overrides the registered requirement for a single
// board. Used by the config loader to apply environment-specific
// thresholds (e.g. securities firm may require 100k for ChiNext on
// a per-account basis). Pass nil to remove the entry.
//
// Returns an error if the input is invalid (negative threshold, etc.).
// This is the only mutating function in the package — all others are
// pure reads, which is why the registry is RWMutex-guarded.
func SetRequirement(board live.Board, req *BoardRequirement) error {
	if board == "" {
		return fmt.Errorf("board must be non-empty")
	}
	if req != nil {
		if req.AssetThresholdCNY < 0 {
			return fmt.Errorf("asset threshold must be ≥ 0, got %.2f", req.AssetThresholdCNY)
		}
		if req.ExperienceMonths < 0 {
			return fmt.Errorf("experience months must be ≥ 0, got %d", req.ExperienceMonths)
		}
		if req.RiskLevel < RiskLevelUnset || req.RiskLevel > RiskLevelSpeculative {
			return fmt.Errorf("risk level out of range: %d", req.RiskLevel)
		}
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if req == nil {
		delete(registry, board)
		return nil
	}
	c := *req
	registry[board] = &c
	return nil
}

// ResetRegistry restores the registry to its default state. Used by
// tests in t.Cleanup() to keep one suite's overrides from leaking
// into the next.
func ResetRegistry() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = buildDefaultRegistry()
}

// AllRequirements returns a copy of the current registered
// requirements. The copy is taken under the read lock so the
// returned slice is safe to iterate / serialize without racing
// the registry mutator.
func AllRequirements() []BoardRequirement {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]BoardRequirement, 0, len(registry))
	for _, r := range registry {
		out = append(out, *r)
	}
	return out
}
