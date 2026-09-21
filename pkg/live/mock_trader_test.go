package live

import (
	"context"
	"math"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AUD-10: GetPositions / GetAccount used to write the refreshed price
// back into the stored *PositionInfo while holding only an RLock. Two
// concurrent readers — the periodic alert loop and an HTTP handler,
// say — would race on the same field, and the stored position ended up
// holding whichever price the last reader fetched.
//
// These tests observe the stored position directly (same package), so
// they catch the write-through without needing the race detector. The
// race detector is a second line of defence (see
// TestMockTrader_ConcurrentReads), but it cannot run on this machine:
// -race needs cgo + gcc, and there is no gcc here.

const (
	auditSymbol      = "600000.SH"
	auditOrderPrice  = 10.0 // the price we pass on the order
	auditMarketPrice = 20.0 // what the PriceProvider reports afterwards
)

// newAuditTrader builds a MockTrader holding one 100-share position and
// returns the trader plus the price the position was actually filled
// at.
//
// Two traps make the fill price hard to predict, so we read it back
// rather than assuming:
//
//   - A market order is priced from the PriceProvider, ignoring the
//     price argument (SubmitOrder). We submit a limit order instead.
//   - MockTraderConfig treats <= 0 as "unset" and substitutes the
//     pkg/fees defaults, so a zero slippage rate becomes 0.0001 and the
//     fill lands slightly above the order price.
func newAuditTrader(t *testing.T, priceProvider func(string) float64) (*MockTrader, float64) {
	t.Helper()

	mt := NewMockTrader(MockTraderConfig{
		InitialCash:   1_000_000,
		PriceProvider: priceProvider,
	}, zerolog.Nop())

	_, err := mt.SubmitOrder(
		context.Background(), auditSymbol,
		domain.DirectionLong, domain.OrderTypeLimit,
		100, auditOrderPrice,
	)
	require.NoError(t, err)

	stored := mt.positions[auditSymbol]
	require.NotNil(t, stored, "precondition: position should exist after the buy")
	require.InDelta(t, 100, stored.Quantity, 1e-9, "precondition: quantity")

	fillPrice := stored.CurrentPrice
	require.Greater(t, fillPrice, 0.0, "precondition: position carries a price")

	// The market price must be distinguishable from the fill price,
	// otherwise these tests could not tell "refreshed" from "unchanged".
	require.Greater(t, math.Abs(fillPrice-auditMarketPrice), 1e-6,
		"precondition: provider price must differ from the fill price")

	return mt, fillPrice
}

// TestMockTrader_GetPositions_DoesNotWriteThrough is the core guard:
// reading must not mutate stored state.
func TestMockTrader_GetPositions_DoesNotWriteThrough(t *testing.T) {
	t.Parallel()

	mt, fillPrice := newAuditTrader(t, func(string) float64 { return auditMarketPrice })

	positions, err := mt.GetPositions(context.Background())
	require.NoError(t, err)
	require.Len(t, positions, 1)

	// The caller sees the refreshed price…
	assert.InDelta(t, auditMarketPrice, positions[0].CurrentPrice, 1e-9,
		"returned snapshot should carry the PriceProvider's price")
	assert.InDelta(t, auditMarketPrice*100, positions[0].MarketValue, 1e-9,
		"market value should be recomputed from the refreshed price")
	assert.InDelta(t, (auditMarketPrice-fillPrice)*100, positions[0].UnrealizedPnL, 1e-9,
		"unrealized PnL should be recomputed from the refreshed price")

	// …but the stored position must be untouched. These are the
	// assertions that fail against the old write-through implementation.
	assert.InDelta(t, fillPrice, mt.positions[auditSymbol].CurrentPrice, 1e-9,
		"GetPositions must not write through to the stored position")
	assert.InDelta(t, fillPrice*100, mt.positions[auditSymbol].MarketValue, 1e-9,
		"GetPositions must not overwrite the stored market value")
	assert.InDelta(t, 0, mt.positions[auditSymbol].UnrealizedPnL, 1e-9,
		"GetPositions must not overwrite the stored unrealized PnL")
}

// TestMockTrader_GetAccount_DoesNotWriteThrough — same contract for the
// account path, which the periodic alert loop calls.
func TestMockTrader_GetAccount_DoesNotWriteThrough(t *testing.T) {
	t.Parallel()

	mt, fillPrice := newAuditTrader(t, func(string) float64 { return auditMarketPrice })

	account, err := mt.GetAccount(context.Background())
	require.NoError(t, err)

	assert.InDelta(t, auditMarketPrice*100, account.MarketValue, 1e-9,
		"account should be marked at the refreshed price")
	assert.InDelta(t, (auditMarketPrice-fillPrice)*100, account.UnrealizedPnL, 1e-9)

	assert.InDelta(t, fillPrice, mt.positions[auditSymbol].CurrentPrice, 1e-9,
		"GetAccount must not write through to the stored position")
	assert.InDelta(t, fillPrice*100, mt.positions[auditSymbol].MarketValue, 1e-9,
		"GetAccount must not overwrite the stored market value")
}

// TestMockTrader_ReadsAreRepeatable — with a constant provider, two
// reads must agree exactly. With write-through the second read would
// start from a position the first read had already rewritten.
func TestMockTrader_ReadsAreRepeatable(t *testing.T) {
	t.Parallel()

	mt, _ := newAuditTrader(t, func(string) float64 { return auditMarketPrice })

	ctx := context.Background()

	first, err := mt.GetPositions(ctx)
	require.NoError(t, err)

	second, err := mt.GetPositions(ctx)
	require.NoError(t, err)

	assert.Equal(t, first, second,
		"repeated reads with a constant provider must return identical snapshots")

	firstAccount, err := mt.GetAccount(ctx)
	require.NoError(t, err)
	secondAccount, err := mt.GetAccount(ctx)
	require.NoError(t, err)

	assert.Equal(t, firstAccount.MarketValue, secondAccount.MarketValue)
	assert.Equal(t, firstAccount.UnrealizedPnL, secondAccount.UnrealizedPnL)
}

// TestMockTrader_ReturnedSliceIsIndependent — mutating what the caller
// got back must not leak into the trader.
func TestMockTrader_ReturnedSliceIsIndependent(t *testing.T) {
	t.Parallel()

	mt, _ := newAuditTrader(t, func(string) float64 { return auditMarketPrice })

	first, err := mt.GetPositions(context.Background())
	require.NoError(t, err)
	require.Len(t, first, 1)

	first[0].Quantity = 99999
	first[0].CurrentPrice = 0.01

	second, err := mt.GetPositions(context.Background())
	require.NoError(t, err)
	require.Len(t, second, 1)

	assert.InDelta(t, 100, second[0].Quantity, 1e-9,
		"mutating a returned snapshot must not affect later reads")
	assert.InDelta(t, auditMarketPrice, second[0].CurrentPrice, 1e-9)
}

// TestMockTrader_ConcurrentReads exercises the readers concurrently.
//
// Note: on a machine without gcc this test cannot run under -race, so
// it only proves the results stay consistent — the write-through bug is
// caught by the assertions above, which inspect stored state directly.
// Under CI's -race build (AUD-12) this is the test that would have
// flagged the original code.
func TestMockTrader_ConcurrentReads(t *testing.T) {
	t.Parallel()

	mt, fillPrice := newAuditTrader(t, func(string) float64 { return auditMarketPrice })

	const (
		goroutines = 8
		iterations = 50
	)

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			ctx := context.Background()
			for j := 0; j < iterations; j++ {
				if id%2 == 0 {
					positions, err := mt.GetPositions(ctx)
					if err != nil {
						t.Errorf("GetPositions: %v", err)
						return
					}
					if len(positions) != 1 {
						t.Errorf("got %d positions, want 1", len(positions))
						return
					}
					if positions[0].CurrentPrice != auditMarketPrice {
						t.Errorf("price = %v, want %v",
							positions[0].CurrentPrice, auditMarketPrice)
						return
					}
				} else {
					account, err := mt.GetAccount(ctx)
					if err != nil {
						t.Errorf("GetAccount: %v", err)
						return
					}
					if account.MarketValue != auditMarketPrice*100 {
						t.Errorf("market value = %v, want %v",
							account.MarketValue, auditMarketPrice*100)
						return
					}
				}
			}
		}(i)
	}

	wg.Wait()

	// After all that concurrent reading, the stored position must still
	// be exactly as it was written.
	assert.InDelta(t, fillPrice, mt.positions[auditSymbol].CurrentPrice, 1e-9,
		"concurrent reads must leave stored state untouched")
}

// TestMockTrader_NoPriceProviderLeavesStoredValuesAlone covers the
// no-provider path: without a provider there is nothing to refresh, so
// the snapshot must equal the stored position.
func TestMockTrader_NoPriceProviderLeavesStoredValuesAlone(t *testing.T) {
	t.Parallel()

	mt, fillPrice := newAuditTrader(t, nil)

	positions, err := mt.GetPositions(context.Background())
	require.NoError(t, err)
	require.Len(t, positions, 1)

	assert.InDelta(t, fillPrice, positions[0].CurrentPrice, 1e-9,
		"without a provider the stored price is returned as-is")
	assert.InDelta(t, fillPrice, mt.positions[auditSymbol].CurrentPrice, 1e-9)
}

// TestMockTrader_SnapshotRefreshesPriceOnly — the snapshot helper must
// refresh the price but leave the cost basis and quantity alone, so
// PnL stays meaningful.
func TestMockTrader_SnapshotRefreshesPriceOnly(t *testing.T) {
	t.Parallel()

	mt, fillPrice := newAuditTrader(t, func(string) float64 { return auditMarketPrice })

	positions, err := mt.GetPositions(context.Background())
	require.NoError(t, err)
	require.Len(t, positions, 1)

	got := positions[0]
	assert.InDelta(t, auditMarketPrice, got.CurrentPrice, 1e-9, "price is refreshed")
	assert.InDelta(t, fillPrice, got.AvgCost, 1e-9, "cost basis is not touched")
	assert.InDelta(t, 100, got.Quantity, 1e-9, "quantity is not touched")
	assert.InDelta(t, 100, got.QuantityToday, 1e-9, "T+1 buckets are not touched")
}
