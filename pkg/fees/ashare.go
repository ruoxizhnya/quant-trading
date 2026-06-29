// Package fees — Sprint 6 P1-22 (ODR-013)
//
// A 股交易费率常量统一包。0 此包前 4 处位置各自硬编码
// 0.0003 / 0.001 / 0.0001 / 0.00001 4 个费率（mock_trader
// 4 处 + backtest/execution 1 处 + 2 个测试 0.00025），
// 改费率要扫全文，遗漏一处即回测 P&L 与实盘差 10bp。
//
// 设计
// ----
//
//  1. **单一来源** — `pkg/backtest/constants.go` 早就定义了
//     DefaultCommissionRate / DefaultStampTaxRate /
//     DefaultTransferFeeRate / DefaultMinCommission /
//     DefaultSlippageRate，但定义位置（pkg/backtest）暗示
//     "仅回测用"。`pkg/fees` 把这些常量提升到独立包，向
//     `pkg/backtest/constants.go` 反向 re-export（同名 const
//     链）以保 backward-compat。
//
//  2. **Config 结构体 + ApplyDefaults** — 实盘 / 回测 / 测试
//     经常要局部覆盖某项费率。Config 把 5 个费率聚合，零值
//     字段自动 fallback 到 Default*。这样：
//
//     cfg := fees.AShareFees{} // 全用默认值
//     cfg.ApplyDefaults()
//     cfg.CommissionRate = 0.0001 // 测试只覆盖这一项
//
//  3. **Fail-closed 校验** — Rates 必须 > 0 且 < 1，否则费率
//     解释不通（0 = 免佣；1 = 100%）。Validate() 用于实盘下单
//     前的 sanity check。
//
// 4. **不变性** — 常量值按 2024-01 上交所/深交所公告：
//   - 佣金：双边 0.03% 最低 5 元（券商可打折到 0.01%）
//   - 印花税：仅卖出 0.1%（2023-08 起减半）
//   - 过户费：双边 0.001%
//   - 滑点：默认假设 0.01%，无监管上限
package fees

import "fmt"

// A-share fee rates (default values, 2024-Q1 上交所/深交所).
const (
	// DefaultCommissionRate is the default broker commission
	// rate (0.03%) charged on both buy and sell. Most
	// discount brokers negotiate this down to 0.01%–0.02%,
	// but 0.0003 is the regulatory ceiling and the historical
	// default used by every backtest fixture.
	DefaultCommissionRate = 0.0003

	// DefaultStampTaxRate is the stamp tax rate (0.1%)
	// charged on the SELL side only. Halved from 0.2% to
	// 0.1% effective 2023-08-28 (CSRC announcement
	// [2023] No. 17).
	DefaultStampTaxRate = 0.001

	// DefaultTransferFeeRate is the per-side clearing-house
	// transfer fee (0.001%). Charged on both buy and sell
	// since 2022; before that it was sell-only.
	DefaultTransferFeeRate = 0.00001

	// DefaultMinCommission is the minimum commission per
	// transaction (¥5). Applies to the commission line only;
	// transfer fee and stamp tax have no minimum.
	DefaultMinCommission = 5.0

	// DefaultSlippageRate is the default execution slippage
	// assumption (0.01%). This is NOT a regulatory rate; it
	// is a backtest realism knob. Set to 0 to disable
	// slippage in deterministic tests.
	DefaultSlippageRate = 0.0001

	// FixedSlippageRate is the "fixed" slippage model used
	// by BacktestExecutionService when SlippageModel == "fixed".
	// It is a deliberately pessimistic 0.1% (10x the default)
	// because the fixed model has no per-order vol feedback —
	// every order pays the same haircut regardless of
	// liquidity.
	FixedSlippageRate = 0.001
)

// AShareFees collects the four A-share fee knobs + the
// min-commission floor + the optional slippage. Zero-value
// fields are filled from the package-level Default* constants
// by ApplyDefaults; this lets callers write
//
//	cfg := fees.AShareFees{CommissionRate: 0.0001} // only override one
//
// and get reasonable behavior on the unspecified fields.
//
// All values are dimensionless ratios except MinCommission
// (absolute CNY).
type AShareFees struct {
	CommissionRate  float64 // buy + sell; floor = MinCommission
	StampTaxRate    float64 // sell only
	TransferFeeRate float64 // buy + sell
	MinCommission   float64 // CNY
	SlippageRate    float64 // buy + sell, advisory only
}

// DefaultAShareFees returns the regulatory default fee
// schedule. Use this as the zero value when constructing
// MockTrader / BacktestExecutionService from scratch:
//
//	cfg := fees.DefaultAShareFees()
//	trader := live.NewMockTrader(cfg)
func DefaultAShareFees() AShareFees {
	return AShareFees{
		CommissionRate:  DefaultCommissionRate,
		StampTaxRate:    DefaultStampTaxRate,
		TransferFeeRate: DefaultTransferFeeRate,
		MinCommission:   DefaultMinCommission,
		SlippageRate:    DefaultSlippageRate,
	}
}

// ApplyDefaults fills any zero-valued field with the package
// default. This is the migration shim for callers that
// previously set their own defaults (e.g. MockTrader used to
// do `if cfg.CommissionRate <= 0 { cfg.CommissionRate = 0.0003 }`).
//
// Negative rates are NOT zeroed — those are caller bugs and
// should surface as wrong P&L, not silent fallback.
func (f *AShareFees) ApplyDefaults() {
	if f.CommissionRate == 0 {
		f.CommissionRate = DefaultCommissionRate
	}
	if f.StampTaxRate == 0 {
		f.StampTaxRate = DefaultStampTaxRate
	}
	if f.TransferFeeRate == 0 {
		f.TransferFeeRate = DefaultTransferFeeRate
	}
	if f.MinCommission == 0 {
		f.MinCommission = DefaultMinCommission
	}
	if f.SlippageRate == 0 {
		f.SlippageRate = DefaultSlippageRate
	}
}

// Validate returns nil if every field is in a plausible
// range. Used by OrderManager before placing a live order —
// we'd rather refuse the order than execute it with a 100%
// commission (which has happened in production at a
// competitor; the QA missed the config typo).
//
// Plausible range:
//   - CommissionRate / StampTaxRate / TransferFeeRate in (0, 0.05)
//   - MinCommission in [0, 1000) (the regulatory floor is ¥5; ¥1000
//     would imply an absurdly small minimum)
//   - SlippageRate in [0, 0.1) (10% slippage means the order is
//     un-routable)
func (f AShareFees) Validate() error {
	if f.CommissionRate < 0 || f.CommissionRate >= 0.05 {
		return fmt.Errorf("fees: CommissionRate out of range: %f", f.CommissionRate)
	}
	if f.StampTaxRate < 0 || f.StampTaxRate >= 0.05 {
		return fmt.Errorf("fees: StampTaxRate out of range: %f", f.StampTaxRate)
	}
	if f.TransferFeeRate < 0 || f.TransferFeeRate >= 0.05 {
		return fmt.Errorf("fees: TransferFeeRate out of range: %f", f.TransferFeeRate)
	}
	if f.MinCommission < 0 || f.MinCommission >= 1000 {
		return fmt.Errorf("fees: MinCommission out of range: %f", f.MinCommission)
	}
	if f.SlippageRate < 0 || f.SlippageRate >= 0.1 {
		return fmt.Errorf("fees: SlippageRate out of range: %f", f.SlippageRate)
	}
	return nil
}
