// Package statistics provides reusable numerical helpers for the
// quantitative trading system. It is the **single source of truth**
// for descriptive statistics that used to be copy-pasted across at
// least six packages (backtest, risk, strategy, ai, data, ...).
//
// Design rules:
//
//  1. Pure functions. No globals, no I/O, no logging. Each function
//     is deterministic and side-effect-free so callers can use them
//     inside backtest loops without a logger dependency.
//
//  2. NaN / Inf policy. Statistics over float64 slices **never
//     silently drop** non-finite values. Use the WithNaN variant
//     (e.g. MeanNonFiniteSafe) when the caller explicitly wants to
//     filter. This avoids the historical bug class "factor has
//     hidden NaN → std = NaN → sharpe = NaN → ranking inverted".
//
//  3. Population vs sample stddev. The function names make the
//     denominator explicit: PopulationStdDev divides by n, SampleStdDev
//     divides by n-1. There is no "stddev" overload — picking the
//     wrong denominator is a classic quant bug.
//
//  4. Empty / single-element slices. Return 0 (not NaN) so callers
//     can `if s == 0 { skip }` without panicking. Higher-level
//     functions (Pearson, etc.) may return NaN to signal "not
//     computable" — see their doc comments.
package statistics
