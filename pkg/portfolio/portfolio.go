// Package portfolio — S7-P1-1 (ODR-043 D1)
//
// 共享仓位/费率计算原语。tracker.go (pkg/backtest) 和 mock_trader.go
// (pkg/live) 之前各自重复实现相同的费率公式和均价计算逻辑（8 处费率
// 公式 + 3 处均价计算），改费率公式要扫两个文件，遗漏一处即回测 P&L
// 与实盘差 bp。
//
// 本包抽取纯计算原语，让两个执行器共享同一份公式。原语均为无状态
// 纯函数，不依赖任何 I/O，便于单元测试。
package portfolio

import (
	"math"

	"github.com/ruoxizhnya/quant-trading/pkg/fees"
)

// FeeBreakdown represents the per-trade fee components for A-share
// transactions. All values are in CNY.
type FeeBreakdown struct {
	Commission  float64 // broker commission (floor = MinCommission)
	TransferFee float64 // clearing-house transfer fee
	StampTax    float64 // stamp tax (sell only; 0 for buys)
}

// Total returns the sum of all fee components.
func (f FeeBreakdown) Total() float64 {
	return f.Commission + f.TransferFee + f.StampTax
}

// ComputeFees calculates the fee breakdown for a trade of the given
// value. isSell must be true for sells (DirectionClose / DirectionShort)
// and false for buys (DirectionLong), because the stamp tax is charged
// on the sell side only.
//
// The formulas mirror the A-share regulatory schedule:
//
//	commission  = max(tradeValue * CommissionRate, MinCommission)
//	transferFee = tradeValue * TransferFeeRate
//	stampTax    = tradeValue * StampTaxRate   (sell only)
//
// A negative or zero tradeValue produces zero fees (defensive — callers
// should not pass negative values, but we fail-safe rather than produce
// negative fees).
func ComputeFees(tradeValue float64, isSell bool, f fees.AShareFees) FeeBreakdown {
	if tradeValue <= 0 {
		return FeeBreakdown{}
	}
	commission := math.Max(tradeValue*f.CommissionRate, f.MinCommission)
	transferFee := tradeValue * f.TransferFeeRate
	var stampTax float64
	if isSell {
		stampTax = tradeValue * f.StampTaxRate
	}
	return FeeBreakdown{
		Commission:  commission,
		TransferFee: transferFee,
		StampTax:    stampTax,
	}
}

// UpdateAvgCost computes the new average cost and quantity after a buy
// that increases (or opens) a long position, or after a short that
// increases (or opens) a short position.
//
// Returns (newAvgCost, newQuantity). If the new quantity is effectively
// zero (the buy exactly offsets an existing short, or vice versa — the
// "ghost position" scenario from S7-P0-17), returns (0, 0) so the caller
// can delete the position from its map.
//
// oldQty may be negative (existing short); filledQty is always positive.
// The caller is responsible for sign conventions: for a long buy,
// pass (oldAvgCost, oldQty, fillPrice, +filledQty); for a short sell
// that increases a short, pass (oldAvgCost, oldQty, fillPrice, -filledQty)
// — but in practice both trackers call this with the magnitude and handle
// the sign externally. The formula is sign-agnostic for the weighted
// average.
func UpdateAvgCost(oldAvgCost, oldQty, fillPrice, filledQty float64) (newAvgCost, newQty float64) {
	newQty = oldQty + filledQty
	if math.Abs(newQty) < 1e-8 {
		// Position is now flat — signal deletion (S7-P0-17 ghost position guard).
		return 0, 0
	}
	newAvgCost = (oldAvgCost*oldQty + fillPrice*filledQty) / newQty
	return newAvgCost, newQty
}
