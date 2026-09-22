package backtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/fees"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AUD-22 — the two wiring guards for the risk-warning daily buy cap.
//
// The tracker has its own behavioural tests (pkg/backtest/tracker/
// tracker_riskwarning_test.go) for the RULE. These two cover the WIRING,
// which is where the rule can be correct and still never fire.

// TestEngine_RiskWarningDailyBuyCap_ApplyTradePath is the guard for the
// PRODUCTION entry point, and it is the non-obvious half of AUD-22.
//
// NewEngine unconditionally installs an execution service
// (executionBridge.Set(executionService)), so `useExecutionService` is
// true for every direction other than Hold, and a buy reaches the
// portfolio through executeViaExecutionService -> Tracker.ApplyTrade —
// NOT through Tracker.ExecuteTrade. A cap enforced only in ExecuteTrade
// would pass every tracker unit test and do nothing in production.
//
// This drives the real execution service and then ApplyTrade, with the
// name table populated the way the engine populates it.
func TestEngine_RiskWarningDailyBuyCap_ApplyTradePath(t *testing.T) {
	const symbol = "830799.BJ" // 北交所，上限 20 万股

	logger := zerolog.Nop()
	tr := NewTracker(
		100_000_000,
		fees.DefaultCommissionRate,
		fees.DefaultSlippageRate,
		defaultTradingConfig(),
		logger,
	)
	tr.SetStockNames(map[string]string{symbol: "*ST田野"})

	execSvc := NewBacktestExecutionService(domain.ExecutionConfig{
		OrderType:      domain.OrderTypeMarket,
		SlippageModel:  "fixed",
		CommissionRate: fees.DefaultCommissionRate,
		MinCommission:  5.0,
		InitialCapital: 100_000_000,
	})

	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	quote := Quote{
		Symbol: symbol,
		Open:   1.0, High: 1.0, Low: 1.0, Close: 1.0,
		Volume: 1_000_000_000, Date: at,
	}

	submit := func(qty float64) error {
		t.Helper()
		trade, err := execSvc.ExecuteOrder(domain.Order{
			Symbol:    symbol,
			Direction: domain.DirectionLong,
			OrderType: domain.OrderTypeMarket,
			Quantity:  qty,
			Timestamp: at,
		}, quote)
		require.NoError(t, err, "the execution service itself must not refuse this")
		_, err = tr.ApplyTrade(trade)
		return err
	}

	require.NoError(t, submit(marketdata.RiskWarningDailyBuyCapBSE),
		"the first order is exactly at the cap and must be accepted")

	err := submit(100)
	require.Error(t, err,
		"ApplyTrade is the production path — the daily buy cap must be enforced there, "+
			"not only in ExecuteTrade")
	assert.Contains(t, err.Error(), "daily buy cap exceeded")

	// The rejected order must have left the portfolio alone.
	pos, exists := tr.GetPosition(symbol)
	require.True(t, exists)
	assert.InDelta(t, marketdata.RiskWarningDailyBuyCapBSE, pos.Quantity, 1e-9,
		"a rejected order must not change the position")
}

// TestEngine_DayLoopPopulatesTrackerStockNames is a STRUCTURAL guard, and
// deliberately weaker than the behavioural tests: it asserts the call
// still EXISTS in engine.go, not that it runs at the right moment.
//
// Its job is narrow but real — turning "someone deleted the wiring" from a
// silent no-op into a red test. The cap fails open when the name table is
// empty (an unknown name must not block trading), so without this guard
// the failure mode is indistinguishable from "this backtest has no
// risk-warning stocks".
//
// Parsed as an AST rather than matched as text, because a text guard
// cannot tell a call from a mention: the phrase survives being commented
// out, quoted in a doc comment, or moved into a string. That mistake was
// already made once in this project — see .workbuddy-ai/memory/PITFALLS.md
// §23 (the first version of the AUD-29 gin guard was fooled by its own
// doc comment).
func TestEngine_DayLoopPopulatesTrackerStockNames(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "engine.go", nil, parser.ParseComments)
	require.NoError(t, err, "engine.go must parse")

	var calls []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "SetStockNames" {
			calls = append(calls, fset.Position(call.Pos()).String())
		}
		return true
	})

	assert.NotEmpty(t, calls,
		"engine.go must keep calling Tracker.SetStockNames in the day loop "+
			"(currently around the fetchMarketDataForDay call). Without it the "+
			"risk-warning daily buy cap is a silent no-op: the tracker fails open "+
			"on an unknown stock name by design, so nothing else would go red.")

	// Exactly one population point. Two would mean two sources of truth for
	// the same table, and the later one would silently win.
	assert.Len(t, calls, 1,
		"expected exactly one SetStockNames call site in engine.go, got %v", calls)
}
