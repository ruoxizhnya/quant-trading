// Package settlement — S7-P1-1 (ODR-043 D1)
//
// A 股 T+1 交收规则共享原语。tracker.go (pkg/backtest) 和
// mock_trader.go (pkg/live) 之前各自重复实现 T+1 交收逻辑（3 处
// RollOver + 3 处卖空限制 + 6+ 处 IsFlat 检查），S7-P0-17 修复的
// ghost zero-quantity position bug 正是因为两个执行器的 IsFlat
// 阈值和清理逻辑不一致。
//
// 本包抽取 T+1 交收纯函数原语，确保两个执行器共享同一份规则。
package settlement

import "math"

// FlatThreshold is the absolute quantity below which a position is
// considered flat (zero). This matches the 1e-8 threshold used in
// S7-P0-17's ghost-position guard.
const FlatThreshold = 1e-8

// IsFlat returns true if the position quantity is effectively zero
// (|quantity| < FlatThreshold). Both tracker.go and mock_trader.go
// must use this to decide when to delete a position from their maps,
// preventing the ghost-position class of bugs (S7-P0-17).
func IsFlat(quantity float64) bool {
	return math.Abs(quantity) < FlatThreshold
}

// RollOver advances the T+1 settlement state by one trading day.
// Shares bought today (QuantityToday) become sellable tomorrow
// (QuantityYesterday). Returns (newYesterday, newToday) where
// newYesterday = oldToday and newToday = 0.
//
// This is the core of AdvanceDay in both tracker.go and mock_trader.go.
// Note: the caller is still responsible for short-selling interest
// accrual (tracker-only feature) and any daily-value recording — this
// function is purely the T+1 quantity rollover.
func RollOver(qtyYesterday, qtyToday float64) (newYesterday, newToday float64) {
	return qtyToday, 0
}

// SellableQty returns the quantity of a position that can be sold
// under T+1 settlement rules. A-share T+1 means only shares held
// since yesterday (QuantityYesterday) can be sold today; shares
// bought today (QuantityToday) are locked until the next trading day.
//
// This is the T+1 sell guard. Both tracker.go (2 sites: ExecuteTrade
// and ApplyTrade) and mock_trader.go (1 site) must use this to enforce
// the rule consistently.
func SellableQty(qtyYesterday float64) float64 {
	if qtyYesterday < 0 {
		return 0
	}
	return qtyYesterday
}
