package live

import (
	"math"
	"os"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/fees"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- S7-P0-5 (ODR-043): SimulatedBroker fee consistency ----
//
// simulated_broker.go:151 previously hardcoded commission = max(amount*0.00025, 5.0).
// The 0.00025 rate (0.025%) is neither the regulatory A-share commission
// ceiling (0.03% = 0.0003, in pkg/fees.DefaultCommissionRate) nor any
// documented discount tier. This silently diverges from MockTrader and
// the backtest engine, both of which source commission from pkg/fees.
// These tests verify the broker now sources its commission rate and
// minimum from the pkg/fees package.

// TestSimulatedBroker_DefaultFeesConfig verifies a newly constructed
// broker carries the default A-share fee schedule from pkg/fees.
func TestSimulatedBroker_DefaultFeesConfig(t *testing.T) {
	broker := NewSimulatedBroker(1_000_000)

	defaults := fees.DefaultAShareFees()
	assert.InDelta(t, defaults.CommissionRate, broker.fees.CommissionRate, 1e-12,
		"broker should default to fees.DefaultCommissionRate")
	assert.InDelta(t, defaults.MinCommission, broker.fees.MinCommission, 1e-12,
		"broker should default to fees.DefaultMinCommission")
}

// TestSimulatedBroker_BuyCommissionUsesFeesPackage verifies a buy
// order deducts commission = max(amount * fees.DefaultCommissionRate,
// fees.DefaultMinCommission). With a large order the proportional
// branch dominates, exposing any rate mismatch (0.00025 vs 0.0003).
func TestSimulatedBroker_BuyCommissionUsesFeesPackage(t *testing.T) {
	const initialBalance = 1_000_000.0
	broker := NewSimulatedBroker(initialBalance)
	require.NoError(t, broker.Connect())

	const qty = 10000.0
	const limitPrice = 100.0

	orderID, err := broker.SubmitOrder(domain.Order{
		Symbol:     "600519.SH",
		Direction:  domain.DirectionLong,
		OrderType:  domain.OrderTypeMarket,
		Quantity:   qty,
		LimitPrice: limitPrice,
	})
	require.NoError(t, err)

	// Retrieve the filled order to read the actual fill price (which
	// includes slippage). The test is in package live, so we can reach
	// the orders map directly.
	filled := broker.orders[orderID]
	require.Equal(t, "filled", filled.Status)
	require.NotZero(t, filled.FillPrice)

	amount := filled.FillPrice * filled.Quantity
	expectedCommission := math.Max(amount*fees.DefaultCommissionRate, fees.DefaultMinCommission)
	expectedBalance := initialBalance - amount - expectedCommission

	balance, err := broker.GetAccountBalance()
	require.NoError(t, err)
	assert.InDelta(t, expectedBalance, balance, 1e-6,
		"buy balance must reflect commission at fees.DefaultCommissionRate (=0.0003), not the old hardcoded 0.00025")
}

// TestSimulatedBroker_SellCommissionUsesFeesPackage verifies a sell
// order deducts the same pkg/fees-sourced commission from proceeds.
func TestSimulatedBroker_SellCommissionUsesFeesPackage(t *testing.T) {
	const initialBalance = 1_000_000.0
	broker := NewSimulatedBroker(initialBalance)
	require.NoError(t, broker.Connect())

	// Seed a long position so the sell doesn't push quantity negative
	// in a way that distorts the balance check.
	broker.positions["600519.SH"] = domain.Position{Symbol: "600519.SH", Quantity: 10000, AvgCost: 100}

	const qty = 10000.0
	const limitPrice = 100.0

	orderID, err := broker.SubmitOrder(domain.Order{
		Symbol:     "600519.SH",
		Direction:  domain.DirectionShort,
		OrderType:  domain.OrderTypeMarket,
		Quantity:   qty,
		LimitPrice: limitPrice,
	})
	require.NoError(t, err)

	filled := broker.orders[orderID]
	require.Equal(t, "filled", filled.Status)

	amount := filled.FillPrice * filled.Quantity
	expectedCommission := math.Max(amount*fees.DefaultCommissionRate, fees.DefaultMinCommission)
	// Sell credits amount and deducts commission.
	expectedBalance := initialBalance + amount - expectedCommission

	balance, err := broker.GetAccountBalance()
	require.NoError(t, err)
	assert.InDelta(t, expectedBalance, balance, 1e-6,
		"sell balance must reflect commission at fees.DefaultCommissionRate (=0.0003), not the old hardcoded 0.00025")
}

// TestSimulatedBroker_MinCommissionFloor verifies small orders pay
// the minimum commission floor (¥5) rather than a sub-floor
// proportional amount. This locks in the max() behaviour.
func TestSimulatedBroker_MinCommissionFloor(t *testing.T) {
	const initialBalance = 1_000_000.0
	broker := NewSimulatedBroker(initialBalance)
	require.NoError(t, broker.Connect())

	// Tiny order: 1 share at ~100 → amount ~100. Proportional commission
	// would be 100 * 0.0003 = 0.03, far below the ¥5 floor.
	const qty = 1.0
	const limitPrice = 100.0

	orderID, err := broker.SubmitOrder(domain.Order{
		Symbol:     "000001.SZ",
		Direction:  domain.DirectionLong,
		OrderType:  domain.OrderTypeMarket,
		Quantity:   qty,
		LimitPrice: limitPrice,
	})
	require.NoError(t, err)

	filled := broker.orders[orderID]
	amount := filled.FillPrice * filled.Quantity
	// The floor must come from fees.DefaultMinCommission (5.0), not a
	// separate hardcoded literal.
	expectedCommission := math.Max(amount*fees.DefaultCommissionRate, fees.DefaultMinCommission)
	assert.InDelta(t, fees.DefaultMinCommission, expectedCommission, 1e-12,
		"small order should pay the min commission floor")

	expectedBalance := initialBalance - amount - expectedCommission
	balance, err := broker.GetAccountBalance()
	require.NoError(t, err)
	assert.InDelta(t, expectedBalance, balance, 1e-6)
}

// TestSimulatedBroker_SourceHasNoHardcodedRate is a regression guard:
// the source must not re-introduce the 0.00025 literal that diverged
// from pkg/fees. The commission rate must be sourced from the fees
// struct, not inlined.
func TestSimulatedBroker_SourceHasNoHardcodedRate(t *testing.T) {
	source, err := os.ReadFile("simulated_broker.go")
	require.NoError(t, err)
	assert.NotContains(t, string(source), "0.00025",
		"simulated_broker.go must not hardcode 0.00025; source commission from pkg/fees (S7-P0-5)")
}
