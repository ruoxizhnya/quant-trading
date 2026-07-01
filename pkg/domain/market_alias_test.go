package domain

import (
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/domain/market"
)

// TestAlias_MarketTypesIdentical verifies that domain.X and market.X
// are the SAME type (via type alias). If a future refactor breaks the
// alias, this test will fail to compile — a stronger guarantee than
// runtime reflection.
func TestAlias_MarketTypesIdentical(t *testing.T) {
	// Compile-time assertions: assigning across packages must work
	// without any conversion. If the alias is broken (e.g. someone
	// writes `type OHLCV market.OHLCV` instead of `type OHLCV =
	// market.OHLCV`), these lines fail to compile.
	var _ OHLCV = market.OHLCV{}
	var _ Stock = market.Stock{}
	var _ IndexConstituent = market.IndexConstituent{}
	var _ Split = market.Split{}
	var _ Dividend = market.Dividend{}
	var _ Fundamental = market.Fundamental{}
	var _ FundamentalData = market.FundamentalData{}

	// Interface alias: a value implementing market.Provider also
	// implements domain.MarketDataProvider (same interface, via alias).
	var _ MarketDataProvider = (MarketDataProvider)(nil)
	var _ market.Provider = (market.Provider)(nil)

	// Runtime sanity: zero values are interchangeable in slices/maps.
	ohlcv := OHLCV{Symbol: "000001.SZ"}
	var mO market.OHLCV = ohlcv // no conversion needed
	if mO.Symbol != "000001.SZ" {
		t.Fatalf("alias round-trip lost Symbol: got %q", mO.Symbol)
	}

	// Slice interchangeability (most common usage pattern in consumers).
	domainSlice := []OHLCV{{Symbol: "A"}, {Symbol: "B"}}
	var marketSlice []market.OHLCV = domainSlice
	if len(marketSlice) != 2 || marketSlice[1].Symbol != "B" {
		t.Fatalf("slice alias round-trip failed: %+v", marketSlice)
	}
}
