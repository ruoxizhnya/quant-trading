// Package live — 融资融券 + 做空 (MarginAccount + ShortableList) (P2-9).
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
// 设计目标:
//   - MarginAccount: 维护保证金余额、融资余额、融券余额、多空持仓,
//     支持融资买入 / 融券卖出 / 买券还券 / 卖券还款 四类操作。
//   - ShortableList: 线程安全的融券标的注册表, 支持查询标的可用性
//     及单券最大可融数量。
//   - MarginCalculator: 纯函数计算器, 计算初始保证金、日利息、维持
//     担保比例、强制平仓触发条件。
//   - 不在这里执行实际下单: MarginAccount 的操作仅更新内部账本,
//     实际委托由调用方 (LiveEngine) 转换为 Order 提交。
package live

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/rs/zerolog"
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
	InitialMarginRate      float64
	MaintenanceRatioFloor  float64
	WarningRatio           float64
	FinancingRate          float64
	SecuritiesLendingRate  float64
	DaysPerYear            int
	Now                    func() time.Time
}

// DefaultMarginConfig returns regulatory-recommended defaults.
//
// The defaults match the 《上海证券交易所融资融券交易实施细则》
// (2023 修订) §2.4-2.7 and 中国证券业协会 reference rates.
func DefaultMarginConfig() MarginConfig {
	return MarginConfig{
		InitialMarginRate:     0.5,
		MaintenanceRatioFloor:  1.3,
		WarningRatio:           1.5,
		FinancingRate:          0.06,
		SecuritiesLendingRate:  0.106,
		DaysPerYear:            365,
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
// 持仓数据结构
// ============================================================

// MarginLongPosition 描述融资买入的多头持仓。
//
//	Quantity:        持仓股数。
//	AvgCost:         加权平均成本 (元/股)。
//	FinancingAmount: 为本笔持仓借入的资金 (元)。
type MarginLongPosition struct {
	Symbol          string  `json:"symbol"`
	Quantity        float64 `json:"quantity"`
	AvgCost         float64 `json:"avg_cost"`
	FinancingAmount float64 `json:"financing_amount"`
}

// MarginShortPosition 描述融券卖出的空头持仓。
//
//	Quantity:      做空股数 (正数)。
//	SalePrice:     卖出价 (元/股)。
//	Proceeds:      卖出所得现金 (元), 由券商冻结作为担保物。
//	LendingAmount: 借券时的市值 (元), 用于计算融券利息。
type MarginShortPosition struct {
	Symbol        string  `json:"symbol"`
	Quantity      float64 `json:"quantity"`
	SalePrice     float64 `json:"sale_price"`
	Proceeds      float64 `json:"proceeds"`
	LendingAmount float64 `json:"lending_amount"`
}

// ============================================================
// 交易结果 / 风险状态
// ============================================================

// MarginOperation 描述一次融资融券操作的类型。
type MarginOperation string

const (
	OpMarginBuy    MarginOperation = "margin_buy"    // 融资买入
	OpShortSell    MarginOperation = "short_sell"    // 融券卖出
	OpBuyToCover   MarginOperation = "buy_to_cover"  // 买券还券
	OpMarginSell   MarginOperation = "margin_sell"   // 卖券还款
)

// MarginTradeResult 记录一次融资融券操作的结果。
type MarginTradeResult struct {
	TradeID          string          `json:"trade_id"`
	Operation        MarginOperation `json:"operation"`
	Symbol           string          `json:"symbol"`
	Quantity         float64         `json:"quantity"`
	Price            float64         `json:"price"`
	TradeValue       float64         `json:"trade_value"`
	FinancingDelta   float64         `json:"financing_delta"`
	LendingDelta     float64         `json:"lending_delta"`
	CashDelta        float64         `json:"cash_delta"`
	MarginRequired   float64         `json:"margin_required"`
	MaintenanceRatio float64         `json:"maintenance_ratio"` // 交易后担保比例
	Warning          bool            `json:"warning"`           // 交易后是否触发预警
	Timestamp       time.Time        `json:"timestamp"`
}

// MarginRiskStatus 描述账户当前的风险状态。
type MarginRiskStatus struct {
	TotalAssets       float64 `json:"total_assets"`
	TotalDebt         float64 `json:"total_debt"`
	NetAssets         float64 `json:"net_assets"`          // equity = total_assets - total_debt
	MaintenanceRatio  float64 `json:"maintenance_ratio"`
	AvailableMargin   float64 `json:"available_margin"`
	FinancingBalance  float64 `json:"financing_balance"`
	LendingBalance    float64 `json:"lending_balance"`     // 融券余额 (按当前市值)
	AccruedInterest   float64 `json:"accrued_interest"`
	Status            string  `json:"status"`               // "safe" / "warning" / "danger" / "forced_liquidation"
	ForcedLiquidation bool    `json:"forced_liquidation"`
}

// Risk status levels.
const (
	MarginStatusSafe             = "safe"               // ratio >= warning
	MarginStatusWarning          = "warning"            // floor <= ratio < warning
	MarginStatusDanger           = "danger"             // ratio < floor but not yet liquidated
	MarginStatusForcedLiquidation = "forced_liquidation" // ratio < floor, liquidation triggered
)

// ============================================================
// ShortableList — 融券标的注册表
// ============================================================

// ShortableEntry 描述一只可融券标的的限制。
type ShortableEntry struct {
	Symbol  string    `json:"symbol"`
	MaxQty  float64   `json:"max_qty"`   // 单券最大可融数量 (股); 0 = 无限制
	AddedAt time.Time `json:"added_at"`
}

// ShortableList 维护 symbol → ShortableEntry 映射, 支持线程安全
// 的查询 / 添加 / 删除。
//
// 内存数据, 不持久化: 融券标的名单由交易所每日公布, 启动时由外部
// (load-on-startup) 灌入即可。
type ShortableList struct {
	mu      sync.RWMutex
	entries map[string]ShortableEntry
}

// NewShortableList creates an empty shortable list.
func NewShortableList() *ShortableList {
	return &ShortableList{
		entries: make(map[string]ShortableEntry),
	}
}

// Add registers a symbol as shortable with the given max quantity.
// A maxQty of 0 means unlimited. If the symbol already exists, it is
// overwritten.
func (s *ShortableList) Add(symbol string, maxQty float64, addedAt time.Time) {
	if symbol == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[symbol] = ShortableEntry{
		Symbol:  symbol,
		MaxQty:  maxQty,
		AddedAt: addedAt,
	}
}

// Remove removes a symbol from the shortable list. No-op if not present.
func (s *ShortableList) Remove(symbol string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, symbol)
}

// IsShortable reports whether the symbol is registered as shortable.
func (s *ShortableList) IsShortable(symbol string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.entries[symbol]
	return ok
}

// Entry returns the shortable entry for a symbol. Returns false if
// the symbol is not registered.
func (s *ShortableList) Entry(symbol string) (ShortableEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[symbol]
	return e, ok
}

// MaxShortableQty returns the maximum shortable quantity for a symbol.
// Returns (-1, false) if the symbol is not shortable. Returns (0, true)
// if shortable with no limit (MaxQty == 0).
func (s *ShortableList) MaxShortableQty(symbol string) (float64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[symbol]
	if !ok {
		return -1, false
	}
	return e.MaxQty, true
}

// All returns a snapshot of all shortable entries sorted by symbol.
func (s *ShortableList) All() []ShortableEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ShortableEntry, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, e)
	}
	return out
}

// Count returns the number of registered shortable symbols.
func (s *ShortableList) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
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
//   available = total_margin - used_margin - maintenance_margin
//
// Where:
//   total_margin      = total_assets (cash + position values + short proceeds)
//   used_margin       = sum(position_value * InitialMarginRate) for all open positions
//   maintenance_margin = total_debt * (1 - 1/MaintenanceRatioFloor)
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

// ============================================================
// MarginAccount — 融资融券账户
// ============================================================

// MarginAccount 维护一个融资融券信用账户的全部状态: 现金、融资余额、
// 融券余额、多空持仓、累计利息。所有公开方法都是线程安全的。
//
// 账户模型:
//   - cash:              可用现金 (含融券卖出冻结的担保金)
//   - financingBalance:  融资余额 (借入资金总额)
//   - longPositions:     融资买入的多头持仓
//   - shortPositions:    融券卖出的空头持仓
//   - accruedFinancingInterest: 累计融资利息
//   - accruedLendingInterest:   累计融券利息
//
// 资产/负债计算 (需要当前行情):
//   - total_assets = cash + Σ(long.qty × price) + Σ(short.proceeds)
//   - total_debt   = financingBalance + Σ(short.qty × price) + accruedInterest
//   - maintenance_ratio = total_assets / total_debt
type MarginAccount struct {
	mu                       sync.RWMutex
	accountID                string
	cash                     float64
	financingBalance         float64
	longPositions            map[string]*MarginLongPosition
	shortPositions           map[string]*MarginShortPosition
	accruedFinancingInterest float64
	accruedLendingInterest   float64
	cfg                      MarginConfig
	calc                     *MarginCalculator
	shortable                *ShortableList
	logger                   zerolog.Logger
	tradeSeq                 int64
}

// NewMarginAccount creates a new margin account with the given initial
// cash deposit and configuration.
func NewMarginAccount(accountID string, initialCash float64, cfg MarginConfig, shortable *ShortableList, logger zerolog.Logger) (*MarginAccount, error) {
	if accountID == "" {
		return nil, errors.New("account_id is required")
	}
	if initialCash < 0 {
		return nil, fmt.Errorf("initial_cash must be >= 0, got %f", initialCash)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid margin config: %w", err)
	}
	if shortable == nil {
		shortable = NewShortableList()
	}
	calc := NewMarginCalculator(cfg)
	return &MarginAccount{
		accountID:      accountID,
		cash:           initialCash,
		longPositions:  make(map[string]*MarginLongPosition),
		shortPositions: make(map[string]*MarginShortPosition),
		cfg:            cfg,
		calc:           calc,
		shortable:      shortable,
		logger:         logger.With().Str("component", "margin_account").Str("account_id", accountID).Logger(),
	}, nil
}

// AccountID returns the account identifier.
func (a *MarginAccount) AccountID() string {
	return a.accountID
}

// Cash returns the current cash balance.
func (a *MarginAccount) Cash() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cash
}

// FinancingBalance returns the total borrowed money (融资余额).
func (a *MarginAccount) FinancingBalance() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.financingBalance
}

// AccruedInterest returns total accrued interest (financing + lending).
func (a *MarginAccount) AccruedInterest() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.accruedFinancingInterest + a.accruedLendingInterest
}

// LongPositions returns a snapshot of all long positions.
func (a *MarginAccount) LongPositions() []MarginLongPosition {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]MarginLongPosition, 0, len(a.longPositions))
	for _, p := range a.longPositions {
		out = append(out, *p)
	}
	return out
}

// ShortPositions returns a snapshot of all short positions.
func (a *MarginAccount) ShortPositions() []MarginShortPosition {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]MarginShortPosition, 0, len(a.shortPositions))
	for _, p := range a.shortPositions {
		out = append(out, *p)
	}
	return out
}

// GetLongPosition returns the long position for a symbol.
func (a *MarginAccount) GetLongPosition(symbol string) (MarginLongPosition, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	p, ok := a.longPositions[symbol]
	if !ok {
		return MarginLongPosition{}, false
	}
	return *p, true
}

// GetShortPosition returns the short position for a symbol.
func (a *MarginAccount) GetShortPosition(symbol string) (MarginShortPosition, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	p, ok := a.shortPositions[symbol]
	if !ok {
		return MarginShortPosition{}, false
	}
	return *p, true
}

// ============================================================
// 资产 / 负债 / 担保比例计算
// ============================================================

// TotalAssets computes total assets given current prices.
//
//	total_assets = cash + Σ(long.qty × price) + Σ(short.proceeds)
//
// short.proceeds are already included in cash (the sale proceeds are
// held as cash collateral), so they are NOT double-counted. The formula
// simplifies to: cash + Σ(long.qty × price).
//
// For symbols without a price in the map, the position's AvgCost
// (long) or SalePrice (short) is used as fallback.
func (a *MarginAccount) TotalAssets(prices map[string]float64) float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.totalAssetsLocked(prices)
}

func (a *MarginAccount) totalAssetsLocked(prices map[string]float64) float64 {
	total := a.cash
	for sym, p := range a.longPositions {
		price := prices[sym]
		if price <= 0 {
			price = p.AvgCost
		}
		total += p.Quantity * price
	}
	// Short proceeds are already in cash; do not add again.
	return total
}

// TotalDebt computes total debt given current prices.
//
//	total_debt = financingBalance + Σ(short.qty × price) + accruedInterest
//
// For short positions, the debt is the current market value of the
// borrowed stock (which must be returned). For symbols without a price,
// the short position's SalePrice is used as fallback.
func (a *MarginAccount) TotalDebt(prices map[string]float64) float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.totalDebtLocked(prices)
}

func (a *MarginAccount) totalDebtLocked(prices map[string]float64) float64 {
	total := a.financingBalance
	for sym, p := range a.shortPositions {
		price := prices[sym]
		if price <= 0 {
			price = p.SalePrice
		}
		total += p.Quantity * price
	}
	total += a.accruedFinancingInterest + a.accruedLendingInterest
	return total
}

// LendingBalance returns the current market value of borrowed stocks
// (融券余额), using the provided prices. Falls back to SalePrice.
func (a *MarginAccount) LendingBalance(prices map[string]float64) float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	total := 0.0
	for sym, p := range a.shortPositions {
		price := prices[sym]
		if price <= 0 {
			price = p.SalePrice
		}
		total += p.Quantity * price
	}
	return total
}

// UsedMargin computes the total initial margin committed to open positions.
//
//	used_margin = Σ(long.value × InitialMarginRate) + Σ(short.value × InitialMarginRate)
func (a *MarginAccount) UsedMargin(prices map[string]float64) float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	rate := a.cfg.InitialMarginRate
	total := 0.0
	for sym, p := range a.longPositions {
		price := prices[sym]
		if price <= 0 {
			price = p.AvgCost
		}
		total += p.Quantity * price * rate
	}
	for sym, p := range a.shortPositions {
		price := prices[sym]
		if price <= 0 {
			price = p.SalePrice
		}
		total += p.Quantity * price * rate
	}
	return total
}

// MaintenanceRatio computes the current 维持担保比例.
func (a *MarginAccount) MaintenanceRatio(prices map[string]float64) float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	assets := a.totalAssetsLocked(prices)
	debt := a.totalDebtLocked(prices)
	return a.calc.MaintenanceRatio(assets, debt)
}

// AvailableMargin computes the margin available for new positions.
func (a *MarginAccount) AvailableMargin(prices map[string]float64) float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	assets := a.totalAssetsLocked(prices)
	debt := a.totalDebtLocked(prices)
	used := a.usedMarginLocked(prices)
	return a.calc.AvailableMargin(assets, debt, used)
}

func (a *MarginAccount) usedMarginLocked(prices map[string]float64) float64 {
	rate := a.cfg.InitialMarginRate
	total := 0.0
	for sym, p := range a.longPositions {
		price := prices[sym]
		if price <= 0 {
			price = p.AvgCost
		}
		total += p.Quantity * price * rate
	}
	for sym, p := range a.shortPositions {
		price := prices[sym]
		if price <= 0 {
			price = p.SalePrice
		}
		total += p.Quantity * price * rate
	}
	return total
}

// RiskStatus returns the current risk status of the account.
func (a *MarginAccount) RiskStatus(prices map[string]float64) MarginRiskStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()

	assets := a.totalAssetsLocked(prices)
	debt := a.totalDebtLocked(prices)
	used := a.usedMarginLocked(prices)
	ratio := a.calc.MaintenanceRatio(assets, debt)
	available := a.calc.AvailableMargin(assets, debt, used)
	lendingBal := 0.0
	for sym, p := range a.shortPositions {
		price := prices[sym]
		if price <= 0 {
			price = p.SalePrice
		}
		lendingBal += p.Quantity * price
	}

	status := MarginStatusSafe
	forced := false
	switch {
	case ratio < a.cfg.MaintenanceRatioFloor:
		status = MarginStatusForcedLiquidation
		forced = true
	case ratio < a.cfg.WarningRatio:
		status = MarginStatusWarning
	}

	return MarginRiskStatus{
		TotalAssets:       assets,
		TotalDebt:         debt,
		NetAssets:        assets - debt,
		MaintenanceRatio: ratio,
		AvailableMargin:  available,
		FinancingBalance: a.financingBalance,
		LendingBalance:   lendingBal,
		AccruedInterest:  a.accruedFinancingInterest + a.accruedLendingInterest,
		Status:           status,
		ForcedLiquidation: forced,
	}
}

// ============================================================
// 交易操作
// ============================================================

// MarginBuy executes a 融资买入 (buy with borrowed money).
//
// The full trade value is borrowed from the broker; the stock becomes
// collateral. The investor's cash is NOT consumed (it serves as
// existing collateral). The initial margin check ensures the account
// has enough available margin to support the new position.
//
// Steps:
//  1. trade_value = qty × price
//  2. required_margin = trade_value × InitialMarginRate
//  3. Check available_margin >= required_margin
//  4. financing_balance += trade_value
//  5. Update long_position (weighted average cost)
//  6. Check post-trade maintenance_ratio >= floor
func (a *MarginAccount) MarginBuy(ctx context.Context, symbol string, qty, price float64) (*MarginTradeResult, error) {
	if err := a.validateTrade(symbol, qty, price); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	tradeValue := qty * price
	requiredMargin := a.calc.RequiredMarginForBuy(tradeValue)

	// Pre-trade available margin check.
	prices := map[string]float64{symbol: price}
	available := a.calc.AvailableMargin(
		a.totalAssetsLocked(prices),
		a.totalDebtLocked(prices),
		a.usedMarginLocked(prices),
	)
	if !a.calc.HasSufficientMargin(available, requiredMargin) {
		a.logger.Warn().
			Str("symbol", symbol).
			Float64("trade_value", tradeValue).
			Float64("required_margin", requiredMargin).
			Float64("available_margin", available).
			Msg("margin buy rejected: insufficient margin")
		return nil, fmt.Errorf("insufficient margin: required %.2f, available %.2f",
			requiredMargin, available)
	}

	// Execute: borrow full trade value, add to long position.
	a.financingBalance += tradeValue
	a.addToLongPosition(symbol, qty, price, tradeValue)

	// Post-trade maintenance ratio check.
	postAssets := a.totalAssetsLocked(prices)
	postDebt := a.totalDebtLocked(prices)
	postRatio := a.calc.MaintenanceRatio(postAssets, postDebt)
	warning := postRatio < a.cfg.WarningRatio

	result := &MarginTradeResult{
		TradeID:          a.nextTradeID(),
		Operation:        OpMarginBuy,
		Symbol:           symbol,
		Quantity:         qty,
		Price:            price,
		TradeValue:       tradeValue,
		FinancingDelta:   tradeValue,
		CashDelta:        0,
		MarginRequired:   requiredMargin,
		MaintenanceRatio: postRatio,
		Warning:          warning,
		Timestamp:        a.now(),
	}

	a.logger.Info().
		Str("trade_id", result.TradeID).
		Str("symbol", symbol).
		Float64("qty", qty).
		Float64("price", price).
		Float64("financing_delta", tradeValue).
		Float64("post_ratio", postRatio).
		Bool("warning", warning).
		Msg("margin buy executed")

	return result, nil
}

// ShortSell executes a 融券卖出 (sell borrowed stock).
//
// The investor borrows stock from the broker and sells it. The sale
// proceeds are held as cash collateral. The investor must post
// additional initial margin (50% of trade value).
//
// Steps:
//  1. Check symbol is in ShortableList and qty ≤ max_shortable
//  2. trade_value = qty × price
//  3. required_margin = trade_value × InitialMarginRate
//  4. Check available_margin >= required_margin
//  5. cash += trade_value (proceeds held)
//  6. Update short_position
//  7. Check post-trade maintenance_ratio >= floor
func (a *MarginAccount) ShortSell(ctx context.Context, symbol string, qty, price float64) (*MarginTradeResult, error) {
	if err := a.validateTrade(symbol, qty, price); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Shortable check (read-only, outside account lock).
	maxQty, shortable := a.shortable.MaxShortableQty(symbol)
	if !shortable {
		return nil, fmt.Errorf("symbol %s is not shortable", symbol)
	}
	if maxQty > 0 {
		existing := 0.0
		a.mu.RLock()
		if p, ok := a.shortPositions[symbol]; ok {
			existing = p.Quantity
		}
		a.mu.RUnlock()
		if existing+qty > maxQty {
			return nil, fmt.Errorf("short qty %f exceeds max shortable %f (already short %f)",
				qty, maxQty, existing)
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	tradeValue := qty * price
	requiredMargin := a.calc.RequiredMarginForShort(tradeValue)

	// Pre-trade available margin check.
	prices := map[string]float64{symbol: price}
	available := a.calc.AvailableMargin(
		a.totalAssetsLocked(prices),
		a.totalDebtLocked(prices),
		a.usedMarginLocked(prices),
	)
	if !a.calc.HasSufficientMargin(available, requiredMargin) {
		a.logger.Warn().
			Str("symbol", symbol).
			Float64("trade_value", tradeValue).
			Float64("required_margin", requiredMargin).
			Float64("available_margin", available).
			Msg("short sell rejected: insufficient margin")
		return nil, fmt.Errorf("insufficient margin: required %.2f, available %.2f",
			requiredMargin, available)
	}

	// Execute: receive proceeds as cash, add to short position.
	a.cash += tradeValue
	a.addToShortPosition(symbol, qty, price, tradeValue)

	// Post-trade maintenance ratio check.
	postAssets := a.totalAssetsLocked(prices)
	postDebt := a.totalDebtLocked(prices)
	postRatio := a.calc.MaintenanceRatio(postAssets, postDebt)
	warning := postRatio < a.cfg.WarningRatio

	result := &MarginTradeResult{
		TradeID:          a.nextTradeID(),
		Operation:        OpShortSell,
		Symbol:           symbol,
		Quantity:         qty,
		Price:            price,
		TradeValue:       tradeValue,
		LendingDelta:     tradeValue,
		CashDelta:        tradeValue,
		MarginRequired:   requiredMargin,
		MaintenanceRatio: postRatio,
		Warning:          warning,
		Timestamp:        a.now(),
	}

	a.logger.Info().
		Str("trade_id", result.TradeID).
		Str("symbol", symbol).
		Float64("qty", qty).
		Float64("price", price).
		Float64("lending_delta", tradeValue).
		Float64("cash_delta", tradeValue).
		Float64("post_ratio", postRatio).
		Bool("warning", warning).
		Msg("short sell executed")

	return result, nil
}

// BuyToCover executes a 买券还券 (buy back to cover short position).
//
// The investor buys back the borrowed stock to return it to the broker.
// This reduces the short position and the securities lending debt.
//
// Steps:
//  1. Check short_position exists and qty ≤ short_quantity
//  2. trade_value = qty × price
//  3. cash -= trade_value (pay to buy back)
//  4. Reduce short_position (FIFO on lending amount)
//  5. Check post-trade maintenance_ratio
func (a *MarginAccount) BuyToCover(ctx context.Context, symbol string, qty, price float64) (*MarginTradeResult, error) {
	if err := a.validateTrade(symbol, qty, price); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	shortPos, ok := a.shortPositions[symbol]
	if !ok {
		return nil, fmt.Errorf("no short position for %s", symbol)
	}
	if shortPos.Quantity < qty {
		return nil, fmt.Errorf("insufficient short position: have %.0f, want to cover %.0f",
			shortPos.Quantity, qty)
	}

	tradeValue := qty * price

	// Execute: pay cash to buy back, reduce short position.
	a.cash -= tradeValue
	// Reduce lending amount proportionally.
	if shortPos.Quantity > 0 {
		ratio := qty / shortPos.Quantity
		lendingReduced := shortPos.LendingAmount * ratio
		shortPos.LendingAmount -= lendingReduced
		shortPos.Proceeds -= shortPos.Proceeds * ratio
	}
	shortPos.Quantity -= qty
	shortPos.SalePrice = 0 // no longer a single-price short after partial cover
	if shortPos.Quantity <= 0 {
		delete(a.shortPositions, symbol)
	}

	// Post-trade maintenance ratio check.
	prices := map[string]float64{symbol: price}
	postAssets := a.totalAssetsLocked(prices)
	postDebt := a.totalDebtLocked(prices)
	postRatio := a.calc.MaintenanceRatio(postAssets, postDebt)
	warning := postRatio < a.cfg.WarningRatio

	result := &MarginTradeResult{
		TradeID:          a.nextTradeID(),
		Operation:        OpBuyToCover,
		Symbol:           symbol,
		Quantity:         qty,
		Price:            price,
		TradeValue:       tradeValue,
		LendingDelta:     -tradeValue,
		CashDelta:        -tradeValue,
		MaintenanceRatio: postRatio,
		Warning:          warning,
		Timestamp:        a.now(),
	}

	a.logger.Info().
		Str("trade_id", result.TradeID).
		Str("symbol", symbol).
		Float64("qty", qty).
		Float64("price", price).
		Float64("cash_delta", -tradeValue).
		Float64("post_ratio", postRatio).
		Msg("buy to cover executed")

	return result, nil
}

// MarginSell executes a 卖券还款 (sell long position to repay financing).
//
// The investor sells part of their long position. The proceeds are used
// to repay the financing balance. The realized P&L is the difference
// between the sale price and the average cost.
//
// Steps:
//  1. Check long_position exists and qty ≤ long_quantity
//  2. trade_value = qty × price
//  3. cash += trade_value (proceeds)
//  4. Repay financing: financing_balance -= min(financing_for_qty, trade_value)
//  5. Reduce long_position
//  6. Check post-trade maintenance_ratio
func (a *MarginAccount) MarginSell(ctx context.Context, symbol string, qty, price float64) (*MarginTradeResult, error) {
	if err := a.validateTrade(symbol, qty, price); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	longPos, ok := a.longPositions[symbol]
	if !ok {
		return nil, fmt.Errorf("no long position for %s", symbol)
	}
	if longPos.Quantity < qty {
		return nil, fmt.Errorf("insufficient long position: have %.0f, want to sell %.0f",
			longPos.Quantity, qty)
	}

	tradeValue := qty * price

	// Execute: receive proceeds, repay financing proportionally.
	a.cash += tradeValue
	if longPos.Quantity > 0 {
		ratio := qty / longPos.Quantity
		financingForQty := longPos.FinancingAmount * ratio
		// Repay the lesser of financing_for_qty or trade_value.
		repay := financingForQty
		if tradeValue < repay {
			repay = tradeValue
		}
		a.financingBalance -= repay
		longPos.FinancingAmount -= financingForQty
	}
	// Reduce long position (weighted average cost unchanged).
	longPos.Quantity -= qty
	if longPos.Quantity <= 0 {
		delete(a.longPositions, symbol)
	}

	// Post-trade maintenance ratio check.
	prices := map[string]float64{symbol: price}
	postAssets := a.totalAssetsLocked(prices)
	postDebt := a.totalDebtLocked(prices)
	postRatio := a.calc.MaintenanceRatio(postAssets, postDebt)
	warning := postRatio < a.cfg.WarningRatio

	result := &MarginTradeResult{
		TradeID:          a.nextTradeID(),
		Operation:        OpMarginSell,
		Symbol:           symbol,
		Quantity:         qty,
		Price:            price,
		TradeValue:       tradeValue,
		FinancingDelta:   -tradeValue,
		CashDelta:        tradeValue,
		MaintenanceRatio: postRatio,
		Warning:          warning,
		Timestamp:        a.now(),
	}

	a.logger.Info().
		Str("trade_id", result.TradeID).
		Str("symbol", symbol).
		Float64("qty", qty).
		Float64("price", price).
		Float64("cash_delta", tradeValue).
		Float64("post_ratio", postRatio).
		Msg("margin sell executed")

	return result, nil
}

// ============================================================
// 利息计提
// ============================================================

// AccrueInterest accrues financing and lending interest for the given
// number of days. Interest is computed on the current financing balance
// and the current market value of borrowed stocks (using the provided
// prices for short positions, falling back to SalePrice).
//
// This is typically called once per trading day at market close.
func (a *MarginAccount) AccrueInterest(ctx context.Context, days int, prices map[string]float64) (financingInterest, lendingInterest float64, err error) {
	if days < 0 {
		return 0, 0, fmt.Errorf("days must be >= 0, got %d", days)
	}
	if days == 0 {
		return 0, 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	financingInterest = a.calc.AccruedFinancingInterest(a.financingBalance, days)
	a.accruedFinancingInterest += financingInterest

	lendingBalance := 0.0
	for sym, p := range a.shortPositions {
		price := prices[sym]
		if price <= 0 {
			price = p.SalePrice
		}
		lendingBalance += p.Quantity * price
	}
	lendingInterest = a.calc.AccruedLendingInterest(lendingBalance, days)
	a.accruedLendingInterest += lendingInterest

	a.logger.Info().
		Int("days", days).
		Float64("financing_interest", financingInterest).
		Float64("lending_interest", lendingInterest).
		Float64("financing_balance", a.financingBalance).
		Float64("lending_balance", lendingBalance).
		Msg("interest accrued")

	return financingInterest, lendingInterest, nil
}

// ============================================================
// 强制平仓
// ============================================================

// ForceLiquidate closes all positions when the maintenance ratio falls
// below the floor (130%). It sells all long positions at the given prices
// and buys back all short positions at the given prices.
//
// Returns a summary of the liquidation. This is a terminal operation
// that zeroes out all positions and repays all debt.
func (a *MarginAccount) ForceLiquidate(ctx context.Context, prices map[string]float64, reason string) (*ForceLiquidateResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	now := a.now()
	result := &ForceLiquidateResult{
		Reason:      reason,
		StartedAt:   now,
	}

	// Sell all long positions.
	for sym, p := range a.longPositions {
		price := prices[sym]
		if price <= 0 {
			price = p.AvgCost
		}
		tradeValue := p.Quantity * price
		a.cash += tradeValue
		a.financingBalance -= p.FinancingAmount
		if a.financingBalance < 0 {
			a.financingBalance = 0
		}
		result.LongSold = append(result.LongSold, ForceLiquidateEntry{
			Symbol:     sym,
			Quantity:   p.Quantity,
			Price:      price,
			TradeValue: tradeValue,
		})
		delete(a.longPositions, sym)
	}

	// Buy back all short positions.
	for sym, p := range a.shortPositions {
		price := prices[sym]
		if price <= 0 {
			price = p.SalePrice
		}
		tradeValue := p.Quantity * price
		a.cash -= tradeValue
		result.ShortCovered = append(result.ShortCovered, ForceLiquidateEntry{
			Symbol:     sym,
			Quantity:   p.Quantity,
			Price:      price,
			TradeValue: tradeValue,
		})
		delete(a.shortPositions, sym)
	}

	// Clear accrued interest (it's now part of the settled debt).
	a.accruedFinancingInterest = 0
	a.accruedLendingInterest = 0

	result.CompletedAt = a.now()
	result.FinalCash = a.cash

	a.logger.Warn().
		Str("reason", reason).
		Int("long_sold", len(result.LongSold)).
		Int("short_covered", len(result.ShortCovered)).
		Float64("final_cash", a.cash).
		Msg("forced liquidation executed")

	return result, nil
}

// ForceLiquidateEntry describes a single position closed during forced
// liquidation.
type ForceLiquidateEntry struct {
	Symbol     string  `json:"symbol"`
	Quantity   float64 `json:"quantity"`
	Price      float64 `json:"price"`
	TradeValue float64 `json:"trade_value"`
}

// ForceLiquidateResult reports the outcome of a ForceLiquidate call.
type ForceLiquidateResult struct {
	LongSold      []ForceLiquidateEntry `json:"long_sold"`
	ShortCovered  []ForceLiquidateEntry `json:"short_covered"`
	FinalCash     float64               `json:"final_cash"`
	StartedAt     time.Time             `json:"started_at"`
	CompletedAt   time.Time             `json:"completed_at"`
	Reason        string                `json:"reason"`
}

// ============================================================
// 存款 / 取款
// ============================================================

// Deposit adds cash to the margin account (increases collateral).
func (a *MarginAccount) Deposit(ctx context.Context, amount float64) error {
	if amount <= 0 {
		return fmt.Errorf("deposit amount must be > 0, got %f", amount)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cash += amount
	a.logger.Info().Float64("amount", amount).Float64("cash", a.cash).Msg("deposit")
	return nil
}

// Withdraw removes cash from the margin account. Fails if the withdrawal
// would breach the maintenance ratio floor.
func (a *MarginAccount) Withdraw(ctx context.Context, amount float64, prices map[string]float64) error {
	if amount <= 0 {
		return fmt.Errorf("withdraw amount must be > 0, got %f", amount)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.cash < amount {
		return fmt.Errorf("insufficient cash: have %.2f, want %.2f", a.cash, amount)
	}

	// Simulate withdrawal and check maintenance ratio.
	oldCash := a.cash
	a.cash -= amount
	ratio := a.calc.MaintenanceRatio(
		a.totalAssetsLocked(prices),
		a.totalDebtLocked(prices),
	)
	if ratio < a.cfg.MaintenanceRatioFloor {
		a.cash = oldCash // rollback
		return fmt.Errorf("withdrawal would breach maintenance ratio: post-ratio %.4f < floor %.4f",
			ratio, a.cfg.MaintenanceRatioFloor)
	}

	a.logger.Info().Float64("amount", amount).Float64("cash", a.cash).Msg("withdraw")
	return nil
}

// ============================================================
// 内部辅助
// ============================================================

func (a *MarginAccount) validateTrade(symbol string, qty, price float64) error {
	if symbol == "" {
		return errors.New("symbol is required")
	}
	if qty <= 0 {
		return fmt.Errorf("quantity must be > 0, got %f", qty)
	}
	if price <= 0 {
		return fmt.Errorf("price must be > 0, got %f", price)
	}
	return nil
}

func (a *MarginAccount) addToLongPosition(symbol string, qty, price, financingAmount float64) {
	pos, ok := a.longPositions[symbol]
	if !ok {
		a.longPositions[symbol] = &MarginLongPosition{
			Symbol:          symbol,
			Quantity:        qty,
			AvgCost:         price,
			FinancingAmount: financingAmount,
		}
		return
	}
	totalQty := pos.Quantity + qty
	totalCost := pos.Quantity*pos.AvgCost + qty*price
	pos.AvgCost = totalCost / totalQty
	pos.Quantity = totalQty
	pos.FinancingAmount += financingAmount
}

func (a *MarginAccount) addToShortPosition(symbol string, qty, price, lendingAmount float64) {
	pos, ok := a.shortPositions[symbol]
	if !ok {
		a.shortPositions[symbol] = &MarginShortPosition{
			Symbol:        symbol,
			Quantity:      qty,
			SalePrice:     price,
			Proceeds:      qty * price,
			LendingAmount: lendingAmount,
		}
		return
	}
	// Weighted average sale price.
	totalQty := pos.Quantity + qty
	totalProceeds := pos.Proceeds + qty*price
	pos.SalePrice = totalProceeds / totalQty
	pos.Proceeds = totalProceeds
	pos.Quantity = totalQty
	pos.LendingAmount += lendingAmount
}

func (a *MarginAccount) now() time.Time {
	if a.cfg.Now != nil {
		return a.cfg.Now()
	}
	return time.Now().UTC()
}

func (a *MarginAccount) nextTradeID() string {
	a.tradeSeq++
	return fmt.Sprintf("MAR-%s-%d", a.accountID, a.tradeSeq)
}

// float64Inf returns positive infinity as a float64.
func float64Inf() float64 {
	return math.Inf(1)
}
