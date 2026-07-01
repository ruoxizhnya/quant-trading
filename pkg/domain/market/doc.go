// Package market contains market-data domain types for the quant
// trading system.
//
// This sub-package was introduced by S7-P3-4 (ODR-043 D3 "soft layering")
// as the canonical home for market-data types. The legacy
// `pkg/domain` package re-exports these types as type aliases, so
// existing code referencing `domain.OHLCV` continues to compile
// unchanged. New code SHOULD prefer the `market.OHLCV` spelling.
//
// Soft layering means the two packages coexist (no big-bang rename):
//   - market.OHLCV  — canonical definition (this package)
//   - domain.OHLCV  — type alias (= market.OHLCV), backward compat
//
// Both spellings refer to the SAME type; they are interchangeable in
// assignments, function signatures, and type assertions.
package market
