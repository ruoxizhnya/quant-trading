// Package domain contains core domain types and interfaces for the quant trading system.
// All types are market-agnostic and can be used across different markets.
package domain

import (
	"context"
	"time"
)

// Direction represents trading direction
type Direction string

const (
	DirectionLong  Direction = "long"
	DirectionShort Direction = "short"
	DirectionClose Direction = "close"
	DirectionHold  Direction = "hold"
)

// OHLCV represents daily candlestick data
type OHLCV struct {
	Symbol    string    `json:"symbol"`
	Date      time.Time `json:"date"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	Volume    float64   `json:"volume"`
	Turnover  float64   `json:"turnover"`
	TradeDays int       `json:"trade_days"`
	// LimitUp indicates the stock hit the upper price limit today (涨停)
	LimitUp bool `json:"limit_up"`
	// LimitDown indicates the stock hit the lower price limit today (跌停)
	LimitDown bool `json:"limit_down"`
}

// Stock represents a tradable security
type Stock struct {
	Symbol    string    `json:"symbol"`
	Name      string    `json:"name"`
	Exchange  string    `json:"exchange"`
	Industry  string    `json:"industry"`
	MarketCap float64   `json:"market_cap"`
	ListDate  time.Time `json:"list_date"`
	Status    string    `json:"status"` // active, suspended, delisted
}

// IndexConstituent represents a constituent stock of an index.
type IndexConstituent struct {
	ID         int64     `json:"id"`
	IndexCode  string    `json:"index_code"`  // e.g. "000300.SH" (CSI 300), "000500.SH" (CSI 500), "000852.SH" (CSI 800)
	Symbol     string    `json:"symbol"`      // stock ts_code, e.g. "000001.SZ"
	InDate     time.Time `json:"in_date"`     // date when stock entered index
	OutDate    time.Time `json:"out_date"`    // date when stock exited index (zero if still in)
	Weight     float64   `json:"weight"`      // weight in index (if available from index_weight API)
}

// Dividend represents a dividend event for a stock.
type Dividend struct {
	ID        int64     `json:"id"`
	Symbol    string    `json:"symbol"`
	AnnDate   time.Time `json:"ann_date"`    // announcement date
	RecDate   time.Time `json:"rec_date"`    // record date (shareholders as of this date receive dividend)
	PayDate   time.Time `json:"pay_date"`    // payment date / ex-dividend date
	DivAmt    float64   `json:"div_amt"`     // cash dividend amount per share
	StkDiv    float64   `json:"stk_div"`     // stock dividend per share (bonus shares)
	StkRatio  float64   `json:"stk_ratio"`   // stock split ratio
	CashRatio float64   `json:"cash_ratio"`  // cash dividend ratio
}

// Fundamental represents fundamental financial data
type Fundamental struct {
	Symbol        string    `json:"symbol"`
	Date         time.Time `json:"date"`
	PE           float64   `json:"pe"`
	PB           float64   `json:"pb"`
	PS           float64   `json:"ps"`
	ROE          float64   `json:"roe"`
	ROA          float64   `json:"roa"`
	DebtToEquity float64   `json:"debt_to_equity"`
	GrossMargin  float64   `json:"gross_margin"`
	NetMargin    float64   `json:"net_margin"`
	Revenue      float64   `json:"revenue"`
	NetProfit    float64   `json:"net_profit"`
	TotalAssets  float64   `json:"total_assets"`
	TotalLiab    float64   `json:"total_liab"`
}

// FundamentalData represents financial data from Tushare financial_data API.
// Used for factor-based screening (PE, PB, PS, ROE, ROA, etc.).
type FundamentalData struct {
	ID             int       `json:"id"`
	TsCode         string    `json:"ts_code"`
	TradeDate      time.Time `json:"trade_date"`
	AnnDate        time.Time `json:"ann_date"` // announcement date
	EndDate        time.Time `json:"end_date"`  // reporting period
	PE             *float64  `json:"pe"`
	PB             *float64  `json:"pb"`
	PS             *float64  `json:"ps"`
	ROE            *float64  `json:"roe"`
	ROA            *float64  `json:"roa"`
	DebtToEquity   *float64  `json:"debt_to_equity"`
	GrossMargin    *float64  `json:"gross_margin"`
	NetMargin      *float64  `json:"net_margin"`
	Revenue        *float64  `json:"revenue"`
	NetProfit      *float64  `json:"net_profit"`
	TotalAssets    *float64  `json:"total_assets"`
	TotalLiab      *float64  `json:"total_liab"`
	CreatedAt      time.Time `json:"created_at"`
}

// FactorType represents a factor name
type FactorType string

const (
	FactorMomentum FactorType = "momentum" // 20-day return
	FactorValue    FactorType = "value"    // EP, BP, SP
	FactorQuality  FactorType = "quality"   // ROE, ROA, gross_margin
	FactorSize     FactorType = "size"     // market cap rank
)

// FactorCacheEntry is a pre-computed factor z-score for a stock on a date
type FactorCacheEntry struct {
	ID         int64      `json:"id"`
	Symbol     string     `json:"symbol"`
	TradeDate  time.Time  `json:"trade_date"`
	FactorName FactorType `json:"factor_name"`
	RawValue   float64    `json:"raw_value"`  // raw factor value before z-score
	ZScore     float64    `json:"z_score"`    // cross-sectional z-score (mean=0, std=1)
	Percentile float64    `json:"percentile"` // percentile rank [0, 100]
}

// ScreenFilters defines filter criteria for stock screening.
type ScreenFilters struct {
	PE_min           *float64 `json:"pe_min"`
	PE_max           *float64 `json:"pe_max"`
	PB_min           *float64 `json:"pb_min"`
	PB_max           *float64 `json:"pb_max"`
	PS_min           *float64 `json:"ps_min"`
	PS_max           *float64 `json:"ps_max"`
	ROE_min          *float64 `json:"roe_min"`
	ROA_min          *float64 `json:"roa_min"`
	DebtToEquity_max *float64 `json:"debt_to_equity_max"`
	GrossMargin_min  *float64 `json:"gross_margin_min"`
	NetMargin_min    *float64 `json:"net_margin_min"`
	MarketCap_min    *float64 `json:"market_cap_min"`
}

// ScreenRequest represents a stock screening request.
type ScreenRequest struct {
	Filters ScreenFilters `json:"filters"`
	Date    string        `json:"date"` // YYYYMMDD, optional - defaults to latest
	Limit   int           `json:"limit"`
}

// ScreenResult represents a single stock screening result.
type ScreenResult struct {
	TsCode   string  `json:"ts_code"`
	PE       *float64 `json:"pe"`
	PB       *float64 `json:"pb"`
	PS       *float64 `json:"ps"`
	ROE      *float64 `json:"roe"`
	ROA      *float64 `json:"roa"`
	DebtToEquity *float64 `json:"debt_to_equity"`
	GrossMargin  *float64 `json:"gross_margin"`
	NetMargin    *float64 `json:"net_margin"`
	MarketCap    *float64 `json:"market_cap"`
}

// Signal represents a trading signal generated by a strategy
type Signal struct {
	Symbol         string             `json:"symbol"`
	Date          time.Time          `json:"date"`
	Direction     Direction          `json:"direction"`
	Strength      float64           `json:"strength"`
	CompositeScore float64          `json:"composite_score"`
	Factors       map[string]float64 `json:"factors"`
	Metadata      map[string]any     `json:"metadata"`
}

// Position represents a single position in the portfolio
type Position struct {
	Symbol           string    `json:"symbol"`
	Quantity         float64   `json:"quantity"`
	AvgCost          float64   `json:"avg_cost"`
	CurrentPrice     float64   `json:"current_price"`
	MarketValue      float64   `json:"market_value"`
	UnrealizedPnL    float64   `json:"unrealized_pnl"`
	RealizedPnL      float64   `json:"realized_pnl"`
	Weight           float64   `json:"weight"`
	EntryDate        time.Time `json:"entry_date"`
	// T+1 Settlement tracking (A-share rule: shares bought today cannot be sold today)
	BuyDate          time.Time `json:"buy_date"`            // Last buy date for this position
	QuantityToday    float64   `json:"quantity_today"`      // Shares bought today (not yet sellable)
	QuantityYesterday float64  `json:"quantity_yesterday"`  // Shares from previous days (sellable today)
}

// Portfolio represents the current portfolio state
type Portfolio struct {
	Cash        float64            `json:"cash"`
	Positions   map[string]Position `json:"positions"`
	TotalValue  float64           `json:"total_value"`
	DailyReturn float64           `json:"daily_return"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// MarketRegime represents detected market regime
type MarketRegime struct {
	Trend      string    `json:"trend"`      // "bull", "bear", "sideways"
	Volatility string    `json:"volatility"` // "low", "medium", "high"
	Sentiment  float64   `json:"sentiment"`  // -1.0 to 1.0
	Timestamp  time.Time `json:"timestamp"`
}

// RiskMetrics contains portfolio risk measurements
type RiskMetrics struct {
	Volatility      float64   `json:"volatility"`
	Beta            float64   `json:"beta"`
	SharpeRatio     float64   `json:"sharpe_ratio"`
	SortinoRatio    float64   `json:"sortino_ratio"`
	MaxDrawdown     float64   `json:"max_drawdown"`
	MaxDrawdownDate time.Time `json:"max_drawdown_date"`
	VaR95           float64   `json:"var_95"`  // Value at Risk (95%)
	CVaR95          float64   `json:"cvar_95"` // Conditional VaR
}

// BacktestResult contains the result of a backtest run
type BacktestResult struct {
	StartDate       time.Time       `json:"start_date"`
	EndDate        time.Time       `json:"end_date"`
	TotalReturn    float64         `json:"total_return"`
	AnnualReturn   float64         `json:"annual_return"`
	SharpeRatio    float64         `json:"sharpe_ratio"`
	SortinoRatio   float64         `json:"sortino_ratio"`
	MaxDrawdown    float64         `json:"max_drawdown"`
	MaxDrawdownDate time.Time      `json:"max_drawdown_date"`
	WinRate        float64         `json:"win_rate"`
	TotalTrades    int             `json:"total_trades"`
	WinTrades      int             `json:"win_trades"`
	LoseTrades     int             `json:"lose_trades"`
	AvgHoldingDays float64         `json:"avg_holding_days"`
	CalmarRatio    float64         `json:"calmar_ratio"`
	PortfolioValues []PortfolioValue `json:"portfolio_values"`
	Trades         []Trade         `json:"trades"`
}

// PortfolioValue represents a single portfolio value snapshot
type PortfolioValue struct {
	Date       time.Time `json:"date"`
	TotalValue float64   `json:"total_value"`
	Cash       float64   `json:"cash"`
	Positions  float64   `json:"positions"`
}

// Trade represents a single trade execution
type Trade struct {
	ID          string    `json:"id"`
	Symbol      string    `json:"symbol"`
	Direction   Direction `json:"direction"`
	Quantity    float64   `json:"quantity"`
	Price       float64   `json:"price"`
	Commission  float64   `json:"commission"`
	TransferFee float64   `json:"transfer_fee"` // 0.001% on both buy and sell (A-share)
	StampTax    float64   `json:"stamp_tax"`    // 0.1% on sell only (A-share)
	Timestamp   time.Time `json:"timestamp"`
	PendingQty  float64   `json:"pending_qty"` // quantity not yet filled from target position
}

// TargetPosition tracks the target vs actual position for execution gap management.
type TargetPosition struct {
	Symbol      string    `json:"symbol"`
	TargetQty   float64   `json:"target_qty"`    // desired quantity from strategy signal
	ActualQty   float64   `json:"actual_qty"`   // what was actually executed
	PendingQty  float64   `json:"pending_qty"`  // quantity not yet filled
	LastUpdated time.Time `json:"last_updated"`
}

// BacktestParams contains parameters for a backtest run
type BacktestParams struct {
	StrategyName   string    `json:"strategy_name"`
	StockPool      []string  `json:"stock_pool"`
	StartDate      time.Time `json:"start_date"`
	EndDate        time.Time `json:"end_date"`
	InitialCapital float64   `json:"initial_capital"`
	RiskFreeRate   float64   `json:"risk_free_rate"`
	LookbackDays   int       `json:"lookback_days"`
}

// Strategy interface must be implemented by all trading strategies
type Strategy interface {
	Name() string
	Description() string
	Configure(config map[string]any) error
	Signals(ctx context.Context, stocks []Stock, ohlcv map[string][]OHLCV, fundamental map[string][]Fundamental, date time.Time) ([]Signal, error)
	Weight(signal Signal, portfolioValue float64) float64
	Cleanup()
}

// MarketDataProvider defines the interface for accessing market data
type MarketDataProvider interface {
	GetOHLCV(ctx context.Context, symbol string, start, end time.Time) ([]OHLCV, error)
	GetFundamental(ctx context.Context, symbol string, date time.Time) (*Fundamental, error)
	GetStocks(ctx context.Context, exchange string) ([]Stock, error)
	GetLatestPrice(ctx context.Context, symbol string) (float64, error)
	GetIndexConstituents(ctx context.Context, indexCode string) ([]string, error)
}

// RiskManager defines the interface for risk management
type RiskManager interface {
	CalculatePosition(ctx context.Context, signal Signal, portfolio *Portfolio, regime *MarketRegime, currentPrice float64) (PositionSize, error)
	DetectRegime(ctx context.Context, ohlcv []OHLCV) (*MarketRegime, error)
	CheckStopLoss(ctx context.Context, positions []Position, prices map[string]float64) ([]StopLossEvent, error)
}

// PositionSize contains the calculated position size
type PositionSize struct {
	Size       float64 `json:"size"`
	Weight     float64 `json:"weight"`
	StopLoss   float64 `json:"stop_loss"`
	TakeProfit float64 `json:"take_profit"`
	RiskScore  float64 `json:"risk_score"`
}

// StopLossEvent represents a stop loss trigger event
type StopLossEvent struct {
	Symbol   string  `json:"symbol"`
	Type     string  `json:"type"` // "stop_loss" or "take_profit"
	Price    float64 `json:"price"`
	Quantity float64 `json:"quantity"`
	Reason   string  `json:"reason"`
}

// Config represents YAML config structure
type Config struct {
	Database DatabaseConfig `json:"database"`
	Redis    RedisConfig    `json:"redis"`
	Services map[string]ServiceConfig `json:"services"`
	Tushare  TushareConfig  `json:"tushare"`
}

// DatabaseConfig holds database connection settings
type DatabaseConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
	SSLMode  string `json:"ssl_mode"`
}

// RedisConfig holds Redis connection settings
type RedisConfig struct {
	URL string `json:"url"`
}

// ServiceConfig holds generic service settings
type ServiceConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// TushareConfig holds tushare API settings
type TushareConfig struct {
	Token     string `json:"token"`
	BaseURL   string `json:"base_url"`
	MaxRetries int   `json:"max_retries"`
}
