// Package contracts is the LEAF package for pkg/backtest subpackages.
//
// S7-P2-1 (ODR-043): extracting shared DTOs + a narrow EngineRunner
// interface here lets the sibling subpackages (tracker/, state/,
// walkforward/, batch/, job/) depend on contracts (not on the parent
// pkg/backtest), which breaks the import cycle that would otherwise
// prevent leaf extraction.
//
// This is a LEAF package: it imports only pkg/domain (for
// PortfolioValue / Trade return types), pkg/fees (canonical fee-rate
// source), and the standard library. It does NOT import pkg/backtest
// or any sibling subpackage, so importing contracts never creates a
// reverse dependency.
//
// Aliasing convention: the parent package pkg/backtest re-exports
// every type and constant defined here via Go type/const aliases
// (`type X = contracts.X`), so all existing callers that use
// `backtest.BacktestRequest` / `backtest.TradingConfig` / etc. keep
// working unchanged.
package contracts

import (
	"context"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/fees"
)

// --- DTOs (moved from engine.go) ---

// TradingConfig holds A-share trading rules. Loaded from viper config
// under `backtest.trading`; falls back to DefaultTradingConfig() when
// StampTaxRate is zero.
type TradingConfig struct {
	StampTaxRate    float64          `mapstructure:"stamp_tax_rate"`
	MinCommission   float64          `mapstructure:"min_commission"`
	TransferFeeRate float64          `mapstructure:"transfer_fee_rate"`
	PriceLimit      PriceLimitConfig `mapstructure:"price_limit"`
	NewStockDays    int              `mapstructure:"new_stock_days"`
}

// PriceLimitConfig holds daily price-limit fractions by stock category.
//
// AUD-07 (ODR-065 H2): the board dimension (ChiNext/STAR 20%, BSE 30%)
// is NOT configured here — it is derived from the symbol via
// pkg/marketdata.ClassifySymbol, because it is market structure rather
// than a tunable. The fields below are only the categories that vary
// by stock status or by date.
type PriceLimitConfig struct {
	Normal float64 `mapstructure:"normal"`
	// ST is the MAIN-BOARD risk-warning limit (ST / *ST).
	//
	// 2026-07-06 起沪深主板 ST/*ST 由 ±5% 调整为 ±10%，与主板其他
	// 股票一致（沪深北三所 2026-04 修订交易规则）。所以「当前值」
	// 是 0.10，而 0.05 只适用于 2026-07-06 之前的交易日 —— 见
	// STBefore。
	//
	// 注意：创业板/科创板的风险警示股不受此调整影响，仍适用其板块
	// 档位（±20%）。该分支在 resolvePriceLimit 里，不看这个字段。
	ST float64 `mapstructure:"st"`
	// STBefore is the main-board risk-warning limit for trading days
	// before 2026-07-06 (±5%). Zero falls back to the historical
	// constant. Kept separate from ST so that a backtest spanning the
	// rule change prices each day correctly instead of applying one
	// rate to the whole window.
	STBefore float64 `mapstructure:"st_before"`
	// New is the limit applied to recently listed stocks. Known to be
	// inaccurate — see PriceLimitInput.TradeDays.
	New float64 `mapstructure:"new"`
}

// BacktestRequest represents the API request to start a backtest.
type BacktestRequest struct {
	Strategy       string   `json:"strategy" binding:"required"`
	StockPool      []string `json:"stock_pool"`
	IndexCode      string   `json:"index_code"`
	StartDate      string   `json:"start_date" binding:"required"`
	EndDate        string   `json:"end_date" binding:"required"`
	InitialCapital float64  `json:"initial_capital"`
	RiskFreeRate   float64  `json:"risk_free_rate"`
}

// BacktestResponse represents the API response for a backtest run.
type BacktestResponse struct {
	ID              string                  `json:"id"`
	Status          string                  `json:"status"`
	Strategy        string                  `json:"strategy,omitempty"`
	StrategyGitHash string                  `json:"strategy_git_hash,omitempty"`
	StartDate       string                  `json:"start_date,omitempty"`
	EndDate         string                  `json:"end_date,omitempty"`
	TotalReturn     float64                 `json:"total_return,omitempty"`
	AnnualReturn    float64                 `json:"annual_return,omitempty"`
	SharpeRatio     float64                 `json:"sharpe_ratio,omitempty"`
	SortinoRatio    float64                 `json:"sortino_ratio,omitempty"`
	MaxDrawdown     float64                 `json:"max_drawdown,omitempty"`
	MaxDrawdownDate string                  `json:"max_drawdown_date,omitempty"`
	WinRate         float64                 `json:"win_rate,omitempty"`
	TotalTrades     int                     `json:"total_trades,omitempty"`
	WinTrades       int                     `json:"win_trades,omitempty"`
	LoseTrades      int                     `json:"lose_trades,omitempty"`
	AvgHoldingDays  float64                 `json:"avg_holding_days,omitempty"`
	CalmarRatio     float64                 `json:"calmar_ratio,omitempty"`
	StartedAt       string                  `json:"started_at,omitempty"`
	CompletedAt     string                  `json:"completed_at,omitempty"`
	Error           string                  `json:"error,omitempty"`
	PortfolioValues []domain.PortfolioValue `json:"portfolio_values,omitempty"`
	Trades          []domain.Trade          `json:"trades,omitempty"`
	StockPool       []string                `json:"stock_pool,omitempty"`
	InitialCapital  float64                 `json:"initial_capital,omitempty"`
}

// --- Constants (moved from constants.go) ---
//
// A-Share trading-rule defaults. The first three are const-aliased to
// pkg/fees (the single source of truth) so a rate change in pkg/fees
// propagates here automatically — see contracts_test.go for the drift
// guard. The remaining literals (price-limit fractions, new-stock
// window, short-selling rate, trading-days convention) are A-share
// market rules with no upstream source.
const (
	// DefaultStampTaxRate is the stamp tax rate for selling A-shares
	// (0.05% since 2023-08-28; it was 0.1% before).
	DefaultStampTaxRate = fees.DefaultStampTaxRate

	// DefaultMinCommission is the minimum commission per transaction (¥5).
	DefaultMinCommission = fees.DefaultMinCommission

	// DefaultTransferFeeRate is the transfer fee rate (0.001%).
	DefaultTransferFeeRate = fees.DefaultTransferFeeRate

	// DefaultPriceLimitNormal is the daily price limit for main-board
	// stocks (±10%). ChiNext / STAR use ±20% and BSE ±30%, derived
	// from the symbol at decision time — not from this constant.
	DefaultPriceLimitNormal = 0.10

	// DefaultPriceLimitST is the MAIN-BOARD risk-warning limit (ST /
	// *ST) for trading days on or after 2026-07-06 (±10%).
	//
	// AUD-07 (ODR-065 H2): this was 0.05. 沪深北交易所 2026-04 修订
	// 交易规则，2026-07-06 起主板 ST/*ST 由 ±5% 上调至 ±10%，与主板
	// 其他股票一致。审计报告（2026-09-21）仍写作 ±5%，同样滞后于
	// 该变化。创业板/科创板的风险警示股不受影响，仍按板块 ±20%。
	DefaultPriceLimitST = 0.10

	// DefaultPriceLimitSTBefore is the main-board risk-warning limit
	// for trading days BEFORE 2026-07-06 (±5%). Kept as a separate
	// constant so a backtest spanning the rule change prices each day
	// under the rule in force on that day.
	DefaultPriceLimitSTBefore = 0.05

	// DefaultPriceLimitNew is the daily price limit for new stocks on listing day (±20% for ChiNext/STAR).
	DefaultPriceLimitNew = 0.20

	// DefaultNewStockDays is the number of days a stock is considered "new" after IPO.
	DefaultNewStockDays = 60

	// DefaultShortSellingRate is the annual securities lending rate
	// for short selling (10.6%, per VISION.md). Accrued daily on the
	// market value of open short positions in Tracker.AdvanceDay using
	// a 252-trading-day convention.
	DefaultShortSellingRate = 0.106

	// TradingDaysPerYear is the convention used to convert annual rates
	// (e.g. short-selling interest) to daily accruals in the backtest
	// tracker. The live margin module uses 365 (natural days); the
	// backtest engine advances one trading day at a time, so 252 is the
	// correct divisor here.
	TradingDaysPerYear = 252
)

// DefaultTradingConfig returns the default A-share trading rules.
//
// Renamed from the unexported `defaultTradingConfig` in engine.go so
// that cross-package callers (notably pkg/backtest/tracker) can reach
// it without going through the parent package. The parent package
// keeps a thin unexported wrapper of the same name so its two internal
// call sites (engine.go NewEngine / NewEngineWithOptions) stay
// unchanged.
func DefaultTradingConfig() TradingConfig {
	return TradingConfig{
		StampTaxRate:    DefaultStampTaxRate,
		MinCommission:   DefaultMinCommission,
		TransferFeeRate: DefaultTransferFeeRate,
		PriceLimit: PriceLimitConfig{
			Normal:   DefaultPriceLimitNormal,
			ST:       DefaultPriceLimitST,
			STBefore: DefaultPriceLimitSTBefore,
			New:      DefaultPriceLimitNew,
		},
		NewStockDays: DefaultNewStockDays,
	}
}

// --- Narrow interface (decouples job/walkforward/batch from *Engine) ---

// EngineRunner is the narrow contract for running a single backtest.
//
// *backtest.Engine implements this; the sibling subpackages
// (walkforward/, batch/, job/) accept this interface instead of the
// concrete *Engine to break the import cycle that would otherwise
// force them to import the parent pkg/backtest.
//
// The method signature matches (*Engine).RunBacktest exactly, so the
// concrete engine satisfies the interface with no adapter.
type EngineRunner interface {
	RunBacktest(ctx context.Context, req BacktestRequest) (*BacktestResponse, error)
}
