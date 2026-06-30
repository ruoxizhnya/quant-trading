// Package margin implements 融资融券 (margin trading) account management,
// shortable stock registry, and pure-function margin calculators (P2-9).
//
// 监管依据:
//   - 《上海证券交易所融资融券交易实施细则》(2023 修订) §2.1: 投资者
//     融资买入证券时, 融资保证金比例不得低于 50% (§2.4); 融券卖出时,
//     融券保证金比例不得低于 50% (§2.5)。
//   - §2.6: 维持担保比例 = (现金 + 信用证券账户内证券市值) / (融资买入
//     金额 + 融券卖出数量 × 市价 + 利息及费用), 不得低于 130%; 低于
//     130% 时, 券商应在 T+1 日内通知投资者补仓, 低于 130% 且未补仓的
//     T+2 日强制平仓。
//   - §2.7: 维持担保比例低于 150% 时, 券商应向投资者发出预警通知。
//   - 《深圳证券交易所融资融券交易实施细则》(2023 修订) 同上。
//   - 中国证券业协会 《证券公司融资融券业务风险管理规范》(2022):
//     融资利率参考值 6%/年, 融券利率参考值 8%/年, 按日计息。
//
// S7-P2-2: extracted from pkg/live/margin.go (1369 lines) as a clean
// leaf sub-package. Zero internal dependencies on the parent pkg/live,
// zero external consumers — the margin subsystem is self-contained.
package margin

import (
	"fmt"
	"math"
	"time"
)

// ============================================================
// 配置 / 阈值
// ============================================================

// MarginConfig 决定 MarginAccount 的保证金比率与利率参数。
//
//	InitialMarginRate:         初始保证金比例, 默认 0.5 (50%, A 股监管下限)。
//	MaintenanceRatioFloor:     维持担保比例下限, 默认 1.3 (130%, 强制平仓线)。
//	WarningRatio:              预警担保比例, 默认 1.5 (150%, 警告线)。
//	FinancingRate:             融资年化利率, 默认 0.06 (6%)。
//	SecuritiesLendingRate:    融券年化利率, 默认 0.106 (10.6%, per VISION.md).
//	DaysPerYear:               计息天数基准, 默认 365 (自然日)。
//	Now:                       时钟注入 (测试用)。nil → time.Now。
type MarginConfig struct {
	InitialMarginRate     float64
	MaintenanceRatioFloor float64
	WarningRatio          float64
	FinancingRate         float64
	SecuritiesLendingRate float64
	DaysPerYear           int
	Now                   func() time.Time
}

// DefaultMarginConfig returns regulatory-recommended defaults.
//
// The defaults match the 《上海证券交易所融资融券交易实施细则》
// (2023 修订) §2.4-2.7 and 中国证券业协会 reference rates.
func DefaultMarginConfig() MarginConfig {
	return MarginConfig{
		InitialMarginRate:     0.5,
		MaintenanceRatioFloor: 1.3,
		WarningRatio:          1.5,
		FinancingRate:         0.06,
		SecuritiesLendingRate: 0.106,
		DaysPerYear:           365,
	}
}

// Validate checks that all rate fields are within sane bounds.
func (c MarginConfig) Validate() error {
	if c.InitialMarginRate < 0 || c.InitialMarginRate > 1 {
		return fmt.Errorf("initial_margin_rate must be in [0, 1], got %f", c.InitialMarginRate)
	}
	if c.MaintenanceRatioFloor < 1 {
		return fmt.Errorf("maintenance_ratio_floor must be >= 1.0, got %f", c.MaintenanceRatioFloor)
	}
	if c.WarningRatio < c.MaintenanceRatioFloor {
		return fmt.Errorf("warning_ratio (%f) must be >= maintenance_ratio_floor (%f)",
			c.WarningRatio, c.MaintenanceRatioFloor)
	}
	if c.FinancingRate < 0 || c.FinancingRate > 1 {
		return fmt.Errorf("financing_rate must be in [0, 1], got %f", c.FinancingRate)
	}
	if c.SecuritiesLendingRate < 0 || c.SecuritiesLendingRate > 1 {
		return fmt.Errorf("securities_lending_rate must be in [0, 1], got %f", c.SecuritiesLendingRate)
	}
	if c.DaysPerYear <= 0 {
		return fmt.Errorf("days_per_year must be > 0, got %d", c.DaysPerYear)
	}
	return nil
}

// ============================================================
// MarginCalculator — 纯函数计算器
// ============================================================

// MarginCalculator provides pure functions for margin arithmetic.
// It is stateless and safe for concurrent use.
type MarginCalculator struct {
	cfg MarginConfig
}

// NewMarginCalculator creates a calculator with the given config.
func NewMarginCalculator(cfg MarginConfig) *MarginCalculator {
	if cfg.DaysPerYear <= 0 {
		cfg.DaysPerYear = 365
	}
	if cfg.InitialMarginRate == 0 {
		cfg.InitialMarginRate = 0.5
	}
	if cfg.MaintenanceRatioFloor == 0 {
		cfg.MaintenanceRatioFloor = 1.3
	}
	if cfg.WarningRatio == 0 {
		cfg.WarningRatio = 1.5
	}
	return &MarginCalculator{cfg: cfg}
}

// RequiredMarginForBuy returns the initial margin required for a
// margin buy (融资买入). Formula: trade_value * InitialMarginRate.
//
// A-share regulation: 融资保证金比例 ≥ 50% (§2.4).
func (c *MarginCalculator) RequiredMarginForBuy(tradeValue float64) float64 {
	if tradeValue <= 0 {
		return 0
	}
	return tradeValue * c.cfg.InitialMarginRate
}

// RequiredMarginForShort returns the initial margin required for a
// short sell (融券卖出). Formula: trade_value * InitialMarginRate.
//
// The 100% stock value (the short sale proceeds) is automatically
// held as cash collateral by the broker; the investor only needs to
// post the additional InitialMarginRate portion.
//
// A-share regulation: 融券保证金比例 ≥ 50% (§2.5).
func (c *MarginCalculator) RequiredMarginForShort(tradeValue float64) float64 {
	if tradeValue <= 0 {
		return 0
	}
	return tradeValue * c.cfg.InitialMarginRate
}

// DailyFinancingInterest returns the daily interest accrued on a
// financing balance. Formula: balance * FinancingRate / DaysPerYear.
func (c *MarginCalculator) DailyFinancingInterest(financingBalance float64) float64 {
	if financingBalance <= 0 {
		return 0
	}
	return financingBalance * c.cfg.FinancingRate / float64(c.cfg.DaysPerYear)
}

// DailyLendingInterest returns the daily interest accrued on a
// securities lending balance. Formula: balance * SecuritiesLendingRate / DaysPerYear.
func (c *MarginCalculator) DailyLendingInterest(lendingBalance float64) float64 {
	if lendingBalance <= 0 {
		return 0
	}
	return lendingBalance * c.cfg.SecuritiesLendingRate / float64(c.cfg.DaysPerYear)
}

// AccruedFinancingInterest returns interest over N days.
func (c *MarginCalculator) AccruedFinancingInterest(financingBalance float64, days int) float64 {
	return c.DailyFinancingInterest(financingBalance) * float64(days)
}

// AccruedLendingInterest returns interest over N days.
func (c *MarginCalculator) AccruedLendingInterest(lendingBalance float64, days int) float64 {
	return c.DailyLendingInterest(lendingBalance) * float64(days)
}

// MaintenanceRatio computes 维持担保比例 = total_assets / total_debt.
// Returns +Inf when total_debt is 0 (no leverage, perfectly safe).
func (c *MarginCalculator) MaintenanceRatio(totalAssets, totalDebt float64) float64 {
	if totalDebt <= 0 {
		if totalAssets < 0 {
			return 0
		}
		return float64Inf()
	}
	if totalAssets <= 0 {
		return 0
	}
	return totalAssets / totalDebt
}

// IsForcedLiquidation reports whether the maintenance ratio is below
// the floor (130%), triggering forced liquidation.
func (c *MarginCalculator) IsForcedLiquidation(ratio float64) bool {
	return ratio < c.cfg.MaintenanceRatioFloor
}

// IsWarning reports whether the maintenance ratio is below the
// warning line (150%) but above the floor.
func (c *MarginCalculator) IsWarning(ratio float64) bool {
	return ratio >= c.cfg.MaintenanceRatioFloor && ratio < c.cfg.WarningRatio
}

// IsSafe reports whether the maintenance ratio is at or above the
// warning line.
func (c *MarginCalculator) IsSafe(ratio float64) bool {
	return ratio >= c.cfg.WarningRatio
}

// AvailableMargin computes the margin available for new positions.
//
// Formula (per requirement):
//
//	available = total_margin - used_margin - maintenance_margin
//
// Where:
//
//	total_margin      = total_assets (cash + position values + short proceeds)
//	used_margin       = sum(position_value * InitialMarginRate) for all open positions
//	maintenance_margin = total_debt * (1 - 1/MaintenanceRatioFloor)
//
// The maintenance_margin term represents the minimum equity buffer
// required to stay above the 130% floor. When available_margin <= 0,
// the account cannot open new positions.
func (c *MarginCalculator) AvailableMargin(totalAssets, totalDebt, usedMargin float64) float64 {
	maintenanceMargin := 0.0
	if totalDebt > 0 {
		maintenanceMargin = totalDebt * (1 - 1/c.cfg.MaintenanceRatioFloor)
	}
	return totalAssets - usedMargin - maintenanceMargin
}

// HasSufficientMargin reports whether the account has enough available
// margin to cover the required margin for a new trade.
func (c *MarginCalculator) HasSufficientMargin(availableMargin, requiredMargin float64) bool {
	return availableMargin >= requiredMargin
}

// float64Inf returns positive infinity as a float64.
func float64Inf() float64 {
	return math.Inf(1)
}
