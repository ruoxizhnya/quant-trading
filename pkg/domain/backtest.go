package domain

import "time"

type BacktestResult struct {
	StartDate       time.Time        `json:"start_date"`
	EndDate         time.Time        `json:"end_date"`
	TotalReturn     float64          `json:"total_return"`
	AnnualReturn    float64          `json:"annual_return"`
	SharpeRatio     float64          `json:"sharpe_ratio"`
	SortinoRatio    float64          `json:"sortino_ratio"`
	MaxDrawdown     float64          `json:"max_drawdown"`
	MaxDrawdownDate time.Time        `json:"max_drawdown_date"`
	WinRate         float64          `json:"win_rate"`
	TotalTrades     int              `json:"total_trades"`
	WinTrades       int              `json:"win_trades"`
	LoseTrades      int              `json:"lose_trades"`
	AvgHoldingDays  float64          `json:"avg_holding_days"`
	CalmarRatio     float64          `json:"calmar_ratio"`
	PortfolioValues []PortfolioValue `json:"portfolio_values"`
	Trades          []Trade          `json:"trades"`
}

type PortfolioValue struct {
	Date       time.Time `json:"date"`
	TotalValue float64   `json:"total_value"`
	Cash       float64   `json:"cash"`
	Positions  float64   `json:"positions"`
}

type Trade struct {
	ID          string    `json:"id"`
	Symbol      string    `json:"symbol"`
	Direction   Direction `json:"direction"`
	Quantity    float64   `json:"quantity"`
	Price       float64   `json:"price"`
	Commission  float64   `json:"commission"`
	TransferFee float64   `json:"transfer_fee"`
	StampTax    float64   `json:"stamp_tax"`
	Timestamp   time.Time `json:"timestamp"`
	PendingQty  float64   `json:"pending_qty"`
	FilledQty   float64   `json:"filled_qty"`
}

type BacktestParams struct {
	StrategyName   string    `json:"strategy_name"`
	StockPool      []string  `json:"stock_pool"`
	StartDate      time.Time `json:"start_date"`
	EndDate        time.Time `json:"end_date"`
	InitialCapital float64   `json:"initial_capital"`
	RiskFreeRate   float64   `json:"risk_free_rate"`
	LookbackDays   int       `json:"lookback_days"`
}

type WalkForwardParams struct {
	TrainDays    int
	TestDays     int
	StepDays     int
	MinTrainDays int
}

type WalkForwardResult struct {
	WindowIndex     int             `json:"window_index"`
	TrainStart      string          `json:"train_start"`
	TrainEnd        string          `json:"train_end"`
	TestStart       string          `json:"test_start"`
	TestEnd         string          `json:"test_end"`
	TrainResult     *BacktestResult `json:"train_result,omitempty"`
	TestResult      *BacktestResult `json:"test_result,omitempty"`
	TrainSharpe     float64         `json:"train_sharpe"`
	TestSharpe      float64         `json:"test_sharpe"`
	TestReturn      float64         `json:"test_return"`
	TestMaxDrawdown float64         `json:"test_max_drawdown"`
	OOSvsTrain      float64         `json:"oos_vs_train"`
}

type WalkForwardReport struct {
	StrategyID     string               `json:"strategy_id"`
	Universe       string               `json:"universe"`
	Windows        []*WalkForwardResult `json:"windows"`
	AvgTestSharpe  float64              `json:"avg_test_sharpe"`
	AvgTestReturn  float64              `json:"avg_test_return"`
	AvgTestMaxDD   float64              `json:"avg_test_max_drawdown"`
	AvgDegradation float64              `json:"avg_degradation"`
	PassRate       float64              `json:"pass_rate"`
	OverallPass    bool                 `json:"overall_pass"`

	StdTestSharpe  float64 `json:"std_test_sharpe"`
	StdDegradation float64 `json:"std_degradation"`

	OverfitScore   float64 `json:"overfit_score"`
	ProbNoOverfit  float64 `json:"prob_no_overfit"`
	StabilityScore float64 `json:"stability_score"`
}
